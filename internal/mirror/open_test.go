package mirror

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// There is one schema and no migrations. A file at another version is thrown
// away and built again, because everything in it is derived from a server that
// still holds the original (ADR-0013). This is the whole of the version
// handling, so it is the whole of what has to hold.
func TestAMirrorAtAnotherVersionIsRebuiltRatherThanMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mirror.db")

	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tx.UpsertMessage(Message{
		Key: "old@example.com", Subject: "from the previous schema",
		Date: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(
		`UPDATE meta SET value = ? WHERE key = 'schema_version'`, schemaVersion-1); err != nil {
		t.Fatal(err)
	}
	m.Close()

	m, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if v, err := readVersion(m.db); err != nil || v != schemaVersion {
		t.Fatalf("reopened mirror is at schema %d (%v), want %d", v, err, schemaVersion)
	}
	var messages int
	if err := m.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 0 {
		t.Fatalf("the old rows survived the rebuild: %d messages, want 0", messages)
	}
}

// An empty or foreign file is version 0, which is not the current one, so it
// takes the same road: replaced, not written over.
func TestAFileThatIsNotAMirrorIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mirror.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if v, err := readVersion(m.db); err != nil || v != schemaVersion {
		t.Fatalf("mirror is at schema %d (%v), want %d", v, err, schemaVersion)
	}
}

// A Mirror that cannot be read right now — a lock held by a sync, a permission
// problem, an I/O error — is not a Mirror at another version. Open has to fail
// and leave the file alone: it is the copy the server may no longer be able to
// hand back, and the dsn deliberately lets a sync and a command hold it at
// once, so a transient lock is an expected condition rather than a rebuild.
func TestAMirrorThatCannotBeReadIsNotDeleted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file anyway")
	}
	path := filepath.Join(t.TempDir(), "mirror.db")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	if _, err := Open(path); err == nil {
		t.Fatal("Open read a mirror it cannot open")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Open deleted a mirror it could not read: %v", err)
	}
}
