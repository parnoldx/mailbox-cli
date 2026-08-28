package mail

import (
	"fmt"
	"html"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/encoding/ianaindex"

	"mailbox/src/internal/htmlmd"
	"mailbox/src/internal/imaputf7"
	"mailbox/src/internal/terminal"
)

func stderr() *os.File { return os.Stderr }

func utf7Decode(s string) string { return imaputf7.Decode(s) }

func ShortFrom(value string) string {
	name := ""
	addr := ""
	if a, err := mail.ParseAddress(value); err == nil {
		name = strings.Join(strings.Fields(a.Name), " ")
		addr = a.Address
	}
	if name != "" {
		return name
	}
	if addr != "" {
		return addr
	}
	return strings.TrimSpace(value)
}

func FmtDate(value string) string {
	if value == "" {
		return ""
	}
	if t, err := mail.ParseDate(value); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "2 Jan 2006 15:04:05 -0700"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	return value
}

// entityNoise matches HTML entity references that have no business in a
// real text/plain part: some ESPs generate the plain alternative by
// crudely stripping tags, leaving &nbsp;/&zwnj;/&#8203; spacer entities
// (and the hidden preheader text they pad) as literal characters.
var entityNoise = regexp.MustCompile(`&(?:nbsp|zwnj|zwj|shy|#x?[0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]+);`)

// blankish matches a line that carries no visible content: empty, or only
// spaces / tabs / no-break and other exotic spaces.
var blankish = regexp.MustCompile(`(?m)^[\s\x{00A0}\x{202F}\x{2000}-\x{200A}\x{205F}\x{3000}]*$`)

// sanitizePlainText repairs a botched text/plain part: it decodes stray
// HTML entities (only when the part clearly contains them), drops the
// invisible spacer characters newsletters pad preheaders with, and
// collapses the blank lines that padding leaves behind.
func sanitizePlainText(text string) string {
	if text == "" {
		return ""
	}
	if entityNoise.MatchString(text) {
		text = html.UnescapeString(text)
	}
	text = terminal.SanitizeText(text)
	text = blankish.ReplaceAllString(text, "")
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(text)
}

func looksLikeHTML(text string) bool {
	if htmlLeadRe.MatchString(text) {
		return true
	}
	head := strings.ToLower(text[:min(200, len(text))])
	return strings.Contains(head, "<html") || strings.Contains(head, "<div")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// partsForThread walks leaves; nested message/rfc822 counts as its own part.
func partsForThread(p *Part) []*Part {
	if p.CType == "message/rfc822" || p.CType == "message/global" {
		return []*Part{p}
	}
	if p.IsMultipart() {
		var out []*Part
		for _, child := range p.Children {
			out = append(out, partsForThread(child)...)
		}
		return out
	}
	return []*Part{p}
}

func previewFromParsed(parsed *Part) string {
	var plain, htmlParts []string
	if parsed.IsMultipart() {
		for _, part := range partsForThread(parsed) {
			disp := strings.ToLower(part.HeaderGet("Content-Disposition"))
			if part.Filename != "" || strings.HasPrefix(disp, "attachment") ||
				part.CType == "message/rfc822" || part.CType == "message/global" {
				continue
			}
			switch part.CType {
			case "text/plain":
				plain = append(plain, part.DecodeText())
			case "text/html":
				htmlParts = append(htmlParts, part.DecodeText())
			}
		}
	} else if parsed.CType == "text/html" {
		htmlParts = append(htmlParts, parsed.DecodeText())
	} else {
		plain = append(plain, parsed.DecodeText())
	}
	text := sanitizePlainText(strings.TrimSpace(strings.Join(plain, "\n")))
	htmlBlob := strings.TrimSpace(strings.Join(htmlParts, "\n"))
	switch {
	case htmlBlob != "" && (text == "" || looksLikeHTML(text)):
		text = HTMLToText(htmlBlob)
	case looksLikeHTML(text):
		text = HTMLToText(text)
	}
	return truncateRunes(strings.Join(strings.Fields(text), " "), PreviewChars)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func threadFromParsed(folder, uid string, parsed *Part) *ThreadMessage {
	var attachments []Attachment
	var plain, htmlParts []string
	index := 0
	if parsed.IsMultipart() {
		for _, part := range partsForThread(parsed) {
			disp := strings.ToLower(part.HeaderGet("Content-Disposition"))
			nested := part.CType == "message/rfc822" || part.CType == "message/global"
			if part.Filename != "" || strings.HasPrefix(disp, "attachment") || nested {
				index++
				name := part.Filename
				if name == "" {
					name = fmt.Sprintf("part-%d", index)
				}
				attachments = append(attachments, Attachment{
					Index:       index,
					Name:        name,
					ContentType: part.CType,
					Size:        len(part.Decoded),
					part:        part,
				})
			} else if part.CType == "text/plain" && !strings.Contains(disp, "attachment") {
				plain = append(plain, part.DecodeText())
			} else if part.CType == "text/html" && !strings.Contains(disp, "attachment") {
				htmlParts = append(htmlParts, part.DecodeText())
			}
		}
	} else if parsed.CType == "text/html" {
		htmlParts = append(htmlParts, parsed.DecodeText())
	} else {
		plain = append(plain, parsed.DecodeText())
	}
	body := sanitizePlainText(strings.TrimSpace(strings.Join(plain, "\n")))
	bodyHTML := strings.TrimSpace(strings.Join(htmlParts, "\n"))
	switch {
	case bodyHTML != "" && (body == "" || looksLikeHTML(body)):
		body = htmlmd.HTMLToMarkdown(bodyHTML)
	case looksLikeHTML(body):
		if bodyHTML == "" {
			bodyHTML = body
		}
		body = htmlmd.HTMLToMarkdown(body)
	}
	state := "hydrated"
	if body == "" {
		state = "bodyless"
	}
	h := func(key string) string { return DecodeHeader(parsed.HeaderGet(key)) }
	return &ThreadMessage{
		Folder:      folder,
		UID:         uid,
		From:        h("From"),
		To:          h("To"),
		Date:        FmtDate(parsed.HeaderGet("Date")),
		Subject:     h("Subject"),
		MessageID:   h("Message-ID"),
		InReplyTo:   h("In-Reply-To"),
		References:  h("References"),
		ReplyTo:     h("Reply-To"),
		Body:        body,
		BodyHTML:    bodyHTML,
		BodyState:   state,
		Attachments: attachments,
		Cc:          h("Cc"),
		Bcc:         h("Bcc"),
	}
}

func addressList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var _ = ianaindex.MIME
