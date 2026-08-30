# Habits live in one object, and its record is JSON

A Habit is a repeating per-day practice: completing one day does not end it, it
has no time, and missing a day is information. iCalendar has no component for
that. A VTODO ends when it is completed; a VEVENT is something that happens at a
time; a recurring VTODO with a completion override per day is one object growing
a component every morning, and no other client would read it as a habit anyway.

So the Habits are not modelled in iCalendar at all. They live in **one** VEVENT,
on a calendar called `mailbox-habits` that this program creates, with the whole
record as JSON in its DESCRIPTION:

```json
{"habits":[{"id":"…","name":"Meditation","days":["mon","tue"],"done":["2026-08-29"]}]}
```

Three things follow, and all three are the point. Completing a day is **one**
read and **one** write of **one** object, so it cannot half-happen. The calendar
server only has to store a VEVENT, which every CalDAV server does — including
one that refuses VTODO. And the format is the one this account's habits are
already in, written by the program this one replaces, so the data moves across
untouched.

The cost is that no other client can show or tick off a habit. That is accepted:
nothing else was ever going to, and a habit that renders as a fake all-day event
in a phone calendar would be worse than one that does not appear at all.

The raw object is still the record and the Mirror's columns are still a
projection of it (ADR-0010). The projection has nothing to say about this one; it
is skipped in the agenda, because it is storage rather than something anybody
has on that day.

**It is dated today, and re-dated on every write.** The obvious anchor is a fixed
date in the past, which is what the program before this one used: `DTSTART` in
1990. This server does not allow it. Open-Xchange exposes only a *window* of each
calendar over CalDAV — roughly a year back — and an object outside that window is
stored, fetchable by its URL, and reported by no listing at all: not by
`sync-collection`, not by `PROPFIND Depth:1`. A 1990 record is written
successfully and is invisible from the moment it exists, which is exactly the
state this account's habits calendar was found in. Every change to the habits
rewrites the object anyway, so dating it the day of the last change keeps it
inside the window for as long as anybody is using it.

**And it is edited, not rebuilt.** The same server keeps its own `SEQUENCE` and
`LAST-MODIFIED` on what it stored, and a `PUT` of a freshly built object without
them is refused as an outdated update — `412`, with or without an `If-Match`,
which reads exactly like somebody else having changed it first. So a change to
the record reads the object, replaces its DESCRIPTION and its date, and writes
that back: the same rule the Todos and the Contacts already follow, for the same
reason.

Rejected: a separate SQLite table. Habits would then be the only thing here with
no server behind it, which makes the Mirror not disposable (ADR-0013) and the
data unreachable from any other machine.
