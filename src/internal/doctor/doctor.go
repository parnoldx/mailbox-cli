// Package doctor ports src/mailbox_cli/doctor.py.
package doctor

import (
	"fmt"
	"strings"

	"mailbox/src/internal/config"
	"mailbox/src/internal/format"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/skill"
)

type Check struct {
	Name   string
	OK     bool
	Detail string
}

type Report struct {
	Checks []Check
}

func (r *Report) OK() bool {
	required := map[string]bool{"credentials": true, "imap": true}
	for _, c := range r.Checks {
		if required[c.Name] && !c.OK {
			return false
		}
	}
	return true
}

func (r *Report) AsDict() *format.OM {
	checks := make([]*format.OM, 0, len(r.Checks))
	for _, c := range r.Checks {
		checks = append(checks, format.NewOM("name", c.Name, "ok", c.OK, "detail", c.Detail))
	}
	return format.NewOM("ok", r.OK(), "checks", checks)
}

func Run(probe *config.Probe, imapOK *bool) *Report {
	if probe == nil {
		probe = config.ProbeAccount()
	}
	return &Report{Checks: []Check{
		credentials(probe),
		imap(probe, imapOK),
		caldav(probe),
		carddav(probe),
		skillCheck(),
	}}
}

func credentials(probe *config.Probe) Check {
	if len(probe.Missing) > 0 {
		detail := "missing " + strings.Join(probe.Missing, ", ")
		if probe.ThunderbirdProfile != "" {
			detail += fmt.Sprintf(" (profile %s)", probe.ThunderbirdProfile)
		} else {
			detail += fmt.Sprintf(" (%s)", probe.Hint)
		}
		return Check{"credentials", false, detail}
	}
	where := "env"
	if probe.ThunderbirdProfile != "" {
		where = probe.ThunderbirdProfile
	}
	return Check{"credentials", true, fmt.Sprintf("%s via %s", probe.Email, where)}
}

func imap(probe *config.Probe, imapOK *bool) Check {
	if len(probe.Missing) > 0 {
		return Check{"imap", false, "skipped; credentials missing"}
	}
	target := fmt.Sprintf("%s:%d", probe.IMAPHost, probe.IMAPPort)
	if imapOK != nil {
		if *imapOK {
			return Check{"imap", true, target}
		}
		return Check{"imap", false, target + " login failed"}
	}
	acct := &config.Account{
		Email: probe.Email, Password: probe.Password(),
		IMAPHost: probe.IMAPHost, IMAPPort: probe.IMAPPort,
	}
	mailClient := mail.New(acct)
	defer mailClient.Close()
	if _, err := mailClient.ListFolders(); err != nil {
		return Check{"imap", false, fmt.Sprintf("%s: %v", target, err)}
	}
	return Check{"imap", true, target}
}

func caldav(probe *config.Probe) Check {
	var missing []string
	if probe.KalenderURL == "" {
		missing = append(missing, "Kalender")
	}
	if probe.AufgabenURL == "" {
		missing = append(missing, "Aufgaben")
	}
	if len(missing) > 0 {
		return Check{"caldav", false, "missing " + strings.Join(missing, ", ")}
	}
	return Check{"caldav", true, "Kalender and Aufgaben URLs set"}
}

func carddav(probe *config.Probe) Check {
	if probe.KontakteURL == "" {
		return Check{"carddav", false, "missing Kontakte"}
	}
	return Check{"carddav", true, "Kontakte URL set"}
}

func skillCheck() Check {
	copies := skill.InstalledCopies("")
	if len(copies) == 0 {
		return Check{"skill", false, "no install location"}
	}
	var installed []skill.CopyRow
	for _, c := range copies {
		if c.Installed {
			installed = append(installed, c)
		}
	}
	if len(installed) == 0 {
		paths := make([]string, len(copies))
		for i, c := range copies {
			paths[i] = c.Path
		}
		return Check{"skill", false, "not installed; run mailbox skill install (" + strings.Join(paths, ", ") + ")"}
	}
	var unmanaged []string
	for _, c := range installed {
		if !c.Managed {
			unmanaged = append(unmanaged, c.Path)
		}
	}
	if len(unmanaged) > 0 {
		return Check{"skill", false, "unmanaged copy at " + strings.Join(unmanaged, ", ")}
	}
	var stale []string
	for _, c := range installed {
		if !c.Current {
			stale = append(stale, c.Path)
		}
	}
	if len(stale) > 0 {
		return Check{"skill", false, "stale copy at " + strings.Join(stale, ", ") + "; run mailbox skill install"}
	}
	paths := make([]string, len(installed))
	for i, c := range installed {
		paths[i] = c.Path
	}
	return Check{"skill", true, strings.Join(paths, ", ")}
}
