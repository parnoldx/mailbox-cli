# The Routing is one Sieve script, and this program owns it

> Superseded in part by ADR-0024: a move out of the Screener from any client is
> now read as a routing decision. Everything below about the script being the
> record, `address :is`, reachability, and not creating Boxes still holds.

Mail is sorted before we see it. A Sieve script named `logic` on the Primary
Account's server discards blocked senders, files the Inbox, the Paper Trail and
the Feed, and drops everything else into the Screener. That is what makes the
Screener a pile of undecided *senders* rather than a second Inbox.

ADR-0002 left the Sieve routing service behind — a separate process on a VPS
that watched IMAP folders for arrivals and inferred a decision from where a
message had been dragged. The Daemon already holds every connection to this
account (ADR-0012), so it holds this one too, and the decision arrives as a
command instead of as a guess about a drag.

**The script is the record.** There is no local list of senders that the server
is brought into line with: there is the script, and what the Mirror holds beside
it is a projection of what it says, rebuilt whenever it is read or written. That
is ADR-0010's rule applied to the second format a server owns rather than we do.
Reading the Routing is therefore a Mirror read like every other one and answers
with the network down (ADR-0001); changing it goes to the server and updates the
Mirror from what the server took (ADR-0004).

## A decision is about a sender, and it applies to the mail already here

`mailbox route bob@example.com --to feed` does two things: it rewrites the
script, so their next mail is filed, and it moves the mail already waiting in the
Screener. One command, because a caller who has to run two to finish one decision
will one day run only the first, and a Screener that keeps mail from a sender
already decided about is a Screener nobody trusts.

The order is the server, then the Mirror, then the mail. A script the server
refused leaves everything exactly as it was. A move that fails after the script
was stored leaves the decision made and the old mail where it is — which the
same command, run again, finishes, because both halves are idempotent.

## `address :is`, not `header :contains`

The script this replaces matched with `header :contains "From"`, a substring test
over the raw header. A sender writes their own display name, so
`From: "anna@example.com" <attacker@example.net>` was filed as Anna. It also
meant `bob@example.com` matched `notbob@example.com`.

The generated script uses `address :is :all "from"`, which tests the parsed
address — the one part of the header a sender cannot dress up. The parser still
reads the old spelling, because the script already on the account is written in
it, and a Routing that cannot be read is a Routing that cannot be changed.

An address that could not be put in a Sieve quoted string safely is refused
rather than escaped. The address comes from a From header, which is attacker
input, and escaping is a thing to get subtly wrong: no real address contains a
quote, a backslash or a newline.

## What this does not do

**It does not activate anything that would switch something else off.** Only
`logic` is read and only `logic` is written. Activating a script deactivates the
one that was running, so ours is activated in exactly one case: when the server
is running no script at all.

The rule is therefore *reachability*, not activity. The Routing runs when `logic`
is the active script **or** when the active script includes it, and the second is
the ordinary case rather than the exception. On this account the active script is
`Open-Xchange`, written by mailbox.org's webmail filter editor, holding four
hand-made rules and ending with `include "logic";`. An "is ours the active one?"
test refuses every decision on that account and would have deactivated those four
rules to enable ours. A decision is refused only when the active script neither
is ours nor includes it — because then the Routing would be stored and never
run — and the refusal says to add the `include` line.

**It does not create Boxes.** A `fileinto` into a Box that is not there files
nowhere: the mail lands in the Inbox looking as though the decision was never
made. So a destination the account does not have is refused before anything is
written, with the Box named.

**It does not route to Aside.** Aside is the read-later pile, and "always read
this sender later, from now on" is a Feed. Mail is put there one at a time with
`mailbox aside`, and there is no rule in the script that ever fills it.
