# A write is tried once

Reads on the work connection reconnect and retry on any failure, because a
dropped connection is normal operation and a repeated read costs nothing
(ADR-0016). Writes do not get that retry.

A MOVE whose ack was lost has still happened on the server. Retrying it after a
reconnect asks the same folder to move the same uid — which, on a folder that
has since had a message expunged and another delivered, is a different message.
Flag changes are idempotent and keep the retry; MOVE does not.

A write that fails is reported as a failure. That is honest rather than
convenient: the caller re-reads and sees the truth, and the next cycle
reconciles whatever the server actually did — the same path that covers a
message moved by another client.

Rejected: making every write idempotent by round-tripping the Message-ID before
and after. It is two extra fetches on every move to protect against a failure
mode the next cycle already repairs.
