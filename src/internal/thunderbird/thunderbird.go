package thunderbird

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type CalDAV struct {
	Name     string
	URL      string
	Username string
	Password string
	Color    string
}

type Account struct {
	Email       string
	DisplayName string
	IMAPHost    string
	IMAPPort    int
	KalenderURL string
	AufgabenURL string
	KontakteURL string
	ExtraCals   []CalDAV
	Password    string
	DAVPassword string
	Profile     string
}

var prefRe = regexp.MustCompile(`^user_pref\("(?P<key>[^"]+)",\s*(?P<val>true|false|-?\d+|"(?P<str>(?:\\.|[^"\\])*)")\);`)

// Prefs preserves file order like python dict iteration.
type Prefs struct {
	Keys []string
	Vals map[string]PrefValue
}

type PrefValue struct {
	Bool   bool
	Int    int
	Str    string
	IsStr  bool
	IsBool bool
	IsInt  bool
}

func (v PrefValue) String() string { return v.Str }

func ParsePrefs(text string) *Prefs {
	p := &Prefs{Vals: map[string]PrefValue{}}
	names := prefRe.SubexpNames()
	for _, line := range strings.Split(text, "\n") {
		m := prefRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		var key, val, str string
		for i, name := range names {
			switch name {
			case "key":
				key = m[i]
			case "val":
				val = m[i]
			case "str":
				str = m[i]
			}
		}
		var pv PrefValue
		switch {
		case val == "true":
			pv.Bool, pv.IsBool = true, true
		case val == "false":
			pv.Bool, pv.IsBool, pv.Int = false, true, 0
		case strings.HasPrefix(val, `"`):
			pv.Str, pv.IsStr = unescape(str), true
		default:
			n := 0
			fmt.Sscanf(val, "%d", &n)
			pv.Int, pv.IsInt = n, true
		}
		if _, seen := p.Vals[key]; !seen {
			p.Keys = append(p.Keys, key)
		}
		p.Vals[key] = pv
	}
	return p
}

func unescape(value string) string {
	r := strings.NewReplacer(`\"`, `"`, `\\`, `\`)
	return r.Replace(value)
}

func getStr(p *Prefs, key string) (string, bool) {
	v, ok := p.Vals[key]
	return v.Str, ok && v.IsStr && v.Str != ""
}

func getInt(p *Prefs, key string) (int, bool) {
	v, ok := p.Vals[key]
	return v.Int, ok && v.IsInt
}

func accountFromPrefs(p *Prefs) *Account {
	acc := &Account{}
	type server struct {
		host, user string
		port       int
	}
	var mailbox, other *server
	for _, key := range p.Keys {
		if !strings.HasPrefix(key, "mail.server.") || !strings.HasSuffix(key, ".type") {
			continue
		}
		if p.Vals[key].Str != "imap" {
			continue
		}
		prefix := strings.TrimSuffix(key, ".type")
		host, ok := getStr(p, prefix+".hostname")
		if !ok {
			continue
		}
		user, _ := getStr(p, prefix+".userName")
		port, _ := getInt(p, prefix+".port")
		s := &server{host: host, user: user, port: port}
		if host == "imap.mailbox.org" {
			mailbox = s
		} else if other == nil {
			other = s
		}
	}
	chosen := mailbox
	if chosen == nil {
		chosen = other
	}
	if chosen != nil {
		acc.IMAPHost = chosen.host
		acc.Email = chosen.user
		acc.IMAPPort = chosen.port
	}

	identities := map[string]map[string]PrefValue{}
	var identOrder []string
	for _, key := range p.Keys {
		if !strings.HasPrefix(key, "mail.identity.") {
			continue
		}
		rest := strings.TrimPrefix(key, "mail.identity.")
		i := strings.Index(rest, ".")
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		id, field := rest[:i], rest[i+1:]
		if _, ok := identities[id]; !ok {
			identities[id] = map[string]PrefValue{}
			identOrder = append(identOrder, id)
		}
		identities[id][field] = p.Vals[key]
	}

	if acc.Email == "" {
		for _, id := range identOrder {
			if v, ok := identities[id]["useremail"]; ok && v.IsStr && v.Str != "" {
				acc.Email = v.Str
				break
			}
		}
	}

	emailL := strings.ToLower(acc.Email)
	fallbackName := ""
	hasFallback := false
	for _, id := range identOrder {
		nameV, ok := identities[id]["fullName"]
		if !ok || !nameV.IsStr {
			continue
		}
		if !hasFallback {
			fallbackName = nameV.Str
			hasFallback = true
		}
		if uv, ok := identities[id]["useremail"]; ok && uv.IsStr && strings.ToLower(uv.Str) == emailL {
			acc.DisplayName = nameV.Str
			break
		}
	}
	if acc.Email != "" && acc.DisplayName == "" {
		if hasFallback {
			acc.DisplayName = fallbackName
		} else {
			acc.DisplayName = ""
		}
	}

	calendars := map[string]map[string]PrefValue{}
	var calOrder []string
	for _, key := range p.Keys {
		if !strings.HasPrefix(key, "calendar.registry.") {
			continue
		}
		rest := strings.TrimPrefix(key, "calendar.registry.")
		i := strings.Index(rest, ".")
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		uid, field := rest[:i], rest[i+1:]
		if _, ok := calendars[uid]; !ok {
			calendars[uid] = map[string]PrefValue{}
			calOrder = append(calOrder, uid)
		}
		calendars[uid][field] = p.Vals[key]
	}
	for _, uid := range calOrder {
		f := calendars[uid]
		disabled := f["disabled"]
		typ := f["type"]
		if disabled.IsBool && disabled.Bool {
			continue
		}
		if typ.IsStr && typ.Str != "caldav" {
			continue
		}
		name := f["name"].Str
		uri := f["uri"].Str
		if uri == "" || !f["uri"].IsStr {
			continue
		}
		if name == "Kalender" {
			acc.KalenderURL = uri
		} else if name == "Aufgaben" {
			acc.AufgabenURL = uri
		} else if name != "" {
			acc.ExtraCals = append(acc.ExtraCals, CalDAV{
				Name:     name,
				URL:      uri,
				Username: f["username"].Str,
				Color:    f["color"].Str,
			})
		}
	}

	books := map[string]map[string]PrefValue{}
	var bookOrder []string
	for _, key := range p.Keys {
		if !strings.HasPrefix(key, "ldap_2.servers.") {
			continue
		}
		rest := strings.TrimPrefix(key, "ldap_2.servers.")
		i := strings.Index(rest, ".")
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		uid, field := rest[:i], rest[i+1:]
		if _, ok := books[uid]; !ok {
			books[uid] = map[string]PrefValue{}
			bookOrder = append(bookOrder, uid)
		}
		books[uid][field] = p.Vals[key]
	}
	chosenURL, fallbackURL := "", ""
	for _, uid := range bookOrder {
		f := books[uid]
		dirType := f["dirType"]
		if !dirType.IsInt || dirType.Int != 102 {
			continue
		}
		uriV, ok := f["carddav.url"]
		if !ok || !uriV.IsStr || uriV.Str == "" {
			continue
		}
		if f["description"].Str == "Kontakte" {
			chosenURL = uriV.Str
			break
		}
		if fallbackURL == "" {
			fallbackURL = uriV.Str
		}
	}
	acc.KontakteURL = chosenURL
	if acc.KontakteURL == "" {
		acc.KontakteURL = fallbackURL
	}
	return acc
}

// tbHomeCandidates lists the Thunderbird homes we know about, most specific
// first. Linux packaging (native, flatpak, snap) keeps profiles directly in the
// home; Windows and macOS keep them under a "Profiles" subdirectory, and under
// WSL the Windows home is reachable through /mnt/c.
func tbHomeCandidates() []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out,
			filepath.Join(home, ".thunderbird"),
			filepath.Join(home, ".var", "app", "org.mozilla.Thunderbird", ".thunderbird"),
			filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
			filepath.Join(home, "Library", "Thunderbird"),
			filepath.Join(home, "AppData", "Roaming", "Thunderbird"),
		)
	}
	// ponytail: single-user glob; enumerate /mnt/c/Users properly if others use this box
	wsl, _ := filepath.Glob("/mnt/c/Users/*/AppData/Roaming/Thunderbird")
	sort.Strings(wsl)
	return append(out, wsl...)
}

func DefaultTBHome() string {
	if env := os.Getenv("MAILBOX_TB_HOME"); env != "" {
		return env
	}
	candidates := tbHomeCandidates()
	for _, c := range candidates {
		if len(profileDirs(c)) > 0 {
			return c
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".thunderbird")
}

// profileRoots is where profile directories live inside a Thunderbird home.
// Both layouts are probed so one binary works across platforms.
func profileRoots(tbHome string) []string {
	return []string{filepath.Join(tbHome, "Profiles"), tbHome}
}

type profileCand struct {
	mtime int64
	path  string
}

// profileDirs returns every directory under tbHome holding a prefs.js, newest first.
func profileDirs(tbHome string) []profileCand {
	var all []profileCand
	seen := map[string]bool{}
	for _, root := range profileRoots(tbHome) {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			if seen[dir] {
				continue
			}
			fi, err := os.Stat(filepath.Join(dir, "prefs.js"))
			if err != nil || fi.IsDir() {
				continue
			}
			seen[dir] = true
			all = append(all, profileCand{mtime: fi.ModTime().UnixNano(), path: dir})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].mtime > all[j].mtime })
	return all
}

func ListProfiles(tbHome string) []string {
	if tbHome == "" {
		tbHome = DefaultTBHome()
	}
	cands := profileDirs(tbHome)
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.path
	}
	return out
}

func FindProfile(tbHome, explicit string) (string, error) {
	if explicit != "" {
		if fi, err := os.Stat(explicit); err == nil && fi.IsDir() {
			if _, err := os.Stat(filepath.Join(explicit, "prefs.js")); err == nil {
				return explicit, nil
			}
		}
		for _, root := range profileRoots(tbHome) {
			named := filepath.Join(root, explicit)
			if fi, err := os.Stat(named); err == nil && fi.IsDir() {
				return named, nil
			}
		}
		var matches []string
		seen := map[string]bool{}
		for _, root := range profileRoots(tbHome) {
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || seen[filepath.Join(root, e.Name())] {
					continue
				}
				if strings.HasSuffix(e.Name(), explicit) || e.Name() == explicit {
					seen[filepath.Join(root, e.Name())] = true
					matches = append(matches, filepath.Join(root, e.Name()))
				}
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		return "", fmt.Errorf("Thunderbird profile not found: %s", explicit)
	}
	if cands := profileDirs(tbHome); len(cands) > 0 {
		return cands[0].path, nil
	}
	return "", fmt.Errorf("no Thunderbird profile with prefs.js under %s", tbHome)
}

func LoadThunderbird(tbHome, profileName string) (*Account, error) {
	if tbHome == "" {
		tbHome = DefaultTBHome()
	}
	profile, err := FindProfile(tbHome, profileName)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(profile, "prefs.js"))
	if err != nil {
		return nil, err
	}
	acc := accountFromPrefs(ParsePrefs(string(raw)))
	acc.Profile = profile
	logins := readLogins(profile)
	acc.Password, acc.DAVPassword = passwordsFromLogins(profile, logins)
	fillExtraCalPasswords(profile, logins, acc.ExtraCals)
	return acc, nil
}

type loginEntry struct {
	Hostname          string `json:"hostname"`
	EncryptedPassword string `json:"encryptedPassword"`
}

func readLogins(profile string) []loginEntry {
	raw, err := os.ReadFile(filepath.Join(profile, "logins.json"))
	if err != nil {
		return nil
	}
	var data struct {
		Logins []loginEntry `json:"logins"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data.Logins
}

func passwordsFromLogins(profile string, logins []loginEntry) (imapPW, davPW string) {
	imapBlob, davBlob := pickLoginBlobs(logins)
	if imapBlob != "" {
		if pw, ok := decryptNSS(profile, imapBlob); ok {
			imapPW = pw
		}
	}
	if davBlob != "" {
		if pw, ok := decryptNSS(profile, davBlob); ok {
			davPW = pw
		}
	}
	return imapPW, davPW
}

func fillExtraCalPasswords(profile string, logins []loginEntry, cals []CalDAV) {
	for i := range cals {
		blob := pickLoginBlobForHost(logins, urlHost(cals[i].URL))
		if blob == "" {
			continue
		}
		if pw, ok := decryptNSS(profile, blob); ok {
			cals[i].Password = pw
		}
	}
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// mailbox.org 2FA issues a separate app password for dav.mailbox.org.
func pickLoginBlobs(logins []loginEntry) (imapBlob, davBlob string) {
	for _, row := range logins {
		switch {
		case strings.Contains(row.Hostname, "imap.mailbox.org"):
			imapBlob = row.EncryptedPassword
		case strings.Contains(row.Hostname, "dav.mailbox.org"):
			davBlob = row.EncryptedPassword
		}
	}
	return imapBlob, davBlob
}

func pickLoginBlobForHost(logins []loginEntry, host string) string {
	if host == "" {
		return ""
	}
	for _, row := range logins {
		if strings.Contains(row.Hostname, host) {
			return row.EncryptedPassword
		}
	}
	return ""
}
