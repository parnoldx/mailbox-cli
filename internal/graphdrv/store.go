package graphdrv

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mailbox/internal/sync/mailsync"

	_ "modernc.org/sqlite"
)

// Store is what makes Graph look like IMAP to the reconciler: a local uid for
// every Graph message id, per folder, handed out in arrival order; a modseq that
// goes up whenever a delta reports a change; and the delta link that says where
// the last delta stopped. It also maps the hrefs calendars and contacts are
// known by to Graph ids.
//
// It lives beside the Mirror and outlives a Mirror rebuild, so uids stay put. If
// it is lost, every folder gets a new UIDVALIDITY on first use and the
// reconciler resyncs, re-matching Messages by Message-ID (ADR-0006).
type Store struct {
	db *sql.DB
}

const storeSchema = `
CREATE TABLE IF NOT EXISTS folders (
	name     TEXT PRIMARY KEY,
	graph_id TEXT NOT NULL,
	validity INTEGER NOT NULL,
	next_uid INTEGER NOT NULL,
	modseq   INTEGER NOT NULL,
	delta    TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	folder     TEXT NOT NULL,
	uid        INTEGER NOT NULL,
	graph_id   TEXT NOT NULL,
	flags      TEXT NOT NULL,
	categories TEXT NOT NULL,
	modseq     INTEGER NOT NULL,
	PRIMARY KEY (folder, uid),
	UNIQUE (folder, graph_id)
);
CREATE TABLE IF NOT EXISTS objects (
	href     TEXT PRIMARY KEY,
	graph_id TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS occurrences (
	graph_id TEXT PRIMARY KEY,
	master   TEXT NOT NULL
);
`

// OpenStore opens or creates the store at path; ":memory:" is a throwaway one.
func OpenStore(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	if path == ":memory:" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: an in-memory database is per connection, and nothing
	// here is busy enough to want two.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(storeSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("graph store %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type folderRow struct {
	Name     string
	GraphID  string
	Validity uint32
	NextUID  uint32
	ModSeq   uint64
	Delta    string
}

func (s *Store) folder(name string) (folderRow, bool, error) {
	var f folderRow
	err := s.db.QueryRow(`SELECT name, graph_id, validity, next_uid, modseq, delta FROM folders WHERE name = ?`, name).
		Scan(&f.Name, &f.GraphID, &f.Validity, &f.NextUID, &f.ModSeq, &f.Delta)
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	return f, err == nil, err
}

// setFolder records which Graph folder a name means. A name that now means a
// different folder — deleted and made again, or renamed onto — starts over.
func (s *Store) setFolder(name, graphID string) error {
	f, ok, err := s.folder(name)
	if err != nil {
		return err
	}
	if ok && f.GraphID == graphID {
		return nil
	}
	return s.resetFolder(name, graphID)
}

// resetFolder forgets everything about a folder and gives it a new UIDVALIDITY,
// which is how the reconciler learns that every uid it holds is meaningless. The
// modseq starts at 1: zero is what a server without CONDSTORE reports, and the
// planner treats that differently.
func (s *Store) resetFolder(name, graphID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	validity := uint32(time.Now().Unix())
	var old uint32
	if tx.QueryRow(`SELECT validity FROM folders WHERE name = ?`, name).Scan(&old) == nil && old >= validity {
		validity = old + 1
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE folder = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO folders (name, graph_id, validity, next_uid, modseq, delta)
		VALUES (?, ?, ?, 1, 1, '')`, name, graphID, validity); err != nil {
		return err
	}
	return tx.Commit()
}

// seenMessage is one message as a delta reports it.
type seenMessage struct {
	GraphID    string
	Removed    bool
	Flags      []string
	Categories []string
}

// applyDelta writes one delta's worth of changes and the link that follows
// them, together: a crash between the two would either lose changes or apply
// them twice.
func (s *Store) applyDelta(folder string, items []seenMessage, link string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next uint32
	var modseq uint64
	if err := tx.QueryRow(`SELECT next_uid, modseq FROM folders WHERE name = ?`, folder).Scan(&next, &modseq); err != nil {
		return fmt.Errorf("folder %s: %w", folder, err)
	}
	for _, it := range items {
		if it.Removed {
			if _, err := tx.Exec(`DELETE FROM messages WHERE folder = ? AND graph_id = ?`, folder, it.GraphID); err != nil {
				return err
			}
			continue
		}
		flags, cats := encodeList(it.Flags), encodeList(it.Categories)
		var uid uint32
		var had string
		err := tx.QueryRow(`SELECT uid, flags FROM messages WHERE folder = ? AND graph_id = ?`, folder, it.GraphID).Scan(&uid, &had)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			modseq++
			if _, err := tx.Exec(`INSERT INTO messages (folder, uid, graph_id, flags, categories, modseq) VALUES (?, ?, ?, ?, ?, ?)`,
				folder, next, it.GraphID, flags, cats, modseq); err != nil {
				return err
			}
			next++
		case err != nil:
			return err
		case had != flags:
			modseq++
			if _, err := tx.Exec(`UPDATE messages SET flags = ?, categories = ?, modseq = ? WHERE folder = ? AND uid = ?`,
				flags, cats, modseq, folder, uid); err != nil {
				return err
			}
		default:
			// Nothing the Mirror holds changed, but a category that is not a
			// keyword may have: keep it, so a later write does not drop it.
			if _, err := tx.Exec(`UPDATE messages SET categories = ? WHERE folder = ? AND uid = ?`, cats, folder, uid); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE folders SET next_uid = ?, modseq = ?, delta = ? WHERE name = ?`, next, modseq, link, folder); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) status(folder string) (mailsync.FolderStatus, error) {
	f, ok, err := s.folder(folder)
	if err != nil {
		return mailsync.FolderStatus{}, err
	}
	if !ok {
		return mailsync.FolderStatus{}, fmt.Errorf("folder %q not found on server", folder)
	}
	var n uint32
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder = ?`, folder).Scan(&n); err != nil {
		return mailsync.FolderStatus{}, err
	}
	return mailsync.FolderStatus{
		Name: folder, UIDValidity: f.Validity, UIDNext: f.NextUID,
		HighestModSeq: f.ModSeq, NumMessages: n,
	}, nil
}

func (s *Store) changed(folder string, since uint64) ([]mailsync.FlagUpdate, error) {
	rows, err := s.db.Query(`SELECT uid, flags FROM messages WHERE folder = ? AND modseq > ? ORDER BY uid`, folder, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailsync.FlagUpdate
	for rows.Next() {
		var u mailsync.FlagUpdate
		var flags string
		if err := rows.Scan(&u.UID, &flags); err != nil {
			return nil, err
		}
		u.Flags = decodeList(flags)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) uids(folder string) ([]uint32, error) {
	rows, err := s.db.Query(`SELECT uid FROM messages WHERE folder = ? ORDER BY uid`, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var u uint32
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// message is one uid's Graph id and the categories it carries.
func (s *Store) message(folder string, uid uint32) (string, []string, error) {
	var id, cats string
	err := s.db.QueryRow(`SELECT graph_id, categories FROM messages WHERE folder = ? AND uid = ?`, folder, uid).Scan(&id, &cats)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf("no message %d in %s", uid, folder)
	}
	return id, decodeList(cats), err
}

// setFlags records what a write left on a message, without moving the modseq:
// the writer has already put it in the Mirror, and the delta that reports it
// again will find nothing different.
func (s *Store) setFlags(folder string, uid uint32, flags, cats []string) error {
	_, err := s.db.Exec(`UPDATE messages SET flags = ?, categories = ? WHERE folder = ? AND uid = ?`,
		encodeList(flags), encodeList(cats), folder, uid)
	return err
}

// place gives a message a uid in a folder — a move landing, or a message we
// created — and returns it. A message the folder already has keeps its uid.
func (s *Store) place(folder, graphID string, flags, cats []string) (uint32, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var uid uint32
	if tx.QueryRow(`SELECT uid FROM messages WHERE folder = ? AND graph_id = ?`, folder, graphID).Scan(&uid) == nil {
		return uid, nil
	}
	var modseq uint64
	if err := tx.QueryRow(`SELECT next_uid, modseq FROM folders WHERE name = ?`, folder).Scan(&uid, &modseq); err != nil {
		return 0, fmt.Errorf("folder %s: %w", folder, err)
	}
	if _, err := tx.Exec(`INSERT INTO messages (folder, uid, graph_id, flags, categories, modseq) VALUES (?, ?, ?, ?, ?, ?)`,
		folder, uid, graphID, encodeList(flags), encodeList(cats), modseq+1); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE folders SET next_uid = ?, modseq = ? WHERE name = ?`, uid+1, modseq+1, folder); err != nil {
		return 0, err
	}
	return uid, tx.Commit()
}

func (s *Store) drop(folder string, uid uint32) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE folder = ? AND uid = ?`, folder, uid)
	return err
}

// objectID is the Graph id an href names; objectHref is the other way.
func (s *Store) objectID(href string) (string, bool) {
	var id string
	err := s.db.QueryRow(`SELECT graph_id FROM objects WHERE href = ?`, href).Scan(&id)
	return id, err == nil
}

func (s *Store) objectHref(graphID string) (string, bool) {
	var href string
	err := s.db.QueryRow(`SELECT href FROM objects WHERE graph_id = ?`, graphID).Scan(&href)
	return href, err == nil
}

func (s *Store) putObject(href, graphID string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO objects (href, graph_id) VALUES (?, ?)`, href, graphID)
	return err
}

func (s *Store) dropObject(graphID string) error {
	_, err := s.db.Exec(`DELETE FROM objects WHERE graph_id = ?`, graphID)
	return err
}

// setMaster remembers which series an occurrence belongs to, because the delta
// that removes one says only its id.
func (s *Store) setMaster(occurrence, master string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO occurrences (graph_id, master) VALUES (?, ?)`, occurrence, master)
	return err
}

func (s *Store) masterOf(occurrence string) (string, bool) {
	var m string
	err := s.db.QueryRow(`SELECT master FROM occurrences WHERE graph_id = ?`, occurrence).Scan(&m)
	return m, err == nil
}

func encodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeList(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
