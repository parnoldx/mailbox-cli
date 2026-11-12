package daemon

// Trash is the one Box the Mirror does not hold (ADR-0003): mail is put there
// to stop being findable, so it is never mirrored, never counted and never
// searched. Seeing it is therefore the one read that goes to the server: the
// exclusion is about what is kept, not about what may be asked for. Nothing
// here writes: the Mirror gains no row, so a Message read out of Trash is
// read-only, and the next listing asks the server again.

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

// rowOrServer reads the Row an id names: from the Mirror where the Box is held,
// and from the server where it is not (Trash and everything under it). remote
// reports which happened, so a caller can leave out what only a Mirror answers:
// a Thread, a Message's other Placements, an invite card.
func (d *Daemon) rowOrServer(ctx context.Context, id string) (acct *Account, folder string, uid uint32, r mirror.Row, remote bool, err error) {
	acct, folder, uid, err = d.resolveID(id)
	if err != nil {
		return nil, "", 0, mirror.Row{}, false, usageErr("%s", err)
	}
	held, ok, err := d.onServer(ctx, acct, folder)
	if err != nil {
		return nil, "", 0, mirror.Row{}, false, err
	}
	if ok {
		r, err := d.serverRow(ctx, acct, held, uid)
		return acct, held, uid, r, true, err
	}
	r, err = d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return nil, "", 0, mirror.Row{}, false, notFoundErr("%s", noSuchMessage(id))
	}
	return acct, folder, uid, r, false, err
}

// onServer resolves name to a Box the Mirror does not hold, if that is what it
// names. The folder comes back in the server's own spelling, which the Mirror
// never learned. ok is false for a Box the Mirror holds (the local path answers
// those) and for a name that is on neither side, which keeps the empty listing
// it always was.
func (d *Daemon) onServer(ctx context.Context, a *Account, name string) (folder string, ok bool, err error) {
	if a == nil || a.Reconciler == nil || name == "" {
		return "", false, nil
	}
	// The Mirror's own Boxes are answered locally and must not cost a round
	// trip: a read never touches the network (ADR-0001).
	if _, held := a.boxNamed(name); held {
		return "", false, nil
	}
	all, err := a.Reconciler.Driver.Folders(ctx)
	if err != nil {
		return "", false, fmt.Errorf("list boxes: %w", err)
	}
	folder = resolveBox(name, all)
	for _, f := range all {
		if !strings.EqualFold(f, folder) {
			continue
		}
		if _, held := a.boxNamed(f); held {
			return "", false, nil
		}
		return f, true, nil
	}
	return "", false, nil
}

// mergedServerView lists a Box the Mirror does not hold from every account that
// has one, newest first. ok is false for fewer than two accounts, the same way
// mergedView decides: one account is listed exactly as it always was.
//
// A Box any account holds is not this path at all, whatever the other accounts
// say about it: mergedView above already answered for an account that has it,
// and only the accounts that do not would be asked here. Asked, they would
// answer from the server, which is a round trip on a listing that is local
// (ADR-0001). Trash is the Box nobody holds.
func (d *Daemon) mergedServerView(ctx context.Context, box string, limit int) ([]row, bool, error) {
	for _, a := range d.accounts() {
		if _, held := a.boxNamed(resolveBox(box, a.Mirrored)); held {
			return nil, false, nil
		}
	}
	type part struct {
		acct   *Account
		folder string
	}
	var parts []part
	for _, a := range d.accounts() {
		folder, ok, err := d.onServer(ctx, a, resolveBox(box, a.Mirrored))
		if err != nil {
			return nil, false, err
		}
		if ok {
			parts = append(parts, part{a, folder})
		}
	}
	if len(parts) < 2 {
		return nil, false, nil
	}
	var out []row
	for _, p := range parts {
		rows, err := d.serverRows(ctx, p.acct, p.folder, limit)
		if err != nil {
			return nil, false, err
		}
		out = append(out, rows...)
	}
	// The date is local "2006-01-02 15:04", which sorts as text; a row with
	// none sorts last. Stable, so a tie keeps the Primary first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, true, nil
}

// serverRows lists a Box the Mirror does not hold, newest first. Only headers
// come home: a Message's text is fetched when that Message is opened.
//
// A driver that answers `AllUIDs` out of its own store rather than the server
// (Graph does) has nothing to say about a Box the Mirror never held, so a
// Microsoft 365 account's Trash lists empty until that driver grows a real
// folder listing. IMAP asks the server and answers.
func (d *Daemon) serverRows(ctx context.Context, a *Account, folder string, limit int) ([]row, error) {
	uids, err := a.Reconciler.Driver.AllUIDs(ctx, folder)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", folder, err)
	}
	// Uids ascend with arrival, so the newest limit of them is the tail: no
	// reason to ask the server about mail nobody is going to look at.
	if limit > 0 && len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	envs, err := a.Reconciler.Driver.FetchEnvelopes(ctx, folder, uids)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", folder, err)
	}
	out := make([]row, 0, len(envs))
	for _, e := range envs {
		out = append(out, serverListRow(a, folder, e))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].UID > out[j].UID
	})
	return out, nil
}

// serverListRow is one listed Message. It is shaped like a mirrored row so a
// client draws it the same way, and marked read-only so the client knows the
// triage it offers everywhere else cannot act on this one.
func serverListRow(a *Account, folder string, e mailsync.Envelope) row {
	r := row{
		ID: a.messageID(folder, e.UID), Account: a.label(), UID: e.UID,
		From: e.From, Subject: e.Subject, Seen: slices.Contains(e.Flags, `\Seen`),
		Labels: mirror.LabelsOf(e.Flags), ReadOnly: true,
	}
	if !e.Date.IsZero() {
		r.Date = e.Date.Local().Format("2006-01-02 15:04")
	}
	return r
}

// serverRow reads one Message out of a Box the Mirror does not hold, as the Row
// the view builders already know how to draw. The text is fetched too, because
// there is no Mirror copy to fall back on. BodyState stays "pending" when the
// server answered with headers only, which is what a reader is told to retry.
func (d *Daemon) serverRow(ctx context.Context, a *Account, folder string, uid uint32) (mirror.Row, error) {
	envs, err := a.Reconciler.Driver.FetchEnvelopes(ctx, folder, []uint32{uid})
	if err != nil {
		return mirror.Row{}, fmt.Errorf("read %s: %w", folder, err)
	}
	if len(envs) == 0 {
		return mirror.Row{}, notFoundErr("%s", noSuchMessage(fmt.Sprintf("%s:%d", folder, uid)))
	}
	e := envs[0]
	r := mirror.Row{
		Placement: mirror.Placement{
			Folder: folder, UID: uid, Flags: e.Flags,
			InternalDate: e.InternalDate, Size: e.Size,
		},
		Message: mirror.Message{
			Key: e.MessageID, Date: e.Date, Subject: e.Subject, From: e.From,
			To: e.To, Cc: e.Cc, InReplyTo: e.InReplyTo, References: e.References,
			BodyState: "pending",
		},
	}
	bodies, err := a.Reconciler.Driver.FetchBodies(ctx, folder, []uint32{uid})
	if err != nil {
		return mirror.Row{}, fmt.Errorf("read %s: %w", folder, err)
	}
	for _, b := range bodies {
		if b.UID != uid {
			continue
		}
		r.Message.TextPlain, r.Message.TextHTML = b.Plain, b.HTML
		r.Message.BodyState = "mirrored"
	}
	return r, nil
}
