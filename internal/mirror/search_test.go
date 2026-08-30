package mirror

import (
	"path/filepath"
	"testing"
	"time"
)

// seedSearch builds a Mirror with three Messages: one in the Inbox, one in the
// Inbox and Archive both, and one whose Placement has gone — trashed after it
// was mirrored.
func seedSearch(t *testing.T) *Mirror {
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

	add := func(key, subject, from, body string, places ...Placement) int64 {
		t.Helper()
		id, _, err := tx.UpsertMessage(Message{
			Key: key, Subject: subject, From: from, To: "me@example.com",
			Date: time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SetBody(id, body, "", body); err != nil {
			t.Fatal(err)
		}
		for _, p := range places {
			p.MessageID = id
			if err := tx.PutPlacement(p); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	add("bill@x", "Rechnung Mai", "billing@example.com", "Ihre Rechnung liegt bereit",
		Placement{Folder: "INBOX", UID: 1})
	add("both@x", "Rechnung an Sie", "billing@example.com", "eine weitere Rechnung",
		Placement{Folder: "Archive", UID: 5}, Placement{Folder: "INBOX", UID: 2})
	add("gone@x", "Rechnung vom Vorjahr", "billing@example.com", "alte Rechnung")

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return m
}

func search(t *testing.T, m *Mirror, q Query) []Hit {
	t.Helper()
	hits, err := m.Search("primary", q)
	if err != nil {
		t.Fatalf("search %+v: %v", q, err)
	}
	return hits
}

// A Message with no Placement left is a Message that was trashed, and Trash is
// not searchable (ADR-0009).
func TestSearchSkipsMessagesWithNoPlacement(t *testing.T) {
	m := seedSearch(t)
	hits := search(t, m, Query{Text: "Rechnung"})
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
	}
	for _, h := range hits {
		if h.Message.Key == "gone@x" {
			t.Errorf("a message with no placement came back")
		}
	}
}

// One Hit per Message, reported under the Inbox when it sits in two Boxes.
func TestSearchReportsAMessageOnceUnderTheInbox(t *testing.T) {
	m := seedSearch(t)
	hits := search(t, m, Query{Text: "weitere"})
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].Placement.Folder != "INBOX" || hits[0].Placement.UID != 2 {
		t.Errorf("hit placement = %s:%d, want INBOX:2", hits[0].Placement.Folder, hits[0].Placement.UID)
	}
}

func TestSearchFilters(t *testing.T) {
	m := seedSearch(t)
	if hits := search(t, m, Query{Text: "Rechnung", Box: "Archive"}); len(hits) != 1 {
		t.Errorf("--in Archive: got %d hits, want 1", len(hits))
	}
	if hits := search(t, m, Query{Text: "Rechnung", From: "billing@"}); len(hits) != 2 {
		t.Errorf("--from billing@: got %d hits, want 2", len(hits))
	}
	if hits := search(t, m, Query{Text: "Rechnung", From: "nobody@"}); len(hits) != 0 {
		t.Errorf("--from nobody@: got %d hits, want 0", len(hits))
	}
}

// The snippet says why a Message matched, so a caller does not have to read
// each result to find out.
func TestSearchReturnsASnippet(t *testing.T) {
	m := seedSearch(t)
	hits := search(t, m, Query{Text: "bereit"})
	if len(hits) != 1 || hits[0].Snippet == "" {
		t.Fatalf("hits = %+v", hits)
	}
}

// Two words mean both words.
func TestSearchTermsAreAnded(t *testing.T) {
	m := seedSearch(t)
	if hits := search(t, m, Query{Text: "Rechnung bereit"}); len(hits) != 1 {
		t.Errorf("got %d hits, want 1", len(hits))
	}
	if hits := search(t, m, Query{Text: "Rechnung nichtvorhanden"}); len(hits) != 0 {
		t.Errorf("got %d hits, want 0", len(hits))
	}
}

// What a caller types is words, not syntax. None of these may reach FTS5 as an
// expression, and none of them may fail.
func TestSearchQueriesAreNeverSyntax(t *testing.T) {
	m := seedSearch(t)
	for _, q := range []string{`rechnung:`, `"unbalanced`, `foo-bar`, `NOT rechnung`, `a*`} {
		if _, err := m.Search("primary", Query{Text: q}); err != nil {
			t.Errorf("search %q: %v", q, err)
		}
	}
	// Punctuation on its own has no words in it, which is the empty query.
	if _, err := m.Search("primary", Query{Text: `(`}); err == nil {
		t.Error(`search "(" should ask for something to look for`)
	}
}

// A quoted phrase is the one piece of syntax that survives, because it is the
// one people mean.
func TestSearchKeepsQuotedPhrasesTogether(t *testing.T) {
	m := seedSearch(t)
	if hits := search(t, m, Query{Text: `"Rechnung liegt"`}); len(hits) != 1 {
		t.Errorf(`"Rechnung liegt": got %d hits, want 1`, len(hits))
	}
	if hits := search(t, m, Query{Text: `"liegt Rechnung"`}); len(hits) != 0 {
		t.Errorf(`"liegt Rechnung": got %d hits, want 0`, len(hits))
	}
}

func TestSearchNeedsSomethingToLookFor(t *testing.T) {
	m := seedSearch(t)
	if _, err := m.Search("primary", Query{Text: "   "}); err == nil {
		t.Error("an empty query should be a usage error")
	}
}
