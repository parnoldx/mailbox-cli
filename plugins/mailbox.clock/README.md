# mailbox.clock: the calendar widget, on the mailbox daemon

The clock in the bar and its calendar popup. It reads and writes the
calendar straight over the mailbox daemon's unix socket, the same command
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
| search colleagues       | `["people","search"]`                |

Writes carry the daemon's arg names (`positional`, `start`, `calendar`,
`all_day`, …), which are the CLI's flags with their dashes off.

## Pushes, not polls

The daemon pushes `calendar.changed`, `event.changed` and `todo.changed`
whenever a collection moves, including when its own DAV poll picks up an
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
- **Teams and invitees.** On a Microsoft 365 calendar (the roster's `teams`
  flag) the pills gain "Teams meeting" and "Invite"; on any other calendar
  they are not there. Invite opens a row of pills,
  with suggestions from `contact search` then `correspondent search` and
  addresses on the calendar owner's domain ranked first. `event add` /
  `event edit` take `teams`, `invite` and `uninvite`; an edit sends only the
  difference. Teams has no off, an event that is one stays one, so the
  switch locks and says so.
- **Shift-click the meet button.** A plain click on the camera mints a link
  and copies it (`["meet"]`), inviting nobody. Shift-clicking asks who the
  meeting is with instead: the picker searches `people search`, the people
  in the Microsoft 365 groups the sign-in belongs to. That is the staff at
  the account's own domain, not every account in the tenant: rooms,
  printers, bug-tracker addresses and other service mailboxes are not group
  members and stay out. Picking a person writes an event with `teams` and
  `invite` on the work calendar, the next quarter hour and half an hour
  long, and opens the join link the write answers with. Exchange sends the
  invitation; the event is the price of that, because a lone online meeting
  cannot invite anyone. The directory needs delegated `User.ReadBasic.All`
  on the Microsoft 365 sign-in, which the daemon's `Scopes` requests and a
  fresh `mailbox setup` mints.

The request-to-arg mapping lives in one place (`Model.requestToArgs`); when a
verb grows a field, that is the only file that changes.

## What the phrase reads

The entry field's parser is a scanner over a folded copy of the line (lower
case, umlauts flattened, same length): every phrase a pattern accepts is
blanked out of both copies, the blanks are what the field paints in its role
colours, and whatever no read took is the title, in the spelling it was typed.
The reading order is the grammar, which is why `15.3.` is a date before `15.3`
could be a time. English and German are both understood at once.

| reads                        | examples                                              |
|------------------------------|-------------------------------------------------------|
| flags                        | `/Work`, `-a15m`, `-r1w`, `-120`, `!`/`!!`/`!!!`, a pasted link |
| calendar by name             | `in Work`, `im Privat` (only names the mirror has)     |
| day                          | `tomorrow`, `morgen`, `next friday`, `15.3.`, `2026-03-15`, `March 15`, `15. März`, `day after tomorrow`, `übermorgen`, `next week`, `am 15.` |
| span                         | `till 25.8.`, `friday to sunday`, `this weekend`       |
| repeat                       | `every monday`, `jeden tag`, `freitags`, `weekly`, `monatlich` |
| start from now               | `in 20 min`, `in 3 days`, `now`                        |
| time                         | `9:30`, `9am`, `9 Uhr`, `at 9`, `9am to 5pm`, `12 bis 13 Uhr`, `21-2`, `half past three`, `halb vier` |
| day part, when no clock      | `friday morning`, `tomorrow evening`, `tonight`         |
| length                       | `for 90m`, `für 2 h`, `for half an hour`, `2h`          |
| place                        | `at Garbe Biegarten`, `beim Italiener`, a capitalized tail (`12:30 Café Central`) |

The rules that keep it honest: a bare number is no time (`3` stays in the
title, `at 3` does not), `15.3.` and `12.30` read as dates, a short weekday
(`fr`, `so`) only counts with a day word before it or a number after it, and
nothing ever dead-ends: text the parser cannot improve on becomes the title.

## Not carried over from the Thunderbird version

`sync-thunderbird-calendar`, `quick-add-thunderbird`,
`focus-thunderbird-calendar`, `new-thunderbird-event`, the
`thunderbird-newevent/` add-on, and `MailboxService.qml`'s state files,
there is no file the panel reads its calendar from anymore, and the
"Open Thunderbird calendar" hero button is gone with it.

`suggest-address` (OSM Photon typeahead for the location row) is unchanged.

## Installing

    cp -r plugins/mailbox.clock ~/.config/omarchy/plugins/

It replaces the previous clock widget of the bar.

## Tests

    tests/run

Pure Model logic, the daemon's answers shaped into the panel's document,
and wire requests turned into socket commands, runs under node against
canned answers. `panel-static.test.js` reads Panel.qml as text and checks
the wiring is there and the script era is gone.
