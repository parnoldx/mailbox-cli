package mirror

import (
	"path/filepath"
	"testing"
	"time"
)

// threadFixture inserts Messages one at a time, each in its own transaction, so
// the tests can control the order they arrive in — which is the whole question
// threading has to answer.
type threadFixture struct {
	t *testing.T
	m *Mirror
	n int
}

func newThreadFixture(t *testing.T) *threadFixture {
	t.Helper()
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return &threadFixture{t: t, m: m}
}

// add mirrors one Message with the given references, in its own transaction.
func (f *threadFixture) add(key, subject string, refs ...string) int64 {
	f.t.Helper()
	tx, err := f.m.Begin("primary")
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback()
	f.n++
	id, _, err := tx.UpsertMessage(Message{
		Key: key, Subject: subject, References: refs,
		Date: time.Date(2026, 8, 20, 9, f.n, 0, 0, time.UTC),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := tx.PutPlacement(Placement{Folder: "INBOX", UID: uint32(f.n), MessageID: id}); err != nil {
		f.t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *threadFixture) threadOf(id int64) []Row {
	f.t.Helper()
	var threadID int64
	if err := f.m.db.QueryRow(`SELECT thread_id FROM messages WHERE id = ?`, id).Scan(&threadID); err != nil {
		f.t.Fatal(err)
	}
	rows, err := f.m.Thread("primary", threadID)
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func TestThreadLinksAReplyToItsParent(t *testing.T) {
	f := newThreadFixture(t)
	parent := f.add("a@x", "Angebot")
	reply := f.add("b@x", "Re: Angebot", "a@x")

	got := f.threadOf(reply)
	if len(got) != 2 {
		t.Fatalf("thread has %d messages, want 2", len(got))
	}
	if got[0].Message.ID != parent || got[1].Message.ID != reply {
		t.Errorf("thread order = %d, %d; want %d, %d oldest first",
			got[0].Message.ID, got[1].Message.ID, parent, reply)
	}
}

// A reply is routinely mirrored before the mail it answers — the reply is in
// the Inbox and the parent is filed in Sent, and the Boxes sync in whatever
// order LIST returns them.
func TestThreadLinksAParentThatArrivesLater(t *testing.T) {
	f := newThreadFixture(t)
	reply := f.add("b@x", "Re: Angebot", "a@x")
	parent := f.add("a@x", "Angebot")

	if got := f.threadOf(parent); len(got) != 2 {
		t.Fatalf("thread from the parent has %d messages, want 2", len(got))
	}
	if got := f.threadOf(reply); len(got) != 2 {
		t.Fatalf("thread from the reply has %d messages, want 2", len(got))
	}
}

// Two conversations turn out to be one when a Message references both. They
// were one all along; the Mirror only just learnt it.
func TestThreadMergesWhenALinkArrives(t *testing.T) {
	f := newThreadFixture(t)
	first := f.add("a@x", "Angebot")
	second := f.add("b@x", "Nachfrage")
	if len(f.threadOf(first)) != 1 || len(f.threadOf(second)) != 1 {
		t.Fatal("the two messages should start out unrelated")
	}

	f.add("c@x", "Re: beides", "a@x", "b@x")
	if got := f.threadOf(first); len(got) != 3 {
		t.Errorf("thread from the first has %d messages, want 3", len(got))
	}
	if got := f.threadOf(second); len(got) != 3 {
		t.Errorf("thread from the second has %d messages, want 3", len(got))
	}
}

// A shared subject is not a Thread (ADR-0008).
func TestThreadIgnoresSubjects(t *testing.T) {
	f := newThreadFixture(t)
	a := f.add("a@x", "Rechnung")
	b := f.add("b@x", "Rechnung")
	if len(f.threadOf(a)) != 1 || len(f.threadOf(b)) != 1 {
		t.Error("two mails with the same subject were threaded together")
	}
}

// A header that references the message itself must not make it its own parent.
func TestThreadIgnoresSelfReferences(t *testing.T) {
	f := newThreadFixture(t)
	id := f.add("a@x", "Angebot", "a@x")
	if got := f.threadOf(id); len(got) != 1 {
		t.Errorf("thread has %d messages, want 1", len(got))
	}
}

// A Message in two Boxes appears once in its Thread.
func TestThreadReportsAMessageOnce(t *testing.T) {
	f := newThreadFixture(t)
	id := f.add("a@x", "Angebot")
	tx, err := f.m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(Placement{Folder: "Archive", UID: 99, MessageID: id}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := f.threadOf(id)
	if len(got) != 1 {
		t.Fatalf("thread has %d rows, want 1", len(got))
	}
	if got[0].Placement.Folder != "INBOX" {
		t.Errorf("reported under %s, want INBOX", got[0].Placement.Folder)
	}
}
