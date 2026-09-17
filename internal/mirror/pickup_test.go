package mirror

import (
	"path/filepath"
	"testing"
	"time"

	"mailbox/internal/pickup"
)

// A Pickup whose stored instant is not RFC3339 must fail the read. Its zero
// time is not "no instant": binExpiredPickups reads a zero InternalDate as
// "older than the window" and moves the mail to Trash on the next tick, so a
// swallowed parse error would bin a real mail.
func TestAPickupWithAnUnreadableInstantIsAnErrorNotAnExpiry(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := tx.UpsertMessage(Message{
		Key: "code@example.com", Subject: "Your login code",
		Date: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(Placement{
		Folder: "INBOX", UID: 1, MessageID: id,
		Flags: []string{pickup.Keyword}, InternalDate: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`UPDATE placements SET internaldate = 'later'`); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Pickups("primary"); err == nil {
		t.Fatal("an unparseable instant read as a zero time; the mail would be binned")
	}
}
