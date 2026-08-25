# mailbox

CLI for one [mailbox.org](https://mailbox.org) account. Talks IMAP, SMTP, CalDAV, and CardDAV itself — mail, Kontakte, Kalender, and Aufgaben.

Built for coding agents; works from a terminal.

```bash
mailbox box list --json
mailbox screener list --json
mailbox thread INBOX/Screener:342 --json
mailbox compose --to someone@mailbox.org --subject "Hi" -m "..."
mailbox events --json
mailbox tasks --json
mailbox contacts search QUERY --json
```

`mailbox` prints the full command list. `mailbox help output` is formats. `mailbox commands --json` is the machine index.

## Install

Go 1.25+.

```bash
git clone https://github.com/parnoldx/mailbox-cli.git
cd mailbox-cli
make install
```

Installs to `~/.local/bin/mailbox`. `make build` writes `bin/mailbox` instead.

## Auth

Reads the Thunderbird profile (newest `prefs.js`) when env is unset. WSL looks under `/mnt/c/Users/*/AppData/Roaming/Thunderbird`; otherwise `~/.thunderbird`.

```
MAILBOX_EMAIL
MAILBOX_PASSWORD
MAILBOX_IMAP_HOST          # default imap.mailbox.org
MAILBOX_IMAP_PORT          # default 993
MAILBOX_SMTP_HOST          # default smtp.mailbox.org
MAILBOX_SMTP_PORT          # default 465
MAILBOX_CALDAV_KALENDER
MAILBOX_CALDAV_AUFGABEN
MAILBOX_CARDDAV_KONTAKTE
MAILBOX_TB_HOME
MAILBOX_TB_PROFILE
```

`mailbox doctor` checks credentials, IMAP, CalDAV/CardDAV URLs, and the installed skill.

## Boxes

| Name | IMAP | Role |
|---|---|---|
| Inbox | `INBOX` | accepted senders |
| Feed | `INBOX/Feed` | skim |
| Paper Trail | `INBOX/Paper Trail` | receipts |
| Screener | `INBOX/Screener` | unknown sender; decision owed |
| Block | `INBOX/Screener/Block` | blacklist this sender |
| Archive | `Archive/…` | topic filing; searchable |
| Drafts | `Drafts` | unsent |
| Sent | `Sent` | copies of mail this CLI delivered |
| Trash | `Trash` | `mailbox trash` |
| Junk | `Junk` | `mailbox spam` |

`mailbox box list` is the routing boxes. `--archive` is the Archive tree. Names match aliases (`feed`) and folder names (`Inbox/Feed`).

Approve, deny, and move only IMAP-move. Sieve routing is owned elsewhere; the next mail from that sender may still land in Screener.

## IDs and output

Message IDs are `[box:]uid` copied from list/search. Bare uid means Inbox (`36722`); otherwise `feed:12` or a path (`Archive/Immo:4`). Attachment IDs are `[box:]uid:index` from `attachment list`. Event, task, and contact IDs come from their lists.

`--json` is `{ok, data}` plus `truncated`/`notice` when a list or thread was cut. `--jq EXPR` filters that envelope (needs `jq`). `--ids-only` is one ID per line; `--count` is a number.

`-m` on compose is Markdown (converted to HTML). `--message-html` is raw HTML. `--draft` saves to Drafts instead of sending.

## Agent skill

```bash
mailbox skill install
```

Copies the packaged skill into `~/.agents/skills/mailbox`, and into `~/.grok` / `~/.claude` if those skill dirs exist.
