package folders

import (
	"fmt"
	"strings"
)

const (
	INBOX       = "INBOX"
	FEED        = "INBOX/Feed"
	PAPER_TRAIL = "INBOX/Paper Trail"
	SCREENER    = "INBOX/Screener"
	BLOCK       = "INBOX/Screener/Block"
	ASIDE       = "INBOX/Aside"
	ARCHIVE     = "Archive"
	DRAFTS      = "Drafts"
	SENT        = "Sent"
	TRASH       = "Trash"
	JUNK        = "Junk"
)

var ScreenTargets = map[string]string{
	"inbox":       INBOX,
	"feed":        FEED,
	"paper-trail": PAPER_TRAIL,
	"block":       BLOCK,
}

var FolderAliases = []struct{ Alias, IMAP string }{
	{"inbox", INBOX},
	{"feed", FEED},
	{"trail", PAPER_TRAIL},
	{"paper-trail", PAPER_TRAIL},
	{"screener", SCREENER},
	{"block", BLOCK},
	{"archive", ARCHIVE},
	{"aside", ASIDE},
	{"drafts", DRAFTS},
	{"sent", SENT},
}

var folderAliasesMap = func() map[string]string {
	m := map[string]string{}
	for _, p := range FolderAliases {
		m[p.Alias] = p.IMAP
	}
	return m
}()

var FolderRoles = map[string]string{
	"inbox":      "accepted senders",
	"feed":       "skim",
	"trail": "receipts",
	"screener":   "sender unknown",
	"block":      "blacklist",
	"aside":      "read-later pile",
	"archive":    "topic filing",
	"drafts":     "unsent",
	"sent":       "sent copies",
}

var RoutingFolders = []string{INBOX, FEED, PAPER_TRAIL, SCREENER}
var SearchRoots = []string{INBOX, FEED, PAPER_TRAIL, SCREENER, ARCHIVE}

// KNOWN_IMAP order in python: alias values then screen target values, deduped.
var KnownIMAP = func() []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range FolderAliases {
		if !seen[p.IMAP] {
			out = append(out, p.IMAP)
			seen[p.IMAP] = true
		}
	}
	for _, v := range []string{ScreenTargets["inbox"], ScreenTargets["feed"], ScreenTargets["paper-trail"], ScreenTargets["block"]} {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}()

func Norm(name string) string {
	key := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
	if strings.HasPrefix(key, "the ") {
		key = key[4:]
	}
	return strings.ReplaceAll(key, " ", "-")
}

func IsArchive(imap string) bool {
	return imap == ARCHIVE || strings.HasPrefix(imap, ARCHIVE+"/")
}

func catalogNames(imapNames []string) []string {
	names := append([]string{}, KnownIMAP...)
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	for _, name := range imapNames {
		if seen[name] || !IsArchive(name) {
			continue
		}
		names = append(names, name)
		seen[name] = true
	}
	return names
}

type UsageError struct{ Msg string }

func (e *UsageError) Error() string     { return e.Msg }
func (e *UsageError) ErrorCode() string { return "usage" }

func usageErr(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

func ResolveFolder(name string, imapNames []string) (string, error) {
	key := strings.TrimSpace(name)
	if key == "" {
		return "", usageErr("folder is empty")
	}
	if aliased, ok := folderAliasesMap[strings.ToLower(key)]; ok {
		return aliased, nil
	}
	if aliased, ok := folderAliasesMap[Norm(key)]; ok {
		return aliased, nil
	}
	catalog := catalogNames(imapNames)
	lowered := strings.ToLower(key)
	normed := Norm(key)
	var pathHits []string
	for _, imap := range catalog {
		if strings.ToLower(imap) == lowered || Norm(imap) == normed {
			pathHits = append(pathHits, imap)
		}
	}
	if len(pathHits) == 1 {
		return pathHits[0], nil
	}
	if len(pathHits) > 1 {
		for _, imap := range pathHits {
			if imap == key {
				return imap, nil
			}
		}
		return pathHits[0], nil
	}
	var segHits []string
	for _, imap := range catalog {
		seg := imap
		if i := strings.LastIndex(imap, "/"); i >= 0 {
			seg = imap[i+1:]
		}
		if Norm(seg) == normed {
			segHits = append(segHits, imap)
		}
	}
	if len(segHits) == 1 {
		return segHits[0], nil
	}
	if len(segHits) > 1 {
		return "", usageErr("ambiguous box %q; matches %s", name, strings.Join(segHits, ", "))
	}
	if key == ARCHIVE || strings.HasPrefix(key, ARCHIVE+"/") {
		return key, nil
	}
	allowed := make([]string, len(FolderAliases))
	for i, p := range FolderAliases {
		allowed[i] = p.Alias
	}
	return "", usageErr("unknown box %q; use %s or Archive/…", name, strings.Join(allowed, ", "))
}

type FolderRow struct {
	ID   string
	IMAP string
	Role string
}

func FolderCatalog(imapNames []string, archive bool) []FolderRow {
	if !archive {
		var rows []FolderRow
		seen := map[string]bool{}
		for _, p := range FolderAliases {
			if IsArchive(p.IMAP) || seen[p.IMAP] {
				continue
			}
			rows = append(rows, FolderRow{ID: p.Alias, IMAP: p.IMAP, Role: FolderRoles[p.Alias]})
			seen[p.IMAP] = true
		}
		return rows
	}
	var rows []FolderRow
	seen := map[string]bool{}
	for _, p := range FolderAliases {
		if !IsArchive(p.IMAP) {
			continue
		}
		rows = append(rows, FolderRow{ID: p.Alias, IMAP: p.IMAP, Role: FolderRoles[p.Alias]})
		seen[p.IMAP] = true
	}
	for _, name := range imapNames {
		if seen[name] || !IsArchive(name) {
			continue
		}
		rows = append(rows, FolderRow{ID: name, IMAP: name, Role: FolderRoles["archive"]})
		seen[name] = true
	}
	return rows
}

func ResolveScreenTarget(name string) (string, error) {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return "", usageErr("folder is empty")
	}
	for _, v := range ScreenTargets {
		if raw == v {
			return raw, nil
		}
	}
	key := Norm(raw)
	dest, ok := ScreenTargets[key]
	if !ok {
		allowed := make([]string, 0, len(ScreenTargets))
		for k := range ScreenTargets {
			allowed = append(allowed, k)
		}
		sortStrings(allowed)
		return "", usageErr("unknown screen target %q; use %s", name, strings.Join(allowed, ", "))
	}
	return dest, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
