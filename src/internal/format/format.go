package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"

	"mailbox/src/internal/terminal"
)

const formatsMsg = "--json, --ids-only, --count, --html, --markdown"

// OM is an insertion-ordered map for JSON output (python dict parity).
type OM struct {
	Keys []string
	Vals map[string]any
}

func NewOM(pairs ...any) *OM {
	o := &OM{Vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i].(string)
		if _, seen := o.Vals[key]; !seen {
			o.Keys = append(o.Keys, key)
		}
		o.Vals[key] = pairs[i+1]
	}
	return o
}

func (o *OM) Set(key string, value any) {
	if _, seen := o.Vals[key]; !seen {
		o.Keys = append(o.Keys, key)
	}
	o.Vals[key] = value
}

func (o *OM) Get(key string) any { return o.Vals[key] }

func (o *OM) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.Vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type Output struct {
	JSON         bool
	IDsOnly      bool
	Count        bool
	HTML         bool
	Markdown     bool
	Quiet        bool
	Styled       bool
	JQ           string
	AllowPartial bool
	TTY          bool
}

// NextPage is an optional WriteList extra: the cursor --page takes next.
type NextPage string

func (o *Output) machine() bool {
	return o.JSON || o.IDsOnly || o.Count || o.HTML || o.Markdown || o.Quiet || o.JQ != ""
}

// ApplyDefaultFormat: TTY stays human; a pipe becomes the JSON envelope unless a format flag or --styled said otherwise.
func ApplyDefaultFormat(o *Output, tty bool) {
	o.TTY = tty
	if !o.machine() && !o.Styled && !tty {
		o.JSON = true
	}
}

func PageSlice[T any](items []T, page, limit int, all bool) (out []T, next string, trunc bool) {
	n := len(items)
	if all || limit <= 0 {
		return items, "", false
	}
	if page < 1 {
		page = 1
	}
	from := (page - 1) * limit
	if from > n {
		from = n
	}
	to := from + limit
	if to > n {
		to = n
	}
	if to < n {
		return items[from:to], strconv.Itoa(page + 1), true
	}
	return items[from:to], "", false
}

func ExitStatus(code string) int {
	switch code {
	case "usage":
		return 1
	case "not_found":
		return 2
	case "auth":
		return 3
	case "forbidden":
		return 4
	case "rate_limit":
		return 5
	case "network":
		return 6
	case "ambiguous":
		return 8
	default:
		return 7
	}
}

func Classify(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "ambiguous"):
		return "ambiguous"
	case strings.Contains(msg, "not found"):
		return "not_found"
	case strings.Contains(msg, "message id must"), strings.Contains(msg, "attachment id must"):
		return "usage"
	case isNetwork(err):
		return "network"
	default:
		return "api"
	}
}

func isNetwork(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") || strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "no such host") || strings.Contains(s, "tls:") ||
		strings.Contains(s, "connection reset")
}

func (o *Output) formatCount() int {
	n := 0
	if o.JSON && o.JQ == "" {
		n++
	}
	for _, b := range []bool{o.IDsOnly, o.Count, o.HTML, o.Markdown} {
		if b {
			n++
		}
	}
	return n
}

func TakeOutputFlags(argv []string) ([]string, *Output, error) {
	out := &Output{}
	var kept []string
	jqSeen := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--json":
			out.JSON = true
		case arg == "--ids-only":
			out.IDsOnly = true
		case arg == "--count":
			out.Count = true
		case arg == "--html":
			out.HTML = true
		case arg == "--markdown":
			out.Markdown = true
		case arg == "--quiet":
			out.Quiet = true
		case arg == "--styled":
			out.Styled = true
		case arg == "--allow-partial":
			out.AllowPartial = true
		case arg == "--jq":
			i++
			if i >= len(argv) {
				return nil, nil, fmt.Errorf("--jq needs an expression")
			}
			out.JQ = argv[i]
			out.JSON = true
			jqSeen = true
		case strings.HasPrefix(arg, "--jq="):
			out.JQ = arg[5:]
			out.JSON = true
			jqSeen = true
		default:
			kept = append(kept, arg)
		}
	}
	if jqSeen && out.JQ == "" {
		return nil, nil, fmt.Errorf("--jq needs an expression")
	}
	exclusive := 0
	for _, b := range []bool{out.IDsOnly, out.Count, out.HTML, out.Markdown} {
		if b {
			exclusive++
		}
	}
	if out.JQ != "" && exclusive > 0 {
		return nil, nil, fmt.Errorf("--jq cannot be combined with --ids-only, --count, --html, or --markdown")
	}
	if out.JQ == "" && out.formatCount() > 1 {
		return nil, nil, fmt.Errorf("use only one of %s", formatsMsg)
	}
	if out.Quiet && exclusive > 0 {
		return nil, nil, fmt.Errorf("--quiet cannot be combined with --ids-only, --count, --html, or --markdown")
	}
	if out.JQ != "" {
		if err := ValidateJQ(out.JQ); err != nil {
			return nil, nil, err
		}
	}
	return kept, out, nil
}

func ValidateJQ(expression string) error {
	cmd := exec.Command("jq", "-n", expression)
	err := cmd.Run()
	if err != nil {
		if _, lookErr := exec.LookPath("jq"); lookErr != nil {
			return fmt.Errorf("jq binary not found; install jq for --jq")
		}
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
			return fmt.Errorf("invalid --jq expression")
		}
		return fmt.Errorf("invalid --jq expression")
	}
	return nil
}

func DumpJSON(value any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return ""
	}
	text := strings.TrimSuffix(buf.String(), "\n")
	// escape DEL + C1 like python dump_json
	var out strings.Builder
	for _, ch := range text {
		if ch >= 0x7F && ch <= 0x9F {
			fmt.Fprintf(&out, `\u%04x`, ch)
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String()
}

func WriteError(message string, out *Output) int {
	return WriteFail(message, "api", out)
}

func WriteFail(message, code string, out *Output) int {
	if out.JSON || out.Quiet {
		fmt.Println(DumpJSON(NewOM("ok", false, "code", code, "error", message)))
	} else {
		fmt.Fprintln(os.Stderr, message)
	}
	return ExitStatus(code)
}

func writeJQ(payload any, out *Output) (int, error) {
	expression := out.JQ
	if expression == "" {
		expression = "."
	}
	cmd := exec.Command("jq", "-r", expression)
	cmd.Stdin = strings.NewReader(DumpJSON(payload))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if _, lookErr := exec.LookPath("jq"); lookErr != nil {
			return 0, fmt.Errorf("jq binary not found; install jq for --jq")
		}
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 4 {
			return 0, fmt.Errorf("jq failed")
		}
	}
	text := stdout.String()
	if out.TTY {
		text = terminal.SanitizeText(text)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	os.Stdout.WriteString(text)
	return 0, nil
}

func truncationNotice(truncated bool, limit *int) string {
	if !truncated {
		return ""
	}
	if limit != nil {
		return fmt.Sprintf("truncated at --limit %d", *limit)
	}
	return "truncated"
}

func noticeStderr(notice string) {
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
}

func WriteList(rows []*OM, columns [][2]string, out *Output, opts ...any) int {
	truncated := false
	var limit *int
	idKey := "id"
	maxWidths := map[string]int{}
	next := ""
	for _, optv := range opts {
		switch v := optv.(type) {
		case bool:
			truncated = v
		case *int:
			limit = v
		case map[string]int:
			maxWidths = v
		case NextPage:
			next = string(v)
		}
	}
	_ = idKey
	if out.HTML {
		return WriteFail("--html is for mailbox thread", "usage", out)
	}
	notice := truncationNotice(truncated, limit)
	if truncated && next != "" {
		notice = fmt.Sprintf("truncated; pass --page %s", next)
	}
	if out.Count {
		fmt.Println(len(rows))
		noticeStderr(notice)
		return 0
	}
	if out.IDsOnly {
		for _, row := range rows {
			fmt.Println(strOr(row.Get(idKey)))
		}
		noticeStderr(notice)
		return 0
	}
	if out.Markdown {
		printMarkdownTable(rows, columns)
		noticeStderr(notice)
		return 0
	}
	payload := NewOM("ok", true, "data", rows, "truncated", truncated)
	if notice != "" {
		payload.Set("notice", notice)
	}
	if next != "" {
		payload.Set("next_page", next)
	}
	if out.JQ != "" {
		target := any(payload)
		if out.Quiet {
			target = rows
		}
		rc, err := writeJQ(target, out)
		if err != nil {
			return WriteFail(err.Error(), "usage", out)
		}
		if out.Quiet {
			noticeStderr(notice)
		}
		return rc
	}
	if out.Quiet {
		fmt.Println(DumpJSON(rows))
		noticeStderr(notice)
		return 0
	}
	if out.JSON {
		fmt.Println(DumpJSON(payload))
		return 0
	}
	printTable(rows, columns, maxWidths)
	noticeStderr(notice)
	return 0
}

func WriteOK(data any, out *Output, notice string) int {
	if out.HTML {
		return WriteFail("--html is for mailbox thread", "usage", out)
	}
	if out.Count {
		switch data.(type) {
		case []*OM:
			fmt.Println(len(data.([]*OM)))
		default:
			fmt.Println(1)
		}
		noticeStderr(notice)
		return 0
	}
	if out.IDsOnly {
		switch d := data.(type) {
		case []*OM:
			for _, row := range d {
				fmt.Println(strOr(row.Get("id")))
			}
		case *OM:
			fmt.Println(strOr(d.Get("id")))
		}
		noticeStderr(notice)
		return 0
	}
	if out.Markdown {
		printMarkdownValue(data)
		noticeStderr(notice)
		return 0
	}
	payload := NewOM("ok", true, "data", data)
	if notice != "" {
		payload.Set("notice", notice)
	}
	if out.JQ != "" {
		target := any(payload)
		if out.Quiet {
			target = data
		}
		rc, err := writeJQ(target, out)
		if err != nil {
			return WriteFail(err.Error(), "usage", out)
		}
		return rc
	}
	if out.Quiet {
		fmt.Println(DumpJSON(data))
		noticeStderr(notice)
		return 0
	}
	if out.JSON {
		fmt.Println(DumpJSON(payload))
		return 0
	}
	switch d := data.(type) {
	case *OM:
		printKV(d)
	case []*OM:
		for _, item := range d {
			printKV(item)
			fmt.Println("---")
		}
	default:
		fmt.Println(terminal.SanitizeLine(fmt.Sprint(data)))
	}
	noticeStderr(notice)
	return 0
}

func WriteThread(messages []*OM, out *Output, truncated bool, notice string) int {
	if truncated && !out.AllowPartial {
		msg := notice
		if msg == "" {
			msg = "thread is incomplete; pass --allow-partial"
		}
		return WriteFail(msg, "api", out)
	}
	emitNotice := func() {
		if truncated && notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
	}
	if out.HTML {
		n := ""
		if truncated {
			n = notice
		}
		fmt.Print(RenderThreadHTML(messages, n))
		emitNotice()
		return 0
	}
	if out.Count {
		fmt.Println(len(messages))
		emitNotice()
		return 0
	}
	if out.IDsOnly {
		for _, row := range messages {
			fmt.Println(strOr(row.Get("id")))
		}
		emitNotice()
		return 0
	}
	if out.Markdown {
		fmt.Print(RenderThreadMarkdown(messages))
		emitNotice()
		return 0
	}
	payload := NewOM("ok", true, "data", messages, "truncated", truncated)
	if truncated && notice != "" {
		payload.Set("notice", notice)
	}
	if out.JQ != "" {
		target := any(payload)
		if out.Quiet {
			target = messages
		}
		rc, err := writeJQ(target, out)
		if err != nil {
			return WriteFail(err.Error(), "usage", out)
		}
		if out.Quiet && truncated && notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
		return rc
	}
	if out.Quiet {
		fmt.Println(DumpJSON(messages))
		emitNotice()
		return 0
	}
	if out.JSON {
		fmt.Println(DumpJSON(payload))
		return 0
	}
	for _, item := range messages {
		printKV(item)
		fmt.Println("---")
	}
	emitNotice()
	return 0
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")

func RenderThreadHTML(messages []*OM, notice string) string {
	var articles []string
	for _, msg := range messages {
		msgID := htmlEscaper.Replace(strOr(msg.Get("id")))
		header := htmlEscaper.Replace(terminal.SanitizeLine(
			strOr(msg.Get("from")) + " — " + strOr(msg.Get("date"))))
		state := htmlEscaper.Replace(strOr(msg.Get("body_state")))
		if state == "" {
			state = "hydrated"
		}
		inner := strOr(msg.Get("body_html"))
		if inner == "" {
			if body := strOr(msg.Get("body")); body != "" {
				inner = "<pre>" + htmlEscaper.Replace(body) + "</pre>"
			}
		}
		articles = append(articles,
			fmt.Sprintf(`<article id="%s" data-id="%s" data-body-state="%s"><header>%s</header>%s</article>`,
				msgID, msgID, state, header, inner))
	}
	title := "thread"
	if len(messages) > 0 {
		title = htmlEscaper.Replace(strOr(messages[0].Get("id")))
	}
	noticeHTML := ""
	if notice != "" {
		safe := strings.ReplaceAll(htmlEscaper.Replace(notice), "--", "")
		noticeHTML = fmt.Sprintf("\n<!-- notice: %s -->", safe)
	}
	return "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">" +
		fmt.Sprintf("<title>Thread %s</title></head><body>\n", title) +
		strings.Join(articles, "\n") + noticeHTML + "\n</body></html>\n"
}

func RenderThreadMarkdown(messages []*OM) string {
	var blocks []string
	for _, msg := range messages {
		who := terminal.SanitizeLine(strOr(msg.Get("from")))
		when := terminal.SanitizeLine(strOr(msg.Get("date")))
		mid := terminal.SanitizeLine(strOr(msg.Get("id")))
		parts := []string{}
		for _, p := range []string{who, when} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		heading := strings.Join(parts, " — ")
		if mid != "" {
			if heading != "" {
				heading += " (" + mid + ")"
			} else {
				heading = mid
			}
		}
		lines := []string{"## " + heading}
		subject := terminal.SanitizeLine(strOr(msg.Get("subject")))
		if subject != "" {
			lines = append(lines, "**"+subject+"**")
		}
		body := terminal.SanitizeText(strOr(msg.Get("body")))
		if body != "" {
			lines = append(lines, "", body)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

func printMarkdownTable(rows []*OM, columns [][2]string) {
	if len(rows) == 0 {
		fmt.Println("(none)")
		return
	}
	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = mdCell(c[1])
	}
	fmt.Println("| " + strings.Join(headers, " | ") + " |")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	fmt.Println("| " + strings.Join(seps, " | ") + " |")
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = mdCell(strOr(row.Get(c[0])))
		}
		fmt.Println("| " + strings.Join(cells, " | ") + " |")
	}
}

func collectKeys(rows []*OM) []string {
	var keys []string
	seen := map[string]bool{}
	for _, row := range rows {
		for _, k := range row.Keys {
			if !seen[k] {
				keys = append(keys, k)
				seen[k] = true
			}
		}
	}
	return keys
}

func printMarkdownValue(data any) {
	switch d := data.(type) {
	case []*OM:
		if len(d) == 0 {
			fmt.Println("(none)")
			return
		}
		keys := collectKeys(d)
		cols := make([][2]string, len(keys))
		for i, k := range keys {
			cols[i] = [2]string{k, k}
		}
		printMarkdownTable(d, cols)
	case *OM:
		var bodyVal any
		hasBody := false
		for _, k := range d.Keys {
			if k == "body" || k == "body_html" {
				bodyVal = d.Vals[k]
				hasBody = k == "body"
				continue
			}
			fmt.Printf("**%s:** %s\n", mdCell(k), mdCell(strOr(d.Vals[k])))
		}
		if hasBody {
			fmt.Println()
			fmt.Println(terminal.SanitizeText(strOr(bodyVal)))
		}
	default:
		fmt.Println(terminal.SanitizeLine(fmt.Sprint(data)))
	}
}

func mdCell(value string) string {
	return strings.ReplaceAll(terminal.SanitizeLine(value), "|", "\\|")
}

func printTable(rows []*OM, columns [][2]string, maxWidths map[string]int) {
	if len(rows) == 0 {
		fmt.Println("(none)")
		return
	}
	widths := make([]int, len(columns))
	for i, c := range columns {
		width := utf8.RuneCountInString(c[1])
		for _, row := range rows {
			if n := utf8.RuneCountInString(terminal.SanitizeLine(strOr(row.Get(c[0])))); n > width {
				width = n
			}
		}
		capWidth := 60
		if maxWidths != nil {
			if w, ok := maxWidths[c[0]]; ok {
				capWidth = w
			}
		}
		widths[i] = width
		if widths[i] > capWidth {
			widths[i] = capWidth
		}
	}
	headerCells := make([]string, len(columns))
	for i, c := range columns {
		headerCells[i] = pad(c[1], widths[i])
	}
	fmt.Println(strings.TrimRight(strings.Join(headerCells, "  "), " "))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cell := terminal.SanitizeLine(strOr(row.Get(c[0])))
			w := widths[i]
			if utf8.RuneCountInString(cell) > w {
				r := []rune(cell)
				cell = string(r[:w-1]) + "…"
			}
			cells[i] = pad(cell, w)
		}
		fmt.Println(strings.TrimRight(strings.Join(cells, "  "), " "))
	}
}

func pad(s string, width int) string {
	n := width - utf8.RuneCountInString(s)
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

func printKV(data *OM) {
	for _, key := range data.Keys {
		value := data.Vals[key]
		if key == "body" || key == "body_html" {
			continue
		}
		switch v := value.(type) {
		case []*OM:
			anyList := make([]any, len(v))
			for i, item := range v {
				anyList[i] = item
			}
			printKVList(key, anyList)
		case []any:
			printKVList(key, v)
		default:
			fmt.Printf("%s: %s\n", key, terminal.SanitizeLine(strOr(value)))
		}
	}
	if body, ok := data.Vals["body"]; ok {
		fmt.Println()
		fmt.Println(terminal.SanitizeText(strOr(body)))
	}
}

func strOr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return fmt.Sprintf("%d", x)
	default:
		return fmt.Sprint(x)
	}
}

// exec helpers kept tiny so tests can stub them if ever needed.
var _ = os.Stdout

func printKVList(key string, v []any) {
	fmt.Printf("%s:\n", key)
	if len(v) == 0 {
		fmt.Println("  (none)")
	}
	for _, item := range v {
		if om, ok := item.(*OM); ok {
			bits := make([]string, 0, len(om.Keys))
			for _, k := range om.Keys {
				bits = append(bits, k+"="+terminal.SanitizeLine(strOr(om.Vals[k])))
			}
			fmt.Println("  " + strings.Join(bits, "  "))
		} else {
			fmt.Println("  " + terminal.SanitizeLine(strOr(item)))
		}
	}
}
