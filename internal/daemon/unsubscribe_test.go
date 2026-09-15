package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// seedList mirrors one message that offers a way out of its list, and returns
// the id it is reachable under.
func seedList(t *testing.T, d *Daemon, listUnsubscribe, listUnsubscribePost, html string) string {
	t.Helper()
	tx, err := d.Mirror.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, _, err := tx.UpsertMessage(mirror.Message{
		Key: listUnsubscribe + listUnsubscribePost + html, Date: time.Now(),
		Subject: "Newsletter #7", From: "news@lists.example",
		ListUnsubscribe: listUnsubscribe, ListUnsubscribePost: listUnsubscribePost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(id, "", html, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX", UID: 700, MessageID: id}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return "700"
}

// unsubServer stands in for a list host. One-click is HTTPS-only, so this is a
// TLS server the client is taught to trust for the length of the test.
func unsubServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	old := unsubscribeClient.Transport
	unsubscribeClient.Transport = srv.Client().Transport
	t.Cleanup(func() { unsubscribeClient.Transport = old })
	return srv
}

func unsub(t *testing.T, d *Daemon, id string) map[string]any {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"unsubscribe"},
		Args: map[string]any{"positional": id}})
	if !resp.OK {
		t.Fatalf("unsubscribe %s: %s (%s)", id, resp.Error, resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unsubscribe %s returned %T", id, resp.Data)
	}
	return data
}

// RFC 8058: the sender declared one-click, so the daemon does the whole thing
// and says it is done — no browser, no page visit.
func TestUnsubscribeOneClickPOSTs(t *testing.T) {
	d := seed(t)
	var method, body string
	srv := unsubServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, body = r.Method, string(b)
	})
	id := seedList(t, d, "<"+srv.URL+"/u/1>", "List-Unsubscribe=One-Click", "")

	if got := unsub(t, d, id)["kind"]; got != "done" {
		t.Fatalf("kind = %v, want done", got)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if body != "List-Unsubscribe=One-Click" {
		t.Errorf("body = %q, want the RFC 8058 one", body)
	}
}

// A redirect is not an unsubscribe. Followed, it becomes a GET with the body
// dropped and the landing page's 200 reads as success — so it has to come back
// as a link for a browser instead, and the target must never be reached.
func TestUnsubscribeRedirectFallsBackToTheLink(t *testing.T) {
	d := seed(t)
	followed := false
	srv := unsubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/u/1" {
			followed = true
			return
		}
		http.Redirect(w, r, "/landing", http.StatusFound)
	})
	url := srv.URL + "/u/1"
	got := unsub(t, d, seedList(t, d, "<"+url+">", "List-Unsubscribe=One-Click", ""))

	if got["kind"] != "link" {
		t.Fatalf("kind = %v, want link", got["kind"])
	}
	if got["url"] != url {
		t.Errorf("url = %v, want %s", got["url"], url)
	}
	if followed {
		t.Error("the client followed the redirect")
	}
}

// A host that 403s anything not shaped like a browser is the host's problem,
// but the caller still gets the URL rather than a dead end.
func TestUnsubscribeRefusedHostFallsBackToTheLink(t *testing.T) {
	d := seed(t)
	srv := unsubServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no bots", http.StatusForbidden)
	})
	url := srv.URL + "/u/1"
	got := unsub(t, d, seedList(t, d, "<"+url+">", "List-Unsubscribe=One-Click", ""))

	if got["kind"] != "link" || got["url"] != url {
		t.Fatalf("got %v, want the link back", got)
	}
}

// A mailto: is the sender's own instruction too: the mail goes out on the
// account the message belongs to, with the subject and body the URI named.
func TestUnsubscribeByMailSendsIt(t *testing.T) {
	d, _ := seedSend(t)
	id := seedList(t, d, "<mailto:leave@lists.example?subject=unsubscribe&body=please%20remove%20me>", "", "")

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"unsubscribe"},
		Args: map[string]any{"positional": id}})
	if !resp.OK {
		t.Fatalf("unsubscribe: %s (%s)", resp.Error, resp.Code)
	}
	if got := resp.Data.(map[string]any)["kind"]; got != "done" {
		t.Fatalf("kind = %v, want done", got)
	}
	var copyOf *mailsync.FakeMsg
	for _, m := range fakeOf(d).Folder("INBOX/Sent").Msgs {
		if strings.Contains(m.To, "leave@lists.example") {
			copyOf = m
		}
	}
	if copyOf == nil {
		t.Fatal("no copy of the unsubscribe mail was filed in Sent")
	}
	if copyOf.Subject != "unsubscribe" {
		t.Errorf("subject = %q", copyOf.Subject)
	}
	if !strings.Contains(copyOf.Plain, "please remove me") {
		t.Errorf("body = %q", copyOf.Plain)
	}
}

// No header and no body link is nothing to offer: a usage error, not a silent
// no-op and not a guessed URL.
func TestUnsubscribeWithoutAnOfferSaysSo(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"unsubscribe"},
		Args: map[string]any{"positional": "7"}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v, want a usage error", resp)
	}
}
