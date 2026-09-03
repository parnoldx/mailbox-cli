//go:build live

// Live check for the bubble slice: the return time is a custom IMAP keyword,
// and this is the one thing a fake cannot answer — whether mailbox.org's
// Dovecot keeps a `$bubble-*` keyword it has never seen before.
//
//	go test -tags live ./internal/imapdrv/ -run TestLiveBubbleKeyword -v
//
// It works in INBOX/mailbox-selftest, appends one message, stores the keyword,
// reads it back on a fresh connection, re-times it (one keyword at a time), and
// strips it. It also asserts `\*` is in the folder's PERMANENTFLAGS, which is
// what makes an arbitrary keyword storable at all.
package imapdrv

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"mailbox/internal/bubble"
)

func TestLiveBubbleKeyword(t *testing.T) {
	cfg := loadLive(t)
	o := dialOther(t, cfg)
	o.ensure()
	o.purge()
	t.Cleanup(o.purge)

	// `\*` in PERMANENTFLAGS is the server saying it will keep keywords it was
	// not told about in advance. Without it a $bubble-* STORE is silently a
	// no-op and every return is lost.
	sel, err := o.c.Select(liveFolder, nil).Wait()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	starred := false
	for _, f := range sel.PermanentFlags {
		if f == imap.FlagWildcard {
			starred = true
		}
	}
	if !starred {
		t.Fatalf("%s PERMANENTFLAGS has no \\*: %v", liveFolder, sel.PermanentFlags)
	}

	o.append("bubble-keyword")
	uids := o.uids()
	if len(uids) != 1 {
		t.Fatalf("scratch folder holds %d messages, want 1", len(uids))
	}
	uid := uids[0]

	first := bubble.Keyword(time.Date(2026, 9, 10, 8, 0, 0, 0, time.Local))
	storeKeyword(t, o, uid, first, imap.StoreFlagsAdd)

	// Read it back on a fresh connection — the record has to survive the
	// connection that wrote it going away, which is how the two Daemons see it.
	o.reconnect()
	got := fetchKeywords(t, o, uid)
	if !has(got, first) {
		t.Fatalf("the server dropped %q: flags are %v", first, got)
	}
	when, ok := bubble.Parse(first)
	if !ok || when.Hour() != 8 {
		t.Fatalf("keyword %q did not round-trip through bubble.Parse", first)
	}

	// Re-time: add the new keyword, remove the old, and end with exactly one.
	second := bubble.Keyword(time.Date(2026, 9, 17, 8, 0, 0, 0, time.Local))
	storeKeyword(t, o, uid, second, imap.StoreFlagsAdd)
	storeKeyword(t, o, uid, first, imap.StoreFlagsDel)
	got = fetchKeywords(t, o, uid)
	n := 0
	for _, f := range got {
		if _, ok := bubble.Parse(f); ok {
			n++
		}
	}
	if n != 1 || !has(got, second) {
		t.Fatalf("after a re-time the message carries %d bubble keywords (%v), want just %q", n, got, second)
	}

	// Strip it, the way an early return does.
	storeKeyword(t, o, uid, second, imap.StoreFlagsDel)
	if got = fetchKeywords(t, o, uid); has(got, second) {
		t.Fatalf("the keyword survived a strip: %v", got)
	}
}

func storeKeyword(t *testing.T, o *otherClient, uid imap.UID, keyword string, op imap.StoreFlagsOp) {
	t.Helper()
	o.do("store "+keyword, func() error {
		if _, err := o.c.Select(liveFolder, nil).Wait(); err != nil {
			return err
		}
		return o.c.Store(imap.UIDSetNum(uid), &imap.StoreFlags{
			Op: op, Flags: []imap.Flag{imap.Flag(keyword)},
		}, nil).Close()
	})
}

func fetchKeywords(t *testing.T, o *otherClient, uid imap.UID) []string {
	t.Helper()
	var out []string
	o.do("fetch flags", func() error {
		if _, err := o.c.Select(liveFolder, nil).Wait(); err != nil {
			return err
		}
		msgs, err := o.c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{Flags: true}).Collect()
		if err != nil {
			return err
		}
		out = nil
		for _, m := range msgs {
			for _, f := range m.Flags {
				out = append(out, string(f))
			}
		}
		return nil
	})
	return out
}

func has(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
