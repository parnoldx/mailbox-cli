//go:build live

// Live conformance run against a real server. The scripted fake in
// mailsync covers the algorithm; this answers the separate question of whether
// we read the RFC the same way the server did.
//
//	go test -tags live ./internal/imapdrv/ -v
//
// It works in INBOX/mailbox-selftest, which it creates once and empties before
// and after each test, and touches nothing else. Deleting the folder is left to
// gate 5, whose subject is what a UIDVALIDITY change does.
package imapdrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

const liveFolder = "INBOX/mailbox-selftest"

type liveConfig struct {
	Account struct {
		Email    string
		Password string
	}
}

func loadLive(t *testing.T) liveConfig {
	t.Helper()
	path := os.Getenv("MAILBOX_CONFIG")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "mailbox", "config.toml")
	}
	var c liveConfig
	if _, err := toml.DecodeFile(path, &c); err != nil {
		t.Skipf("no live config at %s: %v", path, err)
	}
	if c.Account.Email == "" {
		t.Skip("no account in config")
	}
	return c
}

// otherClient is a second connection playing the part of another mail client:
// it is what delivers, flags and expunges messages behind the reconciler's back.
type otherClient struct {
	t   *testing.T
	c   *imapclient.Client
	cfg liveConfig
}

func dialOther(t *testing.T, cfg liveConfig) *otherClient {
	t.Helper()
	c, err := imapclient.DialTLS("imap.mailbox.org:993", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Login(cfg.Account.Email, cfg.Account.Password).Wait(); err != nil {
		t.Fatalf("login: %v", err)
	}
	o := &otherClient{t: t, c: c, cfg: cfg}
	t.Cleanup(func() { o.c.Close() })
	return o
}

// ensure makes the scratch folder exist without deleting it first.
//
// Deleting and recreating it between tests is what this fixture used to do, and
// it is a race with itself: the server drops connections that were looking at
// the folder, serves the next one a stale incarnation, and a test then counts
// the previous test's messages under uids that no longer exist. Emptying a
// folder that stays put has none of those failure modes. Only gate 5 deletes
// it, because UIDVALIDITY is what gate 5 is about.
func (o *otherClient) ensure() {
	o.t.Helper()
	o.do("ensure "+liveFolder, func() error {
		if err := o.c.Create(liveFolder, nil).Wait(); err != nil {
			// Already there is the ordinary case; anything else is not.
			if _, serr := o.c.Select(liveFolder, nil).Wait(); serr != nil {
				return err
			}
		}
		return nil
	})
}

func (o *otherClient) recreate() {
	o.t.Helper()
	for attempt := 0; attempt < 2; attempt++ {
		_ = o.c.Delete(liveFolder).Wait()
		err := o.c.Create(liveFolder, nil).Wait()
		if err == nil {
			return
		}
		o.t.Logf("create %s failed (%v), reconnecting", liveFolder, err)
		o.reconnect()
	}
	o.t.Fatalf("create %s: giving up", liveFolder)
}

// purge makes sure the scratch folder really is empty before a gate counts
// anything in it. DELETE is best-effort — a dropped connection swallows it, and
// this suite has seen that happen — and a folder that quietly kept its messages
// makes every count in every gate wrong, which reads as a failure of the code
// rather than of the fixture.
func (o *otherClient) purge() {
	o.t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		empty := false
		o.do("purge", func() error {
			sel, err := o.c.Select(liveFolder, nil).Wait()
			if err != nil {
				return err
			}
			if sel.NumMessages == 0 {
				empty = true
				return nil
			}
			o.t.Logf("%s still holds %d messages after recreate; purging", liveFolder, sel.NumMessages)
			all := imap.UIDSet{{Start: 1, Stop: 0}}
			if err := o.c.Store(all, &imap.StoreFlags{
				Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
			}, nil).Close(); err != nil {
				return err
			}
			return o.c.Expunge().Close()
		})
		if empty {
			return
		}
	}
	o.t.Fatalf("%s will not come up empty", liveFolder)
}

func (o *otherClient) reconnect() {
	o.t.Helper()
	_ = o.c.Close()
	c, err := imapclient.DialTLS("imap.mailbox.org:993", nil)
	if err != nil {
		o.t.Fatalf("redial: %v", err)
	}
	if err := c.Login(o.cfg.Account.Email, o.cfg.Account.Password).Wait(); err != nil {
		o.t.Fatalf("relogin: %v", err)
	}
	o.c = c
}

// do runs one command against the other client, reconnecting and retrying once.
// This fixture deletes and recreates its folder constantly and the server drops
// the connection that was looking at it — normal operation here, not a fault,
// and a fixture that falls over on it fails tests for reasons that have nothing
// to do with the code under test.
func (o *otherClient) do(what string, fn func() error) {
	o.t.Helper()
	err := fn()
	if err == nil {
		return
	}
	o.t.Logf("%s failed (%v), reconnecting", what, err)
	o.reconnect()
	if err := fn(); err != nil {
		o.t.Fatalf("%s: %v", what, err)
	}
}

func (o *otherClient) remove() {
	_ = o.c.Delete(liveFolder).Wait()
}

func (o *otherClient) append(subject string) {
	o.t.Helper()
	o.do("append "+subject, func() error { return o.appendOnce(subject) })
}

func (o *otherClient) appendOnce(subject string) error {
	// Quoted-printable with a non-ASCII character, because a text part arrives
	// in its transfer encoding and its own charset. Storing the bytes as they
	// came off the wire is a mistake that looks like success.
	body := fmt.Sprintf("Subject: %s\r\nFrom: selftest@example.org\r\n"+
		"Message-ID: <%s@selftest>\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"Content-Transfer-Encoding: quoted-printable\r\n"+
		"\r\nbody of %s w=C3=A4r\r\n", subject, subject, subject)
	return o.put(body)
}

// put writes one literal message into the scratch folder.
func (o *otherClient) put(body string) error {
	cmd := o.c.Append(liveFolder, int64(len(body)), nil)
	if _, err := cmd.Write([]byte(body)); err != nil {
		return err
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	_, err := cmd.Wait()
	return err
}

// appendWithAttachment delivers a multipart message with a base64 file in it.
// The payload is deliberately not valid ASCII text: an attachment that survives
// only because it was text all along proves nothing about decoding.
func (o *otherClient) appendWithAttachment(subject string, payload []byte) {
	o.t.Helper()
	o.do("append "+subject, func() error { return o.appendWithAttachmentOnce(subject, payload) })
}

func (o *otherClient) appendWithAttachmentOnce(subject string, payload []byte) error {
	encoded := base64.StdEncoding.EncodeToString(payload)
	body := fmt.Sprintf("Subject: %s\r\nFrom: selftest@example.org\r\n"+
		"Message-ID: <%s@selftest>\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=\"sep\"\r\n"+
		"\r\n--sep\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nsiehe Anhang\r\n"+
		"--sep\r\nContent-Type: application/pdf; name=\"rechnung.pdf\"\r\n"+
		"Content-Disposition: attachment; filename=\"rechnung.pdf\"\r\n"+
		"Content-Transfer-Encoding: base64\r\n\r\n%s\r\n--sep--\r\n",
		subject, subject, encoded)
	return o.put(body)
}

func (o *otherClient) uids() []imap.UID {
	o.t.Helper()
	var out []imap.UID
	o.do("uids", func() error {
		if _, err := o.c.Select(liveFolder, nil).Wait(); err != nil {
			return err
		}
		data, err := o.c.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{ReturnAll: true}).Wait()
		if err != nil {
			return err
		}
		out = data.AllUIDs()
		return nil
	})
	return out
}

func (o *otherClient) setSeen(uid imap.UID) {
	o.t.Helper()
	o.do("set seen", func() error {
		if _, err := o.c.Select(liveFolder, nil).Wait(); err != nil {
			return err
		}
		return o.c.Store(imap.UIDSetNum(uid), &imap.StoreFlags{
			Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen},
		}, nil).Close()
	})
}

func (o *otherClient) expunge(uid imap.UID) {
	o.t.Helper()
	o.do("expunge", func() error {
		if _, err := o.c.Select(liveFolder, nil).Wait(); err != nil {
			return err
		}
		if err := o.c.Store(imap.UIDSetNum(uid), &imap.StoreFlags{
			Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted},
		}, nil).Close(); err != nil {
			return err
		}
		return o.c.Expunge().Close()
	})
}

func liveSetup(t *testing.T) (*mailsync.Reconciler, *mirror.Mirror, *otherClient, *Driver) {
	t.Helper()
	cfg := loadLive(t)
	other := dialOther(t, cfg)
	other.ensure()
	other.purge()
	// Left empty rather than deleted: see ensure.
	t.Cleanup(other.purge)

	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	drv, err := Dial(Config{
		Host: "imap.mailbox.org", Port: 993,
		Username: cfg.Account.Email, Password: cfg.Account.Password,
	})
	if err != nil {
		t.Fatalf("dial driver: %v", err)
	}
	t.Cleanup(func() { drv.Close() })

	return &mailsync.Reconciler{Account: "primary", Mirror: m, Driver: drv}, m, other, drv
}

func liveSync(t *testing.T, r *mailsync.Reconciler) mailsync.Outcome {
	t.Helper()
	local, _ := r.Mirror.Folder("primary", liveFolder)
	st, err := r.Driver.Status(context.Background(), []string{liveFolder})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st) == 0 {
		t.Fatalf("folder %q missing from LIST-STATUS", liveFolder)
	}
	t.Logf("  local{uv=%d next=%d modseq=%d count=%d} remote{uv=%d next=%d modseq=%d msgs=%d} plan=%+v",
		local.UIDValidity, local.UIDNext, local.HighestModSeq, local.Count,
		st[0].UIDValidity, st[0].UIDNext, st[0].HighestModSeq, st[0].NumMessages,
		mailsync.MakePlan(local, st[0]))
	out, err := r.Sync(context.Background(), liveFolder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return out
}

// converges runs cycles until one has nothing to do, giving up after n.
func converges(t *testing.T, r *mailsync.Reconciler, n int) bool {
	t.Helper()
	for i := 0; i < n; i++ {
		out, err := r.Sync(context.Background(), liveFolder)
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		if out.Action == mailsync.ActionNone {
			return true
		}
	}
	return false
}

func liveRows(t *testing.T, m *mirror.Mirror) []mirror.Row {
	t.Helper()
	rows, err := m.Rows("primary", liveFolder, 100)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	return rows
}

// Gates 1, 3, 4 and 5 against the real server, in one run so they share the
// scratch folder's lifecycle.
func TestLiveGates(t *testing.T) {
	r, m, other, drv := liveSetup(t)

	// Gate 1: cold start.
	other.append("one")
	other.append("two")
	out := liveSync(t, r)
	if out.Action != mailsync.ActionResync {
		t.Fatalf("cold start action = %v, want resync", out.Action)
	}
	if got := liveRows(t, m); len(got) != 2 {
		t.Fatalf("gate 1: %d rows, want 2", len(got))
	}
	for _, row := range liveRows(t, m) {
		if row.BodyState != "mirrored" || row.TextPlain == "" {
			t.Errorf("gate 1: uid %d has no text (state %q)", row.UID, row.BodyState)
			continue
		}
		if !strings.Contains(row.TextPlain, "wär") {
			t.Errorf("gate 1: uid %d text is not decoded: %q", row.UID, row.TextPlain)
		}
	}
	// Selecting a folder clears \Recent on the messages in it, which is a real
	// flag change and bumps HIGHESTMODSEQ — so the cycle after a cold start
	// legitimately has something to do. What matters is that it reaches a fixed
	// point rather than churning forever.
	if !converges(t, r, 3) {
		t.Error("gate 1: the Mirror never stopped finding work")
	}

	// Gate 3: another client sets \Seen; it must converge without a body refetch.
	uids := other.uids()
	other.setSeen(uids[0])
	out = liveSync(t, r)
	if out.FlagsChanged != 1 {
		t.Errorf("gate 3: flags changed = %d, want 1", out.FlagsChanged)
	}
	if out.BodiesFetched != 0 {
		t.Errorf("gate 3: refetched %d bodies for a flag change", out.BodiesFetched)
	}
	var seen int
	for _, row := range liveRows(t, m) {
		if row.Seen() {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("gate 3: %d seen rows, want 1", seen)
	}

	// Gate 4: expunged while we were not looking. No modseq bump reveals this;
	// only the count does.
	other.expunge(uids[0])
	out = liveSync(t, r)
	if out.Expunged != 1 {
		t.Errorf("gate 4: expunged = %d, want 1", out.Expunged)
	}
	if got := liveRows(t, m); len(got) != 1 {
		t.Fatalf("gate 4: %d rows, want 1", len(got))
	}

	// Gate 5: UIDVALIDITY change. DELETE+CREATE is the destructive variant —
	// the folder really is empty afterwards — so the assertion here is that the
	// resync is clean, not that messages are remapped. The migration variant,
	// where the messages survive under new uids, is only reachable in the fake.
	before, err := m.Folder("primary", liveFolder)
	if err != nil {
		t.Fatal(err)
	}
	other.recreate()
	other.append("after-reset")

	out = liveSync(t, r)
	if out.Action != mailsync.ActionResync {
		t.Fatalf("gate 5: action = %v, want resync", out.Action)
	}
	after, err := m.Folder("primary", liveFolder)
	if err != nil {
		t.Fatal(err)
	}
	if after.UIDValidity == before.UIDValidity {
		t.Fatalf("gate 5: uidvalidity did not move (%d) — the server did not reset it",
			before.UIDValidity)
	}
	t.Logf("gate 5: uidvalidity %d -> %d", before.UIDValidity, after.UIDValidity)
	rows := liveRows(t, m)
	for _, r := range rows {
		t.Logf("row uid=%d key=%q subject=%q", r.UID, r.Message.Key, r.Subject)
	}
	if len(rows) != 1 {
		t.Fatalf("gate 5: %d rows, want 1", len(rows))
	}
	if rows[0].Subject != "after-reset" {
		t.Errorf("gate 5: stale row survived: %q", rows[0].Subject)
	}
	_ = drv
}

// Gate 2: a message delivered while IDLE is held is noticed within a second.
func TestLiveGate2Idle(t *testing.T) {
	r, m, other, drv := liveSetup(t)
	other.append("before")
	liveSync(t, r)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	events := make(chan mailsync.Event, 4)
	go drv.Watch(ctx, liveFolder, events)

	// Give IDLE a moment to be established before delivering.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
	}

	start := time.Now()
	other.append("during-idle")

	select {
	case <-events:
		t.Logf("gate 2: idle reported a change after %s", time.Since(start).Round(time.Millisecond))
	case <-ctx.Done():
		t.Fatal("gate 2: idle never reported the new message")
	}

	if out := liveSync(t, r); out.NewMessages != 1 {
		t.Errorf("gate 2: new messages = %d, want 1", out.NewMessages)
	}
	if got := liveRows(t, m); len(got) != 2 {
		t.Errorf("gate 2: %d rows, want 2", len(got))
	}
}

// liveDest is the destination for the move; created and destroyed with the
// selftest folder and nothing else touched.
const liveDest = "INBOX/mailbox-selftest-dest"

// TestLiveWrites answers the two questions the fake cannot: does a STORE
// readback carry the uid, and does this server report COPYUID on MOVE. Both are
// what ADR-0004 leans on to update the Mirror from the ack.
func TestLiveWrites(t *testing.T) {
	r, m, other, drv := liveSetup(t)
	if err := other.c.Create(liveDest, nil).Wait(); err != nil {
		t.Fatalf("create %s: %v", liveDest, err)
	}
	t.Cleanup(func() { _ = other.c.Delete(liveDest).Wait() })

	other.append("write-one")
	other.append("write-two")
	liveSync(t, r)
	rows := liveRows(t, m)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	first, second := rows[0].UID, rows[1].UID

	// A STORE is silent and the flags are read back, because a server need not
	// put the uid in the untagged FETCH it sends.
	updates, err := drv.StoreFlags(context.Background(), liveFolder, []uint32{first}, []string{`\Seen`}, nil)
	if err != nil {
		t.Fatalf("store flags: %v", err)
	}
	if len(updates) != 1 || updates[0].UID != first {
		t.Fatalf("store readback = %+v, want uid %d", updates, first)
	}
	if !hasFlag(updates[0].Flags, `\Seen`) {
		t.Errorf("flags after store = %v", updates[0].Flags)
	}

	// The whole write-through path: server first, Mirror from the ack.
	w := &mailsync.Writer{
		Account: "primary", Mirror: m, Driver: drv,
		Mirrored: []string{liveFolder, liveDest},
	}
	results, err := w.Move(context.Background(), []mailsync.Ref{{Folder: liveFolder, UID: second}}, liveDest)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(results) != 1 || results[0].NewUID == 0 {
		t.Fatalf("move results = %+v, want a COPYUID", results)
	}
	if _, err := m.Row("primary", liveFolder, second); err == nil {
		t.Error("source placement survived the move")
	}
	moved, err := m.Row("primary", liveDest, results[0].NewUID)
	if err != nil {
		t.Fatalf("destination placement: %v", err)
	}
	if moved.TextPlain == "" || moved.BodyState != "mirrored" {
		t.Errorf("body lost in the move: %q (%s)", moved.TextPlain, moved.BodyState)
	}

	// And the server agrees: one message left where it was, one in the destination.
	if got := other.uids(); len(got) != 1 || uint32(got[0]) != first {
		t.Errorf("source folder holds %v, want just %d", got, first)
	}
	if _, err := other.c.Select(liveDest, nil).Wait(); err != nil {
		t.Fatalf("select %s: %v", liveDest, err)
	}
	data, err := other.c.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{ReturnAll: true}).Wait()
	if err != nil {
		t.Fatalf("search %s: %v", liveDest, err)
	}
	if got := data.AllUIDs(); len(got) != 1 || uint32(got[0]) != results[0].NewUID {
		t.Errorf("destination holds %v, want %d", got, results[0].NewUID)
	}
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// TestLiveAttachment asks the real server the questions the fake cannot: does
// BODYSTRUCTURE give us the filename and the path we recorded, and does the
// part come back decoded. A base64 attachment written to disk still encoded is
// not the file the sender sent, and it looks like success.
func TestLiveAttachment(t *testing.T) {
	r, m, other, drv := liveSetup(t)
	payload := []byte{'%', 'P', 'D', 'F', '-', '1', '.', '4', 0x00, 0xff, 0xfe, '\n'}
	other.appendWithAttachment("with-file", payload)
	liveSync(t, r)

	rows := liveRows(t, m)
	for _, r := range rows {
		t.Logf("row uid=%d key=%q subject=%q", r.UID, r.Message.Key, r.Subject)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].TextPlain == "" {
		t.Errorf("the text part was not mirrored: %+v", rows[0].Message)
	}
	parts, err := m.Parts(rows[0].Message.ID)
	if err != nil {
		t.Fatalf("parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d parts, want 1: %+v", len(parts), parts)
	}
	if parts[0].Filename != "rechnung.pdf" || parts[0].MIMEType != "application/pdf" {
		t.Errorf("part = %+v", parts[0])
	}
	if parts[0].Disposition != "attachment" {
		t.Errorf("disposition = %q, want attachment", parts[0].Disposition)
	}

	got, err := drv.FetchPart(context.Background(), liveFolder, rows[0].UID, parts[0].Path)
	if err != nil {
		t.Fatalf("fetch part: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("fetched %q, want %q — the transfer encoding was not undone", got, payload)
	}
}
