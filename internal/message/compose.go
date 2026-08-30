// Package message builds the RFC 5322 bytes of a mail we are about to send.
//
// Composition happens once, before anything durable or networked touches the
// mail: the Outbox stores the finished bytes, SMTP hands over those bytes, and
// the copy filed in Sent is those bytes again. A mail that is rebuilt per step
// is a mail whose Sent copy can disagree with what the recipient got.
package message

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// Address is one mailbox, with the display name it is written under.
type Address struct {
	Name string `json:"name,omitempty"`
	Addr string `json:"addr"`
}

// String renders the address the way a header writes it.
func (a Address) String() string {
	if a.Name == "" {
		return a.Addr
	}
	return (&mail.Address{Name: a.Name, Address: a.Addr}).String()
}

// Attachment is a file to carry. The bytes are read before the Draft is built,
// because a mail that names a file it cannot read must fail before it is
// queued, not after.
type Attachment struct {
	Filename string
	MIMEType string
	Content  []byte
}

// LoadAttachment reads a file from disk. The Daemon does this rather than the
// CLI: same user, same machine, and a 20 MB PDF has no business being base64
// inside NDJSON — the same reason `attachment save` writes the file itself.
func LoadAttachment(path string) (Attachment, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, err
	}
	name := filepath.Base(path)
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind == "" {
		kind = "application/octet-stream"
	}
	return Attachment{Filename: name, MIMEType: kind, Content: body}, nil
}

// Draft is a mail before it exists. Its Message-ID is chosen here rather than
// by the server, so the Outbox row, the copy in Sent and the Thread the reply
// belongs to all name the same Message.
type Draft struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	Body        string
	InReplyTo   []string // Message-IDs, without angle brackets
	References  []string
	Attachments []Attachment
	// MessageID and Date are filled in by Build when empty.
	MessageID string
	Date      time.Time
}

// Recipients is the SMTP envelope: everyone the mail goes to, deduplicated.
//
// Bcc appears here and nowhere else. A Bcc header written into the message is
// how a blind copy stops being blind — the copy filed in Sent would carry it,
// and one careless forward exposes it. The Outbox row keeps the record instead.
func (d Draft) Recipients() []string {
	var out []string
	seen := map[string]bool{}
	for _, group := range [][]Address{d.To, d.Cc, d.Bcc} {
		for _, a := range group {
			key := strings.ToLower(a.Addr)
			if a.Addr == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, a.Addr)
		}
	}
	return out
}

// Build renders the mail. It returns the bytes that go to SMTP, to the Outbox
// and to the Sent folder — all three the same, byte for byte.
func (d *Draft) Build() ([]byte, error) {
	if d.From.Addr == "" {
		return nil, fmt.Errorf("a mail needs a sender")
	}
	if len(d.Recipients()) == 0 {
		return nil, fmt.Errorf("a mail needs at least one recipient")
	}
	if d.Date.IsZero() {
		d.Date = time.Now()
	}
	if d.MessageID == "" {
		d.MessageID = NewMessageID(domainOf(d.From.Addr))
	}

	var h gomail.Header
	h.SetDate(d.Date)
	h.SetSubject(d.Subject)
	h.SetMessageID(d.MessageID)
	h.SetAddressList("From", addrList([]Address{d.From}))
	h.SetAddressList("To", addrList(d.To))
	if len(d.Cc) > 0 {
		h.SetAddressList("Cc", addrList(d.Cc))
	}
	// The chain a Thread is built from, in both directions (ADR-0008). Our own
	// reply has to carry it for the recipient's client, and for ours: the copy
	// filed in Sent is mirrored like any other mail and threads off these.
	if len(d.InReplyTo) > 0 {
		h.SetMsgIDList("In-Reply-To", d.InReplyTo)
	}
	if len(d.References) > 0 {
		h.SetMsgIDList("References", d.References)
	}
	h.Set("User-Agent", "mailbox")

	var buf bytes.Buffer
	if len(d.Attachments) == 0 {
		// The charset goes on the message's own header, because there is no
		// part header to put it on. Leaving it off does not mean "unspecified":
		// a text part with no charset is us-ascii by definition, and every
		// reader on the way — including our own mirror — then turns each byte
		// of a UTF-8 character into a replacement character. A one-attachment
		// mail was fine and a plain reply was quietly mangled.
		h.Set("Content-Type", "text/plain; charset=utf-8")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(d.text())); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return crlf(buf.Bytes()), nil
	}

	h.Set("Content-Type", "multipart/mixed")
	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	var th gomail.InlineHeader
	th.Set("Content-Type", "text/plain; charset=utf-8")
	tw, err := mw.CreateSingleInline(th)
	if err != nil {
		return nil, err
	}
	if _, err := tw.Write([]byte(d.text())); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	for _, a := range d.Attachments {
		var ah gomail.AttachmentHeader
		ah.Set("Content-Type", a.MIMEType)
		ah.SetFilename(a.Filename)
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.Content); err != nil {
			return nil, err
		}
		if err := aw.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return crlf(buf.Bytes()), nil
}

// text is the body as it will be sent. A body that does not end in a newline is
// given one: the last line of a mail is a line like any other.
func (d Draft) text() string {
	if d.Body == "" || strings.HasSuffix(d.Body, "\n") {
		return d.Body
	}
	return d.Body + "\n"
}

// crlf makes every line ending a CRLF. Both destinations require it — an IMAP
// literal is defined in CRLF-terminated lines, and SMTP DATA the same — and the
// encoders below us are not obliged to produce it for every part.
func crlf(b []byte) []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
}

// NewMessageID mints a Message-ID. It is ours, not the server's: the Outbox row
// and the Sent copy have to name the same Message before the server has ever
// seen it.
func NewMessageID(domain string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A clock-only id is still unique enough to be a key here.
		return fmt.Sprintf("%d.mailbox@%s", time.Now().UnixNano(), domain)
	}
	return fmt.Sprintf("%d.%s.mailbox@%s", time.Now().Unix(), hex.EncodeToString(b[:]), domain)
}

func domainOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return "localhost"
}

func addrList(addrs []Address) []*gomail.Address {
	out := make([]*gomail.Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, &gomail.Address{Name: a.Name, Address: a.Addr})
	}
	return out
}

// ParseAddress reads what a caller typed: "a@b", "Name <a@b>", or a name with
// commas in it. An address that will not parse is refused here rather than by
// the server, because the caller is still on the other end of the socket.
func ParseAddress(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Address{}, fmt.Errorf("empty address")
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return Address{}, fmt.Errorf("%q is not an address: %w", s, err)
	}
	return Address{Name: a.Name, Addr: a.Address}, nil
}

// ParseAddressList reads one --to value, which may itself hold several
// comma-separated addresses.
func ParseAddressList(s string) ([]Address, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		// One malformed entry should name itself, not the whole list.
		a, aerr := ParseAddress(s)
		if aerr != nil {
			return nil, fmt.Errorf("%q is not an address list: %w", s, err)
		}
		return []Address{a}, nil
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Addr: a.Address})
	}
	return out, nil
}
