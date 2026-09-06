# A watch is a subscription, a push is a nudge

`mailbox watch` gets a second kind of unsolicited line: a Change, which says
what moved — the Box, the Thread, the subject, whether it is new mail — and not
only that something did. It goes to a connection that asked for it with `watch`
and to no other.

ADR-0011 stands where it was written. A widget is pushed `mail.changed` and
re-reads, and that is still the only way data reaches a widget: one route in,
no two renderings to disagree. A watch is the other shape of client — a script,
a pipe, a `notify-send` one-liner — and it has no query to make. Telling it to
re-read is telling it to reimplement the diff the Daemon has just done.

So the two live side by side and the subscription is what keeps them apart. A
Push still goes to everybody, costs nothing, and carries nothing. A Change costs
a Mirror read per Message, so it is built only when somebody is listening, and
the filters (`--box`, `--events`) are applied in the Daemon rather than in the
client: an idle watch should be an idle socket.

What is *new mail* is decided here rather than by the reader. Unseen, in a Box
where unread means something, and an arrival rather than a move landing from
another client — a move being an expunge in one Box and an append in another
within one cycle, which is the same pair ADR-0024 reads a routing decision out
of. A move across two cycles will be called new; the alternative is a table of
recently-seen Message ids, and being wrong about the rare case costs one
notification.

A Box that resynced is one line and not a thousand. A UIDVALIDITY change
replaces every Placement in it, and calling that a thousand arrivals is a lie a
script would act on. The line says re-read the Box, which is ADR-0011's answer
after all.

Nothing is remembered. There is no `--since` and no journal of changes: a watch
reports what happens while it is connected, and a Daemon that restarts drops
what it never saw. A durable feed means a table, a retention rule and a cursor,
and the Mirror is disposable (ADR-0013) — the thing that survives a restart on
this machine is the server, which is what a fresh watch reads from anyway.
