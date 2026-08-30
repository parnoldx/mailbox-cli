//go:build live

// Live conformance run against the real ManageSieve server.
//
//	go test -tags live ./internal/sievedrv/ -v
//
// It answers the two questions a fake cannot: does this server take the script
// this program generates, and does it hand it back unchanged? A fake has no
// Sieve compiler, so a generated script that is subtly not Sieve would pass
// every unit test in this repo and misfile mail for as long as nobody looked.
//
// It writes under a scratch name and never activates it, so the Routing that is
// actually running is untouched.
package sievedrv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"mailbox/internal/routing"
)

const scratch = "mailbox-live-test"

type liveConfig struct {
	Account struct {
		Email     string
		Password  string
		IMAPHost  string `toml:"imap_host"`
		SieveHost string `toml:"sieve_host"`
		SievePort int    `toml:"sieve_port"`
	}
}

func liveDriver(t *testing.T) *Driver {
	t.Helper()
	path := os.Getenv("MAILBOX_CONFIG")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "mailbox", "config.toml")
	}
	var c liveConfig
	if _, err := toml.DecodeFile(path, &c); err != nil {
		t.Skipf("no live config at %s: %v", path, err)
	}
	if c.Account.Email == "" {
		t.Skip("no account in config")
	}
	if c.Account.IMAPHost == "" {
		c.Account.IMAPHost = "imap.mailbox.org"
	}
	if c.Account.SieveHost == "" {
		c.Account.SieveHost = c.Account.IMAPHost
	}
	return New(Config{
		Host: c.Account.SieveHost, Port: c.Account.SievePort,
		Username: c.Account.Email, Password: c.Account.Password,
	})
}

// TestLiveRoutingScriptCompiles is the gate. The server is the only Sieve
// compiler in reach, and PUTSCRIPT either takes the script or refuses it.
func TestLiveRoutingScriptCompiles(t *testing.T) {
	d := liveDriver(t)
	ctx := context.Background()

	l := routing.New()
	for addr, dest := range map[string]routing.Destination{
		"live-inbox@example.com": routing.Inbox,
		"live-feed@example.com":  routing.Feed,
		"live-paper@example.com": routing.PaperTrail,
		"live-block@example.net": routing.Block,
	} {
		if _, err := l.Set(addr, dest); err != nil {
			t.Fatal(err)
		}
	}
	script := l.Script()
	if err := d.PutScript(ctx, scratch, script, false); err != nil {
		t.Fatalf("the server refused the generated script: %v\n%s", err, script)
	}
	t.Cleanup(func() {
		c, err := d.dial()
		if err != nil {
			t.Logf("cleanup: %v", err)
			return
		}
		defer c.Close()
		if err := c.DeleteScript(scratch); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	back, err := d.Script(ctx, scratch)
	if err != nil {
		t.Fatal(err)
	}
	// A server is entitled to normalise what it is given, so the bytes are not
	// asserted on — what has to survive is the decisions, because those are
	// what the next read projects into the Mirror.
	got := routing.Parse(back)
	for _, r := range l.All() {
		if got.Of(r.Address) != r.To {
			t.Errorf("%s came back as %q, want %q\n%s", r.Address, got.Of(r.Address), r.To, back)
		}
	}

	// The scratch script is stored and is not the active one: this test must
	// not have changed where the account's mail goes.
	names, active, err := d.Scripts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active == scratch {
		t.Fatal("the test script was activated")
	}
	found := false
	for _, n := range names {
		if n == scratch {
			found = true
		}
	}
	if !found {
		t.Errorf("the script was accepted but is not listed: %v", names)
	}
	t.Logf("scripts on the account: %v, active %q", names, active)
}

// TestLiveEmptyRoutingCompiles is the other shape: a Routing nobody is on. It
// is a script with no rules at all, and "no rules" is the one thing a generator
// is likeliest to write as `[]`, which is not Sieve.
func TestLiveEmptyRoutingCompiles(t *testing.T) {
	d := liveDriver(t)
	ctx := context.Background()
	script := routing.New().Script()
	if strings.Contains(script, "[]") {
		t.Fatalf("an empty routing produced an empty list:\n%s", script)
	}
	if err := d.PutScript(ctx, scratch, script, false); err != nil {
		t.Fatalf("the server refused an empty routing: %v\n%s", err, script)
	}
	c, err := d.dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.DeleteScript(scratch); err != nil {
		t.Logf("cleanup: %v", err)
	}
}

// TestLiveRoutingIsReachable reads the account as it stands and says how the
// Routing actually runs. It writes nothing.
//
// This is the gate the first live run of this slice failed. The active script
// on this account is `Open-Xchange`, written by the webmail filter editor, and
// it ends with `include "logic";` — so the Routing runs without being the
// active script. A first version refused every decision on that account, and
// would have deactivated the webmail rules to enable ours.
func TestLiveRoutingIsReachable(t *testing.T) {
	d := liveDriver(t)
	ctx := context.Background()
	names, active, err := d.Scripts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored := false
	for _, n := range names {
		if n == routing.ScriptName {
			stored = true
		}
	}
	if !stored {
		t.Skipf("this account has no %q script yet: %v", routing.ScriptName, names)
	}
	switch {
	case active == routing.ScriptName:
		t.Logf("%q is the active script", routing.ScriptName)
	case active == "":
		t.Fatalf("%q is stored but no script is active: nothing routes", routing.ScriptName)
	default:
		body, err := d.Script(ctx, active)
		if err != nil {
			t.Fatal(err)
		}
		if !routing.Includes(body, routing.ScriptName) {
			t.Fatalf("%q is active and does not include %q: the routing is stored and dead",
				active, routing.ScriptName)
		}
		t.Logf("%q is active and includes %q", active, routing.ScriptName)
	}

	// And the parser reads the script that is really there, which is still in
	// the spelling the program before this one wrote.
	body, err := d.Script(ctx, routing.ScriptName)
	if err != nil {
		t.Fatal(err)
	}
	l := routing.Parse(body)
	if l.Count() == 0 {
		t.Fatalf("the parser read no decisions out of %d bytes", len(body))
	}
	// Nothing is lost on the way back out: the generated script says the same
	// thing about every sender the account's script says something about.
	back := routing.Parse(l.Script())
	if back.Count() != l.Count() {
		t.Fatalf("round trip: %d decisions in, %d out", l.Count(), back.Count())
	}
	for _, r := range l.All() {
		if back.Of(r.Address) != r.To {
			t.Fatalf("round trip changed a decision: %q -> %q", r.To, back.Of(r.Address))
		}
	}
	t.Logf("%d decisions read out of %d bytes: block=%d inbox=%d paper=%d feed=%d",
		l.Count(), len(body), len(l.Addresses(routing.Block)), len(l.Addresses(routing.Inbox)),
		len(l.Addresses(routing.PaperTrail)), len(l.Addresses(routing.Feed)))
}
