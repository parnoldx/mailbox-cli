package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mailbox/internal/mirror"
)

// threadedBox builds a Mirror holding one Thread of three Messages, all
// placed in INBOX (the way a real back-and-forth would arrive), plus one
// unrelated Message — so `box view` has both a conversation to collapse and a
// row that should stay on its own.
func threadedBox(t *testing.T) *Daemon {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	add := func(key, subject string, seen bool, uid uint32, date time.Time, refs ...string) {
		tx, err := m.Begin("primary")
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := tx.UpsertMessage(mirror.Message{Key: key, Subject: subject, References: refs, Date: date})
		if err != nil {
			t.Fatal(err)
		}
		var flags []string
		if seen {
			flags = []string{`\Seen`}
		}
		if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX", UID: uid, MessageID: id, Flags: flags}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	add("d@x", "Rechnung", true, 4, time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC))
	add("a@x", "Angebot", true, 1, time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC))
	add("b@x", "Re: Angebot", true, 2, time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC), "a@x")
	add("c@x", "Re: Angebot", false, 3, time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC), "a@x", "b@x")

	return New("primary", m, nil, []string{"INBOX"}, nil, nil)
}

func boxView(t *testing.T, d *Daemon, box string) []row {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"box", "view"},
		Args: map[string]any{"positional": box}})
	if !resp.OK {
		t.Fatalf("box view %q: %s (%s)", box, resp.Error, resp.Code)
	}
	rows, ok := resp.Data.([]row)
	if !ok {
		t.Fatalf("box view %q returned %T", box, resp.Data)
	}
	return rows
}

func TestBoxViewCollapsesAThreadToOneRow(t *testing.T) {
	d := threadedBox(t)
	rows := boxView(t, d, "inbox")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one thread collapsed, one on its own)", len(rows))
	}
}

func TestBoxViewShowsTheNewestOfAThread(t *testing.T) {
	d := threadedBox(t)
	rows := boxView(t, d, "inbox")
	thread := rows[0]
	if thread.UID != 3 {
		t.Errorf("shown row is uid %d, want 3 (the newest)", thread.UID)
	}
	if thread.Count != 3 {
		t.Errorf("count = %d, want 3", thread.Count)
	}
}

func TestBoxViewMarksAThreadUnreadIfAnyMessageIs(t *testing.T) {
	d := threadedBox(t)
	rows := boxView(t, d, "inbox")
	if rows[0].Seen {
		t.Error("thread has an unread reply but the row reports seen")
	}
}

// The Count badge is the whole conversation's size — the number the reader
// shows — even when only part of the Thread sits in the Box being listed.
func TestBoxViewCountsTheWholeThreadNotJustThisBox(t *testing.T) {
	d := seed(t)
	// The Rechnung thread: billing@ in INBOX:7, plus a reply of ours filed in
	// INBOX/Sent. Only one of the two Messages is in the Inbox.
	var found bool
	for _, r := range boxView(t, d, "inbox") {
		if r.Subject != "Rechnung" {
			continue
		}
		found = true
		if r.Count != 2 {
			t.Errorf("count = %d, want 2 (the reply filed in Sent counts too)", r.Count)
		}
	}
	if !found {
		t.Fatal("no Rechnung row in the inbox listing")
	}
}

// A Message on its own carries no Count: only a conversation gets a badge.
func TestBoxViewOmitsCountForALoneMessage(t *testing.T) {
	d := threadedBox(t)
	rows := boxView(t, d, "inbox")
	lone := rows[1]
	if lone.Subject != "Rechnung" {
		t.Fatalf("rows[1] = %+v, want the Rechnung row", lone)
	}
	if lone.Count != 0 {
		t.Errorf("count = %d, want 0 for a Message with no Thread", lone.Count)
	}
}

// Trash acts on the whole Thread a listing collapsed to one row, not just the
// one Message whose id happened to be given (ADR-0008; see (*Daemon).threaded).
func TestTrashActsOnTheWholeThread(t *testing.T) {
	d := seed(t)
	// "7" (Rechnung, INBOX) is threaded with the reply the seed put at
	// INBOX/Sent:4 (References: plain@example.com).
	got := write(t, d, []string{"trash"}, map[string]any{"positional": []any{"7"}})
	if len(got) != 2 {
		t.Fatalf("trash returned %d changes, want 2 (both Messages of the Thread): %+v", len(got), got)
	}
	for _, m := range []struct {
		folder string
		uid    uint32
	}{{"INBOX", 7}, {"INBOX/Sent", 4}} {
		if _, err := d.Mirror.Row("primary", m.folder, m.uid); !errors.Is(err, mirror.ErrNotFound) {
			t.Errorf("%s:%d still in the Mirror: %v", m.folder, m.uid, err)
		}
	}
}
