// `mailbox meet`: a Teams link with no calendar entry. A link somebody wants
// right now is not an appointment, so nothing is written to any calendar —
// which also means there is nothing to edit or delete afterwards; the meeting
// lives on the Teams server alone.
package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// handleMeet mints a Teams meeting and reports its join link. Only a
// Microsoft 365 account can make one, so the choice of account is the choice
// of whose Teams the meeting lives on — and with several of them, guessing is
// refused rather than the meeting landing on whichever came first.
func (d *Daemon) handleMeet(ctx context.Context, req Request, resp Response) Response {
	title := strings.TrimSpace(req.Str("positional"))
	if title == "" {
		title = "Meeting"
	}
	copyIt := req.Bool("copy")
	// A meeting needs a time: a bare date says "somewhere on that day", which
	// a join link cannot carry, so it is refused where an all-day event would
	// take it.
	start, startDay, err := eventTime(req, "start")
	if err != nil {
		return resp.usage(err.Error())
	}
	if start.IsZero() {
		start = time.Now()
	} else if startDay {
		return resp.usage("a meeting needs a time: --start takes 2026-09-01 14:00, not a bare date")
	}
	end, endDay, err := eventTime(req, "end")
	if err != nil {
		return resp.usage(err.Error())
	}
	if end.IsZero() {
		end = start.Add(time.Hour)
	} else if endDay {
		return resp.usage("a meeting needs a time: --end takes 2026-09-01 15:00, not a bare date")
	}

	if name := req.Str("account"); name != "" {
		acct, err := d.accountNamed(name)
		if err != nil {
			return resp.usage(err.Error())
		}
		if acct.Meet == nil {
			return resp.usage(fmt.Sprintf("%s is not a Microsoft 365 account", acct.Name))
		}
		return d.meetOn(ctx, acct, title, start, end, copyIt, resp)
	}
	var with []*Account
	for _, a := range d.accounts() {
		if a.Meet != nil {
			with = append(with, a)
		}
	}
	switch len(with) {
	case 0:
		return resp.usage("no Microsoft 365 account")
	case 1:
		return d.meetOn(ctx, with[0], title, start, end, copyIt, resp)
	}
	names := make([]string, 0, len(with))
	for _, a := range with {
		names = append(names, a.Name)
	}
	return resp.usage(fmt.Sprintf(
		"several Microsoft 365 accounts: %s — name one with --account", strings.Join(names, ", ")))
}

// meetOn calls the account's Meet and reports what came back. The link is the
// whole point of the command, so it is the reply's first field and the only
// one the plain printer shows. With copy it also hands the link over the way a
// Pickup is handed over — on the clipboard, with a notification saying so —
// which is what a GUI's "meet link" button asks for: the hand-over is the
// daemon's desktop plumbing, not every client's to reimplement.
func (d *Daemon) meetOn(ctx context.Context, acct *Account, title string, start, end time.Time, copyIt bool, resp Response) Response {
	url, err := acct.Meet(ctx, title, start, end)
	if err != nil {
		return resp.failed(err)
	}
	if copyIt {
		// A minted link must not be thrown away because the clipboard failed:
		// the meeting exists now, and the url in the reply is the only record
		// of it. So a failed copy is logged, not failed — the reply still
		// carries the link.
		if err := run("wl-copy", url); err != nil {
			d.logf("meet: no clipboard: %v", err)
		} else if err := run("notify-send", "Meet link copied", title); err != nil {
			d.logf("meet: no notification: %v", err)
		}
	}
	return resp.ok(map[string]any{"url": url, "subject": title, "account": acct.Name})
}
