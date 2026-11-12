package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// And no new id is handed back for it: the Inbox has no row until that
	// cycle runs, so an id here would name nothing.
	if got[0].NewID != "" {
		t.Errorf("new_id = %q, want none until the cycle has the Message", got[0].NewID)
	}
}

// Several Trashed ids are one round trip per Box, not one per id: the server is
// asked which of them are really there with a single header fetch, and the text
// of a Message nobody is going to read is not asked for at all.
func TestMoveOfSeveralTrashedMessagesAsksTheServerOnce(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	for i := 1; i <= 3; i++ {
		f.Deliver("Trash", "gone@example.com", fmt.Sprintf("Weg %d", i), "text")
	}

	got := write(t, d, []string{"move"}, map[string]any{
		"positional": []any{"Trash:1", "Trash:2", "Trash:3"}, "to": "INBOX",
	})
	if len(got) != 3 {
		t.Fatalf("move returned %+v", got)
	}
	for _, op := range []string{"Folders", "FetchEnvelopes"} {
		if n := callsBefore(t, f, "Move", op); n != 1 {
			t.Errorf("%s ran %d times for three ids, want 1", op, n)
		}
	}
	if n := callsBefore(t, f, "Move", "FetchBodies"); n != 0 {
		t.Errorf("FetchBodies ran %d times to move mail out of Trash", n)
	}
	var back []string
	for _, m := range f.Folder("INBOX").Msgs {
		if strings.HasPrefix(m.Subject, "Weg ") {
			back = append(back, m.Subject)
		}
	}
	if len(back) != 3 {
		t.Fatalf("INBOX holds %v, want all three recovered", back)
	}
}

// A uid the server does not answer with is not there: the error says so before
// anything is moved.
func TestMoveOfAVanishedTrashedMessageIsNotFound(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"move"},
		Args: map[string]any{"positional": []any{"Trash:99"}, "to": "INBOX"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v, want not_found", resp)
	}
	if n := f.CallCount("Move"); n != 0 {
		t.Errorf("Move ran %d times for a uid the server does not hold", n)
	}
}

// `trash` on mail already in Trash has nowhere to go, and says so before it
// writes anything: the seed \Seen it used to set was a real STORE on the server.
// `spam` is a move out of Trash like `move`, so it still works.
func TestTrashOfATrashedMessageWritesNothing(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	f.Deliver("Trash", "sorry@example.com", "Aus Versehen", "text")

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"trash"},
		Args: map[string]any{"positional": []any{"Trash:1"}}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("trash: %+v, want a usage error saying it is already there", resp)
	}
	if !strings.Contains(resp.Error, "already in Trash") {
		t.Errorf("error = %q, want it to say the mail is already in Trash", resp.Error)
	}
	for _, op := range []string{"StoreFlags", "Move"} {
		if n := f.CallCount(op); n != 0 {
			t.Errorf("%s ran %d times for mail already in Trash", op, n)
		}
	}

	write(t, d, []string{"spam"}, map[string]any{"positional": []any{"Trash:1"}})
	if n := len(f.Folder("Junk").Msgs); n != 1 {
		t.Errorf("Junk holds %d messages, want the one marked spam", n)
	}
}

// The writes that refuse a Trashed id refuse it without asking the server:
// nothing is there to ask about, and a read-only write is not worth a round
// trip (ADR-0001).
func TestWritesThatRefuseTrashAskTheServerNothing(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	f.Deliver("Trash", "sorry@example.com", "Aus Versehen", "text")

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"seen"},
		Args: map[string]any{"positional": []any{"Trash:1"}}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("seen: %+v, want not_found", resp)
	}
	if n := f.CallCount("Folders"); n != 0 {
		t.Errorf("Folders ran %d times for an id the Mirror does not hold", n)
	}
}

// A Box one account holds is listed from that account and from no server: the
// accounts that do not have it are never asked, because asking them would be a
// round trip on a listing that is local (ADR-0001). Only a Box no account holds
// (Trash) is the server's.
func TestBoxViewOfABoxOneAccountHoldsAsksNobody(t *testing.T) {
	d, second, _ := twoAccounts(t)
	// Screener is the Primary's: the gmx account has no such Box, and its own
	// server is off.
	second.Fail["Folders"] = errors.New("offline")

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"box", "view"},
		Args: map[string]any{"positional": "Screener"}})
	if !resp.OK {
		t.Fatalf("box view: %s (%s)", resp.Error, resp.Code)
	}
	rows := resp.Data.([]row)
	if len(rows) != 1 || rows[0].ID != "Screener:42" {
		t.Fatalf("rows = %+v, want the Primary's Screener", rows)
	}
	if n := second.CallCount("Folders"); n != 0 {
		t.Errorf("the account without the Box was asked %d times", n)
	}
	if n := fakeOf(d).CallCount("Folders"); n != 0 {
		t.Errorf("the Primary's server was asked %d times", n)
	}
}

// callsBefore counts one operation among the calls the fake ran up to its first
// stop, so a test can say what a command asked for before it wrote anything.
func callsBefore(t *testing.T, f *mailsync.Fake, stop, op string) int {
	t.Helper()
	n := 0
	for _, c := range f.Calls {
		if c == stop {
			return n
		}
		if c == op {
			n++
		}
	}
	t.Fatalf("the fake never ran %s", stop)
	return 0
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
