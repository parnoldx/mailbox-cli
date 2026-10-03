# Teams meetings and invites

Planned 2026-10-03. A plan, not design: decisions move into DESIGN.md with the
slice that builds them. `OnlineMeetings.ReadWrite` is granted in the app
registration (2026-10-03); the token on disk still needs a fresh sign-in.

Three things:

1. **`--teams`** on `event add` / `event edit`: the event is also a Teams meeting.
2. **`--invite` / `--uninvite`** on the same verbs: Exchange sends the
   invitations, updates and cancellations itself.
3. **`mailbox meet`**: a Teams link with no calendar entry.

Plus the clock widget (`plugins/mailbox.clock`), which writes events through
the same socket commands and so needs the same three fields.

**Microsoft 365 calendars only.** All Graph collections sit in the Mirror under
`primary` beside the mailbox.org ones and are told apart by URL host
(`graph.microsoft.com`). On a CalDAV calendar `--teams`, `--invite` and
`--uninvite` are refused up front and nothing is written. Invites on mailbox.org
would mean sending iTIP REQUESTs ourselves (or probing whether OX does RFC 6638
server-side scheduling). Leave that out until someone needs it.

## The contract (pinned before any worker starts)

Socket args for `event add` / `event edit`:

| Arg | Type | Meaning |
|---|---|---|
| `teams` | bool | make it a Teams meeting; there is no off |
| `invite` | []string | addresses to add as required attendees |
| `uninvite` | []string | addresses to take off (edit only) |

Answers:

- `calendar list` rows gain `"teams": true` on a Microsoft 365 events calendar.
  That flag is how the widget knows whether to show the Teams and invite rows.
  They also gain `"owner"`: the address the calendar belongs to (from
  `CalendarEmail`). The widget ranks invitee suggestions by its domain.
- `event view` gains `"attendees": [{"address", "name", "answer"}]` (answer is
  `accepted|tentative|declined|none`) and `"teams": true|false`.
- `event add` / `event edit` replies gain `"url"`. That's the join link when
  Graph made one.

Errors: `--teams`/`--invite`/`--uninvite` on a CalDAV calendar → usage error
"Teams meetings and invites need a Microsoft 365 calendar".
`--teams|--invite|--uninvite` with `--occurrence` → usage error. A malformed
address → usage error naming it.

## 1+2. Backend: `--teams`, `--invite`, `--uninvite`

An event is iCalendar from the CLI to Graph (CLI → daemon → `vcal` →
`davsync.Writer` → `graphdrv.DAV.putEvent` → `eventFields` → POST/PATCH).
Both wishes ride in the VEVENT and are read back the same way, so the diff in
`putEvent` stays quiet when nothing changed:

| Step | File | Change |
|---|---|---|
| Flags | `internal/cli/registry.go` | `teams` (bool), `invite` (list) on add and edit; `uninvite` (list) on edit; usage lines and examples |
| Args | `internal/cli/label.go` `eventVerb` | pass the three |
| Edit | `internal/vcal/write.go` | `EventEdit.Teams bool`, `Invite, Uninvite []string`. Teams → `X-MAILBOX-TEAMS:TRUE`. Invite → `ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:…` unless the address is already there; Uninvite deletes the matching ATTENDEE. Matching is by lower-cased address. `Empty()` counts all three |
| Read | `internal/vcal/vcal.go` | `Projection.Attendees []Attendee{Address, Name, Partstat}` and `Projection.Teams bool` |
| Gate | `internal/daemon/event.go` | read the args, refuse on non-Graph calendars and beside `--occurrence` (see contract) |
| Roster | `internal/daemon/calendar.go` | `teams` on the `calendar` row |
| View | `internal/daemon/event.go` | `viewEventObject` adds `url`; `event view` adds `attendees`, `teams` |
| Write | `internal/graphdrv/ical.go` `eventFields` | marker → `isOnlineMeeting: true`, `onlineMeetingProvider: "teamsForBusiness"`. ATTENDEEs other than the account's own address → `attendees: [{emailAddress:{address,name}, type:"required"}]`, sorted by address so the diff is order-blind. `eventFields` therefore takes `self` (the DAV already has `Email`) |
| Read | `internal/graphdrv/ical.go` `veventOf` | select `isOnlineMeeting,attendees,organizer`; write the marker when it's true; one ATTENDEE per Graph attendee with its `status.response` as PARTSTAT. The account's own ATTENDEE (the RSVP answer, line 151) stays as it is |

Things to watch:

- An event someone else organised reads back with its attendees. An unrelated
  edit must not PATCH them, because the sorted, status-free projection compares
  equal. That needs a test.
- There's no `--teams` off. Graph's event docs: "After you set isOnlineMeeting
  to true … Outlook ignores any further changes to isOnlineMeeting, and the
  meeting remains available online." Outlook's own off switch goes through its
  Teams add-in, not Graph. The only API route is delete and recreate, which
  sends invitees a cancellation and a new invite. Not offered. Probe once
  live: a PATCH `isOnlineMeeting: false` on an event with no invitees. If it
  really clears `onlineMeeting`, unlock the toggle.
- If `--teams` was asked for and no join URL came back, print a notice: the
  calendar's `allowedOnlineMeetingProviders` may not include Teams, and Graph
  accepts the POST without saying so.
- `printEventChange` prints the URL on a second line, then `invited: a, b` when
  there were invitees.

## 3. `mailbox meet`

```
mailbox meet [TITLE] [--account NAME] [--start WHEN] [--end WHEN]
```

It prints the join link and nothing else (so it pipes). Default title
"Meeting", start now, one hour.

- `internal/graphdrv/meet.go`: `POST /me/onlineMeetings`
  `{subject, startDateTime, endDateTime}` → `joinWebUrl`.
- `internal/graphdrv/auth.go`: add `OnlineMeetings.ReadWrite` to `Scopes`. A
  403 says "sign in again: `mailbox setup` → repair".
- `internal/daemon/meet.go` + a case in `serve.go`: the named account, or the
  only Graph account; refused on a non-Graph one.
- `internal/cli/registry.go`: the command, in the time section.

## 4. Clock widget (`plugins/mailbox.clock`)

It already shows a "Teams" join pill for Teams links (`linkProviderLabel`), so
reading needs nothing. Writing needs:

- **Roster** (`Model.mailboxRoster`): carry `teams` through.
- **Entry pane** (`Panel.qml`, `Model.js` draft → request → `requestToArgs`):
  - a **Teams** toggle, shown only when the picked calendar has `teams`. When
    editing an event that is already Teams, it shows on and is locked.
  - an **Invitees** row of pills. Suggestions come from `contact search`, then
    `correspondent search`, the same order and calls as
    `gui/qml/RecipientPills.qml` (ported, not imported: the plugin cannot reach
    the gui's QML). Addresses in the calendar `owner`'s domain (on the work
    calendar: colleagues at the company domain) sort first, then the rest in
    the order the searches returned them. The domain comes from the roster,
    not a constant. Only shown on a `teams` calendar.
  - on edit, the request carries the difference from what `event view`
    returned: new pills → `invite`, removed pills → `uninvite`.
- **Detail**: list attendees with their answer (✓ ? ✗ ·) under the event.
- `formatEntrySummary` mentions "Teams" and "n invited".
- Tests in `tests/model.test.js`: roster flag and owner, suggestions with the
  owner's domain first (fixtures on `example.com` / `example.de`), `requestToArgs` for add with
  teams+invite, edit diff → invite/uninvite, Teams toggle locked on edit.

Not doing: recognising "with anna@…" or "teams" in the quick-entry phrase. Add
it if typing it turns out to be the common path.

## Order (pi workers)

The worktree has an uncommitted Routing-rules change. It has to be committed
(or stashed) before this baseline, so each phase's diff reads on its own.

1. **Phase 1, one worker: backend** (sections 1+2). It pins the contract
   above in code. Done when fmt, vet and `go test ./...` pass and the build works.
   Commit.
2. **Phase 2, two workers in parallel, with disjoint files:**
   - **widget**: `plugins/mailbox.clock/**` only.
   - **meet**: `internal/graphdrv/meet.go`, `auth.go` Scopes,
     `internal/daemon/meet.go`, `serve.go` case, the registry entry, the skill
     and README lines.
   Commit after each one.
3. **Check (Claude):** read every diff. Sign in again, then live: Teams event
   with one invitee (the Primary's address), check the invite arrives with
   the link, accept it there, see the answer in `event view` and the widget,
   uninvite and check for the cancellation, delete. `mailbox meet | wl-copy`.
   `MAILBOX_GRAPH_ACCOUNT=work make live LIVE=./internal/graphdrv/`.
