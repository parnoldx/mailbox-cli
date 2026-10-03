// The meet printer: the join link alone on stdout, so it pipes.
package cli

import (
	"bytes"
	"testing"

	"mailbox/internal/daemon"
)

func TestMeetPrintsJustTheLink(t *testing.T) {
	var out, errs bytes.Buffer
	printMeet(&out, &errs, daemon.Response{OK: true, Data: map[string]any{
		"url": "https://teams.example.com/x", "subject": "Sync", "account": "work"}})
	if out.String() != "https://teams.example.com/x\n" {
		t.Errorf("printed %q", out.String())
	}
	if errs.String() != "" {
		t.Errorf("printed %q on stderr", errs.String())
	}
}
