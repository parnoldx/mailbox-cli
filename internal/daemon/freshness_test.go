package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"mailbox/internal/sync/davsync"
)

// deadDAV is a server that refuses every request, the way one behind a network
// that went down does.
type deadDAV struct{}

func (deadDAV) Collections(context.Context) ([]davsync.Collection, error) {
	return nil, errors.New("dial dav.example.org: connection refused")
}

func (deadDAV) Sync(context.Context, string, string) (davsync.Changes, error) {
	return davsync.Changes{}, errors.New("dial dav.example.org: connection refused")
}

func (deadDAV) MultiGet(context.Context, string, []string) ([]davsync.Change, error) {
	return nil, errors.New("dial dav.example.org: connection refused")
}

func mirrorOf(t *testing.T, d *Daemon, cmd ...string) *MirrorState {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: cmd})
	if resp.Mirror == nil {
		t.Fatalf("%v answered with no mirror state", cmd)
	}
	return resp.Mirror
}

// Gate: the collections have their own freshness. A DAV server nobody has
// reached must not be covered for by a healthy IMAP one — the loops are
// separate, so the answers about them are too.
func TestTodoFreshnessIsNotMailFreshness(t *testing.T) {
	d, _, _ := seedTasks(t)
	d.setConnected("primary", true)

	if st := mirrorOf(t, d, "box", "list"); st.Behind {
		t.Errorf("mail read is Behind although the mail server answered: %+v", st)
	}
	st := mirrorOf(t, d, "todo", "list")
	if !st.Behind {
		t.Errorf("todo read is current although the DAV server was never reached: %+v", st)
	}
	if st.SyncedAt != nil {
		t.Errorf("todo read reports a sync time before any DAV cycle: %v", st.SyncedAt)
	}
}

// Gate: a cycle that reached the server makes the reads that follow it current,
// and one that could not reach it makes them Behind again.
func TestDAVCycleSetsCollectionFreshness(t *testing.T) {
	d, _, _ := seedTasks(t)
	ctx := context.Background()

	d.davCycle(ctx, "test", "events", "tasks")
	st := mirrorOf(t, d, "todo", "list")
	if st.Behind || st.SyncedAt == nil {
		t.Fatalf("after a good cycle: %+v", st)
	}
	if st.Syncing {
		t.Errorf("reports a cycle running after it finished: %+v", st)
	}
	if age := time.Since(*st.SyncedAt); age > time.Minute {
		t.Errorf("sync time is %v old, want just now", age)
	}

	d.DAV.Driver = deadDAV{}
	d.davCycle(ctx, "test", "events", "tasks")
	if st := mirrorOf(t, d, "todo", "list"); !st.Behind {
		t.Errorf("current although the DAV server refused every request: %+v", st)
	}
}

// kick takes what a command queued, if anything. It never waits: the point of
// the nudge is that the command it came from did not wait either.
func kick(t *testing.T, d *Daemon) (davKick, bool) {
	t.Helper()
	select {
	case k := <-d.davTrigger:
		return k, true
	default:
		return davKick{}, false
	}
}

// Gate: reading asks for a cycle without waiting for one, so the read after it
// sees the todo that was deleted on the server. Rate limited, because a caller
// in a loop must not become a caller hammering the server.
func TestAReadNudgesADAVCycle(t *testing.T) {
	d, _, _ := seedTasks(t)

	mirrorOf(t, d, "todo", "list")
	k, ok := kick(t, d)
	if !ok {
		t.Fatal("a todo read queued no cycle")
	}
	if got := k.kinds; len(got) != 2 || got[0] != "events" || got[1] != "tasks" {
		t.Errorf("nudged kinds = %v, want events and tasks", got)
	}

	mirrorOf(t, d, "todo", "list")
	if k, ok := kick(t, d); ok {
		t.Errorf("a second read straight afterwards queued another cycle: %+v", k)
	}

	// A different kind has its own limit: an address book nobody nudged is not
	// covered by a task list somebody did.
	mirrorOf(t, d, "contact", "list")
	k, ok = kick(t, d)
	if !ok {
		t.Fatal("a contact read queued no cycle")
	}
	if got := k.kinds; len(got) != 1 || got[0] != "cards" {
		t.Errorf("nudged kinds = %v, want cards", got)
	}

	// Mail is not the DAV loop's business.
	mirrorOf(t, d, "box", "list")
	if k, ok := kick(t, d); ok {
		t.Errorf("a mail read queued a DAV cycle: %+v", k)
	}
}

// Gate: a nudge that was dropped because a cycle was already queued must not
// spend the rate limit. Recording it on the way in would let a nudge that
// caused nothing hold the door shut for the next half minute.
func TestADroppedNudgeDoesNotSpendTheRateLimit(t *testing.T) {
	d, _, _ := seedTasks(t)
	d.davTrigger <- davKick{reason: "poll"}

	mirrorOf(t, d, "todo", "list") // dropped: the queue is full
	if k, _ := kick(t, d); k.reason != "poll" {
		t.Fatalf("queued kick = %+v, want the one that was already there", k)
	}
	mirrorOf(t, d, "todo", "list")
	if _, ok := kick(t, d); !ok {
		t.Error("the read after a dropped nudge queued nothing")
	}
}

// Gate: a daemon with no collections configured is never Behind on them. Saying
// otherwise would mark every reply Behind for ever on a mail-only setup.
func TestNoDAVMeansNoDAVStaleness(t *testing.T) {
	d, _, _ := seedTasks(t)
	d.DAV = nil
	d.setConnected("primary", true)
	if st := mirrorOf(t, d, "todo", "list"); st.Behind {
		t.Errorf("Behind with no DAV configured: %+v", st)
	}
}
