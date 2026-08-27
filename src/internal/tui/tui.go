package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

type ctrlCResetMsg struct{}

type Options struct {
	Screener bool
}

type sectionView interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (tea.Cmd, bool)
	View() string
	HelpBindings() []helpBinding
	SubnavItems() (items []navItem, selected int, label string, centered bool)
	SubnavLeft() tea.Cmd
	SubnavRight() tea.Cmd
	HandleContentKey(msg tea.KeyPressMsg) tea.Cmd
	InThread() bool
	ExitThread()
	Resize(width, height int)
	Loading() bool
}

type inputCapturer interface {
	CapturingInput() bool
}

type pendingDetailCanceler interface {
	CancelPendingDetail() bool
}

type model struct {
	width, height int
	session       *session
	styles        styles
	help          helpBar
	account       string

	section    section
	focus      focusRow
	activeView sectionView

	mailView     *mailView
	contactsView *contactsView
	calendarView *calendarView
	screenerView *screenerView

	toast   notifyMsg
	toastID uint64

	loading      bool
	spinnerPhase float64
	err          error
	ctrlCOnce    bool
	openScreener bool
}

func newModel(s *session, opts Options) model {
	applyTheme()
	st := newStyles()
	mv := newMailView(s, st)
	ov := newContactsView(s, st)
	cv := newCalendarView(s, st)
	sv := newScreenerView(s, st)
	account := ""
	if s.acct != nil {
		account = s.acct.Email
	}
	m := model{
		session:      s,
		styles:       st,
		help:         newHelpBar(st),
		account:      account,
		section:      sectionMail,
		focus:        rowContent,
		activeView:   mv,
		mailView:     mv,
		contactsView: ov,
		calendarView: cv,
		screenerView: sv,
		loading:      true,
		openScreener: opts.Screener,
	}
	return m
}

func (m model) Init() tea.Cmd {
	cmd := m.activeView.Init()
	if m.openScreener {
		return tea.Batch(cmd, spinnerTick(), func() tea.Msg { return openScreenerMsg{} })
	}
	return tea.Batch(cmd, spinnerTick())
}

type openScreenerMsg struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case notifyMsg:
		return m, m.showToast(msg)
	case toastExpiredMsg:
		if msg.id == m.toastID {
			m.toast = notifyMsg{}
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.setWidth(msg.Width)
		h := m.contentHeight()
		m.activeView.Resize(msg.Width, h)
		m.updateHelpBindings()
		return m, nil
	case spinnerTickMsg:
		if m.loading {
			m.spinnerPhase += 0.15
			return m, spinnerTick()
		}
		return m, nil
	case ctrlCResetMsg:
		m.ctrlCOnce = false
		m.updateHelpBindings()
		return m, nil
	case openScreenerMsg:
		return m.switchToScreener()
	case screenerClosedMsg:
		return m.closeScreener()
	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	cmd, consumed := m.activeView.Update(msg)
	if consumed {
		cmd = m.syncLoading(cmd)
		m.updateHelpBindings()
		return m, cmd
	}
	return m, cmd
}

func (m *model) syncLoading(cmd tea.Cmd) tea.Cmd {
	now := m.activeView.Loading()
	if now && !m.loading {
		m.loading = true
		return tea.Batch(cmd, spinnerTick())
	}
	if !now && m.loading {
		m.loading = false
	}
	return cmd
}

const headerHeight = 6

func (m model) contentView() string {
	switch {
	case m.err != nil:
		return errorView(m.err.Error(), m.width)
	case m.loading:
		return loadingView(m.width, m.contentHeight(), m.spinnerPhase)
	default:
		return m.activeView.View()
	}
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString(renderHeader(&m))
	b.WriteString("\n")
	content := m.contentView()
	if toast := m.toastView(); toast != "" && m.width > 0 {
		content = overlayTopRight(content, toast, m.width)
	}
	b.WriteString(content)
	helpView := m.help.view()
	if helpView != "" {
		contentLines := strings.Count(b.String(), "\n")
		helpH := strings.Count(helpView, "\n") + 1
		footerH := 1 + helpH
		pad := m.height - contentLines - footerH - 1
		for range max(pad, 0) {
			b.WriteString("\n")
		}
		b.WriteString(renderRule(m.width, ""))
		b.WriteString("\n" + helpView)
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func overlayTopRight(base, layer string, width int) string {
	x := max(width-lipgloss.Width(layer)-1, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base).Z(0),
		lipgloss.NewLayer(layer).X(x).Y(0).Z(1),
	).Render()
}

func (m *model) updateHelpBindings() {
	quitHint := helpBinding{"ctrl+c ctrl+c", "quit"}
	if m.ctrlCOnce {
		quitHint = helpBinding{"ctrl+c", "press again to quit"}
	}
	var bindings []helpBinding
	if ic, ok := m.activeView.(inputCapturer); ok && ic.CapturingInput() {
		bindings = append(m.activeView.HelpBindings(), quitHint)
	} else if m.activeView.InThread() {
		bindings = append([]helpBinding{{"↑↓", "scroll"}, {"esc/q", "back"}}, m.activeView.HelpBindings()...)
		bindings = append(bindings, quitHint)
	} else {
		switch m.focus {
		case rowSection:
			bindings = []helpBinding{{"←→", "section"}, {"tab", "next row"}, {"shift+M/O/C", "jump"}, quitHint}
		case rowSubnav:
			bindings = []helpBinding{{"←→", "switch"}, {"tab", "next row"}, {"shift+tab", "prev row"}, quitHint}
		case rowContent:
			bindings = append([]helpBinding{
				{"↑↓", "navigate"},
				{"enter", "open"},
				{"tab", "next row"},
				{"shift+tab", "prev row"},
				quitHint,
			}, m.activeView.HelpBindings()...)
		}
	}
	if !m.help.hidden {
		bindings = append(bindings, helpBinding{"?", "toggle help"})
	}
	m.help.setBindings(bindings)
	h := m.contentHeight()
	m.activeView.Resize(m.width, h)
}

func (m model) contentHeight() int {
	footer := 0
	if h := m.help.height(); h > 0 {
		footer = h + 3
	}
	return max(m.height-headerHeight-footer, 1)
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.help.notice != "" {
		m.help.setNotice("")
		m.updateHelpBindings()
	}
	if key == "ctrl+c" {
		if m.ctrlCOnce {
			return m, tea.Quit
		}
		m.ctrlCOnce = true
		m.updateHelpBindings()
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ctrlCResetMsg{} })
	}
	if m.ctrlCOnce {
		m.ctrlCOnce = false
		m.updateHelpBindings()
	}
	if key == "?" {
		if ic, ok := m.activeView.(inputCapturer); ok && ic.CapturingInput() {
			cmd := m.activeView.HandleContentKey(msg)
			return m, m.syncLoading(cmd)
		}
		m.help.setHidden(!m.help.hidden)
		m.updateHelpBindings()
		return m, nil
	}
	if ic, ok := m.activeView.(inputCapturer); ok && ic.CapturingInput() {
		cmd := m.activeView.HandleContentKey(msg)
		cmd = m.syncLoading(cmd)
		m.updateHelpBindings()
		return m, cmd
	}
	if key == "ctrl+s" && m.section == sectionMail && m.activeView == m.mailView && !m.mailView.InThread() {
		return m.switchToScreener()
	}
	if msg.Key().Code == tea.KeyEscape || key == "q" {
		if m.activeView == m.screenerView {
			return m.closeScreener()
		}
		if canceler, ok := m.activeView.(pendingDetailCanceler); ok && canceler.CancelPendingDetail() {
			m.updateHelpBindings()
			return m, nil
		}
		if m.activeView.InThread() {
			m.activeView.ExitThread()
			m.updateHelpBindings()
			m.activeView.Resize(m.width, m.contentHeight())
			return m, m.syncLoading(nil)
		}
		return m, nil
	}
	if m.loading {
		return m, nil
	}
	if msg.Key().Code == tea.KeyTab {
		if msg.Key().Mod == tea.ModShift {
			m.focus = (m.focus + 2) % 3
		} else {
			m.focus = (m.focus + 1) % 3
		}
		m.updateHelpBindings()
		return m, nil
	}
	if sec := sectionForShortcut(key); sec >= 0 {
		return m.switchSection(sec)
	}
	if m.section == sectionMail && m.activeView == m.mailView {
		if cmd := m.mailView.handleBoxShortcut(key); cmd != nil {
			m.updateHelpBindings()
			return m, m.syncLoading(cmd)
		}
	}
	switch m.focus {
	case rowSection:
		return m.handleSectionKeys(msg)
	case rowSubnav:
		cmd := m.handleSubnavKey(msg)
		m.updateHelpBindings()
		return m, m.syncLoading(cmd)
	case rowContent:
		cmd := m.activeView.HandleContentKey(msg)
		cmd = m.syncLoading(cmd)
		m.updateHelpBindings()
		return m, cmd
	}
	return m, nil
}

func (m model) handleSectionKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Key().Code {
	case tea.KeyLeft:
		if m.section > 0 {
			return m.switchSection(m.section - 1)
		}
	case tea.KeyRight:
		if m.section < sectionCalendar {
			return m.switchSection(m.section + 1)
		}
	}
	return m, nil
}

func (m model) handleSubnavKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.Key().Code {
	case tea.KeyLeft:
		return m.activeView.SubnavLeft()
	case tea.KeyRight:
		return m.activeView.SubnavRight()
	}
	return nil
}

func (m model) switchSection(sec section) (tea.Model, tea.Cmd) {
	if sec == m.section {
		return m, nil
	}
	m.section = sec
	switch sec {
	case sectionMail:
		m.activeView = m.mailView
	case sectionContacts:
		m.activeView = m.contactsView
	case sectionCalendar:
		m.activeView = m.calendarView
	}
	m.activeView.Resize(m.width, m.contentHeight())
	cmd := m.syncLoading(m.activeView.Init())
	m.updateHelpBindings()
	return m, cmd
}

func (m model) switchToScreener() (tea.Model, tea.Cmd) {
	m.activeView = m.screenerView
	m.activeView.Resize(m.width, m.contentHeight())
	cmd := m.syncLoading(m.activeView.Init())
	m.updateHelpBindings()
	return m, cmd
}

func (m model) closeScreener() (tea.Model, tea.Cmd) {
	m.activeView = m.mailView
	m.activeView.Resize(m.width, m.contentHeight())
	cmd := m.syncLoading(m.mailView.refreshScreenerCount())
	m.updateHelpBindings()
	return m, cmd
}

// Run starts the interactive TUI. Refuses a non-TTY.
func Run(opts Options) error {
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("mailbox tui needs a terminal")
	}
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	p := tea.NewProgram(newModel(s, opts))
	_, err = p.Run()
	return err
}
