# A new repo rather than evolving omamail

`2026-08-28-omamail` (28k LOC) reads from IMAP inside each command. Making the
Mirror the read model (ADR-0001) inverts that data flow, so the work is a rewrite
of every data path rather than a refactor of one.

Leaf packages with no opinion about where data comes from — `htmlmd`, `format`,
`ids`, `imaputf7`, `config`, `vobject` — are copied across. The TUI (7.5k LOC)
and the Sieve routing service stay behind; omamail remains the reference for what
the command surface is.
