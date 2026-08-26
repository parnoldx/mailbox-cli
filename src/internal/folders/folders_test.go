package folders

import "testing"

func TestScreenTargets(t *testing.T) {
	cases := map[string]string{
		"paper-trail": PAPER_TRAIL,
		"paper trail": PAPER_TRAIL,
		"The Feed":    FEED,
		"block":       BLOCK,
		"Inbox":       INBOX,
	}
	for in, want := range cases {
		got, err := ResolveScreenTarget(in)
		if err != nil || got != want {
			t.Fatalf("%s -> %q %v", in, got, err)
		}
	}
}

func TestScreenRejectsArchive(t *testing.T) {
	if _, err := ResolveScreenTarget("archive"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFolderAliases(t *testing.T) {
	cases := map[string]string{
		"screener":           SCREENER,
		"INBOX/Paper Trail":  PAPER_TRAIL,
		"Archive/Immo":       "Archive/Immo",
		"Inbox/Feed":         FEED,
		"feed":               FEED,
		"The Feed":           FEED,
		"paper trail":        PAPER_TRAIL,
	}
	for in, want := range cases {
		got, err := ResolveFolder(in, nil)
		if err != nil || got != want {
			t.Fatalf("%s -> %q %v", in, got, err)
		}
	}
}

func TestLastSegmentMatchesArchiveTree(t *testing.T) {
	got, err := ResolveFolder("Immo", []string{INBOX, "Archive/Immo"})
	if err != nil || got != "Archive/Immo" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = ResolveFolder("immo", []string{"Archive/Immo"})
	if err != nil || got != "Archive/Immo" {
		t.Fatalf("got %q %v", got, err)
	}
}

func rowsToIDs(rows []FolderRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func sameStrings(a, b []string) bool {
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

func TestFolderCatalogOmitsArchiveTree(t *testing.T) {
	rows := FolderCatalog([]string{"INBOX", "INBOX/Feed", "Junk", "Archive", "Archive/Immo", "Archive/Immo/2024"}, false)
	ids := rowsToIDs(rows)
	want := []string{"inbox", "feed", "trail", "screener", "block", "aside", "drafts", "sent"}
	if !sameStrings(ids, want) {
		t.Fatalf("got %v", ids)
	}
}

func TestFolderCatalogArchiveOnly(t *testing.T) {
	rows := FolderCatalog([]string{"INBOX", "INBOX/Feed", "Archive", "Archive/Immo", "Archive/Immo/2024"}, true)
	ids := rowsToIDs(rows)
	want := []string{"archive", "Archive/Immo", "Archive/Immo/2024"}
	if !sameStrings(ids, want) {
		t.Fatalf("got %v", ids)
	}
	for _, r := range rows {
		if r.Role != "topic filing" {
			t.Fatal("role")
		}
	}
}

func TestUnknownFolderIsRefused(t *testing.T) {
	_, err := ResolveFolder("imbox", nil)
	if err == nil || !contains(err.Error(), "imbox") {
		t.Fatalf("err=%v", err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && stringsIndex(s, sub) >= 0 }

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
