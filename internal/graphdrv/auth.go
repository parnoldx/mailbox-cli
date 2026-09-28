package graphdrv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Scopes is everything the account is used for, asked for once at sign-in.
const Scopes = "offline_access User.Read Mail.ReadWrite Mail.Send Calendars.ReadWrite Contacts.ReadWrite"

// Login is where the device-code flow is spoken; a test points it at a fake.
var Login = "https://login.microsoftonline.com"

// ErrSignIn is a sign-in Microsoft no longer honours: revoked, expired after
// ninety idle days, or a password change. It does not resolve itself, so the
// words are the ones the Daemon's credentials problem looks for (noteAuth).
var ErrSignIn = errors.New("authentication failed: sign in again with `mailbox setup`")

// Token is what is kept on disk. The refresh token is the sign-in; the access
// token is an hour's worth of it.
type Token struct {
	Access  string    `json:"access_token"`
	Refresh string    `json:"refresh_token"`
	Expiry  time.Time `json:"expiry"`
}

// Auth is one account's sign-in: the Entra app it goes through and the file
// its token is kept in (ADR-0030).
type Auth struct {
	Tenant   string
	ClientID string
	Path     string
	HTTP     *http.Client

	mu  sync.Mutex
	tok *Token
}

// NewAuth reads nothing yet: a missing token file is found on first use.
func NewAuth(tenant, clientID, path string) *Auth {
	if tenant == "" {
		tenant = "organizations"
	}
	return &Auth{Tenant: tenant, ClientID: clientID, Path: path, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Token returns an access token good for at least another minute, refreshing
// and saving it when it is not.
func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok == nil {
		tok, err := a.load()
		if err != nil {
			return "", err
		}
		a.tok = tok
	}
	if a.tok.Access != "" && time.Until(a.tok.Expiry) > time.Minute {
		return a.tok.Access, nil
	}
	tok, err := a.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.tok.Refresh},
		"scope":         {Scopes},
	})
	if err != nil {
		return "", err
	}
	if tok.Refresh == "" {
		// Microsoft usually rotates it; when it does not, the old one stands.
		tok.Refresh = a.tok.Refresh
	}
	if err := a.save(tok); err != nil {
		return "", err
	}
	a.tok = tok
	return tok.Access, nil
}

func (a *Auth) load() (*Token, error) {
	data, err := os.ReadFile(a.Path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w (no sign-in at %s)", ErrSignIn, a.Path)
	}
	if err != nil {
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("read %s: %w", a.Path, err)
	}
	if tok.Refresh == "" {
		return nil, fmt.Errorf("%w (%s holds no refresh token)", ErrSignIn, a.Path)
	}
	return &tok, nil
}

// save writes the token by temp file and rename, created 0600 rather than
// chmodded after: a refresh is a write, and a crash halfway through one must
// leave the old sign-in, not half of a new one (ADR-0014, ADR-0030).
func (a *Auth) save(tok *Token) error {
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o700); err != nil {
		return err
	}
	// A temp file of its own, because `mailbox doctor` and the Daemon may both
	// refresh at once; CreateTemp makes it 0600.
	f, err := os.CreateTemp(filepath.Dir(a.Path), ".graph-token-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := json.NewEncoder(f).Encode(tok); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, a.Path)
}

// BrowserLogin signs in with a browser: authorization code with PKCE and the
// redirect landing on this machine (loopback, RFC 8252). The device-code flow
// it replaces is refused by the security defaults most tenants now run
// (Microsoft-managed "Block device code flow", AADSTS530035); a browser
// sign-in is exactly what those defaults are built to allow. A browser on any
// reachable machine will do — on the VPS, the URL is reached through an SSH
// tunnel and lands back on the tunnelled port.
func (a *Auth) BrowserLogin(ctx context.Context, out io.Writer) error {
	verifier, err := pkceVerifier()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(raw[:])

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer l.Close()
	redirect := fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)

	// http://localhost is registered without a port: for native clients Entra
	// matches the loopback regardless of port, so a free one will do.
	authorize := fmt.Sprintf("%s/%s/oauth2/v2.0/authorize?%s", Login, url.PathEscape(a.Tenant), url.Values{
		"client_id":             {a.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {Scopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}.Encode())
	fmt.Fprintln(out, "  Open this address in a browser and sign in:")
	fmt.Fprintln(out, "  "+authorize)
	_ = openBrowser(ctx, authorize)

	code := make(chan any, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			fmt.Fprint(w, "The sign-in was refused. The details are in the terminal.")
			code <- fmt.Errorf("the sign-in was refused: %s", e)
			return
		}
		if q.Get("state") != state {
			fmt.Fprint(w, "Unknown sign-in answer.")
			code <- errors.New("the sign-in answer carries the wrong state")
			return
		}
		fmt.Fprint(w, "Signed in. You can close this tab and return to the terminal.")
		code <- q.Get("code")
	})}
	go srv.Serve(l)
	defer srv.Close()

	var authCode string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case v := <-code:
		switch v := v.(type) {
		case error:
			return v
		case string:
			authCode = v
		}
	}
	if authCode == "" {
		return errors.New("the sign-in came back without a code")
	}
	tok, err := a.exchange(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.save(tok); err != nil {
		return err
	}
	a.tok = tok
	return nil
}

// pkceVerifier is a random code verifier: enough entropy for RFC 7636 and no
// characters that need escaping.
func pkceVerifier() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// openBrowser opens the authorize address in a browser on this machine, best
// effort: a desktop opens it itself; over SSH there is none and the printed
// line is the one the person follows, through a tunnel when needed.
var openBrowser = func(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "xdg-open", target).Start()
}

// exchange asks the token endpoint for a token. A refused refresh becomes
// ErrSignIn.
func (a *Auth) exchange(ctx context.Context, form url.Values) (*Token, error) {
	var body struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	err := a.post(ctx, "token", form, &body)
	var oe *oauthError
	if errors.As(err, &oe) {
		switch oe.Code {
		case "invalid_grant", "interaction_required", "consent_required":
			return nil, fmt.Errorf("%w: %s", ErrSignIn, oe.Description)
		}
	}
	if err != nil {
		return nil, err
	}
	return &Token{
		Access: body.Access, Refresh: body.Refresh,
		Expiry: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

type oauthError struct{ Code, Description string }

func (e *oauthError) Error() string { return e.Code + ": " + e.Description }

func (a *Auth) post(ctx context.Context, endpoint string, form url.Values, out any) error {
	form.Set("client_id", a.ClientID)
	u := fmt.Sprintf("%s/%s/oauth2/v2.0/%s", Login, url.PathEscape(a.Tenant), endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Code        string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(data, &e) == nil && e.Code != "" {
			// The sentence, without the trace and correlation ids after it, which
			// come on the next line from one endpoint and the same line from
			// another.
			desc, _, _ := strings.Cut(e.Description, "\r\n")
			desc, _, _ = strings.Cut(desc, " Trace ID:")
			return &oauthError{Code: e.Code, Description: desc}
		}
		return fmt.Errorf("sign-in %s: %d %s", endpoint, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, out)
}
