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
- **The Feed reads like a feed.** The Feed does not hand off to the reader — it
  is one chronological column of cards, each with the sender, the subject and the
  first few lines of the body. `Return` (or a click) expands a card to its whole
  text in place; `Return` again, `Show less` or `Esc` collapses it. A rule across
  the column marks where you got to last time: everything above it arrived since,
  everything below has been seen. How far you scroll, and anything you expand, is
  remembered between runs in `$XDG_CONFIG_HOME/Mailbox/state.json` (`feed.mark`,
  keyed by message date); `Mark all read` in the header jumps the rule to the top.
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
- **Compose.** The `Compose` button (top-right of a bucket), `c` or `Ctrl+N`
  opens a full-screen composer; `Reply` / `Reply all` in a message's header open
  it prefilled with the sender, an `Re:` subject and the thread id. Recipients
  are tokenised pills with address-book autocomplete (`contact search`) and a
  `+ Cc / Bcc` reveal at the right of the `To` row. The body is **Basecamp's
  [Lexxy](https://github.com/basecamp/lexxy)** editor — the real thing, its
  self-contained bundle vendored in `qml/vendor/lexxy.{js,css}` and inlined into
  a WebEngine document by `LexxyEditor.qml`, retinted to the live Omarchy
  palette; it produces HTML, sent as `body_html` (the daemon keeps a plain-text
  twin, ADR-0022). Files attach via the paperclip in the action bar or by
  drag-and-drop onto the editor. **Send is deferred five seconds** — the
  composer closes at once and a banner counts down with an `Undo` that reopens
  it with everything intact. If the body says "attached" and nothing is, the
  banner says so. Drives `send` / `reply` / `draft save` on the daemon.
- **Real data.** NDJSON to the daemon on `$XDG_RUNTIME_DIR/mailbox.sock`
  (`box list`, `box view`, `message view`, `attachment list`, `attachment save`,
  `attachment bytes`, `contact search`, `send`, `reply`, `draft save`). Offline
  it falls back to a small canned set; green dot = connected, amber = demo.

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
./build/mailbox-omarchy --compose     # open straight into the composer (demo/screenshot)
```

Requires Qt 6 (Core, Gui, Qml, Quick, QuickControls2, Network, WebEngineQuick, PdfQuick, QuickDialogs2).

## Keys

| key            | action                          |
|----------------|---------------------------------|
| `Ctrl+K` / `Ctrl+P` | open the command launcher  |
| `c` / `Ctrl+N` | compose a new message           |
| `1`–`5`        | jump straight to a bucket       |
| `j` / `k` / arrows | move the row highlight       |
| `Return`       | open the message (in The Feed: expand / collapse the card) |
| `o` / `l`      | open the highlighted message (works from The Feed too) |
| `Ctrl+Return`  | send (in the composer)          |
| `Esc`          | close launcher, leave a message, collapse Feed cards, or close the composer |
| `Ctrl+Q`       | quit                            |

`--bucket <name>` (e.g. `--bucket feed`) starts on that bucket instead of Inbox.

## Layout

```
src/OmarchyTheme.*   live palette + derived design tokens, file-watch + poll
src/MailboxClient.*  QLocalSocket NDJSON client, callback-per-request, demo fallback;
                     tiny stateGet/stateSet JSON store for the Feed watermark
src/MailModel.*      one box of message summaries; splits "Name <addr>" and dates
src/PixelBlock.*     QWebEngineUrlRequestInterceptor: drops trackers, counts them
qml/Main.qml         window, view state, all shortcuts
qml/BucketView.qml   full-screen bucket, New/Seen split, keyboard highlight
qml/FeedView.qml     The Feed as a scrolling column; the read watermark + prefetch
qml/FeedCard.qml     one feed item: preview, expand-in-place, "Open full page"
qml/FeedDivider.qml  the "you got to here last time" rule
qml/ReadingView.qml  full-screen message, label-free headers; Reply / Reply all
qml/CommandLauncher.qml  numbered quick switcher
qml/ComposerView.qml     full-screen compose (new + reply), attachment tray, action bar
qml/LexxyEditor.qml      Lexxy (basecamp/lexxy) in a WebEngineView; getHtml/setHtml bridge, palette retint
qml/RecipientPills.qml   tokenised recipient field with contact-search autocomplete (overlay Popup)
qml/AppButton.qml        primary / ghost / danger button
qml/SendUndoToast.qml    five-second delayed-send banner with Undo + forgotten-attachment warning
qml/vendor/lexxy.{js,css}  Lexxy build (rollup, self-contained: Lexical + deps), MIT — see LICENSE.lexxy
qml/AttachmentChip.qml   one attachment: click = Quick Look, floppy = Save as
qml/QuickLook.qml        in-app preview overlay (PDF pages, image, text)
qml/MailRow.qml qml/Avatar.qml qml/Pill.qml qml/Kbd.qml qml/SectionLabel.qml
```

## Regenerating the vendored Lexxy bundle

`qml/vendor/lexxy.js` is the self-contained build (the `rollup.config.mjs`
target, which inlines Lexical and the rest — only `@rails/activestorage` stays
external, and it is only touched if you paste an image *into* the editor).
`qml/vendor/lexxy.css` is `app/assets/stylesheets/lexxy-{variables,editor,content}.css`
concatenated. To refresh against a new Lexxy:

```sh
git clone --depth 1 https://github.com/basecamp/lexxy /tmp/lexxy
cd /tmp/lexxy && npm install && npx rollup -c rollup.config.mjs
cp app/assets/javascript/lexxy.min.js  <repo>/gui-omarchy/qml/vendor/lexxy.js
cat app/assets/stylesheets/lexxy-variables.css \
    app/assets/stylesheets/lexxy-editor.css \
    app/assets/stylesheets/lexxy-content.css > <repo>/gui-omarchy/qml/vendor/lexxy.css
```
