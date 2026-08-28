package calendar

import (
	"strconv"
	"strings"
	"time"

	"mailbox/src/internal/vobject"
)

type byday struct {
	nth int
	wd  time.Weekday
}

type rrule struct {
	freq       string
	interval   int
	count      int
	until      time.Time
	byday      []byday
	bymonthday []int
}

func parseRRule(s string) rrule {
	r := rrule{interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(k)) {
		case "FREQ":
			r.freq = strings.ToUpper(strings.TrimSpace(v))
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.count = n
			}
		case "UNTIL":
			r.until = parseUntil(v)
		case "BYDAY":
			r.byday = parseByday(v)
		case "BYMONTHDAY":
			for _, p := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
					r.bymonthday = append(r.bymonthday, n)
				}
			}
		}
	}
	return r
}

func parseUntil(v string) time.Time {
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("20060102T150405", v, TZ); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("20060102", v, TZ); err == nil {
		return t.Add(24*time.Hour - time.Second)
	}
	return time.Time{}
}

func parseByday(v string) []byday {
	var out []byday
	for _, p := range strings.Split(v, ",") {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		nth, i := 0, 0
		if p[0] == '+' || p[0] == '-' || (p[0] >= '1' && p[0] <= '9') {
			if p[0] == '+' || p[0] == '-' {
				i = 1
			}
			for i < len(p) && p[i] >= '0' && p[i] <= '9' {
				i++
			}
			if i > 0 && i < len(p) {
				nth, _ = strconv.Atoi(p[:i])
				p = p[i:]
			}
		}
		if wd, ok := weekdayName(p); ok {
			out = append(out, byday{nth: nth, wd: wd})
		}
	}
	return out
}

func weekdayName(s string) (time.Weekday, bool) {
	switch s {
	case "SU":
		return time.Sunday, true
	case "MO":
		return time.Monday, true
	case "TU":
		return time.Tuesday, true
	case "WE":
		return time.Wednesday, true
	case "TH":
		return time.Thursday, true
	case "FR":
		return time.Friday, true
	case "SA":
		return time.Saturday, true
	}
	return 0, false
}

func (r rrule) times(dtstart, from, to time.Time) []time.Time {
	if r.interval < 1 {
		r.interval = 1
	}
	var out []time.Time
	seen := 0
	done := false
	take := func(t time.Time) {
		if done || t.Before(dtstart) {
			return
		}
		if !r.until.IsZero() && t.After(r.until) {
			done = true
			return
		}
		seen++
		if !t.Before(from) && t.Before(to) {
			out = append(out, t)
		}
		if r.count > 0 && seen >= r.count {
			done = true
		}
	}

	loc := dtstart.Location()
	h, min, sec := dtstart.Clock()
	switch r.freq {
	case "DAILY":
		for t := dtstart; !done && t.Before(to); t = t.AddDate(0, 0, r.interval) {
			take(t)
		}
	case "WEEKLY":
		days := r.byday
		if len(days) == 0 {
			days = []byday{{wd: dtstart.Weekday()}}
		}
		week := mondayOf(dtstart)
		for !done && week.Before(to) {
			for shift := 0; shift < 7; shift++ {
				wd := time.Weekday((int(time.Monday) + shift) % 7)
				for _, d := range days {
					if d.wd != wd {
						continue
					}
					day := week.AddDate(0, 0, shift)
					take(time.Date(day.Year(), day.Month(), day.Day(), h, min, sec, 0, loc))
				}
			}
			week = week.AddDate(0, 0, 7*r.interval)
		}
	case "MONTHLY":
		y, m := dtstart.Year(), dtstart.Month()
		for !done {
			monthStart := time.Date(y, m, 1, 0, 0, 0, 0, loc)
			if !monthStart.Before(to) {
				break
			}
			if len(r.byday) > 0 {
				for _, d := range r.byday {
					if d.nth == 0 {
						for nth := 1; nth <= 5; nth++ {
							if t, ok := nthWeekday(y, m, nth, d.wd, dtstart); ok {
								take(t)
							}
						}
						continue
					}
					if t, ok := nthWeekday(y, m, d.nth, d.wd, dtstart); ok {
						take(t)
					}
				}
			} else {
				doms := r.bymonthday
				if len(doms) == 0 {
					doms = []int{dtstart.Day()}
				}
				for _, dom := range doms {
					if t, ok := onMonthDay(y, m, dom, dtstart); ok {
						take(t)
					}
				}
			}
			m += time.Month(r.interval)
			for m > 12 {
				y++
				m -= 12
			}
		}
	case "YEARLY":
		for t := dtstart; !done && t.Before(to); t = t.AddDate(r.interval, 0, 0) {
			take(t)
		}
	default:
		take(dtstart)
	}
	return out
}

func mondayOf(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	diff := (int(d.Weekday()) - int(time.Monday) + 7) % 7
	return d.AddDate(0, 0, -diff)
}

func onMonthDay(y int, m time.Month, day int, clock time.Time) (time.Time, bool) {
	h, min, sec := clock.Clock()
	t := time.Date(y, m, day, h, min, sec, 0, clock.Location())
	return t, t.Month() == m && t.Day() == day
}

func nthWeekday(y int, m time.Month, nth int, wd time.Weekday, clock time.Time) (time.Time, bool) {
	h, min, sec := clock.Clock()
	loc := clock.Location()
	if nth > 0 {
		first := time.Date(y, m, 1, h, min, sec, 0, loc)
		shift := (int(wd) - int(first.Weekday()) + 7) % 7
		t := time.Date(y, m, 1+shift+(nth-1)*7, h, min, sec, 0, loc)
		return t, t.Month() == m
	}
	last := time.Date(y, m+1, 0, h, min, sec, 0, loc)
	shift := (int(last.Weekday()) - int(wd) + 7) % 7
	t := last.AddDate(0, 0, -shift)
	return t, t.Month() == m
}

func parseEventTime(props []vobject.Prop, name string) (time.Time, bool, bool) {
	p := findProp(props, name)
	if p.Name == "" {
		return time.Time{}, false, false
	}
	if isDateProp(p) || (len(p.Value) == 8 && !strings.Contains(p.Value, "T")) {
		if t, err := time.ParseInLocation("20060102", p.Value, TZ); err == nil {
			return t, true, true
		}
	}
	t, err := parseICSDateTime(p)
	if err != nil {
		return time.Time{}, false, false
	}
	return t, false, true
}
