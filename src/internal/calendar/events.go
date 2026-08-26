package calendar

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"mailbox/src/internal/dav"
	"mailbox/src/internal/format"
	"mailbox/src/internal/vobject"
)

type EventIn struct {
	Title, Start, End, Calendar, Location, Notes, URL, Repeat, RepeatUntil, Remind string
	AllDay, Circle                                                                 bool
	RepeatTimes                                                                    int
	Has                                                                            EventHas
}

type EventHas struct {
	Title, Start, End, AllDay, Location, Notes, URL, Repeat, Remind, Circle bool
}

const eventsQueryTpl = `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">%s
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`

func icsRange(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

func (cal *Cal) reportEvents(col Collection, start, stop time.Time, expand bool) []*format.OM {
	inner := ""
	timeRange := fmt.Sprintf("\n        <c:time-range start=%q end=%q/>", icsRange(start), icsRange(stop))
	if expand {
		inner = timeRange + fmt.Sprintf("\n        <c:expand start=%q end=%q/>", icsRange(start), icsRange(stop))
	} else {
		inner = timeRange
	}
	query := fmt.Sprintf(eventsQueryTpl, inner)
	raw, status, err := cal.client.Report(col.URL, query, "1")
	if err != nil || (status != 200 && status != 207) {
		return nil
	}
	var rows []*format.OM
	for _, resp := range dav.ParseMultistatus(raw, "calendar-data") {
		props := vobject.Component(resp.Data, "VEVENT")
		if props == nil {
			continue
		}
		has := map[string]bool{"dtstart": findHas(props, "DTSTART"), "dtend": findHas(props, "DTEND")}
		row, err := eventFields(props, "", col.Name, has)
		if err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func eventFields(props []vobject.Prop, uid, calName string, has map[string]bool) (*format.OM, error) {
	if props == nil {
		return nil, fmt.Errorf("event not found: %s", uid)
	}
	var attendees []string
	for _, p := range props {
		if p.Name == "ATTENDEE" {
			attendees = append(attendees, strings.Replace(p.Value, "mailto:", "", 1))
		}
	}
	priority := vobject.First(props, "PRIORITY")
	var reminders []string
	inAlarm := false
	for _, p := range props {
		if p.Name == "BEGIN" && strings.EqualFold(p.Value, "VALARM") {
			inAlarm = true
			continue
		}
		if p.Name == "END" && strings.EqualFold(p.Value, "VALARM") {
			inAlarm = false
			continue
		}
		if inAlarm && p.Name == "TRIGGER" && p.Value != "" {
			reminders = append(reminders, p.Value)
		}
	}
	return format.NewOM(
		"id", shortID(orDefault(vobject.First(props, "UID"), uid)),
		"calendar", calName,
		"summary", vobject.First(props, "SUMMARY"),
		"start", asLocal(findProp(props, "DTSTART"), has["dtstart"]),
		"end", asLocal(findProp(props, "DTEND"), has["dtend"]),
		"location", vobject.First(props, "LOCATION"),
		"notes", vobject.First(props, "DESCRIPTION"),
		"url", vobject.First(props, "URL"),
		"status", vobject.First(props, "STATUS"),
		"circle", priority == "1",
		"rrule", vobject.First(props, "RRULE"),
		"reminders", strings.Join(reminders, ", "),
		"attendees", strings.Join(attendees, ", "),
	), nil
}

func (cal *Cal) Events(start, end, calendar string) ([]*format.OM, error) {
	var begin time.Time
	if start != "" {
		w, err := parseWhen(start)
		if err != nil {
			return nil, err
		}
		begin = w.t
		if w.isDate {
			begin = time.Date(w.t.Year(), w.t.Month(), w.t.Day(), 0, 0, 0, 0, TZ)
		}
	} else {
		begin = time.Now().In(TZ)
	}
	stop := begin.Add(7 * 24 * time.Hour)
	if end != "" {
		w, err := parseWhen(end)
		if err != nil {
			return nil, err
		}
		stop = w.t
		if w.isDate {
			stop = time.Date(w.t.Year(), w.t.Month(), w.t.Day(), 23, 59, 59, 0, TZ)
		}
	}
	cols, err := cal.eventCals()
	if err != nil {
		return nil, err
	}
	if calendar != "" {
		col, err := matchCal(cols, calendar)
		if err != nil {
			return nil, err
		}
		cols = []Collection{col}
	}
	var rows []*format.OM
	for _, col := range cols {
		rows = append(rows, cal.reportEvents(col, begin, stop, true)...)
	}
	sortRows(rows, func(a, b *format.OM) bool { return strOr(a.Get("start")) < strOr(b.Get("start")) })
	return rows, nil
}

func (cal *Cal) Event(uid string) (*format.OM, error) {
	hit, err := cal.lookupEvent(uid)
	if err != nil {
		return nil, err
	}
	has := map[string]bool{"dtstart": findHas(hit.props, "DTSTART"), "dtend": findHas(hit.props, "DTEND")}
	return eventFields(hit.props, uid, hit.col.Name, has)
}

type eventHit struct {
	props []vobject.Prop
	col   Collection
	href  string
	full  string
}

func (cal *Cal) lookupEvent(uid string) (eventHit, error) {
	cols, err := cal.eventCals()
	if err != nil {
		return eventHit{}, err
	}
	for _, col := range cols {
		eventURL := strings.TrimRight(col.URL, "/") + "/" + url.PathEscape(uid) + ".ics"
		if text, status, err := cal.client.Get(eventURL); err == nil && status == 200 {
			if props := vobject.Component(text, "VEVENT"); props != nil {
				full := vobject.First(props, "UID")
				if uidMatches(full, uid) || strings.EqualFold(full, uid) {
					return eventHit{props: props, col: col, href: eventURL, full: full}, nil
				}
			}
		}
	}
	begin := time.Now().In(TZ).Add(-730 * 24 * time.Hour)
	stop := time.Now().In(TZ).Add(730 * 24 * time.Hour)
	var scored []eventHit
	for _, col := range cols {
		for _, resp := range dav.ParseMultistatus(cal.reportRaw(col.URL, begin, stop), "calendar-data") {
			props := vobject.Component(resp.Data, "VEVENT")
			full := vobject.First(props, "UID")
			if uidMatches(full, uid) {
				scored = append(scored, eventHit{
					props: props, col: col, href: absURL(col.URL, resp.Href), full: full,
				})
			}
		}
	}
	if len(scored) == 0 {
		return eventHit{}, fmt.Errorf("event not found: %s", uid)
	}
	unique := map[string]eventHit{}
	var order []string
	for _, h := range scored {
		if _, seen := unique[h.full]; !seen {
			order = append(order, h.full)
		}
		unique[h.full] = h
	}
	if len(unique) > 1 {
		sortStrings(order)
		return eventHit{}, fmt.Errorf("ambiguous event id %q, matches:\n%s", uid, strings.Join(order, "\n"))
	}
	return unique[order[0]], nil
}

func (cal *Cal) reportRaw(calURL string, start, stop time.Time) []byte {
	inner := fmt.Sprintf("\n        <c:time-range start=%q end=%q/>", icsRange(start), icsRange(stop))
	query := fmt.Sprintf(eventsQueryTpl, inner)
	raw, status, err := cal.client.Report(calURL, query, "1")
	if err != nil || (status != 200 && status != 207) {
		return nil
	}
	return raw
}

func (cal *Cal) CreateEvent(in EventIn) (string, string, error) {
	if in.Title == "" {
		return "", "", fmt.Errorf("event add needs --title")
	}
	if in.Start == "" {
		return "", "", fmt.Errorf("event add needs --starts-on")
	}
	col, err := cal.pickEventCal(in.Calendar)
	if err != nil {
		return "", "", err
	}
	ds, de, begin, err := eventTimeProps(in.Start, in.End, in.AllDay)
	if err != nil {
		return "", "", err
	}
	uid := newUUID()
	props := []vobject.Prop{
		{Name: "UID", Value: uid},
		{Name: "DTSTAMP", Value: stampUTC()},
		{Name: "SUMMARY", Value: in.Title},
		ds, de,
	}
	props, err = applyEventExtras(props, in, begin)
	if err != nil {
		return "", "", err
	}
	putURL := strings.TrimRight(col.URL, "/") + "/" + uid + ".ics"
	if err := cal.putICS(putURL, "BEGIN:VEVENT\r\n"+vobject.Serialize(props)+"END:VEVENT"); err != nil {
		return "", "", err
	}
	return uid, col.Name, nil
}

func (cal *Cal) UpdateEvent(uid string, in EventIn) (*format.OM, error) {
	hit, err := cal.lookupEvent(uid)
	if err != nil {
		return nil, err
	}
	props := hit.props
	if in.Has.Title {
		props = setProp(props, "SUMMARY", "", in.Title)
	}
	touchTime := in.Has.Start || in.Has.End || in.Has.AllDay
	if touchTime {
		start := in.Start
		if !in.Has.Start {
			start = asLocal(findProp(props, "DTSTART"), true)
			start = strings.Replace(start, " ", "T", 1)
		}
		end := in.End
		if !in.Has.End {
			end = ""
		}
		allDay := in.AllDay
		if !in.Has.AllDay {
			allDay = isDateProp(findProp(props, "DTSTART"))
		}
		ds, de, _, err := eventTimeProps(start, end, allDay)
		if err != nil {
			return nil, err
		}
		props = setProp(props, "DTSTART", ds.Params, ds.Value)
		props = setProp(props, "DTEND", de.Params, de.Value)
	}
	begin := time.Now().In(TZ)
	if p := findProp(props, "DTSTART"); p.Value != "" {
		if isDateProp(p) {
			if t, err := time.ParseInLocation("20060102", p.Value, TZ); err == nil {
				begin = t
			}
		} else if t, err := parseICSDateTime(p); err == nil {
			begin = t
		}
	}
	props, err = applyEventExtras(props, in, begin)
	if err != nil {
		return nil, err
	}
	if err := cal.putICS(hit.href, "BEGIN:VEVENT\r\n"+vobject.Serialize(props)+"END:VEVENT"); err != nil {
		return nil, err
	}
	has := map[string]bool{"dtstart": findHas(props, "DTSTART"), "dtend": findHas(props, "DTEND")}
	return eventFields(props, uid, hit.col.Name, has)
}

func applyEventExtras(props []vobject.Prop, in EventIn, begin time.Time) ([]vobject.Prop, error) {
	if in.Has.Location {
		props = setProp(props, "LOCATION", "", in.Location)
	}
	if in.Has.Notes {
		props = setProp(props, "DESCRIPTION", "", in.Notes)
	}
	if in.Has.URL {
		props = setProp(props, "URL", "", in.URL)
	}
	if in.Has.Circle && in.Circle {
		props = setProp(props, "PRIORITY", "", "1")
	}
	if in.Has.Repeat {
		rule, err := rruleFromAlias(in.Repeat, begin, in.RepeatUntil, in.RepeatTimes)
		if err != nil {
			return nil, err
		}
		props = setProp(props, "RRULE", "", rule)
	} else if in.RepeatUntil != "" || in.RepeatTimes > 0 {
		return nil, fmt.Errorf("--repeat-until/--repeat-times need --repeat")
	}
	if in.Has.Remind {
		trig, err := icsTrigger(in.Remind)
		if err != nil {
			return nil, err
		}
		props = dropAlarm(props)
		props = append(props,
			vobject.Prop{Name: "BEGIN", Value: "VALARM"},
			vobject.Prop{Name: "ACTION", Value: "DISPLAY"},
			vobject.Prop{Name: "DESCRIPTION", Value: "Reminder"},
			vobject.Prop{Name: "TRIGGER", Value: trig},
			vobject.Prop{Name: "END", Value: "VALARM"},
		)
	}
	return props, nil
}

func (cal *Cal) DeleteEvent(uid string) error {
	hit, err := cal.lookupEvent(uid)
	if err != nil {
		return err
	}
	status, err := cal.client.Delete(hit.href)
	if err != nil || (status != 200 && status != 204 && status != 202) {
		return fmt.Errorf("CalDAV delete failed: %d", status)
	}
	return nil
}
