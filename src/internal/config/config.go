package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"mailbox/src/internal/thunderbird"
)

type Account struct {
	Email       string
	Password    string
	IMAPHost    string
	IMAPPort    int
	KalenderURL string
	AufgabenURL string
	KontakteURL string
	DisplayName string
	SMTPHost    string
	SMTPPort    int
}

// MissingCredsError maps to python SystemExit -> exit code 3.
type MissingCredsError struct{ Msg string }

func (e *MissingCredsError) Error() string     { return e.Msg }
func (e *MissingCredsError) ErrorCode() string { return "auth" }

// LoadThunderbirdHook is overridden in tests.
var LoadThunderbirdHook func(profileName string) (*thunderbird.Account, error)

func loadTB() (*thunderbird.Account, error) {
	if LoadThunderbirdHook != nil {
		return LoadThunderbirdHook(os.Getenv("MAILBOX_TB_PROFILE"))
	}
	return thunderbird.LoadThunderbird("", os.Getenv("MAILBOX_TB_PROFILE"))
}

func pick(envKey string, tbVal string, def string) string {
	if env := os.Getenv(envKey); env != "" {
		return env
	}
	if tbVal != "" {
		return tbVal
	}
	return def
}

func opt(v string) string { return v }

type Probe struct {
	Email             string
	PasswordSet       bool
	IMAPHost          string
	IMAPPort          int
	KalenderURL       string
	AufgabenURL       string
	KontakteURL       string
	DisplayName       string
	ThunderbirdProfile string
	Missing           []string
	Hint              string

	password   string
	SMTPHost   string
	SMTPPort   int
}

func ProbeAccount() *Probe {
	tb, err := loadTB()
	var tbEmail, tbPassword, tbIMAPHost, tbKalender, tbAufgaben, tbKontakte, tbDisplay string
	var tbPort int
	if err == nil && tb != nil {
		tbEmail, tbPassword = tb.Email, tb.Password
		tbIMAPHost = tb.IMAPHost
		tbPort = tb.IMAPPort
		tbKalender, tbAufgaben, tbKontakte = tb.KalenderURL, tb.AufgabenURL, tb.KontakteURL
		tbDisplay = tb.DisplayName
	}

	email := pick("MAILBOX_EMAIL", tbEmail, "")
	password := pick("MAILBOX_PASSWORD", tbPassword, "")
	imapHost := pick("MAILBOX_IMAP_HOST", tbIMAPHost, "imap.mailbox.org")
	imapPort := 993
	if raw := os.Getenv("MAILBOX_IMAP_PORT"); raw != "" {
		imapPort, _ = strconv.Atoi(raw)
	} else if err == nil && tb != nil && tbPort != 0 {
		imapPort = tbPort
	}
	kalender := pick("MAILBOX_CALDAV_KALENDER", tbKalender, "")
	aufgaben := pick("MAILBOX_CALDAV_AUFGABEN", tbAufgaben, "")
	kontakte := pick("MAILBOX_CARDDAV_KONTAKTE", tbKontakte, "")
	display := pick("MAILBOX_DISPLAY_NAME", tbDisplay, "")
	smtpHost := pick("MAILBOX_SMTP_HOST", "", "smtp.mailbox.org")
	smtpPort := 465
	if raw := os.Getenv("MAILBOX_SMTP_PORT"); raw != "" {
		smtpPort, _ = strconv.Atoi(raw)
	}

	var missing []string
	if email == "" {
		missing = append(missing, "MAILBOX_EMAIL")
	}
	if password == "" {
		missing = append(missing, "MAILBOX_PASSWORD")
	}
	hint := ""
	switch {
	case err != nil:
		hint = "Thunderbird profile unread or incomplete; set env"
	case password == "":
		hint = "Thunderbird password unread; set MAILBOX_PASSWORD"
	default:
		hint = "set env"
	}
	profile := ""
	if err == nil && tb != nil && tb.Profile != "" {
		profile = tb.Profile
	}
	if display == "" && email != "" {
		display = strings.SplitN(email, "@", 2)[0]
	}
	return &Probe{
		Email: email, PasswordSet: password != "",
		IMAPHost: imapHost, IMAPPort: imapPort,
		KalenderURL: kalender, AufgabenURL: aufgaben, KontakteURL: kontakte,
		DisplayName: display, ThunderbirdProfile: profile,
		Missing: missing, Hint: hint,
		password: password, SMTPHost: smtpHost, SMTPPort: smtpPort,
	}
}

func LoadAccount(calendars, contacts bool) (*Account, error) {
	probe := ProbeAccount()
	missing := append([]string{}, probe.Missing...)
	if calendars {
		if probe.KalenderURL == "" {
			missing = append(missing, "MAILBOX_CALDAV_KALENDER")
		}
		if probe.AufgabenURL == "" {
			missing = append(missing, "MAILBOX_CALDAV_AUFGABEN")
		}
	}
	if contacts && probe.KontakteURL == "" {
		missing = append(missing, "MAILBOX_CARDDAV_KONTAKTE")
	}
	if len(missing) > 0 {
		return nil, &MissingCredsError{Msg: "missing " + strings.Join(missing, ", ") + " (" + probe.Hint + ")"}
	}
	displayName := probe.DisplayName
	if displayName == "" {
		displayName = strings.SplitN(probe.Email, "@", 2)[0]
	}
	return &Account{
		Email: probe.Email, Password: probe.password,
		IMAPHost: probe.IMAPHost, IMAPPort: probe.IMAPPort,
		KalenderURL: probe.KalenderURL, AufgabenURL: probe.AufgabenURL, KontakteURL: probe.KontakteURL,
		DisplayName: displayName, SMTPHost: probe.SMTPHost, SMTPPort: probe.SMTPPort,
	}, nil
}

func (p *Probe) Password() string { return p.password }

var _ = fmt.Sprintf
