package cli

import (
	"regexp"
	"strings"
	"testing"

	"mailbox/skill"
)

// The skill is the document an agent reads before it types anything, and its
// worth is that it does not repeat the command surface: it names the handful of
// commands a job starts from and sends the reader to `mailbox help` for the
// rest. That is only true while the handful it does name is real. The skill it
// replaced drifted because nothing checked it, and a skill that names a command
// this program has not got teaches an agent to guess.
// It reads the embedded copy rather than the file beside it, so what is gated
// here is what `mailbox setup` installs (ADR-0021's rule, one level down: one
// file, one set of bytes, no second path to disagree with).

// code is one backticked span. A span is checked when it starts with `mailbox`
// or with the name of a top-level command, which leaves the spans that are
// output rather than input — `--json`, `[]`, `{ok, data, mirror}` — alone.
var code = regexp.MustCompile("`([^`]+)`")

func TestSkillNamesOnlyRealCommands(t *testing.T) {
	src := skill.Markdown
	root := tree(Locals{})

	for _, m := range code.FindAllStringSubmatch(src, -1) {
		words := strings.Fields(m[1])
		if len(words) == 0 {
			continue
		}
		if words[0] == "mailbox" {
			words = words[1:]
		} else if find(root, words[0]) == nil {
			continue
		}
		if len(words) == 0 || strings.ContainsAny(m[1], "<") {
			continue // `mailbox` alone, or a template like `mailbox <command> --help`
		}
		if words[0] == "help" {
			checkTopic(t, m[1], words[1:])
			continue
		}
		checkCommand(t, root, m[1], words)
	}
}

// checkTopic holds the help topics the skill sends a reader to. A topic that
// has been renamed is a dead end at the one point the skill is telling somebody
// where to look things up.
func checkTopic(t *testing.T, span string, rest []string) {
	t.Helper()
	if len(rest) == 0 {
		return
	}
	if topic(rest[0]) == nil {
		t.Errorf("`%s`: there is no help topic %q", span, rest[0])
	}
}

// checkCommand walks the span down the tree the way RunWith does, then holds
// every flag left over against what the command declared. A flag is not a flag
// unless it is in the registry (ADR-0020), so this is the same question the
// dispatcher asks.
func checkCommand(t *testing.T, root []*Command, span string, words []string) {
	t.Helper()
	node := find(root, words[0])
	if node == nil {
		t.Errorf("`%s`: there is no command %q", span, words[0])
		return
	}
	path := []string{node.Name}
	words = words[1:]
	for len(node.Sub) > 0 && len(words) > 0 && !strings.HasPrefix(words[0], "-") {
		child := find(node.Sub, words[0])
		if child == nil {
			t.Errorf("`%s`: %s has no %q", span, strings.Join(path, " "), words[0])
			return
		}
		node, path, words = child, append(path, child.Name), words[1:]
	}
	for _, w := range words {
		if !strings.HasPrefix(w, "--") {
			continue
		}
		name := strings.TrimPrefix(strings.SplitN(w, "=", 2)[0], "--")
		if name == "json" || name == "help" {
			continue // taken off the line before any command sees it
		}
		if !declares(node, name) {
			t.Errorf("`%s`: %s has no --%s", span, strings.Join(path, " "), name)
		}
	}
}

func declares(c *Command, name string) bool {
	for _, f := range c.Flags {
		if f.Name == name {
			return true
		}
	}
	return false
}
