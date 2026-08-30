package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
	"mailbox/internal/vcal"
)

// changeEvent adds, edits and deletes appointments. It is the calendar's half
// of what changeTodo does for task lists, and it works the same way: the raw
// iCalendar is the record, so an edit reads what is there, changes what was
// named, and puts it back under the ETag it read (ADR-0010).
func (d *Daemon) changeEvent(ctx context.Context, verb string, req Request, resp Response) Response {
	if d.DAVWriter == nil {
		resp.Code, resp.Error = "api", "this daemon cannot write: no dav connection"
		return resp
	}

	if verb == "add" {
		return d.addEvent(ctx, req, resp)
	}

	id, err := objectID(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	object, err := d.Mirror.Object(d.Account, id)
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", fmt.Sprintf("no event %d in the mirror", id)
		return resp
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	col, err := d.collectionOf(object)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	if verb == "delete" {
		if err := d.DAVWriter.Delete(ctx, col, object); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		d.push(Push{Event: "event.changed", Account: d.Account, Box: col.Name})
		resp.OK, resp.Data = true, map[string]any{
			"id": id, "state": "deleted", "summary": object.Summary, "calendar": col.Name,
		}
		return resp
	}

	// A repeating event is one object and one rule. Editing it moves every
	// instance, which is a bigger thing than it looks from a single line in an
	// agenda, so it is said out loud rather than discovered next week.
	edit, err := eventEdit(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if edit.Empty() {
		resp.Code, resp.Error = "usage",
			"event edit needs something to change: --title, --start, --end, --location, "+
				"--notes, --url, --repeat or --alarm"
		return resp
	}
	raw, err := vcal.SetEvent(object.Raw, edit)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	written, err := d.writeEvent(ctx, col, object.Href, raw, object.ETag)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, viewEventObject(written, col)
	return resp
}

func (d *Daemon) addEvent(ctx context.Context, req Request, resp Response) Response {
	summary := strings.TrimSpace(str(req.Args["positional"]))
	if summary == "" {
		resp.Code, resp.Error = "usage", "an event needs a summary"
		return resp
	}
	edit, err := eventEdit(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if edit.Start.IsZero() {
		resp.Code, resp.Error = "usage", "an event needs --start"
		return resp
	}
	col, err := d.calendarFor(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	uid := vcal.NewUID()
	// A new event says what it is in the text, not in --title, which is how an
	// edit changes one.
	edit.Summary = summary
	raw, err := vcal.NewEvent(uid, edit)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	written, err := d.writeEvent(ctx, col, eventHref(col, uid), raw, "")
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, viewEventObject(written, col)
	return resp
}

func (d *Daemon) writeEvent(ctx context.Context, col mirror.Collection, href, raw, ifMatch string) (mirror.Object, error) {
	object, err := d.DAVWriter.Put(ctx, col, href, raw, ifMatch)
	if err != nil {
		return mirror.Object{}, err
	}
	d.push(Push{Event: "event.changed", Account: d.Account, Box: col.Name})
	return object, nil
}

// eventHref is where a new object goes. The UID names the file, which is what
// every CalDAV server expects and what makes a re-PUT idempotent.
func eventHref(col mirror.Collection, uid string) string {
	return strings.TrimSuffix(col.URL, "/") + "/" + uid + ".ics"
}

// calendarFor picks which calendar a new event goes on. One calendar needs no
// naming; several do, because an appointment landing on the wrong one is worse
// than being asked which.
func (d *Daemon) calendarFor(req Request) (mirror.Collection, error) {
	name, _ := req.Args["calendar"].(string)
	cals, err := d.Mirror.Collections(d.Account, "events")
	if err != nil {
		return mirror.Collection{}, err
	}
	// The habits record lives on a calendar of its own and is not somewhere an
	// appointment belongs (ADR-0018).
	var open []mirror.Collection
	for _, c := range cals {
		if c.Name != habit.CalendarName {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return mirror.Collection{}, errors.New("there are no calendars on this account")
	}
	if name != "" {
		for _, c := range open {
			if strings.EqualFold(c.Name, name) {
				return c, nil
			}
		}
		return mirror.Collection{}, fmt.Errorf("no calendar called %q", name)
	}
	if len(open) == 1 {
		return open[0], nil
	}
	names := make([]string, 0, len(open))
	for _, c := range open {
		names = append(names, c.Name)
	}
	return mirror.Collection{}, fmt.Errorf("there are %d calendars — name one with --calendar: %s",
		len(open), strings.Join(names, ", "))
}

// eventEdit reads the fields an add or an edit was given. An empty one means
// the caller named nothing, which is a usage error for an edit and the ordinary
// case for the optional half of an add.
func eventEdit(req Request) (vcal.EventEdit, error) {
	e := vcal.EventEdit{
		Summary:     strings.TrimSpace(str(req.Args["title"])),
		Description: str(req.Args["notes"]),
		Location:    str(req.Args["location"]),
		URL:         strings.TrimSpace(str(req.Args["url"])),
	}
	rule, err := vcal.Rule(str(req.Args["repeat"]))
	if err != nil {
		return vcal.EventEdit{}, err
	}
	e.Repeat = rule
	alarms, err := alarmMinutes(req)
	if err != nil {
		return vcal.EventEdit{}, err
	}
	e.Alarms = alarms
	start, startDay, err := eventTime(req, "start")
	if err != nil {
		return vcal.EventEdit{}, err
	}
	end, _, err := eventTime(req, "end")
	if err != nil {
		return vcal.EventEdit{}, err
	}
	e.Start, e.End = start, end
	// A start with no clock on it is an all-day event. "Friday" does not mean
	// midnight on Friday, and storing it as one makes every client show a time
	// nobody meant.
	e.AllDay = startDay
	if v, ok := req.Args["all_day"].(bool); ok && v {
		e.AllDay = true
	}
	if !e.Start.IsZero() && !e.End.IsZero() && !e.End.After(e.Start) && !e.AllDay {
		return vcal.EventEdit{}, errors.New("--end is not after --start")
	}
	return e, nil
}

// alarmMinutes reads --alarm: how many minutes before the start each reminder
// fires. Nil is nobody naming any, which leaves whatever reminders another
// client put on the entry; "none" is naming an empty list, which takes them
// off.
func alarmMinutes(req Request) ([]int, error) {
	raw := strings.TrimSpace(str(req.Args["alarm"]))
	if raw == "" {
		return nil, nil
	}
	if strings.EqualFold(raw, "none") {
		return []int{}, nil
	}
	out := []int{}
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return nil, fmt.Errorf(
				"--alarm takes minutes before the start, like 15 or 10,60, or none — got %q", raw)
		}
		out = append(out, n)
	}
	return out, nil
}

// eventTime reads one of --start or --end. Both a bare date and a date with a
// time are accepted; which one was given decides whether the event has a clock.
func eventTime(req Request, key string) (when time.Time, isDay bool, err error) {
	raw := strings.TrimSpace(str(req.Args[key]))
	if raw == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, perr := time.ParseInLocation(layout, raw, time.Local); perr == nil {
			return t, false, nil
		}
	}
	if t, perr := time.ParseInLocation("2006-01-02", raw, time.Local); perr == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf(
		"--%s takes 2026-09-01 or 2026-09-01 14:00, got %q", key, raw)
}

// viewEventObject is what a write reports back: enough to see it landed, and
// the id that reads it whole.
func viewEventObject(o mirror.Object, col mirror.Collection) map[string]any {
	return map[string]any{
		"id": o.ID, "summary": o.Summary, "calendar": col.Name,
		"start": o.Start.Format(time.RFC3339), "state": "saved",
	}
}
