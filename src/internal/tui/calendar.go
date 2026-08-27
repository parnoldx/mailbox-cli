package tui

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/calendar"
	"mailbox/src/internal/format"
	"mailbox/src/internal/terminal"
)

type eventsLoadedMsg struct {
	requestID uint64
	events    []Recording
	todos     []Recording
	habits    []Recording
	done      []Recording
	calendars []Calendar
	err       error
}
type calActionMsg struct {
	label string
	err   error
}

type calendarView struct {
	s              *session
	styles         styles
	mode           calendarViewMode
	anchor         time.Time
	events         []Recording
	todos          []Recording
	habits         []Recording
	done           []Recording
	selected       string
	inCell         bool
	yearOff        int
	width          int
	height         int
	requestID      uint64
	loading        bool
	notice         string
	todoPicker     *todoPicker
	habitPicker    *habitPicker
	habitForm      *habitForm
	eventForm      *eventForm
	calendarPicker *calendarPicker
	calendars      []Calendar
	shownCalendars map[string]bool
	confirmDelete  string
	selectFromEdge int
}

func newCalendarView(s *session, st styles) *calendarView {
	return &calendarView{s: s, styles: st, anchor: time.Now().In(calendar.TZ)}
}

func (v *calendarView) day() time.Time {
	if v.anchor.IsZero() {
		return time.Now().In(calendar.TZ)
	}
	return v.anchor.In(calendar.TZ)
}

func (v *calendarView) Init() tea.Cmd { return v.reload() }

func (v *calendarView) window() (time.Time, time.Time) {
	d := v.day()
	switch v.mode {
	case viewWeek:
		start := weekStartDate(d, time.Monday)
		return start, start.AddDate(0, 0, 7)
	case viewYear:
		start := time.Date(d.Year(), 1, 1, 0, 0, 0, 0, d.Location())
		return start, start.AddDate(1, 0, 0)
	default:
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
		return start, start.AddDate(0, 0, 1)
	}
}

func (v *calendarView) reload() tea.Cmd {
	v.requestID++
	if len(v.events) == 0 && len(v.todos) == 0 {
		v.loading = true
	}
	id := v.requestID
	start, end := v.window()
	when := v.day().Format("2006-01-02")
	return func() tea.Msg {
		rows, err := v.s.events(start, end)
		if err != nil {
			return eventsLoadedMsg{requestID: id, err: err}
		}
		todos, _ := v.s.todos()
		habits, _ := v.s.habits(when)
		cals, _ := v.s.calendars()
		return eventsLoadedMsg{
			requestID: id,
			events:    eventsFromOM(rows),
			todos:     todosFromOM(todos),
			habits:    habitsFromOM(habits, v.day()),
			done:      habitCompletions(habits, v.day()),
			calendars: calendarsFromOM(cals),
		}
	}
}

func eventsFromOM(rows []*format.OM) []Recording {
	out := make([]Recording, 0, len(rows))
	for _, row := range rows {
		start, allDay := parseCalTime(omStr(row, "start"))
		end, _ := parseCalTime(omStr(row, "end"))
		calName := omStr(row, "calendar")
		out = append(out, Recording{
			ID:            omStr(row, "id"),
			Title:         omStr(row, "summary"),
			AllDay:        allDay,
			StartsAt:      start,
			EndsAt:        end,
			Type:          "event",
			Calendar:      calName,
			CalendarColor: calendarHue(calName),
			Location:      omStr(row, "location"),
			Notes:         omStr(row, "notes"),
			URL:           omStr(row, "url"),
			Remind:        omStr(row, "reminders"),
			Repeat:        omStr(row, "rrule"),
			Circle:        omBool(row, "circle"),
		})
	}
	return out
}

func calendarsFromOM(rows []*format.OM) []Calendar {
	out := make([]Calendar, 0, len(rows))
	for _, row := range rows {
		name := omStr(row, "name")
		if name == "" {
			continue
		}
		out = append(out, Calendar{Name: name, Color: calendarHue(name)})
	}
	return out
}

func todosFromOM(rows []*format.OM) []Recording {
	out := make([]Recording, 0, len(rows))
	for _, row := range rows {
		due, _ := parseCalTime(omStr(row, "due"))
		rec := Recording{ID: omStr(row, "id"), Title: omStr(row, "summary"), Type: "todo", StartsAt: due}
		if strings.EqualFold(omStr(row, "status"), "COMPLETED") {
			rec.CompletedAt = time.Now()
		}
		out = append(out, rec)
	}
	return out
}

func habitsFromOM(rows []*format.OM, day time.Time) []Recording {
	out := make([]Recording, 0, len(rows))
	for _, row := range rows {
		rec := Recording{
			ID:    omStr(row, "id"),
			Title: omStr(row, "name"),
			Type:  "habit",
			Color: omStr(row, "color"),
			Icon:  omStr(row, "icon"),
			Days:  omStr(row, "days"),
		}
		if omBool(row, "done") {
			rec.CompletedAt = day
		}
		out = append(out, rec)
	}
	return out
}

func habitCompletions(rows []*format.OM, day time.Time) []Recording {
	var out []Recording
	for _, row := range rows {
		if !omBool(row, "done") {
			continue
		}
		out = append(out, Recording{
			ParentID: omStr(row, "id"),
			StartsAt: day,
			Type:     "habit-completion",
		})
	}
	return out
}

func calendarHue(name string) string {
	colors := []string{"blue", "red", "green", "teal", "purple", "pink", "gold"}
	h := 0
	for _, r := range name {
		h += int(r)
	}
	if name == "" {
		return "blue"
	}
	return colors[h%len(colors)]
}

func parseCalTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, true
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, calendar.TZ); err == nil {
		return t, false
	}
	if t, err := time.ParseInLocation("2006-01-02", s, calendar.TZ); err == nil {
		return t, true
	}
	return time.Time{}, true
}

func (v *calendarView) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case eventsLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load calendar", msg.err)
			return nil, true
		}
		v.events, v.todos, v.habits, v.done = msg.events, msg.todos, msg.habits, msg.done
		v.adoptCalendars(msg.calendars)
		v.settleSelection()
		if v.todoPicker != nil {
			v.todoPicker.setTodos(v.todos)
		}
		if v.habitPicker != nil {
			v.habitPicker.setHabits(v.habits)
		}
		if v.calendarPicker != nil {
			v.calendarPicker.setCalendars(v.listedCalendars(), v.shownCalendars)
		}
		return nil, true
	case calActionMsg:
		if msg.err != nil {
			if v.eventForm != nil {
				v.eventForm.status = errorNotice("Save failed", msg.err)
				return nil, true
			}
			if v.habitForm != nil {
				v.habitForm.status = errorNotice("Save failed", msg.err)
				return nil, true
			}
			return notifyError(msg.label, msg.err), true
		}
		v.eventForm = nil
		v.habitForm = nil
		return tea.Batch(notify(msg.label), v.reload()), true
	}
	return nil, false
}

func (v *calendarView) sel() selection {
	return selection{eventKey: v.selected, day: v.day()}
}

func (v *calendarView) stepHint() string {
	hint := "p/n " + v.mode.unit()
	if !sameDay(v.day(), time.Now().In(calendar.TZ)) {
		hint += " · t today"
	}
	return hint
}

func (v *calendarView) gridHeight() int {
	h := v.height
	if v.mode != viewYear {
		h -= v.todosFooterHeight()
	}
	return max(h, 1)
}

func (v *calendarView) View() string {
	anchor := v.day()
	now := time.Now().In(calendar.TZ)
	hint := v.stepHint()
	w, h := v.width, v.gridHeight()
	var content string
	events := v.visibleEvents()
	switch v.mode {
	case viewWeek:
		content = renderWeekView(events, v.habits, v.done, anchor, time.Monday, w, h, hint, v.sel())
	case viewYear:
		full, top, bot := renderYearView(events, anchor, now, time.Monday, w, h, hint, v.sel(), v.inCell)
		v.syncYearOff(top, bot, h)
		content = clipLines(full, v.yearOff, h)
	default:
		content = renderDayView(events, v.habits, anchor, now, hint, w, h, v.sel())
	}
	if footer := v.todosFooter(); footer != "" {
		content += "\n" + footer
	}
	if v.notice != "" {
		content = v.styles.title.Render(v.notice) + "\n" + content
	}
	if v.habitPicker != nil {
		content = v.habitPicker.draw(content, v.width, v.height)
	}
	if v.todoPicker != nil {
		content = v.todoPicker.draw(content, v.width, v.height)
	}
	if v.calendarPicker != nil {
		content = v.calendarPicker.draw(content, v.width, v.height)
	}
	if v.habitForm != nil {
		content = overlayModal(content, modalFrame(v.habitForm.title(), v.habitForm.view(), v.width), v.width, v.height)
	}
	if v.eventForm != nil {
		content = overlayModal(content, modalFrame(v.eventForm.formTitle(), v.eventForm.view(), v.width), v.width, v.height)
	}
	return content
}

func clipLines(s string, off, height int) string {
	lines := strings.Split(s, "\n")
	if off < 0 {
		off = 0
	}
	if off >= len(lines) {
		off = max(len(lines)-1, 0)
	}
	end := min(off+height, len(lines))
	return strings.Join(lines[off:end], "\n")
}

func (v *calendarView) todosFooter() string {
	if v.mode == viewYear {
		return ""
	}
	header := hintedSectionHeader(todosSectionLabel, "s to manage", v.width)
	if len(v.todos) == 0 {
		return header + "\n" + styleMuted.Render("Nothing to do")
	}
	return header + "\n" + renderTodosRibbon(v.todos, v.width)
}

func (v *calendarView) todosFooterHeight() int {
	if footer := v.todosFooter(); footer != "" {
		return lipgloss.Height(footer)
	}
	return 0
}

func (v *calendarView) HelpBindings() []helpBinding {
	if v.habitForm != nil {
		return v.habitForm.helpBindings()
	}
	if v.eventForm != nil {
		return v.eventForm.helpBindings()
	}
	if v.habitPicker != nil {
		return v.habitPicker.helpBindings()
	}
	if v.todoPicker != nil {
		return v.todoPicker.helpBindings()
	}
	if v.calendarPicker != nil {
		return v.calendarPicker.helpBindings()
	}
	var bindings []helpBinding
	switch v.mode {
	case viewDay:
		bindings = append(bindings, helpBinding{"←→", "event"})
		if _, allDay := v.selectableGroups(); len(allDay) > 0 {
			bindings = append(bindings, helpBinding{"↑↓", "all day"})
		}
	case viewWeek:
		bindings = append(bindings, helpBinding{"←→", "day"}, helpBinding{"↑↓", "event"})
	case viewYear:
		if v.inCell {
			bindings = append(bindings, helpBinding{"↑↓", "event"}, helpBinding{"esc", "leave the day"})
		} else {
			bindings = append(bindings, helpBinding{"←→↑↓", "day"}, helpBinding{"enter", "open the day"})
		}
	}
	bindings = append(bindings, helpBinding{"a", "new event"})
	if event, ok := v.selectedRecording(); ok {
		label := "delete"
		if v.confirmDelete == event.key() {
			label = "press x again to delete"
		}
		bindings = append(bindings, helpBinding{"e", "edit"}, helpBinding{"x", label})
	}
	// Which calendar is being read is only in the menu, so the key that opens it has to be said.
	if len(v.listedCalendars()) > 0 {
		bindings = append(bindings, helpBinding{"g", "calendars"})
	}
	return append(bindings, helpBinding{"b", "habits"})
}

func (v *calendarView) SubnavItems() ([]navItem, int, string, bool) {
	return calendarNavItems(), int(v.mode), v.mode.String(), true
}

func (v *calendarView) SubnavLeft() tea.Cmd  { return v.setMode(v.mode - 1) }
func (v *calendarView) SubnavRight() tea.Cmd { return v.setMode(v.mode + 1) }

func (v *calendarView) setMode(mode calendarViewMode) tea.Cmd {
	if mode < viewDay || mode > viewYear || mode == v.mode {
		return nil
	}
	v.mode = mode
	v.inCell = false
	v.selected = ""
	v.yearOff = 0
	return v.reload()
}

func (v *calendarView) HandleContentKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.habitForm != nil {
		return v.handleHabitForm(msg)
	}
	if v.eventForm != nil {
		return v.handleEventForm(msg)
	}
	if v.habitPicker != nil {
		return v.handleHabitPickerKey(msg)
	}
	if v.todoPicker != nil {
		return v.handleTodoPickerKey(msg)
	}
	if v.calendarPicker != nil {
		return v.handleCalendarPickerKey(msg)
	}
	if msg.String() != "x" {
		v.confirmDelete = ""
	}
	key := msg.String()
	switch key {
	case "p":
		return v.step(-1)
	case "n":
		return v.step(1)
	case "t":
		v.anchor = time.Now().In(calendar.TZ)
		v.selected = ""
		v.inCell = false
		v.yearOff = 0
		return v.reload()
	case "1", "2", "3":
		return v.setMode(calendarViewMode(key[0] - '1'))
	case "a":
		v.eventForm = newEventForm(v.day())
		return nil
	case "e":
		if event, ok := v.selectedRecording(); ok {
			v.eventForm = editEventForm(event)
		}
		return nil
	case "x":
		return v.removeSelectedEvent()
	case "b":
		v.habitPicker = newHabitPicker(v.habits, v.mode != viewYear)
		return nil
	case "s":
		v.todoPicker = newTodoPicker(v.todos)
		return nil
	// g for the calendars, since shift+C is the jump to this section and never reaches
	// here — the model reads the section shortcuts before a view sees a key.
	case "g":
		if listed := v.listedCalendars(); len(listed) > 0 {
			v.ensureShown()
			v.calendarPicker = newCalendarPicker(listed, v.shownCalendars)
		}
		return nil
	case "ctrl+r":
		return v.reload()
	}
	if cmd, handled := v.handleArrowKey(msg); handled {
		return cmd
	}
	return nil
}

func (v *calendarView) step(delta int) tea.Cmd {
	switch v.mode {
	case viewWeek:
		v.anchor = v.day().AddDate(0, 0, 7*delta)
	case viewYear:
		v.anchor = v.day().AddDate(delta, 0, 0)
		v.yearOff = 0
	default:
		v.anchor = v.day().AddDate(0, 0, delta)
	}
	v.selected = ""
	v.inCell = false
	return v.reload()
}

func (v *calendarView) handleArrowKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch v.mode {
	case viewDay:
		switch key {
		case "left":
			return v.moveAlongTheDay(-1), true
		case "right":
			return v.moveAlongTheDay(1), true
		case "up":
			return v.crossTheDay(-1), true
		case "down":
			return v.crossTheDay(1), true
		}
	case viewWeek:
		switch key {
		case "left":
			return v.moveCursorDay(-1), true
		case "right":
			return v.moveCursorDay(1), true
		case "up":
			return v.moveSelection(-1), true
		case "down":
			return v.moveSelection(1), true
		}
	case viewYear:
		switch key {
		case "left":
			return v.moveCursorDay(-1), true
		case "right":
			return v.moveCursorDay(1), true
		case "up":
			if v.inCell {
				return v.moveSelection(-1), true
			}
			return v.moveCursorDay(-7), true
		case "down":
			if v.inCell {
				return v.moveSelection(1), true
			}
			return v.moveCursorDay(7), true
		case "enter":
			v.enterYearCell()
			return nil, true
		}
	}
	return nil, false
}

func (v *calendarView) sameSpan(a, b time.Time) bool {
	switch v.mode {
	case viewWeek:
		return sameDay(weekStartDate(a, time.Monday), weekStartDate(b, time.Monday))
	case viewYear:
		return a.Year() == b.Year()
	default:
		return sameDay(a, b)
	}
}

func (v *calendarView) moveCursorDay(days int) tea.Cmd {
	from := v.day()
	v.anchor = from.AddDate(0, 0, days)
	v.inCell = false
	if v.sameSpan(from, v.anchor) {
		v.settleSelection()
		return nil
	}
	return v.reload()
}

func (v *calendarView) selectableGroups() (timed, allDay []Recording) {
	var onDay []Recording
	switch v.mode {
	case viewDay, viewWeek:
		onDay = eventsByDate(v.visibleEvents())[dateKey(v.day())]
	case viewYear:
		if !v.inCell {
			return nil, nil
		}
		onDay = eventsByDate(v.visibleEvents())[dateKey(v.day())]
	}
	timed, allDay = []Recording{}, []Recording{}
	for _, event := range onDay {
		if event.AllDay {
			allDay = append(allDay, event)
		} else {
			timed = append(timed, event)
		}
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].Starts().Before(timed[j].Starts()) })
	sort.SliceStable(allDay, func(i, j int) bool { return allDay[i].Starts().Before(allDay[j].Starts()) })
	return timed, allDay
}

func (v *calendarView) selectableEvents() []Recording {
	timed, allDay := v.selectableGroups()
	return append(timed, allDay...)
}

// listedCalendars is what the picker offers.
func (v *calendarView) listedCalendars() []Calendar {
	return v.calendars
}

func (v *calendarView) ensureShown() {
	if v.shownCalendars != nil {
		return
	}
	v.shownCalendars = make(map[string]bool, len(v.calendars))
	for _, calendar := range v.calendars {
		v.shownCalendars[calendar.Name] = true
	}
}

func (v *calendarView) adoptCalendars(calendars []Calendar) {
	v.calendars = calendars
	if v.shownCalendars == nil {
		v.ensureShown()
		return
	}
	for _, calendar := range calendars {
		if _, ok := v.shownCalendars[calendar.Name]; !ok {
			v.shownCalendars[calendar.Name] = true
		}
	}
}

// visibleEvents is the period drawn from the calendars currently switched on.
func (v *calendarView) visibleEvents() []Recording {
	if v.shownCalendars == nil || len(v.calendars) == 0 {
		return v.events
	}
	out := make([]Recording, 0, len(v.events))
	for _, event := range v.events {
		if event.Calendar == "" || v.shownCalendars[event.Calendar] {
			out = append(out, event)
		}
	}
	return out
}

func (v *calendarView) settleSelection() {
	events := v.selectableEvents()
	if v.selectFromEdge != 0 && len(events) > 0 {
		if v.selectFromEdge < 0 {
			v.selected = events[len(events)-1].key()
		} else {
			v.selected = events[0].key()
		}
		v.selectFromEdge = 0
		return
	}
	v.selectFromEdge = 0
	for _, event := range events {
		if event.key() == v.selected {
			return
		}
	}
	v.selected = ""
}

func (v *calendarView) selectedRecording() (Recording, bool) {
	for _, event := range v.selectableEvents() {
		if event.key() == v.selected {
			return event, true
		}
	}
	return Recording{}, false
}

func indexOfEvent(events []Recording, key string) int {
	if key == "" {
		return -1
	}
	for i, event := range events {
		if event.key() == key {
			return i
		}
	}
	return -1
}

func (v *calendarView) selectEvent(key string) tea.Cmd {
	v.selected = key
	return nil
}

func (v *calendarView) walk(events []Recording, delta int, past func() tea.Cmd) tea.Cmd {
	if len(events) == 0 {
		if past != nil {
			return past()
		}
		return nil
	}
	at := indexOfEvent(events, v.selected)
	if at < 0 {
		if delta > 0 {
			return v.selectEvent(events[0].key())
		}
		return v.selectEvent(events[len(events)-1].key())
	}
	next := at + delta
	if next < 0 || next >= len(events) {
		if past != nil {
			return past()
		}
		return nil
	}
	return v.selectEvent(events[next].key())
}

func (v *calendarView) moveSelection(delta int) tea.Cmd {
	return v.walk(v.selectableEvents(), delta, nil)
}

func (v *calendarView) stepSpanFromEdge(delta int) tea.Cmd {
	v.selectFromEdge = delta
	return v.step(delta)
}

func (v *calendarView) moveAlongTheDay(delta int) tea.Cmd {
	timed, allDay := v.selectableGroups()
	if indexOfEvent(allDay, v.selected) >= 0 {
		return v.stepSpanFromEdge(delta)
	}
	return v.walk(timed, delta, func() tea.Cmd { return v.stepSpanFromEdge(delta) })
}

func (v *calendarView) crossTheDay(delta int) tea.Cmd {
	timed, allDay := v.selectableGroups()
	if at := indexOfEvent(allDay, v.selected); at >= 0 {
		if delta < 0 && at == 0 {
			if len(timed) == 0 {
				return nil
			}
			return v.selectEvent(timed[len(timed)-1].key())
		}
		return v.walk(allDay, delta, nil)
	}
	if delta > 0 && len(allDay) > 0 {
		return v.selectEvent(allDay[0].key())
	}
	return nil
}

func (v *calendarView) enterYearCell() {
	v.inCell = true
	if events := v.selectableEvents(); len(events) > 0 && v.selected == "" {
		v.selected = events[0].key()
	}
}

func (v *calendarView) leaveYearCell() {
	v.inCell = false
	v.selected = ""
}

func (v *calendarView) CancelPendingDetail() bool {
	if !v.inCell {
		return false
	}
	v.leaveYearCell()
	return true
}

func (v *calendarView) syncYearOff(top, bottom, height int) {
	if top < 0 {
		return
	}
	if top < v.yearOff {
		v.yearOff = top
	} else if bottom >= v.yearOff+height {
		v.yearOff = max(bottom-height+1, 0)
	}
}

func (v *calendarView) removeSelectedEvent() tea.Cmd {
	event, ok := v.selectedRecording()
	if !ok {
		return nil
	}
	if v.confirmDelete != event.key() {
		v.confirmDelete = event.key()
		return nil
	}
	v.confirmDelete = ""
	return func() tea.Msg {
		return calActionMsg{label: "Event deleted", err: v.s.deleteEvent(event.ID)}
	}
}

// handleCalendarPickerKey gives the open picker every key. The picker stays open across a
// toggle: switching calendars on and off is a few decisions at once, not one, and the
// grid behind it is redrawn after each so the reader sees what they just changed.
func (v *calendarView) handleCalendarPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	picker := v.calendarPicker

	switch msg.String() {
	case "esc", "q":
		v.calendarPicker = nil
		return nil
	case "enter", " ", "space":
		calendar, ok := picker.highlighted()
		if !ok {
			return nil
		}
		return v.toggleCalendar(calendar)
	}

	picker.moveCursor(msg)
	return nil
}

func (v *calendarView) toggleCalendar(calendar Calendar) tea.Cmd {
	v.ensureShown()
	on := !v.shownCalendars[calendar.Name]
	v.shownCalendars[calendar.Name] = on
	if v.calendarPicker != nil {
		v.calendarPicker.setCalendars(v.listedCalendars(), v.shownCalendars)
	}
	v.settleSelection()
	return notify(toggleNotice(terminal.SanitizeLine(calendar.Name), on))
}

func (v *calendarView) handleTodoPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	picker := v.todoPicker
	if picker.editing() {
		switch msg.Key().Code {
		case tea.KeyEscape:
			picker.stopEditing()
			return nil
		case tea.KeyEnter:
			if picker.mode == todoRenaming {
				todo, title, ok := picker.renamed()
				picker.stopEditing()
				if !ok {
					return nil
				}
				return v.renameTodo(todo, title)
			}
			title, ok := picker.title()
			if !ok {
				return nil
			}
			picker.stopEditing()
			return v.addTodo(title)
		case tea.KeyBackspace:
			if picker.buf != "" {
				r := []rune(picker.buf)
				picker.buf = string(r[:len(r)-1])
			}
			return nil
		default:
			picker.buf += insertText(msg)
			return nil
		}
	}
	switch msg.String() {
	case "esc", "q":
		v.todoPicker = nil
		return nil
	case "a":
		picker.startAdding()
		return nil
	case "e":
		picker.startRenaming()
		return nil
	case "enter":
		if todo := picker.selected(); todo != nil {
			return v.toggleTodo(*todo)
		}
		return nil
	case "x":
		todo := picker.selected()
		if todo == nil {
			return nil
		}
		if picker.confirmed != todo.ID {
			picker.confirmed = todo.ID
			picker.status = "Press x again to delete " + terminal.SanitizeLine(todo.Title)
			return nil
		}
		return v.deleteTodo(*todo)
	}
	picker.moveCursor(msg)
	picker.status = ""
	return nil
}

func (v *calendarView) addTodo(title string) tea.Cmd {
	return func() tea.Msg {
		return calActionMsg{label: "To-do added", err: v.s.addTodo(title)}
	}
}

func (v *calendarView) renameTodo(todo Recording, title string) tea.Cmd {
	return func() tea.Msg {
		return calActionMsg{label: "To-do renamed", err: v.s.renameTodo(todo.ID, title)}
	}
}

func (v *calendarView) toggleTodo(todo Recording) tea.Cmd {
	return func() tea.Msg {
		if todo.Done() {
			return calActionMsg{label: "To-do cleared", err: v.s.uncompleteTodo(todo.ID)}
		}
		return calActionMsg{label: "To-do done", err: v.s.completeTodo(todo.ID)}
	}
}

func (v *calendarView) deleteTodo(todo Recording) tea.Cmd {
	return func() tea.Msg {
		return calActionMsg{label: "To-do deleted", err: v.s.deleteTodo(todo.ID)}
	}
}

func (v *calendarView) handleHabitPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	picker := v.habitPicker
	switch msg.String() {
	case "esc", "q":
		v.habitPicker = nil
		return nil
	case "a":
		v.habitForm = newHabitForm(habitFormCreate, Recording{})
		return nil
	case "e":
		if habit := picker.selected(); habit != nil {
			v.habitForm = newHabitForm(habitFormEdit, *habit)
		}
		return nil
	case "enter":
		if !picker.completable {
			picker.status = "Keeping a habit is done from the day or the week"
			return nil
		}
		if habit := picker.selected(); habit != nil {
			return v.toggleHabit(*habit)
		}
		return nil
	case "x":
		habit := picker.selected()
		if habit == nil {
			return nil
		}
		if picker.confirmed != habit.ID {
			picker.confirmed = habit.ID
			picker.status = "Press x again to delete " + terminal.SanitizeLine(habit.Title)
			return nil
		}
		return v.deleteHabit(*habit)
	}
	picker.moveCursor(msg)
	picker.status = ""
	return nil
}

func (v *calendarView) toggleHabit(habit Recording) tea.Cmd {
	when := v.day().Format("2006-01-02")
	return func() tea.Msg {
		if habit.Done() {
			return calActionMsg{label: "Habit cleared", err: v.s.uncompleteHabit(habit.ID, when)}
		}
		return calActionMsg{label: "Habit done", err: v.s.completeHabit(habit.ID, when)}
	}
}

func (v *calendarView) deleteHabit(habit Recording) tea.Cmd {
	return func() tea.Msg {
		return calActionMsg{label: "Habit deleted", err: v.s.deleteHabit(habit.ID)}
	}
}

func (v *calendarView) handleHabitForm(msg tea.KeyPressMsg) tea.Cmd {
	if msg.Key().Code == tea.KeyEscape {
		v.habitForm = nil
		return nil
	}
	_, submit := v.habitForm.handleKey(msg)
	if !submit {
		return nil
	}
	f := v.habitForm
	name := strings.TrimSpace(f.name)
	days, color, icon := f.daysCSV(), habitColorNames[f.color], habitIcons[f.icon]
	if f.mode == habitFormEdit {
		return func() tea.Msg {
			return calActionMsg{label: "Habit updated", err: v.s.editHabit(f.id, name, days, color, icon)}
		}
	}
	return func() tea.Msg {
		return calActionMsg{label: "Habit created", err: v.s.createHabit(name, days, color, icon)}
	}
}

func (v *calendarView) handleEventForm(msg tea.KeyPressMsg) tea.Cmd {
	if msg.Key().Code == tea.KeyEscape {
		v.eventForm = nil
		return nil
	}
	_, submit := v.eventForm.handleKey(msg)
	if !submit {
		return nil
	}
	f := v.eventForm
	in, err := f.eventIn()
	if err != nil {
		f.status = err.Error()
		return nil
	}
	if f.mode == eventFormEdit {
		return func() tea.Msg {
			return calActionMsg{label: "Event updated", err: v.s.updateEvent(f.id, in)}
		}
	}
	return func() tea.Msg {
		return calActionMsg{label: "Event created", err: v.s.addEvent(in)}
	}
}

func (v *calendarView) InThread() bool { return false }
func (v *calendarView) ExitThread()    {}
func (v *calendarView) Loading() bool  { return v.loading && !v.CapturingInput() }
func (v *calendarView) Resize(w, h int) {
	v.width, v.height = w, h
}
func (v *calendarView) CapturingInput() bool {
	return v.todoPicker != nil || v.habitPicker != nil || v.eventForm != nil || v.habitForm != nil || v.calendarPicker != nil
}

func omStr(o *format.OM, key string) string {
	if o == nil {
		return ""
	}
	v := o.Get(key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func omBool(o *format.OM, key string) bool {
	if o == nil {
		return false
	}
	b, _ := o.Get(key).(bool)
	return b
}
