// Package bubble is the record a bubbled thread carries: an IMAP keyword that
// names the wall-clock instant the thread is due back in the Inbox.
//
// The keyword is the whole coordination mechanism. It is attached to the
// message, moves with it, and syncs like any other flag (labels are IMAP
// keywords too — see mailsync.Writer.SetLabel). The home Daemon and the
// always-on VPS Daemon both act on it without talking to each other: whichever
// one gets there first returns the thread, and the other syncs the result and
// finds nothing to do. This is ADR-0010's "raw is the record, the columns
// beside it are a projection" applied once more — the projection is
// placements.bubble_at. See docs/bubble-and-screener-handoff.md.
package bubble

import (
	"strings"
	"time"
)

// Prefix is what every return-time keyword starts with. What follows is a local
// wall-clock instant with no zone: the home machine and the VPS are assumed to
// be in the same timezone, and both compare against time.Now() in time.Local.
const Prefix = "$bubble-"

// Returned marks a thread a bubble has just brought back to the Inbox. The
// Inbox listing floats it to the top and badges it, the way HEY puts a
// bubbled-up thread at the top of the Imbox; it is cleared when the thread is
// next read or replied to, or on the next routing pass.
const Returned = "$bubbled"

// keywordLayout is how the instant is spelled inside the keyword: compact,
// minute resolution, no punctuation a server is entitled to fold or trim.
const keywordLayout = "20060102T1504"

// ProjectionLayout is how bubble_at is stored in the Mirror: sortable as text,
// so "due now" is a string comparison and the soonest-first order is an ORDER
// BY.
const ProjectionLayout = "2006-01-02T15:04"

// Keyword is the return-time keyword for an instant.
func Keyword(t time.Time) string {
	return Prefix + t.In(time.Local).Format(keywordLayout)
}

// Parse reads one keyword back to the instant it names. ok is false for a
// keyword that is not a return-time keyword, or carries a time this does not
// understand.
func Parse(keyword string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(keyword, Prefix)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(keywordLayout, rest, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Of finds the return-time keyword among a message's flags and returns the
// instant it names. A thread carries at most one: re-timing a bubble removes
// the old keyword and adds the new one in the same STORE.
func Of(flags []string) (time.Time, bool) {
	for _, f := range flags {
		if t, ok := Parse(f); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// KeywordOf returns the return-time keyword a message carries, empty when it
// carries none. It is what re-timing or an early return has to strip.
func KeywordOf(flags []string) string {
	for _, f := range flags {
		if strings.HasPrefix(f, Prefix) {
			return f
		}
	}
	return ""
}

// Projected is the bubble_at value for a flags list: the instant the keyword
// names in ProjectionLayout, or nil when the mail is not bubbled. It is what
// the Mirror derives wherever a placement's flags are written, so ADR-0013's
// rebuild repopulates the column from the keyword for free.
func Projected(flags []string) any {
	if t, ok := Of(flags); ok {
		return t.Format(ProjectionLayout)
	}
	return nil
}
