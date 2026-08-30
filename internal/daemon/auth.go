package daemon

import "strings"

// looksLikeAuth says whether a server refused the credentials rather than
// failed some other way. There is no typed error to match on: IMAP, SMTP and
// DAV each say it differently, and the text is what they agree on.
//
// A wrong guess in one direction is a notification nobody needed; in the other
// it is silence while mail stops arriving. The strings here are the ones the
// three protocols actually use.
func looksLikeAuth(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, mark := range []string{
		"authenticationfailed", "authentication failed", "authenticate failed",
		"invalid credentials", "login failed", "bad credentials",
		"535 ", "534 ", "401 ", "403 ", "unauthorized", "permission denied",
	} {
		if strings.Contains(text, mark) {
			return true
		}
	}
	return false
}
