# CONDSTORE sync, without QRESYNC

Each cycle, one `LIST "" "*" RETURN (STATUS (MESSAGES UIDNEXT UIDVALIDITY
HIGHESTMODSEQ))` reports every folder — O(folders), not O(messages), so it scales
to a far larger account than this one. Folders whose HIGHESTMODSEQ moved get
`UID FETCH 1:* (FLAGS) CHANGEDSINCE <n>`, which is O(changes). A `UIDVALIDITY`
change drops the folder's Placements and resyncs them.

Expunges are the gap: without `VANISHED` we are not told what disappeared. We
detect it by comparing STATUS MESSAGES against the Mirror's row count and, on a
mismatch only, diffing `UID SEARCH ALL` (ESEARCH returns compressed ranges).
While a folder is IDLE'd, the seq→UID map we have to keep anyway makes live
expunges exact, so this path runs mainly after the daemon has been down.

QRESYNC would close that gap, and mailbox.org's Dovecot supports it — but
`emersion/go-imap/v2` does not, and cannot be extended to: `command`,
`commandBase` and `beginCommand` are unexported and there is no raw-command
escape hatch, so adding QRESYNC or NOTIFY means vendoring a fork of a beta
library. That is too much standing cost for a narrow win over offline expunges.

**Revisit if** a single folder's UID diff ever costs more than a second.

## Consequences: the watch policy

IDLE holds one connection per selected folder and NOTIFY is unavailable, so IDLE
is spent only where sub-second latency is actually worth something: **Inbox and
Screener on the Primary Account, Inbox on each Secondary.** Screener earns it
because sign-in magic links for new services land there, and a link that arrives
a minute late is a link that has expired. Everything else rides the
`LIST-STATUS` poll. `COMPRESS=DEFLATE` is enabled on these long-lived
connections.


## Consequences: surviving a UIDVALIDITY change

A UIDVALIDITY change invalidates every uid in a folder, but it does not
necessarily mean the mail is gone — a server-side migration can renumber a folder
whose contents are intact. So the reconciler drops **Placements, not Messages**
(ADR-0007): it refetches envelopes for the folder, re-maps them onto existing
Messages by `message_key`, and fetches bodies only for the ones that are genuinely
new. A reset therefore costs an envelope pass over one folder, not a body refetch,
and no local state is lost.

The trigger is *changed*, never *greater than*. RFC 3501 does not promise
monotonicity, even though Dovecot happens to increment.

This is verified against the real server, not assumed. `DELETE` followed by
`CREATE` on a scratch folder provokes it with plain IMAP verbs — measured on
mailbox.org: UIDVALIDITY `1681457875` -> `1681457876`, with uids restarting at 1.
