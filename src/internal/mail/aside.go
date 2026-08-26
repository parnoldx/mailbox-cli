package mail

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mailbox/src/internal/folders"
	"mailbox/src/internal/ids"
)

const asideDueLayout = "20060102T1504Z"

var (
	asideDueRe   = regexp.MustCompile(`(?i)\basidedue-(\d{8}T\d{4}Z)\b`)
	remindSpecRe = regexp.MustCompile(`^(\d+)([mhd])$`)
)

// ParseRemind turns a --remind spec (30m, 2h, 3d) into an absolute UTC due time.
func ParseRemind(spec string) (time.Time, error) {
	m := remindSpecRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(spec)))
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid duration %q; use e.g. 30m, 2h, 3d", spec)
	}
	n, _ := strconv.Atoi(m[1])
	var d time.Duration
	switch m[2] {
	case "m":
		d = time.Duration(n) * time.Minute
	case "h":
		d = time.Duration(n) * time.Hour
	default:
		d = time.Duration(n) * 24 * time.Hour
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("duration must be positive")
	}
	return time.Now().Add(d).UTC(), nil
}

func asideDueKeyword(due time.Time) string {
	return "asidedue-" + due.UTC().Format(asideDueLayout)
}

// ParseAsideDue extracts the reminder due time from message flags, if present.
func ParseAsideDue(flags []string) (time.Time, bool) {
	for _, f := range flags {
		if m := asideDueRe.FindStringSubmatch(f); m != nil {
			t, err := time.Parse(asideDueLayout, strings.ToUpper(m[1]))
			if err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// setKeyword stores or clears one keyword atom on a message.
func (m *Mail) setKeyword(folder, uid, keyword string, on bool) error {
	if err := m.Select(folder, false); err != nil {
		return err
	}
	c, _ := m.client()
	op := "+FLAGS"
	if !on {
		op = "-FLAGS"
	}
	st, err := c.Command("UID", "STORE", uid, op, "("+keyword+")")
	if err != nil || st.Status != "OK" {
		return fmt.Errorf("cannot flag %s:%s %s", folder, uid, keyword)
	}
	return nil
}

// Aside moves a Message into the Aside pile, optionally with a due keyword
// that serve's sweeper later uses to move it back to the Inbox.
func (m *Mail) Aside(folder, uid string, remind *time.Time) (string, error) {
	newID, err := m.Screen(folder, uid, folders.ASIDE)
	if err != nil {
		return "", err
	}
	if remind != nil {
		df, du, err := ids.ParseMessageID(newID)
		if err != nil {
			return "", err
		}
		if err := m.setKeyword(df, du, asideDueKeyword(*remind), true); err != nil {
			return "", err
		}
	}
	return newID, nil
}

// Unaside returns a Message to the Inbox and strips any due keyword.
func (m *Mail) Unaside(folder, uid string) (string, error) {
	newID, err := m.Screen(folder, uid, folders.INBOX)
	if err != nil {
		return "", err
	}
	if df, du, err := ids.ParseMessageID(newID); err == nil {
		m.clearAsideDue(df, du)
	}
	return newID, nil
}

func (m *Mail) clearAsideDue(folder, uid string) error {
	if err := m.Select(folder, true); err != nil {
		return err
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", uid, "(FLAGS)")
	if err != nil || resp.Status != "OK" {
		return fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	for _, rec := range splitFetchChunks(resp.Chunks) {
		for _, f := range parseFlags(rec.meta) {
			if asideDueRe.MatchString(f) {
				if err := m.setKeyword(folder, uid, f, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// AsideReturn describes a message the sweeper moved back to the Inbox.
type AsideReturn struct {
	ID  string
	Due time.Time
}

// SweepAside moves every pile message whose due time has passed back to the
// Inbox. Returns what moved.
func (m *Mail) SweepAside(now time.Time) ([]AsideReturn, error) {
	listing, err := m.ListMessages(folders.ASIDE, false, nil)
	if err != nil {
		return nil, err
	}
	var out []AsideReturn
	for _, e := range listing.Items {
		due, ok := ParseAsideDue(e.Flags)
		if !ok || due.After(now) {
			continue
		}
		newID, err := m.Unaside(e.Folder, e.UID)
		if err != nil {
			return out, err
		}
		out = append(out, AsideReturn{ID: newID, Due: due})
	}
	return out, nil
}
