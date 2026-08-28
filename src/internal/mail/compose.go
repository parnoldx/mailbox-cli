package mail

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	nativeNetMail "net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/folders"
	"mailbox/src/internal/htmlmd"
)

type OutAttachment struct {
	Name        string
	Data        []byte
	ContentType string
}

// MaxInlineAttachment is the largest attachment sent inline; bigger ones are
// uploaded to transfer.adminforge.de and linked in the body instead.
const MaxInlineAttachment = 10 << 20 // 10 MiB

var transferURL = "https://transfer.adminforge.de/"

// TransferHost names the third-party host used for oversized attachments, so
// callers can say where the data would go before sending it there.
func TransferHost() string {
	if u, err := url.Parse(transferURL); err == nil && u.Host != "" {
		return u.Host
	}
	return transferURL
}

// UploadToTransfer PUTs data to a transfer.sh host and returns the download URL.
func UploadToTransfer(name string, data []byte) (string, error) {
	url := transferURL + url.PathEscape(name)
	// ponytail: 10min timeout; raise if multi-GB uploads ever matter
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transfer upload failed: %s", resp.Status)
	}
	return strings.TrimSpace(string(body)), nil
}

// ReadAttachmentFile loads an --attach path.
func ReadAttachmentFile(path string) (OutAttachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OutAttachment{}, fmt.Errorf("attachment not found: %s", path)
		}
		return OutAttachment{}, err
	}
	name := filepath.Base(path)
	ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if ctype == "" {
		ctype = "application/octet-stream"
	} else if i := strings.Index(ctype, ";"); i >= 0 {
		ctype = strings.TrimSpace(ctype[:i])
	}
	return OutAttachment{Name: name, Data: data, ContentType: ctype}, nil
}

var msgIDCounter atomic.Uint64

func makeMsgID(domain string) string {
	msgIDCounter.Add(1)
	now := time.Now()
	return fmt.Sprintf("<%d.%d.mailbox@%s>", now.UnixNano(), msgIDCounter.Load(), domain)
}

func encodeHeaderText(value string) string {
	if isASCII(value) {
		return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\n", " ")
	}
	return mime.QEncoding.Encode("utf-8", value)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func formatAddress(display, email string) string {
	if isASCII(display) && !strings.ContainsAny(display, `"()<>,;:@[\]`) {
		if display == "" {
			return email
		}
		return display + " <" + email + ">"
	}
	a := nativeNetMail.Address{Name: display, Address: email}
	return a.String()
}

// BuildOutgoing renders the RFC 5322 message; mirrors python _build_outgoing.
func (m *Mail) BuildOutgoing(out *Outgoing) ([]byte, string, error) {
	to := out.To
	subject := out.Subject
	inReplyTo := out.InReplyTo
	references := out.References
	if out.ReplyTo != nil {
		orig, err := m.full(out.ReplyTo[0], out.ReplyTo[1])
		if err != nil {
			return nil, "", err
		}
		if subject == "" {
			subject = orig.Subject
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
		}
		if orig.MessageID != "" {
			inReplyTo = orig.MessageID
			refs := strings.Fields(orig.References)
			found := false
			for _, r := range refs {
				if r == orig.MessageID {
					found = true
					break
				}
			}
			if !found {
				refs = append(refs, orig.MessageID)
			}
			references = strings.Join(refs, " ")
		}
		if len(to) == 0 {
			replyTo := orig.ReplyTo
			if replyTo == "" {
				replyTo = orig.From
			}
			to = addressList(replyTo)
		}
	}

	msgid := makeMsgID(strings.SplitN(m.Acct.Email, "@", 2)[1])
	var headers []string
	headers = append(headers,
		"From: "+formatAddress(m.Acct.DisplayName, m.Acct.Email),
		"To: "+encodeAddressList(to))
	if len(out.Cc) > 0 {
		headers = append(headers, "Cc: "+encodeAddressList(out.Cc))
	}
	if len(out.Bcc) > 0 {
		headers = append(headers, "Bcc: "+encodeAddressList(out.Bcc))
	}
	headers = append(headers,
		"Date: "+time.Now().Format(time.RFC1123Z),
		"Message-ID: "+msgid,
		"Subject: "+encodeHeaderText(subject))
	if inReplyTo != "" {
		headers = append(headers, "In-Reply-To: "+inReplyTo)
	}
	if references != "" {
		headers = append(headers, "References: "+references)
	}
	headers = append(headers, "MIME-Version: 1.0")

	html := out.HTML
	plain := out.Body
	if html != "" {
		if plain == "" {
			plain = HTMLToText(html)
		}
	} else if plain != "" {
		html = htmlmd.MarkdownToHTML(plain)
	}

	contentType, body := buildBody(plain, html, out.Attachments)
	// RFC 2045 §6.4: a composite type carries no content encoding of its own —
	// each part declares its own. Only the single-part body is quoted-printable.
	topCTE := "quoted-printable"
	if strings.HasPrefix(contentType, "multipart/") {
		topCTE = "7bit"
	}
	headers = append(headers, "Content-Type: "+contentType,
		"Content-Transfer-Encoding: "+topCTE)
	raw := append([]byte(strings.Join(headers, "\r\n")), []byte("\r\n\r\n")...)
	raw = append(raw, body...)
	return raw, msgid, nil
}

func encodeAddressList(addrs []string) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a
	}
	return strings.Join(parts, ", ")
}

func qpPart(ctype string, text string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Type: %s; charset=utf-8\r\n", ctype)
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(&buf)
	w.Write([]byte(text))
	w.Close()
	return buf.Bytes()
}

func attachmentPart(att OutAttachment) []byte {
	ctype := att.ContentType
	if ctype == "" || !strings.Contains(ctype, "/") {
		ctype = "application/octet-stream"
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Type: %s; name=\"%s\"\r\n", ctype, sanitizeToken(att.Name))
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", sanitizeToken(att.Name))
	enc := base64.StdEncoding.EncodeToString(att.Data)
	for len(enc) > 76 {
		buf.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	buf.WriteString(enc + "\r\n")
	return buf.Bytes()
}

func sanitizeToken(s string) string {
	return strings.NewReplacer(`"`, "'", "\r", " ", "\n", " ").Replace(s)
}

func randomBoundary() string {
	return "=_" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// buildBody returns (contentTypeHeader, bodyBytes).
func buildBody(plain, html string, attachments []OutAttachment) (string, []byte) {
	boundary := randomBoundary()
	var buf bytes.Buffer

	writeAlt := func() {
		altBoundary := boundary + "-alt"
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + altBoundary + "\"\r\n\r\n")
		buf.WriteString("--" + altBoundary + "\r\n")
		buf.Write(qpPart("text/plain", plain))
		buf.WriteString("\r\n--" + altBoundary + "\r\n")
		buf.Write(qpPart("text/html", html))
		buf.WriteString("\r\n--" + altBoundary + "--\r\n")
	}

	switch {
	case len(attachments) > 0:
		ct := "multipart/mixed; boundary=\"" + boundary + "\""
		if html != "" {
			writeAlt()
		} else {
			buf.WriteString("--" + boundary + "\r\n")
			buf.Write(qpPart("text/plain", plain))
			buf.WriteString("\r\n")
		}
		for _, att := range attachments {
			buf.WriteString("--" + boundary + "\r\n")
			buf.Write(attachmentPart(att))
			buf.WriteString("\r\n")
		}
		buf.WriteString("--" + boundary + "--\r\n")
		return ct, buf.Bytes()
	case html != "":
		writeAlt()
		return "multipart/alternative; boundary=\"" + boundary + "\"", buf.Bytes()
	default:
		return "text/plain; charset=utf-8", qpPart("text/plain", plain)
	}
}

// SendMessage delivers raw bytes via SMTP (implicit TLS on 465, STARTTLS on 587).
func SendMessage(acct *config.Account, raw []byte, rcpts []string, _ []byte) error {
	from := acct.Email
	hostPort := net.JoinHostPort(acct.SMTPHost, strconv.Itoa(acct.SMTPPort))
	auth := smtp.PlainAuth("", acct.Email, acct.Password, acct.SMTPHost)
	send := func(client *smtp.Client) error {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(from); err != nil {
			return err
		}
		for _, rcpt := range rcpts {
			if err := client.Rcpt(rcpt); err != nil {
				return err
			}
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(raw); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	if acct.SMTPPort == 587 {
		conn, err := net.Dial("tcp", hostPort)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, acct.SMTPHost)
		if err != nil {
			conn.Close()
			return err
		}
		defer client.Close()
		if err := client.StartTLS(&tls.Config{ServerName: acct.SMTPHost}); err != nil {
			return err
		}
		return send(client)
	}
	conn, err := tls.Dial("tcp", hostPort, &tls.Config{ServerName: acct.SMTPHost})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, acct.SMTPHost)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	return send(client)
}

// --- helpers over raw messages ---

func splitRaw(raw []byte) ([]byte, []byte) {
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if i := bytes.Index(raw, []byte(sep)); i >= 0 {
			return raw[:i], raw[i+len(sep):]
		}
	}
	return raw, nil
}

func headerValueOf(raw []byte, key string) string {
	head, _ := splitRaw(raw)
	currentKey := ""
	values := map[string]string{}
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			values[currentKey] += " " + strings.TrimSpace(line)
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		currentKey = strings.ToLower(strings.TrimSpace(line[:i]))
		if _, dup := values[currentKey]; !dup {
			values[currentKey] = strings.TrimSpace(line[i+1:])
		}
	}
	return values[strings.ToLower(key)]
}

func stripBcc(raw []byte) []byte {
	head, body := splitRaw(raw)
	var out []string
	skipping := false
	for _, line := range strings.Split(string(head), "\n") {
		lowered := strings.ToLower(line)
		if strings.HasPrefix(lowered, "bcc:") {
			skipping = true
			continue
		}
		if skipping && line != "" && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		skipping = false
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n") + "\r\n\r\n" + string(body))
}

func recipientsOf(raw []byte, extraBcc []string) []string {
	var rcpts []string
	addrs := func(headerValues ...string) {
		for _, v := range headerValues {
			if v == "" {
				continue
			}
			list, err := nativeNetMail.ParseAddressList(v)
			if err != nil {
				for _, part := range strings.Split(v, ",") {
					part = strings.TrimSpace(part)
					if strings.Contains(part, "@") {
						rcpts = append(rcpts, part)
					}
				}
				continue
			}
			for _, a := range list {
				rcpts = append(rcpts, a.Address)
			}
		}
	}
	addrs(headerValueOf(raw, "To"), headerValueOf(raw, "Cc"), headerValueOf(raw, "Bcc"))
	rcpts = append(rcpts, extraBcc...)
	seen := map[string]bool{}
	var uniq []string
	for _, r := range rcpts {
		r = strings.TrimSpace(r)
		if r != "" && !seen[r] {
			uniq = append(uniq, r)
			seen[r] = true
		}
	}
	return uniq
}

// DeliveredError reports a step that failed after SMTP already accepted the
// message — filing it in Sent, removing the draft. The mail is gone; retrying
// the send would deliver it twice, so callers must report this as a warning on
// a successful send, not as a failure.
type DeliveredError struct {
	Step string
	Err  error
}

func (e *DeliveredError) Error() string {
	return fmt.Sprintf("message was sent, but %s failed: %v", e.Step, e.Err)
}

func (e *DeliveredError) Unwrap() error { return e.Err }

// Delivered reports whether err means the message went out despite the error.
func Delivered(err error) bool {
	var d *DeliveredError
	return errors.As(err, &d)
}

// Compose sends via SMTP or saves to Drafts; returns the new message id.
// A non-nil error for which Delivered reports true means the message was sent.
func (m *Mail) Compose(out *Outgoing, draft bool) (string, error) {
	raw, msgid, err := m.BuildOutgoing(out)
	if err != nil {
		return "", err
	}
	if draft {
		return m.AppendBytes(folders.DRAFTS, "\\Draft", raw, msgid)
	}
	wire := stripBcc(raw)
	rcpts := recipientsOf(raw, nil)
	if err := m.SMTPSend(m.Acct, wire, rcpts, wire); err != nil {
		return "", err
	}
	sentID, err := m.AppendBytes(folders.SENT, "\\Seen", raw, msgid)
	if err != nil {
		return msgid, &DeliveredError{Step: "filing a copy in Sent", Err: err}
	}
	return sentID, nil
}

func (m *Mail) Draft(to []string, subject, body string, replyTo *[2]string) (string, error) {
	return m.Compose(&Outgoing{To: to, Subject: subject, Body: body, ReplyTo: replyTo}, true)
}

func (m *Mail) SendDraft(folder, uid string) (string, error) {
	resolved, err := folders.ResolveFolder(folder, nil)
	if err != nil {
		return "", err
	}
	if resolved != folders.DRAFTS {
		return "", fmt.Errorf("draft must be in Drafts, got %s", resolved)
	}
	if err := m.Select(resolved, true); err != nil {
		return "", err
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", uid, "(BODY.PEEK[])")
	if err != nil || resp.Status != "OK" {
		return "", fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	recs := splitFetchChunks(resp.Chunks)
	if len(recs) == 0 || recs[0].body == nil {
		return "", fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	raw := recs[0].body
	msgid := headerValueOf(raw, "Message-ID")
	wire := stripBcc(raw)
	if err := m.SMTPSend(m.Acct, wire, recipientsOf(raw, nil), wire); err != nil {
		return "", err
	}
	sentID, err := m.AppendBytes(folders.SENT, "\\Seen", raw, msgid)
	if err != nil {
		return msgid, &DeliveredError{Step: "filing a copy in Sent", Err: err}
	}
	if err := m.purge(resolved, uid); err != nil {
		return sentID, &DeliveredError{Step: "removing the draft", Err: err}
	}
	return sentID, nil
}

func (m *Mail) EditDraft(folder, uid string, to, cc, bcc []string, subject, body, htmlBody *string) (string, error) {
	resolved, err := folders.ResolveFolder(folder, nil)
	if err != nil {
		return "", err
	}
	if resolved != folders.DRAFTS {
		return "", fmt.Errorf("draft must be in Drafts, got %s", resolved)
	}
	orig, err := m.full(resolved, uid)
	if err != nil {
		return "", err
	}
	newBody, newHTML := orig.Body, orig.BodyHTML
	switch {
	case body != nil && htmlBody != nil:
		newHTML = *htmlBody
		newBody = *body
	case htmlBody != nil:
		newHTML = *htmlBody
		newBody = ""
	case body != nil:
		newBody = *body
		newHTML = ""
	}
	if to == nil {
		to = addressList(orig.To)
	}
	if cc == nil {
		cc = addressList(orig.Cc)
	}
	if bcc == nil {
		bcc = addressList(orig.Bcc)
	}
	outSubject := orig.Subject
	if subject != nil {
		outSubject = *subject
	}
	atts := make([]OutAttachment, 0, len(orig.Attachments))
	for _, a := range orig.Attachments {
		atts = append(atts, OutAttachment{Name: a.Name, Data: a.Payload(), ContentType: a.ContentType})
	}
	newID, err := m.Compose(&Outgoing{
		To: to, Cc: cc, Bcc: bcc, Subject: outSubject,
		Body: newBody, HTML: newHTML, Attachments: atts,
		InReplyTo: orig.InReplyTo, References: orig.References,
	}, true)
	if err != nil {
		return "", err
	}
	if err := m.purge(resolved, uid); err != nil {
		return "", err
	}
	return newID, nil
}
