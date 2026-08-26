---
name: mailbox
description: >
  Use the mailbox CLI for user@mailbox.org — list, search, read, screen,
  move, compose, and send mail; drafts; Kontakte contacts; Kalender events; Aufgaben tasks.
  Trigger on email, inbox, screener, aside, set aside, read later, contact, calendar, tasks.
---

# mailbox

`mailbox` talks IMAP + CalDAV + CardDAV to mailbox.org. Not himalaya. Not Spark.

```bash
mailbox [--json|--jq EXPR|--ids-only|--count|--markdown] box|search|thread|screener|move|aside|seen|unseen|trash|spam|compose|draft|attachment ...
mailbox aside [ID...] [--remind 30m|2h|3d]
mailbox aside done ID...
mailbox thread [box:]UID [--json|--html|--markdown|--allow-partial]
mailbox compose --to ADDR --subject TEXT [-m TEXT | --message-html HTML] [--draft]
mailbox draft list|show|edit|send|delete
mailbox doctor
mailbox commands --json
mailbox skill install
mailbox help output
mailbox events [--start] [--end]
mailbox events show ID
mailbox events create --title TEXT --start WHEN [--end WHEN] [--all-day]
mailbox tasks
mailbox tasks create --title TEXT [--due WHEN]
mailbox tasks complete ID
mailbox contacts list
mailbox contacts search QUERY
mailbox contacts refresh
mailbox contacts show ID
mailbox contacts add --name TEXT --email ADDR [--note TEXT]
mailbox contacts update ID [--name TEXT] [--email ADDR] [--note TEXT]
```

## Invariants

1. `--json` returns `{ok, data}` plus `truncated`/`notice` when a list or thread was cut. `--jq EXPR` filters that envelope (needs `jq`; `--quiet --jq` filters `data`). `--ids-only` is one ID per line; `--count` is a bare number. Truncation notices for those two go to stderr. `--markdown` is a table (lists) or one document (threads).
2. Message IDs are `[box:]uid` — bare uid means INBOX (`36722`), else `box:uid` with box one of `feed`, `trail`, `screener`, `aside`, `drafts`, `sent`, or a full box path for Archive sub-boxes (`Archive/Immo:4`). Copy them from box view/search/screener list. Attachment IDs are `[box:]uid:index` from `attachment list`.
3. Approve, deny, and move IMAP-move. emailMoveHelper updates Routing. The next mail from that sender may still land in Screener. Deny `--spam` still goes to Block; mailbox.org has no spam trainer.
4. `compose` SMTP-sends and copies to Sent. `--draft` saves to IMAP Drafts instead. `draft send` delivers a saved draft; `draft delete` trashes it.
5. An incomplete thread without `--allow-partial` is a failed read, not a whole thread. `--html` goes to a file, not a terminal. `attachment list` is the same.
6. `mailbox spam` IMAP-moves to Junk (this copy). `mailbox trash` IMAP-moves to Trash. Neither blacklists. Seen/unseen set the IMAP `\Seen` flag.
7. `aside` moves a Message to the Aside pile (read-later); `--remind 2h` stores an `asidedue-…` keyword and serve moves it back to Inbox when due (30-min sweep). `aside done ID...` returns it early. `mailbox aside` alone lists the pile.

## Decision

```
Mail?
├── Which mailbox? → mailbox box list --json
├── Archive boxes? → mailbox box list --archive --json
├── How many in Screener? → mailbox screener list --count
├── Who is waiting? → mailbox screener list --json
├── List a box → mailbox box view feed --json
├── Search → mailbox search QUERY [--from ADDR] [--subject TEXT] [--in archive] --json
├── Read → mailbox thread [box:]UID --json
│   ├── document → --markdown
│   └── incomplete → --allow-partial, or stop
├── Approve → mailbox screener approve [box:]UID [--box feed]
├── Deny → mailbox screener deny [box:]UID [--spam]
├── Move → mailbox move [box:]UID --to feed
├── Set aside → mailbox aside [box:]UID [--remind 2h]
├── Done reading → mailbox aside done Aside:12
├── Seen → mailbox seen [box:]UID
├── Unseen → mailbox unseen [box:]UID
├── Trash → mailbox trash [box:]UID
├── Spam → mailbox spam [box:]UID
├── Compose → mailbox compose --to someone@example.com --subject "Hi" -m "..."
│   ├── draft → add --draft
│   ├── files → add --attach ./file.pdf (repeatable; >10 MiB auto-uploads to transfer.adminforge.de, link goes in body)
│   ├── HTML → --message-html instead of -m
│   └── reply → --reply-to [box:]UID -m "..."
├── Drafts → mailbox draft list --json
│   ├── read → mailbox draft show Drafts:12 --json
│   ├── edit → mailbox draft edit Drafts:12 --subject "v2"
│   ├── send → mailbox draft send Drafts:12
│   └── trash → mailbox draft delete Drafts:12
├── List files → mailbox attachment list [box:]UID --json
└── Save a file → mailbox attachment save [box:]UID:INDEX [--output PATH] [--force]
```

```
Kalender / Aufgaben?
├── Events this week → mailbox events --json
├── One event → mailbox events show ID --json
├── Create → mailbox events create --title "Dentist" --start 2026-08-22T09:00 --end 2026-08-22T10:00
├── Tasks → mailbox tasks --json
├── Add → mailbox tasks create --title "Call landlord" --due 2026-08-21
└── Complete → mailbox tasks complete ID
```

```
Kontakte?
├── List → mailbox contacts list --json
├── Find one → mailbox contacts search QUERY --json
├── Force fresh read → mailbox contacts refresh --json
├── One contact and note → mailbox contacts show ID --json
├── Add → mailbox contacts add --name "Jane Doe" --email jane@example.com
└── Edit → mailbox contacts update ID --name "Jane Dawson"
```

`mailbox commands --json` lists the rest. `mailbox help output` is formats. `mailbox doctor --json` checks credentials, IMAP, CalDAV/CardDAV URLs, and the installed skill.

## Auth

Reads the Windows Thunderbird profile (newest `prefs.js`, or `MAILBOX_TB_PROFILE`). Env overrides: `MAILBOX_EMAIL`, `MAILBOX_PASSWORD`, `MAILBOX_IMAP_HOST`, `MAILBOX_IMAP_PORT`, `MAILBOX_SMTP_HOST`, `MAILBOX_SMTP_PORT`, `MAILBOX_CALDAV_KALENDER`, `MAILBOX_CALDAV_AUFGABEN`, `MAILBOX_CARDDAV_KONTAKTE`, `MAILBOX_TB_HOME`.

## Boxes

| Name | IMAP | Role |
|---|---|---|
| Inbox | `INBOX` | accepted senders |
| Feed | `INBOX/Feed` | skim |
| Paper Trail | `INBOX/Paper Trail` | receipts (space in name) |
| Screener | `INBOX/Screener` | sender unknown; decision owed |
| Block | `INBOX/Screener/Block` | blacklist this sender |
| Archive | `Archive/…` | topic filing; searchable |
| Drafts | `Drafts` | unsent |
| Sent | `Sent` | dest of compose / `draft send` |
| Trash | `Trash` | dest of `trash` |
| Junk | `Junk` | dest of `spam`; this copy |

`mailbox box list` is Inbox/Feed/Paper Trail/Screener/Block/Drafts/Sent. `mailbox box list --archive` is the Archive tree. `mailbox box view` takes a name or id; names match aliases (`feed`) and box names (`Inbox/Feed`, `Feed`). Search `--in` is the same. Unknown names are refused.

`--box` / `--to` accept `inbox`, `feed`, `trail`, `block`, plus `The Feed` and `paper trail`. Approve dests are inbox, feed, trail (default inbox). Deny is Block. Move dests are those four. `mailbox trash` and `mailbox spam` are Trash and Junk; they are not box view or `--to`.

```bash
mailbox box list --json
mailbox box list --archive --json
mailbox box view feed --json
mailbox box view Inbox/Feed
mailbox screener list --json
mailbox screener list --count
mailbox screener approve INBOX/Screener:342
mailbox screener approve INBOX/Screener:342 --box "The Feed"
mailbox screener deny INBOX/Screener:342 INBOX/Screener:343
mailbox screener deny INBOX/Screener:342 --spam
mailbox move 12345 --to feed
mailbox move 12345 67890 --to trail
mailbox seen 12345
mailbox unseen 12345 67890
mailbox trash 12345
mailbox spam 12345
mailbox attachment list 456 --json
mailbox attachment save 456:1
mailbox attachment save 456:1 --output ./reports --force
mailbox compose --to alice@example.com --subject "Lunch plans" -m "Are you free Friday?"
mailbox compose --to alice@example.com --subject "Q3 revenue report" -m "The numbers are attached." --attach ./report.pdf
mailbox compose --to alice@example.com --cc bob@example.com --bcc carol@example.org --subject "Kitchen remodel timeline" -m "Cabinets land the week of the 14th."
mailbox compose --to alice@example.com --subject "Sprint recap" -m "We **shipped** the pagination fix."
mailbox compose --to alice@example.com --subject "Newsletter draft" --message-html "<h1>March</h1><p>What we shipped.</p>"
mailbox compose --subject "Board update" -m "Numbers to follow." --draft
mailbox compose --reply-to 342 -m "Friday works."
mailbox draft list --json
mailbox draft list --all
mailbox draft show Drafts:12 --json
mailbox draft edit Drafts:12 --to alice@example.com --subject "Board update (v2)"
mailbox draft send Drafts:12
mailbox draft delete Drafts:12
```

`attachment save` writes the original filename unless `--output` names a file or directory. Existing files need `--force`.

`-m` is Markdown, converted to HTML on the way out. `--message-html` is raw HTML; the two are mutually exclusive. Omit both to read stdin or `$EDITOR`. `--draft` saves to Drafts instead of sending. `draft send` SMTP-delivers and copies to Sent. Draft IDs look like `Drafts:12`. `--all` on `draft list` reads the whole box; otherwise `--limit` (default 50) truncates like box view.

Search is IMAP keyword (TEXT) over routing boxes plus Archive. `--from`/`--to`/`--subject` are IMAP FROM/TO/SUBJECT. Box view/search rows are `id`, `from` (display name), `summary` (body preview, else subject), `date`; `--json` also has `subject` and `flags`. `--detail` shows flags in the table. Thread bodies in `--json` are Markdown; `--html` is the original HTML.

Events use `YYYY-MM-DD` or `YYYY-MM-DDTHH:MM`, timezone Europe/Berlin, default window now + 7 days. Create is personal only.

Contacts live in CardDAV **Kontakte**. IDs are vCard UIDs from `contacts list`. `show` includes the private note (vCard NOTE). Updates keep omitted fields. `--note` on add/update writes that note; `--note=` clears it. Reads are cached on disk forever (`~/.cache/mailbox/contacts.json`); `contacts refresh` re-reads the address book, writes refresh the cache too. Edits made by other clients stay invisible until a refresh.
