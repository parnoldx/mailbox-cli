package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
)

// systemdFD is the first file descriptor systemd passes to a service. There is
// only ever one here: the daemon listens on one socket. It is a variable so a
// test can hand in a socket of its own without dup2-ing over this process's
// third descriptor.
var systemdFD uintptr = 3

// Listen returns the socket to serve on.
//
// Under systemd the listener is inherited: the socket unit binds the path,
// which is what lets the first widget to connect start the daemon (ADR-0012).
// Passing --systemd-socket is an assertion, not a preference — a daemon that
// finds no inherited socket fails here rather than binding one of its own,
// because a unit that silently binds a second socket looks healthy and is
// talked to by nobody.
func Listen(socket string, systemd bool) (net.Listener, error) {
	if systemd {
		return inherited()
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	// The socket speaks for a logged-in mailbox, and who can open it is the
	// whole access control (ADR-0014's reasoning, one layer out).
	if err := os.Chmod(socket, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// inherited takes the listener systemd bound. LISTEN_PID guards against an
// environment carried into a child process that was never given the fd.
func inherited() (net.Listener, error) {
	if got := os.Getenv("LISTEN_PID"); got != "" && got != strconv.Itoa(os.Getpid()) {
		return nil, fmt.Errorf("--systemd-socket: LISTEN_PID is %s, not this process", got)
	}
	n, _ := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if n < 1 {
		return nil, errors.New("--systemd-socket: no socket was passed in; " +
			"run `mailbox daemon` for a daemon that binds its own, or start mailbox.socket")
	}
	f := os.NewFile(systemdFD, "mailbox.sock")
	if f == nil {
		return nil, errors.New("--systemd-socket: fd 3 is not open")
	}
	defer f.Close()
	ln, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("--systemd-socket: %w", err)
	}
	if _, ok := ln.(*net.UnixListener); !ok {
		return nil, fmt.Errorf("--systemd-socket: the passed socket is %T, not a unix socket", ln)
	}
	return ln, nil
}
