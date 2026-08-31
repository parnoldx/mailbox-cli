package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// Sieve is the ManageSieve surface the Routing needs. It is small because the
// Routing is one script: read it whole, write it whole.
type Sieve interface {
	// Scripts lists the stored script names and which one is active.
	Scripts(ctx context.Context) (names []string, active string, err error)
	// Script fetches one by name.
	Script(ctx context.Context, name string) (string, error)
	// PutScript stores one and, if asked, makes it the active one.
	PutScript(ctx context.Context, name, content string, activate bool) error
	// SetActive makes a script already on the server the active one.
	SetActive(ctx context.Context, name string) error
}

// screenerScan is how much of the Screener a decision looks at. The Screener is
// a pile of undecided senders, not an archive: a Screener with more than this in
// it is telling you something that a longer scan would not.
const screenerScan = 1000

// routingLoop keeps the Mirror's copy of the Routing true. The script is
// rewritten by this program and by nobody else in the ordinary case, but a rule
// added in webmail is exactly the sort of thing a caller should not have to
// restart the daemon to see.
func (d *Daemon) routingLoop(ctx context.Context) {
	if d.Sieve == nil {
		return
	}
	every := d.RoutingEvery
	if every <= 0 {
		every = 10 * time.Minute
	}
	for {
		if err := d.refreshRouting(ctx); err != nil {
			d.logf("routing: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// routingState is the Routing as the server currently has it: the lists, and
// the facts about the script that a write has to know before it touches
// anything.
type routingState struct {
	lists  *routing.Lists
	raw    string
	exists bool
	// active is the name of the script the server is running, which is
	// usually not ours.
	active string
	// inForce is whether our script actually runs: it is the active one, or the
	// active one includes it. A script that is stored and unreachable routes
	// nothing, and writing to it would be writing into a drawer.
	inForce bool
	// activate is whether making ours the active script would switch nothing
	// else off. True only when the server is running no script at all.
	activate bool
}

// readRouting fetches the script and parses it. A server with no `logic` script
// is not an error: it is an account with no Routing yet, which is what a fresh
// one looks like.
func (d *Daemon) readRouting(ctx context.Context) (routingState, error) {
	if d.Sieve == nil {
		return routingState{}, errors.New("this daemon has no sieve connection: routing is not configured")
	}
	names, active, err := d.Sieve.Scripts(ctx)
	if err != nil {
		return routingState{}, err
	}
	st := routingState{lists: routing.New(), active: active}
	for _, n := range names {
		if n == routing.ScriptName {
			st.exists = true
		}
	}
	switch {
	case active == routing.ScriptName:
		st.inForce = true
	case active == "":
		// The server is running nothing at all, so switching ours on switches
		// nothing off. This is the only case where we activate.
		st.activate = true
	default:
		// Somebody else's script is the active one. Ours still runs if theirs
		// includes it, which is how the webmail filter editor and this program
		// share one account: the editor owns the active script and ends it with
		// `include "logic";`.
		body, err := d.Sieve.Script(ctx, active)
		if err != nil {
			return routingState{}, err
		}
		st.inForce = routing.Includes(body, routing.ScriptName)
	}
	if !st.exists {
		return st, nil
	}
	if st.raw, err = d.Sieve.Script(ctx, routing.ScriptName); err != nil {
		return routingState{}, err
	}
	st.lists = routing.Parse(st.raw)
	return st, nil
}

// refreshRouting reads the script and projects it into the Mirror, so that
// every read of the Routing after this one is answered offline (ADR-0001).
func (d *Daemon) refreshRouting(ctx context.Context) error {
	st, err := d.readRouting(ctx)
	if err != nil {
		return err
	}
	if !st.exists {
		return nil
	}
	if !st.inForce {
		d.logf("routing: %q is stored but nothing reaches it — %q is the active script and does not include it",
			routing.ScriptName, st.active)
	}
	return d.storeRouting(st.raw, st.inForce, st.lists)
}

// storeRouting writes the script and its projection into the Mirror.
func (d *Daemon) storeRouting(raw string, active bool, lists *routing.Lists) error {
	routes := make([]mirror.Route, 0, lists.Count())
	for _, r := range lists.All() {
		routes = append(routes, mirror.Route{Address: r.Address, To: string(r.To), Box: r.Box})
	}
	return d.Mirror.PutRouting(d.Account, routing.ScriptName, raw, active, routes)
}

// handleScreener answers who is waiting for a decision. It is a Mirror read
// grouped by sender, because the decision is about a sender and not about a
// mail: five mails from one address are one thing to decide, not five.
func (d *Daemon) handleScreener(req Request, resp Response) Response {
	a := d.primaryAccount()
	box, ok := d.boxNamed(a, routing.BoxScreener)
	if !ok {
		resp.Code, resp.Error = "usage", fmt.Sprintf("this account has no %s box", routing.BoxScreener)
		return resp
	}
	limit := 25
	if v, ok := req.Args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	rows, err := d.Mirror.Rows(a.Name, box, screenerScan)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	out := groupBySender(a, box, rows)
	if len(out) > limit {
		out = out[:limit]
	}
	resp.OK, resp.Data = true, out
	return resp
}

// waiting is one sender the Screener is holding mail from: the decision owed,
// and enough to make it without opening anything.
type waiting struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Unread  int    `json:"unread"`
	Newest  string `json:"newest"`
	Subject string `json:"subject"`
	// ID reads the newest of them, for when the subject is not enough.
	ID string `json:"id"`
}

// groupBySender folds a Box's rows into one entry per sender, newest first. A
// sender whose header cannot be parsed into an address is still reported, under
// the header itself: it is mail somebody has to look at, and hiding it because
// its From line is malformed is how a Screener quietly stops being complete.
func groupBySender(a *Account, box string, rows []mirror.Row) []waiting {
	byAddr := map[string]*waiting{}
	var order []string
	for _, r := range rows {
		addr := routing.AddressOf(r.From)
		if addr == "" {
			addr = strings.TrimSpace(r.From)
		}
		w, seen := byAddr[addr]
		if !seen {
			// Rows arrive newest first, so the first one seen is the newest.
			w = &waiting{
				Address: addr, Name: routing.NameOf(r.From),
				Subject: r.Subject, ID: a.messageID(box, r.Placement.UID),
			}
			if !r.Message.Date.IsZero() {
				w.Newest = r.Message.Date.Format("2006-01-02 15:04")
			}
			byAddr[addr] = w
			order = append(order, addr)
		}
		w.Count++
		if !r.Seen() {
			w.Unread++
		}
	}
	out := make([]waiting, 0, len(order))
	for _, addr := range order {
		out = append(out, *byAddr[addr])
	}
	return out
}

// routingView is the Routing as the Mirror holds it.
type routingView struct {
	Routes []route `json:"routes"`
	// Active is whether the server is running our script. False means the
	// decisions below describe a script that is switched off.
	Active   bool   `json:"active"`
	Script   string `json:"script,omitempty"`
	SyncedAt string `json:"synced_at,omitempty"`
}

type route struct {
	Address string `json:"address"`
	To      string `json:"to"`
	Box     string `json:"box,omitempty"`
}

// handleRouting lists the Routing. It is a Mirror read like every other read
// here: the script is on the server, and what it says is held locally so that
// "where does this sender's mail go" is answerable with the network down.
func (d *Daemon) handleRouting(req Request, resp Response) Response {
	routes, err := d.Mirror.Routing(d.Account)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	view := routingView{Routes: make([]route, 0, len(routes))}
	for _, r := range routes {
		view.Routes = append(view.Routes, route{Address: r.Address, To: r.To, Box: r.Box})
	}
	script, err := d.Mirror.RoutingScript(d.Account)
	switch {
	case errors.Is(err, mirror.ErrNotFound):
		// Never read one. That is not an empty Routing, it is no answer, and
		// saying so is better than reporting nobody is routed anywhere.
		resp.Code, resp.Error = "not_found", "the mirror holds no routing script yet"
		return resp
	case err != nil:
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	view.Active = script.Active
	if !script.SyncedAt.IsZero() {
		view.SyncedAt = script.SyncedAt.Format(time.RFC3339)
	}
	if show, _ := req.Args["script"].(bool); show {
		view.Script = script.Raw
	}
	resp.OK, resp.Data = true, view
	return resp
}

// decision is what one routing decision did: where the sender's mail goes from
// now on, and what happened to the mail that was already here.
type decision struct {
	Address string `json:"address"`
	To      string `json:"to"`
	Box     string `json:"box,omitempty"`
	// Changed is false when the sender was already routed there. The mail in
	// the Screener is moved either way: the decision was made before that mail
	// arrived, and it applies to it.
	Changed bool     `json:"changed"`
	Moved   []string `json:"moved"`
}

// handleRoute decides where a sender's mail goes. One command does both halves
// of the decision — the script that files their next mail, and the mail already
// sitting in the Screener — because a caller who has to run two commands to
// finish one decision will one day run only the first.
//
// The server first, then the Mirror, then the mail (ADR-0004). A script the
// server refused leaves everything exactly as it was; a move that fails after
// the script was stored leaves the decision made and the old mail where it is,
// which the same command run again finishes.
func (d *Daemon) handleRoute(ctx context.Context, req Request, resp Response) Response {
	targets := argStrings(req.Args["positional"])
	if len(targets) == 0 {
		return d.handleRouting(req, resp)
	}
	a := d.primaryAccount()
	if a.Writer == nil {
		resp.Code, resp.Error = "api", "this daemon cannot write: no server connection"
		return resp
	}
	to, err := routing.ParseDestination(str(req.Args["to"]))
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}

	addresses, err := d.senders(targets)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}

	// What is already here, gathered before anything is written: it decides
	// which Boxes have to exist, and a Mirror read costs nothing.
	screener, hasScreener := d.boxNamed(a, routing.BoxScreener)
	waiting := map[string][]mailsync.Ref{}
	total := 0
	if hasScreener && to != routing.None {
		for _, addr := range addresses {
			refs, err := d.screenerRefs(a, screener, addr)
			if err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
			waiting[addr] = refs
			total += len(refs)
		}
	}
	// A Box that is not there is a rule that silently does nothing: Sieve files
	// into a Box it cannot find by not filing at all, and the mail lands in the
	// Inbox looking as though the decision was never made.
	for _, box := range []string{to.Box(), pileFor(to, total)} {
		if box == "" {
			continue
		}
		if _, ok := d.boxNamed(a, box); !ok {
			resp.Code, resp.Error = "usage", fmt.Sprintf(
				"this account has no %q box — create it before routing mail there", box)
			return resp
		}
	}

	st, err := d.readRouting(ctx)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	// Never disable somebody else's filtering to enable ours: activating a
	// script deactivates the one that was running, and that is somebody's
	// webmail rules. So the decision is refused unless the Routing already
	// runs — because it is active, or because the active script includes it.
	if !st.inForce && !st.activate {
		resp.Code, resp.Error = "api", fmt.Sprintf(
			"%q is the active sieve script and it does not include %q, so the routing "+
				"would be stored and never run — add `include %q;` to the end of %q, "+
				"or make %q the active script",
			st.active, routing.ScriptName, routing.ScriptName, st.active, routing.ScriptName)
		return resp
	}

	out := make([]decision, 0, len(addresses))
	changed := false
	for _, addr := range addresses {
		did, err := st.lists.Set(addr, to)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		changed = changed || did
		out = append(out, decision{Address: addr, To: string(to), Box: to.Box(), Changed: did, Moved: []string{}})
	}

	if changed || !st.exists {
		script := st.lists.Script()
		if err := d.Sieve.PutScript(ctx, routing.ScriptName, script, st.activate); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		// What the server accepted is what it compiled: PUTSCRIPT either takes
		// the script or refuses it, so the bytes we sent are the bytes it now
		// runs, and storing them is storing the ack (ADR-0004).
		if err := d.storeRouting(script, true, st.lists); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
	}

	if pile := pileFor(to, total); pile != "" {
		box, _ := d.boxNamed(a, pile)
		for i, addr := range addresses {
			refs := waiting[addr]
			if len(refs) == 0 {
				continue
			}
			results, err := a.Writer.Move(ctx, refs, box)
			if err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
			for _, r := range results {
				if r.NewUID != 0 {
					out[i].Moved = append(out[i].Moved, a.messageID(r.NewFolder, r.NewUID))
				} else {
					out[i].Moved = append(out[i].Moved, box)
				}
			}
		}
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: screener})
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	resp.OK, resp.Data = true, out
	return resp
}

// pileFor is where mail already in the Screener goes for a decision, empty when
// there is none to move.
func pileFor(to routing.Destination, waiting int) string {
	if waiting == 0 {
		return ""
	}
	return to.Pile()
}

// senders turns what a caller typed into addresses. A target with an `@` in it
// is an address; anything else is a message id, and the address is whoever sent
// that Message — which is how the decision is usually made, by an agent that
// has just read the mail and has its id in hand.
func (d *Daemon) senders(targets []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range targets {
		addr := ""
		if strings.Contains(t, "@") {
			if addr = routing.AddressOf(t); addr == "" {
				return nil, fmt.Errorf("%q is not an address", t)
			}
		} else {
			acct, folder, uid, err := d.resolveID(t)
			if err != nil {
				return nil, err
			}
			if !acct.Primary {
				return nil, fmt.Errorf("%s is on the %s account: the routing belongs to the primary one", t, acct.Name)
			}
			r, err := d.Mirror.Row(acct.Name, folder, uid)
			if errors.Is(err, mirror.ErrNotFound) {
				return nil, errors.New(noSuchMessage(t))
			}
			if err != nil {
				return nil, err
			}
			if addr = routing.AddressOf(r.From); addr == "" {
				return nil, fmt.Errorf("cannot tell who %s is from (%q)", t, r.From)
			}
		}
		if !routing.Valid(addr) {
			return nil, fmt.Errorf("%q is not an address this script can carry", addr)
		}
		if !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}
	return out, nil
}

// screenerRefs is every Placement in the Screener that really is from address.
// The Mirror narrows it with a substring match over the From header and the
// header is parsed here, because `bob@example.com` is a substring of
// `notbob@example.com` and of any display name a sender cares to write.
func (d *Daemon) screenerRefs(a *Account, box, address string) ([]mailsync.Ref, error) {
	rows, err := d.Mirror.RowsFrom(a.Name, box, address, screenerScan)
	if err != nil {
		return nil, err
	}
	var refs []mailsync.Ref
	for _, r := range rows {
		if routing.AddressOf(r.From) != address {
			continue
		}
		refs = append(refs, mailsync.Ref{Folder: box, UID: r.Placement.UID})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].UID < refs[j].UID })
	return refs, nil
}

// handleAside moves mail into the read-later pile, and back out of it. Aside is
// a Box on the Primary Account that the Routing never fills: "read this later"
// is a decision about one mail, and a sender whose every mail should be read
// later is a Feed.
func (d *Daemon) handleAside(ctx context.Context, req Request, resp Response) Response {
	return d.movePile(ctx, req, resp, routing.BoxAside)
}

// handleReplyLater moves mail into the reply-later pile, and back out of it.
// Like Aside it is never filled by the Routing: "I owe this a reply" is a
// decision about one mail, not about its sender.
func (d *Daemon) handleReplyLater(ctx context.Context, req Request, resp Response) Response {
	return d.movePile(ctx, req, resp, routing.BoxReplyLater)
}

// movePile puts mail into one of the hand-tended piles, or — with a `done`
// sub-verb — takes it back out to the Inbox. Both piles behave the same way;
// only the Box they land in differs.
func (d *Daemon) movePile(ctx context.Context, req Request, resp Response, pileBox string) Response {
	acct, refs, err := d.refs(req)
	if err != nil {
		return refsFail(resp, err)
	}
	want := pileBox
	if len(req.Cmd) > 1 && req.Cmd[1] == "done" {
		want = routing.BoxInbox
	}
	dest, ok := d.boxNamed(acct, want)
	if !ok {
		resp.Code, resp.Error = "usage", fmt.Sprintf("this account has no %q box", want)
		return resp
	}
	results, err := acct.Writer.Move(ctx, refs, dest)
	return d.wrote(acct, resp, results, err)
}

// boxNamed finds a Box on an account by name, case-insensitively, and reports
// whether it is there at all. A Box that is not there is worth an error rather
// than a write that goes nowhere.
func (d *Daemon) boxNamed(a *Account, want string) (string, bool) {
	for _, b := range a.Mirrored {
		if strings.EqualFold(b, want) {
			return b, true
		}
	}
	return want, false
}

// argStrings reads a positional argument that may be one value or several.
func argStrings(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
