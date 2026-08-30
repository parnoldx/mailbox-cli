package mailsync

import (
	"context"
	"errors"
	"testing"

	"mailbox/internal/mirror"
)

const archive = "Archive"

// writeSetup gives a Writer over a Mirror that has already synced one message
// in INBOX, which is the state every write starts from.
func writeSetup(t *testing.T) (*Writer, *Fake, *mirror.Mirror) {
	t.Helper()
	r, f, m := setup(t)
	f.AddFolder(archive)
	f.Deliver(box, "a@x", "first", "hello")
	runSync(t, r)
	w := &Writer{Account: "primary", Mirror: m, Driver: f, Mirrored: []string{box, archive}}
	return w, f, m
}

func placement(t *testing.T, m *mirror.Mirror, folder string, uid uint32) (mirror.Row, error) {
	t.Helper()
	return m.Row("primary", folder, uid)
}

// The Mirror records what the server acked, not what was asked for.
func TestSetSeenWritesTheAckIntoTheMirror(t *testing.T) {
	w, f, m := writeSetup(t)

	if _, err := w.SetSeen(context.Background(), []Ref{{box, 1}}, true); err != nil {
		t.Fatalf("set seen: %v", err)
	}
	if flags := f.Folder(box).Msgs[0].Flags; len(flags) != 1 || flags[0] != `\Seen` {
		t.Errorf("server flags = %v", flags)
	}
	row, err := placement(t, m, box, 1)
	if err != nil || !row.Seen() {
		t.Fatalf("mirror row = %+v, err %v; want seen", row.Placement, err)
	}

	// Unseen is the same round trip in reverse, and marking something that is
	// already unseen is not an error.
	if _, err := w.SetSeen(context.Background(), []Ref{{box, 1}}, false); err != nil {
		t.Fatalf("set unseen: %v", err)
	}
	row, _ = placement(t, m, box, 1)
	if row.Seen() {
		t.Errorf("still seen after unseen: %v", row.Placement.Flags)
	}
}

// A move takes the Placement to the new Box under the uid the server gave it.
// The Message — and the body already paid for — stays where it is.
func TestMoveRepointsThePlacementAndKeepsTheBody(t *testing.T) {
	w, _, m := writeSetup(t)
	before, _ := placement(t, m, box, 1)

	results, err := w.Move(context.Background(), []Ref{{box, 1}}, archive)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(results) != 1 || results[0].NewFolder != archive || results[0].NewUID == 0 {
		t.Fatalf("results = %+v", results)
	}
	if _, err := placement(t, m, box, 1); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("source placement still there: %v", err)
	}
	after, err := placement(t, m, archive, results[0].NewUID)
	if err != nil {
		t.Fatalf("destination placement: %v", err)
	}
	if after.Message.ID != before.Message.ID {
		t.Errorf("message id changed: %d -> %d", before.Message.ID, after.Message.ID)
	}
	if after.TextPlain != "hello" || after.BodyState != "mirrored" {
		t.Errorf("body lost in the move: %q (%s)", after.TextPlain, after.BodyState)
	}
}

// Trash is not mirrored, so moving there takes the Message out of the Mirror
// rather than parking it in a Box that is never reconciled.
func TestMoveToAnUnmirroredBoxLeavesTheMirror(t *testing.T) {
	w, f, m := writeSetup(t)
	f.AddFolder("Trash")

	if _, err := w.Move(context.Background(), []Ref{{box, 1}}, "Trash"); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := placement(t, m, box, 1); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("source placement still there: %v", err)
	}
	if _, err := placement(t, m, "Trash", 1); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("trash placement written into the mirror: %v", err)
	}
}

// Without UIDPLUS the server does not say where the message landed. The source
// side is still known, and the destination is the next cycle's to find.
func TestMoveWithoutUIDPlusLeavesTheDestinationToTheCycle(t *testing.T) {
	w, f, m := writeSetup(t)
	f.NoUIDPlus = true

	results, err := w.Move(context.Background(), []Ref{{box, 1}}, archive)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if results[0].NewUID != 0 {
		t.Errorf("new uid = %d, want 0", results[0].NewUID)
	}
	if _, err := placement(t, m, box, 1); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("source placement still there: %v", err)
	}
	if rows, err := m.Rows("primary", archive, 10); err != nil || len(rows) != 0 {
		t.Errorf("guessed a destination placement: %v, %v", rows, err)
	}
}

// A write the server refused changes nothing here. The exit code has to mean
// what it says, which it cannot if the Mirror moved anyway (ADR-0004).
func TestAFailedWriteLeavesTheMirrorAlone(t *testing.T) {
	w, f, m := writeSetup(t)
	f.Fail["Move"] = errors.New("server said no")

	if _, err := w.Move(context.Background(), []Ref{{box, 1}}, archive); err == nil {
		t.Fatal("move: want an error")
	}
	if _, err := placement(t, m, box, 1); err != nil {
		t.Errorf("source placement gone after a failed move: %v", err)
	}
}

// Ids from several Boxes are one round trip per Box, not one per message.
func TestWritesAreGroupedByBox(t *testing.T) {
	w, f, _ := writeSetup(t)
	f.Deliver(box, "b@x", "second", "again")
	f.Deliver(archive, "c@x", "third", "elsewhere")
	f.Calls = nil

	if _, err := w.SetSeen(context.Background(), []Ref{{box, 1}, {archive, 1}, {box, 2}}, true); err != nil {
		t.Fatalf("set seen: %v", err)
	}
	if n := f.CallCount("StoreFlags"); n != 2 {
		t.Errorf("StoreFlags called %d times, want 2", n)
	}
}
