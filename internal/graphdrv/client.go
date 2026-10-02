// Package graphdrv speaks Microsoft Graph for a Microsoft 365 account: mail,
// calendars and contacts over one sign-in (ADR-0029). It is written against
// net/http rather than the Microsoft SDK, which is enormous for the dozen
// endpoints used here.
//
// Nothing above this package knows it exists. Mail is served through the
// mailsync.Driver interface the IMAP driver implements, with a local uid map
// standing in for UIDs and a local counter for CONDSTORE's modseq; calendars and
// contacts are served through davsync's, as iCalendar and vCard.
package graphdrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Base is Graph's v1.0 root. Every collection URL and href this package hands
// out is under it, which is how davdrv.Set routes one to here.
const Base = "https://graph.microsoft.com/v1.0"

// Client is one signed-in account's connection to Graph.
type Client struct {
	// Base is Graph's root; a test points it at a fake.
	Base string
	HTTP *http.Client
	// Token returns a current access token, refreshing it when it has to.
	Token func(ctx context.Context) (string, error)
	// ForceRefresh makes the next Token call refresh even if the local clock
	// still calls the current token fresh. Nil where there is nothing to force.
	ForceRefresh func()
}

// NewClient talks to the real Graph as whoever auth signed in.
func NewClient(auth *Auth) *Client {
	return &Client{Base: Base, HTTP: &http.Client{Timeout: 60 * time.Second}, Token: auth.Token, ForceRefresh: auth.Expire}
}

// APIError is Graph refusing a request, with its code — "ErrorItemNotFound",
// "syncStateNotFound" — which is what a caller decides on.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("graph %d %s: %s", e.Status, e.Code, e.Message)
}

// notFound says a thing is gone, which several callers treat as an answer
// rather than a failure.
func notFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// expired says a delta link is no longer honoured: start again from nothing.
func expired(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	code := strings.ToLower(ae.Code)
	return ae.Status == http.StatusGone || strings.Contains(code, "syncstate") ||
		strings.Contains(code, "resyncrequired")
}

// request is one call. Path is either under Base ("/me/...") or absolute, as
// the nextLink and deltaLink Graph hands back are.
type request struct {
	method      string
	path        string
	body        []byte
	contentType string
	prefer      []string
}

// do sends a request and decodes a JSON answer into out, which may be nil. A
// 429 or 503 is waited out as long as Retry-After says, up to three times:
// throttling means the request was not carried out, so sending it again is not
// sending it twice.
func (c *Client) do(ctx context.Context, r request, out any) error {
	raw, err := c.raw(ctx, r)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("graph %s %s: %w", r.method, r.path, err)
	}
	return nil
}

// raw is do without the decoding, for the MIME of a message.
func (c *Client) raw(ctx context.Context, r request) ([]byte, error) {
	url := r.path
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = c.Base + r.path
	}
	for attempt := 0; ; attempt++ {
		token, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, url, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if r.body != nil {
			ct := r.contentType
			if ct == "" {
				ct = "application/json"
			}
			req.Header.Set("Content-Type", ct)
		}
		// Immutable ids, so a message keeps its id when it is moved and the uid
		// map can follow it rather than see a delete and an unrelated add.
		prefer := append([]string{`IdType="ImmutableId"`}, r.prefer...)
		req.Header.Set("Prefer", strings.Join(prefer, ", "))
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			if attempt < 3 {
				wait := retryAfter(resp.Header.Get("Retry-After"))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			// Graph refused the token. The usual cause is a lying clock: between a
			// resume and the first NTP sync the local time is off, so the expiry
			// check keeps a token that Graph already calls stale. Force one refresh
			// and retry; a genuinely dead sign-in comes back as invalid_grant and
			// surfaces as ErrSignIn as before.
			if c.ForceRefresh != nil {
				c.ForceRefresh()
			}
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, apiError(resp.StatusCode, data)
		}
		return data, nil
	}
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 5 * time.Second
}

func apiError(status int, data []byte) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &body)
	e := &APIError{Status: status, Code: body.Error.Code, Message: body.Error.Message}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(data))
		if len(e.Message) > 200 {
			e.Message = e.Message[:200]
		}
	}
	return e
}

// page is one page of a Graph collection, delta or not.
type page[T any] struct {
	Value     []T    `json:"value"`
	NextLink  string `json:"@odata.nextLink"`
	DeltaLink string `json:"@odata.deltaLink"`
}

// all follows nextLink to the end of a collection and returns every item, and
// the deltaLink when the collection was a delta.
func all[T any](ctx context.Context, c *Client, path string, prefer ...string) ([]T, string, error) {
	var out []T
	for path != "" {
		var p page[T]
		if err := c.do(ctx, request{method: http.MethodGet, path: path, prefer: prefer}, &p); err != nil {
			return nil, "", err
		}
		out = append(out, p.Value...)
		if p.DeltaLink != "" {
			return out, p.DeltaLink, nil
		}
		path = p.NextLink
	}
	return out, "", nil
}

// jsonBody marshals a request body; the values here are maps and structs of
// strings, which cannot fail to marshal.
func jsonBody(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Me is the address the sign-in belongs to, which is how a sign-in made with
// the wrong account is caught before it is used.
func (c *Client) Me(ctx context.Context) (string, error) {
	var me struct {
		Mail string `json:"mail"`
		UPN  string `json:"userPrincipalName"`
	}
	if err := c.do(ctx, request{method: http.MethodGet, path: "/me?$select=mail,userPrincipalName"}, &me); err != nil {
		return "", err
	}
	if me.Mail != "" {
		return me.Mail, nil
	}
	return me.UPN, nil
}
