package mirror

import (
	"database/sql"
	"errors"
	"time"
)

// Route is one decision the Routing carries: this sender's mail goes there.
// Box is empty when the destination is not a Box at all — a blocked sender's
// mail is discarded before it lands anywhere.
type Route struct {
	Address string
	To      string
	Box     string
}

// RoutingScript is the Sieve script as the server last gave it to us, with the
// moment it did. It is the record; every Route above is a projection of it.
type RoutingScript struct {
	Name string
	Raw  string
	// Active is whether the server has this script switched on. A stored script
	// that is not active routes nothing.
	Active   bool
	SyncedAt time.Time
}

// PutRouting replaces what the Mirror holds about the Routing: the script, and
// the decisions read out of it. Both in one transaction, because a projection
// that disagrees with the record it came from is worse than no projection.
func (m *Mirror) PutRouting(account, name, raw string, active bool, routes []Route) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM routing WHERE account = ?`, account); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO routing (account, address, dest, box) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range routes {
		if _, err := stmt.Exec(account, r.Address, r.To, r.Box); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO routing_script (account, name, raw, active, synced_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (account) DO UPDATE SET
		  name = excluded.name, raw = excluded.raw, active = excluded.active,
		  synced_at = excluded.synced_at`,
		account, name, raw, active, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// Routing returns every decision, ordered the way the script matches them:
// blocked senders first, then the Inbox, then the piles.
func (m *Mirror) Routing(account string) ([]Route, error) {
	rows, err := m.db.Query(`
		SELECT address, dest, box FROM routing WHERE account = ?
		 ORDER BY CASE dest WHEN 'block' THEN 0 WHEN 'inbox' THEN 1 WHEN 'paper' THEN 2 ELSE 3 END,
		          address`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Route{}
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.Address, &r.To, &r.Box); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RoutingScript returns the script itself. ErrNotFound means the Mirror has
// never read one — the daemon has not looked yet, or the account has no such
// script.
func (m *Mirror) RoutingScript(account string) (RoutingScript, error) {
	var s RoutingScript
	var at sql.NullString
	err := m.db.QueryRow(`SELECT name, raw, active, synced_at FROM routing_script WHERE account = ?`,
		account).Scan(&s.Name, &s.Raw, &s.Active, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if at.Valid {
		s.SyncedAt, _ = time.Parse(time.RFC3339, at.String)
	}
	return s, nil
}

// RowsFrom returns a folder's rows whose From header contains address, newest
// first. It is a filter and not an answer: a header is `Bob <bob@example.com>`
// and a substring of it is not proof that it names that sender, so the caller —
// which is the one that can parse a header — decides which of these really are
// from them.
func (m *Mirror) RowsFrom(account, folder, address string, limit int) ([]Row, error) {
	rows, err := m.db.Query(rowColumns+`
		 WHERE p.account = ? AND p.folder = ?
		   AND lower(m.from_addr) LIKE '%' || lower(?) || '%'
		 ORDER BY m.date DESC, p.uid DESC
		 LIMIT ?`, account, folder, address, limit)
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
