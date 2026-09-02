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

## The CLI covers the whole entry pane

Every field the entry pane collects has a daemon arg to carry it, so nothing
is dropped on write:

- **Todo due time and priority.** `todo add` takes `due` as a date *or* a
  date and time, and a `priority` of high/medium/low.
- **Recurrence and alert minutes.** `event add` / `event edit` take `repeat`
  (a rule, or a keyword like `weekly` / `weekdays`) and `alarm` (minutes
  before the start, one value or several).
- **A URL of its own.** `event add` takes `url`, so the Join link is its own
  field rather than folded into the notes.

The request-to-arg mapping lives in one place (`Model.requestToArgs`); when a
verb grows a field, that is the only file that changes.

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
