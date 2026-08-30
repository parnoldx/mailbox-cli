// Package config reads the account settings. Credentials live in a mode-0600
// file rather than the Secret Service, deliberately (ADR-0014).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Account is one IMAP + SMTP login.
type Account struct {
	Email    string `toml:"email"`
	Password string `toml:"password"`
	// DisplayName is the name mail goes out under. Only sending uses it: a
	// reader takes the name from the header it was given.
	DisplayName string `toml:"display_name"`
	IMAPHost    string `toml:"imap_host"`
	IMAPPort    int    `toml:"imap_port"`
	SMTPHost    string `toml:"smtp_host"`
	// SMTPPort is 465 (implicit TLS) unless it is 587, which negotiates it with
	// STARTTLS. Nothing here ever speaks SMTP in the clear.
	SMTPPort int `toml:"smtp_port"`
	// SentBox overrides the Box a sent copy is filed in. Empty means ask the
	// server, which flags it \Sent.
	SentBox string `toml:"sent_box"`
	// DAVPassword is separate at mailbox.org: the calendar and address book
	// servers take an application password, not the mail one.
	DAVPassword string `toml:"dav_password"`
	// TaskList is where a Todo goes when the caller does not name one. With
	// several task lists and no default, adding is refused rather than guessed.
	TaskList string `toml:"task_list"`
	// Watch is the subset of Boxes that gets an IDLE connection. Every Box is
	// mirrored either way: watching is about how fast we hear, mirroring is
	// about what we hold.
	Watch []string `toml:"watch"`
	// AddressBook is where a new Contact goes when the caller does not name
	// one. The Global Address Book is somebody else's and never a default.
	AddressBook string `toml:"address_book"`
	// DAVEndpoint is where discovery starts. Every collection URL comes from
	// asking this server, never from a URL typed in below (ADR-0010).
	DAVEndpoint string `toml:"dav_endpoint"`
	// SieveHost is where the Routing script lives. Empty means the IMAP host,
	// which is where ManageSieve sits on every provider that offers it. Only
	// the Primary Account has one: a Secondary has an Inbox and nothing to
	// route.
	SieveHost string `toml:"sieve_host"`
	// SievePort is 4190, the registered ManageSieve port. The connection starts
	// in the clear there and is upgraded with STARTTLS, or refused.
	SievePort int `toml:"sieve_port"`
}

// Calendar is a Collection on a server we cannot discover: another provider,
// with its own credentials. Collections on the account's own DAV server need no
// entry here — they are found by asking it.
type Calendar struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Color    string `toml:"color"`
	// Kind is "events" or "tasks"; empty means events.
	Kind string `toml:"kind"`
}

// Collections says which discovered Collections are not mirrored. The decision
// lives here rather than on a row in the Mirror because the Mirror is deleted
// and rebuilt on a schema change (ADR-0013) — a decision stored there has a
// half-life, and the next discovery would put back what was excluded.
type Collections struct {
	// Exclude is matched against a Collection's display name, which is what
	// discovery matches on. A URL is never written down by hand (ADR-0010).
	Exclude []string `toml:"exclude"`
}

// Config is everything on disk.
type Config struct {
	Account Account `toml:"account"`
	// Secondary accounts, keyed by the name their ids are prefixed with:
	// `[accounts.gmx]` makes `gmx/INBOX:412` mean something (ADR-0005). They
	// have an Inbox, Drafts and Sent and the ability to Send; the Screener and
	// the routing belong to the Primary Account alone.
	Secondary map[string]Account `toml:"accounts"`
	// Collections is what discovery finds and this machine does not mirror.
	Collections Collections `toml:"collections"`
	// CalDAV holds hand-configured collections, keyed by a short name. Only
	// ones with their own credentials are used: an entry pointing at the
	// account's own server is already discovered, and a URL copied by hand is
	// how the address book came to point at a 2-entry scratch list.
	CalDAV map[string]Calendar `toml:"caldav"`
}

// Excluded says whether a Collection is one this machine does not mirror.
func (c *Config) Excluded(name string) bool {
	for _, e := range c.Collections.Exclude {
		if strings.EqualFold(strings.TrimSpace(e), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// Path returns the config file location.
func Path() string {
	if p := os.Getenv("MAILBOX_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "mailbox.toml"
	}
	return filepath.Join(dir, "mailbox", "config.toml")
}

// Load reads the config at the usual place.
func Load() (*Config, error) { return LoadFrom(Path()) }

// LoadFrom reads one, applying mailbox.org's defaults. The path is a parameter
// because the wizard is handed the file it edits rather than finding it.
func LoadFrom(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s: run `mailbox setup`", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if c.Account.Email == "" || c.Account.Password == "" {
		return nil, fmt.Errorf("%s: account.email and account.password are required", path)
	}
	if c.Account.IMAPHost == "" {
		c.Account.IMAPHost = "imap.mailbox.org"
	}
	if c.Account.IMAPPort == 0 {
		c.Account.IMAPPort = 993
	}
	if c.Account.SMTPHost == "" {
		c.Account.SMTPHost = "smtp.mailbox.org"
	}
	if c.Account.SMTPPort == 0 {
		c.Account.SMTPPort = 465
	}
	if c.Account.DAVEndpoint == "" {
		c.Account.DAVEndpoint = "https://dav.mailbox.org/"
	}
	if c.Account.DAVPassword == "" {
		c.Account.DAVPassword = c.Account.Password
	}
	if c.Account.SieveHost == "" {
		c.Account.SieveHost = c.Account.IMAPHost
	}
	if c.Account.SievePort == 0 {
		c.Account.SievePort = 4190
	}
	for name, a := range c.Secondary {
		if a.Email == "" || a.Password == "" {
			return nil, fmt.Errorf("%s: accounts.%s needs an email and a password", path, name)
		}
		if a.IMAPHost == "" {
			// No default here: a secondary account is on somebody else's
			// server, and guessing which one is how mail ends up going nowhere.
			return nil, fmt.Errorf("%s: accounts.%s needs imap_host", path, name)
		}
		if a.IMAPPort == 0 {
			a.IMAPPort = 993
		}
		if a.SMTPPort == 0 {
			a.SMTPPort = 465
		}
		c.Secondary[name] = a
	}
	return &c, nil
}

// MirrorPath is where the Mirror file lives. It is derived state and safe to
// delete (ADR-0013), so it belongs in a cache directory.
func MirrorPath() (string, error) {
	if p := os.Getenv("MAILBOX_MIRROR"); p != "" {
		return p, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "mailbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "mirror.db"), nil
}

// OutboxPath is where the Outbox file lives. It is not derived state and is
// never deleted, so it belongs beside the config rather than in a cache
// directory the system may clear (ADR-0013).
func OutboxPath() (string, error) {
	if p := os.Getenv("MAILBOX_OUTBOX"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "mailbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "outbox.db"), nil
}

// SocketPath is where the Daemon listens.
func SocketPath() string {
	if p := os.Getenv("MAILBOX_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "mailbox.sock")
	}
	return filepath.Join(os.TempDir(), "mailbox.sock")
}
