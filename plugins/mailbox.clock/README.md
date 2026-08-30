# mailbox.clock — the calendar widget, on the mailbox daemon

The clock in the bar and its calendar popup. It reads and writes the
calendar straight over the mailbox daemon's unix socket — the same command
surface the CLI speaks, minus one process per call. Ported from the
Thunderbird-backed version in the omamail repo; the QML behavior is
unchanged, only the plumbing under it moved.

## How it talks to the daemon

`MailboxService.qml` is the whole client: one JSON line per request over
`$XDG_RUNTIME_DIR/mailbox.sock` (the same socket `mailbox` itself uses, and
the one the systemd unit socket-activates the daemon on). A Mirror read
answers in microseconds and never waits on a network; a write waits for the
server, and the daemon answers only when it happened.

Three reads build the document the panel draws:

    ["calendar","list"]  ->  the roster the entry pane chooses from
    ["agenda"]           ->  what is on, expanded over the window
    ["todo","list"]      {all: true}

Model shapes the three answers into the one document the panel has always
parsed (`mailboxDocument`), so nothing downstream knows where the rows came
from. Writes are the same:

| the panel does          | the socket gets                      |
|-------------------------|--------------------------------------|
| create an event         | `["event","add"]`                    |
| add a todo              | `["todo","add"]`                     |
| tick a task             | `["todo","done"]` / `["todo","undone"]` |

Writes carry the daemon's arg names (`positional`, `start`, `calendar`,
`all_day`, …), which are the CLI's flags with their dashes off.

## Pushes, not polls

The daemon pushes `calendar.changed`, `event.changed` and `todo.changed`
whenever a collection moves — including when its own DAV poll picks up an
external change. Each push is one re-ask of the three reads; nothing in the
panel runs on a timer to keep the calendar fresh, and a write the panel
itself made is redrawn by the push it caused, with no resync timer.

A request that goes out while the socket is down fails on the spot rather
than queueing, and the service reconnects with a backoff when the daemon
comes back. `mailbox doctor` says when the daemon is not running at all.

## What is missing from the CLI

The widget's surface covers what the daemon serves. The gaps are all on the
write side, where the CLI has no flag for what the entry pane can say:

- **Todo due time and priority.** `todo add` takes a date only (`--due
  DATE`), so a typed hour and the high/medium/low pill are dropped.
- **Recurrence and alert minutes.** `event add` has no `--repeat` and no
  reminder flag; those parts of the entry pane are dropped on write.
- **A URL field of its own.** `event add` has no `--url`, so the Join
  link is folded into `--notes`. On the read side the agenda now carries
  the notes, which is where meeting links are usually written, so the
  Join button finds them again.

When the CLI grows those flags, the mapping lives in one place
(`Model.requestToArgs`) and the widget follows.

## Not carried over from the Thunderbird version

`sync-thunderbird-calendar`, `quick-add-thunderbird`,
`focus-thunderbird-calendar`, `new-thunderbird-event`, the
`thunderbird-newevent/` add-on, and `MailboxService.qml`'s state files —
there is no file the panel reads its calendar from anymore, and the
"Open Thunderbird calendar" hero button is gone with it.

`suggest-address` (OSM Photon typeahead for the location row) is unchanged.

## Installing

    cp -r plugins/mailbox.clock ~/.config/omarchy/plugins/

It replaces the previous clock widget of the bar.

## Tests

    tests/run

Pure Model logic — the daemon's answers shaped into the panel's document,
and wire requests turned into socket commands — runs under node against
canned answers. `panel-static.test.js` reads Panel.qml as text and checks
the wiring is there and the script era is gone.
