package mail

import (
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"golang.org/x/text/encoding/ianaindex"

	"mailbox/src/internal/htmlmd"
	"mailbox/src/internal/imaputf7"
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
	text := strings.TrimSpace(strings.Join(plain, "\n"))
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
	body := strings.TrimSpace(strings.Join(plain, "\n"))
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
