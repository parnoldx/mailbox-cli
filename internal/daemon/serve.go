package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mailbox/internal/htmlmd"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
	"mailbox/internal/terminal"
)

// serve handles one client: NDJSON requests in, replies and pushes out.
func (d *Daemon) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	pushes := make(chan Push, 16)
	d.mu.Lock()
	d.clients[pushes] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.clients, pushes)
		d.mu.Unlock()
	}()

	enc := json.NewEncoder(conn)
	var wmu sync.Mutex
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return enc.Encode(v)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-pushes:
				if err := write(p); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = write(Response{OK: false, Code: "usage", Error: "malformed request"})
			continue
		}
		if err := write(d.handle(ctx, req)); err != nil {
			return
		}
	}
}

// handle runs one request against the Mirror. Reads never touch the network:
// that is the whole point (ADR-0001).
func (d *Daemon) handle(ctx context.Context, req Request) Response {
	if len(req.Cmd) == 0 {
		return Response{ID: req.ID, Mirror: d.state(domainBoth), Code: "usage", Error: "no command"}
	}
	dom := domainOf(req.Cmd[0])
	// Asking for a cycle, not waiting for one: the reply below is the Mirror's
	// as it stands, and the cycle this starts is what makes the next reply
	// better. Before the reply is built, so that a caller told it is Behind is
	// also told a sync is running.
	if dom == domainDAV {
		d.nudgeDAV(nudgeKinds(req.Cmd[0])...)
	}
	resp := Response{ID: req.ID, Mirror: d.state(dom)}

	switch req.Cmd[0] {
	case "reload":
		// Not a CLI command: `mailbox setup` sends it after it writes the
		// config, so the wizard does not sit for up to a minute waiting for
		// the poll to notice (ADR-0021).
		resp.OK, resp.Data = true, map[string]any{"changes": d.reloadConfig("asked over the socket")}
		return resp
	case "outbox":
		return d.handleOutbox(ctx, req, resp)
	case "calendar":
		return d.handleCalendar(req, resp)
	case "agenda":
		return d.handleAgenda(req, resp)
	case "event":
		return d.handleEvent(ctx, req, resp)
	case "todo":
		return d.handleTodo(ctx, req, resp)
	case "habit":
		return d.handleHabit(ctx, req, resp)
	case "contact":
		return d.handleContact(ctx, req, resp)
	case "screener":
		return d.handleScreener(req, resp)
	case "route":
		return d.handleRoute(ctx, req, resp)
	case "aside":
		return d.handleAside(ctx, req, resp)
	case "sieve":
		return d.handleSieve(ctx, req, resp)
	case "label":
		return d.handleLabel(ctx, req, resp)
	case "draft":
		return d.handleDraft(ctx, req, resp)
	}

	switch strings.Join(req.Cmd, " ") {
	case "box list":
		// Both filters are applied here rather than while printing, so a flag
		// means the same thing whether a caller reads the table or the JSON.
		unreadOnly, _ := req.Args["unread"].(bool)
		everything, _ := req.Args["archive"].(bool)
		out := []boxRow{}
		for _, acct := range d.accounts() {
			counts, err := d.Mirror.BoxCounts(acct.Name)
			if err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
			held := map[string]mirror.BoxCount{}
			for _, c := range counts {
				held[c.Folder] = c
			}
			// Driven by what is mirrored rather than by what has messages in
			// it, so an empty Box is a row saying it is empty and not a Box
			// that appears to have stopped existing.
			for _, folder := range listedBoxes(acct, everything) {
				c := held[folder]
				if unreadOnly && c.Unseen == 0 {
					continue
				}
				out = append(out, boxRow{
					Box:     acct.qualify(shortBox(folder, acct.Mirrored)),
					Folder:  folder,
					Account: acct.Name,
					Count:   c.Count,
					Unseen:  c.Unseen,
					Watched: hasFlag(acct.Watched, folder),
				})
			}
		}
		resp.OK, resp.Data = true, out
		return resp

	case "box view":
		// A Box is named the same way a Message is: `[account/]box`, with the
		// Primary Account implicit (ADR-0005).
		name, _ := req.Args["positional"].(string)
		prefix, rest := splitAccount(name, d.accountNames())
		acct, err := d.accountNamed(prefix)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		folder := "INBOX"
		if rest != "" {
			folder = resolveBox(rest, acct.Mirrored)
		}
		limit := 50
		if v, ok := req.Args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		rows, err := d.Mirror.Rows(acct.Name, folder, limit)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewRows(acct, folder, rows)
		return resp

	case "message view":
		id, _ := req.Args["positional"].(string)
		acct, folder, uid, err := d.resolveID(id)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		r, err := d.Mirror.Row(acct.Name, folder, uid)
		if errors.Is(err, mirror.ErrNotFound) {
			resp.Code, resp.Error = "not_found", noSuchMessage(id)
			return resp
		}
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		places, err := d.Mirror.Placements(acct.Name, r.Message.ID)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewMessage(acct, folder, r, places)
		return resp

	case "attachment list":
		id, _ := req.Args["positional"].(string)
		acct, folder, uid, _, err := d.resolveAttachmentID(id)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		parts, err := d.partsOf(acct, folder, uid)
		if err != nil {
			return fail(resp, id, err)
		}
		resp.OK, resp.Data = true, viewParts(acct, folder, uid, parts)
		return resp

	case "attachment save":
		id, _ := req.Args["positional"].(string)
		acct, folder, uid, index, err := d.resolveAttachmentID(id)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		parts, err := d.partsOf(acct, folder, uid)
		if err != nil {
			return fail(resp, id, err)
		}
		part, err := pick(acct, folder, uid, parts, index)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		out, _ := req.Args["output"].(string)
		force, _ := req.Args["force"].(bool)
		saved, err := d.save(ctx, acct, folder, uid, part, out, force)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, saved
		return resp

	case "thread view":
		id, _ := req.Args["positional"].(string)
		acct, folder, uid, err := d.resolveID(id)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		r, err := d.Mirror.Row(acct.Name, folder, uid)
		if errors.Is(err, mirror.ErrNotFound) {
			resp.Code, resp.Error = "not_found", noSuchMessage(id)
			return resp
		}
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		// A Thread never crosses an Account: the same conversation reaching two
		// accounts is two Threads (ADR-0008).
		rows, err := d.Mirror.Thread(acct.Name, r.Message.ThreadID)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := make([]message, 0, len(rows))
		for _, row := range rows {
			out = append(out, viewMessage(acct, row.Placement.Folder, row, nil))
		}
		resp.OK, resp.Data = true, out
		return resp

	case "search":
		text, _ := req.Args["positional"].(string)
		q := mirror.Query{Text: text, Limit: 25}
		if v, ok := req.Args["from"].(string); ok {
			q.From = v
		}
		if v, ok := req.Args["limit"].(float64); ok && v > 0 {
			q.Limit = int(v)
		}
		// `--in` may name an account's Box, and it may name the account alone.
		box, _ := req.Args["in"].(string)
		prefix, rest := splitAccount(box, d.accountNames())
		search := d.accounts()
		if prefix != "" {
			acct, err := d.accountNamed(prefix)
			if err != nil {
				resp.Code, resp.Error = "usage", err.Error()
				return resp
			}
			search = []*Account{acct}
		}
		// Empty rather than nil: an empty list of results is a result, and a
		// caller that gets `null` where it expected a list has to special-case
		// nothing-matched twice.
		out := []hit{}
		for _, acct := range search {
			aq := q
			if rest != "" {
				aq.Box = resolveBox(rest, acct.Mirrored)
			}
			hits, err := d.Mirror.Search(acct.Name, aq)
			if err != nil {
				resp.Code, resp.Error = "usage", err.Error()
				return resp
			}
			out = append(out, viewHits(acct, hits)...)
		}
		// Ranked within an account; newest first across them, because two
		// accounts' relevance scores are not comparable and pretending they are
		// would put a five-year-old mail above today's for no visible reason.
		if len(search) > 1 {
			sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
			if len(out) > q.Limit {
				out = out[:q.Limit]
			}
		}
		resp.OK, resp.Data = true, out
		return resp

	case "seen", "unseen":
		acct, refs, err := d.refs(req)
		if err != nil {
			return refsFail(resp, err)
		}
		results, err := acct.Writer.SetSeen(ctx, refs, req.Cmd[0] == "seen")
		return d.wrote(acct, resp, results, err)

	case "move", "trash", "spam":
		acct, refs, err := d.refs(req)
		if err != nil {
			return refsFail(resp, err)
		}
		dest := "Trash"
		if req.Cmd[0] == "spam" {
			// Junk is the provider's, not ours: this files mail where the
			// server's own spam handling can see it, and blocks nobody. The
			// sender-level decision is `route set --to block`.
			if dest, err = junkBox(acct); err != nil {
				resp.Code, resp.Error = "usage", err.Error()
				return resp
			}
		}
		if req.Cmd[0] == "move" {
			to, _ := req.Args["to"].(string)
			if to == "" {
				resp.Code, resp.Error = "usage", "move needs --to BOX"
				return resp
			}
			// A move within one account. Moving mail between accounts is a
			// different operation — a copy and a delete over two servers — and
			// this is not it.
			_, box := splitAccount(to, d.accountNames())
			dest = resolveBox(box, acct.Mirrored)
		}
		if req.Cmd[0] == "trash" {
			// A binned message should count as unread for nobody. Set \Seen
			// now, while the uid we hold is still the message's — after the
			// move it has a new one in Trash, which the Mirror never sees.
			if _, err := acct.Writer.SetSeen(ctx, refs, true); err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
		}
		results, err := acct.Writer.Move(ctx, refs, dest)
		return d.wrote(acct, resp, results, err)

	case "send":
		return d.handleSend(ctx, req, resp)

	case "reply":
		return d.handleReply(ctx, req, resp)

	case "forward":
		return d.handleForward(ctx, req, resp)

	case "status":
		resp.Problems = d.Problems()
		out := []map[string]any{}
		for _, acct := range d.accounts() {
			f, err := d.Mirror.Folder(acct.Name, "INBOX")
			if err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
			out = append(out, map[string]any{
				"account": acct.Name, "primary": acct.Primary,
				"folder": f.Name, "uidvalidity": f.UIDValidity,
				"uidnext": f.UIDNext, "highestmodseq": f.HighestModSeq, "count": f.Count,
				"boxes": len(acct.Mirrored), "watched": acct.Watched,
			})
		}
		resp.OK, resp.Data = true, out
		return resp
	}

	resp.Code = "usage"
	resp.Error = fmt.Sprintf("unknown command %q", strings.Join(req.Cmd, " "))
	return resp
}

// domainOf says which loop owns the data a command answers from, so a reply can
// carry that loop's freshness rather than an average of both.
func domainOf(cmd string) domain {
	switch cmd {
	case "calendar", "agenda", "event", "todo", "habit", "contact":
		return domainDAV
	case "status":
		return domainBoth
	}
	return domainMail
}

// nudgeKinds is the collections a command reads. Events and task lists go
// together: an agenda shows both, and a Habit is an Event that a Todo is often
// standing next to.
func nudgeKinds(cmd string) []string {
	if cmd == "contact" {
		return []string{"cards"}
	}
	return []string{"events", "tasks"}
}

// partsOf reads a Message and what it carries, both from the Mirror. Listing
// what is attached never goes to the server; only saving one does (ADR-0003).
func (d *Daemon) partsOf(a *Account, folder string, uid uint32) ([]mirror.Part, error) {
	r, err := d.Mirror.Row(a.Name, folder, uid)
	if err != nil {
		return nil, err
	}
	return d.Mirror.Parts(r.Message.ID)
}

// idNotFound is a refs() error for an id that resolved to a real Box and uid but
// matched no Message. It carries the id as the caller typed it so every write
// command answers the same way the read commands do.
type idNotFound struct{ id string }

func (e idNotFound) Error() string { return noSuchMessage(e.id) }

// noSuchMessage is what every command says for an id that parsed but matches no
// Message: the id as the caller typed it, then the reason it is usually gone —
// it was moved or expunged between the listing that printed it and now. It does
// not mention the Mirror; from the outside there is only "the mail", and the
// id is stale whether or not a sync is pending.
func noSuchMessage(id string) string {
	return fmt.Sprintf("%s: no such message — moved or deleted since it was listed", id)
}

// refsFail answers a refs() error. An id that named nothing in the Mirror is
// not_found — it was most likely expunged — and everything else is a usage
// mistake in what the caller typed.
func refsFail(resp Response, err error) Response {
	var nf idNotFound
	if errors.As(err, &nf) {
		resp.Code, resp.Error = "not_found", err.Error()
		return resp
	}
	resp.Code, resp.Error = "usage", err.Error()
	return resp
}

// fail turns a Mirror read error into the right reply. An id the Mirror does
// not hold is not_found rather than a failure: it may have been expunged, and
// the Mirror may be Behind.
func fail(resp Response, id string, err error) Response {
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", noSuchMessage(id)
		return resp
	}
	resp.Code, resp.Error = "api", err.Error()
	return resp
}

// pick chooses the part a save was asked for. A Message with exactly one
// attachment can be named without an index, because that is how people refer to
// it; a Message with several cannot, and the error says what to type instead.
func pick(a *Account, folder string, uid uint32, parts []mirror.Part, index int) (mirror.Part, error) {
	if len(parts) == 0 {
		return mirror.Part{}, fmt.Errorf("%s has nothing attached", a.messageID(folder, uid))
	}
	if index == 0 {
		if len(parts) > 1 {
			ids := make([]string, 0, len(parts))
			for i := range parts {
				ids = append(ids, attachmentID(a, folder, uid, i+1))
			}
			return mirror.Part{}, fmt.Errorf("%s has %d attachments: name one of %s",
				a.messageID(folder, uid), len(parts), strings.Join(ids, ", "))
		}
		index = 1
	}
	if index < 1 || index > len(parts) {
		return mirror.Part{}, fmt.Errorf("no attachment %d on %s (it has %d)",
			index, a.messageID(folder, uid), len(parts))
	}
	return parts[index-1], nil
}

// save fetches one part and writes it to disk. The Daemon writes the file
// rather than sending the bytes back over the socket: they are the same user on
// the same machine, and a 20 MB PDF has no business being base64 in NDJSON.
func (d *Daemon) save(ctx context.Context, a *Account, folder string, uid uint32, part mirror.Part, out string, force bool) (saved, error) {
	if a.Reconciler == nil {
		return saved{}, errors.New("this daemon cannot fetch: no server connection")
	}
	if out == "" {
		return saved{}, errors.New("no output path given")
	}
	path := out
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		path = filepath.Join(out, part.Name())
	}
	if _, err := os.Stat(path); err == nil && !force {
		return saved{}, fmt.Errorf("%s exists (use --force to overwrite)", path)
	}
	body, err := a.Reconciler.Driver.FetchPart(ctx, folder, uid, part.Path)
	if err != nil {
		return saved{}, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return saved{}, err
	}
	return saved{
		Path: path, Bytes: len(body), Filename: part.Name(), MIMEType: part.MIMEType,
	}, nil
}

// saved is what an attachment save did.
type saved struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
}

// attachmentID names one file on one Message: the Placement id, then which
// file. Index is 1-based and matches the listing.
func attachmentID(a *Account, folder string, uid uint32, index int) string {
	return fmt.Sprintf("%s:%d", a.messageID(folder, uid), index)
}

// attachment is one row of a listing.
type attachment struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	MIMEType    string `json:"mime_type"`
	Disposition string `json:"disposition"`
	Size        int64  `json:"size"`
}

func viewParts(a *Account, folder string, uid uint32, parts []mirror.Part) []attachment {
	out := make([]attachment, 0, len(parts))
	for i, p := range parts {
		out = append(out, attachment{
			ID: attachmentID(a, folder, uid, i+1), Index: i + 1,
			Filename: p.Name(), MIMEType: p.MIMEType,
			Disposition: p.Disposition, Size: p.Size,
		})
	}
	return out
}

// parseAttachmentID reads [box:]uid[:index]. The index is optional because a
// Message with one attachment is named by the Message.
func parseAttachmentID(value string, known []string) (folder string, uid uint32, index int, err error) {
	v := strings.TrimSpace(value)
	if i := strings.LastIndex(v, ":"); i >= 0 {
		if n, convErr := strconv.Atoi(v[i+1:]); convErr == nil && n > 0 {
			if f, u, mErr := parseMessageID(v[:i], known); mErr == nil {
				return f, u, n, nil
			}
		}
	}
	f, u, err := parseMessageID(v, known)
	return f, u, 0, err
}

// refs turns the ids a write command was given into Placements to act on. They
// all have to name the same account: a single STORE or MOVE goes to one server,
// and an id list spanning two of them is a mistake worth naming rather than two
// half-done commands.
func (d *Daemon) refs(req Request) (*Account, []mailsync.Ref, error) {
	var ids []string
	switch v := req.Args["positional"].(type) {
	case string:
		ids = []string{v}
	case []any:
		for _, e := range v {
			s, _ := e.(string)
			ids = append(ids, s)
		}
	case []string:
		ids = v
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("no message id given")
	}
	var acct *Account
	refs := make([]mailsync.Ref, 0, len(ids))
	for _, id := range ids {
		a, folder, uid, err := d.resolveID(id)
		if err != nil {
			return nil, nil, err
		}
		// An id that parses but names nothing in the Mirror is the common
		// case — it was expunged since the listing that printed it. Catch it
		// here so the reply says so, rather than after a pointless server
		// round trip that fails with a folder-shaped "INBOX: not found".
		if _, err := d.Mirror.Row(a.Name, folder, uid); errors.Is(err, mirror.ErrNotFound) {
			return nil, nil, idNotFound{id}
		} else if err != nil {
			return nil, nil, err
		}
		if acct == nil {
			acct = a
		} else if !strings.EqualFold(acct.Name, a.Name) {
			return nil, nil, fmt.Errorf("%s and %s are on different accounts — one command, one account", ids[0], id)
		}
		refs = append(refs, mailsync.Ref{Folder: folder, UID: uid})
	}
	if acct.Writer == nil {
		return nil, nil, errors.New("this daemon cannot write: no server connection")
	}
	return acct, refs, nil
}

// wrote finishes a write command: it reports what the server acked and tells
// every listener which Boxes moved. A move the server could not place — no
// UIDPLUS — also asks for a cycle, because only a cycle can find it.
func (d *Daemon) wrote(a *Account, resp Response, results []mailsync.Result, err error) Response {
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	boxes := map[string]struct{}{}
	needCycle := false
	out := make([]change, 0, len(results))
	for _, r := range results {
		c := change{ID: a.messageID(r.Folder, r.UID), Box: r.Folder, Flags: r.Flags, Seen: hasFlag(r.Flags, `\Seen`)}
		boxes[r.Folder] = struct{}{}
		if r.NewFolder != "" {
			c.Moved, c.Box = true, r.NewFolder
			boxes[r.NewFolder] = struct{}{}
			if r.NewUID != 0 {
				c.NewID = a.messageID(r.NewFolder, r.NewUID)
			} else {
				needCycle = true
			}
		}
		out = append(out, c)
	}
	for box := range boxes {
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	if needCycle {
		d.kickAccount(a, "write")
	}
	resp.OK, resp.Data = true, out
	return resp
}

// routingOrder is the order Boxes are listed in: the way mail moves through
// them, which is what somebody scanning the list is following. Alphabetical
// would put Aside first and the Inbox fourth.
//
// These are the Boxes the Routing fills plus the ones every account has. They
// are matched on the short name, so `Feed` finds `INBOX/Feed` wherever the
// server keeps it.
var routingOrder = []string{
	"INBOX", "Feed", "Paper Trail", "Screener", "Aside", "Sent", "Drafts", "Junk",
}

// listedBoxes is which Boxes a listing shows. By default the ones above and
// nothing else: an account of sixty-odd Boxes is fifty-seven Archive folders
// and a scratch folder somebody's test left behind, and burying the eight that
// matter in them is not a listing, it is a haystack.
//
// `Screener/Block` is left out too. It is where a blocked sender's waiting mail
// went so that a mistake can still be found — worth having, not worth a line in
// every listing.
func listedBoxes(a *Account, everything bool) []string {
	byShort := map[string]string{}
	for _, folder := range a.Mirrored {
		byShort[strings.ToLower(shortBox(folder, a.Mirrored))] = folder
	}
	var out []string
	taken := map[string]bool{}
	for _, want := range routingOrder {
		if folder, ok := byShort[strings.ToLower(want)]; ok {
			out = append(out, folder)
			taken[folder] = true
		}
	}
	if !everything {
		return out
	}
	// Everything else after them, in the order the server lists it, so the
	// archive tree reads as a tree.
	rest := make([]string, 0, len(a.Mirrored))
	for _, folder := range a.Mirrored {
		if !taken[folder] {
			rest = append(rest, folder)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// boxRow is one Box in a listing: the name to read it with, and how much of it
// is here.
type boxRow struct {
	Box     string `json:"box"`
	Folder  string `json:"folder"`
	Account string `json:"account"`
	Count   int    `json:"count"`
	Unseen  int    `json:"unseen"`
	Watched bool   `json:"watched"`
}

// junkBox is where this account's server keeps spam. It is found by name among
// the mirrored Boxes rather than configured: every provider has one and they
// disagree only on what to call it.
func junkBox(a *Account) (string, error) {
	for _, want := range []string{"junk", "spam"} {
		if name, ok := boxNamedLeaf(a, want); ok {
			return name, nil
		}
	}
	return "", errors.New("this account has no junk box; move the mail with --to instead")
}

// change is what one write did to one Message.
type change struct {
	ID string `json:"id"`
	// NewID is empty when the Message moved to a Box the Mirror does not hold,
	// or when the server did not say where it landed.
	NewID string   `json:"new_id,omitempty"`
	Box   string   `json:"box"`
	Moved bool     `json:"moved"`
	Flags []string `json:"flags,omitempty"`
	Seen  bool     `json:"seen"`
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// parseMessageID reads the [box:]uid a listing printed back into a Placement.
// A bare uid means the Inbox, which is the Box an agent is usually looking at.
func parseMessageID(value string, known []string) (string, uint32, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", 0, errors.New("message id must be [box:]uid")
	}
	folder := "INBOX"
	if i := strings.LastIndex(v, ":"); i >= 0 {
		folder, v = resolveBox(strings.TrimSpace(v[:i]), known), strings.TrimSpace(v[i+1:])
	}
	uid, err := strconv.ParseUint(v, 10, 32)
	if err != nil || uid == 0 {
		return "", 0, fmt.Errorf("message id must be [box:]uid, got %q", value)
	}
	return folder, uint32(uid), nil
}

// resolveBox maps what a caller typed onto a mirrored folder name. A Box under
// the Inbox answers to its short name — `Screener`, not `INBOX/Screener` —
// because that is the name its ids are printed with. A Box named outright wins
// over one that only matches with the prefix put back, so a top-level folder is
// never shadowed by a child of the Inbox with the same name.
func resolveBox(name string, known []string) string {
	if strings.EqualFold(name, "inbox") {
		return "INBOX"
	}
	for _, k := range known {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	for _, k := range known {
		if strings.EqualFold(k, "INBOX/"+name) {
			return k
		}
	}
	return name
}

// shortBox is the name a Box is printed with. Everything under the Inbox loses
// the prefix, unless a Box of that name exists at the top level too — there the
// short form would name two Boxes, so neither gets it.
func shortBox(folder string, known []string) string {
	short, ok := strings.CutPrefix(folder, "INBOX/")
	if !ok {
		return folder
	}
	for _, k := range known {
		if !strings.EqualFold(k, folder) && strings.EqualFold(k, short) {
			return folder
		}
	}
	return short
}

type row struct {
	ID      string `json:"id"`
	UID     uint32 `json:"uid"`
	Date    string `json:"date"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Seen    bool   `json:"seen"`
	Body    string `json:"body_state"`
}

// formatMessageID is the id a caller hands back to message view. The Inbox is
// implicit, because that is the Box most ids come from, and its children are
// named without it: `Screener:342`, not `INBOX/Screener:342`.
func formatMessageID(folder string, uid uint32, known []string) string {
	if folder == "INBOX" {
		return fmt.Sprintf("%d", uid)
	}
	return fmt.Sprintf("%s:%d", shortBox(folder, known), uid)
}

// message is one Message read whole: its headers, its text, and every Box it
// sits in. Text is what the Mirror holds — HTML is rendered as Markdown, so a
// caller reading this never has to parse HTML (ADR-0003 keeps attachments out).
type message struct {
	ID         string   `json:"id"`
	UID        uint32   `json:"uid"`
	Box        string   `json:"box"`
	Date       string   `json:"date"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Subject    string   `json:"subject"`
	Seen       bool     `json:"seen"`
	Flags      []string `json:"flags"`
	Size       int64    `json:"size"`
	MessageKey string   `json:"message_key"`
	Body       string   `json:"body"`
	BodyFormat string   `json:"body_format"` // "plain" | "markdown" | "none"
	// BodyHTML is the raw HTML part, untouched, for a client that renders it
	// itself (a desktop reading pane). Empty when the message had no HTML part.
	// The text `Body` above stays the canonical read (ADR-0003); this is extra.
	BodyHTML   string   `json:"body_html,omitempty"`
	BodyState  string   `json:"body_state"`
	Placements []string `json:"placements"`
}

func viewMessage(a *Account, folder string, r mirror.Row, places []mirror.Placement) message {
	body, format := renderBody(r.Message)
	m := message{
		ID: a.messageID(folder, r.Placement.UID), UID: r.Placement.UID, Box: folder,
		From: r.From, To: r.To, Subject: r.Subject, Seen: r.Seen(),
		Flags: r.Placement.Flags, Size: r.Placement.Size, MessageKey: r.Message.Key,
		Body: body, BodyFormat: format, BodyState: r.BodyState,
		BodyHTML: r.Message.TextHTML,
	}
	if !r.Message.Date.IsZero() {
		m.Date = r.Message.Date.Format(time.RFC3339)
	}
	for _, p := range places {
		m.Placements = append(m.Placements, a.messageID(p.Folder, p.UID))
	}
	return m
}

// renderBody picks the text to show. A message with both parts is shown as its
// plain one: converting HTML we did not need to is how a signature turns into a
// wall of markup.
func renderBody(m mirror.Message) (body, format string) {
	if strings.TrimSpace(m.TextPlain) != "" {
		return terminal.SanitizeText(m.TextPlain), "plain"
	}
	if strings.TrimSpace(m.TextHTML) != "" {
		return terminal.SanitizeText(htmlmd.HTMLToMarkdown(m.TextHTML)), "markdown"
	}
	return "", "none"
}

// hit is one search result. It carries the Box, because a result list crosses
// all of them, and the text around the match, because an agent that has to read
// each result to find out why it matched has been given a worse answer.
type hit struct {
	row
	Box     string `json:"box"`
	Snippet string `json:"snippet"`
}

func viewHits(a *Account, hits []mirror.Hit) []hit {
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		r := viewRows(a, h.Placement.Folder, []mirror.Row{h.Row})[0]
		out = append(out, hit{
			row: r, Box: h.Placement.Folder,
			Snippet: terminal.SanitizeLine(h.Snippet),
		})
	}
	return out
}

func viewRows(a *Account, folder string, rows []mirror.Row) []row {
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		id := a.messageID(folder, r.UID)
		date := ""
		if !r.Message.Date.IsZero() {
			date = r.Message.Date.Format("2006-01-02 15:04")
		}
		out = append(out, row{
			ID: id, UID: r.UID, Date: date, From: r.From,
			Subject: r.Subject, Seen: r.Seen(), Body: r.BodyState,
		})
	}
	return out
}
