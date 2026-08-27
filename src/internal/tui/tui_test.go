package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"mailbox/src/internal/folders"
	"mailbox/src/internal/mail"
)

func TestSeenFlag(t *testing.T) {
	if seenFlag(nil) {
		t.Fatal("empty flags are unseen")
	}
	if !seenFlag([]string{`\Seen`}) {
		t.Fatal(`\Seen should count`)
	}
	if !seenFlag([]string{"Seen"}) {
		t.Fatal("Seen should count")
	}
}

func TestMailNavIncludesLabels(t *testing.T) {
	items := mailNavItems()
	if items[len(items)-1].label != "Labels" || items[len(items)-1].shortcut != "L" {
		t.Fatalf("last nav %v", items[len(items)-1])
	}
	if items[len(items)-2].label != "Sent" {
		t.Fatalf("Labels should sit after Sent, got %q", items[len(items)-2].label)
	}
	v := &mailView{width: 80, height: 12}
	got, sel, _, _ := v.SubnavItems()
	if len(got) != len(items) || sel != 0 {
		t.Fatalf("inbox nav len=%d sel=%d", len(got), sel)
	}
	v.labelBrowse = true
	got, sel, title, _ := v.SubnavItems()
	if title != "Labels" || sel != len(knownBoxes) {
		t.Fatalf("browse subnav title=%q sel=%d", title, sel)
	}
	if got[sel].label != "Labels" {
		t.Fatalf("selected %q", got[sel].label)
	}
}

func TestBoxForShortcut(t *testing.T) {
	if got := boxForShortcut("1"); got != 0 {
		t.Fatalf("1 -> %d want 0", got)
	}
	if got := boxForShortcut("2"); knownBoxes[got].imap != folders.FEED {
		t.Fatalf("2 -> %s", knownBoxes[got].imap)
	}
	if boxForShortcut("9") != -1 {
		t.Fatal("unknown shortcut")
	}
}

func TestDestIMAP(t *testing.T) {
	if destIMAP("i") != folders.INBOX {
		t.Fatal("i")
	}
	if destIMAP("d") != folders.FEED {
		t.Fatal("d")
	}
	if destIMAP("!") != folders.JUNK {
		t.Fatal("!")
	}
	if destIMAP("n") != folders.BLOCK {
		t.Fatal("n")
	}
	if destIMAP("x") != "" {
		t.Fatal("unknown")
	}
}

func TestSectionForShortcut(t *testing.T) {
	if sectionForShortcut("M") != sectionMail {
		t.Fatal("M")
	}
	if sectionForShortcut("C") != sectionCalendar {
		t.Fatal("C")
	}
	if sectionForShortcut("m") != -1 {
		t.Fatal("lowercase is not a jump")
	}
}

func TestSplitSeen(t *testing.T) {
	items := []*mail.Envelope{
		{UID: "1", Flags: []string{`\Seen`}, Subject: "old"},
		{UID: "2", Subject: "new"},
		{UID: "3", Flags: []string{`\Seen`}, Subject: "older"},
	}
	unseen, seen := splitSeen(items)
	if len(unseen) != 1 || unseen[0].UID != "2" {
		t.Fatalf("unseen=%v", unseen)
	}
	if len(seen) != 2 {
		t.Fatalf("seen=%d", len(seen))
	}
}

func TestTruncateToWidth(t *testing.T) {
	if truncateToWidth("hello", 10) != "hello" {
		t.Fatal("short")
	}
	got := truncateToWidth("hello world", 8)
	if !strings.HasSuffix(got, "...") || len(got) > 8 {
		t.Fatalf("got %q", got)
	}
}

func TestRenderNavRowFits(t *testing.T) {
	row := renderNavRow(sectionItems, 0, true, 80, true)
	if !strings.Contains(row, "ail") || !strings.Contains(row, "alendar") {
		t.Fatalf("row=%q", row)
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("one two three four", 8)
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
}

func TestWeekStartMonday(t *testing.T) {
	// 2026-08-26 is Wednesday
	d := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	got := weekStartDate(d, time.Monday)
	if got.Weekday() != time.Monday || got.Day() != 24 {
		t.Fatalf("got %s", got)
	}
}

func TestParseCalTime(t *testing.T) {
	timed, allDay := parseCalTime("2026-09-02 14:00")
	if allDay || timed.Hour() != 14 {
		t.Fatalf("timed %v allDay %v", timed, allDay)
	}
	day, allDay := parseCalTime("2026-09-02")
	if !allDay || day.Day() != 2 {
		t.Fatalf("day %v allDay %v", day, allDay)
	}
}

func TestHabitLabelUsesIconEmoji(t *testing.T) {
	got := habitLabel(Recording{Title: "Gym", Icon: "💪"})
	if !strings.Contains(got, "💪") || !strings.Contains(got, "Gym") {
		t.Fatalf("got %q", got)
	}
}

func TestWeekHabitBandShowsGymEmoji(t *testing.T) {
	days := make([]weekDayInfo, 7)
	days[0].habits = []Recording{{Title: "Gym", Icon: "💪"}}
	joined := ""
	for _, row := range weekHabitBand(days, 12) {
		joined += strings.Join(row, "")
	}
	if !strings.Contains(joined, "💪") {
		t.Fatalf("missing emoji: %q", joined)
	}
	if strings.Contains(joined, "●") {
		t.Fatalf("still a dot: %q", joined)
	}
}

func TestNextWholeHour(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 41, 0, 0, time.UTC)
	got := nextWholeHour(at)
	if got.Hour() != 10 || got.Minute() != 0 {
		t.Fatalf("got %s", got)
	}
}

func TestEventFormRequiresName(t *testing.T) {
	f := newEventForm(time.Date(2026, 8, 27, 9, 41, 0, 0, time.UTC))
	if f.validate() == "" {
		t.Fatal("empty name should fail")
	}
	f.title = "Dentist"
	if f.validate() != "" {
		t.Fatalf("got %q", f.validate())
	}
}

func TestEventFormShowsNotifyAndMore(t *testing.T) {
	f := newEventForm(time.Date(2026, 8, 27, 9, 41, 0, 0, time.UTC))
	f.title = "Dentist"
	got := f.view()
	if !strings.Contains(got, "Notify") || !strings.Contains(got, "More") {
		t.Fatalf("missing notify/more:\n%s", got)
	}
	if strings.Contains(got, "Location") {
		t.Fatal("more row open by default")
	}
	f.focus = efMore
	f.handleKey(keyPress(" "))
	if !f.revealed {
		t.Fatal("space on More should reveal")
	}
	got = f.view()
	if !strings.Contains(got, "Location") || !strings.Contains(got, "Repeat") || !strings.Contains(got, "Circle") {
		t.Fatalf("more fields missing:\n%s", got)
	}
	in, err := f.eventIn()
	if err != nil {
		t.Fatal(err)
	}
	if !in.Has.Remind || !in.Has.Location {
		t.Fatal("extras should be on the write")
	}
}

func TestRenderDayViewHasHourAxis(t *testing.T) {
	anchor := time.Date(2026, 8, 26, 10, 0, 0, 0, time.Local)
	events := []Recording{{
		ID: "1", Title: "Standup", StartsAt: anchor, EndsAt: anchor.Add(time.Hour),
		CalendarColor: "blue",
	}}
	got := renderDayView(events, nil, anchor, anchor, "p/n day", 96, 24, selection{})
	if !strings.Contains(got, "10") || !strings.Contains(got, "Wednesday") {
		t.Fatalf("day view:\n%s", got)
	}
}

func keyPress(key string) tea.KeyPressMsg {
	k := tea.Key{Text: key}
	switch key {
	case "esc":
		k = tea.Key{Code: tea.KeyEscape}
	case "enter":
		k = tea.Key{Code: tea.KeyEnter}
	case "up":
		k = tea.Key{Code: tea.KeyUp}
	case "down":
		k = tea.Key{Code: tea.KeyDown}
	case "backspace":
		k = tea.Key{Code: tea.KeyBackspace}
	case " ", "space":
		k = tea.Key{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg(k)
}

func testTodoView() *calendarView {
	return &calendarView{
		width: 80, height: 24,
		anchor: time.Date(2026, 8, 22, 9, 0, 0, 0, time.Local),
		todos: []Recording{
			{ID: "7", Title: "Clean the attic", Type: "todo"},
			{ID: "8", Title: "Send the invoice", Type: "todo", CompletedAt: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)},
		},
	}
}

func TestTodosModalListsAndAddsInline(t *testing.T) {
	view := testTodoView()
	view.HandleContentKey(keyPress("s"))
	if view.todoPicker == nil {
		t.Fatal("s did not open the to-dos modal")
	}
	rendered := view.View()
	if !strings.Contains(rendered, todosSectionLabel) {
		t.Fatalf("modal title missing: %q", rendered)
	}
	if !strings.Contains(rendered, "Clean the attic") || !strings.Contains(rendered, "Send the invoice") {
		t.Fatalf("list missing: %q", rendered)
	}
	if !strings.Contains(rendered, "╭") {
		t.Fatalf("no frame: %q", rendered)
	}
	if strings.Contains(rendered, "New to-do:") {
		t.Fatal("input shown while browsing")
	}

	view.HandleContentKey(keyPress("a"))
	if view.todoPicker.mode != todoAdding {
		t.Fatal("a should open the input")
	}
	rendered = view.View()
	if !strings.Contains(rendered, "New to-do:") {
		t.Fatalf("input not inline: %q", rendered)
	}
	if !strings.Contains(rendered, "Clean the attic") {
		t.Fatal("adding replaced the list")
	}

	view.HandleContentKey(keyPress("a"))
	if view.todoPicker.buf != "a" {
		t.Fatalf("key not routed to input: %q", view.todoPicker.buf)
	}

	view.todoPicker.buf = "  "
	if cmd := view.HandleContentKey(keyPress("enter")); cmd != nil {
		t.Error("unnamed to-do was sent")
	}
	if view.todoPicker.status == "" {
		t.Error("unnamed to-do said nothing")
	}

	view.HandleContentKey(keyPress("esc"))
	if view.todoPicker.editing() {
		t.Fatal("esc did not cancel the input")
	}
	view.HandleContentKey(keyPress("esc"))
	if view.todoPicker != nil || view.CapturingInput() {
		t.Error("esc did not close the to-dos modal")
	}
}

func TestWeekArrowsSelectTheDay(t *testing.T) {
	anchor := time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local) // Wednesday
	view := &calendarView{width: 96, height: 24, mode: viewWeek, anchor: anchor}
	view.HandleContentKey(keyPress("left"))
	got := view.day()
	want := time.Date(2026, 8, 25, 12, 0, 0, 0, time.Local)
	if !sameDay(got, want) {
		t.Fatalf("left selected %s, want Tuesday the 25th (not the previous week)", got.Format("2006-01-02"))
	}
	view.HandleContentKey(keyPress("right"))
	if !sameDay(view.day(), anchor) {
		t.Fatalf("right did not return to Wednesday: %s", view.day().Format("2006-01-02"))
	}
}

func TestYearViewFocusesToday(t *testing.T) {
	anchor := time.Date(2026, 8, 22, 9, 0, 0, 0, time.Local)
	view := &calendarView{width: 96, height: 16, mode: viewYear, anchor: anchor}
	got := view.View()
	if strings.Contains(got, "JAN") && !strings.Contains(got, "AUG") {
		t.Fatalf("year opened on January, not August:\n%s", got)
	}
}

func TestHabitFormOpensOverPicker(t *testing.T) {
	view := testTodoView()
	view.HandleContentKey(keyPress("b"))
	view.HandleContentKey(keyPress("a"))
	if view.habitForm == nil {
		t.Fatal("a did not open the habit form")
	}
	rendered := view.View()
	if !strings.Contains(rendered, "Create habit") {
		t.Fatalf("form not overlayed: %q", rendered)
	}
}

func TestSearchStaysOverTheList(t *testing.T) {
	view := &mailView{
		width: 80, height: 12,
		items:       []*mail.Envelope{{Subject: "Hello from Bob", From: "Bob"}},
		searchInput: true,
	}
	got := view.View()
	if !strings.Contains(got, "Hello from Bob") {
		t.Fatalf("list gone during search: %q", got)
	}
	if !strings.Contains(got, "Search") {
		t.Fatalf("search overlay missing: %q", got)
	}
}

func TestTodosModalRenameAndDeleteKeys(t *testing.T) {
	view := testTodoView()
	view.HandleContentKey(keyPress("s"))
	view.HandleContentKey(keyPress("e"))
	if view.todoPicker.mode != todoRenaming || view.todoPicker.buf != "Clean the attic" {
		t.Fatalf("e should fill the title, got mode=%d buf=%q", view.todoPicker.mode, view.todoPicker.buf)
	}
	view.HandleContentKey(keyPress("esc"))

	if cmd := view.HandleContentKey(keyPress("x")); cmd != nil || view.todoPicker.confirmed != "7" {
		t.Fatalf("first x = cmd:%v confirmed:%s", cmd, view.todoPicker.confirmed)
	}
	view.HandleContentKey(keyPress("down"))
	if view.todoPicker.confirmed != "" {
		t.Error("delete question survived the cursor moving")
	}
}

func testCalendarPickerView() *calendarView {
	day := time.Date(2026, 8, 26, 10, 0, 0, 0, time.Local)
	return &calendarView{
		width: 80, height: 20,
		anchor: day,
		calendars: []Calendar{
			{Name: "Design Team", Color: "red"},
			{Name: "Kalender", Color: "blue"},
		},
		shownCalendars: map[string]bool{"Design Team": true, "Kalender": true},
		events: []Recording{
			{ID: "1", Title: "Review", StartsAt: day, EndsAt: day.Add(time.Hour), Calendar: "Design Team", CalendarColor: "red"},
			{ID: "2", Title: "Standup", StartsAt: day.Add(2 * time.Hour), EndsAt: day.Add(3 * time.Hour), Calendar: "Kalender", CalendarColor: "blue"},
		},
	}
}

// The picker lists what can be switched, marks what is on, and stays open across a
// toggle: switching calendars is a few decisions at once rather than one.
func TestCalendarPickerTogglesTheCalendarsItLists(t *testing.T) {
	v := testCalendarPickerView()

	if cmd := v.HandleContentKey(keyPress("g")); cmd != nil || v.calendarPicker == nil {
		t.Fatal("g did not open the calendars modal")
	}
	if !v.CapturingInput() {
		t.Error("the calendars modal does not hold the keys")
	}
	view := v.View()
	if !strings.Contains(view, "Calendars") || !strings.Contains(view, "Design Team") {
		t.Errorf("the modal does not list the calendars: %q", view)
	}

	cmd := v.HandleContentKey(keyPress(" "))
	if cmd == nil {
		t.Fatal("space did not switch the calendar")
	}
	if v.calendarPicker == nil {
		t.Error("the modal closed on a toggle")
	}
	if v.shownCalendars["Design Team"] {
		t.Error("space did not hide the calendar under the cursor")
	}

	got := v.visibleEvents()
	if len(got) != 1 || got[0].Title != "Standup" {
		t.Errorf("visible events = %+v, want Standup alone", got)
	}

	v.HandleContentKey(keyPress("esc"))
	if v.calendarPicker != nil || v.CapturingInput() {
		t.Error("esc did not close the calendars modal")
	}
}

func TestCalendarPickerStaysShutWithNothingToSwitch(t *testing.T) {
	v := &calendarView{}
	v.HandleContentKey(keyPress("g"))
	if v.calendarPicker != nil {
		t.Error("g opened a modal with nothing to switch")
	}
}

func TestScreenerClearAll(t *testing.T) {
	v := &screenerView{
		width: 80, height: 12,
		items: []*mail.Envelope{{UID: "1", Folder: folders.SCREENER, From: "Bob"}},
	}
	v.HandleContentKey(keyPress("x"))
	if !v.confirmingClear || !v.CapturingInput() {
		t.Fatal("x did not ask to clear")
	}
	got := v.View()
	if !strings.Contains(got, "clear them all") || !strings.Contains(got, "trash") {
		t.Fatalf("confirm missing: %q", got)
	}
	if cmd := v.HandleContentKey(keyPress("n")); cmd != nil || v.confirmingClear {
		t.Fatal("n did not cancel")
	}
	v.HandleContentKey(keyPress("x"))
	if cmd := v.HandleContentKey(keyPress("y")); cmd == nil {
		t.Fatal("y did not clear")
	}
}

func TestScreenerTrash(t *testing.T) {
	v := &screenerView{
		items: []*mail.Envelope{{UID: "1", Folder: folders.SCREENER, From: "Bob"}},
	}
	if cmd := v.HandleContentKey(keyPress("t")); cmd == nil {
		t.Fatal("t did not trash")
	}
}

func TestScreenerEnterOpensTheEmail(t *testing.T) {
	v := &screenerView{
		width: 80, height: 12,
		items: []*mail.Envelope{{UID: "1", Folder: folders.SCREENER, From: "Bob", Subject: "Hello"}},
	}
	if cmd := v.HandleContentKey(keyPress("enter")); cmd == nil {
		t.Fatal("enter did not open the email")
	}
	if !v.loading {
		t.Error("opening should wait on the thread")
	}

	v.Update(threadLoadedMsg{
		requestID: v.requestID,
		walk:      &mail.ThreadWalk{Messages: []*mail.ThreadMessage{{From: "Bob", Subject: "Hello", Body: "Can we talk?"}}},
	})
	if !v.inThread || !v.CapturingInput() {
		t.Fatal("thread did not open")
	}
	got := v.View()
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "Can we talk?") {
		t.Fatalf("email not shown: %q", got)
	}

	v.HandleContentKey(keyPress("esc"))
	if v.inThread {
		t.Fatal("esc did not leave the email")
	}
}

func TestCalendarPickerHelpNamesTheKey(t *testing.T) {
	v := testCalendarPickerView()
	found := false
	for _, b := range v.HelpBindings() {
		if b.key == "g" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("g calendars missing from help")
	}
}

func TestMailListShowsLabels(t *testing.T) {
	v := &mailView{
		width: 80, height: 12,
		items: []*mail.Envelope{
			{UID: "1", Folder: folders.INBOX, Subject: "Hi", From: "Ann", Flags: []string{"seen", "invoices"}},
		},
	}
	got := v.View()
	if !strings.Contains(got, "invoices") {
		t.Fatalf("labels missing: %q", got)
	}
}

func TestLabelKeyOpensPicker(t *testing.T) {
	v := &mailView{
		width: 80, height: 12,
		items: []*mail.Envelope{
			{UID: "1", Folder: folders.INBOX, Subject: "Hi", Flags: []string{"travel-receipts"}},
		},
	}
	if cmd := v.HandleContentKey(keyPress("l")); cmd != nil {
		t.Fatal("no session: l should not fetch")
	}
	if !v.labelPrompt || !v.CapturingInput() {
		t.Fatal("l should open labels")
	}
	got := v.View()
	if !strings.Contains(got, "travel-receipts") {
		t.Fatalf("picker: %q", got)
	}
	found := false
	for _, b := range v.HelpBindings() {
		if b.key == "enter" {
			found = true
		}
	}
	if !found {
		t.Fatal("picker help missing enter")
	}
}

func TestLabelEnterViews(t *testing.T) {
	v := &mailView{
		width: 80, height: 12,
		items: []*mail.Envelope{
			{UID: "1", Folder: folders.INBOX, Subject: "Hi", Flags: []string{"travel-receipts"}},
		},
		labelCacheOK: true,
		labelCache:   []string{"travel-receipts"},
	}
	v.HandleContentKey(keyPress("l"))
	v.HandleContentKey(keyPress("enter"))
	if v.labelPrompt {
		t.Fatal("enter should close picker")
	}
	if v.labelFilter != "travel-receipts" {
		t.Fatalf("filter=%q", v.labelFilter)
	}
	_, _, label, _ := v.SubnavItems()
	if label != "Label: travel-receipts" {
		t.Fatalf("subnav %q", label)
	}
	v.HandleContentKey(keyPress("esc"))
	if v.labelFilter != "" {
		t.Fatal("esc should clear filter")
	}
}

func TestLabelSpaceKeepsPicker(t *testing.T) {
	v := &mailView{
		width: 80, height: 12,
		items: []*mail.Envelope{
			{UID: "1", Folder: folders.INBOX, Subject: "Hi", Flags: []string{"travel-receipts"}},
		},
		labelCacheOK: true,
		labelCache:   []string{"travel-receipts"},
	}
	v.HandleContentKey(keyPress("l"))
	v.HandleContentKey(keyPress(" "))
	if !v.labelPrompt {
		t.Fatal("space should keep picker")
	}
	if v.labelFilter != "" {
		t.Fatal("space should not filter")
	}
}

func TestLabelBrowse(t *testing.T) {
	v := &mailView{
		width: 80, height: 12,
		labelCacheOK: true,
		labelCache:   []string{"invoices"},
	}
	v.HandleContentKey(keyPress("L"))
	if !v.labelBrowse {
		t.Fatal("L should open labels")
	}
	_, _, title, _ := v.SubnavItems()
	if title != "Labels" {
		t.Fatalf("subnav %q", title)
	}
	if !strings.Contains(v.View(), "invoices") {
		t.Fatalf("list: %q", v.View())
	}
	v.HandleContentKey(keyPress("enter"))
	if v.labelBrowse {
		t.Fatal("enter should leave browse")
	}
	if v.labelFilter != "invoices" {
		t.Fatalf("filter=%q", v.labelFilter)
	}
	_, _, title, _ = v.SubnavItems()
	if title != "Label: invoices" {
		t.Fatalf("subnav %q", title)
	}
	v.HandleContentKey(keyPress("esc"))
	if !v.labelBrowse || v.labelFilter != "" {
		t.Fatal("esc should return to labels")
	}
}
