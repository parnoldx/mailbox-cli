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

	Reconciler *mailsync.Reconciler
	Writer     *mailsync.Writer
	// Mirrored is every Box held for this account; Watched is the subset with
	// an IDLE connection.
	Mirrored []string
	Watched  []string
	// cancel stops this account's loops, and Close drops its connections. Only
	// a Secondary has them: the Primary lives as long as the process does.
	cancel context.CancelFunc
	Close  func()
	// From is the address this account sends as, and Courier is what empties
	// the shared Outbox of its mail.
	From    compose.Address
	Courier *outbox.Courier

	// trigger serialises this account's cycles, depth one. Several nudges
	// during a cycle mean one cycle after it, which is all they can ever mean.
	trigger chan string
}

// NewAccount builds a Secondary Account. The Primary one is the Daemon's own
// fields, so that everything written before there were several accounts still
// works unchanged.
func NewAccount(name string, r *mailsync.Reconciler, w *mailsync.Writer, mirrored, watched []string) *Account {
	return &Account{
		Name: name, Reconciler: r, Writer: w,
		Mirrored: mirrored, Watched: watched,
		trigger: make(chan string, 1),
	}
}

// primaryAccount is the Daemon's own account, as an Account.
func (d *Daemon) primaryAccount() *Account {
	return &Account{
		Name: d.Account, Primary: true,
		Reconciler: d.Reconciler, Writer: d.Writer,
		Mirrored: d.Mirrored, Watched: d.Watched,
		From: d.From, Courier: d.Courier,
		trigger: d.trigger,
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
	out = append(out, d.primaryAccount())
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
	d.Others = append(d.Others, a)
	d.reload.mu.Unlock()

	go d.cycleLoop(ctx, a)
	go d.poll(ctx, a)
	for _, f := range a.Watched {
		go d.watch(ctx, a, f)
	}
	d.kickAccount(a, "added")
}

// StopAccount takes one off the Daemon and stops its connections. What it left
// in the Mirror is the caller's to drop: the Mirror is not this function's to
// write.
func (d *Daemon) StopAccount(name string) bool {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	for i, a := range d.Others {
		if !strings.EqualFold(a.Name, name) {
			continue
		}
		if a.cancel != nil {
			a.cancel()
		}
		if a.Close != nil {
			a.Close()
		}
		d.Others = append(d.Others[:i], d.Others[i+1:]...)
		return true
	}
	return false
}

// accountNamed finds an account by the name an id prefixes with.
func (d *Daemon) accountNamed(name string) (*Account, error) {
	if name == "" {
		return d.primaryAccount(), nil
	}
	for _, a := range d.accounts() {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no account called %q", name)
}

// resolveID reads `[account/]box:uid`. An unqualified id means the Primary
// Account, so every id that worked when there was one account still works
// verbatim (ADR-0005).
func (d *Daemon) resolveID(value string) (*Account, string, uint32, error) {
	name, rest := splitAccount(value, d.accountNames())
	a, err := d.accountNamed(name)
	if err != nil {
		return nil, "", 0, err
	}
	folder, uid, err := parseMessageID(rest, a.Mirrored)
	return a, folder, uid, err
}

// resolveAttachmentID reads `[account/]box:uid[:index]`.
func (d *Daemon) resolveAttachmentID(value string) (*Account, string, uint32, int, error) {
	name, rest := splitAccount(value, d.accountNames())
	a, err := d.accountNamed(name)
	if err != nil {
		return nil, "", 0, 0, err
	}
	folder, uid, index, err := parseAttachmentID(rest, a.Mirrored)
	return a, folder, uid, index, err
}

func (d *Daemon) accountNames() []string {
	out := make([]string, 0, len(d.Others)+1)
	for _, a := range d.accounts() {
		out = append(out, a.Name)
	}
	return out
}

// splitAccount takes the account prefix off an id. A Box name can contain a
// slash — `INBOX/Screener` — so only a prefix that names an account counts as
// one, and everything else is part of the Box.
func splitAccount(value string, names []string) (account, rest string) {
	v := strings.TrimSpace(value)
	// The account on its own names all of it: `box view gmx` is that account's
	// Inbox, and `search --in gmx` is that account's mail.
	for _, n := range names {
		if n != "" && strings.EqualFold(n, v) {
			return v, ""
		}
	}
	i := strings.Index(v, "/")
	if i <= 0 {
		return "", v
	}
	head := v[:i]
	for _, n := range names {
		if strings.EqualFold(n, head) {
			return head, v[i+1:]
		}
	}
	return "", v
}

// qualify puts the account back on an id. The Primary Account is never written:
// an id from a one-account setup and the same id from a two-account one are the
// same string (ADR-0005).
func (a *Account) qualify(id string) string {
	if a == nil || a.Primary || a.Name == "" {
		return id
	}
	return a.Name + "/" + id
}

// messageID is the id a caller hands back to a read command.
func (a *Account) messageID(folder string, uid uint32) string {
	return a.qualify(formatMessageID(folder, uid, a.Mirrored))
}
