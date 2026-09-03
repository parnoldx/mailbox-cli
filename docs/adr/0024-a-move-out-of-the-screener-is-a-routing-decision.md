# A move out of the Screener is a routing decision

**Supersedes ADR-0019 for the Screener.** ADR-0019's argument about `mailbox
route` — the record is a Sieve script, a decision is about a sender, and it
applies to the mail already here — still stands. What changes is that a message
dragged out of the Screener from *any* client is now read as that decision too.

## Why the drag is unambiguous here

ADR-0019 retired ADR-0002's VPS folder-watcher on the grounds that a decision
should "arrive as a command instead of as a guess about a drag". For a general
mailbox that reasoning holds: a message moved from the Inbox to `Archive/Immo`
could mean "file mail from this sender there from now on", or it could mean "I
have dealt with this one". The two readings both exist and the move does not
say which.

For the **Screener** the second reading does not exist:

- The Screener's only question is *do you want this sender's mail*.
- Reading a message never requires moving it — you read it where it sits.
- So a message leaving the Screener is not ambiguous between "file this sender"
  and "read this once". Moving it out **is** the answer to the Screener's
  question.

And `mailbox route` simply does not run from a phone. The folder move is the
only signal an iPhone or a webmail client can send.

## The rule

A message that **leaves the Screener**, observed by a Daemon in a sync cycle, is
a routing decision, and the destination folder names the destination:

| Moved from Screener to | The script gets           |
|------------------------|---------------------------|
| Inbox                  | route sender → Inbox      |
| Feed                   | route sender → Feed       |
| Paper Trail            | route sender → Paper Trail |
| `Screener/Block`       | block sender              |
| anywhere else          | nothing — a plain move    |

The sender's other mail still waiting in the Screener sweeps to the same
destination, exactly as `mailbox route` does. A message moved *into* the
Screener from one of those four boxes **un-decides** that sender — their next
mail is owed a decision again.

## Idempotency instead of "did I do this?"

The Daemon does not try to tell its own `route`-driven move from an external
one. When the sender already has the matching routing entry, writing it again is
a no-op (`routing.Lists.Set` reports no change). So `mailbox route` writes the
entry, the move syncs a cycle later, the inference re-derives the same entry,
and nothing happens. There is no echo loop, because editing the script moves no
mail — the only thing that could re-trigger the inference is another placement
crossing the Screener boundary.

The cross-folder move is reconstructed after the fact: the reconciler's
`Outcome` now carries `Added` and `Gone` — the placements a cycle wrote and
deleted, named by Message id. A `Gone` from the Screener matched to an `Added`
in a decision folder is the drag.

## The cost, and the mitigations

`mailbox route` has a human in the loop; inference does not. A phone swipe rule,
a server-side filter, or a fat-fingered drag into Feed rewrites the script with
no confirmation. Three mitigations, all in force:

- **Only the four folders above are read as decisions.** An accidental swipe
  almost always lands in Archive or Trash, which do nothing.
- **Every inferred decision is logged loudly** (`journalctl --user -u mailbox`)
  and appears in `mailbox status` as a recent-inferred list, so a wrong one is
  visible.
- **Always reversible by dragging back.** Block still parks waiting mail in
  `INBOX/Screener/Block`, not Trash.

## What this does not do

It does not switch somebody else's active script off to write an inferred
decision — the same reachability rule as ADR-0019. If the active script is not
ours and does not `include "logic"`, the inference is refused and logged, not
applied. A drag must not do what a command is refused.
