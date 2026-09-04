package cli

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Version is the build's version, set with
// -ldflags "-X mailbox/internal/cli.Version=…". A binary built by hand says so
// rather than claiming a release it is not.
var Version = "(devel)"

// Section is the heading a top-level Command appears under in the overview.
// Every top-level Command has exactly one and there are no other headings; the
// invariants test holds that.
type Section string

const (
	SectionMail     Section = "MAIL"
	SectionOrganize Section = "ORGANIZE"
	SectionTime     Section = "CALENDAR & TASKS & CONTACTS"
	SectionSystem   Section = "SYSTEM"
)

// sections is the order the overview prints them in, and inside each one the
// registration order stands. Both are workflow order and not alphabetical: the
// read comes first everywhere, because reading is how a caller gets the id the
// other verbs need.
var sections = []Section{
	SectionMail, SectionOrganize, SectionTime, SectionSystem,
}

// Kind is what a Flag carries.
type Kind int

const (
	KindBool Kind = iota
	KindString
	KindInt
	KindList // repeatable, or comma separated
)

// Flag is one flag on one Command. It is declared here and nowhere else: the
// FlagSet a Command parses with is built from this, so the help text and the
// parser cannot disagree (ADR-0020).
type Flag struct {
	Name string
	Kind Kind
	Arg  string // the placeholder in help; ignored for a bool
	Desc string
	Int  int    // default, for KindInt
	Str  string // default, for KindString
}

// Command is one node of the command tree. A node with Sub is a group, and
// naming a group without a subcommand prints its index rather than guessing at
// a default (ADR-0020).
type Command struct {
	Name     string
	Section  Section // top level only
	Short    string
	Long     string
	Usage    []string
	Flags    []Flag
	Examples []string

	// Needs says the command cannot run without at least one word before its
	// flags. The dispatcher enforces it, so no Run body repeats the check.
	Needs bool

	// Local marks a command that runs in this process instead of dialling the
	// Daemon. It is internal: nothing prints it and nothing exports it.
	Local bool

	Run func(in *input, stdout, stderr io.Writer) int
	Sub []*Command
}

// Locals are the two commands that cannot go through the socket, because one
// owns it and the other writes the config that names it. They live in package
// main, so they are handed in rather than imported.
type Locals struct {
	Daemon func(systemdSocket bool, stdout, stderr io.Writer) int
	Setup  func(stdout, stderr io.Writer) int
}

// options are the flags that mean the same thing on every command. They are
// taken off the command line before any command sees them, so no command
// declares them and none can mean something else by them.
type options struct{ json bool }

// input is a parsed command line: the words before the flags, and the flags.
type input struct {
	Words []string
	json  bool
	bools map[string]*bool
	strs  map[string]*string
	ints  map[string]*int
	lists map[string]*repeated
}

// Text is every word before the flags as one string, which is what a subject,
// a query or a todo's title is.
func (in *input) Text() string { return strings.Join(in.Words, " ") }

// First is the leading word: an id, for the commands that take exactly one.
func (in *input) First() string {
	if len(in.Words) == 0 {
		return ""
	}
	return in.Words[0]
}

func (in *input) JSON() bool { return in.json }

func (in *input) Bool(name string) bool {
	if p, ok := in.bools[name]; ok {
		return *p
	}
	return false
}

func (in *input) Str(name string) string {
	if p, ok := in.strs[name]; ok {
		return *p
	}
	return ""
}

func (in *input) Int(name string) int {
	if p, ok := in.ints[name]; ok {
		return *p
	}
	return 0
}

func (in *input) List(name string) []string {
	if p, ok := in.lists[name]; ok {
		return []string(*p)
	}
	return nil
}

// replyWatchFlags are the "if no reply by" reminder flags — HEY's Bubble Up,
// matched from the sending side: --if-no-reply is the switch, and the timing
// is the same one flag `mailbox bubble` takes (see its Flags below), required
// only once --if-no-reply asks for it. Shared by compose, reply, forward and
// draft send.
var replyWatchFlags = []Flag{
	{Name: "if-no-reply", Kind: KindBool,
		Desc: "bring this back to the Inbox if nobody replies by the timing below"},
	{Name: "on", Kind: KindString, Arg: "DATE", Desc: "a date like 2026-09-10 (08:00, or 18:00 if today)"},
	{Name: "tomorrow", Kind: KindBool, Desc: "08:00 tomorrow"},
	{Name: "weekend", Kind: KindBool, Desc: "08:00 the coming Saturday"},
	{Name: "next-week", Kind: KindBool, Desc: "08:00 the coming Monday"},
}

// tree is the whole command surface. Everything printed as help, everything
// listed by `mailbox commands`, and everything the dispatcher will run is here
// and only here.
func tree(l Locals) []*Command {
	return []*Command{
		// MAIL ───────────────────────────────────────────────────────────
		{
			Name: "box", Section: SectionMail, Short: "The mail in a box",
			Sub: []*Command{{
				Name: "list", Short: "The boxes, and how full",
				Usage: []string{"mailbox box list [--archive] [--unread]"},
				Long: "The boxes mail moves through, in that order: inbox, feed, paper " +
					"trail, screener, aside, reply later, sent, drafts, junk. Everything " +
					"else — the archive tree, and the block pile under the screener — is " +
					"behind --archive. Driven by what is held rather than by what has mail " +
					"in it, so an empty box is a row saying so.",
				Flags: []Flag{
					{Name: "archive", Kind: KindBool, Desc: "every box, not only the ones mail moves through"},
					{Name: "unread", Kind: KindBool, Desc: "only boxes with something unread"},
				},
				Examples: []string{
					"mailbox box list",
					"mailbox box list --unread",
					"mailbox box list --archive",
				},
				Run: runBoxList,
			}, {
				Name: "view", Short: "List the mail in a box",
				Usage: []string{"mailbox box view [BOX] [--limit N]"},
				Long: "With no box named this is the inbox. A box under it is named without " +
					"it — Screener, not INBOX/Screener; see `mailbox help ids`.",
				Flags: []Flag{
					{Name: "limit", Kind: KindInt, Arg: "N", Int: 50, Desc: "how many messages to show"},
				},
				Examples: []string{
					"mailbox box view",
					"mailbox box view Screener --limit 10",
					`mailbox box view "Paper Trail"`,
				},
				Run: runBoxView,
			}},
		},
		{
			Name: "message", Section: SectionMail, Short: "One message, read whole",
			Sub: []*Command{{
				Name: "view", Short: "Read one message", Needs: true,
				Usage:    []string{"mailbox message view ID"},
				Examples: []string{"mailbox message view 36722", "mailbox message view Screener:342"},
				Run:      runMessageView,
			}},
		},
		{
			Name: "thread", Section: SectionMail, Short: "A whole conversation", Needs: true,
			Usage: []string{"mailbox thread ID"},
			Long: "Any message in the conversation names the whole of it, and the " +
				"answer is every message across every box.",
			Examples: []string{"mailbox thread 36722"},
			Run:      runThread,
		},
		{
			Name: "search", Section: SectionMail, Short: "Search every box", Needs: true,
			Usage: []string{"mailbox search QUERY [--in BOX] [--from ADDR] [--limit N]"},
			Long: "Ranked full-text over senders, recipients, subjects and text. " +
				"Words before the flags are the query.",
			Flags: []Flag{
				{Name: "in", Kind: KindString, Arg: "BOX", Desc: "only this box"},
				{Name: "from", Kind: KindString, Arg: "ADDR", Desc: "only mail from a sender containing this"},
				{Name: "limit", Kind: KindInt, Arg: "N", Int: 25, Desc: "how many results to show"},
			},
			Examples: []string{
				"mailbox search rechnung",
				"mailbox search rechnung mai --in feed",
				"mailbox search invoice --from stripe.com --limit 5",
			},
			Run: runSearch,
		},
		{
			Name: "compose", Section: SectionMail, Short: "Write and send a mail",
			Usage: []string{
				"mailbox compose --to ADDR --subject S [--body TEXT] [--cc ADDR] [--bcc ADDR]",
				"                [--attach PATH] [--account NAME] [--draft]",
			},
			Long: "Omit --body and the text is read from stdin, which is how a heredoc " +
				"or a generated mail gets in. The body is Markdown — plain prose is " +
				"Markdown too — and the mail carries both it and a rendered HTML copy; " +
				"pass --body-html instead to send HTML you already have. With --draft " +
				"it goes to the drafts box instead of out, and `mailbox draft` takes " +
				"it from there.",
			Flags: append([]Flag{
				{Name: "to", Kind: KindList, Arg: "ADDR", Desc: "a recipient (repeatable, or comma separated)"},
				{Name: "subject", Kind: KindString, Arg: "S", Desc: "the subject"},
				{Name: "body", Kind: KindString, Arg: "TEXT", Desc: "the text, as Markdown; omit to read it from stdin"},
				{Name: "body-html", Kind: KindString, Arg: "HTML", Desc: "send this HTML as the body instead of rendering --body"},
				{Name: "cc", Kind: KindList, Arg: "ADDR", Desc: "a copied recipient"},
				{Name: "bcc", Kind: KindList, Arg: "ADDR", Desc: "a blind copied recipient"},
				{Name: "attach", Kind: KindList, Arg: "PATH", Desc: "a file to attach (repeatable)"},
				{Name: "account", Kind: KindString, Arg: "NAME", Desc: "which account sends it (default the primary)"},
				{Name: "draft", Kind: KindBool, Desc: "file it in drafts instead of sending it"},
			}, replyWatchFlags...),
			Examples: []string{
				`mailbox compose --to anna@example.com --subject "Kurz" --body "Passt."`,
				`printf 'Langer Text\n' | mailbox compose --to anna@example.com --subject Bericht`,
				"mailbox compose --to anna@example.com --subject Foto --attach ./bild.png",
				`mailbox compose --to anna@example.com --subject Angebot --body "…" --draft`,
				"mailbox compose --to anna@example.com --subject Angebot --if-no-reply --next-week",
			},
			Run: runCompose,
		},
		{
			Name: "reply", Section: SectionMail, Short: "Answer, in its thread", Needs: true,
			Usage: []string{"mailbox reply ID [--all] [--body TEXT] [--attach PATH] [--draft]"},
			Long: "The recipients and the References come from the message being answered, " +
				"so a thread is never assembled by hand. With --draft it goes to the " +
				"drafts box instead of out, and it stays in the thread when it is sent " +
				"from there.",
			Flags: append([]Flag{
				{Name: "all", Kind: KindBool, Desc: "copy everyone the message was addressed to"},
				{Name: "body", Kind: KindString, Arg: "TEXT", Desc: "the text, as Markdown; omit to read it from stdin"},
				{Name: "body-html", Kind: KindString, Arg: "HTML", Desc: "send this HTML as the body instead of rendering --body"},
				{Name: "to", Kind: KindList, Arg: "ADDR", Desc: "answer somebody other than the sender"},
				{Name: "cc", Kind: KindList, Arg: "ADDR", Desc: "an extra copied recipient"},
				{Name: "subject", Kind: KindString, Arg: "S", Desc: "override the Re: subject"},
				{Name: "attach", Kind: KindList, Arg: "PATH", Desc: "a file to attach (repeatable)"},
				{Name: "draft", Kind: KindBool, Desc: "file it in drafts instead of sending it"},
			}, replyWatchFlags...),
			Examples: []string{
				`mailbox reply 36722 --body "Danke, passt."`,
				`mailbox reply Screener:342 --all --body "Cc an alle."`,
				`mailbox reply 36722 --body "Erster Entwurf." --draft`,
				`mailbox reply 36722 --body "Bin dran." --if-no-reply --tomorrow`,
			},
			Run: runReply,
		},
		{
			Name: "forward", Section: SectionMail, Short: "Send a message on", Needs: true,
			Usage: []string{"mailbox forward ID --to ADDR [--body TEXT]"},
			Long: "The original is quoted whole under a header block. A forward starts a " +
				"new conversation rather than joining the old one, because the people it " +
				"goes to were never in that one.",
			Flags: append([]Flag{
				{Name: "to", Kind: KindList, Arg: "ADDR", Desc: "who to send it to (repeatable)"},
				{Name: "cc", Kind: KindList, Arg: "ADDR", Desc: "a copied recipient"},
				{Name: "body", Kind: KindString, Arg: "TEXT", Desc: "a note above the forwarded mail"},
				{Name: "subject", Kind: KindString, Arg: "S", Desc: "override the Fwd: subject"},
				{Name: "attach", Kind: KindList, Arg: "PATH", Desc: "a file to attach (repeatable)"},
			}, replyWatchFlags...),
			Examples: []string{
				"mailbox forward 36722 --to anna@example.com",
				`mailbox forward Screener:342 --to anna@example.com --body "Kennst du die?"`,
			},
			Run: runForward,
		},
		{
			Name: "draft", Section: SectionMail, Short: "Mail written but not sent",
			Long: "A draft is mail in the drafts box carrying the \\Draft flag, so one " +
				"written in webmail and one written here are the same thing. Imap has no " +
				"in-place edit: changing one writes a new version and trashes the old, so " +
				"its id changes and the reply says what the new one is.",
			Sub: []*Command{
				{
					Name: "list", Short: "What is waiting to be finished",
					Usage: []string{"mailbox draft list [--limit N]"},
					Flags: []Flag{
						{Name: "limit", Kind: KindInt, Arg: "N", Int: 25, Desc: "how many to show"},
					},
					Run: draftVerb("list"),
				},
				{
					Name: "show", Short: "Read a draft", Needs: true,
					Usage: []string{"mailbox draft show ID"},
					Long: "The id draft list printed, or a bare uid — the command has already " +
						"said which box this is about.",
					Examples: []string{"mailbox draft show Drafts:12", "mailbox draft show 12"},
					Run:      draftVerb("show"),
				},
				{
					Name: "edit", Short: "Change a draft", Needs: true,
					Usage: []string{"mailbox draft edit ID [--to ADDR] [--subject S] [--body TEXT]"},
					Long: "What is not named keeps what the draft already said. Naming --to " +
						"replaces the recipients rather than adding to them.",
					Flags: []Flag{
						{Name: "to", Kind: KindList, Arg: "ADDR", Desc: "replace the recipients"},
						{Name: "cc", Kind: KindList, Arg: "ADDR", Desc: "replace the copied recipients"},
						{Name: "subject", Kind: KindString, Arg: "S", Desc: "a new subject"},
						{Name: "body", Kind: KindString, Arg: "TEXT", Desc: "new text"},
					},
					Examples: []string{`mailbox draft edit 12 --subject "Rechnung September"`},
					Run:      draftVerb("edit"),
				},
				{
					Name: "send", Short: "Send a draft", Needs: true,
					Usage: []string{"mailbox draft send ID [--to ADDR] [--body TEXT]"},
					Long: "It goes through the outbox like any other send, and the draft is " +
						"trashed only once the mail is out.",
					Flags: append([]Flag{
						{Name: "to", Kind: KindList, Arg: "ADDR", Desc: "replace the recipients"},
						{Name: "cc", Kind: KindList, Arg: "ADDR", Desc: "replace the copied recipients"},
						{Name: "subject", Kind: KindString, Arg: "S", Desc: "a new subject"},
						{Name: "body", Kind: KindString, Arg: "TEXT", Desc: "new text"},
					}, replyWatchFlags...),
					Examples: []string{"mailbox draft send 12"},
					Run:      draftVerb("send"),
				},
				{
					Name: "delete", Short: "Trash a draft", Needs: true,
					Usage: []string{"mailbox draft delete ID"},
					Run:   draftVerb("delete"),
				},
			},
		},
		{
			Name: "outbox", Section: SectionMail, Short: "What is queued to go out",
			Long: "A mail is durable here before SMTP has seen it and stays here " +
				"afterwards, so \"did that go out?\" always has an answer.",
			Sub: []*Command{
				{
					Name: "list", Short: "What is queued to go out",
					Usage: []string{"mailbox outbox list"},
					Run:   outboxVerb("list"),
				},
				{
					Name: "retry", Short: "Send a held mail again", Needs: true,
					Usage:    []string{"mailbox outbox retry ID"},
					Examples: []string{"mailbox outbox retry 3"},
					Run:      outboxVerb("retry"),
				},
				{
					Name: "cancel", Short: "Drop a mail from the queue", Needs: true,
					Usage: []string{"mailbox outbox cancel ID"},
					Run:   outboxVerb("cancel"),
				},
			},
		},
		{
			Name: "attachment", Section: SectionMail, Short: "The files a message carries",
			Long: "Listing is answered locally. Saving is the one read that waits on " +
				"the server, because a file is never held here.",
			Sub: []*Command{
				{
					Name: "list", Short: "What a message carries", Needs: true,
					Usage:    []string{"mailbox attachment list ID"},
					Examples: []string{"mailbox attachment list 36722"},
					Run:      runAttachmentList,
				},
				{
					Name: "save", Short: "Fetch one file", Needs: true,
					Usage: []string{"mailbox attachment save ID[:INDEX] [--output PATH] [--force]"},
					Flags: []Flag{
						{Name: "output", Kind: KindString, Arg: "PATH", Desc: "where to write it: a file, or a directory"},
						{Name: "force", Kind: KindBool, Desc: "overwrite an existing file"},
					},
					Examples: []string{
						"mailbox attachment save 36722:1",
						"mailbox attachment save 36722:1 --output ~/Downloads",
					},
					Run: runAttachmentSave,
				},
				{
					Name: "bytes", Short: "Fetch one small part as base64", Needs: true,
					Long: "For a reading pane inlining the images an HTML body refers to. " +
						"Capped well below attachment save, which streams a real file to disk.",
					Usage:    []string{"mailbox attachment bytes ID[:INDEX]"},
					Examples: []string{"mailbox attachment bytes 36722:2"},
					Run:      runAttachmentBytes,
				},
			},
		},

		// ORGANIZE ───────────────────────────────────────────────────────
		{
			Name: "screener", Section: SectionOrganize, Short: "Senders waiting for a decision",
			Usage: []string{"mailbox screener [--limit N]"},
			Long: "The screener holds mail from senders nothing has been decided about. " +
				"What is owed there is a decision per sender rather than a read per mail, " +
				"and `mailbox route set` is how one is made.",
			Flags: []Flag{
				{Name: "limit", Kind: KindInt, Arg: "N", Int: 25, Desc: "how many senders to show"},
			},
			Examples: []string{"mailbox screener"},
			Run:      runScreener,
		},
		{
			Name: "route", Section: SectionOrganize, Short: "Where a sender's mail goes",
			Long: "Deciding rewrites the sieve script on the server and moves what is " +
				"already waiting, so one command finishes one decision.",
			Sub: []*Command{
				{
					Name: "list", Short: "The decisions already made",
					Usage: []string{"mailbox route list [--script]"},
					Flags: []Flag{
						{Name: "script", Kind: KindBool, Desc: "print the sieve script the routing comes from"},
					},
					Examples: []string{"mailbox route list"},
					Run:      runRouteList,
				},
				{
					Name: "set", Short: "Decide where a sender's mail goes", Needs: true,
					Usage: []string{"mailbox route set TARGET... --to BOX"},
					Long: "TARGET is a message id or an address: whoever has just read " +
						"something in the screener has its id and not its sender's " +
						"address. BOX is inbox, feed, paper, block, or screener, which " +
						"forgets the sender and puts their next mail back there.",
					Flags: []Flag{
						{Name: "to", Kind: KindString, Arg: "BOX", Desc: "inbox, feed, paper, block, or screener"},
					},
					Examples: []string{
						"mailbox route set Screener:342 --to feed",
						"mailbox route set news@example.com --to block",
						"mailbox route set alt@example.com --to screener",
					},
					Run: runRouteSet,
				},
			},
		},
		{
			Name: "aside", Section: SectionOrganize, Short: "The read-later pile",
			Long: "Aside is decided one conversation at a time, never per sender: the " +
				"routing decides about senders, and \"read this later\" is about a " +
				"thread. Setting one message aside takes the rest of its thread with " +
				"it; a reply arriving pulls the thread back. Read the pile with " +
				"`mailbox box view Aside`.",
			Sub: []*Command{
				{
					Name: "add", Short: "Put mail aside", Needs: true,
					Usage:    []string{"mailbox aside add ID..."},
					Examples: []string{"mailbox aside add 36722", "mailbox aside add 36722 36723"},
					Run:      asideVerb(false),
				},
				{
					Name: "done", Short: "Take mail back out", Needs: true,
					Usage: []string{"mailbox aside done ID..."},
					Run:   asideVerb(true),
				},
			},
		},
		{
			Name: "reply-later", Section: SectionOrganize, Short: "The reply-later pile",
			Long: "Reply Later is decided one conversation at a time, never per sender: " +
				"the routing decides about senders, and \"I owe this a reply\" is about " +
				"a thread. It takes and releases a whole thread at once; answering it, " +
				"or a reply arriving, pulls the thread back. Read the pile with " +
				"`mailbox box view \"Reply Later\"`.",
			Sub: []*Command{
				{
					Name: "add", Short: "Put mail in the reply-later pile", Needs: true,
					Usage:    []string{"mailbox reply-later add ID..."},
					Examples: []string{"mailbox reply-later add 36722", "mailbox reply-later add 36722 36723"},
					Run:      replyLaterVerb(false),
				},
				{
					Name: "done", Short: "Take mail back out", Needs: true,
					Usage: []string{"mailbox reply-later done ID..."},
					Run:   replyLaterVerb(true),
				},
			},
		},
		{
			Name: "bubble", Section: SectionOrganize, Short: "Bubble a thread back later",
			Usage: []string{
				"mailbox bubble ID... [--now] [--on DATE] [--tomorrow] [--weekend] [--next-week]",
				"mailbox bubble list",
			},
			Needs: true,
			Long: "Bubble Up, matched from HEY: the thread leaves the inbox and comes " +
				"back on its own at the time you pick, unread so the phone raises a " +
				"notification. It waits in Aside carrying a keyword — no separate box — " +
				"and a reply arriving brings it back early. One timing flag is required " +
				"and there is no default. --on takes a bare date and lands at 08:00, or " +
				"at 18:00 when the date is today. `mailbox bubble list` shows what is " +
				"waiting and when each is due; --now brings a thread back straight away.",
			Flags: []Flag{
				{Name: "now", Kind: KindBool, Desc: "bring the thread back now"},
				{Name: "on", Kind: KindString, Arg: "DATE", Desc: "a date like 2026-09-10 (08:00, or 18:00 if today)"},
				{Name: "tomorrow", Kind: KindBool, Desc: "08:00 tomorrow"},
				{Name: "weekend", Kind: KindBool, Desc: "08:00 the coming Saturday"},
				{Name: "next-week", Kind: KindBool, Desc: "08:00 the coming Monday"},
			},
			Examples: []string{
				"mailbox bubble 36722 --tomorrow",
				"mailbox bubble 36722 --on 2026-09-15",
				"mailbox bubble list",
				"mailbox bubble 36722 --now",
			},
			Run: runBubble,
		},
		{
			Name: "label", Section: SectionOrganize, Short: "Labels on mail",
			Long: "A label is an imap keyword, not a box: a message carries as many as you " +
				"like and keeps all of them when it moves. A label in use needs no " +
				"creating — adding it to mail is what makes it exist.",
			Sub: []*Command{
				{
					Name: "list", Short: "Labels, and how much carries each",
					Usage: []string{"mailbox label list"},
					Run:   runLabelList,
				},
				{
					Name: "view", Short: "Mail carrying a label", Needs: true,
					Usage: []string{"mailbox label view NAME [--limit N]"},
					Flags: []Flag{
						{Name: "limit", Kind: KindInt, Arg: "N", Int: 50, Desc: "how many messages to show"},
					},
					Examples: []string{"mailbox label view learn"},
					Run:      labelVerb("view"),
				},
				{
					Name: "add", Short: "Put a label on mail", Needs: true,
					Usage: []string{"mailbox label add ID... --to NAME"},
					Flags: []Flag{
						{Name: "to", Kind: KindString, Arg: "NAME", Desc: "the label to add"},
					},
					Examples: []string{"mailbox label add 36722 --to learn"},
					Run:      labelApply("add", "to"),
				},
				{
					Name: "remove", Short: "Take a label off mail", Needs: true,
					Usage: []string{"mailbox label remove ID... --from NAME"},
					Flags: []Flag{
						{Name: "from", Kind: KindString, Arg: "NAME", Desc: "the label to remove"},
					},
					Run: labelApply("remove", "from"),
				},
				{
					Name: "create", Short: "Remember a label name", Needs: true,
					Usage: []string{"mailbox label create NAME [ID...]"},
					Long: "Only needed for a label with no mail on it yet: one already in use " +
						"is listed because the mail carrying it says so. Ids after the name " +
						"are labelled at the same time.",
					Examples: []string{"mailbox label create learn", "mailbox label create learn 36722"},
					Run:      runLabelCreate,
				},
			},
		},
		{
			Name: "move", Section: SectionOrganize, Short: "Move mail to another box", Needs: true,
			Usage: []string{"mailbox move ID... --to BOX"},
			Flags: []Flag{
				{Name: "to", Kind: KindString, Arg: "BOX", Desc: "the box to move into"},
			},
			Examples: []string{"mailbox move 36722 --to Archive"},
			Run:      runMove,
		},
		{
			Name: "seen", Section: SectionOrganize, Short: "Mark mail read", Needs: true,
			Usage: []string{"mailbox seen ID..."}, Run: writeVerb("seen"),
		},
		{
			Name: "unseen", Section: SectionOrganize, Short: "Mark mail unread", Needs: true,
			Usage: []string{"mailbox unseen ID..."}, Run: writeVerb("unseen"),
		},
		{
			Name: "trash", Section: SectionOrganize, Short: "Move mail to Trash", Needs: true,
			Usage: []string{"mailbox trash ID..."},
			Long:  "Trash is the only box that is not held locally, so a message moved here stops being readable.",
			Run:   writeVerb("trash"),
		},
		{
			Name: "spam", Section: SectionOrganize, Short: "Move mail to Junk", Needs: true,
			Usage: []string{"mailbox spam ID..."},
			Long: "Files mail where the server's own spam handling can see it. It blocks " +
				"nobody: the decision about a sender is `mailbox route set --to block`.",
			Run: writeVerb("spam"),
		},

		// CALENDAR & TASKS ───────────────────────────────────────────────
		{
			Name: "agenda", Section: SectionTime, Short: "What is on",
			Usage: []string{"mailbox agenda [--days N] [--from DATE] [--calendar NAME]"},
			Long: "A repeating entry has no finite list of instances, so the window is " +
				"the question and the rule is expanded over it.",
			Flags: []Flag{
				{Name: "days", Kind: KindInt, Arg: "N", Int: 7, Desc: "how many days to show"},
				{Name: "from", Kind: KindString, Arg: "DATE", Desc: "the first day, as 2026-08-29 (default today)"},
				{Name: "calendar", Kind: KindString, Arg: "NAME", Desc: "only this calendar"},
				{Name: "limit", Kind: KindInt, Arg: "N", Desc: "at most this many entries"},
			},
			Examples: []string{"mailbox agenda", "mailbox agenda --days 30 --calendar Work"},
			Run:      runAgenda,
		},
		{
			Name: "calendar", Section: SectionTime, Short: "The calendars and task lists",
			Sub: []*Command{{
				Name: "list", Short: "Every collection held",
				Usage: []string{"mailbox calendar list [--kind KIND]"},
				Flags: []Flag{
					{Name: "kind", Kind: KindString, Arg: "KIND", Desc: "only this kind: events, tasks, cards"},
				},
				Examples: []string{"mailbox calendar list", "mailbox calendar list --kind tasks"},
				Run:      runCalendarList,
			}},
		},
		{
			Name: "event", Section: SectionTime, Short: "One entry, and when it repeats",
			Long: "A time on --start makes an appointment; a bare date makes an all-day " +
				"entry, because \"Friday\" does not mean midnight on Friday.",
			Sub: []*Command{
				{
					Name: "view", Short: "Read one entry", Needs: true,
					Usage:    []string{"mailbox event view ID"},
					Examples: []string{"mailbox event view 41"},
					Run:      runEventView,
				},
				{
					Name: "add", Short: "Put something on a calendar", Needs: true,
					Usage: []string{
						"mailbox event add TEXT --start DATE[ TIME] [--end DATE[ TIME]]",
						"                      [--calendar NAME] [--location TEXT] [--notes TEXT]",
						"                      [--url URL] [--repeat RULE] [--alarm MINUTES]",
					},
					Long: "With no --end it lasts an hour, or a whole day for an all-day entry. " +
						"--repeat makes it one entry and a rule, not a row per week.",
					Flags: []Flag{
						{Name: "start", Kind: KindString, Arg: "WHEN", Desc: "2026-09-01, or 2026-09-01 14:00"},
						{Name: "end", Kind: KindString, Arg: "WHEN", Desc: "when it finishes"},
						{Name: "calendar", Kind: KindString, Arg: "NAME", Desc: "which calendar"},
						{Name: "location", Kind: KindString, Arg: "TEXT", Desc: "where it is"},
						{Name: "notes", Kind: KindString, Arg: "TEXT", Desc: "the description"},
						{Name: "url", Kind: KindString, Arg: "URL", Desc: "a link: the call, the ticket, the page"},
						{Name: "repeat", Kind: KindString, Arg: "RULE",
							Desc: "daily, weekly, biweekly, monthly, yearly, weekdays, or FREQ=…"},
						{Name: "alarm", Kind: KindString, Arg: "MINUTES",
							Desc: "remind this many minutes before: 15, or 10,60"},
						{Name: "all-day", Kind: KindBool, Desc: "an all-day entry even with a time given"},
					},
					Examples: []string{
						`mailbox event add Zahnarzt --start "2026-09-01 08:10" --end "2026-09-01 09:00"`,
						"mailbox event add Urlaub --start 2026-09-01 --end 2026-09-15",
						`mailbox event add Standup --start "2026-09-01 09:00" --repeat weekdays --alarm 5`,
						`mailbox event add Review --start "2026-09-01 10:00" --url https://meet.example.org/r`,
					},
					Run: eventVerb("add"),
				},
				{
					Name: "edit", Short: "Change an entry", Needs: true,
					Usage: []string{
						"mailbox event edit ID [--title TEXT] [--start WHEN] [--end WHEN]",
						"                      [--url URL] [--repeat RULE] [--alarm MINUTES]",
					},
					Long: "Only what is named changes. A repeating entry is one rule, so " +
						"editing its time moves every instance of it. --repeat none takes " +
						"the rule off, and --alarm none takes the reminders off.",
					Flags: []Flag{
						{Name: "title", Kind: KindString, Arg: "TEXT", Desc: "a new summary"},
						{Name: "start", Kind: KindString, Arg: "WHEN", Desc: "2026-09-01, or 2026-09-01 14:00"},
						{Name: "end", Kind: KindString, Arg: "WHEN", Desc: "when it finishes"},
						{Name: "location", Kind: KindString, Arg: "TEXT", Desc: "where it is"},
						{Name: "notes", Kind: KindString, Arg: "TEXT", Desc: "the description"},
						{Name: "url", Kind: KindString, Arg: "URL", Desc: "a link, or none to take it off"},
						{Name: "repeat", Kind: KindString, Arg: "RULE",
							Desc: "daily, weekly, monthly, yearly, weekdays, FREQ=…, or none"},
						{Name: "alarm", Kind: KindString, Arg: "MINUTES",
							Desc: "minutes before the start, or none"},
						{Name: "all-day", Kind: KindBool, Desc: "make it an all-day entry"},
					},
					Examples: []string{
						`mailbox event edit 41 --start "2026-09-02 08:10"`,
						"mailbox event edit 41 --repeat weekly --alarm 15",
					},
					Run: eventVerb("edit"),
				},
				{
					Name: "delete", Short: "Take an entry off", Needs: true,
					Usage:    []string{"mailbox event delete ID"},
					Examples: []string{"mailbox event delete 41"},
					Run:      eventVerb("delete"),
				},
			},
		},
		{
			Name: "todo", Section: SectionTime, Short: "The task lists",
			Sub: []*Command{
				{
					Name: "list", Short: "What is on the lists",
					Usage: []string{"mailbox todo list [--list NAME] [--all]"},
					Flags: []Flag{
						{Name: "list", Kind: KindString, Arg: "NAME", Desc: "which task list (default every one)"},
						{Name: "all", Kind: KindBool, Desc: "include what is already done"},
					},
					Run: todoVerb("list"),
				},
				{
					Name: "add", Short: "Add a todo", Needs: true,
					Usage: []string{"mailbox todo add TEXT [--list NAME] [--due WHEN] [--priority P]"},
					Long: "A bare date is a date: \"by Friday\" is not a promise about " +
						"midnight. Give it a time and it keeps the hour.",
					Flags: []Flag{
						{Name: "list", Kind: KindString, Arg: "NAME", Desc: "which task list"},
						{Name: "due", Kind: KindString, Arg: "WHEN",
							Desc: "when it is wanted: 2026-09-01, 2026-09-01 17:00, today, tomorrow"},
						{Name: "priority", Kind: KindString, Arg: "P", Desc: "high, medium or low"},
					},
					Examples: []string{
						"mailbox todo add Rechnung bezahlen",
						"mailbox todo add Rechnung bezahlen --due tomorrow",
						`mailbox todo add Abgabe --due "2026-09-01 17:00" --priority high`,
					},
					Run: todoVerb("add"),
				},
				{
					Name: "done", Short: "Mark a todo complete", Needs: true,
					Usage: []string{"mailbox todo done ID"}, Run: todoVerb("done"),
				},
				{
					Name: "undone", Short: "Mark a todo open again", Needs: true,
					Usage: []string{"mailbox todo undone ID"}, Run: todoVerb("undone"),
				},
				{
					Name: "rename", Short: "Change what a todo says", Needs: true,
					Usage: []string{"mailbox todo rename ID --title TEXT"},
					Flags: []Flag{
						{Name: "title", Kind: KindString, Arg: "TEXT", Desc: "the new text"},
					},
					Run: todoVerb("rename"),
				},
				{
					Name: "drop", Short: "Delete a todo", Needs: true,
					Usage: []string{"mailbox todo drop ID"}, Run: todoVerb("drop"),
				},
			},
		},
		{
			Name: "habit", Section: SectionTime, Short: "The daily practices",
			Long: "Completing a habit for a day does not end it, which is what makes it " +
				"neither an event nor a todo.",
			Sub: []*Command{
				{
					Name: "list", Short: "The habits, and today's streaks",
					Usage: []string{"mailbox habit list [--date DATE]"},
					Flags: []Flag{
						{Name: "date", Kind: KindString, Arg: "DATE", Desc: "which day (default today)"},
					},
					Run: habitVerb("list"),
				},
				{
					Name: "add", Short: "Create a habit", Needs: true,
					Usage: []string{"mailbox habit add NAME [--days mon,tue,…] [--color C] [--icon I]"},
					Flags: []Flag{
						{Name: "days", Kind: KindString, Arg: "DAYS", Desc: "the days it is due: mon,tue,wed,… (default every day)"},
						{Name: "color", Kind: KindString, Arg: "C", Desc: "a colour for the widget"},
						{Name: "icon", Kind: KindString, Arg: "I", Desc: "an icon for the widget"},
					},
					Examples: []string{"mailbox habit add Lesen", "mailbox habit add Laufen --days mon,wed,fri"},
					Run:      habitVerb("add"),
				},
				{
					Name: "edit", Short: "Change a habit", Needs: true,
					Usage: []string{"mailbox habit edit NAME [--title TEXT] [--days mon,tue,…] [--color C] [--icon I]"},
					Long: "Only what is named changes. Renaming a habit leaves its days and " +
						"its record of what was done where they were.",
					Flags: []Flag{
						{Name: "title", Kind: KindString, Arg: "TEXT", Desc: "a new name"},
						{Name: "days", Kind: KindString, Arg: "DAYS", Desc: "the days it is due: mon,tue,wed,…"},
						{Name: "color", Kind: KindString, Arg: "C", Desc: "a colour for the widget"},
						{Name: "icon", Kind: KindString, Arg: "I", Desc: "an icon for the widget"},
					},
					Examples: []string{"mailbox habit edit Lesen --days mon,wed,fri"},
					Run:      habitVerb("edit"),
				},
				{
					Name: "done", Short: "Tick a day", Needs: true,
					Usage: []string{"mailbox habit done NAME [--date DATE]"},
					Flags: []Flag{
						{Name: "date", Kind: KindString, Arg: "DATE", Desc: "which day (default today)"},
					},
					Run: habitVerb("done"),
				},
				{
					Name: "undone", Short: "Untick a day", Needs: true,
					Usage: []string{"mailbox habit undone NAME [--date DATE]"},
					Flags: []Flag{
						{Name: "date", Kind: KindString, Arg: "DATE", Desc: "which day (default today)"},
					},
					Run: habitVerb("undone"),
				},
				{
					Name: "drop", Short: "Delete a habit", Needs: true,
					Usage: []string{"mailbox habit drop NAME"}, Run: habitVerb("drop"),
				},
			},
		},

		// CONTACTS, in the same section: an address book is a collection on
		// the same server as a calendar, reached the same way.
		{
			Name: "contact", Section: SectionTime, Short: "The address books",
			Sub: []*Command{
				{
					Name: "list", Short: "Everyone in the address books",
					Usage: []string{"mailbox contact list [--book NAME] [--limit N]"},
					Flags: []Flag{
						{Name: "book", Kind: KindString, Arg: "NAME", Desc: "which address book"},
						{Name: "limit", Kind: KindInt, Arg: "N", Int: 25, Desc: "how many to show"},
					},
					Run: contactVerb("list"),
				},
				{
					Name: "search", Short: "Find somebody", Needs: true,
					Usage: []string{"mailbox contact search QUERY [--limit N]"},
					Flags: []Flag{
						{Name: "book", Kind: KindString, Arg: "NAME", Desc: "which address book"},
						{Name: "limit", Kind: KindInt, Arg: "N", Int: 25, Desc: "how many to show"},
					},
					Examples: []string{"mailbox contact search anna", "mailbox contact search example.com"},
					Run:      contactVerb("search"),
				},
				{
					Name: "view", Short: "One contact, whole", Needs: true,
					Usage: []string{"mailbox contact view ID"}, Run: contactVerb("view"),
				},
				{
					Name: "add", Short: "Add a contact", Needs: true,
					Usage: []string{"mailbox contact add NAME [--email ADDR] [--phone NUMBER] [--book NAME]"},
					Flags: []Flag{
						{Name: "email", Kind: KindList, Arg: "ADDR", Desc: "an address (repeatable)"},
						{Name: "phone", Kind: KindList, Arg: "NUMBER", Desc: "a number (repeatable)"},
						{Name: "org", Kind: KindString, Arg: "TEXT", Desc: "the organisation"},
						{Name: "note", Kind: KindString, Arg: "TEXT", Desc: "a note"},
						{Name: "book", Kind: KindString, Arg: "NAME", Desc: "which address book"},
					},
					Examples: []string{`mailbox contact add "Anna Beispiel" --email anna@example.com`},
					Run:      contactVerb("add"),
				},
				{
					Name: "update", Short: "Change what a contact says", Needs: true,
					Usage: []string{"mailbox contact update ID [--name TEXT] [--org TEXT] [--note TEXT]"},
					Long: "Only what is named changes, and addresses and numbers are left " +
						"alone — adding one of those is `contact email` or `contact phone`.",
					Flags: []Flag{
						{Name: "name", Kind: KindString, Arg: "TEXT", Desc: "a new name"},
						{Name: "org", Kind: KindString, Arg: "TEXT", Desc: "the organisation"},
						{Name: "note", Kind: KindString, Arg: "TEXT", Desc: "the note"},
					},
					Examples: []string{`mailbox contact update 12 --org "Beispiel GmbH"`},
					Run:      contactVerb("update"),
				},
				{
					Name: "email", Short: "Add an address to a contact", Needs: true,
					Usage: []string{"mailbox contact email ID --value ADDR"},
					Flags: []Flag{
						{Name: "value", Kind: KindString, Arg: "ADDR", Desc: "the address to add"},
					},
					Run: contactVerb("email"),
				},
				{
					Name: "phone", Short: "Add a number to a contact", Needs: true,
					Usage: []string{"mailbox contact phone ID --value NUMBER"},
					Flags: []Flag{
						{Name: "value", Kind: KindString, Arg: "NUMBER", Desc: "the number to add"},
					},
					Run: contactVerb("phone"),
				},
				{
					Name: "drop", Short: "Delete a contact", Needs: true,
					Usage: []string{"mailbox contact drop ID"}, Run: contactVerb("drop"),
				},
			},
		},

		// SYSTEM ─────────────────────────────────────────────────────────
		{
			Name: "status", Section: SectionSystem, Short: "What the mirror holds",
			Usage: []string{"mailbox status"},
			Long:  "One line per account: how many boxes are held, how much is in the inbox, and what is watched.",
			Run:   runStatus,
		},
		{
			Name: "daemon", Section: SectionSystem, Short: "Run the daemon in the foreground",
			Usage: []string{"mailbox daemon [--systemd-socket]"},
			Long: "It holds every connection to the servers and does not return; stop it " +
				"with Ctrl-C. Under socket activation nothing runs this by hand: the unit " +
				"passes --systemd-socket and the daemon takes the listening socket it was " +
				"given instead of binding one. That flag is an assertion — without a socket " +
				"passed in it fails rather than quietly binding a second one nobody is " +
				"connected to.",
			Flags: []Flag{
				{Name: "systemd-socket", Kind: KindBool, Desc: "take the listening socket from systemd"},
			},
			Local: true,
			Run:   localDaemon(l),
		},
		{
			Name: "setup", Section: SectionSystem, Short: "Set up this machine",
			Usage: []string{"mailbox setup"},
			Long: "On a machine with no config it asks for an account, writes the config, " +
				"the systemd units and the agent skill, starts the daemon and watches its " +
				"first sync. On a machine that is already set up it asks nothing: it shows " +
				"what is here and offers to add an account or a calendar, remove one, or " +
				"repair what has drifted. Nothing is overwritten, so there is no --force.",
			Local: true,
			Run:   localSetup(l),
		},
		{
			Name: "doctor", Section: SectionSystem, Short: "Find what is not working",
			Usage: []string{"mailbox doctor [--offline]"},
			Long: "Looks from both ends. The local half opens its own connections, because " +
				"a check that goes through the daemon cannot tell you the daemon is the " +
				"problem; the daemon half reports what it already holds, because that is " +
				"what every other command uses. Exits non-zero if anything failed.",
			Flags: []Flag{
				{Name: "offline", Kind: KindBool, Desc: "check the files only, dial nothing"},
			},
			Examples: []string{"mailbox doctor", "mailbox doctor --offline"},
			Local:    true,
			Run:      runDoctor,
		},
		{
			Name: "sieve", Section: SectionSystem, Short: "Scripts on the server",
			Long: "Raw access to the sieve scripts, for the cases `mailbox route` does not " +
				"cover. Note that route owns the script called \"logic\": putting over it " +
				"or activating something else is how triage stops working.",
			Sub: []*Command{
				{
					Name: "list", Short: "Scripts, and which one runs",
					Usage: []string{"mailbox sieve list"},
					Run:   runSieveList,
				},
				{
					Name: "get", Short: "Print a script",
					Usage: []string{"mailbox sieve get [NAME]"},
					Long:  "With no name it is the active one, which is what the server actually runs.",
					Examples: []string{
						"mailbox sieve get",
						"mailbox sieve get logic > logic.sieve",
					},
					Run: runSieveGet,
				},
				{
					Name: "put", Short: "Upload a script", Needs: true,
					Usage: []string{"mailbox sieve put NAME FILE"},
					Long: "FILE of - reads the script from stdin. The server compiles it and " +
						"refuses what it cannot, so an upload that succeeds is a script that " +
						"runs. It is stored, not activated.",
					Examples: []string{
						"mailbox sieve put logic logic.sieve",
						"cat logic.sieve | mailbox sieve put logic -",
					},
					Run: runSievePut,
				},
				{
					Name: "activate", Short: "Set the active script", Needs: true,
					Usage:    []string{"mailbox sieve activate NAME"},
					Examples: []string{"mailbox sieve activate logic"},
					Run:      runSieveActivate,
				},
			},
		},
		{
			Name: "commands", Section: SectionSystem, Short: "Machine command index",
			Usage: []string{"mailbox commands"},
			Long: "Every runnable command as JSON: its path, its usage, its flags and its " +
				"examples. This is the index to read rather than parsing help text.",
			Local: true,
			Run:   runCommands(l),
		},
		{
			Name: "version", Section: SectionSystem, Short: "Installed version",
			Usage: []string{"mailbox version"},
			Local: true,
			Run: func(in *input, stdout, stderr io.Writer) int {
				fmt.Fprintf(stdout, "mailbox %s\n", Version)
				return ExitOK
			},
		},
	}
}

func localDaemon(l Locals) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		if l.Daemon == nil {
			fmt.Fprintln(stderr, "this build cannot run the daemon")
			return ExitAPI
		}
		return l.Daemon(in.Bool("systemd-socket"), stdout, stderr)
	}
}

func localSetup(l Locals) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		if l.Setup == nil {
			fmt.Fprintln(stderr, "this build cannot run the wizard")
			return ExitAPI
		}
		return l.Setup(stdout, stderr)
	}
}

// Run dispatches a command line. It returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWith(Locals{}, args, stdout, stderr)
}

// RunWith is Run with the two commands that cannot go through the socket
// supplied by the caller.
func RunWith(l Locals, args []string, stdout, stderr io.Writer) int {
	root := tree(l)
	rest, opts, wantHelp := takeGlobals(args)

	if len(rest) == 0 {
		fmt.Fprint(stdout, overview(root, false))
		return ExitOK
	}
	if rest[0] == "help" {
		return runHelp(root, rest[1:], stdout, stderr)
	}

	node, path, words, code := walk(root, rest, stderr)
	if code != ExitOK {
		return code
	}
	if wantHelp || node.Run == nil {
		fmt.Fprint(stdout, page(node, path))
		return ExitOK
	}
	return execute(node, path, words, opts, stdout, stderr)
}

// takeGlobals lifts the flags that mean the same thing on every command off the
// command line, so no command declares them and none can mean something else by
// them. Everything after a bare `--` is left alone, which is the escape hatch
// for a subject or a body that begins with a dash.
func takeGlobals(args []string) (rest []string, o options, help bool) {
	rest = make([]string, 0, len(args))
	for i, a := range args {
		switch a {
		case "--":
			return append(rest, args[i:]...), o, help
		case "--json", "-json":
			o.json = true
		case "--help", "-help", "-h":
			help = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, o, help
}

// walk descends the tree as far as the arguments name commands, and reports
// what is left over. A group reached with nothing after it is not an error: the
// caller prints its index.
func walk(root []*Command, args []string, stderr io.Writer) (*Command, []string, []string, int) {
	node := find(root, args[0])
	if node == nil {
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, overview(root, false))
		return nil, nil, nil, ExitUsage
	}
	path := []string{node.Name}
	args = args[1:]
	for len(node.Sub) > 0 {
		if len(args) == 0 {
			return node, path, nil, ExitOK
		}
		child := find(node.Sub, args[0])
		if child == nil {
			if strings.HasPrefix(args[0], "-") {
				fmt.Fprintf(stderr, "%s takes a subcommand, not a flag\n\n", strings.Join(path, " "))
			} else {
				fmt.Fprintf(stderr, "%s has no %q\n\n", strings.Join(path, " "), args[0])
			}
			fmt.Fprint(stderr, page(node, path))
			return nil, nil, nil, ExitUsage
		}
		node, path, args = child, append(path, child.Name), args[1:]
	}
	return node, path, args, ExitOK
}

// singleDash matches a flag as Go's flag package writes one in an error: one
// dash, after a space or a colon, so a negative number in quotes is left alone.
var singleDash = regexp.MustCompile(`(^|[\s:])-([A-Za-z][\w-]*)`)

// redash writes a flag the way the help does. Go names one with a single dash
// whatever it was given, and a caller told "-limit" after reading "--limit"
// everywhere else has been told the tool has two spellings.
func redash(msg string) string {
	return singleDash.ReplaceAllString(msg, "$1--$2")
}

func find(in []*Command, name string) *Command {
	for _, c := range in {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// execute builds the command's FlagSet from what it declared, splits the words
// before the flags off the line, and runs it. Everything a command can be given
// passes through here, so a flag that is not in the registry is not a flag.
func execute(c *Command, path, args []string, opts options, stdout, stderr io.Writer) int {
	in := &input{
		json:  opts.json,
		bools: map[string]*bool{},
		strs:  map[string]*string{},
		ints:  map[string]*int{},
		lists: map[string]*repeated{},
	}
	fs := flag.NewFlagSet(strings.Join(path, " "), flag.ContinueOnError)
	// Go's own usage text names flags with one dash and knows nothing about
	// this tree, so it is silenced and the command's page is printed instead.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	for _, f := range c.Flags {
		switch f.Kind {
		case KindBool:
			in.bools[f.Name] = fs.Bool(f.Name, false, f.Desc)
		case KindString:
			in.strs[f.Name] = fs.String(f.Name, f.Str, f.Desc)
		case KindInt:
			in.ints[f.Name] = fs.Int(f.Name, f.Int, f.Desc)
		case KindList:
			r := &repeated{}
			fs.Var(r, f.Name, f.Desc)
			in.lists[f.Name] = r
		}
	}
	// Everything before the first flag is the words, so
	// `mailbox todo add Rechnung bezahlen --due tomorrow` reads the way it is
	// written.
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		in.Words, args = append(in.Words, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "%s: %s\n\n", strings.Join(path, " "), redash(err.Error()))
		fmt.Fprint(stderr, page(c, path))
		return ExitUsage
	}
	if c.Needs && len(in.Words) == 0 {
		fmt.Fprint(stderr, page(c, path))
		return ExitUsage
	}
	return c.Run(in, stdout, stderr)
}
