package graphdrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Graph that refuses the first call with the "invalid lifetime" answer a
// lying clock produces — the token on disk looks fresh locally, Graph has
// already expired it — and answers the retry. The client is to force a refresh
// between the two, and the forced refresh is to use the refresh token on disk.
func TestClientRefreshesAfterARefusedToken(t *testing.T) {
	login := &fakeLogin{}
	loginSrv := httptest.NewServer(login)
	defer loginSrv.Close()
	old := Login
	Login = loginSrv.URL
	defer func() { Login = old }()

	// A sign-in on disk whose access token the local clock calls good.
	path := filepath.Join(t.TempDir(), "graph-work.token.json")
	tok := Token{Access: "at-1", Refresh: "rt-1", Expiry: time.Now().Add(time.Hour)}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(&tok); err != nil {
		t.Fatal(err)
	}
	f.Close()
	a := NewAuth("example.com", "00000000-1111-2222-3333-444444444444", path)
	a.HTTP = loginSrv.Client()
	if _, err := a.Token(context.Background()); err != nil || a.tok.Access != "at-1" {
		t.Fatalf("loaded token %q, %v", a.tok.Access, err)
	}

	calls := 0
	saw := []string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		saw = append(saw, r.Header.Get("Authorization"))
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"code":    "InvalidAuthenticationToken",
					"message": "Invalid token lifetime.",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"mail": "me@example.com"})
	}))
	defer api.Close()

	c := NewClient(a)
	c.Base = api.URL
	c.HTTP = api.Client()
	var me struct{ Mail string }
	if err := c.do(context.Background(), request{method: http.MethodGet, path: "/me"}, &me); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if calls != 2 {
		t.Fatalf("%d calls, want a refused one and its retry", calls)
	}
	if saw[0] != "Bearer at-1" || saw[1] != "Bearer at-2" {
		t.Fatalf("tokens sent: %v, want at-1 then the refreshed at-2", saw)
	}
	if len(login.grants) != 1 || login.grants[0] != "rt-1" {
		t.Fatalf("refresh grants: %v, want one with the refresh token from disk", login.grants)
	}
}
