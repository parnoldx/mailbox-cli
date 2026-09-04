package routing

import (
	"strings"
	"testing"
)

// theOldScript is the shape the script on the real account was written in
// before this program owned it: `header :contains`, one placeholder address in
// the lists nobody is on. Reading it is the first thing this package has to do,
// once, before it ever writes one.
const theOldScript = `require ["mailbox","fileinto", "imap4flags"];

# Step 1: Check blacklist - if blacklisted, discard
if header :contains "From" ["spam@example.net","noise@example.org"]
{
  discard;
  stop;
}

# Step 2: Check whitelist - if whitelisted, move to Inbox
if header :contains "From" ["anna@example.com"]
{
  fileinto "INBOX";
  stop;
}

# Step 3: Move to Paper Trail
if header :contains "From" ["example@example.com"]
{
  addflag "\\seen" ;
  fileinto "INBOX/Paper Trail";
  stop;
}

# Step 4: Move to Feed
if header :contains "From" ["news@example.com"]
{
  addflag "\\seen" ;
  fileinto "INBOX/Feed";
  stop;
}

# Step 5: All other move to Screener for review of the user
#addflag "\\seen" ;
fileinto "INBOX/Screener";`

func TestParseReadsTheScriptThatIsAlreadyThere(t *testing.T) {
	l := Parse(theOldScript)
	for _, tc := range []struct {
		addr string
		want Destination
	}{
		{"spam@example.net", Block},
		{"noise@example.org", Block},
		{"anna@example.com", Inbox},
		{"news@example.com", Feed},
		{"nobody@example.com", None},
	} {
		if got := l.Of(tc.addr); got != tc.want {
			t.Errorf("Of(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
	// The placeholder the old generator wrote into an empty list is not a
	// decision somebody made about a sender who does not exist.
	if got := l.Of("example@example.com"); got != None {
		t.Errorf("the placeholder was imported as %q", got)
	}
	if l.Count() != 4 {
		t.Errorf("count = %d, want 4: %v", l.Count(), l.All())
	}
}

func TestScriptRoundTrips(t *testing.T) {
	l := New()
	for addr, d := range map[string]Destination{
		"anna@example.com":  Inbox,
		"bert@example.com":  Inbox,
		"news@example.com":  Feed,
		"bills@example.com": PaperTrail,
		"spam@example.net":  Block,
	} {
		if _, err := l.Set(addr, d); err != nil {
			t.Fatal(err)
		}
	}
	back := Parse(l.Script())
	for _, r := range l.All() {
		if got := back.Of(r.Address); got != r.To {
			t.Errorf("%s came back as %q, want %q\n%s", r.Address, got, r.To, l.Script())
		}
	}
	if back.Count() != l.Count() {
		t.Errorf("count %d -> %d", l.Count(), back.Count())
	}
}

// A sender controls their own From header, including the display name in it.
// `header :contains` is a substring test over that whole header, so a sender
// calling themselves "anna@example.com" would be filed as Anna. The generated
// script matches the parsed address instead, which is the one thing in the
// header the sender cannot dress up.
func TestScriptMatchesTheAddressAndNotTheHeader(t *testing.T) {
	l := New()
	if _, err := l.Set("anna@example.com", Inbox); err != nil {
		t.Fatal(err)
	}
	script := l.Script()
	if strings.Contains(strings.ToLower(script), "header :contains") {
		t.Errorf("the generated script still matches on the raw header:\n%s", script)
	}
	if !strings.Contains(script, `address :is :all "from"`) {
		t.Errorf("the generated script does not match on the address:\n%s", script)
	}
}

// Sieve has no empty string list: `[]` does not compile, and the previous
// generator filled the gap with an invented address. An empty list is no rule.
func TestEmptyListsAreNoRuleAtAll(t *testing.T) {
	script := New().Script()
	if strings.Contains(script, "[]") || strings.Contains(script, "example@example.com") {
		t.Errorf("an empty routing produced a rule:\n%s", script)
	}
	if !strings.Contains(script, `fileinto "INBOX/Screener"`) {
		t.Errorf("an empty routing must still put everything in the screener:\n%s", script)
	}
	if n := strings.Count(script, "if "); n != 0 {
		t.Errorf("%d rules in an empty routing:\n%s", n, script)
	}
}

// The From header is sender-controlled text that ends up inside a quoted string
// in a program the server runs. Anything that could end the string early is
// refused rather than escaped.
func TestAnAddressThatCouldWriteRulesIsRefused(t *testing.T) {
	l := New()
	for _, addr := range []string{
		`a@b.com"] { discard; } if true {`,
		"a@b.com\nif true { discard; }",
		`a@b\.com`,
		"not-an-address",
		"",
	} {
		if ok, err := l.Set(addr, Inbox); err == nil || ok {
			t.Errorf("Set(%q) was accepted", addr)
		}
	}
	if l.Count() != 0 {
		t.Errorf("the lists took %d of them", l.Count())
	}
}

func TestOneSenderHasOneDestination(t *testing.T) {
	l := New()
	if _, err := l.Set("anna@example.com", Feed); err != nil {
		t.Fatal(err)
	}
	changed, err := l.Set("anna@example.com", Inbox)
	if err != nil || !changed {
		t.Fatalf("changing a decision: changed=%v err=%v", changed, err)
	}
	if l.Count() != 1 {
		t.Errorf("the sender is on %d lists", l.Count())
	}
	if got := l.Of("ANNA@Example.com"); got != Inbox {
		t.Errorf("addresses are case-insensitive to sieve; Of gave %q", got)
	}
	// Deciding the same thing twice is not a change, so nothing is written.
	if changed, _ := l.Set("anna@example.com", Inbox); changed {
		t.Error("setting the same destination reported a change")
	}
	// The Screener is the absence of a decision, so routing there forgets.
	if changed, _ := l.Set("anna@example.com", None); !changed || l.Count() != 0 {
		t.Errorf("forgetting a sender left %d decisions", l.Count())
	}
}

func TestAsideIsRefusedByName(t *testing.T) {
	_, err := ParseDestination("aside")
	if err == nil {
		t.Fatal("aside was accepted as a routing destination")
	}
	if !strings.Contains(err.Error(), "mailbox aside") {
		t.Errorf("the error does not say what to do instead: %v", err)
	}
	for _, word := range []string{"inbox", "Feed", "paper", "paper trail", "BLOCK", "screener"} {
		if _, err := ParseDestination(word); err != nil {
			t.Errorf("ParseDestination(%q): %v", word, err)
		}
	}
}

func TestReplyLaterIsRefusedByName(t *testing.T) {
	for _, word := range []string{"reply later", "reply-later", "Reply Later"} {
		err := parseDestErr(word)
		if err == nil {
			t.Fatalf("%q was accepted as a routing destination", word)
		}
		if !strings.Contains(err.Error(), "mailbox reply-later") {
			t.Errorf("the error does not say what to do instead: %v", err)
		}
	}
}

func parseDestErr(word string) error {
	_, err := ParseDestination(word)
	return err
}

func TestAddressOfReadsAFromHeader(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"bob@example.com", "bob@example.com"},
		{"Bob Smith <Bob@Example.com>", "bob@example.com"},
		{`"Smith, Bob" <bob@example.com>`, "bob@example.com"},
		{"Smith, Bob <bob@example.com>", "bob@example.com"},
		{"Bob <bob@example.com>, Ann <ann@example.com>", "bob@example.com"},
		{"", ""},
	} {
		if got := AddressOf(tc.in); got != tc.want {
			t.Errorf("AddressOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A rule filing somewhere this program does not route to belongs to somebody
// else. Reading it as one of ours would move its senders onto a list and then
// rewrite the script without the rule.
func TestARuleWeDoNotOwnIsNotImported(t *testing.T) {
	l := Parse(`require ["fileinto"];
if address :is :all "from" ["boss@example.com"] {
  fileinto "Archive/Work";
  stop;
}
fileinto "INBOX/Screener";`)
	if l.Count() != 0 {
		t.Errorf("imported somebody else's rule: %v", l.All())
	}
}

// A script is in force when it is the active one *or* when the active one
// includes it. That is not a corner case: mailbox.org's webmail filter editor
// owns the active script and ends it with `include "logic";`, so the Routing
// runs without ever being active itself.
func TestIncludesFindsTheOneThatRunsUs(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   bool
	}{
		{`include "logic";`, true},
		{"# Generated by OX Sieve Bundle\nrequire \"include\";\n\ninclude \"logic\";\n", true},
		{`include :personal "logic";`, true},
		{`INCLUDE "Logic";`, true},
		{`include "other";`, false},
		{`fileinto "logic";`, false},
		{"", false},
	} {
		if got := Includes(tc.script, ScriptName); got != tc.want {
			t.Errorf("Includes(%q) = %v, want %v", tc.script, got, tc.want)
		}
	}
}

// A domain key (`@stripe.com`) matches every address at that domain, and a
// specific address always wins — the two-pass order the generated script
// encodes: every address rule, then every domain rule.
func TestADomainKeyMatchesEveryAddressAtThatDomain(t *testing.T) {
	l := New()
	if _, err := l.Set("@stripe.com", PaperTrail); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Set("receipts@stripe.com", Inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Set("spammy@stripe.com", Block); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		addr string
		want Destination
	}{
		{"invoices@stripe.com", PaperTrail},
		{"receipts@stripe.com", Inbox},
		{"spammy@stripe.com", Block},
		{"other@example.com", None},
	} {
		if got := l.Of(tc.addr); got != tc.want {
			t.Errorf("Of(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestADomainKeyRoundTripsThroughTheScript(t *testing.T) {
	l := New()
	for key, d := range map[string]Destination{
		"anna@example.com": Inbox,
		"@stripe.com":      PaperTrail,
		"@evil.com":        Block,
		"news@example.com": Feed,
	} {
		if _, err := l.Set(key, d); err != nil {
			t.Fatal(err)
		}
	}
	script := l.Script()
	if !strings.Contains(script, `address :domain :is "from"`) {
		t.Errorf("the generated script has no domain test:\n%s", script)
	}
	if !strings.Contains(script, `"stripe.com"`) {
		t.Errorf("the domain was not written without the @:\n%s", script)
	}
	// Address rules must appear before domain rules so Sieve's first match
	// is the two-pass: exact address, then domain.
	addrAt := strings.Index(script, `address :is :all "from"`)
	domAt := strings.Index(script, `address :domain :is "from"`)
	if addrAt < 0 || domAt < 0 || addrAt > domAt {
		t.Errorf("address rules must come before domain rules:\n%s", script)
	}

	back := Parse(script)
	for _, r := range l.All() {
		if got := back.Of(r.Address); got != r.To {
			t.Errorf("%s came back as %q, want %q\n%s", r.Address, got, r.To, script)
		}
	}
	if back.Of("invoices@stripe.com") != PaperTrail {
		t.Errorf("parsed domain did not match invoices@stripe.com: %q\n%s",
			back.Of("invoices@stripe.com"), script)
	}
}

func TestADomainThatCouldWriteRulesIsRefused(t *testing.T) {
	l := New()
	for _, key := range []string{
		`@b.com"] { discard; } if true {`,
		"@b.com\nif true { discard; }",
		"@",
		"@nodot",
		"stripe.com",
	} {
		if ok, err := l.Set(key, Inbox); err == nil || ok {
			t.Errorf("Set(%q) was accepted", key)
		}
	}
}
