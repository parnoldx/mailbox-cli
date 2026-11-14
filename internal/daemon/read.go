package daemon

// The read half of the socket: the handlers for everything that answers out of
// the Mirror, the id and Thread helpers they share, and the Box listing rules.
// Nothing here writes, and nothing here goes to the network except where a Box
// is deliberately not mirrored (trash.go).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// handleBoxList lists every account's Boxes. Both filters are applied here
// rather than while printing, so a flag means the same thing whether a caller reads the table or the JSON.
func (d *Daemon) handleBoxList(req Request, resp Response) Response {
	unreadOnly := req.Bool("unread")
	everything := req.Bool("archive")
	out := []boxRow{}
	for _, acct := range d.accounts() {
		counts, err := d.Mirror.BoxCounts(acct.Name)
		if err != nil {
			return resp.api(err.Error())
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
				Account: acct.label(),
				Count:   c.Count,
				Unseen:  c.Unseen,
				Watched: slices.Contains(acct.Watched, folder),
			})
		}
	}
	return resp.ok(out)
}

// handleBoxView lists one Box. A Box is named the same way a Message is:
// `[account/]box`, with the Primary Account implicit (ADR-0005).
// With no account named, a Box several accounts have is listed from all
// of them: a listing spans accounts, an id names one.
func (d *Daemon) handleBoxView(ctx context.Context, req Request, resp Response) Response {
	name := req.Str("positional")
	prefix, rest := splitAccount(name, d.accountNames())
	limit := req.Int("limit", 50)
	if prefix == "" {
		merged, ok, err := d.mergedView(rest, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		if ok {
			return resp.ok(merged)
		}
		// Trash spans accounts the way everything else does, and it is the one
		// Box the mergedView above never sees, because no account holds it.
		merged, ok, err = d.mergedServerView(ctx, rest, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		if ok {
			return resp.ok(merged)
		}
	}
	acct, err := d.accountNamed(prefix)
	if err != nil {
		return resp.usage(err.Error())
	}
	folder := "INBOX"
	if rest != "" {
		folder = resolveBox(rest, acct.Mirrored)
	}
	// Trash and everything under it is not mirrored (ADR-0003), so listing it
	// is a server read. It writes nothing.
	if held, ok, err := d.onServer(ctx, acct, folder); err != nil {
		return resp.api(err.Error())
	} else if ok {
		rows, err := d.serverRows(ctx, acct, held, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(rows)
	}
	rows, err := d.Mirror.Rows(acct.Name, folder, limit)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewRows(acct, folder, rows, d.threadSizesFor(acct.Name, rows)))
}

func (d *Daemon) handleMessageView(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, r, remote, err := d.rowOrServer(ctx, id)
	if err != nil {
		return resp.failed(err)
	}
	if remote {
		m := viewMessage(acct, folder, r, []mirror.Placement{r.Placement})
		m.ReadOnly = true
		return resp.ok(m)
	}
	places, err := d.Mirror.Placements(acct.Name, r.Message.ID)
	if err != nil {
		return resp.api(err.Error())
	}
	m := d.withInvite(ctx, acct, folder, uid, r.Message.ID, r.Message.Key, r.To, viewMessage(acct, folder, r, places))
	return resp.ok(m)
}

func (d *Daemon) handleThreadView(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, _, r, remote, err := d.rowOrServer(ctx, id)
	if err != nil {
		return resp.failed(err)
	}
	// Trash holds no mirrored Placement, so there is no conversation to build
	// (ADR-0008): the one Message comes back on its own, read-only.
	if remote {
		m := viewMessage(acct, folder, r, []mirror.Placement{r.Placement})
		m.ReadOnly = true
		return resp.ok([]message{m})
	}
	// A Thread never crosses an Account: the same conversation reaching two
	// accounts is two Threads (ADR-0008).
	rows, err := d.Mirror.Thread(acct.Name, r.Message.ThreadID)
	if err != nil {
		return resp.api(err.Error())
	}
	out := make([]message, 0, len(rows))
	for _, row := range rows {
		m := viewMessage(acct, row.Placement.Folder, row, nil)
		m.HasInvite = d.hasCalendarPart(acct, row.Message.ID)
		out = append(out, m)
	}
	return resp.ok(out)
}

// handleInviteShow draws an invite card: the one server read in the invite path, and only on demand: a
// client asks for it when it is about to draw the card, not when it
// lists the thread. The cache in loadInvite makes the second ask free.
func (d *Daemon) handleInviteShow(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, r, err := d.rowOf(id)
	if err != nil {
		return resp.failed(err)
	}
	card := d.inviteCardOf(ctx, acct, folder, uid, r.Message.ID, r.Message.Key, r.To)
	if card == nil {
		return resp.ok(nil)
	}
	return resp.ok(card)
}

func (d *Daemon) handleSearch(ctx context.Context, req Request, resp Response) Response {
	text := req.Str("positional")
	q := mirror.Query{Text: text, From: req.Str("from"), Limit: req.Int("limit", 25)}
	// `--in` may name an account's Box, and it may name the account alone.
	box := req.Str("in")
	prefix, rest := splitAccount(box, d.accountNames())
	search := d.accounts()
	if prefix != "" {
		acct, err := d.accountNamed(prefix)
		if err != nil {
			return resp.usage(err.Error())
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
			return resp.usage(err.Error())
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
	return resp.ok(out)
}

func (d *Daemon) handleSeen(ctx context.Context, req Request, resp Response) Response {
	acct, refs, err := d.refs(ctx, req, false)
	if err != nil {
		return resp.failed(err)
	}
	results, err := acct.Writer.SetSeen(ctx, refs, req.Cmd[0] == "seen")
	return d.wrote(acct, resp, results, err)
}

func (d *Daemon) handleMove(ctx context.Context, req Request, resp Response) Response {
	// Move and spam are the two writes that take Trash mail back out, so an id
	// there is allowed even though the Mirror holds no Placement for it.
	// `trash` is not one of them: mail already there has nowhere to go.
	acct, refs, err := d.refs(ctx, req, req.Cmd[0] != "trash")
	if err != nil {
		return resp.failed(err)
	}
	if refs, err = d.threaded(acct.Name, refs); err != nil {
		return resp.api(err.Error())
	}
	dest := "Trash"
	if req.Cmd[0] == "spam" {
		// Junk is the provider's, not ours: this files mail where the
		// server's own spam handling can see it, and blocks nobody. The
		// sender-level decision is `route set --to block`.
		if dest, err = junkBox(acct); err != nil {
			return resp.usage(err.Error())
		}
	}
	if req.Cmd[0] == "move" {
		to := req.Str("to")
		if to == "" {
			return resp.usage("move needs --to BOX")
		}
		// A move within one account. Moving mail between accounts is a
		// different operation, a copy and a delete over two servers, and
		// this is not it.
		_, box := splitAccount(to, d.accountNames())
		dest = resolveBox(box, acct.Mirrored)
	}
	if req.Cmd[0] == "trash" {
		// A binned message should count as unread for nobody. Set \Seen
		// now, while the uid we hold is still the message's, after the
		// move it has a new one in Trash, which the Mirror never sees.
		if _, err := acct.Writer.SetSeen(ctx, refs, true); err != nil {
			return resp.api(err.Error())
		}
	}
	results, err := acct.Writer.Move(ctx, refs, dest)
	return d.wrote(acct, resp, results, err)
}

// handleAccountList is what a client needs to paint an account: the name `box view` takes,
// the address mail leaves from, and its colour. A row's own account
// field is empty for the Primary; primary says which entry that is.
func (d *Daemon) handleAccountList(resp Response) Response {
	out := []accountRow{}
	for _, a := range d.accounts() {
		out = append(out, accountRow{
			Name: a.Name, Email: a.From.Addr, Primary: a.Primary, Color: a.Color,
		})
	}
	return resp.ok(out)
}

func (d *Daemon) handleStatus(resp Response) Response {
	resp.Problems = d.Problems()
	resp.Inferred = d.RecentInferred()
	out := []map[string]any{}
	for _, acct := range d.accounts() {
		f, err := d.Mirror.Folder(acct.Name, "INBOX")
		if err != nil {
			return resp.api(err.Error())
		}
		out = append(out, map[string]any{
			"account": acct.Name, "primary": acct.Primary,
			"from": acct.From.Addr, "from_name": acct.From.Name,
			"folder": f.Name, "uidvalidity": f.UIDValidity,
			"uidnext": f.UIDNext, "highestmodseq": f.HighestModSeq, "count": f.Count,
			"boxes": len(acct.Mirrored), "watched": acct.Watched,
		})
	}
	return resp.ok(out)
}

// refs turns the ids a write command was given into Placements to act on. They
// all have to name the same account: a single STORE or MOVE goes to one server,
// and an id list spanning two of them is a mistake worth naming rather than two
// half-done commands.
//
// unheld says an id in a Box the Mirror does not hold (Trash) is allowed: there
// is no Row to read there, so the id is confirmed against the server and the id
// itself is the whole ref. That confirmation is one folder listing per Box and
// never a body: a move of twenty Trashed Messages is one FETCH, not twenty
// opens. Every other write refuses those: nothing about a Trashed Message can
// be flagged, labelled or piled, and nothing is asked of the server to say so.
//
// What the caller typed wrong carries its own Code: usage, or not_found for an
// id that names nothing (most likely expunged). Confirming a Trash id does talk
// to the server, so a network failure there comes back as it is.
func (d *Daemon) refs(ctx context.Context, req Request, unheld bool) (*Account, []mailsync.Ref, error) {
	ids := req.Strings("positional")
	if len(ids) == 0 {
		return nil, nil, usageErr("no message id given")
	}
	// Every id keeps the place the caller gave it, so that the reply's changes
	// line up with the ids typed even when some of them are the server's.
	type slot struct {
		ref mailsync.Ref
		id  string
	}
	slots := make([]slot, 0, len(ids))
	var acct *Account
	// The Trash ids, grouped by the Box they name. A Box is confirmed with one
	// round trip however many ids are in it, so the group is the unit of work.
	type boxKey struct{ account, box string }
	var order []boxKey
	grouped := map[boxKey][]int{}
	for _, id := range ids {
		a, folder, uid, err := d.resolveID(id)
		if err != nil {
			return nil, nil, usageErr("%s", err)
		}
		if acct == nil {
			acct = a
		} else if !strings.EqualFold(acct.Name, a.Name) {
			return nil, nil, usageErr("%s and %s are on different accounts, one command, one account", ids[0], id)
		}
		if _, held := a.boxNamed(folder); !held {
			if !unheld {
				// Trash is the one Box the Mirror does not hold (ADR-0003), so an
				// id there names nothing a flag or a label could change. `trash`
				// says the mail is already where it was going; every other write
				// names the two that do act there, rather than "deleted since it
				// was listed" about mail that was listed a moment ago.
				if req.Cmd[0] == "trash" {
					return nil, nil, usageErr("%s is already in Trash, nothing to move", id)
				}
				return nil, nil, notFoundErr("%s is in Trash, which is not held locally: only move and spam act on it", id)
			}
			// The Box is spelled the way it was typed until the server's own
			// spelling is learnt below, so the grouping folds case itself.
			k := boxKey{a.Name, strings.ToLower(folder)}
			if _, seen := grouped[k]; !seen {
				order = append(order, k)
			}
			grouped[k] = append(grouped[k], len(slots))
			slots = append(slots, slot{ref: mailsync.Ref{Folder: folder, UID: uid}, id: id})
			continue
		}
		// An id that parses but names nothing in the Mirror is the common case
		//, it was expunged since the listing that printed it. Catch it here so
		// the reply says so, rather than after a pointless server round trip
		// that fails with a folder-shaped "INBOX: not found".
		if _, err := d.Mirror.Row(a.Name, folder, uid); err != nil {
			if errors.Is(err, mirror.ErrNotFound) {
				return nil, nil, notFoundErr("%s", noSuchMessage(id))
			}
			return nil, nil, err
		}
		slots = append(slots, slot{ref: mailsync.Ref{Folder: folder, UID: uid}, id: id})
	}
	for _, k := range order {
		first := slots[grouped[k][0]]
		folder, ok, err := d.onServer(ctx, acct, first.ref.Folder)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, notFoundErr("%s", noSuchMessage(first.id))
		}
		uids := make([]uint32, 0, len(grouped[k]))
		for _, i := range grouped[k] {
			uids = append(uids, slots[i].ref.UID)
		}
		// Headers only: this asks which of the ids are really there, and what
		// one of them says is not part of moving it.
		envs, err := acct.Reconciler.Driver.FetchEnvelopes(ctx, folder, uids)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", folder, err)
		}
		found := make(map[uint32]bool, len(envs))
		for _, e := range envs {
			found[e.UID] = true
		}
		for _, i := range grouped[k] {
			if !found[slots[i].ref.UID] {
				return nil, nil, notFoundErr("%s", noSuchMessage(slots[i].id))
			}
			slots[i].ref.Folder = folder
		}
	}
	refs := make([]mailsync.Ref, 0, len(slots))
	for _, s := range slots {
		refs = append(refs, s.ref)
	}
	if acct.Writer == nil {
		return nil, nil, usageErr("this daemon cannot write: no server connection")
	}
	return acct, refs, nil
}

// threaded expands refs to every Placement of each Message's Thread. A
// grouped listing shows a conversation as one row, so trashing, moving or
// marking spam the id it gives acts on the whole conversation rather than just
// the one Message that happened to be shown (ADR-0008: a Thread is one thing,
// wherever its Messages sit). Deduped, because two refs can expand into the
// same Thread. A ref in a Box the Mirror does not hold (Trash) has no Thread
// here: it stays the one Message it names.
func (d *Daemon) threaded(account string, refs []mailsync.Ref) ([]mailsync.Ref, error) {
	acct, _ := d.accountNamed(account)
	seen := map[mailsync.Ref]bool{}
	out := make([]mailsync.Ref, 0, len(refs))
	for _, ref := range refs {
		if acct != nil {
			if _, held := acct.boxNamed(ref.Folder); !held {
				if !seen[ref] {
					seen[ref] = true
					out = append(out, ref)
				}
				continue
			}
		}
		row, err := d.Mirror.Row(account, ref.Folder, ref.UID)
		if err != nil {
			return nil, err
		}
		members, err := d.Mirror.Thread(account, row.ThreadID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			r := mailsync.Ref{Folder: m.Placement.Folder, UID: m.Placement.UID}
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// threadedWithin keeps the refs it was given and adds each one's Thread
// siblings that sit in one of `boxes`. It is threaded() narrowed to a set of
// Boxes, plus the guarantee that an explicitly named Message always moves: so
// `aside ID` moves ID wherever it is, and also sweeps the rest of its
// conversation out of the Inbox and the other pile, but not its Sent copies
// or a sibling still in the Screener.
func (d *Daemon) threadedWithin(account string, refs []mailsync.Ref, boxes ...string) ([]mailsync.Ref, error) {
	all, err := d.threaded(account, refs)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, b := range boxes {
		keep[b] = true
	}
	seen := map[mailsync.Ref]bool{}
	out := make([]mailsync.Ref, 0, len(all))
	add := func(r mailsync.Ref) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, r := range refs {
		add(r)
	}
	for _, r := range all {
		if keep[r.Folder] {
			add(r)
		}
	}
	return out, nil
}

// routingOrder is the order Boxes are listed in: the way mail moves through
// them, which is what somebody scanning the list is following. Alphabetical
// would put Aside first and the Inbox fourth.
//
// These are the Boxes the Routing fills plus the ones every account has. They
// are matched on the short name, so `Feed` finds `INBOX/Feed` wherever the
// server keeps it.
var routingOrder = []string{
	"INBOX", "Feed", "Paper Trail", "Screener", "Aside", "Reply Later", "Sent", "Drafts", "Junk",
}

// listedBoxes is which Boxes a listing shows. By default the ones above and
// nothing else: an account of sixty-odd Boxes is fifty-seven Archive folders
// and a scratch folder somebody's test left behind, and burying the eight that
// matter in them is not a listing, it is a haystack.
//
// `Screener/Block` is left out too. It is the drop target another client drags
// a block into, and it is emptied as soon as the block is written, worth
// having, not worth a line in every listing.
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
