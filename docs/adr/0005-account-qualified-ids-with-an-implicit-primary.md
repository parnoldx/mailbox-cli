# Account-qualified IDs, with an implicit Primary

A Message is named `[account/]box:uid`. An unqualified ID means the Primary
Account, so every ID that worked when there was one account still works verbatim.
A Secondary Account prefixes: `gmx/INBOX:412`.

Alternatives were `account:box:uid` and an `--account` flag. The flag was rejected
because an ID has to survive being copied out of one command's output and pasted
into another's arguments — agents pass these around as opaque tokens, and a token
that needs a companion flag to mean anything is not one.

Every Mirror row carries an account id from the start. It is one column now and a
migration of every table later.
