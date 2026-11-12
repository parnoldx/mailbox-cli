package daemon

import (
	"context"
	"testing"
	"time"

	"mailbox/internal/sync/mailsync"
)

// Trashed mail is not in the Mirror (ADR-0003), so seeing it is the one read
// that asks the server. It lists what is there, newest first, and says the rows
// are read-only: nothing about them can be moved or flagged.
func TestBoxViewTrashReadsTheServer(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	older := f.Deliver("Trash", "old@example.com", "Alte Rechnung", "text")
	older.Date = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	newer := f.Deliver("Trash", "new@example.com", "Neue Rechnung", "text")
	newer.Date = time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	f.SetFlags("Trash", older.UID, `\Seen`)

	rows := boxView(t, d, "Trash")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Subject != "Neue Rechnung" || rows[1].Subject != "Alte Rechnung" {
		t.Errorf("rows are not newest first: %q, %q", rows[0].Subject, rows[1].Subject)
	}
	if !rows[0].ReadOnly || rows[0].ID != "Trash:2" {
		t.Errorf("row = %+v, want a read-only Trash:2", rows[0])
	}
	if !rows[1].Seen || rows[1].ReadOnly == false {
		t.Errorf("row = %+v, want the \\Seen flag on a read-only row", rows[1])
	}

	// Reading Trash leaves the Mirror as empty as it was: this is a look, not
	// a sync.
	if held, err := d.Mirror.Rows("primary", "Trash", 10); err != nil || len(held) != 0 {
		t.Fatalf("the Mirror gained %d Trash rows (%v)", len(held), err)
	}
}

// One Message out of Trash reads whole: the text is fetched with it, there is
// no Thread to build from it, and it is marked read-only.
func TestTrashMessageAndItsThreadAreReadFromTheServer(t *testing.T) {
	d := seed(t)
	m := fakeOf(d).Deliver("Trash", "gone@example.com", "Weg damit", "der Text\n")

	read := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"},
		Args: map[string]any{"positional": "Trash:1"}})
	if !read.OK {
		t.Fatalf("message view: %s (%s)", read.Error, read.Code)
	}
	trashed := read.Data.(message)
	if trashed.Subject != "Weg damit" || trashed.Body != "der Text\n" {
		t.Errorf("message = %+v", trashed)
	}
	if !trashed.ReadOnly {
		t.Errorf("read_only = false, want true")
	}

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"thread", "view"},
		Args: map[string]any{"positional": "Trash:1"}})
	if !resp.OK {
		t.Fatalf("thread view: %s (%s)", resp.Error, resp.Code)
	}
	thread := resp.Data.([]message)
	if len(thread) != 1 || thread[0].ID != "Trash:1" || !thread[0].ReadOnly {
		t.Fatalf("thread = %+v, want the one read-only Message", thread)
	}
	if thread[0].UID != m.UID {
		t.Errorf("uid = %d, want %d", thread[0].UID, m.UID)
	}
}

// Move is the one write that takes Trash mail back out: the mail is gone from
// the server's Trash and back in the Inbox, and the Mirror learns it on the next
// cycle rather than being handed a row it never had.
func TestMoveOutOfTrashRecoversIt(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	f.Deliver("Trash", "sorry@example.com", "Aus Versehen", "text")

	got := write(t, d, []string{"move"}, map[string]any{
		"positional": []any{"Trash:1"}, "to": "INBOX",
	})
	if len(got) != 1 || !got[0].Moved || got[0].Box != "INBOX" {
		t.Fatalf("move returned %+v", got)
	}
	if n := len(f.Folder("Trash").Msgs); n != 0 {
		t.Errorf("Trash still holds %d messages", n)
	}
	back := f.Folder("INBOX").Msgs
	if len(back) != 2 || back[1].Subject != "Aus Versehen" {
		t.Fatalf("INBOX = %+v, want the recovered message", back)
	}
	// Nothing was invented locally: the recovered mail arrives with the cycle
	// the write asked for, not as a Mirror row built from the ack.
	if held, err := d.Mirror.Rows("primary", "INBOX", 50); err != nil || len(held) != 1 {
		t.Fatalf("the Mirror holds %d Inbox rows (%v), want only the seeded one", len(held), err)
	}
}

// Every other write refuses an id in Trash: there is no Placement to flag or
// label, and saying so is better than a folder-shaped server error. `spam` is
// the one exception besides move, because it is a move out of Trash too.
func TestWritesOtherThanMoveRefuseTrash(t *testing.T) {
	d := seed(t)
	fakeOf(d).Deliver("Trash", "sorry@example.com", "Aus Versehen", "text")
	for _, cmd := range []string{"seen", "unseen", "trash"} {
		resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{cmd},
			Args: map[string]any{"positional": []any{"Trash:1"}}})
		if resp.OK {
			t.Errorf("%s on a Trashed message was allowed", cmd)
		}
	}
}

// A Box the Mirror holds is answered locally: reading mail never touches the
// network (ADR-0001). The one exception above is opt-in by naming Trash.
func TestBoxViewOfAMirroredBoxAsksNothing(t *testing.T) {
	d := seed(t)
	boxView(t, d, "INBOX")
	if n := fakeOf(d).CallCount("Folders"); n != 0 {
		t.Errorf("Folders called %d times for a mirrored Box", n)
	}
}

// Trash spans accounts the way every other Box does: a second account's trash
// is in the same listing, and its rows name the account.
func TestTrashSpansAccounts(t *testing.T) {
	d := seed(t)
	mine := fakeOf(d).Deliver("Trash", "mine@example.com", "Meins", "text")
	mine.Date = time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	work := mailsync.NewFake("INBOX")
	work.AddFolder("Trash")
	theirs := work.Deliver("Trash", "theirs@example.com", "Arbeit", "text")
	theirs.Date = time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	d.StartAccount(NewAccount("work",
		&mailsync.Reconciler{Account: "work", Mirror: d.Mirror, Driver: work},
		&mailsync.Writer{Account: "work", Mirror: d.Mirror, Driver: work, Mirrored: []string{"INBOX"}},
		[]string{"INBOX"}, nil))

	rows := boxView(t, d, "Trash")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want both accounts' trash", len(rows))
	}
	if rows[0].ID != "work/Trash:1" || rows[1].ID != "Trash:1" {
		t.Errorf("rows = %s, %s; want newest first with the account named", rows[0].ID, rows[1].ID)
	}
}
