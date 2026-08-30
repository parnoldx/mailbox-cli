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
	verb := "list"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	switch verb {
	case "list":
		list, _ := req.Args["list"].(string)
		all, _ := req.Args["all"].(bool)
		objects, err := d.Mirror.Todos(d.Account, list, all)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := make([]todo, 0, len(objects))
		for _, o := range objects {
			out = append(out, viewTodo(o))
		}
		resp.OK, resp.Data = true, out
		return resp

	case "add":
		summary, _ := req.Args["positional"].(string)
		if strings.TrimSpace(summary) == "" {
			resp.Code, resp.Error = "usage", "a todo needs something to say"
			return resp
		}
		col, err := d.taskList(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		due, isDate, err := dueDate(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		priority, err := vcal.PriorityNumber(str(req.Args["priority"]))
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		uid := vcal.NewUID()
		raw, err := vcal.NewTodo(uid, summary, due, isDate, priority)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		object, err := d.write(ctx, col, davsync.Href(col, uid), raw, "")
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewTodo(object)
		return resp

	case "done", "undone", "rename", "drop":
		return d.changeTodo(ctx, verb, req, resp)
	}
	resp.Code, resp.Error = "usage", fmt.Sprintf("unknown todo command %q", verb)
	return resp
}

// changeTodo applies one change to one Todo and stores what the server ended up
// with.
func (d *Daemon) changeTodo(ctx context.Context, verb string, req Request, resp Response) Response {
	if d.DAVWriter == nil {
		resp.Code, resp.Error = "api", "this daemon cannot write: no dav connection"
		return resp
	}
	id, err := objectID(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	object, err := d.Mirror.Object(d.Account, id)
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", fmt.Sprintf("no todo %d in the mirror", id)
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

	if verb == "drop" {
		if err := d.DAVWriter.Delete(ctx, col, object); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		d.push(Push{Event: "todo.changed", Account: d.Account, Box: col.Name})
		resp.OK, resp.Data = true, map[string]any{"id": id, "state": "dropped", "summary": object.Summary}
		return resp
	}

	var raw string
	switch verb {
	case "done":
		raw, err = vcal.Complete(object.Raw, time.Now())
	case "undone":
		raw, err = vcal.Uncomplete(object.Raw)
	case "rename":
		title, _ := req.Args["title"].(string)
		if strings.TrimSpace(title) == "" {
			resp.Code, resp.Error = "usage", "rename needs --title"
			return resp
		}
		raw, err = vcal.Rename(object.Raw, title)
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	// If-Match is the ETag we read. A Todo somebody else changed in between is
	// refused rather than overwritten: the next cycle brings their version and
	// the caller can decide again.
	written, err := d.write(ctx, col, object.Href, raw, object.ETag)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, viewTodo(written)
	return resp
}

// write puts an object and tells the listeners.
func (d *Daemon) write(ctx context.Context, col mirror.Collection, href, raw, ifMatch string) (mirror.Object, error) {
	if d.DAVWriter == nil {
		return mirror.Object{}, errors.New("this daemon cannot write: no dav connection")
	}
	object, err := d.DAVWriter.Put(ctx, col, href, raw, ifMatch)
	if err != nil {
		return mirror.Object{}, err
	}
	d.push(Push{Event: "todo.changed", Account: d.Account, Box: col.Name})
	return object, nil
}

// taskList picks where a new Todo goes. One list needs no naming; several do,
// because "add milk" landing on the work list is worse than being asked.
func (d *Daemon) taskList(req Request) (mirror.Collection, error) {
	name, _ := req.Args["list"].(string)
	if name == "" {
		name = d.defaultTaskList()
	}
	lists, err := d.Mirror.Collections(d.Account, "tasks")
	if err != nil {
		return mirror.Collection{}, err
	}
	if len(lists) == 0 {
		return mirror.Collection{}, errors.New("there are no task lists on this account")
	}
	if name != "" {
		for _, c := range lists {
			if strings.EqualFold(c.Name, name) {
				return c, nil
			}
		}
		return mirror.Collection{}, fmt.Errorf("no task list called %q", name)
	}
	if len(lists) == 1 {
		return lists[0], nil
	}
	names := make([]string, 0, len(lists))
	for _, c := range lists {
		names = append(names, c.Name)
	}
	return mirror.Collection{}, fmt.Errorf("there are %d task lists — name one with --list: %s",
		len(lists), strings.Join(names, ", "))
}

func (d *Daemon) collectionOf(o mirror.Object) (mirror.Collection, error) {
	cols, err := d.Mirror.Collections(d.Account, "")
	if err != nil {
		return mirror.Collection{}, err
	}
	for _, c := range cols {
		if c.ID == o.CollectionID {
			return c, nil
		}
	}
	return mirror.Collection{}, fmt.Errorf("the collection %q is no longer in the mirror", o.Collection)
}

// dueDate reads --due, and says whether what it was given was a bare date. A
// bare date is a date: "by Friday" does not mean 00:00 on Friday, and storing
// it as one makes every client show a time nobody meant. A date with a clock on
// it is the other thing people mean — "by 17:00 on Friday" — and that one keeps
// its hour.
func dueDate(req Request) (time.Time, bool, error) {
	raw, _ := req.Args["due"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	// A word and a time is how somebody says it out loud: "tomorrow 09:00".
	word, clock, _ := strings.Cut(raw, " ")
	if day, ok := relativeDay(word); ok {
		if strings.TrimSpace(clock) == "" {
			return day, true, nil
		}
		at, err := time.ParseInLocation("15:04", strings.TrimSpace(clock), time.Local)
		if err != nil {
			return time.Time{}, false, dueError(raw)
		}
		return day.Add(time.Duration(at.Hour())*time.Hour + time.Duration(at.Minute())*time.Minute), false, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, dueError(raw)
}

func dueError(raw string) error {
	return fmt.Errorf("--due takes 2026-09-01, 2026-09-01 17:00, today, or tomorrow — got %q", raw)
}

// relativeDay reads the two words a caller uses instead of a date.
func relativeDay(word string) (time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "today":
		return startOfDay(time.Now()), true
	case "tomorrow":
		return startOfDay(time.Now().AddDate(0, 0, 1)), true
	}
	return time.Time{}, false
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
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
	verb := "list"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	on, err := habitDate(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}

	if verb == "list" {
		bag, _, _, err := d.habits(ctx, false)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewHabits(bag, on)
		return resp
	}

	// Everything else changes the object, so the collection has to exist.
	bag, col, object, err := d.habits(ctx, true)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	name, _ := req.Args["positional"].(string)
	date := on.Format("2006-01-02")

	switch verb {
	case "add":
		if strings.TrimSpace(name) == "" {
			resp.Code, resp.Error = "usage", "a habit needs a name"
			return resp
		}
		days, derr := habit.ParseDays(str(req.Args["days"]))
		if derr != nil {
			resp.Code, resp.Error = "usage", derr.Error()
			return resp
		}
		bag.Habits = append(bag.Habits, habit.Habit{
			ID: vcal.NewUID(), Name: strings.TrimSpace(name), Days: days,
			Color: str(req.Args["color"]), Icon: str(req.Args["icon"]),
		})
	case "edit":
		h, ferr := bag.Find(name)
		if ferr != nil {
			resp.Code, resp.Error = "not_found", ferr.Error()
			return resp
		}
		// Only what was named changes. An edit that reset the days because the
		// caller was renaming would lose a schedule nobody meant to touch, and
		// a habit's days are the part hardest to reconstruct.
		if v := strings.TrimSpace(str(req.Args["title"])); v != "" {
			h.Name = v
		}
		if v := str(req.Args["days"]); v != "" {
			days, derr := habit.ParseDays(v)
			if derr != nil {
				resp.Code, resp.Error = "usage", derr.Error()
				return resp
			}
			h.Days = days
		}
		if v := str(req.Args["color"]); v != "" {
			h.Color = v
		}
		if v := str(req.Args["icon"]); v != "" {
			h.Icon = v
		}
	case "done", "undone", "drop":
		h, ferr := bag.Find(name)
		if ferr != nil {
			resp.Code, resp.Error = "not_found", ferr.Error()
			return resp
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
		resp.Code, resp.Error = "usage", fmt.Sprintf("unknown habit command %q", verb)
		return resp
	}

	if err := d.saveHabits(ctx, col, object, bag); err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, viewHabits(bag, on)
	return resp
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
	id, err := d.Mirror.PutCollection(mirror.Collection{
		Account: d.Account, Kind: found.Kind, URL: found.URL, Name: found.Name, Color: found.Color,
	})
	if err != nil {
		return mirror.Collection{}, err
	}
	col, err := d.Mirror.CollectionNamed(d.Account, "", habit.CalendarName)
	if err != nil {
		return mirror.Collection{}, err
	}
	_ = id
	return col, nil
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
	if _, err := d.write(ctx, col, href, raw, ifMatch); err != nil {
		return err
	}
	d.push(Push{Event: "habit.changed", Account: d.Account, Box: col.Name})
	return nil
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

// habitDate reads --date, defaulting to today. A Habit is a fact about a day,
// so which day has to be answerable.
func habitDate(req Request) (time.Time, error) {
	raw := strings.TrimSpace(str(req.Args["date"]))
	switch strings.ToLower(raw) {
	case "", "today":
		return startOfDay(time.Now()), nil
	case "yesterday":
		return startOfDay(time.Now().AddDate(0, 0, -1)), nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("--date takes a date like 2026-08-29, or today, or yesterday — got %q", raw)
	}
	return t, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
