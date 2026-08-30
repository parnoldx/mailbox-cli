package vcal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// productID names this program in what it writes, which is how the next client
// to open the object knows who made it.
const productID = "-//mailbox//EN"

// NewUID mints an identifier for a new object. It is ours and it never changes:
// it names the object, the file it lives in, and the row in the Mirror.
func NewUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-mailbox", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:]) + "-mailbox"
}

// NewTodo builds a VTODO. A Todo is undated and unranked by default, which is
// the ordinary kind: most of them are things to do, not things to do on
// Thursday and not things that matter more than the one below them.
func NewTodo(uid, summary string, due time.Time, dueIsDate bool, priority int) (string, error) {
	cal := newCalendar()
	todo := ical.NewComponent(ical.CompToDo)
	todo.Props.SetText(ical.PropUID, uid)
	todo.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	todo.Props.SetDateTime(ical.PropCreated, time.Now().UTC())
	todo.Props.SetText(ical.PropSummary, summary)
	todo.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
	if !due.IsZero() {
		setDue(todo, due, dueIsDate)
	}
	setPriority(todo, priority)
	cal.Children = append(cal.Children, todo)
	return encode(cal)
}

// Complete marks a Todo done, the way every other client reads done: a status,
// a timestamp, and a percentage. Writing only one of the three leaves it
// half-done in somebody's client.
func Complete(raw string, at time.Time) (string, error) {
	return edit(raw, func(c *ical.Component) {
		c.Props.SetText(ical.PropStatus, "COMPLETED")
		c.Props.SetDateTime("COMPLETED", at.UTC())
		c.Props.SetText(ical.PropPercentComplete, "100")
	})
}

// Uncomplete puts it back on the list.
func Uncomplete(raw string) (string, error) {
	return edit(raw, func(c *ical.Component) {
		c.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
		c.Props.Del("COMPLETED")
		c.Props.Del(ical.PropPercentComplete)
	})
}

// Rename changes the summary and nothing else.
func Rename(raw, summary string) (string, error) {
	return edit(raw, func(c *ical.Component) {
		c.Props.SetText(ical.PropSummary, summary)
	})
}

// SetDue changes or clears the date something is wanted by.
func SetDue(raw string, due time.Time, dueIsDate bool) (string, error) {
	return edit(raw, func(c *ical.Component) {
		if due.IsZero() {
			c.Props.Del(ical.PropDue)
			return
		}
		setDue(c, due, dueIsDate)
	})
}

func setDue(c *ical.Component, due time.Time, isDate bool) {
	if isDate {
		c.Props.SetDate(ical.PropDue, due)
		return
	}
	c.Props.SetDateTime(ical.PropDue, due.UTC())
}

// SetPriority changes or clears how much a Todo matters. Zero clears it, which
// is iCalendar's "nobody said" and not the same as low; a negative number is
// nobody having named one, and leaves what is there.
func SetPriority(raw string, priority int) (string, error) {
	return edit(raw, func(c *ical.Component) {
		setPriority(c, priority)
	})
}

// PriorityWord says which of the three buckets an iCalendar PRIORITY falls in.
// The format has nine levels and every client that shows them shows three
// (RFC 5545 §3.8.1.9), so the number is the record and the word is what a
// caller reads and types. Zero is "nobody said", which is not low.
func PriorityWord(n int) string {
	switch {
	case n <= 0:
		return ""
	case n <= 4:
		return "high"
	case n == 5:
		return "medium"
	default:
		return "low"
	}
}

// PriorityNumber reads the word back. A bare 1-9 is taken as it is, so a caller
// that already speaks iCalendar is not made to round-trip through the words.
func PriorityNumber(word string) (int, error) {
	word = strings.TrimSpace(strings.ToLower(word))
	switch word {
	case "":
		return -1, nil // nothing named: leave it alone
	case "none", "unset":
		return 0, nil
	case "high":
		return 1, nil
	case "medium", "normal":
		return 5, nil
	case "low":
		return 9, nil
	}
	if n, err := strconv.Atoi(word); err == nil && n >= 0 && n <= 9 {
		return n, nil
	}
	return -1, fmt.Errorf("--priority takes high, medium, low or none — got %q", word)
}

func setPriority(c *ical.Component, priority int) {
	switch {
	case priority < 0:
		return
	case priority == 0:
		c.Props.Del(ical.PropPriority)
		return
	}
	// Written as the number iCalendar says it is: as text it would carry a
	// VALUE=TEXT nobody else writes, over a property that is an integer.
	prop := ical.NewProp(ical.PropPriority)
	prop.SetValueType(ical.ValueInt)
	prop.Value = strconv.Itoa(priority)
	c.Props.Set(prop)
}

// edit applies a change to the object's primary component and re-serialises it.
// Everything else in the object is carried through untouched: a VTODO we did
// not write may hold alarms, categories and X- properties that belong to
// somebody else's client, and dropping them is how two clients fight.
func edit(raw string, fn func(*ical.Component)) (string, error) {
	cal, err := decode(raw)
	if err != nil {
		return "", err
	}
	comp, _ := primary(cal)
	if comp == nil {
		return "", fmt.Errorf("nothing to change in this object")
	}
	fn(comp)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	return encode(cal)
}

func newCalendar() *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, productID)
	cal.Props.SetText(ical.PropVersion, "2.0")
	return cal
}

func encode(cal *ical.Calendar) (string, error) {
	var b strings.Builder
	if err := ical.NewEncoder(&b).Encode(cal); err != nil {
		return "", err
	}
	return b.String(), nil
}

// NewEventObject builds an all-day VEVENT that exists to carry something else.
// It is the shape the habits object needs: a real event nobody attends, so a
// calendar server that only understands VEVENT will hold it (ADR-0018).
//
// It is dated `on`, and that matters more than it looks. This server exposes
// only a window of its calendars over CalDAV — roughly a year back — and an
// event outside it is stored, fetchable by URL, and reported by no listing at
// all. An object anchored in 1990, which is what the program before this one
// used, is invisible to every sync the moment it is written.
func NewEventObject(uid, summary, description string, on time.Time) (string, error) {
	if on.IsZero() {
		on = time.Now()
	}
	day := time.Date(on.Year(), on.Month(), on.Day(), 0, 0, 0, 0, time.UTC)
	cal := newCalendar()
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, uid)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	ev.Props.SetDate(ical.PropDateTimeStart, day)
	ev.Props.SetDate(ical.PropDateTimeEnd, day.AddDate(0, 0, 1))
	// Transparent and private: it is storage, not an appointment, and it should
	// not make anybody look busy.
	ev.Props.SetText("TRANSP", "TRANSPARENT")
	ev.Props.SetText("CLASS", "PRIVATE")
	ev.Props.SetText(ical.PropSummary, summary)
	ev.Props.SetText(ical.PropDescription, description)
	cal.Children = append(cal.Children, ev)
	return encode(cal)
}

// SetDescription replaces the description of an object's primary component and
// re-anchors it to `on`, keeping everything else exactly as it was.
//
// Editing rather than rebuilding matters against a server that tracks changes
// itself. Open-Xchange keeps SEQUENCE and LAST-MODIFIED on what it stored, and a
// PUT of a freshly built object without them is refused as an outdated update —
// `412`, with or without an If-Match — because from its side that is what it
// looks like.
func SetDescription(raw, description string, on time.Time) (string, error) {
	if on.IsZero() {
		on = time.Now()
	}
	day := time.Date(on.Year(), on.Month(), on.Day(), 0, 0, 0, 0, time.UTC)
	return edit(raw, func(c *ical.Component) {
		c.Props.SetText(ical.PropDescription, description)
		c.Props.SetDate(ical.PropDateTimeStart, day)
		c.Props.SetDate(ical.PropDateTimeEnd, day.AddDate(0, 0, 1))
	})
}

// Description reads the description of an object's primary component, which is
// where the habits object keeps its record.
func Description(raw string) (string, error) {
	cal, err := decode(raw)
	if err != nil {
		return "", err
	}
	comp, _ := primary(cal)
	if comp == nil {
		return "", nil
	}
	text, err := comp.Props.Text(ical.PropDescription)
	if err != nil {
		return "", err
	}
	return text, nil
}

// NewEvent builds an appointment. Unlike NewEventObject above — which is
// storage that happens to be shaped like an event — this is something somebody
// actually has on, so it is opaque and it keeps whatever times it was given.
//
// An all-day event is a DATE with an exclusive end, which is what every
// calendar means by "one day": start 2026-09-01, end 2026-09-02.
func NewEvent(uid string, e EventEdit) (string, error) {
	cal := newCalendar()
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, uid)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	setWhen(ev, e.Start, e.End, e.AllDay)
	ev.Props.SetText(ical.PropSummary, e.Summary)
	if e.Description != "" {
		ev.Props.SetText(ical.PropDescription, e.Description)
	}
	if e.Location != "" {
		ev.Props.SetText(ical.PropLocation, e.Location)
	}
	setURL(ev, e.URL)
	setRepeat(ev, e.Repeat)
	setAlarms(ev, e.Alarms, e.Summary)
	cal.Children = append(cal.Children, ev)
	return encode(cal)
}

// EventEdit is what an add or an edit says about an event. An empty field is
// one the caller did not name and is left alone: an edit that reset the times
// because somebody was fixing a typo in the summary would be the worst kind of
// helpful.
type EventEdit struct {
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
	// Repeat is an RRULE as Rule returns it. Empty leaves whatever rule is
	// already there; RepeatNone takes it off.
	Repeat string
	// Alarms is the reminders, each so many minutes before the start. Nil
	// leaves the ones that are there — they may be somebody else's client's —
	// and a non-nil empty slice takes every one of them off.
	Alarms []int
}

// Empty reports that the caller named nothing, which is a usage error for an
// edit and the ordinary case for the optional half of an add.
func (e EventEdit) Empty() bool {
	return e.Summary == "" && e.Description == "" && e.Location == "" && e.URL == "" &&
		e.Start.IsZero() && e.End.IsZero() && !e.AllDay && e.Repeat == "" && e.Alarms == nil
}

// SetEvent applies an edit to an object already on the server. It edits rather
// than rebuilds for the reason SetDescription does: the server keeps SEQUENCE
// and LAST-MODIFIED on what it stored, and a freshly built object without them
// is refused as an outdated update.
func SetEvent(raw string, e EventEdit) (string, error) {
	return edit(raw, func(c *ical.Component) {
		if e.Summary != "" {
			c.Props.SetText(ical.PropSummary, e.Summary)
		}
		if e.Description != "" {
			c.Props.SetText(ical.PropDescription, e.Description)
		}
		if e.Location != "" {
			c.Props.SetText(ical.PropLocation, e.Location)
		}
		setURL(c, e.URL)
		setRepeat(c, e.Repeat)
		setAlarms(c, e.Alarms, summaryOf(c, e.Summary))
		if e.Start.IsZero() {
			return
		}
		end := e.End
		if end.IsZero() {
			end = defaultEnd(e.Start, e.AllDay)
		}
		setWhen(c, e.Start, end, e.AllDay)
	})
}

// URLNone takes a link off, the way RepeatNone takes a rule off. Without it an
// edit could add a link and never remove one.
const URLNone = "none"

func setURL(c *ical.Component, raw string) {
	switch {
	case raw == "":
		return
	case strings.EqualFold(raw, URLNone):
		c.Props.Del(ical.PropURL)
		return
	}
	// Written as a URI rather than as text: the text form escapes the commas
	// and semicolons a query string is made of.
	prop := ical.NewProp(ical.PropURL)
	prop.SetValueType(ical.ValueURI)
	prop.Value = raw
	c.Props.Set(prop)
}

// RepeatNone is the Repeat that takes a recurrence rule off, as against the
// empty string, which leaves the rule that is there alone. The difference is
// the whole reason both exist: an edit names what it changes.
const RepeatNone = "none"

// repeats are the rules people actually pick, so that a caller does not have to
// know RFC 5545 to say "every weekday". Anything else is read as a rule.
var repeats = map[string]string{
	"daily":    "FREQ=DAILY",
	"weekly":   "FREQ=WEEKLY",
	"biweekly": "FREQ=WEEKLY;INTERVAL=2",
	"monthly":  "FREQ=MONTHLY",
	"yearly":   "FREQ=YEARLY",
	"annually": "FREQ=YEARLY",
	"weekdays": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
}

// Rule turns what a caller typed into an RRULE, and refuses one that no
// calendar would accept. It validates here rather than at the server, because a
// rule the server rejects comes back as a 400 with nothing in it worth reading.
func Rule(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil
	}
	if strings.EqualFold(spec, RepeatNone) || strings.EqualFold(spec, "never") {
		return RepeatNone, nil
	}
	if rule, ok := repeats[strings.ToLower(spec)]; ok {
		return rule, nil
	}
	rule := strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(spec), "RRULE:"))
	if _, err := rrule.StrToROption(rule); err != nil {
		return "", fmt.Errorf("--repeat takes %s, none, or a rule like FREQ=WEEKLY;BYDAY=MO — %q is neither (%v)",
			strings.Join(repeatWords(), ", "), spec, err)
	}
	return rule, nil
}

// repeatWords lists the words Rule knows, in an order a reader expects rather
// than the map's.
func repeatWords() []string {
	return []string{"daily", "weekly", "biweekly", "monthly", "yearly", "weekdays"}
}

func setRepeat(c *ical.Component, rule string) {
	switch {
	case rule == "":
		return
	case rule == RepeatNone:
		c.Props.Del(ical.PropRecurrenceRule)
		return
	}
	prop := ical.NewProp(ical.PropRecurrenceRule)
	prop.SetValueType(ical.ValueRecurrence)
	prop.Value = rule
	c.Props.Set(prop)
}

// setAlarms replaces the reminders with the ones named. It is all or nothing
// because "the reminders" is what a caller picks, one list at a time; a nil
// list is the caller not naming them at all, and leaves whatever another
// client put there.
func setAlarms(c *ical.Component, minutes []int, summary string) {
	if minutes == nil {
		return
	}
	kept := make([]*ical.Component, 0, len(c.Children))
	for _, child := range c.Children {
		if child.Name != ical.CompAlarm {
			kept = append(kept, child)
		}
	}
	c.Children = kept
	if summary == "" {
		summary = "Reminder"
	}
	for _, m := range minutes {
		alarm := ical.NewComponent(ical.CompAlarm)
		alarm.Props.SetText(ical.PropAction, "DISPLAY")
		// A DISPLAY alarm with no description is what a client shows when it
		// fires, so it says what it is about rather than nothing at all.
		alarm.Props.SetText(ical.PropDescription, summary)
		trigger := ical.NewProp(ical.PropTrigger)
		trigger.SetValueType(ical.ValueDuration)
		trigger.Value = beforeStart(m)
		alarm.Props.Set(trigger)
		c.Children = append(c.Children, alarm)
	}
}

// beforeStart writes a reminder's trigger the way a calendar reads it back to
// a person: -PT15M rather than the -PT900S a duration in seconds would give.
func beforeStart(minutes int) string {
	switch {
	case minutes <= 0:
		return "PT0S"
	case minutes%60 == 0:
		return fmt.Sprintf("-PT%dH", minutes/60)
	}
	return fmt.Sprintf("-PT%dM", minutes)
}

// summaryOf is what a new alarm should say: the summary the edit is setting, or
// the one already on the object when the edit left it alone.
func summaryOf(c *ical.Component, summary string) string {
	if summary != "" {
		return summary
	}
	text, _ := c.Props.Text(ical.PropSummary)
	return text
}

// setWhen writes the start and the end in the form the kind of event calls for:
// a DATE for an all-day one, a DATE-TIME for anything with a clock on it.
func setWhen(c *ical.Component, start, end time.Time, allDay bool) {
	if end.IsZero() {
		end = defaultEnd(start, allDay)
	}
	if allDay {
		c.Props.SetDate(ical.PropDateTimeStart, dayOf(start))
		c.Props.SetDate(ical.PropDateTimeEnd, dayOf(end))
		return
	}
	c.Props.SetDateTime(ical.PropDateTimeStart, start)
	c.Props.SetDateTime(ical.PropDateTimeEnd, end)
}

// defaultEnd is how long an event lasts when nobody said: a whole day, or an
// hour, which are the two answers that are right most of the time.
func defaultEnd(start time.Time, allDay bool) time.Time {
	if allDay {
		return start.AddDate(0, 0, 1)
	}
	return start.Add(time.Hour)
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
