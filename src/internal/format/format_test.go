package format

import (
	"fmt"
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
	if _, out, err := TakeOutputFlags([]string{"--styled", "box"}); err != nil || !out.Styled {
		t.Fatalf("styled: %+v %v", out, err)
	}
}

func TestApplyDefaultFormatPipesJSON(t *testing.T) {
	_, out, err := TakeOutputFlags([]string{"box", "list"})
	if err != nil {
		t.Fatal(err)
	}
	ApplyDefaultFormat(out, false)
	if !out.JSON {
		t.Fatal("piped default should be JSON")
	}
	_, out, _ = TakeOutputFlags([]string{"--styled", "box"})
	ApplyDefaultFormat(out, false)
	if out.JSON {
		t.Fatal("--styled keeps human output")
	}
	_, out, _ = TakeOutputFlags([]string{"box"})
	ApplyDefaultFormat(out, true)
	if out.JSON {
		t.Fatal("TTY default should stay human")
	}
}

func TestPageSlice(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	got, next, trunc := PageSlice(items, 1, 2, false)
	if len(got) != 2 || got[0] != 1 || next != "2" || !trunc {
		t.Fatalf("page1 %v next=%s trunc=%v", got, next, trunc)
	}
	got, next, trunc = PageSlice(items, 3, 2, false)
	if len(got) != 1 || got[0] != 5 || next != "" || trunc {
		t.Fatalf("page3 %v next=%s trunc=%v", got, next, trunc)
	}
	got, next, trunc = PageSlice(items, 1, 2, true)
	if len(got) != 5 || next != "" || trunc {
		t.Fatalf("all %v", got)
	}
}

func TestExitStatus(t *testing.T) {
	if ExitStatus("usage") != 1 || ExitStatus("not_found") != 2 || ExitStatus("auth") != 3 || ExitStatus("api") != 7 {
		t.Fatal(ExitStatus("usage"), ExitStatus("not_found"), ExitStatus("auth"), ExitStatus("api"))
	}
	if Classify(fmt.Errorf("event not found: x")) != "not_found" {
		t.Fatal(Classify(fmt.Errorf("event not found: x")))
	}
}
