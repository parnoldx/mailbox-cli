# Email CLI

Agent-facing command `mailbox` over one mailbox.org IMAP account, its CalDAV calendars, and its CardDAV address book. Mail verbs sit at the root (`box`, `search`, `thread`, `screener`, `move`, …). `contact`, `events`, and `tasks` stay grouped. One binary; talks IMAP, CalDAV, and CardDAV itself. Not a human mail client. Not Spark. Not a himalaya wrapper.

## Language

**Mailbox**:
My IMAP account. There is one.
_Avoid_: account, hey account

**Routing**:
Sieve script `logic` placing new mail into a folder by sender list (whitelist, feed, paper trail, blacklist, else Screener). Owned by emailMoveHelper on a VPS, not by this CLI.
_Avoid_: filter, rule, category, hey workflow

**Approve**:
IMAP-move a Screener message into Inbox, or `--box` Feed / Paper Trail. emailMoveHelper updates Routing.
_Avoid_: whitelist, accept, screen (as a verb)

**Deny**:
IMAP-move a Screener message into Block. `--spam` is the same dest; mailbox.org has no spam trainer.
_Avoid_: junk, reject, spam (as a dest)

**Move**:
IMAP-move a message into Inbox, Feed, Paper Trail, or Block. Same primitive as Approve and Deny; source can be any folder.
_Avoid_: file, screen, archive (as a verb)

**Seen**:
IMAP `\Seen` flag on a Message. `mailbox seen` sets it; `mailbox unseen` clears it.
_Avoid_: read, unread (those name the `--unread` list filter)

**Trash**:
IMAP folder `Trash`. A Message moved here leaves its source folder. Does not change Routing.
_Avoid_: delete, expunge

**Junk**:
IMAP folder `Junk`. A Message moved here is this copy as spam. Does not change Routing and does not blacklist the sender.
_Avoid_: spam (as a folder name), Block

**Spam**:
IMAP-move a Message into Junk. Same primitive as Move; dest is Junk.
_Avoid_: deny --spam (that's Block)

**File**:
Move a message into Archive. Does not change Routing. Not a v1 verb; search of Archive is.
_Avoid_: move, screen, archive (as a verb)

**Search**:
IMAP keyword query over routing folders and Archive. Headers and body as Dovecot allows. `--from`, `--to`, and `--subject` are IMAP FROM/TO/SUBJECT, not a query language.
_Avoid_: semantic search, hybrid search

**Box**:
A mailbox: Inbox, Feed, Paper Trail, Screener, Block, Archive, Drafts, or Sent. Agents name it by alias (`feed`) or a matching folder name (`Inbox/Feed`).
_Avoid_: folder (as the CLI noun), Imbox

**Inbox**:
IMAP folder `INBOX`. Mail from accepted senders. Not “all mail”.
_Avoid_: Imbox, Important, INBOX/Important

**Feed**:
IMAP folder `INBOX/Feed`. Mail to skim, not correspond with.
_Avoid_: newsletter folder, category:newsletter

**Paper Trail**:
IMAP folder `INBOX/Paper Trail` (space in the name). Receipts, confirmations, records.
_Avoid_: PaperTrail, Papertrail

**Screener**:
IMAP folder `INBOX/Screener`. Mail from senders with no list yet. A decision is still owed.
_Avoid_: Gatekeeper, new senders, quarantine

**Block**:
IMAP folder `INBOX/Screener/Block`. Moving a message here blacklists the sender; future mail is discarded.
_Avoid_: spam, junk (those are provider folders)

**Archive**:
IMAP folder tree `Archive/…` (topic filing: Immo, Privat, Reisen, Finanzen, …). Searchable. Not a routing destination.
_Avoid_: Gmail archive, Spark archive

**Message**:
One email in a Box. Agents name it `box:uid` (example: `INBOX/Screener:342`).
_Avoid_: Spark numeric ID, sequence number

**Attachment**:
A file part of a Message. Agents name it `folder:uid:index` from `attachment list` (example: `INBOX:456:1`).
_Avoid_: hey numeric `messageId:index`

**Draft**:
An unsent Message stored in IMAP `Drafts`. New or reply.
_Avoid_: outbox

**Compose**:
A new outgoing Message. Delivered, or saved as a Draft.
_Avoid_: write

**Send**:
SMTP delivery of a Compose or a Draft. A copy is stored in Sent.
_Avoid_: deliver (as the CLI verb)

**Sent**:
IMAP folder `Sent`. Copies of mail this CLI delivered. Not a routing destination.
_Avoid_: outbox, sent-mail

**Event**:
A timed entry on CalDAV collection **Kalender**.
_Avoid_: meeting (no transcripts), appointment

**Task**:
An entry on CalDAV collection **Aufgaben**.
_Avoid_: todo, reminder

**Contact**:
A person in CardDAV collection **Kontakte**. Agents name it by vCard UID from `contact list`. The note is vCard NOTE, private to this mailbox.
_Avoid_: hey hide/bundle (delivery settings, not an address book), Thunderbird local Personal Address Book

**Envelope**:
`--json` output `{ok, data}` plus `truncated` and `notice` when a list or thread was cut. Errors are `{ok, code, error}` with code `usage`, `auth`, or `runtime` matching exit codes 2/3/1. `--jq` filters that envelope; `--quiet --jq` filters `data`. `--ids-only` and `--count` skip it; their notices go to stderr. `--markdown` is a table or a thread document, not the envelope.
_Avoid_: pretty-printed arrays as the machine contract

**Thread body**:
`--json` `body` is Markdown (plain text kept as-is; HTML converted at the edge). `--html` is the original HTML, refused on a terminal. `body_state` is `hydrated`, `bodyless`, `over_limit`, or `failed`.
_Avoid_: flattened HTML as the only agent body
