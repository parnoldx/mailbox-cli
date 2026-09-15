// Package unsubscribe decides how to leave a mailing list: from the headers
// RFC 2369 and RFC 8058 define for exactly this, or — failing that — from a
// link the message's own body offers.
package unsubscribe

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

// Kind is how a Target is exercised. The zero value means there is nothing to
// offer.
type Kind string

const (
	None Kind = ""
	// OneClick means the sender declared support for RFC 8058: a bare POST to
	// URL unsubscribes, no page visit needed.
	OneClick Kind = "one_click"
	// Email means sending To (with Subject/Body, when the mailto: URI named
	// them) unsubscribes.
	Email Kind = "email"
	// Link means opening URL in a browser is the best that can be done — the
	// sender gave no one-click header, or this is only a guess at a link in
	// the body, and either way a page may want a human on it.
	Link Kind = "link"
)

// Target is what a caller acts on to unsubscribe from a message.
type Target struct {
	Kind    Kind
	URL     string
	To      string
	Subject string
	Body    string
}

// Of decides how to unsubscribe from a message: List-Unsubscribe first (the
// sender's own word for it), and only when the message carries none, a guess
// at an unsubscribe link in its HTML body. A plain-text body is not read — an
// auth-shaped list mail that skips the header is HTML in practice, and a bare
// URL in prose is not an unsubscribe link until something says so.
func Of(listUnsubscribe, listUnsubscribePost, html string) Target {
	if t := fromHeader(listUnsubscribe, listUnsubscribePost); t.Kind != None {
		return t
	}
	return fromBody(html)
}

// angleURI matches one <...> entry of a List-Unsubscribe header (RFC 2369
// lists them comma-separated, each wrapped in angle brackets).
var angleURI = regexp.MustCompile(`<([^>]+)>`)

func fromHeader(listUnsubscribe, listUnsubscribePost string) Target {
	var mailto, https string
	for _, m := range angleURI.FindAllStringSubmatch(listUnsubscribe, -1) {
		uri := strings.TrimSpace(m[1])
		switch {
		case strings.HasPrefix(strings.ToLower(uri), "mailto:") && mailto == "":
			mailto = uri
		case strings.HasPrefix(strings.ToLower(uri), "https://") && https == "":
			https = uri
		}
	}
	oneClick := strings.Contains(strings.ToLower(listUnsubscribePost), "list-unsubscribe=one-click")
	if https != "" && oneClick {
		return Target{Kind: OneClick, URL: https}
	}
	if https != "" {
		return Target{Kind: Link, URL: https}
	}
	if mailto != "" {
		return mailtoTarget(mailto)
	}
	return Target{}
}

func mailtoTarget(uri string) Target {
	u, err := url.Parse(uri)
	if err != nil || u.Opaque == "" {
		return Target{}
	}
	addr, _, _ := strings.Cut(u.Opaque, "?")
	if a, err := mail.ParseAddress(addr); err == nil {
		addr = a.Address
	}
	q := u.Query()
	return Target{Kind: Email, To: addr, Subject: q.Get("subject"), Body: q.Get("body")}
}

// unsubLink matches an <a href="...">text</a> whose href or text names the
// act, in English or German — the two languages this mailbox reads mail in.
var unsubLink = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
var tag = regexp.MustCompile(`(?s)<[^>]*>`)

var keywords = []string{
	"unsub",
	"abmelden", "abbestellen", "abbestellung", "austragen",
}

func mentionsUnsub(s string) bool {
	s = strings.ToLower(s)
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// fromBody is a best-effort guess at an unsubscribe link the message offers
// itself, for the many senders that skip the header. It only ever proposes
// opening the page — a body link was never declared safe to POST to blind.
func fromBody(html string) Target {
	for _, m := range unsubLink.FindAllStringSubmatch(html, -1) {
		href, text := m[1], tag.ReplaceAllString(m[2], " ")
		if mentionsUnsub(href) || mentionsUnsub(text) {
			return Target{Kind: Link, URL: href}
		}
	}
	return Target{}
}
