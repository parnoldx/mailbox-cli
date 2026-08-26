// Package calendar: CalDAV over raw REPORT/GET/PUT (replaces the caldav+icalendar libs).
package calendar

import (
	"crypto/rand"
	"fmt"
	"net/url"

	"mailbox/src/internal/format"
	"regexp"
	"strings"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/dav"
	"mailbox/src/internal/vobject"
)

var TZ *time.Location

func init() {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		loc = time.UTC
	}
	TZ = loc
}

type Cal struct {
	acct    *config.Account
	client  *dav.Client
	baseURL string
}

func NewCal(acct *config.Account) (*Cal, error) {
	if acct.KalenderURL == "" || acct.AufgabenURL == "" {
		return nil, fmt.Errorf("missing MAILBOX_CALDAV_KALENDER, MAILBOX_CALDAV_AUFGABEN")
	}
	return &Cal{
		acct:    acct,
		client:  dav.New(acct.Email, acct.DAVPass()),
		baseURL: "https://dav.mailbox.org/caldav/",
	}, nil
}

func uidMatches(full, query string) bool {
	if query == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(full), strings.ToLower(query))
}

type when struct {
	t       time.Time
	isDate  bool
	dateStr string // for date-only values
}

func parseWhen(value string) (when, error) {
	w := when{}
	if len(value) == 10 {
		t, err := time.ParseInLocation("2006-01-02", value, TZ)
		if err != nil {
			return w, fmt.Errorf("invalid WHEN %q", value)
		}
		w.t, w.isDate, w.dateStr = t, true, t.Format("2006-01-02")
		return w, nil
	}
	layouts := []string{"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02T15:04"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			if t.Location() == time.UTC {
				w.t = t.In(TZ)
			} else if strings.HasSuffix(value, "Z") {
				w.t = t.In(TZ)
			} else {
				w.t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, TZ)
			}
			return w, nil
		}
	}
	return w, fmt.Errorf("invalid WHEN %q", value)
}

// asLocal renders an iCalendar dtstart/dtend prop like python _as_local.
func asLocal(prop vobject.Prop, present bool) string {
	if !present {
		return ""
	}
	value := prop.Value
	if isDateProp(prop) {
		if t, err := time.Parse("20060102", value); err == nil {
			return t.Format("2006-01-02")
		}
		if t, err := time.Parse("2006-01-02", value); err == nil {
			return t.Format("2006-01-02")
		}
		return value
	}
	t, err := parseICSDateTime(prop)
	if err != nil {
		return value
	}
	return t.In(TZ).Format("2006-01-02 15:04")
}

func isDateProp(prop vobject.Prop) bool {
	return strings.Contains(prop.Params, "VALUE=DATE")
}

func parseICSDateTime(prop vobject.Prop) (time.Time, error) {
	v := prop.Value
	layouts := []string{"20060102T150405Z", "20060102T150405"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			if strings.HasSuffix(v, "Z") {
				return t, nil
			}
			if tzid := tzID(prop); tzid != "" && tzid == "Europe/Berlin" {
				return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, TZ), nil
			}
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, TZ), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable datetime %q", v)
}

func tzID(prop vobject.Prop) string {
	i := strings.Index(prop.Params, "TZID=")
	if i < 0 {
		return ""
	}
	rest := prop.Params[i+len("TZID="):]
	if strings.HasPrefix(rest, `"`) {
		end := strings.Index(rest[1:], `"`)
		if end >= 0 {
			return rest[1 : end+1]
		}
	}
	end := strings.Index(rest, ";")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func eventFields(props []vobject.Prop, uid string, has map[string]bool) (*format.OM, error) {
	if props == nil {
		return nil, fmt.Errorf("event not found: %s", uid)
	}
	var attendees []string
	for _, p := range props {
		if p.Name == "ATTENDEE" {
			attendees = append(attendees, strings.Replace(p.Value, "mailto:", "", 1))
		}
	}
	return format.NewOM(
		"id", shortID(orDefault(vobject.First(props, "UID"), uid)),
		"summary", vobject.First(props, "SUMMARY"),
		"start", asLocal(findProp(props, "DTSTART"), has["dtstart"]),
		"end", asLocal(findProp(props, "DTEND"), has["dtend"]),
		"location", vobject.First(props, "LOCATION"),
		"status", vobject.First(props, "STATUS"),
		"attendees", strings.Join(attendees, ", "),
		"description", vobject.First(props, "DESCRIPTION"),
	), nil
}

func findProp(props []vobject.Prop, name string) vobject.Prop {
	for _, p := range props {
		if p.Name == name {
			return p
		}
	}
	return vobject.Prop{}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func stampUTC() string { return time.Now().UTC().Format("20060102T150405Z") }

func wrapVCALENDAR(inner string) string {
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//mailbox-cli//EN",
		inner,
		"END:VCALENDAR",
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// ponytail: no RFC 5545 line folding (>75 octets); SOGo accepts unfolded lines.
func foldless(s string) string { return s }

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := fmt.Sprintf("%x", b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
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

func (cal *Cal) reportEvents(start, stop time.Time, expand bool) []*format.OM {
	inner := ""
	timeRange := fmt.Sprintf("\n        <c:time-range start=%q end=%q/>", icsRange(start), icsRange(stop))
	if expand {
		inner = timeRange + fmt.Sprintf("\n        <c:expand start=%q end=%q/>", icsRange(start), icsRange(stop))
	} else {
		inner = timeRange
	}
	query := fmt.Sprintf(eventsQueryTpl, inner)
	raw, status, err := cal.client.Report(cal.acct.KalenderURL, query, "1")
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
		row, err := eventFields(props, "", has)
		if err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func findHas(props []vobject.Prop, name string) bool {
	for _, p := range props {
		if p.Name == name {
			return true
		}
	}
	return false
}

func (cal *Cal) Events(start, end string) ([]*format.OM, error) {
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
	rows := cal.reportEvents(begin, stop, true)
	sortRows(rows, func(a, b *format.OM) bool { return strOr(a.Get("start")) < strOr(b.Get("start")) })
	return rows, nil
}

func sortRows(rows []*format.OM, less func(a, b *format.OM) bool) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func (cal *Cal) Event(uid string) (*format.OM, error) {
	props, err := cal.lookupEvent(uid)
	if err != nil {
		return nil, err
	}
	has := map[string]bool{"dtstart": findHas(props, "DTSTART"), "dtend": findHas(props, "DTEND")}
	return eventFields(props, uid, has)
}

func (cal *Cal) lookupEvent(uid string) ([]vobject.Prop, error) {
	eventURL := strings.TrimRight(cal.acct.KalenderURL, "/") + "/" + url.PathEscape(uid) + ".ics"
	if text, status, err := cal.client.Get(eventURL); err == nil && status == 200 {
		if props := vobject.Component(text, "VEVENT"); props != nil {
			return props, nil
		}
	}
	begin := time.Now().In(TZ).Add(-730 * 24 * time.Hour)
	stop := time.Now().In(TZ).Add(730 * 24 * time.Hour)
	type hit struct {
		full  string
		props []vobject.Prop
	}
	var scored []hit
	for _, resp := range dav.ParseMultistatus(cal.reportRaw(begin, stop), "calendar-data") {
		props := vobject.Component(resp.Data, "VEVENT")
		full := vobject.First(props, "UID")
		if uidMatches(full, uid) {
			scored = append(scored, hit{full: full, props: props})
		}
	}
	if len(scored) == 0 {
		return nil, fmt.Errorf("event not found: %s", uid)
	}
	unique := map[string][]vobject.Prop{}
	var order []string
	for _, h := range scored {
		if _, seen := unique[h.full]; !seen {
			order = append(order, h.full)
		}
		unique[h.full] = h.props
	}
	if len(unique) > 1 {
		sortStrings(order)
		return nil, fmt.Errorf("ambiguous event id %q, matches:\n%s", uid, strings.Join(order, "\n"))
	}
	return unique[order[0]], nil
}

func (cal *Cal) reportRaw(start, stop time.Time) []byte {
	inner := fmt.Sprintf("\n        <c:time-range start=%q end=%q/>", icsRange(start), icsRange(stop))
	query := fmt.Sprintf(eventsQueryTpl, inner)
	raw, status, err := cal.client.Report(cal.acct.KalenderURL, query, "1")
	if err != nil || (status != 200 && status != 207) {
		return nil
	}
	return raw
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (cal *Cal) CreateEvent(title, start, end string, allDay bool) (string, error) {
	beginW, err := parseWhen(start)
	if err != nil {
		return "", err
	}
	uid := newUUID()
	var lines []string
	lines = append(lines, "BEGIN:VEVENT", "UID:"+uid, "DTSTAMP:"+stampUTC(),
		"SUMMARY:"+vobject.Escape(title))
	if allDay {
		startDate := beginW.t.Format("2006-01-02")
		if !beginW.isDate {
			startDate = beginW.t.In(TZ).Format("2006-01-02")
		}
		endDate := startDate
		if end != "" {
			stopW, err := parseWhen(end)
			if err != nil {
				return "", err
			}
			endDate = stopW.t.In(TZ).Add(24 * time.Hour).Format("2006-01-02")
			if stopW.isDate {
				endDate = stopW.t.Add(24 * time.Hour).Format("2006-01-02")
			}
		} else {
			d, _ := time.Parse("2006-01-02", startDate)
			endDate = d.Add(24 * time.Hour).Format("2006-01-02")
		}
		lines = append(lines,
			"DTSTART;VALUE=DATE:"+startDate,
			"DTEND;VALUE=DATE:"+endDate)
	} else {
		begin := beginW.t
		if beginW.isDate {
			begin = time.Date(beginW.t.Year(), beginW.t.Month(), beginW.t.Day(), 0, 0, 0, 0, TZ)
		}
		stop := begin.Add(time.Hour)
		if end != "" {
			stopW, err := parseWhen(end)
			if err != nil {
				return "", err
			}
			stop = stopW.t
			if stopW.isDate {
				stop = time.Date(stopW.t.Year(), stopW.t.Month(), stopW.t.Day(), 0, 0, 0, 0, TZ)
			}
		}
		// ponytail: serialize timed events as UTC instead of TZID+VTIMEZONE.
		lines = append(lines,
			"DTSTART:"+begin.UTC().Format("20060102T150405Z"),
			"DTEND:"+stop.UTC().Format("20060102T150405Z"))
	}
	lines = append(lines, "END:VEVENT")
	putURL := strings.TrimRight(cal.acct.KalenderURL, "/") + "/" + uid + ".ics"
	status, err := cal.client.Put(putURL, wrapVCALENDAR(strings.Join(lines, "\r\n")),
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	if err != nil || (status != 200 && status != 201 && status != 204) {
		return "", fmt.Errorf("CalDAV put failed: %d", status)
	}
	return uid, nil
}

func (cal *Cal) Tasks() ([]*format.OM, error) {
	raw, status, err := cal.client.Report(cal.acct.AufgabenURL, todosQuery, "1")
	if err != nil || (status != 200 && status != 207) {
		return nil, fmt.Errorf("CalDAV report failed: %d", status)
	}
	var rows []*format.OM
	for _, resp := range dav.ParseMultistatus(raw, "calendar-data") {
		props := vobject.Component(resp.Data, "VTODO")
		if props == nil {
			continue
		}
		if strings.EqualFold(vobject.First(props, "STATUS"), "COMPLETED") {
			continue
		}
		statusVal := vobject.First(props, "STATUS")
		if statusVal == "" {
			statusVal = "NEEDS-ACTION"
		}
		rows = append(rows, format.NewOM(
			"id", shortID(vobject.First(props, "UID")),
			"due", asLocal(findProp(props, "DUE"), findHas(props, "DUE")),
			"status", statusVal,
			"summary", vobject.First(props, "SUMMARY"),
		))
	}
	sortRows(rows, func(a, b *format.OM) bool {
		ka := strOr(a.Get("due")) + "|" + strOr(a.Get("summary"))
		kb := strOr(b.Get("due")) + "|" + strOr(b.Get("summary"))
		return ka < kb
	})
	return rows, nil
}

const todosQuery = `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VTODO"/>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`

func (cal *Cal) CreateTask(title, due string) (string, error) {
	uid := newUUID()
	lines := []string{"BEGIN:VTODO", "UID:" + uid, "DTSTAMP:" + stampUTC(),
		"SUMMARY:" + vobject.Escape(title)}
	if due != "" {
		w, err := parseWhen(due)
		if err != nil {
			return "", err
		}
		if w.isDate {
			lines = append(lines, "DUE;VALUE=DATE:"+w.t.Format("2006-01-02"))
		} else {
			lines = append(lines, "DUE:"+w.t.UTC().Format("20060102T150405Z"))
		}
	}
	lines = append(lines, "END:VTODO")
	putURL := strings.TrimRight(cal.acct.AufgabenURL, "/") + "/" + uid + ".ics"
	status, err := cal.client.Put(putURL, wrapVCALENDAR(strings.Join(lines, "\r\n")),
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	if err != nil || (status != 200 && status != 201 && status != 204) {
		return "", fmt.Errorf("CalDAV put failed: %d", status)
	}
	return uid, nil
}

func (cal *Cal) CompleteTask(uid string) error {
	raw, status, err := cal.client.Report(cal.acct.AufgabenURL, todosQuery, "1")
	if err != nil || (status != 200 && status != 207) {
		return fmt.Errorf("CalDAV report failed: %d", status)
	}
	var matchResp dav.Response
	var matchProps []vobject.Prop
	seen := map[string]bool{}
	n := 0
	for _, resp := range dav.ParseMultistatus(raw, "calendar-data") {
		props := vobject.Component(resp.Data, "VTODO")
		full := vobject.First(props, "UID")
		if !uidMatches(full, uid) || seen[full] {
			continue
		}
		seen[full] = true
		n++
		matchResp, matchProps = resp, props
	}
	if n == 0 {
		return fmt.Errorf("task not found: %s", uid)
	}
	if n > 1 {
		return fmt.Errorf("ambiguous task id %q", uid)
	}
	resp, props := matchResp, matchProps
	{
		// rewrite STATUS / COMPLETED inside the VTODO component
		var out []vobject.Prop
		for _, p := range props {
			switch p.Name {
			case "STATUS":
				p.Value = "COMPLETED"
				out = append(out, p)
			case "COMPLETED":
				continue
			default:
				out = append(out, p)
			}
		}
		out = append(out,
			vobject.Prop{Name: "COMPLETED", Value: stampUTC()},
			vobject.Prop{Name: "STATUS", Value: "COMPLETED"})
		href := resp.Href
		putURL := href
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			base, _ := url.Parse(cal.acct.AufgabenURL)
			if ref, err := url.Parse(href); err == nil {
				putURL = base.ResolveReference(ref).String()
			}
		}
		st, err := cal.client.Put(putURL, wrapVCALENDAR("BEGIN:VTODO\r\n"+vobject.Serialize(out)+"END:VTODO\r\n"),
			map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
		if err != nil || (st != 200 && st != 201 && st != 204) {
			return fmt.Errorf("CalDAV put failed: %d", st)
		}
		return nil
	}
}

var _ = regexp.MustCompile

func strOr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func shortID(uid string) string {
	if len(uid) > 8 {
		return uid[:8]
	}
	return uid
}
