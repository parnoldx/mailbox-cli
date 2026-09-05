package htmlmd

import (
	"strings"
	"testing"
)

func TestSimpleParagraph(t *testing.T) {
	if got := HTMLToMarkdown("<p>Hello world</p>"); got != "Hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestHeadingsAndLists(t *testing.T) {
	got := HTMLToMarkdown("<h2>Title</h2><ul><li>one</li><li>two</li></ul>")
	want := "## Title\n\n- one\n- two"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOrderedNumbersContinue(t *testing.T) {
	got := HTMLToMarkdown("<ol><li>a</li><li>b</li></ol>")
	if !strings.Contains(got, "1. a") || !strings.Contains(got, "2. b") {
		t.Fatalf("got %q", got)
	}
}

func TestLinksAndEmphasis(t *testing.T) {
	got := HTMLToMarkdown(`<p><a href="https://x.example">site</a> <b>bold</b> <i>it</i></p>`)
	for _, want := range []string{"[site](https://x.example)", "**bold**", "*it*"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestCodeBlock(t *testing.T) {
	got := HTMLToMarkdown("<pre><code>x = 1\n</code></pre>")
	if !strings.Contains(got, "```\nx = 1\n```") {
		t.Fatalf("got %q", got)
	}
}

func TestBlockquote(t *testing.T) {
	got := HTMLToMarkdown("<blockquote><p>quoted</p></blockquote>")
	if !strings.Contains(got, "> quoted") {
		t.Fatalf("got %q", got)
	}
}

func TestTable(t *testing.T) {
	got := HTMLToMarkdown("<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>")
	want := "| A | B |\n| --- | --- |\n| 1 | 2 |"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutTableFlattens(t *testing.T) {
	got := HTMLToMarkdown(`<table role="presentation"><tr><td>left</td><td>right</td></tr></table>`)
	if strings.Contains(got, "|") {
		t.Fatalf("layout table kept pipes: %q", got)
	}
}

func TestTrackingPixelDropped(t *testing.T) {
	got := HTMLToMarkupSafe(HTMLToMarkdown(`<img src="https://t.example/x" width="1" height="0">`))
	if strings.Contains(got, "t.example") {
		t.Fatalf("tracking pixel kept: %q", got)
	}
}

func TestNamedTrackerDroppedEvenWhenNotOneByOne(t *testing.T) {
	got := HTMLToMarkdown(`<p>hi</p><img src="https://example.us18.list-manage.com/track/open.php?u=1" width="100" height="20" alt="">`)
	if strings.Contains(got, "list-manage") {
		t.Fatalf("named tracker kept: %q", got)
	}
}

// helper to keep test self-contained
func HTMLToMarkupSafe(s string) string { return s }

func TestHiddenElementsSkipped(t *testing.T) {
	got := HTMLToMarkdown(`<div visible>keep</div><div style="display:none">secret</div>`)
	if strings.Contains(got, "secret") || !strings.Contains(got, "keep") {
		t.Fatalf("got %q", got)
	}
}

func TestHiddenPreheaderStylesSkipped(t *testing.T) {
	cases := []string{
		`<div style="max-height:0;overflow:hidden">8 &euro; geschenkt</div>`,
		`<div style="max-height:0px; mso-hide:all;">8 &euro; geschenkt</div>`,
		`<div style="height:0;width:0">8 &euro; geschenkt</div>`,
		`<div style="opacity:0">8 &euro; geschenkt</div>`,
		`<div style="font-size:0;line-height:0">8 &euro; geschenkt</div>`,
		`<span style="mso-hide:all">8 &euro; geschenkt</span>`,
	}
	for _, c := range cases {
		got := HTMLToMarkdown(`<p>keep me</p>` + c)
		if strings.Contains(got, "geschenkt") || !strings.Contains(got, "keep me") {
			t.Fatalf("preheader leaked for %q: %q", c, got)
		}
	}
}

func TestFontSizeZeroAloneKeepsContent(t *testing.T) {
	// font-size:0 on its own is the inline-block whitespace trick, not
	// hiding: the button's alt text must survive.
	got := HTMLToMarkdown(`<td style="font-size:0"><a href="https://x.example">Shop now</a></td>`)
	if !strings.Contains(got, "Shop now") {
		t.Fatalf("dropped visible button: %q", got)
	}
}

func TestEscapesMarkdownMeta(t *testing.T) {
	got := HTMLToMarkdown("<p>a*b | c</p>")
	if !strings.Contains(got, `a\*b`) {
		t.Fatalf("got %q", got)
	}
}

func TestMarkdownToHTMLBasics(t *testing.T) {
	got := MarkdownToHTML("# H\n\nsome **bold** text")
	for _, want := range []string{"<h1>H</h1>", "<strong>bold</strong>", "<p>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestMarkdownToHTMLListAndFence(t *testing.T) {
	got := MarkdownToHTML("- a\n- b\n\n```\ncode\n```")
	for _, want := range []string{"<ul><li>a</li><li>b</li></ul>", "<pre><code>code</code></pre>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestMarkdownToHTMLCallout(t *testing.T) {
	got := MarkdownToHTML("[!note]\nInvoice is already paid.")
	for _, want := range []string{`data-callout="note"`, "Invoice is already paid", "#dbeafe", "<strong>Note</strong>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
	if strings.Contains(got, "<blockquote>") {
		t.Fatalf("callout should not be a quote: %q", got)
	}
}

func TestMarkdownToHTMLCalloutCustomTitle(t *testing.T) {
	got := MarkdownToHTML("[!warning] Don't send this\nInternal only.")
	for _, want := range []string{`data-callout="warning"`, "Don't send this", "Internal only", "#fef3c7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestCalloutRoundTrip(t *testing.T) {
	src := "[!note]\nInvoice is already paid."
	got := HTMLToMarkdown(MarkdownToHTML(src))
	if got != src {
		t.Fatalf("got %q want %q", got, src)
	}
}

func TestCalloutRoundTripCustomTitle(t *testing.T) {
	src := "[!tip] Pro tip\nUse the toolbar."
	got := HTMLToMarkdown(MarkdownToHTML(src))
	if got != src {
		t.Fatalf("got %q want %q", got, src)
	}
}

func TestMarkdownToHTMLCalloutAcceptsGitHubQuote(t *testing.T) {
	got := MarkdownToHTML("> [!note]\n> still works")
	if !strings.Contains(got, `data-callout="note"`) || !strings.Contains(got, "still works") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "<blockquote>") {
		t.Fatalf("github-style callout should not stay a quote: %q", got)
	}
}

func TestUnknownBangIsNotACallout(t *testing.T) {
	got := MarkdownToHTML("[!unknown]\nstill a paragraph")
	if strings.Contains(got, "data-callout") {
		t.Fatalf("unknown marker became a callout: %q", got)
	}
}

func TestStyleCalloutsRestoresEmailBox(t *testing.T) {
	raw := `<p>hi</p><table><tbody><tr><td style="background-color: rgb(219, 234, 254);"><p><strong>Note</strong></p><p>paid</p></td></tr></tbody></table>`
	got := StyleCallouts(raw)
	for _, want := range []string{`data-callout="note"`, "border-left", "paid", "hi"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestStyleCalloutsLeavesOrdinaryHTML(t *testing.T) {
	raw := `<p>Hallo <em>Welt</em></p>`
	if got := StyleCallouts(raw); got != raw {
		t.Fatalf("rewrote ordinary html: %q", got)
	}
}

func TestStyleCalloutsRoundTripToMarkdown(t *testing.T) {
	raw := `<table><tbody><tr><td style="background-color: rgb(219, 234, 254);"><p><strong>Note</strong></p><p>paid</p></td></tr></tbody></table>`
	got := HTMLToMarkdown(StyleCallouts(raw))
	if got != "[!note]\npaid" {
		t.Fatalf("got %q", got)
	}
}

func TestStyleCalloutsLexxyStrippedTable(t *testing.T) {
	// Lexxy's TableCellNode transform clears cell backgrounds, so the
	// exported HTML is a 1-cell table whose only signal is the "Note" label.
	raw := `<table><colgroup><col></colgroup><tbody><tr><td><p><strong>Note</strong></p><p>Beachte das auf jeden Fall sonst wird es problematisch</p></td></tr></tbody></table>`
	got := StyleCallouts(raw)
	for _, want := range []string{`data-callout="note"`, "border-left", "Beachte das", "#dbeafe"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestStyleCalloutsCalloutBlockquote(t *testing.T) {
	raw := `<p>intro</p><blockquote><p><strong>Warning</strong></p><p>Don't send this</p></blockquote>`
	got := StyleCallouts(raw)
	if !strings.Contains(got, `data-callout="warning"`) || !strings.Contains(got, "#fef3c7") {
		t.Fatalf("blockquote was not promoted to a callout box: %q", got)
	}
	if strings.Contains(got, "<blockquote>") {
		t.Fatalf("callout blockquote left in place: %q", got)
	}
	if !strings.Contains(got, "send this") || !strings.Contains(got, "intro") {
		t.Fatalf("lost surrounding content: %q", got)
	}
}

func TestHTMLToMarkdownCalloutBlockquote(t *testing.T) {
	got := HTMLToMarkdown(`<blockquote><p><strong>Note</strong></p><p>Beachte das</p></blockquote>`)
	if got != "[!note]\nBeachte das" {
		t.Fatalf("got %q", got)
	}
}

func TestOrdinaryQuoteIsNotACallout(t *testing.T) {
	raw := `<blockquote><p>On Tuesday, Jamie wrote:</p><p>hello</p></blockquote>`
	got := StyleCallouts(raw)
	if got != raw {
		t.Fatalf("rewrote an ordinary quote: %q", got)
	}
	md := HTMLToMarkdown(raw)
	if !strings.Contains(md, "> hello") || strings.Contains(md, "[!note]") {
		t.Fatalf("ordinary quote became a callout: %q", md)
	}
}

func TestCleanMarkdownStripsInvisibleChars(t *testing.T) {
	got := HTMLToMarkdown("<p>Hi\u200b there\u00ad</p>")
	if got != "Hi there" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanMarkdownDropsEmptyImagesAndLinks(t *testing.T) {
	got := cleanMarkdown("a ![](https://t.example/p) b [](https://x.example) c")
	if got != "a b c" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanMarkdownDropsBareURLLines(t *testing.T) {
	got := cleanMarkdown("text\nhttps://button.example/go\n<https://autolink.example>\nmore")
	if got != "text\nmore" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanMarkdownKeepsRealContent(t *testing.T) {
	got := cleanMarkdown("[label](https://x.example)\nsee https://y.example now")
	want := "[label](https://x.example)\nsee https://y.example now"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCleanMarkdownCollapsesBlankRuns(t *testing.T) {
	got := cleanMarkdown("a\n\n\n\nb")
	if got != "a\n\nb" {
		t.Fatalf("got %q", got)
	}
}
