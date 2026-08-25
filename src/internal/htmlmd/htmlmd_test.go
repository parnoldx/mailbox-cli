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

// helper to keep test self-contained
func HTMLToMarkupSafe(s string) string { return s }

func TestHiddenElementsSkipped(t *testing.T) {
	got := HTMLToMarkdown(`<div visible>keep</div><div style="display:none">secret</div>`)
	if strings.Contains(got, "secret") || !strings.Contains(got, "keep") {
		t.Fatalf("got %q", got)
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
