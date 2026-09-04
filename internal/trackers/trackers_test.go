package trackers

import "testing"

func TestIdentifyNamesKnownPixels(t *testing.T) {
	for _, tc := range []struct {
		url, want string
	}{
		{"https://example.us18.list-manage.com/track/open.php?u=abc", "Mailchimp"},
		{"https://u123.ct.sendgrid.net/wf/open?upn=xyz", "SendGrid"},
		{"https://t.hubspotemail.net/e2t/o/foo", "HubSpot"},
		{"https://cdn.example.com/logo.png", ""},
		{"", ""},
	} {
		if got := Identify(tc.url); got != tc.want {
			t.Errorf("Identify(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestInHTMLReportsNamedAndGenericPixels(t *testing.T) {
	html := `<p>hi</p>
<img src="https://example.us18.list-manage.com/track/open.php?u=1" width="1" height="1">
<img src="https://t.example/x" width="1" height="0">
<img src="https://cdn.example.com/hero.png" width="600" height="200">`
	got := InHTML(html)
	if len(got) != 2 || got[0] != "Mailchimp" || got[1] != "tracker" {
		t.Fatalf("InHTML = %v, want [Mailchimp tracker]", got)
	}
}

func TestInHTMLDedupes(t *testing.T) {
	html := `<img src="https://list-manage.com/track/a">
<img src="https://list-manage.com/track/b">`
	got := InHTML(html)
	if len(got) != 1 || got[0] != "Mailchimp" {
		t.Fatalf("InHTML = %v", got)
	}
}
