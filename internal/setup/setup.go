// Package setup is the wizard: the only place in this program where a human is
// asked anything, and the thing that makes a machine work — the config, the
// systemd units, the agent skill, and the Routing Boxes on a fresh account.
//
// It writes files and nothing else. After the probes it opens no server
// connection and never touches the Mirror: it writes the config, nudges the
// Daemon, and watches the first sync over the socket like any other client
// (ADR-0021).
//
// It must not ask for a URL. It authenticates and then *enumerates* — IMAP LIST
// for the Boxes and their special-use flags, DAV discovery for the Collections
// — and shows what it found with names and counts. A URL copied by hand is how
// the old config came to point at a 2-entry scratch address book instead of
// Kontakte, and it had been quietly wrong for years.
package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/imapdrv"
	"mailbox/internal/sync/davsync"
)

// Answers is what the wizard learnt, and what it writes.
type Answers struct {
	Email       string
	Password    string
	DAVPassword string
	DisplayName string
	IMAPHost    string
	IMAPPort    int
	SMTPHost    string
	SMTPPort    int
	DAVEndpoint string
	SentBox     string
	Watch       []string
	TaskList    string
	AddressBook string
}

// Prober is everything the wizard needs from the servers. It is an interface so
// the wizard can be tested without any of them, which matters because the
// wizard is the one thing here that cannot be exercised by an agent.
type Prober interface {
	// IMAP logs in and lists the Boxes with their flags and counts.
	IMAP(ctx context.Context, host string, port int, user, password string) ([]imapdrv.Box, error)
	// SMTP logs in and does nothing else. It is a check, not a send.
	SMTP(ctx context.Context, host string, port int, user, password string) error
	// DAV discovers the Collections.
	DAV(ctx context.Context, endpoint, user, password string) ([]davsync.Collection, error)
	// Routing creates the Boxes a fresh account has not got and puts an empty
	// Routing script up if there is none. It is the last thing that talks to a
	// server here.
	Routing(ctx context.Context, a Answers, boxes []string) (Bootstrap, error)
}

// Wizard asks the questions.
type Wizard struct {
	In     io.Reader
	Out    io.Writer
	Prober Prober
	// ReadPassword reads without echoing. It is a field so a test can answer
	// without a terminal.
	ReadPassword func(prompt string) (string, error)
	// ConfigPath is where the answers go. There is no force: a config that is
	// already there is edited a block at a time, never replaced.
	ConfigPath string
	// Socket is where the Daemon listens, which is how setup watches the first
	// sync and how it asks what is in flight before removing an account.
	Socket string
	// Units and Skill are the two things setup installs besides the config.
	Units Units
	Skill Skill
	// Ctl enables the socket unit. Nil where there is no systemd to talk to.
	Ctl Systemctl
	// Doctor is the check setup ends on, supplied by the caller because it
	// lives in the CLI. Ending on it means the person has seen its output once
	// while everything worked, which is what makes the broken version legible.
	Doctor func() int

	in *bufio.Reader
}

// Run is the whole wizard. A machine with no config gets the linear first run;
// a machine that has one is asked nothing and offered what it can change.
func (w *Wizard) Run(ctx context.Context) error {
	w.in = bufio.NewReader(w.In)
	if _, err := os.Stat(w.ConfigPath); err == nil {
		return w.manage(ctx)
	}
	return w.first(ctx)
}

// first is the run that turns a bare machine into a working one.
func (w *Wizard) first(ctx context.Context) error {
	a := Answers{
		IMAPHost: "imap.mailbox.org", IMAPPort: 993,
		SMTPHost: "smtp.mailbox.org", SMTPPort: 465,
		DAVEndpoint: "https://dav.mailbox.org/",
	}

	w.say("mailbox setup")
	w.say("")
	w.say("Nothing here asks for a URL: the servers are asked what they have.")
	w.say("")

	var err error
	if a.Email, err = w.ask("Email address", ""); err != nil {
		return err
	}
	if strings.TrimSpace(a.Email) == "" {
		return fmt.Errorf("an account needs an address")
	}
	if a.Password, err = w.secret("Password"); err != nil {
		return err
	}
	if a.Password == "" {
		return fmt.Errorf("an account needs a password")
	}
	if a.DAVPassword, err = w.secret("Calendar/contacts password (blank: the same one)"); err != nil {
		return err
	}
	if a.DAVPassword == "" {
		a.DAVPassword = a.Password
	}
	if a.DisplayName, err = w.ask("The name mail goes out under", ""); err != nil {
		return err
	}

	// The mail server. Everything below is what it says it has.
	w.say("")
	w.sayf("Asking %s what it has…", a.IMAPHost)
	boxes, err := w.Prober.IMAP(ctx, a.IMAPHost, a.IMAPPort, a.Email, a.Password)
	if err != nil {
		return fmt.Errorf("imap: %w", err)
	}
	a.SentBox = sentBox(boxes)
	w.sayf("  %d boxes, and %s is where sent mail goes", len(boxes), quoteOr(a.SentBox, "nowhere it will admit to"))

	// Both servers, before anything is written. A password that reads mail and
	// is refused for submission fails on the first send otherwise, which is the
	// worst possible moment.
	if err := w.Prober.SMTP(ctx, a.SMTPHost, a.SMTPPort, a.Email, a.Password); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	w.sayf("  %s takes the password too", a.SMTPHost)

	// Watched Boxes. Mirrored is every Box; watched is the few worth an IDLE
	// connection, and that is a question only the person reading the mail can
	// answer.
	candidates := watchCandidates(boxes)
	w.say("")
	w.say("Which boxes should be watched? A watched box notices new mail in a")
	w.say("second; the rest are reconciled on the minute either way.")
	a.Watch, err = w.pickMany(candidates, defaultWatch(candidates))
	if err != nil {
		return err
	}

	// The calendar and contact servers.
	w.say("")
	w.sayf("Asking %s what it has…", a.DAVEndpoint)
	collections, err := w.Prober.DAV(ctx, a.DAVEndpoint, a.Email, a.DAVPassword)
	if err != nil {
		return fmt.Errorf("dav: %w", err)
	}
	for _, c := range collections {
		w.sayf("  %-8s %s", c.Kind, c.Name)
	}
	if lists := names(collections, "tasks"); len(lists) > 0 {
		w.say("")
		w.say("Which task list should `mailbox todo add` use?")
		if a.TaskList, err = w.pickOne(lists, lists[0]); err != nil {
			return err
		}
	}
	if books := names(collections, "cards"); len(books) > 0 {
		w.say("")
		w.say("Which address book should a new contact go in?")
		if a.AddressBook, err = w.pickOne(books, preferredBook(books)); err != nil {
			return err
		}
	}

	// The Routing, on the server, before the config exists. This is the last
	// thing here that talks to a server: everything after it is files.
	if err := w.bootstrapRouting(ctx, a, boxes); err != nil {
		return err
	}

	if err := Write(w.ConfigPath, a); err != nil {
		return err
	}
	w.say("")
	w.sayf("Written to %s (mode 0600).", w.ConfigPath)

	if err := w.install(); err != nil {
		return err
	}
	w.follow(ctx)
	w.check()
	return nil
}

// bootstrapRouting offers the five Boxes the Routing needs and the empty script
// that fills them. It is offered once, with no naming choices: the Boxes are
// the ones `mailbox route` files into, and a Screener under another name is not
// a Screener.
func (w *Wizard) bootstrapRouting(ctx context.Context, a Answers, boxes []imapdrv.Box) error {
	have := make([]string, 0, len(boxes))
	for _, b := range boxes {
		have = append(have, b.Name)
	}
	missing := MissingBoxes(have)
	w.say("")
	if len(missing) == 0 {
		w.say("The screener, the feed, the paper trail and the aside pile are all here.")
	} else {
		w.sayf("This account has not got %s.", strings.Join(missing, ", "))
		w.say("Mail cannot be routed anywhere it cannot be filed, so they are made now.")
	}
	yes, err := w.confirm("Set up the routing", true)
	if err != nil {
		return err
	}
	if !yes {
		w.say("  left alone — `mailbox route` will refuse until they exist")
		return nil
	}
	b, err := w.Prober.Routing(ctx, a, have)
	if err != nil {
		// A server with no ManageSieve still holds mail. The Routing is what
		// stops working, not the account.
		w.sayf("  routing: %v", err)
		return nil
	}
	for _, name := range b.Created {
		w.sayf("  created %s", name)
	}
	switch {
	case b.Activated:
		w.sayf("  %q is up and is the active script", "logic")
	case b.Wrote:
		w.sayf("  %q is up; %q is what the server runs", "logic", b.Active)
	default:
		w.sayf("  %q is already on this account and was not touched", "logic")
	}
	if b.Unreachable {
		w.sayf("  note: %q neither is ours nor includes it. Add `include \"logic\";`", b.Active)
		w.say("  to it, or the routing is stored and never runs.")
	}
	return nil
}

// install writes the two units, enables the socket and installs the skill.
// Everything here is a file: the config named the account, and this is the part
// that makes the machine run it.
func (w *Wizard) install() error {
	w.say("")
	written, err := w.Units.Install()
	if err != nil {
		return fmt.Errorf("systemd: %w", err)
	}
	switch {
	case len(written) == 0:
		w.sayf("systemd  %s — already current", w.Units.Dir)
	default:
		w.sayf("systemd  wrote %s in %s", strings.Join(written, " and "), w.Units.Dir)
	}
	if w.Ctl != nil {
		if err := w.Ctl.EnableNow("mailbox.socket"); err != nil {
			return fmt.Errorf("systemd: %w", err)
		}
		w.say("         mailbox.socket enabled — the first connection starts the daemon")
	}

	// A skill that cannot be installed is not a reason to stop: the mail works
	// without it. It is reported and the wizard carries on.
	changed, err := w.Skill.Install()
	switch {
	case err != nil:
		w.sayf("skill    not installed: %v", err)
	case changed:
		w.sayf("skill    %s", w.Skill.Dir)
	default:
		w.sayf("skill    %s — already current", w.Skill.Dir)
	}
	return nil
}

// follow watches the Daemon's first cycle over the socket. It prints Boxes as
// they land rather than a spinner: a cold start is minutes, and a wizard that
// ends in silence looks broken.
//
// The order — Inbox first, Archive last — is the Daemon's, not this program's.
// Setup used to do this sync itself, in its own process, which made it a second
// writer of the Mirror (ADR-0012).
func (w *Wizard) follow(ctx context.Context) {
	if w.Socket == "" {
		return
	}
	c, err := Dial(w.Socket, 10*time.Second)
	if err != nil {
		w.say("")
		w.sayf("The daemon is not answering yet: %v", err)
		w.say("Start one with `mailbox daemon` and it will fill the mirror.")
		return
	}
	defer c.Close()

	w.say("")
	w.say("Filling the mirror. This is the slow one; every later start is not.")
	pushes, stop, err := Pushes(w.Socket)
	if err == nil {
		defer stop()
	}

	seen := map[string]bool{}
	report := func(box string) {
		if box == "" || seen[box] {
			return
		}
		seen[box] = true
		w.sayf("  %-28s %s", box, countIn(c, box))
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-pushes:
			if p.Event == "mail.changed" {
				report(p.Box)
			}
		case <-tick.C:
			resp, err := c.Do([]string{"status"}, nil)
			if err != nil {
				return
			}
			if resp.Mirror != nil && resp.Mirror.SyncedAt != nil && !resp.Mirror.Syncing {
				w.say("")
				w.say("The mirror is filled.")
				return
			}
		}
	}
}

// countIn asks the Mirror what it now holds in one Box. The push said only that
// something moved (ADR-0011), so the count comes from a read like everybody
// else's.
func countIn(c *Client, box string) string {
	resp, err := c.Do([]string{"box", "list"}, map[string]any{"archive": true})
	if err != nil || !resp.OK {
		return "held"
	}
	rows, _ := resp.Data.([]any)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(m["folder"]) == box || fmt.Sprint(m["box"]) == box {
			n, _ := m["count"].(float64)
			return fmt.Sprintf("%d messages", int(n))
		}
	}
	return "held"
}

// check ends the wizard on `mailbox doctor`: the both-ends check, run once at
// the one moment when everything is known to work.
func (w *Wizard) check() {
	if w.Doctor == nil {
		return
	}
	w.say("")
	w.say("Checking, from both ends:")
	w.say("")
	w.Doctor()
}

// Write puts the first run's answers on disk. It is the one write here that
// replaces a whole file, and it only ever runs when there was no file (ADR-0021
// is why: after this, the config is a record other things edit too).
func Write(path string, a Answers) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# mailbox configuration — written by `mailbox setup`.\n")
	b.WriteString("# It holds passwords, so it is mode 0600 and stays that way (ADR-0014).\n")
	b.WriteString("# The daemon re-reads this file while it runs (ADR-0021): an edit here\n")
	b.WriteString("# lands within a minute, and `mailbox setup` edits it a block at a time.\n\n")
	b.WriteString("[account]\n")
	line(&b, "email", a.Email)
	line(&b, "password", a.Password)
	if a.DAVPassword != a.Password {
		line(&b, "dav_password", a.DAVPassword)
	}
	line(&b, "display_name", a.DisplayName)
	fmt.Fprintf(&b, "imap_host = %q\nimap_port = %d\n", a.IMAPHost, a.IMAPPort)
	fmt.Fprintf(&b, "smtp_host = %q\nsmtp_port = %d\n", a.SMTPHost, a.SMTPPort)
	line(&b, "dav_endpoint", a.DAVEndpoint)
	b.WriteString("\n# Where a sent copy is filed, and where new entries go when the\n")
	b.WriteString("# command does not say. All three were discovered, not typed.\n")
	line(&b, "sent_box", a.SentBox)
	line(&b, "task_list", a.TaskList)
	line(&b, "address_book", a.AddressBook)
	if len(a.Watch) > 0 {
		b.WriteString("\n# The boxes worth an IDLE connection. Every box is mirrored either way.\n")
		fmt.Fprintf(&b, "watch = [%s]\n", quoteList(a.Watch))
	}
	return writeFile(path, b.String())
}

// sentBox is the Box the server flags \Sent. A name is a guess that is wrong in
// every language but one.
func sentBox(boxes []imapdrv.Box) string {
	for _, b := range boxes {
		if b.SpecialUse == "sent" {
			return b.Name
		}
	}
	for _, b := range boxes {
		if strings.EqualFold(b.Name, "Sent") {
			return b.Name
		}
	}
	return ""
}

// watchCandidates are the Boxes worth offering: the Inbox and what is under it.
// Offering all 40 of an account's Boxes would make the question unanswerable.
func watchCandidates(boxes []imapdrv.Box) []string {
	var out []string
	for _, b := range boxes {
		if b.SpecialUse != "" && b.SpecialUse != "archive" {
			continue
		}
		if strings.EqualFold(b.Name, "INBOX") || strings.HasPrefix(strings.ToUpper(b.Name), "INBOX/") {
			out = append(out, b.Name)
		}
	}
	sort.Strings(out)
	return out
}

// defaultWatch is the Inbox and the Screener if there is one. A sign-in link
// that lands in the Screener has expired by the time a minute's poll finds it.
//
// It matches the Screener itself and not what is filed under it: INBOX/Screener
// is where new senders wait, and INBOX/Screener/Block is where the ones you
// never want to hear from again go.
func defaultWatch(candidates []string) []string {
	var out []string
	for _, c := range candidates {
		base := c
		if i := strings.LastIndex(c, "/"); i >= 0 {
			base = c[i+1:]
		}
		if strings.EqualFold(c, "INBOX") || strings.EqualFold(base, "Screener") {
			out = append(out, c)
		}
	}
	return out
}

func names(collections []davsync.Collection, kind string) []string {
	var out []string
	for _, c := range collections {
		if c.Kind == kind {
			out = append(out, c.Name)
		}
	}
	return out
}

// preferredBook avoids defaulting to a book that is not the account's own. A
// global address book is the provider's directory, and a contact written into
// it is at best refused.
func preferredBook(books []string) string {
	for _, b := range books {
		lower := strings.ToLower(b)
		if !strings.Contains(lower, "global") && !strings.Contains(lower, "gesammelt") {
			return b
		}
	}
	return books[0]
}

func (w *Wizard) ask(prompt, fallback string) (string, error) {
	if fallback != "" {
		w.printf("%s [%s]: ", prompt, fallback)
	} else {
		w.printf("%s: ", prompt)
	}
	line, err := w.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

// confirm asks a yes/no question with a default.
func (w *Wizard) confirm(prompt string, fallback bool) (bool, error) {
	def := "y"
	if !fallback {
		def = "n"
	}
	answer, err := w.ask(prompt+"? [y/n]", def)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return fallback, nil
}

func (w *Wizard) secret(prompt string) (string, error) {
	if w.ReadPassword != nil {
		return w.ReadPassword(prompt + ": ")
	}
	return w.ask(prompt, "")
}

// pickOne shows a numbered list and takes a number or a name.
func (w *Wizard) pickOne(options []string, fallback string) (string, error) {
	for i, o := range options {
		w.sayf("  %d) %s", i+1, o)
	}
	answer, err := w.ask("Choose", fallback)
	if err != nil {
		return "", err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(answer)); err == nil {
		if n < 1 || n > len(options) {
			return "", fmt.Errorf("there is no %d in that list", n)
		}
		return options[n-1], nil
	}
	for _, o := range options {
		if strings.EqualFold(o, answer) {
			return o, nil
		}
	}
	return "", fmt.Errorf("%q is not one of those", answer)
}

// pickMany takes several numbers, or "none".
func (w *Wizard) pickMany(options, fallback []string) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	for i, o := range options {
		w.sayf("  %d) %s", i+1, o)
	}
	answer, err := w.ask("Choose (numbers, comma separated, or none)", strings.Join(fallback, ", "))
	if err != nil {
		return nil, err
	}
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "none") {
		return nil, nil
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(answer, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			if n < 1 || n > len(options) {
				return nil, fmt.Errorf("there is no %d in that list", n)
			}
			part = options[n-1]
		}
		matched := ""
		for _, o := range options {
			if strings.EqualFold(o, part) {
				matched = o
			}
		}
		if matched == "" {
			return nil, fmt.Errorf("%q is not one of those", part)
		}
		if !seen[matched] {
			seen[matched] = true
			out = append(out, matched)
		}
	}
	return out, nil
}

func (w *Wizard) say(s string)                   { w.printf("%s\n", s) }
func (w *Wizard) sayf(format string, a ...any)   { w.printf(format+"\n", a...) }
func (w *Wizard) printf(format string, a ...any) { fmt.Fprintf(w.Out, format, a...) }

func quoteOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return strconv.Quote(s)
}
