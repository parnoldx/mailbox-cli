package davsync

import (
	"context"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/vcal"
)

const taskURL = "https://dav.example.org/caldav/aufgaben/"

func writer(t *testing.T) (*Writer, *Fake, *mirror.Mirror, mirror.Collection) {
	t.Helper()
	r, f, m := setup(t)
	f.AddCollection(Collection{Kind: "tasks", URL: taskURL, Name: "Aufgaben"})
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	col, err := m.CollectionNamed("primary", "tasks", "Aufgaben")
	if err != nil {
		t.Fatal(err)
	}
	return &Writer{Account: "primary", Mirror: m, Driver: f, Reconciler: r}, f, m, col
}

func newTodo(t *testing.T, summary string) (uid, raw string) {
	t.Helper()
	uid = vcal.NewUID()
	raw, err := vcal.NewTodo(uid, summary, time.Time{}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	return uid, raw
}

func TestAWriteGoesToTheServerAndTheMirrorFollows(t *testing.T) {
	w, f, m, col := writer(t)
	uid, raw := newTodo(t, "Rechnung bezahlen")

	object, err := w.Put(context.Background(), col, Href(col, uid), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if object.UID != uid || object.Summary != "Rechnung bezahlen" || object.Kind != "todo" {
		t.Fatalf("object = %+v", object)
	}
	if object.ETag == "" {
		t.Fatal("the mirror should hold the etag the server gave it")
	}
	// The href stored is the path form, which is the shape the next sync
	// reports. Anything else and one object becomes two rows.
	if strings.HasPrefix(object.Href, "http") {
		t.Fatalf("href = %q, want a path", object.Href)
	}

	// And it is there for the reading, with no cycle in between (ADR-0004).
	todos, err := m.Todos("primary", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 1 || todos[0].Summary != "Rechnung bezahlen" {
		t.Fatalf("todos = %+v", todos)
	}

	// A sync straight afterwards finds the same object under the same href and
	// leaves one row, not two.
	col, err = m.CollectionNamed("primary", "tasks", "Aufgaben")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Reconciler.Sync(context.Background(), col); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Todos("primary", "", false); len(got) != 1 {
		t.Fatalf("after a sync there are %d todos", len(got))
	}
	_ = f
}

func TestAWriteRefusedBecauseItChangedLeavesTheMirrorAlone(t *testing.T) {
	w, f, m, col := writer(t)
	uid, raw := newTodo(t, "Zahnarzt anrufen")
	object, err := w.Put(context.Background(), col, Href(col, uid), raw, "")
	if err != nil {
		t.Fatal(err)
	}

	// Somebody else edits it. Our etag is now stale.
	f.Deliver(taskURL, object.Href, strings.Replace(raw, "Zahnarzt anrufen", "Zahnarzt abgesagt", 1))

	done, err := vcal.Complete(object.Raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Put(context.Background(), col, object.Href, done, object.ETag); err == nil {
		t.Fatal("a write against a stale etag must be refused, not forced")
	}
	after, err := m.Object("primary", object.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status == "COMPLETED" {
		t.Fatal("the mirror recorded a write the server refused")
	}
}

func TestAServerThatSaysNothingIsReadBack(t *testing.T) {
	w, f, m, col := writer(t)
	f.SilentPut = true
	uid, raw := newTodo(t, "Milch kaufen")

	object, err := w.Put(context.Background(), col, Href(col, uid), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	// No ETag came back, so the object was read again rather than assumed.
	if f.CallCount("MultiGet") != 1 {
		t.Fatalf("multiget ran %d times", f.CallCount("MultiGet"))
	}
	if object.ETag == "" {
		t.Fatalf("object = %+v", object)
	}
	if got, _ := m.Todos("primary", "", false); len(got) != 1 || got[0].Summary != "Milch kaufen" {
		t.Fatalf("todos = %+v", got)
	}
}

func TestCompletingATodoTakesItOffTheList(t *testing.T) {
	w, m := func() (*Writer, *mirror.Mirror) {
		w, _, m, col := writer(t)
		uid, raw := newTodo(t, "Rechnung bezahlen")
		object, err := w.Put(context.Background(), col, Href(col, uid), raw, "")
		if err != nil {
			t.Fatal(err)
		}
		done, err := vcal.Complete(object.Raw, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Put(context.Background(), col, object.Href, done, object.ETag); err != nil {
			t.Fatal(err)
		}
		return w, m
	}()

	open, err := m.Todos("primary", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a completed todo is still on the list: %+v", open)
	}
	all, err := m.Todos("primary", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Status != "COMPLETED" || all[0].Completed.IsZero() {
		t.Fatalf("todos = %+v", all)
	}
	_ = w
}

func TestDeleteTakesItOffTheServerAndOutOfTheMirror(t *testing.T) {
	w, f, m, col := writer(t)
	uid, raw := newTodo(t, "Kündigen")
	object, err := w.Put(context.Background(), col, Href(col, uid), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Delete(context.Background(), col, object); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Todos("primary", "", true); len(got) != 0 {
		t.Fatalf("todos = %+v", got)
	}
	// And the server agrees, which the next sync would tell us anyway.
	changes, err := f.Sync(context.Background(), taskURL, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range changes.Items {
		if it.Href == object.Href && !it.Deleted {
			t.Fatal("the object is still on the server")
		}
	}
}
