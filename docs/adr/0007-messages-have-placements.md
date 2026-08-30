# Messages have Placements

A Message is keyed by `(account, rfc822_message_id)`. Where it currently sits —
box, uid, flags — is a separate Placement row. A move updates a Placement instead
of deleting and re-inserting a Message, a mail sent to yourself has two
Placements, and an Archive filing is recognisably the same Message as the Inbox
copy that preceded it.

The obvious alternative was to make `(account, folder, uid)` the whole identity.
It is simpler, but it loses a message's history at every move, and it gives
threading and dedup no natural key to work with.

Message-IDs are missing or duplicated often enough in real mail that they cannot
be trusted alone: when one is absent or collides, we synthesise an id from
`(folder, uid)`, degrading that message to the simpler model rather than
complicating every row for the sake of the exceptions.
