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
	if socket == "" {
		return nil, errors.New("no socket path: set XDG_RUNTIME_DIR or MAILBOX_SOCKET")
	}
	// Someone answering on this path is a live daemon — most likely the
	// socket-activated one. Removing the path below would unlink it out from
	// under that daemon, which keeps its listening socket and its systemd
	// unit: it looks healthy, logs that it is listening, and is reachable by
	// nobody, because clients only ever find it by path.
	if conn, err := net.Dial("unix", socket); err == nil {
		conn.Close()
		return nil, fmt.Errorf("listen %s: a daemon is already listening there", socket)
	}
	// Nothing answered, so the path is stale (a daemon that died without
	// cleaning up) and taking it is safe.
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	// net.Listen creates the socket 0777&^umask, so chmod-ing after the bind
	// leaves a window in which any local user can connect. Bound under a name
	// nobody can guess and moved into place only once it is already 0600: the
	// socket speaks for a logged-in mailbox, and who can open it is the whole
	// access control (ADR-0014's reasoning, one layer out).
	pending := socket + ".bind"
	if err := os.Remove(pending); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	ln, err := net.Listen("unix", pending)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	if err := os.Chmod(pending, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	// A link, not a rename: rename would write over a socket another daemon
	// had just moved into place, and a daemon whose socket is unlinked while it
	// listens is a daemon nobody can reach. link fails if the name exists.
	if err := os.Link(pending, socket); err != nil {
		ln.Close()
		os.Remove(pending)
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("listen %s: a daemon is already listening there", socket)
		}
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	os.Remove(pending)
	return boundListener{ln.(*net.UnixListener), socket}, nil
}

// boundListener reports the socket's real path. net.Listen bound the private
// name above, and a listener whose Addr() is that name makes its own log line
// a lie.
type boundListener struct {
	*net.UnixListener
	path string
}

func (l boundListener) Addr() net.Addr { return &net.UnixAddr{Name: l.path, Net: "unix"} }

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
