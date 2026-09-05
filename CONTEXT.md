# mailbox

Agent-facing command `mailbox` over mail, calendars, todos and contacts. A daemon
holds a local Mirror of the servers; the CLI reads the Mirror and never waits on
a network. Not a human mail client.

## The Mirror

**Mirror**:
The daemon's local copy of what the servers hold. It is the answer a read command
gives, not an optimisation in front of one — dropping it changes what the CLI can
answer until it is rebuilt.
_Avoid_: cache, index, local store

**Daemon**:
The single long-lived process that owns every server connection and the Mirror.
Nothing else opens IMAP, SMTP or DAV.
_Avoid_: server, service, agent

**Mirrored**:
Held in the Mirror, and therefore answerable offline. Every Message's envelope and
text is Mirrored; an Attachment never is.
_Avoid_: cached, synced, downloaded

**Behind**:
The Mirror has not caught up with a server it can reach, or cannot reach it. A
Behind Mirror still answers; it says so rather than failing.
_Avoid_: stale, dirty, offline (that describes the network, not the Mirror)

## Accounts

**Account**:
One IMAP + SMTP login. Several may be configured.
_Avoid_: mailbox (that named the single account before there were several), profile

**Primary Account**:
The Account carrying the Screener, Feed, Paper Trail, Aside and Block, and the
Sieve Routing that fills them. There is exactly one, and it is what an
unqualified ID means.
_Avoid_: main account, default account

**Secondary Account**:
An Account with an Inbox, Drafts and Sent, and the ability to Send. No Screener,
no Routing. Its own connections and its own cycle, sharing the one Mirror: every
row in it carries an Account.
_Avoid_: extra account, sub-account

## Mail

**Box**:
An IMAP folder on an Account, named by an alias (`inbox`) or by its folder name
(`INBOX/Screener`).
_Avoid_: folder, mailbox, label

**Routing**:
The Sieve script on the Primary Account's server that files new mail before this
program sees it: blocked senders discarded, then the Inbox, the Paper Trail and
the Feed, and everything left over into the Screener. The script is the record;
what the Mirror holds beside it is a projection of it. It runs when it is the
server's active script or when the active one includes it (ADR-0019). A
decision is about an address or a whole domain (`@example.com`); an address
always wins (ADR-0026).
_Avoid_: filter, rule, sieve (that is the language it is written in)

**Destination**:
Where the Routing sends one sender's mail: Inbox, Feed, Paper Trail or Block.
The Screener is the absence of one, not a fifth. Named by an address or by a
domain key (`@example.com`).
_Avoid_: category, label, bucket

**Screener**:
Box `INBOX/Screener`. Mail from senders the Routing has no Destination for. A
decision is owed, and it is owed per sender rather than per Message.
_Avoid_: quarantine, gatekeeper, junk

**Feed**:
Box `INBOX/Feed`. Mail to skim rather than answer, marked read on arrival.
_Avoid_: newsletters, subscriptions

**Paper Trail**:
Box `INBOX/Paper Trail` — the name has a space in it. Receipts, confirmations,
records; marked read on arrival.
_Avoid_: PaperTrail, receipts, archive

**Block**:
A Destination and a Box. Blocking a sender discards their next mail; the mail
already waiting goes to `INBOX/Screener/Block`, where a mistake can still be
found.
_Avoid_: spam, junk (those are the provider's), blacklist

**Aside**:
Box `INBOX/Aside`, the read-later pile. A conversation is moved there and moved
back as a whole — setting one Message aside takes the rest of its thread out of
the Inbox with it, so the two never show halves of the same conversation. The
Routing never fills it, because "read this later" is a decision about a
conversation and not about who sent it. A reply landing in a thread whose
Messages are in Aside pulls them back to the Inbox: the pile is for mail that is
waiting, not for a live thread. A copy in Sent, or a sibling in the Screener,
stays where it is.
_Avoid_: snooze, star, later, set-aside (as a verb)

**Reply Later**:
Box `INBOX/Reply Later` — the name has a space in it. The pile of conversations
you owe a reply. Like the Aside pile it takes and releases a whole thread at a
time, and the Routing never fills it: "I owe this a reply" is a decision about a
conversation, not about who sent it. Answering the conversation — or a reply
arriving in it from anyone — pulls its Reply Later Messages back to the Inbox:
the debt is paid, or the conversation is live again.
_Avoid_: snooze, follow-up, todo, reply-later (as a verb)

**Message**:
One email, identified by its RFC822 Message-ID within an Account. Survives being
moved between Boxes.
_Avoid_: sequence number, uid (that names a Placement)

**Placement**:
Where a Message currently sits: a Box, a uid, and its flags. A Message usually has
one; a mail sent to yourself has two. Named `[account/]box:uid` —
`INBOX/Screener:342` on the Primary Account, `gmx/INBOX:412` on a Secondary one.
_Avoid_: copy, location, instance

**Thread**:
Messages linked by References and In-Reply-To, built by the Daemon across every
Box. Confined to one Account: the same conversation reaching two Accounts is two
Threads. A Box listing collapses a Thread to one row, badged with the whole
Thread's size — every Message wherever it sits, the same number the reader
shows — not just the part of it in the Box being listed.
_Avoid_: conversation, IMAP thread (that is per-folder and narrower)

**Label**:
An IMAP keyword on a Placement. A Message carries as many as you like and keeps
all of them when it moves, which is the whole difference between labelling
something and putting it in a Box. The list is derived from the mail carrying
them; only a Label created and not yet used is remembered in the Mirror. A
listing shows the Labels of the whole Thread, one word each — a space would
reach the server as two keywords.
_Avoid_: tag, folder, category

**Search**:
A ranked full-text query answered entirely from the Mirror, over the sender,
recipients, subject and text of every Message outside Trash. Never a server
query.
_Avoid_: IMAP SEARCH, semantic search

**Attachment**:
A non-text part of a Message. Never Mirrored; naming one fetches it.
_Avoid_: file, part

**Outbox**:
The durable local queue of Messages accepted for Send. A Message is in it before
SMTP has seen it and stays in it afterwards, so "did that go out?" has an answer.
The one place where the Mirror leads the server rather than following it, and the
one file that is never rebuilt.
_Avoid_: queue, drafts, spool

**Held**:
An Outbox Message that was at the SMTP server when the Daemon stopped. It may or
may not have been delivered, so nothing sends it again until a caller says so.
_Avoid_: failed, stuck, pending

## Calendars and contacts

**Collection**:
A CalDAV or CardDAV collection on a server — one calendar, one task list, or one
address book. Found by enumerating the server and matching its display name, never
by a URL written down by hand.
_Avoid_: folder, home, calendar (that is one kind of Collection)

**Calendar**:
A Collection of Events.
_Avoid_: Kalender (that is one Calendar's name)

**Event**:
A timed or all-day entry on a Calendar.
_Avoid_: meeting, appointment

**Todo**:
Work on a task-list Collection. Undated by default; completing it ends it.
_Avoid_: task, reminder

**Habit**:
A repeating per-day practice. Completing one day does not end it. Not an Event and
not a Todo.
_Avoid_: recurring task, recurring event

**Contact**:
A person on an address-book Collection. The vCard is the record; the name,
addresses and numbers the Mirror holds beside it are a projection.
_Avoid_: address, card
