package cli

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/daemon"
	"mailbox/internal/mirror"
)

// serveSeeded starts a real Daemon on a temporary socket with a Mirror holding
// a Screener and a Routing, and points the CLI at it. Everything between the
// two is the wire: the reply is JSON by the time a printer sees it, and a
// printer that expected a Go type rather than what JSON turns it into fails
// here and nowhere else.
func serveSeeded(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	m, err := mirror.Open(filepath.Join(dir, "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i, mail := range []struct{ key, subject, from string }{
		{"a@example.com", "Newsletter", "Beispiel News <news@example.com>"},
		{"b@example.com", "Ihre Rechnung", "bills@example.com"},
	} {
		id, _, err := tx.UpsertMessage(mirror.Message{
			Key: mail.key, Subject: mail.subject, From: mail.from,
			Date: time.Date(2026, 8, 29, 9+i, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutPlacement(mirror.Placement{
			Folder: "INBOX/Screener", UID: uint32(40 + i), MessageID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := m.PutRouting("primary", "logic", "# the script\n", true,
		[]mirror.Route{{Address: "anna@example.com", To: "inbox", Box: "INBOX"}}); err != nil {
		t.Fatal(err)
	}

	socket := filepath.Join(dir, "s.sock")
	d := daemon.New("primary", m, nil, []string{"INBOX", "INBOX/Screener"}, nil,
		log.New(&bytes.Buffer{}, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, socket) }()
	t.Cleanup(func() { cancel(); <-done })

	t.Setenv("MAILBOX_SOCKET", socket)
	// Run blocks on the socket existing, and Run above creates it before it
	// serves anything.
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socket); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the daemon never listened")
}

func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(args, &out, &errs)
	return out.String(), errs.String(), code
}

func TestScreenerPrintsOneLinePerSender(t *testing.T) {
	serveSeeded(t)
	out, errs, code := run(t, "screener")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines:\n%s", len(lines), out)
	}
	// Newest first, and each line carries the id that reads it.
	if !strings.Contains(lines[0], "bills@example.com") ||
		!strings.Contains(lines[0], "Screener:41") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "news@example.com") {
		t.Errorf("second line = %q", lines[1])
	}
}

// The Routing comes back as an object rather than a list, which is the one
// reply shape in this CLI that is not a list of rows.
func TestRoutePrintsTheRouting(t *testing.T) {
	serveSeeded(t)
	out, errs, code := run(t, "route", "list", "--script")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "anna@example.com") || !strings.Contains(out, "inbox") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(out, "# the script") {
		t.Errorf("--script did not print the script:\n%s", out)
	}
	if strings.Contains(errs, "not the active one") {
		t.Errorf("an active script was reported as inactive: %s", errs)
	}
}

// A decision needs a destination. Refusing here rather than at the daemon keeps
// `mailbox route set bob@example.com` from reading as "tell me about bob".
func TestRouteWithoutADestinationIsUsage(t *testing.T) {
	serveSeeded(t)
	_, errs, code := run(t, "route", "set", "bob@example.com")
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs, "--to") {
		t.Errorf("the usage does not mention --to: %s", errs)
	}
}

// Every command now gets its words and its flags from the registry rather than
// building a FlagSet of its own, so this is the path that carries a positional
// and a typed flag all the way to the daemon.
func TestWordsAndFlagsReachTheDaemon(t *testing.T) {
	serveSeeded(t)

	// The box comes off the line as a word, --limit as an int, and the daemon
	// honours both: two messages are in the screener and one is asked for.
	out, errs, code := run(t, "box", "view", "INBOX/Screener", "--limit", "1")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
		t.Errorf("--limit 1 returned %d lines:\n%s", len(lines), out)
	}

	// With no box named it is the inbox, which the seeded mirror leaves empty.
	if out, _, code := run(t, "box", "view"); code != ExitOK || strings.TrimSpace(out) != "" {
		t.Errorf("bare box view: exit %d, out %q", code, out)
	}

	// A declared default arrives when the flag is not given: --limit defaults
	// to 50, so both messages come back.
	out, _, code = run(t, "box", "view", "INBOX/Screener")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 2 {
		t.Errorf("the default limit returned %d lines:\n%s", len(lines), out)
	}

	// --json is global now, and still reaches the printer as the envelope.
	out, _, code = run(t, "box", "view", "--json", "INBOX/Screener")
	if code != ExitOK || !strings.Contains(out, `"ok": true`) {
		t.Errorf("global --json: exit %d, out %q", code, out)
	}
}

// The notice a Behind Mirror prints has to be something a caller can act on:
// how old the answer is, and whether asking again in a moment will help.
func TestBehindNoticeCarriesTheAgeAndTheCycle(t *testing.T) {
	at := time.Now().Add(-41 * time.Minute)
	cases := []struct {
		name  string
		state *daemon.MirrorState
		want  string
	}{
		{"current says nothing", &daemon.MirrorState{Connected: true}, ""},
		{"no state says nothing", nil, ""},
		{
			"behind with an age",
			&daemon.MirrorState{SyncedAt: &at},
			"last reached 41 minutes ago",
		},
		{
			"behind with a cycle running",
			&daemon.MirrorState{SyncedAt: &at, Syncing: true},
			"41 minutes ago; a sync is running",
		},
		{
			"never reached",
			&daemon.MirrorState{},
			"has not reached the server yet",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errs bytes.Buffer
			behindNotice(&errs, daemon.Response{Mirror: tc.state})
			got := errs.String()
			if tc.want == "" {
				if got != "" {
					t.Fatalf("said %q about a Mirror that is not Behind", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("notice = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestSinceReadsLikeSomethingSaidOutLoud(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{20 * time.Second, "20 seconds"},
		{time.Minute + 30*time.Second, "1 minute"},
		{41*time.Minute + 18*time.Second, "41 minutes"},
		{3 * time.Hour, "3 hours"},
		{50 * time.Hour, "2 days"},
	} {
		if got := since(time.Now().Add(-tc.d)); got != tc.want {
			t.Errorf("since(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
