package calendar

import (
	"strings"
	"time"

	"mailbox/src/internal/format"
	"mailbox/src/internal/vobject"
)

func eventsFromICS(data, calName string, from, to time.Time) []*format.OM {
	grouped := map[string][][]vobject.Prop{}
	var order []string
	for _, ev := range vobject.Components(data, "VEVENT") {
		uid := vobject.First(ev, "UID")
		if _, ok := grouped[uid]; !ok {
			order = append(order, uid)
		}
		grouped[uid] = append(grouped[uid], ev)
	}
	var rows []*format.OM
	for _, uid := range order {
		rows = append(rows, expandSeries(grouped[uid], calName, from, to)...)
	}
	return rows
}

func expandSeries(evs [][]vobject.Prop, calName string, from, to time.Time) []*format.OM {
	master, overrides := splitSeries(evs)
	if master == nil {
		var rows []*format.OM
		for _, ev := range overrides {
			if row := eventIfInWindow(ev, calName, from, to); row != nil {
				rows = append(rows, row)
			}
		}
		return rows
	}
	rule := vobject.First(master, "RRULE")
	if rule == "" {
		var rows []*format.OM
		if row := eventIfInWindow(master, calName, from, to); row != nil {
			rows = append(rows, row)
		}
		for _, ev := range overrides {
			if row := eventIfInWindow(ev, calName, from, to); row != nil {
				rows = append(rows, row)
			}
		}
		return rows
	}

	start, allDay, ok := parseEventTime(master, "DTSTART")
	if !ok {
		return nil
	}
	end, _, hasEnd := parseEventTime(master, "DTEND")
	dur := time.Hour
	if allDay {
		dur = 24 * time.Hour
	}
	if hasEnd && end.After(start) {
		dur = end.Sub(start)
	}

	skip := exdateSet(master)
	overByRID := map[int64][]vobject.Prop{}
	for _, ev := range overrides {
		if t, _, ok := parseEventTime(ev, "RECURRENCE-ID"); ok {
			overByRID[t.Unix()] = ev
		}
	}

	var rows []*format.OM
	used := map[int64]bool{}
	for _, inst := range parseRRule(rule).times(start, from.Add(-dur), to) {
		if skip[inst.Unix()] {
			continue
		}
		instEnd := inst.Add(dur)
		if !inst.Before(to) || !instEnd.After(from) {
			continue
		}
		if ev, ok := overByRID[inst.Unix()]; ok {
			used[inst.Unix()] = true
			if strings.EqualFold(vobject.First(ev, "STATUS"), "CANCELLED") {
				continue
			}
			if row := eventIfInWindow(ev, calName, from, to); row != nil {
				rows = append(rows, row)
			}
			continue
		}
		if row := eventRow(stampInstance(master, inst, instEnd, allDay), calName); row != nil {
			rows = append(rows, row)
		}
	}
	for rid, ev := range overByRID {
		if used[rid] {
			continue
		}
		if strings.EqualFold(vobject.First(ev, "STATUS"), "CANCELLED") {
			continue
		}
		if row := eventIfInWindow(ev, calName, from, to); row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func splitSeries(evs [][]vobject.Prop) (master []vobject.Prop, overrides [][]vobject.Prop) {
	for _, ev := range evs {
		switch {
		case vobject.First(ev, "RRULE") != "":
			master = ev
		case vobject.First(ev, "RECURRENCE-ID") != "":
			overrides = append(overrides, ev)
		case master == nil:
			master = ev
		default:
			overrides = append(overrides, ev)
		}
	}
	return master, overrides
}

func exdateSet(props []vobject.Prop) map[int64]bool {
	out := map[int64]bool{}
	for _, p := range props {
		if p.Name != "EXDATE" {
			continue
		}
		for _, raw := range strings.Split(p.Value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			t, _, ok := parseEventTime([]vobject.Prop{{Name: "EXDATE", Params: p.Params, Value: raw}}, "EXDATE")
			if ok {
				out[t.Unix()] = true
			}
		}
	}
	return out
}

func stampInstance(props []vobject.Prop, start, end time.Time, allDay bool) []vobject.Prop {
	out := append([]vobject.Prop(nil), props...)
	if allDay {
		return setProp(setProp(out, "DTSTART", ";VALUE=DATE", start.Format("20060102")),
			"DTEND", ";VALUE=DATE", end.Format("20060102"))
	}
	return setProp(setProp(out, "DTSTART", "", start.UTC().Format("20060102T150405Z")),
		"DTEND", "", end.UTC().Format("20060102T150405Z"))
}

func eventIfInWindow(props []vobject.Prop, calName string, from, to time.Time) *format.OM {
	start, allDay, ok := parseEventTime(props, "DTSTART")
	if !ok {
		return nil
	}
	end, _, hasEnd := parseEventTime(props, "DTEND")
	if !hasEnd || !end.After(start) {
		end = start.Add(time.Hour)
		if allDay {
			end = start.AddDate(0, 0, 1)
		}
	}
	if !start.Before(to) || !end.After(from) {
		return nil
	}
	return eventRow(props, calName)
}

func eventRow(props []vobject.Prop, calName string) *format.OM {
	has := map[string]bool{"dtstart": findHas(props, "DTSTART"), "dtend": findHas(props, "DTEND")}
	row, err := eventFields(props, "", calName, has)
	if err != nil {
		return nil
	}
	return row
}
