package vcal

import (
	"strings"
	"testing"
	"time"
)

// berlin is the timezone this account lives in, and the one whose DST changes
// make a weekly meeting land at a different UTC hour half the year.
var berlin = mustLoad("Europe/Berlin")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func wrap(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + body + "END:VCALENDAR\r\n"
}

const timed = `BEGIN:VEVENT
UID:timed@example.org
DTSTART;TZID=Europe/Berlin:20260829T100000
DTEND;TZID=Europe/Berlin:20260829T113000
SUMMARY:Zahnarzt
LOCATION:Hauptstraße 1
END:VEVENT
`

func TestParseATimedEvent(t *testing.T) {
	p, err := Parse(wrap(timed), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindEvent || p.UID != "timed@example.org" || p.Summary != "Zahnarzt" {
		t.Fatalf("projection = %+v", p)
	}
	if p.AllDay {
		t.Fatal("a timed event is not all day")
	}
	want := time.Date(2026, 8, 29, 10, 0, 0, 0, berlin)
	if !p.Start.Equal(want) {
		t.Fatalf("start = %s, want %s", p.Start, want)
	}
	if p.End.Sub(p.Start) != 90*time.Minute {
		t.Fatalf("duration = %s", p.End.Sub(p.Start))
	}
	if p.Recurring {
		t.Fatal("no rule, no recurrence")
	}
}

func TestParseAnAllDayEvent(t *testing.T) {
	p, err := Parse(wrap(`BEGIN:VEVENT
UID:allday@example.org
DTSTART;VALUE=DATE:20260829
DTEND;VALUE=DATE:20260831
SUMMARY:Urlaub
END:VEVENT
`), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if !p.AllDay {
		t.Fatal("a DATE value is an all-day entry, which is a different thing from midnight")
	}
	if p.Start.Hour() != 0 || p.End.Sub(p.Start) != 48*time.Hour {
		t.Fatalf("%s .. %s", p.Start, p.End)
	}
}

func TestADurationStandsInForAnEnd(t *testing.T) {
	p, err := Parse(wrap(`BEGIN:VEVENT
UID:dur@example.org
DTSTART;TZID=Europe/Berlin:20260829T090000
DURATION:PT45M
SUMMARY:Standup
END:VEVENT
`), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.End.Sub(p.Start) != 45*time.Minute {
		t.Fatalf("duration = %s", p.End.Sub(p.Start))
	}
}

func TestOccurrencesOfAPlainEvent(t *testing.T) {
	from := time.Date(2026, 8, 29, 0, 0, 0, 0, berlin)
	got, err := Occurrences(wrap(timed), from, from.AddDate(0, 0, 1), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Summary != "Zahnarzt" || got[0].Recurring {
		t.Fatalf("occurrences = %+v", got)
	}

	// The day before holds nothing, and neither does the day after.
	for _, day := range []time.Time{from.AddDate(0, 0, -1), from.AddDate(0, 0, 1)} {
		got, err := Occurrences(wrap(timed), day, day.AddDate(0, 0, 1), berlin)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("%s holds %+v", day.Format("2006-01-02"), got)
		}
	}
}

const weekly = `BEGIN:VEVENT
UID:weekly@example.org
DTSTART;TZID=Europe/Berlin:20260302T093000
DTEND;TZID=Europe/Berlin:20260302T100000
RRULE:FREQ=WEEKLY;BYDAY=MO
SUMMARY:Wochenstart
END:VEVENT
`

func TestARepeatingEventKeepsItsLocalTimeAcrossDST(t *testing.T) {
	// Berlin leaves summer time on 25 October 2026. A weekly 09:30 meeting is
	// at 09:30 local on both sides of that, which is two different hours in
	// UTC — expanding in UTC is how a standup drifts by an hour every autumn.
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, berlin)
	to := time.Date(2026, 10, 27, 0, 0, 0, 0, berlin)
	got, err := Occurrences(wrap(weekly), from, to, berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d occurrences, want 2: %+v", len(got), got)
	}
	for _, o := range got {
		if h, m, _ := o.Start.In(berlin).Clock(); h != 9 || m != 30 {
			t.Fatalf("%s is at %02d:%02d local", o.Start.Format(time.RFC3339), h, m)
		}
		if !o.Recurring {
			t.Fatal("an instance of a rule is recurring")
		}
	}
	if got[0].Start.UTC().Hour() == got[1].Start.UTC().Hour() {
		t.Fatal("this test is meant to straddle the DST change and does not")
	}
}

func TestAnExcludedDateDoesNotHappen(t *testing.T) {
	raw := wrap(`BEGIN:VEVENT
UID:weekly@example.org
DTSTART;TZID=Europe/Berlin:20260302T093000
DTEND;TZID=Europe/Berlin:20260302T100000
RRULE:FREQ=WEEKLY;BYDAY=MO
EXDATE;TZID=Europe/Berlin:20260309T093000
SUMMARY:Wochenstart
END:VEVENT
`)
	from := time.Date(2026, 3, 2, 0, 0, 0, 0, berlin)
	got, err := Occurrences(raw, from, from.AddDate(0, 0, 21), berlin)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range got {
		if o.Start.Format("2006-01-02") == "2026-03-09" {
			t.Fatalf("the excluded date happened anyway: %+v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("%d occurrences, want 2: %+v", len(got), got)
	}
}

func TestAnOverriddenInstanceMovesAndKeepsItsPlace(t *testing.T) {
	raw := wrap(`BEGIN:VEVENT
UID:weekly@example.org
DTSTART;TZID=Europe/Berlin:20260302T093000
DTEND;TZID=Europe/Berlin:20260302T100000
RRULE:FREQ=WEEKLY;BYDAY=MO
SUMMARY:Wochenstart
END:VEVENT
BEGIN:VEVENT
UID:weekly@example.org
RECURRENCE-ID;TZID=Europe/Berlin:20260309T093000
DTSTART;TZID=Europe/Berlin:20260309T140000
DTEND;TZID=Europe/Berlin:20260309T150000
SUMMARY:Wochenstart (verschoben)
END:VEVENT
BEGIN:VEVENT
UID:weekly@example.org
RECURRENCE-ID;TZID=Europe/Berlin:20260316T093000
DTSTART;TZID=Europe/Berlin:20260316T093000
STATUS:CANCELLED
SUMMARY:Wochenstart
END:VEVENT
`)
	from := time.Date(2026, 3, 2, 0, 0, 0, 0, berlin)
	got, err := Occurrences(raw, from, from.AddDate(0, 0, 21), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d occurrences, want 2 (one moved, one cancelled): %+v", len(got), got)
	}
	var moved bool
	for _, o := range got {
		if o.Start.Format("2006-01-02") == "2026-03-16" {
			t.Fatal("a cancelled instance is not an instance")
		}
		if o.Start.Format("2006-01-02 15:04") == "2026-03-09 14:00" {
			moved = true
			if o.Summary != "Wochenstart (verschoben)" {
				t.Fatalf("the override's summary was not used: %q", o.Summary)
			}
		}
	}
	if !moved {
		t.Fatalf("the moved instance is not at its new time: %+v", got)
	}
}

func TestARuleThatEndsSaysWhen(t *testing.T) {
	p, err := Parse(wrap(`BEGIN:VEVENT
UID:count@example.org
DTSTART;TZID=Europe/Berlin:20260302T093000
DTEND;TZID=Europe/Berlin:20260302T100000
RRULE:FREQ=WEEKLY;COUNT=3
SUMMARY:Dreimal
END:VEVENT
`), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Recurring {
		t.Fatal("a rule is a recurrence")
	}
	// A window after the last one need not expand it at all.
	if p.RepeatsUntil.Format("2006-01-02") != "2026-03-16" {
		t.Fatalf("repeats until %s", p.RepeatsUntil)
	}

	forever, err := Parse(wrap(weekly), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if !forever.RepeatsUntil.IsZero() {
		t.Fatalf("a rule with no end has no last date, got %s", forever.RepeatsUntil)
	}
}

func TestAnUnknownTimezoneIsReadRatherThanDropped(t *testing.T) {
	// Outlook writes TZIDs that are not IANA names. Losing the entry is worse
	// than reading it in the calendar's own timezone.
	p, err := Parse(wrap(`BEGIN:VEVENT
UID:outlook@example.org
DTSTART;TZID="W. Europe Standard Time":20260829T100000
DTEND;TZID="W. Europe Standard Time":20260829T110000
SUMMARY:Telefonat
END:VEVENT
`), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.Start.IsZero() || p.Summary != "Telefonat" {
		t.Fatalf("projection = %+v", p)
	}
	if h := p.Start.In(berlin).Hour(); h != 10 {
		t.Fatalf("start hour = %d", h)
	}
}

func TestATodoIsProjectedByItsDueDate(t *testing.T) {
	p, err := Parse(wrap(`BEGIN:VTODO
UID:todo@example.org
DUE;VALUE=DATE:20260901
SUMMARY:Rechnung bezahlen
STATUS:NEEDS-ACTION
END:VTODO
`), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindTodo || p.Due.IsZero() || p.Status != "NEEDS-ACTION" {
		t.Fatalf("projection = %+v", p)
	}
	// A Todo is not an Event and never appears in an agenda expansion.
	got, err := Occurrences(wrap(`BEGIN:VTODO
UID:todo@example.org
DUE;VALUE=DATE:20260901
SUMMARY:Rechnung bezahlen
END:VTODO
`), p.Due.AddDate(0, 0, -1), p.Due.AddDate(0, 0, 1), berlin)
	if err != nil || len(got) != 0 {
		t.Fatalf("todos are not occurrences: %+v (%v)", got, err)
	}
}

// What another client wrote is what has to be read: a reminder in seconds, one
// hung off the end rather than the start, and one set for an absolute time are
// all valid, and only the first of them is a number of minutes before the
// start.
func TestParseReadsALinkARuleAndTheRemindersOffAForeignEvent(t *testing.T) {
	const foreign = `BEGIN:VEVENT
UID:foreign@example.org
DTSTART;TZID=Europe/Berlin:20260901T090000
DTEND;TZID=Europe/Berlin:20260901T093000
SUMMARY:Standup
URL:https://meet.example.org/r?a=1,2
RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT900S
DESCRIPTION:Standup
END:VALARM
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER;RELATED=END:-PT5M
DESCRIPTION:Standup
END:VALARM
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER;VALUE=DATE-TIME:20260901T080000Z
DESCRIPTION:Standup
END:VALARM
END:VEVENT
`
	p, err := Parse(wrap(foreign), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.URL != "https://meet.example.org/r?a=1,2" {
		t.Errorf("url = %q", p.URL)
	}
	if !p.Recurring || p.Repeat != "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" {
		t.Errorf("repeat = %q, recurring = %v", p.Repeat, p.Recurring)
	}
	if len(p.Alarms) != 1 || p.Alarms[0] != 15 {
		t.Errorf("alarms = %v, want the one that is 15 minutes before the start", p.Alarms)
	}
}

// A Todo somebody else wrote carries its deadline and its rank the same way,
// and a date without a clock stays a date.
func TestParseReadsATodosDeadlineAndRank(t *testing.T) {
	const dated = `BEGIN:VTODO
UID:dated@example.org
DUE;VALUE=DATE:20260901
PRIORITY:2
SUMMARY:Rechnung
END:VTODO
`
	p, err := Parse(wrap(dated), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if !p.DueAllDay || p.Due.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("due = %v, all day = %v", p.Due, p.DueAllDay)
	}
	// Two is not one, and it is still high: the format has nine levels and
	// every client that shows them shows three.
	if p.Priority != 2 || PriorityWord(p.Priority) != "high" {
		t.Errorf("priority = %d (%q)", p.Priority, PriorityWord(p.Priority))
	}

	const timedDue = `BEGIN:VTODO
UID:timed@example.org
DUE;TZID=Europe/Berlin:20260901T170000
SUMMARY:Abgabe
END:VTODO
`
	p, err = Parse(wrap(timedDue), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if p.DueAllDay || p.Due.In(berlin).Format("15:04") != "17:00" {
		t.Errorf("due = %v, all day = %v", p.Due, p.DueAllDay)
	}
	if p.Priority != 0 || PriorityWord(p.Priority) != "" {
		t.Errorf("a todo nobody ranked came back as %d", p.Priority)
	}
}

// Rule takes the words people pick and the rules a picker produces, and refuses
// what no calendar would accept.
func TestRuleReadsWordsAndRules(t *testing.T) {
	for spec, want := range map[string]string{
		"":                                    "",
		"weekdays":                            "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
		"Daily":                               "FREQ=DAILY",
		"none":                                RepeatNone,
		"FREQ=MONTHLY;UNTIL=20261231T000000Z": "FREQ=MONTHLY;UNTIL=20261231T000000Z",
		"rrule:freq=yearly":                   "FREQ=YEARLY",
	} {
		got, err := Rule(spec)
		if err != nil {
			t.Errorf("Rule(%q): %v", spec, err)
			continue
		}
		if got != want {
			t.Errorf("Rule(%q) = %q, want %q", spec, got, want)
		}
	}
	if _, err := Rule("every other thursday"); err == nil {
		t.Errorf("a rule nobody could act on was accepted")
	}
}

// A repeating Monday 09:00 standup, written the way the daemon writes one:
// instants in UTC, matched against the local dates a caller types.
func weeklyRaw(t *testing.T) string {
	t.Helper()
	raw, err := NewEvent("standup-mailbox", EventEdit{
		Summary: "Standup",
		Start:   mondayAt(9, 0),
		End:     mondayAt(9, 30),
		Repeat:  "FREQ=WEEKLY;BYDAY=MO",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mondayAt is next Monday at hour:minute local time, which is where the
// standup sits.
func mondayAt(hour, minute int) time.Time {
	day := time.Now()
	for day.Weekday() != time.Monday {
		day = day.AddDate(0, 0, 1)
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, minute, 0, 0, time.Local)
}

func TestSetOccurrenceMovesOneInstance(t *testing.T) {
	raw := weeklyRaw(t)
	first, second := mondayAt(9, 0), mondayAt(9, 0).AddDate(0, 0, 7)
	moved := time.Date(second.Year(), second.Month(), second.Day(), 14, 0, 0, 0, time.Local)

	edited, err := SetOccurrence(raw, second, true, EventEdit{
		Summary: "Standup verschoben",
		Start:   moved,
		End:     moved.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(edited, "RECURRENCE-ID"); n != 1 {
		t.Fatalf("%d overrides written:\n%s", n, edited)
	}
	if strings.Count(edited, "RRULE:") != 1 {
		t.Errorf("the rule did not survive, or landed on the override:\n%s", edited)
	}

	got, err := Occurrences(edited, mondayAt(0, 0), mondayAt(0, 0).AddDate(0, 0, 22), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d occurrences, want 4: %+v", len(got), got)
	}
	for _, o := range got {
		switch day := o.Start.Format("2006-01-02"); day {
		case moved.Format("2006-01-02"):
			if o.Start.Format("15:04") != "14:00" || o.Summary != "Standup verschoben" {
				t.Errorf("the moved instance is %+v", o)
			}
		case second.Format("2006-01-02"):
			t.Errorf("the old slot still holds an instance: %+v", got)
		case first.Format("2006-01-02"):
			if o.Summary != "Standup" || o.Start.Format("15:04") != "09:00" {
				t.Errorf("an untouched instance was changed: %+v", o)
			}
		}
	}
}

func TestSetOccurrenceEditsTheOverrideItAlreadyWrote(t *testing.T) {
	raw := weeklyRaw(t)
	day := mondayAt(9, 0).AddDate(0, 0, 7)

	once, err := SetOccurrence(raw, day, true, EventEdit{
		Summary: "Erst verschoben",
		Start:   mondayAt(14, 0).AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatal(err)
	}
	twice, err := SetOccurrence(once, day, true, EventEdit{Summary: "Dann umbenannt"})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(twice, "RECURRENCE-ID"); n != 1 {
		t.Fatalf("%d overrides after two edits:\n%s", n, twice)
	}
	if !strings.Contains(twice, "Dann umbenannt") {
		t.Errorf("the second edit did not land:\n%s", twice)
	}
}

func TestCancelOccurrenceTakesOneInstanceOff(t *testing.T) {
	raw := weeklyRaw(t)
	second := mondayAt(9, 0).AddDate(0, 0, 7)

	cancelled, err := CancelOccurrence(raw, second, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cancelled, "STATUS:CANCELLED") {
		t.Errorf("the override carries no cancellation:\n%s", cancelled)
	}
	got, err := Occurrences(cancelled, mondayAt(0, 0), mondayAt(0, 0).AddDate(0, 0, 22), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range got {
		if o.Start.Format("2006-01-02") == second.Format("2006-01-02") {
			t.Errorf("the cancelled instance happened anyway: %+v", got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("%d occurrences, want 3: %+v", len(got), got)
	}
}

func TestSetOccurrenceRefusesWhatItCannotMean(t *testing.T) {
	raw := weeklyRaw(t)
	day := mondayAt(9, 0)

	// A day with no instance on it.
	if _, err := SetOccurrence(raw, day.AddDate(0, 0, 2), true, EventEdit{Summary: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no instance") {
		t.Errorf("an empty day was accepted: %v", err)
	}
	// A clock that names no instance is refused, even when the day has one.
	if _, err := SetOccurrence(raw, day.Add(3*time.Hour+33*time.Minute), false, EventEdit{Summary: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no instance at 12:33") {
		t.Errorf("a wrong clock picked the day's instance anyway: %v", err)
	}
	// An event that does not repeat.
	plain, err := NewEvent("once-mailbox", EventEdit{Summary: "Einmal", Start: day.Add(10 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetOccurrence(plain, day, true, EventEdit{Summary: "x"}); err == nil ||
		!strings.Contains(err.Error(), "does not repeat") {
		t.Errorf("a plain event was overridden: %v", err)
	}
}

// A rename-only override keeps its own slot: the master's start is the first
// instance's, and copying it wholesale would silently move this instance to
// the head of the series.
func TestSetOccurrenceKeepsItsOwnSlotAndLength(t *testing.T) {
	raw := weeklyRaw(t)
	second := mondayAt(9, 0).AddDate(0, 0, 7)

	edited, err := SetOccurrence(raw, second, true, EventEdit{Summary: "Nur umbenannt"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Occurrences(edited, mondayAt(0, 0), mondayAt(0, 0).AddDate(0, 0, 15), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	var renamed bool
	for _, o := range got {
		if o.Summary == "Nur umbenannt" {
			renamed = true
			if !o.Start.Equal(second) || !o.End.Equal(second.Add(30*time.Minute)) {
				t.Errorf("the renamed instance moved or changed length: %+v", o)
			}
		}
	}
	if !renamed {
		t.Errorf("the renamed instance is not in the expansion: %+v", got)
	}
}

// Multi-valued properties keep every value on the override.
func TestSetOccurrenceKeepsEveryAttendee(t *testing.T) {
	raw := wrap(`BEGIN:VEVENT
UID:many@example.org
DTSTAMP:20260901T000000Z
DTSTART:20261005T090000Z
DTEND:20261005T100000Z
RRULE:FREQ=WEEKLY;BYDAY=MO
SUMMARY:Standup
ATTENDEE:mailto:anna@example.com
ATTENDEE:mailto:bert@example.com
END:VEVENT
`)
	day := time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local)
	edited, err := SetOccurrence(raw, day, true, EventEdit{Summary: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// The master keeps its two, and the override must carry both of its own.
	if n := strings.Count(edited, "ATTENDEE"); n != 4 {
		t.Errorf("%d attendees on the object, want 2 on each component:\n%s", n, edited)
	}
}

// The machine's own zone is written floating, not as TZID=Local — a name no
// other client knows.
func TestALocalEventIsWrittenFloating(t *testing.T) {
	raw := weeklyRaw(t)
	if strings.Contains(raw, "TZID=Local") {
		t.Errorf("the local zone was written as a TZID:\n%s", raw)
	}
	if !strings.Contains(raw, "DTSTART:") {
		t.Errorf("the start is not a plain date-time:\n%s", raw)
	}
}

// On the day the clocks change, an all-day override keeps its whole day:
// 24 clock hours from local midnight land back inside it.
func TestAnAllDayOverrideKeepsItsDayAcrossTheClockChange(t *testing.T) {
	// 2026-10-25 is the Sunday this account's zone (Europe/Berlin) leaves
	// summer time; the 23-hour day is where a clock-hour length went wrong.
	raw, err := NewEvent("urlaub-mailbox", EventEdit{
		Summary: "Urlaub",
		Start:   time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local),
		AllDay:  true,
		Repeat:  "FREQ=DAILY",
	})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := SetOccurrence(raw, time.Date(2026, 10, 25, 0, 0, 0, 0, time.Local), true,
		EventEdit{Summary: "Urlaub (gekippt)"})
	if err != nil {
		t.Fatal(err)
	}
	// The master's dates are the 20th and the 21st, so these can only be the
	// override's.
	if !strings.Contains(edited, "DTSTART;VALUE=DATE:20261025") ||
		!strings.Contains(edited, "DTEND;VALUE=DATE:20261026") {
		t.Errorf("the all-day override lost its day:\n%s", edited)
	}
}

// A floating master gets a floating RECURRENCE-ID: RFC 5545 expects the two
// in the same form, and other clients match them as written.
func TestAFloatingMasterGetsAFloatingRecurrenceID(t *testing.T) {
	raw, err := NewEvent("float-mailbox", EventEdit{
		Summary: "Standup",
		Start:   mondayAt(9, 0),
		End:     mondayAt(9, 30),
		Repeat:  "FREQ=DAILY",
	})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := SetOccurrence(raw, mondayAt(9, 0).AddDate(0, 0, 7), true, EventEdit{Summary: "x"})
	if err != nil {
		t.Fatal(err)
	}
	id := indexLine(edited, "RECURRENCE-ID:")
	if id == "" {
		t.Fatal("no RECURRENCE-ID was written:\n" + edited)
	}
	if strings.HasSuffix(id, "Z") {
		t.Errorf("the override's RECURRENCE-ID is UTC over a floating master: %s", id)
	}
}

// indexLine returns the first line of raw that starts with name.
func indexLine(raw, name string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, name) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// A Teams meeting rides the object as a marker property, because that is how
// the Graph writer knows to ask for an online meeting — and there is no off to
// write, since Graph ignores changes to it once the meeting exists.
func TestNewEventAndSetEventWriteTheTeamsMarkerAndTheAttendees(t *testing.T) {
	raw, err := NewEvent("meet-mailbox", EventEdit{
		Summary: "Review", Start: time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local),
		Teams:  true,
		Invite: []string{"Anna@example.com", "bert@example.de"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if line := indexLine(raw, "X-MAILBOX-TEAMS:"); line != "X-MAILBOX-TEAMS:TRUE" {
		t.Errorf("the marker is %q:\n%s", line, raw)
	}
	if !strings.Contains(raw, "ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:anna@example.com") ||
		!strings.Contains(raw, "ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:bert@example.de") {
		t.Errorf("the invitees are not on the object:\n%s", raw)
	}

	// An edit adds, and a second invite of somebody already there — even under
	// another case — does not put them on twice.
	edited, err := SetEvent(raw, EventEdit{Invite: []string{"ANNA@example.com"}, Uninvite: []string{"BERT@example.de"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(edited, "ATTENDEE"); n != 1 {
		t.Errorf("%d attendees after the edit, want 1:\n%s", n, edited)
	}
	if !strings.Contains(edited, "mailto:anna@example.com") {
		t.Errorf("anna did not survive the edit:\n%s", edited)
	}
}

// Parse reads both back, so a caller can show who is on an event and whether
// it is a Teams meeting without the raw.
func TestParseReadsTeamsAndAttendees(t *testing.T) {
	raw := wrap(`BEGIN:VEVENT
UID:meet@example.org
DTSTAMP:20260901T000000Z
DTSTART:20260901T100000Z
DTEND:20260901T110000Z
SUMMARY:Review
X-MAILBOX-TEAMS:TRUE
ATTENDEE;CN=Anna Beispiel;PARTSTAT=ACCEPTED:mailto:anna@example.com
ATTENDEE;PARTSTAT=DECLINED:mailto:BERT@example.de
END:VEVENT
`)
	p, err := Parse(raw, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Teams {
		t.Errorf("the marker did not come back: %+v", p)
	}
	want := []Attendee{
		{Address: "anna@example.com", Name: "Anna Beispiel", Partstat: "ACCEPTED"},
		{Address: "bert@example.de", Partstat: "DECLINED"},
	}
	if len(p.Attendees) != 2 || p.Attendees[0] != want[0] || p.Attendees[1] != want[1] {
		t.Errorf("attendees = %+v, want %+v", p.Attendees, want)
	}
}
