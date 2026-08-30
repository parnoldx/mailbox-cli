# Search is local only

`search` is answered by SQLite FTS5 over the Mirror. There is no fall-through to
IMAP `SEARCH`, and no flag to ask for one.

Keeping a server search alongside the local one would reintroduce exactly the two
divergent code paths that ADR-0001 rejected, with two different sets of matching
semantics for the same command. Local search also does things IMAP cannot:
ranking, proximity, and scoring across every folder in one query.

Trash is not searchable, because ADR-0003 does not mirror it. That is deliberate
on both sides: deleted mail is meant to stop turning up. Wanting a message back
from Trash is a job for another client, not a reason to keep a second search
engine.
