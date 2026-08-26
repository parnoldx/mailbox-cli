package calendar

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mailbox/src/internal/format"
	"mailbox/src/internal/vobject"
)

const mkHabits = `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set>
    <D:prop>
      <D:displayname>mailbox-habits</D:displayname>
      <C:supported-calendar-component-set>
        <C:comp name="VEVENT"/>
      </C:supported-calendar-component-set>
    </D:prop>
  </D:set>
</C:mkcalendar>`

type habit struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Days  []string `json:"days"`
	Done  []string `json:"done"`
	Color string   `json:"color,omitempty"`
	Icon  string   `json:"icon,omitempty"`
}

type habitBag struct {
	Habits []habit `json:"habits"`
}

var dayOrder = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var dayAlias = map[string]string{
	"sun": "sun", "sunday": "sun", "0": "sun",
	"mon": "mon", "monday": "mon", "1": "mon",
	"tue": "tue", "tues": "tue", "tuesday": "tue", "2": "tue",
	"wed": "wed", "wednesday": "wed", "3": "wed",
	"thu": "thu", "thur": "thu", "thurs": "thu", "thursday": "thu", "4": "thu",
	"fri": "fri", "friday": "fri", "5": "fri",
	"sat": "sat", "saturday": "sat", "6": "sat",
}

func parseDays(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return append([]string{}, dayOrder...), nil
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		d, ok := dayAlias[p]
		if !ok {
			return nil, fmt.Errorf("unknown day %q", p)
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no days")
	}
	return out, nil
}

func (b *habitBag) find(id string) (*habit, error) {
	var hit *habit
	n := 0
	for i := range b.Habits {
		if uidMatches(b.Habits[i].ID, id) {
			hit = &b.Habits[i]
			n++
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("habit not found: %s", id)
	}
	if n > 1 {
		return nil, fmt.Errorf("ambiguous habit id %q", id)
	}
	return hit, nil
}

func (h *habit) hasDone(day string) bool {
	for _, d := range h.Done {
		if d == day {
			return true
		}
	}
	return false
}

func (h *habit) complete(day string) {
	if !h.hasDone(day) {
		h.Done = append(h.Done, day)
	}
}

func (h *habit) uncomplete(day string) {
	var out []string
	for _, d := range h.Done {
		if d != day {
			out = append(out, d)
		}
	}
	h.Done = out
}

func habitDay(when string) (string, error) {
	if when == "" {
		return time.Now().In(TZ).Format("2006-01-02"), nil
	}
	w, err := parseWhen(when)
	if err != nil {
		return "", err
	}
	return w.t.In(TZ).Format("2006-01-02"), nil
}

func (cal *Cal) findHabitsCal() (Collection, bool, error) {
	cols, err := cal.collections()
	if err != nil {
		return Collection{}, false, err
	}
	for _, c := range cols {
		if isHabits(c) {
			return c, true, nil
		}
	}
	return Collection{}, false, nil
}

func (cal *Cal) ensureHabitsCal() (Collection, error) {
	if col, ok, err := cal.findHabitsCal(); err != nil || ok {
		return col, err
	}
	url := strings.TrimRight(cal.homeURL(), "/") + "/" + habitsCalName + "/"
	status, err := cal.client.MkCalendar(url, mkHabits)
	if err != nil {
		return Collection{}, err
	}
	if status != 201 && status != 200 && status != 204 && status != 405 && status != 409 {
		return Collection{}, fmt.Errorf("MKCALENDAR mailbox-habits failed: %d", status)
	}
	cal.discovered = false
	cal.cols = nil
	if col, ok, err := cal.findHabitsCal(); err != nil {
		return Collection{}, err
	} else if ok {
		return col, nil
	}
	return Collection{Name: habitsCalName, URL: url, Calendar: true, Comps: []string{"VEVENT"}}, nil
}

func bagProps(raw string) []vobject.Prop {
	uid := habitsUID
	return []vobject.Prop{
		{Name: "UID", Value: uid},
		{Name: "DTSTAMP", Value: stampUTC()},
		{Name: "DTSTART", Value: "19900101T000000Z"},
		{Name: "DTEND", Value: "19900101T010000Z"},
		{Name: "TRANSP", Value: "TRANSPARENT"},
		{Name: "SUMMARY", Value: habitsCalName},
		{Name: "DESCRIPTION", Value: raw},
	}
}

func (cal *Cal) loadBag() (habitBag, Collection, string, error) {
	col, err := cal.ensureHabitsCal()
	if err != nil {
		return habitBag{}, col, "", err
	}
	href := strings.TrimRight(col.URL, "/") + "/" + habitsUID + ".ics"
	text, status, err := cal.client.Get(href)
	if err != nil {
		return habitBag{}, col, href, err
	}
	if status == 404 {
		return habitBag{}, col, href, nil
	}
	if status != 200 {
		return habitBag{}, col, href, fmt.Errorf("CalDAV get habits failed: %d", status)
	}
	props := vobject.Component(text, "VEVENT")
	desc := vobject.First(props, "DESCRIPTION")
	if strings.TrimSpace(desc) == "" {
		return habitBag{}, col, href, nil
	}
	var bag habitBag
	if err := json.Unmarshal([]byte(desc), &bag); err != nil {
		return habitBag{}, col, href, fmt.Errorf("habits bag corrupt: %w", err)
	}
	return bag, col, href, nil
}

func (cal *Cal) saveBag(bag habitBag, href string) error {
	raw, err := json.Marshal(bag)
	if err != nil {
		return err
	}
	return cal.putICS(href, "BEGIN:VEVENT\r\n"+vobject.Serialize(bagProps(string(raw)))+"END:VEVENT")
}

func habitRow(h habit, day string) *format.OM {
	return format.NewOM(
		"id", shortID(h.ID),
		"name", h.Name,
		"days", strings.Join(h.Days, ","),
		"done", h.hasDone(day),
		"color", h.Color,
		"icon", h.Icon,
	)
}

func (cal *Cal) Habits(when string) ([]*format.OM, error) {
	day, err := habitDay(when)
	if err != nil {
		return nil, err
	}
	bag, _, _, err := cal.loadBag()
	if err != nil {
		return nil, err
	}
	var rows []*format.OM
	for _, h := range bag.Habits {
		rows = append(rows, habitRow(h, day))
	}
	sortRows(rows, func(a, b *format.OM) bool { return strOr(a.Get("name")) < strOr(b.Get("name")) })
	return rows, nil
}

func (cal *Cal) CreateHabit(name, days, color, icon string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("habit create needs a title")
	}
	parsed, err := parseDays(days)
	if err != nil {
		return "", err
	}
	bag, _, href, err := cal.loadBag()
	if err != nil {
		return "", err
	}
	id := newUUID()
	bag.Habits = append(bag.Habits, habit{ID: id, Name: name, Days: parsed, Color: color, Icon: icon})
	if err := cal.saveBag(bag, href); err != nil {
		return "", err
	}
	return id, nil
}

func (cal *Cal) EditHabit(id, name, days, color, icon string, setName, setDays, setColor, setIcon bool) (*format.OM, error) {
	bag, _, href, err := cal.loadBag()
	if err != nil {
		return nil, err
	}
	h, err := bag.find(id)
	if err != nil {
		return nil, err
	}
	if setName {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("habit name cannot be empty")
		}
		h.Name = name
	}
	if setDays {
		parsed, err := parseDays(days)
		if err != nil {
			return nil, err
		}
		h.Days = parsed
	}
	if setColor {
		h.Color = color
	}
	if setIcon {
		h.Icon = icon
	}
	if err := cal.saveBag(bag, href); err != nil {
		return nil, err
	}
	day, _ := habitDay("")
	return habitRow(*h, day), nil
}

func (cal *Cal) DeleteHabit(id string) error {
	bag, _, href, err := cal.loadBag()
	if err != nil {
		return err
	}
	h, err := bag.find(id)
	if err != nil {
		return err
	}
	var out []habit
	for _, x := range bag.Habits {
		if x.ID != h.ID {
			out = append(out, x)
		}
	}
	bag.Habits = out
	return cal.saveBag(bag, href)
}

func (cal *Cal) CompleteHabit(id, when string) error {
	day, err := habitDay(when)
	if err != nil {
		return err
	}
	bag, _, href, err := cal.loadBag()
	if err != nil {
		return err
	}
	h, err := bag.find(id)
	if err != nil {
		return err
	}
	h.complete(day)
	return cal.saveBag(bag, href)
}

func (cal *Cal) UncompleteHabit(id, when string) error {
	day, err := habitDay(when)
	if err != nil {
		return err
	}
	bag, _, href, err := cal.loadBag()
	if err != nil {
		return err
	}
	h, err := bag.find(id)
	if err != nil {
		return err
	}
	h.uncomplete(day)
	return cal.saveBag(bag, href)
}
