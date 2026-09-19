# Design

How the pieces fit. Vocabulary is in [CONTEXT.md](../CONTEXT.md). The `## Decisions`
below are the "why" behind all of this, numbered `ADR-00xx` because the code cites
them by number; nothing here re-argues them.

## Shape

```
    mailbox (CLI)          widget          Qt client
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
                      (SQLite, WAL)         + outbox (own file)
                            |
            IMAP / SMTP / CalDAV / CardDAV / ManageSieve
```

The Daemon is the only process that opens a network connection or writes the
Mirror. The CLI is a socket client and nothing else (ADR-0012).

## Packages

```
main.go              argv -> cli
gui/                   the Qt client   plugins/   the bar widgets
internal/
  cli/                 parsing, dispatch, the registry the help is rendered from
  daemon/              socket server, request routing, push fan-out, watch, lifecycle
  mirror/              THE seam: schema, domain types, queries. All SQL lives here.
  sync/                orchestration: what to sync, when, in what order
    mailsync/          the IMAP reconciler (drives a Driver, writes via mirror)
    davsync/           the sync-collection reconciler
  imapdrv/             Driver impl over go-imap/v2   (+ scripted fake for tests)
  davdrv/              CalDAV/CardDAV: discovery, sync-collection, multiget
  smtpdrv/ sievedrv/ message/ vcal/ vcard/        the other four protocols, and composition
  outbox/              durable send queue, its own SQLite file
  routing/             the Sieve script's format, and the four sender lists in it
  bubble/ habit/ pickup/ vcard/      the three records kept in somebody else's format
  trackers/ unsubscribe/             projections of a mail: pixels, and List-Unsubscribe
  setup/               the wizard: config, systemd units, skill, Collection discovery
  htmlmd/ terminal/ format/ ids/ imaputf7/ config/   ported from omamail
skill/                 the agent skill, embedded so setup can install it
```

`mirror` hands back domain types (`Message`, `Placement`, `Thread`, `Event`). No
package above it writes SQL, and no package below it knows a command exists. The
sync engines are the only writers of mirrored state; command handlers write only
through ADR-0004's write-through path.

The `Driver` interfaces are the test seam: `mailsync` talks to a `Driver`, and the
scripted fake drives it through states no real server produces on demand — a
UIDVALIDITY change that *keeps* the messages under new uids, one that lands
between detect and fetch, an expunge arriving over IDLE mid-fetch, a
HIGHESTMODSEQ that goes backwards, a connection dropped after the fetch and
before the commit, a folder whose count disagrees with its uid set, and messages
with a missing or duplicated Message-ID. go-imap's in-memory server is not an
option: it has no CONDSTORE at all.

## Schema

One SQLite file, WAL, `schema_version` in `meta`; on mismatch the file is deleted
and rebuilt (ADR-0013). Current version is **14**.

```
folders         account, name, uidvalidity, uidnext, highestmodseq, synced_at
messages        id, account, message_key, date, subject, from/to/cc, in_reply_to,
                references_, thread_id, text_plain, text_html, body_state,
                list_unsubscribe{,_post}
message_refs    message_id, ref_key          -- one row per referenced Message-ID
placements      account, folder, uid, message_id, flags, bubble_at, internaldate, size
parts           message_id, path, mime_type, filename, disposition, size, content_id
messages_fts    fts5(subject, addresses, body)
dav_collections id, account, kind(events|tasks|cards), url, name, color, sync_token
dav_objects     collection_id, href, etag, raw, + parsed projection columns
routing         account, address, dest(inbox|feed|paper|block), box
routing_script  account, name, raw, active, synced_at
correspondents  account, email, name, last_seen, count    -- autocomplete fallback
sync_journal    account, folder, intent, started_at
```

`message_key` is the RFC822 Message-ID, or a synthetic `folder:uid` when it is
absent or collides (ADR-0007). `parts` holds attachment *metadata* so
`attachment list` is a Mirror read; the bytes are never stored (ADR-0003). `raw`
is the record and the columns beside it a projection, on `dav_objects` (ADR-0010),
`routing_script` (ADR-0019) and `placements.bubble_at` (ADR-0023) alike —
every one of them repopulates on a rebuild.

The Outbox is a **separate** file and is never dropped (ADR-0013). Its states
answer one question after a crash — may this be sent again?:

```
queued --claim--> sending --smtp ok--> sent --append--> filed
   ^                  |                                    
   |                  +--smtp said no--> queued (with the reason)
   |                  +--we died------->  held  (waits for a person)
   +--`outbox retry`--------------------------+
```

`sending` at startup is the only interesting one: SMTP saying no means the mail
was *not* accepted, so requeueing is safe; a process that died inside the
transaction knows nothing, and a mail that may already be in someone's inbox must
not go again on a hunch. Filing the Sent copy is the opposite — the mail has
already gone, so a failed APPEND is retried on every drain and never re-sends.

## The mail sync cycle

Per account, one cycle:

1. **Detect** — one `LIST "" "*" RETURN (STATUS (MESSAGES UIDNEXT UIDVALIDITY
   HIGHESTMODSEQ))`. O(folders). Compare against `folders`.
2. **Plan** — per folder:
   - `uidvalidity` differs → **resync**: drop this folder's placements (never the
     messages), refetch envelopes, re-map onto existing messages by `message_key`,
     fetch bodies only for what is genuinely new.
   - `highestmodseq` advanced → **incremental**: `UID FETCH 1:* (FLAGS)
     CHANGEDSINCE n`, plus envelope/bodystructure/text for uids ≥ old `uidnext`.
   - `messages` disagrees with the row count after that → **expunge diff**:
     `UID SEARCH ALL`, diff, delete the missing placements.
   - otherwise → nothing.
3. **Journal** — write the intent to `sync_journal` before touching the network.
4. **Apply** — fetch, then write placements, messages, parts and FTS rows in one
   transaction that also advances `highestmodseq`/`uidnext` and clears the journal
   entry.

A crash therefore leaves either the old modseq with the old rows or the new modseq
with the new rows — never an advanced modseq over a half-fetched folder. A journal
entry found at startup means "redo this folder from its stored modseq", which is
always safe because step 2 is idempotent (ADR-0015, mbsync's idea and not its code).

**Mirrored and Watched are different sets.** Every mirrored Box is reconciled on
every cycle, from the single detection pass above; Watched is the subset that also
gets an IDLE connection. Conflating them costs you every unwatched Box silently
never syncing — invisible with one Box mirrored, which is exactly when it gets
written. The Boxes are discovered with `LIST`, not configured. Because go-imap
reports expunges as *sequence numbers*, the reconciler keeps a seq→uid map for each
IDLE'd folder, which makes live expunges exact and leaves step 2's diff mainly for
time the Daemon was down.

## The DAV sync cycle

One algorithm, no ctag fallback, because all three configured servers answer
`sync-collection` with a real token: `REPORT sync-collection` with the stored
token, asking for the object data in the same request. An empty token returns
everything and the token together. What a server names without sending is fetched
with one multiget. Calendars and task lists every 10 minutes, address books every
24 hours; a write does not wait for any of that (ADR-0004). There is no journal on
this side and none is needed: the token and the objects it describes are committed
together.

The requests are ours rather than go-webdav's: its CalDAV client has no
sync-collection at all, and its multiget hands back a *parsed* calendar —
re-encoding that to store it would throw away the record (ADR-0010, ADR-0015).

A repeating event is **one row**: a rule with no end has no finite expansion to
store, and the window belongs to the question rather than the calendar, so
`agenda` expands it on the way out. The projection carries `repeats_until` so a
window query can rule out a rule that has finished, and NULL — unbounded — counts
as always possibly relevant. Expansion is in local time: a weekly 09:30 meeting is
at 09:30 on both sides of the October clock change, which it would not be in UTC.

## Socket contract

NDJSON, one object per line each way, over `$XDG_RUNTIME_DIR/mailbox.sock`, mode
0600, socket-activated.

```
-> {"id":"7","cmd":["box","view"],"args":{"positional":"inbox","limit":50}}
<- {"id":"7","ok":true,"data":[…],"mirror":{"synced_at":"…","behind":false,
                                            "connected":true}}
<- {"event":"mail.changed","account":"primary","box":"inbox"}
<- {"event":"added","box":"inbox","thread":…,"subject":…,"new":true}
```

Every reply carries a `mirror` block; `connected:false` is **not** an error exit,
because a Behind Mirror still answers (ADR-0001). Pushes carry no data — a widget
that receives one re-reads (ADR-0011). A connection that sent `watch` gets the
second kind of line, built per Message from the Mirror and filtered in the Daemon
(ADR-0027); `--include-code`-free Pickups arrive as `pickup`, never as `added`
with `new`. `status` also carries `problems` — the short list of things needing a
person (a config that will not load, credentials a server refuses, Held mail) —
and a `problem.changed` push says to re-read it. `reload` is the one verb with no
CLI command behind it: `mailbox setup` sends it after writing the config
(ADR-0021). With no Daemon the CLI exits `daemon_required` (ADR-0012).

## Setup

`mailbox setup` is the whole install: it writes `config.toml`, the two systemd
units and the agent skill, enables the socket, bootstraps the Routing, and watches
the Daemon's first cycle over the socket as any other client would. It never
writes the Mirror (ADR-0021), and a second run is about *editing* rather than
replacing — it prints what is here and offers add, remove, repair.

The wizard is the only place a human is asked anything, and **it never asks for a
URL**. It authenticates, then enumerates: IMAP `LIST` for Boxes, special-use flags
and counts, `PROPFIND Depth:1` on the DAV homes for Collections — and shows what
the servers said they have, as a choice. That is not polish: the omamail config
had `carddav_home` pointing at a 2-entry scratch book instead of `Kontakte` for
years, a hand-copied URL silently wrong. The password is checked against both
servers before anything is written, the file is opened 0600 rather than chmodded
afterwards (ADR-0014), and the Primary Account cannot be removed — that is an
uninstall.

The Daemon does the opposite of setup about ordering: it opens the socket *before*
its first cycle, because a cold start takes minutes and making callers wait for it
would deny them the Behind Mirror that ADR-0001 says they may always have. The
cold start itself is ordered Inbox first, then the Routing's Boxes, Archive last.

**Cycles never overlap.** The poll fires every minute and a cold start runs for
several, so a naive timer starts a second cycle inside the first, which plans
against half-written state and redoes folders the first has finished. Cycles run
one at a time from a depth-one trigger channel; nudges arriving during a cycle
coalesce into one after it, which is all they can ever mean.

## Decisions

**ADR-0001 — The Mirror is the read model.** Every read command answers from the
local Mirror, unconditionally; the network is touched only by the sync loop and by
writes. Rejected a cache with IMAP fall-through: two code paths for every read
forever, and every command a potential network stall. Freshness is per *domain*
(mail and the collections come up to date on loops minutes or hours apart), so
every reply reports the age of the data it answered from *and* whether a cycle is
running — "behind" alone is not actionable.

**ADR-0002 — A new repo rather than evolving `2026-08-28-omamail`.** Inverting the
data flow is a rewrite of every data path (28k LOC). Leaf packages with no opinion
about where data comes from were copied: `htmlmd`, `format`, `ids`, `imaputf7`,
`config`, `vobject`. The TUI and the old Sieve routing service stayed behind.

**ADR-0003 — Mirror every text part, and no attachments.** Measured, not guessed:
29 MB of text against 610 MB of raw account, attachments ~99% of the bytes. The
Mirror stores text **decoded** — transfer encoding and charset resolved — so no
reader has to know mail has encodings at all; an undecoded part is non-empty and
passes any check that the body arrived. Trash is excluded entirely and settledly:
it is where things go to stop being findable. The rule this gives the CLI: **list,
search and count never touch the network; naming one specific object may.**

**ADR-0004 — Writes go through the server, except Send.** A change blocks on the
round trip and updates the Mirror from the ack in the same transaction, so exit 0
means it happened and the next read sees it. UIDPLUS gives the new uid on MOVE and
APPEND. Send is the exception and gets the durable Outbox, because a half-finished
SMTP transaction is the one failure where losing what the user typed is
unacceptable.

**ADR-0005 — Account-qualified ids, with an implicit Primary.** A Message is
`[account/]box:uid`; unqualified means the Primary, so every id that worked with
one account still works verbatim. A `--account` flag was rejected because an id
has to survive being pasted from one command into another, and a token that needs
a companion flag is not a token. Every row carries an account from the start.

**ADR-0006 — CONDSTORE sync, without QRESYNC.** One LIST-STATUS report per cycle,
CHANGEDSINCE fetches, an expunge diff only on a count mismatch. QRESYNC would
close the expunge gap and Dovecot supports it, but `go-imap/v2` cannot be
extended to (unexported command internals, no raw escape hatch), so it would mean
vendoring a fork of a beta library for a narrow win. *Revisit if* one folder's UID
diff ever costs more than a second. IDLE is spent only where sub-second latency is
worth something — Inbox and Screener on the Primary, Inbox on each Secondary
(sign-in links land in the Screener and a minute late is expired); everything else
rides the poll. `COMPRESS=DEFLATE` on those connections.

**ADR-0007 — Messages have Placements.** A Message is keyed by
`(account, rfc822_message_id)`; where it sits is a separate Placement row. Making
`(account, folder, uid)` the identity loses a message's history at every move and
gives threading and dedup no key. Message-IDs are missing or duplicated often
enough that they cannot be trusted alone: an absent or colliding one is replaced
by a synthetic `folder:uid` key, degrading that message rather than complicating
every row.

**ADR-0008 — Threads are built locally, across every Box.** From References and
In-Reply-To, over the whole Account at once. IMAP `THREAD` operates on the
*selected mailbox*, so it can never link an Inbox message to its reply filed in
`Archive/Immo`. A Thread never crosses Accounts. Subjects are never used. Linking
looks **both ways** — a reply is routinely mirrored before its parent — and when
the links reach several existing Threads they were one conversation all along and
are merged into the oldest.

**ADR-0009 — Search is local only.** FTS5 over the Mirror, no fall-through to IMAP
`SEARCH`, no flag for one: two matching semantics for one command is the split
ADR-0001 rejected. Local search also ranks, which IMAP does not. The index holds
the subject, the addresses and the text a reader would see — indexing markup would
match every newsletter on `table` and `href`. Trash is not searchable on either
side.

**ADR-0010 — Raw iCalendar and vCard are the record.** Parsed columns are a
projection; an edit is applied to the raw text and PUT back, never re-serialised
from our columns, because recurrence overrides, VALARM, attendee state and other
clients' X- properties are far wider than anything we parse — rebuilding would
drop what we do not model, silently, in somebody else's client. A command over
collections *nudges*: it asks for a cycle and does not wait. Rate limited to one
per kind per 30 seconds (an hour for address books). The timer and the nudge go
through one trigger, so DAV cycles are serialised the way mail cycles are: two at
once would both ask from the same sync token and the second would commit the older
answer.

**ADR-0011 — Pushes carry no data.** A push names what changed; a widget re-reads,
which is a ~1ms local query (ADR-0001). Pushing the payload too would give every
fact two routes into a client and the two would drift. Every push goes to every
connected client — per-connection interest is state the Daemon would track for no
benefit (ADR-0027 adds the subscription, beside this, not instead of it).

**ADR-0012 — The Daemon is required.** With nothing listening `mailbox` fails with
`daemon_required`; no `--local`, no fallback to the network. One process owning the
Mirror means one answer to every question about locking, schema and freshness — and
a successful-looking read against a Daemon that is down is the wrong thing to
return. `mailbox daemon --systemd-socket` takes its listener from `LISTEN_FDS` and
*fails* rather than binding a path of its own, because a unit that silently binds a
second socket looks healthy and is talked to by nobody. Both units are written by
`mailbox setup`, compared against what it would write now, and replaced if they
differ — without restarting a Daemon that is serving.

**ADR-0013 — The Mirror is disposable; the Outbox is not.** On a schema mismatch
the file is deleted and resynced from scratch. There are no migrations and there
never will be: every byte is derived from a server that still holds the original,
so a migration is work spent preserving a copy of something we can fetch again.
Not writing migrations is the largest single saving in this design. The Outbox is
the one place the Mirror leads the server, so it lives in its own file that is
never dropped and does get migrations if it ever needs them.

**ADR-0014 — Credentials stay in a mode-0600 file.** Passwords live in plaintext
in `config.toml`, mode 0600. Not the Secret Service, although it is running here:
the Daemon is long-lived and starts at login, so a keyring locked at boot means no
mail until a human unlocks it, and a background service that prompts. There is no
keyring on the VPS at all, so the file path has to exist regardless — and having it
exist as a fallback is the same as having it as the mechanism, minus a second code
path. On a single-user machine with an encrypted disk it is not the weak link.

**ADR-0015 — We talk to the servers ourselves.** IMAP, SMTP, CalDAV, CardDAV
directly; no hosted API, no driving another mail program as a subprocess. Rejected
and recorded because each will be suggested again: **Nylas** is a client for the
hosted Nylas API, so mail would live at a third party on a commercial tier;
**Himalaya** is stateless with no local cache and no CalDAV/CardDAV, and its `sirup`
socket server is exactly the warm-connection design ADR-0001 moves away from;
**mbsync/isync** and **neverest** are batch syncers into a Maildir, costing IDLE
and so the sub-second Screener, write-through, and the static `CGO_ENABLED=0` build
(notmuch's bindings are cgo-only). mbsync also has no CONDSTORE: its twenty years
of UID diff is ADR-0006's fallback path, not something ahead of it. What we took
instead of its code is its state model — journal before acting, replay on restart.

**ADR-0016 — Three connection roles, and never a cached SELECT.** A control
connection that never selects a mailbox, a work connection that selects and
fetches, and one watch connection per IDLE'd folder; re-select before every
operation; reconnect and retry once on any failure. Each is a bug found against a
real Dovecot that the fake passed: a connection with the folder selected answers
LIST-STATUS from its own view, so a flag set elsewhere stayed invisible forever; a
connection running IDLE may issue no other command; a cached SELECT silently
excludes mail delivered since the last command, and on a folder deleted and
recreated elsewhere Dovecot answers `NO Mailbox was deleted under us`, after which
a UID SEARCH returns an empty set *with no error* — indistinguishable from an empty
folder. Errors are not worth classifying, and the fresh SELECT costs one round trip
on a warm connection. Four connections for a Primary with Inbox and Screener
watched.

**ADR-0017 — A write is tried once.** Reads retry, MOVEs do not: a MOVE whose ack
was lost has still happened, and re-asking a folder that has since had one message
expunged and another delivered moves a *different* message. Flag changes are
idempotent and keep the retry. A failed write is reported as a failure — honest
rather than convenient, since the caller re-reads and the next cycle reconciles
what the server actually did. Rejected: round-tripping the Message-ID around every
move to make writes idempotent.

**ADR-0018 — Habits live in one object, and its record is JSON.** A habit is a
repeating per-day practice, and iCalendar has no component for it: a VTODO ends when
completed, a recurring one with a completion override per day grows a component
every morning, and no other client reads it as a habit anyway. A separate table
would make it the only thing here with no server behind it. So all of them live in
**one** VEVENT on a calendar this program creates, the record as JSON in its
DESCRIPTION. Completing a day is one read and one write of one object, so it cannot
half-happen, and it is the format the program this replaces already used. **Dated
today and re-dated on every write**, because Open-Xchange exposes only a ~1-year
window and a 1990 record is written successfully and then invisible forever.
**Edited, not rebuilt**, because the server keeps its own SEQUENCE and LAST-MODIFIED
and refuses a rebuilt object as an outdated update. The cost — no other client can
show or tick a habit — is accepted.

**ADR-0019 — The Routing is one Sieve script, and this program owns it.**
`logic` on the Primary's server sorts mail before we see it, which is what makes the
Screener a pile of undecided *senders* rather than a second Inbox. **The script is
the record** and `routing` is a projection of it: reading the Routing is a Mirror
read, changing it goes to the server. A decision is about a sender and applies to
the mail already here — one command rewrites the script *and* moves the waiting
mail, because a caller made to run two will one day run only the first, and a
Screener that keeps mail from a sender already decided about is one nobody trusts.
`address :is :all "from"`, never `header :contains`: a sender writes their own
display name, so `From: "anna@example.com" <attacker@example.net>` was filed as
Anna, and `bob@example.com` matched `notbob@example.com`. An address that cannot be
quoted safely is refused rather than escaped. The rule is **reachability, not
activity**: ours runs when `logic` is active *or* when the active script includes
it — on this account that is webmail's own script, ending `include "logic";` — and
a decision is refused, with the `include` line named, only when neither holds. It
never creates a Box (`fileinto` into a missing Box files nowhere, looking as though
the decision was made), and never routes to Aside, because "always read this sender
later" is a Feed.

**ADR-0020 — The CLI is a command registry, and a bare noun is help.**
`internal/cli/registry.go` holds the whole tree — name, section, gloss, long
description, usage, flags, examples — and the overview, group pages, leaf pages,
`--help` and `mailbox commands` are all rendered from it. A command's `flag.FlagSet`
is *built* from its declarations, so help and parser cannot disagree. A bare noun
prints help everywhere (`mailbox todo` is the index; `todo list` lists) — an agent
that types a noun learns the noun in one round trip instead of learning one verb and
none of the others. Bare `mailbox` answers on stdout and exits 0: it is a question,
not a mistake, and exit 1 makes `set -e` scripts read it as a failure; a *wrong*
subcommand still exits 1 but prints the group index alongside the error, because the
caller cannot scroll back. `--json` is global. Four sections — MAIL, ORGANIZE
(everything that decides where mail goes rather than acting on one message),
CALENDAR & TASKS & CONTACTS, SYSTEM (`META` describes the implementer's view) —
ordered by workflow, not alphabetically, because reading is how a caller gets the id
the other verbs need. Held by golden files for the two overviews and by invariants
over the registry.

**ADR-0021 — The config is the record, and the Daemon reconciles it.** The Daemon
does not read `config.toml` once at startup: it compares mtime and size at the top of
each cycle and re-reads, and `setup` sends `reload` when it finishes writing. No file
watch — TOML is written by temp-file-and-rename, so a watch would have to be on the
directory and would see two events per save with a window where the file does not
exist. No SIGHUP: under socket activation the pid is not where it was left. A config
edited by hand behaves exactly like one the wizard wrote, which is the point — setup
as the only supported writer would make a hand edit work by accident until it didn't.
Applied while running: Secondary Accounts and their credentials, the Collection
exclude list, and command defaults. Not applied: the Primary's block (its connections
are its identity) and the hand-added `[caldav.*]` calendars (a live DAV cycle is
reading them) — the Daemon logs what changed and **exits 0**, which under socket
activation is not an outage. **It never writes the config**: a process editing it
while a human has it open is how a password gets lost. A config that does not parse
leaves the Daemon on the last good one and reports it, because exiting on a misspelt
key turns a typo into missed mail. And it **may decline** — removing an Account whose
Outbox still holds mail is refused, since a queue is not something to drop quietly —
so the declining is visible in `status`.

**ADR-0022 — A sent mail carries both plain text and HTML.** `--body` is Markdown;
`multipart/alternative` goes out with the plain part verbatim and the HTML rendered
by `htmlmd` — the same converter the reading path uses in the other direction, so a
mail written and read here round-trips through one implementation. Neither part can
be dropped: HTML is what most people see, plain text is the record. Making it
unconditional is the point — a heuristic of "HTML when the body looks like Markdown"
surprises in both directions. `--body-html` is the escape hatch for a caller that
already holds HTML. `forward` stays plain: its quoted header block is plain text
with structure a Markdown renderer would mangle.

**ADR-0023 — Bubble Up's return time is an IMAP keyword.** `$bubble-YYYYMMDDTHHMM`,
local wall-clock with no zone, on the message. Not a Mirror column alone (a rebuild
would lose every pending return), not a sidecar file (it drifts from the server the
moment a message moves elsewhere), not a `time.AfterFunc` (a Daemon that was down
when the instant passed would never fire). A keyword moves with the message, syncs
like a flag, and is what **two Daemons act on without coordinating**;
`placements.bubble_at` is its projection, existing only to make the due scan and the
soonest-first sort a query. The return is a **wall-clock scan**, not a timer, so a
Daemon down across the instant catches every overdue return on its first tick —
never lost, only late. The return strips the keyword, removes `\Seen` (the reminder
*is* the mail reappearing unread; iOS raises nothing for a silently-moved read
thread), moves the thread to the Inbox and sets `$bubbled` so it floats. `--now`
runs the same steps. It needs `\*` in the folder's PERMANENTFLAGS.

**ADR-0024 — A move out of the Screener is a routing decision.** For a general
mailbox a move is ambiguous — "file this sender there" or "I have dealt with this
one" — but the Screener's only question is *do you want this sender's mail*, and
reading a message never requires moving it, so a message leaving the Screener **is**
the answer. Inbox, Feed and Paper Trail name a Destination; `Screener/Block` is a
block; anywhere else is a plain move. The sender's other waiting mail sweeps with
it, and a move *into* the Screener un-decides the sender. It is idempotent rather
than trying to tell its own move from somebody's drag: writing an entry that is
already there is a no-op, so `route` followed by the move syncing writes nothing
twice, and editing the script moves no mail. The cross-folder move is reconstructed
from the cycle — a `Gone` from the Screener matched to an `Added` in a decision
folder. There is no human in the loop, so every inferred decision is logged loudly
and listed in `status`, and the reachability rule still refuses rather than applies.
Blocking marks the mail read and moves it to Trash: a pile nobody empties told you
nothing, and Trash keeps a mistake findable.

**ADR-0025 — Two Daemons for one account, coordinating only through server
records.** The same binary run as a second `mailbox daemon` on an always-on box:
the timed return and the Screener inference have to happen while the home machine
is off. One Mirror file and one Outbox file each, which a separate machine gives
for free. They coordinate through **server-side records only** — the `$bubble-*`
keyword, the Sieve script, `\Seen` and folder placement — and whichever acts first
wins; the other syncs the result and finds nothing to do. No remote socket:
scheduling happens at home, and the VPS Daemon runs the loops and the cycle. It
holds no single-instance assumption: `sync_journal` is keyed per account and folder
in each Daemon's own Mirror, freshness is per-process memory, and every write is
idempotent, server-arbitrated or tolerant of the uid being gone. Deployment needs
no `--headless` mode: env vars for the three file paths, a mode-0600 credentials
file, and a hand-written system unit. See the Makefile's `update-daemon-vps`.

**ADR-0026 — A domain key is a routing decision.** `@stripe.com` is written as
`address :domain :is "from"`, after every address rule, so Sieve's first match
makes the specific address win. The alternative — client-side list files applied
after mail arrived — files too late and splits the record, since the script would
no longer be what the server runs. Sieve already has `:domain`. A domain that cannot
be quoted safely is refused for the same reason an address is: the value comes from
a From header.

**ADR-0027 — A watch is a subscription, a push is a nudge.** A watch gets a Change,
which says what moved, and only on a connection that asked for it. ADR-0011 stands
for widgets. A watch is the other shape of client — a script, a pipe, a `notify-send`
one-liner — and it has no query to make: telling it to re-read is telling it to
reimplement the diff the Daemon has just done. A Change costs a Mirror read per
Message, so it is built only when somebody is listening, and the filters are applied
in the Daemon so an idle watch is an idle socket. What counts as *new mail* is
decided here rather than by the reader: unseen, in a Box where unread means
something, and an arrival rather than a move landing from another client. Nothing is
remembered — no `--since`, no journal — because a durable feed needs a table, a
retention rule and a cursor, and a fresh watch reads from the server anyway.

## What the real servers do

Every one of these was measured against mailbox.org, SOGo or Open-Xchange, and each
one is something a scripted fake would have been happy to get wrong:

- Dovecot answers LIST-STATUS from the selected mailbox's own view, and a UID SEARCH
  in a folder deleted from under us returns empty *without an error* (ADR-0016).
- A UIDVALIDITY change is *changed*, never *greater than* — RFC 3501 promises no
  monotonicity. `DELETE` + `CREATE` on a scratch folder provokes it: measured
  `1681457875` → `1681457876`, uids restarting at 1. It does not mean the mail is
  gone, so the reconciler drops Placements and re-maps by `message_key`; the cost is
  an envelope pass over one folder, not a body refetch.
- mailbox.org keeps `\*` in `PERMANENTFLAGS`, so a custom keyword survives.
- A server is not obliged to put the uid in the untagged FETCH a STORE provokes, so
  the flags are read back rather than assumed.
- PUT on mailbox.org returns **no ETag**: the object is read back in one extra
  request rather than storing what we hoped for. Open-Xchange keeps its own
  `SEQUENCE`/`LAST-MODIFIED` and refuses a rebuilt object with `412`, and exposes only
  a ~1-year window — an object outside it is stored, fetchable by URL, and in no
  listing at all (ADR-0018).
- One CalDAV collection answers `403 <valid-sync-token/>` to the token it issued a
  request earlier, so it resyncs from nothing every cycle; a sync answer can name the
  same href twice, which upserting by `(collection, href)` makes a non-event.
- A FETCH lists its body sections once and applies them to every message in the set,
  so pooling all parts of all messages asks for parts×messages sections — a hundred
  thousand for a 260-message folder, and it does not return. Messages are grouped by
  the shape of their MIME tree first.
- `BODYSTRUCTURE` needs `{Extended: true}`: the non-extended `BODY` carries no
  Content-Disposition, and every disposition test downstream had silently seen
  nothing.
- A text part with no charset *is* us-ascii by definition; assuming otherwise sent a
  plain reply as base64 UTF-8 declared ASCII, which came back as replacement
  characters.
- The real routing script is 10,694 bytes and held 277 decisions in the old
  `header :contains` spelling when the reader first met it.
- The real account is 68 Boxes — 67 plus Archive tree — of which `Sent` is identified
  by its `\Sent` flag rather than a localised name, and 7 collections across events,
  tasks and cards.

## Live tests

A fake tests the algorithm; only a server tests the assumption. `-tags live` runs
against the real servers, and the scratch folder is `INBOX/mailbox-selftest`.

| gate | proves |
| --- | --- |
| `go test -tags live ./internal/imapdrv/ -run TestLiveGates` | a message expunged while the Daemon was stopped is gone after restart; a UIDVALIDITY change forces a clean resync that keeps the Messages |
| `… -run TestLiveGate2Idle` | a message delivered while IDLE is held appears within a second |
| `… -run TestLiveWrites` | whether the STORE readback carries the uid, and whether MOVE reports COPYUID |
| `… -run TestLiveAttachment` | one part fetched and written decoded |
| `… -run TestLiveSend` | one mail to itself over real submission, filed with APPEND, fetched back, deleted |
| `… -run TestLiveBubbleKeyword` | `$bubble-*` stored, read back on a fresh connection, re-timed and stripped, and `\*` in PERMANENTFLAGS |
| `go test -tags live ./internal/davdrv/` | discovery, first sync + token, a forgotten token, multiget — reads and writes nothing |
| `… -run TestLiveWrite` | a task list created with MKCALENDAR, a Todo with an umlaut and a due date read back parsed and completed with `If-Match`, a stale `If-Match` refused |
| `go test -tags live ./internal/sievedrv/` | the server is the only Sieve compiler in reach: PUTSCRIPT either takes the generated script or refuses it, and the Routing is reachable under its name |

No live test exists for the Secondary Accounts or the two-Daemon slices: this
account has no second mail account to invent, and the scripted driver covers what a
second account has to do.