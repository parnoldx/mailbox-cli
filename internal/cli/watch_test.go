package cli

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailbox/internal/daemon"
)

// serveWatch stands in for the Daemon on a temporary socket: it answers the
// subscription, writes the lines a test wants watched, and hangs up. It records
// the request it was sent, because what the flags turn into is half of what this
// command does.
func serveWatch(t *testing.T, lines ...daemon.Change) *daemon.Request {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
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
		enc := json.NewEncoder(conn)
		_ = enc.Encode(daemon.Response{ID: "1", OK: true})
		for _, c := range lines {
			_ = enc.Encode(c)
		}
	}()
	t.Cleanup(func() { <-done })
	return &got
}

// The changes come out as one JSON object per line, in the order they arrived,
// and --timeout ends the watch rather than leaving it dialling.
func TestWatchPrintsOneObjectPerLine(t *testing.T) {
	serveWatch(t,
		daemon.Change{Event: "ready", At: "2026-09-06T09:00:00Z"},
		daemon.Change{Event: "added", Box: "INBOX", Thread: 12, Subject: "Angebot", New: true},
		daemon.Change{Event: "updated", Box: "INBOX", Thread: 12, Subject: "Angebot"},
	)
	out, errs, code := run(t, "watch", "--timeout", "300ms")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// The three that were sent, then the disconnect when the fake hung up.
	if len(lines) != 4 {
		t.Fatalf("lines = %q", lines)
	}
	for i, want := range []string{"ready", "added", "updated", "disconnected"} {
		var c daemon.Change
		if err := json.Unmarshal([]byte(lines[i]), &c); err != nil {
			t.Fatalf("line %d is not one object: %q", i, lines[i])
		}
		if c.Event != want {
			t.Fatalf("line %d = %s, want %s", i, c.Event, want)
		}
	}
}

// --run-sync hands the change to a command instead of printing it, and the two
// lines about the watch itself are printed and never run. --exit-on-first counts
// the change and not the "ready" before it.
func TestWatchRunsACommandPerChange(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "seen")
	got := serveWatch(t,
		daemon.Change{Event: "ready"},
		daemon.Change{Event: "added", Box: "INBOX", Thread: 12, Subject: "Angebot", New: true},
	)
	out, errs, code := run(t, "watch", "--box", "inbox", "--events", "new",
		"--exit-on-first", "--run-sync", "printf '%s %s %s' \"$MAILBOX_EVENT\" \"$MAILBOX_SUBJECT\" \"$MAILBOX_NEW\" > "+seen)
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	body, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the command did not run: %v", err)
	}
	if string(body) != "added Angebot 1" {
		t.Fatalf("the command saw %q", body)
	}
	// Printed: the ready line. Not printed: the change the command handled.
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, `"ready"`) {
		t.Fatalf("stdout = %q", out)
	}
	// The flags are the daemon's to apply, so they have to reach it.
	if got.Cmd[0] != "watch" {
		t.Fatalf("cmd = %v", got.Cmd)
	}
	if boxes, _ := got.Args["box"].([]any); len(boxes) != 1 || boxes[0] != "inbox" {
		t.Fatalf("box = %v", got.Args["box"])
	}
	if events, _ := got.Args["events"].([]any); len(events) != 1 || events[0] != "new" {
		t.Fatalf("events = %v", got.Args["events"])
	}
}

// A daemon that was never there is a setup problem and says so; one that goes
// away under a running watch is a restart, and the watch waits for it.
func TestWatchWithNoDaemonSaysSo(t *testing.T) {
	t.Setenv("MAILBOX_SOCKET", filepath.Join(t.TempDir(), "nothing.sock"))
	out, errs, code := run(t, "watch")
	if code != ExitDaemon {
		t.Fatalf("exit %d (%q)", code, out)
	}
	if !strings.Contains(errs, "no daemon listening") {
		t.Fatalf("stderr = %q", errs)
	}
}
