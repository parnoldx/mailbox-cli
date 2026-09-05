# Adding the work mail account

Planned 2026-09-05. Not built yet.

## Context

There is one mail account today (mailbox.org, the Primary) and one hand-added
work calendar (`[caldav.work]`, SOGo at `sogo.ext.iils.de`). The work *mail*
account — IONOS today, moving to M365 in the near future — should join it.

The plumbing for a second account already exists (slice 12, ADR-0005): its own
connections, its own cycle loop, ids prefixed `work/INBOX:412`, `send --account`,
and a reply that goes out from the account that received it. What does **not**
exist is the part that makes two accounts feel like one mailbox rather than two
programs: the piles do not reach a Secondary, every listing is one account's,
and nothing anywhere says which account a row or a Send belongs to.

Decided:

- Work gets the **piles** (Aside, Reply Later, Bubble) but **no Screener,
  Feed, Paper Trail or Routing**. The piles are IMAP moves plus one keyword and
  survive any server; Routing is a Sieve script, and M365 has no ManageSieve.
  Building work-side Routing now is work that gets thrown away in a few months.
- **Reading merges, writing does not.** `mailbox box view inbox` shows both
  accounts newest-first; `--account work` or `box view work` narrows.
- **Every account has a colour**, and it is the account's identity everywhere:
  the row chip, and the Send button, so which address a mail leaves from is
  never a thing you have to remember.
- Work notifies exactly like personal. A new mail defaults to the Primary.

## Flag before anything else: M365 will need OAuth

`internal/imapdrv/imapdrv.go:85` is a plain `LOGIN` with a password, and
`internal/smtpdrv` is the same. Exchange Online has Basic auth off for IMAP and
SMTP AUTH; app passwords go with it. When work moves to M365 this account will
almost certainly need XOAUTH2 — a token flow, a refresh, and a place to keep
both — which is its own slice and is **not** in this plan. Worth asking IT now
whether the tenant has an exception, because the answer decides whether the work
account keeps working on the switchover day or stops dead.

Nothing below depends on it: IONOS takes a password today, and the piles and the
merged view are server-independent.

## Already there — no work needed

- `movePile` (`internal/daemon/routing.go:518`) is **already account-agnostic**:
  it resolves the account from the id, uses `acct.boxNamed`, threads within that
  account and writes with that account's Writer. Aside and Reply Later work on a
  Secondary the moment the two Boxes exist on the server.
- `box list` (`internal/daemon/serve.go:145`) and `search`
  (`serve.go:312`) already span every account. `status` (`serve.go:403`) already
  reports per-account.
- The quickshell widget already has a multi-account dropdown:
  `plugins/mailbox.email/Model.js:141,189` and `Panel.qml:413`. It lights up on
  its own once a second account exists.
- Push events already carry an account (`gui/src/MailboxClient.cpp:94`).

## The slice

### 1. Account colour in the config

`internal/config/config.go`: add `Color string \`toml:"color"\`` to `Account`,
following `Calendar.Color` (`config.go:70`). `LoadFrom` fills an empty one from a
small fixed palette in sorted-name order, so a hand-edited config always has a
colour; the wizard writes an explicit one when it adds an account. The Primary's
default is the theme accent.

### 2. The piles on a Secondary

- `internal/setup/routingboot.go`: a `PileBoxes = {routing.BoxAside,
  routing.BoxReplyLater}` list beside `RoutingBoxes:19`, and reuse the existing
  `MissingBoxes` + `CreateFolder` loop (`routingboot.go:75-83`) — `EnsureRouting`
  already does boxes-only when handed a nil `SieveOps`.
- `internal/setup/manage.go:296` (`addAccount`): after the IMAP probe, create the
  two missing Boxes and report them the way the Primary's bootstrap does. The
  `Prober` (`setup.go:53`) needs a box-creating call for a non-primary host.
  Also ask for the colour, defaulted.
- `internal/daemon/bubble.go`: drop the `!acct.Primary` guards at `:42` and
  `:378`, and make `bubbleLoop` (`:247`) run every account from `d.accounts()`
  rather than the Primary alone. `setBubble`, `bringBack` and `returnDue` already
  take an `*Account`.
- **Keyword capability guard**: bubble is a `$bubble-*` IMAP keyword (ADR-0023).
  If the server will not store custom keywords (no `\*` in `PERMANENTFLAGS`),
  `bubble` on that account must refuse with a clear message rather than set a
  timer that silently never fires. Check at mirror time, refuse in `handleBubble`.
- `internal/cli/doctor.go:111`: extend the Box check to a Secondary's two piles.

### 3. Merged listings — "a listing spans accounts, an id names one"

- `internal/daemon/serve.go:174` (`box view`): with no account prefix, run over
  every account that has the named Box, call the existing `viewRows(acct, ...)`
  (`serve.go:1011`) per account, merge newest-first on the underlying
  `Message.Date`, truncate to `limit`, then apply the bubbled-float sort once
  over the merged result. Ask each account for `limit` rows before merging.
- `row` (`serve.go:884`) gains `Account string \`json:"account,omitempty"\`` —
  empty for the Primary, matching the id rule exactly. The CLI table prints it
  only when more than one account is configured.
- `--account NAME` narrows. `box view work` already narrows via `splitAccount`.
- A Box only one account has (`Screener`, `Feed`) resolves to that account with
  no special case.
- Threads stay inside an account (ADR-0008), so a conversation reaching both
  addresses is two rows. Say so in `mailbox help box`.
- **Writes are untouched.** "One command, one account" stands: `seen 7 work/1`
  stays refused.

### 4. Accounts on the socket

A new `account list` command returning name, email, primary and colour, so the
Qt app can paint a Send button without parsing `status`.

### 5. The Qt app

- `gui/src/MailModel.cpp` / `MailboxClient.cpp`: carry `account` per row; fetch
  the account list once at startup and hold the name→colour map.
- `gui/qml/MailRow.qml`: a colour chip (or coloured left edge) in the account's
  colour, drawn **only** when more than one account is configured — a
  single-account setup looks exactly as it does today.
- `gui/qml/Main.qml`: an account filter across the current bucket — All /
  Personal / Work — on its own key. The bucket keys 1–7 (`Main.qml:22-27,622`)
  are unchanged.
- `gui/qml/ComposerView.qml`: the Send button takes the sending account's
  colour, and the account is pickable. A new mail defaults to the Primary; a
  reply keeps the account it answers, which is already the daemon's rule
  (`internal/daemon/send.go`).

### 6. Docs

- `CONTEXT.md`: **Secondary Account** gains the piles and keeps its "no Screener,
  no Routing"; a new **Account Colour** term.
- `docs/adr/0027-a-listing-spans-accounts-an-id-names-one.md`.
- Amend ADR-0023: the bubble keyword is per account, not the Primary's.
- `docs/DESIGN.md`: the nineteenth slice, with its own "done when".

## Deliberately not built

- No Screener, Feed, Paper Trail or Routing on the work account. M365.
- No cross-account Threads (ADR-0008 stands).
- No cross-account batch writes.
- No XOAUTH2. It is the M365 slice, and it is separate.

## Verification

- `go test ./...` — the whole suite must stay green.
- New tests against the scripted second account already set up in
  `internal/daemon/account_test.go:17`:
  1. `box view inbox` with two accounts returns both, newest-first, the work rows
     carrying `work/` ids and the Primary's carrying bare ones.
  2. `box view work` and `--account work` return only work.
  3. `aside work/INBOX:N` moves on the work server and nothing on the Primary.
  4. `bubble work/INBOX:N --tomorrow` sets the keyword on the work account and
     `returnDue` brings it back; a server without custom keywords is refused.
  5. A write naming two accounts is still refused.
- `go test ./internal/setup/` for the two pile Boxes being created and the colour
  being written.
- Live, once configured: `mailbox setup` adds the work account, `mailbox doctor`
  is clean, `mailbox box view inbox` shows both, `mailbox aside work/INBOX:N`
  round-trips, and in the Qt app the row chips and the Send button carry the
  right colours for both a new mail and a reply.
