package config

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
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
		out[k] = unquote(strings.TrimSpace(v))
	}
	return out
}

// unquote drops one layer of matching quotes. The file looks like a shell env
// file, so people write MAILBOX_PASSWORD="secret" and would otherwise end up
// with the quotes inside their password.
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	first, last := v[0], v[len(v)-1]
	if first == last && (first == '"' || first == '\'') {
		return v[1 : len(v)-1]
	}
	return v
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
	write := func(k, v string) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strings.ReplaceAll(v, "\n", ""))
		b.WriteByte('\n')
	}
	for _, k := range order {
		if v := vals[k]; v != "" {
			write(k, v)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(vals))
	for k, v := range vals {
		if seen[k] || v == "" {
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		write(k, vals[k])
	}
	return writeAtomic(path, []byte(b.String()))
}

// writeAtomic writes via a temp file in the same directory and renames, so an
// interrupted write cannot leave the credentials file truncated.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
