# We talk to the servers ourselves

The Daemon speaks IMAP, SMTP, CalDAV and CardDAV directly. It does not route mail
through a hosted API, and it does not drive another mail program as a subprocess.
We write the sync engine (ADR-0006).

Three alternatives were looked at properly, and each is recorded here because each
will be suggested again:

**Nylas CLI** (Go, MIT) is a client for the *hosted* Nylas API: you connect your
mailbox to Nylas and it syncs on their servers behind an API key. Mail and
calendar would live at a third party on a commercial tier. Out on those grounds
alone.

**Himalaya** (Rust, MIT/Apache-2.0, well funded) is the closest thing to this CLI
that exists — and it is stateless with no event loop, no local cache and no
CalDAV/CardDAV. Its answer to per-command latency is `sirup`, which serves
pre-authenticated sessions over a unix socket: that is the warm-connection design
we are deliberately moving *away* from (ADR-0001). Being Rust, it could only be a
subprocess, which is the opposite of a Mirror. It remains worth reading for its
command surface.

**mbsync/isync** (C, 20 years old) and **neverest** (Rust) are batch syncers into
a Maildir-like store. Delegating to one costs the things already decided: no IDLE
and so no sub-second Screener (ADR-0006), full attachments on disk (ADR-0003),
write-through (ADR-0004), and — via notmuch's cgo-only Go bindings — the static
`CGO_ENABLED=0` build for the VPS. Note also that mbsync has no CONDSTORE, so its
reliability is twenty years of UID-diff: the fallback path in ADR-0006, not
something ahead of it.

## Consequences

What we take from mbsync instead of its code is its state model. It writes a
journal before it acts and replays on restart, so a sync killed halfway is
recoverable rather than ambiguous. The Mirror does the same thing inside SQLite:
a sync step records its intent, performs it, and commits — a crash leaves either
the old state or the new one, never a folder half-fetched with an advanced
modseq.
