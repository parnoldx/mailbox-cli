package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/daemon"
	compose "mailbox/internal/message"
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
	// One mail in a box whose name has a space in it, so `box view Paper Trail`
	// unquoted and `box view trail` by its routing alias both have something to
	// find.
	pt, _, err := tx.UpsertMessage(mirror.Message{
		Key: "c@example.com", Subject: "Receipt", From: "shop@example.com",
		Date: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{
		Folder: "INBOX/Paper Trail", UID: 60, MessageID: pt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := m.PutRouting("primary", "logic", "# the script\n", true,
		[]mirror.Route{{Address: "anna@example.com", To: "inbox", Box: "INBOX"}}); err != nil {
		t.Fatal(err)
	}

	socket := filepath.Join(dir, "s.sock")
	d := daemon.New("primary", m, nil, []string{"INBOX", "INBOX/Screener", "INBOX/Paper Trail"}, nil,
		log.New(&bytes.Buffer{}, "", 0))
	d.From = compose.Address{Name: "Max Mustermann", Addr: "me@example.com"}
	ln, err := daemon.Listen(socket, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })

	// Listen has bound the path, so a client dialling it now queues in the
	// backlog until Serve accepts it.
	t.Setenv("MAILBOX_SOCKET", socket)
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

	// A box name with a space, given unquoted as two words, and the same box by
	// the short alias `mailbox route` uses — both resolve to INBOX/Paper Trail.
	for _, name := range [][]string{{"Paper", "Trail"}, {"trail"}} {
		args := append([]string{"box", "view"}, name...)
		out, errs, code := run(t, args...)
		if code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, errs)
		}
		if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
			t.Errorf("%v returned %d lines:\n%s", args, len(lines), out)
		}
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

// serveCapture stands in for the Daemon and records the first request, so a
// test can assert the flags turned into Args (they reach the wire as JSON).
func serveCapture(t *testing.T) *daemon.Request {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	// A command that never connects — a flag the parser refuses, say — must
	// not hang the test in its cleanup, waiting for a request that never
	// comes. Any real request arrives at once, so a short deadline is free.
	type deadliner interface{ SetDeadline(time.Time) error }
	if dl, ok := ln.(deadliner); ok {
		dl.SetDeadline(time.Now().Add(5 * time.Second))
	}
	t.Cleanup(func() { ln.Close() })
	t.Setenv("MAILBOX_SOCKET", socket)

	var got daemon.Request
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		_ = json.Unmarshal(sc.Bytes(), &got)
		_ = json.NewEncoder(conn).Encode(daemon.Response{ID: "1", OK: true, Data: map[string]any{}})
	}()
	t.Cleanup(func() { <-done })
	return &got
}

// --title on `habit edit` and --name on `contact update` are documented flags.
// They used to be declared but never put on the wire — an edit that silently
// changed nothing.
func TestChangeFlagsReachTheDaemon(t *testing.T) {
	for _, tt := range [][]string{
		{"habit", "edit", "Lesen", "--title", "Lesen abends"},
		{"contact", "update", "12", "--name", "Anna Beispiel"},
	} {
		got := serveCapture(t)
		if out, errs, code := run(t, tt...); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", tt, code, errs)
		} else if strings.TrimSpace(out) == "" {
			t.Errorf("%v printed nothing", tt)
		}
		want, key := tt[4], "title"
		if tt[0] == "contact" {
			key = "name"
		}
		if got.Args[key] != want {
			t.Errorf("%v: Args[%s] = %v, want %q", tt, key, got.Args[key], want)
		}
	}
}

// --account on the draft verbs says whose drafts box the command looks in. The
// daemon has always read it, but the CLI never put it on the wire, so a draft
// saved on a Secondary — `compose --account work --draft` — was invisible to
// `draft list` and could not be opened, edited or sent.
func TestDraftAccountReachesTheDaemon(t *testing.T) {
	serveSeeded(t)
	// A named account reaches the daemon and names a real one: the same box,
	// the same answer as the bare form. Before the flag existed the parser
	// refused it and nothing reached the daemon at all.
	bareOut, _, bareCode := run(t, "draft", "list")
	namedOut, _, namedCode := run(t, "draft", "list", "--account", "primary")
	if namedCode != bareCode || namedOut != bareOut {
		t.Fatalf("--account primary: exit %d, out %q — bare: exit %d, out %q",
			namedCode, namedOut, bareCode, bareOut)
	}
	if _, errs, code := run(t, "draft", "list", "--account", "gmx"); code == ExitOK || !strings.Contains(errs, "no account called") {
		t.Errorf("unknown account: exit %d, stderr %q", code, errs)
	}

	// And the flag rides the wire on every verb of the pile.
	for _, tt := range [][]string{
		{"draft", "list", "--account", "work"},
		{"draft", "show", "Drafts:6", "--account", "work"},
		{"draft", "send", "Drafts:6", "--account", "work"},
		{"draft", "delete", "Drafts:6", "--account", "work"},
		{"draft", "edit", "Drafts:6", "--account", "work", "--subject", "x"},
	} {
		got := serveCapture(t)
		if _, errs, code := run(t, tt...); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", tt, code, errs)
		}
		if got.Args["account"] != "work" {
			t.Errorf("%v: Args[account] = %v, want %q", tt, got.Args["account"], "work")
		}
	}
}

// --dry-run must stop at the preview: who would get the reply, printed as
// plain lines — never the "sent to" wording of a real send.
func TestReplyDryRunPrintsRecipientsWithoutSending(t *testing.T) {
	serveSeeded(t)
	out, errs, code := run(t, "reply", "Screener:40", "--bcc", "privat@example.com", "--dry-run", "--body", "checking first")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if strings.Contains(out, "sent to") {
		t.Errorf("dry-run used the sent wording:\n%s", out)
	}
	for _, want := range []string{"from me@example.com", "to news@example.com", "bcc privat@example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

// --occurrence has to survive the CLI: dropped here it would be parsed,
// ignored, and the edit would land on every instance of the rule instead of
// the one day the caller named.
func TestEventOccurrenceReachesTheDaemon(t *testing.T) {
	got := serveCapture(t)
	out, errs, code := run(t, "event", "edit", "41", "--occurrence", "2026-10-07", "--title", "Verschoben")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	} else if strings.TrimSpace(out) == "" {
		t.Errorf("printed nothing")
	}
	if got.Args["occurrence"] != "2026-10-07" {
		t.Errorf("Args[occurrence] = %v, want 2026-10-07", got.Args["occurrence"])
	}
	if got.Args["title"] != "Verschoben" {
		t.Errorf("Args[title] = %v, want Verschoben", got.Args["title"])
	}
}
