# Pushes carry no data

The Daemon's unix socket pushes change *notifications* — `{"event":"mail.changed",
"account":"primary","box":"inbox"}` — never the changed data. A widget that gets
one re-reads, which is a ~1ms local query under ADR-0001.

Pushing the data itself would give every fact two routes into a client, and the
two drift: a widget that renders a pushed payload and a widget that queries
disagree, and neither is obviously wrong. One route removes the question.

Every push goes to every connected client; there is no subscription. With a
handful of widgets the fan-out is free, and per-connection interest is state the
Daemon would have to track for no benefit yet.
