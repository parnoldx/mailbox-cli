package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Socket activation is what makes the daemon_required error a setup problem
// rather than an everyday one (ADR-0012): the socket unit binds the path, and
// the first client to connect starts the daemon on the socket it was given.
func TestTheDaemonTakesTheSocketSystemdGivesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	bound, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	file, err := bound.(*net.UnixListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	old := systemdFD
	systemdFD = file.Fd()
	defer func() { systemdFD = old }()
	t.Setenv("LISTEN_FDS", "1")
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))

	ln, err := Listen("/nowhere/at/all.sock", true)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// The path it was told about is not the one it listens on: the inherited
	// socket is, which is the whole point.
	if got := ln.Addr().String(); got != path {
		t.Fatalf("listening on %q, want the inherited %q", got, path)
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestWithoutASocketPassedInItFailsRatherThanBindingOne(t *testing.T) {
	t.Setenv("LISTEN_FDS", "")
	t.Setenv("LISTEN_PID", "")
	// The flag is an assertion. A daemon that quietly binds a second socket
	// looks healthy and is talked to by nobody.
	_, err := Listen(filepath.Join(t.TempDir(), "mailbox.sock"), true)
	if err == nil {
		t.Fatal("--systemd-socket bound its own socket")
	}
	if !strings.Contains(err.Error(), "no socket was passed in") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnEnvironmentFromAnotherProcessIsNotOurs(t *testing.T) {
	t.Setenv("LISTEN_FDS", "1")
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()+1))
	if _, err := Listen("x.sock", true); err == nil ||
		!strings.Contains(err.Error(), "LISTEN_PID") {
		t.Fatalf("err = %v", err)
	}
}
