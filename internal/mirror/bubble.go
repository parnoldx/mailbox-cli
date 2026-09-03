package mirror

import (
	"time"

	"mailbox/internal/bubble"
)

// BubbleRef is one bubbled placement: enough to take its Thread and return the
// whole conversation to the Inbox.
type BubbleRef struct {
	Folder    string
	UID       uint32
	MessageID int64
	ThreadID  int64
	Subject   string
	From      string
	BubbleAt  time.Time
}

// Bubbled is every placement in a folder that carries a return time, soonest
// due first. It is the read behind `mailbox bubble list`, and it is grouped to
// one row per Thread by the caller.
func (m *Mirror) Bubbled(account, folder string) ([]BubbleRef, error) {
	return m.bubbled(account, folder, "")
}

// BubblesDue is every placement in a folder whose return time is at or before
// `at`, soonest first. The scan is by wall clock, not a per-message wake timer:
// a Daemon that was down when a return came due catches it on the first tick
// after startup, and a Mirror rebuilt mid-wait repopulated bubble_at from the
// keyword so nothing is lost.
func (m *Mirror) BubblesDue(account, folder string, at time.Time) ([]BubbleRef, error) {
	return m.bubbled(account, folder, at.In(time.Local).Format(bubble.ProjectionLayout))
}

func (m *Mirror) bubbled(account, folder, dueBy string) ([]BubbleRef, error) {
	q := `SELECT p.folder, p.uid, p.message_id, p.bubble_at,
		     m.thread_id, m.subject, m.from_addr
		FROM placements p JOIN messages m ON m.id = p.message_id
	       WHERE p.account = ? AND p.folder = ? AND p.bubble_at IS NOT NULL`
	args := []any{account, folder}
	if dueBy != "" {
		q += ` AND p.bubble_at <= ?`
		args = append(args, dueBy)
	}
	q += ` ORDER BY p.bubble_at, p.uid`
	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BubbleRef
	for rows.Next() {
		var r BubbleRef
		var at string
		if err := rows.Scan(&r.Folder, &r.UID, &r.MessageID, &at,
			&r.ThreadID, &r.Subject, &r.From); err != nil {
			return nil, err
		}
		r.BubbleAt, _ = time.ParseInLocation(bubble.ProjectionLayout, at, time.Local)
		out = append(out, r)
	}
	return out, rows.Err()
}
