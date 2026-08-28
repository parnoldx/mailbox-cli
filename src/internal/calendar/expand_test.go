package calendar

import (
	"testing"
	"time"

	"mailbox/src/internal/format"
)

func TestExpandWeeklyInDayWindow(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:weekly-team-sync
DTSTART;TZID=Europe/Berlin:20260618T130000
DTEND;TZID=Europe/Berlin:20260618T140000
RRULE:FREQ=WEEKLY;UNTIL=20280608T120000Z
SUMMARY:Weekly Team Sync
END:VEVENT
END:VCALENDAR`
	from := time.Date(2026, 9, 3, 0, 0, 0, 0, TZ)
	to := from.Add(24 * time.Hour)
	rows := eventsFromICS(ics, "Work", from, to)
	if len(rows) != 1 {
		t.Fatalf("got %d rows %v", len(rows), summaries(rows))
	}
	if strOr(rows[0].Get("summary")) != "Weekly Team Sync" {
		t.Fatalf("summary %v", rows[0].Get("summary"))
	}
	if strOr(rows[0].Get("start")) != "2026-09-03 13:00" {
		t.Fatalf("start %v", rows[0].Get("start"))
	}
}

func TestExpandThirdThursdayNotFourth(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:monthly-meeting
DTSTART;TZID=Europe/Berlin:20230119T133000
DTEND;TZID=Europe/Berlin:20230119T150000
RRULE:FREQ=MONTHLY;BYDAY=3TH
SUMMARY:Monthly Project Meeting
END:VEVENT
END:VCALENDAR`
	from := time.Date(2026, 7, 23, 0, 0, 0, 0, TZ)
	to := from.Add(24 * time.Hour)
	if rows := eventsFromICS(ics, "Work", from, to); len(rows) != 0 {
		t.Fatalf("4th Thursday must not match 3TH, got %v", summaries(rows))
	}
	from = time.Date(2026, 7, 16, 0, 0, 0, 0, TZ)
	to = from.Add(24 * time.Hour)
	rows := eventsFromICS(ics, "Work", from, to)
	if len(rows) != 1 || strOr(rows[0].Get("start")) != "2026-07-16 13:30" {
		t.Fatalf("3rd Thursday %v", summaries(rows))
	}
}

func TestExpandWeekWindow(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:weekly-taskforce
DTSTART;TZID=Europe/Berlin:20260610T100000
DTEND;TZID=Europe/Berlin:20260610T110000
RRULE:FREQ=WEEKLY;UNTIL=20260907T060000Z
SUMMARY:Weekly Taskforce
END:VEVENT
BEGIN:VEVENT
UID:mig
DTSTART;TZID=Europe/Berlin:20260810T130000
DTEND;TZID=Europe/Berlin:20260810T140000
RRULE:FREQ=WEEKLY;UNTIL=20261130T130000Z
SUMMARY:Server Upgrade
END:VEVENT
END:VCALENDAR`
	from := time.Date(2026, 8, 31, 0, 0, 0, 0, TZ)
	to := from.AddDate(0, 0, 7)
	rows := eventsFromICS(ics, "Work", from, to)
	got := map[string]string{}
	for _, row := range rows {
		got[strOr(row.Get("summary"))] = strOr(row.Get("start"))
	}
	if got["Weekly Taskforce"] != "2026-09-02 10:00" {
		t.Fatalf("taskforce %q in %#v", got["Weekly Taskforce"], got)
	}
	if got["Server Upgrade"] != "2026-08-31 13:00" {
		t.Fatalf("mig %q in %#v", got["Server Upgrade"], got)
	}
}

func TestExpandOverrideAndExdate(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:series
DTSTART;TZID=Europe/Berlin:20260817T130000
DTEND;TZID=Europe/Berlin:20260817T140000
RRULE:FREQ=WEEKLY
EXDATE;TZID=Europe/Berlin:20260824T130000
SUMMARY:Standup
END:VEVENT
BEGIN:VEVENT
UID:series
RECURRENCE-ID;TZID=Europe/Berlin:20260831T130000
DTSTART;TZID=Europe/Berlin:20260831T150000
DTEND;TZID=Europe/Berlin:20260831T160000
SUMMARY:Standup (moved)
END:VEVENT
END:VCALENDAR`
	from := time.Date(2026, 8, 17, 0, 0, 0, 0, TZ)
	to := time.Date(2026, 9, 7, 0, 0, 0, 0, TZ)
	rows := eventsFromICS(ics, "Work", from, to)
	got := map[string]string{}
	for _, row := range rows {
		got[strOr(row.Get("start"))] = strOr(row.Get("summary"))
	}
	if _, ok := got["2026-08-24 13:00"]; ok {
		t.Fatalf("EXDATE leaked %#v", got)
	}
	if got["2026-08-31 15:00"] != "Standup (moved)" {
		t.Fatalf("override %#v", got)
	}
	if got["2026-08-17 13:00"] != "Standup" {
		t.Fatalf("master instance %#v", got)
	}
}

func summaries(rows []*format.OM) []string {
	var out []string
	for _, row := range rows {
		out = append(out, strOr(row.Get("summary"))+"@"+strOr(row.Get("start")))
	}
	return out
}
