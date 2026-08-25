package ids

import (
	"fmt"
	"strings"

	"mailbox/src/internal/folders"
)

// Display aliases for id formatting; anything not listed shows its full
// IMAP folder name (Archive/… stays fully qualified).
var displayAlias = map[string]string{
	folders.FEED:        "feed",
	folders.PAPER_TRAIL: "trail",
	folders.SCREENER:    "screener",
	folders.BLOCK:       "block",
	folders.DRAFTS:      "drafts",
	folders.SENT:        "sent",
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseMessageID accepts bare uid (INBOX), alias:uid (feed:12, trail:3,
// Archive/Immo:4) and full folder:uid.
func ParseMessageID(value string) (string, string, error) {
	v := strings.TrimSpace(value)
	if isDigits(v) {
		return folders.INBOX, v, nil
	}
	i := strings.LastIndex(v, ":")
	if i < 0 {
		return "", "", fmt.Errorf("message id must be [box:]uid, got %q", value)
	}
	uid := strings.TrimSpace(v[i+1:])
	if !isDigits(uid) {
		return "", "", fmt.Errorf("message id must be [box:]uid, got %q", value)
	}
	folder, err := folders.ResolveFolder(strings.TrimSpace(v[:i]), nil)
	if err != nil {
		return "", "", fmt.Errorf("message id must be [box:]uid, got %q", value)
	}
	return folder, uid, nil
}

func FormatMessageID(folder, uid string) string {
	if folder == folders.INBOX {
		return uid
	}
	if alias, ok := displayAlias[folder]; ok {
		return alias + ":" + uid
	}
	return folder + ":" + uid
}

func ParseAttachmentID(value string) (string, string, int, error) {
	i := strings.LastIndex(value, ":")
	if i < 0 {
		return "", "", 0, fmt.Errorf("attachment id must be [box:]uid:index, got %q", value)
	}
	indexStr := strings.TrimSpace(value[i+1:])
	n := 0
	if !isDigits(indexStr) {
		return "", "", 0, fmt.Errorf("attachment id must be [box:]uid:index, got %q", value)
	}
	fmt.Sscanf(indexStr, "%d", &n)
	if n < 1 {
		return "", "", 0, fmt.Errorf("attachment id must be [box:]uid:index, got %q", value)
	}
	folder, uid, err := ParseMessageID(value[:i])
	if err != nil {
		return "", "", 0, fmt.Errorf("attachment id must be [box:]uid:index, got %q", value)
	}
	return folder, uid, n, nil
}

func FormatAttachmentID(folder, uid string, index int) string {
	return FormatMessageID(folder, uid) + ":" + fmt.Sprint(index)
}
