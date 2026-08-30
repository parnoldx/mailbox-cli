# The Mirror is the read model

Every read command answers from the daemon's local SQLite Mirror, unconditionally.
The network is touched only by the sync loop and by writes, so a read is ~1ms,
works offline, and can never hang on a TLS handshake — which matters because the
callers are coding agents and bar widgets that run constantly.

The alternative was a cache with fall-through to IMAP on a miss. Rejected: it
keeps two code paths for every read forever, and it makes every command a
potential network stall, which is the thing we are trying to remove.

## Consequences

The Mirror can be stale, so freshness has to be part of the output contract
rather than something the caller assumes.

Freshness is per domain, not per Mirror. Mail and the collections are brought up
to date by different loops that can be minutes or hours apart, so every reply
reports the age of the data that reply answered from, and whether a cycle for it
is running. A caller told only "behind" cannot tell whether that matters or
whether asking again would help; those two facts are what make the notice
actionable rather than merely honest.
