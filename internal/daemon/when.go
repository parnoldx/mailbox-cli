package daemon

import (
	"fmt"
	"strings"
	"time"
)

// Reading the dates a caller types. Every command that takes one — --due,
// --start, --end, --date — accepts the same spellings, so they are read in one
// place: a date that works on `todo add` and not on `event add` is a difference
// nobody would guess at.

// parseWhen reads an instant a caller wrote. isDay says they gave a bare date
// with no clock on it, which is a different thing from midnight: "by Friday"
// does not mean 00:00 on Friday, and storing it as one makes every client show
// a time nobody meant.
func parseWhen(raw string) (when time.Time, isDay, ok bool) {
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, false, true
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return t, true, true
	}
	return time.Time{}, false, false
}

// dueDate reads --due. On top of the spellings above it takes the two words
// somebody says out loud instead of a date, optionally with a time on them:
// "tomorrow 09:00".
func dueDate(req Request) (time.Time, bool, error) {
	raw := strings.TrimSpace(req.Str("due"))
	if raw == "" {
		return time.Time{}, false, nil
	}
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
	if when, isDay, ok := parseWhen(raw); ok {
		return when, isDay, nil
	}
	return time.Time{}, false, dueError(raw)
}

func dueError(raw string) error {
	return fmt.Errorf("--due takes 2026-09-01, 2026-09-01 17:00, today, or tomorrow — got %q", raw)
}

// eventTime reads one of --start or --end. Which spelling was given decides
// whether the event has a clock.
func eventTime(req Request, key string) (when time.Time, isDay bool, err error) {
	raw := strings.TrimSpace(req.Str(key))
	if raw == "" {
		return time.Time{}, false, nil
	}
	if when, isDay, ok := parseWhen(raw); ok {
		return when, isDay, nil
	}
	return time.Time{}, false, fmt.Errorf(
		"--%s takes 2026-09-01 or 2026-09-01 14:00, got %q", key, raw)
}

// habitDate reads --date, defaulting to today. A Habit is a fact about a day,
// so which day has to be answerable.
func habitDate(req Request) (time.Time, error) {
	raw := strings.TrimSpace(req.Str("date"))
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

// startOfDay is local midnight on t's day. time.Truncate works in UTC, so it
// cannot be used for this.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// atHour is day at a whole hour, local time.
func atHour(day time.Time, hour int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
}

// comingWeekday is the next target weekday on or after from. strict pushes a
// match on from itself to the following week — "next Monday" said on a Monday
// is not today.
func comingWeekday(from time.Time, target time.Weekday, strict bool) time.Time {
	delta := (int(target) - int(from.Weekday()) + 7) % 7
	if delta == 0 && strict {
		delta = 7
	}
	return from.AddDate(0, 0, delta)
}
