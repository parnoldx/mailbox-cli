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
- **Dropdown Email & Screener Panel**:
  - Account switcher with per-account unread count badges.
  - Split tabs: `New for you` (unread), `Previously seen` (read), and `Screener`.
  - Colored sender avatars with initials deterministically derived from sender addresses.
  - 1-click action buttons on hover/selection:
    - **In "New for you"**: `󰄬` Mark read, `󰔛` Set aside, `󰆴` Move to Trash.
    - **In "Previously seen"**: `󰔛` Set aside, `󰆴` Move to Trash.
- **Opens in the desktop client**: Clicking a message (or <kbd>Enter</kbd>) hands
  it to the mailbox desktop client — `mailbox-gui --open <id>`. That is the
  only mail client we ship, so it is the only target; there is no setting.

- **In-Panel Screening**:
  - Dedicated screener tab listing pending senders waiting for routing decisions.
  - One-click screening actions on each sender card — screen a sender **in** or
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
| Key | Context | Action |
|---|---|---|
| <kbd>1</kbd> / <kbd>U</kbd> | Global | Switch to **New for you** (Unread tab) |
| <kbd>2</kbd> / <kbd>P</kbd> | Global | Switch to **Previously seen** (Read tab) |
| <kbd>3</kbd> / <kbd>S</kbd> | Global | Switch to **Screener** tab |
| <kbd>R</kbd> | Global | Refresh mail from daemon |
| <kbd>,</kbd> (comma) | Global | Toggle Settings view |
| <kbd>Escape</kbd> | Global | Close panel or exit settings |
| <kbd>j</kbd> / <kbd>k</kbd> / <kbd>↑</kbd> / <kbd>↓</kbd> | List | Navigate messages / screener cards |
| <kbd>Enter</kbd> / <kbd>Space</kbd> | List | Open selected email (in the mailbox desktop client) |
| <kbd>T</kbd> | List / Screener | **Move to Trash** |
| <kbd>A</kbd> | Mail List | **Set aside** (read later pile) |
| <kbd>M</kbd> | Mail List | **Toggle read / unread** |
| <kbd>I</kbd> | Screener | Route sender to **Inbox** |
| <kbd>B</kbd> | Screener | **Block** sender |

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
