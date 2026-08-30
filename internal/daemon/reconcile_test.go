package daemon

import (
	"errors"
	"strings"
	"testing"

	"mailbox/internal/config"
)

func cfgWith(secondary map[string]config.Account) *config.Config {
	return &config.Config{
		Account:   config.Account{Email: "me@example.org", Password: "hunter2"},
		Secondary: secondary,
	}
}

func TestAnAccountAddedToTheFileIsUpWithoutARestart(t *testing.T) {
	d := seed(t)
	built := 0
	acc := Accounts{
		Build: func(name string, _ config.Account) (*Account, error) {
			built++
			return NewAccount(name, nil, nil, []string{"INBOX"}, nil), nil
		},
		Forget:   func(string) error { return nil },
		InFlight: func(string) (int, error) { return 0, nil },
	}
	was := cfgWith(nil)
	now := cfgWith(map[string]config.Account{"gmx": {Email: "me@gmx.de", IMAPHost: "imap.gmx.net"}})

	applied := d.Reconcile(was, now, acc)
	if applied.Restart {
		t.Fatal("adding an account restarted the daemon")
	}
	if built != 1 || len(d.accounts()) != 2 {
		t.Fatalf("built %d, accounts %d", built, len(d.accounts()))
	}
	if len(applied.Changes) != 1 || !strings.Contains(applied.Changes[0], "gmx is up") {
		t.Fatalf("changes = %v", applied.Changes)
	}

	// Reading the same config again changes nothing: the poll runs every
	// minute and must not rebuild connections for a file that did not move.
	if applied := d.Reconcile(now, now, acc); len(applied.Changes) != 0 || built != 1 {
		t.Fatalf("a second read rebuilt it: %v", applied.Changes)
	}
}

func TestAnAccountWithMailInFlightIsKeptAndSaidSo(t *testing.T) {
	d := seed(t)
	acct := NewAccount("gmx", nil, nil, []string{"INBOX"}, nil)
	d.StartAccount(acct)

	forgotten := false
	acc := Accounts{
		Build:    func(string, config.Account) (*Account, error) { return nil, errors.New("no") },
		Forget:   func(string) error { forgotten = true; return nil },
		InFlight: func(string) (int, error) { return 2, nil },
	}
	was := cfgWith(map[string]config.Account{"gmx": {Email: "me@gmx.de", IMAPHost: "imap.gmx.net"}})

	applied := d.Reconcile(was, cfgWith(nil), acc)
	if applied.Restart {
		t.Fatal("a removal restarted the daemon")
	}
	// The file says it is gone and the daemon has kept it. That disagreement
	// has to be visible, or a declarative config and a component that may
	// decline disagree in silence (ADR-0021).
	if len(d.accounts()) != 2 {
		t.Fatal("an account with mail in the outbox was dropped anyway")
	}
	if forgotten {
		t.Fatal("its mail was pruned from the mirror while it still had an outbox")
	}
	problems := d.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0].Detail, "still in the outbox") {
		t.Fatalf("problems = %v", problems)
	}

	// Once the outbox is empty the same edit goes through, and the problem
	// clears with it.
	acc.InFlight = func(string) (int, error) { return 0, nil }
	applied = d.Reconcile(was, cfgWith(nil), acc)
	if len(d.accounts()) != 1 || !forgotten {
		t.Fatalf("accounts = %d, forgotten = %v", len(d.accounts()), forgotten)
	}
	if got := d.Problems(); len(got) != 0 {
		t.Fatalf("problems = %v", got)
	}
}

func TestAChangeToThePrimaryIsAnExitAndNothingElseHappens(t *testing.T) {
	d := seed(t)
	built := 0
	acc := Accounts{Build: func(name string, _ config.Account) (*Account, error) {
		built++
		return NewAccount(name, nil, nil, nil, nil), nil
	}}
	was := cfgWith(nil)
	now := cfgWith(map[string]config.Account{"gmx": {Email: "me@gmx.de"}})
	now.Account.Password = "changed"

	applied := d.Reconcile(was, now, acc)
	if !applied.Restart || !strings.Contains(applied.Reason, "password") {
		t.Fatalf("applied = %+v", applied)
	}
	// The exit is the whole answer: nothing is half-applied on the way out.
	if built != 0 || len(d.accounts()) != 1 {
		t.Fatalf("built %d, accounts %d", built, len(d.accounts()))
	}
}

func TestAServerThatIsDownKeepsTheAccountAndReportsIt(t *testing.T) {
	d := seed(t)
	acc := Accounts{
		Build: func(string, config.Account) (*Account, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	now := cfgWith(map[string]config.Account{"gmx": {Email: "me@gmx.de", IMAPHost: "imap.gmx.net"}})
	applied := d.Reconcile(cfgWith(nil), now, acc)
	if applied.Restart {
		t.Fatal("a server being down restarted the daemon")
	}
	problems := d.Problems()
	if len(problems) != 1 || problems[0].Name != "account gmx" {
		t.Fatalf("problems = %v", problems)
	}
}
