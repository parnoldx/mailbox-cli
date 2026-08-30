# Three connection roles, and never a cached SELECT

The IMAP driver keeps a **control** connection that never selects a mailbox, a
**work** connection that selects and fetches, and one **watch** connection per
IDLE'd folder. It re-selects before every operation rather than caching the
selection, and it reconnects and retries once on any failure.

This looks wasteful. It is the result of four bugs that the fake driver passed
and the first run against a real Dovecot did not.

**Detection must not share a connection with fetching.** RFC 3501 says STATUS
"SHOULD NOT be used on the currently selected mailbox", and Dovecot means it: a
connection with the folder selected answers LIST-STATUS from its own view, so a
flag set by another client stayed invisible to us forever. The Mirror looked
permanently up to date while being permanently wrong.

**A watcher cannot share a connection either**, because a connection running
IDLE may issue no other command until DONE.

**A cached SELECT goes stale in two different ways.** A connection only learns
about new mail when the server is allowed to send untagged updates, so `1:*` in
a UID FETCH silently excludes anything delivered since the last command. And
once the folder has been deleted and recreated elsewhere, Dovecot answers
`NO Mailbox was deleted under us` — in which state a UID SEARCH returns an empty
set *with no error*, which is indistinguishable from an empty folder. A fresh
SELECT costs one round trip on a warm connection and makes both impossible.

**Errors are not worth classifying.** A deleted folder drops one connection and
poisons another, and both surface as ordinary command failures. Every operation
on the work connection is a read, so reconnecting and retrying once is free of
consequence and covers the whole class.

## Consequences

Two connections per account plus one per watched folder — four for a Primary
with Inbox and Screener watched. Well inside any provider's limit, and the cost
of a cycle is unchanged: detection is still one LIST-STATUS round trip.

None of this is reachable from the scripted fake, which is the point of running
the gates against a real server as well (docs/DESIGN.md).
