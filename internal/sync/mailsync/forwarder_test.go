package mailsync

import (
	"strings"
	"testing"
)

func TestStripForwarderHeader(t *testing.T) {
	html := `<table><tr><td><div>This email was sent to <span>capital@pa.unbox.at</span> (Created automatically by catch-all) from <span>support@mailer.capital.com</span><br>Click <a href="https://app.unbox.at/deactivate/b1f7f44f?signature=abc">here</a> to deactivate this alias</div></td></tr></table><p>Hello, trading hours changed.</p>`
	got := StripForwarderHeader(html)
	if strings.Contains(got, "deactivate") || strings.Contains(got, "This email was sent to") {
		t.Errorf("StripForwarderHeader(html) kept forwarder boilerplate: %q", got)
	}
	if !strings.Contains(got, "trading hours changed") {
		t.Errorf("StripForwarderHeader(html) lost the real body: %q", got)
	}

	plain := "This email was sent to capital@pa.unbox.at (Created automatically by catch-all) from support@mailer.capital.com\nClick here to deactivate this alias\n\nHello, trading hours changed."
	got = StripForwarderHeader(plain)
	if strings.Contains(got, "deactivate") || strings.Contains(got, "unbox.at") {
		t.Errorf("StripForwarderHeader(plain) kept forwarder boilerplate: %q", got)
	}
	if !strings.Contains(got, "trading hours changed") {
		t.Errorf("StripForwarderHeader(plain) lost the real body: %q", got)
	}

	if got := StripForwarderHeader("ordinary mail, no boilerplate"); got != "ordinary mail, no boilerplate" {
		t.Errorf("ordinary mail changed: %q", got)
	}
	if got := StripForwarderHeader(""); got != "" {
		t.Errorf("empty input changed: %q", got)
	}
}
