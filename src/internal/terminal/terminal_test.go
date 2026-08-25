package terminal

import "testing"
import "strings"

func TestSanitizeLineStripsEscapesAndNewlines(t *testing.T) {
	raw := "hi\x1b[31mred\x1b[0m\nthere"
	if got := SanitizeLine(raw); got != "hired there" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeTextKeepsNewlines(t *testing.T) {
	if SanitizeText("a\nb") != "a\nb" {
		t.Fatal("newline lost")
	}
	if strings.Contains(SanitizeText("a\x1b[0mb"), "\x1b") {
		t.Fatal("escape kept")
	}
}

func TestSanitizeTextKeepsJoiningSequences(t *testing.T) {
	if SanitizeText("👨\u200d👩\u200d👧") != "👨\u200d👩\u200d👧" {
		t.Fatalf("family: %q", SanitizeText("👨\u200d👩\u200d👧"))
	}
	if SanitizeText("می\u200cخواهم") != "می\u200cخواهم" {
		t.Fatalf("zwnj word: %q", SanitizeText("می\u200cخواهم"))
	}
}

func TestSanitizeTextDropsJoinersNextToASCII(t *testing.T) {
	if SanitizeText("pay\u200dpal") != "paypal" {
		t.Fatalf("got %q", SanitizeText("pay\u200dpal"))
	}
	if SanitizeText("\u200dRyan\u200d") != "Ryan" {
		t.Fatalf("got %q", SanitizeText("\u200dRyan\u200d"))
	}
}

func TestSanitizeTextCapsCombiningMarks(t *testing.T) {
	in := "Z" + strings.Repeat("\u0336", 50) + "algo"
	want := "Z" + strings.Repeat("\u0336", 8) + "algo"
	if SanitizeText(in) != want {
		t.Fatalf("got %q", SanitizeText(in))
	}
}
