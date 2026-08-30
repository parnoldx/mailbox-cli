package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/routing"
)

func sieveDaemon(t *testing.T) (*Daemon, *fakeSieve) {
	t.Helper()
	d := seed(t)
	f := newFakeSieve()
	f.scripts[routing.ScriptName] = "# ours\n"
	f.scripts["old"] = "# somebody else's\n"
	f.active = "old"
	d.Sieve = f
	return d, f
}

// The listing says which script runs and which one the routing owns, because
// those are the two facts that decide whether editing one is safe.
func TestSieveListMarksTheActiveOneAndOurs(t *testing.T) {
	d, _ := sieveDaemon(t)
	resp := mustAsk(t, d, []string{"sieve", "list"}, nil)
	got := map[string]script{}
	for _, s := range resp.Data.([]script) {
		got[s.Name] = s
	}
	if !got["old"].Active || got[routing.ScriptName].Active {
		t.Errorf("active is wrong: %+v", got)
	}
	if !got[routing.ScriptName].Ours || got["old"].Ours {
		t.Errorf("ownership is wrong: %+v", got)
	}
}

// With no name, get answers about the script the server actually runs — which
// is the question, when the one this program wrote is not the active one.
func TestSieveGetWithNoNameIsTheActiveScript(t *testing.T) {
	d, _ := sieveDaemon(t)
	resp := mustAsk(t, d, []string{"sieve", "get"}, nil)
	if got := resp.Data.(string); got != "# somebody else's\n" {
		t.Errorf("got %q", got)
	}
	resp = mustAsk(t, d, []string{"sieve", "get"}, map[string]any{"positional": routing.ScriptName})
	if got := resp.Data.(string); got != "# ours\n" {
		t.Errorf("got %q", got)
	}
}

// put stores and does not activate: uploading beside the active script changes
// nothing until somebody says so, and the reply has to make that visible.
func TestSievePutStoresWithoutActivating(t *testing.T) {
	d, f := sieveDaemon(t)
	resp := mustAsk(t, d, []string{"sieve", "put"},
		map[string]any{"positional": "scratch", "content": "# new\n"})
	got := resp.Data.(putResult)
	if got.Name != "scratch" || got.Active {
		t.Errorf("put gave %+v", got)
	}
	if f.scripts["scratch"] != "# new\n" {
		t.Errorf("scripts = %v", f.scripts)
	}
	if f.active != "old" {
		t.Errorf("put changed the active script to %q", f.active)
	}
}

// Activating something that is not there would leave the server running what it
// was, with a command that said it succeeded.
func TestSieveActivateRefusesAScriptTheServerDoesNotHave(t *testing.T) {
	d, f := sieveDaemon(t)
	resp := ask(t, d, []string{"sieve", "activate"}, map[string]any{"positional": "nope"})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
	if f.active != "old" {
		t.Errorf("the active script changed anyway, to %q", f.active)
	}

	mustAsk(t, d, []string{"sieve", "activate"}, map[string]any{"positional": routing.ScriptName})
	if f.active != routing.ScriptName {
		t.Errorf("active = %q", f.active)
	}
}

// A server with no managesieve is a fact about the account, not a crash.
func TestSieveWithNoConnectionSaysSo(t *testing.T) {
	d := seed(t)
	d.Sieve = nil
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"sieve", "list"}})
	if resp.OK || !strings.Contains(resp.Error, "managesieve") {
		t.Fatalf("resp = %+v", resp)
	}
}
