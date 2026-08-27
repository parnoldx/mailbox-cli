package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/format"
	"mailbox/src/internal/terminal"
)

type contactsLoadedMsg struct {
	requestID uint64
	rows      []*format.OM
	err       error
}
type contactShownMsg struct {
	requestID uint64
	row       *format.OM
	err       error
}

type contactsView struct {
	s         *session
	styles    styles
	rows      []*format.OM
	cursor    int
	scroll    int
	width     int
	height    int
	detail    *format.OM
	searching bool
	searchBuf string
	query     string
	requestID uint64
	loading   bool
	notice    string
}

func newContactsView(s *session, st styles) *contactsView {
	return &contactsView{s: s, styles: st}
}

func (v *contactsView) Init() tea.Cmd {
	v.requestID++
	v.loading = true
	v.detail = nil
	id, q := v.requestID, v.query
	return func() tea.Msg {
		var rows []*format.OM
		var err error
		if q == "" {
			rows, err = v.s.contacts()
		} else {
			rows, err = v.s.contactSearch(q)
		}
		return contactsLoadedMsg{requestID: id, rows: rows, err: err}
	}
}

func (v *contactsView) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case contactsLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load contacts", msg.err)
			return nil, true
		}
		v.rows = msg.rows
		v.cursor, v.scroll = 0, 0
		return nil, true
	case contactShownMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			return notifyError("Contact", msg.err), true
		}
		v.detail = msg.row
		return nil, true
	}
	return nil, false
}

func (v *contactsView) View() string {
	if v.searching {
		return styleMuted.Render("Search: ") + v.searchBuf + "█"
	}
	if v.detail != nil {
		return v.detailView()
	}
	var b strings.Builder
	if v.notice != "" {
		b.WriteString(v.styles.title.Render(v.notice) + "\n")
	}
	if len(v.rows) == 0 {
		b.WriteString(styleMuted.Render("  (empty)"))
		return b.String()
	}
	cursorMarker, cursorText := cursorStyles()
	visible := max(v.height, 1)
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	}
	if v.cursor >= v.scroll+visible {
		v.scroll = v.cursor - visible + 1
	}
	end := min(v.scroll+visible, len(v.rows))
	for i := v.scroll; i < end; i++ {
		row := v.rows[i]
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == v.cursor {
			prefix = cursorMarker.Render("│") + " "
			style = cursorText
		}
		name := terminal.SanitizeLine(omStr(row, "name"))
		email := terminal.SanitizeLine(omStr(row, "email"))
		line := truncateToWidth(name, 28) + "  " + styleMuted.Render(truncateToWidth(email, max(v.width-34, 10)))
		if i == v.cursor {
			line = style.Render(truncateToWidth(name+"  "+email, max(v.width-4, 10)))
		}
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (v *contactsView) detailView() string {
	d := v.detail
	var b strings.Builder
	b.WriteString(v.styles.title.Render(omStr(d, "name")) + "\n")
	for _, key := range []string{"email", "note", "updated", "id"} {
		val := omStr(d, key)
		if val == "" {
			continue
		}
		b.WriteString(styleMuted.Render(key+": ") + val + "\n")
	}
	return b.String()
}

func (v *contactsView) HelpBindings() []helpBinding {
	if v.searching {
		return []helpBinding{{"enter", "search"}, {"esc", "cancel"}}
	}
	if v.detail != nil {
		return []helpBinding{{"esc/q", "back"}}
	}
	return []helpBinding{{"/", "search"}, {"enter", "open"}, {"ctrl+r", "reload"}}
}

func (v *contactsView) SubnavItems() ([]navItem, int, string, bool) {
	label := "Contacts"
	if v.query != "" {
		label = "Search: " + v.query
	}
	return nil, 0, label, true
}

func (v *contactsView) SubnavLeft() tea.Cmd  { return nil }
func (v *contactsView) SubnavRight() tea.Cmd { return nil }

func (v *contactsView) HandleContentKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.searching {
		switch msg.Key().Code {
		case tea.KeyEscape:
			v.searching = false
			return nil
		case tea.KeyEnter:
			v.query = strings.TrimSpace(v.searchBuf)
			v.searching = false
			return v.Init()
		case tea.KeyBackspace:
			if v.searchBuf != "" {
				r := []rune(v.searchBuf)
				v.searchBuf = string(r[:len(r)-1])
			}
			return nil
		}
		v.searchBuf += insertText(msg)
		return nil
	}
	if v.detail != nil {
		return nil
	}
	key := msg.String()
	switch msg.Key().Code {
	case tea.KeyUp:
		if v.cursor > 0 {
			v.cursor--
		}
	case tea.KeyDown:
		if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case tea.KeyEnter:
		return v.open()
	case tea.KeyEscape:
		if v.query != "" {
			v.query = ""
			return v.Init()
		}
	}
	switch key {
	case "k":
		if v.cursor > 0 {
			v.cursor--
		}
	case "j":
		if v.cursor < len(v.rows)-1 {
			v.cursor++
		}
	case "/":
		v.searching = true
		v.searchBuf = ""
	case "ctrl+r":
		return v.Init()
	}
	return nil
}

func (v *contactsView) open() tea.Cmd {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	id := omStr(v.rows[v.cursor], "id")
	v.requestID++
	v.loading = true
	rid := v.requestID
	return func() tea.Msg {
		row, err := v.s.contactShow(id)
		return contactShownMsg{requestID: rid, row: row, err: err}
	}
}

func (v *contactsView) InThread() bool { return v.detail != nil }
func (v *contactsView) ExitThread()    { v.detail = nil }
func (v *contactsView) Loading() bool  { return v.loading }
func (v *contactsView) Resize(w, h int) {
	v.width, v.height = w, h
}
func (v *contactsView) CapturingInput() bool { return v.searching }
