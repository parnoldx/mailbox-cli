package mailsync

import (
	"testing"

	"mailbox/internal/mirror"
)

func TestMakePlan(t *testing.T) {
	synced := mirror.FolderState{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 10, Count: 4}

	tests := []struct {
		name   string
		local  mirror.FolderState
		remote FolderStatus
		want   Plan
	}{
		{
			name:   "never synced resyncs",
			local:  mirror.FolderState{},
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 1, HighestModSeq: 1},
			want:   Plan{Action: ActionResync},
		},
		{
			name:   "nothing moved",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 10, NumMessages: 4},
			want:   Plan{Action: ActionNone},
		},
		{
			name:   "uidvalidity changed resyncs",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1001, UIDNext: 5, HighestModSeq: 10, NumMessages: 4},
			want:   Plan{Action: ActionResync},
		},
		{
			// Equality is the test, not ordering: a lower UIDVALIDITY is just as
			// invalid as a higher one.
			name:   "uidvalidity decreased also resyncs",
			local:  synced,
			remote: FolderStatus{UIDValidity: 999, UIDNext: 5, HighestModSeq: 10, NumMessages: 4},
			want:   Plan{Action: ActionResync},
		},
		{
			name:   "modseq backwards resyncs",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 9, NumMessages: 4},
			want:   Plan{Action: ActionResync},
		},
		{
			name:   "flags only",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 12, NumMessages: 4},
			want:   Plan{Action: ActionIncremental, FlagsSince: 10},
		},
		{
			name:   "new messages",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 7, HighestModSeq: 12, NumMessages: 6},
			want:   Plan{Action: ActionIncremental, FlagsSince: 10, NewFrom: 5, ExpungeDiff: true},
		},
		{
			// The count is the only thing that reveals an expunge without
			// QRESYNC, so it must be checked even when the modseq did not move.
			name:   "count drop with unchanged modseq still diffs",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 10, NumMessages: 3},
			want:   Plan{Action: ActionIncremental, ExpungeDiff: true},
		},
		{
			name:   "modseq jump forwards is not a resync",
			local:  synced,
			remote: FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 9999, NumMessages: 4},
			want:   Plan{Action: ActionIncremental, FlagsSince: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MakePlan(tt.local, tt.remote)
			if got != tt.want {
				t.Errorf("MakePlan() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDiffUIDs(t *testing.T) {
	gone := diffUIDs([]uint32{1, 2, 3, 4}, []uint32{1, 3})
	if len(gone) != 2 || gone[0] != 2 || gone[1] != 4 {
		t.Errorf("diffUIDs() = %v, want [2 4]", gone)
	}
	if got := diffUIDs([]uint32{1, 2}, []uint32{1, 2}); got != nil {
		t.Errorf("diffUIDs() with no change = %v, want nil", got)
	}
}

// A server that reports no modseq gives us no way to tell whether flags moved,
// so every cycle must do a flag pass rather than trusting a comparison of two
// zeroes. Getting this wrong looks exactly like "nothing ever changes".
func TestMakePlanWithoutCondStore(t *testing.T) {
	local := mirror.FolderState{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 0, Count: 4}
	remote := FolderStatus{UIDValidity: 1000, UIDNext: 5, HighestModSeq: 0, NumMessages: 4}

	got := MakePlan(local, remote)
	if got.Action != ActionIncremental {
		t.Errorf("action = %v, want incremental — a modseq-less server would never sync flags", got.Action)
	}
	if got.FlagsSince != 0 {
		t.Errorf("FlagsSince = %d, want 0 (fetch all flags)", got.FlagsSince)
	}
}
