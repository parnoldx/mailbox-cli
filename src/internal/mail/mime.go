package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

// Part is a MIME entity; children are multipart sub-parts.
type Part struct {
	Header   textproto.MIMEHeader
	CType    string
	CParams  map[string]string
	Disp     string
	Filename string
	Children []*Part
	Raw      []byte
	Decoded  []byte
}

func (p *Part) IsMultipart() bool { return strings.HasPrefix(p.CType, "multipart/") }

func (p *Part) HeaderGet(key string) string {
	if p.Header == nil {
		return ""
	}
	return p.Header.Get(key)
}

// ParseMessage parses raw email bytes into a part tree; tolerates truncated data.
func ParseMessage(raw []byte) *Part {
	header, body := splitHeaderBody(raw)
	p := &Part{Raw: body}
	p.Header = header
	ct, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || ct == "" {
		ct, params = "text/plain", map[string]string{}
	}
	p.CType = strings.ToLower(ct)
	p.CParams = params
	if disp, dparams, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		p.Disp = strings.ToLower(disp)
		if fn, ok := dparams["filename"]; ok {
			p.Filename = fn
		}
	}
	if p.Filename == "" {
		p.Filename = params["name"]
	}
	p.Decoded = decodeCTE(header.Get("Content-Transfer-Encoding"), body)

	if strings.HasPrefix(p.CType, "multipart/") && p.CParams["boundary"] != "" {
		p.Children = splitMultipart(body, p.CParams["boundary"])
	}
	return p
}

func splitHeaderBody(raw []byte) (textproto.MIMEHeader, []byte) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	tp := textproto.NewReader(reader)
	header, err := tp.ReadMIMEHeader()
	if err != nil && len(header) == 0 {
		return textproto.MIMEHeader{}, raw
	}
	rest, _ := io.ReadAll(reader)
	return header, rest
}

func decodeCTE(cte string, body []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		stripped := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
				return -1
			}
			return r
		}, string(body))
		out, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(stripped)
		if err == nil {
			return out
		}
		// retry ignoring padding errors on truncated input
		for pad := 1; pad <= 2; pad++ {
			if out2, err2 := base64.StdEncoding.DecodeString(stripped + strings.Repeat("=", pad)); err2 == nil {
				return out2
			}
		}
		return body
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil && len(out) > 0 {
			return out
		}
		if err != nil {
			return body
		}
		return out
	default:
		return body
	}
}

func splitMultipart(body []byte, boundary string) []*Part {
	delim := []byte("--" + boundary)
	var parts []*Part
	var current []byte
	inPart := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if bytes.Equal(bytes.TrimRight(line, " "), delim) {
			if inPart {
				parts = append(parts, ParseMessage(current))
				current = nil
			}
			inPart = true
			continue
		}
		if bytes.Equal(bytes.TrimRight(line, " "), append(append([]byte{}, delim...), []byte("--")...)) {
			if inPart {
				parts = append(parts, ParseMessage(current))
			}
			return parts
		}
		if inPart {
			current = append(current, line...)
			current = append(current, '\n')
		}
	}
	if inPart && len(current) > 0 {
		parts = append(parts, ParseMessage(current))
	}
	return parts
}

var charsetReaders = map[string]func(string) (io.Reader, error){
	"utf-8":       func(s string) (io.Reader, error) { return strings.NewReader(s), nil },
	"us-ascii":    func(s string) (io.Reader, error) { return strings.NewReader(s), nil },
	"ascii":       func(s string) (io.Reader, error) { return strings.NewReader(s), nil },
	"iso-8859-1":  func(s string) (io.Reader, error) { r := charmap.ISO8859_1.NewDecoder().Reader(strings.NewReader(s)); return r, nil },
	"windows-1252": func(s string) (io.Reader, error) { r := charmap.Windows1252.NewDecoder().Reader(strings.NewReader(s)); return r, nil },
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	cs := strings.ToLower(charset)
	if f, ok := charsetReaders[cs]; ok {
		data, _ := io.ReadAll(input)
		return f(string(data))
	}
	enc, err := ianaGet(cs)
	if err != nil || enc == nil {
		return unicode.UTF8.NewDecoder().Reader(input), nil
	}
	return enc.NewDecoder().Reader(input), nil
}

// DecodeHeader applies RFC 2047 encoded-word decoding and whitespace collapsing.
func DecodeHeader(value string) string {
	if value == "" {
		return ""
	}
	dec := &mime.WordDecoder{CharsetReader: charsetReader}
	text, err := dec.DecodeHeader(value)
	if err != nil {
		text = value
	}
	return strings.Join(strings.Fields(text), " ")
}

// DecodeText decodes a leaf part's bytes into a string honoring its charset.
func (p *Part) DecodeText() string {
	charset := p.CParams["charset"]
	if charset == "" {
		charset = "utf-8"
	}
	r, err := charsetReader(charset, strings.NewReader(string(p.Decoded)))
	if err != nil {
		return string(p.Decoded)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return string(p.Decoded)
	}
	return string(out)
}

func ianaGet(cs string) (encoding.Encoding, error) {
	if e, err := ianaindex.MIME.Encoding(cs); err == nil && e != nil {
		return e, nil
	}
	return ianaindex.IANA.Encoding(cs)
}

var _ = fmt.Sprintf

var skipTags = map[string]bool{"script": true, "style": true, "head": true}
var breakTags = map[string]bool{
	"br": true, "p": true, "div": true, "tr": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "hr": true, "blockquote": true,
}
var endBreakTags = map[string]bool{
	"p": true, "div": true, "tr": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "blockquote": true,
}

// HTMLToText mirrors python _HTMLToText.
func HTMLToText(value string) string {
	z := html.NewTokenizer(strings.NewReader(value))
	var parts []string
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if skipTags[tag] {
				skip++
			} else if breakTags[tag] && skip == 0 {
				parts = append(parts, "\n")
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if skipTags[tag] && skip > 0 {
				skip--
			} else if endBreakTags[tag] && skip == 0 {
				parts = append(parts, "\n")
			}
		case html.TextToken:
			if skip == 0 {
				parts = append(parts, string(z.Text()))
			}
		}
	}
	lines := strings.Split(strings.Join(parts, ""), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
