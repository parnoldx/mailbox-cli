package cli

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden overview")

// The overview is the one document every caller reads first, so a change to it
// belongs in a diff rather than in a test that only counts lines. Run
// `go test ./internal/cli -update` after changing it on purpose, and read what
// the diff says before committing it.
//
// There are two of them: bare `mailbox` lists commands, and `mailbox help` adds
// the topics, because only the second was asked a question about help.
func TestOverviewMatchesTheGoldenFile(t *testing.T) {
	for _, c := range []struct {
		file   string
		topics bool
	}{
		{"overview.txt", false},
		{"overview-help.txt", true},
	} {
		got := overview(tree(Locals{}), c.topics)
		path := filepath.Join("testdata", c.file)
		if *update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s changed. Run: go test ./internal/cli -update\n\ngot:\n%s", c.file, got)
		}
	}
}

// The topics are for somebody who asked about help. On the root they would be a
// heading of things that are not commands, in a list that exists to say what
// the commands are — but they still have to be findable from there.
func TestTopicsAreListedOnlyUnderHelp(t *testing.T) {
	out, _, code := run(t)
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out, "HELP TOPICS") {
		t.Errorf("the root listed the topics as a section:\n%s", out)
	}
	for _, name := range topicNames() {
		if !strings.Contains(out, name) {
			t.Errorf("the root footer does not name the %q topic:\n%s", name, out)
		}
	}

	out, _, code = run(t, "help")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "HELP TOPICS") {
		t.Errorf("mailbox help did not list the topics:\n%s", out)
	}
}

// walkTree visits every node, leaf and group alike, with the path that reaches
// it.
func walkTree(t *testing.T, nodes []*Command, path []string, fn func(*Command, []string)) {
	t.Helper()
	for _, c := range nodes {
		p := append(append([]string{}, path...), c.Name)
		fn(c, p)
		walkTree(t, c.Sub, p, fn)
	}
}

// Nothing here is generated, so nothing stops a new command being registered
// half-blank. These are the fields that help, `commands` and the parser all
// read, and a command missing one of them is a command that renders wrong
// somewhere nobody looked.
func TestEveryCommandIsComplete(t *testing.T) {
	root := tree(Locals{})
	seen := map[string]bool{}

	walkTree(t, root, nil, func(c *Command, path []string) {
		where := strings.Join(path, " ")
		if seen[where] {
			t.Errorf("%s is registered twice", where)
		}
		seen[where] = true

		if c.Name == "" || c.Short == "" {
			t.Errorf("%s has no name or no short description", where)
		}
		if strings.HasSuffix(c.Short, ".") {
			t.Errorf("%s: a short description is a gloss, not a sentence: %q", where, c.Short)
		}
		if n := len(strings.Fields(c.Short)); n > 6 {
			t.Errorf("%s: short is %d words, the overview has room for 6: %q", where, n, c.Short)
		}

		switch {
		case len(c.Sub) > 0:
			// A group routes; it never runs and never takes flags of its own.
			if c.Run != nil {
				t.Errorf("%s is a group and must not run: naming it prints its index", where)
			}
			if len(c.Flags) > 0 || len(c.Usage) > 0 {
				t.Errorf("%s is a group and must not declare flags or usage", where)
			}
		default:
			if c.Run == nil {
				t.Errorf("%s is a leaf with nothing to run", where)
			}
			if len(c.Usage) == 0 {
				t.Errorf("%s has no usage line", where)
			}
			// A second usage line is either another form or the wrap of the
			// first, and only the first has to name the program.
			if !strings.HasPrefix(c.Usage[0], "mailbox ") {
				t.Errorf("%s: usage starts with the program: %q", where, c.Usage[0])
			}
		}

		// Every flag is turned into a real FlagSet entry by execute, so a
		// duplicate or an unnamed one is a panic waiting for the first caller.
		flags := map[string]bool{}
		for _, f := range c.Flags {
			if f.Name == "" || f.Desc == "" {
				t.Errorf("%s: a flag has no name or no description", where)
			}
			if flags[f.Name] {
				t.Errorf("%s: --%s is declared twice", where, f.Name)
			}
			flags[f.Name] = true
			if f.Name == "json" || f.Name == "help" {
				t.Errorf("%s: --%s is global and must not be redeclared", where, f.Name)
			}
			if f.Kind != KindBool && f.Arg == "" {
				t.Errorf("%s: --%s takes a value and needs a placeholder", where, f.Name)
			}
		}

		// A usage line that names a flag the command does not have is the
		// drift the registry exists to stop.
		for _, u := range c.Usage {
			for _, word := range strings.FieldsFunc(u, func(r rune) bool {
				return r == ' ' || r == '[' || r == ']' || r == '|'
			}) {
				name, ok := strings.CutPrefix(word, "--")
				if !ok || flags[name] {
					continue
				}
				t.Errorf("%s: usage names --%s, which it does not declare", where, name)
			}
		}
	})

	// Only the top level is under a heading, and only the four headings exist.
	for _, c := range root {
		found := false
		for _, s := range sections {
			found = found || c.Section == s
		}
		if !found {
			t.Errorf("%s is under %q, which is not a section", c.Name, c.Section)
		}
		walkTree(t, c.Sub, nil, func(s *Command, path []string) {
			if s.Section != "" {
				t.Errorf("%s %s is not top level and must not name a section", c.Name, s.Name)
			}
		})
	}

	// A topic and a command sharing a name would put the same word under two
	// headings in the overview, meaning two different things.
	for _, tp := range topics {
		if tp.Name == "" || tp.Short == "" || tp.Text == "" {
			t.Errorf("topic %q is incomplete", tp.Name)
		}
		if find(root, tp.Name) != nil {
			t.Errorf("%q is both a command and a help topic", tp.Name)
		}
	}
}

// The index is what an agent reads instead of parsing help text, so it holds
// every leaf, no group, and the four fields it has always carried.
func TestCommandsIndexHoldsEveryLeaf(t *testing.T) {
	out, _, code := run(t, "commands")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got []commandInfo
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	leaves := 0
	walkTree(t, tree(Locals{}), nil, func(c *Command, path []string) {
		if len(c.Sub) == 0 {
			leaves++
		}
	})
	if len(got) != leaves {
		t.Errorf("the index has %d commands, the tree has %d leaves", len(got), leaves)
	}
	for _, c := range got {
		if len(c.Path) == 0 || c.Usage == "" || c.Short == "" || c.Group == "" {
			t.Errorf("%v is missing a field: %+v", c.Path, c)
		}
	}
}

// Bare `mailbox` is a question and not a mistake: it answers on stdout and
// exits 0, so a wrapper script does not read it as a failure.
func TestBareInvocationIsTheOverviewOnStdout(t *testing.T) {
	out, errs, code := run(t)
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if errs != "" {
		t.Errorf("stderr = %q", errs)
	}
	if !strings.HasPrefix(out, tagline) {
		t.Errorf("out does not start with the tagline:\n%s", out)
	}
}

// A bare group name teaches the group. It never guesses at a default, because
// `mailbox todo` listing todos teaches nothing about add, done or drop.
func TestBareGroupIsItsIndex(t *testing.T) {
	for _, name := range []string{"todo", "habit", "contact", "outbox", "route", "aside", "reply-later", "box"} {
		out, _, code := run(t, name)
		if code != ExitOK {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(out, "COMMANDS") {
			t.Errorf("%s did not print its index:\n%s", name, out)
		}
	}
}

// A wrong subcommand is a mistake, so it fails — but it fails with the list the
// caller needed, because the caller cannot scroll back.
func TestAWrongSubcommandPrintsTheIndexAndFails(t *testing.T) {
	out, errs, code := run(t, "todo", "frobnicate")
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if out != "" {
		t.Errorf("an error went to stdout: %q", out)
	}
	if !strings.Contains(errs, `todo has no "frobnicate"`) || !strings.Contains(errs, "undone") {
		t.Errorf("stderr = %q", errs)
	}
}

// --help and -h reach the same page from anywhere on the line, and so does
// `mailbox help <path>`.
func TestHelpIsReachableEveryWay(t *testing.T) {
	root := tree(Locals{})
	want := page(find(find(root, "box").Sub, "view"), []string{"box", "view"})
	for _, args := range [][]string{
		{"box", "view", "--help"},
		{"box", "view", "-h"},
		{"box", "--help", "view"},
		{"help", "box", "view"},
	} {
		out, _, code := run(t, args...)
		if code != ExitOK {
			t.Errorf("%v: exit %d", args, code)
		}
		if out != want {
			t.Errorf("%v printed a different page:\n%s", args, out)
		}
	}
}

// The four topics are reachable, and a name that is neither a topic nor a
// command says so rather than printing something close to it.
func TestHelpTopics(t *testing.T) {
	for _, tp := range topics {
		out, _, code := run(t, "help", tp.Name)
		if code != ExitOK || out != tp.Text {
			t.Errorf("help %s: exit %d", tp.Name, code)
		}
	}
	_, errs, code := run(t, "help", "nonsense")
	if code != ExitUsage || !strings.Contains(errs, "no command or topic") {
		t.Errorf("exit %d, stderr %q", code, errs)
	}
}

// An unknown flag prints the command's own page, not Go's flag package dump —
// and names the flag the way every other line of help names one.
func TestAnUnknownFlagPrintsThePageWithTwoDashes(t *testing.T) {
	_, errs, code := run(t, "search", "rechnung", "--nope")
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs, "--nope") || strings.Contains(errs, "Usage of") {
		t.Errorf("stderr = %q", errs)
	}
	if !strings.Contains(errs, "EXAMPLES") {
		t.Errorf("the page did not follow the error:\n%s", errs)
	}
}

// --json is declared once and taken off the line before a command sees it, so
// it works in any position and no command has to know about it.
func TestGlobalJSONIsTakenOffTheLine(t *testing.T) {
	rest, opts, help := takeGlobals([]string{"todo", "--json", "add", "milch"})
	if !opts.json || help {
		t.Errorf("json=%v help=%v", opts.json, help)
	}
	if strings.Join(rest, " ") != "todo add milch" {
		t.Errorf("rest = %v", rest)
	}
	// Everything after a bare -- is the caller's, including a word that looks
	// like a global.
	rest, opts, _ = takeGlobals([]string{"send", "--", "--json"})
	if opts.json {
		t.Error("--json after -- was taken as a global")
	}
	if strings.Join(rest, " ") != "send -- --json" {
		t.Errorf("rest = %v", rest)
	}
}

// Local is what says a command runs here rather than through the daemon, and
// the four that do are the four that have to answer when nothing is listening:
// two of them are how a caller gets a daemon in the first place.
func TestLocalCommandsAnswerWithNoDaemon(t *testing.T) {
	t.Setenv("MAILBOX_SOCKET", filepath.Join(t.TempDir(), "nothing.sock"))

	var local []string
	for _, c := range tree(Locals{}) {
		if c.Local {
			local = append(local, c.Name)
		}
	}
	if strings.Join(local, " ") != "daemon setup doctor commands version" {
		t.Errorf("the local commands are %v", local)
	}

	for _, name := range []string{"commands", "version"} {
		if _, errs, code := run(t, name); code != ExitOK {
			t.Errorf("%s: exit %d, %s", name, code, errs)
		}
	}
	// Every other command needs one, and says so rather than failing vaguely.
	if _, errs, code := run(t, "status"); code != ExitDaemon {
		t.Errorf("status: exit %d, %s", code, errs)
	}
}

// doctor is the reason local matters. A check that could not run without a
// daemon could never tell you the daemon is what is wrong, so it runs, reports
// the daemon as the failure, and exits non-zero.
func TestDoctorReportsAMissingDaemonRatherThanFailingToRun(t *testing.T) {
	dir := t.TempDir()
	// Its own config and its own paths: doctor reads the real ones otherwise,
	// and a test must not dial somebody's mail server.
	t.Setenv("MAILBOX_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MAILBOX_MIRROR", filepath.Join(dir, "mirror.db"))
	t.Setenv("MAILBOX_OUTBOX", filepath.Join(dir, "outbox.db"))
	t.Setenv("MAILBOX_SOCKET", filepath.Join(dir, "nothing.sock"))
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[account]\nemail = \"me@example.com\"\npassword = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, code := run(t, "doctor", "--offline")
	if code != ExitAPI {
		t.Errorf("exit %d, want the failure to be reported as one", code)
	}
	if !strings.Contains(out, "nothing listening") {
		t.Errorf("doctor did not name the missing daemon:\n%s", out)
	}
	// And the local half still answered, which is the half that works.
	if !strings.Contains(out, "config") || !strings.Contains(out, "mirror") {
		t.Errorf("the local checks did not run:\n%s", out)
	}
}

// --offline is what makes doctor runnable on a machine with no network, and it
// has to actually dial nothing.
func TestDoctorOfflineChecksTheFilesOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAILBOX_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MAILBOX_MIRROR", filepath.Join(dir, "mirror.db"))
	t.Setenv("MAILBOX_OUTBOX", filepath.Join(dir, "outbox.db"))
	t.Setenv("MAILBOX_SOCKET", filepath.Join(dir, "nothing.sock"))
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[account]\nemail = \"me@example.com\"\npassword = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, _ := run(t, "doctor", "--offline")
	for _, name := range []string{"imap", "smtp", "dav", "sieve"} {
		if strings.Contains(out, name) {
			t.Errorf("--offline dialled %s:\n%s", name, out)
		}
	}
}

// A config anyone can read is a password anyone can read (ADR-0014).
func TestDoctorFlagsAWorldReadableConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAILBOX_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MAILBOX_MIRROR", filepath.Join(dir, "mirror.db"))
	t.Setenv("MAILBOX_OUTBOX", filepath.Join(dir, "outbox.db"))
	t.Setenv("MAILBOX_SOCKET", filepath.Join(dir, "nothing.sock"))
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[account]\nemail = \"me@example.com\"\npassword = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ := run(t, "doctor", "--offline")
	if !strings.Contains(out, "FAIL  config mode") || !strings.Contains(out, "0644") {
		t.Errorf("a world-readable config passed:\n%s", out)
	}
}
