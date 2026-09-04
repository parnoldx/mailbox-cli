package daemon

import (
	"fmt"
	"sort"

	"mailbox/internal/config"
)

// Accounts is what the Daemon needs from its caller to change the set of
// Secondary Accounts while it runs. Only the caller knows how to open a
// connection; the deciding is the Daemon's (ADR-0021).
type Accounts struct {
	// Build opens one account's connections.
	Build func(name string, sec config.Account) (*Account, error)
	// Forget drops what an account left in the Mirror.
	Forget func(name string) error
	// InFlight is how many of its mails the Outbox still holds.
	InFlight func(name string) (int, error)
}

// Reconcile brings this process into line with a config it has just read, and
// says what it did. It is the whole of what can be applied in place: the set of
// Secondary Accounts and their credentials, the Collections this machine
// mirrors, and the two defaults. Everything else is an exit.
func (d *Daemon) Reconcile(was, now *config.Config, a Accounts) Applied {
	var out Applied
	// The Primary Account is not re-identified in place. Every id in the Mirror
	// is resolved against it, and a clean exit does correctly what a live swap
	// would do with a great deal of machinery (ADR-0021).
	if reason := primaryDiffers(was.Account, now.Account); reason != "" {
		out.Restart, out.Reason = true, reason
		return out
	}
	// A hand-added calendar is part of the Primary's DAV driver set, which a
	// running cycle is reading. Rebuilding it in place is a race; the exit is
	// not.
	if !sameCalDAV(was.CalDAV, now.CalDAV) {
		out.Restart, out.Reason = true, "the hand-configured calendars changed"
		return out
	}

	for _, name := range sortedAccounts(now.Secondary) {
		sec := now.Secondary[name]
		old, had := was.Secondary[name]
		if had && sameAccount(old, sec) {
			continue
		}
		if had {
			d.StopAccount(name)
		}
		acct, err := a.Build(name, sec)
		if err != nil {
			// The account stays in the config: the file is the record, and a
			// server that is down now may be up in a minute. It is a problem so
			// that somebody can see why it is not there.
			d.SetProblem("account "+name, err.Error())
			out.Changes = append(out.Changes, fmt.Sprintf("account %s: %v", name, err))
			continue
		}
		d.SetProblem("account "+name, "")
		d.StartAccount(acct)
		out.Changes = append(out.Changes, fmt.Sprintf("account %s is up", name))
	}

	for _, name := range sortedAccounts(was.Secondary) {
		if _, still := now.Secondary[name]; still {
			continue
		}
		// A queue is not something to drop quietly, and a Held mail may already
		// have been delivered (ADR-0017). The account is kept and the refusal is
		// reported: a declarative file plus a component that may decline needs
		// somewhere for the declining to be seen (ADR-0021).
		if a.InFlight != nil {
			if held, err := a.InFlight(name); err == nil && held > 0 {
				detail := fmt.Sprintf("%s was removed from the config, but %d of its mails "+
					"are still in the outbox — it is kept until they are sent or cancelled",
					name, held)
				d.SetProblem("account "+name, detail)
				out.Changes = append(out.Changes, detail)
				continue
			}
		}
		d.StopAccount(name)
		d.SetProblem("account "+name, "")
		if a.Forget != nil {
			if err := a.Forget(name); err != nil {
				d.logf("forget %s: %v", name, err)
			}
		}
		out.Changes = append(out.Changes, fmt.Sprintf("account %s is gone", name))
	}

	if d.DAV != nil && !sameStrings(was.Collections.Exclude, now.Collections.Exclude) {
		d.DAV.SetExclude(now.Collections.Exclude)
		out.Changes = append(out.Changes, "the excluded collections changed")
	}
	if was.Account.TaskList != now.Account.TaskList ||
		was.Account.AddressBook != now.Account.AddressBook {
		d.SetDefaults(now.Account.TaskList, now.Account.AddressBook)
		out.Changes = append(out.Changes, "the default task list or address book changed")
	}
	return out
}

// primaryDiffers names the change, or "" when nothing that matters moved. The
// Primary's connections are its identity in practice: its address, its servers,
// its credentials and the Boxes it holds an IDLE connection on.
func primaryDiffers(was, now config.Account) string {
	switch {
	case was.Email != now.Email:
		return "the primary account's address changed"
	case was.IMAPHost != now.IMAPHost || was.IMAPPort != now.IMAPPort:
		return "the primary account's imap server changed"
	case was.SMTPHost != now.SMTPHost || was.SMTPPort != now.SMTPPort:
		return "the primary account's smtp server changed"
	case was.DAVEndpoint != now.DAVEndpoint:
		return "the primary account's dav server changed"
	case was.SieveHost != now.SieveHost || was.SievePort != now.SievePort:
		return "the primary account's sieve server changed"
	case was.Password != now.Password || was.DAVPassword != now.DAVPassword:
		return "the primary account's password changed"
	case !sameStrings(was.Watch, now.Watch):
		return "the watched boxes changed"
	case was.SentBox != now.SentBox:
		return "the sent box changed"
	case was.DisplayName != now.DisplayName:
		return "the display name changed"
	case was.NoDAV != now.NoDAV:
		return "no_dav changed"
	}
	return ""
}

// sameAccount compares two Secondary Account blocks. A Watch list makes the
// struct uncomparable, which is why this is spelt out rather than ==.
func sameAccount(a, b config.Account) bool {
	return a.Email == b.Email && a.Password == b.Password &&
		a.DisplayName == b.DisplayName &&
		a.IMAPHost == b.IMAPHost && a.IMAPPort == b.IMAPPort &&
		a.SMTPHost == b.SMTPHost && a.SMTPPort == b.SMTPPort &&
		a.SentBox == b.SentBox && a.DAVPassword == b.DAVPassword &&
		a.DAVEndpoint == b.DAVEndpoint &&
		a.TaskList == b.TaskList && a.AddressBook == b.AddressBook &&
		a.SieveHost == b.SieveHost && a.SievePort == b.SievePort &&
		sameStrings(a.Watch, b.Watch)
}

func sameCalDAV(was, now map[string]config.Calendar) bool {
	if len(was) != len(now) {
		return false
	}
	for key, a := range was {
		if b, ok := now[key]; !ok || a != b {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sortedAccounts keeps the order stable, so a log read twice says the same
// thing in the same order.
func sortedAccounts(m map[string]config.Account) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
