package mail

import (
	"regexp"
	"strings"
)

var listNameRe = regexp.MustCompile(`^\(.*\) "(?:[^"]*)" (?:"(.*)"|(\S+))\s*$`)

// ListFolderName extracts the folder name from an IMAP LIST line.
func ListFolderName(line string) string {
	trimmed := strings.TrimSpace(line)
	if m := listNameRe.FindStringSubmatch(trimmed); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	if strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`) {
		return line[1 : len(line)-1]
	}
	i := strings.LastIndex(line, " ")
	out := strings.Trim(line[i+1:], `"`)
	return out
}
