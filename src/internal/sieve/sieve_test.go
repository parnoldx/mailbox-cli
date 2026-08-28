package sieve

import (
	"strings"
	"testing"
)

func TestRoundTripAllLists(t *testing.T) {
	in := &Lists{
		Blacklist:  []string{"spam@spam.com", "junk@junk.com"},
		Whitelist:  []string{"white@white.com"},
		PaperTrail: []string{"paper@trail.com"},
		Feed:       []string{"newsletter@feed.com", "news@feed.com"},
	}
	out, err := ParseScript(GenerateScript(in))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Equal(in) {
		t.Fatalf("roundtrip mismatch:\nin=%+v\nout=%+v", in, out)
	}
}

// The generator writes ["example@example.com"] for empty lists; parsing must
// drop that placeholder, not treat it as a real address. This also pins the
// block-context classification: Paper Trail must never be misread as
// Whitelist just because its fileinto line contains `fileinto "INBOX"`.
func TestParseEmptyLists(t *testing.T) {
	out, err := ParseScript(GenerateScript(NewLists()))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]string{
		"blacklist": out.Blacklist, "whitelist": out.Whitelist,
		"papertrail": out.PaperTrail, "feed": out.Feed,
	} {
		if len(got) != 0 {
			t.Errorf("%s: want empty, got %v", name, got)
		}
	}
}

func TestParsePaperTrailOnly(t *testing.T) {
	in := NewLists()
	in.PaperTrail = []string{"receipts@shop.com"}
	out, err := ParseScript(GenerateScript(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Whitelist) != 0 {
		t.Errorf("whitelist polluted: %v", out.Whitelist)
	}
	if len(out.PaperTrail) != 1 || out.PaperTrail[0] != "receipts@shop.com" {
		t.Errorf("papertrail: %v", out.PaperTrail)
	}
}

func TestApplyRouting(t *testing.T) {
	l := NewLists()
	cases := []struct {
		mv      Movement
		list    *[]string
		changed bool
	}{
		{Movement{FolderInbox, "a@x.com"}, &l.Whitelist, true},
		{Movement{FolderFeed, "b@x.com"}, &l.Feed, true},
		{Movement{FolderPaperTrail, "c@x.com"}, &l.PaperTrail, true},
		{Movement{FolderBlock, "d@x.com"}, &l.Blacklist, true},
		{Movement{FolderScreener, "e@x.com"}, nil, false}, // screener changes nothing
		{Movement{FolderFeed, ""}, nil, false},            // no sender
		{Movement{FolderFeed, "b@x.com"}, nil, false},     // dedupe
	}
	for _, tc := range cases {
		if changed := Apply(l, tc.mv); changed != tc.changed {
			t.Errorf("Apply(%+v) changed=%v, want %v", tc.mv, changed, tc.changed)
		}
	}
	if len(l.Whitelist) != 1 || l.Whitelist[0] != "a@x.com" {
		t.Errorf("whitelist: %v", l.Whitelist)
	}
	if len(l.Feed) != 1 || len(l.PaperTrail) != 1 || len(l.Blacklist) != 1 {
		t.Errorf("lists: %+v", l)
	}
}

func TestSyncComposition(t *testing.T) {
	start := NewLists()
	start.Whitelist = []string{"old@x.com"}

	mv := Movement{FolderBlock, "bad@x.com"}
	lists, err := ParseScript(GenerateScript(start))
	if err != nil {
		t.Fatal(err)
	}
	updated := lists.Clone()
	if !Apply(updated, mv) {
		t.Fatal("expected change")
	}
	script := GenerateScript(updated)
	reparsed, err := ParseScript(script)
	if err != nil {
		t.Fatal(err)
	}
	if !reparsed.Equal(updated) {
		t.Fatalf("reparse mismatch:\nwant %+v\ngot  %+v", updated, reparsed)
	}
	if !strings.Contains(script, `"bad@x.com"`) {
		t.Fatal("blacklisted sender missing from script")
	}
}

func TestSameStringsOrderIndependent(t *testing.T) {
	if !sameStrings([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("order-independent equality failed")
	}
	if sameStrings([]string{"a"}, []string{"a", "a"}) {
		t.Error("duplicate counts ignored")
	}
}

func TestApplyRejectsUnsafeAddresses(t *testing.T) {
	unsafe := []string{
		`evil@x.com"]{discard;stop;}if header :contains "From" ["z@z.com`,
		`quote"@x.com`,
		"back\\slash@x.com",
		"new\nline@x.com",
		"spaced out@x.com",
		"no-at-sign",
		"",
	}
	for _, addr := range unsafe {
		lists := NewLists()
		if Apply(lists, Movement{Folder: FolderFeed, Address: addr}) {
			t.Fatalf("accepted unsafe address %q", addr)
		}
		if len(lists.Feed) != 0 {
			t.Fatalf("stored unsafe address %q", addr)
		}
	}
	lists := NewLists()
	if !Apply(lists, Movement{Folder: FolderFeed, Address: "news@feed.example"}) {
		t.Fatal("rejected an ordinary address")
	}
}

func TestGenerateScriptDropsUnsafeAddresses(t *testing.T) {
	poisoned := &Lists{Feed: []string{`evil@x.com"]{discard;stop;}if header :contains "From" ["z@z.com`}}
	script := GenerateScript(poisoned)
	if strings.Contains(script, "discard;stop;") {
		t.Fatalf("injected rule survived into the script:\n%s", script)
	}
	if strings.Count(script, "discard;") != 1 {
		t.Fatalf("expected only the blacklist discard:\n%s", script)
	}
}
