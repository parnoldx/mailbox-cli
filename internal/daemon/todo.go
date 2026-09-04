package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
)

// todo is one entry of a task list.
type todo struct {
	ID      int64  `json:"id"`
	List    string `json:"list"`
	UID     string `json:"uid"`
	Summary string `json:"summary"`
	Due     string `json:"due,omitempty"`
	// Priority is high, medium or low, or empty for the ordinary case of
	// nobody having said. The number iCalendar stores is in the raw.
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
	Done     bool   `json:"done"`
	// Overdue is worth saying rather than leaving to the caller to work out
	// from a date it would have to parse.
	Overdue bool `json:"overdue,omitempty"`
}

// handleTodo answers about the task lists, and changes them. Listing is a
// Mirror read; adding and completing block on the server and update the Mirror
// from the ack (ADR-0004).
func (d *Daemon) handleTodo(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("list")
	switch verb {
	case "list":
		list := req.Str("list")
		all := req.Bool("all")
		objects, err := d.Mirror.Todos(d.Account, list, all)
		if err != nil {
			return resp.api(err.Error())
		}
		out := make([]todo, 0, len(objects))
		for _, o := range objects {
			out = append(out, viewTodo(o))
		}
		return resp.ok(out)

	case "add":
		summary := req.Str("positional")
		if strings.TrimSpace(summary) == "" {
			return resp.usage("a todo needs something to say")
		}
		col, err := d.pick(taskLists, or(req.Str("list"), d.defaultTaskList()))
		if err != nil {
			return resp.failed(err)
		}
		due, isDate, err := dueDate(req)
		if err != nil {
			return resp.usage(err.Error())
		}
		priority, err := vcal.PriorityNumber(req.Str("priority"))
		if err != nil {
			return resp.usage(err.Error())
		}
		uid := vcal.NewUID()
		raw, err := vcal.NewTodo(uid, summary, due, isDate, priority)
		if err != nil {
			return resp.api(err.Error())
		}
		object, err := d.put(ctx, todoChanged, col, davsync.Href(col, uid), raw, "")
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(viewTodo(object))

	case "done", "undone", "rename", "drop":
		return d.changeTodo(ctx, verb, req, resp)
	}
	return resp.usage(fmt.Sprintf("unknown todo command %q", verb))
}

// changeTodo applies one change to one Todo and stores what the server ended up
// with.
func (d *Daemon) changeTodo(ctx context.Context, verb string, req Request, resp Response) Response {
	object, col, err := d.load(req, "todo")
	if err != nil {
		return resp.failed(err)
	}

	if verb == "drop" {
		if err := d.remove(ctx, todoChanged, col, object); err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(map[string]any{"id": object.ID, "state": "dropped", "summary": object.Summary})
	}

	var raw string
	switch verb {
	case "done":
		raw, err = vcal.Complete(object.Raw, time.Now())
	case "undone":
		raw, err = vcal.Uncomplete(object.Raw)
	case "rename":
		title := req.Str("title")
		if strings.TrimSpace(title) == "" {
			return resp.usage("rename needs --title")
		}
		raw, err = vcal.Rename(object.Raw, title)
	}
	if err != nil {
		return resp.api(err.Error())
	}
	// If-Match is the ETag we read. A Todo somebody else changed in between is
	// refused rather than overwritten: the next cycle brings their version and
	// the caller can decide again.
	written, err := d.put(ctx, todoChanged, col, object.Href, raw, object.ETag)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewTodo(written))
}

func viewTodo(o mirror.Object) todo {
	row := todo{
		ID: o.ID, List: o.Collection, UID: o.UID, Summary: o.Summary,
		Priority: vcal.PriorityWord(o.Priority),
		Status:   o.Status, Done: o.Status == "COMPLETED" || !o.Completed.IsZero(),
	}
	if !o.Due.IsZero() {
		due := o.Due.Local()
		row.Due = due.Format("2006-01-02")
		if !o.DueAllDay {
			// The hour is only shown when somebody set one. A todo due "by
			// Friday" showing 00:00 reads as a deadline nobody agreed to.
			row.Due = due.Format("2006-01-02 15:04")
		}
		// A todo due today is not late yet. A dated one runs out at the end of
		// its day; one with a clock on it runs out at that hour.
		deadline := due
		if o.DueAllDay {
			deadline = startOfDay(due).AddDate(0, 0, 1)
		}
		row.Overdue = !row.Done && deadline.Before(time.Now())
	}
	return row
}

// habitRow is one Habit as a caller sees it today.
type habitRow struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Days   []string `json:"days"`
	Due    bool     `json:"due"`
	Done   bool     `json:"done"`
	Streak int      `json:"streak"`
	Date   string   `json:"date"`
	Color  string   `json:"color,omitempty"`
	Icon   string   `json:"icon,omitempty"`
}

// handleHabit lists and ticks off Habits. They live in one object on their own
// calendar, so every change here is one read and one write of that object
// (ADR-0018).
func (d *Daemon) handleHabit(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("list")
	on, err := habitDate(req)
	if err != nil {
		return resp.usage(err.Error())
	}

	if verb == "list" {
		bag, _, _, err := d.habits(ctx, false)
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(viewHabits(bag, on))
	}

	// Everything else changes the object, so the collection has to exist.
	bag, col, object, err := d.habits(ctx, true)
	if err != nil {
		return resp.api(err.Error())
	}
	name := req.Str("positional")
	date := on.Format("2006-01-02")

	switch verb {
	case "add":
		if strings.TrimSpace(name) == "" {
			return resp.usage("a habit needs a name")
		}
		days, derr := habit.ParseDays(req.Str("days"))
		if derr != nil {
			return resp.usage(derr.Error())
		}
		bag.Habits = append(bag.Habits, habit.Habit{
			ID: vcal.NewUID(), Name: strings.TrimSpace(name), Days: days,
			Color: req.Str("color"), Icon: req.Str("icon"),
		})
	case "edit":
		h, ferr := bag.Find(name)
		if ferr != nil {
			return resp.notFound(ferr.Error())
		}
		// Only what was named changes. An edit that reset the days because the
		// caller was renaming would lose a schedule nobody meant to touch, and
		// a habit's days are the part hardest to reconstruct.
		if v := strings.TrimSpace(req.Str("title")); v != "" {
			h.Name = v
		}
		if v := req.Str("days"); v != "" {
			days, derr := habit.ParseDays(v)
			if derr != nil {
				return resp.usage(derr.Error())
			}
			h.Days = days
		}
		if v := req.Str("color"); v != "" {
			h.Color = v
		}
		if v := req.Str("icon"); v != "" {
			h.Icon = v
		}
	case "done", "undone", "drop":
		h, ferr := bag.Find(name)
		if ferr != nil {
			return resp.notFound(ferr.Error())
		}
		switch verb {
		case "done":
			h.Complete(date)
		case "undone":
			h.Uncomplete(date)
		case "drop":
			var keep []habit.Habit
			for _, other := range bag.Habits {
				if other.ID != h.ID {
					keep = append(keep, other)
				}
			}
			bag.Habits = keep
		}
	default:
		return resp.usage(fmt.Sprintf("unknown habit command %q", verb))
	}

	if err := d.saveHabits(ctx, col, object, bag); err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewHabits(bag, on))
}

// habits reads the Habit record. create says whether to make the calendar and
// the object if they are not there yet — a list must not create anything, and a
// change has to.
func (d *Daemon) habits(ctx context.Context, create bool) (habit.Bag, mirror.Collection, mirror.Object, error) {
	col, err := d.Mirror.CollectionNamed(d.Account, "", habit.CalendarName)
	if errors.Is(err, mirror.ErrNotFound) {
		if !create {
			return habit.Bag{}, mirror.Collection{}, mirror.Object{}, nil
		}
		col, err = d.makeHabitsCalendar(ctx)
	}
	if err != nil {
		return habit.Bag{}, mirror.Collection{}, mirror.Object{}, err
	}
	object, err := d.Mirror.ObjectByUID(d.Account, habit.UID)
	if errors.Is(err, mirror.ErrNotFound) {
		return habit.Bag{}, col, mirror.Object{}, nil
	}
	if err != nil {
		return habit.Bag{}, col, mirror.Object{}, err
	}
	description, err := vcal.Description(object.Raw)
	if err != nil {
		return habit.Bag{}, col, object, err
	}
	bag, err := habit.Decode(description)
	return bag, col, object, err
}

// makeHabitsCalendar creates the one collection this program owns rather than
// discovers, and records it.
func (d *Daemon) makeHabitsCalendar(ctx context.Context) (mirror.Collection, error) {
	if d.DAVHome == nil {
		return mirror.Collection{}, errors.New("this daemon cannot create the habits calendar")
	}
	found, err := d.DAVHome.EnsureCalendar(ctx, habit.CalendarName, []string{"VEVENT"})
	if err != nil {
		return mirror.Collection{}, err
	}
	if _, err := d.Mirror.PutCollection(mirror.Collection{
		Account: d.Account, Kind: found.Kind, URL: found.URL, Name: found.Name, Color: found.Color,
	}); err != nil {
		return mirror.Collection{}, err
	}
	// Read back rather than built from `found`: the caller needs the row as the
	// Mirror holds it, id and all, and that is what a write to a collection is
	// addressed by.
	return d.Mirror.CollectionNamed(d.Account, "", habit.CalendarName)
}

// saveHabits writes the record back, in one PUT.
func (d *Daemon) saveHabits(ctx context.Context, col mirror.Collection, object mirror.Object, bag habit.Bag) error {
	description, err := habit.Encode(bag)
	if err != nil {
		return err
	}
	// Dated today on every write, because the server only lists a window of its
	// calendars and an object that falls out of it stops existing as far as
	// every listing is concerned (ADR-0018).
	//
	// An object we already have is *edited* rather than rebuilt. The server
	// keeps its own SEQUENCE and LAST-MODIFIED on what it stored, and a PUT of
	// a newly built object without them is refused as an outdated update.
	var raw string
	if object.Raw != "" {
		raw, err = vcal.SetDescription(object.Raw, description, time.Now())
	} else {
		raw, err = vcal.NewEventObject(habit.UID, habit.CalendarName, description, time.Now())
	}
	if err != nil {
		return err
	}
	href, ifMatch := object.Href, object.ETag
	if href == "" {
		href = davsync.Href(col, habit.UID)
	}
	_, err = d.put(ctx, habitChanged, col, href, raw, ifMatch)
	return err
}

func viewHabits(bag habit.Bag, on time.Time) []habitRow {
	day := habit.DayOf(on)
	date := on.Format("2006-01-02")
	out := make([]habitRow, 0, len(bag.Habits))
	for _, h := range bag.Habits {
		out = append(out, habitRow{
			ID: h.ID, Name: h.Name, Days: h.Days, Due: h.Due(day),
			Done: h.DoneOn(date), Streak: h.Streak(on), Date: date,
			Color: h.Color, Icon: h.Icon,
		})
	}
	return out
}
