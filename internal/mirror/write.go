package mirror

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"mailbox/internal/bubble"
)

// Intent is what a sync step wrote down before it touched the network.
type Intent struct {
	Folder    string
	Intent    string
	StartedAt time.Time
}

// WriteIntent records that a folder is about to be synced. It is written
// outside the apply transaction on purpose: it has to survive the crash that
// the transaction rolls back (ADR-0015).
func (m *Mirror) WriteIntent(account, folder, intent string) error {
	_, err := m.db.Exec(`
		INSERT INTO sync_journal (account, folder, intent, started_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (account, folder) DO UPDATE SET intent = excluded.intent, started_at = excluded.started_at`,
		account, folder, intent, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Intents returns sync steps that were started and never committed. Each one
// means "redo this folder from its stored modseq", which is always safe because
// planning is idempotent.
func (m *Mirror) Intents(account string) ([]Intent, error) {
	rows, err := m.db.Query(`SELECT folder, intent, started_at FROM sync_journal WHERE account = ?`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		var i Intent
		var at string
		if err := rows.Scan(&i.Folder, &i.Intent, &at); err != nil {
			return nil, err
		}
		i.StartedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, i)
	}
	return out, rows.Err()
}

// Tx is one atomic step of a sync. Everything a folder learns in a cycle lands
// in a single transaction together with the modseq that says it was learnt, so
// the two can never disagree.
type Tx struct {
	tx      *sql.Tx
	account string
}

// Begin starts an apply transaction for an account.
func (m *Mirror) Begin(account string) (*Tx, error) {
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, account: account}, nil
}

// Rollback abandons the transaction. Safe to call after Commit.
func (t *Tx) Rollback() { _ = t.tx.Rollback() }

// Commit applies everything the step learnt.
func (t *Tx) Commit() error { return t.tx.Commit() }

// DropPlacements removes every placement in a folder, leaving the Messages
// behind. This is what a UIDVALIDITY change does: the uids are meaningless now,
// but the mail they pointed at may well still be there (ADR-0006).
func (t *Tx) DropPlacements(folder string) error {
	_, err := t.tx.Exec(`DELETE FROM placements WHERE account = ? AND folder = ?`, t.account, folder)
	return err
}

// DeletePlacements removes specific uids, which is what an expunge means.
func (t *Tx) DeletePlacements(folder string, uids []uint32) error {
	stmt, err := t.tx.Prepare(`DELETE FROM placements WHERE account = ? AND folder = ? AND uid = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, uid := range uids {
		if _, err := stmt.Exec(t.account, folder, uid); err != nil {
			return err
		}
	}
	return nil
}

// UpsertMessage inserts a Message or finds the existing one with the same key.
// hasBody reports whether the Mirror already holds its text — the caller uses
// that to avoid refetching bodies it has already paid for.
func (t *Tx) UpsertMessage(m Message) (id int64, hasBody bool, err error) {
	var state string
	err = t.tx.QueryRow(`SELECT id, body_state FROM messages WHERE account = ? AND message_key = ?`,
		t.account, m.Key).Scan(&id, &state)
	if err == nil {
		return id, state == "mirrored", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	res, err := t.tx.Exec(`
		INSERT INTO messages (account, message_key, date, subject, from_addr, to_addr,
		                      cc_addr, in_reply_to, references_, body_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
		t.account, m.Key, nullTime(m.Date), m.Subject, m.From, m.To, m.Cc,
		joinIDs(m.InReplyTo), joinIDs(m.References))
	if err != nil {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	// A Message is searchable by its envelope from the moment it exists. Its
	// body arrives later, or not at all if it is already Mirrored elsewhere.
	if _, err := t.tx.Exec(
		`INSERT INTO messages_fts (rowid, subject, addresses, body) VALUES (?, ?, ?, '')`,
		id, m.Subject, strings.TrimSpace(m.From+" "+m.To+" "+m.Cc)); err != nil {
		return 0, false, err
	}
	return id, false, t.thread(id, m)
}

// thread files a new Message into a conversation. It looks both ways: at the
// Messages this one references, and at the Messages that reference it — a reply
// is routinely mirrored before the mail it answers, and a Thread that only
// grows forwards would leave the two apart forever.
//
// Where the links reach several existing Threads, they were one conversation
// all along and are merged into the oldest. Subjects are never used: a shared
// subject is not a Thread, it is a coincidence (ADR-0008).
func (t *Tx) thread(id int64, m Message) error {
	refs := map[string]bool{}
	for _, r := range append(append([]string(nil), m.InReplyTo...), m.References...) {
		if r != "" && r != m.Key {
			refs[r] = true
		}
	}
	for ref := range refs {
		if _, err := t.tx.Exec(
			`INSERT OR IGNORE INTO message_refs (message_id, ref_key) VALUES (?, ?)`, id, ref); err != nil {
			return err
		}
	}

	threads := map[int64]bool{}
	collect := func(query string, args ...any) error {
		rows, err := t.tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tid int64
			if err := rows.Scan(&tid); err != nil {
				return err
			}
			if tid != 0 {
				threads[tid] = true
			}
		}
		return rows.Err()
	}
	// Forwards: the Messages this one names.
	for ref := range refs {
		if err := collect(
			`SELECT thread_id FROM messages WHERE account = ? AND message_key = ?`, t.account, ref); err != nil {
			return err
		}
	}
	// Backwards: the Messages that name this one.
	if err := collect(`
		SELECT m.thread_id FROM message_refs r JOIN messages m ON m.id = r.message_id
		 WHERE m.account = ? AND r.ref_key = ?`, t.account, m.Key); err != nil {
		return err
	}

	thread := id // a Message with no links is its own Thread
	for tid := range threads {
		if tid < thread {
			thread = tid
		}
	}
	for tid := range threads {
		if tid == thread {
			continue
		}
		if _, err := t.tx.Exec(
			`UPDATE messages SET thread_id = ? WHERE account = ? AND thread_id = ?`,
			thread, t.account, tid); err != nil {
			return err
		}
	}
	_, err := t.tx.Exec(`UPDATE messages SET thread_id = ? WHERE id = ?`, thread, id)
	return err
}

// ThreadOf reports which Thread a Message was filed into — the id of the oldest
// Message known to be in the conversation. Zero means the Mirror does not hold
// the Message.
func (t *Tx) ThreadOf(id int64) (int64, error) {
	var tid int64
	err := t.tx.QueryRow(`SELECT thread_id FROM messages WHERE id = ?`, id).Scan(&tid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return tid, err
}

// SetBody stores a Message's text and marks it mirrored. indexed is the text
// Search should match on — the plain part, or the HTML rendered down to text.
// It is passed in rather than derived here because the Mirror stores what it is
// given; deriving it is the job of the layer that already decodes charsets.
func (t *Tx) SetBody(id int64, plain, html, indexed string) error {
	if _, err := t.tx.Exec(
		`UPDATE messages SET text_plain = ?, text_html = ?, body_state = 'mirrored' WHERE id = ?`,
		plain, html, id); err != nil {
		return err
	}
	_, err := t.tx.Exec(`UPDATE messages_fts SET body = ? WHERE rowid = ?`, indexed, id)
	return err
}

// PutParts records what a Message carries. The set is replaced rather than
// merged: a refetched Message describes itself completely, and a part that has
// gone from the structure has gone.
func (t *Tx) PutParts(messageID int64, parts []Part) error {
	if _, err := t.tx.Exec(`DELETE FROM parts WHERE message_id = ?`, messageID); err != nil {
		return err
	}
	for _, p := range parts {
		if _, err := t.tx.Exec(`
			INSERT INTO parts (message_id, path, mime_type, filename, disposition, size, content_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (message_id, path) DO UPDATE SET
			  mime_type = excluded.mime_type, filename = excluded.filename,
			  disposition = excluded.disposition, size = excluded.size,
			  content_id = excluded.content_id`,
			messageID, p.Path, p.MIMEType, p.Filename, p.Disposition, p.Size, p.ContentID); err != nil {
			return err
		}
	}
	return nil
}

// PutPlacement inserts or updates where a Message sits. bubble_at is derived
// from the flags here rather than passed in, so it is a true projection of the
// `$bubble-*` keyword — the one place a bubbled instant is written.
func (t *Tx) PutPlacement(p Placement) error {
	_, err := t.tx.Exec(`
		INSERT INTO placements (account, folder, uid, message_id, flags, bubble_at, internaldate, size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account, folder, uid) DO UPDATE SET
		  message_id = excluded.message_id, flags = excluded.flags,
		  bubble_at = excluded.bubble_at,
		  internaldate = excluded.internaldate, size = excluded.size`,
		t.account, p.Folder, p.UID, p.MessageID, joinFlags(p.Flags),
		bubble.Projected(p.Flags), nullTime(p.InternalDate), p.Size)
	return err
}

// Placement reads where a Message sits, inside the transaction that is about to
// move it. A move keeps everything but the folder and the uid, so the row has
// to be read before it is deleted.
func (t *Tx) Placement(folder string, uid uint32) (Placement, error) {
	p := Placement{Folder: folder, UID: uid}
	var flags string
	var internal sql.NullString
	err := t.tx.QueryRow(
		`SELECT message_id, flags, internaldate, size FROM placements
		  WHERE account = ? AND folder = ? AND uid = ?`, t.account, folder, uid).
		Scan(&p.MessageID, &flags, &internal, &p.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.Flags = splitFlags(flags)
	if internal.Valid {
		p.InternalDate, _ = time.Parse(time.RFC3339, internal.String)
	}
	return p, nil
}

// SetFlags updates a placement's flags, which is all a CONDSTORE flag change
// is. bubble_at is re-derived from the new flags in the same statement: a
// `$bubble-*` keyword added, moved or stripped in any client is what schedules,
// re-times or cancels a return.
func (t *Tx) SetFlags(folder string, uid uint32, flags []string) error {
	_, err := t.tx.Exec(
		`UPDATE placements SET flags = ?, bubble_at = ? WHERE account = ? AND folder = ? AND uid = ?`,
		joinFlags(flags), bubble.Projected(flags), t.account, folder, uid)
	return err
}

// SaveFolder writes the folder's new sync state. Committed with the rows it
// describes, never before them.
func (t *Tx) SaveFolder(f FolderState) error {
	_, err := t.tx.Exec(`
		INSERT INTO folders (account, name, uidvalidity, uidnext, highestmodseq, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (account, name) DO UPDATE SET
		  uidvalidity = excluded.uidvalidity, uidnext = excluded.uidnext,
		  highestmodseq = excluded.highestmodseq, synced_at = excluded.synced_at`,
		t.account, f.Name, f.UIDValidity, f.UIDNext, f.HighestModSeq,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// ClearIntent removes the journal entry, in the same transaction as the work it
// described.
func (t *Tx) ClearIntent(folder string) error {
	_, err := t.tx.Exec(`DELETE FROM sync_journal WHERE account = ? AND folder = ?`, t.account, folder)
	return err
}

// UIDs returns every uid the Mirror holds for a folder, ordered.
func (m *Mirror) UIDs(account, folder string) ([]uint32, error) {
	rows, err := m.db.Query(`SELECT uid FROM placements WHERE account = ? AND folder = ? ORDER BY uid`, account, folder)
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

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// HasOtherPlacement reports whether a Message already sits in this folder under
// a different uid. Two messages in one folder sharing a Message-ID are two
// messages, however much the header claims otherwise; the same Message-ID in
// two folders is one Message with two Placements, which is the point.
func (t *Tx) HasOtherPlacement(messageID int64, folder string, uid uint32) (bool, error) {
	var n int
	err := t.tx.QueryRow(
		`SELECT count(*) FROM placements WHERE account = ? AND folder = ? AND message_id = ? AND uid != ?`,
		t.account, folder, messageID, uid).Scan(&n)
	return n > 0, err
}
