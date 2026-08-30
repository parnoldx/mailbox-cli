//go:build live

// The live half of writing to a calendar server.
//
//	go test -tags live ./internal/davdrv/ -run TestLiveWrite -v
//
// It creates its own task list, writes to it, and deletes it again. Nothing it
// touches belongs to anybody.
package davdrv

import (
	"context"
	"strings"
	"testing"
	"time"

	"mailbox/internal/vcal"
)

const scratchList = "mailbox-selftest-tasks"

func TestLiveWriteATodo(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	col, err := c.EnsureCalendar(ctx, scratchList, []string{"VTODO"})
	if err != nil {
		t.Fatalf("create %s: %v", scratchList, err)
	}
	t.Cleanup(func() {
		if err := c.DeleteCollection(context.Background(), col.URL); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
	t.Logf("gate: %s exists at %s (kind %s)", col.Name, col.URL, col.Kind)
	if col.Kind != "tasks" {
		t.Errorf("a collection created for VTODO came back as %q", col.Kind)
	}

	uid := vcal.NewUID()
	due := time.Now().AddDate(0, 0, 1)
	raw, err := vcal.NewTodo(uid, "mailbox selftest — Rechnung bezahlen", due, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	href := strings.TrimSuffix(col.URL, "/") + "/" + uid + ".ics"

	etag, err := c.Put(ctx, href, raw, "")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	t.Logf("gate: PUT returned etag %q", etag)

	// What the server holds is what a later read gets, and it has to parse.
	changes, err := c.Sync(ctx, col.URL, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(changes.Items) != 1 {
		t.Fatalf("the list holds %d objects", len(changes.Items))
	}
	stored := changes.Items[0]
	p, err := vcal.Parse(stored.Data, time.Local)
	if err != nil {
		t.Fatalf("what we wrote does not parse coming back: %v\n%s", err, stored.Data)
	}
	if p.Kind != vcal.KindTodo || p.UID != uid {
		t.Fatalf("projection = %+v", p)
	}
	if p.Summary != "mailbox selftest — Rechnung bezahlen" {
		t.Fatalf("summary came back as %q — the encoding did not survive", p.Summary)
	}
	if p.Due.Format("2006-01-02") != due.Format("2006-01-02") {
		t.Fatalf("due = %s, want %s", p.Due, due.Format("2006-01-02"))
	}

	// Completing it is a read, an edit and a conditional write.
	done, err := vcal.Complete(stored.Data, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(ctx, href, done, stored.ETag); err != nil {
		t.Fatalf("conditional put: %v", err)
	}
	after, err := c.Sync(ctx, col.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err = vcal.Parse(after.Items[0].Data, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "COMPLETED" || p.Completed.IsZero() {
		t.Fatalf("after completing: status=%q completed=%s", p.Status, p.Completed)
	}
	t.Log("gate: the todo came back completed, with the timestamp the server kept")

	// A write against the etag we already replaced must be refused: that is
	// somebody else's change we would be stamping on.
	if _, err := c.Put(ctx, href, done, stored.ETag); err == nil {
		t.Error("a stale If-Match was accepted — two clients would silently overwrite each other")
	} else {
		t.Logf("gate: a stale If-Match is refused (%v)", err)
	}

	if err := c.Delete(ctx, href, ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, err := c.Sync(ctx, col.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range gone.Items {
		if !it.Deleted && strings.Contains(it.Href, uid) {
			t.Fatal("the object survived being deleted")
		}
	}
	t.Log("gate: deleted")
}

// TestLiveHabitsObjectIsListed is the regression test for the one thing about
// this server that no RFC would have told us: it exposes a *window* of each
// calendar over CalDAV, roughly a year back, and an object outside it is stored,
// fetchable by URL, and reported by no listing at all (ADR-0018).
//
// The habits record is a VEVENT, so where it is dated decides whether it exists
// as far as every sync is concerned. It is dated today, on every write.
func TestLiveHabitsObjectIsListed(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	col, err := c.EnsureCalendar(ctx, "mailbox-selftest-habits", []string{"VEVENT"})
	if err != nil {
		t.Fatalf("create the scratch calendar: %v", err)
	}
	t.Cleanup(func() {
		if err := c.DeleteCollection(context.Background(), col.URL); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	write := func(uid string, on time.Time) string {
		t.Helper()
		raw, err := vcal.NewEventObject(uid, "mailbox-habits", `{"habits":[{"id":"x","name":"probe"}]}`, on)
		if err != nil {
			t.Fatal(err)
		}
		href := strings.TrimSuffix(col.URL, "/") + "/" + uid + ".ics"
		if _, err := c.Put(ctx, href, raw, ""); err != nil {
			t.Fatalf("put %s: %v", uid, err)
		}
		return href
	}
	write("selftest-habits-today", time.Now())
	write("selftest-habits-1990", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))

	changes, err := c.Sync(ctx, col.URL, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	listed := map[string]bool{}
	for _, it := range changes.Items {
		for _, uid := range []string{"selftest-habits-today", "selftest-habits-1990"} {
			if strings.Contains(it.Href, uid) {
				listed[uid] = true
			}
		}
	}
	if !listed["selftest-habits-today"] {
		t.Fatal("an object dated today is not listed — the habits record would be invisible")
	}
	// Not asserted, because it is the server's choice and it may change. Logged,
	// because it is the whole reason the record is dated today.
	t.Logf("gate: today's object is listed; the 1990 one is %s",
		map[bool]string{true: "listed too", false: "invisible, as this server does it"}[listed["selftest-habits-1990"]])

	// And the second write of the same record has to work. This server keeps
	// its own SEQUENCE and LAST-MODIFIED on what it stored, and a PUT of a
	// freshly built object without them is refused as an outdated update —
	// `412`, with or without an If-Match. The record is therefore *edited*.
	href := strings.TrimSuffix(col.URL, "/") + "/selftest-habits-today.ics"
	stored, err := c.MultiGet(ctx, col.URL, []string{href})
	if err != nil || len(stored) != 1 {
		t.Fatalf("read back: %v (%d)", err, len(stored))
	}
	edited, err := vcal.SetDescription(stored[0].Data, `{"habits":[{"id":"x","name":"probe","done":["2026-08-29"]}]}`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(ctx, href, edited, stored[0].ETag); err != nil {
		t.Fatalf("second write: %v", err)
	}
	t.Log("gate: the record can be written again, having been edited rather than rebuilt")

	rebuilt, err := vcal.NewEventObject("selftest-habits-today", "mailbox-habits", `{"habits":[]}`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(ctx, href, rebuilt, ""); err != nil {
		t.Logf("gate: rebuilding it instead is refused by this server (%v)", err)
	} else {
		t.Log("note: this server also accepts a rebuilt object; it did not when this was written")
	}
}
