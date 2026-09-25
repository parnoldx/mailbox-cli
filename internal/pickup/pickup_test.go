package pickup

import "testing"

// The mails a Pickup is for. The first one is from the Mirror — it was the
// single true positive in 1445 real messages, and it is the reason the subject
// gate allows words between "verify your" and "email". Real senders are
// anonymized to example.com with random tokens; the URL shapes are kept.
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
			subject:  "Verify your Acme™ Web Mail Email",
			body:     "Click to verify:\n\nhttps://example.com/auth/verify?token=pQ7vXwR4nK2mZ9bT3sY6cF1d%3D%3D\n",
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
			// Verbatim from the Screener: the compound is why this one was
			// missed — nothing in the subject starts with a code stem.
			name:     "german compound noun",
			subject:  "Ihr Doctrinus-Kontoprüfcode",
			body:     "Kontoprüfcode:\n19624929\n\nDer Code funktioniert nur für 30 Minuten.",
			wantCode: "19624929",
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
		{
			// Verbatim from the Screener: missed because the subject has
			// neither a code/link word nor an "Anmeldung ... bestätigen" pair,
			// just the bare separable verb.
			name:     "german bare sign-in verb",
			subject:  "Bei Amp anmelden",
			body:     "Sie haben eine Anmeldung bei Amp angefordert. Ihr Einmalcode lautet:\n\n638298\n\nDieser Code läuft in 10 Minuten ab.\n",
			wantCode: "638298",
		},
		{
			// Verbatim from the Screener: HTML-only mail, subject is the bare
			// noun "Anmeldung" over a preference center — no verb, no code
			// word. Body is what HTMLToMarkdown renders; the code is the lone
			// bold line.
			name:     "preference center login",
			subject:  "Ihre Anmeldung im Präferenz-Center",
			body:     "Sie haben kürzlich einen Verifizierungscode angefordert, um sich in Ihr Kommunikationspräferenzzentrum einzuloggen. Der Code ist 10 Minuten lang gültig.\n\nIhr Geheimcode zur einmaligen Verwendung :\n\n**110263**\n\nAus Sicherheitsgründen bitten wir Sie, diesen Code nicht weiterzugeben.\n",
			wantCode: "110263",
		},
		{
			// From the Inbox, 2026-09-24, sender anonymized: the subject is noun over noun
			// with no code word, so the gate stays shut — the link is the whole
			// errand and classifies the mail on its own.
			name:     "act-named link without a vouching subject",
			subject:  "Ihr Konto Login Bestätigung",
			body:     "…klicken Sie bitte auf den nachfolgenden Link, um zu Ihrem Konto zu gelangen.[Jetzt Login bestätigen](https://www.example.com/user/verify/?dec=4b7f2e91a6c3d805f19e27b4c6a8d031)",
			wantLink: true,
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
			"https://example.com/subscriptions/unsubscribe?a=N3pQxV7mK2wR9sT4",
		},
		{
			// token and auth name no act, which is why they are subject-gated:
			// without the gate this shape is every unsubscribe link ever sent.
			"unsubscribe link with a token in the query",
			"Unsere Sommerhöhe",
			"Abmelden: https://news.example.com/u/unsubscribe?token=a83bca31\n",
		},
		{
			"tracking pixel URL with auth in the path",
			"Ihre Rechnung ist da",
			"https://mail.example.com/c/auth/eyJpbWciOiJ0cmFja2luZyJ9",
		},
		{
			// "deactivate" contains "activate" as a substring; without the \b in
			// front of linkAct the provider's cancel link classified the whole
			// mail as a Pickup (a broker's trading-hours mail, 2026-09-25).
			"unsubscribe link whose path says deactivate",
			"Bevorstehende Änderungen der Handelszeiten der Märkte",
			"https://example.com/deactivate/f3a9c2e1-7b4d-4a86-9c5f-2e8b1d6a4c73?signature=a9d4f17b2c8e5036b1f9a7d4c2e8b6053f9a1d7c4e2b8f605a3d9c1e7b4f2a8d",
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

// The clipboard used to carry what the match dragged in rather than the URL:
// an HTML mail renders to Markdown as [text](url), and the regex swallowed the
// closing paren — a magic link that pasted as "…token=abc)" and failed. Same
// for a sentence mark after a plain-text URL.
func TestLinkIsTrimmedOfWrapperAndPunctuation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{
			"markdown-wrapped link",
			"Confirm your address:\n\n[Click here](https://example.com/verify?token=abc123)\n",
			"https://example.com/verify?token=abc123",
		},
		{
			"sentence punctuation",
			"Click https://example.com/verify?token=abc123. to continue.",
			"https://example.com/verify?token=abc123",
		},
		{
			"balanced paren belongs to the URL",
			"See https://en.wikipedia.org/wiki/Magic_(paranormal) for details.",
			"https://en.wikipedia.org/wiki/Magic_(paranormal)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, link := Find("Verify your email address", c.body); link != c.want {
				t.Errorf("link = %q, want %q", link, c.want)
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
