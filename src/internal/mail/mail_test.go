package mail

import (
	"testing"

	"mailbox/src/internal/folders"
)

var names = []string{
	folders.INBOX,
	folders.FEED,
	folders.PAPER_TRAIL,
	folders.SCREENER,
	"Archive",
	"Archive/Immo",
	"Archive/Immo/2024",
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInboxIsNotAPrefixTree(t *testing.T) {
	got, err := ScopedSearchFolders(names, "inbox")
	if err != nil || !eq(got, []string{folders.INBOX}) {
		t.Fatalf("got %v %v", got, err)
	}
	got, _ = ScopedSearchFolders(names, "INBOX")
	if !eq(got, []string{folders.INBOX}) {
		t.Fatalf("got %v", got)
	}
}

func TestFeedAndScreenerAreExact(t *testing.T) {
	got, _ := ScopedSearchFolders(names, "feed")
	if !eq(got, []string{folders.FEED}) {
		t.Fatalf("got %v", got)
	}
	got, _ = ScopedSearchFolders(names, "screener")
	if !eq(got, []string{folders.SCREENER}) {
		t.Fatalf("got %v", got)
	}
}

func TestArchiveWalksTheTree(t *testing.T) {
	got, _ := ScopedSearchFolders(names, "archive")
	want := []string{"Archive", "Archive/Immo", "Archive/Immo/2024"}
	if !eq(got, want) {
		t.Fatalf("got %v", got)
	}
	got, _ = ScopedSearchFolders(names, "Archive/Immo")
	want = []string{"Archive/Immo", "Archive/Immo/2024"}
	if !eq(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestUnscopedKeepsListedOrder(t *testing.T) {
	got, _ := ScopedSearchFolders(names, "")
	if !eq(got, names) {
		t.Fatalf("got %v", got)
	}
}

func TestListFolderName(t *testing.T) {
	cases := map[string]string{
		`(\HasChildren \UnMarked) "/" INBOX`:                            "INBOX",
		`(\HasChildren \UnMarked) "/" INBOX/Screener`:                   "INBOX/Screener",
		`(\HasNoChildren \UnMarked) "/" "INBOX/Paper Trail"`:            "INBOX/Paper Trail",
		`(\HasNoChildren \UnMarked \Trash) "/" Trash`:                   "Trash",
		`LIST (\HasNoChildren \UnMarked) "/" INBOX/Feed`:                "INBOX/Feed",
		`LIST (\HasNoChildren \UnMarked) "/" "INBOX/Paper Trail"`:       "INBOX/Paper Trail",
		`LIST (\HasNoChildren \UnMarked) "/" "Archive/Travel/Japan 26"`: "Archive/Travel/Japan 26",
		`LIST (\HasChildren \Archive) "/" Archive`:                      "Archive",
	}
	for line, want := range cases {
		if got := ListFolderName(line); got != want {
			t.Errorf("ListFolderName(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestSearchCriteria(t *testing.T) {
	got, err := searchCriteria(SearchQuery{From: "a@b.c", Date: "last_7_days", Required: "foo bar"})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"FROM", `"a@b.c"`, "TEXT", `"foo"`, "TEXT", `"bar"`, "SINCE"}
	if len(got) < len(wantPrefix) {
		t.Fatalf("got %v", got)
	}
	for i, w := range wantPrefix {
		if got[i] != w {
			t.Fatalf("got %v want prefix %v", got, wantPrefix)
		}
	}
	_, err = searchCriteria(SearchQuery{Date: "nope"})
	if err == nil {
		t.Fatal("expected bad date")
	}
	_, err = searchCriteria(SearchQuery{Attachment: "pdf"})
	if err == nil {
		t.Fatal("expected bad attachment")
	}
}

func TestRelatedMessageIDs(t *testing.T) {
	msg := &ThreadMessage{
		InReplyTo:  "<b@example>",
		References: "<a@example> <b@example>",
		MessageID:  "<c@example>",
	}
	got := relatedMessageIDs(msg)
	want := []string{"<b@example>", "<a@example>", "<c@example>"}
	if !eq(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestShortFrom(t *testing.T) {
	cases := map[string]string{
		"Test User <test@example.com>": "Test User",
		"<no-name@example.com>":        "no-name@example.com",
		"bare@example.com":             "bare@example.com",
	}
	for in, want := range cases {
		if got := ShortFrom(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}
