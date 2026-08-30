# mailbox-omarchy — a HEY-style desktop client that wears the Omarchy theme

A second, deliberately small prototype of the `mailbox` Qt client (the first one
lives in `../gui`). This one drops the three-pane layout for the HEY workflow —
one full-screen view at a time plus a numbered command launcher — and it retints
itself the instant you switch Omarchy themes.

## What it shows

- **Single view, content first.** No sidebar, no message pane. A full-screen
  bucket (Inbox split into *New for you* / *Previously seen*, plus The Feed,
  Paper Trail, The Screener, Set Aside), and a full-screen reading view with
  label-free headers and a big subject headline.
- **Command launcher.** `Ctrl+K` (or `Ctrl+P`) opens a centred switcher with the
  search field already focused, destinations numbered 1–5, and live per-bucket
  counts. Type to filter, digits or arrows to pick.
- **Live Omarchy theming.** `OmarchyTheme` (C++) reads the active palette from
  `~/.local/state/omarchy/current/theme/colors.toml` and re-emits `changed()` the
  moment it moves. Every colour in the UI is a binding with a `ColorAnimation`,
  so `omarchy theme set "Gruvbox"` morphs the whole window — dark or light — with
  no restart. Font is JetBrainsMono Nerd Font, radii/hairlines/pills match the
  rest of the desktop.
- **Real data.** Talks NDJSON to the daemon on `$XDG_RUNTIME_DIR/mailbox.sock`
  (`box list`, `box view`, `message view`, `attachment list`, `attachment save`).
  If the daemon is down it falls back to a small canned set; a green dot bottom-
  left means connected, amber means demo data.

## Daemon dependency

This build expects `message view` to return `body_html` (added to
`internal/daemon/serve.go`). Rebuild and restart the daemon after pulling:
`go build -o bin/mailbox ./cmd/mailbox && systemctl --user restart mailbox`.

## Why watching the theme is fiddly

`omarchy-theme-set` swaps the palette by `rm -rf`-ing
`~/.local/state/omarchy/current/theme/` and `mv`-ing a fresh copy in. That
destroys any inode-level watch on `colors.toml`, so `OmarchyTheme`:

1. watches the stable parent `~/.local/state/omarchy/current/` directory,
2. re-arms the file watch after every reload, and
3. keeps a 2 s mtime poll as a backstop.

## Build & run

```sh
cmake -B build -G Ninja
cmake --build build
./build/mailbox-omarchy            # normal
./build/mailbox-omarchy --open-first   # auto-open the first message (for demos/screenshots)
```

Requires Qt 6 (Core, Gui, Qml, Quick, QuickControls2, Network, WebEngineQuick).

## Keys

| key            | action                          |
|----------------|---------------------------------|
| `Ctrl+K` / `Ctrl+P` | open the command launcher  |
| `1`–`5`        | jump straight to a bucket       |
| `j` / `k` / arrows | move the row highlight       |
| `Return` / `o` / `l` | open the highlighted message |
| `Esc`          | close launcher, or leave a message |
| `Ctrl+Q`       | quit                            |

## Layout

```
src/OmarchyTheme.*   live palette + derived design tokens, file-watch + poll
src/MailboxClient.*  QLocalSocket NDJSON client, callback-per-request, demo fallback
src/MailModel.*      one box of message summaries; splits "Name <addr>" and dates
src/PixelBlock.*     QWebEngineUrlRequestInterceptor: drops trackers, counts them
qml/Main.qml         window, view state, all shortcuts
qml/BucketView.qml   full-screen bucket, New/Seen split, keyboard highlight
qml/ReadingView.qml  full-screen message, label-free headers
qml/CommandLauncher.qml  numbered quick switcher
qml/AttachmentChip.qml   open / save one attachment via the daemon
qml/MailRow.qml qml/Avatar.qml qml/Pill.qml qml/Kbd.qml qml/SectionLabel.qml
```
