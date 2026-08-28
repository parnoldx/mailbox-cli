// Package calendar: CalDAV over raw REPORT/GET/PUT (replaces the caldav+icalendar libs).
package calendar

import (
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/dav"
	"mailbox/src/internal/format"
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

const (
	habitsCalName = "mailbox-habits"
	habitsUID     = "mailbox-habits"
)

type Cal struct {
	acct       *config.Account
	client     *dav.Client
	baseURL    string
	cols       []Collection
	discovered bool
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

type Collection struct {
	Name     string
	URL      string
	Color    string
	Comps    []string
	Calendar bool
	client   *dav.Client
}

func (c Collection) hasComp(name string) bool {
	if len(c.Comps) == 0 {
		return true
	}
	for _, comp := range c.Comps {
		if strings.EqualFold(comp, name) {
			return true
		}
	}
	return false
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
	dateStr string
}

func requireDate(name, value string) error {
	if len(value) != 10 {
		return fmt.Errorf("invalid %s %q; use YYYY-MM-DD", name, value)
	}
	_, err := time.ParseInLocation("2006-01-02", value, TZ)
	if err != nil {
		return fmt.Errorf("invalid %s %q; use YYYY-MM-DD", name, value)
	}
	return nil
}

func requireClock(name, value string) error {
	if _, err := time.Parse("15:04", value); err != nil {
		return fmt.Errorf("invalid %s: %s", name, value)
	}
	return nil
}

// CombineEventWhen turns hey-style date/time flags into parseWhen strings.
func CombineEventWhen(startsOn, startTime, endsOn, endTime string, allDay bool) (start, end string, isAllDay bool, err error) {
	if startsOn == "" {
		startsOn = time.Now().In(TZ).Format("2006-01-02")
	}
	if err := requireDate("starts-on date", startsOn); err != nil {
		return "", "", false, err
	}
	if endsOn == "" {
		endsOn = startsOn
	} else if err := requireDate("ends-on date", endsOn); err != nil {
		return "", "", false, err
	}
	if endsOn < startsOn {
		return "", "", false, fmt.Errorf("ends-on %s is before starts-on %s", endsOn, startsOn)
	}
	if allDay || startTime == "" {
		return startsOn, endsOn, true, nil
	}
	if err := requireClock("start-time", startTime); err != nil {
		return "", "", false, err
	}
	if endTime == "" {
		t, _ := time.Parse("15:04", startTime)
		endTime = t.Add(time.Hour).Format("15:04")
	} else if err := requireClock("end-time", endTime); err != nil {
		return "", "", false, err
	}
	return startsOn + "T" + startTime, endsOn + "T" + endTime, false, nil
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
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, TZ), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable datetime %q", v)
}

func findProp(props []vobject.Prop, name string) vobject.Prop {
	for _, p := range props {
		if p.Name == name {
			return p
		}
	}
	return vobject.Prop{}
}

func findHas(props []vobject.Prop, name string) bool {
	for _, p := range props {
		if p.Name == name {
			return true
		}
	}
	return false
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

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := fmt.Sprintf("%x", b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

func sortRows(rows []*format.OM, less func(a, b *format.OM) bool) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

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

func absURL(base, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return u.ResolveReference(ref).String()
}

func (cal *Cal) dav(col Collection) *dav.Client {
	if col.client != nil {
		return col.client
	}
	return cal.client
}

func (cal *Cal) putICS(putURL, inner string) error {
	return cal.putICSClient(cal.client, putURL, inner)
}

func (cal *Cal) putICSClient(client *dav.Client, putURL, inner string) error {
	if client == nil {
		client = cal.client
	}
	status, err := client.Put(putURL, wrapVCALENDAR(inner),
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	if err != nil || (status != 200 && status != 201 && status != 204) {
		return fmt.Errorf("CalDAV put failed: %d", status)
	}
	return nil
}

func (cal *Cal) homeURL() string {
	if cal.acct.KalenderURL != "" {
		u := strings.TrimRight(cal.acct.KalenderURL, "/")
		if i := strings.LastIndex(u, "/"); i > 0 {
			return u[:i+1]
		}
	}
	return cal.baseURL
}

const calPropfind = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="http://apple.com/ns/ical/">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
    <c:supported-calendar-component-set/>
    <a:calendar-color/>
  </d:prop>
</d:propfind>`

func (cal *Cal) collections() ([]Collection, error) {
	if cal.discovered {
		return cal.cols, nil
	}
	raw, status, err := cal.client.Propfind(cal.homeURL(), calPropfind, "1")
	if err == nil && (status == 200 || status == 207) {
		cal.cols = parseCalendarsXML(raw, cal.homeURL())
	}
	if len(cal.cols) == 0 {
		cal.cols = []Collection{{
			Name: "Kalender", URL: cal.acct.KalenderURL, Calendar: true, Comps: []string{"VEVENT"},
		}}
	}
	cal.cols = append(cal.cols, cal.extraCols()...)
	cal.discovered = true
	return cal.cols, nil
}

func (cal *Cal) extraCols() []Collection {
	skip := urlHost(cal.acct.KalenderURL)
	var out []Collection
	for _, e := range cal.acct.ExtraCals {
		if e.Name == "" || e.URL == "" {
			continue
		}
		if skip != "" && urlHost(e.URL) == skip {
			continue
		}
		user := e.Username
		if user == "" {
			user = cal.acct.Email
		}
		out = append(out, Collection{
			Name: e.Name, URL: e.URL, Color: e.Color,
			Calendar: true, Comps: []string{"VEVENT"},
			client: dav.New(user, e.Password),
		})
	}
	return out
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func (cal *Cal) Calendars() ([]*format.OM, error) {
	cols, err := cal.eventCals()
	if err != nil {
		return nil, err
	}
	var rows []*format.OM
	for _, c := range cols {
		rows = append(rows, format.NewOM(
			"name", c.Name,
			"color", c.Color,
		))
	}
	sortRows(rows, func(a, b *format.OM) bool { return strOr(a.Get("name")) < strOr(b.Get("name")) })
	return rows, nil
}

func (cal *Cal) eventCals() ([]Collection, error) {
	cols, err := cal.collections()
	if err != nil {
		return nil, err
	}
	var out []Collection
	for _, c := range cols {
		if !isHabits(c) && isEventCal(c) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		out = []Collection{{
			Name: "Kalender", URL: cal.acct.KalenderURL, Calendar: true, Comps: []string{"VEVENT"},
		}}
	}
	return out, nil
}

func isHabits(c Collection) bool {
	return strings.EqualFold(c.Name, habitsCalName)
}

func isEventCal(c Collection) bool {
	if !c.Calendar && len(c.Comps) == 0 {
		return false
	}
	if strings.EqualFold(c.Name, "Aufgaben") {
		return false
	}
	if len(c.Comps) == 0 {
		return c.Calendar
	}
	return c.hasComp("VEVENT") && !c.hasComp("VTODO")
}

func (cal *Cal) pickEventCal(name string) (Collection, error) {
	cols, err := cal.eventCals()
	if err != nil {
		return Collection{}, err
	}
	if name != "" {
		return matchCal(cols, name)
	}
	for _, c := range cols {
		if strings.EqualFold(c.Name, "Kalender") {
			return c, nil
		}
	}
	return cols[0], nil
}

func matchCal(cols []Collection, q string) (Collection, error) {
	ql := strings.ToLower(q)
	for _, c := range cols {
		if strings.EqualFold(c.Name, q) {
			return c, nil
		}
	}
	var hits []Collection
	for _, c := range cols {
		if strings.HasPrefix(strings.ToLower(c.Name), ql) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) == 0 {
		return Collection{}, fmt.Errorf("calendar not found: %s", q)
	}
	return Collection{}, fmt.Errorf("ambiguous calendar %q", q)
}

func parseCalendarsXML(raw []byte, base string) []Collection {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var out []Collection
	var cur Collection
	var href string
	capture := ""
	inType := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch tt := tok.(type) {
		case xml.StartElement:
			name := dav.LocalName(tok)
			switch name {
			case "response":
				cur = Collection{}
				href = ""
			case "href", "displayname", "calendar-color":
				capture = name
			case "resourcetype":
				inType = true
			case "calendar":
				if inType {
					cur.Calendar = true
				}
			case "comp":
				for _, a := range tt.Attr {
					if a.Name.Local == "name" && a.Value != "" {
						cur.Comps = append(cur.Comps, a.Value)
					}
				}
			}
		case xml.CharData:
			text := string(tt)
			switch capture {
			case "href":
				href += text
			case "displayname":
				cur.Name += text
			case "calendar-color":
				cur.Color += text
			}
		case xml.EndElement:
			name := dav.LocalName(tok)
			switch name {
			case "href", "displayname", "calendar-color":
				capture = ""
			case "resourcetype":
				inType = false
			case "response":
				cur.Name = strings.TrimSpace(cur.Name)
				cur.Color = strings.TrimSpace(cur.Color)
				cur.URL = absURL(base, strings.TrimSpace(href))
				if cur.Name != "" && (cur.Calendar || len(cur.Comps) > 0) {
					out = append(out, cur)
				}
			}
		}
	}
	return out
}

func setProp(props []vobject.Prop, name, params, value string) []vobject.Prop {
	found := false
	var out []vobject.Prop
	for _, p := range props {
		if p.Name != name {
			out = append(out, p)
			continue
		}
		if !found {
			out = append(out, vobject.Prop{Name: name, Params: params, Value: value})
			found = true
		}
	}
	if !found {
		out = append(out, vobject.Prop{Name: name, Params: params, Value: value})
	}
	return out
}

func dropProp(props []vobject.Prop, name string) []vobject.Prop {
	var out []vobject.Prop
	for _, p := range props {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return out
}

func dropAlarm(props []vobject.Prop) []vobject.Prop {
	var out []vobject.Prop
	in := false
	for _, p := range props {
		if p.Name == "BEGIN" && strings.EqualFold(p.Value, "VALARM") {
			in = true
			continue
		}
		if p.Name == "END" && strings.EqualFold(p.Value, "VALARM") {
			in = false
			continue
		}
		if !in {
			out = append(out, p)
		}
	}
	return out
}

var remindSpecRe = regexp.MustCompile(`^(\d+)([mhd])$`)

func icsTrigger(spec string) (string, error) {
	m := remindSpecRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(spec)))
	if m == nil {
		return "", fmt.Errorf("invalid --remind %q; use e.g. 10m, 2h, 3d", spec)
	}
	if m[1] == "0" {
		return "", fmt.Errorf("duration must be positive")
	}
	switch m[2] {
	case "m":
		return "-PT" + m[1] + "M", nil
	case "h":
		return "-PT" + m[1] + "H", nil
	default:
		return "-P" + m[1] + "D", nil
	}
}

func rruleFromAlias(alias string, start time.Time, until string, times int) (string, error) {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(alias), "-", "_"))
	var freq string
	switch key {
	case "every_day":
		freq = "FREQ=DAILY"
	case "every_weekday":
		freq = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
	case "every_week":
		freq = "FREQ=WEEKLY"
	case "every_other_week":
		freq = "FREQ=WEEKLY;INTERVAL=2"
	case "every_day_of_month":
		day := start.In(TZ).Day()
		if day == 0 {
			day = start.Day()
		}
		freq = fmt.Sprintf("FREQ=MONTHLY;BYMONTHDAY=%d", day)
	case "every_year":
		freq = "FREQ=YEARLY"
	default:
		return "", fmt.Errorf("unknown --repeat %q (every_day|every_weekday|every_week|every_other_week|every_day_of_month|every_year)", alias)
	}
	if until != "" {
		w, err := parseWhen(until)
		if err != nil {
			return "", err
		}
		if w.isDate {
			freq += ";UNTIL=" + w.t.Format("20060102")
		} else {
			freq += ";UNTIL=" + w.t.UTC().Format("20060102T150405Z")
		}
	}
	if times > 0 {
		freq += fmt.Sprintf(";COUNT=%d", times)
	}
	return freq, nil
}

func eventTimeProps(start, end string, allDay bool) (ds, de vobject.Prop, begin time.Time, err error) {
	beginW, err := parseWhen(start)
	if err != nil {
		return ds, de, begin, err
	}
	begin = beginW.t
	if allDay {
		startDate := beginW.t.Format("20060102")
		if !beginW.isDate {
			startDate = beginW.t.In(TZ).Format("20060102")
		}
		endDate := startDate
		if end != "" {
			stopW, err := parseWhen(end)
			if err != nil {
				return ds, de, begin, err
			}
			t := stopW.t
			if !stopW.isDate {
				t = stopW.t.In(TZ)
			}
			endDate = t.Add(24 * time.Hour).Format("20060102")
			if stopW.isDate {
				endDate = stopW.t.Add(24 * time.Hour).Format("20060102")
			}
		} else {
			d, _ := time.ParseInLocation("20060102", startDate, TZ)
			endDate = d.Add(24 * time.Hour).Format("20060102")
		}
		return vobject.Prop{Name: "DTSTART", Params: ";VALUE=DATE", Value: startDate},
			vobject.Prop{Name: "DTEND", Params: ";VALUE=DATE", Value: endDate},
			begin, nil
	}
	if beginW.isDate {
		begin = time.Date(beginW.t.Year(), beginW.t.Month(), beginW.t.Day(), 0, 0, 0, 0, TZ)
	}
	stop := begin.Add(time.Hour)
	if end != "" {
		stopW, err := parseWhen(end)
		if err != nil {
			return ds, de, begin, err
		}
		stop = stopW.t
		if stopW.isDate {
			stop = time.Date(stopW.t.Year(), stopW.t.Month(), stopW.t.Day(), 0, 0, 0, 0, TZ)
		}
	}
	// ponytail: serialize timed events as UTC instead of TZID+VTIMEZONE.
	return vobject.Prop{Name: "DTSTART", Value: begin.UTC().Format("20060102T150405Z")},
		vobject.Prop{Name: "DTEND", Value: stop.UTC().Format("20060102T150405Z")},
		begin, nil
}
