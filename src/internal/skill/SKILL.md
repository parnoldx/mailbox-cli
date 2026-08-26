---
name: mailbox
description: |
  Interact with mailbox.org via the mailbox CLI. Read and send emails, manage contacts,
  boxes, calendars, todos, and habits. Use for ANY mailbox-related question or action.
triggers:
  - mailbox
  - /mailbox
  - mailbox box
  - mailbox search
  - mailbox thread
  - mailbox contact
  - mailbox screener
  - mailbox reply
  - mailbox compose
  - mailbox draft
  - mailbox attachment
  - mailbox calendar
  - mailbox event
  - mailbox todo
  - mailbox habit
  - mailbox aside
  - mailbox seen
  - mailbox unseen
  - mailbox move
  - mailbox trash
  - mailbox spam
  - screen a sender
  - approve a sender
  - deny a sender
  - check my email
  - read email
  - send email
  - reply to email
  - forward email
  - compose email
  - list mailboxes
  - search email
  - find email
  - list contacts
  - add contact
  - check calendar
  - add todo
  - complete todo
  - my emails
  - my inbox
  - my todos
  - my calendar
  - mailbox.org
invocable: true
argument-hint: "[command] [args...]"
---

# /mailbox - mailbox.org Email Workflow Command

CLI for one mailbox.org account: boxes, email threads, contacts, replies, compose, calendars, todos, and habits. IMAP + CalDAV + CardDAV. Not HEY.

## Agent Invariants

**MUST follow these rules:**

1. **Choose the right structured output** — use `--jq '<expression>'` to filter or extract fields and `--json` for the full response. `--jq` shells out to the `jq` binary and implies `--json`. Never pipe mailbox to an external `jq`.
2. **One ID scheme** — messages are `[box:]uid`. Bare uid is Inbox (`36722`). Else `feed:12`, `screener:342`, `drafts:12`, or a path (`Archive/Immo:4`). Copy IDs from box view, search, or screener list. Attachment IDs are `[box:]uid:index` from `attachment list`. Draft verbs take a bare uid (`12`) as Drafts.
3. **Approve, deny, and move IMAP-move.** Routing (Sieve `logic`) is updated on the VPS. The next mail from that sender may still land in Screener. Deny `--spam` still goes to Block; mailbox.org has no spam trainer.
4. **Incomplete thread without `--allow-partial` is a failed read**, not a whole thread. `--html` is the original HTML. `attachment list` is the same.
5. **`mailbox spam` IMAP-moves to Junk. `mailbox trash` IMAP-moves to Trash.** Neither blacklists. Seen/unseen set the IMAP `\Seen` flag.

## Output Filtering

`--jq` filters the full JSON success envelope, so result data is under `.data`. String results print as plain text; objects and arrays print as formatted JSON. Use `--quiet --jq` when the expression should run against result data directly. Errors retain their complete structured envelope. A TTY is a table; a pipe is `{ok, data}` unless `--styled`.

```bash
mailbox box list --jq '.data[] | {id, name}'
mailbox search "quarterly planning" --jq '.data[].id'
mailbox box list --quiet --jq '.[].name'
```

An empty result is an empty array rather than `null`, so `.data[]` is safe to run against a
listing that found nothing.

For the two commonest shapes there is no need for an expression at all: `--ids-only` prints
one ID per line and `--count` prints a bare number, both on stdout with any pagination
notice on stderr. Both need list data, so they work on `mailbox box list`, `mailbox box view`,
`mailbox search`, `mailbox screener list`, `mailbox aside`, `mailbox draft list`,
`mailbox attachment list`, `mailbox calendar list`, `mailbox event list`, `mailbox todo list`,
`mailbox habit list`, and `mailbox contact list`.

`--page` / `--all` page lists. `--markdown` is a table (lists) or one document (threads).

## Quick Reference

| Task | Command |
|------|---------|
| List boxes | `mailbox box list --json` |
| Archive boxes | `mailbox box list --archive --json` |
| List emails in a box | `mailbox box view feed --json` |
| Search email | `mailbox search "quarterly planning" --json` |
| List search filters | `mailbox search filters --json` |
| Read email thread | `mailbox thread 36722 --json` |
| List files in a thread | `mailbox attachment list 36722 --json` |
| Save a file | `mailbox attachment save 36722:1` |
| Reply | `mailbox reply 36722 -m "Friday works."` |
| Forward | `mailbox forward 36722 --to alice@example.com -m "FYI"` |
| Compose | `mailbox compose --to alice@example.com --subject "Lunch plans" -m "Are you free Friday?"` |
| List drafts | `mailbox draft list --json` |
| Draft for review | `mailbox compose --to alice@example.com --subject "Lunch plans" -m "Free Friday?" --draft` |
| Read a draft | `mailbox draft show 12 --json` |
| Change a draft | `mailbox draft edit 12 --subject "New subject"` |
| Send a draft | `mailbox draft send 12` |
| Trash a draft | `mailbox draft delete 12` |
| Who is waiting in Screener | `mailbox screener list --json` |
| Number waiting | `mailbox screener list --count` |
| Let a sender through | `mailbox screener approve screener:342` |
| Turn a sender away | `mailbox screener deny screener:342` |
| Mark as seen | `mailbox seen 36722` |
| Mark as unseen | `mailbox unseen 36722` |
| Move email | `mailbox move 36722 --to feed` |
| Set aside (read-later) | `mailbox aside 36722 [--remind 2h]` |
| List the Aside pile | `mailbox aside --json` |
| Return from Aside | `mailbox aside done Aside:12` |
| Move to Trash | `mailbox trash 36722` |
| Mark as spam | `mailbox spam 36722` |
| List contacts | `mailbox contact list --json` |
| Find a contact | `mailbox contact search jane --json` |
| View contact and note | `mailbox contact show <id> --json` |
| Add contact | `mailbox contact add --name "Jane Doe" --email jane@example.com` |
| Edit contact | `mailbox contact update <id> --name "Jane Dawson"` |
| Refresh contacts from CardDAV | `mailbox contact refresh --json` |
| List calendars | `mailbox calendar list --json` |
| List events | `mailbox event list --json` |
| Add an event | `mailbox event add "Design review" --starts-on 2026-09-02 --start-time 14:00` |
| List todos | `mailbox todo list --json` |
| Add todo | `mailbox todo add --title "Draft the quarterly report"` |
| Complete todo | `mailbox todo complete <id>` |
| List habits | `mailbox habit list --json` |
| Create habit | `mailbox habit create "Gym" --days mon,wed,fri` |
| Tick today's habit | `mailbox habit complete <id>` |
| Check credentials and skill | `mailbox doctor --json` |
| Refresh this skill | `mailbox skill install` |

`mailbox commands --json` lists the rest. `mailbox help output` is formats.

## Decision Trees

### Reading Email

```
Want to read email?
├── Which box? → mailbox box list --json
├── Archive boxes? → mailbox box list --archive --json
├── List emails in a box? → mailbox box view <name|id> --json
├── Search? → mailbox search <query> --json
├── Need available refinements? → mailbox search filters --json
├── Read full thread? → mailbox thread [box:]UID --json
│   ├── document → --markdown
│   └── incomplete → --allow-partial, or stop
├── List or save files? → mailbox attachment list [box:]UID --json / mailbox attachment save [box:]UID:INDEX
├── Mark as seen? → mailbox seen [box:]UID
├── Mark as unseen? → mailbox unseen [box:]UID
├── Move to another box? → mailbox move [box:]UID --to inbox|feed|trail|block
├── Set aside? → mailbox aside [box:]UID [--remind 2h]
├── Done with Aside? → mailbox aside done Aside:12
├── Move to Trash? → mailbox trash [box:]UID
├── Mark as spam? → mailbox spam [box:]UID
├── Who is waiting to be screened? → mailbox screener list --json
└── Screen a sender in or out? → mailbox screener approve|deny [box:]UID
```

### Sending Email

```
Want to send email?
├── Reply? → mailbox reply [box:]UID -m "message"
│   ├── Open editor? → mailbox reply [box:]UID (omit -m to open $EDITOR)
│   └── Attach files? → add --attach ./report.pdf (repeatable)
├── Forward? → mailbox forward [box:]UID --to <email>
│   └── Add a note? → add -m "note"
├── Compose new? → mailbox compose --to <email> --subject "Subject"
│   ├── With body? → add -m "Body"
│   ├── With files? → add --attach ./report.pdf (repeatable; >10 MiB uploads to transfer.adminforge.de, link goes in body)
│   ├── HTML? → --message-html instead of -m/--message
│   ├── With CC? → add --cc <email>
│   └── With BCC? → add --bcc <email>
├── Draft instead of sending? → add --draft to compose or reply
│   ├── Read it back? → mailbox draft show 12 --json
│   ├── Change it? → mailbox draft edit 12 --subject/--to/--cc/--bcc/-m (flags replace; omitted fields are kept)
│   ├── Deliver it? → mailbox draft send 12
│   └── Discard it? → mailbox draft delete 12
└── Check drafts? → mailbox draft list --json
```

### Calendar, Todos, Habits

```
Want to manage time?
├── Which calendars? → mailbox calendar list --json
├── Events this week? → mailbox event list --json
├── One calendar? → mailbox event list --calendar Maybe --json
├── One event? → mailbox event show ID --json
├── Add? → mailbox event add "Dentist" --starts-on 2026-08-22 --start-time 09:00 --end-time 10:00
├── Edit / delete? → mailbox event edit ID --notes "..." / mailbox event delete ID
├── Todos? → mailbox todo list --json
├── Add a todo? → mailbox todo add --title "Call landlord" [--date 2026-08-21]
├── Complete / undo / delete? → mailbox todo complete|uncomplete|delete ID
├── Habits? → mailbox habit list --json
├── Add a habit? → mailbox habit create "Gym" --days mon,wed,fri
└── Tick today? → mailbox habit complete ID
```

### Contacts

```
Want to manage contacts?
├── List? → mailbox contact list --json
├── Find one? → mailbox contact search QUERY --json
├── Force fresh read? → mailbox contact refresh --json
├── One contact and note? → mailbox contact show ID --json
├── Add? → mailbox contact add --name "Jane Doe" --email jane@example.com
└── Edit? → mailbox contact update ID --name "Jane Dawson"
```

## Resource Reference

### Email - Boxes

```bash
mailbox box list --json
mailbox box list --archive --json
mailbox box view feed --json
mailbox box view Inbox/Feed
mailbox box view feed --page <next_page> --json
```

| Name | IMAP | Role |
|---|---|---|
| Inbox | `INBOX` | accepted senders |
| Feed | `INBOX/Feed` | skim |
| Paper Trail | `INBOX/Paper Trail` | receipts (space in the name) |
| Screener | `INBOX/Screener` | sender unknown; decision owed |
| Block | `INBOX/Screener/Block` | blacklist this sender |
| Aside | `INBOX/Aside` | read-later pile |
| Archive | `Archive/…` | topic filing; searchable |
| Drafts | `Drafts` | unsent |
| Sent | `Sent` | dest of compose / `draft send` |
| Trash | `Trash` | dest of `trash` |
| Junk | `Junk` | dest of `spam`; this copy |

`mailbox box list` is Inbox/Feed/Paper Trail/Screener/Block/Aside/Drafts/Sent. `--archive` is the Archive tree. `box view` takes a name or id; names match aliases (`feed`) and box names (`Inbox/Feed`, `Feed`). Unknown names are refused.

`--box` / `--to` accept `inbox`, `feed`, `trail`, `block`, plus `The Feed` and `paper trail`. Approve dests are inbox, feed, trail (default inbox). Deny is Block. Move dests are those four. Trash and Junk are not box view or `--to`.

**Response format:** each row has `id` (`[box:]uid`), `from` (display name), `summary` (body preview, else subject), `date`. `--json` also has `subject` and `flags`. `--detail` shows flags in the table. Use `id` for thread, reply, forward, move, seen, unseen, trash, spam, aside, and attachment list.

`next_page` is the cursor `--page` takes. `--all` reads to the end instead.

### Email - Search

```bash
mailbox search "quarterly planning" --json
mailbox search --from jane@example.com --date last_30_days --json
mailbox search --subject invoice --attachment pdfs --all --json
mailbox search filters --json
```

IMAP over routing boxes plus Archive. `--from`/`--to`/`--subject` are IMAP FROM/TO/SUBJECT. `--required`/`--any`/`--none`/`--exact` are TEXT words. `--in`, `--date`, and `--attachment` accept only the values `mailbox search filters` lists. An unrecognized value is refused before anything is sent.

Default search covers Inbox/Feed/Paper Trail/Screener plus Archive.

**Response format:** same row shape as box view. `--page` selects one result page; `--all` reads everything.

### Email - Threads

```bash
mailbox thread 36722 --json
mailbox thread feed:12 --json
mailbox thread 36722 --html
mailbox thread 36722 --markdown
mailbox thread 36722 --allow-partial
```

`mailbox thread` walks the IMAP thread, oldest first. Each message `body` is **Markdown** (plain text kept as-is; HTML converted at the edge). `--html` returns the original HTML instead. `body_state` is `hydrated`, `bodyless`, `over_limit`, or `failed`.

An incomplete thread without `--allow-partial` is a failed read (exit 7 / `api`), not a partial result.

There is one ID, not two: the same `[box:]uid` works for thread, reply, forward, move, seen, and attachment list.

### Email - Attachments

```bash
mailbox attachment list 36722 --json
mailbox attachment save 36722:1
mailbox attachment save 36722
mailbox attachment save 36722:1 --output ./reports
mailbox attachment save 36722:1 --output ./report.pdf --force
```

`attachment list` walks the thread the same way `thread` does (incomplete without `--allow-partial` fails). An attachment ID is `[box:]uid:index`. Saving uses the original filename unless `--output` names a destination. Existing files need `--force`. A bare message ID saves that message's only file; several files still need `:index`.

### Email - Reply, Forward & Compose

```bash
mailbox reply 36722 -m "Friday works for me — I'll send an agenda."
mailbox reply 36722
mailbox reply 36722 -m "Here is the wiring diagram." --attach ./diagram.png
mailbox forward 36722 --to alice@example.com
mailbox forward 36722 --to alice@example.com -m "Please review before Thursday."
mailbox compose --to alice@example.com --subject "Lunch plans" -m "Are you free Friday?"
mailbox compose --to alice@example.com --subject "Q3 revenue report" -m "The numbers are attached." --attach ./report.pdf
mailbox compose --to alice@example.com --cc bob@example.com --bcc carol@example.org --subject "Kitchen remodel timeline" -m "Cabinets land the week of the 14th."
mailbox compose --to alice@example.com --subject "Sprint recap" -m "We **shipped** the pagination fix."
mailbox compose --to alice@example.com --subject "Newsletter draft" --message-html "<h1>March</h1><p>What we shipped.</p>"
```

`-m` / `--message` is Markdown, converted to HTML on the way out. `--message-html` is raw HTML. The two are mutually exclusive. Omit both to read stdin or `$EDITOR`.

`compose` SMTP-sends and copies to Sent. `--draft` saves to IMAP Drafts instead.

### Email - The Screener

```bash
mailbox screener list --json
mailbox screener list --count
mailbox screener approve screener:342
mailbox screener approve screener:342 --box "The Feed"
mailbox screener deny screener:342 screener:343
mailbox screener deny screener:342 --spam
```

Screener is first-time senders. `screener list` returns message IDs with the sender and a summary. `--count` is a bare number.

Approving IMAP-moves that message into Inbox (or `--box` feed/trail). Denying IMAP-moves it into Block. `--spam` is still Block — mailbox.org has no spam trainer. Routing updates happen on the VPS; the next mail from that sender may still land in Screener.

### Email - Aside

```bash
mailbox aside --json
mailbox aside 36722
mailbox aside 36722 --remind 2h
mailbox aside done Aside:12
mailbox aside --sweep
```

Aside is the read-later pile (`INBOX/Aside`). `mailbox aside` with IDs moves those messages there. `--remind 30m|2h|3d` stores an `asidedue-…` keyword; serve moves due mail back to Inbox on a 30-minute sweep. `aside done` returns it early. `aside --sweep` runs one pass now.

### Email - Seen, Move, Trash, Spam

```bash
mailbox seen 36722
mailbox seen 36722 feed:12
mailbox unseen 36722
mailbox move 36722 --to feed
mailbox move 36722 36723 --to trail
mailbox trash 36722
mailbox spam 36722
```

All take `[box:]uid`. Move dests are inbox, feed, trail, block. Trash is Trash. Spam is Junk. Neither trash nor spam changes Routing or blacklists the sender.

### Drafts

```bash
mailbox compose --subject "Board update" -m "Numbers to follow." --draft
mailbox reply 36722 -m "Drafting this." --draft
mailbox draft list --json
mailbox draft show 12 --json
mailbox draft edit 12 --to alice@example.com --subject "Board update (v2)"
mailbox draft send 12
mailbox draft delete 12
```

`--draft` on compose or reply saves to IMAP Drafts. Draft verbs take a bare uid (`12`); `drafts:12` still works. An edit is a revision: each flag replaces its field; omitted flags stay. `draft send` SMTP-delivers and copies to Sent. `draft delete` trashes it.

`--all` on a list reads everything; otherwise `--limit` (default 50) and `--page` (from `next_page`) page it.

### Calendars

```bash
mailbox calendar list --json
```

Discovered Event calendars. `mailbox-habits` is omitted. `--calendar` on event/todo lists takes a discovered name (not mailbox-habits).

### Events

```bash
mailbox event list --json
mailbox event list --calendar Maybe --starts-on 2026-01-01 --ends-on 2026-01-31 --json
mailbox event show ID --json
mailbox event add "Design review" --starts-on 2026-09-02 --start-time 14:00 --end-time 15:00
mailbox event add "Sarah's birthday" --starts-on 2026-09-02
mailbox event add "Standup" --start-time 09:15 --repeat every_weekday --remind 10m
mailbox event edit ID --title "Design review (moved)"
mailbox event delete ID
```

No `--start-time` is all-day; a `--start-time` with no `--end-time` runs an hour. Dates are `YYYY-MM-DD`, times `HH:MM`, timezone Europe/Berlin, default window now + 7 days. `--repeat` is `every_day|every_weekday|every_week|every_other_week|every_day_of_month|every_year`. `--circle` is PRIORITY=1.

### Todos

```bash
mailbox todo list --json
mailbox todo add --title "Draft the quarterly report"
mailbox todo add --title "Book the venue" --date 2026-09-04
mailbox todo complete ID
mailbox todo uncomplete ID
mailbox todo delete ID
```

Todos live on CalDAV **Aufgaben**. Default undated (the week pile). `--date` is a due day so other clients round-trip; a due Todo also appears on that day.

### Habits

```bash
mailbox habit list --json
mailbox habit list --date 2026-09-02 --json
mailbox habit create "Gym" --days mon,wed,fri
mailbox habit create "Practice piano" --icon music --color green --days mon,wed,fri
mailbox habit edit ID --name "Evening walk"
mailbox habit complete ID
mailbox habit complete ID --date 2026-03-15
mailbox habit uncomplete ID
mailbox habit delete ID
```

Habits are a JSON bag on calendar mailbox-habits (hidden from `calendar list` and event lists). Completing one day does not end the habit. `--days` is `mon,wed,fri` (or `0`–`6`).

### Contacts

```bash
mailbox contact list --json
mailbox contact search jane --json
mailbox contact refresh --json
mailbox contact show ID --json
mailbox contact add --name "Jane Doe" --email jane@example.com
mailbox contact add --name "Jane Doe" --email jane@example.com --note "Prefers email"
mailbox contact update ID --name "Jane Dawson"
mailbox contact update ID --note=
```

Contacts live in CardDAV **Kontakte**. IDs are vCard UIDs from `contact list`. `show` includes the private note (vCard NOTE). Updates keep omitted fields. `--note` on add/update writes that note; `--note=` clears it.

Reads are cached on disk forever (`~/.cache/mailbox/contacts.json`). `contact refresh` re-reads the address book; writes refresh the cache too. Edits made by other clients stay invisible until a refresh.

### Authentication

```bash
mailbox doctor --json
mailbox help environment
```

Reads the Windows Thunderbird profile (newest `prefs.js`, or `MAILBOX_TB_PROFILE`). Env overrides live in `mailbox help environment`. IMAP/SMTP use the imap.mailbox.org password; CalDAV/CardDAV use dav.mailbox.org (`MAILBOX_DAV_PASSWORD`, else that Thunderbird login, else `MAILBOX_PASSWORD`).

`mailbox doctor --json` checks credentials, IMAP, CalDAV/CardDAV, and whether the installed skill matches this binary. If the skill check fails, run `mailbox skill install`.
