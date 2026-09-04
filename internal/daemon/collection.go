package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"mailbox/internal/habit"
	"mailbox/internal/mirror"
)

// The three kinds of Collection, and how each one is spoken about: what it is
// called when an error has to name it, and the flag that picks one. Calendars,
// task lists and address books differ in nothing else, so they are described
// here once rather than implemented three times.
var (
	calendars    = kindOf{"events", "calendar", "--calendar"}
	taskLists    = kindOf{"tasks", "task list", "--list"}
	addressBooks = kindOf{"cards", "address book", "--book"}
)

type kindOf struct {
	kind string // as the Mirror stores it
	noun string // singular; the plural is this plus an s, for all three
	flag string
}

// pick is where a new object goes: the one the caller named, or the only one
// there is. Naming becomes required as soon as there is a choice, because an
// appointment on the wrong calendar and "add milk" on the work list are both
// worse than being asked which.
//
// Every error here is a usage error — the caller named a collection that is not
// there, or did not name one when they had to — so they carry their own Code
// and a handler can hand them straight to Response.failed.
func (d *Daemon) pick(k kindOf, name string) (mirror.Collection, error) {
	all, err := d.Mirror.Collections(d.Account, k.kind)
	if err != nil {
		return mirror.Collection{}, err
	}
	// The habits record lives on a calendar of its own. It is storage shaped
	// like a calendar, and anything put there is written over by the next habit
	// tick, so it is never a target (ADR-0018).
	open := make([]mirror.Collection, 0, len(all))
	for _, c := range all {
		if k.kind == calendars.kind && c.Name == habit.CalendarName {
			continue
		}
		open = append(open, c)
	}
	if len(open) == 0 {
		return mirror.Collection{}, usageErr("there are no %ss on this account", k.noun)
	}
	if name != "" {
		for _, c := range open {
			if strings.EqualFold(c.Name, name) {
				return c, nil
			}
		}
		return mirror.Collection{}, usageErr("no %s called %q", k.noun, name)
	}
	if len(open) == 1 {
		return open[0], nil
	}
	names := make([]string, 0, len(open))
	for _, c := range open {
		names = append(names, c.Name)
	}
	return mirror.Collection{}, usageErr("there are %d %ss — name one with %s: %s",
		len(open), k.noun, k.flag, strings.Join(names, ", "))
}

// collectionOf is the Collection an object sits on, by the id the object
// carries. A miss means the collection went away between the read and now,
// which a rebuilt Mirror can do.
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

// load reads the object a change command named, with the Collection it sits on.
// It is the opening of every edit, complete or delete: the id is resolved
// against the Mirror before anything is sent, so a stale id fails as the
// not_found it is rather than as a server error about a href.
//
// noun is what the thing is called when the id names nothing — "todo", "event",
// "contact" — which is the message a caller sees most often, an id from a
// listing that has since been synced away.
func (d *Daemon) load(req Request, noun string) (mirror.Object, mirror.Collection, error) {
	if d.DAVWriter == nil {
		return mirror.Object{}, mirror.Collection{}, errNoDAVWriter
	}
	id, err := objectID(req, noun)
	if err != nil {
		return mirror.Object{}, mirror.Collection{}, usageErr("%s", err)
	}
	object, err := d.Mirror.Object(d.Account, id)
	if errors.Is(err, mirror.ErrNotFound) {
		return mirror.Object{}, mirror.Collection{}, notFoundErr("no %s %d in the mirror", noun, id)
	}
	if err != nil {
		return mirror.Object{}, mirror.Collection{}, err
	}
	col, err := d.collectionOf(object)
	return object, col, err
}

// objectID reads the Mirror id a command was given. noun is what the thing is
// called in the error — "event", "todo", "contact" — because the same reader
// serves all three and being told an event id is wanted for a `todo done` is
// the sort of small lie that costs somebody a minute.
func objectID(req Request, noun string) (int64, error) {
	raw := req.Text("positional")
	if raw == "" {
		return 0, fmt.Errorf("no %s id given", noun)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s id must be a number, got %q", noun, raw)
	}
	return id, nil
}

// put writes one object to the server and tells the listeners what kind of
// thing moved. The Mirror is updated from the ack rather than left to the next
// cycle, so a read straight after a write sees it (ADR-0004).
func (d *Daemon) put(ctx context.Context, event string, col mirror.Collection, href, raw, ifMatch string) (mirror.Object, error) {
	if d.DAVWriter == nil {
		return mirror.Object{}, errNoDAVWriter
	}
	object, err := d.DAVWriter.Put(ctx, col, href, raw, ifMatch)
	if err != nil {
		return mirror.Object{}, err
	}
	d.push(Push{Event: event, Account: d.Account, Box: col.Name})
	return object, nil
}

// remove deletes one object from the server and tells the listeners the same
// thing put does. It is put's other half: an appointment, a todo and a card are
// deleted in exactly the same three steps, and only the noun in the reply
// differs.
func (d *Daemon) remove(ctx context.Context, event string, col mirror.Collection, o mirror.Object) error {
	if d.DAVWriter == nil {
		return errNoDAVWriter
	}
	if err := d.DAVWriter.Delete(ctx, col, o); err != nil {
		return err
	}
	d.push(Push{Event: event, Account: d.Account, Box: col.Name})
	return nil
}

// errNoDAVWriter is a Daemon configured for mail alone, or one whose DAV
// credentials did not work. Reading the Mirror still answers; writing cannot.
var errNoDAVWriter = errors.New("this daemon cannot write: no dav connection")

// The push events a DAV write raises. They carry no data: a widget that
// receives one re-reads (ADR-0011), so what a name has to do is say which of a
// caller's open views is now stale.
const (
	eventChanged   = "event.changed"
	todoChanged    = "todo.changed"
	habitChanged   = "habit.changed"
	contactChanged = "contact.changed"
)

// or is the first of two names that was actually given: what the caller typed,
// else the configured default.
func or(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}
