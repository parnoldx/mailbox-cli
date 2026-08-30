# A sent mail carries both plain text and HTML

`compose`, `reply` and `draft` treat `--body` as Markdown and send a
`multipart/alternative`: the `text/plain` part is the body verbatim, the
`text/html` part is that body rendered. `--body-html` supplies the HTML part
directly, for a caller — a GUI composer — that already has it; then `--body`, if
given, is the plain-text twin and is not read from stdin.

Rendering is `internal/htmlmd`, the same converter the reading path already uses
in the other direction (`serve.go`, the mail-sync reconciler). A mail written
here and read back here round-trips through one implementation, not two.

## Why both parts, always

A modern recipient's client wants HTML; a `text/plain`-only mail from a person
reads as terse or broken. But the body an agent pipes in is plain text, and the
plain text is the record — it is what the `Sent` copy must still say, and what a
`text/plain` reader must get unchanged. So neither part can be dropped: the
`text/html` part is what most people see, the `text/plain` part is the source of
truth, and `multipart/alternative` is exactly the container for "the same thing
in two forms, least-preferred first."

Making it unconditional is the point. A rule of "HTML only when the body looks
like Markdown" is a heuristic that surprises: a stray `*` promotes a mail to
multipart, a deliberate heading does not because it was on the first line. Every
send taking the same shape is one shape to test, one shape to mirror, one shape
to reason about. `Build()` still writes a bare `text/plain` when `BodyHTML` is
empty — that path is untouched — but the Daemon fills `BodyHTML` for every
non-empty body, so in practice every send from this tool is an alternative.

## Markdown, not a new body flag

`--body` stays the name. The body is now interpreted as Markdown, and plain
prose is valid Markdown that renders to itself, so a caller that never thought
about Markdown gets an HTML paragraph and nothing it did not intend. Adding a
second `-m/--message` flag beside `--body` would be two spellings for one thing;
adding a `--markdown` bool would make the common case opt in. Neither earns its
place when "the body is Markdown" is just true.

The escape hatch goes the other way. `--body-html` is for HTML a caller holds
already; it is sent verbatim, and its `text/plain` twin is either `--body` or,
failing that, `htmlmd.HTMLToMarkdown` of the HTML. There is no round of
"render, then re-parse" — the two parts come from the two inputs.

## Forward stays plain

`forward` quotes the whole original under a `---------- Forwarded message
----------` header block. That block is plain text with structure a Markdown
renderer would mangle — the rule line becomes a heading, the `>` quotes shift a
level. A forward is a faithful copy of what was said, so it is `text/plain` only;
the Daemon clears `BodyHTML` on that path after assembling the quoted body.
