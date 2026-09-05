package htmlmd

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A callout is a colored aside in compose. The marker is `[!note]`, not a
// blockquote: GitHub's `> [!NOTE]` form is accepted as input, but we never
// emit `>`.

type calloutSpec struct {
	kind   string
	label  string
	bg     string
	fg     string
	accent string
	rgb    string
}

func (s calloutSpec) tdStyle() string {
	return fmt.Sprintf("background-color:%s;color:%s;border-left:4px solid %s;padding:12px 16px", s.bg, s.fg, s.accent)
}

func (s calloutSpec) tableStyle() string {
	return "border-collapse:collapse;margin:1em 0;width:100%"
}

var callouts = []calloutSpec{
	{kind: "note", label: "Note", bg: "#dbeafe", fg: "#1e3a8a", accent: "#2563eb", rgb: "219, 234, 254"},
	{kind: "tip", label: "Tip", bg: "#dcfce7", fg: "#14532d", accent: "#16a34a", rgb: "220, 252, 231"},
	{kind: "warning", label: "Warning", bg: "#fef3c7", fg: "#78350f", accent: "#d97706", rgb: "254, 243, 199"},
}

var calloutByKind = func() map[string]calloutSpec {
	m := map[string]calloutSpec{}
	for _, s := range callouts {
		m[s.kind] = s
	}
	return m
}()

var calloutMarkerRe = regexp.MustCompile(`(?i)^\[!(note|tip|warning)\](?:\s+(.*))?$`)

func specFor(kind string) (calloutSpec, bool) {
	s, ok := calloutByKind[strings.ToLower(strings.TrimSpace(kind))]
	return s, ok
}

func parseCalloutMarker(line string) (kind, title string, ok bool) {
	m := calloutMarkerRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", "", false
	}
	return strings.ToLower(m[1]), strings.TrimSpace(m[2]), true
}

func isCalloutMarkerLine(line string) bool {
	_, _, ok := parseCalloutMarker(line)
	return ok
}

func renderCallout(kind, title, body string) string {
	spec, ok := specFor(kind)
	if !ok {
		return ""
	}
	if title == "" {
		title = spec.label
	}
	inner := "<p><strong>" + mdInline(title) + "</strong></p>"
	if strings.TrimSpace(body) != "" {
		inner += MarkdownToHTML(body)
	}
	return `<table data-callout="` + spec.kind + `" border="0" cellpadding="0" cellspacing="0" style="` + spec.tableStyle() + `"><tr><td bgcolor="` + spec.bg + `" style="` + spec.tdStyle() + `">` + inner + `</td></tr></table>`
}

func consumeCallout(lines []string, i int) (html string, next int, ok bool) {
	kind, title, ok := parseCalloutMarker(lines[i])
	if !ok {
		return "", i, false
	}
	i++
	var body []string
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !isCalloutMarkerLine(lines[i]) {
		body = append(body, lines[i])
		i++
	}
	return renderCallout(kind, title, strings.Join(body, "\n")), i, true
}

func consumeQuotedCallout(quote []string) (html string, ok bool) {
	if len(quote) == 0 {
		return "", false
	}
	kind, title, ok := parseCalloutMarker(quote[0])
	if !ok {
		return "", false
	}
	return renderCallout(kind, title, strings.Join(quote[1:], "\n")), true
}

func (m *markdownizer) tableCallout(n *node) bool {
	kind, cell := calloutFromTable(n)
	if kind == "" || cell == nil {
		return false
	}
	m.emitCallout(kind, cell)
	return true
}

func (m *markdownizer) emitCallout(kind string, cell *node) {
	spec, ok := specFor(kind)
	if !ok {
		m.children(cell)
		return
	}
	title, body := splitCalloutCell(cell, spec)
	m.flushLine()
	m.blank()
	if title != "" {
		m.write("[!" + spec.kind + "] " + title)
	} else {
		m.write("[!" + spec.kind + "]")
	}
	m.flushLine()
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			m.write(line)
			m.flushLine()
		}
	}
	m.blank()
}

func splitCalloutCell(cell *node, spec calloutSpec) (title, body string) {
	kids := contentChildren(cell)
	start := 0
	if len(kids) > 0 {
		if t, ok := calloutTitlePara(kids[0], spec); ok {
			title = t
			start = 1
		}
	}
	inner := newMarkdownizer()
	for _, child := range kids[start:] {
		inner.walk(child)
	}
	return title, cleanMarkdown(inner.text())
}

func calloutTitlePara(n *node, spec calloutSpec) (string, bool) {
	head := strings.TrimSpace(plainText(n))
	if head == "" {
		return "", false
	}
	if strings.EqualFold(head, spec.label) {
		return "", true
	}
	if n.tag == "p" && (hasTag(n, "strong") || hasTag(n, "b")) {
		return head, true
	}
	return "", false
}

func hasTag(n *node, tag string) bool {
	if n.tag == tag {
		return true
	}
	for _, child := range n.children {
		if hasTag(child, tag) {
			return true
		}
	}
	return false
}

func contentChildren(n *node) []*node {
	var out []*node
	for _, child := range n.children {
		if child.text != nil && strings.TrimSpace(*child.text) == "" {
			continue
		}
		out = append(out, child)
	}
	return out
}

func plainText(n *node) string {
	if n.text != nil {
		return *n.text
	}
	var b strings.Builder
	for _, child := range n.children {
		b.WriteString(plainText(child))
	}
	return b.String()
}

func calloutFromTable(n *node) (kind string, cell *node) {
	if k := strings.ToLower(attrOr(n, "data-callout", "")); k != "" {
		if _, ok := specFor(k); ok {
			return k, firstTableCell(n)
		}
	}
	cell = firstTableCell(n)
	if cell == nil {
		return "", nil
	}
	if k := strings.ToLower(attrOr(cell, "data-callout", "")); k != "" {
		if _, ok := specFor(k); ok {
			return k, cell
		}
	}
	if k := kindFromStyle(attrOr(cell, "style", "")); k != "" {
		return k, cell
	}
	if k := kindFromLabelNode(cell); k != "" && tableColumnCount(n) <= 1 {
		return k, cell
	}
	return "", nil
}

func calloutFromBlock(n *node) (kind string, inner *node) {
	if k := strings.ToLower(attrOr(n, "data-callout", "")); k != "" {
		if _, ok := specFor(k); ok {
			return k, n
		}
	}
	if k := kindFromLabelNode(n); k != "" {
		return k, n
	}
	return "", nil
}

func kindFromLabelNode(n *node) string {
	kids := contentChildren(n)
	if len(kids) == 0 {
		return ""
	}
	head := strings.TrimSpace(plainText(kids[0]))
	for _, spec := range callouts {
		if strings.EqualFold(head, spec.label) {
			return spec.kind
		}
	}
	return ""
}

func tableColumnCount(n *node) int {
	cols := 0
	for _, row := range tableCells(n) {
		if len(row) > cols {
			cols = len(row)
		}
	}
	return cols
}

func firstTableCell(n *node) *node {
	if n.tag == "td" || n.tag == "th" {
		return n
	}
	for _, child := range n.children {
		if c := firstTableCell(child); c != nil {
			return c
		}
	}
	return nil
}

func kindFromStyle(style string) string {
	s := strings.ToLower(strings.ReplaceAll(style, " ", ""))
	for _, spec := range callouts {
		hex := strings.ToLower(spec.bg)
		rgb := strings.ReplaceAll(spec.rgb, " ", "")
		if strings.Contains(s, hex) || strings.Contains(s, "rgb("+rgb+")") {
			return spec.kind
		}
	}
	return ""
}

// StyleCallouts rewrites recognized callouts to the email-safe box (left
// border, padding, data-callout, bgcolor). Lexxy strips table-cell
// backgrounds and does not keep custom attributes, so the editor signal is
// the "Note"/"Tip"/"Warning" label on a 1-cell table or a blockquote.
func StyleCallouts(s string) string {
	if s == "" {
		return s
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		return s
	}
	changed := false
	for i, n := range nodes {
		if styleCalloutWalk(n) {
			changed = true
		}
		if neu := promoteCallout(n); neu != nil {
			nodes[i] = neu
			changed = true
		} else if n.Type == html.ElementNode && n.Data == "table" {
			if styleCalloutTable(n) {
				changed = true
			}
		}
	}
	if !changed {
		return s
	}
	var b strings.Builder
	for _, n := range nodes {
		if err := html.Render(&b, n); err != nil {
			return s
		}
	}
	return b.String()
}

func styleCalloutWalk(n *html.Node) bool {
	changed := false
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if styleCalloutWalk(c) {
			changed = true
		}
		if c.Parent != n {
			c = next
			continue
		}
		if neu := promoteCallout(c); neu != nil {
			n.InsertBefore(neu, c)
			n.RemoveChild(c)
			changed = true
		} else if c.Type == html.ElementNode && c.Data == "table" {
			if styleCalloutTable(c) {
				changed = true
			}
		}
		c = next
	}
	return changed
}

func promoteCallout(n *html.Node) *html.Node {
	if n == nil || n.Type != html.ElementNode || n.Data != "blockquote" {
		return nil
	}
	kind := htmlCalloutKind(n)
	spec, ok := specFor(kind)
	if !ok {
		return nil
	}
	return wrapAsCalloutTable(n, spec)
}

func htmlCalloutKind(n *html.Node) string {
	if k := htmlAttr(n, "data-callout"); k != "" {
		if _, ok := specFor(k); ok {
			return strings.ToLower(k)
		}
	}
	switch n.Data {
	case "table":
		td := firstHTMLTableCell(n)
		if td == nil {
			return ""
		}
		if k := htmlAttr(td, "data-callout"); k != "" {
			if _, ok := specFor(k); ok {
				return strings.ToLower(k)
			}
		}
		if k := kindFromStyle(htmlAttr(td, "style")); k != "" {
			return k
		}
		if k := kindFromHTMLLabel(td); k != "" && htmlTableColumns(n) <= 1 {
			return k
		}
	case "blockquote":
		return kindFromHTMLLabel(n)
	}
	return ""
}

func kindFromHTMLLabel(n *html.Node) string {
	p := firstHTMLParagraph(n)
	if p == nil {
		return ""
	}
	head := strings.TrimSpace(htmlText(p))
	for _, spec := range callouts {
		if strings.EqualFold(head, spec.label) {
			return spec.kind
		}
	}
	return ""
}

func firstHTMLParagraph(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "p" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstHTMLParagraph(c); found != nil {
			return found
		}
	}
	return nil
}

func htmlText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(htmlText(c))
	}
	return b.String()
}

func htmlTableColumns(n *html.Node) int {
	cols := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			c := 0
			for td := n.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && (td.Data == "td" || td.Data == "th") {
					c++
				}
			}
			if c > cols {
				cols = c
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return cols
}

func wrapAsCalloutTable(n *html.Node, spec calloutSpec) *html.Node {
	td := &html.Node{Type: html.ElementNode, Data: "td", DataAtom: atom.Td}
	setHTMLAttr(td, "bgcolor", spec.bg)
	setHTMLAttr(td, "style", spec.tdStyle())
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		td.AppendChild(c)
		c = next
	}
	tr := &html.Node{Type: html.ElementNode, Data: "tr", DataAtom: atom.Tr}
	tr.AppendChild(td)
	table := &html.Node{Type: html.ElementNode, Data: "table", DataAtom: atom.Table}
	setHTMLAttr(table, "data-callout", spec.kind)
	setHTMLAttr(table, "border", "0")
	setHTMLAttr(table, "cellpadding", "0")
	setHTMLAttr(table, "cellspacing", "0")
	setHTMLAttr(table, "style", spec.tableStyle())
	table.AppendChild(tr)
	return table
}

func styleCalloutTable(table *html.Node) bool {
	kind := htmlCalloutKind(table)
	spec, ok := specFor(kind)
	if !ok {
		return false
	}
	td := firstHTMLTableCell(table)
	if td == nil {
		return false
	}
	changed := false
	if htmlAttr(table, "data-callout") != spec.kind {
		setHTMLAttr(table, "data-callout", spec.kind)
		changed = true
	}
	if htmlAttr(table, "style") != spec.tableStyle() {
		setHTMLAttr(table, "style", spec.tableStyle())
		changed = true
	}
	if htmlAttr(td, "style") != spec.tdStyle() {
		setHTMLAttr(td, "style", spec.tdStyle())
		changed = true
	}
	if htmlAttr(td, "bgcolor") != spec.bg {
		setHTMLAttr(td, "bgcolor", spec.bg)
		changed = true
	}
	return changed
}

func firstHTMLTableCell(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstHTMLTableCell(c); found != nil {
			return found
		}
	}
	return nil
}

func htmlAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setHTMLAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
