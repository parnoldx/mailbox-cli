package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"mailbox/internal/bubble"
	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// deliverInbox delivers one mail into the Inbox of a seedScreener daemon and
// syncs, so a bubble test starts from mail sitting where mail sits.
func deliverInbox(t *testing.T, d *Daemon, key, subject, from string, when time.Time) {
	t.Helper()
	f := fakeOf(d)
	m := f.Deliver("INBOX", key, subject, subject)
	m.From, m.Date = from, when
	if _, err := d.primaryAccount().Reconciler.SyncAll(context.Background(), d.Mirrored); err != nil {
		t.Fatal(err)
	}
}

func placement(t *testing.T, d *Daemon, folder string, uid uint32) mirror.Row {
	t.Helper()
	r, err := d.Mirror.Row("primary", folder, uid)
	if err != nil {
		t.Fatalf("no placement %s:%d: %v", folder, uid, err)
	}
	return r
}

func rowsIn(t *testing.T, d *Daemon, folder string) []mirror.Row {
	t.Helper()
	rows, err := d.Mirror.Rows("primary", folder, 100)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Gate 1. `bubble <id> --tomorrow` puts the whole thread in Aside with an 08:00
// return time; a Sent copy and a Screener sibling do not move.
func TestBubblePutsTheThreadInAsideWithAReturnTime(t *testing.T) {
	d, _ := seedScreener(t)
	f := fakeOf(d)
	f.AddFolder("INBOX/Sent")
	a := d.primaryAccount()
	a.Mirrored = append(a.Mirrored, "INBOX/Sent")
	d.Mirrored = a.Mirrored
	d.Writer.Mirrored = a.Mirrored
	ctx := context.Background()

	when := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	got := f.Deliver("INBOX", "deal@example.com", "Angebot", "das Angebot")
	got.From, got.Date = "sales@example.com", when
	mine := f.Deliver("INBOX/Sent", "re-deal@example.com", "Re: Angebot", "meine Antwort")
	mine.From, mine.Date = "me@example.com", when.Add(time.Hour)
	mine.Reply(got)
	if _, err := a.Reconciler.SyncAll(ctx, a.Mirrored); err != nil {
		t.Fatal(err)
	}

	resp := mustAsk(t, d, []string{"bubble"}, map[string]any{
		"positional": "1", "tomorrow": true,
	})
	rows, ok := resp.Data.([]bubbleRow)
	if !ok || len(rows) != 1 {
		t.Fatalf("bubble returned %T %+v", resp.Data, resp.Data)
	}
	// 08:00 the next local day.
	tomorrow := atHour(startOfDay(time.Now()).AddDate(0, 0, 1), 8)
	if want := tomorrow.Format("2006-01-02 15:04"); rows[0].Return != want {
		t.Errorf("return time = %q, want %q", rows[0].Return, want)
	}

	// The Inbox message is in Aside now, carrying the keyword.
	aside := boxView(t, d, "Aside")
	if len(aside) != 1 {
		t.Fatalf("Aside = %+v, want the bubbled thread", aside)
	}
	if _, ok := bubble.Of(placement(t, d, routing.BoxAside, 1).Placement.Flags); !ok {
		t.Errorf("the Aside message carries no $bubble-* keyword")
	}
	if rows := boxView(t, d, "inbox"); len(rows) != 0 {
		t.Errorf("inbox still holds %+v", rows)
	}
	// The Sent copy stayed put.
	if sent := boxView(t, d, "Sent"); len(sent) != 1 {
		t.Errorf("Sent = %+v, want the copy untouched", sent)
	}
}

// Gate 2 / Gate 7. The thread returns to the Inbox at or after its instant with
// the keyword gone; a Daemon down across the instant catches it on the first
// tick after startup, because the scan is by wall clock.
func TestBubbleReturnsOnTheFirstTickAfterTheInstant(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))

	// A date already in the past: not an error, it just comes due at once.
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": yesterday})
	if len(boxView(t, d, "Aside")) != 1 {
		t.Fatal("the thread did not go into Aside")
	}

	// The first bubbleLoop tick.
	d.returnDue(ctx, d.primaryAccount())

	inbox := boxView(t, d, "inbox")
	if len(inbox) != 1 {
		t.Fatalf("inbox = %+v after the return, want the thread back", inbox)
	}
	if len(boxView(t, d, "Aside")) != 0 {
		t.Errorf("Aside still holds the thread")
	}
	// Keyword gone.
	for _, r := range rowsIn(t, d, routing.BoxInbox) {
		if _, ok := bubble.Of(r.Placement.Flags); ok {
			t.Errorf("the returned message still carries a $bubble-* keyword: %v", r.Placement.Flags)
		}
	}
}

// Gate 9. A returned thread is \Unseen in the Inbox, so the iPhone raises a
// push, and it is marked $bubbled so the Inbox floats it to the top.
func TestBubbleReturnsUnreadAndFloated(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	// Read it before bubbling, so "returns unread" is a change and not the
	// starting state.
	if _, err := d.Writer.SetSeen(ctx, []mailsync.Ref{{Folder: "INBOX", UID: 1}}, true); err != nil {
		t.Fatal(err)
	}
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": yesterday})
	d.returnDue(ctx, d.primaryAccount())

	rows := rowsIn(t, d, routing.BoxInbox)
	if len(rows) != 1 {
		t.Fatalf("inbox = %+v", rows)
	}
	if rows[0].Seen() {
		t.Errorf("the returned message is \\Seen; the phone will not push it")
	}
	if !hasFlag(rows[0].Placement.Flags, bubble.Returned) {
		t.Errorf("the returned message is not marked %s: %v", bubble.Returned, rows[0].Placement.Flags)
	}
	// And the Inbox listing puts it first.
	if view := boxView(t, d, "inbox"); len(view) == 0 || !view[0].Bubbled {
		t.Errorf("the Inbox listing does not float the bubbled thread: %+v", view)
	}
}

// Gate 10. `--now` on a thread already in the Inbox floats it and marks it
// unread without an Aside round trip.
func TestBubbleNowOnAnInboxThreadDoesNotRoundTrip(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	if _, err := d.Writer.SetSeen(ctx, []mailsync.Ref{{Folder: "INBOX", UID: 1}}, true); err != nil {
		t.Fatal(err)
	}

	resp := mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "now": true})
	changes, ok := resp.Data.([]change)
	if !ok || len(changes) != 1 {
		t.Fatalf("bubble --now returned %T %+v", resp.Data, resp.Data)
	}
	// It never went to Aside.
	if a := fakeOf(d).Folder(routing.BoxAside); len(a.Msgs) != 0 {
		t.Errorf("bubble --now round-tripped through Aside: %+v", a.Msgs)
	}
	rows := rowsIn(t, d, routing.BoxInbox)
	if len(rows) != 1 || rows[0].Seen() {
		t.Errorf("inbox = %+v, want one unread message", rows)
	}
	if !hasFlag(rows[0].Placement.Flags, bubble.Returned) {
		t.Errorf("not floated: %v", rows[0].Placement.Flags)
	}
}

// Gate 3. A return scheduled by one Daemon fires from a second Daemon that
// shares only the Mirror and the server — the keyword is the whole coordination.
func TestBubbleReturnFiresFromASecondDaemon(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": yesterday})

	// A second Daemon on the same Mirror and the same server, which never saw
	// the bubble command.
	vps := New("primary", d.Mirror,
		&mailsync.Reconciler{Account: "primary", Mirror: d.Mirror, Driver: fakeOf(d)},
		d.Mirrored, nil, nil)
	vps.returnDue(ctx, vps.primaryAccount())

	if len(boxView(t, d, "inbox")) != 1 || len(boxView(t, d, "Aside")) != 0 {
		t.Fatalf("the VPS Daemon did not return the thread from the shared keyword")
	}
}

// Gate 4. Two Daemons both seeing one bubble come due: the thread lands in the
// Inbox once, not moved twice and not bounced.
func TestBubbleReturnIsIdempotentAcrossDaemons(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": yesterday})

	vps := New("primary", d.Mirror,
		&mailsync.Reconciler{Account: "primary", Mirror: d.Mirror, Driver: fakeOf(d)},
		d.Mirrored, nil, nil)

	d.returnDue(ctx, d.primaryAccount())
	vps.returnDue(ctx, vps.primaryAccount()) // the loser: the uid is already gone

	inbox := fakeOf(d).Folder("INBOX")
	n := 0
	for _, m := range inbox.Msgs {
		if m.MessageID == "n1@example.com" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the thread is in the Inbox %d times, want once", n)
	}
	if len(boxView(t, d, "Aside")) != 0 {
		t.Errorf("Aside still holds the thread after both Daemons ran")
	}
}

// Gate 5. A reply into a bubbled thread returns it early and clears the keyword,
// so bubbleLoop does not re-fire on a thread that is already back.
func TestAReplyReturnsABubbledThreadEarly(t *testing.T) {
	d, _ := seedScreener(t)
	f := fakeOf(d)
	a := d.primaryAccount()
	ctx := context.Background()

	opener := f.Deliver("INBOX", "deal@example.com", "Angebot", "das Angebot")
	opener.From = "sales@example.com"
	opener.Date = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if _, err := a.Reconciler.SyncAll(ctx, a.Mirrored); err != nil {
		t.Fatal(err)
	}
	future := startOfDay(time.Now()).AddDate(0, 0, 30).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": future})
	if len(boxView(t, d, "Aside")) != 1 {
		t.Fatal("the thread is not in Aside")
	}

	reply := f.Deliver("INBOX", "reply@example.com", "Re: Angebot", "ja, gerne")
	reply.From = "sales@example.com"
	reply.Date = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	reply.Reply(opener)
	d.cycle(ctx, a, "test")

	if len(boxView(t, d, "Aside")) != 0 {
		t.Fatalf("the reply did not pull the bubbled thread back")
	}
	for _, r := range rowsIn(t, d, routing.BoxInbox) {
		if _, ok := bubble.Of(r.Placement.Flags); ok {
			t.Errorf("the early-returned thread still carries a $bubble-* keyword")
		}
	}
	// A later bubbleLoop tick finds nothing due.
	d.returnDue(ctx, a)
}

// Gate 8. A resync that rewrites every placement from the server repopulates
// bubble_at from the keyword — the projection survives a Mirror rebuild
// (ADR-0013), because it is derived wherever a placement's flags are written.
func TestBubbleAtIsRederivedFromTheKeyword(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	future := startOfDay(time.Now()).AddDate(0, 0, 10).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "on": future})

	// Force a resync of Aside: it drops every placement (bubble_at with them)
	// and re-places from the server's envelopes, where place() re-derives
	// bubble_at from the `$bubble-*` keyword each one still carries. This is what
	// a Mirror rebuild does, folder by folder.
	fakeOf(d).Renumber(routing.BoxAside, 2000)
	if _, err := d.primaryAccount().Reconciler.SyncAll(ctx, d.Mirrored); err != nil {
		t.Fatal(err)
	}

	box, _ := d.primaryAccount().boxNamed(routing.BoxAside)
	got, err := d.Mirror.Bubbled("primary", box)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("bubble_at was not re-derived from the keyword: %+v", got)
	}
}

// Gate 6. `--on <today>` resolves to 18:00 today — "Later today", this morning
// having passed — and a future date to 08:00. The hours are configurable.
func TestBubbleWhenResolvesTheTimingFlags(t *testing.T) {
	d := &Daemon{BubbleMorning: 7, BubbleEvening: 20}
	today := startOfDay(time.Now())

	cases := []struct {
		name string
		args map[string]any
		want time.Time
		now  bool
	}{
		{"now", map[string]any{"now": true}, time.Time{}, true},
		{"tomorrow", map[string]any{"tomorrow": true}, atHour(today.AddDate(0, 0, 1), 7), false},
		{"on today", map[string]any{"on": today.Format("2006-01-02")}, atHour(today, 20), false},
		{"on future", map[string]any{"on": today.AddDate(0, 0, 5).Format("2006-01-02")}, atHour(today.AddDate(0, 0, 5), 7), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			when, now, err := d.bubbleWhen(Request{Args: tc.args})
			if err != nil {
				t.Fatalf("bubbleWhen: %v", err)
			}
			if now != tc.now {
				t.Fatalf("now = %v, want %v", now, tc.now)
			}
			if !tc.now && !when.Equal(tc.want) {
				t.Fatalf("when = %v, want %v", when, tc.want)
			}
		})
	}

	// No flag, and two flags, are both usage errors: HEY requires exactly one.
	if _, _, err := d.bubbleWhen(Request{Args: map[string]any{}}); err == nil {
		t.Error("no timing flag was accepted")
	}
	if _, _, err := d.bubbleWhen(Request{Args: map[string]any{"now": true, "tomorrow": true}}); err == nil {
		t.Error("two timing flags were accepted")
	}
}

// Re-timing a bubble swaps the keyword rather than stacking a second one.
func TestBubbleRetimingKeepsOneKeyword(t *testing.T) {
	d, _ := seedScreener(t)
	deliverInbox(t, d, "n1@example.com", "Newsletter", "news@example.com",
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "tomorrow": true})
	// It is in Aside now; re-time it from there.
	mustAsk(t, d, []string{"bubble"}, map[string]any{
		"positional": "Aside:1",
		"on":         startOfDay(time.Now()).AddDate(0, 0, 7).Format("2006-01-02"),
	})
	flags := placement(t, d, routing.BoxAside, 1).Placement.Flags
	n := 0
	for _, f := range flags {
		if strings.HasPrefix(f, bubble.Prefix) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the re-timed thread carries %d bubble keywords, want 1: %v", n, flags)
	}
	want := atHour(startOfDay(time.Now()).AddDate(0, 0, 7), 8)
	if got, _ := bubble.Of(flags); !got.Equal(want) {
		t.Errorf("re-timed to %v, want %v", got, want)
	}
}

// `--to bubble` is refused by name, the way `--to aside` is.
func TestRouteToBubbleIsRefused(t *testing.T) {
	d, sieve := seedScreener(t)
	resp := ask(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "bubble"})
	if resp.OK {
		t.Fatal("bubble was accepted as a routing destination")
	}
	if !strings.Contains(resp.Error, "mailbox bubble") {
		t.Errorf("the error does not point at the bubble command: %s", resp.Error)
	}
	if sieve.puts != 0 {
		t.Error("the script was written anyway")
	}
}

// bubble list reads from the Mirror and is grouped one row per thread.
func TestBubbleListIsGroupedByThread(t *testing.T) {
	d, _ := seedScreener(t)
	f := fakeOf(d)
	a := d.primaryAccount()
	ctx := context.Background()

	first := f.Deliver("INBOX", "q1@example.com", "Angebot", "erste")
	first.Date = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	second := f.Deliver("INBOX", "q2@example.com", "Re: Angebot", "zweite")
	second.Date = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	second.Reply(first)
	if _, err := a.Reconciler.SyncAll(ctx, a.Mirrored); err != nil {
		t.Fatal(err)
	}
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "1", "tomorrow": true})

	resp := mustAsk(t, d, []string{"bubble", "list"}, nil)
	rows := resp.Data.([]bubbleRow)
	if len(rows) != 1 {
		t.Fatalf("bubble list = %+v, want one row for the one thread", rows)
	}
}

// ---- "if no reply by" — HEY's Bubble Up applied on the way out ------------

// send --if-no-reply writes the same $bubble-* keyword straight onto the
// filed Sent copy. Unlike an ordinary bubble it does not move anywhere — it
// stays in Sent until either a reply cancels it or the deadline brings it
// back.
func TestIfNoReplyWatchesTheSentCopyWithoutMovingIt(t *testing.T) {
	d, _ := seedSend(t)
	out := send(t, d, map[string]any{
		"to": []string{"kunde@example.com"}, "subject": "Angebot",
		"body": "Anbei das Angebot.", "if_no_reply": true, "tomorrow": true,
	})

	row := placement(t, d, "INBOX/Sent", out.UID)
	if _, ok := bubble.Of(row.Placement.Flags); !ok {
		t.Errorf("the Sent copy carries no $bubble-* keyword: %v", row.Placement.Flags)
	}
	if sent := boxView(t, d, "Sent"); len(sent) != 1 {
		t.Errorf("Sent = %+v, want the copy to stay put", sent)
	}
}

// --if-no-reply with no timing flag is a usage error, and fails before
// anything is enqueued or sent — a bad flag must not cost the mail its send.
func TestIfNoReplyWithoutATimingFlagFailsBeforeSending(t *testing.T) {
	d, tr := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"}, Args: map[string]any{
		"to": []string{"kunde@example.com"}, "subject": "Angebot", "body": "Text",
		"if_no_reply": true,
	}})
	if resp.OK {
		t.Fatal("if-no-reply with no timing flag was accepted")
	}
	if resp.Code != "usage" {
		t.Errorf("code = %q, want usage", resp.Code)
	}
	if tr.count() != 0 {
		t.Errorf("smtp saw the mail despite the usage error")
	}
}

// A reply landing in the thread cancels a pending watch — the same "any new
// mail on the thread" signal reclaimPiled already acts on for Aside and Reply
// Later — but only cancels it: the Sent copy is not moved, since the reply
// itself is what shows up in the Inbox.
func TestAReplyCancelsTheNoReplyWatch(t *testing.T) {
	d, _ := seedSend(t)
	a := d.primaryAccount()
	ctx := context.Background()
	// Past its first sync, so the reply below is a genuine incremental cycle —
	// reclaimPiled only runs on ActionIncremental, never on a folder's first
	// ever sync.
	if _, err := a.Reconciler.SyncAll(ctx, a.Mirrored); err != nil {
		t.Fatal(err)
	}
	out := send(t, d, map[string]any{
		"to": []string{"kunde@example.com"}, "subject": "Angebot",
		"body": "Anbei das Angebot.", "if_no_reply": true, "next_week": true,
	})

	f := fakeOf(d)
	sentCopy := filedCopy(t, d, out.UID)
	reply := f.Deliver("INBOX", "antwort@example.com", "Re: Angebot", "Danke, passt.")
	reply.From = "kunde@example.com"
	reply.Reply(sentCopy)
	d.cycle(ctx, a, "test")

	row := placement(t, d, "INBOX/Sent", out.UID)
	if _, ok := bubble.Of(row.Placement.Flags); ok {
		t.Errorf("the watch was not cancelled: %v", row.Placement.Flags)
	}
	if sent := boxView(t, d, "Sent"); len(sent) != 1 {
		t.Errorf("the Sent copy moved: %+v", sent)
	}
}

// With the deadline passed and no reply, the Sent copy comes back to the
// Inbox exactly the way an Aside thread does: unread, $bubbled, keyword gone.
func TestNoReplyWatchBringsTheSentCopyToTheInboxWhenDue(t *testing.T) {
	d, _ := seedSend(t)
	ctx := context.Background()
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	send(t, d, map[string]any{
		"to": []string{"kunde@example.com"}, "subject": "Angebot",
		"body": "Anbei das Angebot.", "if_no_reply": true, "on": yesterday,
	})

	d.returnDue(ctx, d.primaryAccount())

	if sent := boxView(t, d, "Sent"); len(sent) != 0 {
		t.Errorf("Sent still holds the copy: %+v", sent)
	}
	var found *mirror.Row
	for _, r := range rowsIn(t, d, routing.BoxInbox) {
		r := r
		if r.Message.Subject == "Angebot" {
			found = &r
		}
	}
	if found == nil {
		t.Fatal("the Sent copy did not come back to the Inbox")
	}
	if found.Seen() {
		t.Errorf("the returned copy is \\Seen; the phone will not push it")
	}
	if !hasFlag(found.Placement.Flags, bubble.Returned) {
		t.Errorf("not marked %s: %v", bubble.Returned, found.Placement.Flags)
	}
	if _, ok := bubble.Of(found.Placement.Flags); ok {
		t.Errorf("still carries a $bubble-* keyword: %v", found.Placement.Flags)
	}
}

// One returnDue tick picks up an Aside bubble and a Sent no-reply watch
// together — the scan is account-wide, not folder-scoped, because bubble_at
// is a projection of a flag and not a property of any one folder.
func TestReturnDueSweepsAsideAndSentTogether(t *testing.T) {
	d, _ := seedSend(t)
	f := fakeOf(d)
	f.AddFolder(routing.BoxAside)
	d.Mirrored = append(d.Mirrored, routing.BoxAside)
	d.Writer.Mirrored = d.Mirrored
	a := d.primaryAccount()
	ctx := context.Background()
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")

	newsletter := f.Deliver("INBOX", "n1@example.com", "Newsletter", "News")
	newsletter.From, newsletter.Date = "news@example.com", time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if _, err := a.Reconciler.SyncAll(ctx, a.Mirrored); err != nil {
		t.Fatal(err)
	}
	var newsletterID string
	for _, r := range boxView(t, d, "inbox") {
		if r.Subject == "Newsletter" {
			newsletterID = r.ID
		}
	}
	if newsletterID == "" {
		t.Fatal("the newsletter is not in the inbox listing")
	}
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": newsletterID, "on": yesterday})

	send(t, d, map[string]any{
		"to": []string{"kunde@example.com"}, "subject": "Angebot",
		"body": "Anbei das Angebot.", "if_no_reply": true, "on": yesterday,
	})

	d.returnDue(ctx, a)

	if len(boxView(t, d, "Aside")) != 0 {
		t.Errorf("the Aside thread did not return")
	}
	for _, r := range boxView(t, d, "Sent") {
		if r.Subject == "Angebot" {
			t.Errorf("the Angebot Sent copy did not return: %+v", r)
		}
	}
	var haveNewsletter, haveAngebot bool
	for _, r := range boxView(t, d, "inbox") {
		haveNewsletter = haveNewsletter || r.Subject == "Newsletter"
		haveAngebot = haveAngebot || r.Subject == "Angebot"
	}
	if !haveNewsletter || !haveAngebot {
		t.Fatalf("inbox = %+v, want both the Aside thread and the Sent copy back",
			boxView(t, d, "inbox"))
	}
}
