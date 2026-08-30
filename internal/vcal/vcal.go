// Package vcal reads iCalendar. The raw object is the record and everything
// here is a projection of it (ADR-0010): nothing in this package is stored
// except beside the bytes it was derived from, so a projection that turns out
// to be wrong is a bug that one resync fixes.
package vcal

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// Kind says which component this object is. One href holds one of them.
type Kind string

const (
	KindEvent Kind = "event"
	KindTodo  Kind = "todo"
	KindOther Kind = "other"
)

// Projection is what the Mirror stores beside the raw object so that listing,
// searching and windowing do not have to parse iCalendar. It is derived, never
// authoritative.
type Projection struct {
	Kind        Kind
	UID         string
	Summary     string
	Location    string
	Description string
	Status      string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Recurring   bool
	// Repeat is the recurrence rule as it is written, for a caller that wants
	// to show it back or change it. Recurring says whether there is one.
	Repeat string
	// Alarms is the reminders, each so many minutes before the start. A
	// reminder set for an absolute time is not one of these and is left out.
	Alarms []int
	// RepeatsUntil is the last instant an occurrence can start. It is zero for
	// a rule with no end, which is the case a window query has to treat as
	// "always possibly relevant".
	RepeatsUntil time.Time
	// Due, DueAllDay, Priority and Completed belong to a Todo and are zero on
	// an Event. DueAllDay says the due date has no clock on it: "by Friday" is
	// a different thing from "by Friday at 00:00", and only one of them is
	// worth showing a time for.
	Due       time.Time
	DueAllDay bool
	Priority  int
	Completed time.Time
}

// Parse projects one raw object. A component we do not understand is still
// stored — the raw is the record — so this reports what it can rather than
// failing.
func Parse(raw string, loc *time.Location) (Projection, error) {
	cal, err := decode(raw)
	if err != nil {
		return Projection{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	master, kind := primary(cal)
	if master == nil {
		return Projection{Kind: KindOther}, nil
	}

	p := Projection{Kind: kind}
	p.UID, _ = master.Props.Text(ical.PropUID)
	p.Summary, _ = master.Props.Text(ical.PropSummary)
	p.Location, _ = master.Props.Text(ical.PropLocation)
	p.Description, _ = master.Props.Text(ical.PropDescription)
	if status, err := master.Props.Text(ical.PropStatus); err == nil {
		p.Status = strings.ToUpper(status)
	}
	if prop := master.Props.Get(ical.PropURL); prop != nil {
		p.URL = prop.Value
	}
	p.Start, p.AllDay = timeOf(master, ical.PropDateTimeStart, loc)
	p.End, _ = endOf(master, p.Start, p.AllDay, loc)
	p.Alarms = alarmMinutes(master)
	if kind == KindTodo {
		p.Due, p.DueAllDay = timeOf(master, ical.PropDue, loc)
		p.Priority = priorityOf(master)
		p.Completed, _ = timeOf(master, "COMPLETED", loc)
		if p.Start.IsZero() {
			// An undated Todo is the ordinary kind; its due date is the only
			// time it has.
			p.Start = p.Due
		}
	}
	if rule := master.Props.Get(ical.PropRecurrenceRule); rule != nil {
		p.Recurring = true
		p.Repeat = rule.Value
		p.RepeatsUntil = lastStart(master, loc)
	}
	return p, nil
}

// Occurrence is one instance of an object in a window: a single event, or one
// repeat of a repeating one, with any override for that date already applied.
type Occurrence struct {
	UID      string
	Summary  string
	Location string
	Status   string
	// URL is the link on the entry — the call, the ticket, the page. It is on
	// every instance because it belongs to the object the rule came from.
	URL    string
	Start  time.Time
	End    time.Time
	AllDay bool
	// Recurring says this instance came from a rule rather than from a plain
	// dated event.
	Recurring bool
}

// Occurrences expands one raw object into the instances that fall in
// [from, to). Expansion happens on read rather than being stored, because a
// rule with no end has no finite expansion to store, and because a window is a
// property of the question, not of the calendar.
func Occurrences(raw string, from, to time.Time, loc *time.Location) ([]Occurrence, error) {
	cal, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.Local
	}
	master, kind := primary(cal)
	if master == nil || kind != KindEvent {
		return nil, nil
	}
	start, allDay := timeOf(master, ical.PropDateTimeStart, loc)
	if start.IsZero() {
		return nil, nil
	}
	end, _ := endOf(master, start, allDay, loc)
	duration := end.Sub(start)
	if duration < 0 {
		duration = 0
	}
	base := Occurrence{AllDay: allDay}
	base.UID, _ = master.Props.Text(ical.PropUID)
	base.Summary, _ = master.Props.Text(ical.PropSummary)
	base.Location, _ = master.Props.Text(ical.PropLocation)
	if prop := master.Props.Get(ical.PropURL); prop != nil {
		base.URL = prop.Value
	}
	if status, err := master.Props.Text(ical.PropStatus); err == nil {
		base.Status = strings.ToUpper(status)
	}

	overrides := overridesOf(cal, loc)

	set, err := recurrenceSet(master, loc)
	if err != nil {
		return nil, err
	}
	var out []Occurrence
	if set == nil {
		// A plain event: one instance, and an override cannot apply to it.
		if overlaps(start, start.Add(duration), from, to) {
			o := base
			o.Start, o.End = start, start.Add(duration)
			out = append(out, o)
		}
	} else {
		// An instance that began before the window may still be running in it,
		// so the expansion starts one duration early.
		for _, at := range set.Between(from.Add(-duration-time.Second), to, true) {
			o := base
			o.Recurring = true
			o.Start, o.End = at, at.Add(duration)
			if ov, ok := overrides[at.UTC()]; ok {
				if ov.cancelled {
					continue
				}
				o.Start, o.End, o.Summary, o.Location = ov.start, ov.end, ov.summary, ov.location
				delete(overrides, at.UTC())
			}
			if overlaps(o.Start, o.End, from, to) {
				out = append(out, o)
			}
		}
	}

	// An override that moved an instance into the window is in the window, even
	// though the rule it came from never lands there.
	for _, ov := range overrides {
		if ov.cancelled || !overlaps(ov.start, ov.end, from, to) {
			continue
		}
		o := base
		o.Recurring = true
		o.Start, o.End, o.Summary, o.Location = ov.start, ov.end, ov.summary, ov.location
		out = append(out, o)
	}
	return out, nil
}

// override is a VEVENT that replaces one instance of a repeating one.
type override struct {
	start, end        time.Time
	summary, location string
	cancelled         bool
}

func overridesOf(cal *ical.Calendar, loc *time.Location) map[time.Time]override {
	out := map[time.Time]override{}
	for _, child := range cal.Children {
		if child.Name != ical.CompEvent {
			continue
		}
		prop := child.Props.Get("RECURRENCE-ID")
		if prop == nil {
			continue
		}
		at, err := prop.DateTime(loc)
		if err != nil {
			continue
		}
		start, allDay := timeOf(child, ical.PropDateTimeStart, loc)
		end, _ := endOf(child, start, allDay, loc)
		ov := override{start: start, end: end}
		ov.summary, _ = child.Props.Text(ical.PropSummary)
		ov.location, _ = child.Props.Text(ical.PropLocation)
		if status, err := child.Props.Text(ical.PropStatus); err == nil {
			ov.cancelled = strings.EqualFold(status, "CANCELLED")
		}
		out[at.UTC()] = ov
	}
	return out
}

// primary picks the component this object is about: the VEVENT or VTODO that
// is not an override of another one.
func primary(cal *ical.Calendar) (*ical.Component, Kind) {
	var fallback *ical.Component
	var fallbackKind Kind
	for _, child := range cal.Children {
		var kind Kind
		switch child.Name {
		case ical.CompEvent:
			kind = KindEvent
		case ical.CompToDo:
			kind = KindTodo
		default:
			continue
		}
		if child.Props.Get("RECURRENCE-ID") == nil {
			return child, kind
		}
		if fallback == nil {
			fallback, fallbackKind = child, kind
		}
	}
	if fallback != nil {
		// Only overrides: the master is on the server somewhere else, and this
		// is still an object worth listing.
		return fallback, fallbackKind
	}
	return nil, KindOther
}

// recurrenceSet builds the rule set, tolerating a rule we cannot parse: an
// object with a broken RRULE is better listed once than dropped.
func recurrenceSet(comp *ical.Component, loc *time.Location) (*rrule.Set, error) {
	if comp.Props.Get(ical.PropRecurrenceRule) == nil {
		return nil, nil
	}
	set, err := comp.RecurrenceSet(loc)
	if err != nil {
		return nil, fmt.Errorf("recurrence: %w", err)
	}
	return set, nil
}

// lastStart is the last instant an occurrence can begin, or zero when the rule
// never ends. A COUNT is expanded because it is finite; a rule with neither
// UNTIL nor COUNT is the "zero means forever" case.
func lastStart(comp *ical.Component, loc *time.Location) time.Time {
	opt, err := comp.Props.RecurrenceRule()
	if err != nil || opt == nil {
		return time.Time{}
	}
	if !opt.Until.IsZero() {
		return opt.Until
	}
	if opt.Count <= 0 {
		return time.Time{}
	}
	set, err := comp.RecurrenceSet(loc)
	if err != nil || set == nil {
		return time.Time{}
	}
	all := set.All()
	if len(all) == 0 {
		return time.Time{}
	}
	return all[len(all)-1]
}

// priorityOf reads PRIORITY. A value the format does not define is read as
// nothing said rather than guessed at.
func priorityOf(comp *ical.Component) int {
	prop := comp.Props.Get(ical.PropPriority)
	if prop == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(prop.Value))
	if err != nil || n < 0 || n > 9 {
		return 0
	}
	return n
}

// alarmMinutes reads the reminders as the minutes before the start they fire.
// A trigger set to an absolute time, or hung off the end rather than the start,
// is not a number of minutes before and is left out: it is still in the raw,
// which is the record.
func alarmMinutes(comp *ical.Component) []int {
	var out []int
	for _, child := range comp.Children {
		if child.Name != ical.CompAlarm {
			continue
		}
		prop := child.Props.Get(ical.PropTrigger)
		if prop == nil || strings.EqualFold(prop.Params.Get("RELATED"), "END") {
			continue
		}
		dur, err := prop.Duration()
		if err != nil {
			continue
		}
		out = append(out, int(-dur/time.Minute))
	}
	return out
}

// timeOf reads a date-time property, and says whether it was a date — an
// all-day entry is a different thing from one that starts at midnight.
func timeOf(comp *ical.Component, name string, loc *time.Location) (time.Time, bool) {
	prop := comp.Props.Get(name)
	if prop == nil {
		return time.Time{}, false
	}
	allDay := prop.ValueType() == ical.ValueDate ||
		(prop.ValueType() == ical.ValueDefault && len(prop.Value) == 8)
	t, err := prop.DateTime(loc)
	if err != nil {
		// A TZID the system does not know — Outlook writes "W. Europe Standard
		// Time" — is not a reason to lose the entry. Read it as local time.
		bare := *prop
		bare.Params = ical.Params{}
		if prop.ValueType() != ical.ValueDefault {
			bare.SetValueType(prop.ValueType())
		}
		t, err = bare.DateTime(loc)
		if err != nil {
			return time.Time{}, allDay
		}
	}
	return t, allDay
}

// endOf reads the end of an entry, which iCalendar writes as DTEND, as a
// DURATION, or not at all.
func endOf(comp *ical.Component, start time.Time, allDay bool, loc *time.Location) (time.Time, bool) {
	if end, isDate := timeOf(comp, ical.PropDateTimeEnd, loc); !end.IsZero() {
		return end, isDate
	}
	if prop := comp.Props.Get(ical.PropDuration); prop != nil {
		if dur, err := prop.Duration(); err == nil {
			return start.Add(dur), allDay
		}
	}
	if start.IsZero() {
		return time.Time{}, allDay
	}
	if allDay {
		return start.AddDate(0, 0, 1), true
	}
	return start, false
}

// overlaps is half-open on both sides: an entry that ends exactly when the
// window opens is not in it, and neither is one that starts as it closes. A
// zero-length entry at the window's start still counts.
func overlaps(start, end, from, to time.Time) bool {
	if !start.Before(to) {
		return false
	}
	if end.Equal(start) {
		return !start.Before(from)
	}
	return end.After(from)
}

func decode(raw string) (*ical.Calendar, error) {
	cal, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		return nil, fmt.Errorf("icalendar: %w", err)
	}
	return cal, nil
}
