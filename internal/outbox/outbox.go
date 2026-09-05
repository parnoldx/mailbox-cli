// Package outbox is the durable send queue: the one place where the Mirror
// leads the server rather than following it (ADR-0004).
//
// It is a separate SQLite file from the Mirror and it is never deleted. The
// Mirror is derived from a server that still holds the original and can always
// be rebuilt; a mail that has been composed but not yet sent exists nowhere
// else (ADR-0013).
package outbox

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// State is where a mail has got to. The states exist to answer one question
// after a crash: may this mail be sent again?
//
//	queued  -> composed and durable, not yet handed to SMTP. Safe to send.
//	sending -> handed to SMTP by a process that then died. NOT safe to send:
//	           it may have gone out. It becomes held and waits for a person.
//	sent    -> SMTP accepted it. It must never go to SMTP again; all that is
//	           left is filing the copy in Sent, which may be retried freely.
//	filed   -> the copy is in Sent. Finished.
//	held    -> needs a decision: `outbox retry` sends it, `outbox cancel` drops it.
type State string

const (
	Queued  State = "queued"
	Sending State = "sending"
	Sent    State = "sent"
	Filed   State = "filed"
	Held    State = "held"
)

// Item is one mail in the queue, with the finished bytes that will be sent.
type Item struct {
	ID         int64
	Account    string
	MessageKey string // the Message-ID we minted, without angle brackets
	From       string
	Recipients []string
	Subject    string
	Raw        []byte
	State      State
	Attempts   int
	LastError  string
	CreatedAt  time.Time
	SentAt     time.Time
	FiledAt    time.Time
	Box        string // where the copy was filed
	UID        uint32
	// NotBefore is a "send later": queued but not handed to SMTP until this
	// instant. Zero means send it as soon as a drain sees it, which is every
	// mail before this existed.
	NotBefore time.Time
}

// Outbox is the queue.
type Outbox struct {
	db   *sql.DB
	path string
}

// schemaVersion is the Outbox's own. Unlike the Mirror's, a mismatch here is an
// error rather than a reason to delete the file: this is not derived state, and
// there is nowhere to fetch it from again.
const schemaVersion = 2

const schema = `
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE outbox (
  id          INTEGER PRIMARY KEY,
  account     TEXT    NOT NULL,
  message_key TEXT    NOT NULL,
  from_addr   TEXT    NOT NULL,
  recipients  TEXT    NOT NULL,
  subject     TEXT    NOT NULL DEFAULT '',
  raw         BLOB    NOT NULL,
  state       TEXT    NOT NULL,
  attempts    INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT    NOT NULL DEFAULT '',
  created_at  TEXT    NOT NULL,
  sent_at     TEXT,
  filed_at    TEXT,
  box         TEXT    NOT NULL DEFAULT '',
  uid         INTEGER NOT NULL DEFAULT 0,
  not_before  TEXT
);

CREATE INDEX outbox_state ON outbox(state, id);
`

// Open opens the Outbox, creating it if it is not there.
func Open(path string) (*Outbox, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	var version int
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&version)
	switch {
	case err == nil && version == schemaVersion:
		return &Outbox{db: db, path: path}, nil
	case err == nil && version == 1 && schemaVersion == 2:
		// Never deleted, so this is where a migration goes: schema 2 only adds
		// a "send later" instant, nothing existing changes meaning.
		if _, aerr := db.Exec(`ALTER TABLE outbox ADD COLUMN not_before TEXT`); aerr != nil {
			db.Close()
			return nil, fmt.Errorf("migrate outbox %s to schema 2: %w", path, aerr)
		}
		if _, aerr := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, schemaVersion); aerr != nil {
			db.Close()
			return nil, fmt.Errorf("migrate outbox %s to schema 2: %w", path, aerr)
		}
		return &Outbox{db: db, path: path}, nil
	case err == nil:
		db.Close()
		return nil, fmt.Errorf("outbox %s is at schema %d, this build speaks %d", path, version, schemaVersion)
	}
	if _, execErr := db.Exec(schema); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("create outbox schema: %w", execErr)
	}
	if _, execErr := db.Exec(`INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, schemaVersion); execErr != nil {
		db.Close()
		return nil, execErr
	}
	return &Outbox{db: db, path: path}, nil
}

// Close closes the file.
func (o *Outbox) Close() error { return o.db.Close() }

// ErrNotFound means there is no such row.
var ErrNotFound = errors.New("not found")

// Enqueue makes a composed mail durable. Nothing reaches SMTP until this has
// committed: a mail that is lost because the daemon died between "accepted" and
// "sent" is the one failure this queue exists to prevent.
func (o *Outbox) Enqueue(it Item) (int64, error) {
	var notBefore any
	if !it.NotBefore.IsZero() {
		notBefore = it.NotBefore.UTC().Format(time.RFC3339)
	}
	res, err := o.db.Exec(`
		INSERT INTO outbox (account, message_key, from_addr, recipients, subject, raw, state, created_at, not_before)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Account, it.MessageKey, it.From, strings.Join(it.Recipients, " "), it.Subject,
		it.Raw, string(Queued), now(), notBefore)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Claim marks a mail as being handed to SMTP right now. It is written before
// the transaction opens, so a process that dies inside SMTP leaves a row that
// says so.
func (o *Outbox) Claim(id int64) error {
	return o.move(id, []State{Queued}, `
		UPDATE outbox SET state = 'sending', attempts = attempts + 1, last_error = ''
		 WHERE id = ?`)
}

// MarkSent records that SMTP accepted the mail. From here on it may be filed
// again and again, but never sent again.
func (o *Outbox) MarkSent(id int64) error {
	return o.move(id, []State{Sending}, `UPDATE outbox SET state = 'sent', sent_at = datetime('now') WHERE id = ?`)
}

// MarkFiled records the copy that landed in Sent, and where.
func (o *Outbox) MarkFiled(id int64, box string, uid uint32) error {
	_, err := o.db.Exec(
		`UPDATE outbox SET state = 'filed', filed_at = datetime('now'), box = ?, uid = ? WHERE id = ?`,
		box, uid, id)
	return err
}

// Requeue puts a mail SMTP refused back in the queue with the reason. An error
// from SMTP means it was not accepted, so trying again is safe — that is what
// separates this from a process that died mid-send.
func (o *Outbox) Requeue(id int64, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, err := o.db.Exec(`UPDATE outbox SET state = 'queued', last_error = ? WHERE id = ?`, msg, id)
	return err
}

// NoteError records a failure that did not change the state, such as a copy
// that could not be filed.
func (o *Outbox) NoteError(id int64, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, err := o.db.Exec(`UPDATE outbox SET last_error = ? WHERE id = ?`, msg, id)
	return err
}

// HoldInterrupted turns every row left mid-send by a dead process into a held
// one, and returns them. It is deliberately not a retry: a mail whose SMTP
// transaction was cut off may have been delivered, and sending it again would
// deliver it twice. Nobody can tell which from here, so the queue says so and
// waits to be told (ADR-0017).
func (o *Outbox) HoldInterrupted() ([]Item, error) {
	items, err := o.byState(Sending)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if _, err := o.db.Exec(`
		UPDATE outbox SET state = 'held',
		       last_error = 'the daemon stopped while this was at the smtp server; it may have been delivered'
		 WHERE state = 'sending'`); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].State = Held
	}
	return items, nil
}

// Pending is everything waiting for SMTP, oldest first.
func (o *Outbox) Pending() ([]Item, error) { return o.byState(Queued) }

// Unfiled is everything SMTP took but that has no copy in Sent yet. Filing is
// the retryable half of a send.
func (o *Outbox) Unfiled() ([]Item, error) { return o.byState(Sent) }

// PendingFor and UnfiledFor are the same, for one account. The Outbox is one
// file for every account — a mail is a mail — but each account's own SMTP
// server is the only one that can send its mail.
func (o *Outbox) PendingFor(account string) ([]Item, error) {
	return o.byStateAndAccount(Queued, account)
}

func (o *Outbox) UnfiledFor(account string) ([]Item, error) {
	return o.byStateAndAccount(Sent, account)
}

func (o *Outbox) byStateAndAccount(s State, account string) ([]Item, error) {
	if account == "" {
		return o.byState(s)
	}
	return o.query(
		`WHERE state = ? AND account = ? AND (not_before IS NULL OR not_before <= ?) ORDER BY id`,
		string(s), account, now())
}

// Retry puts a held mail back in the queue. Being told to send it is the only
// thing that can move it: the daemon will not decide that on its own.
func (o *Outbox) Retry(id int64) error {
	return o.move(id, []State{Held, Queued}, `UPDATE outbox SET state = 'queued', last_error = '' WHERE id = ?`)
}

// Cancel drops a mail that has not been sent. One that has cannot be recalled,
// and saying so is more use than deleting the record of it.
func (o *Outbox) Cancel(id int64) error {
	it, err := o.Get(id)
	if err != nil {
		return err
	}
	if it.State == Sent || it.State == Filed {
		return fmt.Errorf("#%d was already sent — it cannot be cancelled", id)
	}
	_, err = o.db.Exec(`DELETE FROM outbox WHERE id = ?`, id)
	return err
}

// Get reads one row.
func (o *Outbox) Get(id int64) (Item, error) {
	items, err := o.query(`WHERE id = ?`, id)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, ErrNotFound
	}
	return items[0], nil
}

// List is the whole queue, newest first, for a caller asking what is in it.
// Filed mail stays: "did that go out?" is a question worth being able to answer.
func (o *Outbox) List(limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 50
	}
	return o.query(`ORDER BY id DESC LIMIT ?`, limit)
}

func (o *Outbox) byState(s State) ([]Item, error) {
	return o.query(`WHERE state = ? AND (not_before IS NULL OR not_before <= ?) ORDER BY id`, string(s), now())
}

// move applies a state transition, refusing one that does not start where it
// says it does. A queue whose states are only advisory is not a queue.
func (o *Outbox) move(id int64, from []State, stmt string) error {
	it, err := o.Get(id)
	if err != nil {
		return err
	}
	ok := false
	for _, f := range from {
		if it.State == f {
			ok = true
		}
	}
	if !ok {
		want := make([]string, 0, len(from))
		for _, f := range from {
			want = append(want, string(f))
		}
		return fmt.Errorf("#%d is %s, not %s", id, it.State, strings.Join(want, " or "))
	}
	_, err = o.db.Exec(stmt, id)
	return err
}

func (o *Outbox) query(where string, args ...any) ([]Item, error) {
	rows, err := o.db.Query(`
		SELECT id, account, message_key, from_addr, recipients, subject, raw, state,
		       attempts, last_error, created_at, sent_at, filed_at, box, uid, not_before
		  FROM outbox `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var rcpt, state, created string
		var sentAt, filedAt, notBefore sql.NullString
		if err := rows.Scan(&it.ID, &it.Account, &it.MessageKey, &it.From, &rcpt, &it.Subject,
			&it.Raw, &state, &it.Attempts, &it.LastError, &created, &sentAt, &filedAt,
			&it.Box, &it.UID, &notBefore); err != nil {
			return nil, err
		}
		it.State = State(state)
		if rcpt != "" {
			it.Recipients = strings.Fields(rcpt)
		}
		it.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if sentAt.Valid {
			it.SentAt, _ = time.Parse("2006-01-02 15:04:05", sentAt.String)
		}
		if filedAt.Valid {
			it.FiledAt, _ = time.Parse("2006-01-02 15:04:05", filedAt.String)
		}
		if notBefore.Valid {
			it.NotBefore, _ = time.Parse(time.RFC3339, notBefore.String)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
