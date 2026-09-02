package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mailbox/internal/config"
	"mailbox/skill"
)

// A second run asks nothing. It shows what is here and offers to change it,
// because a person coming back to this wizard wants to see the state before
// choosing — the same reason the first run enumerates instead of asking.

// snapshot is what this machine looks like right now. Live state comes from the
// Daemon; with no Daemon the config and the filesystem still answer, which is
// exactly when somebody needs to see them.
type snapshot struct {
	cfg    *config.Config
	cfgErr error
	client *Client
	// accounts, calendars and active are the Daemon's answers, empty when it is
	// not running.
	accounts  []map[string]any
	calendars []map[string]any
	active    string
}

func (s *snapshot) live() bool { return s.client != nil }

func (s *snapshot) close() {
	if s.client != nil {
		s.client.Close()
	}
}

func (w *Wizard) snapshot(ctx context.Context) *snapshot {
	s := &snapshot{}
	s.cfg, s.cfgErr = config.LoadFrom(w.ConfigPath)
	if w.Socket == "" {
		return s
	}
	c, err := Dial(w.Socket, 0)
	if err != nil {
		return s
	}
	s.client = c
	if resp, err := c.Do([]string{"status"}, nil); err == nil && resp.OK {
		s.accounts = rowsOf(resp.Data)
	}
	if resp, err := c.Do([]string{"calendar", "list"}, nil); err == nil && resp.OK {
		s.calendars = rowsOf(resp.Data)
	}
	if resp, err := c.Do([]string{"sieve", "list"}, nil); err == nil && resp.OK {
		for _, r := range rowsOf(resp.Data) {
			if b, _ := r["active"].(bool); b {
				s.active = fmt.Sprint(r["name"])
			}
		}
	}
	return s
}

// repair rewrites what has drifted: the systemd units, and the Routing Boxes on
// a Primary Account that has lost them. An account restored from backup, or one
// that was set up before there was a wizard, comes back without INBOX/Screener,
// and every `mailbox route` call is refused until it is here again (ADR-0019).
func (w *Wizard) repair(ctx context.Context, s *snapshot) error {
	if err := w.install(); err != nil {
		return err
	}
	if s.cfg == nil {
		return s.cfgErr
	}
	acc := s.cfg.Account
	boxes, err := w.Prober.IMAP(ctx, acc.IMAPHost, acc.IMAPPort, acc.Email, acc.Password)
	if err != nil {
		return fmt.Errorf("imap: %w", err)
	}
	have := make([]string, len(boxes))
	for i, b := range boxes {
		have[i] = b.Name
	}
	if len(MissingBoxes(have)) == 0 {
		return nil
	}
	return w.bootstrapRouting(ctx, Answers{
		Email: acc.Email, Password: acc.Password,
		IMAPHost: acc.IMAPHost, IMAPPort: acc.IMAPPort,
	}, boxes)
}

// manage is the second run: state, then one action, then the state again.
func (w *Wizard) manage(ctx context.Context) error {
	for {
		s := w.snapshot(ctx)
		w.show(s)
		choice, err := w.ask("a) add   r) remove   p) repair   q) quit", "q")
		if err != nil {
			s.close()
			return err
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "a", "add":
			err = w.add(ctx, s)
		case "r", "remove":
			err = w.remove(ctx, s)
		case "p", "repair":
			err = w.repair(ctx, s)
		default:
			s.close()
			return nil
		}
		// One action failing is not the end of the session: the person is
		// standing here and can try something else.
		if err != nil {
			w.sayf("  %v", err)
		}
		s.close()
		w.say("")
	}
}

func (w *Wizard) show(s *snapshot) {
	w.say("")
	w.say("mailbox — this machine")
	w.say("")
	if s.cfgErr != nil {
		w.sayf("  config     %v", s.cfgErr)
	} else {
		w.sayf("  config     %s", w.ConfigPath)
	}
	if !s.live() {
		w.say("             the daemon is not answering — read from the config and the disk")
	}

	if s.cfg != nil {
		w.say("  accounts")
		w.sayf("    %-9s %-28s %s", "primary", s.cfg.Account.Email, w.liveNote(s, "primary"))
		for _, name := range sortedKeys(s.cfg.Secondary) {
			a := s.cfg.Secondary[name]
			w.sayf("    %-9s %-28s %s", name, a.Email, w.liveNote(s, name))
		}

		// One line per Collection, whether it was discovered or configured by
		// hand. A hand-added one is mirrored like any other, so listing the
		// config blocks separately printed it twice and made two calendars out
		// of one.
		w.say("  calendars")
		configured := map[string]string{}
		for _, key := range sortedKeys(s.cfg.CalDAV) {
			cal := s.cfg.CalDAV[key]
			name := cal.Name
			if name == "" {
				name = key
			}
			configured[strings.ToLower(name)] = fmt.Sprintf("caldav.%s at %s", key, hostOf(cal.URL))
		}
		if len(s.calendars) == 0 {
			w.say("    discovered on the account's own server")
		}
		for _, c := range s.calendars {
			if b, _ := c["internal"].(bool); b {
				continue
			}
			name := fmt.Sprint(c["name"])
			where := fmt.Sprintf("%d entries", int(numOf(c["count"])))
			if from, ok := configured[strings.ToLower(name)]; ok {
				where += ", " + from
				delete(configured, strings.ToLower(name))
			}
			w.sayf("    %-9s %-28s %s", fmt.Sprint(c["kind"]), name, where)
		}
		// A configured Collection with no row beside it is one the daemon is
		// not holding — down, refused, or excluded — and that is worth seeing.
		for _, name := range sortedKeys(configured) {
			w.sayf("    %-9s %-28s %s", "", name, configured[name]+", not mirrored")
		}
		for _, name := range s.cfg.Collections.Exclude {
			w.sayf("    %-9s %-28s excluded", "", name)
		}
	}

	units, stale := "current", w.pending()
	switch {
	case len(stale) == len(w.Units.Files()):
		units = "not installed"
	case len(stale) > 0:
		units = "out of date: " + strings.Join(stale, ", ")
	}
	if w.Ctl != nil {
		if on, err := w.Ctl.IsEnabled("mailbox.socket"); err == nil && on {
			units += ", socket enabled"
		} else {
			units += ", socket not enabled"
		}
	}
	w.sayf("  systemd    %s", units)

	skillState := "not installed"
	if got, err := os.ReadFile(filepath.Join(w.Skill.Dir, "SKILL.md")); err == nil {
		skillState = "out of date"
		if string(got) == skill.Markdown {
			skillState = "current"
		}
	}
	w.sayf("  skill      %s — %s", w.Skill.Dir, skillState)

	if s.active != "" {
		w.sayf("  routing    the server runs %q", s.active)
	}
	w.say("")
}

// liveNote is what the Daemon says about one account, or nothing when it is not
// running. A wizard that can only describe a machine when the daemon happens to
// be up is one nobody can trust when things are broken.
func (w *Wizard) liveNote(s *snapshot, account string) string {
	for _, r := range s.accounts {
		if fmt.Sprint(r["account"]) == account {
			return fmt.Sprintf("%d boxes, %d in the inbox", int(numOf(r["boxes"])), int(numOf(r["count"])))
		}
	}
	if s.live() {
		return "not held by the daemon yet"
	}
	return ""
}

// pending is the units that would be written if install ran now: the ones that
// are missing, and the ones that say something else. A unit this program did
// not write cannot be assumed to agree with this binary about
// --systemd-socket (ADR-0012).
func (w *Wizard) pending() []string {
	var out []string
	for name, want := range w.Units.Files() {
		got, err := os.ReadFile(filepath.Join(w.Units.Dir, name))
		if err != nil || string(got) != want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// add is a second mail account, or a calendar on a server this account's own
// one cannot be asked about.
func (w *Wizard) add(ctx context.Context, s *snapshot) error {
	w.say("")
	kind, err := w.pickOne([]string{"a mail account", "a calendar or address book"}, "a mail account")
	if err != nil {
		return err
	}
	name, err := w.newName(s)
	if err != nil {
		return err
	}
	if strings.HasPrefix(kind, "a mail") {
		return w.addAccount(ctx, s, name)
	}
	return w.addCalendar(ctx, s, name)
}

// newName takes the short name the thing is known by. One namespace across both
// tables: `search --in gmx` and the agenda reading two different `gmx` is a bug
// nobody would suspect.
func (w *Wizard) newName(s *snapshot) (string, error) {
	name, err := w.ask("A short name for it (this is the id prefix)", "")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("it needs a name")
	case strings.ContainsAny(name, " /.\"'[]"):
		return "", fmt.Errorf("%q has a character an id cannot carry", name)
	case strings.EqualFold(name, "primary"):
		return "", fmt.Errorf("primary is the account with no prefix")
	}
	if s.cfg != nil {
		if _, taken := s.cfg.Secondary[name]; taken {
			return "", fmt.Errorf("there is already an account called %q", name)
		}
		if _, taken := s.cfg.CalDAV[name]; taken {
			return "", fmt.Errorf("there is already a calendar called %q", name)
		}
	}
	return name, nil
}

// addAccount asks for the four things a Secondary Account cannot be asked for,
// and asks the server for the rest. It has an Inbox and the ability to send; the
// Screener and the Routing belong to the Primary alone.
func (w *Wizard) addAccount(ctx context.Context, s *snapshot, name string) error {
	email, err := w.ask("Email address", "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("an account needs an address")
	}
	password, err := w.secret("Password")
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("an account needs a password")
	}
	// No default host: a Secondary is on somebody else's server, and guessing
	// which one is how mail ends up going nowhere. The domain is a suggestion,
	// not an answer.
	imapHost, err := w.ask("IMAP host", "imap."+domainOf(email))
	if err != nil {
		return err
	}
	display, err := w.ask("The name mail goes out under", "")
	if err != nil {
		return err
	}

	block := AccountBlock{
		Name: name, Email: email, Password: password, DisplayName: display,
		IMAPHost: imapHost, IMAPPort: 993, SMTPPort: 465,
	}
	w.sayf("Asking %s what it has…", imapHost)
	boxes, err := w.Prober.IMAP(ctx, block.IMAPHost, block.IMAPPort, email, password)
	if err != nil {
		return fmt.Errorf("imap: %w", err)
	}
	w.sayf("  %d boxes", len(boxes))

	// Submission is derived and checked rather than asked for. Only a server
	// that refuses the derived name is worth a question.
	smtp := "smtp." + domainOf(email)
	if err := w.Prober.SMTP(ctx, smtp, 465, email, password); err != nil {
		w.sayf("  %s will not take it: %v", smtp, err)
		if smtp, err = w.ask("SMTP host (blank: this account cannot send)", ""); err != nil {
			return err
		}
		if smtp != "" {
			if err := w.Prober.SMTP(ctx, smtp, 465, email, password); err != nil {
				return fmt.Errorf("smtp: %w", err)
			}
		}
	}
	if smtp != "" {
		w.sayf("  %s takes the password too", smtp)
		block.SMTPHost = smtp
	}

	if err := AddAccount(w.ConfigPath, block); err != nil {
		return err
	}
	w.sayf("  added as %q — its ids read %s/INBOX:412", name, name)
	w.reload(s)
	return nil
}

// addCalendar finds a Collection on another provider's server. It asks for the
// server and the credentials and then enumerates: the URL that goes in the
// config is the one the server gave, never one typed here (ADR-0010).
func (w *Wizard) addCalendar(ctx context.Context, s *snapshot, name string) error {
	endpoint, err := w.ask("The server (https://dav.example.org/)", "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("it needs a server to ask")
	}
	fallbackUser := ""
	if s.cfg != nil {
		fallbackUser = s.cfg.Account.Email
	}
	user, err := w.ask("Username", fallbackUser)
	if err != nil {
		return err
	}
	password, err := w.secret("Password")
	if err != nil {
		return err
	}

	w.sayf("Asking %s what it has…", endpoint)
	cols, err := w.Prober.DAV(ctx, endpoint, user, password)
	if err != nil {
		return fmt.Errorf("dav: %w", err)
	}
	if len(cols) == 0 {
		return fmt.Errorf("%s offered nothing", endpoint)
	}
	labels := make([]string, 0, len(cols))
	for _, c := range cols {
		labels = append(labels, fmt.Sprintf("%s (%s)", c.Name, c.Kind))
	}
	w.say("")
	w.say("Which of these should be mirrored?")
	picked, err := w.pickMany(labels, labels[:1])
	if err != nil {
		return err
	}
	n := 0
	for i, label := range labels {
		if !contains(picked, label) {
			continue
		}
		key := name
		if n > 0 {
			key = fmt.Sprintf("%s%d", name, n+1)
		}
		n++
		if err := AddCalendar(w.ConfigPath, CalendarBlock{
			Key: key, Name: cols[i].Name, URL: cols[i].URL, User: user,
			Password: password, Kind: cols[i].Kind, Color: cols[i].Color,
		}); err != nil {
			return err
		}
		w.sayf("  added %q as %q", cols[i].Name, key)
	}
	if n == 0 {
		return fmt.Errorf("nothing picked, nothing written")
	}
	w.reload(s)
	return nil
}

// remove takes one thing out of the config. The Primary Account is not on the
// list: removing it is an uninstall, not a removal (ADR-0005).
func (w *Wizard) remove(ctx context.Context, s *snapshot) error {
	if s.cfg == nil {
		return s.cfgErr
	}
	type item struct {
		label  string
		header string
		kind   string
		name   string
	}
	var items []item
	for _, name := range sortedKeys(s.cfg.Secondary) {
		items = append(items, item{
			label:  fmt.Sprintf("account %s (%s)", name, s.cfg.Secondary[name].Email),
			header: "accounts." + name, kind: "account", name: name,
		})
	}
	for _, key := range sortedKeys(s.cfg.CalDAV) {
		items = append(items, item{
			label:  fmt.Sprintf("calendar %s (%s)", key, s.cfg.CalDAV[key].Name),
			header: "caldav." + key, kind: "calendar", name: key,
		})
	}
	for _, c := range s.calendars {
		if b, _ := c["internal"].(bool); b {
			continue
		}
		cname := fmt.Sprint(c["name"])
		if s.cfg.Excluded(cname) {
			continue
		}
		items = append(items, item{
			label: fmt.Sprintf("stop mirroring %s (discovered)", cname),
			kind:  "exclude", name: cname,
		})
	}
	if len(items) == 0 {
		return fmt.Errorf("there is nothing here that can be removed")
	}

	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, it.label)
	}
	w.say("")
	choice, err := w.pickOne(labels, "")
	if err != nil {
		return err
	}
	var chosen item
	for _, it := range items {
		if it.label == choice {
			chosen = it
		}
	}

	switch chosen.kind {
	case "account":
		// The Daemon is the only thing that knows what is in flight, so it is
		// asked before the file is changed — otherwise the config says the
		// account is gone while a Held mail of its is still waiting for a
		// person (ADR-0021).
		if held, err := inFlight(s, chosen.name); err != nil {
			return err
		} else if len(held) > 0 {
			return fmt.Errorf("%s still has %s in the outbox: %s",
				chosen.name, plural(len(held), "mail", "mails"), strings.Join(held, ", "))
		}
		if err := RemoveBlock(w.ConfigPath, chosen.header); err != nil {
			return err
		}
		w.sayf("  removed %q; the daemon drops its mail from the mirror", chosen.name)
	case "calendar":
		if err := RemoveBlock(w.ConfigPath, chosen.header); err != nil {
			return err
		}
		w.sayf("  removed %q", chosen.name)
	case "exclude":
		// A discovered Collection cannot be removed, only excluded: delete its
		// row and the next discovery puts it straight back.
		if err := SetExcluded(w.ConfigPath, append(s.cfg.Collections.Exclude, chosen.name)); err != nil {
			return err
		}
		w.sayf("  %q is no longer mirrored", chosen.name)
	}
	w.reload(s)
	return nil
}

// inFlight is what the Daemon still holds for an account. Anything not yet
// filed counts: a Held mail may already have been delivered, and nothing sends
// it again until a person says so.
func inFlight(s *snapshot, account string) ([]string, error) {
	if !s.live() {
		return nil, fmt.Errorf("the daemon is not running, so nothing can say " +
			"whether that account has mail in flight — start it and try again")
	}
	resp, err := s.client.Do([]string{"outbox", "list"}, nil)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, nil // an account with no outbox has nothing in flight
	}
	var out []string
	for _, r := range rowsOf(resp.Data) {
		if fmt.Sprint(r["account"]) != account {
			continue
		}
		switch fmt.Sprint(r["state"]) {
		case "queued", "sent", "held":
			out = append(out, fmt.Sprintf("#%d %s", int(numOf(r["id"])), fmt.Sprint(r["state"])))
		}
	}
	return out, nil
}

// reload tells the Daemon the file moved. It re-reads on its own within a
// minute either way; this is only so the wizard does not look laggy (ADR-0021).
func (w *Wizard) reload(s *snapshot) {
	if !s.live() {
		return
	}
	if resp, err := s.client.Do([]string{"reload"}, nil); err == nil && resp.OK {
		w.say("  the daemon has re-read the config")
	}
}

func rowsOf(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func numOf(v any) float64 {
	n, _ := v.(float64)
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func domainOf(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return email[i+1:]
	}
	return email
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
