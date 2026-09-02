package mirror

import (
	"database/sql"
	"net/url"
	"strings"
	"time"
)

// hrefKey is the spelling of an object's href the Mirror stores and compares
// by: the root-relative path, which is how a server reports its own objects. A
// locally built href can turn up with a scheme and host on it (it was made from
// a collection URL), and left alone the two spellings of one resource become
// two rows under the UNIQUE (collection_id, href) key.
func hrefKey(href string) string {
	if u, err := url.Parse(strings.TrimSpace(href)); err == nil && u.Path != "" {
		return u.Path
	}
	return href
}

// Collection is one CalDAV or CardDAV collection: a calendar, a task list, or
// an address book. It is named by its display name, because that is what a
// caller has seen and can type (ADR-0010).
type Collection struct {
	ID        int64
	Account   string
	Kind      string // "events" | "tasks" | "cards"
	URL       string
	Name      string
	Color     string
	SyncToken string
	SyncedAt  time.Time
	Count     int
}

// Object is one entry on a Collection. Raw is the record; every other field is
// a projection of it and is rebuilt whenever Raw changes.
type Object struct {
	ID           int64
	CollectionID int64
	Collection   string // the collection's display name, when the query joined it
	Href         string
	ETag         string
	Raw          string
	Kind         string // "event" | "todo" | "other"
	UID          string
	Summary      string
	Location     string
	Description  string
	Status       string
	Emails       []string
	Phones       []string
	Start        time.Time
	End          time.Time
	Due          time.Time
	DueAllDay    bool
	Priority     int
	Completed    time.Time
	AllDay       bool
	Recurring    bool
	RepeatsUntil time.Time
}

// PutCollection records a Collection the server told us about. The sync token
// is not touched here: it belongs to the sync that earned it, and discovery
// must never reset it.
func (m *Mirror) PutCollection(c Collection) (int64, error) {
	_, err := m.db.Exec(`
		INSERT INTO dav_collections (account, kind, url, name, color)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (account, url) DO UPDATE SET
		  kind = excluded.kind, name = excluded.name, color = excluded.color`,
		c.Account, c.Kind, c.URL, c.Name, c.Color)
	if err != nil {
		return 0, err
	}
	var id int64
	err = m.db.QueryRow(`SELECT id FROM dav_collections WHERE account = ? AND url = ?`,
		c.Account, c.URL).Scan(&id)
	return id, err
}

// Collections returns what the Mirror holds, optionally of one kind.
func (m *Mirror) Collections(account, kind string) ([]Collection, error) {
	where := `WHERE account = ?`
	args := []any{account}
	if kind != "" {
		where += ` AND kind = ?`
		args = append(args, kind)
	}
	rows, err := m.db.Query(`
		SELECT c.id, c.kind, c.url, c.name, c.color, c.sync_token, c.synced_at,
		       (SELECT count(*) FROM dav_objects o WHERE o.collection_id = c.id)
		  FROM dav_collections c `+where+` ORDER BY c.kind, c.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c := Collection{Account: account}
		var syncedAt sql.NullString
		if err := rows.Scan(&c.ID, &c.Kind, &c.URL, &c.Name, &c.Color, &c.SyncToken, &syncedAt, &c.Count); err != nil {
			return nil, err
		}
		if syncedAt.Valid {
			c.SyncedAt, _ = time.Parse(time.RFC3339, syncedAt.String)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CollectionNamed finds a Collection by its display name, case-insensitively.
// A name is what a caller types; a URL is not.
func (m *Mirror) CollectionNamed(account, kind, name string) (Collection, error) {
	all, err := m.Collections(account, kind)
	if err != nil {
		return Collection{}, err
	}
	for _, c := range all {
		if strings.EqualFold(c.Name, name) {
			return c, nil
		}
	}
	return Collection{}, ErrNotFound
}

// SetSyncToken stores the token the server gave us, together with the objects
// it describes: the two are committed in one transaction, so a token can never
// claim to cover changes the Mirror did not store.
func (t *Tx) SetSyncToken(collectionID int64, token string) error {
	_, err := t.tx.Exec(
		`UPDATE dav_collections SET sync_token = ?, synced_at = ? WHERE id = ?`,
		token, time.Now().UTC().Format(time.RFC3339), collectionID)
	return err
}

// PutObject stores an object and its projection. The projection is passed in
// rather than derived here: the Mirror stores what it is given, and parsing
// iCalendar is somebody else's job.
func (t *Tx) PutObject(o Object) error {
	o.Href = hrefKey(o.Href)
	_, err := t.tx.Exec(`
		INSERT INTO dav_objects (collection_id, href, etag, raw, kind, uid, summary,
		                         location, description, status, emails, phones,
		                         starts_at, ends_at,
		                         due_at, due_all_day, priority, completed_at,
		                         all_day, recurring, repeats_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (collection_id, href) DO UPDATE SET
		  etag = excluded.etag, raw = excluded.raw, kind = excluded.kind,
		  uid = excluded.uid, summary = excluded.summary, location = excluded.location,
		  description = excluded.description, status = excluded.status,
		  emails = excluded.emails, phones = excluded.phones,
		  starts_at = excluded.starts_at, ends_at = excluded.ends_at,
		  due_at = excluded.due_at, due_all_day = excluded.due_all_day,
		  priority = excluded.priority, completed_at = excluded.completed_at,
		  all_day = excluded.all_day, recurring = excluded.recurring,
		  repeats_until = excluded.repeats_until`,
		o.CollectionID, o.Href, o.ETag, o.Raw, o.Kind, o.UID, o.Summary,
		o.Location, o.Description, o.Status,
		joinValues(o.Emails), joinValues(o.Phones),
		nullTime(o.Start), nullTime(o.End),
		nullTime(o.Due), o.DueAllDay, o.Priority, nullTime(o.Completed),
		o.AllDay, o.Recurring, nullTime(o.RepeatsUntil))
	return err
}

// DeleteObject removes an object the server says is gone.
func (t *Tx) DeleteObject(collectionID int64, href string) error {
	_, err := t.tx.Exec(`DELETE FROM dav_objects WHERE collection_id = ? AND href = ?`,
		collectionID, hrefKey(href))
	return err
}

// ClearCollection empties a Collection, which is what a sync token the server
// no longer recognises means: start again from nothing.
func (t *Tx) ClearCollection(collectionID int64) error {
	_, err := t.tx.Exec(`DELETE FROM dav_objects WHERE collection_id = ?`, collectionID)
	return err
}

const objectColumns = `
	SELECT o.id, o.collection_id, c.name, o.href, o.etag, o.raw, o.kind, o.uid,
	       o.summary, o.location, o.description, o.status, o.emails, o.phones,
	       o.starts_at, o.ends_at,
	       o.due_at, o.due_all_day, o.priority, o.completed_at,
	       o.all_day, o.recurring, o.repeats_until
	  FROM dav_objects o JOIN dav_collections c ON c.id = o.collection_id`

// ObjectsIn returns the objects of one kind that could have something in the
// window [from, to). "Could" is the operative word: a repeating entry is one
// row and only expanding it says where its instances actually fall, so this
// narrows the work and does not do it.
func (m *Mirror) ObjectsIn(account, kind string, from, to time.Time, collection string) ([]Object, error) {
	where := `WHERE c.account = ? AND c.kind = ? AND (
	            (o.recurring = 0 AND (o.starts_at IS NULL OR o.starts_at < ?)
	                            AND (o.ends_at IS NULL OR o.ends_at > ?))
	         OR (o.recurring = 1 AND (o.repeats_until IS NULL OR o.repeats_until >= ?)
	                            AND (o.starts_at IS NULL OR o.starts_at < ?)))`
	args := []any{account, kind,
		to.UTC().Format(time.RFC3339), from.UTC().Format(time.RFC3339),
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)}
	if collection != "" {
		where += ` AND c.name = ? COLLATE NOCASE`
		args = append(args, collection)
	}
	return m.objects(where+` ORDER BY o.starts_at`, args...)
}

// Object reads one object by the id a listing printed.
func (m *Mirror) Object(account string, id int64) (Object, error) {
	out, err := m.objects(`WHERE c.account = ? AND o.id = ?`, account, id)
	if err != nil {
		return Object{}, err
	}
	if len(out) == 0 {
		return Object{}, ErrNotFound
	}
	return out[0], nil
}

// ObjectByUID finds an object by the UID the server and every other client know
// it as. That is the id a write has to use, because ours is local.
func (m *Mirror) ObjectByUID(account, uid string) (Object, error) {
	out, err := m.objects(`WHERE c.account = ? AND o.uid = ?`, account, uid)
	if err != nil {
		return Object{}, err
	}
	if len(out) == 0 {
		return Object{}, ErrNotFound
	}
	return out[0], nil
}

// ObjectByHref reads one object of a collection back after it was written.
func (m *Mirror) ObjectByHref(account string, collectionID int64, href string) (Object, error) {
	out, err := m.objects(`WHERE c.account = ? AND o.collection_id = ? AND o.href = ?`,
		account, collectionID, hrefKey(href))
	if err != nil {
		return Object{}, err
	}
	if len(out) == 0 {
		return Object{}, ErrNotFound
	}
	return out[0], nil
}

// Todos returns the entries of the task lists, soonest due first and undated
// last: an undated Todo is the ordinary kind and it should not crowd out the
// ones with a date. Completed ones are left out unless they are asked for,
// because a task list is a list of what is left.
func (m *Mirror) Todos(account, collection string, includeDone bool) ([]Object, error) {
	where := `WHERE c.account = ? AND c.kind = 'tasks'`
	args := []any{account}
	if collection != "" {
		where += ` AND c.name = ? COLLATE NOCASE`
		args = append(args, collection)
	}
	if !includeDone {
		where += ` AND o.status != 'COMPLETED' AND o.completed_at IS NULL`
	}
	// Due first, then by what matters: a priority nobody set sorts as medium,
	// because an unranked todo is neither more nor less urgent than one somebody
	// called normal.
	return m.objects(where+` ORDER BY o.due_at IS NULL, o.due_at,
		CASE WHEN o.priority = 0 THEN 5 ELSE o.priority END, o.summary`, args...)
}

// joinValues and splitValues keep a list of addresses or numbers in one column.
// They are separated by newlines rather than spaces because a phone number has
// spaces in it — "+49 30 000" stored space-separated comes back as three
// numbers, which is how a contact ends up with a phone book instead of a phone.
func joinValues(values []string) string { return strings.Join(values, "\n") }

func splitValues(s string) []string {
	var out []string
	for _, v := range strings.Split(s, "\n") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Contacts searches the address books. Every term has to match somewhere on the
// card — the name, an address, a number, the organisation — which is how a
// caller finds somebody by the half of the name they remember.
//
// It is a LIKE over the projection rather than an FTS index: an address book is
// a few hundred rows, and a second index to keep true would cost more than the
// scan it saves (ADR-0009 keeps search local either way).
func (m *Mirror) Contacts(account, query string, limit int) ([]Object, error) {
	where := `WHERE c.account = ? AND c.kind = 'cards'`
	args := []any{account}
	for _, term := range strings.Fields(query) {
		where += ` AND (o.summary LIKE ? COLLATE NOCASE OR o.emails LIKE ? COLLATE NOCASE
		              OR o.phones LIKE ? COLLATE NOCASE OR o.location LIKE ? COLLATE NOCASE)`
		like := "%" + term + "%"
		args = append(args, like, like, like, like)
	}
	if limit <= 0 {
		limit = 25
	}
	args = append(args, limit)
	return m.objects(where+` ORDER BY o.summary LIMIT ?`, args...)
}

// Hrefs returns every href the Mirror holds for a Collection, which is what a
// diff against the server needs.
func (m *Mirror) Hrefs(collectionID int64) (map[string]string, error) {
	rows, err := m.db.Query(`SELECT href, etag FROM dav_objects WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var href, etag string
		if err := rows.Scan(&href, &etag); err != nil {
			return nil, err
		}
		out[href] = etag
	}
	return out, rows.Err()
}

func (m *Mirror) objects(where string, args ...any) ([]Object, error) {
	rows, err := m.db.Query(objectColumns+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Object
	for rows.Next() {
		var o Object
		var start, end, due, completed, until sql.NullString
		var emails, phones string
		if err := rows.Scan(&o.ID, &o.CollectionID, &o.Collection, &o.Href, &o.ETag, &o.Raw,
			&o.Kind, &o.UID, &o.Summary, &o.Location, &o.Description, &o.Status,
			&emails, &phones,
			&start, &end, &due, &o.DueAllDay, &o.Priority, &completed,
			&o.AllDay, &o.Recurring, &until); err != nil {
			return nil, err
		}
		o.Emails, o.Phones = splitValues(emails), splitValues(phones)
		for _, f := range []struct {
			src sql.NullString
			dst *time.Time
		}{{start, &o.Start}, {end, &o.End}, {due, &o.Due}, {completed, &o.Completed}, {until, &o.RepeatsUntil}} {
			if f.src.Valid {
				*f.dst, _ = time.Parse(time.RFC3339, f.src.String)
			}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
