# The config is the record, and the Daemon reconciles it

`~/.config/mailbox/config.toml` says which Accounts exist, which Collections are
mirrored, and what the credentials are. The Daemon does not read it once at
startup and hold the result: it brings itself into line with the file while it
runs. `mailbox setup` writes that file and nothing else.

This is ADR-0010's rule and ADR-0019's rule applied one level up. The Sieve
script is the record and the `routing` table is a projection of it; the vCard is
the record and the columns beside it are a projection. The config is the record
of what is configured, and the connections, the cycle loops and the
`dav_collections` rows are a projection of *it*.

Two things fall out that are worth the ADR on their own.

**A config edited by hand behaves exactly like one the wizard wrote.** There is
one path into a running system, not two, so the file can be edited by a person,
by an agent, or by `mailbox setup` with the same result and no ceremony. The
alternative — setup as the only supported writer, restarting the Daemon when it
finishes — makes a hand edit a second-class thing that works by accident until
it doesn't.

**Setup stops writing the Mirror.** The first version dialled IMAP itself and
filled the Mirror in its own process, which is the one thing the shape of this
program forbids (ADR-0012). It was survivable only while setup ran before any
Daemon existed; the moment setup enables the socket unit, its own `doctor` call
starts a second writer on the same file. With the config as the record, setup
writes TOML, nudges the Daemon and watches its first cycle over the socket as
any other client would.

## How it learns, and what it can apply

The Daemon compares the file's mtime and size at the top of each cycle — once a
minute, on a timer that already runs — and re-reads when either moved. Setup
sends a `reload` over the socket when it finishes writing, so the wizard does not
sit for up to a minute looking broken.

There is no file watch. TOML is written by temp-file-and-rename, so an inotify
watch has to be on the directory rather than the file, and it sees two events per
save with a window in between where the file does not exist. A minute's poll and
an explicit nudge cover both cases with neither of those problems. There is no
SIGHUP either: under socket activation the pid is not where it was left.

Applied while running: the set of Secondary Accounts and their credentials, the
Collection exclude list, and the defaults a command falls back on (`task_list`,
`address_book`). Each of those is either a set of connections that stands on its
own — a Secondary Account has its own reconciler and its own cycle loop — or a
value read under a lock.

Not applied while running: anything in the Primary Account's block, and the
hand-configured `[caldav.*]` calendars. The Primary's connections are its
identity in practice: its address, its servers, its credentials, the Boxes it
holds an IDLE connection on. The calendars are part of the driver set a running
DAV cycle is reading, so swapping them under it is a race. Both make the Daemon
log what changed and **exit 0**.

Under socket activation that is not an outage: the next connection starts it
again on the new config. A live re-identification of the Account every id is
resolved against is a great deal of machinery for something a clean exit does
correctly, and the exit is the same code path a restart already had to work.

**The Daemon never writes the config.** It is a record kept by a person and a
wizard, and a process that edits it while a human has it open in an editor is how
a password gets lost. State the Daemon needs to keep goes in the Mirror.

## A bad config does not stop it

A file that does not parse, or that fails validation, leaves the Daemon running
on the last config that worked. The failure is reported — in `status`, in
`doctor`, in the journal, and as a `problem.changed` push — and mail keeps
arriving.

A daemon that exits because a TOML key was misspelt turns a typo into missed
mail, and every other decision here goes the other way: a Behind Mirror still
answers rather than failing (ADR-0001), one unreachable Box does not stop the
other sixty-seven. The exception is startup with no last-good config to fall
back to, which is the same setup-shaped error as no Daemon at all (ADR-0012).

## It may decline, and it says so

Removing an Account whose Outbox still holds mail is refused: a queue is not
something to drop quietly, and a **Held** message may already have been
delivered. So the file can ask for something the Daemon will not do.

`mailbox setup` avoids that by asking first — it is a socket client, and the
Daemon is the only thing that knows what is in flight, so the file never claims a
removal that did not happen. A hand edit has nobody to ask, and there the rule is:
reconcile what can be reconciled, keep what must be kept, and report the rest as
a problem in `status` and in the journal. A declarative file plus a component
that may decline needs somewhere for the declining to be visible, or the two
disagree in silence.
