package tui

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/calendar"
)

const (
	efTitle = iota
	efAllDay
	efStartsOn
	efStartTime
	efEndsOn
	efEndTime
	efNotify
	efMore
	efLocation
	efLink
	efNotes
	efRepeat
	efCircle
)

type eventFormMode int

const (
	eventFormCreate eventFormMode = iota
	eventFormEdit
)

type eventReminder struct {
	label, spec string
}

var eventReminders = []eventReminder{
	{"5m", "5m"},
	{"10m", "10m"},
	{"15m", "15m"},
	{"30m", "30m"},
	{"1h", "1h"},
	{"2h", "2h"},
	{"1d", "1d"},
}

var eventRepeatChoices = []struct{ label, alias string }{
	{"does not repeat", ""},
	{"every day", "every_day"},
	{"every weekday", "every_weekday"},
	{"every week", "every_week"},
	{"every other week", "every_other_week"},
	{"every month", "every_day_of_month"},
	{"every year", "every_year"},
}

type eventForm struct {
	mode      eventFormMode
	id        string
	title     string
	allDay    bool
	startsOn  string
	startTime string
	endsOn    string
	endTime   string

	chosenReminders []bool
	notify          int

	revealed bool
	location string
	link     string
	notes    string
	repeat   int
	keepRR   bool
	circled  bool

	focus  int
	status string
}

func nextWholeHour(at time.Time) time.Time {
	rounded := at.Truncate(time.Hour)
	if rounded.Before(at) {
		rounded = rounded.Add(time.Hour)
	}
	return rounded
}

func newEventForm(on time.Time) *eventForm {
	starts := nextWholeHour(on.In(calendar.TZ))
	ends := starts.Add(time.Hour)
	return &eventForm{
		startsOn:        starts.Format("2006-01-02"),
		startTime:       starts.Format("15:04"),
		endsOn:          ends.Format("2006-01-02"),
		endTime:         ends.Format("15:04"),
		chosenReminders: make([]bool, len(eventReminders)),
	}
}

func editEventForm(ev Recording) *eventForm {
	starts := ev.StartsAt.In(calendar.TZ)
	ends := ev.EndsAt.In(calendar.TZ)
	if ends.IsZero() {
		ends = starts.Add(time.Hour)
	}
	f := &eventForm{
		mode:            eventFormEdit,
		id:              ev.ID,
		title:           ev.Title,
		allDay:          ev.AllDay,
		startsOn:        starts.Format("2006-01-02"),
		startTime:       starts.Format("15:04"),
		endsOn:          ends.Format("2006-01-02"),
		endTime:         ends.Format("15:04"),
		chosenReminders: remindersFromICS(ev.Remind),
		location:        ev.Location,
		link:            ev.URL,
		notes:           ev.Notes,
		circled:         ev.Circle,
	}
	f.repeat, f.keepRR = repeatIndexFor(ev.Repeat)
	f.revealed = f.location != "" || f.link != "" || f.notes != "" || ev.Repeat != "" || f.circled
	return f
}

func (f *eventForm) formTitle() string {
	if f.mode == eventFormEdit {
		return "Edit event"
	}
	return "New event"
}

func (f *eventForm) fields() []int {
	fields := []int{efTitle, efAllDay, efStartsOn}
	if !f.allDay {
		fields = append(fields, efStartTime)
	}
	fields = append(fields, efEndsOn)
	if !f.allDay {
		fields = append(fields, efEndTime)
	}
	fields = append(fields, efNotify, efMore)
	if f.revealed {
		fields = append(fields, efLocation, efLink, efNotes, efRepeat, efCircle)
	}
	return fields
}

func (f *eventForm) typed() *string {
	switch f.focus {
	case efTitle:
		return &f.title
	case efStartsOn:
		return &f.startsOn
	case efStartTime:
		return &f.startTime
	case efEndsOn:
		return &f.endsOn
	case efEndTime:
		return &f.endTime
	case efLocation:
		return &f.location
	case efLink:
		return &f.link
	case efNotes:
		return &f.notes
	}
	return nil
}

func (f *eventForm) step(delta int) {
	fields := f.fields()
	at := 0
	for i, field := range fields {
		if field == f.focus {
			at = i
			break
		}
	}
	f.focus = fields[(at+delta+len(fields))%len(fields)]
}

func (f *eventForm) setAllDay(on bool) {
	f.allDay = on
	if on && (f.focus == efStartTime || f.focus == efEndTime) {
		f.focus = efStartsOn
	}
}

func (f *eventForm) validate() string {
	if strings.TrimSpace(f.title) == "" {
		return "Name is required"
	}
	_, _, _, err := calendar.CombineEventWhen(f.startsOn, f.startTime, f.endsOn, f.endTime, f.allDay)
	if err != nil {
		return err.Error()
	}
	if problem := eventLinkProblem(f.link); problem != "" {
		return problem
	}
	return ""
}

func (f *eventForm) remindSpec() string {
	var parts []string
	for i, on := range f.chosenReminders {
		if on {
			parts = append(parts, eventReminders[i].spec)
		}
	}
	return strings.Join(parts, ",")
}

func (f *eventForm) repeatLabels() []string {
	labels := make([]string, 0, 1+len(eventRepeatChoices))
	if f.keepRR {
		labels = append(labels, "keeps its schedule")
	}
	for _, c := range eventRepeatChoices {
		labels = append(labels, c.label)
	}
	return labels
}

func (f *eventForm) repeatValue() (alias string, send bool) {
	i := f.repeat
	if f.keepRR {
		if i == 0 {
			return "", false
		}
		i--
	}
	if i < 0 || i >= len(eventRepeatChoices) {
		return "", false
	}
	return eventRepeatChoices[i].alias, true
}

func (f *eventForm) eventIn() (calendar.EventIn, error) {
	start, end, allDay, err := calendar.CombineEventWhen(f.startsOn, f.startTime, f.endsOn, f.endTime, f.allDay)
	if err != nil {
		return calendar.EventIn{}, err
	}
	repeat, hasRepeat := f.repeatValue()
	return calendar.EventIn{
		Title:    strings.TrimSpace(f.title),
		Start:    start,
		End:      end,
		AllDay:   allDay,
		Location: strings.TrimSpace(f.location),
		Notes:    strings.TrimSpace(f.notes),
		URL:      eventLink(f.link),
		Repeat:   repeat,
		Remind:   f.remindSpec(),
		Circle:   f.circled,
		Has: calendar.EventHas{
			Title: true, Start: true, End: true, AllDay: true,
			Location: true, Notes: true, URL: true,
			Repeat: hasRepeat, Remind: true, Circle: true,
		},
	}, nil
}

func isSpace(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == " " || s == "space"
}

func (f *eventForm) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case msg.Key().Code == tea.KeyTab && msg.Key().Mod == tea.ModShift:
		f.step(-1)
		return nil, false
	case msg.Key().Code == tea.KeyTab:
		f.step(1)
		return nil, false
	case msg.Key().Code == tea.KeyEnter && f.focus == efNotes:
		f.notes += "\n"
		return nil, false
	case msg.Key().Code == tea.KeyEnter:
		f.step(1)
		return nil, false
	case msg.String() == "ctrl+s":
		if problem := f.validate(); problem != "" {
			f.status = problem
			if eventLinkProblem(f.link) != "" {
				f.revealed = true
			}
			return nil, false
		}
		return nil, true
	case msg.Key().Code == tea.KeyBackspace:
		if in := f.typed(); in != nil && *in != "" {
			r := []rune(*in)
			*in = string(r[:len(r)-1])
		}
		return nil, false
	}
	switch f.focus {
	case efAllDay:
		if isSpace(msg) {
			f.setAllDay(!f.allDay)
		}
	case efNotify:
		if isSpace(msg) {
			f.chosenReminders[f.notify] = !f.chosenReminders[f.notify]
		} else {
			f.notify = wrapIndex(f.notify, len(eventReminders), msg)
		}
	case efMore:
		if isSpace(msg) {
			f.revealed = !f.revealed
			if !f.revealed && f.focus > efMore {
				f.focus = efMore
			}
		}
	case efCircle:
		if isSpace(msg) {
			f.circled = !f.circled
		}
	case efRepeat:
		f.repeat = wrapIndex(f.repeat, len(f.repeatLabels()), msg)
	default:
		if in := f.typed(); in != nil {
			*in += insertText(msg)
		}
	}
	return nil, false
}

func (f *eventForm) helpBindings() []helpBinding {
	bindings := []helpBinding{{"tab", "next field"}}
	switch f.focus {
	case efAllDay, efCircle:
		bindings = append(bindings, helpBinding{"space", "toggle"})
	case efNotify:
		bindings = append(bindings, helpBinding{"←→", "move"}, helpBinding{"space", "toggle"})
	case efMore:
		action := "show"
		if f.revealed {
			action = "hide"
		}
		bindings = append(bindings, helpBinding{"space", action})
	case efRepeat:
		bindings = append(bindings, helpBinding{"←→", "choose"})
	case efNotes:
		bindings = append(bindings, helpBinding{"enter", "new line"})
	}
	return append(bindings, helpBinding{"ctrl+s", "save"}, helpBinding{"esc", "cancel"})
}

func (f *eventForm) view() string {
	var b strings.Builder
	f.writeRow(&b, "Name", f.title, efTitle)
	f.writeRow(&b, "All day", checkbox(f.allDay), efAllDay)
	f.writeRow(&b, "Starts", f.startsOn, efStartsOn)
	if !f.allDay {
		f.writeRow(&b, "Start", f.startTime, efStartTime)
	}
	f.writeRow(&b, "Ends", f.endsOn, efEndsOn)
	if !f.allDay {
		f.writeRow(&b, "End", f.endTime, efEndTime)
	}
	f.writeRow(&b, "Notify", f.notifyField(), efNotify)
	f.writeRow(&b, "More", f.moreField(), efMore)
	if f.revealed {
		f.writeRow(&b, "Location", f.location, efLocation)
		f.writeRow(&b, "Link", f.link, efLink)
		f.writeRow(&b, "Notes", f.notes, efNotes)
		f.writeRow(&b, "Repeat", f.repeatField(), efRepeat)
		f.writeRow(&b, "Circle", checkbox(f.circled), efCircle)
	}
	if f.status != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(colorError).Render(f.status))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (f *eventForm) writeRow(b *strings.Builder, label, value string, field int) {
	style := styleMuted
	cursor := ""
	if f.focus == field {
		style = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
		if f.typed() != nil && field == f.focus {
			cursor = "█"
		}
	}
	fmt.Fprintf(b, "%s %s%s\n", style.Render(fmt.Sprintf("%-12s", label)), value, cursor)
}

func (f *eventForm) notifyField() string {
	chips := make([]string, 0, len(eventReminders))
	for i, reminder := range eventReminders {
		chip := "○" + reminder.label
		if f.chosenReminders[i] {
			chip = "◉" + reminder.label
		}
		switch {
		case f.focus == efNotify && i == f.notify:
			chip = lipgloss.NewStyle().Foreground(colorActive).Bold(true).Render(chip)
		case !f.chosenReminders[i]:
			chip = styleMuted.Render(chip)
		}
		chips = append(chips, chip)
	}
	return strings.Join(chips, " ")
}

func (f *eventForm) moreField() string {
	if f.revealed {
		return "▾ hide"
	}
	return styleMuted.Render("▸ location, notes, repeat…")
}

func (f *eventForm) repeatField() string {
	labels := f.repeatLabels()
	if f.repeat >= 0 && f.repeat < len(labels) {
		return labels[f.repeat]
	}
	return labels[0]
}

func checkbox(on bool) string {
	if on {
		return "◉ yes"
	}
	return "○ no"
}

func eventLink(typed string) string {
	typed = strings.TrimSpace(typed)
	if typed == "" || strings.Contains(typed, "://") || strings.HasPrefix(typed, "mailto:") {
		return typed
	}
	return "https://" + typed
}

func eventLinkProblem(typed string) string {
	link := eventLink(typed)
	if link == "" {
		return ""
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Host == "" || strings.ContainsAny(typed, " \t") {
		return "Link must be a web address"
	}
	return ""
}

func remindersFromICS(raw string) []bool {
	chosen := make([]bool, len(eventReminders))
	for _, part := range strings.Split(raw, ",") {
		spec := parseICSTrigger(strings.TrimSpace(part))
		for i, r := range eventReminders {
			if r.spec == spec {
				chosen[i] = true
			}
		}
	}
	return chosen
}

func parseICSTrigger(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "-")
	switch {
	case strings.HasPrefix(s, "PT") && strings.HasSuffix(s, "M"):
		return strings.TrimSuffix(strings.TrimPrefix(s, "PT"), "M") + "m"
	case strings.HasPrefix(s, "PT") && strings.HasSuffix(s, "H"):
		return strings.TrimSuffix(strings.TrimPrefix(s, "PT"), "H") + "h"
	case strings.HasPrefix(s, "P") && strings.HasSuffix(s, "D"):
		return strings.TrimSuffix(strings.TrimPrefix(s, "P"), "D") + "d"
	}
	return strings.ToLower(s)
}

func rruleAlias(rule string) string {
	u := strings.ToUpper(rule)
	switch {
	case strings.Contains(u, "FREQ=DAILY"):
		return "every_day"
	case strings.Contains(u, "BYDAY=MO,TU,WE,TH,FR"):
		return "every_weekday"
	case strings.Contains(u, "INTERVAL=2") && strings.Contains(u, "WEEKLY"):
		return "every_other_week"
	case strings.Contains(u, "FREQ=WEEKLY"):
		return "every_week"
	case strings.Contains(u, "FREQ=MONTHLY"):
		return "every_day_of_month"
	case strings.Contains(u, "FREQ=YEARLY"):
		return "every_year"
	}
	return ""
}

func repeatIndexFor(rule string) (int, bool) {
	if strings.TrimSpace(rule) == "" {
		return 0, false
	}
	alias := rruleAlias(rule)
	if alias == "" {
		return 0, true
	}
	for i, c := range eventRepeatChoices {
		if c.alias == alias {
			return i, false
		}
	}
	return 0, true
}
