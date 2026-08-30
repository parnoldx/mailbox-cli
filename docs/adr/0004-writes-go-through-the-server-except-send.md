# Writes go through the server, except Send

A command that changes something blocks on the server round trip and updates the
Mirror from the ack, in the same transaction — UIDPLUS gives us the new UID on
MOVE and APPEND, so the Mirror never has to guess. The exit code therefore means
what it says, and the next read sees the result.

Rejected: local-first writes with a reconciling queue. Moves and flag changes are
one round trip on an already-warm connection; making the exit code a promise
rather than a fact buys nothing at that latency, and it imports conflict
resolution we would otherwise never need.

`Send` is the exception and gets a durable Outbox, because a half-finished SMTP
transaction is the one failure where losing what the user typed is unacceptable.
