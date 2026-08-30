// Package habit is the record a Habit is kept in.
//
// A Habit is neither an Event nor a Todo: completing one day does not end it,
// and it has no time. iCalendar has no component for that, so all of them live
// in one object on a calendar of their own, as JSON in its DESCRIPTION
// (ADR-0018). One object makes completing a day a single write, and it is the
// format the previous program already put this account's habits in.
package habit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CalendarName is the collection the object lives on, and UID is the object.
// Both are fixed: this is the one collection this program creates rather than
// discovers.
const (
	CalendarName = "mailbox-habits"
	UID          = "mailbox-habits"
)

// Habit is one practice, and the days it was done.
type Habit struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Days  []string `json:"days"`
	Done  []string `json:"done"`
	Color string   `json:"color,omitempty"`
	Icon  string   `json:"icon,omitempty"`
}

// Bag is every Habit, which is what the one object holds.
type Bag struct {
	Habits []Habit `json:"habits"`
}

// Weekdays in the order a week is read, and the only spellings stored.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var dayAlias = map[string]string{
	"mon": "mon", "monday": "mon", "montag": "mon", "1": "mon",
	"tue": "tue", "tues": "tue", "tuesday": "tue", "dienstag": "tue", "2": "tue",
	"wed": "wed", "wednesday": "wed", "mittwoch": "wed", "3": "wed",
	"thu": "thu", "thur": "thu", "thurs": "thu", "thursday": "thu", "donnerstag": "thu", "4": "thu",
	"fri": "fri", "friday": "fri", "freitag": "fri", "5": "fri",
	"sat": "sat", "saturday": "sat", "samstag": "sat", "6": "sat",
	"sun": "sun", "sunday": "sun", "sonntag": "sun", "0": "sun", "7": "sun",
}

// ParseDays reads what a caller typed. Nothing means every day, which is what
// most habits are.
func ParseDays(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return append([]string(nil), Weekdays...), nil
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		day, ok := dayAlias[key]
		if !ok {
			return nil, fmt.Errorf("%q is not a day: try mon,tue,wed,thu,fri,sat,sun", part)
		}
		if !seen[day] {
			seen[day] = true
			out = append(out, day)
		}
	}
	sort.Slice(out, func(i, j int) bool { return indexOf(out[i]) < indexOf(out[j]) })
	return out, nil
}

func indexOf(day string) int {
	for i, d := range Weekdays {
		if d == day {
			return i
		}
	}
	return len(Weekdays)
}

// DayOf is the short name of the weekday a date falls on.
func DayOf(t time.Time) string {
	switch t.Weekday() {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	case time.Saturday:
		return "sat"
	default:
		return "sun"
	}
}

// Decode reads the bag out of the object's description. An empty description is
// an empty bag rather than an error: that is what a calendar with no habits yet
// looks like.
func Decode(description string) (Bag, error) {
	if strings.TrimSpace(description) == "" {
		return Bag{}, nil
	}
	var bag Bag
	if err := json.Unmarshal([]byte(description), &bag); err != nil {
		return Bag{}, fmt.Errorf("the habits record is not readable: %w", err)
	}
	return bag, nil
}

// Encode writes the bag back.
func Encode(bag Bag) (string, error) {
	raw, err := json.Marshal(bag)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Find locates a Habit by id or by name, refusing an ambiguous one rather than
// guessing which practice somebody meant to tick off.
func (b *Bag) Find(what string) (*Habit, error) {
	want := strings.ToLower(strings.TrimSpace(what))
	if want == "" {
		return nil, fmt.Errorf("which habit?")
	}
	var hits []*Habit
	for i := range b.Habits {
		h := &b.Habits[i]
		if strings.EqualFold(h.ID, what) || strings.HasPrefix(strings.ToLower(h.ID), want) ||
			strings.EqualFold(h.Name, what) {
			hits = append(hits, h)
		}
	}
	if len(hits) == 0 {
		// A name somebody typed loosely: "medi" for "Meditation".
		for i := range b.Habits {
			h := &b.Habits[i]
			if strings.Contains(strings.ToLower(h.Name), want) {
				hits = append(hits, h)
			}
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no habit called %q", what)
	case 1:
		return hits[0], nil
	default:
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.Name)
		}
		return nil, fmt.Errorf("%q could be any of: %s", what, strings.Join(names, ", "))
	}
}

// Due reports whether this Habit is meant to be done on that weekday.
func (h Habit) Due(day string) bool {
	if len(h.Days) == 0 {
		return true
	}
	for _, d := range h.Days {
		if d == day {
			return true
		}
	}
	return false
}

// DoneOn reports whether it was done on a date, written 2006-01-02.
func (h Habit) DoneOn(date string) bool {
	for _, d := range h.Done {
		if d == date {
			return true
		}
	}
	return false
}

// Complete records a day. Doing it twice is doing it once: a Habit is a fact
// about a day, not a counter.
func (h *Habit) Complete(date string) {
	if !h.DoneOn(date) {
		h.Done = append(h.Done, date)
		sort.Strings(h.Done)
	}
}

// Uncomplete takes a day back.
func (h *Habit) Uncomplete(date string) {
	var keep []string
	for _, d := range h.Done {
		if d != date {
			keep = append(keep, d)
		}
	}
	h.Done = keep
}

// Streak counts the days up to and including `on` that this Habit was due and
// done, stopping at the first day it was due and missed. A day it was not due
// breaks nothing.
func (h Habit) Streak(on time.Time) int {
	streak := 0
	for i := 0; i < 366; i++ {
		day := on.AddDate(0, 0, -i)
		if !h.Due(DayOf(day)) {
			continue
		}
		if !h.DoneOn(day.Format("2006-01-02")) {
			// Today not being done yet is not a broken streak; it is today.
			if i == 0 {
				continue
			}
			return streak
		}
		streak++
	}
	return streak
}
