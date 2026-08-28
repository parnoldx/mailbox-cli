package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/folders"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/terminal"
)

type listLoadedMsg struct {
	requestID uint64
	folder    string
	page      int
	listing   *mail.Listing
	err       error
}

type threadLoadedMsg struct {
	requestID uint64
	folder    string
	uid       string
	walk      *mail.ThreadWalk
	err       error
}

type searchLoadedMsg struct {
	requestID uint64
	query     string
	listing   *mail.Listing
	err       error
}

type actionDoneMsg struct {
	action string
	err    error
}

type screenerCountMsg struct {
	count int
	err   error
}

type composeSentMsg struct {
	label string
	err   error
}

type labelsLoadedMsg struct {
	names []string
	err   error
}

type labelDoneMsg struct {
	folder, uid, name string
	on                bool
	err               error
}

type labelViewLoadedMsg struct {
	requestID uint64
	name      string
	page      int
	listing   *mail.Listing
	err       error
}

type composeReadyMsg struct {
	form *composeForm
	err  error
}

type mailView struct {
	s      *session
	styles styles

	boxIndex int
	items    []*mail.Envelope
	cursor   int
	scroll   int
	page     int
	nextPage string
	width    int
	height   int

	inThread     bool
	threadLines  []string
	threadOff    int
	threadID     string
	threadFolder string
	threadUID    string

	searchActive bool
	searchQuery  string
	searchInput  bool
	searchBuf    string

	compose    *composeForm
	movePrompt bool
	moveCursor int

	labelPrompt     bool
	labelNames      []string
	labelCursor     int
	labelInput      bool
	labelBuf        string
	labelCache      []string
	labelCacheOK    bool
	labelFilter     string
	labelBrowse     bool
	labelFromBrowse bool

	screenerCount int
	notice        string
	requestID     uint64
	loading       bool
}

func newMailView(s *session, st styles) *mailView {
	return &mailView{s: s, styles: st, page: 1}
}

func (v *mailView) currentFolder() string { return knownBoxes[v.boxIndex].imap }

func (v *mailView) Init() tea.Cmd {
	return tea.Batch(v.loadBox(), v.refreshScreenerCount())
}

func (v *mailView) loadBox() tea.Cmd {
	v.inThread = false
	v.searchActive = false
	v.labelFilter = ""
	v.labelBrowse = false
	v.labelFromBrowse = false
	v.page = 1
	if v.s == nil {
		return nil
	}
	v.requestID++
	v.loading = true
	id, folder, page := v.requestID, v.currentFolder(), 1
	return func() tea.Msg {
		listing, err := v.s.list(folder, page)
		return listLoadedMsg{requestID: id, folder: folder, page: page, listing: listing, err: err}
	}
}

func (v *mailView) refreshScreenerCount() tea.Cmd {
	return func() tea.Msg {
		n, err := v.s.count(folders.SCREENER)
		return screenerCountMsg{count: n, err: err}
	}
}

func (v *mailView) handleBoxShortcut(key string) tea.Cmd {
	if key == "L" {
		return v.openLabelBrowse()
	}
	if idx := boxForShortcut(key); idx >= 0 {
		v.boxIndex = idx
		return v.loadBox()
	}
	return nil
}

func (v *mailView) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case listLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load mail", msg.err)
			return nil, true
		}
		if msg.page <= 1 {
			v.items = msg.listing.Items
			v.cursor, v.scroll = 0, 0
		} else {
			v.items = append(v.items, msg.listing.Items...)
		}
		v.nextPage = msg.listing.NextPage
		v.page = msg.page
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
		v.threadFolder, v.threadUID = msg.folder, msg.uid
		v.threadID = msg.folder + ":" + msg.uid
		v.threadLines = renderThread(msg.walk, v.width)
		v.threadOff = 0
		return func() tea.Msg {
			_ = v.s.setSeen(msg.folder, msg.uid, true)
			return nil
		}, true
	case searchLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Search failed", msg.err)
			return nil, true
		}
		v.searchActive = true
		v.labelFilter = ""
		v.items = msg.listing.Items
		v.cursor, v.scroll = 0, 0
		v.nextPage = msg.listing.NextPage
		return nil, true
	case actionDoneMsg:
		if msg.err != nil {
			return notifyError(msg.action, msg.err), true
		}
		if v.inThread {
			v.inThread = false
		}
		if name := v.labelFilter; name != "" {
			return tea.Batch(notify(msg.action), v.viewLabel(name)), true
		}
		return tea.Batch(notify(msg.action), v.loadBox()), true
	case screenerCountMsg:
		if msg.err == nil {
			v.screenerCount = msg.count
		}
		return nil, true
	case composeSentMsg:
		v.compose = nil
		if msg.err != nil {
			return notifyError(msg.label, msg.err), true
		}
		return notify(msg.label), true
	case composeReadyMsg:
		v.loading = false
		if msg.err != nil {
			return notifyError("compose", msg.err), true
		}
		v.compose = msg.form
		return nil, true
	case labelsLoadedMsg:
		if !v.labelPrompt && !v.labelBrowse {
			return nil, true
		}
		if msg.err != nil {
			return notifyError("labels", msg.err), true
		}
		v.labelCache = msg.names
		v.labelCacheOK = true
		if v.labelBrowse {
			v.labelNames = msg.names
			return nil, true
		}
		current := []string{}
		if e := v.targetEnv(); e != nil {
			current = mail.LabelsFromFlags(e.Flags)
		}
		v.labelNames = mergeLabels(msg.names, current)
		if len(v.labelNames) == 0 {
			v.labelInput = true
		}
		return nil, true
	case labelViewLoadedMsg:
		if msg.requestID != v.requestID {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			v.notice = errorNotice("Could not load label", msg.err)
			return nil, true
		}
		v.labelFilter = msg.name
		v.searchActive = false
		if msg.page <= 1 {
			v.items = msg.listing.Items
			v.cursor, v.scroll = 0, 0
		} else {
			v.items = append(v.items, msg.listing.Items...)
		}
		v.nextPage = msg.listing.NextPage
		v.page = msg.page
		return nil, true
	case labelDoneMsg:
		if msg.err != nil {
			return notifyError("label", msg.err), true
		}
		if e := v.envBy(msg.folder, msg.uid); e != nil {
			e.Flags = setFlag(e.Flags, msg.name, msg.on)
		}
		if msg.on {
			v.labelNames = mergeLabels(v.labelNames, []string{msg.name})
			v.labelCache = mergeLabels(v.labelCache, []string{msg.name})
			v.labelCacheOK = true
			return notify("Labeled " + msg.name), true
		}
		return notify("Unlabeled " + msg.name), true
	}
	return nil, false
}

func (v *mailView) View() string {
	if v.compose != nil {
		return v.compose.view(v.width, v.height)
	}
	if v.labelBrowse {
		content := v.labelsView()
		if v.labelInput {
			body := styleMuted.Render("New label: ") + v.labelBuf + "█"
			content = overlayModal(content, modalFrame("New label", body, v.width), v.width, v.height)
		}
		return content
	}
	var content string
	if v.inThread {
		content = v.threadView()
	} else {
		content = v.listView()
	}
	if v.searchInput {
		body := styleMuted.Render("Search: ") + v.searchBuf + "█"
		content = overlayModal(content, modalFrame("Search email", body, v.width), v.width, v.height)
	}
	if v.movePrompt {
		content = overlayModal(content, v.moveFrame(), v.width, v.height)
	}
	if v.labelPrompt {
		content = overlayModal(content, v.labelFrame(), v.width, v.height)
	}
	return content
}

var moveDests = []struct{ key, label string }{
	{"i", "Inbox"},
	{"d", "The Feed"},
	{"p", "Paper Trail"},
	{"a", "Aside"},
	{"t", "Trash"},
	{"!", "Junk"},
	{"n", "Block"},
}

func (v *mailView) moveFrame() string {
	var b strings.Builder
	for i, d := range moveDests {
		prefix := "  "
		label := d.label
		if i == v.moveCursor {
			prefix = "› "
			label = lipgloss.NewStyle().Foreground(colorActive).Bold(true).Render(label)
		}
		b.WriteString(prefix + label + "\n")
	}
	return modalFrame("Move thread", strings.TrimRight(b.String(), "\n"), v.width)
}

func (v *mailView) listHeader() string {
	var lines []string
	if v.notice != "" {
		lines = append(lines, v.styles.title.Render(v.notice))
	}
	if v.screenerCount > 0 {
		noun := "first-time senders"
		if v.screenerCount == 1 {
			noun = "first-time sender"
		}
		hint := fmt.Sprintf("Screen %d %s · ctrl+s", v.screenerCount, noun)
		lines = append(lines, centerText(v.styles.pill.Render(hint), v.width), "")
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (v *mailView) listView() string {
	header := v.listHeader()
	headerH := 0
	if header != "" {
		headerH = strings.Count(header, "\n")
	}
	listH := max(v.height-headerH, 1)
	if len(v.items) == 0 {
		return header + styleMuted.Render("  (empty)")
	}
	unseen, seen := splitSeen(v.items)
	ordered := append(unseen, seen...)
	v.ensureCursor(len(ordered), listH)
	var b strings.Builder
	b.WriteString(header)
	cursorMarker, cursorText := cursorStyles()
	subjectBase := lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	dateBase := lipgloss.NewStyle().Foreground(colorBright)
	senderBase := lipgloss.NewStyle().Foreground(colorLink).Bold(true)
	excerptBase := styleMuted
	unseenDot := lipgloss.NewStyle().Foreground(colorAlert).Bold(true)
	dateCol := 12
	prefixWidth := 4
	textWidth := max(v.width-prefixWidth-2-dateCol, 10)

	renderRow := func(i int, e *mail.Envelope, sectionStart bool, label string) {
		if sectionStart {
			fmt.Fprintln(&b, sectionHeader(label, v.width))
		}
		isCursor := i == v.cursor
		emphasize := func(base lipgloss.Style) lipgloss.Style {
			if isCursor {
				return cursorText
			}
			return base
		}
		var line1 strings.Builder
		if isCursor {
			line1.WriteString(cursorMarker.Render("│") + " ")
		} else {
			line1.WriteString("  ")
		}
		if !seenFlag(e.Flags) {
			line1.WriteString(unseenDot.Render("●") + " ")
		} else {
			line1.WriteString("  ")
		}
		subject := terminal.SanitizeLine(e.Subject)
		if subject == "" {
			subject = e.Summary()
		}
		subject = truncateToWidth(subject, textWidth)
		date := formatDisplayDate(e.Date)
		gap := max(textWidth-lipgloss.Width(subject)+2+dateCol-lipgloss.Width(date), 1)
		line1.WriteString(emphasize(subjectBase).Render(subject))
		line1.WriteString(strings.Repeat(" ", gap))
		line1.WriteString(emphasize(dateBase).Render(date))

		var line2 strings.Builder
		if isCursor {
			line2.WriteString(cursorMarker.Render("│") + "     ")
		} else {
			line2.WriteString("      ")
		}
		sender := terminal.SanitizeLine(e.FromShort())
		excerpt := ""
		if e.Preview != "" && e.Preview != e.Subject {
			excerpt = " — " + terminal.SanitizeLine(e.Preview)
		}
		if labs := mail.LabelsFromFlags(e.Flags); len(labs) > 0 {
			excerpt += " · " + strings.Join(labs, ", ")
		}
		detailWidth := max(textWidth-2, 1)
		if lipgloss.Width(sender) > detailWidth {
			sender = truncateToWidth(sender, detailWidth)
			excerpt = ""
		} else {
			excerpt = truncateToWidth(excerpt, detailWidth-lipgloss.Width(sender))
		}
		line2.WriteString(emphasize(senderBase).Render(sender))
		line2.WriteString(emphasize(excerptBase).Render(excerpt))
		fmt.Fprintln(&b, line1.String())
		fmt.Fprintln(&b, line2.String())
	}

	end := min(v.scroll+max(listH/2, 1), len(ordered))
	for i := v.scroll; i < end; i++ {
		e := ordered[i]
		label := ""
		start := false
		if i == 0 || seenFlag(ordered[i-1].Flags) != seenFlag(e.Flags) {
			start = true
			if seenFlag(e.Flags) {
				label = "Previously Seen"
			} else {
				label = "New for You"
			}
		}
		renderRow(i, e, start, label)
	}
	return b.String()
}

func splitSeen(items []*mail.Envelope) (unseen, seen []*mail.Envelope) {
	for _, e := range items {
		if seenFlag(e.Flags) {
			seen = append(seen, e)
		} else {
			unseen = append(unseen, e)
		}
	}
	return
}

func (v *mailView) ordered() []*mail.Envelope {
	u, s := splitSeen(v.items)
	return append(u, s...)
}

func (v *mailView) ensureCursor(n, listH int) {
	if n == 0 {
		v.cursor, v.scroll = 0, 0
		return
	}
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	visible := max(listH/2, 1)
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	}
	if v.cursor >= v.scroll+visible {
		v.scroll = v.cursor - visible + 1
	}
}

func (v *mailView) threadView() string {
	head := ""
	if e := v.targetEnv(); e != nil {
		if labs := mail.LabelsFromFlags(e.Flags); len(labs) > 0 {
			head = styleMuted.Render("labels: "+strings.Join(labs, ", ")) + "\n"
		}
	}
	if len(v.threadLines) == 0 {
		return head + styleMuted.Render("  (empty thread)")
	}
	end := min(v.threadOff+v.height, len(v.threadLines))
	return head + strings.Join(v.threadLines[v.threadOff:end], "\n")
}

func renderThread(walk *mail.ThreadWalk, width int) []string {
	var lines []string
	for i, msg := range walk.Messages {
		if i > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorChrome).Render(strings.Repeat("─", max(width, 1))))
		}
		from := terminal.SanitizeLine(mail.ShortFrom(msg.From))
		date := formatDisplayDate(mail.FmtDate(msg.Date))
		lines = append(lines, lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render(from)+"  "+styleMuted.Render(date))
		if msg.Subject != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(terminal.SanitizeLine(msg.Subject)))
		}
		lines = append(lines, "")
		body := strings.ReplaceAll(msg.Body, "\r\n", "\n")
		for _, para := range strings.Split(body, "\n") {
			para = terminal.SanitizeLine(para)
			if para == "" {
				lines = append(lines, "")
				continue
			}
			for _, wrapped := range wrapText(para, max(width, 20)) {
				lines = append(lines, wrapped)
			}
		}
		if len(msg.Attachments) > 0 {
			var names []string
			for _, a := range msg.Attachments {
				names = append(names, a.Name)
			}
			lines = append(lines, styleMuted.Render("attachments: "+strings.Join(names, ", ")))
		}
		lines = append(lines, "")
	}
	if walk.Notice != "" {
		lines = append(lines, styleMuted.Render(walk.Notice))
	}
	return lines
}

func (v *mailView) HelpBindings() []helpBinding {
	if v.compose != nil {
		return []helpBinding{{"tab", "field"}, {"ctrl+s", "send"}, {"ctrl+d", "draft"}, {"esc", "cancel"}}
	}
	if v.searchInput {
		return []helpBinding{{"enter", "search"}, {"esc", "cancel"}}
	}
	if v.movePrompt {
		return []helpBinding{{"↑↓", "select"}, {"enter", "move"}, {"esc", "cancel"}}
	}
	if v.labelPrompt && v.labelInput {
		return []helpBinding{{"enter", "add"}, {"esc", "cancel"}}
	}
	if v.labelPrompt {
		return []helpBinding{{"↑↓", "select"}, {"enter", "view"}, {"space", "toggle"}, {"n", "new"}, {"esc", "cancel"}}
	}
	if v.labelBrowse && v.labelInput {
		return []helpBinding{{"enter", "create"}, {"esc", "cancel"}}
	}
	if v.labelBrowse {
		return []helpBinding{{"enter", "view"}, {"n", "new"}, {"esc", "back"}}
	}
	if v.inThread {
		return []helpBinding{{"r", "reply"}, {"f", "forward"}, {"l", "label"}, {"t", "trash"}}
	}
	if v.labelFilter != "" {
		return []helpBinding{{"enter", "open"}, {"l", "label"}, {"esc", "back"}}
	}
	if v.searchActive {
		return []helpBinding{{"enter", "open"}, {"/", "new search"}, {"esc", "clear"}}
	}
	return modifiersLast([]helpBinding{
		{"/", "search"},
		{"ctrl+s", "screener"},
		{"c", "compose"},
		{"r", "reply"},
		{"f", "forward"},
		{"v", "move"},
		{"l", "label"},
		{"L", "labels"},
		{"e", "seen"},
		{"u", "unseen"},
		{"i", "inbox"},
		{"a", "aside"},
		{"d", "feed"},
		{"p", "paper trail"},
		{"t", "trash"},
		{"!", "spam"},
		{"ctrl+r", "reload"},
	})
}

func (v *mailView) SubnavItems() ([]navItem, int, string, bool) {
	if v.searchActive || v.searchInput {
		label := "Search"
		if v.searchQuery != "" {
			label = "Search: " + v.searchQuery
		}
		return nil, 0, label, true
	}
	items := mailNavItems()
	if v.labelBrowse || v.labelFilter != "" {
		label := "Labels"
		if v.labelFilter != "" {
			label = "Label: " + v.labelFilter
		}
		return items, len(knownBoxes), label, true
	}
	label := "Mail"
	if v.boxIndex >= 0 && v.boxIndex < len(knownBoxes) {
		label = knownBoxes[v.boxIndex].name
	}
	return items, v.boxIndex, label, true
}

func (v *mailView) navIndex() int {
	if v.labelBrowse || v.labelFilter != "" {
		return len(knownBoxes)
	}
	return v.boxIndex
}

func (v *mailView) SubnavLeft() tea.Cmd {
	i := v.navIndex()
	if i <= 0 {
		return nil
	}
	v.boxIndex = i - 1
	return v.loadBox()
}

func (v *mailView) SubnavRight() tea.Cmd {
	i := v.navIndex()
	if i >= len(knownBoxes) {
		return nil
	}
	if i == len(knownBoxes)-1 {
		return v.openLabelBrowse()
	}
	v.boxIndex = i + 1
	return v.loadBox()
}

func (v *mailView) HandleContentKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.compose != nil {
		return v.handleComposeKey(msg)
	}
	if v.searchInput {
		return v.handleSearchKey(msg)
	}
	if v.movePrompt {
		return v.handleMoveKey(msg)
	}
	if v.labelPrompt {
		return v.handleLabelKey(msg)
	}
	if v.labelBrowse {
		return v.handleLabelBrowseKey(msg)
	}
	key := msg.String()
	if v.inThread {
		switch msg.Key().Code {
		case tea.KeyUp:
			if v.threadOff > 0 {
				v.threadOff--
			}
		case tea.KeyDown:
			if v.threadOff+v.height < len(v.threadLines) {
				v.threadOff++
			}
		}
		switch key {
		case "k":
			if v.threadOff > 0 {
				v.threadOff--
			}
		case "j":
			if v.threadOff+v.height < len(v.threadLines) {
				v.threadOff++
			}
		case "r":
			return v.openCompose(composeReply)
		case "f":
			return v.openCompose(composeForward)
		case "l":
			return v.openLabels()
		case "t":
			return v.moveSelected(folders.TRASH, "Trashed")
		}
		return nil
	}
	items := v.ordered()
	switch msg.Key().Code {
	case tea.KeyUp:
		if v.cursor > 0 {
			v.cursor--
		}
		return nil
	case tea.KeyDown:
		if v.cursor < len(items)-1 {
			v.cursor++
		}
		return v.maybeMore()
	case tea.KeyEnter:
		return v.openSelected()
	case tea.KeyEscape:
		if v.searchActive {
			v.searchActive = false
			v.searchQuery = ""
			return v.loadBox()
		}
		if v.labelFilter != "" {
			return v.leaveLabelMail()
		}
	}
	if cmd := v.handleBoxShortcut(key); cmd != nil {
		return cmd
	}
	switch key {
	case "k":
		if v.cursor > 0 {
			v.cursor--
		}
	case "j":
		if v.cursor < len(items)-1 {
			v.cursor++
		}
		return v.maybeMore()
	case "q":
		if v.labelFilter != "" {
			return v.leaveLabelMail()
		}
	case "/":
		v.searchInput = true
		v.searchBuf = ""
	case "c":
		return v.openCompose(composeNew)
	case "r":
		return v.openCompose(composeReply)
	case "f":
		return v.openCompose(composeForward)
	case "v":
		v.movePrompt = true
		v.moveCursor = 0
	case "l":
		return v.openLabels()
	case "e":
		return v.actSelected("seen", func(e *mail.Envelope) error { return v.s.setSeen(e.Folder, e.UID, true) })
	case "u":
		return v.actSelected("unseen", func(e *mail.Envelope) error { return v.s.setSeen(e.Folder, e.UID, false) })
	case "i":
		return v.moveSelected(folders.INBOX, "Moved to Inbox")
	case "d":
		return v.moveSelected(folders.FEED, "Moved to The Feed")
	case "p":
		return v.moveSelected(folders.PAPER_TRAIL, "Moved to Paper Trail")
	case "a":
		return v.actSelected("Set aside", func(e *mail.Envelope) error { return v.s.aside(e.Folder, e.UID) })
	case "t":
		return v.moveSelected(folders.TRASH, "Trashed")
	case "!":
		return v.moveSelected(folders.JUNK, "Marked as spam")
	case "ctrl+r":
		if v.labelFilter != "" {
			return v.viewLabel(v.labelFilter)
		}
		return v.loadBox()
	}
	return nil
}

func (v *mailView) maybeMore() tea.Cmd {
	if v.nextPage == "" || v.loading {
		return nil
	}
	if len(v.items)-v.cursor > 5 {
		return nil
	}
	v.requestID++
	id := v.requestID
	if v.labelFilter != "" {
		name, page := v.labelFilter, v.page+1
		return func() tea.Msg {
			listing, err := v.s.labeled(name, page)
			return labelViewLoadedMsg{requestID: id, name: name, page: page, listing: listing, err: err}
		}
	}
	folder, page := v.currentFolder(), v.page+1
	return func() tea.Msg {
		listing, err := v.s.list(folder, page)
		return listLoadedMsg{requestID: id, folder: folder, page: page, listing: listing, err: err}
	}
}

func (v *mailView) selected() *mail.Envelope {
	items := v.ordered()
	if v.cursor < 0 || v.cursor >= len(items) {
		return nil
	}
	return items[v.cursor]
}

func (v *mailView) openSelected() tea.Cmd {
	e := v.selected()
	if e == nil {
		return nil
	}
	v.requestID++
	v.loading = true
	id, folder, uid := v.requestID, e.Folder, e.UID
	return func() tea.Msg {
		walk, err := v.s.thread(folder, uid)
		return threadLoadedMsg{requestID: id, folder: folder, uid: uid, walk: walk, err: err}
	}
}

func (v *mailView) actSelected(label string, fn func(*mail.Envelope) error) tea.Cmd {
	e := v.targetEnv()
	if e == nil || e.UID == "" {
		return nil
	}
	return func() tea.Msg {
		err := fn(e)
		return actionDoneMsg{action: label, err: err}
	}
}

func (v *mailView) moveSelected(dest, label string) tea.Cmd {
	return v.actSelected(label, func(e *mail.Envelope) error { return v.s.move(e.Folder, e.UID, dest) })
}

func (v *mailView) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.Key().Code {
	case tea.KeyEscape:
		v.searchInput = false
		v.searchBuf = ""
		return nil
	case tea.KeyEnter:
		q := strings.TrimSpace(v.searchBuf)
		v.searchInput = false
		if q == "" {
			return nil
		}
		v.searchQuery = q
		v.requestID++
		v.loading = true
		id := v.requestID
		return func() tea.Msg {
			listing, err := v.s.search(q, 1)
			return searchLoadedMsg{requestID: id, query: q, listing: listing, err: err}
		}
	case tea.KeyBackspace:
		if v.searchBuf != "" {
			v.searchBuf = v.searchBuf[:len(v.searchBuf)-1]
		}
		return nil
	}
	v.searchBuf += insertText(msg)
	return nil
}

func (v *mailView) handleMoveKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if msg.Key().Code == tea.KeyEscape || key == "q" {
		v.movePrompt = false
		return nil
	}
	switch msg.Key().Code {
	case tea.KeyUp:
		if v.moveCursor > 0 {
			v.moveCursor--
		}
		return nil
	case tea.KeyDown:
		if v.moveCursor < len(moveDests)-1 {
			v.moveCursor++
		}
		return nil
	case tea.KeyEnter:
		key = moveDests[v.moveCursor].key
	}
	if dest := destIMAP(key); dest != "" {
		v.movePrompt = false
		label := "Moved"
		switch dest {
		case folders.INBOX:
			label = "Moved to Inbox"
		case folders.FEED:
			label = "Moved to The Feed"
		case folders.PAPER_TRAIL:
			label = "Moved to Paper Trail"
		case folders.ASIDE:
			label = "Set aside"
		case folders.TRASH:
			label = "Trashed"
		case folders.JUNK:
			label = "Marked as spam"
		case folders.BLOCK:
			label = "Denied"
		}
		if dest == folders.ASIDE {
			return v.actSelected(label, func(e *mail.Envelope) error { return v.s.aside(e.Folder, e.UID) })
		}
		return v.moveSelected(dest, label)
	}
	return nil
}

func (v *mailView) InThread() bool { return v.inThread || v.compose != nil }
func (v *mailView) ExitThread() {
	v.inThread = false
	v.compose = nil
	v.movePrompt = false
	v.labelPrompt = false
}
func (v *mailView) Resize(w, h int) { v.width, v.height = w, h }
func (v *mailView) Loading() bool   { return v.loading }
func (v *mailView) CapturingInput() bool {
	return v.compose != nil || v.searchInput || v.movePrompt || v.labelPrompt || v.labelBrowse || v.labelFilter != ""
}

func (v *mailView) targetEnv() *mail.Envelope {
	if v.inThread {
		if e := v.envBy(v.threadFolder, v.threadUID); e != nil {
			return e
		}
		return &mail.Envelope{Folder: v.threadFolder, UID: v.threadUID}
	}
	return v.selected()
}

func (v *mailView) envBy(folder, uid string) *mail.Envelope {
	for _, e := range v.items {
		if e.Folder == folder && e.UID == uid {
			return e
		}
	}
	return nil
}

func (v *mailView) openLabels() tea.Cmd {
	current := []string{}
	if e := v.targetEnv(); e != nil && e.UID != "" {
		current = mail.LabelsFromFlags(e.Flags)
	}
	v.labelPrompt = true
	v.labelCursor = 0
	v.labelInput = false
	v.labelBuf = ""
	if v.labelCacheOK {
		v.labelNames = mergeLabels(v.labelCache, current)
		if len(v.labelNames) == 0 {
			v.labelInput = true
		}
		return nil
	}
	v.labelNames = current
	if len(v.labelNames) == 0 {
		v.labelInput = true
	}
	if v.s == nil {
		return nil
	}
	return func() tea.Msg {
		names, err := v.s.labels()
		return labelsLoadedMsg{names: names, err: err}
	}
}

func (v *mailView) openLabelBrowse() tea.Cmd {
	v.labelBrowse = true
	v.labelFromBrowse = false
	v.labelFilter = ""
	v.inThread = false
	v.searchActive = false
	v.searchQuery = ""
	v.labelPrompt = false
	v.labelInput = false
	v.labelBuf = ""
	v.labelCursor = 0
	if v.labelCacheOK {
		v.labelNames = append([]string{}, v.labelCache...)
		return nil
	}
	if names, ok := mail.CatalogLabels(); ok {
		v.labelCache = names
		v.labelCacheOK = true
		v.labelNames = names
		return nil
	}
	v.labelNames = nil
	if v.s == nil {
		return nil
	}
	return func() tea.Msg {
		names, err := v.s.labels()
		return labelsLoadedMsg{names: names, err: err}
	}
}

func (v *mailView) labelsView() string {
	header := v.listHeader()
	rows := append(append([]string{}, v.labelNames...), "+ new")
	if v.labelCursor >= len(rows) {
		v.labelCursor = len(rows) - 1
	}
	if v.labelCursor < 0 {
		v.labelCursor = 0
	}
	var b strings.Builder
	b.WriteString(header)
	cursorMarker, cursorText := cursorStyles()
	for i, name := range rows {
		prefix := "  "
		label := name
		if i == v.labelCursor {
			prefix = cursorMarker.Render("│") + " "
			label = cursorText.Render(label)
		}
		b.WriteString(prefix + label + "\n")
	}
	return b.String()
}

func (v *mailView) handleLabelBrowseKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.labelInput {
		switch msg.Key().Code {
		case tea.KeyEscape:
			v.labelInput = false
			v.labelBuf = ""
			return nil
		case tea.KeyEnter:
			name, err := mail.CreateLabel(v.labelBuf)
			if err != nil {
				v.notice = err.Error()
				return nil
			}
			v.labelInput = false
			v.labelBuf = ""
			v.labelNames = mergeLabels(v.labelNames, []string{name})
			v.labelCache = mergeLabels(v.labelCache, []string{name})
			v.labelCacheOK = true
			for i, n := range v.labelNames {
				if n == name {
					v.labelCursor = i
					break
				}
			}
			return nil
		case tea.KeyBackspace:
			if v.labelBuf != "" {
				v.labelBuf = v.labelBuf[:len(v.labelBuf)-1]
			}
			return nil
		}
		v.labelBuf += insertText(msg)
		return nil
	}
	key := msg.String()
	if msg.Key().Code == tea.KeyEscape || key == "q" {
		return v.loadBox()
	}
	if cmd := v.handleBoxShortcut(key); cmd != nil {
		return cmd
	}
	n := len(v.labelNames)
	switch msg.Key().Code {
	case tea.KeyUp:
		if v.labelCursor > 0 {
			v.labelCursor--
		}
		return nil
	case tea.KeyDown:
		if v.labelCursor < n {
			v.labelCursor++
		}
		return nil
	case tea.KeyEnter:
		if v.labelCursor >= len(v.labelNames) {
			v.labelInput = true
			v.labelBuf = ""
			return nil
		}
		return v.viewLabel(v.labelNames[v.labelCursor])
	}
	switch key {
	case "k":
		if v.labelCursor > 0 {
			v.labelCursor--
		}
	case "j":
		if v.labelCursor < n {
			v.labelCursor++
		}
	case "n":
		v.labelInput = true
		v.labelBuf = ""
	}
	return nil
}

func (v *mailView) labelFrame() string {
	if v.labelInput {
		body := styleMuted.Render("New label: ") + v.labelBuf + "█"
		return modalFrame("Label", body, v.width)
	}
	has := map[string]bool{}
	if e := v.targetEnv(); e != nil {
		for _, name := range mail.LabelsFromFlags(e.Flags) {
			has[name] = true
		}
	}
	var b strings.Builder
	rows := append(append([]string{}, v.labelNames...), "+ new")
	for i, name := range rows {
		prefix := "  "
		label := name
		if i < len(v.labelNames) {
			mark := "[ ] "
			if has[name] {
				mark = "[x] "
			}
			label = mark + name
		}
		if i == v.labelCursor {
			prefix = "› "
			label = lipgloss.NewStyle().Foreground(colorActive).Bold(true).Render(label)
		}
		b.WriteString(prefix + label + "\n")
	}
	return modalFrame("Label", strings.TrimRight(b.String(), "\n"), v.width)
}

func (v *mailView) handleLabelKey(msg tea.KeyPressMsg) tea.Cmd {
	if v.labelInput {
		switch msg.Key().Code {
		case tea.KeyEscape:
			if len(v.labelNames) == 0 {
				v.labelPrompt = false
			}
			v.labelInput = false
			v.labelBuf = ""
			return nil
		case tea.KeyEnter:
			name, err := mail.NormalizeLabel(v.labelBuf)
			if err != nil {
				v.notice = err.Error()
				return nil
			}
			v.labelInput = false
			v.labelBuf = ""
			return v.toggleLabel(name)
		case tea.KeyBackspace:
			if v.labelBuf != "" {
				v.labelBuf = v.labelBuf[:len(v.labelBuf)-1]
			}
			return nil
		}
		v.labelBuf += insertText(msg)
		return nil
	}
	key := msg.String()
	if msg.Key().Code == tea.KeyEscape || key == "q" {
		v.labelPrompt = false
		return nil
	}
	n := len(v.labelNames) // + new row
	switch msg.Key().Code {
	case tea.KeyUp:
		if v.labelCursor > 0 {
			v.labelCursor--
		}
		return nil
	case tea.KeyDown:
		if v.labelCursor < n {
			v.labelCursor++
		}
		return nil
	case tea.KeyEnter:
		if v.labelCursor >= len(v.labelNames) {
			v.labelInput = true
			v.labelBuf = ""
			return nil
		}
		return v.viewLabel(v.labelNames[v.labelCursor])
	case tea.KeySpace:
		if v.labelCursor < len(v.labelNames) {
			return v.toggleLabel(v.labelNames[v.labelCursor])
		}
		return nil
	}
	switch key {
	case "k":
		if v.labelCursor > 0 {
			v.labelCursor--
		}
	case "j":
		if v.labelCursor < n {
			v.labelCursor++
		}
	case "n":
		v.labelInput = true
		v.labelBuf = ""
	}
	return nil
}

func (v *mailView) leaveLabelMail() tea.Cmd {
	if v.labelFromBrowse {
		return v.openLabelBrowse()
	}
	return v.loadBox()
}

func (v *mailView) viewLabel(name string) tea.Cmd {
	v.labelFromBrowse = v.labelBrowse
	v.labelBrowse = false
	v.labelPrompt = false
	v.labelInput = false
	v.inThread = false
	v.searchActive = false
	v.searchQuery = ""
	v.labelFilter = name
	v.page = 1
	if v.s == nil {
		return nil
	}
	v.requestID++
	v.loading = true
	id := v.requestID
	return func() tea.Msg {
		listing, err := v.s.labeled(name, 1)
		return labelViewLoadedMsg{requestID: id, name: name, page: 1, listing: listing, err: err}
	}
}

func (v *mailView) toggleLabel(name string) tea.Cmd {
	e := v.targetEnv()
	if e == nil || v.s == nil {
		return nil
	}
	on := true
	for _, l := range mail.LabelsFromFlags(e.Flags) {
		if l == name {
			on = false
			break
		}
	}
	folder, uid := e.Folder, e.UID
	return func() tea.Msg {
		err := v.s.setLabel(folder, uid, name, on)
		return labelDoneMsg{folder: folder, uid: uid, name: name, on: on, err: err}
	}
}

func mergeLabels(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, xs := range [][]string{a, b} {
		for _, x := range xs {
			if x == "" || seen[x] {
				continue
			}
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func setFlag(flags []string, name string, on bool) []string {
	name = strings.ToLower(name)
	var out []string
	for _, f := range flags {
		if strings.EqualFold(f, name) {
			continue
		}
		out = append(out, f)
	}
	if on {
		out = append(out, name)
	}
	return out
}

type composeMode int

const (
	composeNew composeMode = iota
	composeReply
	composeForward
)

type composeForm struct {
	mode    composeMode
	fields  []string // to, cc, subject, body
	labels  []string
	focus   int
	replyTo *[2]string
	status  string
}

func (v *mailView) openCompose(mode composeMode) tea.Cmd {
	f := &composeForm{
		mode:   mode,
		fields: []string{"", "", "", ""},
		labels: []string{"To", "Cc", "Subject", "Body"},
	}
	e := v.selected()
	if v.inThread && v.threadUID != "" {
		e = &mail.Envelope{Folder: v.threadFolder, UID: v.threadUID}
	}
	switch mode {
	case composeReply:
		if e == nil {
			return notify("Nothing to reply to")
		}
		f.replyTo = &[2]string{e.Folder, e.UID}
		folder, uid := e.Folder, e.UID
		return func() tea.Msg {
			msg, err := v.s.message(folder, uid)
			if err != nil {
				return composeReadyMsg{err: err}
			}
			f.fields[0] = msg.From
			subj := msg.Subject
			if !strings.HasPrefix(strings.ToLower(subj), "re:") {
				subj = "Re: " + subj
			}
			f.fields[2] = subj
			return composeReadyMsg{form: f}
		}
	case composeForward:
		if e == nil {
			return notify("Nothing to forward")
		}
		folder, uid := e.Folder, e.UID
		return func() tea.Msg {
			msg, err := v.s.message(folder, uid)
			if err != nil {
				return composeReadyMsg{err: err}
			}
			subj := msg.Subject
			if !strings.HasPrefix(strings.ToLower(subj), "fwd:") {
				subj = "Fwd: " + subj
			}
			f.fields[2] = subj
			f.fields[3] = fmt.Sprintf("----- Forwarded message -----\nFrom: %s\nDate: %s\nSubject: %s\n\n%s",
				msg.From, msg.Date, msg.Subject, msg.Body)
			return composeReadyMsg{form: f}
		}
	}
	v.compose = f
	return nil
}

func (v *mailView) handleComposeKey(msg tea.KeyPressMsg) tea.Cmd {
	f := v.compose
	key := msg.String()
	switch msg.Key().Code {
	case tea.KeyEscape:
		v.compose = nil
		return nil
	case tea.KeyTab:
		if msg.Key().Mod == tea.ModShift {
			f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
		} else {
			f.focus = (f.focus + 1) % len(f.fields)
		}
		return nil
	case tea.KeyBackspace:
		if f.fields[f.focus] != "" {
			r := []rune(f.fields[f.focus])
			f.fields[f.focus] = string(r[:len(r)-1])
		}
		return nil
	case tea.KeyEnter:
		if f.focus == 3 {
			f.fields[3] += "\n"
		} else {
			f.focus++
		}
		return nil
	}
	if key == "ctrl+s" {
		return v.sendCompose(false)
	}
	if key == "ctrl+d" {
		return v.sendCompose(true)
	}
	f.fields[f.focus] += insertText(msg)
	return nil
}

func (v *mailView) sendCompose(draft bool) tea.Cmd {
	f := v.compose
	to := splitAddrs(f.fields[0])
	cc := splitAddrs(f.fields[1])
	if !draft && len(to)+len(cc) == 0 {
		f.status = "Need a recipient"
		return nil
	}
	out := &mail.Outgoing{
		To: to, Cc: cc, Subject: f.fields[2], Body: f.fields[3], ReplyTo: f.replyTo,
	}
	label := "Sent"
	if draft {
		label = "Saved draft"
	}
	return func() tea.Msg {
		_, err := v.s.compose(out, draft)
		return composeSentMsg{label: label, err: err}
	}
}

func (f *composeForm) view(width, height int) string {
	title := "New message"
	switch f.mode {
	case composeReply:
		title = "Reply"
	case composeForward:
		title = "Forward"
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render(title))
	b.WriteString("\n\n")
	for i, label := range f.labels {
		marker := "  "
		style := lipgloss.NewStyle()
		if i == f.focus {
			marker = "│ "
			style = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
		}
		val := f.fields[i]
		if i == f.focus {
			val += "█"
		}
		if i == 3 {
			b.WriteString(marker + style.Render(label) + "\n")
			for _, line := range strings.Split(val, "\n") {
				b.WriteString("    " + line + "\n")
			}
		} else {
			b.WriteString(marker + style.Render(label+": ") + val + "\n")
		}
	}
	if f.status != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(colorError).Render(f.status))
	}
	_ = height
	_ = width
	return b.String()
}

func splitAddrs(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
