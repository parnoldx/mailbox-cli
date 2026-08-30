package mailsync

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"mailbox/internal/mirror"
)

// Writer is the write-through path. A command that changes something blocks on
// the server round trip and updates the Mirror from the ack, so the exit code
// means what it says and the next read sees the result (ADR-0004).
//
// It shares the Driver with the Reconciler, which serialises them on the work
// connection: a write and a fetch never interleave (ADR-0016).
type Writer struct {
	Account string
	Mirror  *mirror.Mirror
	Driver  Driver
	// Mirrored is every Box the Mirror holds. A Message moved out of all of
	// them — to Trash — leaves the Mirror rather than reappearing in a Box that
	// is never reconciled.
	Mirrored []string
}

// Ref names one Placement to act on.
type Ref struct {
	Folder string
	UID    uint32
}

// Result is what became of one Ref. A moved Message reports where it landed;
// NewUID is 0 when the server did not say, which leaves the destination to the
// next cycle.
type Result struct {
	Ref
	NewFolder string
	NewUID    uint32
	Flags     []string
}

// SetSeen adds or removes \Seen. Flags are idempotent, so asking for what is
// already true is not an error — it is the ordinary case for an agent that did
// not read the flag first.
func (w *Writer) SetSeen(ctx context.Context, refs []Ref, seen bool) ([]Result, error) {
	add, remove := []string{`\Seen`}, []string(nil)
	if !seen {
		add, remove = nil, []string{`\Seen`}
	}
	return w.setFlags(ctx, refs, add, remove)
}

func (w *Writer) setFlags(ctx context.Context, refs []Ref, add, remove []string) ([]Result, error) {
	var out []Result
	err := w.perFolder(refs, func(folder string, uids []uint32) error {
		updates, err := w.Driver.StoreFlags(ctx, folder, uids, add, remove)
		if err != nil {
			return err
		}
		tx, err := w.Mirror.Begin(w.Account)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, u := range updates {
			if err := tx.SetFlags(folder, u.UID, u.Flags); err != nil {
				return err
			}
			out = append(out, Result{Ref: Ref{Folder: folder, UID: u.UID}, Flags: u.Flags})
		}
		return tx.Commit()
	})
	return out, err
}

// Move moves Messages to another Box. The Placement moves with them: the
// Message itself, and the body already paid for, stay exactly where they are.
func (w *Writer) Move(ctx context.Context, refs []Ref, dest string) ([]Result, error) {
	var out []Result
	err := w.perFolder(refs, func(folder string, uids []uint32) error {
		if strings.EqualFold(folder, dest) {
			return fmt.Errorf("%s is already in %s", strings.Join(uidStrings(uids), ", "), dest)
		}
		moved, err := w.Driver.Move(ctx, folder, uids, dest)
		if err != nil {
			return err
		}
		tx, err := w.Mirror.Begin(w.Account)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, uid := range uids {
			p, err := tx.Placement(folder, uid)
			if err != nil {
				return err
			}
			if err := tx.DeletePlacements(folder, []uint32{uid}); err != nil {
				return err
			}
			r := Result{Ref: Ref{Folder: folder, UID: uid}, NewFolder: dest, NewUID: moved[uid]}
			// A destination the Mirror does not hold — Trash — takes the
			// Message out of the Mirror, which is what deleting it means here.
			if r.NewUID != 0 && w.mirrors(dest) {
				p.Folder, p.UID = dest, r.NewUID
				if err := tx.PutPlacement(p); err != nil {
					return err
				}
			}
			out = append(out, r)
		}
		return tx.Commit()
	})
	return out, err
}

// mirrors reports whether a Box is one the Mirror holds.
func (w *Writer) mirrors(folder string) bool {
	for _, m := range w.Mirrored {
		if strings.EqualFold(m, folder) {
			return true
		}
	}
	return false
}

// perFolder groups refs by Box and runs fn once per Box, so a command over ids
// from three Boxes is three round trips rather than one per message.
func (w *Writer) perFolder(refs []Ref, fn func(folder string, uids []uint32) error) error {
	if len(refs) == 0 {
		return nil
	}
	byFolder := map[string][]uint32{}
	var order []string
	for _, r := range refs {
		if _, seen := byFolder[r.Folder]; !seen {
			order = append(order, r.Folder)
		}
		byFolder[r.Folder] = append(byFolder[r.Folder], r.UID)
	}
	sort.Strings(order)
	for _, folder := range order {
		if err := fn(folder, byFolder[folder]); err != nil {
			return fmt.Errorf("%s: %w", folder, err)
		}
	}
	return nil
}

func uidStrings(uids []uint32) []string {
	out := make([]string, 0, len(uids))
	for _, u := range uids {
		out = append(out, fmt.Sprintf("%d", u))
	}
	return out
}

// SetLabel adds or removes an IMAP keyword. A label is a keyword and not a
// folder, which is what lets a Message carry several of them and keep them all
// when it is moved.
func (w *Writer) SetLabel(ctx context.Context, refs []Ref, label string, add bool) ([]Result, error) {
	if add {
		return w.setFlags(ctx, refs, []string{label}, nil)
	}
	return w.setFlags(ctx, refs, nil, []string{label})
}
