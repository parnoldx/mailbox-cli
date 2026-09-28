package graphdrv

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLogin is the Microsoft token endpoint: an authorization-code exchange
// that checks the PKCE pair, and refreshes that rotate the refresh token
// until refused is set.
type fakeLogin struct {
	refused   bool
	grants    []string
	challenge string
}

func (f *fakeLogin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Form.Get("grant_type") == "authorization_code":
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "good" ||
			r.Form.Get("redirect_uri") == "" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			write(400, map[string]string{"error": "invalid_grant",
				"error_description": "AADSTS65001: the code or the pair is wrong"})
			return
		}
		write(200, map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600})
	case r.Form.Get("grant_type") == "refresh_token":
		f.grants = append(f.grants, r.Form.Get("refresh_token"))
		if f.refused {
			write(400, map[string]string{"error": "invalid_grant",
				"error_description": "AADSTS700082: The refresh token has expired due to inactivity.\r\nTrace ID: 0"})
			return
		}
		write(200, map[string]any{"access_token": "at-2", "refresh_token": "rt-2", "expires_in": 3600})
	default:
		write(400, map[string]string{"error": "unsupported"})
	}
}

// signedInURL takes the authorize line BrowserLogin printed.
func signedInURL(t *testing.T, out string) *url.URL {
	t.Helper()
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "oauth2/v2.0/authorize") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no sign-in address was shown: %q", out)
	}
	u, err := url.Parse(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// signIn runs BrowserLogin the way a browser would: it waits for the authorize
// address to appear, then answers the loopback redirect with what answer makes
// of it. It returns the authorize address and how the sign-in ended.
func signIn(t *testing.T, a *Auth, out *strings.Builder, answer func(*url.URL) string) (*url.URL, error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- a.BrowserLogin(context.Background(), out) }()
	var authorize *url.URL
	deadline := time.Now().Add(5 * time.Second)
	for authorize == nil && time.Now().Before(deadline) {
		if strings.Contains(out.String(), "authorize?") {
			authorize = signedInURL(t, out.String())
			break
		}
		time.Sleep(time.Millisecond)
	}
	if authorize == nil {
		t.Fatalf("no sign-in address was printed: %q", out.String())
	}
	res, err := http.Get(authorize.Query().Get("redirect_uri") + "/?" + answer(authorize))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return authorize, <-done
}

func TestBrowserSignInAndRefreshAndARefusedSignIn(t *testing.T) {
	login := &fakeLogin{}
	srv := httptest.NewServer(login)
	defer srv.Close()
	old := Login
	Login = srv.URL
	defer func() { Login = old }()
	oldOpen := openBrowser
	openBrowser = func(context.Context, string) error { return nil }
	defer func() { openBrowser = oldOpen }()

	path := filepath.Join(t.TempDir(), "graph-work.token.json")
	a := NewAuth("example.com", "00000000-1111-2222-3333-444444444444", path)
	ctx := context.Background()
	if _, err := a.Token(ctx); !errors.Is(err, ErrSignIn) {
		t.Fatalf("no sign-in yet: %v", err)
	}
	var out strings.Builder
	authorize, err := signIn(t, a, &out, func(u *url.URL) string {
		login.challenge = u.Query().Get("code_challenge")
		return "code=good&state=" + u.Query().Get("state")
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorize.Query().Get("scope") != Scopes {
		t.Fatalf("the scopes were not asked for: %v", authorize.Query().Get("scope"))
	}
	if authorize.Query().Get("response_type") != "code" || authorize.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("not a PKCE sign-in: %v", authorize.RawQuery)
	}
	login.challenge = authorize.Query().Get("code_challenge")

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file %v, %v", info, err)
	}
	if tok, err := a.Token(ctx); err != nil || tok != "at-1" {
		t.Fatalf("token %q, %v", tok, err)
	}

	// An expired access token is refreshed, and the rotated refresh token is
	// what a new process reads.
	a.tok.Expiry = time.Now()
	if tok, err := a.Token(ctx); err != nil || tok != "at-2" {
		t.Fatalf("refreshed %q, %v", tok, err)
	}
	b := NewAuth("example.com", "00000000-1111-2222-3333-444444444444", path)
	b.HTTP = srv.Client()
	if tok, err := b.load(); err != nil || tok.Refresh != "rt-2" {
		t.Fatalf("on disk %+v, %v", tok, err)
	}

	// A refused refresh is the sign-in problem, with the first line of why.
	login.refused = true
	a.tok.Expiry = time.Now()
	_, err = a.Token(ctx)
	if !errors.Is(err, ErrSignIn) || !strings.Contains(err.Error(), "AADSTS700082") || strings.Contains(err.Error(), "Trace") {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "authentication failed") {
		t.Fatalf("the daemon would not see this as a credentials problem: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestBrowserSignInChecksTheState(t *testing.T) {
	login := &fakeLogin{}
	srv := httptest.NewServer(login)
	defer srv.Close()
	old := Login
	Login = srv.URL
	defer func() { Login = old }()
	oldOpen := openBrowser
	openBrowser = func(context.Context, string) error { return nil }
	defer func() { openBrowser = oldOpen }()

	a := NewAuth("example.com", "00000000-1111-2222-3333-444444444444",
		filepath.Join(t.TempDir(), "graph-work.token.json"))
	var out strings.Builder
	if _, err := signIn(t, a, &out, func(*url.URL) string {
		return "code=good&state=wrong"
	}); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("a wrong state went through: %v", err)
	}
}
