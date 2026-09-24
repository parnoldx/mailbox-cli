# Adding the work account

Planned 2026-09-05, rewritten 2026-09-23 for Microsoft Graph. Not built yet. This
is a plan, not design: the design is [docs/DESIGN.md](DESIGN.md) and the
vocabulary is [CONTEXT.md](../CONTEXT.md). The three decisions it needs are
drafted at the end as ADR-0029 to ADR-0031 and move into DESIGN.md with the slice
that builds them.

## Context

There is one mail account today (mailbox.org, the Primary) and one hand-added
work calendar (`[caldav.work]`, SOGo at `sogo.ext.iils.de`). The work account —
IONOS today, moving to M365 — should join it with its **mail, calendar and
contacts**.

The plumbing for a second account already exists (ADR-0005): its own
connections, its own cycle loop, ids prefixed `work/INBOX:412`, `send --account`,
and a reply that goes out from the account that received it. What does **not**
exist is the part that makes two accounts feel like one mailbox rather than two
programs: the piles do not reach a Secondary, every listing is one account's,
and nothing anywhere says which account a row or a Send belongs to. That part is
Part A, and it does not care what server the work account is on.

Part B is the M365 backend. **M365 is spoken to over Microsoft Graph, for all
three domains** — not IMAP with XOAUTH2 plus something else for the rest:

- Exchange Online has **no CalDAV or CardDAV**, and EWS is retired for Exchange
  Online from October 2026. Calendar and contacts are Graph or nothing, so Graph
  and its token are needed anyway; mail over IMAP would be a second protocol and
  a second token audience for the same account.
- Exchange Online's IMAP (to be confirmed with a probe, but as far as is known)
  has **no CONDSTORE**, so ADR-0006's incremental cycle degrades to fetching
  every flag every cycle, and it may not keep `\*` in `PERMANENTFLAGS`, which
  ADR-0023's bubble keyword needs.
- SMTP AUTH has its Basic variant retired and is commonly **disabled per tenant**
  even with OAuth. Graph `sendMail` takes raw MIME, so composition and the
  Outbox do not change.

This amends ADR-0015 ("we talk to the servers ourselves"): Graph *is* the server
for M365, spoken to directly over HTTP, not a hosted third party holding the mail.

Decided:

- Work gets the **piles** (Aside, Reply Later, Bubble) but **no Screener,
  Feed, Paper Trail or Routing**. Graph has inbox rules (`messageRules`) that
  could carry a Routing later; not now.
- **Reading merges, writing does not.** `mailbox box view inbox` shows both
  accounts newest-first; `box view work` or `box view primary` narrows.
- **Every account has a colour**, and it is the account's identity everywhere:
  the row chip, and the Send button, so which address a mail leaves from is
  never a thing you have to remember.
- Work notifies exactly like personal. A new mail defaults to the Primary.
- No Microsoft SDK. `msgraph-sdk-go` is enormous for about eight endpoints;
  `net/http` and `encoding/json` do it. `golang.org/x/oauth2` (device flow,
  token refresh) is the one new dependency.

## Ask IT first — it blocks Part B

1. An **Entra app registration**: public client, device-code flow allowed,
   delegated scopes `Mail.ReadWrite Mail.Send Calendars.ReadWrite
   Contacts.ReadWrite offline_access User.Read`.
2. **Consent**: many tenants block user consent. If so, an admin grants it once.
3. Conditional access that would refuse a device-code sign-in from the VPS
   (ADR-0025's second Daemon).

The answer decides whether Part B can start. Part A does not wait for it.

---

## Part A — a second account, server-independent

**A1–A4 built 2026-09-23** on branch `accounts-part-a`; the decision is ADR-0028 in
DESIGN.md. Where the build differs from the text below:

- The colour is written by the wizard, not asked for — one line to change in the
  config. It is an Omarchy palette name or `#rrggbb`; anything else is refused.
- No `--account` flag and no account column in the CLI table: the id prefix
  already names the account (`box view work`, `box view primary`, `work/1`).
- The bubble guard reads the flags the STORE reports back instead of tracking
  PERMANENTFLAGS at mirror time — no new state, same refusal. Missing Aside is
  now refused before anything is written.
- The pile reclaim (a reply landing, or answering, pulls a thread out of Aside /
  Reply Later) now runs on every account too; without it the piles on a
  Secondary would hide live threads.
- `bubble list` spans accounts. `reply --if-no-reply` stays Primary-only.
- Pile Box names are the Primary's (`INBOX/Aside`). A server whose hierarchy
  delimiter is not `/` would get a different folder; check on the first real
  Secondary.

### Already there — no work needed

- `movePile` (`internal/daemon/routing.go`) is **already account-agnostic**: it
  resolves the account from the id, uses `acct.boxNamed`, threads within that
  account and writes with that account's Writer.
- `box list` and `search` (`internal/daemon/serve.go`) already span every
  account. `status` already reports per account.
- The quickshell widget already has a multi-account dropdown
  (`plugins/mailbox.email/Model.js`, `Panel.qml`). It lights up on its own once a
  second account exists.
- Push events already carry an account (`gui/src/MailboxClient.cpp`).

### A1. Account colour in the config

`internal/config/config.go`: add `Color string \`toml:"color"\`` to `Account`,
following `Calendar.Color`. `LoadFrom` fills an empty one from a small fixed
palette in sorted-name order, so a hand-edited config always has a colour; the
wizard writes an explicit one when it adds an account. The Primary's default is
the theme accent.

### A2. The piles on a Secondary

- `internal/setup/routingboot.go`: a `PileBoxes = {routing.BoxAside,
  routing.BoxReplyLater}` list beside `RoutingBoxes`, reusing the existing
  `MissingBoxes` + `CreateFolder` loop — `EnsureRouting` already does boxes-only
  when handed a nil `SieveOps`.
- `internal/setup/manage.go` (`addAccount`): after the probe, create the two
  missing Boxes and report them the way the Primary's bootstrap does. Also ask
  for the colour, defaulted.
- `internal/daemon/bubble.go`: drop the `!acct.Primary` guards (`:42`, `:370`)
  and make `bubbleLoop` (`:247`) run every account from `d.accounts()`.
  `setBubble`, `bringBack` and `returnDue` already take an `*Account`.
- **Bubble capability guard** for an IMAP Secondary: no `\*` in
  `PERMANENTFLAGS` means `bubble` on that account refuses with a clear message
  rather than set a timer that never fires. Check at mirror time, refuse in
  `handleBubble`. A Graph account always has it (ADR-0031).
- `internal/cli/doctor.go`: extend the Box check to a Secondary's two piles.

### A3. Merged listings — "a listing spans accounts, an id names one"

- `box view` (`serve.go:209`): with no account prefix, run over every account
  that has the named Box, call the existing `viewRows(acct, ...)` per account,
  merge newest-first on `Message.Date`, truncate to `limit`, then apply the
  bubbled-float sort once over the merged result. Ask each account for `limit`
  rows before merging.
- `row` (`serve.go:922`) gains `Account string \`json:"account,omitempty"\`` —
  empty for the Primary, matching the id rule exactly. The CLI table prints it
  only when more than one account is configured.
- `--account NAME` narrows. `box view work` already narrows via `splitAccount`.
- A Box only one account has (`Screener`, `Feed`) resolves to that account with
  no special case.
- Threads stay inside an account (ADR-0008), so a conversation reaching both
  addresses is two rows. Say so in `mailbox help box`.
- **Writes are untouched.** "One command, one account" stands: `seen 7 work/1`
  stays refused.
- Same rule for `agenda`, `todo list` and contact lookup: they already read every
  collection in the Mirror, so work's collections appear once B4 fills them.

### A4. Accounts on the socket

A new `account list` command returning name, email, primary and colour, so the
Qt app can paint a Send button without parsing `status`.

### A5. The Qt app

Layout chosen by prototype (2026-09-23): **one merged list, the account as a
coloured edge**. Rejected: two side-by-side lanes (splits the one Inbox the HEY
workflow is built on) and an account rail with a day panel (a second navigation
axis beside the command launcher). The prototype is on branch
`prototype/accounts-ui` (`make -C gui prototype`).

Everything below is drawn **only** when more than one account is configured; a
single-account setup looks exactly as it does today.

- `gui/src/MailModel.cpp` / `MailboxClient.cpp`: carry `account` per row; fetch
  the account list once at startup and hold the name→colour map.
- `gui/qml/MailRow.qml`: the existing left bar, which today shows only for
  fresh mail, becomes the account's colour on **every** row — solid when fresh,
  faded when seen, so it still carries the fresh signal. With the list showing
  both accounts, a Secondary's row also gets a small label chip with the account's
  name beside the time; the Primary's rows get none.
- `gui/qml/Main.qml`: filter pills beside the bucket title — All / Personal /
  Work, each outlined in its account's colour — on keys `0` / first letter
  of the account label. The bucket keys 1–7 are unchanged; check that
  the letter keys do not collide with existing shortcuts before binding them.
- `gui/qml/ComposerView.qml`: the From field is a pill in the sending account's
  colour showing label and address, clicked or `Tab`-cycled to switch; the Send
  button is filled in that colour and reads "Send as <label>". A new mail
  defaults to the Primary; a reply keeps the account it answers (already the rule
  in `daemon/send.go`).
- The merged agenda keeps its current layout; each event takes its account's
  colour the way calendars already carry `Calendar.Color`.

**A5 built 2026-09-23**, checked against a fake two-account daemon (screenshots,
not a real Secondary). Where it differs:

- The name→colour map lives in QML (`win.accounts` from `account list`), not in
  `MailboxClient`; `MailModel` only carries `account` per row.
- The filter narrows through the daemon (`work/INBOX`), and only on the
  buckets a Secondary has too: Inbox, Set Aside, Reply Later, Sent.
- The Primary is labelled "Personal"; a Secondary by its capitalised name. A
  label whose first letter is already a list or reader key gets no key.
- The From pill switches on click only, and only for a new mail; `Tab` stays
  focus traversal. A reply, forward or draft is fixed to its id's account.
- No agenda in the Qt app — the merged-agenda colour belongs to the calendar
  widget.

### A6. Docs

- `CONTEXT.md`: **Secondary Account** gains the piles and keeps its "no Screener,
  no Routing"; a new **Account Colour** term.
- A new decision in DESIGN.md, beside ADR-0005: a listing spans accounts and an
  id names one. Amend ADR-0023: the bubble return is per account.
- Name the commands it adds, so the registry and the skill stay the one place a
  command exists (ADR-0020).

---

## Part B — M365 over Graph

### B1. Sign-in and the token file (ADR-0030)

- Config: `[accounts.work]` gains `backend = "graph"`, `tenant` and
  `client_id`. No hosts, no password. `backend` empty means IMAP, so every
  existing config is unchanged.
- New `internal/graphdrv/auth.go`: device-code sign-in and refresh through
  `golang.org/x/oauth2`, a `TokenSource` that persists every refreshed token.
- Tokens live in **their own file**, `$XDG_STATE_HOME/mailbox/tokens.json`,
  mode 0600, opened 0600 rather than chmodded (ADR-0014) and written by
  temp-file-and-rename. Not in `config.toml`: the Daemon never writes the config
  (ADR-0021), and a refresh is a write.
- `mailbox setup` runs the device-code flow when adding a Graph account (print
  the code and URL, wait), then enumerates folders, calendars and address books
  from Graph and offers them as a choice — it still never asks for a URL.
- A refused refresh (revoked, expired after 90 idle days, password change)
  becomes a `status` problem, "work: sign in again — `mailbox setup`", like
  credentials a server refuses today.
- **Each Daemon signs in on its own** (the home one and the VPS one, ADR-0025),
  so two processes never race to rotate one refresh token.

### B2. Mail sync — `internal/sync/graphsync` (ADR-0029)

The cycle is shaped like the DAV cycle, not the IMAP one: a delta link is a sync
token.

- Per mirrored folder, `GET /me/mailFolders/{id}/messages/delta`, following
  `@odata.nextLink` to the `@odata.deltaLink`, which is stored. Changes and the
  new link commit in **one transaction**, so no `sync_journal` is needed.
  `@removed` entries drop the Placement. A `410 Gone` on a stored link means
  resync that folder from nothing, re-mapped by `message_key` (the UIDVALIDITY
  path in spirit).
- Every request sends `Prefer: IdType="ImmutableId"`, so a message keeps its id
  across moves inside the mailbox.
- **Ids stay `work/INBOX:412`.** A small map table `graph_ids(account, folder,
  uid, graph_id)` hands out the next local uid per folder the first time a
  message is seen there; `uidvalidity` is fixed. Nothing above the mirror learns
  that Graph ids are strings. A Mirror rebuild renumbers, which ADR-0013 already
  accepts for a UIDVALIDITY change.
- Flags: `isRead` ↔ `\Seen`, `flag.flagStatus` ↔ `\Flagged`, categories ↔
  keywords (bubble, `$bubbled`).
- Bodies: `GET /me/messages/{id}/$value` returns the MIME, which goes through the
  existing parse / `htmlmd` / parts path unchanged. Attachments stay unmirrored
  (ADR-0003); `attachment get` reads `/attachments/{id}/$value`.
- Folders: `/me/mailFolders` with `includeHiddenFolders` off, recursing
  `childFolders`; Sent, Archive and Deleted Items are recognised by their
  well-known names, not display names (which are localised).
- **No IDLE.** Graph change notifications need a public HTTPS webhook, which a
  laptop does not have. Watched folders are delta-polled every minute, the rest
  on the normal cycle. Pickups on the work account can therefore be up to a
  minute late — accepted. Mind throttling (`429` + `Retry-After`, honoured, not
  retried blind).
- Tests: a scripted fake `graphsync.Driver`, as `mailsync` has — a delta that
  pages, a `410`, an `@removed` for a message also added in the same answer, a
  move that arrives as remove-here/add-there.

### B3. Writes and Send

- `daemon/account.go`: `Account.Writer` becomes an interface — the four methods
  the daemon calls (`SetSeen`, `StoreFlags`, `Move`, `SetLabel`), plus
  `FetchPart` and `Mirrored`. `mailsync.Writer` and a new `graphsync.Writer`
  implement it. This is the one new abstraction, and it has two implementations.
- Graph writes follow ADR-0004: `PATCH` (read, flag, categories) and
  `POST /move` block, and the Mirror is updated from the response in the same
  transaction. `/move` returns the moved message, so the destination uid is known
  (the UIDPLUS case). A move is tried once (ADR-0017).
- Send: a Graph `outbox.Transport` posting the MIME to `/me/sendMail`
  (`Content-Type: text/plain`, base64 body). Graph files the Sent copy itself, so
  the Courier gets `SentBox: ""` and no `Filer`, which `courier.go` already treats
  as "do not file". Also: when `SentBox` is empty, `file` should `MarkFiled`
  rather than leave the item `sent`, or `UnfiledFor` returns it on every drain.
  The copy then arrives in the Mirror on the next Sent Items delta.

### B4. Calendar and contacts

- `internal/graphdrv` also implements `davsync.Driver`'s shape for Graph:
  `Collections` from `/me/calendars` and `/me/contactFolders` (plus
  `/me/contacts` for the default folder), `Sync` from the delta endpoints.
  `dav_collections.sync_token` holds the delta link; `url` holds the Graph id.
- Events: `/me/calendars/{id}/events/delta` returns series masters and
  exceptions, which fits "a repeating event is one row". If that endpoint
  does not do what this needs on the tenant, fall back to
  `calendarView/delta` over a rolling window. Decide with a live probe, not
  from the docs.
- **The record is Graph's JSON** (ADR-0029 amends ADR-0010): `dav_objects.raw`
  holds the object as Graph returned it, and the projection columns are filled
  from it. An edit is a `PATCH` of the changed fields, which cannot drop what we
  do not model — the reason ADR-0010 exists — so the rule's intent holds.
- Tasks: Graph To Do (`/me/todo/lists`) is a separate API. **Not in this
  plan**; work tasks stay where they are until there is a reason.
- Habits (ADR-0018) stay on the Primary. Nothing moves.
- Retire `[caldav.work]` (SOGo) once the M365 calendar is in, in the same change,
  so the work calendar is never shown twice.

### B5. Doctor, status, docs

- `mailbox doctor` for a Graph account: token present and refreshable, `/me`
  answers with the configured email, each mirrored folder and collection still
  exists, and the delta links are not being refused every cycle.
- DESIGN.md: ADR-0029 to ADR-0031 below, the `graph_ids` table in Schema
  (schema bump, rebuild per ADR-0013), a "Graph sync cycle" section beside the DAV
  one, and new entries under "What the real servers do" for everything the live
  tests find.
- CONTEXT.md: **Backend** (IMAP or Graph) as a property of an Account.

## Deliberately not built

- No Screener, Feed, Paper Trail or Routing on the work account.
- No cross-account Threads (ADR-0008 stands), and no cross-account batch writes.
- No Graph change notifications / webhook; polling only.
- No Graph To Do tasks.
- No IMAP+XOAUTH2 path. If IT refuses Graph, revisit this plan rather than
  building it.
- No Microsoft SDK.

## Verification

Part A:

- `go test ./...` stays green.
- Against the scripted second account in `internal/daemon/account_test.go`:
  1. `box view inbox` with two accounts returns both newest-first, work rows
     with `work/` ids and the Primary's with bare ones.
  2. `box view work` and `--account work` return only work.
  3. `aside work/INBOX:N` moves on the work server and nothing on the Primary.
  4. `bubble work/INBOX:N --tomorrow` sets the keyword on the work account and
     `returnDue` brings it back; a server without custom keywords is refused.
  5. A write naming two accounts is still refused.
- `go test ./internal/setup/` for the two pile Boxes and the colour.

Part B:

- The fake-driven tests listed in B2, plus: a token refresh that fails becomes a
  `status` problem; a sent Graph mail ends `filed`, with no `Append`.
- `-tags live` gates against the tenant, scratch folder `mailbox-selftest`:
  first sync + delta link, a delta after one flag change, a stored link refused
  (`410`) and recovered, a move that keeps the immutable id, a category
  round-trip (bubble), one mail to itself through `sendMail` appearing in Sent
  Items once, one event created, patched and read back with its other fields
  intact.
- Live, once configured: `mailbox setup` signs in and adds work, `mailbox doctor`
  is clean, `mailbox box view inbox` and `mailbox agenda` show both accounts, the
  VPS Daemon has signed in on its own, and in the Qt app the row chips and the
  Send button carry the right colours for a new mail and a reply.

---

## Draft decisions

**ADR-0029 — An M365 account is spoken to over Microsoft Graph.** Mail, calendar
and contacts, one protocol, one token. Exchange Online has no CalDAV or CardDAV and
EWS is retired, so Graph is required for two of the three already; its IMAP lacks
CONDSTORE (ADR-0006 would degrade to a full flag fetch per cycle) and SMTP AUTH is
disabled in many tenants. The Graph sync is shaped like the DAV cycle — a delta
link is a sync token, committed with the changes it describes, so no journal.
Messages keep `[account/]box:uid` ids through a local uid map, so nothing above the
Mirror knows there are two backends. The raw record of an event or contact is
Graph's JSON, and an edit is a `PATCH` of the changed fields: ADR-0010's concern,
losing what we do not model on a rewrite, cannot happen with a partial update, so
this amends its letter and keeps its reason. Graph is the server here, not a
hosted intermediary; ADR-0015's rejection of Nylas stands. Rejected: IMAP+XOAUTH2
for mail beside Graph for the rest (two protocols and two token audiences for one
account), and `msgraph-sdk-go` (a very large dependency for about eight endpoints).
No webhook push, because a laptop Daemon has no public endpoint; watched folders
are delta-polled every minute and a Pickup on the work account may be that late.

**ADR-0030 — OAuth tokens live in their own file, written by the Daemon.** A
refresh is a write, and the Daemon never writes `config.toml` (ADR-0021), so tokens
go in `tokens.json` beside the Mirror: mode 0600 from creation, temp-file-and-rename
on every refresh, never dropped on a Mirror rebuild. Same reasoning as ADR-0014 for
not using the Secret Service. Each Daemon signs in on its own through the device
flow, so the home and the VPS Daemon never share, and never race to rotate, one
refresh token (ADR-0025). A refused refresh is a `status` problem, not a crash.

**ADR-0031 — Bubble Up on Graph is a category.** `bubble-YYYYMMDDTHHMM` as an
Outlook category, and `bubbled` for the float, mapped to and from the
`$bubble-*` / `$bubbled` keywords at the driver so ADR-0023's logic does not know
the difference. A category moves with the message, syncs through delta and is a
server-side record, so two Daemons still coordinate without talking. It shows in
Outlook as a category, which is accepted and arguably useful.
