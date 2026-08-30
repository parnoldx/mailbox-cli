package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"mailbox/skill"
)

// Skill installs the agent skill: one file, under two names. It writes the
// embedded copy, which is the same file `make skill` installs from the working
// tree, so a machine with no checkout gets the same bytes and there is no
// second copy free to disagree with this one.
type Skill struct {
	// Dir is where the skill lives, normally ~/.agents/skills, alongside every
	// other skill on the machine.
	Dir string
	// Link is the name Claude Code reads it through, normally
	// ~/.claude/skills/mailbox, and it is a symlink at the directory above.
	Link string
}

// SkillPaths are the two locations, under the user's home.
func SkillPaths() (dir, link string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".agents", "skills", "mailbox"),
		filepath.Join(home, ".claude", "skills", "mailbox"), nil
}

// Install writes the skill and links it. It reports whether anything moved.
//
// It refuses to remove a real directory standing where the link belongs: that
// is somebody's own skill under our name, and deleting it is a decision for a
// person rather than for a wizard.
func (s Skill) Install() (changed bool, err error) {
	path := filepath.Join(s.Dir, "SKILL.md")
	if got, err := os.ReadFile(path); err != nil || string(got) != skill.Markdown {
		if err := os.MkdirAll(s.Dir, 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, []byte(skill.Markdown), 0o644); err != nil {
			return false, err
		}
		changed = true
	}
	if s.Link == "" {
		return changed, nil
	}
	if info, lerr := os.Lstat(s.Link); lerr == nil && info.Mode()&os.ModeSymlink == 0 {
		return changed, fmt.Errorf("%s is a directory, not a symlink — "+
			"remove it and run setup again", s.Link)
	}
	target, terr := filepath.Rel(filepath.Dir(s.Link), s.Dir)
	if terr != nil {
		target = s.Dir
	}
	if got, lerr := os.Readlink(s.Link); lerr == nil && got == target {
		return changed, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Link), 0o755); err != nil {
		return changed, err
	}
	_ = os.Remove(s.Link)
	if err := os.Symlink(target, s.Link); err != nil {
		return changed, err
	}
	return true, nil
}
