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
		// One instance off, the rest of the rule standing: an override with
		// STATUS:CANCELLED, not the whole object.
		if raw := strings.TrimSpace(req.Str("occurrence")); raw != "" {
			at, isDay, ok := parseWhen(raw)
			if !ok {
				return resp.usage(fmt.Sprintf("--occurrence takes 2026-09-01 or 2026-09-01 14:00, got %q", raw))
			}
			edited, err := vcal.CancelOccurrence(object.Raw, at, isDay)
			if err != nil {
				return resp.api(err.Error())
			}
			if _, err := d.put(ctx, eventChanged, col, object.Href, edited, object.ETag); err != nil {
				return resp.api(err.Error())
			}
			return resp.ok(map[string]any{
				"id": object.ID, "state": "deleted", "occurrence": at.Format("2006-01-02"),
				"summary": object.Summary, "calendar": col.Name,
			})
		}
		if err := d.remove(ctx, eventChanged, col, object); err != nil {
			return resp.api(err.Error())
		}
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
	teams, invite, uninvite, err := meetingWish(req, true)
	if err != nil {
		return resp.usage(err.Error())
	}
	wants := teams || len(invite) > 0 || len(uninvite) > 0
	if wants && !graphCalendar(col) {
		return resp.usage("Teams meetings and invites need a Microsoft 365 calendar")
	}
	edit.Teams, edit.Invite, edit.Uninvite = teams, invite, uninvite
	if edit.Empty() {
		return resp.usage(
			"event edit needs something to change: --title, --start, --end, --location, " +
				"--notes, --url, --repeat, --alarm, --teams, --invite or --uninvite")
	}
	// One instance of a repeating event, the rest of the rule standing. An
	// override cannot carry a rule of its own, so --repeat beside --occurrence
	// is a caller confusing the two scopes.
	if raw := strings.TrimSpace(req.Str("occurrence")); raw != "" {
		if wants {
			return resp.usage("--teams, --invite and --uninvite change the whole event and cannot be combined with --occurrence")
		}
		if edit.Repeat != "" {
			return resp.usage("--repeat changes every instance of a rule; --occurrence changes one, and cannot be combined with it")
		}
		at, isDay, ok := parseWhen(raw)
		if !ok {
			return resp.usage(fmt.Sprintf("--occurrence takes 2026-09-01 or 2026-09-01 14:00, got %q", raw))
		}
		edited, err := vcal.SetOccurrence(object.Raw, at, isDay, edit)
		if err != nil {
			return resp.api(err.Error())
		}
		written, err := d.put(ctx, eventChanged, col, object.Href, edited, object.ETag)
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(viewEventObject(written, col, nil))
	}
	raw, err := vcal.SetEvent(object.Raw, edit)
	if err != nil {
		return resp.api(err.Error())
	}
	written, err := d.put(ctx, eventChanged, col, object.Href, raw, object.ETag)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(meetingReply(written, col, teams, invite, object.Raw))
}

func (d *Daemon) addEvent(ctx context.Context, req Request, resp Response) Response {
	summary := strings.TrimSpace(req.Str("positional"))
	if summary == "" {
		return resp.usage("an event needs a summary")
	}
	teams, invite, _, err := meetingWish(req, false)
	if err != nil {
		return resp.usage(err.Error())
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
	if (teams || len(invite) > 0) && !graphCalendar(col) {
		return resp.usage("Teams meetings and invites need a Microsoft 365 calendar")
	}
	uid := vcal.NewUID()
	// A new event says what it is in the text, not in --title, which is how an
	// edit changes one.
	edit.Summary = summary
	edit.Teams, edit.Invite = teams, invite
	raw, err := vcal.NewEvent(uid, edit)
	if err != nil {
		return resp.api(err.Error())
	}
	// davsync.Href names the new object by the same root-relative path the sync
	// REPORT will report it under. Spelling it absolutely (col.URL carries the
	// scheme and host) made the created row and the synced row miss each other on
	// the (collection_id, href) key and the Mirror kept both.
	written, err := d.put(ctx, eventChanged, col, davsync.Href(col, uid), raw, "")
	if err != nil {
		return resp.api(err.Error())
	}
	// On a fresh event everybody named is new.
	return resp.ok(meetingReply(written, col, teams, invite, ""))
}

// graphCalendar says whether a collection is one of a Microsoft 365 account's
// calendars. That is the one kind of calendar whose server makes Teams links
// and sends invitations itself; everywhere else the wishes are refused before
// anything is written.
func graphCalendar(col mirror.Collection) bool {
	return strings.Contains(col.URL, "/me/calendars/")
}

// meetingWish reads the Teams and invite arguments an add or an edit was
// given, and refuses what no calendar could make sense of. uninvite is an
// edit's word: a new event has nobody on it to take off.
func meetingWish(req Request, isEdit bool) (teams bool, invite, uninvite []string, err error) {
	teams = req.Bool("teams")
	invite = req.Strings("invite")
	uninvite = req.Strings("uninvite")
	if !isEdit && len(uninvite) > 0 {
		return false, nil, nil, errors.New("--uninvite takes an attendee off an event that is there; an add has nobody to take off")
	}
	for _, wish := range []struct {
		flag  string
		addrs []string
	}{{"invite", invite}, {"uninvite", uninvite}} {
		for _, addr := range wish.addrs {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			if !addrIsOne(addr) {
				return false, nil, nil, fmt.Errorf("--%s takes an address like anna@example.com, got %q", wish.flag, addr)
			}
		}
	}
	return teams, invite, uninvite, nil
}

// addrIsOne checks the shape of an address, not whether it exists: one @ and
// a dot in the domain, which is the least a mail client would accept.
func addrIsOne(addr string) bool {
	if n := strings.Count(addr, "@"); n != 1 {
		return false
	}
	domain := addr[strings.Index(addr, "@")+1:]
	return strings.Contains(domain, ".")
}

// meetingReply is what an add or an edit that named Teams or invitees reports:
// the ordinary view plus who this call invited. When Teams was asked for and
// no join link came back, the calendar did not make one — worth saying now
// rather than when somebody looks for the link.
func meetingReply(written mirror.Object, col mirror.Collection, teams bool, invite []string, before string) map[string]any {
	out := viewEventObject(written, col, newlyInvited(before, invite))
	if teams && out["url"] == "" {
		out["notice"] = "Microsoft 365 made no Teams link — the calendar may not allow Teams meetings"
	}
	return out
}

// newlyInvited is who this call adds: an invite of somebody already on the
// event reaches nobody new. before is the object as it was, empty for an
// add, where everybody named is new.
func newlyInvited(before string, invite []string) []string {
	out := []string{}
	if len(invite) == 0 {
		return out
	}
	on := map[string]bool{}
	if before != "" {
		if p, err := vcal.Parse(before, time.Local); err == nil {
			for _, a := range p.Attendees {
				on[a.Address] = true
			}
		}
	}
	for _, addr := range invite {
		addr = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(addr)), "mailto:")
		if addr == "" || on[addr] {
			continue
		}
		out = append(out, addr)
	}
	return out
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
// the id that reads it whole. The link is the event's own — on a Teams meeting
// it is the join link the calendar made — and invited names who this call put
// on, which is what a caller showing a toast wants without reading it back.
func viewEventObject(o mirror.Object, col mirror.Collection, invited []string) map[string]any {
	out := map[string]any{
		"id": o.ID, "summary": o.Summary, "calendar": col.Name,
		"start": o.Start.Format(time.RFC3339), "state": "saved",
		"url": "", "invited": invited,
	}
	if invited == nil {
		out["invited"] = []string{}
	}
	if p, err := vcal.Parse(o.Raw, time.Local); err == nil {
		out["url"] = p.URL
	}
	return out
}
