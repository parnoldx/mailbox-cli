package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/sync/davsync"
)

// A bare date is an all-day entry and a date with a clock on it is an
// appointment. "Friday" does not mean midnight on Friday, and storing it as one
// makes every client show a time nobody meant.
func TestEventAddReadsADateAsAllDayAndATimeAsAnAppointment(t *testing.T) {
	d, f, _ := seedTasks(t)

	resp := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Urlaub", "start": "2026-09-01", "end": "2026-09-05",
	})
	got := resp.Data.(map[string]any)
	if got["summary"] != "Urlaub" || got["calendar"] != "Kalender" {
		t.Fatalf("add gave %+v", got)
	}
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "DTSTART;VALUE=DATE:20260901") {
		t.Errorf("an all-day event was stored with a time:\n%s", raw)
	}

	resp = mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Zahnarzt", "start": "2026-09-01 08:10", "end": "2026-09-01 09:00",
	})
	if _, ok := resp.Data.(map[string]any); !ok {
		t.Fatalf("add gave %T", resp.Data)
	}
	if raw := eventNamed(t, f, "Zahnarzt"); strings.Contains(raw, "DTSTART;VALUE=DATE:") {
		t.Errorf("an appointment was stored as all-day:\n%s", raw)
	}
}

// With no --end an appointment lasts an hour and an all-day entry a day, which
// are the two answers that are right most of the time.
func TestEventAddWithoutAnEndLastsAnHour(t *testing.T) {
	d, f, _ := seedTasks(t)
	mustAsk(t, d, []string{"event", "add"},
		map[string]any{"positional": "Standup", "start": "2026-09-01 09:00"})
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "T090000") || !strings.Contains(raw, "T100000") {
		t.Errorf("an hour was not the default:\n%s", raw)
	}
}

func TestEventAddNeedsAStartAndASummary(t *testing.T) {
	d, _, _ := seedTasks(t)
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{"positional": "Urlaub"}); resp.OK ||
		!strings.Contains(resp.Error, "--start") {
		t.Errorf("resp = %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{"start": "2026-09-01"}); resp.OK ||
		!strings.Contains(resp.Error, "summary") {
		t.Errorf("resp = %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "2026-09-01 10:00", "end": "2026-09-01 09:00",
	}); resp.OK || !strings.Contains(resp.Error, "--end") {
		t.Errorf("an end before the start was accepted: %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "next friday",
	}); resp.OK || !strings.Contains(resp.Error, "2026-09-01") {
		t.Errorf("resp = %+v", resp)
	}
}

// An edit changes only what it was given: fixing a summary must not move the
// event, which for a repeating one would move every instance of it.
func TestEventEditChangesOnlyWhatItWasGiven(t *testing.T) {
	d, f, _ := seedTasks(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Zahnarzt", "start": "2026-09-01 08:10", "end": "2026-09-01 09:00",
	}).Data.(map[string]any)

	mustAsk(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "title": "Zahnreinigung",
	})
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "Zahnreinigung") {
		t.Errorf("the summary did not change:\n%s", raw)
	}
	if !strings.Contains(raw, "T081000") {
		t.Errorf("renaming moved the event:\n%s", raw)
	}

	// And an edit that names nothing is a mistake, not a no-op that reports
	// success.
	if resp := ask(t, d, []string{"event", "edit"},
		map[string]any{"positional": added["id"]}); resp.OK {
		t.Errorf("an empty edit succeeded: %+v", resp)
	}
}

func TestEventDeleteTakesItOffTheCalendar(t *testing.T) {
	d, f, _ := seedTasks(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Urlaub", "start": "2026-09-01",
	}).Data.(map[string]any)

	resp := mustAsk(t, d, []string{"event", "delete"}, map[string]any{"positional": added["id"]})
	if got := resp.Data.(map[string]any); got["state"] != "deleted" {
		t.Fatalf("delete gave %+v", got)
	}
	if n := len(eventsOn(f)); n != 0 {
		t.Errorf("%d events left on the calendar", n)
	}
}

// The habits record lives on a calendar of its own and is not somewhere an
// appointment belongs (ADR-0018).
func TestEventAddNamesTheCalendarWhenThereAreSeveral(t *testing.T) {
	d, _, _ := seedTasks(t)
	resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "2026-09-01", "calendar": "Nope",
	})
	if resp.OK || !strings.Contains(resp.Error, "Nope") {
		t.Errorf("resp = %+v", resp)
	}
}

// eventsOn reads back what is actually on the calendar, through the same
// sync-collection call the reconciler uses.
func eventsOn(f *davsync.Fake) []string {
	changes, err := f.Sync(context.Background(), testCalURL, "")
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range changes.Items {
		if !c.Deleted {
			out = append(out, c.Data)
		}
	}
	return out
}

func onlyEvent(t *testing.T, f *davsync.Fake) string {
	t.Helper()
	got := eventsOn(f)
	if len(got) != 1 {
		t.Fatalf("%d events on the calendar, want 1", len(got))
	}
	return got[0]
}

// eventNamed finds one by its summary. The fake hands its objects back in map
// order, so picking by position is picking at random.
func eventNamed(t *testing.T, f *davsync.Fake, summary string) string {
	t.Helper()
	for _, raw := range eventsOn(f) {
		if strings.Contains(raw, "SUMMARY:"+summary) {
			return raw
		}
	}
	t.Fatalf("no event called %q on the calendar", summary)
	return ""
}

// A rule, a reminder and a link are things a caller picks in one breath with
// the time, so they are written with it rather than dropped and typed again in
// somebody else's client.
func TestEventAddWritesTheRuleTheReminderAndTheLink(t *testing.T) {
	d, f, _ := seedTasks(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": "2026-09-01 09:00",
		"repeat": "weekdays", "alarm": "5,60", "url": "https://meet.example.org/r?a=1,2",
	}).Data.(map[string]any)

	raw := onlyEvent(t, f)
	for _, want := range []string{
		"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
		"TRIGGER:-PT5M", "TRIGGER:-PT1H",
		// The link is written as a URI, so the comma in its query survives
		// instead of being escaped as text.
		"URL:https://meet.example.org/r?a=1,2",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("the event does not hold %q:\n%s", want, raw)
		}
	}

	// The agenda carries the link on every instance, so a caller listing the
	// week can join the call without reading each entry whole.
	agenda := mustAsk(t, d, []string{"agenda"},
		map[string]any{"from": "2026-09-01", "days": 3}).Data.([]occurrence)
	if len(agenda) == 0 || agenda[0].URL != "https://meet.example.org/r?a=1,2" {
		t.Errorf("agenda = %+v", agenda)
	}

	// And it reads back, which is what a caller filling in a form again needs.
	view := mustAsk(t, d, []string{"event", "view"},
		map[string]any{"positional": added["id"]}).Data.(event)
	if view.Repeat != "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" || view.URL != "https://meet.example.org/r?a=1,2" {
		t.Errorf("view = %+v", view)
	}
	if len(view.Alarms) != 2 || view.Alarms[0] != 5 || view.Alarms[1] != 60 {
		t.Errorf("alarms = %v", view.Alarms)
	}
	if !view.Recurring {
		t.Errorf("a weekday rule did not make it recurring: %+v", view)
	}
}

// A rule nobody could act on is refused here rather than at the server, which
// answers a bad RRULE with a 400 and nothing in it worth reading.
func TestEventAddRefusesARuleItCannotRead(t *testing.T) {
	d, _, _ := seedTasks(t)
	resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": "2026-09-01 09:00", "repeat": "every other thursday",
	})
	if resp.OK || resp.Code != "usage" || !strings.Contains(resp.Error, "--repeat") {
		t.Errorf("resp = %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": "2026-09-01 09:00", "alarm": "soon",
	}); resp.OK || !strings.Contains(resp.Error, "--alarm") {
		t.Errorf("resp = %+v", resp)
	}
}

// An edit names what it changes, and "none" is how a caller names taking one
// off — otherwise a rule could be added and never removed.
func TestEventEditTakesTheRuleAndTheRemindersOff(t *testing.T) {
	d, f, _ := seedTasks(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": "2026-09-01 09:00",
		"repeat": "weekly", "alarm": "15", "url": "https://example.org/",
	}).Data.(map[string]any)

	// Changing the summary leaves all three where they are.
	mustAsk(t, d, []string{"event", "edit"},
		map[string]any{"positional": added["id"], "title": "Weekly"})
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "RRULE:") || !strings.Contains(raw, "TRIGGER:-PT15M") ||
		!strings.Contains(raw, "URL:https://example.org/") {
		t.Fatalf("a rename dropped the rule, the reminder or the link:\n%s", raw)
	}

	mustAsk(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "repeat": "none", "alarm": "none", "url": "none",
	})
	raw = onlyEvent(t, f)
	if strings.Contains(raw, "RRULE:") || strings.Contains(raw, "VALARM") ||
		strings.Contains(raw, "URL:") {
		t.Errorf("none left something behind:\n%s", raw)
	}
}
