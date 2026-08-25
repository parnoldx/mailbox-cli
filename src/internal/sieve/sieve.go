// Package sieve ports the emailMoveHelper service (~/dev/emailMoveHelper):
// watches routing folders for arrivals and keeps the "logic" ManageSieve
// script's address lists in sync. Pure logic here; IO in server.go/watch.go.
package sieve

import (
	"fmt"
	"regexp"
	"strings"
)

// Routing folders the service owns (mirrors internal/folders).
const (
	FolderInbox      = "INBOX"
	FolderFeed       = "INBOX/Feed"
	FolderPaperTrail = "INBOX/Paper Trail"
	FolderScreener   = "INBOX/Screener"
	FolderBlock      = "INBOX/Screener/Block"
)

// ScriptName is the ManageSieve script this service owns.
const ScriptName = "logic"

var RuleFolders = []string{FolderInbox, FolderFeed, FolderPaperTrail, FolderScreener, FolderBlock}

// Lists are the four sender lists encoded in the logic script.
type Lists struct {
	Blacklist  []string
	Whitelist  []string
	PaperTrail []string
	Feed       []string
}

func NewLists() *Lists {
	return &Lists{Blacklist: []string{}, Whitelist: []string{}, PaperTrail: []string{}, Feed: []string{}}
}

func (l *Lists) Clone() *Lists {
	return &Lists{
		Blacklist:  append([]string{}, l.Blacklist...),
		Whitelist:  append([]string{}, l.Whitelist...),
		PaperTrail: append([]string{}, l.PaperTrail...),
		Feed:       append([]string{}, l.Feed...),
	}
}

func (l *Lists) Equal(other *Lists) bool {
	return sameStrings(l.Blacklist, other.Blacklist) &&
		sameStrings(l.Whitelist, other.Whitelist) &&
		sameStrings(l.PaperTrail, other.PaperTrail) &&
		sameStrings(l.Feed, other.Feed)
}

// Movement is one folder arrival that may change the lists.
type Movement struct {
	Folder  string
	Address string
}

// Apply folds a movement into the lists; reports whether anything changed.
func Apply(lists *Lists, mv Movement) bool {
	if mv.Address == "" {
		return false
	}
	var list *[]string
	switch mv.Folder {
	case FolderInbox:
		list = &lists.Whitelist
	case FolderFeed:
		list = &lists.Feed
	case FolderPaperTrail:
		list = &lists.PaperTrail
	case FolderBlock:
		list = &lists.Blacklist
	default:
		return false
	}
	if contains(*list, mv.Address) {
		return false
	}
	*list = append(*list, mv.Address)
	return true
}

var addrListRe = regexp.MustCompile(`if header :contains "From" \[(.*?)\]`)

// ParseScript extracts the four lists from a logic script body.
// The generator writes ["example@example.com"] for empty lists; that
// placeholder is filtered out so it never leaks into the live lists.
func ParseScript(content string) (*Lists, error) {
	lists := NewLists()
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := addrListRe.FindStringSubmatch(line)
		if len(m) <= 1 {
			continue
		}
		addrs := parseAddressString(m[1])
		ctx := contextWindow(lines, i)
		switch {
		case strings.Contains(ctx, "discard"):
			lists.Blacklist = addrs
		case strings.Contains(ctx, `fileinto "INBOX/Paper Trail"`):
			lists.PaperTrail = addrs
		case strings.Contains(ctx, `fileinto "INBOX/Feed"`):
			lists.Feed = addrs
		case strings.Contains(ctx, `fileinto "INBOX";`) && strings.Contains(ctx, "stop"):
			lists.Whitelist = addrs
		}
	}
	return lists, nil
}

// contextWindow returns the block following the if-header line, enough to see
// its fileinto/discard action.
func contextWindow(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	end := i + 6
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[i:end], " ")
}

func parseAddressString(raw string) []string {
	raw = strings.ReplaceAll(raw, `"`, "")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var out []string
	for _, addr := range strings.Split(raw, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" || addr == "example@example.com" {
			continue
		}
		if !contains(out, addr) {
			out = append(out, addr)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// GenerateScript renders the logic script from the lists, byte-compatible
// with emailMoveHelper's output.
func GenerateScript(lists *Lists) string {
	format := func(addresses []string) string {
		if len(addresses) == 0 {
			return `["example@example.com"]`
		}
		quoted := make([]string, len(addresses))
		for i, addr := range addresses {
			quoted[i] = fmt.Sprintf(`"%s"`, addr)
		}
		return "[" + strings.Join(quoted, ",") + "]"
	}

	return fmt.Sprintf(`require ["mailbox","fileinto", "imap4flags"];

# Step 1: Check blacklist - if blacklisted, discard
if header :contains "From" %s
{
  discard;
  stop;
}

# Step 2: Check whitelist - if whitelisted, move to Inbox
if header :contains "From" %s
{
  fileinto "INBOX";
  stop;
}

# Step 3: Move to Paper Trail
if header :contains "From" %s
{
  addflag "\\seen" ;
  fileinto "INBOX/Paper Trail";
  stop;
}

# Step 4: Move to Feed
if header :contains "From" %s
{
  addflag "\\seen" ;
  fileinto "INBOX/Feed";
  stop;
}

# Step 5: All other move to Screener for review of the user
#addflag "\\seen" ;
fileinto "INBOX/Screener";`,
		format(lists.Blacklist),
		format(lists.Whitelist),
		format(lists.PaperTrail),
		format(lists.Feed))
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
