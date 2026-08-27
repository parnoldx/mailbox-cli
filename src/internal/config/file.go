package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func ConfigPath() string {
	if p := os.Getenv("MAILBOX_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mailbox", "env")
}

func ReadFile() map[string]string {
	out := map[string]string{}
	path := ConfigPath()
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func WriteFile(vals map[string]string) error {
	path := ConfigPath()
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	order := []string{
		"MAILBOX_TB_PROFILE",
		"MAILBOX_EMAIL",
		"MAILBOX_PASSWORD",
		"MAILBOX_DAV_PASSWORD",
		"MAILBOX_CALDAV_KALENDER",
		"MAILBOX_CALDAV_AUFGABEN",
		"MAILBOX_CARDDAV_KONTAKTE",
	}
	seen := map[string]bool{}
	for _, k := range order {
		if v := vals[k]; v != "" {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(strings.ReplaceAll(v, "\n", ""))
			b.WriteByte('\n')
			seen[k] = true
		}
	}
	for k, v := range vals {
		if seen[k] || v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strings.ReplaceAll(v, "\n", ""))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
