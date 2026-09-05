# mailbox.email — Email Notifications & Screening for Omarchy

A Quickshell bar widget and dropdown panel for Omarchy, powered directly by the `mailbox` daemon over its unix socket.

## Features

- **Dynamic Bar Notification**: The email icon appears in the Omarchy top bar **only when there is new mail** (unread messages in watched boxes or pending screening decisions). When all mail is seen and screened, the icon collapses and hides completely.
- **Pure Vector GPU Icon**: Crisp vector envelope rendered with 4x MSAA antialiasing that adapts dynamically to your theme:
  - **Vibrant Blue (`Color.accent`)** when unread mail is waiting in your inbox.
  - **Urgent Red (`Color.urgent`)** when senders are waiting in the Screener.
  - **Bar Foreground** when opened in rest state.
- **Audio & Visual Alerts**:
  - Plays the system new-email notification sound effect when a new message arrives while running (silent on initial startup/reloads).
  - Optional desktop toast notifications.
  - Both audio and visual notifications can be toggled in Settings.
- **Dropdown Email & Screener Panel** — one stream, not tabs:
  - Everything **new** in one reverse-chron list: unread mail and screener
    senders interleaved in the order they arrived. Read mail is never listed —
    that is the desktop client's job.
  - `ALL` / `MAIL` / `SCREENER` chips narrow that one list. `ALL` is the
    default, so opening the panel answers "what's new?" with no mode choice.
  - Screener rows are the same row shape as mail, marked with a hairline, a
    `SCREENER` tag and a sender/volume line — a decision, not a different card.
  - Account switcher with per-account unread count badges.
  - Colored sender avatars with initials deterministically derived from sender addresses.
  - 1-click action buttons on hover/selection:
    - **Mail**: `󰄬` Mark read, `󰔛` Set aside, `󰆴` Move to Trash.
    - **Screener**: 📥 Inbox, 🚫 Block, 🗑 Trash.
- **Opens in the desktop client**: Clicking a message (or <kbd>Enter</kbd>) hands
  it to the mailbox desktop client — `mailbox-gui --open <id>`. That is the
  only mail client we ship, so it is the only target; there is no setting.

- **In-Panel Screening**:
  - Pending senders appear inline in the stream, in arrival order, alongside mail.
  - One-click screening actions on each sender row — screen a sender **in** or
    **out**, nothing more; sorting mail into Feed / Paper Trail is a decision for
    the full desktop client:
    - 📥 **Inbox** (`I`): Route future mail to Inbox and move existing Screener mail to Inbox.
    - 🚫 **Block** (`B`): Block sender and move existing mail to `Screener/Block`.
    - 🗑 **Trash** (`T`): Move sender's screener mail directly to Trash.
- **Live Push Updates**: Connects directly to `$XDG_RUNTIME_DIR/mailbox.sock`. Updates in real time whenever the daemon pushes `mail.changed`.

---

## Keyboard Shortcuts

### Global Desktop Shortcut
- <kbd>Super</kbd> + <kbd>Alt</kbd> + <kbd>Shift</kbd> + <kbd>E</kbd>: Open / toggle the Mailbox panel from anywhere.

### In-Panel Navigation & Actions

Action keys apply to whatever the cursor is on, so they work anywhere in the
mixed stream: mail keys are ignored on a screener row and vice versa.

| Key | Context | Action |
|---|---|---|
| <kbd>1</kbd> | Global | Chip: **All** (mail + screener) |
| <kbd>2</kbd> / <kbd>U</kbd> | Global | Chip: **Mail** (unread only) |
| <kbd>3</kbd> / <kbd>S</kbd> | Global | Chip: **Screener** only |
| <kbd>R</kbd> | Global | Refresh mail from daemon |
| <kbd>,</kbd> (comma) | Global | Toggle Settings view |
| <kbd>Escape</kbd> | Global | Close panel or exit settings |
| <kbd>j</kbd> / <kbd>k</kbd> / <kbd>↑</kbd> / <kbd>↓</kbd> | Stream | Navigate the stream |
| <kbd>Enter</kbd> / <kbd>Space</kbd> | Stream | **Open** in the desktop client (a screener row opens that sender's newest mail) |
| <kbd>T</kbd> | Any row | **Move to Trash** |
| <kbd>A</kbd> | Mail row | **Set aside** (read later pile) |
| <kbd>M</kbd> | Mail row | **Toggle read / unread** |
| <kbd>I</kbd> | Screener row | Route sender to **Inbox** |
| <kbd>B</kbd> | Screener row | **Block** sender |

---

## Installation

1. Copy the plugin into Omarchy:
   ```bash
   cp -r plugins/mailbox.email ~/.config/omarchy/plugins/
   ```

2. Enable the plugin in Omarchy:
   ```bash
   omarchy plugin enable mailbox.email
   ```
   *(Or add `{ "id": "mailbox.email" }` to `bar.layout` in `~/.config/omarchy/shell.json`)*

3. Bind the desktop shortcut in `~/.config/hypr/bindings.lua`:
   ```lua
   o.bind("SUPER + SHIFT + ALT + E", "Mailbox panel", "omarchy-shell shell toggle mailbox.email")
   ```

---

## Running Tests

```bash
plugins/mailbox.email/tests/run
```
