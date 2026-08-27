// Package cli ports src/mailbox_cli/cli.py (argparse surface, hand-rolled).
package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mailbox/src/internal/calendar"
	"mailbox/src/internal/config"
	"mailbox/src/internal/contacts"
	"mailbox/src/internal/doctor"
	"mailbox/src/internal/folders"
	"mailbox/src/internal/format"
	"mailbox/src/internal/ids"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/tui"
)

// Filled by -ldflags at build time.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

var helpTopics = map[string]string{
	"output": `Output

  default         table on a TTY; JSON envelope when piped
  --styled        force the table
  --json          envelope {ok, data}; truncated/notice/next_page when a list was cut
  --jq EXPR       filter that envelope (implies --json; needs the jq binary)
                  --quiet --jq filters data directly
  --quiet         JSON of data, no envelope
  --ids-only      one ID per line; truncation notice on stderr
  --count         a bare number; truncation notice on stderr
  --markdown      list as a table; thread as one document
  --html          original HTML for mailbox thread
  --allow-partial incomplete thread is still a result

Use only one of --json, --ids-only, --count, --html, --markdown.
String --jq results print as plain text; objects and arrays print as JSON.
Errors keep the structured envelope with a code field; --jq is not applied to them.
`,
	"exit-codes": `Exit codes

  0  success
  1  usage error (unknown flag or box, missing argument, invalid --jq)
  2  not found
  3  credentials missing or unreadable
  6  network failure
  7  operational failure (IMAP, CalDAV, incomplete thread without --allow-partial)
  8  ambiguous id

JSON errors carry a stable code field: usage, not_found, auth, network, api, ambiguous.
Any command accepts -h/--help; mailbox commands lists all.
`,
	"environment": `Environment

  MAILBOX_EMAIL
  MAILBOX_PASSWORD
  MAILBOX_DAV_PASSWORD
  MAILBOX_IMAP_HOST
  MAILBOX_IMAP_PORT
  MAILBOX_SMTP_HOST
  MAILBOX_SMTP_PORT
  MAILBOX_CALDAV_KALENDER
  MAILBOX_CALDAV_AUFGABEN
  MAILBOX_CARDDAV_KONTAKTE
  MAILBOX_TB_HOME
  MAILBOX_TB_PROFILE
  MAILBOX_CONFIG

Reads ~/.config/mailbox/env (or MAILBOX_CONFIG), then the Thunderbird profile (newest prefs.js, or MAILBOX_TB_PROFILE), when env is unset.
IMAP/SMTP use the imap.mailbox.org password. CalDAV/CardDAV use dav.mailbox.org (MAILBOX_DAV_PASSWORD, else that Thunderbird login, else MAILBOX_PASSWORD).
Run mailbox setup to write the env file. MAILBOX_NONINTERACTIVE=1 skips the wizard.
`,
}

type col = [2]string

var (
	folderColumns = []col{{"id", "ID"}, {"imap", "IMAP"}, {"role", "Role"}}
	mailColumns   = []col{{"id", "ID"}, {"from", "From"}, {"summary", "Summary"}, {"date", "Date"}}
	mailColumnsD  = []col{{"id", "ID"}, {"from", "From"}, {"summary", "Summary"}, {"date", "Date"}, {"flags", "Flags"}}
	labelColumns  = []col{{"id", "ID"}}
	mailWidths    = map[string]int{"from": 24, "summary": 60}
	attachColumns = []col{{"id", "ID"}, {"name", "Name"}, {"type", "Type"}, {"size", "Size"}}
	draftColumns  = []col{{"id", "ID"}, {"to", "To"}, {"summary", "Summary"}, {"date", "Date"}}
	draftColumnsD = []col{{"id", "ID"}, {"to", "To"}, {"summary", "Summary"}, {"date", "Date"}, {"flags", "Flags"}}
	eventColumns  = []col{{"id", "ID"}, {"calendar", "Calendar"}, {"start", "Start"}, {"end", "End"}, {"summary", "Summary"}}
	taskColumns   = []col{{"id", "ID"}, {"due", "Due"}, {"status", "Status"}, {"summary", "Summary"}}
	habitColumns  = []col{{"id", "ID"}, {"name", "Name"}, {"days", "Days"}, {"done", "Done"}, {"color", "Color"}, {"icon", "Icon"}}
	calColumns    = []col{{"name", "Name"}, {"color", "Color"}}
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
		return format.ExitStatus("usage")
	}
	format.ApplyDefaultFormat(out, isTTY(os.Stdout))
	if len(argv) == 0 || sameStrings(argv, []string{"-h"}) || sameStrings(argv, []string{"--help"}) || sameStrings(argv, []string{"help"}) {
		fmt.Print(helpText(nil))
		return 0
	}
	if argv[0] == "help" {
		return helpTopic(argv[1:])
	}
	if argv[0] == "--version" {
		argv[0] = "version"
	}

	rc, err := dispatch(argv, out)
	if err != nil {
		code := format.Classify(err)
		if out.JSON || out.Quiet {
			fmt.Println(format.DumpJSON(format.NewOM("ok", false, "code", code, "error", err.Error())))
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		return format.ExitStatus(code)
	}
	return rc
}

func dispatch(argv []string, out *format.Output) (int, error) {
	if i := helpFlagIndex(argv); i >= 0 {
		text := helpText(argv[:i])
		if text == "" {
			fmt.Fprintf(os.Stderr, "no command matches %q\n", strings.Join(argv[:i], " "))
			return 1, nil
		}
		fmt.Print(text)
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
	case "aside":
		sub, flags, err := subcommand(rest, "aside")
		if err != nil {
			return 0, err
		}
		return cmdAside(sub, flags, out)
	case "label":
		sub, flags, err := subcommand(rest, "label")
		if err != nil {
			return 0, err
		}
		return cmdLabel(sub, flags, out)
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
	case "reply":
		flags, err := parseFlags(flagSpec("reply", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdReply(flags, out)
	case "forward":
		flags, err := parseFlags(flagSpec("forward", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdForward(flags, out)
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
	case "sieve":
		sub, flags, err := subcommand(rest, "sieve")
		if err != nil {
			return 0, err
		}
		return cmdSieve(sub, flags, out)
	case "event":
		return cmdEvent(rest, out)
	case "calendar":
		return cmdCalendar(rest, out)
	case "todo":
		return cmdTodo(rest, out)
	case "habit":
		return cmdHabit(rest, out)
	case "contact":
		sub, flags, err := subcommand(rest, "contact")
		if err != nil {
			return 0, err
		}
		return cmdContact(sub, flags, out)
	case "doctor":
		return cmdDoctor(out), nil
	case "commands":
		return cmdCommands(out)
	case "setup", "skill":
		return cmdSetup(rest, out)
	case "version":
		return cmdVersion(out)
	case "serve":
		flags, err := parseFlags(flagSpec("serve", ""), rest)
		if err != nil {
			return 0, err
		}
		return cmdServe(flags, out)
	case "tui":
		flags, err := parseFlags(flagSpec("tui", ""), rest)
		if err != nil {
			return 0, err
		}
		if len(flags.positional) != 0 {
			return printUsage("tui"), nil
		}
		return cmdTui(flags)
	default:
		fmt.Print(helpText(nil))
		return 1, nil
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
		fmt.Print(helpText(nil))
		return 0
	}
	text, ok := helpTopics[args[0]]
	if !ok {
		names := make([]string, 0, len(helpTopics))
		for k := range helpTopics {
			names = append(names, k)
		}
		fmt.Fprintf(os.Stderr, "unknown help topic %q; use %s\n", args[0], strings.Join(names, ", "))
		return 1
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

func pageOf(flags *parsed) (limit, page int, all bool, err error) {
	all = flags.has("all")
	page = 1
	if flags.has("page") || flags.one("page") != "" {
		page, err = strconv.Atoi(flags.one("page"))
		if err != nil || page < 1 {
			return 0, 0, false, usageErr("--page must be a positive integer")
		}
	}
	limit, err = limitOf(flags, 50)
	return
}

func messageOf(flags *parsed) string {
	if flags.has("message") {
		return flags.one("message")
	}
	return flags.one("m")
}

func listOpts(flags *parsed) (lim *int, page int, all bool, err error) {
	limit, page, all, err := pageOf(flags)
	if err != nil {
		return nil, 0, false, err
	}
	if all {
		return nil, 1, true, nil
	}
	return limitPtr(limit), page, false, nil
}

// --- flag parsing (strict: unknown flags are usage errors) ---

type flagspec struct{ bools, vals set }

var noFlags = flagspec{newSet(), newSet()}

// Output flags (--json, --jq, ...) are stripped earlier by format.TakeOutputFlags;
// these tables cover the remaining domain flags per command (or per group union).
var cmdFlagSpecs = map[string]flagspec{
	"box":              {newSet("archive", "unread", "detail", "all"), newSet("limit", "page")},
	"aside":            {newSet("sweep", "detail", "all"), newSet("remind", "limit", "page")},
	"screener list":    {newSet("unread", "detail", "all"), newSet("limit", "page")},
	"screener approve": {nil, newSet("box")},
	"screener deny":    {newSet("spam"), nil},
	"search":           {newSet("detail", "all"), newSet("from", "to", "subject", "in", "limit", "page", "required", "any", "none", "exact", "date", "attachment")},
	"move":             {nil, newSet("to")},
	"label view":       {newSet("all", "detail"), newSet("limit", "page")},
	"label add":        {nil, newSet("to")},
	"label remove":     {nil, newSet("from")},
	"compose":          {newSet("draft"), newSet("to", "cc", "bcc", "subject", "m", "message", "message-html", "attach")},
	"reply":            {newSet("draft"), newSet("to", "cc", "bcc", "m", "message", "message-html", "attach")},
	"forward":          {nil, newSet("to", "cc", "bcc", "m", "message", "message-html", "attach")},
	"draft list":       {newSet("all", "unread", "detail"), newSet("limit", "page")},
	"draft edit":       {nil, newSet("to", "cc", "bcc", "subject", "m", "message", "message-html")},
	"attachment save":  {newSet("force"), newSet("output")},
	"sieve get":        {nil, newSet("output")},
	"event":            {newSet("all-day", "circle", "all"), newSet("starts-on", "start-time", "ends-on", "end-time", "title", "calendar", "location", "notes", "link", "repeat", "repeat-until", "repeat-times", "remind", "limit", "page")},
	"todo":             {newSet("all"), newSet("title", "date", "calendar", "starts-on", "ends-on", "limit", "page")},
	"habit":            {nil, newSet("days", "name", "date", "color", "icon")},
	"contact":          {nil, newSet("name", "email", "note")},
	"serve":            {newSet("web", "print"), newSet("web-port", "interval")},
	"tui":              {newSet("screener"), nil},
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
		"aside":      {"done", "list"},
		"label":      {"list", "create", "view", "add", "remove"},
		"box":        {"list", "view"},
		"screener":   {"list", "approve", "deny"},
		"draft":      {"list", "show", "edit", "send", "delete"},
		"attachment": {"list", "save"},
		"sieve":      {"list", "get", "put", "activate"},
		"contact":    {"list", "show", "add", "update", "search"},
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
	short string
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

func printUsage(path ...string) int {
	for _, s := range cmdSpecs {
		if sameStrings(s.path, path) {
			fmt.Printf("usage: %s\n", s.usage)
			return 1
		}
	}
	fmt.Print(helpText(nil))
	return 1
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
		"labels", strings.Join(mail.LabelsFromFlags(e.Flags), ","),
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
		lim, page, _, err := listOpts(flags)
		if err != nil {
			return 0, err
		}
		listing, err := m.ListMessages(folder, flags.has("unread"), lim, page)
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(listing.Items))
		for _, e := range listing.Items {
			rows = append(rows, envRow(e))
		}
		return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
	})
}

func cmdSearch(flags *parsed, out *format.Output) (int, error) {
	if len(flags.positional) == 1 && flags.positional[0] == "filters" &&
		flags.one("from") == "" && flags.one("to") == "" && flags.one("subject") == "" &&
		flags.one("required") == "" && flags.one("any") == "" && flags.one("none") == "" &&
		flags.one("exact") == "" && flags.one("date") == "" && flags.one("attachment") == "" && flags.one("in") == "" {
		vals := mail.SearchFilterValues()
		row := format.NewOM("in", strings.Join(vals["in"], ", "), "date", strings.Join(vals["date"], ", ")+", or a year", "attachment", strings.Join(vals["attachment"], ", "))
		return format.WriteOK(row, out, ""), nil
	}
	query := mail.SearchQuery{
		From:       flags.one("from"),
		To:         flags.one("to"),
		Subject:    flags.one("subject"),
		Required:   flags.one("required"),
		Any:        flags.one("any"),
		None:       flags.one("none"),
		Exact:      flags.one("exact"),
		Date:       flags.one("date"),
		Attachment: flags.one("attachment"),
	}
	if len(flags.positional) > 0 {
		query.Text = strings.Join(flags.positional, " ")
	}
	if query.Empty() {
		return 0, usageErr("search needs QUERY or a refinement")
	}
	limit, page, all, err := pageOf(flags)
	if err != nil {
		return 0, err
	}
	if all {
		limit = -1
	}
	return withMail(func(m *mail.Mail) (int, error) {
		listing, err := m.Search(query, limit, page, flags.one("in"))
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(listing.Items))
		for _, e := range listing.Items {
			rows = append(rows, envRow(e))
		}
		var lim *int
		if !all {
			lim = limitPtr(limit)
		}
		return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
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
		if out.Count {
			return withMail(func(m *mail.Mail) (int, error) {
				n, err := m.CountMessages("screener", flags.has("unread"))
				if err != nil {
					return 0, err
				}
				fmt.Println(n)
				return 0, nil
			})
		}
		lim, page, _, err := listOpts(flags)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			listing, err := m.ListMessages("screener", flags.has("unread"), lim, page)
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(listing.Items))
			for _, e := range listing.Items {
				rows = append(rows, envRow(e))
			}
			return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
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

func cmdAside(sub string, flags *parsed, out *format.Output) (int, error) {
	if sub == "" && flags.has("sweep") {
		return withMail(func(m *mail.Mail) (int, error) {
			returned, err := m.SweepAside(time.Now())
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(returned))
			for _, r := range returned {
				rows = append(rows, format.NewOM("id", r.ID, "due", r.Due.Format(time.RFC3339)))
			}
			if len(rows) == 1 {
				return format.WriteOK(rows[0], out, ""), nil
			}
			return format.WriteOK(rows, out, ""), nil
		})
	}
	if sub == "done" {
		pairs, err := idPairs(flags.positional)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			var rows []*format.OM
			for _, p := range pairs {
				newID, err := m.Unaside(p.folder, p.uid)
				if err != nil {
					return 0, err
				}
				rows = append(rows, format.NewOM("id", newID, "to", folders.INBOX))
			}
			if len(rows) == 1 {
				return format.WriteOK(rows[0], out, ""), nil
			}
			return format.WriteOK(rows, out, ""), nil
		})
	}
	if sub == "list" || (len(flags.positional) == 0 && flags.one("remind") == "") {
		lim, page, _, err := listOpts(flags)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			listing, err := m.ListMessages(folders.ASIDE, false, lim, page)
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(listing.Items))
			for _, e := range listing.Items {
				row := envRow(e)
				if due, ok := mail.ParseAsideDue(e.Flags); ok {
					row.Set("due", due.Format(time.RFC3339))
				}
				rows = append(rows, row)
			}
			return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
		})
	}
	if len(flags.positional) == 0 {
		return printUsage("aside"), nil
	}
	var remind *time.Time
	if spec := flags.one("remind"); spec != "" {
		t, err := mail.ParseRemind(spec)
		if err != nil {
			return 0, usageErr("%v", err)
		}
		remind = &t
	}
	pairs, err := idPairs(flags.positional)
	if err != nil {
		return 0, err
	}
	return withMail(func(m *mail.Mail) (int, error) {
		if err := ensureAside(m); err != nil {
			return 0, err
		}
		var rows []*format.OM
		for _, p := range pairs {
			newID, err := m.Aside(p.folder, p.uid, remind)
			if err != nil {
				return 0, err
			}
			row := format.NewOM("id", newID, "from", ids.FormatMessageID(p.folder, p.uid), "to", folders.ASIDE)
			if remind != nil {
				row.Set("due", remind.Format(time.RFC3339))
			}
			rows = append(rows, row)
		}
		if len(rows) == 1 {
			return format.WriteOK(rows[0], out, ""), nil
		}
		return format.WriteOK(rows, out, ""), nil
	})
}

type idPair struct{ folder, uid string }

func idPairs(idList []string) ([]idPair, error) {
	pairs := make([]idPair, 0, len(idList))
	for _, mid := range idList {
		folder, uid, err := ids.ParseMessageID(mid)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, idPair{folder, uid})
	}
	return pairs, nil
}

// ensureAside creates the Aside pile folder on first use.
func ensureAside(m *mail.Mail) error {
	names, err := m.ListFolders()
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == folders.ASIDE {
			return nil
		}
	}
	return m.CreateFolder(folders.ASIDE)
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

func cmdLabel(cmd string, flags *parsed, out *format.Output) (int, error) {
	if cmd == "" && len(flags.positional) > 0 {
		return 0, usageErr("unknown label command %q", flags.positional[0])
	}
	switch cmd {
	case "create":
		if len(flags.positional) == 0 {
			return printUsage("label", "create"), nil
		}
		name, err := mail.CreateLabel(flags.positional[0])
		if err != nil {
			return 0, usageErr("%v", err)
		}
		if len(flags.positional) == 1 {
			return format.WriteOK(format.NewOM("id", name), out, ""), nil
		}
		pairs, err := idPairs(flags.positional[1:])
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			var rows []*format.OM
			for _, p := range pairs {
				if err := m.SetLabel(p.folder, p.uid, name, true); err != nil {
					return 0, err
				}
				rows = append(rows, format.NewOM("id", ids.FormatMessageID(p.folder, p.uid), "label", name))
			}
			if len(rows) == 1 {
				return format.WriteOK(rows[0], out, ""), nil
			}
			return format.WriteOK(rows, out, ""), nil
		})
	case "view":
		if len(flags.positional) == 0 {
			return printUsage("label", "view"), nil
		}
		raw := strings.Join(flags.positional, " ")
		name, err := mail.NormalizeLabel(raw)
		if err != nil {
			return 0, usageErr("%v", err)
		}
		limit, page, all, err := pageOf(flags)
		if err != nil {
			return 0, err
		}
		if all {
			limit = -1
		}
		return withMail(func(m *mail.Mail) (int, error) {
			listing, err := m.Labeled(name, limit, page)
			if err != nil {
				return 0, err
			}
			rows := make([]*format.OM, 0, len(listing.Items))
			for _, e := range listing.Items {
				rows = append(rows, envRow(e))
			}
			var lim *int
			if !all {
				lim = limitPtr(limit)
			}
			return format.WriteList(rows, mailColumnsFor(flags.has("detail")), out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
		})
	case "add":
		to := flags.one("to")
		if to == "" || len(flags.positional) == 0 {
			return printUsage("label", "add"), nil
		}
		name, err := mail.NormalizeLabel(to)
		if err != nil {
			return 0, usageErr("%v", err)
		}
		pairs, err := idPairs(flags.positional)
		if err != nil {
			return 0, err
		}
		return withMail(func(m *mail.Mail) (int, error) {
			var rows []*format.OM
			for _, p := range pairs {
				if err := m.SetLabel(p.folder, p.uid, name, true); err != nil {
					return 0, err
				}
				rows = append(rows, format.NewOM("id", ids.FormatMessageID(p.folder, p.uid), "label", name))
			}
			if len(rows) == 1 {
				return format.WriteOK(rows[0], out, ""), nil
			}
			return format.WriteOK(rows, out, ""), nil
		})
	case "remove":
		from := flags.one("from")
		if from == "" || len(flags.positional) == 0 {
			return printUsage("label", "remove"), nil
		}
		pairs, err := idPairs(flags.positional)
		if err != nil {
			return 0, err
		}
		clearAll := strings.EqualFold(from, "all")
		var name string
		if !clearAll {
			name, err = mail.NormalizeLabel(from)
			if err != nil {
				return 0, usageErr("%v", err)
			}
		}
		return withMail(func(m *mail.Mail) (int, error) {
			var rows []*format.OM
			for _, p := range pairs {
				id := ids.FormatMessageID(p.folder, p.uid)
				if clearAll {
					if err := m.ClearLabels(p.folder, p.uid); err != nil {
						return 0, err
					}
					rows = append(rows, format.NewOM("id", id, "label", "all"))
					continue
				}
				if err := m.SetLabel(p.folder, p.uid, name, false); err != nil {
					return 0, err
				}
				rows = append(rows, format.NewOM("id", id, "label", name))
			}
			if len(rows) == 1 {
				return format.WriteOK(rows[0], out, ""), nil
			}
			return format.WriteOK(rows, out, ""), nil
		})
	default:
		if names, ok := mail.CatalogLabels(); ok {
			return writeLabelList(names, out), nil
		}
		return withMail(func(m *mail.Mail) (int, error) {
			names, err := m.ListLabels()
			if err != nil {
				return 0, err
			}
			return writeLabelList(names, out), nil
		})
	}
}

func writeLabelList(names []string, out *format.Output) int {
	rows := make([]*format.OM, 0, len(names))
	for _, name := range names {
		rows = append(rows, format.NewOM("id", name))
	}
	return format.WriteList(rows, labelColumns, out)
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
	draft := flags.has("draft")
	if !draft && len(to) == 0 {
		return usageErr("compose needs --to")
	}
	if flags.one("subject") == "" {
		return usageErr("compose needs --subject")
	}
	if (flags.has("m") || flags.has("message")) && flags.has("message-html") {
		return usageErr("-m/--message and --message-html are mutually exclusive")
	}
	return nil
}

func collectAttachments(paths []string) (atts []mail.OutAttachment, links []string, err error) {
	for _, path := range paths {
		att, err := mail.ReadAttachmentFile(path)
		if err != nil {
			return nil, nil, err
		}
		if len(att.Data) > mail.MaxInlineAttachment {
			url, err := mail.UploadToTransfer(att.Name, att.Data)
			if err != nil {
				return nil, nil, fmt.Errorf("%s over %d MiB and upload failed: %w", att.Name, mail.MaxInlineAttachment>>20, err)
			}
			links = append(links, fmt.Sprintf("- [%s](%s)", att.Name, url))
			continue
		}
		atts = append(atts, att)
	}
	return atts, links, nil
}

func appendLinks(body string, links []string) string {
	if len(links) == 0 {
		return body
	}
	return body + "\n\nLarge attachments available for download:\n" + strings.Join(links, "\n")
}

func cmdCompose(flags *parsed, out *format.Output) (int, error) {
	if err := checkCompose(flags); err != nil {
		return 0, err
	}
	body, htmlBody, err := composeBody(messageOf(flags), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
	if err != nil {
		return 0, err
	}
	attachments, links, err := collectAttachments(flags.list("attach"))
	if err != nil {
		return 0, err
	}
	body = appendLinks(body, links)
	outgoing := &mail.Outgoing{
		To:          splitAddrs(flags.list("to")),
		Cc:          splitAddrs(flags.list("cc")),
		Bcc:         splitAddrs(flags.list("bcc")),
		Subject:     flags.one("subject"),
		Body:        body,
		HTML:        htmlBody,
		Attachments: attachments,
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

func cmdReply(flags *parsed, out *format.Output) (int, error) {
	if len(flags.positional) != 1 {
		return printUsage("reply"), nil
	}
	if (flags.has("m") || flags.has("message")) && flags.has("message-html") {
		return 0, usageErr("-m/--message and --message-html are mutually exclusive")
	}
	folder, uid, err := ids.ParseMessageID(flags.positional[0])
	if err != nil {
		return 0, err
	}
	body, htmlBody, err := composeBody(messageOf(flags), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
	if err != nil {
		return 0, err
	}
	attachments, links, err := collectAttachments(flags.list("attach"))
	if err != nil {
		return 0, err
	}
	body = appendLinks(body, links)
	outgoing := &mail.Outgoing{
		To:          splitAddrs(flags.list("to")),
		Cc:          splitAddrs(flags.list("cc")),
		Bcc:         splitAddrs(flags.list("bcc")),
		Body:        body,
		HTML:        htmlBody,
		Attachments: attachments,
		ReplyTo:     &[2]string{folder, uid},
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

func cmdForward(flags *parsed, out *format.Output) (int, error) {
	if len(flags.positional) != 1 {
		return printUsage("forward"), nil
	}
	to := splitAddrs(flags.list("to"))
	cc := splitAddrs(flags.list("cc"))
	bcc := splitAddrs(flags.list("bcc"))
	if len(to)+len(cc)+len(bcc) == 0 {
		return 0, usageErr("forward needs --to")
	}
	if (flags.has("m") || flags.has("message")) && flags.has("message-html") {
		return 0, usageErr("-m/--message and --message-html are mutually exclusive")
	}
	folder, uid, err := ids.ParseMessageID(flags.positional[0])
	if err != nil {
		return 0, err
	}
	note, htmlBody, err := composeBody(messageOf(flags), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
	if err != nil {
		return 0, err
	}
	attachments, links, err := collectAttachments(flags.list("attach"))
	if err != nil {
		return 0, err
	}
	return withMail(func(m *mail.Mail) (int, error) {
		msg, err := m.Message(folder, uid)
		if err != nil {
			return 0, err
		}
		subject := msg.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
			subject = "Fwd: " + subject
		}
		quote := fmt.Sprintf("----- Forwarded message -----\nFrom: %s\nDate: %s\nSubject: %s\nTo: %s\n\n%s",
			msg.From, msg.Date, msg.Subject, msg.To, msg.Body)
		body := note
		if htmlBody == "" {
			if body != "" {
				body += "\n\n"
			}
			body += quote
			body = appendLinks(body, links)
		} else {
			htmlBody = htmlBody + "<pre>" + quote + "</pre>"
		}
		outgoing := &mail.Outgoing{
			To: to, Cc: cc, Bcc: bcc, Subject: subject,
			Body: body, HTML: htmlBody, Attachments: attachments,
		}
		newID, err := m.Compose(outgoing, false)
		if err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("id", newID, "folder", folders.SENT), out, ""), nil
	})
}

func draftID(value string) (string, string, error) {
	folder, uid, err := ids.ParseMessageIDIn(value, folders.DRAFTS)
	if err != nil {
		return "", "", err
	}
	if folder != folders.DRAFTS {
		return "", "", usageErr("draft id must be in Drafts, got %q", value)
	}
	return folder, uid, nil
}

func cmdDraft(cmd string, flags *parsed, out *format.Output) (int, error) {
	if cmd == "list" || cmd == "" {
		lim, page, _, err := listOpts(flags)
		if err != nil {
			return 0, err
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
			listing, err := m.ListMessages(folders.DRAFTS, flags.has("unread"), lim, page)
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
			return format.WriteList(rows, cols, out, listing.Truncated, lim, mailWidths, format.NextPage(listing.NextPage)), nil
		})
	}
	if len(flags.positional) != 1 {
		return printUsage("draft", cmd), nil
	}
	id := flags.positional[0]
	folder, uid, err := draftID(id)
	if err != nil {
		return 0, err
	}
	switch cmd {
	case "show":
		return withMail(func(m *mail.Mail) (int, error) {
			msg, err := m.Message(folder, uid)
			if err != nil {
				return 0, err
			}
			return format.WriteThread([]*format.OM{threadRow(msg, false)}, out, false, ""), nil
		})
	case "edit":
		hasAny := flags.has("to") || flags.has("cc") || flags.has("bcc") ||
			flags.has("subject") || flags.has("m") || flags.has("message") || flags.has("message-html")
		if !hasAny {
			return 0, usageErr("draft edit needs --to, --cc, --bcc, --subject, -m, or --message-html")
		}
		var bodyPtr, htmlPtr *string
		if flags.has("m") || flags.has("message") || flags.has("message-html") {
			body, htmlBody, err := composeBody(messageOf(flags), flags.one("message-html"), os.Stdin, isTTY(os.Stdin))
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
		return withMail(func(m *mail.Mail) (int, error) {
			newID, err := m.SendDraft(folder, uid)
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", newID, "folder", folders.SENT), out, ""), nil
		})
	default: // delete
		return imapMove([]string{ids.FormatMessageID(folder, uid)}, folders.TRASH, out, "")
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
	// save: [box:]uid:index, or [box:]uid when that message has exactly one file
	folder, uid, index, err := ids.ParseAttachmentID(id)
	if err != nil {
		folder, uid, err = ids.ParseMessageID(id)
		if err != nil {
			return 0, fmt.Errorf("attachment id must be [box:]uid:index, got %q", id)
		}
	}
	return withMail(func(m *mail.Mail) (int, error) {
		if index == 0 {
			msg, err := m.Message(folder, uid)
			if err != nil {
				return 0, err
			}
			switch len(msg.Attachments) {
			case 0:
				return 0, fmt.Errorf("%s has no attachments", msg.ID())
			case 1:
				index = msg.Attachments[0].Index
			default:
				return 0, fmt.Errorf("%s has %d attachments; pass [box:]uid:index", msg.ID(), len(msg.Attachments))
			}
		}
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

// --- calendar / todo / contacts ---

func withCal(fn func(*calendar.Cal) (int, error)) (int, error) {
	acct, err := config.LoadAccount(true, false)
	if err != nil {
		return 0, err
	}
	cal, err := calendar.NewCal(acct)
	if err != nil {
		return 0, err
	}
	return fn(cal)
}

func eventInFrom(flags *parsed, add bool) (calendar.EventIn, error) {
	in := calendar.EventIn{
		Title: flags.one("title"), Calendar: flags.one("calendar"), Location: flags.one("location"),
		Notes: flags.one("notes"), URL: flags.one("link"),
		Repeat: flags.one("repeat"), RepeatUntil: flags.one("repeat-until"),
		Remind: flags.one("remind"), Circle: flags.has("circle"),
		Has: calendar.EventHas{
			Title: flags.has("title"), Location: flags.has("location"), Notes: flags.has("notes"),
			URL: flags.has("link"), Repeat: flags.has("repeat"), Remind: flags.has("remind"),
			Circle: flags.has("circle"),
		},
	}
	touchTime := add || flags.has("starts-on") || flags.has("start-time") || flags.has("ends-on") || flags.has("end-time") || flags.has("all-day")
	if touchTime {
		start, end, allDay, err := calendar.CombineEventWhen(
			flags.one("starts-on"), flags.one("start-time"), flags.one("ends-on"), flags.one("end-time"), flags.has("all-day"))
		if err != nil {
			return in, usageErr("%v", err)
		}
		in.Start, in.End, in.AllDay = start, end, allDay
		in.Has.Start, in.Has.End, in.Has.AllDay = true, true, true
	}
	if flags.has("repeat-times") {
		n, err := strconv.Atoi(flags.one("repeat-times"))
		if err != nil || n <= 0 {
			return in, usageErr("--repeat-times must be a positive integer")
		}
		in.RepeatTimes = n
	}
	return in, nil
}

func cmdCalendar(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(noFlags, args)
	if err != nil {
		return 0, err
	}
	if len(flags.positional) > 0 && flags.positional[0] != "list" {
		return 0, usageErr("unknown calendar command %q", flags.positional[0])
	}
	return withCal(func(cal *calendar.Cal) (int, error) {
		rows, err := cal.Calendars()
		if err != nil {
			return 0, err
		}
		return format.WriteList(rows, calColumns, out), nil
	})
}

func cmdEvent(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(flagSpec("event", ""), args)
	if err != nil {
		return 0, err
	}
	sub, rest := "", flags.positional
	if len(rest) > 0 {
		switch rest[0] {
		case "list", "add", "show", "edit", "delete":
			sub, rest = rest[0], rest[1:]
		default:
			return 0, usageErr("unknown event command %q", rest[0])
		}
	}
	if sub == "" && (flags.has("title") || flags.has("starts-on") && flags.has("title")) {
		sub = "add"
	}
	if sub == "" {
		sub = "list"
	}
	if sub == "add" && !flags.has("title") && len(rest) > 0 {
		flags.flags["title"] = []string{strings.Join(rest, " ")}
		rest = nil
	}
	in, err := eventInFrom(flags, sub == "add")
	if err != nil {
		return 0, err
	}
	return withCal(func(cal *calendar.Cal) (int, error) {
		switch sub {
		case "add":
			if in.Title == "" {
				return 0, usageErr("event add needs a title")
			}
			uid, name, err := cal.CreateEvent(in)
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", uid, "calendar", name), out, ""), nil
		case "show":
			if len(rest) < 1 {
				return 0, usageErr("event show needs ID")
			}
			row, err := cal.Event(rest[0])
			if err != nil {
				return 0, err
			}
			return format.WriteOK(row, out, ""), nil
		case "edit":
			if len(rest) < 1 {
				return 0, usageErr("event edit needs ID")
			}
			row, err := cal.UpdateEvent(rest[0], in)
			if err != nil {
				return 0, err
			}
			return format.WriteOK(row, out, ""), nil
		case "delete":
			if len(rest) < 1 {
				return 0, usageErr("event delete needs ID")
			}
			if err := cal.DeleteEvent(rest[0]); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0]), out, ""), nil
		default:
			rows, err := cal.Events(flags.one("starts-on"), flags.one("ends-on"), flags.one("calendar"))
			if err != nil {
				return 0, err
			}
			limit, page, all, err := pageOf(flags)
			if err != nil {
				return 0, err
			}
			rows, next, trunc := format.PageSlice(rows, page, limit, all)
			var lim *int
			if !all {
				lim = limitPtr(limit)
			}
			return format.WriteList(rows, eventColumns, out, trunc, lim, format.NextPage(next)), nil
		}
	})
}

func cmdTodo(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(flagSpec("todo", ""), args)
	if err != nil {
		return 0, err
	}
	sub, rest := "", flags.positional
	if len(rest) > 0 {
		switch rest[0] {
		case "list", "add", "complete", "uncomplete", "delete":
			sub, rest = rest[0], rest[1:]
		default:
			return 0, usageErr("unknown todo command %q", rest[0])
		}
	}
	if sub == "" && flags.has("title") {
		sub = "add"
	}
	if sub == "" {
		sub = "list"
	}
	return withCal(func(cal *calendar.Cal) (int, error) {
		switch sub {
		case "add":
			if !flags.has("title") {
				return 0, usageErr("todo add needs --title")
			}
			uid, err := cal.CreateTask(flags.one("title"), flags.one("date"))
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", uid, "calendar", "Aufgaben"), out, ""), nil
		case "complete":
			if len(rest) < 1 {
				return 0, usageErr("todo complete needs ID")
			}
			if err := cal.CompleteTask(rest[0]); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0], "status", "COMPLETED"), out, ""), nil
		case "uncomplete":
			if len(rest) < 1 {
				return 0, usageErr("todo uncomplete needs ID")
			}
			if err := cal.UncompleteTask(rest[0]); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0], "status", "NEEDS-ACTION"), out, ""), nil
		case "delete":
			if len(rest) < 1 {
				return 0, usageErr("todo delete needs ID")
			}
			if err := cal.DeleteTask(rest[0]); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0]), out, ""), nil
		default:
			if calName := flags.one("calendar"); calName != "" && !strings.EqualFold(calName, "Aufgaben") {
				return 0, usageErr("unknown todo calendar %q", calName)
			}
			rows, err := cal.Tasks(flags.one("starts-on"), flags.one("ends-on"))
			if err != nil {
				return 0, err
			}
			limit, page, all, err := pageOf(flags)
			if err != nil {
				return 0, err
			}
			rows, next, trunc := format.PageSlice(rows, page, limit, all)
			var lim *int
			if !all {
				lim = limitPtr(limit)
			}
			return format.WriteList(rows, taskColumns, out, trunc, lim, format.NextPage(next)), nil
		}
	})
}

func cmdHabit(args []string, out *format.Output) (int, error) {
	flags, err := parseFlags(flagSpec("habit", ""), args)
	if err != nil {
		return 0, err
	}
	sub, rest := "", flags.positional
	if len(rest) > 0 {
		switch rest[0] {
		case "list", "create", "edit", "delete", "complete", "uncomplete":
			sub, rest = rest[0], rest[1:]
		default:
			return 0, usageErr("unknown habit command %q", rest[0])
		}
	}
	return withCal(func(cal *calendar.Cal) (int, error) {
		switch sub {
		case "create":
			name := strings.Join(rest, " ")
			uid, err := cal.CreateHabit(name, flags.one("days"), flags.one("color"), flags.one("icon"))
			if err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", uid), out, ""), nil
		case "edit":
			if len(rest) < 1 {
				return 0, usageErr("habit edit needs ID")
			}
			if !flags.has("name") && !flags.has("days") && !flags.has("color") && !flags.has("icon") {
				return 0, usageErr("habit edit needs --name, --days, --color, or --icon")
			}
			row, err := cal.EditHabit(rest[0], flags.one("name"), flags.one("days"), flags.one("color"), flags.one("icon"),
				flags.has("name"), flags.has("days"), flags.has("color"), flags.has("icon"))
			if err != nil {
				return 0, err
			}
			return format.WriteOK(row, out, ""), nil
		case "delete":
			if len(rest) < 1 {
				return 0, usageErr("habit delete needs ID")
			}
			if err := cal.DeleteHabit(rest[0]); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0]), out, ""), nil
		case "complete":
			if len(rest) < 1 {
				return 0, usageErr("habit complete needs ID")
			}
			if err := cal.CompleteHabit(rest[0], flags.one("date")); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0], "done", true), out, ""), nil
		case "uncomplete":
			if len(rest) < 1 {
				return 0, usageErr("habit uncomplete needs ID")
			}
			if err := cal.UncompleteHabit(rest[0], flags.one("date")); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("id", rest[0], "done", false), out, ""), nil
		default:
			rows, err := cal.Habits(flags.one("date"))
			if err != nil {
				return 0, err
			}
			return format.WriteList(rows, habitColumns, out), nil
		}
	})
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
			return 0, usageErr("contact add needs --name and --email")
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
			return 0, usageErr("contact show needs ID")
		}
		row, err := book.Show(flags.positional[0])
		if err != nil {
			return 0, err
		}
		return format.WriteOK(row, out, ""), nil
	case "search":
		if len(flags.positional) != 1 {
			return 0, usageErr("contact search needs QUERY")
		}
		rows, err := book.Search(flags.positional[0])
		if err != nil {
			return 0, err
		}
		return format.WriteList(rows, contactCols, out), nil
	case "update":
		if len(flags.positional) != 1 {
			return 0, usageErr("contact update needs ID")
		}
		name, email, note := flags.one("name"), flags.one("email"), flags.one("note")
		if !flags.has("name") && !flags.has("email") && !flags.has("note") {
			return 0, usageErr("contact update needs --name, --email, or --note")
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

func cmdTui(flags *parsed) (int, error) {
	err := tui.Run(tui.Options{Screener: flags.has("screener")})
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func cmdDoctor(out *format.Output) int {
	report := doctor.Run(nil, nil)
	format.WriteOK(report.AsDict(), out, "")
	if report.OK() {
		return 0
	}
	return 7
}

var cmdSpecs = []cmdspec{
	{[]string{"box", "list"}, "mailbox box list [--archive]", []string{"--archive"}, "Routing boxes"},
	{[]string{"box", "view"}, "mailbox box view NAME [--unread] [--limit N] [--page N] [--all] [--detail]", []string{"--unread", "--limit", "--page", "--all", "--detail"}, "Mail in a box"},
	{[]string{"search"}, "mailbox search [QUERY] [--from ADDR] [--to ADDR] [--subject TEXT] [--in BOX] [--date RANGE] [--required W] [--any W] [--none W] [--exact PHRASE] [--attachment KIND] [--limit N] [--page N] [--all] [--detail]", []string{"--from", "--to", "--subject", "--in", "--date", "--required", "--any", "--none", "--exact", "--attachment", "--limit", "--page", "--all", "--detail"}, "Search mail"},
	{[]string{"search", "filters"}, "mailbox search filters", nil, "Available --in/--date/--attachment values"},
	{[]string{"thread"}, "mailbox thread ID [--allow-partial] [--html]", []string{"--allow-partial", "--html"}, "Read a thread"},
	{[]string{"reply"}, "mailbox reply ID [-m TEXT]", []string{"-m", "--message", "--message-html", "--attach", "--draft", "--to", "--cc", "--bcc"}, "Reply"},
	{[]string{"forward"}, "mailbox forward ID --to ADDR [-m TEXT]", []string{"--to", "--cc", "--bcc", "-m", "--message", "--message-html", "--attach"}, "Forward the latest message"},
	{[]string{"screener", "list"}, "mailbox screener list [--unread] [--limit N] [--page N] [--all] [--detail]", []string{"--unread", "--limit", "--page", "--all", "--detail"}, "Who is waiting"},
	{[]string{"screener", "approve"}, "mailbox screener approve ID... [--box inbox|feed|trail]", []string{"--box"}, "Let a sender through"},
	{[]string{"screener", "deny"}, "mailbox screener deny ID... [--spam]", []string{"--spam"}, "Turn a sender away"},
	{[]string{"move"}, "mailbox move ID... --to inbox|feed|trail|block", []string{"--to"}, "Move to inbox|feed|trail|block"},
	{[]string{"aside"}, "mailbox aside [ID...] [--remind DURATION] [--limit N] [--page N] [--all] [--detail]", []string{"--remind", "--limit", "--page", "--all", "--detail", "--sweep"}, "Read-later pile"},
	{[]string{"aside", "done"}, "mailbox aside done ID...", nil, "Return from Aside"},
	{[]string{"label", "list"}, "mailbox label list", nil, "Labels"},
	{[]string{"label", "create"}, "mailbox label create NAME [ID...]", nil, "Create a label"},
	{[]string{"label", "view"}, "mailbox label view NAME [--limit N] [--page N] [--all] [--detail]", []string{"--limit", "--page", "--all", "--detail"}, "Mail with a label"},
	{[]string{"label", "add"}, "mailbox label add ID... --to NAME", []string{"--to"}, "Add a label"},
	{[]string{"label", "remove"}, "mailbox label remove ID... --from NAME|all", []string{"--from"}, "Remove a label"},
	{[]string{"seen"}, "mailbox seen ID...", nil, "Mark seen"},
	{[]string{"unseen"}, "mailbox unseen ID...", nil, "Mark unseen"},
	{[]string{"trash"}, "mailbox trash ID...", nil, "Move to Trash"},
	{[]string{"spam"}, "mailbox spam ID...", nil, "Move to Junk"},
	{[]string{"compose"}, "mailbox compose --to ADDR --subject TEXT [-m TEXT]", []string{"--to", "--cc", "--bcc", "--subject", "-m", "--message", "--message-html", "--attach", "--draft"}, "Write and send"},
	{[]string{"draft", "list"}, "mailbox draft list [--all] [--limit N] [--page N]", []string{"--all", "--limit", "--page", "--detail"}, "Unsent drafts"},
	{[]string{"draft", "show"}, "mailbox draft show ID", nil, "Read a draft"},
	{[]string{"draft", "edit"}, "mailbox draft edit ID [--to ADDR] [--subject TEXT] [-m TEXT]", []string{"--to", "--cc", "--bcc", "--subject", "-m", "--message", "--message-html"}, "Change a draft"},
	{[]string{"draft", "send"}, "mailbox draft send ID", nil, "Deliver a draft"},
	{[]string{"draft", "delete"}, "mailbox draft delete ID", nil, "Trash a draft"},
	{[]string{"attachment", "list"}, "mailbox attachment list ID", nil, "Files in a thread"},
	{[]string{"attachment", "save"}, "mailbox attachment save ID [--output PATH] [--force]", []string{"--output", "--force"}, "Save a file"},
	{[]string{"calendar", "list"}, "mailbox calendar list", nil, "Discovered calendars"},
	{[]string{"event", "list"}, "mailbox event list [--starts-on DATE] [--ends-on DATE] [--calendar NAME] [--limit N] [--all]", []string{"--starts-on", "--ends-on", "--calendar", "--limit", "--all", "--page"}, "Events in a date window"},
	{[]string{"event", "show"}, "mailbox event show ID", nil, "One event"},
	{[]string{"event", "add"}, "mailbox event add [TITLE] [--title TEXT] [--starts-on DATE] [--start-time HH:MM] [--ends-on DATE] [--end-time HH:MM] [--all-day] [--calendar NAME] [--circle] [--repeat ALIAS] [--repeat-until DATE] [--repeat-times N] [--location TEXT] [--notes TEXT] [--link URL] [--remind DURATION]", []string{"--title", "--starts-on", "--start-time", "--ends-on", "--end-time", "--all-day", "--calendar", "--circle", "--repeat", "--repeat-until", "--repeat-times", "--location", "--notes", "--link", "--remind"}, "Create an event"},
	{[]string{"event", "edit"}, "mailbox event edit ID [same flags as add]", []string{"--title", "--starts-on", "--start-time", "--ends-on", "--end-time", "--all-day", "--calendar", "--circle", "--repeat", "--repeat-until", "--repeat-times", "--location", "--notes", "--link", "--remind"}, "Change an event"},
	{[]string{"event", "delete"}, "mailbox event delete ID", nil, "Delete an event"},
	{[]string{"todo", "list"}, "mailbox todo list [--starts-on DATE] [--ends-on DATE] [--calendar NAME] [--limit N] [--all]", []string{"--starts-on", "--ends-on", "--calendar", "--limit", "--all", "--page"}, "Todos on Aufgaben"},
	{[]string{"todo", "add"}, "mailbox todo add --title TEXT [--date WHEN]", []string{"--title", "--date"}, "Add a todo"},
	{[]string{"todo", "complete"}, "mailbox todo complete ID", nil, "Mark complete"},
	{[]string{"todo", "uncomplete"}, "mailbox todo uncomplete ID", nil, "Mark incomplete"},
	{[]string{"todo", "delete"}, "mailbox todo delete ID", nil, "Delete a todo"},
	{[]string{"habit", "list"}, "mailbox habit list [--date WHEN]", []string{"--date"}, "Habits"},
	{[]string{"habit", "create"}, "mailbox habit create TITLE [--days DAYS] [--color TEXT] [--icon TEXT]", []string{"--days", "--color", "--icon"}, "Create a habit"},
	{[]string{"habit", "edit"}, "mailbox habit edit ID [--name TEXT] [--days DAYS] [--color TEXT] [--icon TEXT]", []string{"--name", "--days", "--color", "--icon"}, "Change a habit"},
	{[]string{"habit", "delete"}, "mailbox habit delete ID", nil, "Delete a habit"},
	{[]string{"habit", "complete"}, "mailbox habit complete ID [--date WHEN]", []string{"--date"}, "Tick a day"},
	{[]string{"habit", "uncomplete"}, "mailbox habit uncomplete ID [--date WHEN]", []string{"--date"}, "Untick a day"},
	{[]string{"contact", "list"}, "mailbox contact list", nil, "All contacts"},
	{[]string{"contact", "search"}, "mailbox contact search QUERY", nil, "Find a contact"},
	{[]string{"contact", "refresh"}, "mailbox contact refresh", nil, "Re-read CardDAV"},
	{[]string{"contact", "show"}, "mailbox contact show ID", nil, "One contact and note"},
	{[]string{"contact", "add"}, "mailbox contact add --name TEXT --email ADDR [--note TEXT]", []string{"--name", "--email", "--note"}, "Add a contact"},
	{[]string{"contact", "update"}, "mailbox contact update ID [--name TEXT] [--email ADDR] [--note TEXT]", []string{"--name", "--email", "--note"}, "Edit a contact"},
	{[]string{"sieve", "list"}, "mailbox sieve list", nil, "Scripts on the server"},
	{[]string{"sieve", "get"}, "mailbox sieve get [NAME] [--output PATH]", []string{"--output"}, "Print a script"},
	{[]string{"sieve", "put"}, "mailbox sieve put NAME FILE|-", nil, "Upload a script"},
	{[]string{"sieve", "activate"}, "mailbox sieve activate NAME", nil, "Set the active script"},
	{[]string{"doctor"}, "mailbox doctor", nil, "Credentials, IMAP, CalDAV, skill"},
	{[]string{"serve"}, "mailbox serve [--web] [--web-port N] [--interval S] [--print]", []string{"--web", "--web-port", "--interval", "--print"}, "Routing service"},
	{[]string{"tui"}, "mailbox tui [--screener]", []string{"--screener"}, "Interactive terminal UI"},
	{[]string{"setup"}, "mailbox setup", nil, "First-run wizard"},
	{[]string{"setup", "skill"}, "mailbox setup skill", nil, "Install the agent skill"},
	{[]string{"commands"}, "mailbox commands", nil, "Machine command index"},
	{[]string{"version"}, "mailbox version", nil, "Installed version"},
	{[]string{"help"}, "mailbox help [output|exit-codes|environment]", nil, "output | exit-codes | environment"},
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
			"short", s.short,
			"flags", flagVals,
		))
	}
	return rows
}

func cmdCommands(out *format.Output) (int, error) {
	return format.WriteOK(commandsPayload(), out, ""), nil
}

func cmdVersion(out *format.Output) (int, error) {
	return format.WriteOK(format.NewOM(
		"version", Version,
		"commit", Commit,
		"date", Date,
		"go", runtime.Version(),
	), out, ""), nil
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
