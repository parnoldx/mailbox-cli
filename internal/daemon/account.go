package daemon

import (
	"context"
	"fmt"
	"strings"

	compose "mailbox/internal/message"
	"mailbox/internal/outbox"
	"mailbox/internal/sync/mailsync"
)

// Account is one IMAP + SMTP login and everything the Daemon needs to serve it:
// its own connections, its own Boxes, its own sender.
//
// The Mirror is shared and every row in it carries an account, so a second
// account is a second set of connections rather than a second database
// (ADR-0005).
type Account struct {
	// Name is what an id prefixes with: "gmx" in `gmx/INBOX:412`. The Primary
	// Account's name is never written in an id.
	Name    string
	Primary bool
	// Graph is a Microsoft 365 account. Exchange puts an invite on its
	// calendar before any client sees it, so an RSVP answers that event
	// through Respond — Exchange tells the organizer — rather than sending
	// iMIP and writing a second copy.
	Graph   bool
	Respond func(ctx context.Context, href, partstat string) error

	Reconciler *mailsync.Reconciler
	Writer     *mailsync.Writer
	// Mirrored is every Box held for this account, all of them reconciled on
	// every cycle from one LIST-STATUS round trip. Watched is the subset that
	// gets an IDLE connection, for sub-second latency; everything else rides
	// the poll (ADR-0006). Watching is about how fast we hear, mirroring about
	// what we hold.
	Mirrored []string
	Watched  []string
	// cancel stops this account's loops, and Close drops its connections. The
	// Primary has neither: it lives as long as the process does.
	cancel context.CancelFunc
	Close  func()
	// From is the address this account sends as, and Courier is what empties
	// the shared Outbox of its mail.
	From    compose.Address
	Courier *outbox.Courier
	// Routing is a Graph account's inbox rules, the Routing that account runs
	// server-side (ADR-0032). Nil when the account has no Routing: a Sieve
	// Primary's Routing is the Daemon's Sieve connection, and a Graph account
	// without a Screener Box was never set up with one.
	Routing RuleKeeper
	// Color is the account's colour from the config, handed to clients by
	// `account list` so a row and a Send button can be painted with it.
	Color string

	// trigger serialises this account's cycles. A cold start takes minutes and
	// the poll fires every minute, so without it a second cycle starts inside
	// the first and plans against half-written state. Depth one coalesces:
	// several nudges during a cycle mean one cycle after it, which is all they
	// can ever mean.
	trigger chan string
}

// NewAccount builds an Account, Secondary unless the caller says otherwise.
func NewAccount(name string, r *mailsync.Reconciler, w *mailsync.Writer, mirrored, watched []string) *Account {
	return &Account{
		Name: name, Reconciler: r, Writer: w,
		Mirrored: mirrored, Watched: watched,
		trigger: make(chan string, 1),
	}
}

// accounts is every account, the Primary first.
//
// The list is copied under the lock because Secondary Accounts come and go
// while the Daemon runs: the config is the record and adding one to it adds one
// here, without a restart (ADR-0021).
func (d *Daemon) accounts() []*Account {
	d.reload.mu.Lock()
	others := append([]*Account(nil), d.Others...)
	d.reload.mu.Unlock()
	out := make([]*Account, 0, len(others)+1)
	out = append(out, d.Primary)
	out = append(out, others...)
	return out
}

// StartAccount adds a Secondary Account to a running Daemon and starts its
// loops: its own cycle loop, its own poll and its own IDLE watchers, because a
// slow server on one account must not hold up the one somebody is reading.
func (d *Daemon) StartAccount(a *Account) {
	d.reload.mu.Lock()
	ctx := d.reload.runCtx
	if ctx == nil {
		// Before Serve: it will be started with the rest.
		d.Others = append(d.Others, a)
		d.reload.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	// Publish last, while the lock is still held: between publish and loop
	// start a concurrent StopAccount could cancel and close the account, and
	// these goroutines would then spawn anyway. Nothing the loops do before
	// their first kick takes reload.mu, so starting them under it is safe.
	go d.cycleLoop(ctx, a)
	go d.poll(ctx, a)
	for _, f := range a.Watched {
		go d.watch(ctx, a, f)
	}
	d.Others = append(d.Others, a)
	d.reload.mu.Unlock()
	d.kickAccount(a, "added")
}

// StopAccount takes one off the Daemon and stops its connections. What it left
// in the Mirror is the caller's to drop: the Mirror is not this function's to
// write.
func (d *Daemon) StopAccount(name string) bool {
	d.reload.mu.Lock()
	for i, a := range d.Others {
		if !strings.EqualFold(a.Name, name) {
			continue
		}
		// Take the entry off under the lock, then cancel and close outside it:
		// Close waits on Logout, which on a silent server is bounded only by
		// cmdCap, and holding mu through it would block every account lookup
		// and reload for that long.
		d.Others = append(d.Others[:i], d.Others[i+1:]...)
		d.reload.mu.Unlock()
		if a.cancel != nil {
			a.cancel()
		}
		if a.Close != nil {
			a.Close()
		}
		return true
	}
	d.reload.mu.Unlock()
	return false
}

// accountNamed finds an account by the name an id prefixes with.
func (d *Daemon) accountNamed(name string) (*Account, error) {
	if name == "" {
		return d.Primary, nil
	}
	for _, a := range d.accounts() {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no account called %q", name)
}

func (d *Daemon) accountNames() []string {
	// Via accounts(), under the lock: reading d.Others here directly would
	// race StartAccount/StopAccount appending under it.
	accounts := d.accounts()
	out := make([]string, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, a.Name)
	}
	return out
}

// label is the account's name as a row carries it: empty for the Primary, by
// the same rule as qualify.
func (a *Account) label() string {
	if a == nil || a.Primary {
		return ""
	}
	return a.Name
}
