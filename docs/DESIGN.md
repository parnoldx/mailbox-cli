# Design

How the pieces fit. Vocabulary is in [CONTEXT.md](../CONTEXT.md); every decision
here is justified in [docs/adr](./adr) and not re-argued.

## Shape

```
    mailbox (CLI)          widget          widget
          \                  |               /
           `------- unix socket, NDJSON ----'
                            |
                        [ Daemon ]
                            |
                +-----------+-----------+
                |                       |
          sync engines            command handlers
          (only writers)          (readers, + writes)
                \                       /
                 `------ mirror -------'
                      (SQLite, WAL)
                            |
            IMAP / SMTP / CalDAV / CardDAV / ManageSieve
```

The Daemon is the only process that opens a network connection or writes the
Mirror. The CLI is a socket client and nothing else (ADR-0012).

## Packages

```
cmd/mailbox/           argv -> cli
internal/
  cli/                 parsing, the output envelope, dispatch over the socket
  daemon/              socket server, request routing, push fan-out, lifecycle
  mirror/              THE seam: schema, domain types, queries. All SQL lives here.
  sync/                orchestration: what to sync, when, in what order
    mailsync/          the IMAP reconciler (drives a Driver, writes via mirror)
    davsync/           the sync-collection reconciler
  imapdrv/             Driver impl over go-imap/v2   (+ scripted fake for tests)
  davdrv/              CalDAV/CardDAV: discovery, sync-collection, multiget
  smtpdrv/             go-smtp
  sievedrv/            ManageSieve: the Routing script, fetched and stored whole
  routing/             the Sieve script's format, and the four sender lists in it
  outbox/              durable send queue, its own SQLite file
  message/             go-message: composing what we send
  vcal/                iCalendar: the projection, and expanding a rule
  setup/               the wizard: config, systemd units, skill, Collection discovery
  htmlmd/ terminal/ format/ ids/ imaputf7/ config/   ported from omamail
skill/                 the agent skill, embedded so setup can install it
```

`mirror` hands back domain types (`Message`, `Placement`, `Thread`, `Event`). No
package above it writes SQL, and no package below it knows a command exists. The
sync engines are the only writers of mirrored state; command handlers write only
through the write-through path in ADR-0004.

The `Driver` interfaces are the test seam (see Acceptance): `mailsync` talks to a
`Driver`, and the scripted fake drives it through states no real server can be
asked for on demand. go-imap's in-memory server is not an option here — it has no
CONDSTORE at all.

The fake must be able to produce, at a scripted point in the cycle:

- a UIDVALIDITY change that **keeps** the messages under new uids (the migration
  case — the only thing ADR-0006's re-map protection is actually for, and the one
  a real server will not do for you)
- a UIDVALIDITY change **between detect and fetch**, so the plan was made against
  one incarnation and the fetch lands on the next
- an expunge arriving over IDLE as a sequence number, mid-fetch
- a HIGHESTMODSEQ that goes backwards, or jumps
- a connection dropped after the fetch and before the commit
- a folder whose MESSAGES count disagrees with the uid set it returns
- messages with a missing or duplicated Message-ID (the ADR-0007 synthetic-key
  path)

## Schema

One SQLite file, WAL, `schema_version` in `meta`; on mismatch the file is deleted
and rebuilt (ADR-0013).

```
accounts        id, name, role(primary|secondary), imap/smtp settings
folders         account_id, name, uidvalidity, uidnext, highestmodseq,
                mirrored_count, watch(idle|poll|never), synced_at

messages        id, account_id, message_key, date, subject,
                from/to/cc, in_reply_to, references, thread_id,
                text_plain, text_html, body_state
placements      account_id, folder, uid, message_id, flags, internaldate, size
parts           message_id, path, mime_type, filename, size   -- metadata only
messages_fts    fts5(subject, addresses, body)  content=messages

dav_collections id, account_id, url, kind(events|tasks|cards), display_name,
                sync_token, synced_at
dav_objects     collection_id, href, etag, raw, + parsed projection columns

routing         account, address, dest(inbox|feed|paper|block), box
routing_script  account, name, raw, active, synced_at

sync_journal    account_id, scope, intent, started_at
```

`message_key` is the RFC822 Message-ID, or a synthetic `folder:uid` when it is
absent or collides (ADR-0007). `parts` holds attachment *metadata* so
`attachment list` is a Mirror read; the bytes are never stored (ADR-0003). `raw`
on `dav_objects` is the record and the columns beside it are a projection
(ADR-0010), and `routing_script.raw` stands in the same relation to `routing`
(ADR-0019).

The Outbox is a **separate** file and is never dropped (ADR-0013).

## The mail sync cycle

Per account, one cycle:

1. **Detect** — one `LIST "" "*" RETURN (STATUS (MESSAGES UIDNEXT UIDVALIDITY
   HIGHESTMODSEQ))`. O(folders). Compare against `folders`.
2. **Plan** — for each folder, one of:
   - `uidvalidity` differs → **resync**: drop this folder's placements (never the
     messages), refetch envelopes, re-map onto existing messages by `message_key`,
     fetch bodies only for what is genuinely new.
   - `highestmodseq` advanced → **incremental**: `UID FETCH 1:* (FLAGS)
     CHANGEDSINCE n`, plus envelope/bodystructure/text for uids ≥ old `uidnext`.
   - `messages` disagrees with `mirrored_count` after that → **expunge diff**:
     `UID SEARCH ALL`, diff, delete the missing placements.
   - otherwise → nothing.
3. **Journal** — write the intent to `sync_journal` before touching the network.
4. **Apply** — fetch, then write placements, messages, parts and FTS rows in one
   transaction that also advances `highestmodseq`/`uidnext` and clears the
   journal entry.

Fetching text has one trap worth naming. A FETCH lists body sections *once* and
applies them to every message in the set, so pooling all the parts of all the
messages into a single request asks for parts×messages sections — for a folder
of 260 mails, a hundred thousand of them, and it does not return. Messages are
therefore grouped by the shape of their MIME tree first, which is a handful of
round trips whatever the folder size.

A crash therefore leaves either the old modseq with the old rows, or the new
modseq with the new rows — never an advanced modseq over a half-fetched folder.
A journal entry found at startup means "redo this folder from its stored modseq",
which is always safe because step 2 is idempotent. This is mbsync's idea, not its
code (ADR-0015).

**Mirrored and Watched are different sets.** Every mirrored Box is reconciled on
every cycle, from the single detection pass above. Watched is the subset that
also gets an IDLE connection. Conflating the two is easy and costs you every
unwatched Box silently never syncing — with one Box mirrored the bug is
invisible, which is exactly when it gets written.

The Boxes are discovered with `LIST`, not configured. A Box created in webmail
should simply appear, and a name copied by hand is how the CardDAV collection
came to point at a 2-entry scratch address book instead of `Kontakte`.

Live, IDLE runs on Inbox and Screener of the Primary and Inbox of each Secondary
(ADR-0006). Because go-imap reports expunges as *sequence numbers*, the reconciler
keeps a seq→uid map for each IDLE'd folder; that makes live expunges exact and
leaves step 2's diff mainly for time the Daemon was down. `COMPRESS=DEFLATE` is
enabled on these connections. Everything else rides the poll.

## The DAV sync cycle

All three configured servers answer `sync-collection` (RFC 6578), so there is one
algorithm and no ctag fallback. Per Collection: `REPORT sync-collection` with the
stored token, asking for the object data in the same request; an empty token
returns everything and the token together. What a server names without sending is
fetched with one multiget. Calendars and task lists every 10 minutes, address
books every 24 hours (ADR-0010). A write does not wait for any of that: it goes
to the server and updates the Mirror from the ack (ADR-0004).

The requests are ours. go-webdav's CalDAV client has no sync-collection at all,
and its multiget hands back a *parsed* calendar — re-encoding that to store it
would throw away the record (ADR-0010, ADR-0015).

Two things the real servers do that the RFCs allow and a fake would not have
shown: one collection answers `403 <valid-sync-token/>` to the token it issued a
request earlier, so it resyncs from nothing every cycle; and a sync answer can
name the same href twice, which upserting by `(collection, href)` makes a
non-event.

## Socket contract

NDJSON, one object per line each way, over `$XDG_RUNTIME_DIR/mailbox.sock`,
mode 0600, socket-activated.

```
-> {"id":"7","cmd":["box","view"],"args":{"positional":"inbox","limit":50}}
<- {"id":"7","ok":true,"data":[…],"mirror":{"synced_at":"…","behind":false,
                                            "connected":true}}
<- {"event":"mail.changed","account":"primary","box":"inbox"}
```

`status` also carries `problems`: the short list of things this program needs a
person for — a config that will not load, a server refusing a password, Held
mail — and a `problem.changed` push says to re-read it. `reload` is the one verb
with no CLI command behind it: `mailbox setup` sends it after writing the config
so the Daemon does not wait for the minute's poll (ADR-0021).

Every reply carries a `mirror` block; `connected:false` is **not** an error exit,
because a Behind Mirror still answers (ADR-0001). Pushes carry no data — a widget
that receives one re-reads (ADR-0011). With no Daemon the CLI exits
`daemon_required` (ADR-0012).

## Setup

The wizard is the only place a human is asked anything, and it must not ask for a
URL. It authenticates, then **enumerates**: IMAP `LIST` for folders and special-use
flags, `PROPFIND Depth:1` on the CalDAV and CardDAV roots for Collections. It
shows what it found with display names and item counts and lets you pick, marking
`schedule-inbox`/`schedule-outbox` and similar as not selectable.

This is not polish. The current omamail config has `carddav_home` pointing at
`Gesammelte Adressen` (2 vCards) instead of `Kontakte` — a hand-copied URL that
has been silently wrong. Discovery matching on display name is the fix, and the
wizard is where it belongs.

Setup owns the whole install rather than only the account: it writes
`config.toml`, the two systemd units and the agent skill, and it enables the
socket. What it does *not* do is touch the Mirror. It writes the config, nudges
the Daemon over the socket, and watches the first cycle as any other client would
(ADR-0021) — so the progress on screen is the Daemon's real one rather than a
private rehearsal against the same file.

The order of that first sync is therefore the Daemon's cold start: the Inbox,
then the Boxes the Routing files beside it, then everything else with Archive
last, so useful answers exist in seconds rather than after the whole ~18 MB.

A second run asks nothing. It prints what is here — the accounts, the calendars
with excluded ones marked, the units, the skill and the Routing — and offers to
add one, remove one, or repair what has drifted. Adding asks the kind first, a
mail account or a calendar, and a calendar is found by asking a server rather
than by a URL like everything else here. The Primary Account cannot be removed:
that is an uninstall, not a removal.

The Daemon does the opposite: it opens the socket *before* its first cycle, not
after. A cold start touches every Box and takes minutes, and making callers wait
for it would deny them a Behind Mirror — the one answer ADR-0001 says they may
always have. An empty result with `behind: true` is a better answer than a
blocked socket.

**Cycles never overlap.** The poll fires every minute and a cold start runs for
several, so a naive timer starts a second cycle inside the first. That second
cycle plans against half-written state, decides folders the first one has
already finished still need a resync, and does them again. The Daemon therefore
runs cycles one at a time from a depth-one trigger channel: several nudges
arriving during a cycle coalesce into one cycle after it, which is all they can
ever mean.

This was found by running it, not by reasoning about it. The damage was small
only because the re-map protection held: a redundant resync of a 260-message
folder refetched exactly one body.

## Acceptance: the first slice

Inbox on the Primary Account only — envelopes, text parts, flags — into the
Mirror, with `mailbox box view inbox` reading it. No writes, no DAV, no threading,
no search. Done when all five hold:

1. Cold start from an empty Mirror produces the correct folder.
2. A message delivered while IDLE is held appears within a second.
3. A flag changed in another client converges within one poll.
4. **A message expunged while the Daemon was stopped is gone after restart.**
5. **A UIDVALIDITY change forces a clean resync.**

4 and 5 are the gates. They are the failure modes ADR-0006 knowingly left to the
diff path, and they are why `imapdrv` has a scripted fake rather than a real
server behind it.

Both are also reproducible against the real server, which the scripted fake does
not excuse us from doing at least once. On a scratch folder `INBOX/mailbox-selftest`
that the test creates and destroys:

- **gate 4** — append two messages, stop the Daemon, `UID STORE \Deleted` +
  `EXPUNGE` one of them, restart, assert it is gone from the Mirror.
- **gate 5** — append a message, `DELETE` the folder, `CREATE` it again, append
  again. Measured on mailbox.org this moves UIDVALIDITY (`1681457875` ->
  `1681457876`) and restarts uids at 1. Assert the Mirror resyncs cleanly and
  keeps the Messages it already had.

That makes gate 5 a live conformance test rather than only a fake-driver one, and
it is cheap enough to run in CI against a real account.

    go test -tags live ./internal/imapdrv/ -v

Both suites pass. The live run is not a formality: it caught four defects that
the fake passed cleanly, all of them in the space between what the RFC permits
and what Dovecot actually does — see ADR-0016, and the CONDSTORE-without-ENABLE
note in `imapdrv.Dial`. A fake tests the algorithm; only a server tests the
assumption.

## Acceptance: the second slice

Reading one Message out of the Mirror: `mailbox message view [box:]uid` prints
its headers and its text, `--json` gives the envelope. Still no writes — reading
a Message does not mark it `\Seen`. That is a write-through and belongs with
ADR-0004's slice, and until it exists an agent reading mail must not change what
a human sees in another client.

The body is a rendering decision, not a transport one. A Message with a plain
part is shown as it stands; an HTML-only one is rendered to Markdown by the
`htmlmd` port, because a caller of this CLI should never have to parse HTML to
find out what a mail says. Converting HTML we did not need to is how a signature
becomes a wall of markup, so plain wins whenever it is there. Both go through
`terminal.SanitizeText` first: a mail body is untrusted input and the thing
reading it is a terminal.

Done when all four hold:

1. A bare uid reads the Inbox; `box:uid` reads any mirrored Box, case-insensitively.
2. An HTML-only Message comes back as Markdown, not as markup.
3. A uid the Mirror does not hold exits `not_found` (2) — an expunged uid is an
   ordinary thing to ask a Mirror that may be Behind.
4. A Message with two Placements says so, so its other Box can be found.

`message view` also reports `body_state`. A Message whose envelope is Mirrored
but whose text is not yet fetched is a state the reconciler really produces, and
an empty body that does not say why is the wrong answer to it.

## Acceptance: the third slice

Changing something: `mailbox seen ID...`, `mailbox unseen ID...`,
`mailbox move ID... --to BOX`, `mailbox trash ID...`. Each blocks on the server
and writes the Mirror from the ack in one transaction, so exit 0 means it
happened and the next read sees it (ADR-0004). Ids are the ones a listing
printed, and one command may name several Boxes — the write is grouped by Box,
one round trip each.

The Mirror stores what the server said, not what was asked for. A STORE is
therefore silent and the flags are read back: a server is not obliged to put the
uid in the untagged FETCH a STORE provokes, and a flag written against the wrong
uid is worse than the round trip it saves. A MOVE carries its answer in COPYUID,
which is why the destination Placement can be written immediately; a server
without UIDPLUS leaves it to the next cycle, and the write asks for one.

Trash is not a mirrored Box, so `trash` takes the Message out of the Mirror
rather than parking it somewhere that is never reconciled again.

Done when all five hold:

1. `seen` then a read shows the Message read, with no cycle in between.
2. `move` reports the id the Message now has, and its text survives the move —
   the Message is one row, the Placement is another (ADR-0007).
3. `trash` removes it from the Mirror.
4. A write the server refused leaves the Mirror exactly as it was.
5. A write is tried once: a MOVE is never replayed onto a reconnected
   connection (ADR-0017).

`go test -tags live ./internal/imapdrv/ -run TestLiveWrites` asks the real server
the two questions the fake cannot answer for us: whether the STORE readback
carries the uid, and whether MOVE reports COPYUID. It creates and destroys its
own two folders.

One defect came out of this slice rather than the plan: `cycleLoop` was written
but never started, so `kick` filled the trigger channel and nothing read it.
Every cycle after the startup one — every poll, every IDLE nudge — was dropped.
It only showed up here because a write with no UIDPLUS asks for a cycle and
nothing happened.

## Acceptance: the fourth slice

`mailbox search QUERY [--in BOX] [--from ADDR] [--limit N]`, answered by FTS5
over the Mirror and never by the server (ADR-0009). One line per Message with
the id to read it, the Box it is in, and the text around the match — a result an
agent has to open to find out why it matched is a worse result.

What a caller types is words, not query syntax. Every term is quoted and the
terms are ANDed, so `subject:`, `foo-bar`, `NOT x` and a stray `(` are ordinary
text and none of them can make the query fail to parse. A "quoted phrase"
survives, because that is the one piece of syntax people actually mean.

The index holds the subject, the addresses, and the text a reader would see —
the plain part, or the HTML rendered down to it. Indexing markup instead would
match every newsletter on `table` and `href`. The rendering is derived where the
charsets already are, in the sync engine, and the Mirror stores what it is given
(ADR-0003).

Trash is not searchable twice over: it is never mirrored, and a Message trashed
after it was mirrored loses its Placement, which the join drops.

Done when all five hold:

1. A synced Message is findable by subject and by body text, with no separate
   indexing step.
2. An HTML-only Message matches on its rendered text and not on its tags.
3. A Message in two Boxes is one result, reported under the Inbox.
4. A Message with no Placement left never appears.
5. No input can turn into FTS5 syntax or into an error the caller cannot read.

Schema version 2. The Mirror is rebuilt rather than migrated, which is what
ADR-0013 buys: the index is derived state and one resync repopulates it.

`snippet()` and `bm25()` are only usable where the FTS table is queried
directly — not through a subquery or a window function — so the
one-result-per-Message rule is applied in Go over a rank-ordered join rather
than in SQL. The first version used `row_number() OVER (PARTITION BY ...)` and
SQLite answered `unable to use function snippet in the requested context`.

## Acceptance: the fifth slice

`mailbox thread ID` reads the whole conversation from any Message in it, oldest
first, each under the id that reads it on its own. Threads are built as mail is
mirrored, from References and In-Reply-To, over the whole Account at once — IMAP
`THREAD` cannot link an Inbox message to its reply filed in `Archive/Immo`,
because it only ever sees the selected mailbox (ADR-0008).

ENVELOPE carries In-Reply-To but not References, so the envelope fetch asks for
one header section alongside it. That is one more section on a FETCH that was
already being made, not one more round trip.

Linking looks **both ways**, which is the part that is easy to get wrong. A
reply is routinely mirrored before the mail it answers — the reply is in the
Inbox, the parent is in Sent, and the Boxes sync in whatever order `LIST`
returns them. A Thread that only grew forwards would leave those two apart
forever. `message_refs` holds one row per referenced Message-ID so the lookup
works in either direction, and when the links reach several existing Threads
they were one conversation all along and are merged into the oldest.

Subjects are never used. A shared subject is a coincidence, not a Thread.

Done when all five hold:

1. A reply and its parent are one Thread, whichever arrives first.
2. Two Threads merge when a Message referencing both arrives.
3. Two mails with the same subject and no references stay apart.
4. A Message in two Boxes appears once in its Thread, under the Inbox one.
5. A Message nothing links to is a Thread of one, not an error.

Schema version 3.

The live suite grew a `purge` step while this was being built. `recreate` had
been deleting the scratch folder best-effort — `_ = Delete(...)` — and when that
DELETE was swallowed by a dropped connection the next run counted the previous
run's messages. It showed up first as a one-off "gate 1: 3 rows, want 2" that
would not reproduce, and then as gate 2 counting three new messages where one
was appended. The fixture now selects the folder after recreating it and empties
it for real. Neither failure was in the product, which is exactly why it was
worth chasing: a fixture that lies makes the gates useless.

## Acceptance: the sixth slice

`mailbox attachment list ID` and `mailbox attachment save ID[:INDEX]`. Listing is
a Mirror read like any other; saving is the one read that goes to the server by
design. That is ADR-0003's rule made visible: list, search and count never touch
the network, naming one specific object may.

The `parts` table holds a name, a type, a size and the IMAP part path for
everything a Message carries that is not mirrored text. The path is what makes
the later fetch possible without a second structure walk on the read side. A
part with no filename is still saveable, under a name made from its path and
type.

The Daemon writes the file rather than handing the bytes back over the socket:
same user, same machine, and a 20 MB PDF has no business being base64 inside
NDJSON. The CLI therefore resolves `--output` to an absolute path — with no
`--output` that is the caller's directory — and an existing file is not
overwritten without `--force`.

An attachment id is `[box:]uid:index`, 1-based over the listing. A Message with
exactly one attachment can be named by the Message; one with several cannot, and
the error says which ids to type instead.

Done when all five hold:

1. Listing what a Message carries never calls the server.
2. Saving fetches exactly one part and writes it decoded — a base64 PDF written
   to disk still encoded is not a PDF, and it looks like success.
3. An existing file survives a save without `--force`.
4. A bare `[box:]uid` works when there is one attachment and is refused, with
   the ids spelled out, when there are several.
5. Inline images are listed too, marked by their disposition, because "what is
   in this mail" is a different question from "what did the sender attach".

Schema version 4.

**The live test found a defect that had been there since the first slice.** The
driver asked for `BodyStructure: &imap.FetchItemBodyStructure{}` — go-imap's
non-extended form, which is IMAP `BODY` and carries no Content-Disposition at
all. Every disposition test downstream therefore saw nothing: the new listing
reported an attachment as having no disposition, and, quietly, `textParts` had
been unable to tell an attached `.txt` from the body since slice one. It is
`{Extended: true}` now. The fake could not have caught this — it has no wire
format to be wrong about.

The live fixture also stopped pretending its connection is stable. Deleting and
recreating a folder drops the connection that was looking at it, and until now
only `recreate` handled that; an `append` straight afterwards failed with
`unexpected EOF` and read as a product failure. Every command the other client
issues now reconnects and retries once, and says so in the log.

## Acceptance: the seventh slice

`mailbox send`, `mailbox reply` and `mailbox outbox`. This is the one command
that makes something exist rather than reporting on something that already
does, and it is the only place the Mirror leads the server (ADR-0004).

The order is the whole design. A mail is composed once, written to the **Outbox**
— a separate SQLite file that is never deleted (ADR-0013) — and only then handed
to SMTP. Those same bytes are what SMTP takes and what is APPENDed to Sent, so
the copy can never disagree with what the recipient got. Composition is one step
because a mail rebuilt per step is a mail with two versions of itself.

The states exist to answer one question after a crash: may this be sent again?

```
queued --claim--> sending --smtp ok--> sent --append--> filed
   ^                  |                                    
   |                  +--smtp said no--> queued (with the reason)
   |                  +--we died------->  held  (waits for a person)
   +--`outbox retry`--------------------------+
```

`sending` at startup is the only interesting one. SMTP returning an error means
the mail was *not* accepted, so requeueing is safe; a process that died inside
the transaction knows nothing, and a mail that may already be in someone's inbox
must not be sent again on a hunch. Those are **held** and reported, and only
`mailbox outbox retry` moves them (ADR-0017, the same rule that stops a MOVE
being replayed). Filing the copy is the opposite kind of operation: the mail has
already gone, so a failed APPEND is retried on every drain and never re-sends.

The copy in Sent is mirrored by the ordinary cycle rather than by a second write
path — APPENDUID says where it landed, the Box it landed in is synced there and
then, and the id `send` prints is readable immediately. That also means a reply
is threaded by the code that threads everything else, from the References it
carries (ADR-0008).

Bcc is in the SMTP envelope and in the Outbox row, and in the message nowhere. A
Bcc header written into the mail is carried by the copy in Sent, and one forward
of that copy is how a blind copy stops being blind.

Done when all six hold:

1. What is composed survives being read: an umlaut in the subject and the body
   comes back as itself, and an attachment comes back byte for byte.
2. Nothing reaches SMTP that is not already durable, and a mail SMTP refused is
   still there, with the reason, and goes out on the next drain.
3. A mail interrupted mid-send is never sent again on the daemon's own
   initiative.
4. A reply goes to the sender, carries In-Reply-To and References, and lands in
   its parent's Thread; `--all` copies everyone except us.
5. `send` reports the id of the filed copy, and that id reads back out of the
   Mirror without waiting for a poll.
6. A body goes out as `multipart/alternative`: the `text/plain` part is what the
   caller wrote, the `text/html` part is it rendered from Markdown. `--body-html`
   supplies the HTML directly. A forward stays `text/plain` (ADR-0022).

Schema version 5: `messages` gains `cc_addr`. Reply-to-all is built from Cc, and
a Cc that is not mirrored would have to be fetched again to answer the mail.

**The end-to-end run found a defect the unit tests were blind to.** A mail with
an attachment carried `charset=utf-8` on its text part; a mail without one
carried no Content-Type at all, and a text part with no charset *is* us-ascii by
definition. So a plain reply went out as base64-encoded UTF-8 declared as ASCII,
and came back with one replacement character per byte — "Test — same thread"
read as "Test ??? same thread" in our own Mirror. It survived the compose tests
because the reader they parse with does not apply the charset it was told, which
is the whole lesson: a round trip through a library that shares your assumption
proves nothing. The test now asserts on the composed bytes (`charset=utf-8`,
`quoted-printable`, and the em dash present as `=E2=80=94`) for both shapes of
mail, and the real one reads back with its umlauts, its em dash and its € sign.

The live fixture stopped deleting its scratch folder between tests. DELETE +
CREATE drops every connection that was looking at the folder and leaves the
server serving a stale incarnation to the next one; `TestLiveAttachment` failed
counting a previous test's message under a uid that no longer existed. Setup now
creates the folder if it is missing and empties it, and cleanup empties it
again. Only gate 5 deletes it, because UIDVALIDITY is what gate 5 is about. That
is the third time this fixture has produced a failure that was not in the
product, and the first time the fix removes the mechanism rather than retrying
around it.

`go test -tags live ./internal/imapdrv/ -run TestLiveSend` sends one mail from
the account to itself over real submission, files the copy with APPEND, fetches
it back, waits for delivery through a real MTA, and then deletes the delivered
mail. It is what says the composer produces something a server will take: 963
bytes went out, `APPENDUID` came back on the copy, the attachment returned
identical, and the mail was in the Inbox nine seconds later.

End to end on the real account: `mailbox send --to me --subject "… — Grüße"
--attach beleg.pdf` printed `filed as Sent:1292`, `message view Sent:1292` read
it straight back out of the Mirror, `attachment save` returned the file byte for
byte, `reply Sent:1292` filed `Sent:1294` and `thread` showed both under one
conversation, and `outbox` listed both as filed.

## Acceptance: the eighth slice

The calendars. `mailbox agenda [--days N] [--from DATE] [--calendar NAME]`,
`mailbox calendar list`, `mailbox event view ID` — all of them Mirror reads, all
of them offline (ADR-0001).

**Nothing is configured by URL.** Discovery is three requests:
`current-user-principal`, then the calendar and address book homes, then one
`PROPFIND Depth:1` per home. What comes back is display names, colours and
component sets, and *that* decides what a collection is: a calendar advertising
`VTODO` and not `VEVENT` is a task list whatever it is called. Running it against
the real account is the argument for it — the old config called `Einkauf` a
calendar and it is a task list, and both `Kontakte` (142 cards) and the
`Gesammelte Adressen` scratch list the config had been pointing at for years
came back side by side, named (ADR-0010).

A collection on another provider — a work calendar with its own credentials —
cannot be discovered from our server, so it is configured, and the driver set
routes each request to the server whose host owns the URL. A server that is down
hides its own collections and nothing else.

The requests are written here rather than taken from go-webdav, whose CalDAV
client has no sync-collection at all, and whose multiget hands back a *parsed*
calendar — re-encoding that to store it would throw away the record (ADR-0010).
Four XML documents is a smaller thing to own than a mismatch with the one
operation the design is built on (ADR-0015).

**A repeating event is one row.** A rule with no end has no finite expansion to
store, and the window belongs to the question rather than to the calendar, so
`dav_objects` holds the rule and `agenda` expands it on the way out. The
projection carries `repeats_until` so a window query can rule out a rule that has
finished, and NULL — the unbounded case — is treated as "always possibly
relevant". Expansion is in local time: a weekly 09:30 meeting is at 09:30 on both
sides of the October clock change, which it would not be in UTC.

There is no journal on this side and none is needed. The sync token and the
objects it describes are committed together, so a crash leaves the old token with
the old objects, and asking again from the old token returns the same changes.
The mail side needs a journal because a modseq can be advanced over a
half-fetched folder; that state cannot be constructed here.

Done when all five hold:

1. Collections are found by asking the server, named by display name, and
   classified by what they hold rather than by what they are called.
2. A first sync stores everything and a token; the next sync with that token
   costs one request and returns nothing.
3. A token the server has forgotten starts again from nothing instead of
   failing, and the Mirror ends up with what the server has — not with a merge
   of old and new.
4. A repeating event appears on every day it happens, at the right local time
   across a DST change, with EXDATE dropped, a moved instance at its new time,
   and a cancelled one absent.
5. An agenda answers from the Mirror with no network at all.

Schema version 6: `dav_collections` and `dav_objects`, raw text and a projection
beside it.

`go test -tags live ./internal/davdrv/` reads the real account and writes
nothing: discovery listed 7 collections across events, tasks and cards; the
first sync of `Kalender` returned **1344 objects and a token in one request**,
all 1344 projected, 6 of them repeating; the second sync with that token
returned nothing. End to end, `mailbox agenda --days 10` printed the week from
two servers at once — mailbox.org and a SOGo work calendar — with the repeating
entries marked, and `mailbox event view` listed the next five Mondays of a
weekly one.

## Acceptance: the ninth slice

Todos and Habits. `mailbox todo [list|add|done|undone|rename|drop]` and
`mailbox habit [list|add|done|undone|drop]`. Listing is a Mirror read; every
change blocks on the server and updates the Mirror from the ack, so the next
read sees it with no cycle in between (ADR-0004).

A write carries the ETag it read as `If-Match`. A Todo somebody else changed in
between is **refused**, not overwritten — the next cycle brings their version and
the caller decides again. That is the same rule as ADR-0017's write-once: the
server is the arbiter, and the loser of a race is told rather than silently
undone.

`PUT` on mailbox.org **returns no ETag**. The Mirror therefore does not store
what it hoped for: with no ETag in the ack the object is read back in one extra
request, and what the server actually holds is what gets stored. A server is
entitled to rewrite what it is given, and a Mirror holding our version instead
disagrees with every other client until the next cycle.

Adding a Todo with several task lists and no `--list` is refused with the list
names, the way an ambiguous attachment id is (slice six). `task_list` in the
config answers it once for the ordinary case.

**Habits are not iCalendar** and are not pretended to be: all of them live in one
VEVENT as JSON, on a calendar this program creates (ADR-0018). Completing a day
is one read and one write of one object, so it cannot half-happen, and the format
is the one this account's habits are already in. A streak counts only the days a
Habit was *due*: a weekend does not break a weekday habit, and today not being
done yet is not a missed day — it is today.

Done when all five hold:

1. A Todo added is on the list immediately, with the list and the due date it
   was given, and completing it takes it off the list without deleting it.
2. A write against an ETag that has moved is refused and the Mirror is unchanged.
3. A server that acknowledges a write without saying what it stored is read
   back, not assumed.
4. Adding with several task lists and no name says which names to type.
5. Habits survive a round trip through one object: two habits are one object,
   ticking a day twice is ticking it once, and a day the habit was not due is
   neither done nor missed.

`go test -tags live ./internal/davdrv/ -run TestLiveWrite` creates its own task
list with MKCALENDAR, writes a Todo with an umlaut and a due date, reads it back
parsed, completes it with `If-Match`, watches a **stale** `If-Match` be refused,
deletes it and removes the list. It is what established that this server returns
no ETag on PUT.

Two things the live run found that no fake would have:

- **A server can refuse the token it just issued.** `Gesammelte Adressen`
  answers `403 <valid-sync-token/>` to the token it handed out one request
  earlier, so that collection resyncs from nothing on every cycle — 2 objects,
  and correct, because starting again is what an expired token means. It is also
  why only a 403 that actually names `valid-sync-token` is read that way: taking
  every 403 for an expired token would turn a permission problem into a silent
  full resync forever.
- **A sync answer can name the same href twice.** `Kontakte` reports 143 items
  with 141 distinct hrefs. Upserting by `(collection, href)` makes that a
  non-event, which is why the count in the log and the count in the Mirror
  differ by two.

**A third thing the live run found, and the worst of them.** The habits record
was a VEVENT dated 1990, copied from the program this one replaces. The server
takes it — `201 Created` — and a `GET` of its URL returns it. It appears in no
listing whatsoever: not `sync-collection`, not `PROPFIND Depth:1`. Open-Xchange
exposes only a window of each calendar over CalDAV, about a year back, and an
object outside it is stored and unfindable. The record is dated *today* now, and
re-dated on every write, which is free because every change rewrites it anyway.
`TestLiveHabitsObjectIsListed` writes one object dated today and one dated 1990
into a scratch calendar and asserts the first is listed. This is the shape of
defect that only a real server produces: every fake in this repo would have
returned both.

The same object then found the *fourth* one. Rewriting the habits record as a
freshly built object is refused with `412` — with or without an `If-Match` —
because the server keeps its own `SEQUENCE` and `LAST-MODIFIED` and reads an
object without them as an outdated update. Habits are edited now, like the Todos
and the Contacts: read the object, change the one field, write it back. Every
`habit done` after the first one had been failing with a message about somebody
else changing it, which was true of nobody.

End to end on the real account: `mailbox todo add … --list Einkauf --due
tomorrow` printed the new id, `todo list` showed it beside the user's own
entries, `todo done` moved it to `--all` only, `todo drop` removed it; and a
habit was created, ticked for today and yesterday, reported with its streak, and
dropped again.

## Acceptance: the tenth slice

The address books. `mailbox contact QUERY`, `contact view ID`, `contact add NAME
[--email …] [--phone …]`, `contact email ID --value …`, `contact drop ID`.

Searching is a LIKE over the projection — name, addresses, numbers, organisation
— and every term has to match somewhere on the card, so half a name and half an
address find one person. It is not FTS: an address book is a few hundred rows,
and a second index to keep true costs more than the scan it saves. It is still
local-only, like every other search here (ADR-0009).

A card is written as vCard 4 under a `.vcf` href, and an edit keeps everything it
was not asked about: the photo, the addresses, the X- properties somebody else's
client wrote. Dropping them is how two clients fight over one contact.

The Global Address Book is somebody else's, so with several books a new contact
has to name one — the same rule as task lists, with the same error that lists the
names.

Done when all five hold:

1. A contact is findable by any part of the card, and two terms that match
   different people match nobody.
2. A new contact is a vCard the server keeps, findable immediately with no cycle
   in between.
3. An edit adds without replacing: the address it had still finds it.
4. Several address books have to be named, and a configured default answers it.
5. Dropping one removes it from the server and from the Mirror.

Schema version 8: `dav_objects` gains `emails` and `phones`.

**A phone number has spaces in it.** The first version joined the projected
values with spaces, and `+49 30 000 111` came back out of the Mirror as three
phone numbers — a contact with a phone book instead of a phone. They are stored
newline-separated now, and the test that says so uses a number with spaces in it,
because that is the only kind that catches this.

End to end on the real account: `contact arnold` searched 143 real cards and
printed five, `contact add` wrote one with an umlaut-free name and a spaced
number, `contact email` added a second address to it, and `contact drop` removed
it again.

## Acceptance: the eleventh slice

`mailbox setup`. The only place in this program where a human is asked anything,
and it must not ask for a URL.

It asks for an address and a password, and then it **enumerates**: IMAP `LIST` with
`STATUS` for the Boxes, their special-use flags and their counts; the DAV
discovery chain for the Collections. Everything after that is a choice between
things the servers said they have — which Boxes to watch, which task list a new
Todo goes on, which address book a new Contact goes in. Nothing is typed that
could be wrong.

That is not polish. The old config pointed `carddav_home` at `Gesammelte
Adressen`, a 2-entry scratch list, instead of `Kontakte`, and had done for years.
Run against the real account, the wizard lists both by name and defaults to
`Kontakte`, and it finds the Sent box by its `\Sent` flag rather than by a name
that is `Gesendet` on half the servers in this country.

The password is checked against **both** servers before anything is written. A
password that reads mail but is refused for submission is a failure that would
otherwise surface on the first send, which is the worst possible moment.

The file is opened 0600 rather than chmodded afterwards — between the two there
is a moment where the password is readable (ADR-0014) — and an existing config is
never replaced without `--force`.

Then the first sync runs in the **foreground**, in the order that makes the
Mirror useful soonest: the Inbox, then the Boxes routing files beside it, then
everything else, with Archive last. A wizard that ends in silence for four
minutes looks broken; one that prints `INBOX  260 messages` does not.

Done when all five hold:

1. Nothing is asked that the servers could be asked instead — no URL, no folder
   name, no port.
2. A password neither server accepts is not written to disk.
3. The choices offered are the ones that exist, taken by number or by name, and
   "none" is an answer.
4. The file is 0600 from the moment it exists, and an existing one survives.
5. The first sync reports each Box as it lands, Inbox first and Archive last.

Live, against the real account: 68 Boxes read, `Sent` identified by its flag,
submission checked, 7 Collections enumerated with their kinds, `Kontakte`
defaulted rather than the scratch book, `INBOX` and `INBOX/Screener` offered as
the Boxes worth watching — and not `INBOX/Screener/Block`, which is where the
senders you never want to hear from again are filed.

## Acceptance: the twelfth slice

Secondary Accounts. `[accounts.gmx]` in the config makes `gmx/INBOX:412` mean
something, and every id that worked when there was one account still works
verbatim (ADR-0005).

The Mirror does not change: every row has carried an account since the first
slice, which is the whole reason this is a slice and not a migration. A second
account is a second set of **connections** — its own IMAP driver, its own
reconciler, its own cycle loop, its own IDLE watchers, its own SMTP — sharing one
Mirror file and one Outbox file.

Each account gets its own cycle loop rather than one loop over all of them: a
server that takes thirty seconds to answer must not hold up the account somebody
is actually reading. The Mirror reports itself Behind if **any** account's server
is unreachable; telling a caller "current" when one Inbox has not been looked at
for an hour is the kind of true-in-part answer this design exists to avoid
(ADR-0001).

Four rules fall out of the id format, and each one is a decision:

- **A Box name can contain a slash.** `INBOX/Screener:42` is a Box on the
  Primary Account, not an account called `INBOX`. Only a prefix that names a
  configured account is an account prefix.
- **The account on its own names all of it.** `box view gmx` is that account's
  Inbox and `search --in gmx` is that account's mail.
- **One command, one account.** `seen 7 gmx/1` is refused: a STORE goes to one
  server, and a list spanning two of them would be two half-done commands.
- **A reply is sent by the account that received it.** Answering from a
  different address than the one that was written to is not a default anybody
  wants. `send` takes `--account`, because which account sends a *new* mail is
  a choice about the mail rather than an id.

Search covers every account and orders newest-first across them: two accounts'
relevance scores come from two FTS tables and are not comparable, and pretending
they are would put a five-year-old mail above today's for no visible reason.
Within one account the ranking is unchanged (ADR-0009). Threads stay inside an
account: the same conversation reaching two accounts is two Threads (ADR-0008).

Done when all five hold:

1. An unqualified id still means the Primary Account, and the Primary's ids are
   never printed with a prefix.
2. A Secondary Account is readable under its own name, and the ids it prints
   read back.
3. A write goes to the server the id names, and only to that one.
4. A send uses the named account's sender and SMTP, and its copy is filed in
   that account's Sent box.
5. Search and status cover every account and say which is which.

There is no live test for this one: this account has no second mail account to
add, and inventing one on somebody else's server to prove it is not a test worth
its side effects. The scripted driver covers it, which is what it is for.

**Nothing-matched is an empty list, not `null`.** The multi-account search built
its results with `var out []hit`, which is nil when nothing matched, and JSON
turns that into `null` — so a caller reading the socket had to special-case
nothing-matched twice. It is `[]hit{}` now, and the test asserts on the type
rather than on the count.

## Acceptance: the thirteenth slice

The Routing. `mailbox screener`, `mailbox route [TARGET... --to BOX]` and
`mailbox aside` — the Primary Account's Screener, Feed, Paper Trail, Block and
Aside, and the Sieve script that fills the first four (ADR-0019).

The script is what sorts mail before this program ever sees it, so it is the
record and the Mirror holds a projection of it. Reading the Routing is a Mirror
read like every other read here; changing it goes to the server and stores what
the server took (ADR-0004). The Daemon holds the ManageSieve connection like it
holds every other one (ADR-0012) and re-reads the script on a ten-minute timer,
because a rule added in webmail should appear without a restart.

**The Screener is a list of senders, not of mail.** `mailbox screener` groups it
by address: two mails from one sender are one decision, and a listing that made
them two would ask for the same decision twice. Each line carries the count, how
many are unread, the newest subject and the id that reads it, so the decision
usually does not need the mail opened at all.

**One command finishes one decision.** `route` takes a message id or an address
— an agent that has just read something in the Screener has its id and not its
sender's address — rewrites the script so the sender's *next* mail is filed, and
moves the mail already waiting. `--to screener` is the undo: it takes the sender
off every list, and their next mail is owed a decision again.

Blocking is the asymmetric one. Their next mail is `discard`ed, because that is
what blocking means; the mail already in the Screener goes to
`INBOX/Screener/Block` rather than to Trash, so a block made by mistake can still
be found while the evidence exists.

Aside is a Box on this account and is not a Destination. "Always read this
sender later, from now on" is a Feed, so `--to aside` is refused by name and
points at `mailbox aside ID`, which moves one Message there, and `mailbox aside
done ID`, which moves it back.

Done when all five hold:

1. The Screener reads as one line per sender, newest first, with the id that
   reads their newest mail.
2. A decision rewrites the script *and* moves what was already waiting, and the
   Mirror answers "where does this sender's mail go" straight afterwards with no
   cycle and no second read of the server.
3. A sender has one Destination: deciding again moves them rather than listing
   them twice, which is what the script itself does — the first rule that
   matches wins and the second is text nothing reaches.
4. A destination Box the account does not have is refused before anything is
   written, with the Box named, and the script and the Screener are untouched.
5. An active script this program did not write is never switched off. It is
   enough that it reaches ours: the Routing is in force when `logic` is active
   *or* when the active script includes it, and a decision is refused only when
   neither holds.

Schema version 9: `routing` and `routing_script`.

**`header :contains "From"` was a bypass, not a style.** The script this replaces
matched a substring of the raw From header, and a sender writes their own display
name: `From: "anna@example.com" <attacker@example.net>` was filed as Anna, and
`bob@example.com` matched `notbob@example.com` besides. The generated script uses
`address :is :all "from"`, which tests the parsed address. The parser still reads
the old spelling, because the script already on the account is written in it, and
a Routing that cannot be read is a Routing that cannot be changed.

An empty list is written as no rule at all. Sieve has no empty string list — `[]`
does not compile — and the previous generator filled the gap with
`["example@example.com"]`, which reads like a decision somebody made about a
sender who does not exist. The parser drops that placeholder on the way in.

**The live run found the defect, and it was the whole rule.** The first version
refused a decision whenever the active script was not `logic`, on the reasoning
that switching somebody else's filtering off to switch ours on is not something a
routing decision should do. That reasoning is right and the test was wrong. The
active script on this account is `Open-Xchange`, written by mailbox.org's webmail
filter editor: four hand-made rules, ending in `include "logic";`. So the Routing
was already running, from inside a script that is not it — and the check refused
every decision on the one account that matters, while `PutScript(…, activate:
true)` would have deactivated `Open-Xchange` and thrown those four rules away.
The rule is reachability now: active, or included by whatever is active. Nothing
is activated unless the server is running nothing at all.

The same run put the parser against the real 10,694-byte script: 277 decisions —
18 blocked, 65 Inbox, 109 Paper Trail, 85 Feed — read out of the old
`header :contains` spelling and regenerated with no decision lost or changed. The
count is *lower* than the number of entries in the file, because senders listed on
two lists are counted once, under the rule Sieve actually reaches. Rewriting drops
the entries the server was already stepping over.

All six Boxes this slice names are on the real account — `INBOX`,
`INBOX/Screener`, `INBOX/Feed`, `INBOX/Paper Trail`, `INBOX/Screener/Block` and
`INBOX/Aside`, out of 68 — so nothing here needs a Box created to work. Gate 4 is
for the account that does not have them, and for the day one is renamed.

`go test -tags live ./internal/sievedrv/` is the gate no fake can be: the server
is the only Sieve compiler in reach, and PUTSCRIPT either takes the generated
script or refuses it. It writes under a scratch name, never activates it, checks
the decisions survive the round trip, and deletes it. A generated script that is
subtly not Sieve would otherwise pass every unit test in this repo and misfile
mail for as long as nobody looked.


## Acceptance: the fourteenth slice

The agent skill. `skill/SKILL.md` in this repo is what an agent reads before it
types anything here. `make skill` installs it to `~/.agents/skills/mailbox/`,
where every other skill on this machine lives, and links
`~/.claude/skills/mailbox` at it the way the rest of them are linked — one file
under two names. The skill it replaces was two real copies in those two
directories, free to disagree with each other as well as with the program.

**The skill does not carry the command surface.** ADR-0020 already made the
registry the one place a command exists, and it prints itself three ways:
`mailbox help` for what there is, `mailbox <command> --help` for a command's
flags, examples and the reason it works the way it does, and `mailbox commands`
for the whole tree as JSON. Copying any of that into a document is a cache of a
one-command lookup, and a cache with no invalidation.

That is not a hypothesis. The skill this replaces was written on 2026-08-28,
two slices before the end, and by the thirteenth it named `screener approve`,
`screener deny`, `contact show`, `contact refresh`, `event list`,
`todo complete`, `habit create`, `habit complete` and `search filters` — none of
which exist — and eight flags this program has never had, `--jq`, `--ids-only`,
`--count`, `--page`, `--all`, `--markdown`, `--allow-partial` and `-m`. An agent
following it would have failed on nearly every write. It was 22 KB; this one is
4.5 KB, and most of the difference is the reference it stopped keeping.

What it carries instead is what the binary cannot say about itself, because it
is about conduct rather than usage: that a Behind Mirror still answers and an
empty listing from one is not an empty Inbox, that exit 2 is an ordinary answer
from a Mirror and not a reason to retry, that sending is outward-facing so text
the agent wrote goes out as a `--draft` with its id handed back, that a **Held**
mail may already have been delivered and waits for a person, and that reading a
Message leaves the flags where the human set them.

Done when all six hold:

1. Every command and flag the skill names resolves in the registry, held by a
   test rather than by somebody rereading it.
2. Adding a command to the registry requires no edit to the skill.
3. The description alone is enough to reach it: an agent asked about mail,
   the screener or the agenda finds it without being told the program's name.
4. An agent that has read only the skill can get any command's flags in one
   lookup.
5. Text the agent composed itself does not reach SMTP without a person.
6. The skill exists once: every path that reads it reads the same file.

`TestSkillNamesOnlyRealCommands` is gate 1. It walks every backticked span in
the skill down the same tree `RunWith` walks, holds the flags left over against
what the command declared, and checks the help topics resolve. Fed the old
skill's `mailbox screener approve 342 --spam` it says `screener has no --spam`,
which is the sentence that would have caught the drift a week ago.

Gate 2 is what the whole design is for, and it is why there is no generator
here: a generated reference would be a cache kept honest by machinery, and no
cache is cheaper than one that was never taken.

Gate 6 is the same rule one level up. `make skill` writes the one file and links
the other name at it, and it refuses to remove a directory standing where the
link belongs — that is a decision for a person, and the target says so and
stops.

## Acceptance: the fifteenth slice

Setup as the install, and the config as the record. `mailbox setup` stops being a
wizard that writes an account and starts being the thing that makes a machine
work: the config, the two systemd units, the agent skill, the socket enabled, the
Routing bootstrapped, and the first sync watched over the socket rather than
performed. The Daemon reads the config while it runs instead of once at startup
(ADR-0021), which is what lets a second run be about *editing* rather than about
replacing.

**The first run is unchanged where it was already right.** It asks for an address
and a password, checks both against IMAP and SMTP before writing anything,
enumerates, and never asks for a URL. What follows the config write is new: the
units, `systemctl --user enable --now mailbox.socket`, the skill, and then setup
connects to the socket **as a client** and prints the Daemon's first cycle as it
lands. It ends by running `doctor` — the both-ends check, at the one moment when
everything is known to work, so the broken version is legible later.

`firstSync` is gone from `main.go` and `firstOrder` now orders the Boxes the
Daemon holds, so the cold start is the run that does the Inbox first. The order
had been the wizard's private property and was lost on the way through anyway:
`SyncAll` iterated a LIST-STATUS reply in whatever order the server gave it, so
it now follows the order it was asked for. That removes the second writer —
after the probes, setup opens no server connection and writes no Mirror.
`--force` retires with it, because nothing is overwritten any more: the account
list is edited, and `help_test.go` pins the system commands, so the test is
where that surface change is stated.

**Socket activation becomes real.** `mailbox daemon --systemd-socket` takes its
listener from `LISTEN_FDS` and fails rather than binding a path of its own
(ADR-0012). The units on this machine already pass that flag and the binary does
not accept it — they were written by hand, and nothing has ever kept them in step
with the program. Setup writes both, compares them on a later run, and replaces
what differs without restarting a Daemon that is serving.

**The Primary's Boxes are created, with no naming choices.** A fresh account has
no `INBOX/Screener`, `INBOX/Feed`, `INBOX/Paper Trail`, `INBOX/Aside` or
`INBOX/Screener/Block`, and until it does every `route` call fails gate 4 of the
thirteenth slice. Setup offers to create the missing ones and to put `logic` up,
under the same reachability rule — nothing is activated unless the server is
running no script at all. ADR-0019's "it does not create Boxes" is unchanged:
`route` still refuses a Box that is not there. Creating them is setup's job
because setup is the only place a human is present to be asked.

**A second run edits.** It opens with what is here — accounts, calendars with the
excluded ones marked, units, skill, Routing — read from `status` when the Daemon
answers and from the config and the filesystem when it does not, saying which.
Then: add, remove, repair, quit.

Adding asks the kind first, because a Secondary is either another IMAP/SMTP
account or a foreign calendar. The two live in different tables — `[accounts.gmx]`
and `[caldav.verein]` — and share **one namespace**, because `search --in gmx`
and `agenda` reading two different `gmx` is a bug nobody would suspect. A mail
account is asked for address, password, IMAP host and display name and nothing
else: the rest its own server can be asked for. A calendar is asked for a server
and credentials and then discovered, listed by display name and picked — the
`[caldav.*]` block with a typed URL is the last place in this program a person
types one, and it is the same mistake `carddav_home` was.

Removing:

- **The Primary is refused.** It is what an unqualified id means (ADR-0005);
  removing it is an uninstall.
- **A Secondary mail account** is refused while its Outbox holds anything, and
  setup asks the Daemon before it writes, because the Daemon is the only thing
  that knows what is in flight (ADR-0021). Otherwise the block goes and the
  Daemon prunes that account's rows. Deleting the whole Mirror would work and is
  the wrong tool: ADR-0013 is about a schema change, not about a removal, and it
  would cold-start every other account to forget one.
- **A hand-added calendar** is its config block and `ForgetCollection`.
- **A discovered Collection** cannot be removed, only excluded, and the exclusion
  is an entry in the config keyed by display name. It cannot live on
  `dav_collections`: that file is deleted and rebuilt on a schema change, so a
  decision stored there has a half-life. `Discover` skips excluded names on the
  way in.

**Nothing has ever been pruned.** `davsync.Discover`'s comment says "a collection
that disappears from the server is dropped, with its objects", and
`PutCollection` is an upsert with no delete beside it. A calendar removed on the
server has stayed in the Mirror since the ninth slice. Removal needs that path
anyway, so this slice makes the comment true.

**The skill gets embedded.** Setup installs it on machines with no checkout, so
`skill/SKILL.md` is `go:embed`ed — the first embed in this repo — and `make skill`
keeps installing from the working tree. Both are the same bytes because both are
the same file, so the sixth gate of the fourteenth slice holds, and
`TestSkillNamesOnlyRealCommands` reads the embedded copy so what ships is what is
gated. Setup keeps `make skill`'s refusal: a real directory standing where
`~/.claude/skills/mailbox` should be a symlink is reported as a step that failed,
not a wizard that dies.

**Problems are a short list, and a person is on the other end of it.** The
Daemon carries `problems` in `status`, `doctor` prints them beside its own
checks, every one of them goes to the journal — the Daemon logs to stderr and the
unit puts that in `journalctl --user -u mailbox` — and each change pushes
`problem.changed`. Three things qualify: a config that will not load, credentials
a server refuses, and Held mail. Not Behind: every reply already carries the
Mirror's state, a laptop lid produces one per suspend, and it resolves itself.
Not a failing Box: `SyncAll` is built to carry on past one. The rule is that a
problem means the program needs a human, and firing a few times a year is the
point.

The Daemon does not raise a desktop notification. It publishes the fact and a UI
renders it, which is the widget's job and the ADR-0011 shape; until that slice
exists the visible surfaces are the journal and `doctor`, and this slice does not
pretend otherwise.

Done when all six hold:

1. Nothing is typed that a server could be asked — including for a foreign
   calendar, which is discovered and picked by display name.
2. A second run adds and removes, and never rewrites what a hand wrote: comments,
   `[caldav.*]` blocks and untouched accounts survive it.
3. The units and the skill match the binary that wrote them — a unit passing a
   flag the binary does not have is replaced — and a Daemon that is serving is
   not restarted to do it.
4. After the probes, setup opens no server connection and writes nothing but
   files. The first sync is the Daemon's, streamed over the socket.
5. A config change lands without a restart: an account added by hand is being
   synced within a minute, and one removed by hand while it has Outbox mail is
   kept, with the reason in `status` and in the journal.
6. A config that does not parse does not stop the Daemon: it carries on with the
   last good one, says so in `status`, `doctor` and the journal, and pushes
   `problem.changed`.

Gate 4 is the ADR-0012 violation this slice removes, and 5 and 6 are ADR-0021's
two halves. Gate 1 extends the eleventh slice's first gate to the branch it never
covered.

The wizard is still the one thing an agent cannot exercise, which is why `Prober`
is an interface. The install steps need the same treatment: the unit and skill
writers take a root directory, so a test drives a whole install into a temporary
one and reads back what was written, and `systemctl` is a small interface with a
recording fake behind it. What no test covers is whether systemd accepts the
generated unit, so the live check for this slice is a real
`enable --now mailbox.socket` on this machine followed by `mailbox status`
answering over the socket with no Daemon started by hand.
