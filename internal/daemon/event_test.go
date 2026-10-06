package daemon

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
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
func eventsOn(f *davsync.Fake) []string { return eventsOnCal(f, testCalURL) }

func eventsOnCal(f *davsync.Fake, calURL string) []string {
	changes, err := f.Sync(context.Background(), calURL, "")
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

// graphEventNamed is eventNamed on the Microsoft 365 calendar.
func graphEventNamed(t *testing.T, f *davsync.Fake, summary string) string {
	t.Helper()
	for _, raw := range eventsOnCal(f, testGraphCal) {
		if strings.Contains(raw, "SUMMARY:"+summary) {
			return raw
		}
	}
	t.Fatalf("no event called %q on the Microsoft 365 calendar", summary)
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
	// The reminders ride the row too, so a widget with no VALARM handling
	// of its own can say something as early as the phone does.
	if len(agenda) == 0 || len(agenda[0].Alarms) != 2 || agenda[0].Alarms[0] != 5 || agenda[0].Alarms[1] != 60 {
		t.Errorf("agenda alarms = %+v", agenda)
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

// One instance of a repeating event moves on its own: the rule keeps every
// other Monday, and the moved day is an override beside the master.
func TestEventEditOccurrenceMovesOneInstanceOnly(t *testing.T) {
	d, f, _ := seedTasks(t)
	// The rule's first Monday, derived from mondayAt so the test does not
	// silently rot when the real calendar rolls past a hardcoded date.
	first, err := time.ParseInLocation("2006-01-02", mondayAt(9, 0)[:10], time.Local)
	if err != nil {
		t.Fatal(err)
	}
	day, nextDay, nextWeek := first.Format("2006-01-02"),
		first.AddDate(0, 0, 1).Format("2006-01-02"),
		first.AddDate(0, 0, 7).Format("2006-01-02")
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": mondayAt(9, 0), "repeat": "FREQ=WEEKLY;BYDAY=MO",
	}).Data.(map[string]any)

	if resp := ask(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "occurrence": day, "repeat": "none",
	}); resp.OK || !strings.Contains(resp.Error, "cannot be combined") {
		t.Errorf("an occurrence took a rule of its own: %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "occurrence": nextDay, "title": "x",
	}); resp.OK || !strings.Contains(resp.Error, "no instance") {
		t.Errorf("an empty day was edited: %+v", resp)
	}

	moved := nextDay + " 14:00"
	mustAsk(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "occurrence": day,
		"title": "Standup verschoben", "start": moved,
	})
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "RECURRENCE-ID") || !strings.Contains(raw, "Standup verschoben") {
		t.Errorf("the override is not there:\n%s", raw)
	}
	if !strings.Contains(raw, "RRULE:FREQ=WEEKLY;BYDAY=MO") {
		t.Errorf("the rule did not survive:\n%s", raw)
	}

	// And the agenda says so: the Monday slot is empty, the Tuesday 14:00 is
	// filled, and the next Monday still at 09:00.
	agenda := mustAsk(t, d, []string{"agenda"}, map[string]any{
		"from": day, "days": 10.0,
	}).Data.([]occurrence)
	if len(agenda) != 2 {
		t.Fatalf("agenda holds %d entries, want 2: %v", len(agenda), agenda)
	}
	var days []string
	for _, row := range agenda {
		days = append(days, row.Start[:16])
	}
	sort.Strings(days)
	want := []string{nextDay + "T14:00", nextWeek + "T09:00"}
	if !slices.Equal(days, want) {
		t.Errorf("agenda holds %v, want %v", days, want)
	}
}

func TestEventDeleteOccurrenceTakesOneInstanceOff(t *testing.T) {
	d, f, _ := seedTasks(t)
	first, err := time.ParseInLocation("2006-01-02", mondayAt(9, 0)[:10], time.Local)
	if err != nil {
		t.Fatal(err)
	}
	day, nextWeek := first.Format("2006-01-02"), first.AddDate(0, 0, 7).Format("2006-01-02")
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": mondayAt(9, 0), "repeat": "FREQ=WEEKLY;BYDAY=MO",
	}).Data.(map[string]any)

	mustAsk(t, d, []string{"event", "delete"}, map[string]any{
		"positional": added["id"], "occurrence": day,
	})
	raw := onlyEvent(t, f)
	if !strings.Contains(raw, "STATUS:CANCELLED") {
		t.Errorf("the instance was not cancelled:\n%s", raw)
	}
	if !strings.Contains(raw, "RRULE:") {
		t.Errorf("the whole rule went with the one instance:\n%s", raw)
	}
	agenda := mustAsk(t, d, []string{"agenda"}, map[string]any{
		"from": day, "days": 10.0,
	}).Data.([]occurrence)
	if len(agenda) != 1 || agenda[0].Start[:16] != nextWeek+"T09:00" {
		t.Fatalf("agenda holds %v, want the surviving Monday only", agenda)
	}
}

// mondayAt is next Monday at hour:minute local, the same helper shape the
// vcal tests use: a repeating event needs a weekday the rule can land on.
func mondayAt(hour, minute int) string {
	day := time.Now()
	for day.Weekday() != time.Monday {
		day = day.AddDate(0, 0, 1)
	}
	y, m, d := day.Date()
	if minute == 0 {
		return fmt.Sprintf("%s %02d:%02d", time.Date(y, m, d, 0, 0, 0, 0, time.Local).Format("2006-01-02"), hour, minute)
	}
	return time.Date(y, m, d, hour, minute, 0, 0, time.Local).Format("2006-01-02 15:04")
}

// testGraphCal is what a Microsoft 365 calendar's collection URL looks like:
// the /me/calendars/ path is how a Graph calendar is told from a CalDAV one.
const testGraphCal = "https://graph.microsoft.com/v1.0/me/calendars/cal-1"

// seedGraph is seedTasks with a Microsoft 365 calendar beside the CalDAV one,
// and the address its invites come from mapped to it.
func seedGraph(t *testing.T) (*Daemon, *davsync.Fake) {
	t.Helper()
	d, f, _ := seedTasks(t, davsync.Collection{Kind: "events", URL: testGraphCal, Name: "Work"})
	d.CalendarEmail = map[string]string{"work@example.com": "Work"}
	return d, f
}

// Teams meetings and invites are things a Microsoft 365 calendar's server
// does; anywhere else the wish is refused before anything is written.
func TestTeamsAndInvitesNeedAGraphCalendar(t *testing.T) {
	d, f, _ := seedTasks(t)
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "2026-09-01 10:00", "teams": true,
	}); resp.OK || !strings.Contains(resp.Error, "Microsoft 365") {
		t.Errorf("teams on a CalDAV calendar: %+v", resp)
	}
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "2026-09-01 10:00",
		"invite": []string{"anna@example.com"},
	}); resp.OK || !strings.Contains(resp.Error, "Microsoft 365") {
		t.Errorf("an invite on a CalDAV calendar: %+v", resp)
	}
	if n := len(eventsOn(f)); n != 0 {
		t.Errorf("%d events were written anyway", n)
	}
}

// --teams, --invite and --uninvite change the whole event; --occurrence
// changes one instance, and the two scopes do not mix.
func TestTeamsAndInvitesRefuseAnOccurrence(t *testing.T) {
	d, _ := seedGraph(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Standup", "start": mondayAt(9, 0), "calendar": "Work",
		"repeat": "FREQ=WEEKLY;BYDAY=MO",
	}).Data.(map[string]any)
	for _, args := range []map[string]any{
		{"positional": added["id"], "occurrence": "2026-10-05", "teams": true},
		{"positional": added["id"], "occurrence": "2026-10-05", "invite": []string{"anna@example.com"}},
		{"positional": added["id"], "occurrence": "2026-10-05", "uninvite": []string{"anna@example.com"}},
	} {
		if resp := ask(t, d, []string{"event", "edit"}, args); resp.OK ||
			!strings.Contains(resp.Error, "cannot be combined") {
			t.Errorf("resp = %+v", resp)
		}
	}
}

// An address that no mail client would accept is refused here, with the
// offending one named, rather than sent to Exchange.
func TestTeamsAndInvitesRefuseABadAddress(t *testing.T) {
	d, _ := seedGraph(t)
	for _, addr := range []string{"anna", "anna@example", "anna@@example.com"} {
		if resp := ask(t, d, []string{"event", "add"}, map[string]any{
			"positional": "Termin", "start": "2026-09-01 10:00", "calendar": "Work",
			"invite": []string{addr},
		}); resp.OK || !strings.Contains(resp.Error, addr) {
			t.Errorf("%q was accepted: %+v", addr, resp)
		}
	}
	// And uninvite is an edit's word: a new event has nobody to take off.
	if resp := ask(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Termin", "start": "2026-09-01 10:00", "calendar": "Work",
		"uninvite": []string{"anna@example.com"},
	}); resp.OK || !strings.Contains(resp.Error, "uninvite") {
		t.Errorf("resp = %+v", resp)
	}
}

// The row is how a caller knows which calendars take --teams and --invite at
// all, and whose address the invites come from.
func TestCalendarListSaysTeamsAndOwner(t *testing.T) {
	d, _ := seedGraph(t)
	rows, ok := mustAsk(t, d, []string{"calendar", "list"}, nil).Data.([]calendar)
	if !ok {
		t.Fatalf("calendar list returned %T", mustAsk(t, d, []string{"calendar", "list"}, nil).Data)
	}
	byName := map[string]calendar{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if r := byName["Work"]; !r.Teams || r.Owner != "work@example.com" {
		t.Errorf("Work = %+v", r)
	}
	if r := byName["Kalender"]; r.Teams || r.Owner != "" {
		t.Errorf("Kalender = %+v", r)
	}
}

// A Teams event carries the marker and its invitees in the raw, and the reply
// says what Graph was asked for — including that no link came back, which on
// the fake is always the case.
func TestEventAddWithTeamsAndInvite(t *testing.T) {
	d, f := seedGraph(t)
	got := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Review", "start": "2026-09-01 10:00", "calendar": "Work",
		"teams": true, "invite": []string{"Anna@example.com"},
	}).Data.(map[string]any)
	if got["url"] != "" {
		t.Errorf("url = %v, want none before the server made a link", got["url"])
	}
	if invited, ok := got["invited"].([]string); !ok || !slices.Equal(invited, []string{"anna@example.com"}) {
		t.Errorf("invited = %#v", got["invited"])
	}
	if notice, _ := got["notice"].(string); !strings.Contains(notice, "Teams link") {
		t.Errorf("notice = %q", notice)
	}
	raw := graphEventNamed(t, f, "Review")
	if !strings.Contains(raw, "X-MAILBOX-TEAMS:TRUE") ||
		!strings.Contains(raw, "ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:anna@example.com") {
		t.Errorf("the wish did not reach the object:\n%s", raw)
	}
}

// An edit invites and uninvites, the reply names only who is new, and the
// view reads the roster back with the answers it holds.
func TestEventEditAndViewCarryTheRoster(t *testing.T) {
	d, f := seedGraph(t)
	added := mustAsk(t, d, []string{"event", "add"}, map[string]any{
		"positional": "Review", "start": "2026-09-01 10:00", "calendar": "Work",
		"teams": true, "invite": []string{"anna@example.com"},
	}).Data.(map[string]any)

	view := mustAsk(t, d, []string{"event", "view"},
		map[string]any{"positional": added["id"]}).Data.(event)
	if !view.Teams {
		t.Errorf("view lost the Teams marker: %+v", view)
	}
	if len(view.Attendees) != 1 || view.Attendees[0].Address != "anna@example.com" ||
		view.Attendees[0].Answer != "none" {
		t.Errorf("attendees = %+v", view.Attendees)
	}

	// Re-inviting anna reaches nobody new; bert does.
	got := mustAsk(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "invite": []string{"BERT@example.de", "anna@example.com"},
	}).Data.(map[string]any)
	if invited, ok := got["invited"].([]string); !ok || !slices.Equal(invited, []string{"bert@example.de"}) {
		t.Errorf("invited = %#v", got["invited"])
	}
	raw := graphEventNamed(t, f, "Review")
	if n := strings.Count(raw, "ATTENDEE"); n != 2 {
		t.Errorf("%d attendees after the edit:\n%s", n, raw)
	}

	// And an uninvite on its own is a valid edit.
	mustAsk(t, d, []string{"event", "edit"}, map[string]any{
		"positional": added["id"], "uninvite": []string{"anna@example.com"},
	})
	raw = graphEventNamed(t, f, "Review")
	if strings.Contains(raw, "anna@example.com") || strings.Count(raw, "ATTENDEE") != 1 {
		t.Errorf("anna did not leave:\n%s", raw)
	}
}

// event view drops the account's own answer from the roster, and a name that
// is only the address again — Exchange's spelling of an outsider.
func TestViewAttendeesDropsTheOwnerAndEchoedNames(t *testing.T) {
	got := viewAttendees([]vcal.Attendee{
		{Address: "work@example.com", Partstat: vcal.PartstatAccepted},
		{Address: "zora@example.de", Name: "Zora@Example.de", Partstat: vcal.PartstatAccepted},
		{Address: "anna@example.com", Name: "Anna", Partstat: vcal.PartstatTentative},
	}, "Work@example.com")
	want := []attendee{
		{Address: "zora@example.de", Answer: "accepted"},
		{Address: "anna@example.com", Name: "Anna", Answer: "tentative"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("viewAttendees = %+v, want %+v", got, want)
	}
}
