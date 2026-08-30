package setup

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/routing"
)

// account is the scripted ManageSieve server and the Boxes beside it.
type sieveFake struct {
	scripts map[string]string
	active  string
	created []string
	puts    []string
}

func (f *sieveFake) CreateFolder(_ context.Context, name string) error {
	f.created = append(f.created, name)
	return nil
}

func (f *sieveFake) Scripts(context.Context) ([]string, string, error) {
	var names []string
	for n := range f.scripts {
		names = append(names, n)
	}
	return names, f.active, nil
}

func (f *sieveFake) Script(_ context.Context, name string) (string, error) {
	return f.scripts[name], nil
}

func (f *sieveFake) PutScript(_ context.Context, name, content string, activate bool) error {
	f.scripts[name] = content
	f.puts = append(f.puts, name)
	if activate {
		f.active = name
	}
	return nil
}

func TestAFreshAccountGetsTheBoxesAndAnEmptyScript(t *testing.T) {
	f := &sieveFake{scripts: map[string]string{}}
	b, err := EnsureRouting(context.Background(), f, f, []string{"INBOX", "Sent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Created) != 5 {
		t.Fatalf("created %v", b.Created)
	}
	// The names are the ones `mailbox route` files into. A Screener under
	// another name is not a Screener, so nothing here is asked.
	if b.Created[0] != routing.BoxScreener || !contains(b.Created, routing.BoxBlock) {
		t.Fatalf("created %v", b.Created)
	}
	// An account running nothing gets ours switched on; that is the only case
	// where anything is activated (ADR-0019).
	if !b.Wrote || !b.Activated || f.active != routing.ScriptName {
		t.Fatalf("bootstrap = %+v, active = %q", b, f.active)
	}
}

func TestAnAccountThatAlreadyHasTheRoutingIsNotTouched(t *testing.T) {
	f := &sieveFake{
		scripts: map[string]string{
			routing.ScriptName: "# 277 decisions live here\n",
			"Open-Xchange":     "require [\"include\"];\ninclude \"logic\";\n",
		},
		active: "Open-Xchange",
	}
	have := append([]string{"INBOX"}, RoutingBoxes...)
	b, err := EnsureRouting(context.Background(), f, f, have)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Created) != 0 || b.Wrote || len(f.puts) != 0 {
		t.Fatalf("a working account was changed: %+v, puts %v", b, f.puts)
	}
	// Reachability, not activity: ours runs because the active script includes
	// it, which is the ordinary case on an account whose webmail wrote the
	// first one.
	if b.Unreachable {
		t.Fatal("an included script was called unreachable")
	}
	if f.scripts[routing.ScriptName] != "# 277 decisions live here\n" {
		t.Fatal("the script holding the decisions was overwritten")
	}
}

func TestAnActiveScriptThatDoesNotReachOursIsReportedNotFixed(t *testing.T) {
	f := &sieveFake{
		scripts: map[string]string{"Theirs": "keep;\n"},
		active:  "Theirs",
	}
	b, err := EnsureRouting(context.Background(), f, f, append([]string{"INBOX"}, RoutingBoxes...))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Wrote {
		t.Fatal("the routing script was not written")
	}
	// Switching somebody's filtering off to switch ours on is not something a
	// wizard does: activating a script deactivates the one that was running.
	if f.active != "Theirs" {
		t.Fatalf("active = %q — somebody else's script was switched off", f.active)
	}
	if !b.Unreachable {
		t.Fatal("a script that never runs was reported as running")
	}
}

func TestMissingBoxesIgnoresCase(t *testing.T) {
	got := MissingBoxes([]string{"inbox/screener", "INBOX/FEED", "INBOX/Paper Trail"})
	if len(got) != 2 || !strings.Contains(strings.Join(got, " "), "Aside") {
		t.Fatalf("missing = %v", got)
	}
}
