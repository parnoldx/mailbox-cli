package mirror

// Removing an Account or a Collection is a prune, not a rebuild. Deleting the
// whole Mirror file would also work and is the wrong tool: ADR-0013 is about a
// schema change, and using it here would cold-start every other account —
// minutes of refetching — to forget one.

// ForgetCollection drops a Collection and everything on it.
//
// Nothing pruned Collections until this existed. Discover's comment has said
// since the ninth slice that a Collection which disappears from the server is
// dropped with its objects; PutCollection is an upsert with no delete beside
// it, so a calendar removed on the server stayed in the Mirror forever.
func (m *Mirror) ForgetCollection(account, url string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		DELETE FROM dav_objects WHERE collection_id IN
		  (SELECT id FROM dav_collections WHERE account = ? AND url = ?)`, account, url); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM dav_collections WHERE account = ? AND url = ?`,
		account, url); err != nil {
		return err
	}
	return tx.Commit()
}

// ForgetAccount drops everything one Account put in the Mirror. The Outbox is
// not touched: it is a separate file that is never dropped (ADR-0013), and a
// mail in it may already have gone out.
// ForgetFolder drops one Box and everything in it: its placements, the
// messages only it held, and its sync state. The prune that goes with
// discovery — a Box the server no longer offers (deleted in webmail, or thrown
// into Trash with its folder) leaves the Mirror here instead of haunting
// search as a ghost.
func (m *Mirror) ForgetFolder(account, folder string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Placements go first: what a message still has left afterwards decides
	// whether the message itself stays.
	if _, err := tx.Exec(`DELETE FROM placements WHERE account = ? AND folder = ?`,
		account, folder); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM messages_fts WHERE rowid IN (SELECT id FROM messages
		   WHERE account = ? AND id NOT IN (SELECT message_id FROM placements WHERE account = ?))`,
		`DELETE FROM message_refs WHERE message_id IN (SELECT id FROM messages
		   WHERE account = ? AND id NOT IN (SELECT message_id FROM placements WHERE account = ?))`,
		`DELETE FROM parts WHERE message_id IN (SELECT id FROM messages
		   WHERE account = ? AND id NOT IN (SELECT message_id FROM placements WHERE account = ?))`,
		`DELETE FROM messages WHERE account = ? AND id NOT IN
		   (SELECT message_id FROM placements WHERE account = ?)`,
		`DELETE FROM sync_journal WHERE account = ? AND folder = ?`,
		`DELETE FROM folders WHERE account = ? AND name = ?`,
	} {
		if _, err := tx.Exec(stmt, account, account); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FolderNames is every Box the Mirror holds for an account, whether or not it
// has mail in it.
func (m *Mirror) FolderNames(account string) ([]string, error) {
	rows, err := m.db.Query(`SELECT name FROM folders WHERE account = ?`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// KeepFolders prunes the Mirror down to the Boxes discovery still offers. A
// folder that left the server — deleted in webmail, or inside Trash — is
// forgotten here, so no listing or search keeps seeing it.
func (m *Mirror) KeepFolders(account string, keep []string) error {
	held, err := m.FolderNames(account)
	if err != nil {
		return err
	}
	stay := make(map[string]bool, len(keep))
	for _, name := range keep {
		stay[name] = true
	}
	for _, name := range held {
		if !stay[name] {
			if err := m.ForgetFolder(account, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Mirror) ForgetAccount(account string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM messages_fts WHERE rowid IN (SELECT id FROM messages WHERE account = ?)`,
		`DELETE FROM message_refs WHERE message_id IN (SELECT id FROM messages WHERE account = ?)`,
		`DELETE FROM parts WHERE message_id IN (SELECT id FROM messages WHERE account = ?)`,
		`DELETE FROM dav_objects WHERE collection_id IN
		   (SELECT id FROM dav_collections WHERE account = ?)`,
		`DELETE FROM dav_collections WHERE account = ?`,
		`DELETE FROM placements WHERE account = ?`,
		`DELETE FROM messages WHERE account = ?`,
		`DELETE FROM correspondents WHERE account = ?`,
		`DELETE FROM folders WHERE account = ?`,
		`DELETE FROM routing WHERE account = ?`,
		`DELETE FROM routing_script WHERE account = ?`,
		`DELETE FROM sync_journal WHERE account = ?`,
	} {
		if _, err := tx.Exec(stmt, account); err != nil {
			return err
		}
	}
	return tx.Commit()
}
