// Package dav: minimal DAV (CalDAV/CardDAV) HTTP client with basic auth.
package dav

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
)

type Client struct {
	User string
	Pass string
	HTTP *http.Client
}

func New(user, pass string) *Client {
	return &Client{User: user, Pass: pass, HTTP: &http.Client{}}
}

func (c *Client) do(method, url string, body string, depth string, headers map[string]string) ([]byte, int, error) {
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.User, c.Pass)
	if body != "" {
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) Report(url, query string, depth string) ([]byte, int, error) {
	return c.do("REPORT", url, query, depth, nil)
}

func (c *Client) Put(url, body string, headers map[string]string) (int, error) {
	_, status, err := c.do("PUT", url, body, "", headers)
	return status, err
}

// Response is one <d:response> of a multistatus with the payload tag's text.
type Response struct {
	Href string
	Etag string
	Data string
}

func LocalName(t xml.Token) string {
	switch tt := t.(type) {
	case xml.StartElement:
		return tt.Name.Local
	case xml.EndElement:
		return tt.Name.Local
	}
	return ""
}

// ParseMultistatus walks any DAV multistatus regardless of namespace prefixes.
func ParseMultistatus(raw []byte, dataTags ...string) []Response {
	wanted := map[string]bool{}
	for _, t := range dataTags {
		wanted[t] = true
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out []Response
	cur := Response{}
	capture := ""
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch tt := tok.(type) {
		case xml.StartElement:
			name := LocalName(tok)
			if name == "response" {
				cur = Response{}
			} else if wanted[name] || name == "href" || name == "getetag" {
				capture = name
				depth = 0
			} else if capture != "" {
				depth++
			}
		case xml.CharData:
			switch capture {
			case "":
			case "href":
				cur.Href += string(tt)
				capture = ""
			case "getetag":
				cur.Etag += string(tt)
				capture = ""
			default:
				if wanted[capture] {
					cur.Data += string(tt)
				}
			}
		case xml.EndElement:
			name := LocalName(tok)
			if name == "response" {
				if cur.Data != "" {
					out = append(out, cur)
				}
				cur = Response{}
			}
			if capture == name {
				capture = ""
			} else if depth > 0 && capture != "" {
				depth--
			}
		}
	}
	return out
}

func (c *Client) Get(url string) (string, int, error) {
	data, status, err := c.do("GET", url, "", "", nil)
	return string(data), status, err
}
