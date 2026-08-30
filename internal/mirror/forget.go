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
