package graphdrv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
	"mailbox/internal/vcard"
)

func davSetup(t *testing.T) (*fakeGraph, *DAV) {
	t.Helper()
	f, c := newFakeGraph(t)
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d := NewDAV(c, s, "work")
	d.Email = "Me@Example.de"
	d.loc, _ = time.LoadLocation("Europe/Berlin")
	d.zoneName = "Europe/Berlin"
	d.now = func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }

	f.events["ev-a"] = map[string]any{
		"id": "ev-a", "iCalUId": "040000008200E00074C5B7101A82E00800000000AAAA", "type": "singleInstance",
		"changeKey": "ck-a", "calendar": "cal-1", "subject": "Planung",
		"body":         map[string]any{"contentType": "html", "content": "<html><body><p>Agenda <b>hier</b></p></body></html>"},
		"location":     map[string]any{"displayName": "Raum 1"},
		"start":        map[string]any{"dateTime": "2026-09-28T08:00:00.0000000", "timeZone": "UTC"},
		"end":          map[string]any{"dateTime": "2026-09-28T09:00:00.0000000", "timeZone": "UTC"},
		"isReminderOn": true, "reminderMinutesBeforeStart": 15,
		"onlineMeeting":  map[string]any{"joinUrl": "https://teams.example.com/l/meetup-join/19%3ameeting_Zm9vYmFy%40thread.v2/0"},
		"responseStatus": map[string]any{"response": "notResponded"},
	}
	// Mondays at ten Berlin time, four of them, across the end of summer time:
	// the second is an hour later in UTC than the first.
	f.events["ev-s"] = map[string]any{
		"id": "ev-s", "iCalUId": "040000008200E00074C5B7101A82E00800000000BBBB", "type": "seriesMaster",
		"changeKey": "ck-s", "calendar": "cal-1", "subject": "Jour fixe",
		"body":  map[string]any{"contentType": "text", "content": ""},
		"start": map[string]any{"dateTime": "2026-10-19T08:00:00.0000000", "timeZone": "UTC"},
		"end":   map[string]any{"dateTime": "2026-10-19T08:30:00.0000000", "timeZone": "UTC"},
		"recurrence": map[string]any{
			"pattern": map[string]any{"type": "weekly", "interval": 1, "daysOfWeek": []string{"monday"}, "firstDayOfWeek": "monday"},
			"range":   map[string]any{"type": "numbered", "startDate": "2026-10-19", "numberOfOccurrences": 4},
		},
	}
	instance := func(id, typ, original, start, end, subject string) map[string]any {
		return map[string]any{"id": id, "type": typ, "seriesMasterId": "ev-s", "subject": subject,
			"originalStart": original,
			"start":         map[string]any{"dateTime": start, "timeZone": "UTC"},
			"end":           map[string]any{"dateTime": end, "timeZone": "UTC"}}
	}
	f.instances["ev-s"] = []map[string]any{
		instance("ev-s-1", "occurrence", "2026-10-19T08:00:00Z", "2026-10-19T08:00:00.0000000", "2026-10-19T08:30:00.0000000", "Jour fixe"),
		instance("ev-s-2", "occurrence", "2026-10-26T09:00:00Z", "2026-10-26T09:00:00.0000000", "2026-10-26T09:30:00.0000000", "Jour fixe"),
		// Moved an hour later and retitled.
		instance("ev-s-3", "exception", "2026-11-02T09:00:00Z", "2026-11-02T10:00:00.0000000", "2026-11-02T10:30:00.0000000", "Jour fixe (später)"),
		// The fourth, 2026-11-09, was cancelled: Graph no longer has it.
	}
	f.contacts["c-erika"] = map[string]any{
		"id": "c-erika", "changeKey": "ck-c1", "folder": "cf-default",
		"displayName": "Erika Mustermann", "givenName": "Erika", "surname": "Mustermann",
		"emailAddresses": []any{map[string]string{"name": "Erika", "address": "erika@example.com"}},
		"businessPhones": []string{"+49 30 1234"}, "mobilePhone": "+49 170 5555", "homePhones": []string{},
		"companyName": "Example GmbH", "personalNotes": "met at the fair",
	}
	return f, d
}

func byName(t *testing.T, d *DAV) map[string]davsync.Collection {
	t.Helper()
	cols, err := d.Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]davsync.Collection{}
	for _, c := range cols {
		out[c.Kind+" "+c.Name] = c
	}
	return out
}

func TestCollectionsAreNamedAfterTheAccount(t *testing.T) {
	_, d := davSetup(t)
	got := byName(t, d)
	for _, want := range []string{"events work", "events work/Feiertage", "cards work", "cards work/Kunden"} {
		if _, ok := got[want]; !ok {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if got["events work"].Color != "#1e90ff" || !d.Owns(got["events work"].URL) {
		t.Errorf("default calendar %+v", got["events work"])
	}
}

func objects(t *testing.T, ch davsync.Changes) map[string]davsync.Change {
	t.Helper()
	out := map[string]davsync.Change{}
	for _, it := range ch.Items {
		out[it.Href] = it
	}
	return out
}

func TestEventsArriveAsICalendar(t *testing.T) {
	_, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	got := objects(t, ch)
	if len(got) != 2 {
		t.Fatalf("%d objects: %v", len(got), got)
	}
	single := got["/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000AAAA.ics"]
	p, err := vcal.Parse(single.Data, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if p.UID != "040000008200E00074C5B7101A82E00800000000AAAA" || p.Summary != "Planung" || p.Location != "Raum 1" ||
		!p.Start.Equal(time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)) || !strings.Contains(p.Description, "Agenda") ||
		!strings.Contains(p.URL, "teams.example.com") || !slices.Equal(p.Alarms, []int{15}) || p.Recurring {
		t.Fatalf("single %+v", p)
	}

	series := got["/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000BBBB.ics"]
	occ, err := vcal.Occurrences(series.Data, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), d.loc)
	if err != nil {
		t.Fatal(err)
	}
	var starts []string
	for _, o := range occ {
		starts = append(starts, o.Start.UTC().Format("01-02T15:04")+" "+o.Summary)
	}
	slices.Sort(starts)
	want := []string{"10-19T08:00 Jour fixe", "10-26T09:00 Jour fixe", "11-02T10:00 Jour fixe (später)"}
	if !slices.Equal(starts, want) {
		t.Fatalf("occurrences\n%v\nwant\n%v\n%s", starts, want, series.Data)
	}
}

func TestAnEditSendsOnlyWhatChanged(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	href := "/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000AAAA.ics"
	raw, err := vcal.SetEvent(objects(t, ch)[href].Data, vcal.EventEdit{Summary: "Planung Q4"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, f.srv.URL+href, raw, "ck-a"); err != nil {
		t.Fatal(err)
	}
	// The HTML body, the reminder and the join link were not sent back: a
	// PATCH of the title cannot lose what the title edit did not touch.
	w := f.writes()
	if len(w) != 1 || w[0] != `PATCH /me/events/ev-a {"subject":"Planung Q4"}` {
		t.Fatalf("writes %v", w)
	}
	if body := f.events["ev-a"]["body"].(map[string]any); body["contentType"] != "html" {
		t.Fatalf("the body was rewritten: %v", body)
	}

	// An edit that changes nothing sends nothing.
	if _, err := d.Put(ctx, f.srv.URL+href, raw, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.writes()) != 1 {
		t.Fatalf("a no-op edit wrote %v", f.writes())
	}
}

func TestANewEventKeepsItsHrefAndUID(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	uid := "3f9c7e2a-51b8-4d0e-9a6f-2c1d8e7b4a90"
	raw, err := vcal.NewEvent(uid, vcal.EventEdit{
		Summary: "Review", Start: time.Date(2026, 10, 1, 14, 0, 0, 0, d.loc), Repeat: "FREQ=WEEKLY;BYDAY=TH", Alarms: []int{10},
	})
	if err != nil {
		t.Fatal(err)
	}
	href := "/v1.0/me/calendars/cal-1/" + uid + ".ics"
	if _, err := d.Put(ctx, f.srv.URL+href, raw, ""); err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if len(w) != 1 || !strings.HasPrefix(w[0], "POST /me/calendars/cal-1/events ") ||
		!strings.Contains(w[0], `"dateTime":"2026-10-01T14:00:00","timeZone":"Europe/Berlin"`) ||
		!strings.Contains(w[0], `"daysOfWeek":["thursday"]`) || !strings.Contains(w[0], `"reminderMinutesBeforeStart":10`) {
		t.Fatalf("writes %v", w)
	}

	// It comes back through the delta under the href and UID it was made with,
	// so the Mirror does not hold it twice.
	ch, err = d.Sync(ctx, cal.URL, ch.Token)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := objects(t, ch)[href]
	if !ok {
		t.Fatalf("the new event came back as %v", objects(t, ch))
	}
	if p, _ := vcal.Parse(got.Data, d.loc); p.UID != uid || p.Repeat != "FREQ=WEEKLY;BYDAY=TH" {
		t.Fatalf("came back as %+v", p)
	}
}

func TestADeletedEventGoesAndAStaleWindowStartsOver(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	href := "/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000AAAA.ics"
	if err := d.Delete(ctx, f.srv.URL+href, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.events["ev-a"]; ok {
		t.Fatal("the event is still on the server")
	}
	next, err := d.Sync(ctx, cal.URL, ch.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 0 {
		// Our own delete: the href is already unmapped, so the echo says nothing.
		t.Fatalf("after our own delete: %v", next.Items)
	}

	// Deleted in Outlook: the delta names an id the Store maps.
	f.mu.Lock()
	delete(f.events, "ev-s")
	f.change("cal-1", "ev-s-1", true)
	f.mu.Unlock()
	next, err = d.Sync(ctx, cal.URL, next.Token)
	if err != nil {
		t.Fatal(err)
	}
	gone := objects(t, next)["/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000BBBB.ics"]
	if !gone.Deleted {
		t.Fatalf("a series deleted in Outlook: %v", next.Items)
	}

	d.now = func() time.Time { return time.Date(2026, 12, 26, 12, 0, 0, 0, time.UTC) }
	if _, err := d.Sync(ctx, cal.URL, next.Token); !errors.Is(err, davsync.ErrTokenExpired) {
		t.Fatalf("a window three months old: %v", err)
	}
}

func TestADayOldDeltaTokenStartsOver(t *testing.T) {
	_, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	// A day later the token is retired: delta has been known to sit on a
	// deletion forever, and the sweep from nothing is what heals it.
	d.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, err := d.Sync(ctx, cal.URL, ch.Token); !errors.Is(err, davsync.ErrTokenExpired) {
		t.Fatalf("a day-old token: %v", err)
	}
	// A token from before tokens carried their issue time is spent too: it
	// starts over once and comes back in the new shape.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	old := fmt.Sprintf("%d|%d|%s", day.Add(-windowBack).Unix(), day.Add(windowForward).Unix(),
		cal.URL+"/calendarView/delta?$deltatoken=old")
	if _, err := d.Sync(ctx, cal.URL, old); !errors.Is(err, davsync.ErrTokenExpired) {
		t.Fatalf("a token without an issue time: %v", err)
	}
}

// Exchange put the invite on the calendar unanswered; answering it goes to
// Exchange, which replies to the organizer, and the answer comes back on the
// next read as the account's PARTSTAT.
func TestAnInviteIsAnsweredOnExchange(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	href := "/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000AAAA.ics"
	if got := vcal.AnswerOf(objects(t, ch)[href].Data, "me@example.de"); got != vcal.PartstatNeedsAction {
		t.Fatalf("before: %q\n%s", got, objects(t, ch)[href].Data)
	}
	if err := d.Respond(ctx, href, vcal.PartstatTentative); err != nil {
		t.Fatal(err)
	}
	if w := f.writes(); len(w) != 1 || w[0] != `POST /me/events/ev-a/tentativelyAccept {"sendResponse":true}` {
		t.Fatalf("writes %v", w)
	}
	ch, err = d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := vcal.AnswerOf(objects(t, ch)[href].Data, "me@example.de"); got != vcal.PartstatTentative {
		t.Fatalf("after: %q", got)
	}
	if err := d.Respond(ctx, "/v1.0/me/calendars/cal-1/nope.ics", vcal.PartstatAccepted); err == nil {
		t.Fatal("answered an event that is not there")
	}
}

func TestADeleteOfAnEventDeletedSomewhereElseStillGoes(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	cal := byName(t, d)["events work"]
	if _, err := d.Sync(ctx, cal.URL, ""); err != nil {
		t.Fatal(err)
	}
	href := "/v1.0/me/calendars/cal-1/040000008200E00074C5B7101A82E00800000000AAAA.ics"
	f.mu.Lock()
	delete(f.events, "ev-a")    // gone from the folder...
	f.softDeleted["ev-a"] = true // ...but known to Graph, which says 400 not 404
	f.mu.Unlock()
	if err := d.Delete(ctx, f.srv.URL+href, ""); err != nil {
		t.Fatalf("delete of an already-deleted event: %v", err)
	}
	if _, ok := d.s.objectID(href); ok {
		t.Fatal("the store still maps the deleted event")
	}
}

func TestContactsArriveAsVCardAndKeepTheirPhoneKinds(t *testing.T) {
	f, d := davSetup(t)
	ctx := context.Background()
	book := byName(t, d)["cards work"]
	ch, err := d.Sync(ctx, book.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Items) != 1 {
		t.Fatalf("items %v", ch.Items)
	}
	it := ch.Items[0]
	c, err := vcard.Parse(it.Data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Erika Mustermann" || c.Organisation != "Example GmbH" || c.Note != "met at the fair" ||
		!slices.Equal(c.Emails, []string{"erika@example.com"}) || !slices.Equal(c.Phones, []string{"+49 30 1234", "+49 170 5555"}) {
		t.Fatalf("contact %+v", c)
	}

	raw, err := vcard.AddPhone(it.Data, "+49 40 9999")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, f.srv.URL+it.Href, raw, it.ETag); err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if len(w) != 1 || w[0] != `PATCH /me/contacts/c-erika {"businessPhones":["+49 30 1234","+49 40 9999"]}` {
		t.Fatalf("writes %v", w)
	}

	uid := "a1b2c3d4-0000-4000-8000-00000000c0de"
	raw, _ = vcard.New(uid, "Max Beispiel", []string{"max@example.de"}, nil, "", "")
	href := "/v1.0/me/contactFolders/cf-clients/" + uid + ".vcf"
	if _, err := d.Put(ctx, f.srv.URL+href, raw, ""); err != nil {
		t.Fatal(err)
	}
	if w := f.writes(); len(w) != 2 || !strings.HasPrefix(w[1], "POST /me/contactFolders/cf-clients/contacts ") {
		t.Fatalf("writes %v", w)
	}

	f.mu.Lock()
	delete(f.contacts, "c-erika")
	f.change("cf-default", "c-erika", true)
	f.mu.Unlock()
	next, err := d.Sync(ctx, book.URL, ch.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got := objects(t, next)[it.Href]; !got.Deleted {
		t.Fatalf("a contact deleted in Outlook: %v", next.Items)
	}
}

func TestRulesTranslateBothWays(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	// Tuesday 2026-10-13, 10:00.
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, berlin)
	for _, rule := range []string{
		"FREQ=DAILY",
		"FREQ=DAILY;INTERVAL=2",
		"FREQ=WEEKLY;BYDAY=TU",
		"FREQ=WEEKLY;BYDAY=MO,WE,FR",
		"FREQ=WEEKLY;BYDAY=TU;INTERVAL=2",
		"FREQ=MONTHLY;BYMONTHDAY=13",
		"FREQ=MONTHLY;BYDAY=TU;BYSETPOS=2",
		"FREQ=MONTHLY;BYDAY=FR;BYSETPOS=-1",
		"FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=13",
		"FREQ=WEEKLY;BYDAY=TU;COUNT=5",
	} {
		r, err := recurrenceOf(rule, start, berlin, "Europe/Berlin")
		if err != nil {
			t.Errorf("%s: %v", rule, err)
			continue
		}
		back, err := ruleOf(*r, berlin)
		if err != nil {
			t.Errorf("%s: back: %v", rule, err)
			continue
		}
		if back != strings.Replace(rule, "INTERVAL=2;", "", 1) && !sameRule(back, rule) {
			t.Errorf("%s came back as %s", rule, back)
		}
	}
	// The words vcal.Rule turns into rules all translate.
	for _, word := range []string{"daily", "weekly", "biweekly", "monthly", "yearly", "weekdays"} {
		rule, _ := vcal.Rule(word)
		if _, err := recurrenceOf(rule, start, berlin, "Europe/Berlin"); err != nil {
			t.Errorf("%s (%s): %v", word, rule, err)
		}
	}
	// An end date keeps its last day.
	r, _ := recurrenceOf("FREQ=DAILY;UNTIL=20261031T090000Z", start, berlin, "Europe/Berlin")
	if r.Range.Type != "endDate" || r.Range.EndDate != "2026-10-31" {
		t.Errorf("until %+v", r.Range)
	}
	for _, rule := range []string{"FREQ=HOURLY", "FREQ=DAILY;BYHOUR=9,17", "FREQ=MONTHLY;BYDAY=TU"} {
		if _, err := recurrenceOf(rule, start, berlin, "Europe/Berlin"); err == nil {
			t.Errorf("%s was accepted", rule)
		}
	}
}

// sameRule compares two RRULEs part by part, since the order is free.
func sameRule(a, b string) bool {
	pa, pb := strings.Split(a, ";"), strings.Split(b, ";")
	slices.Sort(pa)
	slices.Sort(pb)
	return slices.Equal(pa, pb)
}
