package mirror

import (
	"database/sql"
	"time"

	"mailbox/internal/pickup"
)

// PickupRef is one Placement the Daemon has taken a code out of, and when the
// mail arrived. The arrival instant is the whole state: the expiry is derived
// from it rather than written down, so changing the configured window applies
// to mail already flagged instead of only to the next one.
type PickupRef struct {
	Folder       string
	UID          uint32
	MessageID    int64
	Subject      string
	InternalDate time.Time
}

// Pickups is every placement carrying the pickup keyword, oldest first.
//
// ponytail: a LIKE over the flags column, no index. A Pickup lives about
// fifteen minutes and there are a handful a day, so this scans a few rows out
// of a few thousand once a minute. If it ever needs to be a real query it wants
// the bubble_at treatment — a projected column written wherever flags are
// (ADR-0023) — but a column would be four times the code for no measurable
// difference at this size.
func (m *Mirror) Pickups(account string) ([]PickupRef, error) {
	rows, err := m.db.Query(`
		SELECT p.folder, p.uid, p.message_id, m.subject,
		       COALESCE(p.internaldate, m.date)
		  FROM placements p JOIN messages m ON m.id = p.message_id
		 WHERE p.account = ? AND p.flags LIKE ?
		 ORDER BY COALESCE(p.internaldate, m.date)`, account, "%"+pickup.Keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PickupRef
	for rows.Next() {
		var r PickupRef
		var internal sql.NullString
		if err := rows.Scan(&r.Folder, &r.UID, &r.MessageID, &r.Subject, &internal); err != nil {
			return nil, err
		}
		// The server's own instant, falling back to the Date: header. A
		// Placement with neither would never expire, and a Pickup that never
		// expires is the one outcome this feature must not have.
		if internal.Valid {
			r.InternalDate, _ = time.Parse(time.RFC3339, internal.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
