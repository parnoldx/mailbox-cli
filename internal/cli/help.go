package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// tagline says what the program is over, not how it works. What it is built on
// is a question somebody may go on to ask, and `mailbox help architecture` is
// where it is answered.
const tagline = "mailbox — mail, calendars, todos and contacts"

// globalNote is the one line every page carries about the flag every command
// takes. The overview prints the flag properly; a page mentions it and moves on.
const globalNote = "--json prints the response envelope on any command.\n"

// Topic is a page that is not a command: the handful of things worth knowing
// once rather than per command.
type Topic struct {
	Name  string
	Short string
	Text  string
}

var topics = []Topic{
	{
		Name: "ids", Short: "How mail and entries are named",
		Text: `Ids

A message is [account/]box:uid. A bare uid is the inbox on the primary
account, so 36722 and INBOX:36722 name the same message.

  36722          INBOX on the primary account
  Screener:342   INBOX/Screener on the primary account
  gmx/INBOX:412  INBOX on the secondary account named gmx

A box under the inbox is named without it: Screener, Feed, Aside,
"Paper Trail", Screener/Block. Everything else is named outright: Archive,
Drafts, Sent, Junk. Case does not matter; the space in the paper trail
does. "mailbox box list" is the whole list.

An attachment is a message id and an index: 36722:1. Events, todos, habits
and contacts carry ids of their own. Copy an id out of a listing rather
than building one.
`,
	},
	{
		Name: "exit-codes", Short: "What an exit status means",
		Text: `Exit codes

  0  it happened
  1  usage: an unknown command, a missing argument, an unknown flag
  2  not found: nothing has that id
  7  the daemon or a server failed
  9  no daemon is listening

A write that exits 0 has reached the server, and the next read sees it.
A read never waits on a server at all.
`,
	},
	{
		Name: "environment", Short: "The variables that are read",
		Text: `Environment

  MAILBOX_CONFIG   the config file (default ~/.config/mailbox/config.toml)
  MAILBOX_MIRROR   where the mirror file is kept
  MAILBOX_OUTBOX   where the outbox file is kept
  MAILBOX_SOCKET   where the daemon listens
                   (default $XDG_RUNTIME_DIR/mailbox.sock)
  MAILBOX_FOLDER   mirror only these boxes, comma separated — for development

Credentials are in the config file and not in the environment. Write it
with mailbox setup.
`,
	},
	{
		Name: "architecture", Short: "How a command is answered",
		Text: `Architecture

A daemon holds every connection to the servers, and a local copy of what
they hold. Every command here talks to that daemon over a socket and to
nothing else, so a read is answered locally and never waits on a network.
With no daemon listening a command fails rather than dialling out on its
own.

Writes are the other way round. They wait for the server, so an exit code
of 0 means the change has happened and the next read sees it.

Sending is the exception to both. A mail is written to a durable outbox
before SMTP has seen it and stays there afterwards, so "did that go out?"
always has an answer, and a send that fails is one that will be retried
rather than one that is lost.

The local copy is the mirror. It can be thrown away and rebuilt from the
servers; the outbox cannot, and never is.
`,
	},
}

func topic(name string) *Topic {
	for i := range topics {
		if topics[i].Name == name {
			return &topics[i]
		}
	}
	return nil
}

// runHelp answers `mailbox help`, `mailbox help ids` and `mailbox help todo add`
// alike: a topic, a command page, or the overview.
func runHelp(root []*Command, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, overview(root, true))
		return ExitOK
	}
	if t := topic(args[0]); t != nil {
		fmt.Fprint(stdout, t.Text)
		return ExitOK
	}
	if find(root, args[0]) == nil {
		fmt.Fprintf(stderr, "no command or topic named %q\n\n", args[0])
		fmt.Fprint(stderr, overview(root, true))
		return ExitUsage
	}
	node, path, _, code := walk(root, args, stderr)
	if code != ExitOK {
		return code
	}
	fmt.Fprint(stdout, page(node, path))
	return ExitOK
}

// overview is the index: every top-level command under its heading, a name and
// a gloss and nothing else. The flags and the subcommands are one `--help`
// away, and this has to stay a screen.
//
// The topics are listed only when somebody asked for help. On the root they
// would be a second heading of things that are not commands, in a list whose
// whole job is to say what the commands are; the footer names them there
// instead, so they stay one word away without taking a section.
func overview(root []*Command, withTopics bool) string {
	var b strings.Builder
	b.WriteString(tagline)
	b.WriteString("\n\nUSAGE\n  mailbox <command> [flags]\n")

	w := 0
	for _, c := range root {
		w = max(w, len(c.Name))
	}
	for _, t := range topics {
		w = max(w, len(t.Name))
	}

	for _, s := range sections {
		var rows []string
		for _, c := range root {
			if c.Section == s {
				rows = append(rows, fmt.Sprintf("  %-*s  %s", w, c.Name, c.Short))
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n%s\n", s, strings.Join(rows, "\n"))
	}

	if withTopics {
		b.WriteString("\nHELP TOPICS\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "  %-*s  %s\n", w, t.Name, t.Short)
		}
	}

	b.WriteString("\nFLAGS\n")
	fmt.Fprintf(&b, "  %-*s  %s\n", w, "--json", "print the response envelope")

	b.WriteString("\nIds are [account/]box:uid — 36722, Screener:342, gmx/INBOX:412.\n")
	b.WriteString("\n  mailbox <command> --help   one command\n")
	if withTopics {
		b.WriteString("  mailbox help <topic>       a topic above\n")
	} else {
		fmt.Fprintf(&b, "  mailbox help               %s\n", strings.Join(topicNames(), ", "))
	}
	return b.String()
}

// topicNames names the topics for the root footer, which is the only place they
// appear when nobody has asked for help yet.
func topicNames() []string {
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		out = append(out, t.Name)
	}
	return out
}

// page is one command's own help: a group's index, or a leaf's usage, flags and
// examples. Both are rendered from the registry, so neither can drift from what
// the command actually accepts.
func page(c *Command, path []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", c.Short)
	if c.Long != "" {
		fmt.Fprintf(&b, "\n%s\n", wrap(c.Long, 74))
	}

	b.WriteString("\nUSAGE\n")
	switch {
	case len(c.Sub) > 0:
		fmt.Fprintf(&b, "  mailbox %s <command>\n", strings.Join(path, " "))
	default:
		for _, u := range c.Usage {
			fmt.Fprintf(&b, "  %s\n", u)
		}
	}

	if len(c.Sub) > 0 {
		w := 0
		for _, s := range c.Sub {
			w = max(w, len(s.Name))
		}
		b.WriteString("\nCOMMANDS\n")
		for _, s := range c.Sub {
			fmt.Fprintf(&b, "  %-*s  %s\n", w, s.Name, s.Short)
		}
	}

	if len(c.Flags) > 0 {
		w := 0
		for _, f := range c.Flags {
			w = max(w, len(flagName(f)))
		}
		b.WriteString("\nFLAGS\n")
		for _, f := range c.Flags {
			fmt.Fprintf(&b, "  %-*s  %s%s\n", w, flagName(f), f.Desc, flagDefault(f))
		}
	}

	if len(c.Examples) > 0 {
		b.WriteString("\nEXAMPLES\n")
		for _, e := range c.Examples {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}

	// A local command answers here rather than through the daemon, so there is
	// no envelope for --json to print.
	if len(c.Sub) == 0 && !c.Local {
		b.WriteString("\n" + globalNote)
	}
	return b.String()
}

// flagName is how a flag is written on a command line: two dashes, and its
// placeholder when it takes a value.
func flagName(f Flag) string {
	if f.Kind == KindBool {
		return "--" + f.Name
	}
	arg := f.Arg
	if arg == "" {
		arg = "VALUE"
	}
	return "--" + f.Name + " " + arg
}

func flagDefault(f Flag) string {
	switch {
	case f.Kind == KindInt && f.Int != 0:
		return fmt.Sprintf(" (default %d)", f.Int)
	case f.Kind == KindString && f.Str != "":
		return fmt.Sprintf(" (default %s)", f.Str)
	}
	return ""
}

// wrap breaks a Long paragraph so a page reads at any terminal width.
func wrap(s string, n int) string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= n:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// commandInfo is one runnable command in the machine index. The first four
// fields are what the index has always carried; the rest were added beside
// them, so anything reading path, usage, short or flags still reads.
type commandInfo struct {
	Path     []string   `json:"path"`
	Group    string     `json:"group"`
	Usage    string     `json:"usage"`
	Short    string     `json:"short"`
	Flags    []flagInfo `json:"flags"`
	Examples []string   `json:"examples,omitempty"`
}

// flagInfo says whether a flag takes a value, which the name alone does not.
type flagInfo struct {
	Flag string `json:"flag"`
	Arg  string `json:"arg,omitempty"`
	Desc string `json:"desc,omitempty"`
}

// runCommands prints every runnable command as JSON. Groups are not in it:
// naming one only prints help, so there is nothing for a caller to run.
func runCommands(l Locals) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		out := []commandInfo{}
		for _, c := range tree(l) {
			out = collect(out, c, nil, c.Section)
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "write: %v\n", err)
			return ExitAPI
		}
		return ExitOK
	}
}

func collect(out []commandInfo, c *Command, path []string, section Section) []commandInfo {
	path = append(append([]string{}, path...), c.Name)
	if len(c.Sub) > 0 {
		for _, s := range c.Sub {
			out = collect(out, s, path, section)
		}
		return out
	}
	info := commandInfo{
		Path: path, Group: string(section), Short: c.Short,
		Flags: []flagInfo{}, Examples: c.Examples,
	}
	if len(c.Usage) > 0 {
		info.Usage = c.Usage[0]
	}
	for _, f := range c.Flags {
		fi := flagInfo{Flag: "--" + f.Name, Desc: f.Desc}
		if f.Kind != KindBool {
			fi.Arg = f.Arg
			if fi.Arg == "" {
				fi.Arg = "VALUE"
			}
		}
		info.Flags = append(info.Flags, fi)
	}
	return append(out, info)
}
