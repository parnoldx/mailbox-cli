// Command mailbox reads mail from a local Mirror kept by a daemon.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"mailbox/internal/cli"
	"mailbox/internal/config"
	"mailbox/internal/daemon"
	"mailbox/internal/davdrv"
	"mailbox/internal/imapdrv"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
	"mailbox/internal/routing"
	"mailbox/internal/setup"
	"mailbox/internal/sievedrv"
	"mailbox/internal/smtpdrv"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/sync/mailsync"

	"golang.org/x/term"
)

// main hands the CLI the two commands that cannot go through the socket: one
// owns it, and the other writes the config that names it. Everything else about
// them — where they appear in help, what flags they take — is in the registry
// with every other command (ADR-0020).
func main() {
	os.Exit(cli.RunWith(cli.Locals{
		Daemon: func(systemdSocket bool, stdout, stderr io.Writer) int {
			if err := runDaemon(systemdSocket); err != nil {
				fmt.Fprintf(stderr, "mailbox daemon: %v\n", err)
				return cli.ExitAPI
			}
			return cli.ExitOK
		},
		Setup: func(stdout, stderr io.Writer) int {
			if err := runSetup(); err != nil {
				fmt.Fprintf(stderr, "mailbox setup: %v\n", err)
				return cli.ExitAPI
			}
			return cli.ExitOK
		},
	}, os.Args[1:], os.Stdout, os.Stderr))
}

// runSetup runs the wizard. Everything it needs from this machine — where the
// units go, which binary the service should run, where the skill lives — is
// worked out here and handed in, so the wizard itself writes into a directory
// it was given and a test can give it a temporary one.
func runSetup() error {
	unitDir, err := setup.UnitDir()
	if err != nil {
		return err
	}
	exe, err := setup.ExecPath()
	if err != nil {
		return err
	}
	skillDir, skillLink, err := setup.SkillPaths()
	if err != nil {
		return err
	}
	// No systemd is a real answer, not a failure: on the VPS there is no user
	// session to install into and the daemon is started another way (ADR-0012).
	var ctl setup.Systemctl = setup.SystemctlUser{}
	if _, err := exec.LookPath("systemctl"); err != nil {
		ctl = setup.NoSystemd{}
	}

	w := &setup.Wizard{
		In: os.Stdin, Out: os.Stdout, Prober: setup.Servers{},
		ConfigPath: config.Path(),
		Socket:     config.SocketPath(),
		Units:      setup.Units{Dir: unitDir, Exec: exe, Ctl: ctl},
		Skill:      setup.Skill{Dir: skillDir, Link: skillLink},
		Ctl:        ctl,
		// The wizard ends on doctor, which lives in the CLI: the check the
		// person will be told to run when something breaks, seen once while
		// everything worked.
		Doctor: func() int { return cli.Run([]string{"doctor"}, os.Stdout, os.Stderr) },
	}
	// Echo is only turned off where there is a terminal to turn it off on.
	// Piped answers are read by the wizard's own reader: two readers over one
	// pipe means the second one finds nothing, because the first one buffered
	// it all.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		w.ReadPassword = readPassword
	}
	return w.Run(context.Background())
}

// readPassword reads a password from the terminal without echoing it.
func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return strings.TrimSpace(string(secret)), err
}

// firstOrder puts the Boxes in the order that makes the Mirror useful soonest:
// the Inbox, then what Sieve routing files beside it, then everything else,
// with Archive last because it is the biggest and the least urgent.
//
// This is the Daemon's cold start order, and it used to be the wizard's: setup
// did its own first sync in its own process, which made it a second writer of
// the Mirror (ADR-0012). The order belongs where the sync is (ADR-0021).
func firstOrder(boxes []string) []string {
	rank := func(name string) int {
		upper := strings.ToUpper(name)
		switch {
		case upper == "INBOX":
			return 0
		case strings.HasPrefix(upper, "INBOX/"):
			return 1
		case strings.HasPrefix(upper, "ARCHIVE"):
			return 3
		default:
			return 2
		}
	}
	out := append([]string(nil), boxes...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

func runDaemon(systemdSocket bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	path, err := config.MirrorPath()
	if err != nil {
		return err
	}
	m, err := mirror.Open(path)
	if err != nil {
		return fmt.Errorf("open mirror: %w", err)
	}
	defer m.Close()

	drv, err := imapdrv.Dial(imapdrv.Config{
		Host:     cfg.Account.IMAPHost,
		Port:     cfg.Account.IMAPPort,
		Username: cfg.Account.Email,
		Password: cfg.Account.Password,
	})
	if err != nil {
		return err
	}
	defer drv.Close()

	ctx0 := context.Background()
	logger := log.New(os.Stderr, "", log.LstdFlags)
	r := &mailsync.Reconciler{Account: "primary", Mirror: m, Driver: drv}

	mirrored, watched, err := boxes(ctx0, drv, cfg.Account.Watch)
	if err != nil {
		return err
	}
	logger.Printf("mirroring %d boxes, watching %v", len(mirrored), watched)
	d := daemon.New("primary", m, r, mirrored, watched, logger)

	// The Outbox is a separate file from the Mirror and outlives it: a mail
	// that has been composed but not yet sent exists nowhere else (ADR-0013).
	outPath, err := config.OutboxPath()
	if err != nil {
		return err
	}
	box, err := outbox.Open(outPath)
	if err != nil {
		return err
	}
	defer box.Close()

	sentBox := cfg.Account.SentBox
	if sentBox == "" {
		// Asked for by its special-use flag rather than guessed from a name.
		if sentBox, err = drv.SentFolder(ctx0); err != nil {
			logger.Printf("cannot ask the server where sent mail goes: %v", err)
		}
	}
	if sentBox == "" {
		logger.Printf("no \\Sent box found: sent mail will not be filed")
	}
	d.Outbox = box
	d.From = compose.Address{Name: cfg.Account.DisplayName, Addr: cfg.Account.Email}
	d.Courier = &outbox.Courier{
		Box: box, Account: "primary", Filer: drv, SentBox: sentBox, Log: logger,
		Transport: smtpdrv.New(smtpdrv.Config{
			Host: cfg.Account.SMTPHost, Port: cfg.Account.SMTPPort,
			Username: cfg.Account.Email, Password: cfg.Account.Password,
		}),
	}
	logger.Printf("outbox at %s, filing sent mail in %q", outPath, sentBox)

	// The calendars and task lists. The account's own server is enumerated;
	// anything configured by hand is only used when it has its own credentials,
	// which means it is on a server we cannot ask (ADR-0010). Skipped entirely
	// when NoDAV is set (ADR-0025's VPS Daemon): d.DAV stays nil, which the
	// reconciler and every command already treat as "no calendars configured".
	if !cfg.Account.NoDAV {
		clients := []*davdrv.Client{davdrv.New(davdrv.Config{
			Endpoint: cfg.Account.DAVEndpoint,
			Username: cfg.Account.Email, Password: cfg.Account.DAVPassword,
		})}
		for key, cal := range cfg.CalDAV {
			if cal.URL == "" || cal.Password == "" {
				continue // on our own server: discovery finds it
			}
			name := cal.Name
			if name == "" {
				name = key
			}
			kind := cal.Kind
			if kind == "" {
				kind = "events"
			}
			user := cal.User
			if user == "" {
				user = cfg.Account.Email
			}
			clients = append(clients, davdrv.Static(
				davdrv.Config{Endpoint: cal.URL, Username: user, Password: cal.Password},
				davsync.Collection{Kind: kind, URL: cal.URL, Name: name, Color: cal.Color},
			))
			logger.Printf("calendar %q comes from %s with its own credentials", name, cal.URL)
		}
		primaryDAV := clients[0]
		d.DAV = &davsync.Reconciler{
			Account: "primary", Mirror: m, Driver: davdrv.NewSet(clients...),
			Location: time.Local,
			// Collections this machine does not mirror. Discovery skips them on the
			// way in and drops them if they are already held (ADR-0013 is why the
			// list is in the config and not on the row).
			Exclude: cfg.Collections.Exclude,
			OnCollection: func(name string, out davsync.Outcome, err error) {
				switch {
				case err != nil:
					logger.Printf("dav %s: %v", name, err)
				case out.Changed > 0 || out.Deleted > 0:
					logger.Printf("dav %s: changed=%d deleted=%d full=%v", name, out.Changed, out.Deleted, out.Full)
				}
			},
		}

		d.DAVWriter = &davsync.Writer{
			Account: "primary", Mirror: m, Driver: davdrv.NewSet(clients...), Reconciler: d.DAV,
		}
		d.DAVHome = primaryDAV
		if u, err := url.Parse(cfg.Account.DAVEndpoint); err == nil {
			d.DAVHost = u.Host
		}
		d.CalendarEmail = calendarEmailMap(cfg)
		d.TaskList = cfg.Account.TaskList
		d.AddressBook = cfg.Account.AddressBook
	} else {
		logger.Printf("no_dav set: calendars, task lists and address books are skipped")
	}
	d.BubbleMorning, d.BubbleEvening = cfg.Bubble.Morning, cfg.Bubble.Evening

	// The Routing: one Sieve script on the Primary Account's server, which is
	// what puts mail in the Screener, the Feed and the Paper Trail before this
	// program ever sees it. The daemon holds the connection like every other
	// one (ADR-0012); a server with no ManageSieve simply logs and the Routing
	// stays empty.
	d.Sieve = sievedrv.New(sievedrv.Config{
		Host: cfg.Account.SieveHost, Port: cfg.Account.SievePort,
		Username: cfg.Account.Email, Password: cfg.Account.Password,
	})
	logger.Printf("routing script %q at %s:%d", routing.ScriptName,
		cfg.Account.SieveHost, cfg.Account.SievePort)

	// Secondary Accounts: an Inbox, a Sent box and the ability to send, each
	// with its own connections and its own name in an id (ADR-0005).
	for _, name := range sortedNames(cfg.Secondary) {
		acct, err := buildSecondary(ctx0, name, cfg.Secondary[name], m, box, logger)
		if err != nil {
			logger.Printf("account %s: %v (skipped)", name, err)
			continue
		}
		d.StartAccount(acct)
	}

	// From here the config is the record and this process reconciles it
	// (ADR-0021): an account added to the file is up within a minute without a
	// restart, and a change this process cannot make in place is an exit.
	d.WatchConfig(config.Path(), applier(d, cfg, m, box, logger))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Printf("mirror at %s", path)
	ln, err := daemon.Listen(config.SocketPath(), systemdSocket)
	if err != nil {
		return err
	}
	return d.Serve(ctx, ln)
}

// watchable are the Boxes worth an IDLE connection: the ones where a minute of
// latency would be felt. Screener is on the list because sign-in links for new
// services land there, and a link that arrives late has expired (ADR-0006).
var watchable = []string{"INBOX", "INBOX/Screener"}

// boxes asks the server what Boxes exist and splits them into what to mirror
// and what to watch. Discovery rather than a hardcoded list, because a Box added
// in webmail should just appear — and because a URL or name copied by hand is
// how the CardDAV collection ended up pointing at the wrong address book.
func boxes(ctx context.Context, drv *imapdrv.Driver, watch []string) (mirrored, watched []string, err error) {
	// A development escape hatch: mirror only these Boxes, comma separated, so
	// a change can be tried against a real account in seconds rather than after
	// a full cold start.
	if only := os.Getenv("MAILBOX_FOLDER"); only != "" {
		names := strings.Split(only, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		return names, names, nil
	}
	all, err := drv.Folders(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list boxes: %w", err)
	}
	// What to watch is a setting, because it is a judgement about which mail is
	// worth a connection. What to mirror is not: every Box is.
	wanted := watch
	if len(wanted) == 0 {
		wanted = watchable
	}
	for _, b := range all {
		// Trash is excluded from the Mirror entirely (ADR-0003).
		if strings.EqualFold(b, "Trash") {
			continue
		}
		mirrored = append(mirrored, b)
		for _, w := range wanted {
			if strings.EqualFold(b, w) {
				watched = append(watched, b)
			}
		}
	}
	return firstOrder(mirrored), watched, nil
}

// calendarEmailMap is which of our addresses an invite belongs on. The account
// address is the home CalDAV (empty name); a hand-added calendar with an
// email, or a Secondary Account, names its calendar. Unmapped addresses make
// the RSVP ask.
func calendarEmailMap(cfg *config.Config) map[string]string {
	out := map[string]string{}
	if cfg.Account.Email != "" {
		out[strings.ToLower(cfg.Account.Email)] = ""
	}
	for key, cal := range cfg.CalDAV {
		if cal.Email == "" {
			continue
		}
		name := cal.Name
		if name == "" {
			name = key
		}
		out[strings.ToLower(cal.Email)] = name
	}
	for name, a := range cfg.Secondary {
		addr := strings.ToLower(a.Email)
		if addr == "" {
			continue
		}
		if _, ok := out[addr]; ok {
			continue
		}
		out[addr] = name
	}
	return out
}

// sortedNames keeps the order of accounts stable across runs, so a log read
// twice says the same thing in the same order.
func sortedNames(m map[string]config.Account) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildSecondary opens one Secondary Account's connections. It is a function
// rather than a loop body because an account can now arrive while the Daemon is
// running (ADR-0021), and the two paths must build the same thing.
func buildSecondary(ctx context.Context, name string, sec config.Account,
	m *mirror.Mirror, box *outbox.Outbox, logger *log.Logger) (*daemon.Account, error) {

	drv, err := imapdrv.Dial(imapdrv.Config{
		Host: sec.IMAPHost, Port: sec.IMAPPort, Username: sec.Email, Password: sec.Password,
	})
	if err != nil {
		return nil, err
	}
	mirrored, watched, err := boxes(ctx, drv, sec.Watch)
	if err != nil {
		drv.Close()
		return nil, err
	}
	acct := daemon.NewAccount(name,
		&mailsync.Reconciler{Account: name, Mirror: m, Driver: drv},
		&mailsync.Writer{Account: name, Mirror: m, Driver: drv, Mirrored: mirrored},
		mirrored, watched)
	acct.From = compose.Address{Name: sec.DisplayName, Addr: sec.Email}
	acct.Close = func() { drv.Close() }

	sent := sec.SentBox
	if sent == "" {
		if sent, err = drv.SentFolder(ctx); err != nil {
			logger.Printf("account %s: cannot ask where sent mail goes: %v", name, err)
		}
	}
	if sec.SMTPHost != "" {
		acct.Courier = &outbox.Courier{
			Box: box, Account: name, Filer: drv, SentBox: sent, Log: logger,
			Transport: smtpdrv.New(smtpdrv.Config{
				Host: sec.SMTPHost, Port: sec.SMTPPort,
				Username: sec.Email, Password: sec.Password,
			}),
		}
	}
	logger.Printf("account %s: %d boxes, watching %v, filing sent mail in %q",
		name, len(mirrored), watched, sent)
	return acct, nil
}

// applier keeps the config this process last acted on and hands the Daemon the
// three things changing the account set needs from here: how to build one, how
// to drop what it left in the Mirror, and what it still has in flight. The
// deciding is the Daemon's (ADR-0021); only the building is main's, because
// only main knows how to open a connection.
func applier(d *daemon.Daemon, was *config.Config, m *mirror.Mirror,
	box *outbox.Outbox, logger *log.Logger) daemon.Applier {

	accounts := daemon.Accounts{
		Build: func(name string, sec config.Account) (*daemon.Account, error) {
			return buildSecondary(context.Background(), name, sec, m, box, logger)
		},
		Forget:   m.ForgetAccount,
		InFlight: func(name string) (int, error) { return stillInFlight(box, name) },
	}
	return func(cfg *config.Config) (daemon.Applied, error) {
		applied := d.Reconcile(was, cfg, accounts)
		was = cfg
		return applied, nil
	}
}

// stillInFlight counts what one account has left in the Outbox: queued, at the
// server, or Held.
func stillInFlight(box *outbox.Outbox, account string) (int, error) {
	if box == nil {
		return 0, nil
	}
	items, err := box.List(500)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		if it.Account != account {
			continue
		}
		switch it.State {
		case outbox.Queued, outbox.Sent, outbox.Held:
			n++
		}
	}
	return n, nil
}
