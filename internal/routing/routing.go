// Package routing is the Sieve script that decides where new mail lands on the
// Primary Account, and the four sender lists inside it. Text in, text out: the
// connection that fetches and stores the script is in sievedrv, and the
// decision to change it is in the daemon.
//
// The script is the record. There is no local list of senders that the server
// is brought into line with — there is one script on the server, and what the
// Mirror holds beside it is a projection of what that script says (the same
// rule as ADR-0010).
package routing

import (
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
)

// ScriptName is the Sieve script this program owns. It is a name and not "the
// active script" on purpose: an account may hold others, and rewriting one we
// did not write is not something a routing decision should do.
const ScriptName = "logic"

// The Boxes on the Primary Account. Feed, Paper Trail and Block are where the
// Routing files mail; Aside and Reply Later are piles mail is put in one at a
// time and never routed into, because "read this later" and "I owe a reply" are
// decisions about a mail rather than about the sender it came from.
const (
	BoxInbox      = "INBOX"
	BoxFeed       = "INBOX/Feed"
	BoxPaperTrail = "INBOX/Paper Trail"
	BoxScreener   = "INBOX/Screener"
	BoxAside      = "INBOX/Aside"
	BoxReplyLater = "INBOX/Reply Later"
	BoxBlock      = "INBOX/Screener/Block"
)

// Destination is what was decided about a sender. There are four, and the
// Screener is not one of them: mail reaches the Screener precisely because its
// sender has no Destination yet.
type Destination string

const (
	Inbox      Destination = "inbox"
	Feed       Destination = "feed"
	PaperTrail Destination = "paper"
	Block      Destination = "block"
	// None is the absence of a decision. Routing a sender there takes them off
	// every list, which puts their next mail back in the Screener.
	None Destination = "screener"
)

// spec is what one Destination means: what the script does with new mail, and
// where mail already in the Screener goes when the decision is made.
type spec struct {
	dest Destination
	// files is the Box new mail is filed into. Empty means discarded, which is
	// what blocking a sender does.
	files string
	// lands is where mail already sitting in the Screener is moved to. It is
	// the same Box as files, except for Block: mail is discarded before it
	// arrives, but mail that is already here goes to a pile that can be looked
	// at, because a mistaken block should be recoverable for as long as the
	// evidence exists.
	lands string
	// seen marks the mail read on arrival. Paper Trail and Feed are piles to
	// skim, not things to be told about.
	seen    bool
	aliases []string
	// title is the comment the script carries above the list.
	title string
}

// order is the order the rules are written in, and therefore the order they
// match in: the first rule that matches wins and the rest are dead text. Block
// is first because a blocked sender is blocked whatever else was decided.
var order = []spec{
	{dest: Block, files: "", lands: BoxBlock, aliases: []string{"block", "blocked", "blacklist"},
		title: "Blocked senders. Their mail is discarded, which is what blocking means."},
	{dest: Inbox, files: BoxInbox, lands: BoxInbox, aliases: []string{"inbox", "in"},
		title: "The Inbox: senders worth an interruption."},
	{dest: PaperTrail, files: BoxPaperTrail, lands: BoxPaperTrail, seen: true,
		aliases: []string{"paper", "papertrail", "paper trail", "trail"},
		title:   "Paper Trail: receipts, confirmations, records. Read on arrival."},
	{dest: Feed, files: BoxFeed, lands: BoxFeed, seen: true,
		aliases: []string{"feed", "feeds"},
		title:   "Feed: mail to skim, not to correspond with. Read on arrival."},
}

func specOf(d Destination) (spec, bool) {
	for _, s := range order {
		if s.dest == d {
			return s, true
		}
	}
	return spec{}, false
}

// Box is the Box the script files a sender's new mail into. It is empty for
// Block, whose mail is discarded, and for None, which files nothing because it
// is not a rule.
func (d Destination) Box() string {
	s, ok := specOf(d)
	if !ok {
		return ""
	}
	return s.files
}

// Pile is where mail already in the Screener goes when this decision is made.
// Empty for None: forgetting a sender leaves their mail where it is.
func (d Destination) Pile() string {
	s, ok := specOf(d)
	if !ok {
		return ""
	}
	return s.lands
}

// ParseDestination reads what a caller typed. Aside and Reply Later are refused
// by name rather than by falling through to "unknown": they are real Boxes on
// this account and the caller who typed one wants a real thing that is not a
// routing decision.
func ParseDestination(word string) (Destination, error) {
	w := strings.ToLower(strings.TrimSpace(word))
	switch w {
	case "":
		return "", fmt.Errorf("no destination: --to inbox, feed, paper, block or screener")
	case "screener", "none", "forget", "undecided":
		return None, nil
	case "aside":
		return "", fmt.Errorf("aside is a pile, not a route: put one mail there with `mailbox aside ID`")
	case "reply later", "reply-later", "replylater":
		return "", fmt.Errorf("reply later is a pile, not a route: put one mail there with `mailbox reply-later add ID`")
	case "bubble":
		return "", fmt.Errorf("bubble is a timed pile, not a route: `mailbox bubble ID --tomorrow`")
	}
	for _, s := range order {
		for _, a := range s.aliases {
			if w == a {
				return s.dest, nil
			}
		}
	}
	return "", fmt.Errorf("no destination called %q: --to inbox, feed, paper, block or screener", word)
}

// Lists is the Routing: which sender goes where. One address has at most one
// Destination, because the script cannot express two — the first matching rule
// wins and the second is text nothing reaches.
type Lists struct {
	by map[string]Destination
}

// New returns an empty Routing: every sender undecided, all mail in the
// Screener.
func New() *Lists { return &Lists{by: map[string]Destination{}} }

// Of reports what was decided about a sender, None when nothing was.
// An exact address always wins over a domain key (`@example.com`).
func (l *Lists) Of(address string) Destination {
	a := normalise(address)
	if d, ok := l.by[a]; ok {
		return d
	}
	if IsDomain(a) {
		return None
	}
	if dom := DomainOf(a); dom != "" {
		if d, ok := l.by["@"+dom]; ok {
			return d
		}
	}
	return None
}

// Set records a decision and reports whether it changed anything. Setting None
// takes the sender off every list. An address or domain the script could not
// carry safely is refused rather than escaped (see Valid, ValidDomain).
func (l *Lists) Set(address string, d Destination) (bool, error) {
	a := normalise(address)
	if IsDomain(a) {
		if !ValidDomain(a) {
			return false, fmt.Errorf("%q is not a domain this script can carry", address)
		}
	} else if !Valid(a) {
		return false, fmt.Errorf("%q is not an address this script can carry", address)
	}
	if d == None {
		if _, had := l.by[a]; !had {
			return false, nil
		}
		delete(l.by, a)
		return true, nil
	}
	if _, ok := specOf(d); !ok {
		return false, fmt.Errorf("no destination called %q", d)
	}
	if l.by[a] == d {
		return false, nil
	}
	l.by[a] = d
	return true, nil
}

// Addresses returns the senders on one list, sorted.
func (l *Lists) Addresses(d Destination) []string {
	var out []string
	for a, got := range l.by {
		if got == d {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// All returns every decision, sorted by Destination in the order the script
// matches them and then by address.
func (l *Lists) All() []Route {
	var out []Route
	for _, s := range order {
		for _, a := range l.Addresses(s.dest) {
			out = append(out, Route{Address: a, To: s.dest, Box: s.files})
		}
	}
	return out
}

// Route is one decision: this sender's mail goes there. Address is either an
// email address or a domain key (`@example.com`).
type Route struct {
	Address string      `json:"address"`
	To      Destination `json:"to"`
	// Box is the Box the script files this sender's mail into, empty for a
	// blocked sender whose mail is discarded.
	Box string `json:"box"`
}

// IsDomain reports a routing key that names every address at a domain:
// `@example.com`.
func IsDomain(key string) bool {
	k := normalise(key)
	return strings.HasPrefix(k, "@") && !strings.Contains(k[1:], "@") && len(k) > 1
}

// DomainOf is the domain of an address or of a domain key, empty when neither.
func DomainOf(address string) string {
	a := normalise(address)
	if IsDomain(a) {
		return a[1:]
	}
	if i := strings.LastIndex(a, "@"); i >= 0 && i+1 < len(a) {
		return a[i+1:]
	}
	return ""
}

// Count is how many senders have been decided about.
func (l *Lists) Count() int { return len(l.by) }

// normalise is what an address is stored and compared as. Sieve's default
// comparator for :is is case-insensitive, so storing anything else would let
// two spellings of one sender be two entries that behave as one.
func normalise(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// addressRe is the shape of an address that can be embedded in a Sieve quoted
// string. Senders control the From header, so an address carrying a quote, a
// backslash, a bracket or a newline could write rules into the generated
// script. Those are refused outright rather than escaped: escaping is a thing
// to get subtly wrong, and no real address needs it.
var addressRe = regexp.MustCompile(`^[^\s"\\\[\]<>,;]+@[^\s"\\\[\]<>,;]+$`)

// Valid reports whether an address can be stored in a list.
func Valid(address string) bool {
	return len(address) <= 320 && addressRe.MatchString(address)
}

// domainRe is the shape of a domain that can be embedded in a Sieve quoted
// string. Same refusal as Valid: a quote or a newline in a domain is attacker
// input, and no real domain needs it. At least one dot, so `@com` is not a
// domain somebody meant.
var domainRe = regexp.MustCompile(`^[^\s"\\\[\]<>,;@]+(\.[^\s"\\\[\]<>,;@]+)+$`)

// ValidDomain reports whether `@example.com` can be stored as a domain key.
func ValidDomain(key string) bool {
	k := normalise(key)
	if !IsDomain(k) || len(k) > 320 {
		return false
	}
	return domainRe.MatchString(k[1:])
}

// AddressOf pulls the address out of a From header. The Mirror stores the
// header as it was written — `Bob <bob@example.com>`, or several of those — and
// a decision is about the address, never about the name in front of it.
func AddressOf(header string) string {
	h := strings.TrimSpace(header)
	if h == "" {
		return ""
	}
	if list, err := mail.ParseAddressList(h); err == nil && len(list) > 0 {
		return normalise(list[0].Address)
	}
	// An unquoted display name with a comma in it is not a header net/mail will
	// parse, and it is common enough that giving up on it would mean giving up
	// on the sender. The address is what sits in the angle brackets.
	if i := strings.LastIndex(h, "<"); i >= 0 {
		if j := strings.Index(h[i:], ">"); j > 0 {
			return normalise(h[i+1 : i+j])
		}
	}
	if fields := strings.Fields(h); len(fields) == 1 {
		return normalise(strings.Trim(fields[0], "<>"))
	}
	return ""
}

// includeRe matches `include "logic";`, with the optional `:personal` and
// `:global` flags RFC 6609 allows in front of the name.
var includeRe = regexp.MustCompile(`(?is)\binclude\s+(?::\w+\s+)*"([^"]*)"`)

// Includes reports whether a script pulls another one in. A script is in force
// when it is the active one *or* when the active one includes it, which is not
// a corner case: mailbox.org's webmail filter editor owns a script of its own
// and ends it with `include "logic";`, so the Routing runs without ever being
// the active script itself.
func Includes(script, name string) bool {
	for _, m := range includeRe.FindAllStringSubmatch(script, -1) {
		if strings.EqualFold(strings.TrimSpace(m[1]), name) {
			return true
		}
	}
	return false
}

// NameOf pulls the display name out of a From header, empty when the sender
// wrote none. It is worth showing beside a decision that is about to be made
// and worth nothing else: the name is whatever the sender typed.
func NameOf(header string) string {
	list, err := mail.ParseAddressList(strings.TrimSpace(header))
	if err != nil || len(list) == 0 {
		return ""
	}
	return strings.TrimSpace(list[0].Name)
}

// Script renders the Routing as the Sieve script that enforces it.
//
// A list with nobody on it is written as no rule at all rather than as a
// placeholder address: Sieve has no empty string list, and inventing an address
// to fill one is how `example@example.com` ends up looking like a decision
// somebody made.
func (l *Lists) Script() string {
	var b strings.Builder
	b.WriteString(scriptHeader)
	// Address rules first, then domain rules. Sieve's first match wins, so
	// this is the two-pass: a specific address always beats `@domain`.
	for _, s := range order {
		writeRule(&b, s, l.keys(s.dest, false), false)
	}
	for _, s := range order {
		writeRule(&b, s, l.keys(s.dest, true), true)
	}
	fmt.Fprintf(&b, "\n# Everyone else: a sender nothing has been decided about.\nfileinto %q;\n", BoxScreener)
	return b.String()
}

func (l *Lists) keys(d Destination, domains bool) []string {
	var out []string
	for a, got := range l.by {
		if got == d && IsDomain(a) == domains {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

func writeRule(b *strings.Builder, s spec, keys []string, domain bool) {
	if len(keys) == 0 {
		return
	}
	title := s.title
	test := `address :is :all "from"`
	if domain {
		title = strings.TrimSuffix(s.title, ".") + ", by domain."
		test = `address :domain :is "from"`
	}
	fmt.Fprintf(b, "\n# %s\nif %s [\n", title, test)
	for i, k := range keys {
		val := k
		if domain {
			val = strings.TrimPrefix(k, "@")
		}
		comma := ","
		if i == len(keys)-1 {
			comma = ""
		}
		fmt.Fprintf(b, "  %q%s\n", val, comma)
	}
	b.WriteString("] {\n")
	if s.seen {
		b.WriteString("  addflag \"\\\\Seen\";\n")
	}
	if s.files == "" {
		b.WriteString("  discard;\n")
	} else {
		fmt.Fprintf(b, "  fileinto %q;\n", s.files)
	}
	b.WriteString("  stop;\n}\n")
}

const scriptHeader = `# Generated by ` + "`mailbox route`" + `. This whole script is rewritten on every
# decision, so anything added here by hand is lost. Other Sieve scripts on this
# account are never read and never written.
require ["fileinto", "imap4flags"];
`

// listRe finds a rule's address list. Both spellings are read: the one this
// program writes, and the ` + "`header :contains \"From\"`" + ` one the script on the account
// was written with before this program owned it.
var listRe = regexp.MustCompile(`(?is)((?:header\s+:contains\s+"from"|address\s+[^"\[]*"from"))\s*\[([^\]]*)\]`)

// fileintoRe finds the Box a rule files into.
var fileintoRe = regexp.MustCompile(`(?i)fileinto\s+"([^"]*)"`)

// Parse reads the lists back out of a script. It is deliberately forgiving: the
// script it has to read first is the one that is already on the account, and a
// rule it cannot make sense of is skipped rather than failing the whole read —
// a Routing that cannot be read is a Routing that cannot be changed.
//
// A rule is identified by what it does, not by where it sits: the addresses in
// front of a `discard` are blocked and the addresses in front of a
// `fileinto "INBOX/Feed"` are the Feed, wherever in the file they are.
func Parse(script string) *Lists {
	l := New()
	for _, m := range listRe.FindAllStringSubmatchIndex(script, -1) {
		test := script[m[2]:m[3]]
		domain := strings.Contains(strings.ToLower(test), ":domain")
		addrs := parseKeys(script[m[4]:m[5]], domain)
		if len(addrs) == 0 {
			continue
		}
		d, ok := actionAfter(script[m[1]:])
		if !ok {
			continue
		}
		for _, a := range addrs {
			// First rule wins, which is what the script itself does: a sender
			// listed twice is matched by the earlier rule and never the later
			// one, so reading it the other way would report a Routing the
			// server does not perform.
			if _, taken := l.by[a]; taken {
				continue
			}
			if _, err := l.Set(a, d); err != nil {
				continue
			}
		}
	}
	return l
}

// actionAfter reads the block following a rule's address list and says which
// list it is. The window stops at the end of the block, so that a rule with no
// action of its own cannot borrow the next rule's — including the comment above
// it, which is prose and says whatever it likes.
func actionAfter(rest string) (Destination, bool) {
	for _, end := range []string{"\n}", "\nif "} {
		if i := strings.Index(rest, end); i >= 0 {
			rest = rest[:i]
		}
	}
	if len(rest) > 400 {
		rest = rest[:400]
	}
	discard := strings.Index(strings.ToLower(rest), "discard")
	box := ""
	if m := fileintoRe.FindStringSubmatch(rest); m != nil {
		box = m[1]
	}
	// A rule that both files and discards is read as the one that comes first,
	// because that is the one Sieve reaches.
	if discard >= 0 && (box == "" || discard < strings.Index(strings.ToLower(rest), "fileinto")) {
		return Block, true
	}
	for _, s := range order {
		if s.files != "" && strings.EqualFold(s.files, box) {
			return s.dest, true
		}
	}
	// The catch-all rule at the end of the script files into the Screener. It
	// carries no addresses, so it is only ever reached here by a rule that
	// files somewhere this program does not route to — somebody else's rule,
	// left alone.
	return "", false
}

// parseAddresses reads a Sieve string list. `example@example.com` is dropped
// because it is the placeholder the previous generator wrote into empty lists,
// and importing it would turn a list nobody is on into a decision about a
// sender who does not exist.
func parseAddresses(raw string) []string {
	return parseKeys(raw, false)
}

func parseKeys(raw string, domain bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range strings.Split(raw, ",") {
		a := normalise(strings.Trim(strings.TrimSpace(field), `"`))
		if a == "" || a == "example@example.com" {
			continue
		}
		if domain && !strings.HasPrefix(a, "@") {
			a = "@" + a
		}
		ok := Valid(a)
		if domain {
			ok = ValidDomain(a)
		}
		if !ok || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}
