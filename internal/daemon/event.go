package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
)

// changeEvent adds, edits and deletes appointments. It is the calendar's half
// of what changeTodo does for task lists, and it works the same way: the raw
// iCalendar is the record, so an edit reads what is there, changes what was
// named, and puts it back under the ETag it read (ADR-0010).
func (d *Daemon) changeEvent(ctx context.Context, verb string, req Request, resp Response) Response {
	if verb == "add" {
		return d.addEvent(ctx, req, resp)
	}

	object, col, err := d.load(req, "event")
	if err != nil {
		return resp.failed(err)
	}

	if verb == "delete" {
		if err := d.DAVWriter.Delete(ctx, col, object); err != nil {
			return resp.api(err.Error())
		}
		d.push(Push{Event: eventChanged, Account: d.Account, Box: col.Name})
		return resp.ok(map[string]any{
			"id": object.ID, "state": "deleted", "summary": object.Summary, "calendar": col.Name,
		})
	}

	// A repeating event is one object and one rule. Editing it moves every
	// instance, which is a bigger thing than it looks from a single line in an
	// agenda, so it is said out loud rather than discovered next week.
	edit, err := eventEdit(req)
	if err != nil {
		return resp.usage(err.Error())
	}
	if edit.Empty() {
		return resp.usage(
			"event edit needs something to change: --title, --start, --end, --location, " +
				"--notes, --url, --repeat or --alarm")
	}
	raw, err := vcal.SetEvent(object.Raw, edit)
	if err != nil {
		return resp.api(err.Error())
	}
	written, err := d.put(ctx, eventChanged, col, object.Href, raw, object.ETag)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewEventObject(written, col))
}

func (d *Daemon) addEvent(ctx context.Context, req Request, resp Response) Response {
	summary := strings.TrimSpace(req.Str("positional"))
	if summary == "" {
		return resp.usage("an event needs a summary")
	}
	edit, err := eventEdit(req)
	if err != nil {
		return resp.usage(err.Error())
	}
	if edit.Start.IsZero() {
		return resp.usage("an event needs --start")
	}
	col, err := d.pick(calendars, req.Str("calendar"))
	if err != nil {
		return resp.failed(err)
	}
	uid := vcal.NewUID()
	// A new event says what it is in the text, not in --title, which is how an
	// edit changes one.
	edit.Summary = summary
	raw, err := vcal.NewEvent(uid, edit)
	if err != nil {
		return resp.api(err.Error())
	}
	written, err := d.put(ctx, eventChanged, col, eventHref(col, uid), raw, "")
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewEventObject(written, col))
}

// eventHref is where a new object goes. It defers to davsync.Href so a new
// event is named by the same root-relative path the sync REPORT will report it
// under: spelling it absolutely here (col.URL carries the scheme and host) made
// the created row and the synced row miss each other on the (collection_id,
// href) key and the Mirror kept both.
func eventHref(col mirror.Collection, uid string) string {
	return davsync.Href(col, uid)
}

// eventEdit reads the fields an add or an edit was given. An empty one means
// the caller named nothing, which is a usage error for an edit and the ordinary
// case for the optional half of an add.
func eventEdit(req Request) (vcal.EventEdit, error) {
	e := vcal.EventEdit{
		Summary:     strings.TrimSpace(req.Str("title")),
		Description: req.Str("notes"),
		Location:    req.Str("location"),
		URL:         strings.TrimSpace(req.Str("url")),
	}
	rule, err := vcal.Rule(req.Str("repeat"))
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
	if req.Bool("all_day") {
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
	raw := strings.TrimSpace(req.Str("alarm"))
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

// viewEventObject is what a write reports back: enough to see it landed, and
// the id that reads it whole.
func viewEventObject(o mirror.Object, col mirror.Collection) map[string]any {
	return map[string]any{
		"id": o.ID, "summary": o.Summary, "calendar": col.Name,
		"start": o.Start.Format(time.RFC3339), "state": "saved",
	}
}
