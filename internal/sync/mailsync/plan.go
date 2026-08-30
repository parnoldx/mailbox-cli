package mailsync

import "mailbox/internal/mirror"

// Action is what a cycle decided to do to one folder.
type Action int

const (
	// ActionNone: the folder is up to date.
	ActionNone Action = iota
	// ActionResync: every uid we hold is meaningless. Placements go, Messages
	// stay (ADR-0006).
	ActionResync
	// ActionIncremental: fetch what changed since our modseq.
	ActionIncremental
)

func (a Action) String() string {
	switch a {
	case ActionResync:
		return "resync"
	case ActionIncremental:
		return "incremental"
	default:
		return "none"
	}
}

// Plan is a decision about one folder, taken from two cheap numbers.
type Plan struct {
	Action Action
	// FlagsSince asks for flag changes after this modseq. Zero means don't ask.
	FlagsSince uint64
	// NewFrom is the first uid we have never seen. Zero means don't ask.
	NewFrom uint32
	// ExpungeDiff means the server's message count disagrees with ours, so
	// something left without us being told. Without QRESYNC this is the only
	// way to find out (ADR-0006).
	ExpungeDiff bool
}

// MakePlan decides what to do with a folder. It is pure: everything hard about
// the sync engine is decided here, where it can be tested without a server.
func MakePlan(local mirror.FolderState, remote FolderStatus) Plan {
	// Never synced, or a different incarnation of the folder. The trigger is
	// *changed*, never *greater than*: RFC 3501 does not promise monotonicity
	// even though Dovecot happens to increment.
	if !local.Synced() || local.UIDValidity != remote.UIDValidity {
		return Plan{Action: ActionResync}
	}

	// A modseq that went backwards means the server lost the state we were
	// counting on, so nothing we hold can be trusted to be current.
	if remote.HighestModSeq < local.HighestModSeq {
		return Plan{Action: ActionResync}
	}

	// A server without CONDSTORE reports no modseq at all, so there is no oracle
	// for "did a flag change" and the only safe answer is to look every cycle.
	// mailbox.org never takes this path; a lesser server would.
	if remote.HighestModSeq == 0 {
		p := Plan{Action: ActionIncremental}
		if remote.UIDNext > local.UIDNext {
			p.NewFrom = local.UIDNext
		}
		p.ExpungeDiff = int(remote.NumMessages) != local.Count
		return p
	}

	p := Plan{Action: ActionNone}
	if remote.HighestModSeq > local.HighestModSeq {
		p.Action = ActionIncremental
		p.FlagsSince = local.HighestModSeq
		if remote.UIDNext > local.UIDNext {
			p.NewFrom = local.UIDNext
		}
	}

	// The count is the only thing that reveals an expunge. It is checked even
	// when the modseq did not move, because a server that expunges without
	// bumping HIGHESTMODSEQ would otherwise hide it from us forever.
	if int(remote.NumMessages) != local.Count {
		p.Action = ActionIncremental
		p.ExpungeDiff = true
	}
	return p
}

// diffUIDs returns the uids present locally but not remotely: the expunged
// ones. Both slices must be sorted.
func diffUIDs(local, remote []uint32) []uint32 {
	have := make(map[uint32]struct{}, len(remote))
	for _, u := range remote {
		have[u] = struct{}{}
	}
	var gone []uint32
	for _, u := range local {
		if _, ok := have[u]; !ok {
			gone = append(gone, u)
		}
	}
	return gone
}
