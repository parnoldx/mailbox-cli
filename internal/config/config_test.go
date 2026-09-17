package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = "[account]\nemail = \"me@example.com\"\npassword = \"x\"\n"

// A config carries the mail and DAV passwords, so a file anyone else can read
// is refused rather than merely reported. Doctor warns, but every command and
// the daemon load through here, so this is where the contract holds.
func TestAConfigAnyoneElseCanReadIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("a world-readable config loaded")
	} else if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("err = %v, want the fix named", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Account.IMAPHost != "imap.mailbox.org" {
		t.Fatalf("defaults were not applied: %+v", cfg.Account)
	}
}

// A bare mailbox.sock in the world-writable /tmp can be created by any local
// user first, who is then handed the requests meant for the daemon.
func TestTheSocketIsNeverABareNameInTheSharedTempDir(t *testing.T) {
	t.Setenv("MAILBOX_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := SocketPath(); got != "/run/user/1000/mailbox.sock" {
		t.Fatalf("SocketPath() = %q", got)
	}

	// Without a runtime directory it falls back to a directory only this user
	// can reach, never os.TempDir() directly.
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)
	got := SocketPath()
	if got == filepath.Join(os.TempDir(), "mailbox.sock") {
		t.Fatalf("SocketPath() fell back to %q", got)
	}
	if !strings.HasPrefix(got, home+string(filepath.Separator)) {
		t.Fatalf("SocketPath() = %q, want it under %s", got, home)
	}
	if info, err := os.Stat(filepath.Dir(got)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("socket directory mode = %v (%v), want 0700", info, err)
	}

	t.Setenv("MAILBOX_SOCKET", "/tmp/explicit.sock")
	if got := SocketPath(); got != "/tmp/explicit.sock" {
		t.Fatalf("SocketPath() ignored the override: %q", got)
	}
}
