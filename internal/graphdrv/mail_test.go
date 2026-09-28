package graphdrv

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

func mailSetup(t *testing.T) (*fakeGraph, *Mail, *mailsync.Reconciler, *mirror.Mirror) {
	t.Helper()
	f, c := newFakeGraph(t)
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	mail := NewMail(c, s)
	return f, mail, &mailsync.Reconciler{Account: "work", Mirror: m, Driver: mail}, m
}

func syncAll(t *testing.T, r *mailsync.Reconciler, folders ...string) map[string]mailsync.Outcome {
	t.Helper()
	out, err := r.SyncAll(context.Background(), folders)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return out
}

func TestFoldersAreNamedTheWayTheRestOfTheProgramNamesThem(t *testing.T) {
	_, mail, _, _ := mailSetup(t)
	got, err := mail.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Localised display names are recognised by their well-known id; the
	// Outbox is not mail anybody reads; a child is named under its parent.
	want := []string{"INBOX", "INBOX/Projekte", "Sent", "Drafts", "Trash"}
	if !slices.Equal(got, want) {
		t.Fatalf("folders %v, want %v", got, want)
	}
}

func TestGraphMailReconcilesLikeIMAP(t *testing.T) {
	f, mail, r, m := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	for i, subject := range []string{"one", "two", "three"} {
		f.deliver("f-inbox", subject, "id"+string(rune('a'+i))+"@example.com")
	}

	// Cold start: three messages, over a delta that pages.
	out := syncAll(t, r, "INBOX")["INBOX"]
	if out.Action != mailsync.ActionResync || out.NewMessages != 3 || out.BodiesFetched != 3 {
		t.Fatalf("cold start %+v", out)
	}
	rows, _ := m.Rows("work", "INBOX", 10)
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.TextPlain, "body of ") || row.From != "Sender <sender@example.com>" || row.Size == 0 {
			t.Fatalf("row %+v", row)
		}
	}

	// Nothing changed: nothing happens.
	if out := syncAll(t, r, "INBOX")["INBOX"]; out.Action != mailsync.ActionNone {
		t.Fatalf("an unchanged folder did %+v", out)
	}

	// Read in Outlook, one deleted, one new: all three arrive in one cycle.
	f.mu.Lock()
	var ids []string
	for _, msg := range f.sortedMessages() {
		ids = append(ids, msg.id)
	}
	f.messages[ids[0]].read = true
	f.change("f-inbox", ids[0], false)
	delete(f.messages, ids[1])
	f.change("f-inbox", ids[1], true)
	f.mu.Unlock()
	f.deliver("f-inbox", "four", "idd@example.com")

	out = syncAll(t, r, "INBOX")["INBOX"]
	if out.FlagsChanged != 1 || out.NewMessages != 1 || out.Expunged != 1 || len(out.NewThreads) != 1 {
		t.Fatalf("incremental %+v", out)
	}
	rows, _ = m.Rows("work", "INBOX", 10)
	seen := 0
	for _, row := range rows {
		if row.Seen() {
			seen++
		}
	}
	if len(rows) != 3 || seen != 1 {
		t.Fatalf("%d rows, %d seen", len(rows), seen)
	}
}

func TestAForgottenDeltaResyncsWithoutRefetchingBodies(t *testing.T) {
	f, mail, r, _ := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	f.deliver("f-inbox", "one", "one@example.com")
	f.deliver("f-inbox", "two", "two@example.com")
	syncAll(t, r, "INBOX")

	f.mu.Lock()
	f.gone["f-inbox"] = true
	f.mu.Unlock()
	out := syncAll(t, r, "INBOX")["INBOX"]
	// A new UIDVALIDITY, and the Messages are recognised by Message-ID.
	if out.Action != mailsync.ActionResync || out.Remapped != 2 || out.BodiesFetched != 0 {
		t.Fatalf("after a 410 %+v", out)
	}
}

func TestFlagsAndKeywordsWriteThroughAndKeepOutlooksCategories(t *testing.T) {
	f, mail, r, _ := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	id := f.deliver("f-inbox", "one", "one@example.com")
	f.mu.Lock()
	f.messages[id].categories = []string{"Red category"}
	f.mu.Unlock()
	syncAll(t, r, "INBOX")

	got, err := mail.StoreFlags(ctx, "INBOX", []uint32{1}, []string{`\Seen`, `\Flagged`, "bubble-20260927T0800", "$bubbled"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"$bubbled", `\Flagged`, `\Seen`, "bubble-20260927T0800"}
	if len(got) != 1 || !slices.Equal(got[0].Flags, want) {
		t.Fatalf("store answered %+v", got)
	}
	f.mu.Lock()
	cats := f.messages[id].categories
	f.mu.Unlock()
	if !slices.Contains(cats, "Red category") || !slices.Contains(cats, "$bubbled") {
		t.Fatalf("categories on the server %v", cats)
	}
	if _, err := mail.StoreFlags(ctx, "INBOX", []uint32{1}, nil, []string{"$bubbled"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	cats = f.messages[id].categories
	f.mu.Unlock()
	if slices.Contains(cats, "$bubbled") || !slices.Contains(cats, "Red category") {
		t.Fatalf("categories after removing a keyword %v", cats)
	}

	// The write's own echo in the next delta is not a change.
	syncAll(t, r, "INBOX")
	if out := syncAll(t, r, "INBOX")["INBOX"]; out.Action != mailsync.ActionNone {
		t.Fatalf("settled folder did %+v", out)
	}
}

func TestAMoveKeepsTheMessageAndNamesItsNewUID(t *testing.T) {
	f, mail, r, m := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	f.deliver("f-inbox", "one", "one@example.com")
	syncAll(t, r, "INBOX", "INBOX/Projekte")

	w := &mailsync.Writer{Account: "work", Mirror: m, Driver: mail, Mirrored: []string{"INBOX", "INBOX/Projekte"}}
	if _, err := w.Move(ctx, []mailsync.Ref{{Folder: "INBOX", UID: 1}}, "INBOX/Projekte"); err != nil {
		t.Fatal(err)
	}
	dest, _ := m.UIDs("work", "INBOX/Projekte")
	if !slices.Equal(dest, []uint32{1}) {
		t.Fatalf("destination uids %v", dest)
	}
	syncAll(t, r, "INBOX", "INBOX/Projekte")
	src, _ := m.UIDs("work", "INBOX")
	dest, _ = m.UIDs("work", "INBOX/Projekte")
	if len(src) != 0 || !slices.Equal(dest, []uint32{1}) {
		t.Fatalf("after the next cycle: inbox %v, projekte %v", src, dest)
	}
}

func TestBodiesAndPartsComeFromTheMIME(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\nContent-Type: multipart/alternative; boundary=\"b2\"\r\n\r\n" +
		"--b2\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nw=E4r\r\n" +
		"--b2\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>wär</p>\r\n--b2--\r\n" +
		"--b1\r\nContent-Type: application/pdf; name=\"r.pdf\"\r\nContent-Disposition: attachment; filename=\"rechnung.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 fake")) + "\r\n--b1--\r\n"
	b := parseBody([]byte(raw))
	if strings.TrimSpace(b.Plain) != "wär" || !strings.Contains(b.HTML, "wär") {
		t.Fatalf("text %q / %q", b.Plain, b.HTML)
	}
	if len(b.Parts) != 1 || b.Parts[0].Path != "2" || b.Parts[0].Filename != "rechnung.pdf" ||
		b.Parts[0].Disposition != "attachment" || b.Parts[0].Size != int64(len("%PDF-1.4 fake")) {
		t.Fatalf("parts %+v", b.Parts)
	}

	f, mail, _, _ := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	id := f.deliver("f-inbox", "x", "x@example.com")
	f.mu.Lock()
	f.messages[id].mime = raw
	f.mu.Unlock()
	if _, err := mail.Status(ctx, []string{"INBOX"}); err != nil {
		t.Fatal(err)
	}
	got, err := mail.FetchPart(ctx, "INBOX", 1, "2")
	if err != nil || string(got) != "%PDF-1.4 fake" {
		t.Fatalf("part %q, %v", got, err)
	}
}

func TestSendingPutsHiddenRecipientsBackAsBcc(t *testing.T) {
	f, mail, _, _ := mailSetup(t)
	raw := []byte("From: me@example.com\r\nTo: a@example.com\r\nCc: b@example.com\r\nSubject: x\r\n\r\nhi\r\n")
	if err := mail.Send(context.Background(), "me@example.com",
		[]string{"a@example.com", "B@example.com", "hidden@example.com"}, raw); err != nil {
		t.Fatal(err)
	}
	calls := f.writes()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "POST /me/sendMail ") {
		t.Fatalf("calls %v", calls)
	}
	sent, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(calls[0], "POST /me/sendMail "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(sent), "Bcc: hidden@example.com\r\nFrom: me@example.com") {
		t.Fatalf("sent %q", sent)
	}
}

func TestADraftIsAppendedToDrafts(t *testing.T) {
	f, mail, _, _ := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := mail.Status(ctx, []string{"Drafts"}); err != nil {
		t.Fatal(err)
	}
	uid, err := mail.Append(ctx, "Drafts", []string{`\Seen`, `\Draft`}, []byte("Subject: d\r\n\r\nx\r\n"))
	if err != nil || uid != 1 {
		t.Fatalf("uid %d, %v", uid, err)
	}
	for _, c := range f.writes() {
		if strings.Contains(c, "/move") {
			t.Fatalf("a draft was moved: %v", f.writes())
		}
	}
}
