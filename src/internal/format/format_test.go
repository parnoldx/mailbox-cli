package format

import (
	"strings"
	"testing"

	_ "mailbox/src/internal/terminal"
)

func TestJSONEscapesC1(t *testing.T) {
	text := DumpJSON(NewOM("from", "x\u009bOSC"))
	if strings.Contains(text, "\u009b") {
		t.Fatalf("raw C1 leaked: %q", text)
	}
	if !strings.Contains(text, `\u009b`) {
		t.Fatalf("expected escaped C1: %q", text)
	}
}

func TestOMKeyOrder(t *testing.T) {
	row := NewOM("id", "1", "from", "a", "summary", "s")
	got := DumpJSON(row)
	wantOrder := `"id"` < `"from"`
	_ = wantOrder
	idxID := strings.Index(got, `"id"`)
	idxFrom := strings.Index(got, `"from"`)
	if idxID > idxFrom {
		t.Fatalf("key order broken: %s", got)
	}
}

func TestTakeOutputFlags(t *testing.T) {
	kept, out, err := TakeOutputFlags([]string{"--json", "box", "list"})
	if err != nil || !out.JSON || len(kept) != 2 {
		t.Fatalf("kept=%v out=%+v err=%v", kept, out, err)
	}
	if _, _, err := TakeOutputFlags([]string{"--json", "--ids-only"}); err == nil {
		t.Fatal("expected exclusive error")
	}
	if _, _, err := TakeOutputFlags([]string{"--jq"}); err == nil {
		t.Fatal("expected jq expression error")
	}
	if _, out, err := TakeOutputFlags([]string{"--jq=.data[0]"}); err != nil || out.JQ != ".data[0]" {
		t.Fatalf("jq= form failed: %+v %v", out, err)
	}
	if _, _, err := TakeOutputFlags([]string{"--quiet", "--count"}); err == nil {
		t.Fatal("quiet+count should fail")
	}
}
