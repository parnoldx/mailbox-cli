package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
	"mailbox/internal/sync/mailsync"
)

// stubTransport stands in for the submission server. It counts what it was
// given, because "how many times did this mail go out" is the question the
// Outbox exists to answer.
type stubTransport struct {
	mu   sync.Mutex
	sent [][]byte
	to   [][]string
	err  error
}

func (s *stubTransport) Send(ctx context.Context, from string, to []string, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, raw)
	s.to = append(s.to, to)
	return nil
}

func (s *stubTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *stubTransport) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// seedSend is a seeded Daemon that can also send: a durable Outbox, a scripted
// SMTP server, and the scripted IMAP server it already had to file the copy in.
func seedSend(t *testing.T) (*Daemon, *stubTransport) {
	t.Helper()
	d := seed(t)
	box, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { box.Close() })
	tr := &stubTransport{}
	d.Outbox = box
	d.From = compose.Address{Name: "Max Mustermann", Addr: "me@example.com"}
	d.Courier = &outbox.Courier{
		Box: box, Transport: tr, Filer: fakeOf(d), SentBox: "INBOX/Sent",
	}
	return d, tr
}

// send runs one send command and insists it worked.
func send(t *testing.T, d *Daemon, args map[string]any) sent {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"}, Args: args})
	if !resp.OK {
		t.Fatalf("send: %s (%s)", resp.Error, resp.Code)
	}
	out, ok := resp.Data.(sent)
	if !ok {
		t.Fatalf("send returned %T", resp.Data)
	}
	return out
}

// filedCopy is the message the fake server holds in Sent under a uid.
func filedCopy(t *testing.T, d *Daemon, uid uint32) *mailsync.FakeMsg {
	t.Helper()
	for _, m := range fakeOf(d).Folder("INBOX/Sent").Msgs {
		if m.UID == uid {
			return m
		}
	}
	t.Fatalf("no message %d in INBOX/Sent", uid)
	return nil
}

func TestSendFilesTheCopyAndTheMirrorHoldsIt(t *testing.T) {
	d, tr := seedSend(t)
	out := send(t, d, map[string]any{
		"to": []string{"Käthe Groß <kaethe@example.com>"}, "subject": "Rechnung für März",
		"body": "Hallo Käthe,\n\nanbei die Rechnung.",
	})

	if out.State != string(outbox.Filed) {
		t.Fatalf("state = %s", out.State)
	}
	if tr.count() != 1 {
		t.Fatalf("smtp saw the mail %d times", tr.count())
	}
	if len(tr.to[0]) != 1 || tr.to[0][0] != "kaethe@example.com" {
		t.Fatalf("envelope recipients = %v", tr.to[0])
	}
	if out.ID == "" || out.Box != "INBOX/Sent" {
		t.Fatalf("copy = %q in %q", out.ID, out.Box)
	}

	// The id it reported is one the read commands take, now, without waiting
	// for a poll: the copy was mirrored by the ordinary cycle before the reply
	// came back.
	got := view(t, d, out.ID)
	if got["subject"] != "Rechnung für März" {
		t.Fatalf("subject in the mirror = %v", got["subject"])
	}
	if !strings.Contains(got["body"].(string), "anbei die Rechnung") {
		t.Fatalf("body in the mirror = %q", got["body"])
	}
	// The mail that was sent and the copy that was filed are the same bytes.
	copyOf := filedCopy(t, d, out.UID)
	if copyOf.MessageID != out.MessageID {
		t.Fatalf("filed copy is %q, sent mail was %q", copyOf.MessageID, out.MessageID)
	}
}

func TestSendCarriesItsAttachment(t *testing.T) {
	d, _ := seedSend(t)
	payload := []byte{0x00, 0xff, '%', 'P', 'D', 'F', 0x80}
	path := filepath.Join(t.TempDir(), "rechnung.pdf")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	out := send(t, d, map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "Anhang",
		"body": "siehe Anhang", "attach": []string{path},
	})

	copyOf := filedCopy(t, d, out.UID)
	if len(copyOf.Parts) != 1 {
		t.Fatalf("the filed copy carries %d parts", len(copyOf.Parts))
	}
	if copyOf.Parts[0].Filename != "rechnung.pdf" || copyOf.Parts[0].MIMEType != "application/pdf" {
		t.Fatalf("part = %+v", copyOf.Parts[0].PartInfo)
	}
	if string(copyOf.Parts[0].Bytes) != string(payload) {
		t.Fatalf("attachment bytes changed on the way out")
	}
	// And the Mirror lists it, from the cycle that mirrored the copy.
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"attachment", "list"},
		Args: map[string]any{"positional": out.ID}})
	if !resp.OK {
		t.Fatalf("attachment list: %s", resp.Error)
	}
	if rows, _ := resp.Data.([]attachment); len(rows) != 1 || rows[0].Filename != "rechnung.pdf" {
		t.Fatalf("attachment list = %+v", resp.Data)
	}
}

func TestAnAttachmentPathMustBeAbsolute(t *testing.T) {
	d, _ := seedSend(t)
	// The Daemon reads the file, and its working directory is not the caller's.
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"}, Args: map[string]any{
		"to": []string{"k@example.com"}, "subject": "x", "body": "y", "attach": []string{"rechnung.pdf"},
	}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestReplyAnswersTheSenderInTheSameThread(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reply"}, Args: map[string]any{
		"positional": "7", "body": "schon überwiesen",
	}})
	if !resp.OK {
		t.Fatalf("reply: %s (%s)", resp.Error, resp.Code)
	}
	out := resp.Data.(sent)

	copyOf := filedCopy(t, d, out.UID)
	if copyOf.Subject != "Re: Rechnung" {
		t.Fatalf("subject = %q", copyOf.Subject)
	}
	if !strings.Contains(copyOf.To, "billing@example.com") {
		t.Fatalf("to = %q, the reply should go to whoever sent it", copyOf.To)
	}
	if len(copyOf.InReplyTo) != 1 || copyOf.InReplyTo[0] != "plain@example.com" {
		t.Fatalf("in-reply-to = %v", copyOf.InReplyTo)
	}
	if len(copyOf.References) == 0 || copyOf.References[len(copyOf.References)-1] != "plain@example.com" {
		t.Fatalf("references = %v", copyOf.References)
	}

	// And the Thread the parent is in now holds the answer, because the copy
	// was mirrored like any other mail and threaded from those headers.
	thread := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"thread", "view"},
		Args: map[string]any{"positional": "7"}})
	if !thread.OK {
		t.Fatalf("thread: %s", thread.Error)
	}
	rows := thread.Data.([]message)
	var subjects []string
	for _, r := range rows {
		subjects = append(subjects, r.Subject)
	}
	if len(rows) < 2 || subjects[len(subjects)-1] != "Re: Rechnung" {
		t.Fatalf("thread = %v", subjects)
	}
}

// A reply keeps the caller's text at the top and quotes the parent under an
// attribution line, so it still reads on a client with no conversation view.
func TestReplyQuotesTheParentUnderTheAnswer(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reply"}, Args: map[string]any{
		"positional": "7", "body": "schon überwiesen",
	}})
	if !resp.OK {
		t.Fatalf("reply: %s (%s)", resp.Error, resp.Code)
	}
	body := filedCopy(t, d, resp.Data.(sent).UID).Plain
	if !strings.HasPrefix(strings.TrimSpace(body), "schon überwiesen") {
		t.Errorf("the answer is not at the top:\n%s", body)
	}
	for _, want := range []string{"billing@example.com", "wrote:", "> the text"} {
		if !strings.Contains(body, want) {
			t.Errorf("the quoted parent is missing %q:\n%s", want, body)
		}
	}
}

// A caller that already assembled the quote block (a GUI that showed it for
// trimming) marks it, and answer() leaves that alone rather than quote twice.
func TestReplyDoesNotDoubleQuoteAMarkedBody(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reply"}, Args: map[string]any{
		"positional": "7", "body": "kurz",
		"body_html": "<p>kurz</p><div data-mailbox-quote>already quoted</div>",
	}})
	if !resp.OK {
		t.Fatalf("reply: %s (%s)", resp.Error, resp.Code)
	}
	if body := filedCopy(t, d, resp.Data.(sent).UID).Plain; strings.Contains(body, "> the text") {
		t.Errorf("answer() quoted a body that already carried the block:\n%s", body)
	}
}

func TestReplyAllCopiesEveryoneExceptUs(t *testing.T) {
	d, _ := seedSend(t)
	// A mail addressed to us and a colleague, copying the boss.
	tx, err := d.Mirror.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := tx.UpsertMessage(mirror.Message{
		Key: "runde@example.com", Subject: "Termin", From: "Chef <chef@example.com>",
		To: "me@example.com, kollege@example.com", Cc: "assistenz@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX", UID: 11, MessageID: id}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reply"}, Args: map[string]any{
		"positional": "11", "all": true, "body": "passt mir",
	}})
	if !resp.OK {
		t.Fatalf("reply: %s (%s)", resp.Error, resp.Code)
	}
	out := resp.Data.(sent)

	// Everyone on the mail, minus ourselves: replying to all should not mail
	// us a copy of our own answer every time.
	want := map[string]bool{"chef@example.com": true, "kollege@example.com": true, "assistenz@example.com": true}
	if len(out.Recipients) != len(want) {
		t.Fatalf("recipients = %v", out.Recipients)
	}
	for _, r := range out.Recipients {
		if !want[r] {
			t.Fatalf("recipients = %v", out.Recipients)
		}
	}
}

func TestASendSMTPRefusedIsQueuedNotLost(t *testing.T) {
	d, tr := seedSend(t)
	tr.fail(errors.New("dial smtp.example.org: connection refused"))

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"}, Args: map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "Rechnung", "body": "anbei",
	}})
	if resp.OK {
		t.Fatal("a mail that did not go out must not report success")
	}
	if !strings.Contains(resp.Error, "outbox") || !strings.Contains(resp.Error, "connection refused") {
		t.Fatalf("error = %q", resp.Error)
	}

	list := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "list"}})
	rows := list.Data.([]outboxRow)
	if len(rows) != 1 || rows[0].State != string(outbox.Queued) || rows[0].Attempts != 1 {
		t.Fatalf("outbox = %+v", rows)
	}

	// The next drain — every cycle runs one — takes it out.
	tr.fail(nil)
	d.drain(context.Background(), d.primaryAccount())
	list = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "list"}})
	rows = list.Data.([]outboxRow)
	if len(rows) != 1 || rows[0].State != string(outbox.Filed) {
		t.Fatalf("outbox after drain = %+v", rows)
	}
	if tr.count() != 1 {
		t.Fatalf("smtp saw the mail %d times", tr.count())
	}
}

func TestAHeldMailWaitsToBeTold(t *testing.T) {
	d, tr := seedSend(t)
	id, err := d.Outbox.Enqueue(outbox.Item{
		Account: "primary", MessageKey: "halb@example.com", From: "me@example.com",
		Recipients: []string{"kaethe@example.com"}, Subject: "unterwegs",
		Raw: []byte("Subject: unterwegs\r\nMessage-ID: <halb@example.com>\r\n\r\nhallo\r\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The daemon died with the mail at the SMTP server.
	if err := d.Outbox.Claim(id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Courier.Recover(); err != nil {
		t.Fatal(err)
	}

	d.drain(context.Background(), d.primaryAccount())
	if tr.count() != 0 {
		t.Fatal("a mail that may already have been delivered must not be sent again on its own")
	}

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "retry"},
		Args: map[string]any{"positional": "1"}})
	if !resp.OK {
		t.Fatalf("retry: %s (%s)", resp.Error, resp.Code)
	}
	if tr.count() != 1 {
		t.Fatalf("after being told, smtp saw it %d times", tr.count())
	}
}

func TestOutboxCancelDropsAQueuedMail(t *testing.T) {
	d, tr := seedSend(t)
	tr.fail(errors.New("no network"))
	d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"}, Args: map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "doch nicht", "body": "x",
	}})

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "cancel"},
		Args: map[string]any{"positional": "1"}})
	if !resp.OK {
		t.Fatalf("cancel: %s (%s)", resp.Error, resp.Code)
	}
	list := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "list"}})
	if rows := list.Data.([]outboxRow); len(rows) != 0 {
		t.Fatalf("outbox = %+v", rows)
	}
}

// A forward is a reply's mirror image: it keeps the text and changes the
// thread, where a reply keeps the thread and changes the text. Carrying
// References would put it in a conversation its recipients were never in.
func TestForwardStartsANewConversationAndQuotesTheOriginal(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"forward"}, Args: map[string]any{
		"positional": "7", "to": []any{"anna@example.com"}, "body": "Kennst du die?",
	}})
	if !resp.OK {
		t.Fatalf("forward: %s (%s)", resp.Error, resp.Code)
	}
	out := resp.Data.(sent)

	copyOf := filedCopy(t, d, out.UID)
	if copyOf.Subject != "Fwd: Rechnung" {
		t.Errorf("subject = %q", copyOf.Subject)
	}
	if !strings.Contains(copyOf.To, "anna@example.com") {
		t.Errorf("to = %q", copyOf.To)
	}
	if len(copyOf.InReplyTo) != 0 || len(copyOf.References) != 0 {
		t.Errorf("a forward joined the thread: in-reply-to %v, references %v",
			copyOf.InReplyTo, copyOf.References)
	}
	// The note is above the original, and the original is quoted whole under a
	// header block naming who actually sent it.
	body := copyOf.Plain
	if !strings.HasPrefix(strings.TrimSpace(body), "Kennst du die?") {
		t.Errorf("the note is not at the top:\n%s", body)
	}
	for _, want := range []string{"Forwarded message", "billing@example.com", "the text"} {
		if !strings.Contains(body, want) {
			t.Errorf("the forwarded body is missing %q:\n%s", want, body)
		}
	}
}

// Fwd: is prefixed once. "Fwd: Fwd: Fwd:" is somebody's client doing this
// wrong three times.
func TestForwardSubjectIsPrefixedOnce(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Rechnung", "Fwd: Rechnung"},
		{"Fwd: Rechnung", "Fwd: Rechnung"},
		{"FW: Rechnung", "FW: Rechnung"},
		{"", "Fwd:"},
	} {
		if got := forwardSubject(c.in); got != c.want {
			t.Errorf("forwardSubject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Forwarding to nobody would queue a mail with no recipients, which the outbox
// would then keep failing to send.
func TestForwardNeedsARecipient(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"forward"},
		Args: map[string]any{"positional": "7"}})
	if resp.OK || !strings.Contains(resp.Error, "--to") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSendRendersMarkdownToAnHTMLAlternative(t *testing.T) {
	d, tr := seedSend(t)
	send(t, d, map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "Formatiert",
		"body": "Hallo,\n\ndas ist **wichtig**.",
	})
	if tr.count() != 1 {
		t.Fatalf("sent %d mails", tr.count())
	}
	raw := string(tr.sent[0])
	if !strings.Contains(raw, "multipart/alternative") {
		t.Fatalf("no alternative part:\n%s", raw)
	}
	if !strings.Contains(raw, "<strong>wichtig</strong>") {
		t.Fatalf("markdown was not rendered to HTML:\n%s", raw)
	}
	// The text/plain twin is the body verbatim, Markdown and all.
	if !strings.Contains(raw, "das ist **wichtig**.") {
		t.Fatalf("the text/plain twin is not the body verbatim:\n%s", raw)
	}
}

func TestSendCarriesAnHTMLBodyVerbatim(t *testing.T) {
	d, tr := seedSend(t)
	send(t, d, map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "HTML",
		"body_html": "<p>Hallo <em>Welt</em></p>",
	})
	raw := string(tr.sent[0])
	if !strings.Contains(raw, "multipart/alternative") {
		t.Fatalf("no alternative part:\n%s", raw)
	}
	if !strings.Contains(raw, "<p>Hallo <em>Welt</em></p>") {
		t.Fatalf("the html body was not carried verbatim:\n%s", raw)
	}
}

func TestEvenAPlainSendCarriesBothParts(t *testing.T) {
	// The body is always Markdown, so every send is a multipart/alternative
	// whose text/plain part is the body verbatim and whose text/html part is
	// the rendering. Prose with no Markdown in it round-trips unchanged.
	d, tr := seedSend(t)
	send(t, d, map[string]any{
		"to": []string{"kaethe@example.com"}, "subject": "kurz", "body": "Passt.",
	})
	raw := string(tr.sent[0])
	if !strings.Contains(raw, "multipart/alternative") {
		t.Fatalf("a send should carry both parts:\n%s", raw)
	}
	if !strings.Contains(raw, "\nPasst.") || !strings.Contains(raw, "<p>Passt.</p>") {
		t.Fatalf("both parts should hold the body:\n%s", raw)
	}
}
