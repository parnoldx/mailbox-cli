package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/sync/davsync"
)

// changes drains what a watch has been told so far. Every line is written
// before the cycle that caused it returns, so there is nothing to wait for.
func changes(ch chan Change) []Change {
	var out []Change
	for {
		select {
		case c := <-ch:
			out = append(out, c)
		default:
			return out
		}
	}
}

// only is the changes of one event, which is what an assertion is usually about:
// a cycle also reports the Boxes the test is not looking at.
func only(got []Change, event string) []Change {
	var out []Change
	for _, c := range got {
		if c.Event == event {
			out = append(out, c)
		}
	}
	return out
}

func watchOn(t *testing.T, d *Daemon, args map[string]any) chan Change {
	t.Helper()
	ch := make(chan Change, 64)
	resp := d.subscribe(Request{ID: "1", Cmd: []string{"watch"}, Args: args}, ch)
	if !resp.OK {
		t.Fatalf("watch: %s (%s)", resp.Error, resp.Code)
	}
	return ch
}

// A delivery is new mail; reading it elsewhere is an update and not new; and a
// move is a deleted and an added that is not new either — the mail was already
// here, it only changed Box.
func TestWatchReportsNewMailAndTellsAMoveApartFromIt(t *testing.T) {
	d := seed(t)
	ctx := context.Background()
	a := d.primaryAccount()
	d.cycle(ctx, a, "baseline")

	ch := watchOn(t, d, nil)
	f := fakeOf(d)
	msg := f.Deliver("INBOX", "neu@example.com", "Angebot", "das Angebot")
	msg.From = "sales@example.com"
	d.cycle(ctx, a, "delivery")

	added := only(changes(ch), eventAdded)
	if len(added) != 1 {
		t.Fatalf("added = %+v", added)
	}
	if !added[0].New || added[0].Box != "INBOX" || added[0].Subject != "Angebot" {
		t.Fatalf("delivery = %+v", added[0])
	}
	if added[0].From != "sales@example.com" || added[0].Thread == 0 || added[0].Message == 0 {
		t.Fatalf("delivery names nothing to act on: %+v", added[0])
	}

	// Read in another client: an update, and nothing new about it.
	f.SetFlags("INBOX", msg.UID, `\Seen`)
	d.cycle(ctx, a, "read elsewhere")
	updated := only(changes(ch), eventUpdated)
	if len(updated) != 1 || updated[0].Message != added[0].Message || updated[0].New {
		t.Fatalf("updated = %+v", updated)
	}

	// Moved in another client, and unread, so that only the move itself can be
	// what stops it being called new mail.
	second := f.Deliver("INBOX", "zweit@example.com", "Nachfrage", "und noch was")
	second.From = "sales@example.com"
	d.cycle(ctx, a, "second delivery")
	arrivedFirst := only(changes(ch), eventAdded)
	if len(arrivedFirst) != 1 || !arrivedFirst[0].New {
		t.Fatalf("the second delivery is new mail too: %+v", arrivedFirst)
	}
	if _, err := f.Move(ctx, "INBOX", []uint32{second.UID}, "Archive"); err != nil {
		t.Fatal(err)
	}
	d.cycle(ctx, a, "moved elsewhere")
	got := changes(ch)
	gone, arrived := only(got, eventDeleted), only(got, eventAdded)
	if len(gone) != 1 || gone[0].Box != "INBOX" || gone[0].Message != arrivedFirst[0].Message {
		t.Fatalf("deleted = %+v", gone)
	}
	if len(arrived) != 1 || arrived[0].Box != "Archive" || arrived[0].New {
		t.Fatalf("a move is not new mail: %+v", arrived)
	}
}

// The feed is a subscription: a connection that never asked for it is told
// nothing, whatever the cycle did (ADR-0027).
func TestOnlyASubscribedConnectionIsToldWhatMoved(t *testing.T) {
	d := seed(t)
	ctx := context.Background()
	a := d.primaryAccount()
	d.cycle(ctx, a, "baseline")

	pushes := make(chan Push, 16)
	d.mu.Lock()
	d.clients[pushes] = struct{}{}
	d.mu.Unlock()

	f := fakeOf(d)
	f.Deliver("INBOX", "neu@example.com", "Angebot", "das Angebot")
	d.cycle(ctx, a, "delivery")

	if len(pushes) == 0 {
		t.Fatal("a widget was not nudged")
	}
	d.mu.Lock()
	watchers := len(d.watchers)
	d.mu.Unlock()
	if watchers != 0 {
		t.Fatalf("%d watchers without anybody asking", watchers)
	}
}

// A watch scoped to a Box reports that Box and no other, and is a watch on mail:
// the collections are off without a flag that says so.
func TestABoxScopedWatchIsAWatchOnMail(t *testing.T) {
	w := &watcher{boxes: []string{"inbox"}}
	for _, c := range []Change{
		{Event: eventAdded, Box: "INBOX"},
		{Event: eventDeleted, Box: "INBOX"},
	} {
		if !w.wants(c) {
			t.Fatalf("%+v was not reported", c)
		}
	}
	for _, c := range []Change{
		{Event: eventAdded, Box: "Screener"},
		{Event: eventObjectAdded, Collection: "Kalender"},
	} {
		if w.wants(c) {
			t.Fatalf("%+v should be out of scope", c)
		}
	}
	// --events new is the new ones, whatever they are called on the line.
	sel := &watcher{events: map[string]bool{eventNew: true}}
	if !sel.wants(Change{Event: eventAdded, New: true}) || sel.wants(Change{Event: eventAdded}) {
		t.Fatal("--events new did not select the new mail")
	}
	// Both lines that describe the watch always get through.
	for _, e := range []string{eventReady, eventDisconnected} {
		if !sel.wants(Change{Event: e}) {
			t.Fatalf("%s was filtered out", e)
		}
	}
}

// An event nobody has heard of is a typo, and a watch that reported nothing
// because of one would look exactly like a quiet mailbox.
func TestAnUnknownEventIsRefusedRatherThanIgnored(t *testing.T) {
	d := seed(t)
	ch := make(chan Change, 4)
	resp := d.subscribe(Request{ID: "1", Cmd: []string{"watch"},
		Args: map[string]any{"events": []any{"added", "adde"}}}, ch)
	if resp.OK || resp.Code != "usage" || !strings.Contains(resp.Error, "adde") {
		t.Fatalf("resp = %+v", resp)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.watchers) != 0 {
		t.Fatal("a refused watch was registered anyway")
	}
}

// A calendar reports what moved on it once it is past its first sync, which is
// a resync: everything on a collection read from nothing would look new.
func TestWatchReportsWhatMovedOnACollection(t *testing.T) {
	d, f, _ := seedTasks(t)
	ctx := context.Background()
	d.davCycle(ctx, "baseline", "events", "tasks")

	ch := watchOn(t, d, nil)
	f.Deliver(testCalURL, "standup.ics", ics(`BEGIN:VEVENT
UID:standup@example.org
DTSTART:20260907T090000Z
DTEND:20260907T091500Z
SUMMARY:Standup
END:VEVENT`))
	d.davCycle(ctx, "delivery", "events")

	got := only(changes(ch), eventObjectAdded)
	if len(got) != 1 {
		t.Fatalf("object_added = %+v", got)
	}
	if got[0].Collection != "Kalender" || got[0].Summary != "Standup" || got[0].Object == 0 {
		t.Fatalf("added = %+v", got[0])
	}
	if got[0].Kind != "event" {
		t.Fatalf("kind = %q", got[0].Kind)
	}

	// The same object again is an update, and its removal names what went.
	f.Deliver(testCalURL, "standup.ics", ics(`BEGIN:VEVENT
UID:standup@example.org
DTSTART:20260907T100000Z
DTEND:20260907T101500Z
SUMMARY:Standup (verlegt)
END:VEVENT`))
	d.davCycle(ctx, "changed", "events")
	if got := only(changes(ch), eventObjectUpdated); len(got) != 1 || got[0].Summary != "Standup (verlegt)" {
		t.Fatalf("object_updated = %+v", got)
	}
	f.Remove(testCalURL, "standup.ics")
	d.davCycle(ctx, "removed", "events")
	if got := only(changes(ch), eventObjectDeleted); len(got) != 1 || got[0].Summary != "Standup (verlegt)" {
		t.Fatalf("object_deleted = %+v", got)
	}
}

// A calendar added on the server is a change like any other, and discovery is
// the only place that sees it.
func TestWatchReportsACollectionComingAndGoing(t *testing.T) {
	d, f, _ := seedTasks(t)
	ctx := context.Background()

	ch := watchOn(t, d, nil)
	before, err := d.Mirror.Collections(d.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	f.AddCollection(davsync.Collection{Kind: "events", URL: "https://dav.example.com/cal/work/", Name: "Work"})
	now, err := d.DAV.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.watchCollections(before, now)
	if got := only(changes(ch), eventCollectionAdded); len(got) != 1 || got[0].Collection != "Work" {
		t.Fatalf("collection_added = %+v", got)
	}

	before = now
	f.RemoveCollection("https://dav.example.com/cal/work/")
	if now, err = d.DAV.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	d.watchCollections(before, now)
	if got := only(changes(ch), eventCollectionDeleted); len(got) != 1 || got[0].Collection != "Work" {
		t.Fatalf("collection_deleted = %+v", got)
	}
}
