// Package config reads the account settings. Credentials live in a mode-0600
// file rather than the Secret Service, deliberately (ADR-0014).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
	// NoDAV skips the calendars, task lists and address books entirely: no
	// discovery, no hand-configured collections, no periodic cycle. For the VPS
	// Daemon (ADR-0025), which exists for mail routing and bubble timing and
	// reads no calendar — leaving DAV configured there just fails it on every
	// cycle once the app password is gone, since an empty DAVPassword falls
	// back to the mail one and mailbox.org refuses that for CalDAV/CardDAV.
	NoDAV bool `toml:"no_dav"`
	// Color is the account's identity on screen: the edge of its rows and the
	// Send button of a mail going out from it. An Omarchy palette name, which
	// follows the theme, or "#rrggbb", which does not. Empty is filled in by
	// LoadFrom: the accent for the Primary, the palette in turn for the rest.
	Color string `toml:"color"`
}

// AccountColors is the palette a Secondary Account's colour is taken from when
// it has none, in order. The accent is left out: it is the Primary's.
var AccountColors = []string{"orange", "magenta", "green", "yellow", "cyan", "red"}

// validColor says whether a colour is one the clients can paint: a palette
// name they resolve against the live theme, or a literal hex one.
func validColor(c string) bool {
	switch c {
	case "accent", "red", "yellow", "orange", "green", "cyan", "blue", "magenta", "brown":
		return true
	}
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// NextColor is the first palette colour no account in use has, so an account
// added by the wizard does not look like one already there. With every one
// taken it starts again from the top.
func NextColor(used []string) string {
	for _, c := range AccountColors {
		if !slices.Contains(used, c) {
			return c
		}
	}
	return AccountColors[0]
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
	// Email is the address whose meeting invites belong on this calendar. Empty
	// means the calendar is only a choice when an RSVP cannot tell which one.
	Email string `toml:"email"`
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

// Bubble is the two hours `mailbox bubble` resolves its timing flags to. HEY's
// "Later today" is the evening one; every other flag lands on the morning one.
// There is no config default for the flag itself — one is always required, to
// match HEY exactly.
type Bubble struct {
	// Morning is the hour --tomorrow, --weekend, --next-week and a future --on
	// return at. Default 8.
	Morning int `toml:"morning"`
	// Evening is the hour `--on <today>` returns at, this morning having passed.
	// Default 18.
	Evening int `toml:"evening"`
}

// Pickup is the one knob on code mail. Detection itself is not configurable:
// a phrase list somebody has to maintain by hand is a worse answer than a log
// line saying what nearly matched.
type Pickup struct {
	// Expiry is how long a Pickup waits before it is binned, as a Go duration
	// ("15m", "1h"). Empty means the Daemon's default. It is a knob because the
	// right value depends on how fast you log in, which no default can know.
	Expiry string `toml:"expiry"`
}

// Fileee is the fileee.com drop box: sending a mail there with a file attached
// is fileee's entire integration, so this is the one address that means.
type Fileee struct {
	// Address is the personal inbox address fileee assigned. Empty means the
	// "send to fileee" button has nothing to send to.
	Address string `toml:"address"`
}

// Config is everything on disk.
type Config struct {
	Account Account `toml:"account"`
	// Bubble is when a bubbled thread comes back.
	Bubble Bubble `toml:"bubble"`
	// Pickup is how long a collected login code's mail is kept.
	Pickup Pickup `toml:"pickup"`
	// Fileee is the address the "send to fileee" attachment button uses.
	Fileee Fileee `toml:"fileee"`
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
	// The file holds the mail and DAV passwords, so who can read it is part of
	// whether it is usable at all (ADR-0014). Doctor reports the mode, but every
	// command and the daemon load through here, so this is where the contract is
	// kept rather than merely advised.
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is mode %04o: it holds a password, so chmod 600 it",
			path, info.Mode().Perm())
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
	if c.Account.Color == "" {
		c.Account.Color = "accent"
	}
	if !validColor(c.Account.Color) {
		return nil, fmt.Errorf("%s: account.color %q is neither a palette name nor #rrggbb", path, c.Account.Color)
	}
	// Sorted, so an account without a colour gets the same one on every load
	// rather than whichever the map hands out first.
	names := make([]string, 0, len(c.Secondary))
	used := []string{c.Account.Color}
	for name, a := range c.Secondary {
		names = append(names, name)
		used = append(used, a.Color)
	}
	sort.Strings(names)
	for _, name := range names {
		a := c.Secondary[name]
		if a.Color == "" {
			a.Color = NextColor(used)
			used = append(used, a.Color)
		}
		if !validColor(a.Color) {
			return nil, fmt.Errorf("%s: accounts.%s.color %q is neither a palette name nor #rrggbb", path, name, a.Color)
		}
		c.Secondary[name] = a
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
	// No runtime directory and no home to fall back to. The empty path is
	// deliberate: Listen refuses it rather than binding a bare name in a
	// world-writable /tmp, where any local user can answer as the daemon.
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "mailbox")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return filepath.Join(dir, "mailbox.sock")
		}
	}
	return ""
}
