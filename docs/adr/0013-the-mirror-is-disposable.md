# The Mirror is disposable; the Outbox is not

The Mirror carries a `schema_version`. On a mismatch the Daemon deletes the file
and resyncs from scratch — roughly 18 MB and a couple of minutes (ADR-0003).
There are no migrations, and there never will be.

Every byte in the Mirror is derived from a server that still holds the original,
so a migration would be work spent preserving a copy of something we can simply
fetch again. Not writing migrations is the largest single saving in this design:
no migration ever has to be correct, and the schema stays free to change while the
shape of the thing is still being learnt.

The **Outbox** is the exception, because it is the one place the Mirror leads the
server rather than following it (ADR-0004). It lives in its own SQLite file which
is never dropped, and which does get migrations if it ever needs them.
