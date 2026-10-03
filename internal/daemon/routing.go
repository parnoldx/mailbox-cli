package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/pickup"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// RuleKeeper is a Graph account's inbox rules, the Routing that account runs
// server-side (ADR-0032). It is read whole and replaced whole, like the Sieve
// script: the rules on the server are the record, one write rewrites ours.
type RuleKeeper interface {
	Rules(ctx context.Context) ([]routing.Rule, error)
	SetRules(ctx context.Context, rules []routing.Rule) error
}

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

// routingLoop keeps the Mirror's copy of every Routing true: the Primary's
// Sieve script, and each Graph account's inbox rules. The script and the rules
// are rewritten by this program and by nobody else in the ordinary case, but a
// rule added in webmail or Outlook is exactly the sort of thing a caller should
// not have to restart the daemon to see.
func (d *Daemon) routingLoop(ctx context.Context) {
	every := d.RoutingEvery
	if every <= 0 {
		every = 10 * time.Minute
	}
	for {
		// An account can gain a Routing mid-run — a config reload builds one
		// (ADR-0021) — so who has one is asked every tick, not once at start.
		for _, a := range d.accounts() {
			if !(a.Primary && d.Sieve != nil) && a.Routing == nil {
				continue
			}
			if err := d.refreshRoutingOn(ctx, a); err != nil {
				d.logf("routing %s: %v", a.label(), err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// routingState is the Routing as the server currently has it: the lists, and
// the facts about the script or rules that a write has to know before it
// touches anything.
type routingState struct {
	lists  *routing.Lists
	raw    string
	exists bool
	// active is the name of the script the server is running, which is
	// usually not ours.
	active string
	// inForce is whether our routing actually runs: for a Sieve script, that it
	// is the active one or included by the active one; for Graph rules, that
	// the Screener's catch-all is among them. A Routing that is stored but not
	// in force decides nothing, and writing to it would be writing into a
	// drawer.
	inForce bool
	// activate is whether making ours the active script would switch nothing
	// else off. True only when the server is running no script at all.
	activate bool
	// rules is the Routing as a Graph account currently carries it, nil on a
	// Sieve account.
	rules  []routing.Rule
	keeper RuleKeeper
}

// readRouting fetches the Primary's script and parses it. A server with no
// `logic` script is not an error: it is an account with no Routing yet, which
// is what a fresh one looks like.
func (d *Daemon) readRouting(ctx context.Context) (routingState, error) {
	return d.readRoutingOn(ctx, d.Primary)
}

// readRoutingOn is readRouting for one account: a Graph account's Routing is
// its inbox rules, the Primary's is the Sieve script (ADR-0032). An IMAP
// Secondary has neither — its accounts do not route — and falling through to
// the Primary's Sieve here would let a decision made on a Secondary rewrite
// the Primary's script.
func (d *Daemon) readRoutingOn(ctx context.Context, a *Account) (routingState, error) {
	if a.Routing != nil {
		rules, err := a.Routing.Rules(ctx)
		if err != nil {
			return routingState{}, err
		}
		st := routingState{
			lists: routing.ListsFromRules(rules), rules: rules,
			keeper: a.Routing, active: routing.RuleName,
		}
		if len(rules) > 0 {
			st.exists = true
			// Raw is what the server answered, JSON-encoded: the record the
			// Mirror holds beside the projection, as the script is on the Primary.
			b, err := json.Marshal(rules)
			if err != nil {
				return routingState{}, err
			}
			st.raw = string(b)
		}
		// The rules run on arrival by definition; what decides whether the
		// Routing is in force is whether undecided senders land somewhere this
		// program watches for them.
		st.inForce = routing.HasCatchAll(rules)
		return st, nil
	}
	if d.Sieve == nil || !a.Primary {
		return routingState{}, errors.New(
			"the routing belongs to the primary account, or to a Microsoft 365 one as inbox rules")
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

// refreshRouting reads the Primary's script and projects it into the Mirror,
// so that every read of the Routing after this one is answered offline
// (ADR-0001).
func (d *Daemon) refreshRouting(ctx context.Context) error {
	return d.refreshRoutingOn(ctx, d.Primary)
}

// refreshRoutingOn is refreshRouting for one account.
func (d *Daemon) refreshRoutingOn(ctx context.Context, a *Account) error {
	// One decision and one refresh at a time: the write is a read-modify-write
	// over several server round trips, and a refresh that read halfway through
	// it would project half a Routing into the Mirror.
	d.routingMu.Lock()
	defer d.routingMu.Unlock()
	st, err := d.readRoutingOn(ctx, a)
	if err != nil {
		return err
	}
	if !st.exists {
		// A Graph account whose rules have all gone — deleted in Outlook, say —
		// had a Routing the Mirror still shows. The server is the record, so
		// the projection follows it to empty; but only when there was one, and
		// only on a clean read, so a database error does not overwrite it with
		// nothing.
		if a.Routing != nil {
			if s, err := d.Mirror.RoutingScript(a.Name); err == nil && s.Raw != "" {
				return d.storeRoutingOn(a, "", false, routing.New())
			}
		}
		return nil
	}
	if routing.Unreadable(st.rules) {
		d.logf("routing %s: %d of its rules cannot be read back — decisions there are refused until it is repaired",
			a.label(), len(st.rules))
	}
	if !st.inForce {
		d.logf("routing %s: not in force — undecided senders are not being held for a decision", a.label())
	}
	return d.storeRoutingOn(a, st.raw, st.inForce, st.lists)
}

// storeRouting writes the Primary's script and its projection into the Mirror.
func (d *Daemon) storeRouting(raw string, active bool, lists *routing.Lists) error {
	return d.storeRoutingOn(d.Primary, raw, active, lists)
}

// storeRoutingOn is storeRouting for one account: the Mirror keys every
// Routing by the account it belongs to.
func (d *Daemon) storeRoutingOn(a *Account, raw string, active bool, lists *routing.Lists) error {
	routes := make([]mirror.Route, 0, lists.Count())
	for _, r := range lists.All() {
		routes = append(routes, mirror.Route{Address: r.Address, To: string(r.To), Box: r.Box})
	}
	name := routing.ScriptName
	if a.Routing != nil {
		name = "inbox rules"
	}
	return d.Mirror.PutRouting(a.Name, name, raw, active, routes)
}

// handleScreener answers who is waiting for a decision. It is a Mirror read
// grouped by sender, because the decision is about a sender and not about a
// mail: five mails from one address are one thing to decide, not five. Every
// account with a Routing has a Screener, so the account named — the Primary
// when none is — decides whose is listed.
func (d *Daemon) handleScreener(req Request, resp Response) Response {
	a, err := d.routeAccount(req, nil)
	if err != nil {
		return resp.usage(err.Error())
	}
	box, ok := a.boxNamed(routing.BoxScreener)
	if !ok {
		return resp.usage(fmt.Sprintf("this account has no %s box", routing.BoxScreener))
	}
	limit := req.Int("limit", 25)
	rows, err := d.Mirror.Rows(a.Name, box, screenerScan)
	if err != nil {
		return resp.api(err.Error())
	}
	out := groupBySender(a, box, rows)
	if len(out) > limit {
		out = out[:limit]
	}
	return resp.ok(out)
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
		// A Pickup owes no decision. The code has been taken out of it, it is
		// already read, and the Daemon bins it within the quarter hour — the
		// sender is a login form you used once, not somebody to route. Leaving
		// it in would put a red badge on the widget for exactly as long as the
		// mail took to expire.
		if slices.Contains(r.Placement.Flags, pickup.Keyword) {
			continue
		}
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
				// .Local() for the same reason the Box listing does it: the
				// server's instant is usually UTC, and a wait list that shows a
				// different hour than the mail it points at reads as wrong.
				w.Newest = r.Message.Date.Local().Format("2006-01-02 15:04")
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
	a, err := d.routeAccount(req, nil)
	if err != nil {
		return resp.usage(err.Error())
	}
	routes, err := d.Mirror.Routing(a.Name)
	if err != nil {
		return resp.api(err.Error())
	}
	view := routingView{Routes: make([]route, 0, len(routes))}
	for _, r := range routes {
		view.Routes = append(view.Routes, route{Address: r.Address, To: r.To, Box: r.Box})
	}
	script, err := d.Mirror.RoutingScript(a.Name)
	switch {
	case errors.Is(err, mirror.ErrNotFound):
		// Never read one. That is not an empty Routing, it is no answer, and
		// saying so is better than reporting nobody is routed anywhere.
		return resp.notFound("the mirror holds no routing script yet")
	case err != nil:
		return resp.api(err.Error())
	}
	view.Active = script.Active
	if !script.SyncedAt.IsZero() {
		view.SyncedAt = script.SyncedAt.Format(time.RFC3339)
	}
	if req.Bool("script") {
		view.Script = script.Raw
	}
	return resp.ok(view)
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
	// Binned is how many of their mails were marked read and moved to Trash,
	// which is what a block does with what is already here. It has no ids to
	// report: Trash is not Mirrored, so there is nothing left to name.
	Binned int `json:"binned,omitempty"`
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
	targets := req.Strings("positional")
	if len(targets) == 0 {
		return d.handleRouting(req, resp)
	}
	a, err := d.routeAccount(req, targets)
	if err != nil {
		return resp.usage(err.Error())
	}
	if a.Writer == nil {
		return resp.api("this daemon cannot write: no server connection")
	}
	// One decision at a time (see refreshRoutingOn): the write below is a
	// read-modify-write of the account's script or rules, and two at once
	// would be two Routing half-written.
	d.routingMu.Lock()
	defer d.routingMu.Unlock()
	to, err := routing.ParseDestination(req.Str("to"))
	if err != nil {
		return resp.usage(err.Error())
	}

	addresses, err := d.sendersOn(a, targets)
	if err != nil {
		return resp.usage(err.Error())
	}

	st, err := d.readRoutingOn(ctx, a)
	if err != nil {
		return resp.api(err.Error())
	}
	// A Graph rule that could not be read back — its Box is gone — is the one
	// thing a decision refuses on a Graph account: the write would carry over
	// only the decisions it could read and quietly forget the rest.
	if a.Routing != nil && routing.Unreadable(st.rules) {
		return resp.api("some of this account's routing rules cannot be read back — a Box they name is gone. " +
			"Recreate the Box, or delete the rule where it was written, and decide again")
	}
	// Never disable somebody else's filtering to enable ours: activating a
	// script deactivates the one that was running, and that is somebody's
	// webmail rules. So the decision is refused unless the Routing already
	// runs — because it is active, or because the active script includes it.
	if a.Routing == nil && !st.inForce && !st.activate {
		return resp.api(fmt.Sprintf(
			"%q is the active sieve script and it does not include %q, so the routing "+
				"would be stored and never run — add `include %q;` to the end of %q, "+
				"or make %q the active script",
			st.active, routing.ScriptName, routing.ScriptName, st.active, routing.ScriptName))
	}

	out := make([]decision, 0, len(addresses))
	changed := false
	for _, addr := range addresses {
		did, err := st.lists.Set(addr, to)
		if err != nil {
			return resp.usage(err.Error())
		}
		changed = changed || did
		out = append(out, decision{Address: addr, To: string(to), Box: to.Box(), Changed: did, Moved: []string{}})
	}

	// What is already here, after the decision is in the lists: a domain key
	// has to see Of() so a more specific address rule is not swept along. Block
	// is not collected here — it bins out of two Boxes, once the script is
	// stored, and files nothing.
	screener, hasScreener := a.boxNamed(routing.BoxScreener)
	waiting := map[string][]mailsync.Ref{}
	total := 0
	if hasScreener && to != routing.None && to != routing.Block {
		for _, addr := range addresses {
			refs, err := d.senderRefs(a, screener, addr, st.lists, to)
			if err != nil {
				return resp.api(err.Error())
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
		if _, ok := a.boxNamed(box); !ok {
			return resp.usage(fmt.Sprintf(
				"this account has no %q box — create it before routing mail there", box))
		}
	}

	if changed || !st.exists {
		if a.Routing != nil {
			// The rules the server should run, whole; ours are replaced and
			// nobody else's touched (ADR-0032).
			rules := st.lists.Rules()
			if err := a.Routing.SetRules(ctx, rules); err != nil {
				return resp.api(err.Error())
			}
			b, err := json.Marshal(rules)
			if err != nil {
				return resp.api(err.Error())
			}
			// The server took the list or refused it, so what we sent is what
			// it now runs, and storing it is storing the ack (ADR-0004).
			if err := d.storeRoutingOn(a, string(b), true, st.lists); err != nil {
				return resp.api(err.Error())
			}
		} else {
			script := st.lists.Script()
			if err := d.Sieve.PutScript(ctx, routing.ScriptName, script, st.activate); err != nil {
				return resp.api(err.Error())
			}
			// What the server accepted is what it compiled: PUTSCRIPT either takes
			// the script or refuses it, so the bytes we sent are the bytes it now
			// runs, and storing them is storing the ack (ADR-0004).
			if err := d.storeRoutingOn(a, script, true, st.lists); err != nil {
				return resp.api(err.Error())
			}
		}
	}

	if to == routing.Block {
		for i, addr := range addresses {
			binned, err := d.binBlocked(ctx, a, addr, st.lists)
			if err != nil {
				return resp.api(err.Error())
			}
			out[i].Binned = binned
		}
		return resp.ok(out)
	}

	if pile := pileFor(to, total); pile != "" {
		box, _ := a.boxNamed(pile)
		for i, addr := range addresses {
			refs := waiting[addr]
			if len(refs) == 0 {
				continue
			}
			results, err := a.Writer.Move(ctx, refs, box)
			if err != nil {
				return resp.api(err.Error())
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

	// The mail the decision was made about moves with it, when the Screener is
	// not the one holding it: "this belongs in the Paper Trail", read in the
	// Inbox, is a decision about the sender, and the mail it was read on should
	// not stay behind to contradict it. Read on arrival, the same way the
	// script marks what it files there. A Screener target is the sweep's job
	// above, and one already sitting in the destination needs nothing.
	if to == routing.Feed || to == routing.PaperTrail {
		dest, _ := a.boxNamed(to.Box())
		for _, t := range targets {
			t = strings.TrimSpace(t)
			if strings.Contains(t, "@") {
				continue // an address was decided about, no mail named
			}
			acct, folder, uid, err := d.resolveID(t)
			if err != nil {
				return resp.api(err.Error())
			}
			if acct.Name != a.Name ||
				strings.EqualFold(folder, routing.BoxScreener) ||
				strings.EqualFold(folder, to.Box()) {
				continue
			}
			row, err := d.Mirror.Row(acct.Name, folder, uid)
			if err != nil {
				return resp.api(err.Error())
			}
			ref := mailsync.Ref{Folder: folder, UID: uid}
			// \Seen first, while the uid we hold is still the mail's — the
			// same order trash bins in, for the same reason.
			if _, err := a.Writer.SetSeen(ctx, []mailsync.Ref{ref}, true); err != nil {
				return resp.api(err.Error())
			}
			results, err := a.Writer.Move(ctx, []mailsync.Ref{ref}, dest)
			if err != nil {
				return resp.api(err.Error())
			}
			d.push(Push{Event: "mail.changed", Account: a.Name, Box: folder})
			d.push(Push{Event: "mail.changed", Account: a.Name, Box: to.Box()})
			addr := routing.AddressOf(row.From)
			for i := range out {
				if out[i].Address != addr {
					continue
				}
				for _, r := range results {
					if r.NewUID != 0 {
						out[i].Moved = append(out[i].Moved, a.messageID(r.NewFolder, r.NewUID))
					} else {
						out[i].Moved = append(out[i].Moved, to.Box())
					}
				}
			}
		}
	}
	return resp.ok(out)
}

// binBlocked empties a blocked sender's mail out of the Screener and out of the
// Block Box: marked read first, then moved to Trash. The sieve entry is the
// record of the block, so the mail it was made about is rubbish; Trash already
// keeps a mistake recoverable for as long as the server holds it, and a second
// pile nobody empties buys nothing over that. What it does buy is a Block Box
// that is empty exactly when every drag into it has been written to the script.
func (d *Daemon) binBlocked(ctx context.Context, a *Account, addr string, lists *routing.Lists) (int, error) {
	var refs []mailsync.Ref
	for _, want := range []string{routing.BoxScreener, routing.BoxBlock} {
		box, ok := a.boxNamed(want)
		if !ok {
			continue
		}
		found, err := d.senderRefs(a, box, addr, lists, routing.Block)
		if err != nil {
			return 0, err
		}
		refs = append(refs, found...)
	}
	if len(refs) == 0 {
		return 0, nil
	}
	// \Seen first, while the uids we hold are still the mails': after the move
	// they have new ones in Trash, which the Mirror never sees. `trash` sets it
	// in the same order and for the same reason — a binned mail counts as
	// unread for nobody.
	if _, err := a.Writer.SetSeen(ctx, refs, true); err != nil {
		return 0, err
	}
	results, err := a.Writer.Move(ctx, refs, "Trash")
	if err != nil {
		return 0, err
	}
	for _, box := range []string{routing.BoxScreener, routing.BoxBlock} {
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	return len(results), nil
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
// routeAccount is the account a screener or route call is about: the account
// the caller named, else the account a target id carries — `work/Screener:12`
// is a decision on work — else the Primary. Every target must sit on the one
// account, because a decision is made on one server's lists.
func (d *Daemon) routeAccount(req Request, targets []string) (*Account, error) {
	name := req.Str("account")
	if name == "" {
		for _, t := range targets {
			if prefix, _ := splitAccount(t, d.accountNames()); prefix != "" {
				name = prefix
				break
			}
		}
	}
	a, err := d.accountNamed(name)
	if err != nil {
		return nil, fmt.Errorf("no account called %q", name)
	}
	return a, nil
}

func (d *Daemon) sendersOn(a *Account, targets []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range targets {
		t = strings.TrimSpace(t)
		addr := ""
		if routing.IsDomain(t) {
			addr = strings.ToLower(t)
			if !routing.ValidDomain(addr) {
				return nil, fmt.Errorf("%q is not a domain this script can carry", t)
			}
		} else if strings.Contains(t, "@") {
			if addr = routing.AddressOf(t); addr == "" {
				return nil, fmt.Errorf("%q is not an address", t)
			}
		} else {
			acct, folder, uid, err := d.resolveID(t)
			if err != nil {
				return nil, err
			}
			if acct.Name != a.Name {
				return nil, fmt.Errorf("%s is on the %s account: one decision is made on one account", t, acct.label())
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
		if routing.IsDomain(addr) {
			if !routing.ValidDomain(addr) {
				return nil, fmt.Errorf("%q is not a domain this script can carry", addr)
			}
		} else if !routing.Valid(addr) {
			return nil, fmt.Errorf("%q is not an address this script can carry", addr)
		}
		if !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}
	return out, nil
}

// senderRefs is every Placement in a Box that really is from address — the
// Screener for a decision's sweep, and the Block Box too when the decision is a
// block. The Mirror narrows it with a substring match over the From header and the
// header is parsed here, because `bob@example.com` is a substring of
// `notbob@example.com` and of any display name a sender cares to write.
func (d *Daemon) senderRefs(a *Account, box, key string, lists *routing.Lists, to routing.Destination) ([]mailsync.Ref, error) {
	needle := key
	if routing.IsDomain(key) {
		needle = routing.DomainOf(key)
	}
	rows, err := d.Mirror.RowsFrom(a.Name, box, needle, screenerScan)
	if err != nil {
		return nil, err
	}
	var refs []mailsync.Ref
	for _, r := range rows {
		addr := routing.AddressOf(r.From)
		if lists.Of(addr) != to {
			continue
		}
		if routing.IsDomain(key) {
			if routing.DomainOf(addr) != routing.DomainOf(key) {
				continue
			}
		} else if addr != key {
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
//
// A pile is a decision about a conversation, not one Message (the user thinks
// in HEY's terms: the thread is set aside, or owed a reply). So the id given is
// expanded to its whole Thread and every one of its Messages in the Inbox or
// the other pile moves with it — otherwise half the thread shows in the Inbox
// and half in the pile.
func (d *Daemon) movePile(ctx context.Context, req Request, resp Response, pileBox string) Response {
	acct, refs, err := d.refs(req)
	if err != nil {
		return resp.failed(err)
	}
	want := pileBox
	if req.Verb("") == "done" {
		want = routing.BoxInbox
	}
	dest, ok := acct.boxNamed(want)
	if !ok {
		return resp.usage(fmt.Sprintf("this account has no %q box", want))
	}
	// Sweep the whole Thread out of everywhere it could be shown alongside the
	// destination — the Inbox and both piles — minus the destination itself,
	// which Writer.Move refuses as a no-op move.
	var from []string
	for _, b := range []string{routing.BoxInbox, routing.BoxAside, routing.BoxReplyLater} {
		if !strings.EqualFold(b, dest) {
			from = append(from, b)
		}
	}
	if refs, err = d.threadedWithin(acct.Name, refs, from...); err != nil {
		return resp.api(err.Error())
	}
	results, err := acct.Writer.Move(ctx, refs, dest)
	return d.wrote(acct, resp, results, err)
}

// boxNamed finds a Box on this account by name, case-insensitively, and reports
// whether it is there at all. A Box that is not there is worth an error rather
// than a write that goes nowhere. It is a question about an Account and nothing
// else, which is why it does not go through the Daemon.
func (a *Account) boxNamed(want string) (string, bool) {
	for _, b := range a.Mirrored {
		if strings.EqualFold(b, want) {
			return b, true
		}
	}
	return want, false
}
