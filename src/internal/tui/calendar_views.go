package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"mailbox/src/internal/terminal"
)

type calendarViewMode int

const (
	viewDay calendarViewMode = iota
	viewWeek
	viewYear
)

func (m calendarViewMode) String() string {
	switch m {
	case viewDay:
		return "Day"
	case viewWeek:
		return "Week"
	case viewYear:
		return "Year"
	}
	return "Day"
}

func (m calendarViewMode) unit() string {
	switch m {
	case viewWeek:
		return "week"
	case viewYear:
		return "year"
	}
	return "day"
}

func calendarNavItems() []navItem {
	return []navItem{
		{shortcut: "1", label: viewDay.String()},
		{shortcut: "2", label: viewWeek.String()},
		{shortcut: "3", label: viewYear.String()},
	}
}

func weekStartDate(t time.Time, firstDay time.Weekday) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	diff := (int(d.Weekday()) - int(firstDay) + 7) % 7
	return d.AddDate(0, 0, -diff)
}

// Recording is anything on the calendar grid — event, todo, or habit.
type Recording struct {
	ID            string
	ParentID      string
	Title         string
	AllDay        bool
	StartsAt      time.Time
	EndsAt        time.Time
	Type          string
	Color         string
	Icon          string
	Days          string
	Calendar      string
	CalendarColor string
	Location      string
	Notes         string
	URL           string
	Remind        string
	Repeat        string
	Circle        bool
	CompletedAt   time.Time
}

func (r Recording) Starts() time.Time {
	if r.StartsAt.IsZero() || r.AllDay {
		return r.StartsAt
	}
	return r.StartsAt.Local()
}
func (r Recording) Ends() time.Time {
	if r.EndsAt.IsZero() || r.AllDay {
		return r.EndsAt
	}
	return r.EndsAt.Local()
}
func (r Recording) Done() bool  { return !r.CompletedAt.IsZero() }
func (r Recording) key() string { return r.ID }

func eventsByDate(events []Recording) map[string][]Recording {
	m := make(map[string][]Recording)
	for _, e := range events {
		st := e.Starts()
		if st.IsZero() {
			continue
		}
		et := e.Ends()
		if et.IsZero() || !et.After(st) || dateKey(st) == dateKey(et) {
			m[dateKey(st)] = append(m[dateKey(st)], e)
			continue
		}
		d := time.Date(st.Year(), st.Month(), st.Day(), 0, 0, 0, 0, st.Location())
		endDay := time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, et.Location())
		if et.Equal(endDay) {
			endDay = endDay.AddDate(0, 0, -1)
		}
		for !d.After(endDay) {
			m[dateKey(d)] = append(m[dateKey(d)], e)
			d = d.AddDate(0, 0, 1)
		}
	}
	return m
}

func dateKey(t time.Time) string { return t.Format("2006-01-02") }

const todosSectionLabel = "Sometime this week"

type selection struct {
	eventKey string
	day      time.Time
}

func (s selection) has(event Recording) bool {
	return s.eventKey != "" && s.eventKey == event.key()
}
func (s selection) onDay(day time.Time) bool {
	return !s.day.IsZero() && sameDay(s.day, day)
}

type cellKind int

const (
	cellEmpty cellKind = iota
	cellRule
	cellChrome
	cellTitle
	cellEdge
)

const eventEdge = '│'

type dayCell struct {
	kind     cellKind
	color    string
	selected bool
	now      bool
}

func (cell dayCell) style(muted lipgloss.Style) lipgloss.Style {
	style := cell.baseStyle(muted)
	if cell.now {
		return style.Foreground(colorAlert).Bold(true)
	}
	return style
}

func (cell dayCell) baseStyle(muted lipgloss.Style) lipgloss.Style {
	if cell.selected {
		fill, ink := eventFillAndInk(cell.color)
		return lipgloss.NewStyle().Background(ink).Foreground(fill).Bold(true)
	}
	switch cell.kind {
	case cellChrome:
		return lipgloss.NewStyle().Background(eventFillColor(cell.color))
	case cellTitle, cellEdge:
		return eventTextStyle(cell.color)
	default:
		return muted
	}
}

func eventFillColor(calendarColor string) color.Color {
	if slot, ok := heyColors[calendarColor]; ok {
		return slot
	}
	return colorPrimary
}

func eventFillAndInk(calendarColor string) (fill, ink color.Color) {
	return eventFillColor(calendarColor), lipgloss.Black
}

func eventTextStyle(calendarColor string) lipgloss.Style {
	fill, ink := eventFillAndInk(calendarColor)
	return lipgloss.NewStyle().Background(fill).Foreground(ink).Bold(true)
}

const hourRule = '┊'
const nowRule = '╎'

func nowColumn(day, now time.Time, daySpan int) int {
	if !sameDay(day, now) {
		return -1
	}
	at := (now.Hour()*60 + now.Minute()) * daySpan / (24 * 60)
	return min(at, daySpan-1)
}

func nowClock(now time.Time) string {
	separator := ":"
	if now.Second()%2 == 1 {
		separator = " "
	}
	return now.Format("15") + separator + now.Format("04")
}

func nowRow(now time.Time, nowCol, gridWidth int) string {
	clock := nowClock(now)
	at := min(max(nowCol-len(clock)/2, 0), max(gridWidth-len(clock), 0))
	row := make([]rune, gridWidth)
	for i := range row {
		row[i] = ' '
	}
	copy(row[at:], []rune(clock))
	return lipgloss.NewStyle().Foreground(colorAlert).Bold(true).Render(strings.TrimRight(string(row), " "))
}

func clockTime(t time.Time) string { return t.Format("15:04") }

func hourAxisLabels() []string {
	labels := make([]string, 24)
	for h := range 24 {
		labels[h] = fmt.Sprintf("%02d", h)
	}
	return labels
}

type placedEvent struct {
	rec      Recording
	startCol int
	endCol   int
	lane     int
}

func renderDayView(events, habits []Recording, anchor, now time.Time, hint string, width, height int, sel selection) string {
	var b strings.Builder
	muted := styleMuted
	chrome := lipgloss.NewStyle().Foreground(colorChrome)

	if len(habits) > 0 {
		b.WriteString(hintedSectionHeader("Habits", "b to manage", width))
		b.WriteString("\n")
		b.WriteString(renderHabitsRibbon(habits, width))
		b.WriteString("\n")
	}

	labels := hourAxisLabels()
	closing := labels[0]
	colWidth := max((width-len(closing))/24, 3)
	daySpan := colWidth * 24
	gridWidth := daySpan + 1

	b.WriteString(hintedSectionHeader(anchor.Local().Format("Monday, January 2"), hint, width))
	b.WriteString("\n")

	nowCol := nowColumn(anchor, now, daySpan)
	if nowCol >= 0 {
		b.WriteString(nowRow(now, nowCol, gridWidth))
		b.WriteString("\n")
	}

	var header strings.Builder
	for _, label := range labels {
		header.WriteString(label)
		if pad := colWidth - len(label); pad > 0 {
			header.WriteString(strings.Repeat(" ", pad))
		}
	}
	header.WriteString(closing)
	b.WriteString(chrome.Render(header.String()))
	b.WriteString("\n")

	var timed, allDay []Recording
	for _, e := range events {
		if e.AllDay {
			allDay = append(allDay, e)
		} else {
			timed = append(timed, e)
		}
	}

	placed := make([]placedEvent, 0, len(timed))
	for _, e := range timed {
		st := e.Starts()
		et := e.Ends()
		if st.IsZero() {
			continue
		}
		if et.IsZero() || !et.After(st) {
			et = st.Add(time.Hour)
		}
		startPos := (st.Hour()*60 + st.Minute()) * daySpan / (24 * 60)
		endPos := (et.Hour()*60 + et.Minute()) * daySpan / (24 * 60)
		if et.Day() != st.Day() || (et.Hour() == 0 && et.Minute() == 0 && et.After(st)) {
			endPos = daySpan
		}
		if endPos <= startPos {
			endPos = startPos + colWidth
		}
		startPos = min(startPos, daySpan-1)
		endPos = min(endPos, daySpan)
		if endPos-startPos < 3 {
			endPos = min(startPos+3, daySpan)
		}
		placed = append(placed, placedEvent{rec: e, startCol: startPos, endCol: endPos})
	}

	laneEnds := []int{}
	for i := range placed {
		assigned := false
		for l, laneEnd := range laneEnds {
			if placed[i].startCol >= laneEnd {
				placed[i].lane = l
				laneEnds[l] = placed[i].endCol
				assigned = true
				break
			}
		}
		if !assigned {
			placed[i].lane = len(laneEnds)
			laneEnds = append(laneEnds, placed[i].endCol)
		}
	}
	numLanes := len(laneEnds)
	lanes := make([][]placedEvent, numLanes)
	for _, pe := range placed {
		lanes[pe.lane] = append(lanes[pe.lane], pe)
	}

	spent := 2
	if nowCol >= 0 {
		spent++
	}
	if len(habits) > 0 {
		spent += 2
	}
	if len(allDay) > 0 {
		spent += 1 + len(allDay)
	}
	b.WriteString(renderDayGrid(lanes, gridWidth, colWidth, height-spent, nowCol, muted, sel))

	if len(allDay) > 0 {
		b.WriteString(sectionHeader("All day", width))
		b.WriteString("\n")
		for _, e := range allDay {
			b.WriteString(eventPill(e, gridWidth, sel))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderDayGrid(lanes [][]placedEvent, gridWidth, colWidth, rows, nowCol int, muted lipgloss.Style, sel selection) string {
	gaps := max(len(lanes)-1, 0)
	laneRows := shareDayRows(max(rows-gaps, 1), len(lanes))
	height := max(rows, 1)
	if total := sumOf(laneRows) + gaps; total > height {
		height = total
	}
	grid := make([][]string, height)
	cells := make([][]dayCell, height)
	for row := range height {
		grid[row] = make([]string, gridWidth)
		cells[row] = make([]dayCell, gridWidth)
		for col := range gridWidth {
			grid[row][col] = " "
		}
	}
	offset := 0
	for i, lane := range lanes {
		drawDayLane(grid, cells, lane, offset, laneRows[i], nowCol, sel)
		offset += laneRows[i] + 1
	}
	for row := range height {
		for col := 0; col < gridWidth; col += colWidth {
			if cells[row][col].kind == cellEmpty {
				grid[row][col] = string(hourRule)
				cells[row][col] = dayCell{kind: cellRule}
			}
		}
	}
	if nowCol >= 0 && nowCol < gridWidth {
		for row := range height {
			if cells[row][nowCol].kind == cellTitle {
				continue
			}
			grid[row][nowCol] = string(nowRule)
			cells[row][nowCol].now = true
		}
	}
	var b strings.Builder
	for row := range height {
		var seg strings.Builder
		cell := dayCell{}
		flush := func() {
			if s := seg.String(); s != "" {
				b.WriteString(cell.style(muted).Render(s))
				seg.Reset()
			}
		}
		for col := range gridWidth {
			if cells[row][col] != cell {
				flush()
				cell = cells[row][col]
			}
			seg.WriteString(grid[row][col])
		}
		flush()
		b.WriteString("\n")
	}
	return b.String()
}

func shareDayRows(rows, lanes int) []int {
	if lanes == 0 {
		return nil
	}
	const minLaneRows = 3
	share := max(rows/lanes, minLaneRows)
	extra := 0
	if share > minLaneRows {
		extra = rows - share*lanes
	}
	shares := make([]int, lanes)
	for i := range shares {
		shares[i] = share
		if i < extra {
			shares[i]++
		}
	}
	return shares
}

func sumOf(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func titleColumn(startCol, endCol, nowCol int) int {
	at := startCol + max((endCol-startCol-1)/2, 0)
	if at != nowCol || endCol-startCol < 2 {
		return at
	}
	if at+1 < endCol {
		return at + 1
	}
	return at - 1
}

func drawDayLane(grid [][]string, cells [][]dayCell, lane []placedEvent, rowOffset, rows, nowCol int, sel selection) {
	top := rowOffset
	bottom := rowOffset + rows - 1
	previousEnd := -1
	for _, pe := range lane {
		sc, ec := pe.startCol, pe.endCol
		selected := sel.has(pe.rec)
		fill := dayCell{kind: cellChrome, color: pe.rec.CalendarColor, selected: selected}
		titled := dayCell{kind: cellTitle, color: pe.rec.CalendarColor, selected: selected}
		edged := dayCell{kind: cellEdge, color: pe.rec.CalendarColor, selected: selected}
		for row := top; row <= bottom; row++ {
			for col := sc; col < ec; col++ {
				grid[row][col] = " "
				cells[row][col] = fill
			}
		}
		if sc == previousEnd && sc < ec {
			for row := top; row <= bottom; row++ {
				grid[row][sc] = string(eventEdge)
				cells[row][sc] = edged
			}
		}
		previousEnd = ec
		titleGlyphs := verticalTitleGlyphs(terminal.SanitizeLine(pe.rec.Title))
		n := bottom - top + 1
		titleRow := top + max((n-len(titleGlyphs))/2, 0)
		titleCol := titleColumn(sc, ec, nowCol)
		for i, glyph := range titleGlyphs {
			row := titleRow + i
			if row > bottom {
				break
			}
			text, width := glyph.text, glyph.width
			if width > ec-sc {
				text, width = "…", 1
			}
			col := min(max(titleCol, sc), ec-width)
			grid[row][col] = text
			for occupied := col; occupied < col+width; occupied++ {
				cells[row][occupied] = titled
				if occupied > col {
					grid[row][occupied] = ""
				}
			}
		}
	}
}

type titleGlyph struct {
	text  string
	width int
}

func verticalTitleGlyphs(title string) []titleGlyph {
	glyphs := make([]titleGlyph, 0, len(title))
	for title != "" {
		text, width := firstCluster(title)
		if text == "" {
			break
		}
		title = title[len(text):]
		if width > 0 {
			glyphs = append(glyphs, titleGlyph{text: text, width: width})
		}
	}
	return glyphs
}

type weekDayInfo struct {
	date   time.Time
	habits []Recording
	events []Recording
	allDay []Recording
}

func renderWeekView(events, habits, completions []Recording, anchor time.Time, firstWeekDay time.Weekday, width, height int, hint string, sel selection) string {
	var b strings.Builder
	muted := styleMuted
	chrome := lipgloss.NewStyle().Foreground(colorChrome)
	ws := weekStartDate(anchor, firstWeekDay)
	colWidth := (width - 6) / 7
	if colWidth < 8 {
		colWidth = 8
	}
	byDate := eventsByDate(events)
	days := make([]weekDayInfo, 7)
	for i := range 7 {
		d := ws.AddDate(0, 0, i)
		days[i] = weekDayInfo{date: d}
		for _, e := range byDate[dateKey(d)] {
			if e.AllDay {
				days[i].allDay = append(days[i].allDay, e)
			} else {
				days[i].events = append(days[i].events, e)
			}
		}
	}
	byID := make(map[string]Recording, len(habits))
	for _, habit := range habits {
		byID[habit.ID] = habit
	}
	for _, completion := range completions {
		done := completion.Starts()
		if done.IsZero() {
			continue
		}
		habit, ok := byID[completion.ParentID]
		if !ok {
			continue
		}
		for i := range days {
			if sameDay(days[i].date, done) {
				days[i].habits = append(days[i].habits, habit)
			}
		}
	}
	sep := chrome.Render(string(hourRule))
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(padTo(cell, colWidth))
		}
		b.WriteString("\n")
	}
	rowsUsed := 0
	if len(habits) > 0 {
		b.WriteString(hintedSectionHeader("Habits", "b to manage", width))
		b.WriteString("\n")
		rowsUsed++
		for _, row := range weekHabitBand(days, colWidth) {
			writeRow(row)
			rowsUsed++
		}
	}
	b.WriteString(hintedSectionHeader(weekLabel(ws), hint, width))
	b.WriteString("\n")
	rowsUsed++
	headers := make([]string, 7)
	for i := range 7 {
		label := weekDayColumnLabel(days[i].date, i == 0)
		style := chrome
		if sel.onDay(days[i].date) {
			style = cursorDayStyle(false)
		}
		headers[i] = style.Render(centerPad(label, colWidth))
	}
	writeRow(headers)
	rowsUsed++
	cols := make([][]string, 7)
	for i := range 7 {
		cols[i] = buildWeekDayColumn(days[i], colWidth, muted, sel)
	}
	maxH := 0
	for _, col := range cols {
		if len(col) > maxH {
			maxH = len(col)
		}
	}
	allDay := weekAllDayBand(days, colWidth, sel)
	if len(allDay) > 0 {
		rowsUsed += 1 + len(allDay)
	}
	maxH = max(maxH, height-rowsUsed, 1)
	cells := make([]string, 7)
	for row := range maxH {
		for i := range 7 {
			cells[i] = ""
			if row < len(cols[i]) {
				cells[i] = cols[i][row]
			}
		}
		writeRow(cells)
	}
	if len(allDay) > 0 {
		b.WriteString(sectionHeader("All day", width))
		b.WriteString("\n")
		for _, row := range allDay {
			writeRow(row)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func weekLabel(start time.Time) string {
	end := start.AddDate(0, 0, 6)
	if start.Month() == end.Month() {
		return fmt.Sprintf("%s %d – %d", start.Format("January"), start.Day(), end.Day())
	}
	return fmt.Sprintf("%s – %s", start.Format("January 2"), end.Format("January 2"))
}

func weekHabitBand(days []weekDayInfo, colWidth int) [][]string {
	const iconWidth = 3
	perRow := max(colWidth/iconWidth, 1)
	icons := make([][]string, len(days))
	rows := 0
	for i, day := range days {
		for _, habit := range day.habits {
			glyph := habitEmoji(habit.Icon)
			if glyph == "" {
				glyph = habitMarker(true)
			}
			icons[i] = append(icons[i], habitMarkerStyle(habit.Color).Render(glyph))
		}
		rows = max(rows, (len(icons[i])+perRow-1)/perRow)
	}
	rows = max(rows, 1)
	band := make([][]string, rows)
	for row := range band {
		band[row] = make([]string, len(days))
		for day := range days {
			var cell strings.Builder
			for i := row * perRow; i < min((row+1)*perRow, len(icons[day])); i++ {
				cell.WriteString(icons[day][i])
				cell.WriteString(" ")
			}
			band[row][day] = padTo(cell.String(), colWidth)
		}
	}
	return band
}

func buildWeekDayColumn(d weekDayInfo, width int, muted lipgloss.Style, sel selection) []string {
	var lines []string
	for _, e := range d.events {
		if when := eventTimeSpan(e, width); when != "" {
			lines = append(lines, muted.Render(when))
		}
		lines = append(lines, eventPill(e, width, sel))
	}
	return lines
}

func eventTimeSpan(event Recording, width int) string {
	starts := event.Starts()
	if starts.IsZero() {
		return ""
	}
	from := clockTime(starts)
	if ends := event.Ends(); !ends.IsZero() && ends.After(starts) {
		if span := from + "–" + clockTime(ends); len(span) <= width {
			return span
		}
	}
	return from
}

func weekAllDayBand(days []weekDayInfo, colWidth int, sel selection) [][]string {
	spans := weekAllDaySpans(days)
	if len(spans) == 0 {
		return nil
	}
	var lanes [][]bool
	rows := make([][]string, 0, len(spans))
	for _, span := range spans {
		lane := 0
		for ; lane < len(lanes); lane++ {
			if !span.overlaps(lanes[lane]) {
				break
			}
		}
		if lane == len(lanes) {
			lanes = append(lanes, make([]bool, len(days)))
			rows = append(rows, make([]string, len(days)))
		}
		for _, day := range span.days {
			lanes[lane][day] = true
			rows[lane][day] = eventPill(span.event, colWidth, sel)
		}
	}
	return rows
}

type weekAllDaySpan struct {
	event Recording
	days  []int
}

func (span weekAllDaySpan) overlaps(taken []bool) bool {
	for _, day := range span.days {
		if taken[day] {
			return true
		}
	}
	return false
}

func weekAllDaySpans(days []weekDayInfo) []weekAllDaySpan {
	var order []string
	spans := make(map[string]weekAllDaySpan)
	for i, day := range days {
		for _, event := range day.allDay {
			span, seen := spans[event.ID]
			if !seen {
				span.event = event
				order = append(order, event.ID)
			}
			span.days = append(span.days, i)
			spans[event.ID] = span
		}
	}
	gathered := make([]weekAllDaySpan, 0, len(order))
	for _, id := range order {
		gathered = append(gathered, spans[id])
	}
	sort.SliceStable(gathered, func(i, j int) bool {
		return len(gathered[i].days) > len(gathered[j].days)
	})
	return gathered
}

func eventPill(event Recording, width int, sel selection) string {
	title := truncateStr(terminal.SanitizeLine(event.Title), width)
	if pad := width - displayWidth(title); pad > 0 {
		title += strings.Repeat(" ", pad)
	}
	if sel.has(event) {
		fill, ink := eventFillAndInk(event.CalendarColor)
		return lipgloss.NewStyle().Background(ink).Foreground(fill).Bold(true).Render(title)
	}
	return eventTextStyle(event.CalendarColor).Render(title)
}

func weekDayColumnLabel(d time.Time, isFirstCol bool) string {
	dayName := strings.ToUpper(d.Weekday().String()[:3])
	dayNum := d.Day()
	if dayNum == 1 || isFirstCol {
		monthName := strings.ToUpper(d.Month().String()[:3])
		return fmt.Sprintf("%s %s %d", monthName, dayName, dayNum)
	}
	return fmt.Sprintf("%s %d", dayName, dayNum)
}

func renderYearView(events []Recording, anchor, now time.Time, firstWeekDay time.Weekday, width, _ int, hint string, sel selection, inCell bool) (view string, cursorTop, cursorBottom int) {
	var b strings.Builder
	muted := styleMuted
	chrome := lipgloss.NewStyle().Foreground(colorChrome)
	bright := lipgloss.NewStyle().Foreground(colorBright)
	primary := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	faint := styleMuted
	loc := anchor.Location()
	yearStart := time.Date(anchor.Year(), 1, 1, 0, 0, 0, 0, loc)
	yearEnd := time.Date(anchor.Year()+1, 1, 1, 0, 0, 0, 0, loc)
	gridStart := weekStartDate(yearStart, firstWeekDay)
	gridEndWeek := weekStartDate(yearEnd.AddDate(0, 0, -1), firstWeekDay)
	gridEnd := gridEndWeek.AddDate(0, 0, 7)
	byDate := eventsByDate(events)
	colWidth := max((width-6)/7, 9)
	sep := chrome.Render(string(hourRule))
	line := 0
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(padTo(cell, colWidth))
		}
		b.WriteString("\n")
		line++
	}
	b.WriteString(hintedSectionHeader(anchor.Format("2006"), hint, width))
	b.WriteString("\n")
	line++
	weekRule := chrome.Render(strings.Repeat("─", colWidth*7+6))
	cells := make([]string, 7)
	cursorTop, cursorBottom = -1, -1
	for d := gridStart; d.Before(gridEnd); {
		holdsCursor := false
		columns := make([][]string, 7)
		for i := range 7 {
			columns[i] = buildYearDayCell(d, byDate[dateKey(d)], colWidth,
				sameDay(d, now), d.Year() == anchor.Year(), inCell, i == 0, sel,
				primary, bright, muted, faint)
			holdsCursor = holdsCursor || sel.onDay(d)
			d = d.AddDate(0, 0, 1)
		}
		maxH := 0
		for _, column := range columns {
			maxH = max(maxH, len(column))
		}
		maxH = max(maxH, 1)
		if holdsCursor {
			cursorTop = line
		}
		for row := range maxH {
			for i := range 7 {
				cells[i] = ""
				if row < len(columns[i]) {
					cells[i] = columns[i][row]
				}
			}
			writeRow(cells)
		}
		if holdsCursor {
			cursorBottom = line - 1
		}
		if d.Before(gridEnd) {
			b.WriteString(weekRule)
			b.WriteString("\n")
			line++
		}
	}
	return strings.TrimRight(b.String(), "\n"), cursorTop, cursorBottom
}

func buildYearDayCell(d time.Time, dayEvents []Recording, colWidth int,
	isToday, isCurrentYear, inCell, weekStart bool, sel selection,
	primary, bright, muted, faint lipgloss.Style,
) []string {
	month, day := yearDayLabel(d, weekStart)
	label := day
	if month != "" {
		label = month + " " + day
	}
	if lipgloss.Width(label) > colWidth && d.Day() != 1 {
		month, label = "", day
	}
	headerStyle := muted
	switch {
	case isToday:
		headerStyle = primary
	case len(dayEvents) > 0 && isCurrentYear:
		headerStyle = bright
	case !isCurrentYear:
		headerStyle = faint
	}
	if sel.onDay(d) {
		return append([]string{cursorDayStyle(inCell).Render(padTo(truncateStr(label, colWidth), colWidth))},
			yearCellEvents(dayEvents, colWidth, isCurrentYear, sel)...)
	}
	head := headerStyle.Render(truncateStr(label, colWidth))
	if month != "" && lipgloss.Width(label) <= colWidth {
		head = yearMonthStyle(isCurrentYear).Render(month) + headerStyle.Render(" "+day)
	}
	return append([]string{head}, yearCellEvents(dayEvents, colWidth, isCurrentYear, sel)...)
}

func yearMonthStyle(isCurrentYear bool) lipgloss.Style {
	if !isCurrentYear {
		return styleMuted
	}
	return lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
}

func yearCellEvents(dayEvents []Recording, colWidth int, isCurrentYear bool, sel selection) []string {
	if !isCurrentYear {
		return nil
	}
	lines := make([]string, 0, len(dayEvents))
	for _, event := range dayEvents {
		lines = append(lines, eventPill(event, colWidth, sel))
	}
	return lines
}

func cursorDayStyle(inCell bool) lipgloss.Style {
	style := lipgloss.NewStyle().Reverse(true).Bold(true)
	if inCell {
		return style.Foreground(colorActive)
	}
	return style
}

func yearDayLabel(d time.Time, weekStart bool) (month, day string) {
	day = fmt.Sprintf("%s %d", strings.ToUpper(d.Weekday().String()[:3]), d.Day())
	if d.Day() == 1 || weekStart {
		month = strings.ToUpper(d.Month().String()[:3])
	}
	return month, day
}

func renderHabitsRibbon(habits []Recording, width int) string {
	return renderRibbon(habits, width, func(habit Recording) (string, lipgloss.Style, string) {
		return habitMarker(habit.Done()), habitMarkerStyle(habit.Color), habitLabel(habit)
	})
}

func renderTodosRibbon(todos []Recording, width int) string {
	return renderRibbon(todos, width, func(todo Recording) (string, lipgloss.Style, string) {
		label := terminal.SanitizeLine(todo.Title)
		if todo.Done() {
			return "■", styleMuted, label
		}
		return "□", lipgloss.NewStyle().Foreground(colorAlert).Bold(true), label
	})
}

func renderRibbon(items []Recording, width int, describe func(Recording) (string, lipgloss.Style, string)) string {
	var b strings.Builder
	used := 0
	for i, item := range items {
		marker, markerStyle, label := describe(item)
		labelStyle := lipgloss.NewStyle().Foreground(colorBright)
		if item.Done() {
			labelStyle = styleMuted
		}
		gap := ""
		if i > 0 {
			gap = "  "
		}
		if used+displayWidth(gap+marker+" "+label) > width {
			if used < width {
				b.WriteString(styleMuted.Render("…"))
			}
			break
		}
		used += displayWidth(gap + marker + " " + label)
		b.WriteString(gap + markerStyle.Render(marker) + " " + labelStyle.Render(label))
	}
	return b.String()
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func truncateStr(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if displayWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return fitGraphemes(s, maxLen-1) + "…"
}

func padTo(s string, width int) string {
	if pad := width - displayWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func centerPad(s string, width int) string {
	sw := displayWidth(s)
	pad := width - sw
	if pad <= 0 {
		return fitGraphemes(s, width)
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}
