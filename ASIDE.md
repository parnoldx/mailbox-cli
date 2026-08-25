# Aside — read-later marker

Set aside a message for later without moving it or touching Routing. IMAP-only,
no new folder. Not implemented yet; this is the design to implement against.

## Marker

IMAP keyword `SetAside` (a custom flag, same mechanism Thunderbird tags use).

    STORE +FLAGS (\SetAside)   /   -FLAGS to clear

- Requires the server's PERMANENTFLAGS to end in `\*` (arbitrary keywords
  allowed). mailbox.org is Dovecot-based and allows them.
- Probe once after login: parse PERMANENTFLAGS from SELECT. If no `\*`,
  fall back to `\Flagged`.
- Other clients ignore unknown keywords → invisible on webmail/mobile.
  This is a private, agent-facing marker by design.
- Keywords survive MOVE/COPY on Dovecot; Sieve delivery happens before
  tagging, so no interaction with Routing.

## Verbs (sketch)

    mailbox aside ID...        set \SetAside
    mailbox unaside ID...      clear it
    box view / search --aside  filter: SEARCH KEYWORD SetAside

Listing = `UID SEARCH KEYWORD SetAside` per box (indexed by Dovecot, cheap;
no STATUS counter exists for keywords).

## Language

**Aside**: the `\SetAside` keyword marking a Message as read-later. A message
stays in its Box; Aside changes nothing else.
_Avoid_: star, pin, snooze, set-aside (as verb)
