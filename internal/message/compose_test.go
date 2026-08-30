package message

import (
	"bytes"
	"io"
	"strings"
	"testing"

	gomail "github.com/emersion/go-message/mail"
)

// read parses composed bytes the way a receiving client would.
func read(t *testing.T, raw []byte) (gomail.Header, string, []Attachment) {
	t.Helper()
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the mail we composed does not parse: %v", err)
	}
	var text string
	var files []Attachment
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("part: %v", err)
		}
		body, err := io.ReadAll(p.Body)
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		switch h := p.Header.(type) {
		case *gomail.AttachmentHeader:
			name, _ := h.Filename()
			kind, _, _ := h.ContentType()
			files = append(files, Attachment{Filename: name, MIMEType: kind, Content: body})
		default:
			text += string(body)
		}
	}
	return r.Header, text, files
}

func TestComposeSurvivesBeingRead(t *testing.T) {
	d := Draft{
		From:    Address{Name: "Max Mustermann", Addr: "peter@example.org"},
		To:      []Address{{Name: "Käthe Groß", Addr: "kaethe@example.com"}},
		Subject: "Rechnung für März",
		Body:    "Hallo Käthe,\n\nanbei die Rechnung. Grüße!",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	h, text, files := read(t, raw)

	// The header goes out encoded and has to come back as what was typed.
	if subject, _ := h.Subject(); subject != "Rechnung für März" {
		t.Fatalf("subject = %q", subject)
	}
	if !strings.Contains(text, "Grüße") {
		t.Fatalf("body = %q", text)
	}
	if len(files) != 0 {
		t.Fatalf("a mail with no attachments carries %d", len(files))
	}
	to, err := h.AddressList("To")
	if err != nil || len(to) != 1 || to[0].Address != "kaethe@example.com" || to[0].Name != "Käthe Groß" {
		t.Fatalf("to = %v (%v)", to, err)
	}
	// The Message-ID is ours, minted before the server has seen the mail, and
	// it is what the Outbox row and the Sent copy are both named by.
	id, err := h.MessageID()
	if err != nil || id != d.MessageID || id == "" {
		t.Fatalf("message-id = %q, draft says %q (%v)", id, d.MessageID, err)
	}
	if strings.ContainsAny(d.MessageID, "<>") {
		t.Fatalf("the draft's message id should be bare, got %q", d.MessageID)
	}
	if d.Date.IsZero() {
		t.Fatal("a mail with no Date is a mail that sorts wrong everywhere")
	}
}

func TestTheTextPartSaysWhatCharsetItIsIn(t *testing.T) {
	// A text part with no charset is us-ascii by definition, and a UTF-8 body
	// declared as us-ascii comes back as one replacement character per byte.
	// This is checked on the bytes rather than through a reader, because a
	// reader that ignores the charset is exactly how it went unnoticed.
	for _, tc := range []struct {
		name  string
		files []Attachment
	}{
		{"plain", nil},
		{"with an attachment", []Attachment{{Filename: "x.pdf", MIMEType: "application/pdf", Content: []byte{1, 2, 3}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Draft{
				From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
				Subject: "Umlaute", Body: "Der Körper — mit Gedankenstrich.", Attachments: tc.files,
			}
			raw, err := d.Build()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(bytes.ToLower(raw), []byte("charset=utf-8")) {
				t.Fatalf("no charset on the text part:\n%s", raw)
			}
			if !bytes.Contains(bytes.ToLower(raw), []byte("quoted-printable")) {
				t.Fatalf("the text part is not quoted-printable:\n%s", raw)
			}
			// The em dash went out as its UTF-8 bytes, encoded, not as itself.
			if !bytes.Contains(raw, []byte("=E2=80=94")) {
				t.Fatalf("the body is not encoded as utf-8:\n%s", raw)
			}
		})
	}
}

func TestAttachmentSurvivesByteForByte(t *testing.T) {
	// Deliberately not text: an attachment that survives because it was ASCII
	// all along proves nothing about the encoding.
	payload := []byte{0x00, 0xff, 0xfe, '%', 'P', 'D', 'F', 0x80, 0x0a, 0x0d, 0x1a}
	d := Draft{
		From:    Address{Addr: "peter@example.org"},
		To:      []Address{{Addr: "kaethe@example.com"}},
		Subject: "Anhang", Body: "siehe Anhang",
		Attachments: []Attachment{{Filename: "rechnung.pdf", MIMEType: "application/pdf", Content: payload}},
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	_, text, files := read(t, raw)
	if !strings.Contains(text, "siehe Anhang") {
		t.Fatalf("text part = %q", text)
	}
	if len(files) != 1 {
		t.Fatalf("attachments = %d", len(files))
	}
	if files[0].Filename != "rechnung.pdf" || files[0].MIMEType != "application/pdf" {
		t.Fatalf("attachment = %+v", Attachment{Filename: files[0].Filename, MIMEType: files[0].MIMEType})
	}
	if !bytes.Equal(files[0].Content, payload) {
		t.Fatalf("attachment bytes changed: %v", files[0].Content)
	}
}

func TestBccIsInTheEnvelopeAndNotInTheMail(t *testing.T) {
	d := Draft{
		From:    Address{Addr: "peter@example.org"},
		To:      []Address{{Addr: "kaethe@example.com"}},
		Cc:      []Address{{Addr: "chef@example.com"}},
		Bcc:     []Address{{Addr: "archiv@example.org"}},
		Subject: "Angebot", Body: "hier",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	rcpt := d.Recipients()
	if len(rcpt) != 3 || rcpt[2] != "archiv@example.org" {
		t.Fatalf("recipients = %v", rcpt)
	}
	// A blind copy that is written into the message is not blind: the copy
	// filed in Sent would carry it, and one forward exposes it.
	if bytes.Contains(raw, []byte("archiv@example.org")) {
		t.Fatalf("the bcc address is in the message:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("chef@example.com")) {
		t.Fatal("the cc address should be in the message")
	}
}

func TestReplyHeadersAreWritten(t *testing.T) {
	d := Draft{
		From:       Address{Addr: "peter@example.org"},
		To:         []Address{{Addr: "kaethe@example.com"}},
		Subject:    "Re: Angebot",
		Body:       "passt",
		InReplyTo:  []string{"parent@example.com"},
		References: []string{"first@example.com", "parent@example.com"},
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	h, _, _ := read(t, raw)
	inReplyTo, err := h.MsgIDList("In-Reply-To")
	if err != nil || len(inReplyTo) != 1 || inReplyTo[0] != "parent@example.com" {
		t.Fatalf("in-reply-to = %v (%v)", inReplyTo, err)
	}
	refs, err := h.MsgIDList("References")
	if err != nil || len(refs) != 2 || refs[0] != "first@example.com" {
		t.Fatalf("references = %v (%v)", refs, err)
	}
}

func TestEveryLineEndsCRLF(t *testing.T) {
	// An IMAP literal is defined in CRLF-terminated lines and so is SMTP DATA.
	// A bare LF is the kind of thing one server tolerates and the next refuses.
	d := Draft{
		From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
		Subject: "zwei Zeilen", Body: "erste Zeile\nzweite Zeile",
		Attachments: []Attachment{{Filename: "x.bin", MIMEType: "application/octet-stream", Content: bytes.Repeat([]byte{7}, 200)}},
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range raw {
		if b == '\n' && (i == 0 || raw[i-1] != '\r') {
			t.Fatalf("bare LF at byte %d", i)
		}
	}
}

func TestABodylessDraftIsRefused(t *testing.T) {
	if _, err := (&Draft{To: []Address{{Addr: "k@example.com"}}}).Build(); err == nil {
		t.Fatal("a mail with no sender should not build")
	}
	if _, err := (&Draft{From: Address{Addr: "p@example.org"}}).Build(); err == nil {
		t.Fatal("a mail with no recipient should not build")
	}
}

func TestParseAddressList(t *testing.T) {
	list, err := ParseAddressList("Käthe Groß <k@example.com>, chef@example.com")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v (%v)", list, err)
	}
	if list[0].Name != "Käthe Groß" || list[1].Addr != "chef@example.com" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := ParseAddressList("not an address"); err == nil {
		t.Fatal("that is not an address")
	}
}
