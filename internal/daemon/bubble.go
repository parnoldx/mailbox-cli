package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mailbox/internal/bubble"
	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// handleBubble sets a thread aside with a return time, lists the threads that
// carry one, or — with --now — brings one back straight away.
//
// It matches HEY's `hey bubble up`: one timing flag is required and there is no
// config default (D2). A bubbled thread is an Aside thread that also carries a
// `$bubble-*` keyword — no new Box (D5) — and `bubbleLoop` moves it back to the
// Inbox when its instant passes, `\Seen` stripped so the iPhone raises a push
// (D8). Scheduling happens only on the home machine; the always-on VPS Daemon
// runs only the loop (D7, D9). See docs/bubble-and-screener-handoff.md.
func (d *Daemon) handleBubble(ctx context.Context, req Request, resp Response) Response {
	a := d.primaryAccount()
	if len(req.Cmd) > 1 && req.Cmd[1] == "list" {
		return d.bubbleList(a, resp)
	}
	if a.Writer == nil {
		resp.Code, resp.Error = "api", "this daemon cannot write: no server connection"
		return resp
	}

	when, now, err := d.bubbleWhen(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}

	acct, refs, err := d.refs(req)
	if err != nil {
		return refsFail(resp, err)
	}
	if !acct.Primary {
		resp.Code, resp.Error = "usage", "bubble belongs to the primary account"
		return resp
	}
	// Set the whole conversation aside, like `aside`: the id is expanded to its
	// Thread and every member in the Inbox or a pile moves with it. A Sent copy
	// or a Screener sibling stays where it is.
	refs, err = d.threadedWithin(acct.Name, refs, routing.BoxInbox, routing.BoxAside, routing.BoxReplyLater)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	if now {
		// `--now` is the manual form of the timed return: the exact same code
		// path, run immediately. It works on a thread that is not in Aside too —
		// an Inbox thread just gets `\Unseen` + `$bubbled` and floats, no round
		// trip.
		results, err := d.bringBack(ctx, acct, refs)
		return d.wrote(acct, resp, results, err)
	}

	rows, err := d.setBubble(ctx, acct, refs, when)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	d.push(Push{Event: "mail.changed", Account: acct.Name, Box: routing.BoxInbox})
	d.push(Push{Event: "mail.changed", Account: acct.Name, Box: routing.BoxAside})
	resp.OK, resp.Data = true, rows
	return resp
}

// bubbleRow is one bubbled thread as `bubble list` and a schedule reply show
// it: the id that reads it, and when it comes back.
type bubbleRow struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	// Return is the wall-clock instant it is due, "2006-01-02 15:04".
	Return string `json:"return"`
	// Due is true when that instant has already passed: the thread returns on
	// the next bubbleLoop tick, which is not an error (D3, the past-instant
	// rule).
	Due bool `json:"due"`
}

// bubbleList reports the bubbled threads, soonest-due first, one row per Thread.
func (d *Daemon) bubbleList(a *Account, resp Response) Response {
	box, ok := d.boxNamed(a, routing.BoxAside)
	if !ok {
		resp.OK, resp.Data = true, []bubbleRow{}
		return resp
	}
	refs, err := d.Mirror.Bubbled(a.Name, box)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, bubbleRows(a, refs, nil)
	return resp
}

// bubbleRows folds bubbled placements to one row per Thread. keep, when set,
// limits the result to those Thread ids.
func bubbleRows(a *Account, refs []mirror.BubbleRef, keep map[int64]bool) []bubbleRow {
	now := time.Now()
	seen := map[int64]bool{}
	out := []bubbleRow{}
	for _, b := range refs {
		if keep != nil && !keep[b.ThreadID] {
			continue
		}
		if seen[b.ThreadID] {
			continue
		}
		seen[b.ThreadID] = true
		out = append(out, bubbleRow{
			ID:      a.messageID(b.Folder, b.UID),
			Subject: b.Subject,
			From:    b.From,
			Return:  b.BubbleAt.Format("2006-01-02 15:04"),
			Due:     !b.BubbleAt.After(now),
		})
	}
	return out
}

// setBubble writes the return-time keyword onto a thread's members and, for the
// ones still in the Inbox, moves them into Aside. Re-timing swaps the keyword —
// one at a time — and moves nothing, because the thread is already in Aside.
func (d *Daemon) setBubble(ctx context.Context, a *Account, refs []mailsync.Ref, when time.Time) ([]bubbleRow, error) {
	keyword := bubble.Keyword(when)

	// Strip any keyword already there, so a re-timed thread carries exactly one.
	// Grouped by the old keyword, because a STORE removes a named flag.
	drop := map[string][]mailsync.Ref{}
	touched := map[int64]bool{}
	for _, ref := range refs {
		row, err := d.Mirror.Row(a.Name, ref.Folder, ref.UID)
		if err != nil {
			return nil, err
		}
		touched[row.Message.ThreadID] = true
		if old := bubble.KeywordOf(row.Placement.Flags); old != "" && old != keyword {
			drop[old] = append(drop[old], ref)
		}
	}
	for old, rs := range drop {
		if _, err := a.Writer.StoreFlags(ctx, rs, nil, []string{old}); err != nil {
			return nil, err
		}
	}
	// Add the new keyword. It rides the MOVE below into Aside like any flag.
	if _, err := a.Writer.StoreFlags(ctx, refs, []string{keyword}, nil); err != nil {
		return nil, err
	}

	var toAside []mailsync.Ref
	for _, ref := range refs {
		if strings.EqualFold(ref.Folder, routing.BoxInbox) {
			toAside = append(toAside, ref)
		}
	}
	if len(toAside) > 0 {
		dest, ok := d.boxNamed(a, routing.BoxAside)
		if !ok {
			return nil, fmt.Errorf("this account has no %q box", routing.BoxAside)
		}
		if _, err := a.Writer.Move(ctx, toAside, dest); err != nil {
			return nil, err
		}
	}

	box, _ := d.boxNamed(a, routing.BoxAside)
	all, err := d.Mirror.Bubbled(a.Name, box)
	if err != nil {
		return nil, err
	}
	return bubbleRows(a, all, touched), nil
}

// bringBack returns a thread's Aside and Reply Later members to the Inbox: it
// strips the `$bubble-*` keyword, drops `\Seen` so an unseen mail landing in
// INBOX makes the iPhone push a notification (D8), sets `$bubbled` so the Inbox
// listing floats it to the top, and moves. A member already in the Inbox is
// only re-flagged, no round trip.
//
// One code path serves the manual `--now` and the scheduled return, and it is
// idempotent enough for the home Daemon and the VPS Daemon to both run it on
// the same second: one MOVE wins and the other gets "no such uid", which the
// loop logs (gate 4).
func (d *Daemon) bringBack(ctx context.Context, a *Account, refs []mailsync.Ref) ([]mailsync.Result, error) {
	inbox, _ := d.boxNamed(a, routing.BoxInbox)
	var out []mailsync.Result
	var move []mailsync.Ref
	for _, ref := range refs {
		row, err := d.Mirror.Row(a.Name, ref.Folder, ref.UID)
		if err != nil {
			return out, err
		}
		remove := []string{`\Seen`}
		if kw := bubble.KeywordOf(row.Placement.Flags); kw != "" {
			remove = append(remove, kw)
		}
		res, err := a.Writer.StoreFlags(ctx, []mailsync.Ref{ref}, []string{bubble.Returned}, remove)
		if err != nil {
			return out, err
		}
		if strings.EqualFold(ref.Folder, inbox) {
			out = append(out, res...)
		} else {
			move = append(move, ref)
		}
	}
	if len(move) > 0 {
		moved, err := a.Writer.Move(ctx, move, inbox)
		if err != nil {
			return out, err
		}
		out = append(out, moved...)
	}
	for _, box := range []string{routing.BoxAside, routing.BoxReplyLater, inbox} {
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	return out, nil
}

// stripBubble removes whatever `$bubble-*` keyword a set of refs carries, one
// STORE per distinct keyword. It is what an early return calls before the
// thread is moved, so the timer does not re-fire on it.
func (d *Daemon) stripBubble(ctx context.Context, a *Account, refs []mailsync.Ref) {
	drop := map[string][]mailsync.Ref{}
	for _, ref := range refs {
		row, err := d.Mirror.Row(a.Name, ref.Folder, ref.UID)
		if err != nil {
			continue
		}
		if kw := bubble.KeywordOf(row.Placement.Flags); kw != "" {
			drop[kw] = append(drop[kw], ref)
		}
	}
	for kw, rs := range drop {
		if _, err := a.Writer.StoreFlags(ctx, rs, nil, []string{kw}); err != nil {
			d.logf("strip bubble keyword %s: %v", kw, err)
		}
	}
}

// bubbleLoop returns bubbled threads whose instant has passed. It scans by wall
// clock rather than sleeping on a per-message timer, so a Daemon that was down
// across the instant catches it on the first tick after startup. It runs the
// Primary Account only — the piles and the Routing are the Primary's.
func (d *Daemon) bubbleLoop(ctx context.Context) {
	a := d.primaryAccount()
	if a.Writer == nil {
		return
	}
	every := d.PollEvery
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		d.returnDue(ctx, a)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// returnDue is one bubbleLoop tick: every bubbled thread in Aside whose
// bubble_at is at or before now, brought back.
func (d *Daemon) returnDue(ctx context.Context, a *Account) {
	box, ok := d.boxNamed(a, routing.BoxAside)
	if !ok {
		return
	}
	due, err := d.Mirror.BubblesDue(a.Name, box, time.Now())
	if err != nil {
		d.logf("bubble scan: %v", err)
		return
	}
	seen := map[int64]bool{}
	for _, b := range due {
		if seen[b.ThreadID] {
			continue
		}
		seen[b.ThreadID] = true
		members, err := d.Mirror.Thread(a.Name, b.ThreadID)
		if err != nil {
			d.logf("bubble thread %d: %v", b.ThreadID, err)
			continue
		}
		var refs []mailsync.Ref
		for _, m := range members {
			switch m.Placement.Folder {
			case routing.BoxAside, routing.BoxReplyLater:
				refs = append(refs, mailsync.Ref{Folder: m.Placement.Folder, UID: m.Placement.UID})
			}
		}
		if len(refs) == 0 {
			continue
		}
		if _, err := d.bringBack(ctx, a, refs); err != nil {
			// The other Daemon got there first, most likely: it moved the uids
			// this one is holding and the MOVE here finds nothing. Not an error
			// worth a problem, just a note (gate 4).
			d.logf("bubble return thread %d: %v", b.ThreadID, err)
		}
	}
}

// bubbleWhen resolves the one required timing flag. now is true for --now, the
// manual return; otherwise `when` is the wall-clock instant to come back at.
func (d *Daemon) bubbleWhen(req Request) (when time.Time, now bool, err error) {
	nowFlag, _ := req.Args["now"].(bool)
	tomorrow, _ := req.Args["tomorrow"].(bool)
	weekend, _ := req.Args["weekend"].(bool)
	nextWeek, _ := req.Args["next_week"].(bool)
	on := strings.TrimSpace(str(req.Args["on"]))

	set := 0
	for _, b := range []bool{nowFlag, tomorrow, weekend, nextWeek, on != ""} {
		if b {
			set++
		}
	}
	if set == 0 {
		return time.Time{}, false, fmt.Errorf(
			"bubble needs one of --now, --on <date>, --tomorrow, --weekend, --next-week")
	}
	if set > 1 {
		return time.Time{}, false, fmt.Errorf("bubble takes exactly one timing flag")
	}

	morning, evening := d.bubbleHours()
	today := startOfDay(time.Now())
	switch {
	case nowFlag:
		return time.Time{}, true, nil
	case tomorrow:
		return atHour(today.AddDate(0, 0, 1), morning), false, nil
	case weekend:
		return atHour(comingWeekday(today, time.Saturday, false), morning), false, nil
	case nextWeek:
		return atHour(comingWeekday(today, time.Monday, true), morning), false, nil
	default:
		day, perr := time.ParseInLocation("2006-01-02", on, time.Local)
		if perr != nil {
			return time.Time{}, false, fmt.Errorf(
				"--on takes a date like 2026-09-10 — got %q", on)
		}
		if day.Equal(today) {
			// This morning has passed: HEY's "Later today".
			return atHour(today, evening), false, nil
		}
		return atHour(day, morning), false, nil
	}
}

// bubbleHours is the configured morning and evening hours, or 8 and 18.
func (d *Daemon) bubbleHours() (morning, evening int) {
	morning, evening = d.BubbleMorning, d.BubbleEvening
	if morning < 0 || morning > 23 {
		morning = 0
	}
	if morning == 0 {
		morning = 8
	}
	if evening <= 0 || evening > 23 {
		evening = 18
	}
	return morning, evening
}

// atHour is `day` at a whole hour, local time.
func atHour(day time.Time, hour int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
}

// comingWeekday is the next `target` weekday on or after `from`. strict pushes
// a match on `from` itself to the following week — "next Monday" said on a
// Monday is not today.
func comingWeekday(from time.Time, target time.Weekday, strict bool) time.Time {
	delta := (int(target) - int(from.Weekday()) + 7) % 7
	if delta == 0 && strict {
		delta = 7
	}
	return from.AddDate(0, 0, delta)
}
