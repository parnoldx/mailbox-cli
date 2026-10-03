# Going live with the M365 Routing

The Routing (Screener, Feed, Paper Trail, Block) is built for the work account
as Graph inbox rules — ADR-0032 in [DESIGN.md](DESIGN.md). Everything is
against the real tenant tested: the live gate passed after the
`MailboxSettings.ReadWrite` scope was granted and the token re-signed
(2026-09-29). What is left is switching it on. Checklist, in order:

## 1. Set the account up with the Routing

```bash
mailbox setup        # → work account → repair
```

The repair creates the missing Routing Boxes (`INBOX/Screener`, `INBOX/Feed`,
`INBOX/Paper Trail`, `INBOX/Screener/Block`) and seeds the catch-all rule that
holds every undecided sender in the Screener. Piles already exist and are left
alone. The daemon must be restarted after this — it only sees Boxes that
existed when the account was built.

## 2. Restart the Daemons

```bash
make update-daemon   # home and VPS; the VPS one signs in on its own
```

## 3. Verify

```bash
mailbox screener --account work     # senders waiting for a decision
mailbox route list --account work   # the decisions made so far
```

Send nothing by hand: within a minute of a new work mail from an undecided
sender, `screener --account work` lists them. A decision:

```bash
mailbox route set work/Screener:12 --to feed
```

decides for the sender, writes the rule, and moves their waiting mail. From
then on their mail files server-side, before any client sees it.

## What changes on the account

- **All work mail from undecided senders lands in `INBOX/Screener`** and is not
  in the Inbox. That is the point; it also means the work Inbox goes quiet from
  step 1 on, and the Screener is the front page for that account.
- Rules named `mailbox: …` appear in the account's inbox rules. They are
  rewritten whole on every decision — do not edit or reorder them by hand; the
  next decision overwrites it.
- Blocked senders: mail is marked read and moved to Deleted Items, server-side.

## Backing out

Delete the `mailbox:` rules (Outlook on the web → Settings → Mail → Rules) and
the Screener folder; everything is back to a plain mailbox. Undecided senders
return to the Inbox. Decisions already made are not recoverable once the rules
are gone — `mailbox route list --account work` prints them, save that first.
