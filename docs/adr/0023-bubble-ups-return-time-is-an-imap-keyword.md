# Bubble Up's return time is an IMAP keyword

`mailbox bubble ID --tomorrow` sets a thread aside and has it come back to the
Inbox on its own at a time you pick — HEY's Bubble Up, matched closely. The
thread waits in `INBOX/Aside` and returns unread, so the phone raises a
notification the way it does for any new mail.

The question this record answers: **where is the return time kept, and what
brings the thread back?**

## The return time is a custom keyword on the message

The instant lives in an IMAP keyword, `$bubble-YYYYMMDDTHHMM`, in local
wall-clock time with no zone. The home machine and the always-on VPS are assumed
to be in the same timezone, and both compare it against `time.Now()` in
`time.Local`.

Three things it is not:

- **Not a Mirror column alone.** The Mirror is disposable (ADR-0013): a schema
  bump deletes and rebuilds it, and a column with nothing behind it would lose
  every pending return.
- **Not a sidecar file like the Outbox.** A durable file beside the Mirror
  drifts from the server the moment a message is moved or deleted in another
  client, and it needs per-Daemon bookkeeping that two Daemons would get wrong.
- **Not a wake timer.** A `time.AfterFunc` per bubbled thread is state in one
  process's memory. A Daemon that was down when the instant passed would never
  fire it.

A keyword is attached to the message. It moves with the message when it is
filed, it syncs like any other flag, and there is precedent: a label is an IMAP
keyword too (`mailsync.Writer.SetLabel`). It is the **shared record two Daemons
act on without coordinating** — this is ADR-0010's "raw is the record, the
columns beside it are a projection" applied once more.

It needs `\*` in the mailbox's `PERMANENTFLAGS`, which mailbox.org's Dovecot
has. The `-tags live` test writes `$bubble-*` to a scratch folder and reads it
back to confirm the server keeps a keyword it was never told about.

## `placements.bubble_at` is the projection

Schema version 12 adds `placements.bubble_at TEXT`, derived from the flags
string wherever a placement's flags are written — `Tx.SetFlags` and
`Tx.PutPlacement` in `internal/mirror/write.go`. It is a true projection: a
Mirror rebuilt mid-wait repopulates it from the keyword folder by folder, for
free. It exists only to make the "due now" scan and the soonest-first sort a
query rather than a table walk.

## The return is a wall-clock scan, and one code path

`bubbleLoop` ticks with the poll. Each tick reads the Aside placements whose
`bubble_at` is at or before now and, as one operation per thread: strips the
`$bubble-*` keyword, removes `\Seen`, moves the thread's Aside members to the
Inbox, and sets `$bubbled` so the Inbox listing floats it to the top. Because
the trigger is a clock reading and not a fired timer, a Daemon that was down
across the instant catches every overdue return on its first tick after
startup — never lost, only late, and the VPS Daemon makes it punctual.

`--now` runs the exact same steps immediately: one return path, manual or
scheduled. It also works on a thread that is not in Aside — an Inbox thread just
gets `\Unseen` + `$bubbled` and floats, with no round trip.

## Why `\Seen` is removed on return

The reminder *is* the mail reappearing unread. iOS raises a push only for an
unseen message landing in INBOX; a silently-moved read thread gives the user
nothing on the phone. This is the one place `bubble` touches read state, and it
is deliberate. HEY does the same.

## Re-timing, cancelling, and the early return

Re-timing removes the old `$bubble-*` and adds the new one in the same STORE —
one keyword at a time. A reply landing in a bubbled thread already pulls it back
through `reclaimPiled`; that path now also strips the keyword, so `bubbleLoop`
does not fire later on a thread that is already home.

## Two Daemons, one bubble

If the home Daemon and the VPS Daemon both see a return come due in the same
second, both take the Thread and both issue the MOVE. One wins; the other gets
"no such uid" and logs it. The thread lands in the Inbox once, because the
keyword — the only thing they share — is gone after the first move, and a
`bubble_at` scan on the next tick finds nothing.
