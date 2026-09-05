package outbox

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// stubTransport is an SMTP server that can be told to refuse, and that counts
// what it was asked to send. The count is the whole point: a mail must never be
// handed over twice.
type stubTransport struct {
	mu   sync.Mutex
	sent [][]byte
	err  error
}

func (s *stubTransport) Send(ctx context.Context, from string, to []string, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, raw)
	return nil
}

func (s *stubTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// stubFiler is the IMAP side: it files copies and can refuse to.
type stubFiler struct {
	filed [][]byte
	err   error
	uid   uint32
}

func (f *stubFiler) Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.filed = append(f.filed, raw)
	f.uid++
	return f.uid, nil
}

func courier(t *testing.T) (*Courier, *stubTransport, *stubFiler) {
	t.Helper()
	box, err := Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { box.Close() })
	tr, fi := &stubTransport{}, &stubFiler{}
	return &Courier{Box: box, Transport: tr, Filer: fi, SentBox: "Sent"}, tr, fi
}

func queue(t *testing.T, c *Courier, subject string) int64 {
	t.Helper()
	id, err := c.Box.Enqueue(Item{
		Account: "primary", MessageKey: subject + "@local", From: "me@example.org",
		Recipients: []string{"you@example.com"}, Subject: subject,
		Raw: []byte("Subject: " + subject + "\r\n\r\nhello\r\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDeliverSendsThenFiles(t *testing.T) {
	c, tr, fi := courier(t)
	id := queue(t, c, "Angebot")

	it, err := c.Deliver(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if it.State != Filed || it.Box != "Sent" || it.UID != 1 {
		t.Fatalf("item = %s %s:%d", it.State, it.Box, it.UID)
	}
	if tr.count() != 1 || len(fi.filed) != 1 {
		t.Fatalf("sent %d, filed %d", tr.count(), len(fi.filed))
	}
	// The copy in Sent is the mail that was sent, byte for byte.
	if string(fi.filed[0]) != string(tr.sent[0]) {
		t.Fatal("the filed copy is not what went out")
	}
}

func TestARefusedMailStaysQueuedAndGoesOutLater(t *testing.T) {
	c, tr, _ := courier(t)
	tr.err = errors.New("connection refused")
	id := queue(t, c, "Rechnung")

	it, err := c.Deliver(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if it.State != Queued {
		t.Fatalf("state = %s, want queued: a mail SMTP refused is not lost", it.State)
	}
	if it.Attempts != 1 || it.LastError == "" {
		t.Fatalf("attempts = %d, error = %q", it.Attempts, it.LastError)
	}

	tr.err = nil
	sent, err := c.Drain(context.Background())
	if err != nil || sent != 1 {
		t.Fatalf("drain sent %d (%v)", sent, err)
	}
	after, _ := c.Box.Get(id)
	if after.State != Filed || after.Attempts != 2 {
		t.Fatalf("after = %s, attempts %d", after.State, after.Attempts)
	}
}

func TestAnInterruptedSendIsHeldRatherThanResent(t *testing.T) {
	c, tr, _ := courier(t)
	id := queue(t, c, "halb gesendet")
	// The daemon died between claiming the mail and hearing back from SMTP.
	if err := c.Box.Claim(id); err != nil {
		t.Fatal(err)
	}

	held, err := c.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].ID != id {
		t.Fatalf("held = %v", held)
	}
	it, _ := c.Box.Get(id)
	if it.State != Held || it.LastError == "" {
		t.Fatalf("state = %s, error = %q", it.State, it.LastError)
	}

	// A drain must not touch it: nobody here can tell whether it went out, and
	// guessing wrong sends the same mail twice (ADR-0017).
	if _, err := c.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.count() != 0 {
		t.Fatalf("a held mail was sent %d times without being asked for", tr.count())
	}

	// Being told to is the only thing that moves it.
	if err := c.Box.Retry(id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Deliver(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if tr.count() != 1 {
		t.Fatalf("after retry, sent %d times", tr.count())
	}
}

func TestAFiledCopyIsRetriedWithoutSendingAgain(t *testing.T) {
	c, tr, fi := courier(t)
	fi.err = errors.New("IMAP is down")
	id := queue(t, c, "Anhang")

	it, err := c.Deliver(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// The mail has gone; only the copy is outstanding. That is not a failed
	// send, and it must not turn into a second one.
	if it.State != Sent {
		t.Fatalf("state = %s, want sent", it.State)
	}
	if it.LastError == "" {
		t.Fatal("the reason the copy is missing should be on the row")
	}

	fi.err = nil
	if _, err := c.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Box.Get(id)
	if after.State != Filed || after.UID != 1 {
		t.Fatalf("after = %s %s:%d", after.State, after.Box, after.UID)
	}
	if tr.count() != 1 {
		t.Fatalf("the mail went to smtp %d times", tr.count())
	}
}

func TestASentMailCannotBeCancelled(t *testing.T) {
	c, _, _ := courier(t)
	id := queue(t, c, "weg")
	if _, err := c.Deliver(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := c.Box.Cancel(id); err == nil {
		t.Fatal("a mail that has been sent cannot be recalled by deleting the record of it")
	}

	other := queue(t, c, "noch nicht")
	if err := c.Box.Cancel(other); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Box.Get(other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after cancel: %v", err)
	}
}

func TestAQueuedMailSurvivesTheProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	box, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := box.Enqueue(Item{
		Account: "primary", MessageKey: "k@local", From: "me@example.org",
		Recipients: []string{"you@example.com"}, Subject: "morgen", Raw: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	box.Close()

	// The Outbox is the one file that is never rebuilt from a server, because
	// there is no server that has this mail yet (ADR-0013).
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	it, err := again.Get(id)
	if err != nil || it.State != Queued || string(it.Raw) != "hello" {
		t.Fatalf("item = %+v (%v)", it, err)
	}
}

func TestAWrongSchemaIsRefusedRatherThanDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	box, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Enqueue(Item{Account: "primary", From: "me@example.org", Raw: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	box.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value = 99 WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("an outbox at an unknown schema version must not be opened, and must not be deleted")
	}
	// Still there, with the mail still in it.
	db, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM outbox`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d (%v)", n, err)
	}
}

func TestAScheduledMailWaitsUntilItsTime(t *testing.T) {
	c, tr, _ := courier(t)
	id, err := c.Box.Enqueue(Item{
		Account: "primary", MessageKey: "later@local", From: "me@example.org",
		Recipients: []string{"you@example.com"}, Subject: "Send later",
		Raw:       []byte("Subject: Send later\r\n\r\nhello\r\n"),
		NotBefore: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.count() != 0 {
		t.Fatalf("a mail scheduled an hour out went out early, sent %d times", tr.count())
	}
	if it, _ := c.Box.Get(id); it.State != Queued {
		t.Fatalf("state = %s, want queued", it.State)
	}

	// Once its instant has passed, the next drain finds it like any other
	// queued mail — this is what a periodic drain tick does on its own.
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, err := c.Box.db.Exec(`UPDATE outbox SET not_before = ? WHERE id = ?`, past, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.count() != 1 {
		t.Fatalf("a mail whose time has come was not sent, sent %d times", tr.count())
	}
}

func TestTheQueueRefusesAStateItIsNotIn(t *testing.T) {
	c, _, _ := courier(t)
	id := queue(t, c, "einmal")
	if _, err := c.Deliver(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	// Delivering a filed mail again would send it again.
	if _, err := c.Deliver(context.Background(), id); err == nil {
		t.Fatal("a filed mail must not be deliverable again")
	}
}
