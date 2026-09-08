package pickup

import "testing"

// The mails a Pickup is for. The Charm one is verbatim from the Mirror — it was
// the single true positive in 1445 real messages, and it is the reason the
// subject gate allows words between "verify your" and "email".
func TestFindsCodesAndLinks(t *testing.T) {
	cases := []struct {
		name     string
		subject  string
		body     string
		wantCode string
		wantLink bool
	}{
		{
			name:     "magic link, real mail",
			subject:  "Verify your Charm™ Hyper Email",
			body:     "Click to verify:\n\nhttps://hyper.charm.land/auth/verify?token=y02NzwzMVMWNmniK1mmFRQ%3D%3D\n",
			wantLink: true,
		},
		{
			name:     "labelled code",
			subject:  "Your verification code",
			body:     "Hi,\n\nYour code is 481920.\n\nIt expires in 10 minutes.",
			wantCode: "481920",
		},
		{
			name:     "code first, subject shaped like an SMS",
			subject:  "739104 is your login code",
			body:     "739104 is your verification code. Do not share it.",
			wantCode: "739104",
		},
		{
			name:     "german, code on its own line",
			subject:  "Ihr Bestätigungscode",
			body:     "Guten Tag,\n\n  552019  \n\nDieser Code ist 5 Minuten gültig.",
			wantCode: "552019",
		},
		{
			name:     "german verb phrase",
			subject:  "Bitte bestätigen Sie Ihre E-Mail-Adresse",
			body:     "Zum Aktivieren: https://example.de/konto/aktivieren/8f2ad91c4b\n",
			wantLink: true,
		},
		{
			name:     "alphanumeric code stays uppercase",
			subject:  "Security code",
			body:     "Passcode: 9F4KQ2\n",
			wantCode: "9F4KQ2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, link := Find(c.subject, c.body)
			if code != c.wantCode {
				t.Errorf("code = %q, want %q", code, c.wantCode)
			}
			if (link != "") != c.wantLink {
				t.Errorf("link = %q, want any: %v", link, c.wantLink)
			}
		})
	}
}

// What the subject gate exists to keep out. Every one of these bodies carries
// something a body-first detector reported as a login code, and the measurement
// that produced them is described in the package comment.
func TestIgnoresMailThatIsNotAPickup(t *testing.T) {
	cases := []struct{ name, subject, body string }{
		{
			"order confirmation with a lone order number",
			"Vielen Dank für deine Bestellung",
			"Bestellnummer:\n\n  2818304  \n\nVersand in 2 Tagen.",
		},
		{
			"booking reference that looks like a code",
			"Buchungsbestätigung DHSMSA",
			"Ihr Buchungscode lautet 70563. Gute Reise!",
		},
		{
			"newsletter that happens to say einmal",
			"Heilung braucht Widerstand",
			"Man muss das einmal ausprobieren. Code: 123456",
		},
		{
			"unsubscribe link with an opaque token",
			"Smart Home – neu gestaltet",
			"https://manage.kmail-lists.com/subscriptions/unsubscribe?a=Kx8Tq2mV9pLd0aZr",
		},
		{
			"discount code in a shop mail",
			"Dein Gutscheincode wartet",
			"Dein Rabattcode: SPAR20",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if code, link := Find(c.subject, c.body); code != "" || link != "" {
				t.Errorf("Find() = (%q, %q), want empty: this is not a Pickup", code, link)
			}
		})
	}
}

// A subject that reads like an auth mail but carries nothing to collect is not
// a Pickup: raising a notification with no code in it is worse than silence.
func TestSubjectAloneIsNotEnough(t *testing.T) {
	if code, link := Find("Your login code", "Sorry, we could not send it. Please try again."); code != "" || link != "" {
		t.Fatalf("Find() = (%q, %q), want empty", code, link)
	}
	if !Candidate("Your login code") {
		t.Fatal("Candidate() = false, want true: it should still be logged for tuning")
	}
}

// A case-insensitive [A-Z0-9]{4,8} matches ordinary words. This is the guard
// for that: "code" followed by prose must not yield a word as the code.
func TestDoesNotReportAWordAsACode(t *testing.T) {
	code, _ := Find("Verification code", "Your code is on the yellow card we posted to you.")
	if code == "yellow" {
		t.Fatal(`code = "yellow": the code alternative must stay case-sensitive`)
	}
}
