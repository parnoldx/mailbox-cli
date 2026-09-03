# Two Daemons for one account, coordinating only through server records

An extension of ADR-0012 ("the Daemon is required"): where there is no user
session — the VPS — a Daemon runs there too. ADR-0012 already sanctions it. This
records what it means to have **two** Daemons on one account at once.

## What the VPS Daemon is

The **same `mailbox` binary**, run as a second `mailbox daemon` on an always-on
VPS. Not a new component, not the revived "routing service" ADR-0019 was right
to drop. It has its own Mirror file, its own Outbox file, its own connections
and its own socket — which on a separate machine it gets for free.

## Why both new slices need it

- **`bubble`** (ADR-0023): the timed return must fire when the home machine is
  off. Home-only, a thread returns on the next home-Daemon wake — never lost,
  just late. The VPS Daemon makes it punctual.
- **Screener decisions** (ADR-0024): the whole point is acting from the phone
  while away. Only an always-on Daemon can observe the move and rewrite the
  script.

## The coordination is server-side records, and nothing else

- Two Daemons, one account, one Mirror file **each**. Both IDLE the same
  folders, both run the full sync cycle. Servers handle multiple clients.
- They coordinate through **server-side records only**: the `$bubble-*` keyword
  for returns, the Sieve script for routing, `\Seen` and folder placement for
  everything else. Whichever Daemon acts first wins; the other syncs the result
  and finds nothing to do.
- **No remote socket.** Scheduling (`mailbox bubble …`) happens only on the home
  machine. The VPS Daemon takes no commands in practice — it runs `bubbleLoop`,
  the Screener-move inference, and the ordinary cycle.
- A race on `bubbleLoop` or `reclaimPiled` — both Daemons moving the same thread
  in the same second — resolves by one MOVE winning and the other getting "no
  such uid", which is logged and is not a problem.

## Why nothing needs a lock

The audit that this ADR records: the Daemon holds no single-instance
assumptions.

- `sync_journal` is keyed by `(account, folder)` in each Daemon's **own** Mirror
  file. Two Mirrors, two journals, no contention.
- Freshness (`lastSync`, `connected`) is per-process memory. Nothing persists
  "last synced by me".
- Every write is already idempotent or server-arbitrated: `routing.Lists.Set`
  is a no-op when the entry is already there (ADR-0024); a DAV write carries
  `If-Match` (ninth slice); a MOVE is tried once and never replayed (ADR-0017);
  `reclaimPiled` and `bubbleLoop` tolerate the uid being gone.
- The reachability rule (ADR-0019) stops either Daemon from deactivating a
  script it did not write.

## Still deployment, not code

The VPS install is `mailbox daemon` with `MAILBOX_MIRROR`, `MAILBOX_OUTBOX` and
`MAILBOX_CONFIG` pointed at that box's own files, and a credentials file at mode
0600 (ADR-0014 — no keyring on a headless box). `mailbox setup` reads piped
answers, and the config is hand-writable TOML, so no `--headless` mode is
needed. There is no user session, so the service is a system unit written by
hand rather than a `--user` one; `mailbox daemon` already binds its own socket
path when it is not handed one under socket activation (ADR-0012).
