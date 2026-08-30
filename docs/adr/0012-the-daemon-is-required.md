# The Daemon is required

With no Daemon listening, `mailbox` fails with `daemon_required`. It does not open
the Mirror file itself, and it does not fall back to the network. `--local` does
not exist.

The tempting alternative was to let the CLI open the Mirror read-only — SQLite in
WAL mode supports it, and reads would survive a crashed Daemon. We chose the hard
error because one process owning the Mirror means one answer to every question
about locking, schema version and freshness, and because the failure it guards
against is better fixed than papered over: if the Daemon is not running, the
Mirror is going stale and a successful-looking read is the wrong thing to return.

Where there is no user session — the VPS — the answer is to run a Daemon there
too.

## Consequences

Under `systemd --user` socket activation this is mostly theoretical: connecting to
the socket starts the Daemon. The hard error is what you get when no socket unit
is installed at all, which is a setup problem and should read like one.

That was written before anything implemented it. `mailbox.socket` binds
`%t/mailbox.sock` and `mailbox.service` runs `mailbox daemon --systemd-socket`,
which takes the listening socket from `LISTEN_FDS` rather than binding a path of
its own. The flag is an **assertion**: with it, a Daemon that finds no inherited
socket fails instead of binding one, because a unit that silently binds a second
socket looks healthy and is talked to by nobody. Without it, the Daemon binds the
path itself and runs in the foreground, which is what `mailbox daemon` is for.

Both units are written by `mailbox setup`, which is also the only thing that
enables them. They are generated files: on a later run setup compares them
against what it would write now and replaces them if they differ, because the
unit and the binary have to agree about `--systemd-socket` and a unit this
program did not write cannot be assumed to. It does not restart a Daemon that is
serving to do it — the new unit takes effect the next time one starts.

What the Daemon reads out of `config.toml` is a separate matter, and it does not
stop at startup either (ADR-0021).
