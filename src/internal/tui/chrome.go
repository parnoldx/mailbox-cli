package tui

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// --- nav ---

type section int

const (
	sectionMail section = iota
	sectionContacts
	sectionCalendar
)

type focusRow int

const (
	rowSection focusRow = iota
	rowSubnav
	rowContent
)

type navItem struct {
	shortcut string
	label    string
}

var sectionItems = []navItem{
	{"M", "Mail"},
	{"O", "Contacts"},
	{"C", "Calendar"},
}

func sectionForShortcut(key string) section {
	switch key {
	case "M":
		return sectionMail
	case "O":
		return sectionContacts
	case "C":
		return sectionCalendar
	}
	return -1
}

type boxSpec struct {
	name   string
	imap   string
	key    string
	action string // i/d/p/a shortcuts that also move mail
}

var knownBoxes = []boxSpec{
	{"Inbox", "INBOX", "1", "i"},
	{"The Feed", "INBOX/Feed", "2", "d"},
	{"Paper Trail", "INBOX/Paper Trail", "3", "p"},
	{"Aside", "INBOX/Aside", "4", "a"},
	{"Drafts", "Drafts", "5", ""},
	{"Sent", "Sent", "6", ""},
}

func boxNavItems() []navItem {
	items := make([]navItem, len(knownBoxes))
	for i, b := range knownBoxes {
		items[i] = navItem{shortcut: b.key, label: b.name}
	}
	return items
}

func mailNavItems() []navItem {
	return append(boxNavItems(), navItem{shortcut: "L", label: "Labels"})
}

func boxForShortcut(key string) int {
	for i, spec := range knownBoxes {
		if spec.key == key {
			return i
		}
	}
	return -1
}

func renderRule(width int, label string) string {
	if width <= 0 {
		return ""
	}
	rule := lipgloss.NewStyle().Foreground(colorChrome)
	if label == "" || width < 3 {
		return rule.Render(strings.Repeat("─", width))
	}
	label = truncateToWidth(label, width-2)
	padded := " " + label + " "
	padLen := lipgloss.Width(padded)
	ruleLen := max(width-padLen, 0)
	left := ruleLen / 2
	right := ruleLen - left
	return rule.Render(strings.Repeat("─", left) + padded + strings.Repeat("─", right))
}

func renderNavLabel(label, shortcut string, base lipgloss.Style) string {
	if shortcut == "" {
		return base.Render(label)
	}
	idx := strings.Index(strings.ToLower(label), strings.ToLower(shortcut))
	if idx < 0 {
		return base.Underline(true).Render(shortcut) + base.Render(" "+label)
	}
	end := idx + len(shortcut)
	out := base.Underline(true).Render(label[idx:end])
	if idx > 0 {
		out = base.Render(label[:idx]) + out
	}
	if end < len(label) {
		out += base.Render(label[end:])
	}
	return out
}

func renderNavRow(items []navItem, selected int, focused bool, width int, centered bool) string {
	const sep = "  "
	sepW := lipgloss.Width(sep)
	type rendered struct {
		str string
		w   int
	}
	all := make([]rendered, len(items))
	totalW := 0
	for i, item := range items {
		style := lipgloss.NewStyle().Foreground(colorChrome).Bold(true)
		if i == selected {
			if focused {
				style = style.Foreground(colorActive)
			} else {
				style = style.Foreground(colorPrimary)
			}
		}
		s := renderNavLabel(item.label, item.shortcut, style)
		w := lipgloss.Width(s)
		all[i] = rendered{s, w}
		totalW += w
	}
	totalW += sepW * max(len(items)-1, 0)
	if totalW <= width {
		parts := make([]string, len(all))
		for i, r := range all {
			parts[i] = r.str
		}
		row := strings.Join(parts, sep)
		if centered {
			return centerText(row, width)
		}
		return row
	}
	leftArrow := lipgloss.NewStyle().Foreground(colorChrome).Render("‹ ")
	rightArrow := lipgloss.NewStyle().Foreground(colorChrome).Render(" ›")
	arrowW := lipgloss.Width(leftArrow)
	lo, hi := selected, selected
	usedW := all[selected].w
	for {
		expandedLeft, expandedRight := false, false
		if lo > 0 {
			need := sepW + all[lo-1].w
			reserveR, reserveL := 0, 0
			if hi < len(items)-1 {
				reserveR = arrowW
			}
			if lo-1 > 0 {
				reserveL = arrowW
			}
			if usedW+need+reserveL+reserveR <= width {
				lo--
				usedW += need
				expandedLeft = true
			}
		}
		if hi < len(items)-1 {
			need := sepW + all[hi+1].w
			reserveL, reserveR := 0, 0
			if lo > 0 {
				reserveL = arrowW
			}
			if hi+1 < len(items)-1 {
				reserveR = arrowW
			}
			if usedW+need+reserveL+reserveR <= width {
				hi++
				usedW += need
				expandedRight = true
			}
		}
		if !expandedLeft && !expandedRight {
			break
		}
	}
	var b strings.Builder
	if lo > 0 {
		b.WriteString(leftArrow)
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			b.WriteString(sep)
		}
		b.WriteString(all[i].str)
	}
	if hi < len(items)-1 {
		b.WriteString(rightArrow)
	}
	row := b.String()
	if centered {
		return centerText(row, width)
	}
	return row
}

func centerText(text string, width int) string {
	pad := max((width-lipgloss.Width(text))/2, 0)
	return strings.Repeat(" ", pad) + text
}

func renderTopRule(width int, account string) string {
	ruleStyle := lipgloss.NewStyle().Foreground(colorChrome)
	labelStyle := lipgloss.NewStyle().Foreground(colorChrome).Bold(true)
	accountWidth := 0
	if account != "" {
		accountWidth = lipgloss.Width(account) + 2
	}
	const word = 9 // " mailbox "
	const tail = 2
	left := min((width-word)/2, width-word-accountWidth-tail-1)
	mid := width - left - word - accountWidth - tail
	if left < 1 || mid < 1 {
		return renderRule(width, strings.TrimSuffix("mailbox · "+account, " · "))
	}
	var b strings.Builder
	b.WriteString(ruleStyle.Render(strings.Repeat("─", left)))
	b.WriteString(" " + labelStyle.Render("mailbox") + " ")
	b.WriteString(ruleStyle.Render(strings.Repeat("─", mid)))
	if account != "" {
		b.WriteString(" " + labelStyle.Render(account) + " ")
	}
	b.WriteString(ruleStyle.Render(strings.Repeat("─", tail)))
	return b.String()
}

func renderHeader(m *model) string {
	var b strings.Builder
	b.WriteString(renderTopRule(m.width, m.account))
	b.WriteString("\n")
	b.WriteString(renderNavRow(sectionItems, int(m.section), m.focus == rowSection, m.width, true))
	b.WriteString("\n")
	row2Items, row2Selected, row2Label, centered := m.activeView.SubnavItems()
	b.WriteString(renderRule(m.width, row2Label))
	b.WriteString("\n")
	if len(row2Items) > 0 {
		b.WriteString(renderNavRow(row2Items, row2Selected, m.focus == rowSubnav, m.width, centered))
		b.WriteString("\n")
	}
	b.WriteString(renderRule(m.width, ""))
	return b.String()
}

// --- help ---

type helpBinding struct {
	key  string
	desc string
}

func modifiersLast(bindings []helpBinding) []helpBinding {
	plain := make([]helpBinding, 0, len(bindings))
	chorded := make([]helpBinding, 0, len(bindings))
	for _, binding := range bindings {
		if strings.Contains(binding.key, "+") {
			chorded = append(chorded, binding)
		} else {
			plain = append(plain, binding)
		}
	}
	return append(plain, chorded...)
}

type helpBar struct {
	width    int
	bindings []helpBinding
	styles   styles
	hidden   bool
	notice   string
}

func newHelpBar(s styles) helpBar { return helpBar{styles: s} }

func (h *helpBar) setWidth(w int)              { h.width = w }
func (h *helpBar) setBindings(b []helpBinding) { h.bindings = b }
func (h *helpBar) setHidden(hidden bool)       { h.hidden = hidden }
func (h *helpBar) setNotice(notice string)     { h.notice = notice }

func (h helpBar) height() int {
	v := h.view()
	if v == "" {
		return 0
	}
	return strings.Count(v, "\n") + 1
}

func (h helpBar) view() string {
	if h.notice != "" {
		notice := lipgloss.NewStyle().Foreground(colorError).Render(h.notice)
		if h.width > 0 {
			return ansi.Wrap(notice, h.width, "")
		}
		return notice
	}
	if h.hidden || len(h.bindings) == 0 {
		return ""
	}
	sep := h.styles.helpSep.Render(" • ")
	sepWidth := lipgloss.Width(sep)
	type item struct {
		str   string
		width int
	}
	var items []item
	for _, b := range h.bindings {
		rendered := h.styles.helpKey.Render(b.key) + " " + h.styles.helpDesc.Render(b.desc)
		items = append(items, item{str: rendered, width: lipgloss.Width(rendered)})
	}
	maxWidth := h.width
	var lines []string
	var line strings.Builder
	lineWidth := 0
	for _, it := range items {
		if lineWidth > 0 && maxWidth > 0 && lineWidth+sepWidth+it.width > maxWidth {
			lines = append(lines, line.String())
			line.Reset()
			lineWidth = 0
		}
		if lineWidth > 0 {
			line.WriteString(sep)
			lineWidth += sepWidth
		}
		line.WriteString(it.str)
		lineWidth += it.width
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// --- toast ---

const toastDuration = 2 * time.Second

type toastKind int

const (
	toastInfo toastKind = iota
	toastError
)

type notifyMsg struct {
	text string
	kind toastKind
}

type toastExpiredMsg struct{ id uint64 }

func notify(text string) tea.Cmd {
	return func() tea.Msg { return notifyMsg{text: text} }
}

func notifyError(what string, err error) tea.Cmd {
	return func() tea.Msg { return notifyMsg{text: errorNotice(what, err), kind: toastError} }
}

func (m *model) showToast(msg notifyMsg) tea.Cmd {
	m.toastID++
	m.toast = msg
	id := m.toastID
	return tea.Tick(toastDuration, func(time.Time) tea.Msg { return toastExpiredMsg{id: id} })
}

func (m model) toastView() string {
	if m.toast.text == "" {
		return ""
	}
	border := colorChrome
	text := lipgloss.NewStyle().Foreground(colorBright)
	if m.toast.kind == toastError {
		border = colorError
		text = lipgloss.NewStyle().Foreground(colorError)
	}
	body := truncateToWidth(m.toast.text, max(m.width/2-4, 10))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Render(text.Render(body))
}

// --- loading ---

const (
	hourglassVisualWidth = 11
	hourglassDrainFrames = 7
	hourglassFlipFrames  = 6
	hourglassTotalFrames = 13
	hourglassDrainPhase  = 9.0
	hourglassFlipPhase   = 3.6
	hourglassCyclePhase  = hourglassDrainPhase + hourglassFlipPhase
)

var hourglassFrames = [hourglassTotalFrames][]string{
	{"   ╭───╮   ", "   │⣿⣿⣿│   ", "   ╰╮⣿╭╯   ", "   ╭╯ ╰╮   ", "   │   │   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │⣿⣿⣿│   ", "   ╰╮ ╭╯   ", "   ╭╯⡇╰╮   ", "   │ ⣀ │   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │⣿ ⣿│   ", "   ╰╮ ╭╯   ", "   ╭╯⡇╰╮   ", "   │⣀⣤⣀│   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │⣀ ⣀│   ", "   ╰╮ ╭╯   ", "   ╭╯⡇╰╮   ", "   │⣤⣤⣤│   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │   │   ", "   ╰╮ ╭╯   ", "   ╭╯⡇╰╮   ", "   │⣿⣤⣿│   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │   │   ", "   ╰╮ ╭╯   ", "   ╭╯⡇╰╮   ", "   │⣿⣿⣿│   ", "   ╰───╯   "},
	{"   ╭───╮   ", "   │   │   ", "   ╰╮ ╭╯   ", "   ╭╯ ╰╮   ", "   │⣿⣿⣿│   ", "   ╰───╯   "},
	{"╭───╮      ", " │   │     ", "  ╰╮ ╭╯    ", "    ╭╯ ╰╮  ", "     │⣿⣿⣿│ ", "      ╰───╯"},
	{"╭─         ", "│ ──╮      ", "╰─   ⣠╭──  ", "  ──╯ ⣾⣿⣿─╮", "      ╰──⣿│", "         ─╯"},
	{"           ", "╭───╮ ╭───╮", "│    ⣠⣾⣿⣿⣿│", "╰───╯ ╰───╯", "           ", "           "},
	{"         ─╮", "      ╭──⣿│", "  ──╮ ⣾⣿⣿─╯", "╭─    ╰──  ", "│ ──╯      ", "╰─         "},
	{"      ╭───╮", "     │⣿⣿⣿│ ", "    ╰╮ ╭╯  ", "  ╭╯ ╰╮    ", " │   │     ", "╰───╯      "},
	{"   ╭───╮   ", "   │⣿⣿⣿│   ", "   ╰╮⣿╭╯   ", "   ╭╯ ╰╮   ", "   │   │   ", "   ╰───╯   "},
}

type spinnerTickMsg struct{}

func spinnerTick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

func hourglassFrameIndex(phase float64) int {
	p := math.Mod(phase, hourglassCyclePhase)
	if p < hourglassDrainPhase {
		idx := int(p * float64(hourglassDrainFrames) / hourglassDrainPhase)
		return min(idx, hourglassDrainFrames-1)
	}
	fp := p - hourglassDrainPhase
	idx := int(fp * float64(hourglassFlipFrames) / hourglassFlipPhase)
	return hourglassDrainFrames + min(idx, hourglassFlipFrames-1)
}

func loadingView(width, contentHeight int, phase float64) string {
	if width < hourglassVisualWidth || contentHeight < 2 {
		return "Loading..."
	}
	frame := hourglassFrames[hourglassFrameIndex(phase)]
	glass := lipgloss.NewStyle().Foreground(colorPrimary)
	padLeft := max((width-hourglassVisualWidth)/2, 0)
	prefix := strings.Repeat(" ", padLeft)
	var lines []string
	for _, line := range frame {
		lines = append(lines, prefix+glass.Render(line))
	}
	labelText := "Loading..."
	labelPad := max((width-len(labelText))/2, 0)
	lines = append(lines, strings.Repeat(" ", labelPad)+styleMuted.Render(labelText))
	topPad := max((contentHeight-len(lines))/2, 0)
	var b strings.Builder
	for range topPad {
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// --- width ---

func truncateToWidth(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 3 {
		return ""
	}
	var b strings.Builder
	remain := w - 3
	for _, r := range s {
		rw := 1
		if r > utf8.RuneSelf {
			rw = lipgloss.Width(string(r))
		}
		if rw > remain {
			break
		}
		b.WriteRune(r)
		remain -= rw
	}
	return b.String() + "..."
}

func sectionHeader(label string, width int) string {
	s := lipgloss.NewStyle().Foreground(colorChrome).Bold(true).Render(label)
	if fill := width - lipgloss.Width(label) - 3; fill > 0 {
		s += " " + lipgloss.NewStyle().Foreground(colorChrome).Render(strings.Repeat("─", fill))
	}
	return s
}

func hintedSectionHeader(label, hint string, width int) string {
	rule := lipgloss.NewStyle().Foreground(colorChrome)
	fill := width - lipgloss.Width(label) - lipgloss.Width(hint) - 4
	if fill < 1 {
		return sectionHeader(label, width)
	}
	return rule.Bold(true).Render(label) + " " +
		rule.Render(strings.Repeat("─", fill)) + " " +
		styleMuted.Render(hint)
}

func displayWidth(s string) int { return lipgloss.Width(s) }

func firstCluster(s string) (string, int) {
	cluster, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
	return cluster, lipgloss.Width(cluster)
}

func fitGraphemes(s string, width int) string {
	var b strings.Builder
	for s != "" {
		cluster, w := firstCluster(s)
		if cluster == "" || w > width {
			break
		}
		b.WriteString(cluster)
		width -= w
		s = s[len(cluster):]
	}
	return b.String()
}

const (
	modalChromeWidth = 6
	modalChromeRows  = 6
)

func modalContentWidth(width int) int { return max(width-modalChromeWidth, 1) }

func modalContentRows(height int) int { return max(height-modalChromeRows, 1) }

func modalListWindow(count, cursor, visible int) (start, end int) {
	if cursor >= visible {
		start = cursor - visible + 1
	}
	return start, min(start+visible, count)
}

func stepListCursor(cursor, count int, msg tea.KeyPressMsg) int {
	switch msg.Key().Code {
	case tea.KeyUp:
		if cursor > 0 {
			return cursor - 1
		}
	case tea.KeyDown:
		if cursor < count-1 {
			return cursor + 1
		}
	}
	return cursor
}

func overlayModal(base, modal string, width, height int) string {
	x := max((width-lipgloss.Width(modal))/2, 0)
	y := max((height-lipgloss.Height(modal))/2, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base).Z(0),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	).Render()
}

func modalFrame(title, body string, width int) string {
	heading := lipgloss.NewStyle().Foreground(colorChrome).Bold(true).Render(truncateToWidth(title, max(width-6, 10)))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorChrome).
		Padding(0, 2).
		Render(heading + "\n\n" + body)
}

func formatDisplayDate(raw string) string {
	if raw == "" {
		return ""
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", raw, time.Local); err == nil {
		return t.Format("Jan 2, 2006")
	}
	if len(raw) >= 10 {
		if t, err := time.Parse("2006-01-02", raw[:10]); err == nil {
			return t.Format("Jan 2, 2006")
		}
	}
	return raw
}

func seenFlag(flags []string) bool {
	for _, f := range flags {
		if strings.EqualFold(strings.Trim(f, `\`+" "), "Seen") {
			return true
		}
	}
	return false
}

func insertText(msg tea.KeyPressMsg) string {
	if t := msg.Key().Text; t != "" {
		return t
	}
	s := msg.String()
	if s == "" || strings.Contains(s, "+") || utf8.RuneCountInString(s) != 1 {
		return ""
	}
	return s
}
