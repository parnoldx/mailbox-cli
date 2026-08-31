package daemon

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/sync/mailsync"
)

// Daemon owns the Mirror and every server connection. Nothing else opens
// either (ADR-0012).
type Daemon struct {
	Account    string
	Mirror     *mirror.Mirror
	Reconciler *mailsync.Reconciler
	// Mirrored is every Box the Daemon keeps in the Mirror. All of them are
	// reconciled on every cycle, from one LIST-STATUS round trip.
	Mirrored []string
	// Watched is the subset of Mirrored that gets an IDLE connection, for
	// sub-second latency. Everything else rides the poll (ADR-0006). These are
	// two different sets: watching is about how fast we hear, mirroring is
	// about what we hold.
	Watched []string
	// Writer is the write-through path: a command that changes something goes
	// to the server and updates the Mirror from the ack (ADR-0004).
	Writer *mailsync.Writer
	// Others are the Secondary Accounts, each with its own connections. The
	// fields above are the Primary Account's, which is why an id with no
	// account prefix means that one (ADR-0005).
	Others []*Account
	// Outbox is the durable send queue, and Courier is what empties it. They
	// are the one place the Mirror leads the server rather than following it
	// (ADR-0004), and the Outbox file outlives every rebuild of the Mirror
	// (ADR-0013).
	Outbox  *outbox.Outbox
	Courier *outbox.Courier
	// From is the address this account sends as.
	From compose.Address
	// PollEvery is how often the LIST-STATUS cycle runs.
	PollEvery time.Duration
	// DAV reconciles the calendars, task lists and address books. It runs on
	// its own timer: there is no IDLE for DAV, and nothing there is worth a
	// connection held open (ADR-0010).
	DAV      *davsync.Reconciler
	DAVEvery time.Duration
	// DAVWriter is the write-through path for collections: a Todo added or
	// completed goes to the server and the Mirror is updated from the ack
	// (ADR-0004).
	DAVWriter *davsync.Writer
	// DAVHome can create the one collection this program owns rather than
	// finds: the habits calendar (ADR-0018).
	DAVHome CalendarMaker
	// TaskList is where a Todo goes when the caller does not say. With one task
	// list it is unnecessary; with several, naming one is better than guessing.
	TaskList string
	// AddressBook is where a new Contact goes when the caller does not say.
	AddressBook string
	// Sieve is the ManageSieve connection that holds the Routing: the script
	// that decides where new mail lands on the Primary Account. Only the
	// Primary has one — a Secondary Account has an Inbox and nothing to route.
	Sieve Sieve
	// RoutingEvery is how often the script is re-read, for the rule somebody
	// added in webmail.
	RoutingEvery time.Duration
	Log          *log.Logger

	// ConfigPath is the record this Daemon reconciles itself against, and Apply
	// is what does the reconciling (ADR-0021). Both are set by WatchConfig; a
	// Daemon without them reads its config once, at startup, like it used to.
	ConfigPath string
	Apply      Applier
	reload     reloadState

	// trigger serialises cycles. A cold start takes minutes and the poll fires
	// every minute, so without this a second cycle starts inside the first,
	// plans against half-written state, and redoes folders the first one has
	// already done. Depth one coalesces: several nudges during a cycle mean one
	// cycle after it, which is all they can ever mean.
	trigger chan string

	// davTrigger serialises DAV cycles the way trigger serialises mail ones.
	// The timer and a command's nudge both go through it, so two cycles can
	// never run against the same collection — which would have both of them
	// asking from the same sync token and one of them committing the older
	// answer.
	davTrigger chan davKick

	mu        sync.Mutex
	clients   map[chan Push]struct{}
	connected bool
	reachable map[string]bool
	lastSync  time.Time
	syncing   int
	// The collections keep their own freshness, because they have their own
	// loop. Without this a caller reading the agenda is told "current" on the
	// strength of the IMAP server being up.
	davReachable bool
	davLastSync  time.Time
	davSyncing   bool
	davNudged    map[string]time.Time
}

// davKick asks for one DAV cycle. kinds empty means "whatever the poll would
// have done", which is what the timer wants and what a nudge never says.
type davKick struct {
	reason string
	kinds  []string
}

// CalendarMaker creates a collection that is not there yet, and returns it as
// the server now describes it.
type CalendarMaker interface {
	EnsureCalendar(ctx context.Context, displayName string, comps []string) (davsync.Collection, error)
}

// New builds a Daemon. mirrored is every Box to hold; watched is the subset to
// hold an IDLE connection on.
func New(account string, m *mirror.Mirror, r *mailsync.Reconciler, mirrored, watched []string, logger *log.Logger) *Daemon {
	d := &Daemon{
		Account: account, Mirror: m, Reconciler: r,
		Mirrored: mirrored, Watched: watched,
		PollEvery: 60 * time.Second, Log: logger,
		trigger:    make(chan string, 1),
		davTrigger: make(chan davKick, 1),
		clients:    map[chan Push]struct{}{},
	}
	if r != nil {
		d.Writer = &mailsync.Writer{
			Account: account, Mirror: m, Driver: r.Driver, Mirrored: mirrored,
		}
	}
	return d
}

// Run syncs, then serves until ctx is done.
func (d *Daemon) Run(ctx context.Context, socket string) error {
	ln, err := Listen(socket, false)
	if err != nil {
		return err
	}
	return d.Serve(ctx, ln)
}

// Serve is Run on a listener somebody else opened — systemd, under socket
// activation (ADR-0012).
func (d *Daemon) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	d.Log.Printf("listening on %s", ln.Addr())
	d.reload.mu.Lock()
	d.reload.runCtx = ctx
	d.reload.mu.Unlock()

	// The socket opens before the first sync, not after it. A cold start fetches
	// every Box, which takes minutes; making callers wait for that would deny
	// them a Behind Mirror, which is the one thing ADR-0001 says they may
	// always have.
	//
	// Every cycle goes through cycleLoop, including this first one. Running the
	// startup cycle directly instead left nothing reading the trigger channel,
	// and a kick that nobody reads is a poll that never syncs.
	for _, acct := range d.accounts() {
		go d.cycleLoop(ctx, acct)
		d.kickAccount(acct, "startup")
	}

	for _, acct := range d.accounts() {
		name := acct.Name
		if acct.Reconciler == nil {
			continue
		}
		acct.Reconciler.OnFolder = func(folder string, out mailsync.Outcome, err error) {
			switch {
			case err != nil:
				d.Log.Printf("sync %s/%s: %v", name, folder, err)
			case out.Action != mailsync.ActionNone:
				d.Log.Printf("sync %s/%s: %s new=%d flags=%d expunged=%d remapped=%d",
					name, folder, out.Action, out.NewMessages, out.FlagsChanged, out.Expunged, out.Remapped)
			}
		}
		go d.poll(ctx, acct)
		for _, f := range acct.Watched {
			go d.watch(ctx, acct, f)
		}
	}
	go d.davLoop(ctx)
	go d.routingLoop(ctx)

	// Either a signal, or a config change this process cannot make in place
	// (ADR-0021). The second is an ordinary exit: under socket activation the
	// next connection starts a new Daemon on the new config.
	go func() {
		select {
		case <-ctx.Done():
		case <-d.quitting():
		}
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.serve(ctx, conn)
	}
}

// kick asks for a cycle on the Primary Account.
func (d *Daemon) kick(reason string) { d.kickAccount(d.primaryAccount(), reason) }

// kickAccount asks for a cycle on one account. If one is already queued this
// does nothing, because a second queued cycle would do exactly what the first
// will.
func (d *Daemon) kickAccount(a *Account, reason string) {
	if a == nil || a.trigger == nil {
		return
	}
	select {
	case a.trigger <- reason:
	default:
	}
}

// cycleLoop runs one account's cycles, one at a time, forever. Accounts have a
// loop each: a slow server on one of them must not stop the others.
func (d *Daemon) cycleLoop(ctx context.Context, a *Account) {
	if a.Reconciler == nil {
		return
	}
	// A folder whose intent was written but never cleared was interrupted
	// mid-sync. Redoing it is always safe (ADR-0015).
	if err := a.Reconciler.Resume(ctx); err != nil {
		d.logf("resume %s: %v", a.Name, err)
	}
	// A mail left at the SMTP server by a process that died is a different
	// matter: redoing it is not safe, so it is held and reported (ADR-0017).
	if a.Courier != nil {
		if _, err := a.Courier.Recover(); err != nil {
			d.logf("outbox recover: %v", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case reason := <-a.trigger:
			// The top of the cycle is where the config is re-read: the poll
			// already runs every minute, so this costs one stat (ADR-0021).
			if a.Primary && d.configMoved() {
				d.reloadConfig("the file changed")
			}
			d.cycle(ctx, a, reason)
		}
	}
}

// poll runs the cycle on a timer. It is the safety net under IDLE, and the only
// thing watching folders that do not get an IDLE connection.
func (d *Daemon) poll(ctx context.Context, a *Account) {
	t := time.NewTicker(d.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.kickAccount(a, "poll")
		}
	}
}

// watch holds IDLE and runs a cycle whenever the server says anything.
func (d *Daemon) watch(ctx context.Context, a *Account, folder string) {
	events := make(chan mailsync.Event, 16)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-events:
				d.kickAccount(a, "idle")
			}
		}
	}()
	for ctx.Err() == nil {
		if err := a.Reconciler.Driver.Watch(ctx, folder, events); err != nil && ctx.Err() == nil {
			d.logf("watch %s/%s: %v (retrying)", a.Name, folder, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			// Whatever the server said while the watcher was down was said to
			// nobody. The poll would find it within the minute, but the whole
			// point of watching this Box is not to wait that long.
			d.kickAccount(a, "watch resumed")
		}
	}
}

// cycle reconciles every mirrored Box of one account and pushes a notification
// for each one that moved. One detection pass covers all of them.
func (d *Daemon) cycle(ctx context.Context, a *Account, reason string) {
	d.setSyncing(1)
	defer d.setSyncing(-1)
	outcomes, err := a.Reconciler.SyncAll(ctx, a.Mirrored)
	if err != nil {
		d.logf("cycle %s (%s): %v", a.Name, reason, err)
	}
	if len(outcomes) == 0 && err != nil {
		// Nothing succeeded: the server is unreachable, not one bad Box. A
		// server that is up and refusing the password is a different thing —
		// it does not resolve itself, and mail stops arriving until somebody
		// changes something, so it is a problem and not a log line.
		d.setConnected(a.Name, false)
		d.noteAuth(a.Name, err)
		return
	}
	d.setConnected(a.Name, true)
	d.noteAuth(a.Name, nil)
	// The server is reachable, which is the condition a queued mail is waiting
	// for. Draining here rather than on its own timer means a send that failed
	// while the network was down goes out with the first cycle that works.
	d.drain(ctx, a)
	for folder, out := range outcomes {
		if out.Action == mailsync.ActionNone {
			continue
		}
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: folder})
	}
}

// logf logs if there is anywhere to log to. A Daemon built by a test has no
// logger, and a nil one must not turn a send into a panic.
func (d *Daemon) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log.Printf(format, args...)
	}
}

// setSyncing counts the mail cycles in flight. It is a count and not a flag
// because every account has its own loop.
func (d *Daemon) setSyncing(delta int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.syncing += delta
}

// setDAVConnected records whether the DAV server answered, and when it last
// did. This is the collections' half of the Mirror's freshness: a task list
// nobody has reached for an hour is Behind however healthy IMAP is.
func (d *Daemon) setDAVConnected(ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.davReachable = ok
	if ok {
		d.davLastSync = time.Now()
	}
}

// setDAVSyncing records that a DAV cycle is running, so a read answered from a
// Behind Mirror can say that the answer is already on its way.
func (d *Daemon) setDAVSyncing(running bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.davSyncing = running
}

// setConnected records whether one account's server answered. The Mirror is
// Behind if any of them is unreachable: a caller reading mail cannot be told
// "current" when one account's Inbox has not been looked at for an hour.
func (d *Daemon) setConnected(account string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reachable == nil {
		d.reachable = map[string]bool{}
	}
	d.reachable[account] = ok
	all := true
	for _, up := range d.reachable {
		if !up {
			all = false
		}
	}
	d.connected = all
	if ok {
		d.lastSync = time.Now()
	}
}

// push fans a notification out to every connected client. There is no
// subscription: with a handful of widgets the fan-out is free (ADR-0011).
func (d *Daemon) push(p Push) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.clients {
		select {
		case ch <- p:
		default: // a client that cannot keep up misses the nudge and re-reads later
		}
	}
}

// domain names the loop that owns the data a command answers from. Freshness
// is reported per domain because the loops are separate: mail rides IDLE and a
// minute's poll, the collections a timer of their own (ADR-0010).
type domain int

const (
	domainMail domain = iota
	domainDAV
	domainBoth
)

// state describes how fresh one domain is. Where a domain has several sources —
// several accounts, or both loops at once — the answer is the worst of them:
// "current" has to mean that everything the caller just read is current.
func (d *Daemon) state(dom domain) *MirrorState {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := &MirrorState{Connected: true}
	add := func(ok bool, at time.Time, syncing bool) {
		st.Connected = st.Connected && ok
		st.Syncing = st.Syncing || syncing
		if at.IsZero() {
			return
		}
		if st.SyncedAt == nil || at.Before(*st.SyncedAt) {
			t := at
			st.SyncedAt = &t
		}
	}
	if dom == domainMail || dom == domainBoth {
		add(d.connected, d.lastSync, d.syncing > 0)
	}
	// A daemon with no DAV configured has no collections to be Behind on, and
	// saying so would make every reply Behind for ever.
	if (dom == domainDAV || dom == domainBoth) && d.DAV != nil {
		add(d.davReachable, d.davLastSync, d.davSyncing)
	}
	st.Behind = !st.Connected
	return st
}
