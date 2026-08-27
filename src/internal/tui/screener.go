package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/folders"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/terminal"
)

type screenerLoadedMsg struct {
	requestID uint64
	listing   *mail.Listing
	err       error
}

type screenerDecisionMsg struct {
	name   string
	status string
	err    error
}

type screenerClosedMsg struct{}

type screenerClearedMsg struct{ err error }

type screenerView struct {
	s               *session
	styles          styles
	items           []*mail.Envelope
	cursor          int
	scroll          int
	width           int
	height          int
	requestID       uint64
	loading         bool
	notice          string
	inThread        bool
	threadLines     []string
	threadOff       int
	confirmingClear bool
}

func newScreenerView(s *session, st styles) *screenerView {
	return &screenerView{s: s, styles: st}
}

func (v *screenerView) Init() tea.Cmd {
	v.requestID++
	v.loading = true
	v.inThread = false
	v.confirmingClear = false
	id := v.requestID
	return func() tea.Msg {
		listing, err := v.s.list(folders.SCREENER, 1)
		return screenerLoadedMsg{requestID: id, listing: listing, err: err}
	}
}

func (v *screenerView) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case screenerLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load The Screener", msg.err)
			return nil, true
		}
		v.items = msg.listing.Items
		if v.cursor >= len(v.items) {
			v.cursor = max(len(v.items)-1, 0)
		}
		return nil, true
	case threadLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load thread", msg.err)
			return nil, true
		}
		v.inThread = true
		v.threadLines = renderThread(msg.walk, v.width)
		v.threadOff = 0
		return nil, true
	case screenerDecisionMsg:
		if msg.err != nil {
			return notifyError(msg.status, msg.err), true
		}
		return tea.Batch(notify(msg.status+" "+msg.name), v.Init()), true
	case screenerClearedMsg:
		if msg.err != nil {
			return notifyError("Could not clear The Screener", msg.err), true
		}
		v.items = nil
		v.inThread = false
		v.notice = "The Screener is clearing. Everyone waiting will be asked about again on their next email."
		return v.Init(), true
	}
	return nil, false
}

func (v *screenerView) View() string {
	if v.confirmingClear {
		return v.clearConfirmationView()
	}
	if v.inThread {
		if len(v.threadLines) == 0 {
			return styleMuted.Render("  (empty thread)")
		}
		end := min(v.threadOff+v.height, len(v.threadLines))
		return strings.Join(v.threadLines[v.threadOff:end], "\n")
	}
	var b strings.Builder
	if v.notice != "" {
		b.WriteString(v.styles.title.Render(v.notice) + "\n")
	}
	if len(v.items) == 0 {
		b.WriteString(styleMuted.Render("  Nobody waiting"))
		return b.String()
	}
	cursorMarker, cursorText := cursorStyles()
	for i, e := range v.items {
		if i < v.scroll {
			continue
		}
		if i-v.scroll >= max(v.height, 1) {
			break
		}
		isCursor := i == v.cursor
		prefix := "  "
		style := lipgloss.NewStyle()
		if isCursor {
			prefix = cursorMarker.Render("│") + " "
			style = cursorText
		}
		from := terminal.SanitizeLine(e.FromShort())
		subj := terminal.SanitizeLine(e.Subject)
		if subj == "" {
			subj = terminal.SanitizeLine(e.Summary())
		}
		line := fmt.Sprintf("%s  %s", truncateToWidth(from, 24), truncateToWidth(subj, max(v.width-30, 10)))
		b.WriteString(prefix + style.Render(line) + "\n")
		if isCursor && e.Preview != "" {
			b.WriteString("      " + styleMuted.Render(truncateToWidth(terminal.SanitizeLine(e.Preview), max(v.width-8, 10))) + "\n")
		}
	}
	return b.String()
}

func (v *screenerView) HelpBindings() []helpBinding {
	if v.confirmingClear {
		return []helpBinding{
			{"y", "clear all unscreened email"},
			{"n/esc", "cancel"},
		}
	}
	if v.inThread {
		return []helpBinding{
			{"↑↓", "scroll"},
			{"y/i", "approve"},
			{"d", "to feed"},
			{"p", "to paper trail"},
			{"n", "deny"},
			{"t", "trash"},
			{"esc/q", "back"},
		}
	}
	return []helpBinding{
		{"enter", "open"},
		{"y/i", "approve"},
		{"d", "to feed"},
		{"p", "to paper trail"},
		{"n", "deny"},
		{"t", "trash"},
		{"x", "clear all"},
		{"esc/q", "back"},
	}
}

func (v *screenerView) SubnavItems() ([]navItem, int, string, bool) {
	label := "The Screener"
	if n := len(v.items); n > 0 {
		label = fmt.Sprintf("The Screener (%d)", n)
	}
	return []navItem{{label: "The Screener"}}, 0, label, true
}

func (v *screenerView) SubnavLeft() tea.Cmd  { return nil }
func (v *screenerView) SubnavRight() tea.Cmd { return nil }

func (v *screenerView) HandleContentKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.confirmingClear {
		return v.handleClearConfirmationKey(msg)
	}
	key := msg.String()
	if v.inThread {
		switch msg.Key().Code {
		case tea.KeyEscape:
			v.inThread = false
			return nil
		case tea.KeyUp:
			if v.threadOff > 0 {
				v.threadOff--
			}
			return nil
		case tea.KeyDown:
			if v.threadOff+v.height < len(v.threadLines) {
				v.threadOff++
			}
			return nil
		}
		switch key {
		case "q":
			v.inThread = false
			return nil
		case "k":
			if v.threadOff > 0 {
				v.threadOff--
			}
			return nil
		case "j":
			if v.threadOff+v.height < len(v.threadLines) {
				v.threadOff++
			}
			return nil
		}
	} else {
		switch msg.Key().Code {
		case tea.KeyEscape:
			return func() tea.Msg { return screenerClosedMsg{} }
		case tea.KeyUp:
			if v.cursor > 0 {
				v.cursor--
			}
		case tea.KeyDown:
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
		case tea.KeyEnter:
			return v.openSelected()
		}
		switch key {
		case "q":
			return func() tea.Msg { return screenerClosedMsg{} }
		case "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "j":
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
		case "x", "X":
			if len(v.items) > 0 {
				v.confirmingClear = true
				v.notice = ""
			}
			return nil
		}
	}
	switch key {
	case "y", "i":
		return v.decide(folders.INBOX, "Approved")
	case "d":
		return v.decide(folders.FEED, "Approved to The Feed")
	case "p":
		return v.decide(folders.PAPER_TRAIL, "Approved to Paper Trail")
	case "n":
		return v.decide(folders.BLOCK, "Denied")
	case "t":
		return v.decide(folders.TRASH, "Trashed")
	}
	return nil
}

func (v *screenerView) openSelected() tea.Cmd {
	if v.cursor < 0 || v.cursor >= len(v.items) {
		return nil
	}
	e := v.items[v.cursor]
	v.requestID++
	v.loading = true
	id, folder, uid := v.requestID, e.Folder, e.UID
	if folder == "" {
		folder = folders.SCREENER
	}
	return func() tea.Msg {
		walk, err := v.s.thread(folder, uid)
		return threadLoadedMsg{requestID: id, folder: folder, uid: uid, walk: walk, err: err}
	}
}

func (v *screenerView) decide(dest, status string) tea.Cmd {
	if v.cursor < 0 || v.cursor >= len(v.items) {
		return nil
	}
	e := v.items[v.cursor]
	name := e.FromShort()
	return func() tea.Msg {
		err := v.s.move(e.Folder, e.UID, dest)
		return screenerDecisionMsg{name: name, status: status, err: err}
	}
}

func (v *screenerView) InThread() bool { return true }
func (v *screenerView) ExitThread() {
	if v.inThread {
		v.inThread = false
	}
}
func (v *screenerView) Loading() bool { return v.loading }
func (v *screenerView) Resize(w, h int) {
	v.width, v.height = w, h
}
func (v *screenerView) CapturingInput() bool { return v.inThread || v.confirmingClear }

func (v *screenerView) handleClearConfirmationKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.Key().Code == tea.KeyEnter || msg.String() == "y" {
		v.confirmingClear = false
		return func() tea.Msg {
			return screenerClearedMsg{err: v.s.clearScreener()}
		}
	}
	if msg.Key().Code == tea.KeyEscape || msg.String() == "n" || msg.String() == "q" {
		v.confirmingClear = false
	}
	return nil
}

func (v *screenerView) clearConfirmationView() string {
	width := max(v.width-4, 20)
	var b strings.Builder
	b.WriteString(v.styles.title.Render("Not sure what to do with these?") + "\n\n")
	for _, line := range wrapText("If you don't want to decide on these senders, you can clear them all.", width) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for _, line := range wrapText("All emails currently in the Screener will go to the trash. You'll be asked to screen each sender again if they email you in the future.", width) {
		b.WriteString(styleMuted.Render(line) + "\n")
	}
	return b.String()
}
