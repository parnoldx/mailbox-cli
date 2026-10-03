// EnsureGraphRouting bootstraps a Microsoft 365 account's Routing: the
// missing Boxes, then the catch-all rule when no `mailbox:` rule exists yet.
package setup

import (
	"context"
	"slices"
	"testing"

	"mailbox/internal/routing"
)

// rulesFake is the scripted inbox-rules surface beside the Boxes.
type rulesFake struct {
	created []string
	rules   []routing.Rule
	sets    [][]routing.Rule
}

func (f *rulesFake) CreateFolder(_ context.Context, name string) error {
	f.created = append(f.created, name)
	return nil
}

func (f *rulesFake) Rules(context.Context) ([]routing.Rule, error) {
	return f.rules, nil
}

func (f *rulesFake) SetRules(_ context.Context, rules []routing.Rule) error {
	f.sets = append(f.sets, rules)
	f.rules = rules
	return nil
}

func TestAFreshGraphAccountGetsTheBoxesAndTheCatchAll(t *testing.T) {
	f := &rulesFake{}
	b, err := EnsureGraphRouting(context.Background(), f, f, []string{"INBOX", "Sent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Created) != 6 {
		t.Fatalf("created %v", b.Created)
	}
	if b.Created[0] != routing.BoxScreener || !slices.Contains(b.Created, routing.BoxBlock) {
		t.Fatalf("created %v", b.Created)
	}
	// No `mailbox:` rule existed, so the catch-all goes up.
	if !b.Wrote || len(f.sets) != 1 {
		t.Fatalf("wrote=%v sets=%d", b.Wrote, len(f.sets))
	}
	if len(f.sets[0]) != 1 || !f.sets[0][0].CatchAll {
		t.Fatalf("seeded %+v, want only the catch-all", f.sets[0])
	}
}

func TestAGraphAccountWithOurRulesIsLeftAlone(t *testing.T) {
	f := &rulesFake{}
	ours := routing.Rule{Name: routing.RuleName + " feed", Dest: routing.Feed, From: []string{"news@example.com"}}
	f.rules = []routing.Rule{ours, {Name: routing.RuleName + " screener", Dest: routing.None, CatchAll: true}}
	have := append([]string{"INBOX"}, RoutingBoxes...)
	b, err := EnsureGraphRouting(context.Background(), f, f, have)
	if err != nil {
		t.Fatal(err)
	}
	if b.Wrote || len(f.sets) != 0 || len(b.Created) != 0 {
		t.Fatalf("bootstrap rewrote an existing Routing: wrote=%v sets=%d created=%v", b.Wrote, len(f.sets), b.Created)
	}
}
