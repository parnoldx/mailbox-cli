package mirror

import (
	"path/filepath"
	"testing"
	"time"
)

func seedCorrespondents(t *testing.T) *Mirror {
	t.Helper()
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	add := func(key, from, to, cc string, date time.Time) {
		t.Helper()
		if _, _, err := tx.UpsertMessage(Message{Key: key, From: from, To: to, Cc: cc, Date: date}); err != nil {
			t.Fatal(err)
		}
	}

	add("older@x", `"Jamie Roe" <jamie@example.com>`, "me@example.com", "",
		time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC))
	// A later mail from the same address, with a name for the first time —
	// the name should stick, and last_seen should move to this one.
	add("newer@x", "jamie@example.com", "me@example.com", `"Ana Silva" <ana@example.com>`,
		time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	add("named@x", `"Jamie Roe" <jamie@example.com>`, "me@example.com", "",
		time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC))

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSearchCorrespondentsMatchesEmailPrefix(t *testing.T) {
	m := seedCorrespondents(t)
	hits, err := m.SearchCorrespondents("primary", "jam", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Email != "jamie@example.com" {
		t.Fatalf("hits = %+v, want one jamie@example.com", hits)
	}
}

func TestSearchCorrespondentsMatchesNamePrefix(t *testing.T) {
	m := seedCorrespondents(t)
	hits, err := m.SearchCorrespondents("primary", "ana", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Email != "ana@example.com" {
		t.Fatalf("hits = %+v, want one ana@example.com", hits)
	}
}

// The same address seen across three messages is one row, its name kept
// once a message carries one.
func TestSearchCorrespondentsKeepsTheNameOnceLearned(t *testing.T) {
	m := seedCorrespondents(t)
	hits, err := m.SearchCorrespondents("primary", "jamie", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want exactly one row for jamie@example.com", hits)
	}
	if hits[0].Name != "Jamie Roe" {
		t.Errorf("name = %q, want %q", hits[0].Name, "Jamie Roe")
	}
}

func TestSearchCorrespondentsIsAccountScoped(t *testing.T) {
	m := seedCorrespondents(t)
	hits, err := m.SearchCorrespondents("someone-else", "jam", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v, want none for a different account", hits)
	}
}

func TestSearchCorrespondentsNeedsAPrefix(t *testing.T) {
	m := seedCorrespondents(t)
	hits, err := m.SearchCorrespondents("primary", "", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v, want none for an empty prefix", hits)
	}
}
