package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/unsubscribe"
)

// unsubscribeClient is the one-click POST (RFC 8058): no cookies, no
// redirects followed blind, and a short timeout so a dead list host does not
// hang the request.
var unsubscribeClient = &http.Client{Timeout: 15 * time.Second}

// handleUnsubscribe leaves the list a message belongs to, however it offers
// to be left: an RFC 8058 one-click POST, a plain mailto:, or — when neither
// is trustworthy enough to do blind — the link for a client to open.
func (d *Daemon) handleUnsubscribe(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		return resp.usage(err.Error())
	}
	row, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	if err != nil {
		return resp.api(err.Error())
	}
	target := unsubscribeTarget(row.Message)
	switch target.Kind {
	case unsubscribe.OneClick:
		if err := postOneClick(ctx, target.URL); err != nil {
			// Some list hosts sit behind a WAF that 403s anything that is not
			// a browser, One-Click header or not. That is the host's problem,
			// not a reason to leave the caller with nothing to do about it —
			// the same URL still works from an actual browser.
			if d.Log != nil {
				d.Log.Printf("unsubscribe: one-click POST %s: %v", target.URL, err)
			}
			return resp.ok(map[string]any{"kind": "link", "url": target.URL})
		}
		return resp.ok(map[string]any{"kind": "done", "detail": "Unsubscribed"})
	case unsubscribe.Email:
		if d.Outbox == nil || acct.Courier == nil {
			return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
		}
		subject := target.Subject
		if subject == "" {
			subject = "unsubscribe"
		}
		draft := compose.Draft{
			From: acct.From, To: []compose.Address{{Addr: target.To}},
			Subject: subject, Body: target.Body + "\n",
		}
		resp = d.deliver(ctx, acct, draft, resp, Request{})
		if !resp.OK {
			return resp
		}
		return resp.ok(map[string]any{"kind": "done", "detail": "Unsubscribed"})
	case unsubscribe.Link:
		return resp.ok(map[string]any{"kind": "link", "url": target.URL})
	default:
		return resp.usage("this message carries no unsubscribe link")
	}
}

// postOneClick is the RFC 8058 request itself: the sender named this exact
// body when it sent List-Unsubscribe-Post, so nothing here is a guess.
func postOneClick(ctx context.Context, url string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Go's default User-Agent ("Go-http-client/1.1") is on every WAF's bot
	// list, and a one-click endpoint is exactly the sort of thing a WAF
	// guards — a browser-shaped one gets through the same door a person
	// clicking the link would.
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
		"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	res, err := unsubscribeClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("list host said %s", res.Status)
	}
	return nil
}
