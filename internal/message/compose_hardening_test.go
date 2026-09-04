package message

import (
	"bytes"
	"strings"
	"testing"

	gomail "github.com/emersion/go-message/mail"
)

func TestHardening_HeaderInjectionCannotSmuggleBcc(t *testing.T) {
	d := Draft{
		From:    Address{Addr: "me@example.com"},
		To:      []Address{{Addr: "you@example.com"}},
		Subject: "Hello\r\nBcc: evil@example.net",
		Body:    "hi",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("\nbcc:")) || bytes.Contains(bytes.ToLower(raw), []byte("\rbcc:")) {
		t.Fatalf("injected Bcc header:\n%s", raw)
	}
	h, _, _ := read(t, raw)
	if addrs, err := h.AddressList("Bcc"); err == nil && len(addrs) > 0 {
		t.Fatalf("bcc address list = %v", addrs)
	}
	if subj, _ := h.Subject(); strings.Contains(subj, "\n") {
		t.Fatalf("subject still has a newline: %q", subj)
	}
}

func TestHardening_FilenameNewlinesAreStripped(t *testing.T) {
	d := Draft{
		From: Address{Addr: "me@example.com"}, To: []Address{{Addr: "you@example.com"}},
		Subject: "file", Body: "x",
		Attachments: []Attachment{{Filename: "../../etc/passwd", MIMEType: "text/plain", Content: []byte("n")}},
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("../")) {
		t.Fatalf("path traversal survived:\n%s", raw)
	}
	_, _, files := read(t, raw)
	if len(files) != 1 || files[0].Filename != "passwd" {
		t.Fatalf("filename = %+v", files)
	}

	d.Attachments[0].Filename = "foo.txt\nX-Evil: 1"
	raw, err = d.Build()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("\nx-evil:")) {
		t.Fatalf("injected header from filename:\n%s", raw)
	}
}

func TestHardening_PlainComesBeforeHTML(t *testing.T) {
	d := Draft{
		From: Address{Addr: "me@example.com"}, To: []Address{{Addr: "you@example.com"}},
		Subject: "hi", Body: "plain", BodyHTML: "<p>html</p>",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	plainAt := bytes.Index(bytes.ToLower(raw), []byte("text/plain"))
	htmlAt := bytes.Index(bytes.ToLower(raw), []byte("text/html"))
	if plainAt < 0 || htmlAt < 0 || plainAt > htmlAt {
		t.Fatalf("plain should come before html:\n%s", raw)
	}
}

func TestHardening_MessageIDUsesSenderDomain(t *testing.T) {
	d := Draft{
		From: Address{Addr: "me@mailbox.org"}, To: []Address{{Addr: "you@example.com"}},
		Subject: "hi", Body: "x",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	h, _, _ := read(t, raw)
	id, err := h.MessageID()
	if err != nil || !strings.HasSuffix(id, "@mailbox.org") {
		t.Fatalf("message-id = %q (%v)", id, err)
	}
	if strings.Contains(id, "@localhost") || strings.Contains(id, "@mailbox") && !strings.Contains(id, "@mailbox.org") {
		t.Fatalf("message-id used a placeholder domain: %q", id)
	}
}

func TestHardening_RecipientsAreCompleteAndDeduped(t *testing.T) {
	d := Draft{
		From:    Address{Addr: "me@example.com"},
		To:      []Address{{Addr: "a@example.com"}, {Addr: "A@example.com"}},
		Cc:      []Address{{Addr: "b@example.com"}},
		Bcc:     []Address{{Addr: "c@example.com"}, {Addr: "b@example.com"}},
		Subject: "hi", Body: "x",
	}
	got := d.Recipients()
	if len(got) != 3 {
		t.Fatalf("recipients = %v", got)
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("c@example.com")) {
		t.Fatalf("bcc on the wire:\n%s", raw)
	}
}

func TestHardening_CalendarReplyIsInlineAndAttached(t *testing.T) {
	ics := []byte("BEGIN:VCALENDAR\r\nMETHOD:REPLY\r\nEND:VCALENDAR\r\n")
	d := Draft{
		From: Address{Addr: "me@example.com"}, To: []Address{{Addr: "boss@example.org"}},
		Subject: "Accepted: Design review", Body: "ACCEPTED: Design review\n",
		CalendarMethod: "REPLY", CalendarICS: ics,
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(bytes.ToLower(raw), []byte("text/calendar")) {
		t.Fatalf("no text/calendar part:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("invite.ics")) {
		t.Fatalf("no invite.ics attachment:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("METHOD:REPLY")) && !bytes.Contains(raw, ics) {
		t.Fatalf("calendar body missing:\n%s", raw)
	}
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_ = r
}
