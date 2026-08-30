package mirror

import (
	"path/filepath"
	"testing"
)

func TestBoxCountsCountsUnseenSeparately(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range []struct {
		folder string
		flags  []string
	}{
		{"INBOX", nil},
		{"INBOX", []string{`\Seen`}},
		{"INBOX/Screener", []string{`\Seen`, `\Answered`}},
	} {
		id, _, err := tx.UpsertMessage(Message{Key: string(rune('a' + i))})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutPlacement(Placement{
			Folder: f.folder, UID: uint32(10 + i), MessageID: id, Flags: f.flags,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := m.BoxCounts("primary")
	if err != nil {
		t.Fatal(err)
	}
	want := []BoxCount{
		{Folder: "INBOX", Count: 2, Unseen: 1},
		{Folder: "INBOX/Screener", Count: 1, Unseen: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
