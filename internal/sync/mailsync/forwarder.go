package mailsync

import "regexp"

// The pa.unbox.at alias forwarder prepends this boilerplate to every mail it
// forwards: "This email was sent to X@pa.unbox.at (Created automatically by
// catch-all) from Y … Click here to deactivate this alias", with a deactivate
// link at app.unbox.at/deactivate/…. It is transport metadata from the
// forwarder, not something the sender wrote, so it is stripped before the text
// is stored — it must never reach classification (pickup), the search index,
// or the reader as if it were part of the mail.
var forwarderHeader = regexp.MustCompile(`(?s)This email was sent to .*?unbox\.at.*?deactivate this alias`)

// StripForwarderHeader removes the unbox alias-forwarder boilerplate.
func StripForwarderHeader(s string) string {
	if s == "" {
		return s
	}
	return forwarderHeader.ReplaceAllString(s, "")
}
