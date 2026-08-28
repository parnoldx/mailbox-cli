package mail

import (
	"strings"
	"testing"
)

func TestSanitizePlainTextStripsPreheaderEntitySpam(t *testing.T) {
	// Shape of Lieferando's botched text/plain alternative: a headline,
	// then a wall of &nbsp;/&zwnj; spacer entities left as literal text.
	in := "Lieferando\n \n" +
		strings.Repeat("  &nbsp;&zwnj;&nbsp;&zwnj;&nbsp;&zwnj;&nbsp;&zwnj;\n", 20) +
		"\nEs ist Zeit zu sparen\n"
	got := sanitizePlainText(in)
	for _, bad := range []string{"zwnj", "&nbsp;", "‌", " "} {
		if strings.Contains(got, bad) {
			t.Fatalf("leaked %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "Lieferando") || !strings.Contains(got, "Es ist Zeit zu sparen") {
		t.Fatalf("dropped real content: %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("blank runs not collapsed: %q", got)
	}
}

func TestSanitizePlainTextLeavesCleanTextAlone(t *testing.T) {
	in := "Hi Peter,\n\nMeeting at 3pm about R&D budget. Tom & Jerry will join.\n\nThanks"
	if got := sanitizePlainText(in); got != in {
		t.Fatalf("mangled clean text:\n in:  %q\n got: %q", in, got)
	}
}
