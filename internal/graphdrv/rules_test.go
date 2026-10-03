// The Routing on a Graph account: rules written whole, read back, and rules a
// human wrote left alone (ADR-0032).
package graphdrv

import (
	"context"
	"testing"

	"mailbox/internal/routing"
)

func rulesSetup(t *testing.T) (*fakeGraph, *Mail) {
	t.Helper()
	f, mail, _, _ := mailSetup(t)
	ctx := context.Background()
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	// The Routing Boxes: setup makes them, and every rule names one of them —
	// a decision never creates a Box (ADR-0019).
	for _, name := range []string{routing.BoxScreener, routing.BoxFeed, routing.BoxPaperTrail, routing.BoxBlock} {
		if err := mail.CreateFolder(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	return f, mail
}

// A fresh account gets the catch-all and nothing else: every sender is
// undecided, so every sender's mail waits in the Screener.
func TestEmptyRoutingIsOneCatchAllRule(t *testing.T) {
	_, mail := rulesSetup(t)
	ctx := context.Background()
	if err := mail.SetRules(ctx, routing.New().Rules()); err != nil {
		t.Fatal(err)
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].CatchAll || got[0].Dest != routing.None {
		t.Fatalf("rules = %+v, want one catch-all", got)
	}
	if l := routing.ListsFromRules(got); l.Count() != 0 {
		t.Errorf("an empty Routing read back with %d decisions", l.Count())
	}
}

// A decision is written as the rule the sieve script would have been, and it
// reads back as the same decision.
func TestDecisionsRoundTripThroughRules(t *testing.T) {
	_, mail := rulesSetup(t)
	ctx := context.Background()
	l := routing.New()
	for _, d := range []struct {
		key string
		to  routing.Destination
	}{
		{"news@example.com", routing.Feed},
		{"bills@example.com", routing.PaperTrail},
		{"@stripemail.example", routing.PaperTrail},
		{"spam@example.net", routing.Block},
		{"boss@example.org", routing.Inbox},
	} {
		if _, err := l.Set(d.key, d.to); err != nil {
			t.Fatal(err)
		}
	}
	if err := mail.SetRules(ctx, l.Rules()); err != nil {
		t.Fatal(err)
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	back := routing.ListsFromRules(got)
	for _, a := range []string{
		"news@example.com", "bills@example.com", "spam@example.net",
		"boss@example.org", "anything@stripemail.example", "other@example.com",
	} {
		if back.Of(a) != l.Of(a) {
			t.Errorf("%s: Of = %v, want %v", a, back.Of(a), l.Of(a))
		}
	}
	if !routing.HasCatchAll(got) {
		t.Errorf("the catch-all did not survive the write")
	}
	// Feed and Paper Trail are read on arrival, Block is deleted, Inbox is
	// neither — the same semantics the script writes.
	for _, r := range got {
		switch {
		case r.CatchAll || r.Dest == routing.Block:
		case r.Dest.Seen() != (r.Dest == routing.Feed || r.Dest == routing.PaperTrail):
			t.Errorf("%s: seen = %v", r.Name, r.Dest.Seen())
		}
	}
}

// A rule without the mailbox: prefix is somebody's, and a write of ours
// replaces only ours.
func TestSetRulesLeavesForeignRulesAlone(t *testing.T) {
	f, mail := rulesSetup(t)
	ctx := context.Background()
	// The rule's move target has to exist: a decision never makes a Box
	// (ADR-0019), and neither does a test.
	if err := mail.CreateFolder(ctx, routing.BoxFeed); err != nil {
		t.Fatal(err)
	}
	f.rules = append(f.rules, fakeRule{id: "r1", body: map[string]any{
		"displayName": "move newsletters",
		"sequence":    1.0,
		"isEnabled":   true,
	}})
	l := routing.New()
	if _, err := l.Set("news@example.com", routing.Feed); err != nil {
		t.Fatal(err)
	}
	if err := mail.SetRules(ctx, l.Rules()); err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 3 || f.rules[0].body["displayName"] != "move newsletters" {
		t.Fatalf("foreign rule touched: %+v", f.rules)
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d of our rules read back, want 2 (foreign one excluded)", len(got))
	}
}

// A write replaces the whole set: two writes leave one catch-all, not two —
// a decision that forgets to delete the old rules would double every list.
func TestSetRulesReplaces(t *testing.T) {
	f, mail := rulesSetup(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		l := routing.New()
		if _, err := l.Set("news@example.com", routing.Feed); err != nil {
			t.Fatal(err)
		}
		if err := mail.SetRules(ctx, l.Rules()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d rules after two identical writes, want 2", len(got))
	}
	if len(f.rules) != 2 {
		t.Errorf("the server holds %d rules, want 2", len(f.rules))
	}
}

// A rule whose move target is a folder this program no longer knows is read
// back unreadable rather than dropped, and Unreadable says so — the caller
// refuses to decide on an account whose Routing it cannot read whole.
func TestUnreadableRulesAreCarriedNotDropped(t *testing.T) {
	f, mail := rulesSetup(t)
	f.rules = append(f.rules, fakeRule{id: "r1", body: map[string]any{
		"displayName": routing.RuleName + " feed",
		"sequence":    1.0,
		"isEnabled":   true,
		"conditions":  map[string]any{},
		"actions":     map[string]any{"moveToFolder": "f-gone"},
	}})
	got, err := mail.Rules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Dest != "" {
		t.Fatalf("rules = %+v, want the unreadable one carried", got)
	}
	if !routing.Unreadable(got) {
		t.Errorf("Unreadable missed %+v", got)
	}
}

// A create refused halfway through rolls the created rules back: the old set
// stays the record, and what the server is left with is what it had.
func TestFailedWriteLeavesTheOldRules(t *testing.T) {
	f, mail := rulesSetup(t)
	ctx := context.Background()
	before, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	l := routing.New()
	if _, err := l.Set("news@example.com", routing.Feed); err != nil {
		t.Fatal(err)
	}
	// The routing writes the feed rule, then the catch-all; the second create
	// is the one that dies.
	f.failPost = 2
	if err := mail.SetRules(ctx, l.Rules()); err == nil {
		t.Fatalf("a refused write succeeded")
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(before) || routing.ListsFromRules(got).Of("news@example.com") != routing.None {
		t.Fatalf("the server was left with %+v, want the old set", got)
	}
}

// A delete of the old set that fails is an error, not a success — old and new
// sets running side by side is two Routings — and a second write clears the
// leftovers, because ours are replaced whole.
func TestFailedDeleteIsAnErrorAndTheNextWriteClearsIt(t *testing.T) {
	f, mail := rulesSetup(t)
	ctx := context.Background()
	l := routing.New()
	if _, err := l.Set("news@example.com", routing.Feed); err != nil {
		t.Fatal(err)
	}
	// A first write puts a set up; the second write is the one whose delete of
	// the old set fails.
	if err := mail.SetRules(ctx, l.Rules()); err != nil {
		t.Fatal(err)
	}
	f.failDelete = 1
	if err := mail.SetRules(ctx, l.Rules()); err == nil {
		t.Fatalf("a failed delete of the old set reported success")
	}
	if n := len(f.rules); n < 3 {
		t.Fatalf("only %d rules left; old and new sets should both be there", n)
	}
	// The same decision again: ours are replaced whole, so the leftovers go.
	if err := mail.SetRules(ctx, l.Rules()); err != nil {
		t.Fatal(err)
	}
	if got, err := mail.Rules(ctx); err != nil || len(got) != 2 {
		t.Fatalf("rules after the retry = %+v (%v), want exactly the new set", got, err)
	}
}
