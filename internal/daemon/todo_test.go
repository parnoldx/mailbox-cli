package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
)

const (
	testCalURL   = "https://dav.example.org/caldav/kalender/"
	testTaskURL  = "https://dav.example.org/caldav/aufgaben/"
	testShopURL  = "https://dav.example.org/caldav/einkauf/"
	testHabitURL = "https://dav.example.org/caldav/mailbox-habits/"
)

// maker stands in for the server's ability to create a collection. It is the
// one collection this program makes rather than finds.
type maker struct {
	fake    *davsync.Fake
	created []string
}

func (m *maker) EnsureCalendar(ctx context.Context, name string, comps []string) (davsync.Collection, error) {
	m.created = append(m.created, name)
	col := davsync.Collection{Kind: "events", URL: testHabitURL, Name: name}
	m.fake.AddCollection(col)
	return col, nil
}

// seedTasks builds a Daemon with a scripted DAV server holding a calendar and
// one task list.
func seedTasks(t *testing.T, extraLists ...davsync.Collection) (*Daemon, *davsync.Fake, *maker) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	f := davsync.NewFake("Kalender", testCalURL)
	f.AddCollection(davsync.Collection{Kind: "tasks", URL: testTaskURL, Name: "Aufgaben"})
	for _, extra := range extraLists {
		f.AddCollection(extra)
	}
	r := &davsync.Reconciler{Account: "primary", Mirror: m, Driver: f, Location: time.Local}
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := New("primary", m, nil, nil, nil, nil)
	d.DAV = r
	d.DAVWriter = &davsync.Writer{Account: "primary", Mirror: m, Driver: f, Reconciler: r}
	mk := &maker{fake: f}
	d.DAVHome = mk
	return d, f, mk
}

func todos(t *testing.T, d *Daemon, args map[string]any) []todo {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "list"}, Args: args})
	if !resp.OK {
		t.Fatalf("todo list: %s (%s)", resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]todo)
	if !ok {
		t.Fatalf("todo list returned %T", resp.Data)
	}
	return out
}

func addTodo(t *testing.T, d *Daemon, args map[string]any) todo {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "add"}, Args: args})
	if !resp.OK {
		t.Fatalf("todo add: %s (%s)", resp.Error, resp.Code)
	}
	return resp.Data.(todo)
}

func TestATodoAddedIsOnTheListWithNoCycleInBetween(t *testing.T) {
	d, _, _ := seedTasks(t)
	added := addTodo(t, d, map[string]any{"positional": "Rechnung bezahlen", "due": "tomorrow"})
	if added.Summary != "Rechnung bezahlen" || added.List != "Aufgaben" {
		t.Fatalf("added = %+v", added)
	}
	if added.Due != time.Now().AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("due = %q", added.Due)
	}

	list := todos(t, d, nil)
	if len(list) != 1 || list[0].ID != added.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestCompletingATodoTakesItOffTheList(t *testing.T) {
	d, _, _ := seedTasks(t)
	added := addTodo(t, d, map[string]any{"positional": "Milch kaufen"})

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "done"},
		Args: map[string]any{"positional": added.ID}})
	if !resp.OK {
		t.Fatalf("todo done: %s (%s)", resp.Error, resp.Code)
	}
	if done := resp.Data.(todo); !done.Done {
		t.Fatalf("todo = %+v", done)
	}
	if left := todos(t, d, nil); len(left) != 0 {
		t.Fatalf("still on the list: %+v", left)
	}
	// It is not gone, it is done: a list of what is left is not a list of what
	// never happened.
	if all := todos(t, d, map[string]any{"all": true}); len(all) != 1 || !all[0].Done {
		t.Fatalf("with --all: %+v", all)
	}

	// And it can come back.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "undone"},
		Args: map[string]any{"positional": added.ID}})
	if !resp.OK {
		t.Fatalf("todo undone: %s", resp.Error)
	}
	if left := todos(t, d, nil); len(left) != 1 {
		t.Fatalf("after undone: %+v", left)
	}
}

func TestSeveralTaskListsHaveToBeNamed(t *testing.T) {
	d, _, _ := seedTasks(t, davsync.Collection{Kind: "tasks", URL: testShopURL, Name: "Einkauf"})
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "add"},
		Args: map[string]any{"positional": "Milch"}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
	// The error says what to type instead, the way an ambiguous attachment id
	// does.
	if !strings.Contains(resp.Error, "Aufgaben") || !strings.Contains(resp.Error, "Einkauf") {
		t.Fatalf("error = %q", resp.Error)
	}

	added := addTodo(t, d, map[string]any{"positional": "Milch", "list": "einkauf"})
	if added.List != "Einkauf" {
		t.Fatalf("added = %+v", added)
	}

	// A configured default answers it for the ordinary case.
	d.TaskList = "Aufgaben"
	if added := addTodo(t, d, map[string]any{"positional": "Rechnung"}); added.List != "Aufgaben" {
		t.Fatalf("added = %+v", added)
	}
	// A list that does not exist is a mistake, not a new list.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "add"},
		Args: map[string]any{"positional": "x", "list": "Garten"}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDroppingATodoRemovesIt(t *testing.T) {
	d, _, _ := seedTasks(t)
	added := addTodo(t, d, map[string]any{"positional": "Doch nicht"})
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "drop"},
		Args: map[string]any{"positional": added.ID}})
	if !resp.OK {
		t.Fatalf("todo drop: %s", resp.Error)
	}
	if all := todos(t, d, map[string]any{"all": true}); len(all) != 0 {
		t.Fatalf("todos = %+v", all)
	}
}

func habits(t *testing.T, d *Daemon, verb string, args map[string]any) []habitRow {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"habit", verb}, Args: args})
	if !resp.OK {
		t.Fatalf("habit %s: %s (%s)", verb, resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]habitRow)
	if !ok {
		t.Fatalf("habit %s returned %T", verb, resp.Data)
	}
	return out
}

func TestHabitsAreKeptInOneObjectOnACalendarWeCreate(t *testing.T) {
	d, f, mk := seedTasks(t)

	// Nothing yet, and listing must not create anything.
	if rows := habits(t, d, "list", nil); len(rows) != 0 {
		t.Fatalf("habits = %+v", rows)
	}
	if len(mk.created) != 0 {
		t.Fatal("listing habits created a calendar")
	}

	rows := habits(t, d, "add", map[string]any{"positional": "Meditation", "days": "mon,tue,wed,thu,fri"})
	if len(rows) != 1 || rows[0].Name != "Meditation" {
		t.Fatalf("habits = %+v", rows)
	}
	if len(mk.created) != 1 || mk.created[0] != habit.CalendarName {
		t.Fatalf("created = %v", mk.created)
	}
	// One object holds all of them, so a second habit is a second entry in the
	// same object rather than a second object (ADR-0018).
	habits(t, d, "add", map[string]any{"positional": "Sport"})
	changes, err := f.Sync(context.Background(), testHabitURL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Items) != 1 {
		t.Fatalf("the habits calendar holds %d objects", len(changes.Items))
	}
	if !strings.Contains(changes.Items[0].Data, "Meditation") || !strings.Contains(changes.Items[0].Data, "Sport") {
		t.Fatalf("the object does not hold both habits:\n%s", changes.Items[0].Data)
	}
}

func TestTickingOffAHabitIsAFactAboutADay(t *testing.T) {
	d, _, _ := seedTasks(t)
	habits(t, d, "add", map[string]any{"positional": "Meditation"})

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	habits(t, d, "done", map[string]any{"positional": "Meditation", "date": yesterday})
	rows := habits(t, d, "done", map[string]any{"positional": "Meditation"})
	if len(rows) != 1 || !rows[0].Done {
		t.Fatalf("habits = %+v", rows)
	}
	if rows[0].Streak != 2 {
		t.Fatalf("streak = %d, want 2", rows[0].Streak)
	}

	// Doing it twice is doing it once.
	rows = habits(t, d, "done", map[string]any{"positional": "Meditation"})
	if rows[0].Streak != 2 {
		t.Fatalf("streak after a second tick = %d", rows[0].Streak)
	}

	// Yesterday, seen from yesterday, is done; today, from yesterday, is not
	// even asked about.
	rows = habits(t, d, "list", map[string]any{"date": yesterday})
	if !rows[0].Done {
		t.Fatalf("yesterday = %+v", rows[0])
	}

	rows = habits(t, d, "undone", map[string]any{"positional": "Meditation"})
	if rows[0].Done {
		t.Fatalf("habits = %+v", rows)
	}
}

func TestAHabitThatIsNotDueTodayIsNotMissed(t *testing.T) {
	d, _, _ := seedTasks(t)
	// Due on a day that is not today, whichever day today is.
	today := habit.DayOf(time.Now())
	var other string
	for _, day := range habit.Weekdays {
		if day != today {
			other = day
			break
		}
	}
	rows := habits(t, d, "add", map[string]any{"positional": "Wäsche", "days": other})
	if len(rows) != 1 || rows[0].Due {
		t.Fatalf("habits = %+v", rows)
	}
}

func TestAnUnknownHabitIsNotFound(t *testing.T) {
	d, _, _ := seedTasks(t)
	habits(t, d, "add", map[string]any{"positional": "Meditation"})
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"habit", "done"},
		Args: map[string]any{"positional": "Klavier"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

// An edit changes only what it was given. A rename that also reset the days
// would lose a schedule nobody meant to touch, and the days are the part
// hardest to reconstruct.
func TestHabitEditChangesOnlyWhatItWasGiven(t *testing.T) {
	d, _, _ := seedTasks(t)
	mustAsk(t, d, []string{"habit", "add"},
		map[string]any{"positional": "Lesen", "days": "mon,wed", "color": "#123456"})
	mustAsk(t, d, []string{"habit", "done"}, map[string]any{"positional": "Lesen"})

	mustAsk(t, d, []string{"habit", "edit"},
		map[string]any{"positional": "Lesen", "title": "Lesen abends"})

	bag, _, _, err := d.habits(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(bag.Habits) != 1 {
		t.Fatalf("habits = %+v", bag.Habits)
	}
	h := bag.Habits[0]
	if h.Name != "Lesen abends" {
		t.Errorf("name = %q", h.Name)
	}
	if strings.Join(h.Days, ",") != "mon,wed" {
		t.Errorf("renaming changed the days to %v", h.Days)
	}
	if h.Color != "#123456" {
		t.Errorf("renaming changed the colour to %q", h.Color)
	}
	if len(h.Done) != 1 {
		t.Errorf("renaming lost what was already done: %v", h.Done)
	}

	// And the days can be changed on their own, leaving the new name alone.
	mustAsk(t, d, []string{"habit", "edit"},
		map[string]any{"positional": "Lesen abends", "days": "fri"})
	bag, _, _, _ = d.habits(context.Background(), false)
	if got := bag.Habits[0]; got.Name != "Lesen abends" || strings.Join(got.Days, ",") != "fri" {
		t.Errorf("habit = %+v", got)
	}
}

// Editing something that is not there must not create it: a typo would
// otherwise leave two habits and a streak split between them.
func TestHabitEditRefusesAnUnknownHabit(t *testing.T) {
	d, _, _ := seedTasks(t)
	mustAsk(t, d, []string{"habit", "add"}, map[string]any{"positional": "Lesen"})
	resp := ask(t, d, []string{"habit", "edit"},
		map[string]any{"positional": "Laufen", "title": "Joggen"})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

// "By Friday" and "by Friday at 17:00" are different promises. A bare date
// stays a date, and an hour somebody typed is kept rather than rounded off to
// midnight.
func TestATodoKeepsTheHourItWasGiven(t *testing.T) {
	d, f, _ := seedTasks(t)

	dated := addTodo(t, d, map[string]any{"positional": "Rechnung", "due": "2026-09-01"})
	if dated.Due != "2026-09-01" {
		t.Errorf("a bare date grew a time: %q", dated.Due)
	}
	timed := addTodo(t, d, map[string]any{"positional": "Abgabe", "due": "2026-09-01 17:00"})
	if timed.Due != "2026-09-01 17:00" {
		t.Errorf("the hour was dropped: %q", timed.Due)
	}
	if raw := todoNamed(t, f, "Rechnung"); !strings.Contains(raw, "DUE;VALUE=DATE:20260901") {
		t.Errorf("a date was stored with a clock on it:\n%s", raw)
	}
	if raw := todoNamed(t, f, "Abgabe"); strings.Contains(raw, "DUE;VALUE=DATE:") {
		t.Errorf("a deadline was stored as a date:\n%s", raw)
	}

	// And the words take an hour too, because that is how somebody says it.
	soon := addTodo(t, d, map[string]any{"positional": "Anruf", "due": "tomorrow 09:00"})
	if soon.Due != time.Now().AddDate(0, 0, 1).Format("2006-01-02")+" 09:00" {
		t.Errorf("due = %q", soon.Due)
	}
	if resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "add"},
		Args: map[string]any{"positional": "Irgendwann", "due": "sometime"}}); resp.OK ||
		!strings.Contains(resp.Error, "--due") {
		t.Errorf("resp = %+v", resp)
	}
}

// The three words are what a caller picks; 1, 5 and 9 are what iCalendar
// stores, and the list is ordered by them so that what matters is at the top of
// the day it is due.
func TestTodoPriorityIsWrittenAndSortsTheList(t *testing.T) {
	d, f, _ := seedTasks(t)
	for _, c := range []struct{ summary, priority string }{
		{"Später", "low"}, {"Dringend", "high"}, {"Normal", ""},
	} {
		added := addTodo(t, d, map[string]any{
			"positional": c.summary, "due": "2026-09-01", "priority": c.priority,
		})
		if added.Priority != c.priority {
			t.Errorf("%s came back as %q, want %q", c.summary, added.Priority, c.priority)
		}
	}
	if raw := todoNamed(t, f, "Dringend"); !strings.Contains(raw, "PRIORITY:1") {
		t.Errorf("high was not stored as 1:\n%s", raw)
	}
	if raw := todoNamed(t, f, "Später"); !strings.Contains(raw, "PRIORITY:9") {
		t.Errorf("low was not stored as 9:\n%s", raw)
	}
	// Nothing said is nothing written: an unranked todo is not a low one.
	if raw := todoNamed(t, f, "Normal"); strings.Contains(raw, "PRIORITY:") {
		t.Errorf("a priority nobody set was written anyway:\n%s", raw)
	}

	var order []string
	for _, row := range todos(t, d, nil) {
		order = append(order, row.Summary)
	}
	if strings.Join(order, ",") != "Dringend,Normal,Später" {
		t.Errorf("the list is in the order %v", order)
	}

	if resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"todo", "add"},
		Args: map[string]any{"positional": "Egal", "priority": "urgent"}}); resp.OK ||
		!strings.Contains(resp.Error, "--priority") {
		t.Errorf("resp = %+v", resp)
	}
}

// todoNamed finds one task on the list by what it says.
func todoNamed(t *testing.T, f *davsync.Fake, summary string) string {
	t.Helper()
	changes, err := f.Sync(context.Background(), testTaskURL, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes.Items {
		if !c.Deleted && strings.Contains(c.Data, "SUMMARY:"+summary) {
			return c.Data
		}
	}
	t.Fatalf("no todo called %q on the list", summary)
	return ""
}

// A todo due today is not late yet: a date runs out at the end of its day, and
// only a deadline with a clock on it runs out during one.
func TestOverdueIsMarkedOnlyOnceTheDeadlineHasPassed(t *testing.T) {
	d, _, _ := seedTasks(t)
	today := addTodo(t, d, map[string]any{"positional": "Heute", "due": "today"})
	if today.Overdue {
		t.Errorf("something due today was called overdue: %+v", today)
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if late := addTodo(t, d, map[string]any{"positional": "Gestern", "due": yesterday}); !late.Overdue {
		t.Errorf("yesterday's todo was not overdue: %+v", late)
	}
	past := time.Now().Add(-time.Hour).Format("2006-01-02 15:04")
	if late := addTodo(t, d, map[string]any{"positional": "Vorhin", "due": past}); !late.Overdue {
		t.Errorf("an hour ago was not overdue: %+v", late)
	}
}
