---
name: mailbox
description: Read, search, answer and file mail, and manage calendars, todos, habits and contacts, through the mailbox CLI. Use when the user asks about their inbox, screener or a message, wants one read, answered, sent or filed, or asks what is on their agenda, task lists or address books.
argument-hint: "[what you want done]"
---

# mailbox

One agent-facing command over mail, calendars, todos and contacts. A daemon holds
every server connection and a local **Mirror** of what the servers hold. Reads are
answered from the Mirror and never wait on a network; writes wait for the server,
so an exit status of 0 means it happened and the next read sees it.

## Finding the command

The binary carries its own reference, and it is current when this file is not.

| Ask | Run |
|---|---|
| what commands exist | `mailbox help` |
| how one works, with flags and examples | `mailbox <command> --help` |
| the whole surface as JSON | `mailbox commands` |
| how mail and entries are named | `mailbox help ids` |
| what an exit status means | `mailbox help exit-codes` |

Read `mailbox <command> --help` before using a command you have not used this
session. The flags are shorter than other mail CLIs' (`--body`, not `-m`), the
subcommands are verbs this program chose (`route set`, `todo done`, `habit add`),
and each help text carries the reason the command works the way it does.

## The jobs

| To | Run |
|---|---|
| see what is waiting | `mailbox status` then `mailbox box list` |
| read a box | `mailbox box view Screener --limit 20` |
| read one message | `mailbox message view 36722` |
| read the conversation | `mailbox thread 36722` |
| find something | `mailbox search rechnung --in feed` |
| answer one | `mailbox reply 36722 --body "..."` |
| write a new one | `mailbox compose --to a@b.de --subject "..." --body "..."` |
| decide about a sender | `mailbox screener`, then `mailbox route set ID --to feed` |
| route a whole domain | `mailbox route set @stripe.com --to paper` |
| accept a meeting | `mailbox rsvp ID --accept` |
| keep one for later | `mailbox aside add 36722` |
| flag one you owe a reply | `mailbox reply-later add 36722` |
| file it | `mailbox move 36722 --to Archive/Immo` |
| flag it, bin it | `mailbox seen 36722`, `mailbox trash 36722`, `mailbox spam 36722` |
| get a file out | `mailbox attachment list 36722`, then `mailbox attachment save 36722:1` |
| what is on | `mailbox agenda --days 14` |
| tasks and practices | `mailbox todo list`, `mailbox habit list` |
| who is that | `mailbox contact search jane` |
| what went out | `mailbox outbox list` |
| something is broken | `mailbox doctor` |

`route set` owns the Sieve script that sorts mail on the server, so it is how a
sender is blocked or let through. A target `@example.com` is every address at
that domain; a specific address always wins. `mailbox sieve` is raw access for
the cases route does not cover. `mailbox rsvp ID --accept` answers a meeting
invite (iMIP to the organizer, and the event on the calendar). `attachment save`
writes into the working directory unless `--output` names somewhere else.

## Reading the answer

Every reply carries the state of the Mirror it came out of, and that state is
part of the answer:

- **Behind** — `mailbox` prints `notice: mirror is behind` and `--json` carries
  `"behind": true`. The Mirror still answers; report that it is Behind, and read
  an empty listing as "nothing has reached the Mirror", not as "you have no mail".
- **Exit 2 is ordinary.** Nothing has that id — usually a message expunged since
  the listing that named it. Say so and move on; a second attempt asks the same
  Mirror the same question.
- **Search reads the Mirror only.** Trash is never mirrored, and no query reaches
  the server. Nothing found means nothing mirrored.
- Copy ids out of a listing rather than building them. `mailbox help ids` is the
  shape.
- Parse `--json` (`{ok, data, mirror}`, empty lists as `[]`); the plain output is
  a table for a person to read.

## Care

Four writes reach further than the Mirror, so they get a decision from the user
rather than from you:

- **Sending.** Send exactly what the user asked, in the words they approved. When
  you wrote the text, add `--draft` and hand back the id — `mailbox draft send 12`
  is theirs to run.
- **Held mail.** A **Held** mail in the outbox was at the SMTP server when the
  daemon stopped and may already have been delivered. Report it and let the user
  pick `mailbox outbox retry` or `mailbox outbox cancel`.
- **Blocking.** `mailbox route set ID --to block` discards that sender's next
  mail; what is already waiting goes to `Screener/Block`, where a block made by
  mistake can still be found. `--to screener` is the undo.
- **Flags.** Reading a message leaves it unread, by design — the unread count
  belongs to whoever is looking at it in another client. Run `seen` and `unseen`
  when asked for them.
