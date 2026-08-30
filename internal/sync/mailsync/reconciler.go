package mailsync

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"mailbox/internal/htmlmd"
	"mailbox/internal/mirror"
)

// Reconciler brings one account's folders in the Mirror up to date. It is the
// only writer of mirrored mail state.
type Reconciler struct {
	Account string
	Mirror  *mirror.Mirror
	Driver  Driver
	// OnFolder, if set, is called as each folder finishes. A cold start takes
	// minutes and touches every Box; reporting only at the end makes it look
	// hung.
	OnFolder func(folder string, out Outcome, err error)
}

// Outcome reports what a cycle did, for logging and for tests to assert on.
type Outcome struct {
	Action        Action
	NewMessages   int
	FlagsChanged  int
	Expunged      int
	BodiesFetched int
	// Remapped counts messages a resync recognised from a previous incarnation
	// of the folder and therefore did not refetch the body of.
	Remapped int
}

// Resume redoes any folder whose intent was written but never cleared, which
// means the process died mid-sync. Planning is idempotent, so redoing is always
// safe (ADR-0015).
func (r *Reconciler) Resume(ctx context.Context) error {
	intents, err := r.Mirror.Intents(r.Account)
	if err != nil {
		return err
	}
	for _, in := range intents {
		if _, err := r.Sync(ctx, in.Folder); err != nil {
			return fmt.Errorf("resume %s: %w", in.Folder, err)
		}
	}
	return nil
}

// Sync runs one cycle for one folder.
func (r *Reconciler) Sync(ctx context.Context, folder string) (Outcome, error) {
	statuses, err := r.Driver.Status(ctx, []string{folder})
	if err != nil {
		return Outcome{}, fmt.Errorf("status: %w", err)
	}
	if len(statuses) == 0 {
		return Outcome{}, fmt.Errorf("folder %q not found on server", folder)
	}
	return r.apply(ctx, folder, statuses[0])
}

// SyncAll runs one cycle over many folders from a single detection pass.
//
// This is the whole point of LIST-STATUS: one round trip reports every folder,
// so the cost of a cycle is one command however many folders there are. Calling
// Sync in a loop would issue one LIST per folder and turn an O(folders) design
// into an O(folders) *round trips* one.
//
// A folder that fails does not stop the others. The Mirror is allowed to be
// Behind on one folder and current on the rest; the alternative is one bad
// folder freezing the whole Account.
func (r *Reconciler) SyncAll(ctx context.Context, folders []string) (map[string]Outcome, error) {
	statuses, err := r.Driver.Status(ctx, folders)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	// The order is the caller's, not the server's. A cold start is minutes and
	// the Inbox is what makes the Mirror useful first, so `folders` arrives
	// ordered (Inbox, the Boxes the Routing files beside it, then the rest with
	// Archive last) and a LIST-STATUS reply in whatever order the server likes
	// does not get to undo that.
	statuses = inOrder(folders, statuses)
	out := make(map[string]Outcome, len(statuses))
	var firstErr error
	for _, st := range statuses {
		o, err := r.apply(ctx, st.Name, st)
		if r.OnFolder != nil {
			r.OnFolder(st.Name, o, err)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", st.Name, err)
			}
			continue
		}
		out[st.Name] = o
	}
	return out, firstErr
}

// inOrder puts the statuses back in the order the folders were asked for. A
// status for a folder nobody asked about is kept, at the end: the server saying
// something unexpected is not a reason to skip it.
func inOrder(folders []string, statuses []FolderStatus) []FolderStatus {
	byName := make(map[string]FolderStatus, len(statuses))
	for _, st := range statuses {
		byName[st.Name] = st
	}
	out := make([]FolderStatus, 0, len(statuses))
	seen := make(map[string]bool, len(statuses))
	for _, name := range folders {
		if st, ok := byName[name]; ok && !seen[name] {
			seen[name] = true
			out = append(out, st)
		}
	}
	for _, st := range statuses {
		if !seen[st.Name] {
			seen[st.Name] = true
			out = append(out, st)
		}
	}
	return out
}

// apply plans and performs one folder's work against an already-fetched status.
func (r *Reconciler) apply(ctx context.Context, folder string, remote FolderStatus) (Outcome, error) {
	local, err := r.Mirror.Folder(r.Account, folder)
	if err != nil {
		return Outcome{}, err
	}

	plan := MakePlan(local, remote)
	if plan.Action == ActionNone {
		return Outcome{Action: ActionNone}, nil
	}

	// Written before anything else, so a crash anywhere below is recoverable.
	if err := r.Mirror.WriteIntent(r.Account, folder, plan.Action.String()); err != nil {
		return Outcome{}, err
	}

	switch plan.Action {
	case ActionResync:
		return r.resync(ctx, folder, remote)
	default:
		return r.incremental(ctx, folder, local, remote, plan)
	}
}

// resync rebuilds a folder whose uids no longer mean what they did. It drops
// Placements and keeps Messages: a UIDVALIDITY change does not imply the mail
// is gone, and a server-side migration can renumber a folder whose contents are
// intact. Bodies are refetched only for messages we do not already recognise.
func (r *Reconciler) resync(ctx context.Context, folder string, remote FolderStatus) (Outcome, error) {
	out := Outcome{Action: ActionResync}

	uids, err := r.Driver.AllUIDs(ctx, folder)
	if err != nil {
		return out, fmt.Errorf("all uids: %w", err)
	}
	envs, err := r.Driver.FetchEnvelopes(ctx, folder, uids)
	if err != nil {
		return out, fmt.Errorf("fetch envelopes: %w", err)
	}

	tx, err := r.Mirror.Begin(r.Account)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	if err := tx.DropPlacements(folder); err != nil {
		return out, err
	}

	wantBodies, ids, err := r.place(tx, folder, envs, &out)
	if err != nil {
		return out, err
	}
	out.NewMessages = len(wantBodies)

	if err := r.fetchBodies(ctx, tx, folder, wantBodies, ids, &out); err != nil {
		return out, err
	}
	if err := r.finish(tx, folder, remote); err != nil {
		return out, err
	}
	return out, nil
}

// incremental applies what changed since our modseq: flags, then new messages,
// then — only if the count disagrees — the uid diff that finds expunges.
func (r *Reconciler) incremental(ctx context.Context, folder string, local mirror.FolderState, remote FolderStatus, plan Plan) (Outcome, error) {
	out := Outcome{Action: ActionIncremental}

	var changed []FlagUpdate
	if plan.FlagsSince > 0 {
		var err error
		changed, err = r.Driver.ChangedFlags(ctx, folder, plan.FlagsSince)
		if err != nil {
			return out, fmt.Errorf("changed flags: %w", err)
		}
	}

	// Anything at or above the uidnext we last saw is new to us. Flag updates
	// for those uids are redundant: the envelope fetch carries current flags.
	var newUIDs []uint32
	if plan.NewFrom > 0 {
		for _, u := range changed {
			if u.UID >= plan.NewFrom {
				newUIDs = append(newUIDs, u.UID)
			}
		}
		sort.Slice(newUIDs, func(i, j int) bool { return newUIDs[i] < newUIDs[j] })
	}

	var envs []Envelope
	if len(newUIDs) > 0 {
		var err error
		envs, err = r.Driver.FetchEnvelopes(ctx, folder, newUIDs)
		if err != nil {
			return out, fmt.Errorf("fetch envelopes: %w", err)
		}
	}

	var remoteUIDs []uint32
	if plan.ExpungeDiff {
		var err error
		remoteUIDs, err = r.Driver.AllUIDs(ctx, folder)
		if err != nil {
			return out, fmt.Errorf("all uids: %w", err)
		}
	}

	tx, err := r.Mirror.Begin(r.Account)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	isNew := map[uint32]bool{}
	for _, u := range newUIDs {
		isNew[u] = true
	}
	for _, u := range changed {
		if isNew[u.UID] {
			continue
		}
		if err := tx.SetFlags(folder, u.UID, u.Flags); err != nil {
			return out, err
		}
		out.FlagsChanged++
	}

	wantBodies, ids, err := r.place(tx, folder, envs, &out)
	if err != nil {
		return out, err
	}
	out.NewMessages = len(envs)

	if err := r.fetchBodies(ctx, tx, folder, wantBodies, ids, &out); err != nil {
		return out, err
	}

	if plan.ExpungeDiff {
		localUIDs, err := r.Mirror.UIDs(r.Account, folder)
		if err != nil {
			return out, err
		}
		// The uids we just added are in the transaction, not yet in that read.
		localUIDs = append(localUIDs, newUIDs...)
		sort.Slice(localUIDs, func(i, j int) bool { return localUIDs[i] < localUIDs[j] })
		gone := diffUIDs(localUIDs, remoteUIDs)
		if err := tx.DeletePlacements(folder, gone); err != nil {
			return out, err
		}
		out.Expunged = len(gone)
	}

	if err := r.finish(tx, folder, remote); err != nil {
		return out, err
	}
	return out, nil
}

// place writes each envelope as a Message plus a Placement, and reports which
// uids still need their text fetched. A Message whose body the Mirror already
// holds is counted as remapped and not refetched — that is what makes a
// UIDVALIDITY change cost an envelope pass rather than a body pass.
func (r *Reconciler) place(tx *mirror.Tx, folder string, envs []Envelope, out *Outcome) ([]uint32, map[uint32]int64, error) {
	var wantBodies []uint32
	ids := map[uint32]int64{}
	for _, e := range envs {
		msg := messageOf(e, folder)
		id, hasBody, err := tx.UpsertMessage(msg)
		if err != nil {
			return nil, nil, err
		}
		// A real Message-ID that already sits in this folder under another uid
		// belongs to a different message, whatever the header says (ADR-0007).
		if e.MessageID != "" {
			taken, err := tx.HasOtherPlacement(id, folder, e.UID)
			if err != nil {
				return nil, nil, err
			}
			if taken {
				msg.Key = syntheticKey(folder, e.UID)
				if id, hasBody, err = tx.UpsertMessage(msg); err != nil {
					return nil, nil, err
				}
			}
		}
		ids[e.UID] = id
		if err := tx.PutPlacement(placementOf(e, folder, id)); err != nil {
			return nil, nil, err
		}
		if hasBody {
			out.Remapped++
		} else {
			wantBodies = append(wantBodies, e.UID)
		}
	}
	return wantBodies, ids, nil
}

func (r *Reconciler) fetchBodies(ctx context.Context, tx *mirror.Tx, folder string, uids []uint32, ids map[uint32]int64, out *Outcome) error {
	if len(uids) == 0 {
		return nil
	}
	bodies, err := r.Driver.FetchBodies(ctx, folder, uids)
	if err != nil {
		return fmt.Errorf("fetch bodies: %w", err)
	}
	for _, b := range bodies {
		id, ok := ids[b.UID]
		if !ok {
			continue
		}
		if err := tx.SetBody(id, b.Plain, b.HTML, searchText(b)); err != nil {
			return err
		}
		if err := tx.PutParts(id, partsOf(b)); err != nil {
			return err
		}
		out.BodiesFetched++
	}
	return nil
}

// partsOf turns what the driver saw in the MIME tree into rows the Mirror can
// answer an attachment listing from.
func partsOf(b Body) []mirror.Part {
	out := make([]mirror.Part, 0, len(b.Parts))
	for _, p := range b.Parts {
		out = append(out, mirror.Part{
			Path: p.Path, MIMEType: p.MIMEType, Filename: p.Filename,
			Disposition: p.Disposition, Size: p.Size,
		})
	}
	return out
}

// searchText is what Search matches a Message's body on: the plain part when
// there is one, and otherwise the HTML rendered down to text. Indexing the
// markup instead would match every message with a <table> in it on "table",
// and the reader already sees this rendering (docs/DESIGN.md, second slice).
func searchText(b Body) string {
	if strings.TrimSpace(b.Plain) != "" {
		return b.Plain
	}
	if strings.TrimSpace(b.HTML) == "" {
		return ""
	}
	return htmlmd.HTMLToMarkdown(b.HTML)
}

// finish commits the rows and the modseq that describes them together, and
// clears the journal in the same breath.
func (r *Reconciler) finish(tx *mirror.Tx, folder string, remote FolderStatus) error {
	if err := tx.SaveFolder(mirror.FolderState{
		Name:          folder,
		UIDValidity:   remote.UIDValidity,
		UIDNext:       remote.UIDNext,
		HighestModSeq: remote.HighestModSeq,
	}); err != nil {
		return err
	}
	if err := tx.ClearIntent(folder); err != nil {
		return err
	}
	return tx.Commit()
}

// messageOf builds the Message a Placement points at. A missing Message-ID gets
// a synthetic key from folder and uid, degrading that one message to
// folder-scoped identity rather than complicating every row (ADR-0007).
func messageOf(e Envelope, folder string) mirror.Message {
	key := e.MessageID
	if key == "" {
		key = syntheticKey(folder, e.UID)
	}
	return mirror.Message{
		Key:        key,
		Date:       e.Date,
		Subject:    e.Subject,
		From:       e.From,
		To:         e.To,
		Cc:         e.Cc,
		InReplyTo:  e.InReplyTo,
		References: e.References,
	}
}

// syntheticKey identifies a message that cannot be identified by its header,
// degrading that one message to folder-scoped identity.
func syntheticKey(folder string, uid uint32) string {
	return fmt.Sprintf("%s:%d", folder, uid)
}

func placementOf(e Envelope, folder string, messageID int64) mirror.Placement {
	return mirror.Placement{
		Folder:       folder,
		UID:          e.UID,
		MessageID:    messageID,
		Flags:        e.Flags,
		InternalDate: e.InternalDate,
		Size:         e.Size,
	}
}
