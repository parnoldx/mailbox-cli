package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
	"mailbox/internal/vcal"
)

// calendar is one Collection, as a caller sees it: a name to type and a count.
//
// Internal marks a collection this program keeps for itself. It is listed --
// it exists on the server and its count and sync time are worth seeing -- but
// it is not somewhere a caller may put anything, which is what a chooser in a
// UI needs to know before it offers it as a target.
type calendar struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Color    string `json:"color,omitempty"`
	Count    int    `json:"count"`
	SyncedAt string `json:"synced_at,omitempty"`
	Internal bool   `json:"internal,omitempty"`
}

// occurrence is one instance of an Event in the window that was asked for. A
// repeating Event is one object on the server and many of these, which is why
// the expansion happens on the way out rather than in the Mirror.
type occurrence struct {
	ID        int64  `json:"id"`
	Calendar  string `json:"calendar"`
	UID       string `json:"uid"`
	Summary   string `json:"summary"`
	Location  string `json:"location,omitempty"`
	Notes     string `json:"notes,omitempty"`
	URL       string `json:"url,omitempty"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	AllDay    bool   `json:"all_day"`
	Recurring bool   `json:"recurring"`
	Status    string `json:"status,omitempty"`
}

// event is one calendar object read whole.
type event struct {
	ID          int64  `json:"id"`
	Calendar    string `json:"calendar"`
	UID         string `json:"uid"`
	Summary     string `json:"summary"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	URL         string `json:"url,omitempty"`
	AllDay      bool   `json:"all_day"`
	Recurring   bool   `json:"recurring"`
	// Repeat is the rule as iCalendar writes it, and Alarms the reminders in
	// minutes before the start. They are read off the raw here rather than
	// projected into the Mirror: only the caller looking at one entry wants
	// them, and the raw is the record (ADR-0010).
	Repeat string       `json:"repeat,omitempty"`
	Alarms []int        `json:"alarms,omitempty"`
	Next   []occurrence `json:"next,omitempty"`
}

// handleCalendar lists the Collections the Mirror holds. Like every other list
// command this never touches the network (ADR-0001).
func (d *Daemon) handleCalendar(req Request, resp Response) Response {
	verb := req.Verb("list")
	if verb != "list" {
		return resp.usage(fmt.Sprintf("unknown calendar command %q", verb))
	}
	kind := req.Str("kind")
	cols, err := d.Mirror.Collections(d.Account, kind)
	if err != nil {
		return resp.api(err.Error())
	}
	out := make([]calendar, 0, len(cols))
	for _, c := range cols {
		// The habits record is storage shaped like a calendar, and an event
		// added to it would be written over by the next habit tick (ADR-0018).
		// `event add` already refuses it; saying so here is what stops a
		// chooser from offering it in the first place.
		row := calendar{Name: c.Name, Kind: c.Kind, Color: c.Color, Count: c.Count,
			Internal: c.Name == habit.CalendarName}
		if !c.SyncedAt.IsZero() {
			row.SyncedAt = c.SyncedAt.Local().Format("2006-01-02 15:04")
		}
		out = append(out, row)
	}
	return resp.ok(out)
}

// handleAgenda answers "what is on" for a window. The window is the question:
// a repeating Event has no finite list of instances to have stored, so the
// Mirror holds the rule and this expands it.
func (d *Daemon) handleAgenda(req Request, resp Response) Response {
	from, to, err := window(req)
	if err != nil {
		return resp.usage(err.Error())
	}
	name := req.Str("calendar")
	if name != "" {
		// A calendar nobody has heard of is a mistake worth naming, rather than
		// an empty agenda that reads like a quiet week.
		if _, err := d.Mirror.CollectionNamed(d.Account, "", name); errors.Is(err, mirror.ErrNotFound) {
			return resp.notFound(fmt.Sprintf("no calendar called %q", name))
		} else if err != nil {
			return resp.api(err.Error())
		}
	}
	objects, err := d.Mirror.ObjectsIn(d.Account, "events", from, to, name)
	if err != nil {
		return resp.api(err.Error())
	}
	out, err := expand(objects, from, to)
	if err != nil {
		return resp.api(err.Error())
	}
	if limit := req.Int("limit", 0); limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return resp.ok(out)
}

// handleEvent reads one calendar object, with the next few times it happens.
func (d *Daemon) handleEvent(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("view")
	switch verb {
	case "add", "edit", "delete":
		return d.changeEvent(ctx, verb, req, resp)
	}
	if verb != "view" {
		return resp.usage(fmt.Sprintf("unknown event command %q", verb))
	}
	id, err := objectID(req, "event")
	if err != nil {
		return resp.usage(err.Error())
	}
	o, err := d.Mirror.Object(d.Account, id)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(fmt.Sprintf("no event %d in the mirror", id))
	}
	if err != nil {
		return resp.api(err.Error())
	}
	out := event{
		ID: o.ID, Calendar: o.Collection, UID: o.UID, Summary: o.Summary,
		Location: o.Location, Description: o.Description, Status: o.Status,
		AllDay: o.AllDay, Recurring: o.Recurring,
	}
	// An entry that will not parse is still an entry: what was read off it is
	// what gets shown, and the rest of the view stands.
	if p, perr := vcal.Parse(o.Raw, time.Local); perr == nil {
		out.URL, out.Repeat, out.Alarms = p.URL, p.Repeat, p.Alarms
	}
	// The next few times it happens, which for a plain Event is once and for a
	// weekly one is however many fit in the next year.
	now := time.Now()
	next, err := expand([]mirror.Object{o}, now.Add(-24*time.Hour), now.AddDate(1, 0, 0))
	if err != nil {
		return resp.api(err.Error())
	}
	if len(next) > 5 {
		next = next[:5]
	}
	out.Next = next
	return resp.ok(out)
}

// expand turns objects into the instances that fall in the window, in order.
func expand(objects []mirror.Object, from, to time.Time) ([]occurrence, error) {
	// Built empty rather than nil: a nil slice is dropped by `omitempty` and
	// reaches the caller as null, which is not an empty agenda, it is a
	// listing that failed to say it found nothing.
	out := []occurrence{}
	for _, o := range objects {
		if o.UID == habit.UID {
			// The habits record is storage that happens to be shaped like an
			// event. It is not something anybody has on that day (ADR-0018).
			continue
		}
		instances, err := vcal.Occurrences(o.Raw, from, to, time.Local)
		if err != nil {
			// One unparseable object must not empty the agenda. The raw is
			// still in the Mirror and `event view` will show it.
			continue
		}
		for _, in := range instances {
			out = append(out, viewOccurrence(o, in))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start == out[j].Start {
			return out[i].Summary < out[j].Summary
		}
		return out[i].Start < out[j].Start
	})
	return out, nil
}

func viewOccurrence(o mirror.Object, in vcal.Occurrence) occurrence {
	start, end := in.Start.Local(), in.End.Local()
	row := occurrence{
		ID: o.ID, Calendar: o.Collection, UID: in.UID, Summary: in.Summary,
		Location: in.Location, AllDay: in.AllDay, Recurring: in.Recurring,
		Status: in.Status, Notes: o.Description, URL: in.URL,
		Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
		Date: start.Format("2006-01-02"),
	}
	if in.AllDay {
		row.Time = "all day"
	} else {
		row.Time = start.Format("15:04") + "–" + end.Format("15:04")
	}
	if row.Summary == "" {
		row.Summary = o.Summary
	}
	return row
}

// window reads the days a caller asked about. The default is a week from
// today, because "what is coming up" is the question an agenda answers.
func window(req Request) (from, to time.Time, err error) {
	from = startOfDay(time.Now())
	if v := req.Str("from"); v != "" {
		parsed, perr := time.ParseInLocation("2006-01-02", strings.TrimSpace(v), time.Local)
		if perr != nil {
			return from, to, fmt.Errorf("--from takes a date like 2026-08-29, got %q", v)
		}
		from = parsed
	}
	days := 7
	if v, ok := req.Args["days"].(float64); ok && v != 0 {
		days = int(v)
	}
	if days < 0 {
		return from, to, fmt.Errorf("--days cannot be negative")
	}
	if days == 0 {
		days = 1
	}
	return from, from.AddDate(0, 0, days), nil
}

// davCycle reconciles collections of the kinds named and reports what moved.
// They are polled rather than watched: there is no IDLE for DAV and nothing
// here is worth a connection held open (ADR-0010).
func (d *Daemon) davCycle(ctx context.Context, reason string, kinds ...string) {
	if d.DAV == nil {
		return
	}
	d.setDAVSyncing(true)
	defer d.setDAVSyncing(false)
	outcomes, err := d.DAV.SyncKinds(ctx, kinds...)
	if err != nil {
		d.logf("dav cycle (%s): %v", reason, err)
	}
	// Nothing came back and something failed: the server is unreachable, not
	// one bad collection. Anything less than that counts as reached, which is
	// what the freshness a caller reads is about.
	d.setDAVConnected(!(len(outcomes) == 0 && err != nil))
	// What each collection did is reported by the reconciler as it finishes,
	// which is where a caller watching a cold start wants it. Here there is
	// only the nudge: a widget that gets one re-reads (ADR-0011).
	for name, out := range outcomes {
		if out.Changed == 0 && out.Deleted == 0 {
			continue
		}
		d.push(Push{Event: "calendar.changed", Account: d.Account, Box: name})
	}
}

// davLoop runs the DAV cycles, one at a time, forever: the timer's and the ones
// a command asked for. It is the mail cycleLoop's counterpart, and it exists
// for the same reason — two cycles at once would both ask from the same sync
// token, and the one that finished second would commit the older answer over
// the newer one.
//
// Discovery is repeated on every cycle but costs three requests: a calendar
// created in webmail should simply appear, the same way a Box does.
func (d *Daemon) davLoop(ctx context.Context) {
	if d.DAV == nil {
		return
	}
	every := d.DAVEvery
	if every <= 0 {
		every = 10 * time.Minute
	}
	// An address book changes a few times a year and costs 142 objects to read
	// from nothing, so it does not ride the ten-minute timer (ADR-0010).
	const cardsEvery = 24 * time.Hour
	var lastCards time.Time

	run := func(k davKick) {
		if _, err := d.DAV.Discover(ctx); err != nil {
			d.logf("dav discover: %v", err)
			d.setDAVConnected(false)
		}
		kinds := k.kinds
		if len(kinds) == 0 {
			kinds = []string{"events", "tasks"}
			if time.Since(lastCards) >= cardsEvery {
				kinds = append(kinds, "cards")
			}
		}
		for _, kind := range kinds {
			if kind == "cards" {
				lastCards = time.Now()
			}
		}
		d.davCycle(ctx, k.reason, kinds...)
	}

	run(davKick{reason: "startup"})
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run(davKick{reason: "poll"})
		case k := <-d.davTrigger:
			run(k)
		}
	}
}

// How often a command may ask for a cycle of one kind. Calendars and task lists
// are what a caller is usually looking at and cost one request each to ask
// about, so they are nudged freely; an address book changes a few times a year
// and does not deserve the same (ADR-0010).
const (
	nudgeEvery      = 30 * time.Second
	nudgeCardsEvery = time.Hour
)

// nudgeDAV asks for a cycle over the kinds a command reads, and does not wait
// for it. The command it came from answers from the Mirror as it always did
// (ADR-0001); what this buys is the read after it, which is the one that shows
// the todo somebody deleted on their phone. Without it a caller has no way to
// make anything happen and no reason not to fall into "sync, then read" before
// every single call.
//
// Every command of a kind nudges, not only the reads. Telling them apart would
// mean naming every mutating verb, and the rate limit below already makes the
// difference too small to be worth the list.
func (d *Daemon) nudgeDAV(kinds ...string) {
	if d.DAV == nil || d.davTrigger == nil {
		return
	}
	now := time.Now()
	d.mu.Lock()
	var want []string
	for _, k := range kinds {
		every := nudgeEvery
		if k == "cards" {
			every = nudgeCardsEvery
		}
		if last, ok := d.davNudged[k]; ok && now.Sub(last) < every {
			continue
		}
		want = append(want, k)
	}
	d.mu.Unlock()
	if len(want) == 0 {
		return
	}
	select {
	case d.davTrigger <- davKick{reason: "read", kinds: want}:
		// Recorded only once it is queued. Recording on the way in would let a
		// nudge that got dropped below hold the door shut for the next half
		// minute, having caused nothing.
		d.mu.Lock()
		if d.davNudged == nil {
			d.davNudged = map[string]time.Time{}
		}
		for _, k := range want {
			d.davNudged[k] = now
		}
		d.mu.Unlock()
	default:
		// A cycle is already queued and will run in a moment. A second one
		// behind it would ask the same question of the same server.
	}
}
