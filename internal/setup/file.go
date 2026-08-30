package setup

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The config file is the record (ADR-0021), and a person or an agent may have
// written parts of it. So setup edits it a block at a time: it appends the
// block it is adding and removes the block it is removing, and every other
// line — comments, hand-written tables, accounts it never touched — is left
// exactly as it was. Nothing here rewrites the file wholesale except the first
// write, when there is nothing to lose.

// blockAt finds `[header]` and returns the line range it covers, up to the next
// table header or the end of the file. ok is false when the header is absent.
func blockAt(lines []string, header string) (start, end int, ok bool) {
	want := "[" + header + "]"
	start = -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if trimmed == want {
				start = i
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			return start, i, true
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(lines), true
}

// withBlock replaces the block, or appends it when it is not there. An empty
// body removes it.
func withBlock(src, header, body string) string {
	lines := strings.Split(src, "\n")
	start, end, ok := blockAt(lines, header)
	if !ok {
		if body == "" {
			return src
		}
		out := strings.TrimRight(src, "\n")
		return out + "\n\n" + strings.TrimRight(body, "\n") + "\n"
	}
	// A comment block directly above the header belongs to it, and goes with it.
	for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
		start--
	}
	var out []string
	out = append(out, lines[:start]...)
	if body != "" {
		out = append(out, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
		out = append(out, "")
	}
	// Drop the blank lines the removed block left behind, so removing and
	// adding the same account twice does not walk the file down the page.
	rest := lines[end:]
	for len(out) > 0 && out[len(out)-1] == "" && len(rest) > 0 && rest[0] == "" {
		rest = rest[1:]
	}
	out = append(out, rest...)
	return strings.Join(out, "\n")
}

// editConfig reads, edits and writes back, 0600 throughout (ADR-0014).
func editConfig(path string, fn func(src string) string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return writeFile(path, fn(string(src)))
}

// writeFile writes 0600 from the moment the file exists rather than chmodding
// afterwards: between the two there is a moment where the password is readable
// (ADR-0014).
func writeFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// AccountBlock is one Secondary Account: another IMAP/SMTP login, under the
// name its ids are prefixed with (ADR-0005).
type AccountBlock struct {
	Name        string
	Email       string
	Password    string
	DisplayName string
	IMAPHost    string
	IMAPPort    int
	SMTPHost    string
	SMTPPort    int
}

func (a AccountBlock) body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Added by `mailbox setup`. Ids on this account read %s/INBOX:412.\n", a.Name)
	fmt.Fprintf(&b, "[accounts.%s]\n", a.Name)
	line(&b, "email", a.Email)
	line(&b, "password", a.Password)
	line(&b, "display_name", a.DisplayName)
	fmt.Fprintf(&b, "imap_host = %q\nimap_port = %d\n", a.IMAPHost, a.IMAPPort)
	if a.SMTPHost != "" {
		fmt.Fprintf(&b, "smtp_host = %q\nsmtp_port = %d\n", a.SMTPHost, a.SMTPPort)
	}
	return b.String()
}

// CalendarBlock is one Collection on a server this account's own DAV server
// cannot be asked about: another provider, with its own credentials. The URL in
// it was discovered by asking that server, never typed (ADR-0010).
type CalendarBlock struct {
	Key      string
	Name     string
	URL      string
	User     string
	Password string
	Kind     string
	Color    string
}

func (c CalendarBlock) body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Added by `mailbox setup`: found by asking %s, not typed.\n", hostOf(c.URL))
	fmt.Fprintf(&b, "[caldav.%s]\n", c.Key)
	line(&b, "name", c.Name)
	line(&b, "url", c.URL)
	line(&b, "user", c.User)
	line(&b, "password", c.Password)
	line(&b, "kind", c.Kind)
	line(&b, "color", c.Color)
	return b.String()
}

// AddAccount appends a Secondary Account.
func AddAccount(path string, a AccountBlock) error {
	return editConfig(path, func(src string) string {
		return withBlock(src, "accounts."+a.Name, a.body())
	})
}

// AddCalendar appends a foreign Collection.
func AddCalendar(path string, c CalendarBlock) error {
	return editConfig(path, func(src string) string {
		return withBlock(src, "caldav."+c.Key, c.body())
	})
}

// RemoveBlock takes one table out and leaves the rest of the file alone.
func RemoveBlock(path, header string) error {
	return editConfig(path, func(src string) string { return withBlock(src, header, "") })
}

// SetExcluded writes the Collections that are discovered but not mirrored.
//
// This lives in the config rather than on `dav_collections` because the Mirror
// is deleted and rebuilt on a schema change (ADR-0013): a decision stored there
// has a half-life, and the next discovery would put back what was excluded.
func SetExcluded(path string, names []string) error {
	return editConfig(path, func(src string) string {
		if len(names) == 0 {
			return withBlock(src, "collections", "")
		}
		var b strings.Builder
		b.WriteString("# Collections the servers offer and this machine does not mirror.\n")
		b.WriteString("# Excluded by name, because that is what discovery matches on.\n")
		b.WriteString("[collections]\n")
		fmt.Fprintf(&b, "exclude = [%s]\n", quoteList(names))
		return withBlock(src, "collections", b.String())
	})
}

func line(b *strings.Builder, key, value string) {
	if value != "" {
		fmt.Fprintf(b, "%s = %q\n", key, value)
	}
}

func hostOf(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(s, "/:"); i > 0 {
		return s[:i]
	}
	return s
}

func quoteList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, strconv.Quote(v))
	}
	return strings.Join(quoted, ", ")
}
