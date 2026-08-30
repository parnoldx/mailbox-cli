# Raw iCalendar and vCard are the record

For calendars, task lists and address books the Mirror stores the object's raw
text, with parsed columns beside it as a projection for querying. An edit is
applied to the raw text and PUT back; it is never re-serialised from the columns.

Recurrence overrides, VALARM blocks, attendee state and the X- properties other
clients write are far wider than anything we parse. Rebuilding an object from our
own columns would silently drop whatever we did not model — data loss caused by
this CLI, in someone else's client, with no error anywhere.

All three configured servers (mailbox.org CalDAV, mailbox.org CardDAV, and a
SOGo instance) answer `sync-collection` (RFC 6578) with a real token, so one
token-based algorithm covers them with no ctag fallback. Calendars and task lists
are polled every 10 minutes, address books every 24 hours.

A command over collections also nudges: it asks for a cycle and does not wait for
one, so the reply is the Mirror's as it stands and the reply after it is current.
Rate limited to one nudge per kind per 30 seconds, and per hour for address books.
This is what keeps the poll interval from being the only answer to "I deleted that
todo on my phone and it is still here", without making a read wait on the network —
which ADR-0001 rules out, and which would also teach every agent to run a sync
command before every read.

The timer and the nudge go through one trigger, so DAV cycles are serialised the
way mail cycles are. Two at once would both ask from the same sync token, and the
one that finished second would commit the older answer over the newer one.
