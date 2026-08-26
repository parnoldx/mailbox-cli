package calendar

import (
	"strings"
	"testing"
	"time"

	"mailbox/src/internal/vobject"
)

func TestParseCalendarsXML(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:CAL="urn:ietf:params:xml:ns:caldav" xmlns:APPLE="http://apple.com/ns/ical/">
  <D:response>
    <D:href>/caldav/</D:href>
    <D:propstat><D:prop>
      <D:displayname>Calendars</D:displayname>
      <D:resourcetype><D:collection/></D:resourcetype>
    </D:prop></D:propstat>
  </D:response>
  <D:response>
    <D:href>/caldav/Y2FsOi8vMC8zMQ/</D:href>
    <D:propstat><D:prop>
      <D:displayname>Kalender</D:displayname>
      <calendar-color xmlns="http://apple.com/ns/ical/">#CEE7FFFF</calendar-color>
      <D:resourcetype><D:collection/><CAL:calendar/></D:resourcetype>
      <supported-calendar-component-set xmlns="urn:ietf:params:xml:ns:caldav">
        <CAL:comp name="VEVENT"/>
      </supported-calendar-component-set>
    </D:prop></D:propstat>
  </D:response>
  <D:response>
    <D:href>/caldav/MzM/</D:href>
    <D:propstat><D:prop>
      <D:displayname>Aufgaben</D:displayname>
      <D:resourcetype><D:collection/><CAL:calendar/></D:resourcetype>
      <supported-calendar-component-set xmlns="urn:ietf:params:xml:ns:caldav">
        <CAL:comp name="VTODO"/>
      </supported-calendar-component-set>
    </D:prop></D:propstat>
  </D:response>
  <D:response>
    <D:href>/caldav/maybe/</D:href>
    <D:propstat><D:prop>
      <D:displayname>Maybe</D:displayname>
      <D:resourcetype><D:collection/><CAL:calendar/></D:resourcetype>
      <supported-calendar-component-set xmlns="urn:ietf:params:xml:ns:caldav">
        <CAL:comp name="VEVENT"/>
      </supported-calendar-component-set>
    </D:prop></D:propstat>
  </D:response>
  <D:response>
    <D:href>/caldav/habits/</D:href>
    <D:propstat><D:prop>
      <D:displayname>mailbox-habits</D:displayname>
      <D:resourcetype><D:collection/><CAL:calendar/></D:resourcetype>
      <supported-calendar-component-set xmlns="urn:ietf:params:xml:ns:caldav">
        <CAL:comp name="VEVENT"/>
      </supported-calendar-component-set>
    </D:prop></D:propstat>
  </D:response>
</D:multistatus>`)
	cols := parseCalendarsXML(raw, "https://dav.mailbox.org/caldav/")
	if len(cols) != 4 {
		t.Fatalf("got %d cols %#v", len(cols), cols)
	}
	var names []string
	var event []string
	for _, c := range cols {
		names = append(names, c.Name)
		if !isHabits(c) && isEventCal(c) {
			event = append(event, c.Name)
		}
	}
	if strings.Join(names, ",") != "Kalender,Aufgaben,Maybe,mailbox-habits" {
		t.Fatalf("names %q", names)
	}
	if strings.Join(event, ",") != "Kalender,Maybe" {
		t.Fatalf("event cals %q", event)
	}
	if cols[0].Color != "#CEE7FFFF" {
		t.Fatalf("color %q", cols[0].Color)
	}
	if !strings.HasSuffix(cols[0].URL, "/caldav/Y2FsOi8vMC8zMQ/") {
		t.Fatalf("url %q", cols[0].URL)
	}
}

func TestRruleFromAlias(t *testing.T) {
	start := time.Date(2026, 8, 22, 9, 0, 0, 0, TZ)
	got, err := rruleFromAlias("every_weekday", start, "", 0)
	if err != nil || got != "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = rruleFromAlias("every_day_of_month", start, "2026-12-31", 0)
	if err != nil || got != "FREQ=MONTHLY;BYMONTHDAY=22;UNTIL=20261231" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = rruleFromAlias("every_week", start, "", 4)
	if err != nil || got != "FREQ=WEEKLY;COUNT=4" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := rruleFromAlias("weekly", start, "", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestIcsTrigger(t *testing.T) {
	got, err := icsTrigger("10m")
	if err != nil || got != "-PT10M" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = icsTrigger("2h")
	if err != nil || got != "-PT2H" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = icsTrigger("3d")
	if err != nil || got != "-P3D" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := icsTrigger("10"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDays(t *testing.T) {
	got, err := parseDays("mon,Wed,5")
	if err != nil || strings.Join(got, ",") != "mon,wed,fri" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = parseDays("")
	if err != nil || len(got) != 7 {
		t.Fatalf("default %q %v", got, err)
	}
	if _, err := parseDays("funday"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHabitCompleteUncomplete(t *testing.T) {
	h := habit{ID: "abc", Name: "Gym", Days: []string{"mon", "wed"}}
	h.complete("2026-08-26")
	h.complete("2026-08-26")
	if !h.hasDone("2026-08-26") || len(h.Done) != 1 {
		t.Fatalf("done %#v", h.Done)
	}
	h.uncomplete("2026-08-26")
	if h.hasDone("2026-08-26") {
		t.Fatal("still done")
	}
	bag := habitBag{Habits: []habit{{ID: "aaaaaaaa-1111", Name: "A"}, {ID: "bbbbbbbb-2222", Name: "B"}}}
	hit, err := bag.find("aaaa")
	if err != nil || hit.Name != "A" {
		t.Fatalf("find %v %v", hit, err)
	}
	if _, err := bag.find("zzz"); err == nil {
		t.Fatal("expected missing")
	}
}

func TestCombineEventWhen(t *testing.T) {
	start, end, allDay, err := CombineEventWhen("2026-09-02", "", "", "", false)
	if err != nil || !allDay || start != "2026-09-02" || end != "2026-09-02" {
		t.Fatalf("all-day %s %s %v %v", start, end, allDay, err)
	}
	start, end, allDay, err = CombineEventWhen("2026-09-02", "14:00", "", "", false)
	if err != nil || allDay || start != "2026-09-02T14:00" || end != "2026-09-02T15:00" {
		t.Fatalf("hour %s %s %v %v", start, end, allDay, err)
	}
	_, _, _, err = CombineEventWhen("2026-09-04", "", "2026-09-02", "", false)
	if err == nil {
		t.Fatal("expected ends-on before starts-on")
	}
}

func TestEventTimePropsAllDay(t *testing.T) {
	ds, de, _, err := eventTimeProps("2026-08-22", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Value != "20260822" || de.Value != "20260823" {
		t.Fatalf("start=%s end=%s", ds.Value, de.Value)
	}
	if !strings.Contains(ds.Params, "VALUE=DATE") {
		t.Fatalf("params %q", ds.Params)
	}
}

func TestEventFieldsCircle(t *testing.T) {
	props := []vobject.Prop{
		{Name: "UID", Value: "abcdef12-xxxx"},
		{Name: "SUMMARY", Value: "Dentist"},
		{Name: "DTSTART", Value: "20260822T070000Z"},
		{Name: "PRIORITY", Value: "1"},
		{Name: "LOCATION", Value: "Clinic"},
	}
	row, err := eventFields(props, "", "Kalender", map[string]bool{"dtstart": true})
	if err != nil {
		t.Fatal(err)
	}
	if row.Get("circle") != true || row.Get("calendar") != "Kalender" || row.Get("location") != "Clinic" {
		t.Fatalf("%v", row.Vals)
	}
	if row.Get("id") != "abcdef12" {
		t.Fatalf("id %v", row.Get("id"))
	}
}
