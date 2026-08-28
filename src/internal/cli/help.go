package cli

import (
	"fmt"
	"strings"
)

const overview = `mailbox — mailbox.org mail, contacts, calendars, todos, habits

MAIL
  box          List boxes / mail in a box
  search       Search mail
  thread       Read a thread
  reply        Reply
  forward      Forward the latest message
  compose      Write and send
  draft        Unsent drafts
  attachment   List and save files
  screener     First-time senders
  move         Move to inbox|feed|trail|block
  aside        Read-later pile
  label        Labels on mail
  seen         Mark seen
  unseen       Mark unseen
  trash        Move to Trash
  spam         Move to Junk

CALENDARS
  calendar     Discovered calendars
  event        Events
  habit        Repeating per-day practices

TODOS
  todo         Todos on Aufgaben

CONTACTS
  contact      CardDAV Kontakte

META
  tui          Interactive terminal UI
  sieve        ManageSieve scripts
  setup        First-run wizard
  doctor       Credentials, IMAP, CalDAV, skill
  serve        Routing service
  commands     Machine command index
  version      Installed version
  help         output | exit-codes | environment

Mail IDs: 36722, feed:12, Drafts:12.
Boxes: inbox, feed, trail, screener, aside, archive, drafts, sent.

  mailbox <command> --help
  mailbox help output|exit-codes|environment
`

var flagDesc = map[string]string{
	"--archive":       "Archive tree, not routing boxes",
	"--unread":        "unseen only",
	"--limit":         "page size (default 50)",
	"--page":          "continue from next_page",
	"--all":           "every page",
	"--detail":        "include flags in the table",
	"--from":          "sender",
	"--to":            "recipient",
	"--subject":       "subject",
	"--in":            "box (see search filters)",
	"--date":          "last_7_days|last_30_days|last_90_days|YYYY",
	"--required":      "words that must all appear",
	"--any":           "at least one of these words",
	"--none":          "words that must not appear",
	"--exact":         "exact phrase",
	"--attachment":    "kind (see search filters)",
	"--allow-partial": "incomplete thread is still a result",
	"--html":          "original HTML",
	"-m":              "body as Markdown",
	"--message":       "body as Markdown",
	"--message-html":  "body as raw HTML",
	"--attach":        "file (repeatable)",
	"--upload-large":  "send files over 10 MiB via a third-party host as a link",
	"--draft":         "save to Drafts instead of sending",
	"--cc":            "CC",
	"--bcc":           "BCC",
	"--box":           "inbox|feed|trail",
	"--spam":          "still Block; no spam trainer",
	"--remind":        "duration (30m, 2h, 3d)",
	"--sweep":         "return due Aside mail now",
	"--output":        "dest path",
	"--force":         "overwrite",
	"--starts-on":     "YYYY-MM-DD",
	"--ends-on":       "YYYY-MM-DD",
	"--calendar":      "calendar name",
	"--title":         "title",
	"--start-time":    "HH:MM",
	"--end-time":      "HH:MM",
	"--all-day":       "all-day event",
	"--circle":        "PRIORITY=1",
	"--repeat":        "every_day|every_weekday|every_week|every_other_week|every_day_of_month|every_year",
	"--repeat-until":  "YYYY-MM-DD",
	"--repeat-times":  "occurrence count",
	"--location":      "location",
	"--notes":         "notes",
	"--link":          "URL",
	"--name":          "name",
	"--days":          "mon,wed,fri or 0-6",
	"--color":         "color",
	"--icon":          "icon",
	"--email":         "email address",
	"--note":          "vCard NOTE",
	"--screener":      "open The Screener",
	"--web":           "list-management UI",
	"--web-port":      "UI port (default 8080)",
	"--web-addr":      "UI bind address (default 127.0.0.1; the UI has no auth)",
	"--interval":      "poll seconds",
	"--print":         "print Sieve script instead of uploading",
}

func flagHelp(path []string, flag string) string {
	if flag == "--to" && len(path) > 0 && path[0] == "move" {
		return "inbox|feed|trail|block"
	}
	if flag == "--to" && len(path) > 0 && path[0] == "label" {
		return "label name"
	}
	if flag == "--from" && len(path) > 0 && path[0] == "label" {
		return "label name or all"
	}
	if flag == "--date" && len(path) > 0 && path[0] != "search" {
		return "YYYY-MM-DD"
	}
	return flagDesc[flag]
}

func helpText(argv []string) string {
	if len(argv) == 0 {
		return overview
	}
	var exact *cmdspec
	var children []cmdspec
	seen := map[string]bool{}
	for i, s := range cmdSpecs {
		if sameStrings(s.path, argv) {
			exact = &cmdSpecs[i]
			continue
		}
		if !prefixOf(argv, s.path) || len(s.path) != len(argv)+1 {
			continue
		}
		name := s.path[len(argv)]
		if seen[name] {
			continue
		}
		seen[name] = true
		children = append(children, s)
	}
	if exact != nil {
		for _, c := range children {
			if c.usage == exact.usage {
				exact = nil
				break
			}
		}
	}
	if exact == nil && len(children) == 0 {
		return ""
	}

	var b strings.Builder
	if len(children) > 0 {
		if exact != nil && exact.short != "" {
			fmt.Fprintf(&b, "%s\n\n", exact.short)
		}
		if exact != nil {
			fmt.Fprintf(&b, "usage: %s\n\n", exact.usage)
		} else {
			fmt.Fprintf(&b, "usage: mailbox %s <command>\n\n", strings.Join(argv, " "))
		}
		width := 0
		for _, c := range children {
			if n := len(c.path[len(argv)]); n > width {
				width = n
			}
		}
		for _, c := range children {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, c.path[len(argv)], c.short)
		}
		if exact != nil && len(exact.flags) > 0 {
			b.WriteByte('\n')
			writeFlags(&b, *exact)
		}
		return b.String()
	}
	if exact.short != "" {
		fmt.Fprintf(&b, "%s\n\n", exact.short)
	}
	fmt.Fprintf(&b, "usage: %s\n", exact.usage)
	if len(exact.flags) > 0 {
		b.WriteByte('\n')
		writeFlags(&b, *exact)
	}
	return b.String()
}

func writeFlags(b *strings.Builder, s cmdspec) {
	width := 0
	for _, f := range s.flags {
		if n := len(f); n > width {
			width = n
		}
	}
	for _, f := range s.flags {
		fmt.Fprintf(b, "  %-*s  %s\n", width, f, flagHelp(s.path, f))
	}
}
