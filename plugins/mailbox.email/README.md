# mailbox.email — Email Notifications for Omarchy

A Quickshell bar widget and dropdown panel for Omarchy, powered directly by the `mailbox` daemon over its unix socket.

It does one thing: tell you new inbox mail arrived. Screening, filing, buckets
and everything else are the desktop client's job (`mailbox-gui`).

## Features

- **Dynamic Bar Notification**: The email icon appears in the Omarchy top bar **only when there is unread inbox mail**. When everything is read, the icon collapses and hides completely.

  The Screener is not here at all. Screening is a decision owed whenever you
  next sit down, not an interruption — and because the screener empties one
  sender at a time it was never empty, so anything driven by it was on
  permanently and said nothing. What used to be genuinely urgent in there,
  login codes and registration links, the daemon now collects by itself before
  the widget would ever see it (Pickups — see the repo README).
- **Pure Vector GPU Icon**: Crisp vector envelope rendered with 4x MSAA antialiasing that adapts dynamically to your theme:
  - **Vibrant Blue (`Color.accent`)** when unread mail is waiting.
  - **Bar Foreground** when opened in rest state.
- **Audio & Visual Alerts**:
  - Plays the system new-email notification sound effect when a new message arrives while running (silent on initial startup/reloads).
  - Optional desktop toast notifications.
  - Both audio and visual notifications can be toggled in Settings.
- **Dropdown Panel** — one list, no tabs:
  - Everything **new** in one reverse-chron list: unread inbox mail. Read mail
    is never listed — that is the desktop client's job.
  - Account switcher with per-account unread count badges.
  - Colored sender avatars with initials deterministically derived from sender addresses.
  - 1-click action buttons on hover/selection: `󰄬` Mark read, `󰔛` Set aside, `󰆴` Move to Trash.
- **Opens in the desktop client**: Clicking a message (or <kbd>Enter</kbd>) hands
  it to the mailbox desktop client — `mailbox-gui --open <id>`. That is the
  only mail client we ship, so it is the only target; there is no setting.
- **Live Push Updates**: Connects directly to `$XDG_RUNTIME_DIR/mailbox.sock`. Updates in real time whenever the daemon pushes `mail.changed`.

---

## Keyboard Shortcuts

### Global Desktop Shortcut
- <kbd>Super</kbd> + <kbd>Alt</kbd> + <kbd>Shift</kbd> + <kbd>E</kbd>: Open / toggle the Mailbox panel from anywhere.

### In-Panel Navigation & Actions

| Key | Action |
|---|---|
| <kbd>R</kbd> | Refresh mail from daemon |
| <kbd>,</kbd> (comma) | Toggle Settings view |
| <kbd>Escape</kbd> | Close panel or exit settings |
| <kbd>j</kbd> / <kbd>k</kbd> / <kbd>↑</kbd> / <kbd>↓</kbd> | Navigate the list |
| <kbd>Enter</kbd> / <kbd>Space</kbd> | **Open** in the desktop client |
| <kbd>T</kbd> | **Move to Trash** |
| <kbd>A</kbd> | **Set aside** (read later pile) |
| <kbd>M</kbd> | **Toggle read / unread** |

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
