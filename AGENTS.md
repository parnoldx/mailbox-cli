# Agents

## Before push

Run `make vet` before every push — CI runs the same target, and its gofmt
check is what bit the cc/bcc commit (2026-10-07).

## Test data

Anonymize URLs, domains and tokens in test fixtures: swap real sender domains
for `example.com`/`example.de` and replace real tokens, UUIDs and signatures
with random-looking placeholders of the same shape (length, encoding,
percent-escapes). The fixture must still exercise the same case — keep the
path, query keys and structure that the code under test keys on. If a subject
line carried a real brand, generalize it too, as long as the gate it tests
stays shut (or open) exactly as before.

Real-mail sightings may be named in comments without their URLs — the history
matters, the identifiers do not.

## GUI logs

GUI (Qt) output goes to journald, not to a file: when stderr is not a TTY,
Qt routes console output to the journal. Read it with
`journalctl --user _COMM=mailbox-gui` (or grep the journal for the app name) —
never go looking for `/tmp/mboxgui.log` or a similar file; it stays empty.
