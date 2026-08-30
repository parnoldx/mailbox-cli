# Mirror every text part, and no attachments

The Mirror holds each Message's envelope, BODYSTRUCTURE and every `text/*` part,
for every folder. It never holds an Attachment.

We picked this after measuring the real account rather than guessing. Summing
BODYSTRUCTURE part sizes over the four heaviest Archive folders: 555 messages,
218 MB on the server, **7.4 MB of text** — about 18 MB extrapolated to the whole
account against ~610 MB of raw size. Attachments are ~99% of the bytes and
essentially all of the cost; text is free. Restricting bodies to the routing
boxes would have saved little and cost offline reads and local body search.

**The built Mirror measures 29 MB**, not the projected 18 MB: 66 Boxes, 1,377
Messages, 26.5 MB of text of which only 3.3 MB is plain and 23 MB is HTML. The
four-folder sample was plain-heavy, so the projection was low by half. The
decision is unaffected — 29 MB against 610 MB is the same argument — but the
projection is replaced here by the measurement rather than left standing.

## Consequences

The rule this gives the CLI, which is the point of it: **list, search and count
never touch the network; naming one specific object may.** Under this ADR that
reduces to Attachments — nobody expects `attachment save` to work offline.

Trash (2,629 messages, 203 MB) is excluded entirely, and this is settled rather
than provisional: Trash is where things go to stop being findable. Deleted mail
not appearing in results is the behaviour, not a gap in it.

## The Mirror stores decoded text, not wire bytes

A text part arrives in a transfer encoding (quoted-printable, base64) and in its
own charset. The Mirror decodes both before storing, so `text_plain` holds
"wär" and not "w=C3=A4r".

Worth stating because the mistake is invisible: an undecoded part is non-empty,
so it passes any check that the body arrived. The first version of this code
stored wire bytes and every test passed. Decoding at the edge also means no
reader of the Mirror — CLI, widget, future Search index — has to know that mail
has transfer encodings at all.

