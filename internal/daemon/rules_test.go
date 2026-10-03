// The Routing on a Graph account: a decision writes the account's inbox rules
// and moves what waits, the screener is listed per account, and a decision
// never crosses accounts (ADR-0032).
package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// fakeRules is a Graph account's inbox rules in a slice, which is what the
// driver sees them as.
type fakeRules struct {
	rules []routing.Rule
	sets  int
	fail  error
}

func (f *fakeRules) Rules(ctx context.Context) ([]routing.Rule, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return f.rules, nil
}

func (f *fakeRules) SetRules(ctx context.Context, rules []routing.Rule) error {
	if f.fail != nil {
		return f.fail
	}
	f.sets++
	f.rules = rules
	return nil
}

// seedWorkScreener builds a Daemon whose Primary is an empty account and whose
// Secondary "work" is a Graph account with a Screener holding mail from two
// senders, and the catch-all rule a fresh account is set up with.
func seedWorkScreener(t *testing.T) (*Daemon, *fakeRules) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	f := mailsync.NewFake("INBOX")
	for _, name := range screenerBoxes[1:] {
		f.AddFolder(name)
	}
	f.AddFolder("Trash")
	f.Folder(routing.BoxScreener).UIDNext = 10

	tx, err := m.Begin("work")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	when := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	for _, mail := range []struct{ key, subject, from string }{
		{"a1@example.com", "Newsletter #41", "Beispiel News <news@example.com>"},
		{"b1@example.com", "Ihre Rechnung", "Rechnungen <bills@example.com>"},
	} {
		when = when.Add(time.Hour)
		id, _, err := tx.UpsertMessage(mirror.Message{
			Key: mail.key, Date: when, Subject: mail.subject, From: mail.from,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SetBody(id, mail.subject, "", mail.subject); err != nil {
			t.Fatal(err)
		}
		msg := f.Deliver(routing.BoxScreener, mail.key, mail.subject, mail.subject)
		msg.From, msg.Date = mail.from, when
		if err := tx.PutPlacement(mirror.Placement{
			Folder: routing.BoxScreener, UID: msg.UID, MessageID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	d := New("primary", m,
		// A Reconciler, so the Primary has a Writer: the cross-account refusal
		// below has to be reached, not short-circuited by a daemon that cannot
		// write at all.
		&mailsync.Reconciler{Account: "primary", Mirror: m, Driver: f}, nil, nil, nil)
	acct := NewAccount("work",
		&mailsync.Reconciler{Account: "work", Mirror: m, Driver: f},
		&mailsync.Writer{Account: "work", Mirror: m, Driver: f, Mirrored: screenerBoxes},
		screenerBoxes, nil)
	acct.Graph = true
	rules := &fakeRules{rules: routing.New().Rules()}
	acct.Routing = rules
	d.StartAccount(acct)
	return d, rules
}

// The screener answers per account: work's waiting senders under --account,
// and an account without a Screener box refuses rather than listing nothing.
func TestScreenerIsPerAccount(t *testing.T) {
	d, _ := seedWorkScreener(t)
	resp := mustAsk(t, d, []string{"screener"}, map[string]any{"account": "work"})
	got := resp.Data.([]waiting)
	if len(got) != 2 || got[0].Address != "bills@example.com" {
		t.Fatalf("screener = %+v", got)
	}
	resp = ask(t, d, []string{"screener"}, nil)
	if resp.OK || !strings.Contains(resp.Error, "no INBOX/Screener box") {
		t.Errorf("the screener-less primary answered %+v", resp)
	}
}

// A decision on work writes work's rules — the feed rule and the catch-all —
// and moves what was waiting. The Primary's sieve is never touched, because
// there isn't one: this account has none, and the call must not need it.
func TestRouteOnAGraphAccountWritesItsRules(t *testing.T) {
	d, rules := seedWorkScreener(t)
	resp := mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{"work/Screener:10"}, "to": "feed",
	})
	got, ok := resp.Data.([]decision)
	if !ok || len(got) != 1 || got[0].Address != "news@example.com" {
		t.Fatalf("route returned %+v", resp.Data)
	}
	if rules.sets != 1 {
		t.Fatalf("the rules were written %d times", rules.sets)
	}
	l := routing.ListsFromRules(rules.rules)
	if l.Of("news@example.com") != routing.Feed {
		t.Errorf("rules do not route them: %+v", rules.rules)
	}
	if !routing.HasCatchAll(rules.rules) {
		t.Errorf("the catch-all was dropped: %+v", rules.rules)
	}
	routes, err := d.Mirror.Routing("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Address != "news@example.com" || routes[0].Box != routing.BoxFeed {
		t.Errorf("mirror holds %+v", routes)
	}
	rows, err := d.Mirror.Rows("work", routing.BoxScreener, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("%d mails left in the screener, want 1", len(rows))
	}
	// And the listing answers offline from the projection.
	resp = mustAsk(t, d, []string{"route"}, map[string]any{"account": "work"})
	view := resp.Data.(routingView)
	if len(view.Routes) != 1 || !view.Active {
		t.Errorf("route list = %+v", view)
	}
}

// A domain decision is written as a senderDomains rule, which is what the
// server matches every address at that domain with.
func TestRouteOnAGraphAccountWritesDomainRules(t *testing.T) {
	d, rules := seedWorkScreener(t)
	mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{"@example.com"}, "to": "block", "account": "work",
	})
	var domains []string
	for _, r := range rules.rules {
		domains = append(domains, r.Domains...)
	}
	if !slicesContains(domains, "example.com") {
		t.Errorf("no senderDomains rule for example.com: %+v", rules.rules)
	}
}

// A decision refused by the account's server leaves everything as it was —
// the rules on the server are the record, and a write that did not happen
// writes no projection.
func TestRefusedRulesLeaveTheMirrorAlone(t *testing.T) {
	d, rules := seedWorkScreener(t)
	rules.fail = errors.New("graph 500 oops")
	resp := ask(t, d, []string{"route"}, map[string]any{
		"positional": []any{"work/Screener:10"}, "to": "feed",
	})
	if resp.OK {
		t.Fatalf("route succeeded against a broken server")
	}
	if _, err := d.Mirror.RoutingScript("work"); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("the mirror holds a routing for a write that never happened: %v", err)
	}
	if len(rules.rules) != 1 || !rules.rules[0].CatchAll {
		t.Errorf("the rules changed anyway: %+v", rules.rules)
	}
}

// A decision made on one account is never written onto another's lists: a
// target naming work and --account home contradict each other.
func TestRouteRefusesACrossAccountDecision(t *testing.T) {
	d, rules := seedWorkScreener(t)
	resp := ask(t, d, []string{"route"}, map[string]any{
		"positional": []any{"work/Screener:10"}, "to": "feed", "account": "primary",
	})
	if resp.OK || !strings.Contains(resp.Error, "one decision is made on one account") {
		t.Fatalf("resp = %+v", resp)
	}
	if rules.sets != 0 {
		t.Errorf("the rules were written anyway")
	}
}

func slicesContains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// An IMAP Secondary has no Routing of its own, and the fall-through to the
// Primary's Sieve would let a decision made on it rewrite the Primary's
// script — so it refuses, whatever the destination.
func TestRouteRefusedOnAnIMAPSecondary(t *testing.T) {
	d, rules := seedWorkScreener(t)
	acct := NewAccount("gmx",
		&mailsync.Reconciler{Account: "gmx", Mirror: d.Mirror, Driver: mailsync.NewFake("INBOX")},
		&mailsync.Writer{Account: "gmx", Mirror: d.Mirror, Driver: mailsync.NewFake("INBOX"), Mirrored: []string{"INBOX"}},
		[]string{"INBOX"}, nil)
	d.StartAccount(acct)
	for _, to := range []string{"block", "inbox", "feed"} {
		resp := ask(t, d, []string{"route"}, map[string]any{
			"positional": []any{"news@example.com"}, "to": to, "account": "gmx",
		})
		if resp.OK || !strings.Contains(resp.Error, "routing belongs to the primary") {
			t.Fatalf("--account gmx --to %s: %+v", to, resp)
		}
	}
	if rules.sets != 0 {
		t.Errorf("work's rules were written anyway")
	}
}

// A rule that cannot be read back is a decision refused: writing would carry
// over only the readable decisions and forget the rest for good.
func TestRouteRefusedWhenRulesCannotBeReadBack(t *testing.T) {
	d, rules := seedWorkScreener(t)
	rules.rules = append(rules.rules, routing.Rule{Name: routing.RuleName + " feed"})
	resp := ask(t, d, []string{"route"}, map[string]any{
		"positional": []any{"work/Screener:10"}, "to": "feed",
	})
	if resp.OK || !strings.Contains(resp.Error, "cannot be read back") {
		t.Fatalf("resp = %+v", resp)
	}
	if rules.sets != 0 {
		t.Errorf("the rules were written anyway")
	}
}
