package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailbox/internal/config"
)

// A config on disk, minimal but loadable.
func writeConfig(t *testing.T, path, extra string) {
	t.Helper()
	body := "[account]\nemail = \"me@example.org\"\npassword = \"hunter2\"\n" + extra
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAConfigThatWillNotParseLeavesTheDaemonRunningOnTheLastGoodOne(t *testing.T) {
	d := seed(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, "")

	applied := 0
	d.WatchConfig(path, func(cfg *config.Config) (Applied, error) {
		applied++
		return Applied{Changes: []string{"read"}}, nil
	})
	if got := d.reloadConfig("test"); len(got) != 1 {
		t.Fatalf("a good config was not applied: %v", got)
	}

	// A typo is a typo. A daemon that exits here turns it into missed mail.
	if err := os.WriteFile(path, []byte("[account\nemail = "), 0o600); err != nil {
		t.Fatal(err)
	}
	d.reloadConfig("test")
	if applied != 1 {
		t.Fatal("a config that does not parse was applied anyway")
	}
	select {
	case <-d.quitting():
		t.Fatal("a config that does not parse stopped the daemon")
	default:
	}
	problems := d.Problems()
	if len(problems) != 1 || problems[0].Name != "config" {
		t.Fatalf("problems = %v", problems)
	}

	// And it clears when the file is fixed, so the notification does not
	// outlive the thing it was about.
	writeConfig(t, path, "")
	d.reloadConfig("test")
	if applied != 2 {
		t.Fatal("a fixed config was not applied")
	}
	if got := d.Problems(); len(got) != 0 {
		t.Fatalf("problems = %v", got)
	}
}

func TestAChangeThatCannotBeMadeInPlaceIsAnExit(t *testing.T) {
	d := seed(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, "")
	d.WatchConfig(path, func(*config.Config) (Applied, error) {
		return Applied{Restart: true, Reason: "the primary account's address changed"}, nil
	})
	d.reloadConfig("test")
	select {
	case <-d.quitting():
	default:
		t.Fatal("a restart-only change did not stop the accept loop")
	}
}

func TestTheFileIsOnlyReadWhenItHasMoved(t *testing.T) {
	d := seed(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, "")
	d.WatchConfig(path, func(*config.Config) (Applied, error) { return Applied{}, nil })

	if d.configMoved() {
		t.Fatal("an untouched file was reported as changed")
	}
	writeConfig(t, path, "display_name = \"Peter\"\n")
	if !d.configMoved() {
		t.Fatal("a rewritten file was not noticed")
	}
	if d.configMoved() {
		t.Fatal("the same file was reported as changed twice")
	}
}

func TestStatusCarriesTheProblemsAndReloadReportsWhatChanged(t *testing.T) {
	d := seed(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, "")
	d.WatchConfig(path, func(*config.Config) (Applied, error) {
		return Applied{Changes: []string{"account gmx is up"}}, nil
	})

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reload"}})
	if !resp.OK {
		t.Fatalf("reload: %s", resp.Error)
	}
	changes, _ := resp.Data.(map[string]any)["changes"].([]string)
	if len(changes) != 1 || !strings.Contains(changes[0], "gmx") {
		t.Fatalf("changes = %v", resp.Data)
	}

	// A problem is read off status, which is what a client re-reads after a
	// problem.changed push (ADR-0011).
	d.SetProblem("credentials gmx", "gmx refuses the password")
	resp = d.handle(context.Background(), Request{ID: "2", Cmd: []string{"status"}})
	if len(resp.Problems) != 1 || resp.Problems[0].Name != "credentials gmx" {
		t.Fatalf("problems = %v", resp.Problems)
	}
}

func TestASecondaryAccountArrivesAndLeavesWithoutARestart(t *testing.T) {
	d := seed(t)
	closed := false
	acct := NewAccount("gmx", nil, nil, []string{"INBOX"}, nil)
	acct.Close = func() { closed = true }

	d.StartAccount(acct)
	if len(d.accounts()) != 2 {
		t.Fatalf("accounts = %d", len(d.accounts()))
	}
	if _, err := d.accountNamed("gmx"); err != nil {
		t.Fatal(err)
	}

	if !d.StopAccount("gmx") {
		t.Fatal("removing an account that is there said it was not")
	}
	if len(d.accounts()) != 1 {
		t.Fatalf("accounts = %d", len(d.accounts()))
	}
	if !closed {
		t.Fatal("a removed account kept its connections open")
	}
	if d.StopAccount("gmx") {
		t.Fatal("removing it twice said it was there twice")
	}
}
