// Package imapclient: minimal IMAP4rev1-over-TLS client covering what the CLI
// needs (login, list, select, uid search/fetch/store/move/copy/expunge, append).
// ponytail: one connection, sequential commands, no PREAUTH/proxy/tunnel support;
// switch to a full client library if server quirks appear.
package imapclient

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Literal marks an argument sent as {n} literal bytes.
type Literal []byte

type Chunk struct {
	Text  string
	Bytes []byte
}

func (c Chunk) String() string {
	if c.Bytes != nil {
		return string(c.Bytes)
	}
	return c.Text
}

type Response struct {
	Status string   // OK/NO/BAD
	Text   string   // tagged line text
	Lines  []string // untagged lines (literals stripped)
	Chunks []Chunk  // all chunks in order (text + literals), untagged + tagged
}

type Client struct {
	conn net.Conn
	br   *bufio.Reader
	tagN int
}

func Dial(host string, port int, user, pass string) (*Client, error) {
	conn, err := tls.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{})
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, br: bufio.NewReader(conn)}
	if _, err := c.readLine(); err != nil { // greeting
		conn.Close()
		return nil, err
	}
	resp, err := c.Command("LOGIN", QuoteString(user), QuoteString(pass))
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.Status != "OK" {
		conn.Close()
		return nil, fmt.Errorf("imap login failed")
	}
	return c, nil
}

func (c *Client) Logout() {
	c.Command("LOGOUT")
	c.conn.Close()
}

func QuoteString(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func (c *Client) nextTag() string {
	c.tagN++
	return "a" + strconv.Itoa(c.tagN)
}

func (c *Client) readLine() (string, error) {
	line, err := c.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Command sends parts; the last part may be a Literal.
func (c *Client) Command(parts ...any) (*Response, error) {
	tag := c.nextTag()
	var b strings.Builder
	b.WriteString(tag)
	for _, p := range parts {
		b.WriteByte(' ')
		switch v := p.(type) {
		case Literal:
			fmt.Fprintf(&b, "{%d}", len(v))
			if _, err := c.conn.Write([]byte(b.String() + "\r\n")); err != nil {
				return nil, err
			}
			cont, err := c.readLine()
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(cont, "+") {
				return nil, fmt.Errorf("imap: expected continuation, got %q", cont)
			}
			if _, err := c.conn.Write(append([]byte(v), '\r', '\n')); err != nil {
				return nil, err
			}
			b.Reset()
			continue
		default:
			b.WriteString(fmt.Sprint(v))
		}
	}
	if _, err := c.conn.Write([]byte(b.String() + "\r\n")); err != nil {
		return nil, err
	}

	resp := &Response{}
	for {
		chunks, err := c.readResponseLine()
		if err != nil {
			return nil, err
		}
		full := ""
		for _, ch := range chunks {
			full += ch.String()
		}
		resp.Chunks = append(resp.Chunks, chunks...)
		if strings.HasPrefix(full, "* ") {
			resp.Lines = append(resp.Lines, strings.TrimPrefix(full, "* "))
			continue
		}
		if strings.HasPrefix(full, tag+" ") {
			fields := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(full, tag+" ")), " ", 2)
			resp.Status = fields[0]
			if len(fields) > 1 {
				resp.Text = fields[1]
			} else {
				resp.Text = ""
			}
			return resp, nil
		}
		// stray line (e.g. continuation of untagged data); keep as chunk only
	}
}

var trailingLiteralRe = regexp.MustCompile(`\{(\d+)\}$`)

// readResponseLine reads one logical line, returning literals as separate chunks.
func (c *Client) readResponseLine() ([]Chunk, error) {
	line, err := c.readLine()
	if err != nil {
		return nil, err
	}
	var chunks []Chunk
	for {
		m := trailingLiteralRe.FindStringSubmatch(line)
		if m == nil {
			chunks = append(chunks, Chunk{Text: line})
			return chunks, nil
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			chunks = append(chunks, Chunk{Text: line})
			return chunks, nil
		}
		head := line[:len(line)-len(m[0])]
		raw := make([]byte, n)
		if _, err := ioReadFull(c.br, raw); err != nil {
			return nil, err
		}
		rest, err := c.readLine()
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, Chunk{Text: head}, Chunk{Bytes: raw})
		line = rest
	}
}

func ioReadFull(br *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := br.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
