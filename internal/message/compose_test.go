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

// readTyped parses composed bytes and keeps each inline part under its own
// content type, so a test can tell the text/plain twin from the text/html one.
func readTyped(t *testing.T, raw []byte) (topType string, parts map[string]string, files []Attachment) {
	t.Helper()
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the mail we composed does not parse: %v", err)
	}
	topType, _, _ = r.Header.ContentType()
	parts = map[string]string{}
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
		case *gomail.InlineHeader:
			kind, _, _ := h.ContentType()
			parts[kind] = string(body)
		}
	}
	return topType, parts, files
}

func TestPlainBodyStaysASinglePart(t *testing.T) {
	// The promise for the ordinary agent send: no BodyHTML, no multipart, byte
	// for byte what it was before this existed.
	d := Draft{
		From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
		Subject: "kurz", Body: "Passt.",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	top, parts, _ := readTyped(t, raw)
	if top != "text/plain" {
		t.Fatalf("top-level type = %q, want text/plain", top)
	}
	if _, ok := parts["text/html"]; ok {
		t.Fatal("a plain body should not carry an HTML part")
	}
}

func TestBodyHTMLMakesAnAlternative(t *testing.T) {
	d := Draft{
		From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
		Subject:  "zwei Formate",
		Body:     "Hallo Käthe,\n\nes ist **wichtig**.",
		BodyHTML: "<p>Hallo Käthe,</p><p>es ist <strong>wichtig</strong>.</p>",
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	top, parts, files := readTyped(t, raw)
	if top != "multipart/alternative" {
		t.Fatalf("top-level type = %q, want multipart/alternative", top)
	}
	if len(files) != 0 {
		t.Fatalf("no attachments were added, got %d", len(files))
	}
	plain := strings.ReplaceAll(strings.TrimRight(parts["text/plain"], "\r\n"), "\r\n", "\n")
	if plain != "Hallo Käthe,\n\nes ist **wichtig**." {
		t.Fatalf("text/plain twin = %q; it must be the body verbatim", plain)
	}
	if !strings.Contains(parts["text/html"], "<strong>wichtig</strong>") {
		t.Fatalf("text/html part = %q", parts["text/html"])
	}
	// Least-preferred first is what multipart/alternative means.
	if pi, hi := bytes.Index(raw, []byte("text/plain")), bytes.Index(raw, []byte("text/html")); pi > hi {
		t.Fatal("text/plain part must come before text/html")
	}
}

func TestBodyHTMLWithAttachmentNestsTheAlternative(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nmock")
	d := Draft{
		From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
		Subject:     "mit Anhang",
		Body:        "siehe Anhang",
		BodyHTML:    "<p>siehe Anhang</p>",
		Attachments: []Attachment{{Filename: "bild.png", MIMEType: "image/png", Content: png}},
	}
	raw, err := d.Build()
	if err != nil {
		t.Fatal(err)
	}
	top, parts, files := readTyped(t, raw)
	if top != "multipart/mixed" {
		t.Fatalf("top-level type = %q, want multipart/mixed", top)
	}
	if len(files) != 1 || files[0].Filename != "bild.png" || !bytes.Equal(files[0].Content, png) {
		t.Fatalf("attachment did not survive: %+v", files)
	}
	if _, ok := parts["text/plain"]; !ok {
		t.Fatal("missing text/plain part")
	}
	if _, ok := parts["text/html"]; !ok {
		t.Fatal("missing text/html part")
	}
}

func TestAlternativeLinesEndCRLF(t *testing.T) {
	d := Draft{
		From: Address{Addr: "peter@example.org"}, To: []Address{{Addr: "k@example.com"}},
		Subject: "crlf", Body: "erste\nzweite", BodyHTML: "<p>erste<br>zweite</p>",
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
