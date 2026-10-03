// `mailbox meet`: which account mints the link, what the defaults are, and the
// one shape of --start a join link cannot carry.
package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

// meetCall is what one Meet call was given, for a test to hold against.
type meetCall struct {
	account string
	subject string
	start   time.Time
	end     time.Time
}

// meetAccounts builds a Daemon whose Primary and "gmx" Secondary both make
// Teams meetings, each recording its call. calls points at the Primary's
// record; the Secondary's is the second element.
func meetAccounts(t *testing.T) (d *Daemon, primary, gmx *meetCall) {
	t.Helper()
	d, _, _ = seedTasks(t)
	primary, gmx = &meetCall{account: "primary"}, &meetCall{account: "gmx"}
	link := func(c *meetCall) func(context.Context, string, time.Time, time.Time) (string, error) {
		return func(_ context.Context, subject string, start, end time.Time) (string, error) {
			*c = meetCall{account: c.account, subject: subject, start: start, end: end}
			return "https://teams.example.com/" + c.account, nil
		}
	}
	d.Primary.Meet = link(primary)
	second := NewAccount("gmx", nil, nil, nil, nil)
	second.Meet = link(gmx)
	d.Others = append(d.Others, second)
	return d, primary, gmx
}

func TestMeetUsesTheNamedAccount(t *testing.T) {
	d, primary, gmx := meetAccounts(t)
	resp := mustAsk(t, d, []string{"meet"}, map[string]any{"positional": "Sync", "account": "gmx"})
	got := resp.Data.(map[string]any)
	if got["url"] != "https://teams.example.com/gmx" || got["subject"] != "Sync" || got["account"] != "gmx" {
		t.Fatalf("meet gave %+v", got)
	}
	if gmx.subject != "Sync" || primary.subject != "" {
		t.Errorf("the wrong account was called: primary %+v, gmx %+v", primary, gmx)
	}
}

// With no account named, one Microsoft 365 account needs no flag and several
// refuse to be guessed: the meeting would land on whichever came first, and
// whose Teams it lives on is not ours to pick.
func TestMeetPicksTheOnlyAccountOrAsks(t *testing.T) {
	d, _, _ := meetAccounts(t)
	resp := ask(t, d, []string{"meet"}, nil)
	if resp.OK || !strings.Contains(resp.Error, "--account") ||
		!strings.Contains(resp.Error, "primary") || !strings.Contains(resp.Error, "gmx") {
		t.Errorf("two accounts were not refused: %+v", resp)
	}
	d.Others = nil
	if got := mustAsk(t, d, []string{"meet"}, nil).Data.(map[string]any); got["account"] != "primary" {
		t.Errorf("the only account was not used: %+v", got)
	}
}

func TestMeetRefusesAccountsThatCannotMakeOne(t *testing.T) {
	d, _, _ := meetAccounts(t)
	d.Primary.Meet = nil
	if resp := ask(t, d, []string{"meet"}, map[string]any{"account": "primary"}); resp.OK ||
		!strings.Contains(resp.Error, "not a Microsoft 365 account") {
		t.Errorf("resp = %+v", resp)
	}
	d.Others = nil
	if resp := ask(t, d, []string{"meet"}, nil); resp.OK ||
		!strings.Contains(resp.Error, "no Microsoft 365 account") {
		t.Errorf("resp = %+v", resp)
	}
	if resp := ask(t, d, []string{"meet"}, map[string]any{"account": "nowhere"}); resp.OK ||
		!strings.Contains(resp.Error, "no account called") {
		t.Errorf("resp = %+v", resp)
	}
}

// A bare date is an all-day event's answer, and a join link has no place for
// "somewhere on that day" — so it is refused, where an event would take it.
func TestMeetNeedsATimeNotADate(t *testing.T) {
	d, _, _ := meetAccounts(t)
	for _, key := range []string{"start", "end"} {
		if resp := ask(t, d, []string{"meet"}, map[string]any{key: "2026-10-05"}); resp.OK ||
			!strings.Contains(resp.Error, "needs a time") {
			t.Errorf("--%s as a bare date was accepted: %+v", key, resp)
		}
	}
}

// Default title "Meeting", default window one hour from now.
func TestMeetDefaults(t *testing.T) {
	d, call, _ := meetAccounts(t)
	d.Others = nil
	before := time.Now().Add(-time.Minute)
	mustAsk(t, d, []string{"meet"}, nil)
	after := time.Now().Add(time.Minute)
	if call.subject != "Meeting" {
		t.Errorf("subject = %q", call.subject)
	}
	if call.start.Before(before) || call.start.After(after) {
		t.Errorf("start = %v, want about now", call.start)
	}
	if call.end.Sub(call.start) != time.Hour {
		t.Errorf("end = %v, one hour after %v", call.end, call.start)
	}
}

// A time given is the time used, end included.
func TestMeetTakesTheTimesGiven(t *testing.T) {
	d, call, _ := meetAccounts(t)
	d.Others = nil
	mustAsk(t, d, []string{"meet"}, map[string]any{
		"start": "2026-10-05 09:00", "end": "2026-10-05 10:30"})
	if !call.start.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)) ||
		!call.end.Equal(time.Date(2026, 10, 5, 10, 30, 0, 0, time.Local)) {
		t.Errorf("start %v, end %v", call.start, call.end)
	}
}

// --copy hands the link over the way a Pickup is: on the clipboard, with a
// notification saying so. Without it the desktop is left alone — piping is
// still the CLI's own way.
func TestMeetCopyHandsTheLinkOver(t *testing.T) {
	d, _, _ := meetAccounts(t)
	d.Others = nil
	var h handedOver
	h.install(t)

	resp := mustAsk(t, d, []string{"meet"}, map[string]any{"positional": "Sync", "copy": true})
	got := resp.Data.(map[string]any)
	if c := h.arg("wl-copy"); len(c) != 1 || c[0] != got["url"] {
		t.Errorf("clipboard got %v, want the minted link %v", c, got["url"])
	}
	if n := h.arg("notify-send"); len(n) == 0 || !strings.Contains(strings.Join(n, " "), "Sync") {
		t.Errorf("notification %v does not name the meeting", n)
	}

	h.calls = nil
	mustAsk(t, d, []string{"meet"}, map[string]any{"positional": "Sync"})
	if len(h.calls) != 0 {
		t.Errorf("without --copy the desktop was touched: %v", h.calls)
	}
}
