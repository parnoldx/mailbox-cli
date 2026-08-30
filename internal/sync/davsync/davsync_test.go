package davsync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mailbox/internal/mirror"
)

const calURL = "https://dav.example.org/caldav/kalender/"

func event(uid, summary, start string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:" + uid + "\r\nDTSTART;TZID=Europe/Berlin:" + start + "\r\nDURATION:PT1H\r\n" +
		"SUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func setup(t *testing.T) (*Reconciler, *Fake, *mirror.Mirror) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	f := NewFake("Kalender", calURL)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return &Reconciler{Account: "primary", Mirror: m, Driver: f, Location: berlin}, f, m
}

// only returns the single collection the Mirror holds, refreshed.
func only(t *testing.T, m *mirror.Mirror) mirror.Collection {
	t.Helper()
	cols, err := m.Collections("primary", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 {
		t.Fatalf("%d collections, want 1", len(cols))
	}
	return cols[0]
}

func TestDiscoveryRecordsWhatTheServerHas(t *testing.T) {
	r, f, m := setup(t)
	f.AddCollection(Collection{Kind: "tasks", URL: "https://dav.example.org/caldav/aufgaben/", Name: "Aufgaben"})
	cols, err := r.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("collections = %+v", cols)
	}
	// Named by their display names, which is what a caller can type.
	byName, err := m.CollectionNamed("primary", "events", "kalender")
	if err != nil || byName.URL != calURL {
		t.Fatalf("lookup by name: %+v (%v)", byName, err)
	}
}

func TestFirstSyncTakesEverythingAndRemembersTheToken(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt", "20260829T100000"))
	f.Deliver(calURL, "b.ics", event("b@example.org", "Standup", "20260830T093000"))
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}

	out, err := r.Sync(context.Background(), only(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Full || out.Changed != 2 {
		t.Fatalf("outcome = %+v", out)
	}
	c := only(t, m)
	if c.SyncToken == "" || c.Count != 2 {
		t.Fatalf("collection = %+v", c)
	}

	// The projection is there, so a window query never has to parse anything.
	objs, err := m.ObjectsIn("primary", "events",
		time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Summary != "Zahnarzt" || objs[0].Collection != "Kalender" {
		t.Fatalf("objects in the window = %+v", objs)
	}

	// And a second sync with the stored token has nothing to do.
	out, err = r.Sync(context.Background(), only(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if out.Full || out.Changed != 0 || out.Deleted != 0 {
		t.Fatalf("a quiet cycle did %+v", out)
	}
}

func TestChangesAndDeletionsArriveIncrementally(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt", "20260829T100000"))
	f.Deliver(calURL, "b.ics", event("b@example.org", "Standup", "20260830T093000"))
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(context.Background(), only(t, m)); err != nil {
		t.Fatal(err)
	}

	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt (verschoben)", "20260829T140000"))
	f.Remove(calURL, "b.ics")

	out, err := r.Sync(context.Background(), only(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if out.Full || out.Changed != 1 || out.Deleted != 1 {
		t.Fatalf("outcome = %+v", out)
	}
	if c := only(t, m); c.Count != 1 {
		t.Fatalf("collection holds %d", c.Count)
	}
	objs, err := m.ObjectsIn("primary", "events",
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Summary != "Zahnarzt (verschoben)" {
		t.Fatalf("objects = %+v", objs)
	}
}

func TestAForgottenTokenStartsAgainRatherThanFailing(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt", "20260829T100000"))
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(context.Background(), only(t, m)); err != nil {
		t.Fatal(err)
	}

	// The server's change log rolled over; our token means nothing to it. It
	// also quietly dropped an object while we were not looking, which is
	// exactly the state that makes a partial answer wrong.
	f.Remove(calURL, "a.ics")
	f.Deliver(calURL, "c.ics", event("c@example.org", "Neu", "20260901T100000"))
	f.Expire = true

	out, err := r.Sync(context.Background(), only(t, m))
	if err != nil {
		t.Fatalf("an expired token is not a failure: %v", err)
	}
	if !out.Full {
		t.Fatal("an expired token means starting from nothing")
	}
	if c := only(t, m); c.Count != 1 {
		t.Fatalf("collection holds %d objects, want just the surviving one", c.Count)
	}
	objs, _ := m.ObjectsIn("primary", "events",
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), "")
	if len(objs) != 1 || objs[0].Summary != "Neu" {
		t.Fatalf("objects after the restart = %+v", objs)
	}
}

func TestAChangeReportedWithoutItsDataIsFetched(t *testing.T) {
	r, f, m := setup(t)
	f.Detached = true
	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt", "20260829T100000"))
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(context.Background(), only(t, m)); err != nil {
		t.Fatal(err)
	}
	if f.CallCount("MultiGet") != 1 {
		t.Fatalf("multiget ran %d times", f.CallCount("MultiGet"))
	}
	objs, _ := m.ObjectsIn("primary", "events",
		time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), "")
	if len(objs) != 1 || objs[0].Raw == "" {
		t.Fatalf("objects = %+v", objs)
	}
}

func TestARepeatingEventIsOneRowAndStaysRelevant(t *testing.T) {
	r, f, m := setup(t)
	weekly := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:w@example.org\r\nDTSTART;TZID=Europe/Berlin:20260302T093000\r\nDURATION:PT30M\r\n" +
		"RRULE:FREQ=WEEKLY;BYDAY=MO\r\nSUMMARY:Wochenstart\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	f.Deliver(calURL, "w.ics", weekly)
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(context.Background(), only(t, m)); err != nil {
		t.Fatal(err)
	}

	// A window a year after it started still has to consider it: the rule has
	// no end, so there is no date after which it can be ruled out.
	objs, err := m.ObjectsIn("primary", "events",
		time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 3, 8, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || !objs[0].Recurring {
		t.Fatalf("a rule with no end fell out of a later window: %+v", objs)
	}
}

func TestSyncAllReportsEachCollection(t *testing.T) {
	r, f, m := setup(t)
	f.AddCollection(Collection{Kind: "events", URL: "https://dav.example.org/caldav/einkauf/", Name: "Einkauf"})
	f.Deliver(calURL, "a.ics", event("a@example.org", "Zahnarzt", "20260829T100000"))
	f.Deliver("https://dav.example.org/caldav/einkauf/", "e.ics", event("e@example.org", "Markt", "20260829T110000"))
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcomes, err := r.SyncAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes["Kalender"].Changed != 1 || outcomes["Einkauf"].Changed != 1 {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	cols, _ := m.Collections("primary", "events")
	if len(cols) != 2 {
		t.Fatalf("collections = %+v", cols)
	}
}

// partial is a server set where one server did not answer: the collections it
// returns are real but incomplete.
type partial struct {
	*Fake
	err error
}

func (p partial) Collections(ctx context.Context) ([]Collection, error) {
	cols, _ := p.Fake.Collections(ctx)
	return cols, p.err
}

func TestACollectionThatDisappearsIsDroppedWithItsObjects(t *testing.T) {
	r, f, m := setup(t)
	aufgaben := "https://dav.example.org/caldav/aufgaben/"
	f.AddCollection(Collection{Kind: "tasks", URL: aufgaben, Name: "Aufgaben"})
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Deliver(aufgaben, "1.ics", event("t1", "Milch", "20260901T090000"))
	if _, err := r.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Until this slice nothing was ever pruned: PutCollection is an upsert and
	// Discover had no delete beside it, so a calendar removed on the server
	// stayed in the Mirror forever.
	f.RemoveCollection(aufgaben)
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := only(t, m); got.Name != "Kalender" {
		t.Fatalf("the collection left is %q", got.Name)
	}
	objs, err := m.ObjectsIn("primary", "events",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "Aufgaben")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Fatalf("%d objects survived the collection they were on", len(objs))
	}
}

func TestAnExcludedCollectionIsNotMirroredAndIsDroppedIfItWas(t *testing.T) {
	r, f, m := setup(t)
	f.AddCollection(Collection{Kind: "cards", URL: "https://dav.example.org/card/2/", Name: "Gesammelte Adressen"})
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cols, _ := m.Collections("primary", ""); len(cols) != 2 {
		t.Fatalf("%d collections before excluding one", len(cols))
	}

	// The decision lives in the config, so it survives the Mirror being thrown
	// away and rebuilt (ADR-0013) — and it has to be applied on the way in, or
	// the next discovery puts back what was excluded.
	r.SetExclude([]string{"gesammelte adressen"})
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := only(t, m); got.Name != "Kalender" {
		t.Fatalf("the collection left is %q", got.Name)
	}
}

func TestAPartialAnswerNeverPrunes(t *testing.T) {
	r, f, m := setup(t)
	aufgaben := "https://dav.example.org/caldav/aufgaben/"
	f.AddCollection(Collection{Kind: "tasks", URL: aufgaben, Name: "Aufgaben"})
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}

	// One server of several did not answer. Its calendars are missing from the
	// list, and dropping them would turn a network problem into data loss.
	f.RemoveCollection(aufgaben)
	r.Driver = partial{Fake: f, err: errors.New("dial tcp: connection refused")}
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cols, _ := m.Collections("primary", ""); len(cols) != 2 {
		t.Fatalf("%d collections kept, want both", len(cols))
	}
}
