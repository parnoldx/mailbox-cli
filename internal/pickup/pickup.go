// Package pickup recognises mail that is not read but collected: a login code
// or a magic link, worth thirty seconds and then worth nothing.
//
// The detection is deliberately lopsided. It gates on the **subject** and only
// then reads the body, because the two halves of the mail carry very different
// amounts of information:
//
//   - A subject announces the genre. An auth mail has to say what it is up
//     front, or the reader cannot find it while a login form is waiting.
//   - A body does not. Measured over 1445 real messages, every body-first
//     approach drowned: a keyword net matched 116 (the German "einmal" is an
//     ordinary word), a lone code-shaped line matched 61, digits-only 22, and
//     an opaque-token URL 15 — between them one true positive. Order
//     confirmations, booking references and BICs look exactly like one-time
//     codes from the body down.
//
// The same subject gate over the same corpus matched one message, and it was
// the right one. So the body is where the code is *extracted*, never where the
// mail is *classified*: once the subject says "verification code", a bare
// six-digit line means the code rather than an order number.
//
// The ignore list comes from OTPHelper, the Android notification reader, which
// has taken this class of false positive in the field for years.
package pickup

import "regexp"

// Keyword marks a Message the Daemon has taken a code out of. It is an IMAP
// keyword rather than a Mirror column so it survives a Mirror rebuild and both
// Daemons see it (the reasoning is ADR-0023's, applied again) — and so the
// Screener can skip it without a second source of truth.
const Keyword = "$pickup"

// subject is the gate. Noun phrases in English and German, never a lone word:
// "code" on its own is in every second order confirmation, "Einmalkennwort" is
// in none of them. Up to three intervening words are allowed after the verb so
// that "Verify your Charm™ Hyper Email" still reads as one phrase.
var subject = regexp.MustCompile(`(?i)` +
	`\b(otp|magic ?link|passcode)\b` +
	`|\bone[- ]?time[ -](code|password|passcode|link|pin)\b` +
	`|\b(verification|security|confirmation|access|login|sign[- ]?in|auth\w*)[ -](code|link|pin)\b` +
	`|\b(bestätigungs|sicherheits|verifizierungs|zugangs|anmelde|einmal)(code|link|pin|passwort|kennwort)\b` +
	`|\b(verify|confirm|validate) (your |the |dein[er]? |deine )?(\S+ ){0,3}(e-?mail|account|identity|address|login|sign[- ]?in)\b` +
	`|\b(bestätigen?|verifizieren?) (sie )?(ihre|deine|die) (\S+ ){0,3}(e-?mail|adresse|anmeldung|konto|registrierung)\b` +
	`|\b(is|ist) (your|dein|ihr) (\S+ ){0,2}code\b` +
	`|^[0-9]{4,8}\b.*\b(code|verif|login|sign)`)

// labelled is a code introduced by a word: "Your code is 123456", "PIN: 8842".
// The label is case-insensitive but the code is not — a case-insensitive
// [A-Z0-9]{4,8} matches the word "yellow", which is how the first draft of this
// reported a colour as somebody's login code.
var labelled = regexp.MustCompile(
	`(?i:\b(?:code|pin|otp|passcode|passwort|kennwort)\b)[^\p{L}\p{N}\n]{0,12}(?:(?i:is|ist|lautet)[^\p{L}\p{N}\n]{0,4})?([0-9]{4,8}|[A-Z0-9]{4,8})\b`)

// trailing is the other order, which is how most SMS-shaped mail reads:
// "123456 is your verification code".
var trailing = regexp.MustCompile(
	`\b([0-9]{4,8})\b[^\n]{0,24}?(?i:\b(?:code|pin|otp|passcode|passwort|kennwort)\b)`)

// lone is a code sitting on a line of its own, which is how a rendered HTML
// code block flattens to text. Only safe after the subject gate: on its own
// this shape is an order number far more often than it is a code.
var lone = regexp.MustCompile(`(?m)^[^\p{L}\p{N}\n]{0,4}([0-9]{4,8})[^\p{L}\p{N}\n]{0,4}$`)

// link is a URL that logs you in by being followed. The path or query has to
// name the act — a bare opaque token is every tracking and unsubscribe link
// ever sent.
var link = regexp.MustCompile(`(?i)https?://[^\s<>"'\]]*` +
	`(login|signin|sign-in|magic|verify|verifizier|confirm|bestaetig|activate|aktivier|token|auth|otp|passwordless)` +
	`[^\s<>"'\]]*`)

// ignore is the false-positive list OTPHelper learned the hard way, plus the
// German shopping equivalents. A discount code is not a login.
var ignore = regexp.MustCompile(`(?i)discount code|promo code|coupon code|barcode|encode|decode|unicode|` +
	`gutscheincode|rabattcode|aktionscode|bestellnummer|auftragsnummer|tracking`)

// Find reports the code and the login link a Pickup carries. Both may be empty
// on a mail that is not one; a caller treats "neither" as "not a Pickup", so a
// subject that reads like an auth mail but carries nothing to collect is
// correctly ignored rather than raised with nothing in it.
func Find(subject_, body string) (code, link_ string) {
	if !subject.MatchString(subject_) {
		return "", ""
	}
	if m := link.FindString(body); m != "" {
		link_ = m
	}
	code = findCode(body)
	return code, link_
}

// findCode pulls the code out of a body already known to be a Pickup, trying
// the shapes in falling order of how much the mail told us: a labelled code
// first, then a code that labels itself from the right, then a bare line.
func findCode(body string) string {
	for _, re := range []*regexp.Regexp{labelled, trailing} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if ignore.MatchString(m[0]) {
				continue
			}
			return m[1]
		}
	}
	for _, m := range lone.FindAllStringSubmatch(body, -1) {
		return m[1]
	}
	return ""
}

// Candidate says a mail's subject reads like a Pickup, whatever its body turned
// out to hold. The Daemon logs these so the phrase list above can be tuned
// against mail that actually arrives: the corpus it was built on contains
// almost no true positives, because these mails get deleted.
func Candidate(subject_ string) bool { return subject.MatchString(subject_) }
