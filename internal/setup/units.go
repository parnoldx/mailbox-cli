package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Units are the two systemd user units this program owns. They are generated
// files: setup writes them, compares them on a later run, and replaces them
// when they differ, because the unit and the binary have to agree about
// --systemd-socket and a unit this program did not write cannot be assumed to
// (ADR-0012).
type Units struct {
	// Dir is where the units go, normally ~/.config/systemd/user.
	Dir string
	// Exec is the ExecStart path. %h is systemd's home directory, so a binary
	// under the user's home is written relative to it and the unit survives a
	// move of the home itself.
	Exec string
	Ctl  Systemctl
}

// UnitDir is where a systemd --user unit belongs.
func UnitDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

// ExecPath is the command the service unit runs: this binary, with the home
// directory folded back into %h.
//
// The unit runs the binary that installed it, which is the only claim setup can
// honestly make. Running `make install` and `mailbox setup` again is what moves
// a unit from a checkout to ~/.local/bin — and the compare-and-replace above is
// what makes that a repair rather than a puzzle.
func ExecPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, exe); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("%h", rel), nil
		}
	}
	return exe, nil
}

// Files is what the two units should contain.
func (u Units) Files() map[string]string {
	return map[string]string{
		"mailbox.socket": `[Unit]
Description=mailbox daemon socket
Documentation=man:mailbox(1)

[Socket]
# %t is the runtime directory, so this is the path mailbox looks for by
# default. SocketMode is the whole access control: the socket speaks for a
# logged-in mailbox, and nobody else on the machine gets to.
ListenStream=%t/mailbox.sock
SocketMode=0600

[Install]
WantedBy=sockets.target
`,
		"mailbox.service": fmt.Sprintf(`[Unit]
Description=mailbox daemon
Documentation=man:mailbox(1)
# Started by the socket, not by the session: the first widget to connect is
# what brings it up.
Requires=mailbox.socket
After=mailbox.socket

[Service]
Type=simple
ExecStart=%s daemon --systemd-socket
# --systemd-socket is an assertion. Without it a misconfigured unit would bind
# a second socket, look healthy, and be talked to by nobody.
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=default.target
`, u.Exec),
	}
}

// Install writes the units that are missing or out of date and reloads systemd
// if anything moved. It returns the names it wrote.
//
// It does not restart a Daemon that is serving. A unit is read when a service
// starts, so the new one takes effect on the next start, and interrupting a
// cycle to make that happen sooner is a worse trade than saying so.
func (u Units) Install() (written []string, err error) {
	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		return nil, err
	}
	for name, want := range u.Files() {
		path := filepath.Join(u.Dir, name)
		got, err := os.ReadFile(path)
		if err == nil && string(got) == want {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return written, err
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			return written, err
		}
		written = append(written, name)
	}
	if len(written) > 0 && u.Ctl != nil {
		if err := u.Ctl.DaemonReload(); err != nil {
			return written, err
		}
	}
	return written, nil
}

// Systemctl is the part of systemd this program drives. It is an interface
// because a test must be able to run a whole install without one, and because
// there is no systemd at all on the VPS.
type Systemctl interface {
	DaemonReload() error
	EnableNow(unit string) error
	IsEnabled(unit string) (bool, error)
	IsActive(unit string) (bool, error)
}

// SystemctlUser drives `systemctl --user`.
type SystemctlUser struct{}

func (SystemctlUser) DaemonReload() error { return runCmd("systemctl", "--user", "daemon-reload") }

func (SystemctlUser) EnableNow(unit string) error {
	return runCmd("systemctl", "--user", "enable", "--now", unit)
}

func (SystemctlUser) IsEnabled(unit string) (bool, error) { return ask("is-enabled", unit) }
func (SystemctlUser) IsActive(unit string) (bool, error)  { return ask("is-active", unit) }

// ask runs a systemctl query. A non-zero exit is the answer "no" rather than a
// failure — `is-enabled` on a unit that is not there exits 1 and says so.
func ask(verb, unit string) (bool, error) {
	out, err := exec.Command("systemctl", "--user", verb, unit).Output()
	answer := strings.TrimSpace(string(out))
	if err != nil && answer == "" {
		return false, err
	}
	switch verb {
	case "is-enabled":
		return answer == "enabled" || answer == "enabled-runtime" || answer == "static", nil
	default:
		return answer == "active", nil
	}
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// NoSystemd stands in where there is no user session to install into — the VPS,
// where the answer is a system unit written by hand (ADR-0012).
type NoSystemd struct{}

func (NoSystemd) DaemonReload() error            { return nil }
func (NoSystemd) EnableNow(string) error         { return nil }
func (NoSystemd) IsEnabled(string) (bool, error) { return false, nil }
func (NoSystemd) IsActive(string) (bool, error)  { return false, nil }
