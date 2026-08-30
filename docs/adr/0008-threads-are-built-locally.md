# Threads are built locally, across every Box

The Daemon threads Messages itself, from References and In-Reply-To, over the
whole Account at once.

Dovecot advertises `THREAD=REFERENCES` and we do not use it, which is the part
worth writing down: IMAP `THREAD` operates on the *selected mailbox*, so it can
never link an Inbox message to its reply filed in `Archive/Immo`. Since ADR-0003
puts every message's headers in the Mirror anyway, local threading is both
strictly more capable and microseconds of work at this scale.

A Thread does not cross Account boundaries. The same conversation reaching two
Accounts is two Threads — they have different Placements, different flags, and
replying picks a different sender.
