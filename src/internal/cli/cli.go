// Package cli ports src/mailbox_cli/cli.py (argparse surface, hand-rolled).
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"mailbox/src/internal/calendar"
	"mailbox/src/internal/config"
	"mailbox/src/internal/contacts"
	"mailbox/src/internal/doctor"
	"mailbox/src/internal/folders"
	"mailbox/src/internal/format"
	"mailbox/src/internal/ids"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/skill"
)

const usage = `mailbox — mailbox.org mail, Kontakte, Kalender, Aufgaben

Mail
  mailbox box list [--archive]
  mailbox box view NAME [--unread] [--limit N] [--detail]
  mailbox search [QUERY] [--from ADDR] [--to ADDR] [--subject TEXT] [--in BOX] [--limit N] [--detail]
  mailbox thread ID [--allow-partial]
  mailbox screener list [--unread] [--limit N] [--detail]
  mailbox screener approve ID... [--box inbox|feed|trail]
  mailbox screener deny ID... [--spam]
  mailbox move ID... --to inbox|feed|trail|block
  mailbox seen ID...
  mailbox unseen ID...
  mailbox trash ID...
  mailbox spam ID...
  mailbox compose --to ADDR --subject TEXT [-m TEXT | --message-html HTML] [--draft]
  mailbox draft list [--all] [--limit N]
  mailbox draft show ID
  mailbox draft edit ID [--to ADDR] [--subject TEXT] [-m TEXT]
  mailbox draft send ID
  mailbox draft delete ID
  mailbox attachment list ID
  mailbox attachment save ID [--output PATH] [--force]

Events  (Kalender)
  mailbox events [--start WHEN] [--end WHEN]
  mailbox events show ID
  mailbox events create --title TEXT --start WHEN [--end WHEN] [--all-day]

Tasks  (Aufgaben)
  mailbox tasks
  mailbox tasks create --title TEXT [--due WHEN]
  mailbox tasks complete ID

Contacts  (Kontakte)
  mailbox contacts list
  mailbox contacts search QUERY
  mailbox contacts refresh
  mailbox contacts show ID
  mailbox contacts add --name TEXT --email ADDR [--note TEXT]
  mailbox contacts update ID [--name TEXT] [--email ADDR] [--note TEXT]

Meta
  mailbox doctor
  mailbox commands
  mailbox skill install
  mailbox serve [--web] [--web-port N] [--interval S] [--print]
  mailbox help [output|exit-codes|environment]

Boxes: inbox, feed, trail, screener, archive, drafts, sent, or Archive/…
mailbox box list is routing boxes; --archive is the Archive tree. box view matches name or id (feed, Inbox/Feed).
Mail IDs look like INBOX/Screener:342. Event/task/contact IDs come from the list.
WHEN is YYYY-MM-DD or YYYY-MM-DDTHH:MM (Europe/Berlin).
--json envelope {ok, data}. --jq EXPR filters it (needs jq). --quiet --jq filters data.
--ids-only / --count skip the envelope. --markdown is a table or a thread document.
--html thread HTML (redirect to a file). --allow-partial for an incomplete thread.

Search is IMAP keyword (not semantic). --from/--to/--subject are IMAP FROM/TO/SUBJECT.
Default search covers Inbox/Feed/Paper Trail/Screener plus Archive.
Approve, deny, and move only IMAP-move; Sieve updates happen on the VPS.
compose sends via SMTP unless --draft. -m is Markdown; --message-html is raw HTML.
serve runs the mail routing service: watches Inbox/Feed/Paper Trail/Screener/Block
and updates the "logic" Sieve script (sieve host: MAILBOX_SIEVE_HOST/_PORT, default IMAP host:4190).
--web serves the list-management UI on :8080. Needs the same env as the mail verbs.
`

var helpTopics = map[string]string{
	"output": `Output

  --json          envelope {ok, data}; truncated/notice when a list or thread was cut
  --jq EXPR       filter that envelope (implies --json; needs the jq binary)
                  --quiet --jq filters data directly
  --quiet         JSON of data, no envelope
  --ids-only      one ID per line; truncation notice on stderr
  --count         a bare number; truncation notice on stderr
  --markdown      list as a table; thread as one document
  --html          original HTML for mailbox thread; redirect to a file
  --allow-partial incomplete thread is still a result

Use only one of --json, --ids-only, --count, --html, --markdown.
String --jq results print as plain text; objects and arrays print as JSON.
Errors keep the structured envelope with a code field (usage, auth, runtime); --jq is not applied to them.
`,
	"exit-codes": `Exit codes

  0  success
  1  runtime error (IMAP, CalDAV, incomplete thread without --allow-partial)
  2  usage error (unknown flag or box, missing argument, invalid --jq)
  3  credentials missing or unreadable

JSON errors carry a stable code field: usage (2), auth (3), runtime (1).
Any command accepts -h/--help for its own usage line; mailbox commands lists all.
`,
	"environment": `Environment

  MAILBOX_EMAIL
  MAILBOX_PASSWORD
  MAILBOX_IMAP_HOST
  MAILBOX_IMAP_PORT
  MAILBOX_SMTP_HOST
  MAILBOX_SMTP_PORT
  MAILBOX_CALDAV_KALENDER
  MAILBOX_CALDAV_AUFGABEN
  MAILBOX_CARDDAV_KONTAKTE
  MAILBOX_TB_HOME
  MAILBOX_TB_PROFILE

Reads the Windows Thunderbird profile when env is unset (newest prefs.js, or MAILBOX_TB_PROFILE).
`,
}

type col = [2]string

var (
	folderColumns = []col{{"id", "ID"}, {"imap", "IMAP"}, {"role", "Role"}}
	mailColumns   = []col{{"id", "ID"}, {"from", "From"}, {"summary", "Summary"}, {"date", "Date"}}
	mailColumnsD  = []col{{"id", "ID"}, {"from", "From"}, {"summary", "Summary"}, {"date", "Date"}, {"flags", "Flags"}}
	mailWidths    = map[string]int{"from": 24, "summary": 60}
	attachColumns = []col{{"id", "ID"}, {"name", "Name"}, {"type", "Type"}, {"size", "Size"}}
	draftColumns  = []col{{"id", "ID"}, {"to", "To"}, {"summary", "Summary"}, {"date", "Date"}}
	draftColumnsD = []col{{"id", "ID"}, {"to", "To"}, {"summary", "Summary"}, {"date", "Date"}, {"flags", "Flags"}}
	eventColumns  = []col{{"id", "ID"}, {"start", "Start"}, {"end", "End"}, {"summary", "Summary"}}
	taskColumns   = []col{{"id", "ID"}, {"due", "Due"}, {"status", "Status"}, {"summary", "Summary"}}
	contactCols   = []col{{"id", "ID"}, {"name", "Name"}, {"email", "Email"}, {"updated", "Updated"}}
)

const screenNote = "Routing updates are mailbox serve's job; next mail from this sender may still land in Screener."

// UsageError maps to python ValueError -> exit 2.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string     { return e.Msg }
func (e *UsageError) ErrorCode() string { return "usage" }

func usageErr(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

func Main(argv []string) int {
	argv, out, err := format.TakeOutputFlags(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	out.TTY = isTTY(os.Stdout)
	if len(argv) == 0 || sameStrings(argv, []string{"-h"}) || sameStrings(argv, []string{"--help"}) || sameStrings(argv, []string{"help"}) {
		fmt.Print(usage)
		return 0
	}
	if argv[0] == "help" {
		return helpTopic(argv[1:])
	}

	rc, err := dispatch(argv, out)
	if err != nil {
		code := "runtime"
		var coded interface{ ErrorCode() string }
		if errors.As(err, &coded) {
			code = coded.ErrorCode()
		}
		if out.JSON || out.Quiet {
			fmt.Println(format.DumpJSON(format.NewOM("ok", false, "code", code, "error", err.Error())))
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		switch code {
		case "usage":
			return 2
		case "auth":
			return 3
		}
		return 1
	}
	return rc
}

func dispatch(argv []string, out *format.Output) (int, error) {
	if i := helpFlagIndex(argv); i >= 0 {
		lines := usageFor(argv[:i])
		if len(lines) == 0 {
			fmt.Fprintf(os.Stderr, "no command matches %q\n", strings.Join(argv[:i], " "))
			return 2, nil
		}
		fmt.Println(strings.Join(lines, "\n"))
		return 0, nil
	}

	group := argv[0]
	rest := argv[1:]
	switch group {
	case "box":
		sub, flags, err := subcommand(rest, "box")
		if err != nil {
			return 0, err
		}
		return cmdBox(sub, flags, out)
	case "search":
		flags, err := parseFlags(flagSpec("search", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdSearch(flags, out)
	case "thread":
		flags, err := parseFlags(noFlags, rest)
		if err != nil {
			return 0, err
		}
		if len(flags.positional) != 1 {
			return printUsage("thread"), nil
		}
		return cmdThread(flags.positional[0], out)
	case "screener":
		sub, flags, err := subcommand(rest, "screener")
		if err != nil {
			return 0, err
		}
		return cmdScreener(sub, flags, out)
	case "move":
		flags, err := parseFlags(flagSpec("move", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdMove(flags, out)
	case "seen", "unseen":
		flags, err := parseFlags(noFlags, rest)
		if err != nil {
			return 0, err
		}
		return cmdSeen(group, flags.positional, out)
	case "trash":
		flags, err := parseFlags(noFlags, rest)
		if err != nil {
			return 0, err
		}
		return imapMove(flags.positional, folders.TRASH, out, "")
	case "spam":
		flags, err := parseFlags(noFlags, rest)
		if err != nil {
			return 0, err
		}
		return imapMove(flags.positional, folders.JUNK, out, "")
	case "compose":
		flags, err := parseFlags(flagSpec("compose", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdCompose(flags, out)
	case "draft":
		sub, flags, err := subcommand(rest, "draft")
		if err != nil {
			return 0, err
		}
		return cmdDraft(sub, flags, out)
	case "attachment":
		sub, flags, err := subcommand(rest, "attachment")
		if err != nil {
			return 0, err
		}
		return cmdAttachment(sub, flags, out)
	case "events":
		return cmdEvents(rest, out)
	case "tasks":
		return cmdTasks(rest, out)
	case "contacts":
		sub, flags, err := subcommand(rest, "contacts")
		if err != nil {
			return 0, err
		}
		return cmdContact(sub, flags, out)
	case "doctor":
		return cmdDoctor(out), nil
	case "commands":
		return cmdCommands(out)
	case "skill":
		sub, _, err := subcommand(rest, "skill")
		if err != nil {
			return 0, err
		}
		return cmdSkill(sub, out)
	case "serve":
		flags, err := parseFlags(flagSpec("serve", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdServe(flags, out)
	default:
		fmt.Print(usage)
		return 2, nil
	}
}

type set = map[string]bool

func newSet(keys ...string) set {
	m := set{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func helpTopic(args []string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return 0
	}
	text, ok := helpTopics[args[0]]
	if !ok {
		names := make([]string, 0, len(helpTopics))
		for k := range helpTopics {
			names = append(names, k)
		}
		fmt.Fprintf(os.Stderr, "unknown help topic %q; use %s\n", args[0], strings.Join(names, ", "))
		return 2
	}
	fmt.Print(text)
	return 0
}

func limitOf(flags *parsed, def int) (int, error) {
	raw := ""
	if flags.has("limit") || flags.one("limit") != "" {
		raw = flags.one("limit")
	}
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, usageErr("--limit must be an integer")
	}
	return n, nil
}

func limitPtr(n int) *int { return &n }

// --- flag parsing (strict: unknown flags are usage errors) ---

type flagspec struct{ bools, vals set }

var noFlags = flagspec{newSet(), newSet()}

// Output flags (--json, --jq, ...) are stripped earlier by format.TakeOutputFlags;
// these tables cover the remaining domain flags per command (or per group union).
var cmdFlagSpecs = map[string]flagspec{
	"box":              {newSet("archive", "unread", "detail"), newSet("limit")},
	"screener list":    {newSet("unread", "detail"), newSet("limit")},
	"screener approve": {nil, newSet("box")},
	"screener deny":    {newSet("spam"), nil},
	"search":           {newSet("detail"), newSet("from", "to", "subject", "in", "limit")},
	"move":             {nil, newSet("to")},
	"compose":          {newSet("draft"), newSet("to", "cc", "bcc", "subject", "m", "message-html", "attach", "reply-to")},
	"draft list":       {newSet("all", "unread", "detail"), newSet("limit")},
	"draft edit":       {nil, newSet("to", "cc", "bcc", "subject", "m", "message-html")},
	"attachment save":  {newSet("force"), newSet("output")},
	"events":           {newSet("all-day"), newSet("start", "end", "title")},
	"tasks":            {nil, newSet("title", "due")},
	"contacts":         {nil, newSet("name", "email", "note")},
	"serve":            {newSet("web", "print"), newSet("web-port", "interval")},
}

func flagSpec(group, sub string) flagspec {
	if s, ok := cmdFlagSpecs[group+" "+sub]; ok {
		return s
	}
	if s, ok := cmdFlagSpecs[group]; ok {
		return s
	}
	return noFlags
}

type parsed struct {
	positional []string
	flags      map[string][]string // value flags
	bools      map[string]bool
}

func (p *parsed) one(key string) string {
	if v, ok := p.flags[key]; ok && len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

func (p *parsed) has(key string) bool {
	if _, ok := p.flags[key]; ok {
		return true
	}
	return p.bools[key]
}

func (p *parsed) list(key string) []string {
	var out []string
	if vs, ok := p.flags[key]; ok {
		out = append(out, vs...)
	}
	if p.bools[key+"-bare"] {
		out = append(out, "")
	}
	return out
}

func parseFlags(spec flagspec, tokens []string) (*parsed, error) {
	p := &parsed{flags: map[string][]string{}, bools: map[string]bool{}}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		takeValue := func(key string) {
			i++
			v := ""
			if i < len(tokens) {
				v = tokens[i]
			}
			p.flags[key] = append(p.flags[key], v)
		}
		if strings.HasPrefix(tok, "--") {
			name := tok[2:]
			if eq := strings.Index(name, "="); eq >= 0 {
				key := name[:eq]
				if !spec.vals[key] {
					return nil, usageErr("unknown flag --%s", key)
				}
				p.flags[key] = append(p.flags[key], name[eq+1:])
				continue
			}
			if spec.bools[name] {
				p.bools[name] = true
				continue
			}
			if spec.vals[name] {
				takeValue(name)
				continue
			}
			return nil, usageErr("unknown flag --%s", name)
		}
		if len(tok) > 1 && tok[0] == '-' {
			short := tok[1:]
			if spec.vals[short] {
				takeValue(short)
				continue
			}
			return nil, usageErr("unknown flag %s", tok)
		}
		p.positional = append(p.positional, tok)
	}
	return p, nil
}

func subcommand(tokens []string, group string) (string, *parsed, error) {
	subs := map[string][]string{
		"box":        {"list", "view"},
		"screener":   {"list", "approve", "deny"},
		"draft":      {"list", "show", "edit", "send", "delete"},
		"attachment": {"list", "save"},
		"contacts":   {"list", "show", "add", "update", "search"},
		"skill":      {"install"},
	}[group]
	sub, rest := "", tokens
	if len(tokens) > 0 && containsStr(subs, tokens[0]) {
		sub, rest = tokens[0], tokens[1:]
	}
	p, err := parseFlags(flagSpec(group, sub), rest)
	return sub, p, err
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func helpFlagIndex(argv []string) int {
	for i, a := range argv {
		if a == "-h" || a == "--help" {
			return i
		}
	}
	return -1
}

// --- per-command help (usage lines come from the commands catalog) ---

type cmdspec struct {
	path  []string
	usage string
	flags []string
}

func prefixOf(prefix, full []string) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i, v := range prefix {
		if v != full[i] {
			return false
		}
	}
	return true
}

// usageFor lists catalog entries the given argv is a prefix of, so
// `mailbox box --help` shows every box subcommand.
func usageFor(argv []string) []string {
	var lines []string
	for _, s := range cmdSpecs {
		if prefixOf(argv, s.path) {
			lines = append(lines, s.usage)
		}
	}
	return lines
}

func printUsage(path ...string) int {
	for _, s := range cmdSpecs {
		if sameStrings(s.path, path) {
			fmt.Printf("usage: %s\n", s.usage)
			return 2
		}
	}
	fmt.Print(usage)
	return 2
}

// --- mail commands ---

func withMail(fn func(*mail.Mail) (int, error)) (int, error) {
	acct, err := config.LoadAccount(false, false)
	if err != nil {
		return 0, err
	}
	m := mail.New(acct)
	defer m.Close()
	if err := m.Connect(); err != nil {
		return 0, err
	}
	return fn(m)
}

func envRow(e *mail.Envelope) *format.OM {
	return format.NewOM(
		"id", e.ID(),
		"from", e.FromShort(),
		"summary", e.Summary(),
		"date", e.Date,
		"subject", e.Subject,
		"flags", strings.Join(e.Flags, ","),
	)
}

func draftRowFn(e *mail.Envelope) *format.OM {
	return format.NewOM(
		"id", e.ID(),
		"to", e.To,
		"summary", e.Summary(),
		"date", e.Date,
		"subject", e.Subject,
		"from", e.FromShort(),
		"flags", strings.Join(e.Flags, ","),
	)
}

func mailColumnsFor(detail bool) []col {
	if detail {
		return mailColumnsD
	}
	return mailColumns
}

func cmdBox(cmd string, flags *parsed, out *format.Output) (int, error) {
	return withMail(func(m *mail.Mail) (int, error) {
		names, err := m.ListFolders()
		if err != nil {
			return 0, err
		}
		if cmd == "list" {
			rows := []*format.OM{}
			for _, r := range folders.FolderCatalog(names, flags.has("archive")) {
				rows = append(rows, format.NewOM("id", r.ID, "imap", r.IMAP, "role", r.Role))
			}
			return format.WriteList(rows, folderColumns, out), nil
		}
		if len(flags.positional) != 1 {
			return printUsage("box", "view"), nil
		}
		folder, err := folders.ResolveFolder(flags.positional[0], names)
		if err != nil {
			return 0, err
		}
		limit, err := limitOf(flags, 50)
		if err != nil {
			return 0, err
		}
		listing, err := m.ListMessages(folder, flags.has("unread"), limitPtr(limit))
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(listing.Items))
		for _, e := range listing.Items {
			rows = append(rows, envRow(e))
		}
		return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, limitPtr(limit), mailWidths), nil
	})
}

func cmdSearch(flags *parsed, out *format.Output) (int, error) {
	query := mail.SearchQuery{
		Text:    "",
		From:    flags.one("from"),
		To:      flags.one("to"),
		Subject: flags.one("subject"),
	}
	if len(flags.positional) > 0 {
		query.Text = strings.Join(flags.positional, " ")
	}
	if query.Empty() {
		return 0, usageErr("search needs QUERY or --from/--to/--subject")
	}
	limit, err := limitOf(flags, 50)
	if err != nil {
		return 0, err
	}
	return withMail(func(m *mail.Mail) (int, error) {
		listing, err := m.Search(query, limit, flags.one("in"))
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(listing.Items))
		for _, e := range listing.Items {
			rows = append(rows, envRow(e))
		}
		return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, limitPtr(limit), mailWidths), nil
	})
}

func threadRow(msg *mail.ThreadMessage, htmlOut bool) *format.OM {
	atts := make([]*format.OM, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		atts = append(atts, format.NewOM(
			"index", a.Index, "name", a.Name, "type", a.ContentType, "size", a.Size,
		))
	}
	row := format.NewOM(
		"id", msg.ID(),
		"from", msg.From,
		"to", msg.To,
		"cc", msg.Cc,
		"bcc", msg.Bcc,
		"date", msg.Date,
		"subject", msg.Subject,
		"message-id", msg.MessageID,
		"body_state", msg.BodyState,
		"attachments", atts,
		"body", msg.Body,
	)
	if htmlOut {
		row.Set("body_html", msg.BodyHTML)
	}
	return row
}

func cmdThread(id string, out *format.Output) (int, error) {
	folder, uid, err := ids.ParseMessageID(id)
	if err != nil {
		return 0, err
	}
	return withMail(func(m *mail.Mail) (int, error) {
		walk, err := m.Thread(folder, uid)
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(walk.Messages))
		for _, msg := range walk.Messages {
			rows = append(rows, threadRow(msg, out.HTML))
		}
		return format.WriteThread(rows, out, walk.Truncated, walk.Notice), nil
	})
}

func cmdScreener(cmd string, flags *parsed, out *format.Output) (int, error) {
	if cmd == "list" || cmd == "" {
		limit, err := limitOf(flags, 50)
		if err != nil {
			return 0, err
		}
		if flags.has("count") {
			n, err := withMailCount("screener", flags.has("unread"))
			return n, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			listing, err := m.ListMessages("screener", flags.has("unread"), limitPtr(limit))
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(listing.Items))
			for _, e := range listing.Items {
				rows = append(rows, envRow(e))
			}
			return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, limitPtr(limit), mailWidths), nil
		})
	}
	if cmd == "approve" {
		destName := flags.one("box")
		if destName == "" {
			destName = "inbox"
		}
		dest, err := folders.ResolveScreenTarget(destName)
		if err != nil {
			return 0, err
		}
		if dest == folders.BLOCK {
			return 0, usageErr("approve --box cannot be block; use deny")
		}
		return imapMove(flags.positional, dest, out, screenNote)
	}
	// deny
	return imapMove(flags.positional, folders.BLOCK, out, screenNote)
}

func withMailCount(folder string, unread bool) (int, error) {
	return withMail(func(m *mail.Mail) (int, error) {
		return m.CountMessages(folder, unread)
	})
}

func cmdMove(flags *parsed, out *format.Output) (int, error) {
	dest, err := folders.ResolveScreenTarget(flags.one("to"))
	if err != nil {
		return 0, err
	}
	return imapMove(flags.positional, dest, out, screenNote)
}

func imapMove(idList []string, dest string, out *format.Output, note string) (int, error) {
	type pair struct{ folder, uid string }
	pairs := make([]pair, 0, len(idList))
	for _, mid := range idList {
		folder, uid, err := ids.ParseMessageID(mid)
		if err != nil {
			return 0, err
		}
		pairs = append(pairs, pair{folder, uid})
	}
	return withMail(func(m *mail.Mail) (int, error) {
		var rows []*format.OM
		for _, p := range pairs {
			newID, err := m.Screen(p.folder, p.uid, dest)
			if err != nil {
				return 0, err
			}
			row := format.NewOM("id", newID, "from", ids.FormatMessageID(p.folder, p.uid), "to", dest)
			if note != "" {
				row.Set("note", note)
			}
			rows = append(rows, row)
		}
		if len(rows) == 1 {
			return format.WriteOK(rows[0], out, ""), nil
		}
		return format.WriteOK(rows, out, ""), nil
	})
}

func cmdSeen(group string, idList []string, out *format.Output) (int, error) {
	seen := group == "seen"
	type pair struct{ folder, uid string }
	pairs := make([]pair, 0, len(idList))
	for _, mid := range idList {
		folder, uid, err := ids.ParseMessageID(mid)
		if err != nil {
			return 0, err
		}
		pairs = append(pairs, pair{folder, uid})
	}
	return withMail(func(m *mail.Mail) (int, error) {
		var rows []*format.OM
		for _, p := range pairs {
			if _, err := m.SetSeen(p.folder, p.uid, seen); err != nil {
				return 0, err
			}
			rows = append(rows, format.NewOM("id", ids.FormatMessageID(p.folder, p.uid), "seen", seen))
		}
		if len(rows) == 1 {
			return format.WriteOK(rows[0], out, ""), nil
		}
		return format.WriteOK(rows, out, ""), nil
	})
}

func splitAddrs(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func composeBody(message, messageHTML string, stdin io.Reader, tty bool) (string, string, error) {
	if message != "" && messageHTML != "" {
		return "", "", usageErr("-m and --message-html are mutually exclusive")
	}
	if messageHTML != "" {
		return "", messageHTML, nil
	}
	if message != "" {
		return message, "", nil
	}
	if !tty {
		data, _ := io.ReadAll(stdin)
		return string(data), "", nil
	}
	text, err := editInEditor()
	if err != nil {
		return "", "", err
	}
	return text, "", nil
}

func editInEditor() (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return "", usageErr("compose needs -m, --message-html, stdin, or $EDITOR")
	}
	tmp, err := os.CreateTemp("", "mailbox-*.md")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	parts := strings.Fields(editor)
	cmdParts := append(parts, path)
	proc := exec.Command(cmdParts[0], cmdParts[1:]...)
	proc.Stdin, proc.Stdout, proc.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := proc.Run(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

func checkCompose(flags *parsed) error {
	to := splitAddrs(flags.list("to"))
	reply := flags.one("reply-to")
	draft := flags.has("draft")
	if !draft && len(to) == 0 && reply == "" {
		return usageErr("compose needs --to or --reply-to")
	}
	if flags.one("subject") == "" && reply == "" {
		return usageErr("compose needs --subject")
	}
	if flags.has("m") && flags.has("message-html") {
		return usageErr("-m and --message-html are mutually exclusive")
	}
	return nil
}

func cmdCompose(flags *parsed, out *format.Output) (int, error) {
	if err := checkCompose(flags); err != nil {
		return 0, err
	}
	body, htmlBody, err := composeBody(flags.one("m"), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
	if err != nil {
		return 0, err
	}
	var attachments []mail.OutAttachment
	for _, path := range flags.list("attach") {
		att, err := mail.ReadAttachmentFile(path)
		if err != nil {
			return 0, err
		}
		attachments = append(attachments, att)
	}
	outgoing := &mail.Outgoing{
		To:          splitAddrs(flags.list("to")),
		Cc:          splitAddrs(flags.list("cc")),
		Bcc:         splitAddrs(flags.list("bcc")),
		Subject:     flags.one("subject"),
		Body:        body,
		HTML:        htmlBody,
		Attachments: attachments,
	}
	if reply := flags.one("reply-to"); reply != "" {
		folder, uid, err := ids.ParseMessageID(reply)
		if err != nil {
			return 0, err
		}
		outgoing.ReplyTo = &[2]string{folder, uid}
	}
	draftFlag := flags.has("draft")
	return withMail(func(m *mail.Mail) (int, error) {
		newID, err := m.Compose(outgoing, draftFlag)
		if err != nil {
			return 0, err
		}
		folderName := folders.SENT
		if draftFlag {
			folderName = folders.DRAFTS
		}
		return format.WriteOK(format.NewOM("id", newID, "folder", folderName), out, ""), nil
	})
}

func draftID(value string) (string, string, error) {
	folder, uid, err := ids.ParseMessageID(value)
	if err != nil {
		return "", "", err
	}
	resolved, err := folders.ResolveFolder(folder, nil)
	if err != nil {
		return "", "", err
	}
	if resolved != folders.DRAFTS {
		return "", "", usageErr("draft id must be in Drafts, got %q", value)
	}
	return resolved, uid, nil
}

func cmdDraft(cmd string, flags *parsed, out *format.Output) (int, error) {
	if cmd == "list" || cmd == "" {
		limit, err := limitOf(flags, 50)
		if err != nil {
			return 0, err
		}
		var lim *int
		if !flags.has("all") {
			lim = limitPtr(limit)
		}
		return withMail(func(m *mail.Mail) (int, error) {
			if out.Count {
				n, err := m.CountMessages(folders.DRAFTS, flags.has("unread"))
				if err != nil {
					return 0, err
				}
				fmt.Println(n)
				return 0, nil
			}
			listing, err := m.ListMessages(folders.DRAFTS, flags.has("unread"), lim)
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(listing.Items))
			for _, e := range listing.Items {
				rows = append(rows, draftRowFn(e))
			}
			cols := draftColumns
			if flags.has("detail") {
				cols = draftColumnsD
			}
			truncLim := limitPtr(limit)
			if lim == nil {
				truncLim = nil
			}
			return format.WriteList(rows, cols, out, listing.Truncated, truncLim, mailWidths), nil
		})
	}
	if len(flags.positional) != 1 {
		return printUsage("draft", cmd), nil
	}
	id := flags.positional[0]
	switch cmd {
	case "show":
		folder, uid, err := draftID(id)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			msg, err := m.Message(folder, uid)
			if err != nil {
				return 0, err
			}
			return format.WriteThread([]*format.OM{threadRow(msg, false)}, out, false, ""), nil
		})
	case "edit":
		folder, uid, err := draftID(id)
		if err != nil {
			return 0, err
		}
		hasAny := flags.has("to") || flags.has("cc") || flags.has("bcc") ||
			flags.has("subject") || flags.has("m") || flags.has("message-html")
		if !hasAny {
			return 0, usageErr("draft edit needs --to, --cc, --bcc, --subject, -m, or --message-html")
		}
		var bodyPtr, htmlPtr *string
		if flags.has("m") || flags.has("message-html") {
			body, htmlBody, err := composeBody(flags.one("m"), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
			if err != nil {
				return 0, err
			}
			bodyPtr, htmlPtr = &body, &htmlBody
		}
		strPtr := func(k string) *string {
			if !flags.has(k) {
				return nil
			}
			v := flags.one(k)
			return &v
		}
		listOrNil := func(k string) []string {
			if !flags.has(k) {
				return nil
			}
			return splitAddrs(flags.list(k))
		}
		return withMail(func(m *mail.Mail) (int, error) {
			newID, err := m.EditDraft(folder, uid,
				listOrNil("to"), listOrNil("cc"), listOrNil("bcc"),
				strPtr("subject"), bodyPtr, htmlPtr)
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", newID, "folder", folders.DRAFTS), out, ""), nil
		})
	case "send":
		folder, uid, err := draftID(id)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			newID, err := m.SendDraft(folder, uid)
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", newID, "folder", folders.SENT), out, ""), nil
		})
	default: // delete
		return imapMove([]string{id}, folders.TRASH, out, "")
	}
}

func attachmentDest(name, output string, force bool) (string, error) {
	filename := filepath.Base(name)
	if filename == "." || filename == "/" || filename == "" {
		filename = "attachment"
	}
	dest := filename
	if output != "" {
		if fi, err := os.Stat(output); err == nil && fi.IsDir() || strings.HasSuffix(output, "/") || strings.HasSuffix(output, "\\") {
			dest = filepath.Join(output, filename)
		} else {
			dest = output
		}
	}
	if _, err := os.Stat(dest); err == nil && !force {
		return "", fmt.Errorf("%s exists; pass --force", dest)
	}
	return dest, nil
}

func cmdAttachment(cmd string, flags *parsed, out *format.Output) (int, error) {
	if len(flags.positional) != 1 {
		return printUsage("attachment", cmd), nil
	}
	id := flags.positional[0]
	if cmd == "list" {
		folder, uid, err := ids.ParseMessageID(id)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			walk, err := m.Thread(folder, uid)
			if err != nil {
				return 0, err
			}
			if walk.Truncated && !out.AllowPartial {
				msg := walk.Notice
				if msg == "" {
					msg = "thread is incomplete; pass --allow-partial"
				}
				return 0, fmt.Errorf("%s", msg)
			}
			rows := []*format.OM{}
			for _, msg := range walk.Messages {
				for _, att := range msg.Attachments {
					rows = append(rows, format.NewOM(
						"id", ids.FormatAttachmentID(msg.Folder, msg.UID, att.Index),
						"name", att.Name,
						"type", att.ContentType,
						"size", att.Size,
						"message", msg.ID(),
					))
				}
			}
			return format.WriteList(rows, attachColumns, out), nil
		})
	}
	// save
	folder, uid, index, err := ids.ParseAttachmentID(id)
	if err != nil {
		return 0, err
	}
	return withMail(func(m *mail.Mail) (int, error) {
		att, blob, err := m.Attachment(folder, uid, index)
		if err != nil {
			return 0, err
		}
		dest, err := attachmentDest(att.Name, flags.one("output"), flags.has("force"))
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(dest, blob, 0o644); err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM(
			"id", ids.FormatAttachmentID(folder, uid, att.Index),
			"name", att.Name,
			"path", dest,
		), out, ""), nil
	})
}

// --- calendar / tasks / contacts ---

func cmdEvents(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(flagSpec("events", ""), args)
	if err != nil {
		return 0, err
	}
	acct, err := config.LoadAccount(true, false)
	if err != nil {
		return 0, err
	}
	cal, err := calendar.NewCal(acct)
	if err != nil {
		return 0, err
	}
	if flags.has("title") { // events create
		if flags.one("start") == "" {
			return 0, usageErr("events create needs --start")
		}
		uid, err := cal.CreateEvent(flags.one("title"), flags.one("start"), flags.one("end"), flags.has("all-day"))
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("id", uid, "calendar", "Kalender"), out, ""), nil
	}
	if len(flags.positional) >= 1 && flags.positional[0] == "show" {
		if len(flags.positional) < 2 {
			return 0, usageErr("events show needs ID")
		}
		row, err := cal.Event(flags.positional[1])
		if err != nil {
			return 0, err
		}
		return format.WriteOK(row, out, ""), nil
	}
	if len(flags.positional) > 0 {
		return 0, usageErr("unknown events command %q", flags.positional[0])
	}
	rows, err := cal.Events(flags.one("start"), flags.one("end"))
	if err != nil {
		return 0, err
	}
	return format.WriteList(rows, eventColumns, out), nil
}

func cmdTasks(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(flagSpec("tasks", ""), args)
	if err != nil {
		return 0, err
	}
	acct, err := config.LoadAccount(true, false)
	if err != nil {
		return 0, err
	}
	cal, err := calendar.NewCal(acct)
	if err != nil {
		return 0, err
	}
	if flags.has("title") {
		uid, err := cal.CreateTask(flags.one("title"), flags.one("due"))
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("id", uid, "calendar", "Aufgaben"), out, ""), nil
	}
	if len(flags.positional) >= 1 && flags.positional[0] == "complete" {
		if len(flags.positional) < 2 {
			return 0, usageErr("tasks complete needs ID")
		}
		if err := cal.CompleteTask(flags.positional[1]); err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("id", flags.positional[1], "status", "COMPLETED"), out, ""), nil
	}
	rows, err := cal.Tasks()
	if err != nil {
		return 0, err
	}
	return format.WriteList(rows, taskColumns, out), nil
}

func cmdContact(cmd string, flags *parsed, out *format.Output) (int, error) {
	acct, err := config.LoadAccount(false, true)
	if err != nil {
		return 0, err
	}
	book, err := contacts.New(acct)
	if err != nil {
		return 0, err
	}
	switch cmd {
	case "add":
		name := flags.one("name")
		email := flags.one("email")
		if name == "" || email == "" {
			return 0, usageErr("contacts add needs --name and --email")
		}
		uid, err := book.Add(name, email, flags.one("note"))
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("id", uid, "addressbook", "Kontakte"), out, ""), nil
	case "refresh":
		n, err := book.Refresh()
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("addressbook", "Kontakte", "count", n), out, ""), nil
	case "show":
		if len(flags.positional) != 1 {
			return 0, usageErr("contacts show needs ID")
		}
		row, err := book.Show(flags.positional[0])
		if err != nil {
			return 0, err
		}
		return format.WriteOK(row, out, ""), nil
	case "search":
		if len(flags.positional) != 1 {
			return 0, usageErr("contacts search needs QUERY")
		}
		rows, err := book.Search(flags.positional[0])
		if err != nil {
			return 0, err
		}
		return format.WriteList(rows, contactCols, out), nil
	case "update":
		if len(flags.positional) != 1 {
			return 0, usageErr("contacts update needs ID")
		}
		name, email, note := flags.one("name"), flags.one("email"), flags.one("note")
		if !flags.has("name") && !flags.has("email") && !flags.has("note") {
			return 0, usageErr("contacts update needs --name, --email, or --note")
		}
		strPtr := func(s string, present bool) *string {
			if !present {
				return nil
			}
			return &s
		}
		row, err := book.Update(flags.positional[0],
			strPtr(name, flags.has("name")),
			strPtr(email, flags.has("email")),
			strPtr(note, flags.has("note")))
		if err != nil {
			return 0, err
		}
		return format.WriteOK(row, out, ""), nil
	default: // list
		rows, err := book.List()
		if err != nil {
			return 0, err
		}
		return format.WriteList(rows, contactCols, out), nil
	}
}

// --- meta ---

func cmdDoctor(out *format.Output) int {
	report := doctor.Run(nil, nil)
	format.WriteOK(report.AsDict(), out, "")
	if report.OK() {
		return 0
	}
	return 1
}

var cmdSpecs = []cmdspec{
	{[]string{"box", "list"}, "mailbox box list [--archive]", []string{"--archive"}},
	{[]string{"box", "view"}, "mailbox box view NAME [--unread] [--limit N] [--detail]", []string{"--unread", "--limit", "--detail"}},
	{[]string{"search"}, "mailbox search [QUERY] [--from ADDR] [--to ADDR] [--subject TEXT] [--in BOX] [--limit N] [--detail]", []string{"--from", "--to", "--subject", "--in", "--limit", "--detail"}},
	{[]string{"thread"}, "mailbox thread ID [--allow-partial] [--html]", []string{"--allow-partial", "--html"}},
	{[]string{"screener", "list"}, "mailbox screener list [--unread] [--limit N] [--detail]", []string{"--unread", "--limit", "--detail"}},
	{[]string{"screener", "approve"}, "mailbox screener approve ID... [--box inbox|feed|trail]", []string{"--box"}},
	{[]string{"screener", "deny"}, "mailbox screener deny ID... [--spam]", []string{"--spam"}},
	{[]string{"move"}, "mailbox move ID... --to inbox|feed|trail|block", []string{"--to"}},
	{[]string{"seen"}, "mailbox seen ID...", nil},
	{[]string{"unseen"}, "mailbox unseen ID...", nil},
	{[]string{"trash"}, "mailbox trash ID...", nil},
	{[]string{"spam"}, "mailbox spam ID...", nil},
	{[]string{"compose"}, "mailbox compose --to ADDR --subject TEXT [-m TEXT]", []string{"--to", "--cc", "--bcc", "--subject", "-m", "--message-html", "--attach", "--reply-to", "--draft"}},
	{[]string{"draft", "list"}, "mailbox draft list [--all] [--limit N]", []string{"--all", "--limit", "--detail"}},
	{[]string{"draft", "show"}, "mailbox draft show ID", nil},
	{[]string{"draft", "edit"}, "mailbox draft edit ID [--to ADDR] [--subject TEXT] [-m TEXT]", []string{"--to", "--cc", "--bcc", "--subject", "-m", "--message-html"}},
	{[]string{"draft", "send"}, "mailbox draft send ID", nil},
	{[]string{"draft", "delete"}, "mailbox draft delete ID", nil},
	{[]string{"attachment", "list"}, "mailbox attachment list ID", nil},
	{[]string{"attachment", "save"}, "mailbox attachment save ID [--output PATH] [--force]", []string{"--output", "--force"}},
	{[]string{"events"}, "mailbox events [--start WHEN] [--end WHEN]", []string{"--start", "--end"}},
	{[]string{"events", "show"}, "mailbox events show ID", nil},
	{[]string{"events", "create"}, "mailbox events create --title TEXT --start WHEN [--end WHEN] [--all-day]", []string{"--title", "--start", "--end", "--all-day"}},
	{[]string{"tasks"}, "mailbox tasks", nil},
	{[]string{"tasks", "create"}, "mailbox tasks create --title TEXT [--due WHEN]", []string{"--title", "--due"}},
	{[]string{"tasks", "complete"}, "mailbox tasks complete ID", nil},
	{[]string{"contacts", "list"}, "mailbox contacts list", nil},
	{[]string{"contacts", "search"}, "mailbox contacts search QUERY", nil},
	{[]string{"contacts", "refresh"}, "mailbox contacts refresh", nil},
	{[]string{"contacts", "show"}, "mailbox contacts show ID", nil},
	{[]string{"contacts", "add"}, "mailbox contacts add --name TEXT --email ADDR [--note TEXT]", []string{"--name", "--email", "--note"}},
	{[]string{"contacts", "update"}, "mailbox contacts update ID [--name TEXT] [--email ADDR] [--note TEXT]", []string{"--name", "--email", "--note"}},
	{[]string{"doctor"}, "mailbox doctor", nil},
	{[]string{"serve"}, "mailbox serve [--web] [--web-port N] [--interval S] [--print]", []string{"--web", "--web-port", "--interval", "--print"}},
	{[]string{"commands"}, "mailbox commands", nil},
	{[]string{"skill", "install"}, "mailbox skill install", nil},
	{[]string{"help"}, "mailbox help [output|exit-codes|environment]", nil},
}

func commandsPayload() []*format.OM {
	rows := make([]*format.OM, 0, len(cmdSpecs))
	for _, s := range cmdSpecs {
		flagVals := make([]*format.OM, 0)
		for _, f := range s.flags {
			flagVals = append(flagVals, format.NewOM("flag", f))
		}
		pathVals := make([]string, len(s.path))
		copy(pathVals, s.path)
		rows = append(rows, format.NewOM(
			"path", pathVals,
			"usage", s.usage,
			"flags", flagVals,
		))
	}
	return rows
}

func cmdCommands(out *format.Output) (int, error) {
	return format.WriteOK(commandsPayload(), out, ""), nil
}

func cmdSkill(cmd string, out *format.Output) (int, error) {
	if cmd == "install" {
		paths, err := skill.InstallSkill("")
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("installed", paths), out, ""), nil
	}
	packaged := skill.PackagedSkill()
	targets := skill.DefaultTargets("")
	copiesRows := make([]*format.OM, 0, len(copiesToList(skill.InstalledCopies(""))))
	for _, c := range skill.InstalledCopies("") {
		copiesRows = append(copiesRows, format.NewOM(
			"path", c.Path,
			"installed", c.Installed,
			"managed", c.Managed,
			"current", c.Current,
		))
	}
	return format.WriteOK(format.NewOM(
		"packaged_bytes", len(packaged),
		"targets", targets,
		"copies", copiesRows,
	), out, ""), nil
}

func copiesToList(rows []skill.CopyRow) []skill.CopyRow { return rows }

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
