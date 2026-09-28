package graphdrv

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/htmlmd"
	"mailbox/internal/vcal"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// graphEvent is an event, an occurrence of one, or an exception to one, as
// Graph describes it. Times are asked for in UTC (Prefer: outlook.timezone), so
// dateTime needs no zone table to read.
type graphEvent struct {
	ID      string `json:"id"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
	ICalUID        string `json:"iCalUId"`
	Type           string `json:"type"`
	SeriesMasterID string `json:"seriesMasterId"`
	ChangeKey      string `json:"changeKey"`
	Subject        string `json:"subject"`
	Body           struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Start                      zonedTime  `json:"start"`
	End                        zonedTime  `json:"end"`
	OriginalStart              *time.Time `json:"originalStart"`
	IsAllDay                   bool       `json:"isAllDay"`
	IsCancelled                bool       `json:"isCancelled"`
	IsReminderOn               bool       `json:"isReminderOn"`
	ReminderMinutesBeforeStart int        `json:"reminderMinutesBeforeStart"`
	OnlineMeeting              *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	Recurrence *recurrence `json:"recurrence"`
}

type zonedTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// time reads a Graph dateTime in the zone it names. Only UTC is ever asked for,
// so a zone this machine cannot load is read as UTC rather than lost.
func (z zonedTime) time() time.Time {
	loc, err := time.LoadLocation(z.TimeZone)
	if err != nil || z.TimeZone == "" {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05.9999999", z.DateTime, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

type recurrence struct {
	Pattern struct {
		Type           string   `json:"type"`
		Interval       int      `json:"interval"`
		Month          int      `json:"month,omitempty"`
		DayOfMonth     int      `json:"dayOfMonth,omitempty"`
		DaysOfWeek     []string `json:"daysOfWeek,omitempty"`
		FirstDayOfWeek string   `json:"firstDayOfWeek,omitempty"`
		Index          string   `json:"index,omitempty"`
	} `json:"pattern"`
	Range struct {
		Type                string `json:"type"`
		StartDate           string `json:"startDate"`
		EndDate             string `json:"endDate,omitempty"`
		NumberOfOccurrences int    `json:"numberOfOccurrences,omitempty"`
		RecurrenceTimeZone  string `json:"recurrenceTimeZone,omitempty"`
	} `json:"range"`
}

// eventSelect is every property the translation reads.
const eventSelect = "$select=iCalUId,type,seriesMasterId,changeKey,subject,body,location,start,end," +
	"originalStart,isAllDay,isCancelled,isReminderOn,reminderMinutesBeforeStart,onlineMeeting,recurrence"

// zone is the time zone a repeating event is written in, so that "every Monday
// at ten" stays at ten across a DST change; and the zone an event we write is
// sent in. It is this machine's, by IANA name, because that is what both
// iCalendar and Graph accept.
func zone() (*time.Location, string) {
	name := strings.TrimPrefix(os.Getenv("TZ"), ":")
	if name == "" {
		if link, err := os.Readlink("/etc/localtime"); err == nil {
			if i := strings.Index(link, "zoneinfo/"); i >= 0 {
				name = link[i+len("zoneinfo/"):]
			}
		}
	}
	if loc, err := time.LoadLocation(name); err == nil && name != "" && name != "Local" {
		return loc, name
	}
	return time.UTC, "UTC"
}

// veventOf is one Graph event as a VEVENT. A repeating one keeps its rule and
// is written in loc; a single one is written in UTC, which cannot drift.
func veventOf(uid string, g graphEvent, loc *time.Location) *ical.Component {
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, uid)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	start, end := g.Start.time(), g.End.time()
	switch {
	case g.IsAllDay:
		ev.Props.SetDate(ical.PropDateTimeStart, start)
		ev.Props.SetDate(ical.PropDateTimeEnd, end)
	case g.Recurrence != nil:
		ev.Props.SetDateTime(ical.PropDateTimeStart, start.In(loc))
		ev.Props.SetDateTime(ical.PropDateTimeEnd, end.In(loc))
	default:
		ev.Props.SetDateTime(ical.PropDateTimeStart, start.UTC())
		ev.Props.SetDateTime(ical.PropDateTimeEnd, end.UTC())
	}
	ev.Props.SetText(ical.PropSummary, g.Subject)
	if g.Location.DisplayName != "" {
		ev.Props.SetText(ical.PropLocation, g.Location.DisplayName)
	}
	if text := bodyText(g); text != "" {
		ev.Props.SetText(ical.PropDescription, text)
	}
	// The join link is what an agenda line most wants to carry for a meeting.
	if g.OnlineMeeting != nil && g.OnlineMeeting.JoinURL != "" {
		prop := ical.NewProp(ical.PropURL)
		prop.SetValueType(ical.ValueURI)
		prop.Value = g.OnlineMeeting.JoinURL
		ev.Props.Set(prop)
	}
	if g.IsCancelled {
		ev.Props.SetText(ical.PropStatus, "CANCELLED")
	}
	if g.Recurrence != nil {
		if rule, err := ruleOf(*g.Recurrence, loc); err == nil {
			prop := ical.NewProp(ical.PropRecurrenceRule)
			prop.SetValueType(ical.ValueRecurrence)
			prop.Value = rule
			ev.Props.Set(prop)
		}
		// A rule that does not translate leaves the master as a single entry
		// at its first date: shown, never guessed.
	}
	if g.IsReminderOn {
		alarm := ical.NewComponent(ical.CompAlarm)
		alarm.Props.SetText(ical.PropAction, "DISPLAY")
		alarm.Props.SetText(ical.PropDescription, g.Subject)
		trigger := ical.NewProp(ical.PropTrigger)
		trigger.SetValueType(ical.ValueDuration)
		trigger.Value = fmt.Sprintf("-PT%dM", g.ReminderMinutesBeforeStart)
		alarm.Props.Set(trigger)
		ev.Children = append(ev.Children, alarm)
	}
	return ev
}

// bodyText is the event's notes as text. An Outlook invite's body is HTML,
// rendered down the way a mail's is (ADR-0009).
func bodyText(g graphEvent) string {
	if strings.EqualFold(g.Body.ContentType, "html") {
		return strings.TrimSpace(htmlmd.HTMLToMarkdown(g.Body.Content))
	}
	return strings.TrimSpace(strings.ReplaceAll(g.Body.Content, "\r\n", "\n"))
}

// singleICal is a one-off event as a calendar object.
func singleICal(uid string, g graphEvent, loc *time.Location) (string, error) {
	cal := calendarOf(veventOf(uid, g, loc))
	return encodeCal(cal)
}

// seriesICal is a repeating event as one calendar object: the master with its
// rule, an override for every instance that was moved or retitled, and a
// cancelled override for every instance the rule makes and the server no longer
// has. That is what vcal expands — "a repeating event is one row" — and only the
// window the instances were read for is described.
func seriesICal(uid string, master graphEvent, instances []graphEvent, from, to time.Time, loc *time.Location) (string, error) {
	mc := veventOf(uid, master, loc)
	cal := calendarOf(mc)
	have := map[time.Time]bool{}
	for _, in := range instances {
		if in.OriginalStart == nil {
			continue
		}
		at := in.OriginalStart.UTC()
		have[at] = true
		if in.Type != "exception" && !in.IsCancelled {
			continue
		}
		ov := ical.NewComponent(ical.CompEvent)
		ov.Props.SetText(ical.PropUID, uid)
		ov.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
		ov.Props.SetDateTime(ical.PropRecurrenceID, at)
		ov.Props.SetDateTime(ical.PropDateTimeStart, in.Start.time().UTC())
		ov.Props.SetDateTime(ical.PropDateTimeEnd, in.End.time().UTC())
		ov.Props.SetText(ical.PropSummary, in.Subject)
		if in.Location.DisplayName != "" {
			ov.Props.SetText(ical.PropLocation, in.Location.DisplayName)
		}
		if in.IsCancelled {
			ov.Props.SetText(ical.PropStatus, "CANCELLED")
		}
		cal.Children = append(cal.Children, ov)
	}
	if set, err := mc.RecurrenceSet(loc); err == nil && set != nil && !master.IsAllDay {
		for _, at := range set.Between(from, to, true) {
			if have[at.UTC()] {
				continue
			}
			ov := ical.NewComponent(ical.CompEvent)
			ov.Props.SetText(ical.PropUID, uid)
			ov.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
			ov.Props.SetDateTime(ical.PropRecurrenceID, at.UTC())
			ov.Props.SetDateTime(ical.PropDateTimeStart, at.UTC())
			ov.Props.SetText(ical.PropStatus, "CANCELLED")
			cal.Children = append(cal.Children, ov)
		}
	}
	return encodeCal(cal)
}

func calendarOf(ev *ical.Component) *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//mailbox//graph//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Children = append(cal.Children, ev)
	return cal
}

func encodeCal(cal *ical.Calendar) (string, error) {
	var b strings.Builder
	if err := ical.NewEncoder(&b).Encode(cal); err != nil {
		return "", err
	}
	return b.String(), nil
}

var graphDays = map[string]string{
	"monday": "MO", "tuesday": "TU", "wednesday": "WE", "thursday": "TH",
	"friday": "FR", "saturday": "SA", "sunday": "SU",
}

var graphIndex = map[string]string{"first": "1", "second": "2", "third": "3", "fourth": "4", "last": "-1"}

// ruleOf is a Graph recurrence as an RRULE.
func ruleOf(r recurrence, loc *time.Location) (string, error) {
	p := r.Pattern
	var parts []string
	days := func() error {
		var out []string
		for _, d := range p.DaysOfWeek {
			code, ok := graphDays[strings.ToLower(d)]
			if !ok {
				return fmt.Errorf("day %q", d)
			}
			out = append(out, code)
		}
		if len(out) > 0 {
			parts = append(parts, "BYDAY="+strings.Join(out, ","))
		}
		return nil
	}
	switch p.Type {
	case "daily":
		parts = append(parts, "FREQ=DAILY")
	case "weekly":
		parts = append(parts, "FREQ=WEEKLY")
		if err := days(); err != nil {
			return "", err
		}
		// Monday is iCalendar's default week start, and only matters at all
		// for a rule that skips weeks.
		if code, ok := graphDays[strings.ToLower(p.FirstDayOfWeek)]; ok && code != "MO" && p.Interval > 1 {
			parts = append(parts, "WKST="+code)
		}
	case "absoluteMonthly":
		parts = append(parts, "FREQ=MONTHLY", "BYMONTHDAY="+strconv.Itoa(p.DayOfMonth))
	case "relativeMonthly", "relativeYearly":
		if p.Type == "relativeMonthly" {
			parts = append(parts, "FREQ=MONTHLY")
		} else {
			parts = append(parts, "FREQ=YEARLY", "BYMONTH="+strconv.Itoa(p.Month))
		}
		if err := days(); err != nil {
			return "", err
		}
		if idx, ok := graphIndex[p.Index]; ok {
			parts = append(parts, "BYSETPOS="+idx)
		}
	case "absoluteYearly":
		parts = append(parts, "FREQ=YEARLY", "BYMONTH="+strconv.Itoa(p.Month), "BYMONTHDAY="+strconv.Itoa(p.DayOfMonth))
	default:
		return "", fmt.Errorf("recurrence %q", p.Type)
	}
	if p.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(p.Interval))
	}
	switch r.Range.Type {
	case "endDate":
		// The last day counts, whenever on it the instance falls.
		end, err := time.ParseInLocation("2006-01-02", r.Range.EndDate, loc)
		if err != nil {
			return "", err
		}
		parts = append(parts, "UNTIL="+end.AddDate(0, 0, 1).Add(-time.Second).UTC().Format("20060102T150405Z"))
	case "numbered":
		parts = append(parts, "COUNT="+strconv.Itoa(r.Range.NumberOfOccurrences))
	}
	return strings.Join(parts, ";"), nil
}

// rruleDays is indexed by rrule's Weekday.Day(), which counts from Monday.
var rruleDays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

var rruleIndex = map[int]string{1: "first", 2: "second", 3: "third", 4: "fourth", -1: "last"}

// recurrenceOf is an RRULE as a Graph recurrence, for the rules this program
// writes (vcal.Rule) and the ones Outlook itself can express. Anything else is
// refused with what could not be said, rather than written as something close.
func recurrenceOf(rule string, start time.Time, loc *time.Location, zoneName string) (*recurrence, error) {
	o, err := rrule.StrToROption(rule)
	if err != nil {
		return nil, err
	}
	if len(o.Byhour) > 0 || len(o.Byminute) > 0 || len(o.Bysecond) > 0 || len(o.Byyearday) > 0 ||
		len(o.Byweekno) > 0 || len(o.Bymonthday) > 1 || len(o.Bymonth) > 1 || len(o.Bysetpos) > 1 {
		return nil, fmt.Errorf("Microsoft 365 cannot repeat an event by %q", rule)
	}
	start = start.In(loc)
	r := &recurrence{}
	p := &r.Pattern
	p.Interval = max(o.Interval, 1)
	var days []string
	index := ""
	for _, d := range o.Byweekday {
		days = append(days, rruleDays[d.Day()])
		if n := d.N(); n != 0 {
			index = rruleIndex[n]
		}
	}
	if len(o.Bysetpos) == 1 {
		index = rruleIndex[o.Bysetpos[0]]
	}
	month := int(start.Month())
	if len(o.Bymonth) == 1 {
		month = o.Bymonth[0]
	}
	dayOfMonth := start.Day()
	if len(o.Bymonthday) == 1 {
		dayOfMonth = o.Bymonthday[0]
	}
	switch o.Freq {
	case rrule.DAILY:
		p.Type = "daily"
	case rrule.WEEKLY:
		p.Type = "weekly"
		if len(days) == 0 {
			days = []string{strings.ToLower(start.Weekday().String())}
		}
		p.DaysOfWeek = days
		p.FirstDayOfWeek = "monday"
	case rrule.MONTHLY, rrule.YEARLY:
		relative := len(days) > 0
		if relative && index == "" {
			return nil, fmt.Errorf("Microsoft 365 cannot repeat an event by %q", rule)
		}
		switch {
		case o.Freq == rrule.MONTHLY && relative:
			p.Type, p.DaysOfWeek, p.Index = "relativeMonthly", days, index
		case o.Freq == rrule.MONTHLY:
			p.Type, p.DayOfMonth = "absoluteMonthly", dayOfMonth
		case relative:
			p.Type, p.Month, p.DaysOfWeek, p.Index = "relativeYearly", month, days, index
		default:
			p.Type, p.Month, p.DayOfMonth = "absoluteYearly", month, dayOfMonth
		}
	default:
		return nil, fmt.Errorf("Microsoft 365 cannot repeat an event by %q", rule)
	}
	r.Range.StartDate = start.Format("2006-01-02")
	r.Range.RecurrenceTimeZone = zoneName
	switch {
	case o.Count > 0:
		r.Range.Type, r.Range.NumberOfOccurrences = "numbered", o.Count
	case !o.Until.IsZero():
		r.Range.Type, r.Range.EndDate = "endDate", o.Until.In(loc).Format("2006-01-02")
	default:
		r.Range.Type = "noEnd"
	}
	return r, nil
}

// eventFields is what a write sends: an event's calendar object as the Graph
// properties it maps to. The same function reads what the server has now, so a
// field the edit did not touch compares equal and is not sent — a PATCH of what
// changed, which cannot drop what we do not model (ADR-0010).
func eventFields(raw string, loc *time.Location, zoneName string) (map[string]any, error) {
	p, err := vcal.Parse(raw, loc)
	if err != nil {
		return nil, err
	}
	if p.Kind != vcal.KindEvent {
		return nil, fmt.Errorf("Microsoft 365 calendars hold events, not %s", p.Kind)
	}
	f := map[string]any{
		"subject":  p.Summary,
		"body":     map[string]string{"contentType": "text", "content": p.Description},
		"location": map[string]string{"displayName": p.Location},
		"isAllDay": p.AllDay,
	}
	when := func(t time.Time) map[string]string {
		if p.AllDay {
			return map[string]string{"dateTime": t.Format("2006-01-02") + "T00:00:00", "timeZone": zoneName}
		}
		return map[string]string{"dateTime": t.In(loc).Format("2006-01-02T15:04:05"), "timeZone": zoneName}
	}
	f["start"], f["end"] = when(p.Start), when(p.End)
	if p.Repeat != "" {
		r, err := recurrenceOf(p.Repeat, p.Start, loc, zoneName)
		if err != nil {
			return nil, err
		}
		f["recurrence"] = r
	} else {
		f["recurrence"] = nil
	}
	f["isReminderOn"] = len(p.Alarms) > 0
	if len(p.Alarms) > 0 {
		f["reminderMinutesBeforeStart"] = p.Alarms[0]
	}
	return f, nil
}
