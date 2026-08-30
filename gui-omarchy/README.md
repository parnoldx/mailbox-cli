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
- **Quick Look.** Clicking an attachment fetches it via `attachment save` to a
  cache dir and previews it in-app: PDFs page-rendered (`QtQuick.Pdf`), images
  and text inline, everything else with an "open externally" fallback. Esc / Q /
  click-away dismiss; the toolbar keeps "open externally" and "save as".
- **Dark mail.** On a dark Omarchy theme the reading view inlines a vendored
  [Dark Reader](https://github.com/darkreader/darkreader) engine
  (`qml/vendor/darkreader.js`) and calls `DarkReader.enable()` with the live
  palette, so a white newsletter lands on the app's own background with photos
  and logos intact. A header chip flips the current message back to its original
  colours; light themes never touch the mail.
- **Inline images.** `<img src="cid:…">` parts are pulled with `attachment
  bytes` and swapped for `data:` URIs before the page renders. When every
  attachment is one of these inline images, the cards collapse behind a small
  `N inline images` toggle instead of a wall of chips.
- **Real data.** NDJSON to the daemon on `$XDG_RUNTIME_DIR/mailbox.sock`
  (`box list`, `box view`, `message view`, `attachment list`, `attachment save`,
  `attachment bytes`). Offline it falls back to a small canned set; green dot =
  connected, amber = demo.

## Daemon dependency

This build expects `message view` to return `body_html`, `attachment list` to
carry a `content_id` per part, and an `attachment bytes ID[:INDEX]` verb that
returns one small inline part base64-wrapped (all in `internal/daemon/serve.go`).
The Mirror schema is bumped to 11 for the `content_id` column, so the first
restart **rebuilds the Mirror and resyncs** (ADR-0013). Rebuild and restart the
daemon after pulling:
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
./build/mailbox-omarchy --open <id>    # open straight into a message (demo/screenshot)
./build/mailbox-omarchy --open <id> --ql   # ...and pop Quick Look on its first attachment
```

Requires Qt 6 (Core, Gui, Qml, Quick, QuickControls2, Network, WebEngineQuick, PdfQuick, QuickDialogs2).

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
qml/AttachmentChip.qml   one attachment: click = Quick Look, floppy = Save as
qml/QuickLook.qml        in-app preview overlay (PDF pages, image, text)
qml/MailRow.qml qml/Avatar.qml qml/Pill.qml qml/Kbd.qml qml/SectionLabel.qml
```
