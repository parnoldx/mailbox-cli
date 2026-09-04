package mirror

import "strings"

// Correspondent is one address the mailbox has actually exchanged mail with —
// the "seen in mail" layer of recipient autocomplete, behind the address book.
type Correspondent struct {
	Name  string
	Email string
}

// SearchCorrespondents answers a prefix typed into the To/Cc/Bcc field with
// addresses the mailbox has sent to or heard from, most recently corresponded
// with first. It reads the correspondents cache kept warm by
// Tx.upsertCorrespondents rather than parsing message headers here, so a
// keystroke never pays for that.
func (m *Mirror) SearchCorrespondents(account, prefix string, limit int) ([]Correspondent, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	like := strings.ToLower(prefix) + "%"
	rows, err := m.db.Query(`
		SELECT name, email FROM correspondents
		WHERE account = ? AND (email LIKE ? OR name LIKE ? COLLATE NOCASE)
		ORDER BY last_seen DESC
		LIMIT ?`, account, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Correspondent
	for rows.Next() {
		var c Correspondent
		if err := rows.Scan(&c.Name, &c.Email); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
