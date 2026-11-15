package mirror

import "strings"

// Correspondent is one address the mailbox has actually exchanged mail with,
// the "seen in mail" layer of recipient autocomplete, behind the address book.
type Correspondent struct {
	Name  string
	Email string
}

// SearchCorrespondents answers words typed into a recipient field with
// addresses the mailbox has sent to or heard from, most recently corresponded
// with first. A word matches anywhere in the address or the name, not only at
// the start: "roe" is how somebody looking for a surname types it, and a
// prefix match on "roe@" alone would not find "Jamie Roe". It reads the
// correspondents cache kept warm by Tx.upsertCorrespondents rather than
// parsing message headers here, so a keystroke never pays for that.
func (m *Mirror) SearchCorrespondents(account, query string, limit int) ([]Correspondent, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	like := "%" + strings.ToLower(query) + "%"
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
