package mirror

import (
	"path/filepath"
	"testing"
	"time"
)

// Removing an account is a prune of its rows, not a rebuild of the file: the
// Mirror is disposable for a schema change (ADR-0013), which is a different
// thing from forgetting one account out of two.
func TestForgettingOneAccountLeavesTheOtherAlone(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	for _, account := range []string{"primary", "gmx"} {
		tx, err := m.Begin(account)
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := tx.UpsertMessage(Message{
			Key: account + "@example.com", Subject: "Rechnung", From: "billing@example.com",
			Date: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SetBody(id, "the text\n", "", "the text"); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutPlacement(Placement{Folder: "INBOX", UID: 7, MessageID: id}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err := m.PutCollection(Collection{
			Account: account, Kind: "events", URL: "https://dav/" + account + "/", Name: "Kalender",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.ForgetAccount("gmx"); err != nil {
		t.Fatal(err)
	}
	if rows, err := m.Rows("gmx", "INBOX", 10); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Fatalf("%d rows survived the account", len(rows))
	}
	if cols, err := m.Collections("gmx", ""); err != nil {
		t.Fatal(err)
	} else if len(cols) != 0 {
		t.Fatalf("%d collections survived the account", len(cols))
	}
	// The other account is untouched, which is the whole reason this is a
	// prune and not a delete of the file.
	if rows, err := m.Rows("primary", "INBOX", 10); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 {
		t.Fatalf("primary has %d rows, want 1", len(rows))
	}
	if hits, err := m.Search("primary", Query{Text: "Rechnung", Limit: 10}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 1 {
		t.Fatalf("search found %d, want the one that is left", len(hits))
	}
}

func TestForgettingACollectionTakesItsObjects(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	id, err := m.PutCollection(Collection{
		Account: "primary", Kind: "tasks", URL: "https://dav/1/", Name: "Aufgaben",
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutObject(Object{
		CollectionID: id, Href: "1.ics", ETag: "a",
		Raw:  "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
		Kind: "todo", UID: "t1", Summary: "Milch",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := m.ForgetCollection("primary", "https://dav/1/"); err != nil {
		t.Fatal(err)
	}
	if cols, err := m.Collections("primary", ""); err != nil {
		t.Fatal(err)
	} else if len(cols) != 0 {
		t.Fatalf("%d collections left", len(cols))
	}
	if todos, err := m.Todos("primary", "", true); err != nil {
		t.Fatal(err)
	} else if len(todos) != 0 {
		t.Fatalf("%d objects survived the collection", len(todos))
	}
}
