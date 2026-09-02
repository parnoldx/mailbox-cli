# mailbox

`mailbox` is a fast, agent-oriented CLI and background daemon for email, calendars, tasks, daily habits, and contacts.

Instead of hitting IMAP, SMTP, CalDAV, CardDAV, and ManageSieve servers on every command, a single background daemon maintains a local SQLite **Mirror** of server state. Read commands are answered directly from the local mirror in milliseconds with zero network latency, while write operations synchronize with remote servers to guarantee consistency.

```
┌────────────────────────────────────────────────────────┐
│               Agents / Scripts / Widgets               │
│         (mailbox CLI, mailbox.clock, Skills)           │
└───────────────────────────┬────────────────────────────┘
                            │ NDJSON over Unix Socket
┌───────────────────────────▼────────────────────────────┐
│                    mailbox daemon                      │
│   ┌─────────────────────┐       ┌──────────────────┐   │
│   │    SQLite Mirror    │       │  Durable Outbox  │   │
│   └──────────▲──────────┘       └─────────┬────────┘   │
└──────────────┼────────────────────────────┼────────────┘
               │ IMAP / CalDAV / CardDAV    │ SMTP
┌──────────────▼────────────────────────────▼────────────┐
│                     Mail & DAV Servers                 │
└────────────────────────────────────────────────────────┘
```

---

## Highlights

- **Instant Local Reads**: Commands like `mailbox status`, `mailbox box view`, `mailbox agenda`, or `mailbox search` read from the local SQLite mirror (~1ms response time) and work offline.
- **Write-Through Consistency**: Modifying commands (`move`, `seen`, `event add`, `todo done`, `route set`) wait for the server acknowledgment before updating the mirror, ensuring an exit code of `0` means the change is live.
- **Durable Outbox**: Sent mail is staged in a durable local queue before SMTP submission and retained for auditing and automatic retry.
- **Server-Side Sieve Routing**: Owns and compiles Sieve filtering scripts on the server to automatically route mail into `Inbox`, `Feed`, `Paper Trail`, `Screener`, or `Block`.
- **First-Class Agent Support**: Built-in `--json` envelopes with mirror freshness metadata, self-describing command discovery (`mailbox commands`), predictable exit codes, and an agent skill (`skill/SKILL.md`).
- **Unified Personal Data Surface**: Covers email threads and attachments alongside RFC 5545 iCalendar (events, todos, habits) and RFC 6350 vCard (contacts).

---

## Architecture & Concepts

### 1. The Mirror
The daemon keeps an offline-capable SQLite database representing the read model of all configured mailboxes and collections.
- **Freshness & Behind Notices**: Every response includes mirror sync metadata. If the mirror has pending changes or disconnected networks, `mailbox` reports `behind: true` rather than failing or blocking.
- **Disposable**: The mirror can be deleted and reconstructed at any time; no authoritative local-only data is lost.

### 2. The Outbox
The outbox stores outgoing messages on disk. If a network disruption occurs during submission or if the daemon restarts, the message enters a `Held` state rather than being silently dropped or endlessly duplicated.

### 3. Server-Side Routing & The Screener
Mail routing uses a structured Sieve script managed directly on the server:
- **Inbox**: Important mail requiring attention.
- **Feed**: Newsletters, automated updates, and reading material (marked read on arrival).
- **Paper Trail**: Receipts, bills, delivery notices, and transactions (marked read on arrival).
- **Screener**: Mail from first-time or unknown senders awaiting a routing decision.
- **Block**: Unwanted senders dropped at the server.

---

## Installation & Setup

### Prerequisites
- **Linux** (systemd user session recommended)
- **Go 1.22+**
- **Node.js** (optional, for running plugin test suites)

### Building and Installing

```bash
# Build the binary into bin/mailbox
make build

# Run unit tests
make test

# Install mailbox to ~/.local/bin/mailbox
make install

# Install the agent skill to ~/.agents/skills and ~/.claude/skills
make skill
```

### Initial Configuration

Run the interactive setup wizard to discover folders, calendars, address books, and configure credentials:

```bash
mailbox setup
```

Configuration is stored securely in `~/.config/mailbox/config.toml` (mode `0600`).

### Running the Daemon

You can run the daemon directly in the foreground:

```bash
mailbox daemon
```

Or enable the systemd user service and socket (configured during `mailbox setup`):

```bash
systemctl --user enable --now mailbox.socket
```

---

## Command Reference

### Mail & Reading
```bash
mailbox status                     # Summary of unread/screener mail & mirror health
mailbox box list                   # List all boxes and message counts
mailbox box view Screener --limit 20 # View recent messages in a box
mailbox message view 36722         # View full headers and text of a message
mailbox thread 36722               # View an entire conversation thread
mailbox search "rechnung" --in feed # Full-text search across mirrored messages
mailbox attachment list 36722      # List files attached to a message
mailbox attachment save 36722:1    # Save attachment to disk
```

### Composing & Sending
```bash
mailbox compose --to user@example.com --subject "Hello" --body "Message body"
mailbox reply 36722 --body "Thanks for the update."
mailbox forward 36722 --to colleague@example.com
mailbox compose --to user@example.com --subject "Draft" --body "WIP" --draft  # File in drafts instead of sending
mailbox draft list                 # Mail written but not yet sent
mailbox draft send 12              # Send a draft (optionally override --to/--subject/--body)
mailbox outbox list                # View queue status and held messages
mailbox outbox retry 1             # Retry a held outgoing message
```

### Organization & Routing
```bash
mailbox screener                   # List senders waiting for a routing decision
mailbox route set sender@domain --to feed        # Route sender to Feed
mailbox route set sender@domain --to inbox       # Route sender to Inbox
mailbox route set sender@domain --to paper        # Route sender to Paper Trail
mailbox route set sender@domain --to block       # Block sender at server level
mailbox aside add 36722            # Put a message into the read-later pile
mailbox reply-later add 36722      # Put a message into the reply-later pile
mailbox move 36722 --to Archive    # Move message to a box
mailbox seen 36722                 # Mark message as read
mailbox unseen 36722               # Mark message as unread
mailbox trash 36722                # Move message to Trash
mailbox spam 36722                 # Move message to Junk
mailbox label add 36722 --to Rechnungen  # Put a label on a message
mailbox label list                # Labels, and how much mail carries each
mailbox label view Rechnungen     # Mail carrying a label
```

### Calendars, Todos, Habits & Contacts
```bash
mailbox agenda --days 7            # Show upcoming events and due tasks
mailbox calendar list              # List all discovered calendars and task lists
mailbox event add "Team Sync" --start "tomorrow 10:00" --end "tomorrow 11:00"
mailbox todo list                  # List active tasks
mailbox todo add "Pay invoice" --due "2026-09-01"
mailbox todo done 42               # Mark task completed
mailbox habit list                 # View daily habits and streaks
mailbox habit done "meditation"    # Log today's habit completion
mailbox contact search "Jane"      # Search address books
mailbox contact add "Jane Doe" --email jane@example.com --phone "+1 555 0199"
```

### System & Diagnostics
```bash
mailbox doctor                     # Inspect daemon, socket, mirror, and server connectivity
mailbox sieve get                  # Retrieve active server Sieve script
mailbox commands                   # Output machine-readable command registry
mailbox version                    # Print version information
```

---

## Agent & Automation Integration

### Structured Output (`--json`)
Every command supports `--json`, returning a structured envelope:

```json
{
  "ok": true,
  "data": { ... },
  "mirror": {
    "behind": false,
    "synced_at": "2026-08-30T16:00:00Z"
  }
}
```

### Standard Exit Codes
- `0`: Success (server acknowledged for writes, mirror answered for reads).
- `1`: Usage error (invalid syntax, missing argument, unknown flag).
- `2`: Not found (no message, event, contact, or item matches the ID).
- `7`: Server or daemon error (network down, credential rejected, server failure).
- `9`: No daemon listening on the Unix socket.

### Skills & Plugins
- **Agent Skill**: [`skill/SKILL.md`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/skill/SKILL.md) provides instruction mappings for AI coding assistants.
- **Calendar Bar Widget**: [`plugins/mailbox.clock/`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/plugins/mailbox.clock) — Omarchy / Quickshell calendar and reminder widget backed directly by the daemon socket.
- **Mail Notification Widget**: [`plugins/mailbox.email/`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/plugins/mailbox.email) — Omarchy bar widget and dropdown panel for new-mail alerts and sender screening, also on the daemon socket.
- **Desktop Client**: [`gui-omarchy/`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/gui-omarchy) — a HEY-style Qt desktop mail client that follows the live Omarchy theme.

---

## Configuration & Environment Variables

| Variable | Description | Default |
|---|---|---|
| `MAILBOX_CONFIG` | Path to configuration file | `~/.config/mailbox/config.toml` |
| `MAILBOX_SOCKET` | Path to Unix socket | `$XDG_RUNTIME_DIR/mailbox.sock` |
| `MAILBOX_MIRROR` | Path to SQLite mirror database | `~/.local/share/mailbox/mirror.db` |
| `MAILBOX_OUTBOX` | Path to outbox spool directory | `~/.local/share/mailbox/outbox/` |
| `MAILBOX_FOLDER` | Comma-separated boxes to mirror (dev only) | *(All boxes)* |

---

## Development

```bash
# Run unit tests across all packages
make test

# Format code
make fmt

# Static analysis and live tag verification
make vet

# Run live server integration tests (requires live config in ~/.config/mailbox/config.toml)
make live LIVE=./internal/sievedrv/
make live LIVE=./internal/davdrv/
make live LIVE=./internal/imapdrv/
```

Architecture Decision Records are documented in [`docs/adr/`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/docs/adr). Detailed design notes are in [`docs/DESIGN.md`](file:///home/pa/Work/tries/2026-08-29-mailbox-cli/docs/DESIGN.md).
