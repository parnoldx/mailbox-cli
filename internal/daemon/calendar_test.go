package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
)

// seedCalendar builds a Daemon whose Mirror holds one calendar: a dentist
// appointment tomorrow, a weekly standup with no end, and a holiday that lasts
// two days.
func seedCalendar(t *testing.T) (*Daemon, time.Time) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	id, err := m.PutCollection(mirror.Collection{
		Account: "primary", Kind: "events",
		URL: "https://dav.example.org/caldav/kalender/", Name: "Kalender", Color: "#3355ff",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Anchored on a Monday so the weekly rule has a predictable shape.
	monday := time.Date(2026, 8, 31, 0, 0, 0, 0, time.Local)
	r := &davsync.Reconciler{Account: "primary", Mirror: m, Location: time.Local}
	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	put := func(href, raw string) {
		t.Helper()
		col := mirror.Collection{ID: id, Kind: "events"}
		if err := tx.PutObject(objectOf(r, col, href, raw)); err != nil {
			t.Fatal(err)
		}
	}
	put("zahnarzt.ics", ics(`BEGIN:VEVENT
UID:zahnarzt@example.org
DTSTART;TZID=`+tzName(t)+`:20260901T100000
DTEND;TZID=`+tzName(t)+`:20260901T113000
SUMMARY:Zahnarzt
LOCATION:Hauptstraße 1
DESCRIPTION:Join https://meet.google.com/abc-defg-hij
END:VEVENT`))
	put("standup.ics", ics(`BEGIN:VEVENT
UID:standup@example.org
DTSTART;TZID=`+tzName(t)+`:20260831T093000
DURATION:PT15M
RRULE:FREQ=WEEKLY;BYDAY=MO
SUMMARY:Wochenstart
END:VEVENT`))
	put("urlaub.ics", ics(`BEGIN:VEVENT
UID:urlaub@example.org
DTSTART;VALUE=DATE:20260902
DTEND;VALUE=DATE:20260904
SUMMARY:Urlaub
END:VEVENT`))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	d := New("primary", m, nil, nil, nil, nil)
	return d, monday
}

// objectOf runs the reconciler's projection, so the test stores rows the same
// way a sync does rather than by hand.
func objectOf(r *davsync.Reconciler, c mirror.Collection, href, raw string) mirror.Object {
	return r.Project(c, davsync.Change{Href: href, ETag: `"1"`, Data: raw})
}

func tzName(t *testing.T) string {
	t.Helper()
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	return "Europe/Berlin"
}

func ics(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + body + "\r\nEND:VCALENDAR\r\n"
}

func agenda(t *testing.T, d *Daemon, args map[string]any) []occurrence {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"agenda"}, Args: args})
	if !resp.OK {
		t.Fatalf("agenda: %s (%s)", resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]occurrence)
	if !ok {
		t.Fatalf("agenda returned %T", resp.Data)
	}
	return out
}

func TestAgendaExpandsTheWindowItWasAskedFor(t *testing.T) {
	d, monday := seedCalendar(t)
	// Eight days, so the window reaches the next Monday: a rule with no end
	// keeps producing, and that is the thing worth asserting.
	got := agenda(t, d, map[string]any{"from": monday.Format("2006-01-02"), "days": float64(8)})

	var summaries []string
	for _, o := range got {
		summaries = append(summaries, o.Date+" "+o.Time+" "+o.Summary)
	}
	// Monday's standup, Tuesday's dentist, the two-day holiday, and the
	// following Monday's standup: a rule with no end keeps producing.
	want := []string{
		"2026-08-31 09:30–09:45 Wochenstart",
		"2026-09-01 10:00–11:30 Zahnarzt",
		"2026-09-02 all day Urlaub",
		"2026-09-07 09:30–09:45 Wochenstart",
	}
	if len(summaries) != len(want) {
		t.Fatalf("agenda = %v", summaries)
	}
	for i := range want {
		if summaries[i] != want[i] {
			t.Fatalf("agenda[%d] = %q, want %q", i, summaries[i], want[i])
		}
	}
	for _, o := range got {
		if o.Calendar != "Kalender" {
			t.Fatalf("entry has no calendar: %+v", o)
		}
		if o.Summary == "Wochenstart" && !o.Recurring {
			t.Fatalf("an instance of a rule says so: %+v", o)
		}
	}
}

// The agenda carries the notes beside every occurrence: a meeting link is
// usually written there, and a caller that scans free text for one needs the
// description without asking for each event whole.
func TestAgendaCarriesTheNotes(t *testing.T) {
	d, monday := seedCalendar(t)
	got := agenda(t, d, map[string]any{"from": monday.Format("2006-01-02"), "days": float64(1)})
	for _, o := range got {
		if o.Summary == "Zahnarzt" && o.Notes == "" {
			t.Fatalf("an event with notes answered without them: %+v", o)
		}
	}
}

func TestAgendaOfOneDayIsThatDay(t *testing.T) {
	d, monday := seedCalendar(t)
	got := agenda(t, d, map[string]any{"from": monday.Format("2006-01-02"), "days": float64(1)})
	if len(got) != 1 || got[0].Summary != "Wochenstart" {
		t.Fatalf("agenda = %+v", got)
	}
}

func TestAgendaCanBeAskedForOneCalendar(t *testing.T) {
	d, monday := seedCalendar(t)
	got := agenda(t, d, map[string]any{
		"from": monday.Format("2006-01-02"), "days": float64(8), "calendar": "kalender",
	})
	if len(got) != 4 {
		t.Fatalf("agenda = %+v", got)
	}
	// A calendar that does not exist is a mistake, not an empty week.
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"agenda"},
		Args: map[string]any{"calendar": "Arbeit"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestEventViewSaysWhenItHappensNext(t *testing.T) {
	d, monday := seedCalendar(t)
	got := agenda(t, d, map[string]any{"from": monday.Format("2006-01-02"), "days": float64(8)})
	var standup int64
	for _, o := range got {
		if o.Summary == "Wochenstart" {
			standup = o.ID
		}
	}
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"event", "view"},
		Args: map[string]any{"positional": standup}})
	if !resp.OK {
		t.Fatalf("event view: %s (%s)", resp.Error, resp.Code)
	}
	e := resp.Data.(event)
	if e.Summary != "Wochenstart" || !e.Recurring || e.Calendar != "Kalender" {
		t.Fatalf("event = %+v", e)
	}
	if len(e.Next) == 0 {
		t.Fatal("a repeating event that happens next week should say so")
	}

	// An id the Mirror does not hold is not_found, like a uid that was expunged.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"event", "view"},
		Args: map[string]any{"positional": "9999"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

// The habits calendar is listed like any other -- it is on the server and its
// count is worth seeing -- but it is marked, so nothing offers it as a place to
// put an appointment (ADR-0018).
func TestCalendarListMarksTheHabitsCalendarInternal(t *testing.T) {
	d, _ := seedCalendar(t)
	if _, err := d.Mirror.PutCollection(mirror.Collection{
		Account: "primary", Kind: "events",
		URL:  "https://dav.example.org/caldav/mailbox-habits/",
		Name: habit.CalendarName,
	}); err != nil {
		t.Fatal(err)
	}
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"calendar", "list"}})
	if !resp.OK {
		t.Fatalf("calendar list: %s", resp.Error)
	}
	rows := resp.Data.([]calendar)
	seen := map[string]calendar{}
	for _, r := range rows {
		seen[r.Name] = r
	}
	if !seen[habit.CalendarName].Internal {
		t.Fatalf("habits calendar not marked internal: %+v", rows)
	}
	if seen["Kalender"].Internal {
		t.Fatalf("an ordinary calendar came back internal: %+v", rows)
	}
}

func TestCalendarListNamesWhatIsMirrored(t *testing.T) {
	d, _ := seedCalendar(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"calendar", "list"}})
	if !resp.OK {
		t.Fatalf("calendar list: %s", resp.Error)
	}
	rows := resp.Data.([]calendar)
	if len(rows) != 1 || rows[0].Name != "Kalender" || rows[0].Count != 3 || rows[0].Kind != "events" {
		t.Fatalf("calendars = %+v", rows)
	}
}
