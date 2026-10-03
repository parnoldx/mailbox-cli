// `mailbox meet`: a Teams meeting with no calendar entry, and the one error it
// can hit that a fresh sign-in fixes.
package graphdrv

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMeetPostsAndReturnsTheJoinLink(t *testing.T) {
	f, c := newFakeGraph(t)
	start := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	url, err := c.Meet(context.Background(), "Standup", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://teams.example.com/") {
		t.Errorf("join link = %q", url)
	}
	w := f.writes()
	if len(w) != 1 {
		t.Fatalf("wrote %v", w)
	}
	if !strings.HasPrefix(w[0], "POST /me/onlineMeetings ") {
		t.Errorf("wrote %q", w[0])
	}
	for _, field := range []string{`"subject":"Standup"`, `"startDateTime":"2026-10-05T09:00:00Z"`, `"endDateTime":"2026-10-05T10:00:00Z"`} {
		if !strings.Contains(w[0], field) {
			t.Errorf("%s missing from %q", field, w[0])
		}
	}
}

// The token on disk was minted before OnlineMeetings.ReadWrite was asked for,
// so a 403 here means the sign-in, not the meeting, is the problem.
func TestMeetSaysSignInAgainOnARefusal(t *testing.T) {
	f, c := newFakeGraph(t)
	f.failPost = 1
	_, err := c.Meet(context.Background(), "Standup", time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "OnlineMeetings.ReadWrite") ||
		!strings.Contains(err.Error(), "mailbox setup") {
		t.Errorf("err = %v, want the sign-in-again advice", err)
	}
}
