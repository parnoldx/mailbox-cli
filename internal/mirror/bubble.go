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

// BubblesDueAccount is BubblesDue across every folder of the account, not just
// one. bubble_at is a projection of a flag, not a folder property (ADR-0023),
// so a return-time keyword can sit anywhere — the "if no reply by" reminder
// (see docs/adr) puts one on a Sent copy, not on an Aside placement.
func (m *Mirror) BubblesDueAccount(account string, at time.Time) ([]BubbleRef, error) {
	return m.bubbled(account, "", at.In(time.Local).Format(bubble.ProjectionLayout))
}

// bubbled reads placements with a return time, soonest first. An empty folder
// scans every folder of the account instead of one.
func (m *Mirror) bubbled(account, folder, dueBy string) ([]BubbleRef, error) {
	q := `SELECT p.folder, p.uid, p.message_id, p.bubble_at,
		     m.thread_id, m.subject, m.from_addr
		FROM placements p JOIN messages m ON m.id = p.message_id
	       WHERE p.account = ? AND p.bubble_at IS NOT NULL`
	args := []any{account}
	if folder != "" {
		q += ` AND p.folder = ?`
		args = append(args, folder)
	}
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
