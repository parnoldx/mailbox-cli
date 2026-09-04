// Package mirror owns the Mirror: its schema, its domain types, and every
// query against it. No package above it writes SQL, and no package below it
// knows a command exists.
package mirror

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Mirror is the local copy of what the servers hold. It is the answer a read
// command gives, not an optimisation in front of one (ADR-0001).
type Mirror struct {
	db   *sql.DB
	path string
}

// dsn is how the Mirror is opened, in both the read and the rebuild: WAL so a
// sync and a command can hold it at once, a busy timeout so the loser of that
// waits instead of failing, and foreign keys on so a Placement cannot outlive
// its Message.
func dsn(path string) string {
	return "file:" + path + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
}

// Open opens the Mirror at path, creating it if absent. There is one schema and
// no migrations: a file at any other version is deleted and built again,
// because every byte in here is derived from a server that still holds the
// original (ADR-0013). That is the whole of the version handling — nothing
// reads an old shape and nothing translates one.
func Open(path string) (*Mirror, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if readVersion(db) == schemaVersion {
		return &Mirror{db: db, path: path}, nil
	}
	db.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	db, err = sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, schemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	return &Mirror{db: db, path: path}, nil
}

// readVersion is the version the file claims. Anything that cannot be read as
// one — no row, no meta table, not a database at all — is 0, which is not the
// current version and so means rebuild.
func readVersion(db *sql.DB) int {
	var v int
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
		return 0
	}
	return v
}

// Close closes the underlying database.
func (m *Mirror) Close() error { return m.db.Close() }

// Folder returns what the Mirror knows about a folder. An unsynced folder comes
// back zeroed rather than as an error, because "never synced" is a state the
// reconciler plans against.
func (m *Mirror) Folder(account, name string) (FolderState, error) {
	f := FolderState{Account: account, Name: name}
	var syncedAt sql.NullString
	err := m.db.QueryRow(
		`SELECT uidvalidity, uidnext, highestmodseq, synced_at FROM folders WHERE account = ? AND name = ?`,
		account, name).Scan(&f.UIDValidity, &f.UIDNext, &f.HighestModSeq, &syncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if syncedAt.Valid {
		f.SyncedAt, _ = time.Parse(time.RFC3339, syncedAt.String)
	}
	if err := m.db.QueryRow(
		`SELECT count(*) FROM placements WHERE account = ? AND folder = ?`,
		account, name).Scan(&f.Count); err != nil {
		return f, err
	}
	return f, nil
}

// ErrNotFound means the Mirror holds no such row. A caller turns it into a
// not_found reply rather than an error: asking for a uid that has been expunged
// is an ordinary thing to do against a Mirror that may be Behind.
var ErrNotFound = errors.New("not found")

// rowFields is every column scanRow reads, in the order it reads them. It is
// written once because three queries scan it and a fourth column added to one
// copy is a scan-count error in the other two.
const rowFields = `p.uid, p.flags, p.internaldate, p.size,
		       m.id, m.message_key, m.date, m.subject, m.from_addr, m.to_addr,
		       m.cc_addr, m.text_plain, m.text_html, m.body_state, m.thread_id,
		       m.in_reply_to, m.references_`

const rowColumns = `
		SELECT ` + rowFields + `
		  FROM placements p JOIN messages m ON m.id = p.message_id`

// Rows returns a folder's placements joined to their messages, newest first.
func (m *Mirror) Rows(account, folder string, limit int) ([]Row, error) {
	rows, err := m.db.Query(rowColumns+`
		 WHERE p.account = ? AND p.folder = ?
		 ORDER BY m.date DESC, p.uid DESC
		 LIMIT ?`, account, folder, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		r, err := scanRow(rows.Scan, folder)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Row returns one Placement joined to its Message. It is the read behind
// message view, and the only place the Mirror hands back a whole body.
func (m *Mirror) Row(account, folder string, uid uint32) (Row, error) {
	row := m.db.QueryRow(rowColumns+`
		 WHERE p.account = ? AND p.folder = ? AND p.uid = ?`, account, folder, uid)
	r, err := scanRow(row.Scan, folder)
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	return r, err
}

// Placements returns every Box this Message currently sits in. A mail sent to
// yourself has two; a moved one still has exactly one.
func (m *Mirror) Placements(account string, messageID int64) ([]Placement, error) {
	rows, err := m.db.Query(
		`SELECT folder, uid FROM placements WHERE account = ? AND message_id = ? ORDER BY folder, uid`,
		account, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Placement
	for rows.Next() {
		var p Placement
		if err := rows.Scan(&p.Folder, &p.UID); err != nil {
			return nil, err
		}
		p.MessageID = messageID
		out = append(out, p)
	}
	return out, rows.Err()
}

// SenderOf returns a Message's From header by its id. It outlives the
// Placements that point at it (ADR-0007), so it still answers after a move —
// which is when a Screener-decision inference needs it: the mail has already
// left the Screener and the question is whose it was.
func (m *Mirror) SenderOf(account string, messageID int64) (string, error) {
	var from string
	err := m.db.QueryRow(
		`SELECT from_addr FROM messages WHERE account = ? AND id = ?`, account, messageID).Scan(&from)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return from, err
}

// scanRow reads one joined row, whether it came from Query or QueryRow.
func scanRow(scan func(...any) error, folder string) (Row, error) {
	var r Row
	var flags, inReplyTo, references string
	var internal, date sql.NullString
	if err := scan(&r.Placement.UID, &flags, &internal, &r.Placement.Size,
		&r.Message.ID, &r.Message.Key, &date, &r.Message.Subject,
		&r.Message.From, &r.Message.To, &r.Message.Cc, &r.Message.TextPlain, &r.Message.TextHTML,
		&r.Message.BodyState, &r.Message.ThreadID, &inReplyTo, &references); err != nil {
		return Row{}, err
	}
	r.Placement.Folder = folder
	r.Placement.Flags = splitFlags(flags)
	// The chain a Message belongs to. Threading is built from message_refs, so
	// nothing needed these until a draft had to be sent from where it was left
	// and still land in its conversation.
	r.Message.InReplyTo = splitFlags(inReplyTo)
	r.Message.References = splitFlags(references)
	if internal.Valid {
		r.Placement.InternalDate, _ = time.Parse(time.RFC3339, internal.String)
	}
	if date.Valid {
		r.Message.Date, _ = time.Parse(time.RFC3339, date.String)
	}
	return r, nil
}

func splitFlags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, " ")
}

func joinFlags(f []string) string { return strings.Join(f, " ") }

// joinIDs stores a list of Message-IDs as the header had them: space separated,
// oldest first.
func joinIDs(ids []string) string { return strings.Join(ids, " ") }

// Parts returns what a Message carries, in the order the MIME tree lists it.
func (m *Mirror) Parts(messageID int64) ([]Part, error) {
	rows, err := m.db.Query(`
		SELECT path, mime_type, filename, disposition, size, content_id
		  FROM parts WHERE message_id = ? ORDER BY path`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Part
	for rows.Next() {
		p := Part{MessageID: messageID}
		if err := rows.Scan(&p.Path, &p.MIMEType, &p.Filename, &p.Disposition, &p.Size, &p.ContentID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Thread returns every Message in a conversation, oldest first, each under one
// Placement — the Inbox one where there is one. A Thread never crosses an
// Account (ADR-0008).
func (m *Mirror) Thread(account string, threadID int64) ([]Row, error) {
	rows, err := m.db.Query(`
		SELECT `+rowFields+`, p.folder
		  FROM placements p JOIN messages m ON m.id = p.message_id
		 WHERE p.account = ? AND m.thread_id = ?
		 ORDER BY m.date, m.id, (p.folder = 'INBOX') DESC, p.folder`, account, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	seen := map[int64]bool{}
	for rows.Next() {
		var folder string
		scan := func(dest ...any) error { return rows.Scan(append(dest, &folder)...) }
		r, err := scanRow(scan, "")
		if err != nil {
			return nil, err
		}
		if seen[r.Message.ID] {
			continue // the same Message under another Placement
		}
		seen[r.Message.ID] = true
		r.Placement.Folder = folder
		out = append(out, r)
	}
	return out, rows.Err()
}

// ThreadSizes counts each named Thread whole: every distinct Message that has a
// Placement, wherever it sits — the number a reader shows, so a listing can
// badge a conversation with its real size and not just the part of it that is
// in the Box being listed. Ids of 0 (a Message that is its own Thread) are
// skipped, and a Thread with one Message is left out of the result.
func (m *Mirror) ThreadSizes(account string, threadIDs []int64) (map[int64]int, error) {
	out := map[int64]int{}
	var ids []int64
	seen := map[int64]bool{}
	for _, id := range threadIDs {
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, account)
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT m.thread_id, count(DISTINCT m.id)
	        FROM messages m JOIN placements p ON p.message_id = m.id
	       WHERE m.account = ? AND m.thread_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)
	       GROUP BY m.thread_id`
	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		if n > 1 {
			out[id] = n
		}
	}
	return out, rows.Err()
}

// BoxCount is one folder's share of the Mirror.
type BoxCount struct {
	Folder string
	Count  int
	Unseen int
}

// BoxCounts is every folder this account holds, with how much is in it. One
// query rather than a Folder call per Box: the answer is a listing, and a
// listing that costs a round trip per row is a listing nobody runs.
func (m *Mirror) BoxCounts(account string) ([]BoxCount, error) {
	rows, err := m.db.Query(`
		SELECT folder, count(*),
		       sum(CASE WHEN ' ' || flags || ' ' LIKE '% \Seen %' THEN 0 ELSE 1 END)
		  FROM placements
		 WHERE account = ?
		 GROUP BY folder
		 ORDER BY folder`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoxCount{}
	for rows.Next() {
		var b BoxCount
		if err := rows.Scan(&b.Folder, &b.Count, &b.Unseen); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// labelPrefix keeps remembered label names out of the way of every other key
// the meta table holds.
const labelPrefix = "label:"

// RememberLabel records a label that exists. A label in use is already visible
// in the flags of the mail carrying it; this is only how one nobody has applied
// yet survives until they do.
func (m *Mirror) RememberLabel(name string) error {
	_, err := m.db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, labelPrefix+name, name)
	return err
}

// ForgetLabel drops a remembered name. The keyword on any mail still carrying
// it is not touched: removing a label from mail is a write to the server, and
// this is only the list.
func (m *Mirror) ForgetLabel(name string) error {
	_, err := m.db.Exec(`DELETE FROM meta WHERE key = ?`, labelPrefix+name)
	return err
}

// Labels is every label there is: the keywords actually on mail, and the ones
// that have been created but not used yet. Keywords beginning with a backslash
// are the server's own — \Seen, \Answered — and are not labels anybody chose.
func (m *Mirror) Labels(account string) ([]string, error) {
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !systemKeyword(name) {
			seen[name] = true
		}
	}
	rows, err := m.db.Query(`SELECT DISTINCT flags FROM placements WHERE account = ? AND flags <> ''`, account)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var flags string
		if err := rows.Scan(&flags); err != nil {
			rows.Close()
			return nil, err
		}
		for _, f := range splitFlags(flags) {
			add(f)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	remembered, err := m.db.Query(`SELECT value FROM meta WHERE key LIKE ?`, labelPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer remembered.Close()
	for remembered.Next() {
		var name string
		if err := remembered.Scan(&name); err != nil {
			return nil, err
		}
		add(name)
	}
	if err := remembered.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// systemKeyword reports whether a keyword is the server's rather than a label
// somebody chose. Three namespaces: the IMAP system flags, which begin with a
// backslash; the registered keywords of RFC 5788, which begin with a dollar
// ($Forwarded, $MDNSent); and the spam-training names every provider agrees on
// without anybody having standardised them.
func systemKeyword(name string) bool {
	if strings.HasPrefix(name, `\`) || strings.HasPrefix(name, "$") {
		return true
	}
	switch strings.ToLower(name) {
	case "junk", "nonjunk", "notjunk":
		return true
	}
	return false
}

// Labelled is the mail carrying one label, newest first. It is a Mirror read
// like a Box listing: a keyword is stored beside the Placement it is on.
//
// The Placements are found first and read second, rather than in one join,
// because a Row is assembled from a fixed column list that every other read
// shares — and a listing of at most a page is not worth a second copy of it.
func (m *Mirror) Labelled(account, label string, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.db.Query(`
		SELECT p.folder, p.uid
		  FROM placements p JOIN messages m ON m.id = p.message_id
		 WHERE p.account = ?
		   AND ' ' || p.flags || ' ' LIKE '% ' || ? || ' %'
		 ORDER BY m.date DESC, p.uid DESC
		 LIMIT ?`, account, label, limit)
	if err != nil {
		return nil, err
	}
	type place struct {
		folder string
		uid    uint32
	}
	var places []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.folder, &p.uid); err != nil {
			rows.Close()
			return nil, err
		}
		places = append(places, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []Row{}
	for _, p := range places {
		r, err := m.Row(account, p.folder, p.uid)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
