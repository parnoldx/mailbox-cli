package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config is the record and a person may have written parts of it
// (ADR-0021), so an edit is a block at a time and everything else comes back
// byte for byte.
const handWritten = `# my config, do not eat
[account]
email = "me@example.org"
password = "hunter2"

# the calendar at the Verein, added by hand years ago
[caldav.verein]
name = "Verein"
url = "https://dav.example.org/cal/1/"
password = "secret"
`

func tempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddingAnAccountLeavesEveryOtherLineAlone(t *testing.T) {
	path := tempConfig(t)
	err := AddAccount(path, AccountBlock{
		Name: "gmx", Email: "me@gmx.de", Password: "second", DisplayName: "Peter",
		IMAPHost: "imap.gmx.net", IMAPPort: 993, SMTPHost: "mail.gmx.net", SMTPPort: 465,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), handWritten) {
		t.Fatalf("the hand-written config was rewritten:\n%s", got)
	}
	for _, want := range []string{
		`[accounts.gmx]`, `email = "me@gmx.de"`, `imap_host = "imap.gmx.net"`,
		`smtp_host = "mail.gmx.net"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the config does not contain %s:\n%s", want, got)
		}
	}

	// It holds a password, and it holds it at 0600 (ADR-0014).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestRemovingABlockPutsTheFileBackAsItWas(t *testing.T) {
	path := tempConfig(t)
	if err := AddAccount(path, AccountBlock{Name: "gmx", Email: "me@gmx.de", IMAPHost: "imap.gmx.net", IMAPPort: 993}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBlock(path, "accounts.gmx"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.TrimSpace(string(got)) != strings.TrimSpace(handWritten) {
		t.Fatalf("removing what was added did not restore the file:\n%q", got)
	}
}

func TestRemovingTheHandWrittenCalendarTakesItsCommentWithIt(t *testing.T) {
	path := tempConfig(t)
	if err := RemoveBlock(path, "caldav.verein"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, gone := range []string{"caldav.verein", "dav.example.org", "added by hand"} {
		if strings.Contains(string(got), gone) {
			t.Fatalf("%q survived the removal:\n%s", gone, got)
		}
	}
	if !strings.Contains(string(got), `email = "me@example.org"`) {
		t.Fatalf("the account went with it:\n%s", got)
	}
}

func TestExcludingACollectionIsWrittenAndTakenBack(t *testing.T) {
	path := tempConfig(t)
	if err := SetExcluded(path, []string{"Gesammelte Adressen"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `exclude = ["Gesammelte Adressen"]`) {
		t.Fatalf("config = %s", got)
	}
	if err := SetExcluded(path, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "collections") {
		t.Fatalf("an empty exclude list left a table behind:\n%s", got)
	}
}
