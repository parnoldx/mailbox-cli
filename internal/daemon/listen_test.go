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

// A socket anyone can open is a mailbox anyone can read. It is bound under a
// private name and only given the real one once it is already 0600, so there is
// no window to connect through — this pins the 0600 and the real path down, and
// Listen's own code is what keeps the window shut.
func TestTheSocketIsPrivateAndAtItsOwnPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	ln, err := Listen(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %04o, want 0600", got)
	}
	if got := ln.Addr().String(); got != path {
		t.Fatalf("Addr() = %q, want %q", got, path)
	}
	// The private bind name is not left behind beside the real one.
	if _, err := os.Stat(path + ".bind"); !os.IsNotExist(err) {
		t.Fatalf("the bind name survived: %v", err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("the socket is not reachable at its own path: %v", err)
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

// The socket-activated daemon is only ever found by path, so a second daemon
// that takes the path away leaves it listening on an unlinked socket: alive,
// logging that it listens, and talked to by nobody. Refuse instead.
func TestItWillNotTakeThePathFromALiveDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	live, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()

	if _, err := Listen(path, false); err == nil ||
		!strings.Contains(err.Error(), "already listening") {
		t.Fatalf("err = %v", err)
	}

	// A stale path with nobody behind it is still fair game.
	live.Close()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(path, false)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}
