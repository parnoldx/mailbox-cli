package daemon

import (
	"context"
	"strings"
	"time"

	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// inferScreenerDecisions reads a drag out of (or into) the Screener as a
// routing decision.
//
// ADR-0019 retired ADR-0002's folder-watcher because a decision should arrive
// as a command rather than as a guess about a drag. For the Screener that
// reasoning does not hold: reading a message never requires moving it, so a
// message leaving the Screener is not ambiguous between "file this sender" and
// "read this once" — the second reading does not exist, and the move is the
// answer to the Screener's one question. And `mailbox route` does not work from
// a phone; the folder move is the only signal an iPhone can send. This
// supersedes ADR-0019 for the Screener alone (ADR-0023,
// docs/bubble-and-screener-handoff.md).
func (d *Daemon) inferScreenerDecisions(ctx context.Context, a *Account, outcomes map[string]mailsync.Outcome) {
	if d.Sieve == nil {
		return
	}
	screener, ok := a.boxNamed(routing.BoxScreener)
	if !ok {
		return
	}

	added := map[int64][]string{}
	gone := map[int64][]string{}
	for _, out := range outcomes {
		for _, p := range out.Added {
			added[p.MessageID] = append(added[p.MessageID], p.Folder)
		}
		for _, p := range out.Gone {
			gone[p.MessageID] = append(gone[p.MessageID], p.Folder)
		}
	}
	if len(added) == 0 && len(gone) == 0 {
		return
	}

	// address -> what this cycle's moves decided about it. The script can hold
	// one destination per sender, so a sender moved twice takes the last.
	want := map[string]routing.Destination{}

	// Forward: a message that left the Screener and landed in a decision folder.
	for msgID, from := range gone {
		if !containsFold(from, screener) {
			continue
		}
		for _, to := range added[msgID] {
			dest, isDecision := decisionFolder(to)
			if !isDecision {
				continue // Archive, Trash, anywhere else: a plain move, undecided
			}
			if addr := d.senderFor(a, msgID); addr != "" {
				want[addr] = dest
			}
		}
	}
	// Reverse: a message moved into the Screener from a decision folder
	// un-decides its sender — their next mail is owed a decision again.
	for msgID, to := range added {
		if !containsFold(to, screener) {
			continue
		}
		for _, from := range gone[msgID] {
			if _, isDecision := decisionFolder(from); !isDecision {
				continue
			}
			if addr := d.senderFor(a, msgID); addr != "" {
				want[addr] = routing.None
			}
		}
	}
	if len(want) == 0 {
		return
	}
	d.applyInferred(ctx, a, screener, want)
}

// decisionFolder maps a folder a message landed in to the Destination its
// arrival there decides. Only these four are read as decisions; a drag into
// Archive or Trash — where an accidental swipe almost always lands — rewrites
// nothing.
func decisionFolder(folder string) (routing.Destination, bool) {
	switch {
	case strings.EqualFold(folder, routing.BoxInbox):
		return routing.Inbox, true
	case strings.EqualFold(folder, routing.BoxFeed):
		return routing.Feed, true
	case strings.EqualFold(folder, routing.BoxPaperTrail):
		return routing.PaperTrail, true
	case strings.EqualFold(folder, routing.BoxBlock):
		return routing.Block, true
	}
	return "", false
}

func containsFold(hay []string, needle string) bool {
	for _, s := range hay {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

// senderFor is the address a Message is from, or "" when it cannot be carried
// in the script. The Message row outlives the Placement (ADR-0007), so this
// still answers after the move that triggered the inference.
func (d *Daemon) senderFor(a *Account, messageID int64) string {
	raw, err := d.Mirror.SenderOf(a.Name, messageID)
	if err != nil {
		return ""
	}
	addr := routing.AddressOf(raw)
	if addr == "" || !routing.Valid(addr) {
		return ""
	}
	return addr
}

// applyInferred writes the decisions into the script and sweeps each sender's
// waiting Screener mail to the destination — the same two halves `mailbox
// route` does, minus the human.
//
// It is idempotent instead of trying to tell its own `route`-driven move from
// an external one: a sender that already has the matching entry is a no-op, so
// a `mailbox route` decision whose move syncs a cycle later re-derives the same
// entry and nothing happens. No echo loop, because editing the script moves no
// mail.
func (d *Daemon) applyInferred(ctx context.Context, a *Account, screener string, want map[string]routing.Destination) {
	st, err := d.readRouting(ctx)
	if err != nil {
		d.logf("screener inference: cannot read routing: %v", err)
		return
	}
	if !st.inForce && !st.activate {
		// Same rule as `mailbox route`: never switch somebody else's active
		// script off to switch ours on. A drag must not do what a command is
		// refused.
		d.logf("screener inference: %q is active and does not include %q — an inferred decision is not written",
			st.active, routing.ScriptName)
		return
	}

	changed := false
	for addr, dest := range want {
		did, serr := st.lists.Set(addr, dest)
		if serr != nil {
			d.logf("screener inference: %s -> %s: %v", addr, dest, serr)
			continue
		}
		changed = changed || did
	}
	if changed {
		script := st.lists.Script()
		if err := d.Sieve.PutScript(ctx, routing.ScriptName, script, st.activate); err != nil {
			d.logf("screener inference: put script: %v", err)
			return
		}
		if err := d.storeRouting(script, true, st.lists); err != nil {
			d.logf("screener inference: store routing: %v", err)
			return
		}
	}

	for addr, dest := range want {
		var refs []mailsync.Ref
		pile := ""
		if dest != routing.None {
			var rerr error
			if refs, rerr = d.screenerRefs(a, screener, addr); rerr != nil {
				d.logf("screener inference: waiting mail for %s: %v", addr, rerr)
			}
			pile = pileFor(dest, len(refs))
		}
		moved := 0
		if pile != "" {
			if box, ok := a.boxNamed(pile); !ok {
				d.logf("screener inference: %s decided for %s but that box is not here", addr, pile)
			} else if results, mErr := a.Writer.Move(ctx, refs, box); mErr != nil {
				d.logf("screener inference: sweeping %s to %s: %v", addr, pile, mErr)
			} else {
				moved = len(results)
				d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
			}
		}
		d.recordInferred(addr, string(dest), moved)
		// Loudly: an inference has no human in the loop, so a wrong one has to
		// be legible in `journalctl --user -u mailbox`.
		d.logf("screener decision inferred from a move: %s -> %s (%d waiting mail swept)", addr, dest, moved)
	}

	d.push(Push{Event: "mail.changed", Account: a.Name, Box: screener})
}

// recordInferred keeps the last few inferred decisions for `status`, so a
// mistaken drag is visible without reading the journal.
func (d *Daemon) recordInferred(address, to string, moved int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inferred = append(d.inferred, InferredDecision{
		At: time.Now().Format(time.RFC3339), Address: address, To: to, Moved: moved,
	})
	if len(d.inferred) > 10 {
		d.inferred = d.inferred[len(d.inferred)-10:]
	}
}

// RecentInferred is the inferred-decision list `status` carries.
func (d *Daemon) RecentInferred() []InferredDecision {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]InferredDecision(nil), d.inferred...)
}
