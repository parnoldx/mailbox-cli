package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"mailbox/src/internal/config"
	"mailbox/src/internal/dav"
	"mailbox/src/internal/doctor"
	"mailbox/src/internal/format"
	"mailbox/src/internal/skill"
	"mailbox/src/internal/thunderbird"
)

const wordmark = `          ###
        #######                                      ## ###
       ########                                 ### ### ###
  #######   ####                         ###        ### ### ##       ##
  ######       ####     #############  #######  ### ### ########  ######## ###  ###
  ####        #####     ###  ###   ###      ### ### ### ###   ### ###   ###  #####
     ####   #######     ###  ###   ### ######## ### ### ###   ### ###   ###  ####
      #########         ###  ###   ### ### #### ### ### ######### ########  #######
       ######           ###  ###   ### ######## ### ### ########   ###### ####  ####
        ####`

const brandGreen = "\033[38;2;118;187;33m"

func cmdSetup(args []string, out *format.Output) (int, error) {
	if len(args) > 0 && args[0] != "skill" && args[0] != "install" {
		return 0, usageErr("unknown setup command %q", args[0])
	}
	skillOnly := len(args) > 0
	if skillOnly || !setupInteractive(out) {
		return installSkill(out)
	}
	return runSetupWizard(out)
}

func setupInteractive(out *format.Output) bool {
	if out.JSON || out.Quiet || out.IDsOnly || out.Count || out.JQ != "" {
		return false
	}
	if os.Getenv("MAILBOX_NONINTERACTIVE") != "" {
		return false
	}
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func installSkill(out *format.Output) (int, error) {
	paths, err := skill.InstallSkill("")
	if err != nil {
		return 0, err
	}
	return format.WriteOK(format.NewOM("installed", paths), out, ""), nil
}

func runSetupWizard(out *format.Output) (int, error) {
	w := &wizard{in: os.Stdin, out: os.Stdout, br: bufio.NewReader(os.Stdin)}
	if err := w.run(); err != nil {
		return 0, err
	}
	return 0, nil
}

type wizard struct {
	in  io.Reader
	out io.Writer
	br  *bufio.Reader
}

func (w *wizard) run() error {
	w.welcome()
	vals, err := w.collect()
	if err != nil {
		return err
	}
	if err := config.WriteFile(vals); err != nil {
		return err
	}
	fmt.Fprintf(w.out, "Wrote %s\n\n", config.ConfigPath())

	paths, err := skill.InstallSkill("")
	if err != nil {
		return err
	}
	fmt.Fprintln(w.out, statusOK("Agent skill installed"))
	for _, p := range paths {
		fmt.Fprintf(w.out, "  %s\n", p)
	}
	fmt.Fprintln(w.out)

	w.verify()
	w.tryItOut()
	return nil
}

func (w *wizard) welcome() {
	fmt.Fprintln(w.out, tint(wordmark))
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, bold("mailbox.org — command-line interface"))
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "Let's get you set up.")
	fmt.Fprintln(w.out)
}

func (w *wizard) collect() (map[string]string, error) {
	profiles := thunderbird.ListProfiles("")
	def := "2"
	if len(profiles) > 0 {
		def = "1"
	}
	fmt.Fprintln(w.out, bold("Step 1: Account"))
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "How should mailbox find your account?")
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "  1) Thunderbird (reads the profile each run)")
	fmt.Fprintln(w.out, "  2) Enter email and passwords here")
	fmt.Fprintln(w.out)
	choice := w.ask(">", def)
	if choice == "1" {
		vals, err := w.collectThunderbird(profiles)
		if err == nil {
			return vals, nil
		}
		fmt.Fprintf(w.out, "%s\n\n", err)
	}
	return w.collectManual()
}

func (w *wizard) collectThunderbird(profiles []string) (map[string]string, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no Thunderbird profile found")
	}
	path := profiles[0]
	if len(profiles) > 1 {
		fmt.Fprintln(w.out, "Thunderbird profiles:")
		for i, p := range profiles {
			fmt.Fprintf(w.out, "  %d) %s\n", i+1, p)
		}
		n := w.askInt("Profile", 1, len(profiles))
		path = profiles[n-1]
	}
	tb, err := thunderbird.LoadThunderbird("", path)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(w.out)
	fmt.Fprintf(w.out, "  profile  %s\n", path)
	fmt.Fprintf(w.out, "  email    %s\n", dash(tb.Email))
	fmt.Fprintf(w.out, "  IMAP     %s\n", yesNo(tb.Password != ""))
	fmt.Fprintf(w.out, "  DAV      %s\n", yesNo(tb.DAVPassword != ""))
	fmt.Fprintf(w.out, "  Kalender %s\n", yesNo(tb.KalenderURL != ""))
	fmt.Fprintf(w.out, "  Aufgaben %s\n", yesNo(tb.AufgabenURL != ""))
	fmt.Fprintf(w.out, "  Kontakte %s\n", yesNo(tb.KontakteURL != ""))
	fmt.Fprintln(w.out)
	if !w.yesNo("Use this profile?", true) {
		return nil, fmt.Errorf("Thunderbird declined")
	}
	vals := map[string]string{"MAILBOX_TB_PROFILE": path}
	if tb.Password == "" {
		fmt.Fprintln(w.out)
		fmt.Fprintln(w.out, "Thunderbird has the account but no IMAP password.")
		imap, davPW := w.askPasswords()
		vals["MAILBOX_PASSWORD"] = imap
		if davPW != imap {
			vals["MAILBOX_DAV_PASSWORD"] = davPW
		}
	} else if tb.DAVPassword == "" {
		fmt.Fprintln(w.out)
		fmt.Fprintln(w.out, "No DAV password in Thunderbird (imap.mailbox.org and dav.mailbox.org differ when app passwords are on).")
		if w.yesNo("Use app passwords? IMAP and DAV are then different.", true) {
			vals["MAILBOX_DAV_PASSWORD"] = w.askSecret("DAV password (dav.mailbox.org): ")
		}
	}
	return vals, nil
}

func (w *wizard) collectManual() (map[string]string, error) {
	fmt.Fprintln(w.out)
	email := w.ask("Email:", "")
	for email == "" || !strings.Contains(email, "@") {
		email = w.ask("Email (user@mailbox.org):", "")
	}
	imap, davPW := w.askPasswords()
	vals := map[string]string{
		"MAILBOX_EMAIL":    email,
		"MAILBOX_PASSWORD": imap,
	}
	if davPW != imap {
		vals["MAILBOX_DAV_PASSWORD"] = davPW
	}
	fmt.Fprintln(w.out, "Looking up CalDAV / CardDAV…")
	kal, auf, kon := discoverDAV(email, davPW)
	if kal == "" {
		kal = w.ask("CalDAV Kalender URL:", "")
	} else {
		fmt.Fprintf(w.out, "  Kalender %s\n", kal)
	}
	if auf == "" {
		auf = w.ask("CalDAV Aufgaben URL:", "")
	} else {
		fmt.Fprintf(w.out, "  Aufgaben %s\n", auf)
	}
	if kon == "" {
		kon = w.ask("CardDAV Kontakte URL:", "")
	} else {
		fmt.Fprintf(w.out, "  Kontakte %s\n", kon)
	}
	if kal != "" {
		vals["MAILBOX_CALDAV_KALENDER"] = kal
	}
	if auf != "" {
		vals["MAILBOX_CALDAV_AUFGABEN"] = auf
	}
	if kon != "" {
		vals["MAILBOX_CARDDAV_KONTAKTE"] = kon
	}
	return vals, nil
}

func (w *wizard) askPasswords() (imap, davPW string) {
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "mailbox.org 2FA uses application passwords. IMAP (imap.mailbox.org)")
	fmt.Fprintln(w.out, "and DAV (dav.mailbox.org) then have different ones.")
	fmt.Fprintln(w.out)
	if w.yesNo("Use app passwords?", false) {
		imap = w.askSecret("IMAP password: ")
		davPW = w.askSecret("DAV password: ")
		return imap, davPW
	}
	pw := w.askSecret("Password: ")
	return pw, pw
}

func (w *wizard) verify() {
	fmt.Fprintln(w.out, bold("Step 2: Check"))
	fmt.Fprintln(w.out)
	report := doctor.Run(nil, nil)
	for _, c := range report.Checks {
		if c.OK {
			fmt.Fprintln(w.out, statusOK(c.Name+" — "+c.Detail))
		} else {
			fmt.Fprintln(w.out, statusBad(c.Name+" — "+c.Detail))
		}
	}
	fmt.Fprintln(w.out)
}

func (w *wizard) tryItOut() {
	fmt.Fprintln(w.out, bold("Step 3: Try it out"))
	fmt.Fprintln(w.out)
	examples := [][2]string{
		{"mailbox tui", "Open TUI"},
		{"mailbox box list", "List your boxes"},
		{"mailbox box view inbox", "Read Inbox"},
		{"mailbox doctor", "Check credentials"},
	}
	width := 0
	for _, ex := range examples {
		if len(ex[0]) > width {
			width = len(ex[0])
		}
	}
	for _, ex := range examples {
		fmt.Fprintf(w.out, "  %s%s  %s\n", ex[0], strings.Repeat(" ", width-len(ex[0])), ex[1])
	}
	fmt.Fprintln(w.out)
}

func (w *wizard) ask(prompt, def string) string {
	if def != "" {
		fmt.Fprintf(w.out, "%s [%s] ", prompt, def)
	} else {
		fmt.Fprintf(w.out, "%s ", prompt)
	}
	line, err := w.br.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func (w *wizard) askInt(prompt string, def, max int) int {
	raw := w.ask(prompt, strconv.Itoa(def))
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return def
	}
	return n
}

func (w *wizard) yesNo(prompt string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	switch strings.ToLower(w.ask(prompt, hint)) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return def
	}
}

func (w *wizard) askSecret(prompt string) string {
	fmt.Fprint(w.out, prompt)
	if f, ok := w.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(w.out)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	line, _ := w.br.ReadString('\n')
	return strings.TrimSpace(line)
}

var discoverDAV = discoverDAVLive

func discoverDAVLive(email, pass string) (kalender, aufgaben, kontakte string) {
	client := dav.New(email, pass)
	kalender, aufgaben = discoverCalDAV(client)
	kontakte = discoverCardDAV(client)
	return
}

func discoverCalDAV(client *dav.Client) (kalender, aufgaben string) {
	const base = "https://dav.mailbox.org/caldav/"
	raw, status, err := client.Propfind(base, `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:displayname/></d:prop></d:propfind>`, "1")
	if err != nil || (status != 200 && status != 207) {
		return "", ""
	}
	rows := dav.ParseMultistatus(raw, "displayname")
	kalender = namedHref(rows, base, "Kalender")
	aufgaben = namedHref(rows, base, "Aufgaben")
	return kalender, aufgaben
}

func discoverCardDAV(client *dav.Client) string {
	const base = "https://dav.mailbox.org/carddav/"
	raw, status, err := client.Propfind(base, `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:displayname/></d:prop></d:propfind>`, "1")
	if err != nil || (status != 200 && status != 207) {
		return ""
	}
	rows := dav.ParseMultistatus(raw, "displayname")
	if u := namedHref(rows, base, "Kontakte"); u != "" {
		return u
	}
	root := strings.TrimRight(base, "/")
	for _, r := range rows {
		u := absURL(base, r.Href)
		if strings.TrimRight(u, "/") != root {
			return u
		}
	}
	return ""
}

func namedHref(rows []dav.Response, base, name string) string {
	for _, r := range rows {
		if strings.EqualFold(strings.TrimSpace(r.Data), name) {
			return absURL(base, r.Href)
		}
	}
	return ""
}

func absURL(base, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return u.ResolveReference(ref).String()
}

func tint(s string) string {
	if os.Getenv("NO_COLOR") != "" || !isTTY(os.Stdout) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = brandGreen + line + "\033[0m"
	}
	return strings.Join(lines, "\n")
}

func bold(s string) string {
	if os.Getenv("NO_COLOR") != "" || !isTTY(os.Stdout) {
		return s
	}
	return "\033[1m" + s + "\033[0m"
}

func statusOK(s string) string {
	if os.Getenv("NO_COLOR") != "" || !isTTY(os.Stdout) {
		return "OK  " + s
	}
	return "\033[32m✓\033[0m " + s
}

func statusBad(s string) string {
	if os.Getenv("NO_COLOR") != "" || !isTTY(os.Stdout) {
		return "ERR " + s
	}
	return "\033[31m✗\033[0m " + s
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
