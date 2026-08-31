package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailbox/internal/imapdrv"
	"mailbox/internal/sync/davsync"
)

// server is a scripted account: the Boxes and Collections a real one would
// report, without any of them existing.
type server struct {
	boxes       []imapdrv.Box
	collections []davsync.Collection
	imapErr     error
	smtpErr     error
	davErr      error
	sawPassword string
	// booted is what Routing was asked to do, and what it said it did.
	booted  *Bootstrap
	bootErr error
	// configPath and afterWrite are gate 4: after the probes, setup opens no
	// server connection. Every probe records whether the config was already on
	// disk when it was called.
	configPath string
	afterWrite bool
}

// note records that a server was talked to, and when.
func (s *server) note() {
	if s.configPath == "" {
		return
	}
	if _, err := os.Stat(s.configPath); err == nil {
		s.afterWrite = true
	}
}

func (s *server) IMAP(ctx context.Context, host string, port int, user, password string) ([]imapdrv.Box, error) {
	s.sawPassword = password
	s.note()
	return s.boxes, s.imapErr
}

func (s *server) SMTP(ctx context.Context, host string, port int, user, password string) error {
	s.note()
	return s.smtpErr
}

func (s *server) DAV(ctx context.Context, endpoint, user, password string) ([]davsync.Collection, error) {
	s.note()
	return s.collections, s.davErr
}

func (s *server) Routing(ctx context.Context, a Answers, boxes []string) (Bootstrap, error) {
	s.note()
	if s.bootErr != nil {
		return Bootstrap{}, s.bootErr
	}
	b := Bootstrap{Created: MissingBoxes(boxes), Wrote: true, Active: "Open-Xchange"}
	s.booted = &b
	return b, nil
}

func account() *server {
	return &server{
		boxes: []imapdrv.Box{
			{Name: "INBOX", Messages: 260},
			{Name: "INBOX/Screener", Messages: 12},
			{Name: "INBOX/Feed", Messages: 400},
			{Name: "Gesendet", SpecialUse: "sent", Messages: 900},
			{Name: "Papierkorb", SpecialUse: "trash"},
			{Name: "Archive", SpecialUse: "archive", Messages: 8000},
		},
		collections: []davsync.Collection{
			{Kind: "events", URL: "https://dav/1/", Name: "Kalender"},
			{Kind: "tasks", URL: "https://dav/2/", Name: "Aufgaben"},
			{Kind: "tasks", URL: "https://dav/3/", Name: "Einkauf"},
			{Kind: "cards", URL: "https://dav/4/", Name: "Gesammelte Adressen"},
			{Kind: "cards", URL: "https://dav/5/", Name: "Kontakte"},
		},
	}
}

func run(t *testing.T, srv *server, answers string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "mailbox", "config.toml")
	srv.configPath = path
	var out strings.Builder
	w := &Wizard{
		In: strings.NewReader(answers), Out: &out, Prober: srv, ConfigPath: path,
		// The install steps write into directories they are given, so a whole
		// setup can be run into a temporary one. There is no systemd here and
		// no socket: those are the two things a test cannot supply.
		Units: Units{Dir: filepath.Join(home, "systemd"), Exec: "/usr/bin/mailbox"},
		Skill: Skill{Dir: filepath.Join(home, "skills", "mailbox")},
	}
	err := w.Run(context.Background())
	written := ""
	if b, readErr := os.ReadFile(path); readErr == nil {
		written = string(b)
	}
	if err == nil {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		// It holds a password (ADR-0014).
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %v", info.Mode().Perm())
		}
	}
	return written, out.String(), err
}

// The answers, in order: address, password, dav password, display name,
// watched boxes, task list, address book, and whether to set up the routing.
const straightThrough = "me@example.org\nhunter2\n\nMax Mustermann\n\n\n\n\n"

func TestTheWizardAsksAndThenWritesWhatTheServersSaid(t *testing.T) {
	srv := account()
	written, _, err := run(t, srv, straightThrough)
	if err != nil {
		t.Fatal(err)
	}

	// Nothing was typed. The Sent box is the one the server flags, whatever it
	// is called in whatever language; the watch defaults are the Inbox and the
	// Screener, because a sign-in link that arrives a minute late has expired;
	// and the address book is not the scratch one the old config pointed at
	// for years.
	for _, want := range []string{
		`email = "me@example.org"`, `password = "hunter2"`,
		`display_name = "Max Mustermann"`, `sent_box = "Gesendet"`,
		`task_list = "Aufgaben"`, `address_book = "Kontakte"`,
		`watch = ["INBOX", "INBOX/Screener"]`,
		`imap_host = "imap.mailbox.org"`, `smtp_port = 465`,
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the config does not contain %s:\n%s", want, written)
		}
	}
	// The same password twice is written once.
	if strings.Contains(written, "dav_password") {
		t.Errorf("dav_password was written even though it is the same one:\n%s", written)
	}
}

func TestTheWizardTakesChoicesByNumberOrName(t *testing.T) {
	srv := account()
	// The candidates are INBOX, INBOX/Feed, INBOX/Screener in that order:
	// watch only the third, take the shopping list by name and the second book.
	written, _, err := run(t, srv, "me@example.org\nhunter2\ndav-secret\nMax\n3\nEinkauf\n2\n\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`watch = ["INBOX/Screener"]`, `task_list = "Einkauf"`,
		`address_book = "Kontakte"`, `dav_password = "dav-secret"`,
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the config does not contain %s:\n%s", want, written)
		}
	}
}

func TestWatchingNothingIsAnAnswer(t *testing.T) {
	written, _, err := run(t, account(), "me@example.org\nhunter2\n\nMax\nnone\n\n\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(written, "watch =") {
		t.Fatalf("watching nothing wrote a watch list:\n%s", written)
	}
}

func TestAServerThatRefusesThePasswordStopsBeforeWriting(t *testing.T) {
	srv := account()
	srv.imapErr = errors.New("AUTHENTICATIONFAILED")
	written, _, err := run(t, srv, straightThrough)
	if err == nil || !strings.Contains(err.Error(), "AUTHENTICATIONFAILED") {
		t.Fatalf("err = %v", err)
	}
	if written != "" {
		t.Fatal("a password that does not work was written to disk anyway")
	}

	// The same for submission: a password that works for reading and not for
	// sending fails on the first send otherwise, which is the worst moment.
	srv = account()
	srv.smtpErr = errors.New("535 authentication failed")
	if written, _, err := run(t, srv, straightThrough); err == nil || written != "" {
		t.Fatalf("err = %v, written = %q", err, written)
	}
}

func TestAConfigThatIsAlreadyThereIsNotAskedAboutAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	theirs := "# theirs\n[account]\nemail = \"me@example.org\"\npassword = \"hunter2\"\n"
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	w := &Wizard{
		In: strings.NewReader("q\n"), Out: &out, Prober: account(), ConfigPath: path,
		Units: Units{Dir: filepath.Join(dir, "systemd"), Exec: "/usr/bin/mailbox"},
		Skill: Skill{Dir: filepath.Join(dir, "skills", "mailbox")},
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second run shows the machine and offers to change it. It asks for no
	// address and no password, and it replaces nothing: there is no --force
	// because there is no overwrite.
	if got := out.String(); !strings.Contains(got, "mailbox — this machine") ||
		strings.Contains(got, "Email address") {
		t.Fatalf("a second run should show state, not ask questions:\n%s", got)
	}
	if b, _ := os.ReadFile(path); string(b) != theirs {
		t.Fatalf("the config was changed by a run that only looked:\n%s", b)
	}
}

func TestAnAccountWithNoAddressIsNotAnAccount(t *testing.T) {
	if _, _, err := run(t, account(), "\n"); err == nil {
		t.Fatal("an empty address should stop the wizard")
	}
}

func TestNothingTalksToAServerOnceTheConfigExists(t *testing.T) {
	srv := account()
	if _, _, err := run(t, srv, straightThrough); err != nil {
		t.Fatal(err)
	}
	// Gate 4: after the probes setup opens no server connection and writes
	// nothing but files. The first sync is the daemon's, watched over the
	// socket, because a second writer of the Mirror is the one thing the shape
	// of this program forbids (ADR-0012).
	if srv.afterWrite {
		t.Fatal("a server was talked to after the config had been written")
	}
	if srv.booted == nil {
		t.Fatal("the routing was never set up")
	}
	// The Boxes a fresh account has not got, and no naming choices.
	if len(srv.booted.Created) != 4 {
		t.Fatalf("created %v", srv.booted.Created)
	}
}

func TestASecondRunAddsAnAccountWithoutRewritingTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}
	// add, the default kind (a mail account), the name, the address, the
	// password, the derived imap host, the display name, then quit.
	answers := "a\n\ngmx\nme@gmx.de\nsecond\n\nMax\nq\n"
	var out strings.Builder
	w := &Wizard{
		In: strings.NewReader(answers), Out: &out, Prober: account(), ConfigPath: path,
		Units: Units{Dir: filepath.Join(dir, "systemd"), Exec: "/usr/bin/mailbox"},
		Skill: Skill{Dir: filepath.Join(dir, "skills", "mailbox")},
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), handWritten) {
		t.Fatalf("the hand-written config was rewritten:\n%s", got)
	}
	for _, want := range []string{`[accounts.gmx]`, `imap_host = "imap.gmx.de"`, `smtp_host = "smtp.gmx.de"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the config does not contain %s:\n%s", want, got)
		}
	}
}

func TestOneNameMeansOneThing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}
	// `verein` is already a calendar, so it cannot also be an account: two
	// different things under one prefix is a bug nobody would suspect.
	var out strings.Builder
	w := &Wizard{
		In:     strings.NewReader("a\n\nverein\nq\n"),
		Out:    &out,
		Prober: account(), ConfigPath: path,
		Units: Units{Dir: filepath.Join(dir, "systemd"), Exec: "/usr/bin/mailbox"},
		Skill: Skill{Dir: filepath.Join(dir, "skills", "mailbox")},
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already a calendar") {
		t.Fatalf("a name in use was taken anyway:\n%s", out.String())
	}
}

func TestACalendarOnAnotherServerIsFoundRatherThanTyped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}
	// add, the second kind, a name, the server, the default user, a password,
	// the fifth collection it offered, then quit.
	answers := "a\n2\nkontakte\nhttps://dav.example.org/\n\nsecret\n5\nq\n"
	var out strings.Builder
	w := &Wizard{
		In: strings.NewReader(answers), Out: &out, Prober: account(), ConfigPath: path,
		Units: Units{Dir: filepath.Join(dir, "systemd"), Exec: "/usr/bin/mailbox"},
		Skill: Skill{Dir: filepath.Join(dir, "skills", "mailbox")},
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	// The URL written is the one the server gave for the collection that was
	// picked by name — not the address that was typed to reach the server.
	// `carddav_home` pointed at a 2-entry scratch book for years because
	// somebody copied a URL by hand.
	for _, want := range []string{
		`[caldav.kontakte]`, `name = "Kontakte"`, `url = "https://dav/5/"`, `kind = "cards"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the config does not contain %s:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), `url = "https://dav.example.org/"`) {
		t.Errorf("the typed address was written as the collection's url:\n%s", got)
	}
}
