# 20. The CLI is a command registry, and a bare noun is help

Date: 2026-08-30

## Status

Accepted.

## Context

The command surface was a `switch` in `cli.Run`, a hand-written `usage` string
listing every command with its full flag signature, and a scatter of `usage:`
constants — one per group, several per file. Three consequences:

The overview was 30 lines of flag signatures rather than an index. The old CLI
this one replaces printed a name and a six-word gloss under `MAIL`,
`CALENDARS`, `TODOS`, `CONTACTS`, `META`, which fits a screen and reads.

Group help did not exist. `mailbox todo --help` fell through to Go's `flag`
package, which printed `Usage of todo list:` and named flags with one dash
(`-json`) — a spelling that appears nowhere else in the tool.

A bare noun meant different things in different places. `mailbox todo` listed
todos, `mailbox contact QUERY` searched, `mailbox box` was a usage error. An
agent had to learn which nouns were special one failure at a time.

There was also no `commands`, no `version`, no help topics, and `setup` was
dispatched in `main.go` before `cli.Run` ever saw it — so it appeared in no
help text at all, `--force` included.

## Decision

**One registry.** `internal/cli/registry.go` holds the whole command tree:
name, section, gloss, long description, usage lines, flags, examples. The
dispatcher walks it, and the overview, group pages, leaf pages, `--help` and
`mailbox commands` are all rendered from it. A command's `flag.FlagSet` is
*built* from its declared flags, so help text and parser cannot disagree — a
flag that is not in the registry is not a flag.

**A bare noun prints help, everywhere.** `mailbox todo` prints the todo index;
`todo list` lists. No noun defaults to an action, including the ones that used
to. An agent that types a noun learns the noun, in one round-trip, rather than
learning one verb and none of the others.

**Bare `mailbox` answers on stdout and exits 0.** It is a question, not a
mistake; exiting 1 makes wrappers and `set -e` scripts read it as a failure. A
*wrong* subcommand still exits 1 — but prints the group index alongside the
error, because the caller cannot scroll back.

**`--json` is global.** Declared once, lifted off the command line before any
command sees it, shown once in the overview, and mentioned in one line per leaf
page rather than repeated as a block.

**Four sections**: `MAIL`, `ORGANIZE`, `CALENDAR & TASKS & CONTACTS`, `SYSTEM`.
`ORGANIZE` splits from `MAIL` because everything in it decides
*where mail goes* rather than acting on one message — the distinction the domain
model is built around. Contacts share a section with the calendars and the task
lists rather than holding one alone: an address book is a collection on the same
server, reached the same way, and a heading with one row under it is a heading
that earns nothing. `SYSTEM`, not `META`: "meta" describes the implementer's
view, `SYSTEM` the caller's. The help topics are not a fifth section: bare
`mailbox` is a list of commands, and a heading of things that are not commands
does not belong in it. `mailbox help` adds a `HELP TOPICS` section, and the
root footer names them on one line so they stay findable.

**Ordering is workflow, not alphabetical**, in the overview and inside every
group. The read comes first because reading is how a caller gets the id the
other verbs need.

**`setup` and `daemon` join the registry** with an internal `Local` field
saying they run in this process rather than dialling the socket. They live in
package `main`, so `RunWith` takes them as `Locals`. They are now in the
overview, in `commands`, and have real `--help`.

**The tagline stops naming the mirror.** It is `mail, calendars, todos and
contacts`. How a command is answered is a question somebody may go on to ask,
and `mailbox help architecture` is where it is answered.

## Consequences

Invocations that changed:

| was | is |
| --- | --- |
| `mailbox todo` (listed) | `mailbox todo list` |
| `mailbox habit` (listed) | `mailbox habit list` |
| `mailbox outbox` (listed) | `mailbox outbox list` |
| `mailbox contact QUERY` | `mailbox contact search QUERY` |
| `mailbox aside ID...` | `mailbox aside add ID...` |
| `mailbox route` (printed the routing) | `mailbox route list` |
| `mailbox route TARGET --to BOX` | `mailbox route set TARGET --to BOX` |
| `mailbox send` | `mailbox compose` |
| bare `mailbox` on stderr, exit 1 | stdout, exit 0 |

New: `mailbox commands`, `mailbox version`, `mailbox help ids|exit-codes|environment|architecture`,
and `setup` in the overview.

Two tests hold the surface. Golden files for the two overviews — with and
without the topics — so a change to the document every caller reads first shows
up in a diff (`go test ./internal/cli -update` rewrites them). And invariants over the registry: every command has a
gloss of six words or fewer, a usage line and a section; no group runs or
declares flags; no leaf is missing a `Run`; no usage line names a flag the
command does not declare; no help topic shares a name with a command; and the
`Local` commands are exactly the four that answer with no daemon listening.

`aside` has only `add` and `done` — there is no `aside list`, because the daemon
has no such call. The read is `mailbox box view INBOX/Aside`, and the group page
says so.

Writing the help surfaced one thing worth recording: **the box aliases are
gone**. The old CLI took `inbox, feed, trail, screener, aside, archive, drafts,
sent`. `resolveBox` in the daemon aliases `inbox` and nothing else, so every
other box has to be named by its folder — `INBOX/Screener`, `"INBOX/Paper
Trail"`. The help says so rather than promising aliases that do not resolve.
Restoring them is a daemon change, and belongs with the missing verbs below.

## What this deliberately did not do

Three follow-ups, cut from this change on purpose rather than forgotten:

1. **The missing verbs**, since taken up in their own passes and now all
   built: `box list`, `spam`, `sieve list|get|put|activate`, `habit edit`,
   `event add|edit|delete`, `contact update`, `forward`, `label
   list|create|view|add|remove`, `draft list|show|edit|send|delete`, `doctor`.

   `send` is named **`compose`**, as the old CLI named it, and `compose
   --draft` files the mail in drafts instead of sending it. That closes the one
   gap the old CLI had here — it could list, read, change, send and delete a
   draft but never make one, so the group only worked on drafts written in
   webmail. A draft is an append and not a send, so `--draft` goes to `draft
   save` on the wire rather than through the outbox. `reply --draft` and
   `forward --draft` would be the same few lines and are not there yet.

   `box list` shows the eight boxes mail moves through, in that order — inbox,
   feed, paper trail, screener, aside, sent, drafts, junk — and nothing else.
   An account here is sixty-seven boxes, of which fifty-seven are the archive
   tree; `--archive` is all of them, with those eight still first.
   `Screener/Block` is out of the default too: it is where a blocked sender's
   waiting mail went so a mistake can still be found, which is worth having and
   not worth a line in every listing.

   Three are not coming, and not by omission. **`tui`** is an interactive
   terminal UI; the first line of CONTEXT.md is that this is an agent-facing
   command and not a human mail client, so it is a separate product rather than
   a missing command. **`routing --web`** was an unauthenticated HTTP
   list-management UI in front of a VPS polling service; ADR-0019 moved the
   script into the daemon and `route list` / `route set` is what replaced it.
   **`contact refresh`** re-read CardDAV by hand, which the daemon now does on
   its own; the honest equivalent would be a general "sync now", and that is a
   new command rather than this one.

   Two things the old CLI had are also gone at the daemon level rather than the
   CLI's: the **box aliases** (above), and there is no `--unread` on `box view`.

2. **The output envelope.** `--jq`, `--quiet`, `--styled`, `--ids-only`,
   `--count`, and `{ok, data}` on a pipe. `--json` being global is the
   groundwork; the rest changes every command's output and belongs on its own.
   There is no `output` help topic until it lands, because a topic that says
   "there is a `--json` flag" teaches an agent the tool has no output control.

3. **`SKILL.md`** — done in the fourteenth slice (docs/DESIGN.md). The old skill
   documented a CLI that no longer exists (`--jq`, `--quiet`, `box list`,
   `Drafts:12`) as two real copies free to drift; `skill/SKILL.md` is now one
   embedded file, installed by `make skill`, gated by
   `TestSkillNamesOnlyRealCommands`, and it carries conduct rather than the
   command surface — including that `mailbox daemon` and `mailbox setup` do not
   return.
