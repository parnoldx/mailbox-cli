// Package htmlmd ports src/mailbox_cli/htmlmd.py: HTML→Markdown and a small
// Markdown→HTML converter.
package htmlmd

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"unicode"
	"unicode/utf8"

	"mailbox/internal/terminal"
	"mailbox/internal/trackers"
)

var inlineMeta = map[rune]bool{'\\': true, '`': true, '*': true, '_': true, '~': true, '[': true, ']': true, '<': true, '>': true, '|': true}
var lineStartMeta = map[rune]bool{'#': true, '>': true, '+': true, '-': true, '=': true}

var entityRefRe = regexp.MustCompile(`^&(#[0-9]{1,8}|#[xX][0-9a-fA-F]{1,8}|[A-Za-z][A-Za-z0-9]{1,31});`)
var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
var destEnd = map[rune]bool{'<': true, '>': true, '(': true, ')': true, '\\': true, '"': true, '|': true}
var joiners = map[rune]bool{'‌': true, '‍': true}
var fenceInfoRe = regexp.MustCompile(`^[A-Za-z0-9_+#.-]{1,32}$`)

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}
var autoClose = map[string][]string{
	"p": {"p"}, "li": {"li"},
	"td": {"td", "th"}, "th": {"td", "th"},
	"tr": {"tr", "td", "th"},
	"dt": {"dt", "dd"}, "dd": {"dt", "dd"},
}
var skipTags = map[string]bool{"script": true, "style": true, "head": true, "title": true, "noscript": true}
var blockInCell = map[string]bool{
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "table": true, "pre": true, "blockquote": true,
	"figure": true, "hr": true,
}

const maxNesting = 16

type node struct {
	tag      string
	attrs    map[string]string
	children []*node
	text     *string
}

func textNode(s string) *node { return &node{text: &s} }

// TreeBuilder mirrors python's custom HTMLParser tree (auto-close semantics).
type treeBuilder struct {
	root  *node
	stack []*node
}

func newTreeBuilder() *treeBuilder {
	b := &treeBuilder{root: &node{tag: "document", attrs: map[string]string{}}}
	b.stack = []*node{b.root}
	return b
}

func (b *treeBuilder) feed(value string) {
	z := html.NewTokenizer(strings.NewReader(value))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return
		case html.TextToken:
			b.handleData(string(z.Text()))
		case html.StartTagToken:
			name, attrs := readTag(z)
			b.handleStartTag(name, attrs, false)
		case html.SelfClosingTagToken:
			name, attrs := readTag(z)
			b.handleStartTag(name, attrs, false)
			b.handleEndTag(name)
		case html.EndTagToken:
			name, _ := z.TagName()
			b.handleEndTag(string(name))
		}
	}
}

func readTag(z *html.Tokenizer) (string, map[string]string) {
	name, moreAttr := z.TagName()
	tag := string(name)
	attrs := map[string]string{}
	for moreAttr {
		var key, val []byte
		key, val, moreAttr = z.TagAttr()
		k := string(key)
		if _, seen := attrs[k]; !seen || val != nil {
			attrs[k] = string(val)
		}
	}
	return tag, attrs
}

func (b *treeBuilder) handleStartTag(tag string, attrs map[string]string, selfClosing bool) {
	if closers, ok := autoClose[tag]; ok {
		for len(b.stack) > 1 {
			top := b.stack[len(b.stack)-1]
			matched := false
			for _, c := range closers {
				if top.tag == c {
					matched = true
					break
				}
			}
			if !matched {
				break
			}
			b.stack = b.stack[:len(b.stack)-1]
		}
	}
	n := &node{tag: tag, attrs: attrs}
	if n.attrs == nil {
		n.attrs = map[string]string{}
	}
	parent := b.stack[len(b.stack)-1]
	parent.children = append(parent.children, n)
	if !voidTags[tag] {
		b.stack = append(b.stack, n)
	}
}

func (b *treeBuilder) handleEndTag(tag string) {
	if voidTags[tag] {
		return
	}
	for i := len(b.stack) - 1; i > 0; i-- {
		if b.stack[i].tag == tag {
			b.stack = b.stack[:i]
			return
		}
	}
}

func (b *treeBuilder) handleData(data string) {
	parent := b.stack[len(b.stack)-1]
	parent.children = append(parent.children, textNode(data))
}

func parseHTML(value string) *node {
	b := newTreeBuilder()
	b.feed(value)
	return b.root
}

// --- public API ---

var (
	// Invisible chars used as email preheader spacers: U+034F combining
	// grapheme joiner, U+00AD soft hyphen, U+200B-D zero-width spaces,
	// U+FEFF BOM.
	reInvisible = regexp.MustCompile(`[\x{034F}\x{00AD}\x{200B}\x{200C}\x{200D}\x{FEFF}]+`)
	// Empty images/links left behind after tracking pixels are dropped
	// (plus one following space to avoid doubled gaps).
	reEmptyImage = regexp.MustCompile(`!\[\s*\]\([^)]*\) ?`)
	reEmptyLink  = regexp.MustCompile(`\[\s*\]\([^)]*\) ?`)
	// Lines that are only a bare URL or autolink: remnants of image-only
	// buttons whose text was a stripped pixel.
	reBareURLLine = regexp.MustCompile(`(?m)^[^\S\n]*(?:https?://\S+|<https?://\S+>)[^\S\n]*(?:\n|$)`)
	// Lines made only of exotic spaces (&nbsp; and friends); plain
	// space/tab lines are left alone to protect code fences.
	reNbspLine = regexp.MustCompile(`(?m)^[\x{00A0}\x{202F}\x{2003}\x{2009}]+(?:\n|$)`)
)

// cleanMarkdown removes newsletter noise from converted markdown:
// invisible spacer characters, empty images/links, bare-URL lines,
// &nbsp;-only lines, and excess blank lines.
func cleanMarkdown(s string) string {
	s = reInvisible.ReplaceAllString(s, "")
	s = reEmptyImage.ReplaceAllString(s, "")
	s = reEmptyLink.ReplaceAllString(s, "")
	s = reBareURLLine.ReplaceAllString(s, "")
	s = reNbspLine.ReplaceAllString(s, "")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

func HTMLToMarkdown(value string) string {
	root := parseHTML(value)
	md := newMarkdownizer()
	md.walk(root)
	return cleanMarkdown(md.text())
}

var mdHeadingRe = regexp.MustCompile(`^(#{1,6}) (.+)$`)
var mdULRe = regexp.MustCompile(`^[-*] `)
var mdOLRe = regexp.MustCompile(`^\d+\. `)
var mdCodeRe = regexp.MustCompile("`([^`]+)`")
var mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
var mdBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)

func markdownBlockStart(line string) bool {
	return strings.HasPrefix(line, "```") ||
		mdHeadingRe.MatchString(line) ||
		strings.HasPrefix(line, "> ") ||
		mdULRe.MatchString(line) ||
		mdOLRe.MatchString(line)
}

func MarkdownToHTML(value string) string {
	text := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(value)
	lines := strings.Split(text, "\n")
	var blocks []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if strings.HasPrefix(line, "```") {
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(lines[i], "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			blocks = append(blocks, "<pre><code>"+escapeHTML(strings.Join(code, "\n"))+"</code></pre>")
			continue
		}
		if m := mdHeadingRe.FindStringSubmatch(line); m != nil {
			n := len(m[1])
			blocks = append(blocks, "<h"+strconv.Itoa(n)+">"+mdInline(m[2])+"</h"+strconv.Itoa(n)+">")
			i++
			continue
		}
		if strings.HasPrefix(line, "> ") {
			var quote []string
			for i < len(lines) && strings.HasPrefix(lines[i], "> ") {
				quote = append(quote, lines[i][2:])
				i++
			}
			blocks = append(blocks, "<blockquote>"+MarkdownToHTML(strings.Join(quote, "\n"))+"</blockquote>")
			continue
		}
		if mdULRe.MatchString(line) {
			var items []string
			for i < len(lines) && mdULRe.MatchString(lines[i]) {
				items = append(items, "<li>"+mdInline(lines[i][2:])+"</li>")
				i++
			}
			blocks = append(blocks, "<ul>"+strings.Join(items, "")+"</ul>")
			continue
		}
		if mdOLRe.MatchString(line) {
			var items []string
			for i < len(lines) && mdOLRe.MatchString(lines[i]) {
				content := mdOLRe.ReplaceAllString(lines[i], "")
				items = append(items, "<li>"+mdInline(content)+"</li>")
				i++
			}
			blocks = append(blocks, "<ol>"+strings.Join(items, "")+"</ol>")
			continue
		}
		para := []string{line}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !markdownBlockStart(lines[i]) {
			para = append(para, lines[i])
			i++
		}
		blocks = append(blocks, "<p>"+mdInline(strings.Join(para, " "))+"</p>")
	}
	return strings.Join(blocks, "")
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func escapeHTMLQuote(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}

func mdReplace(pattern *regexp.Regexp, text string, repl func(groups []string) string) string {
	out := pattern.ReplaceAllStringFunc(text, func(m string) string {
		return repl(pattern.FindStringSubmatch(m))
	})
	return out
}

func mdInline(text string) string {
	slots := []string{}
	hold := func(fragment string) string {
		slots = append(slots, fragment)
		return "\x00" + strconv.Itoa(len(slots)-1) + "\x00"
	}

	text = mdReplace(mdCodeRe, text, func(g []string) string {
		return hold("<code>" + escapeHTML(g[1]) + "</code>")
	})
	text = mdReplace(mdLinkRe, text, func(g []string) string {
		href := escapeHTMLQuote(g[2])
		label := escapeHTML(g[1])
		return hold(`<a href="` + href + `">` + label + `</a>`)
	})
	text = mdReplace(mdBoldRe, text, func(g []string) string {
		inner := g[1]
		if inner == "" {
			inner = g[2]
		}
		return hold("<strong>" + escapeHTML(inner) + "</strong>")
	})
	text = replaceItalic(text, func(inner string) string {
		return hold("<em>" + escapeHTML(inner) + "</em>")
	})
	text = escapeHTML(text)
	for idx, fragment := range slots {
		text = strings.ReplaceAll(text, "\x00"+strconv.Itoa(idx)+"\x00", fragment)
	}
	return text
}

// replaceItalic emulates the lookaround regex: single-delimiter spans, shortest match.
func replaceItalic(text string, repl func(string) string) string {
	runes := []rune(text)
	var b strings.Builder
	i := 0
	for i < len(runes) {
		delim, start, end, ok := findItalicSpan(runes, i)
		if !ok {
			break
		}
		b.WriteString(string(runes[i:start]))
		b.WriteString(repl(string(runes[start+1 : end])))
		i = end + 1
		_ = delim
	}
	b.WriteString(string(runes[i:]))
	return b.String()
}

func isSingle(runes []rune, k int, d rune) bool {
	if runes[k] != d {
		return false
	}
	if k > 0 && runes[k-1] == d {
		return false
	}
	if k < len(runes)-1 && runes[k+1] == d {
		return false
	}
	return true
}

func findItalicSpan(runes []rune, from int) (d rune, start, end int, ok bool) {
	for p := from; p+1 < len(runes); p++ {
		if !isSingle(runes, p, '*') && !isSingle(runes, p, '_') {
			continue
		}
		d := runes[p]
		for q := p + 2; q < len(runes); q++ {
			if isSingle(runes, q, d) {
				if q > p+1 {
					return d, p, q, true
				}
				p = q - 1
				break
			}
		}
	}
	return 0, 0, 0, false
}

func isControlRune(ch rune) bool {
	o := ch
	return o < 0x20 || o == 0x7F || (o >= 0x80 && o <= 0x9F)
}

func escapeText(s string, line string) string {
	s = sanitizeProse(s, line)
	var out strings.Builder
	runes := []rune(s)
	lineRunes := utf8.RuneCountInString(line)
	for i, ch := range runes {
		if ch == '&' {
			out.WriteString("&amp;")
			continue
		}
		escaped := inlineMeta[ch] ||
			(line == "" && i == 0 && lineStartMeta[ch]) ||
			((ch == '.' || ch == ')') && closesOrderedMarker(lineRunes+i, string(runes[:i])))
		if escaped {
			out.WriteString("\\")
		}
		out.WriteRune(ch)
	}
	return out.String()
}

func sanitizeProse(s string, line string) string {
	s = strings.NewReplacer("\n", "", "\t", "").Replace(s)
	context := lineContext(line)
	whole := terminal.SanitizeText(context + s)
	head := terminal.SanitizeText(context)
	var tail string
	if strings.HasPrefix(whole, context) {
		tail = whole[len(context):]
	} else if strings.HasPrefix(whole, head) {
		tail = whole[len(head):]
	} else {
		return recollapse(terminal.SanitizeText(s), line)
	}
	if s != "" {
		last := []rune(s)[len([]rune(s))-1]
		if joiners[last] && !strings.HasSuffix(tail, string(last)) &&
			strings.HasSuffix(terminal.SanitizeText(context+tail+string(last)+"é"), string(last)+"é") {
			tail += string(last)
		}
	}
	return recollapse(tail, line)
}

func isCombiningMarkRune(ch rune) bool {
	if ch < 0x300 {
		return false
	}
	return unicode.Is(unicode.Mn, ch) || unicode.Is(unicode.Me, ch) || unicode.Is(unicode.Mc, ch)
}

func lineContext(line string) string {
	runes := []rune(line)
	end := len(runes)
	for range [32]struct{}{} {
		if end <= 0 {
			break
		}
		ch := runes[end-1]
		end--
		if !joiners[ch] && !isCombiningMarkRune(ch) {
			break
		}
	}
	return string(runes[:end])
}

func recollapse(s string, line string) string {
	folded := strings.Join(strings.Fields(s), " ")
	if folded == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) > 0 && runes[0] == ' ' && line != "" && !strings.HasSuffix(line, " ") {
		folded = " " + folded
	}
	if len(runes) > 0 && runes[len(runes)-1] == ' ' {
		folded += " "
	}
	return folded
}

func closesOrderedMarker(totalLen int, run string) bool {
	if totalLen > 9 {
		return false
	}
	return allDigits(run)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func destination(raw string) string {
	var cleaned strings.Builder
	for _, ch := range strings.TrimSpace(raw) {
		if isControlRune(ch) {
			continue
		}
		cleaned.WriteRune(ch)
	}
	rawStr := cleaned.String()
	if rawStr == "" || !allowedScheme(rawStr) {
		return ""
	}
	var out strings.Builder
	runes := []rune(rawStr)
	for i, ch := range runes {
		if ch <= 32 || destEnd[ch] {
			fmtPct(&out, ch)
		} else if ch == '&' && entityRefRe.MatchString(string(runes[i:])) {
			out.WriteString("&amp;")
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String()
}

func fmtPct(out *strings.Builder, ch rune) {
	const hexdigits = "0123456789ABCDEF"
	v := int(ch)
	if v > 255 {
		v = '?'
	}
	out.WriteByte('%')
	out.WriteByte(hexdigits[v>>4])
	out.WriteByte(hexdigits[v&15])
}

func allowedScheme(raw string) bool {
	m := schemeRe.FindString(raw)
	if m == "" {
		return true
	}
	switch strings.ToLower(m) {
	case "http:", "https:", "mailto:":
		return true
	}
	return false
}

func absoluteDest(dest string) bool {
	return schemeRe.MatchString(dest)
}

type listLevel struct {
	ordered bool
	number  int
	prefix  string
}

type markdownizer struct {
	lines         []string
	line          string
	prefix        string
	pendingPrefix string
	lists         []*listLevel
	breaking      bool
	quoteDepth    int
}

func newMarkdownizer() *markdownizer { return &markdownizer{} }

func (m *markdownizer) text() string {
	m.flushLine()
	result := strings.ReplaceAll(strings.Join(m.lines, "\n"), "\r", "")
	trailingSpace := regexp.MustCompile(`[ \t]+\n`)
	result = trailingSpace.ReplaceAllString(result, "\n")
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

func (m *markdownizer) walk(n *node) {
	if n.text != nil {
		m.writeText(*n.text)
		return
	}
	if n.tag == "document" {
		m.children(n)
		return
	}
	m.element(n)
}

func (m *markdownizer) children(n *node) {
	for _, child := range n.children {
		m.walk(child)
	}
}

func (m *markdownizer) element(n *node) {
	tag := n.tag
	if skipTags[tag] || isHidden(n) {
		return
	}
	switch {
	case tag == "br":
		m.hardBreak()
	case tag == "hr":
		m.block(func() { m.write("---") })
	case tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" || tag == "h5" || tag == "h6":
		m.heading(n)
	case contains([]string{"p", "div", "section", "article", "header", "footer", "tbody", "thead"}, tag):
		m.block(func() { m.children(n) })
	case tag == "blockquote":
		m.blockquote(n)
	case tag == "ul" || tag == "ol":
		m.list(n)
	case tag == "li":
		m.children(n)
	case tag == "pre":
		m.codeBlock(n)
	case tag == "table":
		m.table(n)
	case tag == "strong" || tag == "b":
		m.emphasis(n, "**")
	case tag == "em" || tag == "i":
		m.emphasis(n, "*")
	case tag == "code":
		m.code(n)
	case tag == "a":
		m.link(n)
	case tag == "img":
		m.image(n)
	default:
		m.children(n)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (m *markdownizer) heading(n *node) {
	depth := int(n.tag[1] - '0')
	prefix := strings.Repeat("#", depth) + " "
	m.block(func() {
		m.write(prefix)
		m.children(n)
	})
}

func (m *markdownizer) blockquote(n *node) {
	if m.quoteDepth >= maxNesting {
		m.block(func() { m.children(n) })
		return
	}
	outer := m.prefix
	m.flushLine()
	m.blank()
	m.prefix = outer + "> "
	m.quoteDepth++
	start := len(m.lines)
	m.children(n)
	m.flushLine()
	m.trimBlankLines(start)
	m.quoteDepth--
	m.prefix = outer
	m.blank()
}

func (m *markdownizer) trimBlankLines(start int) {
	blank := strings.TrimRight(m.prefix, " ")
	for len(m.lines) > start && m.lines[len(m.lines)-1] == blank {
		m.lines = m.lines[:len(m.lines)-1]
	}
	for len(m.lines) > start && m.lines[start] == blank {
		m.lines = append(m.lines[:start], m.lines[start+1:]...)
	}
}

func (m *markdownizer) list(n *node) {
	if len(m.lists) >= maxNesting {
		m.listItems(n, m.lists[len(m.lists)-1])
		return
	}
	level := &listLevel{ordered: n.tag == "ol", prefix: m.prefix}
	m.lists = append(m.lists, level)
	m.flushLine()
	if len(m.lists) == 1 {
		m.blank()
	}
	m.listItems(n, level)
	m.flushLine()
	m.lists = m.lists[:len(m.lists)-1]
	if len(m.lists) == 0 {
		m.blank()
	}
}

func (m *markdownizer) listItems(n *node, level *listLevel) {
	for _, child := range n.children {
		if child.tag == "li" {
			m.listItem(child, level)
		} else {
			m.walk(child)
		}
	}
}

func (m *markdownizer) listItem(n *node, level *listLevel) {
	marker := "- "
	if level.ordered {
		level.number++
		marker = itoa(level.number) + ". "
	}
	outer := m.prefix
	m.flushLine()
	m.pendingPrefix = level.prefix + marker
	m.prefix = level.prefix + strings.Repeat(" ", len(marker))
	m.children(n)
	m.flushLine()
	m.pendingPrefix = ""
	m.prefix = outer
}

func itoa(n int) string { return strconv.Itoa(n) }

func (m *markdownizer) codeBlock(n *node) {
	content := stripControls(preformatted(n))
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.Trim(content, "\n")
	fence := codeFence(content)
	m.flushLine()
	m.blank()
	m.write(fence + fenceInfoValue(codeLanguage(n)))
	m.flushLine()
	for _, line := range strings.Split(content, "\n") {
		m.rawLine(line)
	}
	m.write(fence)
	m.flushLine()
	m.blank()
}

func (m *markdownizer) table(n *node) {
	cells := tableCells(n)
	if cells == nil {
		return
	}
	if isLayoutTable(n, cells) {
		m.layoutTable(n)
		return
	}
	rows := make([][]string, len(cells))
	for r, rowCells := range cells {
		rows[r] = make([]string, len(rowCells))
		for c, cell := range rowCells {
			rows[r][c] = inlineMarkdown(cell)
		}
	}
	width := len(rows[0])
	m.flushLine()
	m.blank()
	m.write("| " + strings.Join(rows[0], " | ") + " |")
	m.flushLine()
	m.write("|" + strings.Repeat(" --- |", width))
	m.flushLine()
	for _, row := range rows[1:] {
		padded := make([]string, width)
		copy(padded, row)
		m.write("| " + strings.Join(padded, " | ") + " |")
		m.flushLine()
	}
	m.blank()
}

func (m *markdownizer) layoutTable(n *node) {
	for _, child := range n.children {
		if child.tag == "td" || child.tag == "th" {
			c := child
			m.block(func() { m.children(c) })
		} else if contains([]string{"tbody", "thead", "tfoot", "tr"}, child.tag) {
			m.layoutTable(child)
		} else {
			m.walk(child)
		}
	}
}

func (m *markdownizer) emphasis(n *node, delimiter string) {
	m.inline(n, func(inner string) string {
		if inner == "" {
			return ""
		}
		return delimiter + inner + delimiter
	})
}

func (m *markdownizer) code(n *node) {
	leading, trailing := surroundingSpace(n)
	formatted := codeSpan(elementText(n))
	if leading {
		m.writeSpace()
	}
	m.write(formatted)
	if trailing {
		m.writeSpace()
	}
}

func (m *markdownizer) link(n *node) {
	href := attrOr(n, "href", "")
	dest := destination(href)
	fmtFn := func(text string) string {
		if dest == "" {
			return text
		}
		label := strings.TrimSpace(elementText(n))
		if strings.TrimSpace(text) == "" || label == strings.TrimSpace(href) {
			if absoluteDest(dest) {
				return "<" + dest + ">"
			}
			return "[" + escapeText(dest, m.line) + "](" + dest + ")"
		}
		return "[" + text + "](" + dest + ")"
	}
	m.inline(n, fmtFn)
}

func (m *markdownizer) image(n *node) {
	if isTrackingImage(n) {
		return
	}
	alt := strings.Join(strings.Fields(attrOr(n, "alt", "")), " ")
	src := destination(attrOr(n, "src", ""))
	if src != "" {
		m.write("![" + escapeText(alt, "alt") + "](" + src + ")")
	} else if alt != "" {
		m.write(escapeText(alt, m.line))
	}
}

func attrOr(n *node, key, def string) string {
	if v, ok := n.attrs[key]; ok {
		return v
	}
	return def
}

func (m *markdownizer) inline(n *node, formatInner func(string) string) {
	leading, trailing := surroundingSpace(n)
	formatted := formatInner(inlineMarkdown(n))
	if leading {
		m.writeSpace()
	}
	m.write(formatted)
	if trailing {
		m.writeSpace()
	}
}

func (m *markdownizer) block(render func()) {
	m.flushLine()
	m.blank()
	render()
	m.flushLine()
	m.blank()
}

func (m *markdownizer) write(s string) { m.line += s }

func (m *markdownizer) writeSpace() {
	if m.line != "" && !strings.HasSuffix(m.line, " ") {
		m.write(" ")
	}
}

func (m *markdownizer) writeText(s string) {
	collapsed := strings.Join(strings.Fields(s), " ")
	firstIsSpace := len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r')
	if collapsed == "" || firstIsSpace {
		m.writeSpace()
	}
	if collapsed == "" {
		return
	}
	m.write(escapeText(collapsed, m.line))
	if s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' {
		m.writeSpace()
	}
}

func (m *markdownizer) hardBreak() {
	m.breaking = m.line != ""
	m.flushLine()
}

func (m *markdownizer) rawLine(line string) {
	m.write(line)
	if strings.TrimSpace(line) == "" && m.pendingPrefix == "" {
		m.lines = append(m.lines, strings.TrimRight(m.prefix, " "))
		m.line = ""
		return
	}
	m.flushLine()
}

func (m *markdownizer) flushLine() {
	content := strings.TrimRight(m.line, " \t\u200c\u200d")
	breaking := m.breaking
	m.line = ""
	m.breaking = false
	if content == "" && m.pendingPrefix == "" {
		return
	}
	prefix := m.prefix
	if m.pendingPrefix != "" {
		prefix = m.pendingPrefix
		m.pendingPrefix = ""
	}
	if breaking {
		m.lines = append(m.lines, prefix+content)
	} else {
		m.lines = append(m.lines, strings.TrimRight(prefix+content, " "))
	}
}

func (m *markdownizer) blank() {
	if len(m.lists) > 0 || len(m.lines) == 0 {
		return
	}
	if m.lines[len(m.lines)-1] == strings.TrimRight(m.prefix, " ") {
		return
	}
	m.lines = append(m.lines, strings.TrimRight(m.prefix, " "))
}

func inlineMarkdown(n *node) string {
	md := newMarkdownizer()
	md.children(n)
	md.flushLine()
	return strings.TrimSpace(strings.Join(md.lines, " "))
}

func tableCells(n *node) [][]*node {
	if n.tag == "tr" {
		var cells []*node
		for _, child := range n.children {
			if child.tag == "td" || child.tag == "th" {
				cells = append(cells, child)
			}
		}
		if cells == nil {
			return nil
		}
		return [][]*node{cells}
	}
	var rows [][]*node
	for _, child := range n.children {
		rows = append(rows, tableCells(child)...)
	}
	return rows
}

func isLayoutTable(n *node, cells [][]*node) bool {
	role := attrOr(n, "role", "")
	if role == "presentation" || role == "none" {
		return true
	}
	columns := 0
	for _, row := range cells {
		if len(row) > columns {
			columns = len(row)
		}
		for _, cell := range row {
			if _, hasColspan := cell.attrs["colspan"]; hasColspan {
				return true
			}
			if _, hasRowspan := cell.attrs["rowspan"]; hasRowspan {
				return true
			}
			if holdsBlock(cell) {
				return true
			}
		}
	}
	return columns < 2
}

func holdsBlock(n *node) bool {
	for _, child := range n.children {
		if blockInCell[child.tag] || holdsBlock(child) {
			return true
		}
	}
	return false
}

func preformatted(n *node) string {
	var parts []string
	var collect func(*node)
	collect = func(x *node) {
		if x.text != nil {
			parts = append(parts, *x.text)
			return
		}
		if skipTags[x.tag] {
			return
		}
		if x.tag == "br" {
			parts = append(parts, "\n")
			return
		}
		for _, child := range x.children {
			collect(child)
		}
	}
	collect(n)
	return strings.Join(parts, "")
}

func elementText(n *node) string {
	var parts []string
	var collect func(*node)
	collect = func(x *node) {
		if x.text != nil {
			parts = append(parts, *x.text)
			return
		}
		if skipTags[x.tag] {
			return
		}
		for _, child := range x.children {
			collect(child)
		}
	}
	collect(n)
	return strings.Join(parts, "")
}

func surroundingSpace(n *node) (bool, bool) {
	text := elementText(n)
	if text == "" {
		return false, false
	}
	first := text[0]
	last := text[len(text)-1]
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' }
	return isSpace(first), isSpace(last)
}

func isHidden(n *node) bool {
	if _, ok := n.attrs["hidden"]; ok {
		return true
	}
	if _, ok := n.attrs["inert"]; ok {
		return true
	}
	return styleHides(attrOr(n, "style", ""))
}

func styleHides(style string) bool {
	display := styleProp(style, "display")
	visibility := styleProp(style, "visibility")
	contentVisibility := styleProp(style, "content-visibility")
	userSelect := styleProp(style, "user-select", "-webkit-user-select")
	if display == "none" ||
		visibility == "hidden" || visibility == "collapse" ||
		contentVisibility == "hidden" ||
		userSelect == "none" ||
		styleProp(style, "mso-hide") == "all" {
		return true
	}
	// Fully transparent box.
	if o := styleProp(style, "opacity"); o != "" {
		if f, err := strconv.ParseFloat(o, 64); err == nil && f == 0 {
			return true
		}
	}
	// Newsletter preheader spacers: a zero-height (or zero-width) box with
	// its overflow clipped, so the padded text never renders.
	overflow := styleProp(style, "overflow", "overflow-x", "overflow-y")
	clipped := overflow == "hidden" || overflow == "clip"
	zeroH := zeroCSSLength(styleProp(style, "max-height")) || zeroCSSLength(styleProp(style, "height"))
	zeroW := zeroCSSLength(styleProp(style, "max-width")) || zeroCSSLength(styleProp(style, "width"))
	if (zeroH || zeroW) && clipped {
		return true
	}
	if zeroH && zeroW {
		return true
	}
	// Text collapsed to nothing: font-size:0 paired with line-height:0.
	// Both are required so image-button cells that only zero font-size
	// (to kill inline-block whitespace) keep their alt text.
	if zeroCSSLength(styleProp(style, "font-size")) &&
		zeroCSSLength(styleProp(style, "line-height")) {
		return true
	}
	return false
}

// zeroCSSLength reports whether a CSS length value is zero, ignoring any
// unit suffix (0, 0px, 0.0em, 0%, …). Keywords like "normal" are not zero.
func zeroCSSLength(value string) bool {
	value = strings.TrimRight(strings.TrimSpace(value), "abcdefghijklmnopqrstuvwxyz%")
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	f, err := strconv.ParseFloat(value, 64)
	return err == nil && f == 0
}

func styleProp(style string, names ...string) string {
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[strings.ToLower(n)] = true
	}
	selected := ""
	selectedImportant := false
	found := false
	for _, declaration := range strings.Split(strings.ToLower(style), ";") {
		if !strings.Contains(declaration, ":") {
			continue
		}
		i := strings.Index(declaration, ":")
		prop := strings.TrimSpace(declaration[:i])
		value := strings.TrimSpace(declaration[i+1:])
		if !wanted[prop] {
			continue
		}
		important := strings.HasSuffix(value, "!important")
		if important {
			value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		}
		if !found || important || !selectedImportant {
			selected = value
			selectedImportant = important
			found = true
		}
	}
	if !found {
		return ""
	}
	return selected
}

func isTrackingImage(n *node) bool {
	src := attrOr(n, "src", "")
	if trackers.Identify(src) != "" {
		return true
	}
	width := strings.TrimSpace(attrOr(n, "width", ""))
	height := strings.TrimSpace(attrOr(n, "height", ""))
	return (width == "0" || width == "1") && (height == "0" || height == "1")
}

func codeSpan(content string) string {
	content = stripControls(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(content))
	if strings.TrimSpace(content) == "" {
		return ""
	}
	delimiter := strings.Repeat("`", backtickRun(content)+1)
	if needsPadding(content) || needsPadding(terminal.SanitizeText(content)) {
		content = " " + content + " "
	}
	return delimiter + content + delimiter
}

func needsPadding(content string) bool {
	return strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") ||
		(strings.HasPrefix(content, " ") && strings.HasSuffix(content, " "))
}

func codeFence(content string) string {
	n := backtickRun(content) + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

func backtickRun(content string) int {
	a := longestRun(content, '`')
	b := longestRun(terminal.SanitizeText(content), '`')
	if b > a {
		return b
	}
	return a
}

func longestRun(s string, ch rune) int {
	longest, run := 0, 0
	for _, c := range s {
		if c == ch {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

func fenceInfoValue(language string) string {
	if fenceInfoRe.MatchString(language) {
		return language
	}
	return ""
}

func codeLanguage(n *node) string {
	language := attrOr(n, "language", "")
	if language != "" {
		return language
	}
	for _, child := range n.children {
		if child.tag == "code" {
			for _, className := range strings.Fields(attrOr(child, "class", "")) {
				if strings.HasPrefix(className, "language-") {
					return className[len("language-"):]
				}
			}
		}
	}
	return ""
}

func stripControls(s string) string {
	var out strings.Builder
	for _, ch := range s {
		if ch == '\n' || ch == '\t' || !isControlRune(ch) {
			out.WriteRune(ch)
		}
	}
	return out.String()
}
