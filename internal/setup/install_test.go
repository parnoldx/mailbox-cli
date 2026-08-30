package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailbox/skill"
)

type fakeCtl struct {
	reloads int
	enabled []string
}

func (f *fakeCtl) DaemonReload() error { f.reloads++; return nil }
func (f *fakeCtl) EnableNow(unit string) error {
	f.enabled = append(f.enabled, unit)
	return nil
}
func (f *fakeCtl) IsEnabled(string) (bool, error) { return true, nil }
func (f *fakeCtl) IsActive(string) (bool, error)  { return true, nil }

func TestTheUnitsAreWrittenOnceAndReplacedWhenTheyDrift(t *testing.T) {
	dir := t.TempDir()
	ctl := &fakeCtl{}
	u := Units{Dir: dir, Exec: "%h/.local/bin/mailbox", Ctl: ctl}

	written, err := u.Install()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("wrote %v", written)
	}
	if ctl.reloads != 1 {
		t.Fatalf("daemon-reload ran %d times", ctl.reloads)
	}

	// The service and the binary have to agree about --systemd-socket: it is
	// an assertion, and a daemon that finds no inherited socket fails rather
	// than binding one nobody is connected to (ADR-0012).
	service, err := os.ReadFile(filepath.Join(dir, "mailbox.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(service), "ExecStart=%h/.local/bin/mailbox daemon --systemd-socket") {
		t.Fatalf("service = %s", service)
	}

	// Run again and nothing moves: this is what makes a second `mailbox setup`
	// cheap and what stops it reloading systemd for no reason.
	if written, err := u.Install(); err != nil || len(written) != 0 {
		t.Fatalf("written = %v, err = %v", written, err)
	}
	if ctl.reloads != 1 {
		t.Fatalf("daemon-reload ran again for no change: %d", ctl.reloads)
	}

	// A unit this program did not write is a liability rather than a
	// customisation: it is replaced, and the person is told which one.
	stale := filepath.Join(dir, "mailbox.service")
	if err := os.WriteFile(stale, []byte("[Service]\nExecStart=/usr/bin/mailbox daemon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err = u.Install()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "mailbox.service" {
		t.Fatalf("written = %v", written)
	}
	if got, _ := os.ReadFile(stale); !strings.Contains(string(got), "--systemd-socket") {
		t.Fatalf("the stale unit was left in place: %s", got)
	}
}

func TestTheSkillIsOneFileUnderTwoNames(t *testing.T) {
	home := t.TempDir()
	s := Skill{
		Dir:  filepath.Join(home, ".agents", "skills", "mailbox"),
		Link: filepath.Join(home, ".claude", "skills", "mailbox"),
	}
	changed, err := s.Install()
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	got, err := os.ReadFile(filepath.Join(s.Dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The bytes are the embedded ones, which are the bytes the tests gate.
	if string(got) != skill.Markdown {
		t.Fatal("the installed skill is not the embedded one")
	}
	// The second name is a link at the first, not a second copy.
	if target, err := os.Readlink(s.Link); err != nil {
		t.Fatalf("the link is not a link: %v", err)
	} else if !strings.HasSuffix(target, filepath.Join(".agents", "skills", "mailbox")) {
		t.Fatalf("link points at %q", target)
	}
	if changed, err := s.Install(); err != nil || changed {
		t.Fatalf("a second install moved something: changed = %v, err = %v", changed, err)
	}

	// A real directory where the link belongs is somebody's own skill under
	// our name. Removing it is a decision for a person.
	other := Skill{Dir: s.Dir, Link: filepath.Join(home, "theirs")}
	if err := os.MkdirAll(other.Link, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Install(); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(other.Link); err != nil {
		t.Fatal("the directory was removed anyway")
	}
}
