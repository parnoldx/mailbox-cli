package mail

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mailbox/src/internal/folders"
	"mailbox/src/internal/imapclient"
)

// reservedFlags are IMAP system / client keywords, not Labels.
var reservedFlags = map[string]bool{
	"seen": true, "answered": true, "flagged": true, "deleted": true,
	"draft": true, "recent": true, "*": true,
	"$forwarded": true, "$mdnsent": true, "$junk": true, "$notjunk": true,
	"$phishing": true, "$hasattachment": true, "junk": true, "nonjunk": true,
}

var labelsPathFunc = defaultLabelsPath

func defaultLabelsPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mailbox", "labels"), nil
}

// IsLabel reports whether a FLAGS atom is a user Label (not a system flag,
// client color slot, $-prefixed keyword, or an Aside due keyword).
func IsLabel(flag string) bool {
	f := strings.ToLower(flag)
	if f == "" || reservedFlags[f] || strings.HasPrefix(f, "asidedue-") || strings.HasPrefix(f, "$") {
		return false
	}
	return !clientColorSlot(f)
}

func clientColorSlot(f string) bool {
	f = strings.TrimPrefix(f, "$")
	if !strings.HasPrefix(f, "cl_") {
		return false
	}
	rest := f[3:]
	if rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

// LabelsFromFlags returns the Label names in a FLAGS list, in order.
func LabelsFromFlags(flags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range flags {
		f = strings.ToLower(f)
		if !IsLabel(f) || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// NormalizeLabel turns a user-supplied name into an IMAP keyword atom.
func NormalizeLabel(name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '_' || r == '-':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	s = strings.Trim(b.String(), "-")
	if s == "" {
		return "", fmt.Errorf("invalid label %q", name)
	}
	if !IsLabel(s) {
		return "", fmt.Errorf("%q is not a label", name)
	}
	return s, nil
}

// CreateLabel records a Label name. IMAP keywords only exist on a Message;
// the catalog is what `label list` shows for names not yet applied.
func CreateLabel(name string) (string, error) {
	s, err := NormalizeLabel(name)
	if err != nil {
		return "", err
	}
	rememberLabel(s)
	return s, nil
}

// CatalogLabels is the on-disk catalog. ok is false when it has never been written.
func CatalogLabels() ([]string, bool) {
	return loadCatalog()
}

// ListLabels returns catalog names. On first use it seeds from routing boxes
// (not Archive) so client keywords and the Archive tree are not scanned.
// ponytail: one local file, IMAP seed once; rescan if other-client labels must show.
func (m *Mail) ListLabels() ([]string, error) {
	if names, ok := loadCatalog(); ok {
		return names, nil
	}
	return m.seedLabels()
}

func (m *Mail) seedLabels() ([]string, error) {
	seen := map[string]bool{}
	var out []string
	okFolders := 0
	for _, folder := range folders.RoutingFolders {
		names, err := m.folderLabels(folder)
		if err != nil {
			continue
		}
		okFolders++
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	if okFolders == 0 {
		return nil, fmt.Errorf("cannot list labels")
	}
	sort.Strings(out)
	saveCatalog(out)
	return out, nil
}

func (m *Mail) folderLabels(folder string) ([]string, error) {
	c, err := m.client()
	if err != nil {
		return nil, err
	}
	resp, err := c.Command("EXAMINE", imapclient.QuoteString(folder))
	if err != nil {
		return nil, err
	}
	if resp.Status != "OK" {
		return nil, fmt.Errorf("cannot select %s", folder)
	}
	for _, line := range resp.Lines {
		if strings.HasPrefix(line, "FLAGS ") {
			return LabelsFromFlags(parseFlags(line)), nil
		}
	}
	return nil, nil
}

// SetLabel adds or removes one Label on a Message.
func (m *Mail) SetLabel(folder, uid, label string, on bool) error {
	name, err := NormalizeLabel(label)
	if err != nil {
		return err
	}
	if err := m.setKeyword(folder, uid, name, on); err != nil {
		return err
	}
	if on {
		rememberLabel(name)
	}
	return nil
}

// ClearLabels removes every Label on a Message. System flags and Aside due
// keywords stay.
func (m *Mail) ClearLabels(folder, uid string) error {
	if err := m.Select(folder, true); err != nil {
		return err
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", uid, "(FLAGS)")
	if err != nil || resp.Status != "OK" {
		return fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	var flags []string
	for _, rec := range splitFetchChunks(resp.Chunks) {
		flags = parseFlags(rec.meta)
		break
	}
	for _, name := range LabelsFromFlags(flags) {
		if err := m.setKeyword(folder, uid, name, false); err != nil {
			return err
		}
	}
	return nil
}

func loadCatalog() ([]string, bool) {
	path, err := labelsPathFunc()
	if err != nil {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if !IsLabel(line) || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	sort.Strings(out)
	return out, true
}

func rememberLabel(name string) {
	names, _ := loadCatalog()
	for _, n := range names {
		if n == name {
			saveCatalog(names)
			return
		}
	}
	names = append(names, name)
	sort.Strings(names)
	saveCatalog(names)
}

func saveCatalog(names []string) {
	path, err := labelsPathFunc()
	if err != nil {
		return
	}
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		os.WriteFile(path, []byte(b.String()), 0o600)
	}
}
