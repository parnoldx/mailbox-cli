// The Rules rendering and ListsFromRules are each other's inverse over the
// decisions, in the same order the Sieve script matches them.
package routing

import (
	"slices"
	"testing"
)

func TestRulesRoundTrip(t *testing.T) {
	l := New()
	for _, d := range []struct {
		key string
		to  Destination
	}{
		{"spam@example.net", Block},
		{"@junk.example", Block},
		{"boss@example.org", Inbox},
		{"bills@example.com", PaperTrail},
		{"@receipts.example", PaperTrail},
		{"news@example.com", Feed},
	} {
		if _, err := l.Set(d.key, d.to); err != nil {
			t.Fatal(err)
		}
	}
	rules := l.Rules()
	// Address rules first, then domain rules, then the catch-all — the order
	// the script writes and the first match wins by.
	var got []string
	for _, r := range rules {
		got = append(got, r.Name)
	}
	want := []string{
		RuleName + " block", RuleName + " inbox", RuleName + " paper",
		RuleName + " feed", RuleName + " block (domains)",
		RuleName + " paper (domains)", RuleName + " screener",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rules in order %v, want %v", got, want)
	}
	back := ListsFromRules(rules)
	for a, wantTo := range map[string]Destination{
		"spam@example.net": Block, "anything@junk.example": Block,
		"boss@example.org": Inbox, "bills@example.com": PaperTrail,
		"x@receipts.example": PaperTrail, "news@example.com": Feed,
		"nobody@example.net": None,
	} {
		if back.Of(a) != wantTo {
			t.Errorf("%s: Of = %v, want %v", a, back.Of(a), wantTo)
		}
	}
	if !HasCatchAll(rules) {
		t.Errorf("no catch-all in %v", rules)
	}
}

// A list with nobody on it is no rule, and a decision removed takes its rule
// with it — the same shape the script has.
func TestEmptyListsAreNoRules(t *testing.T) {
	l := New()
	if _, err := l.Set("news@example.com", Feed); err != nil {
		t.Fatal(err)
	}
	rules := l.Rules()
	if len(rules) != 2 {
		t.Fatalf("%d rules, want the feed one and the catch-all", len(rules))
	}
	if _, err := l.Set("news@example.com", None); err != nil {
		t.Fatal(err)
	}
	if rules = l.Rules(); len(rules) != 1 || !rules[0].CatchAll {
		t.Fatalf("rules = %+v, want only the catch-all", rules)
	}
}
