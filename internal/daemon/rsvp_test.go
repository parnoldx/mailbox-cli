package daemon

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/sync/mailsync"
	"mailbox/internal/vcal"
)

const testInviteICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
METHOD:REQUEST
BEGIN:VEVENT
UID:meet-1@example.org
DTSTART:20260910T140000Z
DTEND:20260910T150000Z
SUMMARY:Design review
ORGANIZER:mailto:boss@example.org
ATTENDEE;RSVP=TRUE:mailto:me@example.com
END:VEVENT
END:VCALENDAR
`

func seedInvite(t *testing.T) (*Daemon, *stubTransport, string) {
	t.Helper()
	d, tr := seedSend(t)
	f := fakeOf(d)
	msg := f.Deliver("INBOX", "meet-1@example.org", "Invitation: Design review", "please come")
	msg.From = "Boss <boss@example.org>"
	msg.Attach("2", "text/calendar", "invite.ics", []byte(testInviteICS))

	tx, err := d.Mirror.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, _, err := tx.UpsertMessage(mirror.Message{
		Key: "meet-1@example.org", Subject: "Invitation: Design review",
		From: "Boss <boss@example.org>", To: "me@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(id, "please come\n", "", "please come"); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutParts(id, []mirror.Part{
		{Path: "2", MIMEType: "text/calendar", Filename: "invite.ics", Disposition: "attachment", Size: int64(len(testInviteICS))},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX", UID: msg.UID, MessageID: id}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return d, tr, d.primaryAccount().messageID("INBOX", msg.UID)
}

func seedInviteDAV(t *testing.T) (*Daemon, *davsync.Fake, string) {
	t.Helper()
	d, _, id := seedInvite(t)
	f := davsync.NewFake("Kalender", testCalURL)
	f.AddCollection(davsync.Collection{Kind: "events", URL: "https://sogo.example.org/work/", Name: "Work"})
	r := &davsync.Reconciler{Account: "primary", Mirror: d.Mirror, Driver: f, Location: time.Local}
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.DAV = r
	d.DAVWriter = &davsync.Writer{Account: "primary", Mirror: d.Mirror, Driver: f, Reconciler: r}
	d.DAVHost = "dav.example.org"
	d.CalendarEmail = map[string]string{
		"me@example.com": "",
		"me@work.test":   "Work",
	}
	return d, f, id
}

func TestInviteToTheAccountAddressPicksTheHomeCalendar(t *testing.T) {
	d, _, id := seedInviteDAV(t)
	resp := mustAsk(t, d, []string{"message", "view"}, map[string]any{"positional": id})
	got := resp.Data.(message).Invite
	if got == nil || got.Calendar != "Kalender" {
		t.Fatalf("calendar = %+v", got)
	}
}

func TestInviteToAWorkAddressPicksWork(t *testing.T) {
	d, _, _ := seedInviteDAV(t)
	in := vcal.Invite{Attendees: []string{"me@work.test"}}
	name, names, err := d.inviteTarget(in, "", d.primaryAccount())
	if err != nil {
		t.Fatal(err)
	}
	if name != "Work" {
		t.Fatalf("got %q from %v", name, names)
	}
}

func TestInviteToAnUnknownAddressNeedsAChoice(t *testing.T) {
	d, _, _ := seedInviteDAV(t)
	in := vcal.Invite{Attendees: []string{"stranger@example.org"}}
	name, names, err := d.inviteTarget(in, "", d.primaryAccount())
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("guessed %q", name)
	}
	if len(names) < 2 {
		t.Fatalf("choices = %v", names)
	}
}

func TestRSVPAcceptPutsTheEventOnTheHomeCalendar(t *testing.T) {
	d, f, id := seedInviteDAV(t)
	mustAsk(t, d, []string{"rsvp"}, map[string]any{"positional": id, "accept": true})
	raw := eventNamed(t, f, "Design review")
	if !strings.Contains(raw, "PARTSTAT=ACCEPTED") {
		t.Errorf("accepted event missing PARTSTAT:\n%s", raw)
	}
	card := mustAsk(t, d, []string{"invite", "show"}, map[string]any{"positional": id}).Data.(*inviteCard)
	if card.Response != vcal.PartstatAccepted {
		t.Errorf("card response = %q", card.Response)
	}
}

func TestThreadViewMarksAnInviteWithoutFetchingIt(t *testing.T) {
	d, _, id := seedInvite(t)
	resp := mustAsk(t, d, []string{"thread", "view"}, map[string]any{"positional": id})
	rows, ok := resp.Data.([]message)
	if !ok || len(rows) != 1 || !rows[0].HasInvite {
		t.Fatalf("thread = %T %+v", resp.Data, resp.Data)
	}
	if rows[0].Invite != nil {
		t.Errorf("thread view fetched the card; it must stay lazy")
	}
}

func TestInviteShowReturnsTheCard(t *testing.T) {
	d, _, id := seedInvite(t)
	resp := mustAsk(t, d, []string{"invite", "show"}, map[string]any{"positional": id})
	card, ok := resp.Data.(*inviteCard)
	if !ok || card == nil || card.Summary != "Design review" {
		t.Fatalf("card = %T %+v", resp.Data, resp.Data)
	}
	if card.Organizer != "boss@example.org" {
		t.Errorf("organizer = %q", card.Organizer)
	}
}

func TestInviteShowCachesTheFetch(t *testing.T) {
	d, _, id := seedInvite(t)
	for i := 0; i < 2; i++ {
		if resp := mustAsk(t, d, []string{"invite", "show"}, map[string]any{"positional": id}); !resp.OK {
			t.Fatalf("invite show: %s", resp.Error)
		}
	}
	f := fakeOf(d)
	if got := f.CallCount("FetchPart"); got != 1 {
		t.Errorf("the part was fetched %d times, want once", got)
	}
}

func TestInviteShowWithoutAnInviteIsNull(t *testing.T) {
	d, _ := seedSend(t)
	msg := fakeOf(d).Deliver("INBOX", "a@example.com", "plain", "hi")
	tx, err := d.Mirror.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mid, _, err := tx.UpsertMessage(mirror.Message{Key: "a@example.com", Subject: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX", UID: msg.UID, MessageID: mid}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	resp := mustAsk(t, d, []string{"invite", "show"}, map[string]any{"positional": d.primaryAccount().messageID("INBOX", msg.UID)})
	if !resp.OK || resp.Data != nil {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestRSVPAcceptSendsIMIPToTheOrganizer(t *testing.T) {
	d, tr, id := seedInvite(t)
	resp := mustAsk(t, d, []string{"rsvp"}, map[string]any{"positional": id, "accept": true})
	out := resp.Data.(sent)
	if len(out.Recipients) != 1 || out.Recipients[0] != "boss@example.org" {
		t.Fatalf("recipients = %v", out.Recipients)
	}
	if tr.count() != 1 {
		t.Fatalf("sent %d mails", tr.count())
	}
	raw := tr.sent[0]
	if !bytes.Contains(raw, []byte("text/calendar")) {
		t.Fatalf("no text/calendar part:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("PARTSTAT=ACCEPTED")) && !bytes.Contains(raw, []byte("ACCEPTED")) {
		t.Fatalf("no ACCEPTED in the wire bytes:\n%s", raw)
	}
	if !strings.HasPrefix(out.Subject, "Accepted:") {
		t.Errorf("subject = %q", out.Subject)
	}
}

func TestRSVPWithoutAnInviteIsRefused(t *testing.T) {
	d, _ := seedSend(t)
	resp := ask(t, d, []string{"rsvp"}, map[string]any{"positional": "7", "accept": true})
	if resp.OK {
		t.Fatal("rsvp on a pdf was accepted")
	}
}

func TestMessageViewSurfacesAnInvite(t *testing.T) {
	d, _, id := seedInvite(t)
	resp := mustAsk(t, d, []string{"message", "view"}, map[string]any{"positional": id})
	m := resp.Data.(message)
	if m.Invite == nil || m.Invite.Organizer != "boss@example.org" {
		t.Fatalf("invite = %+v", m.Invite)
	}
	if m.Invite.Summary != "Design review" {
		t.Errorf("summary = %q", m.Invite.Summary)
	}
}

func TestParsePartstatFromFlags(t *testing.T) {
	got, err := rsvpPartstat(Request{Args: map[string]any{"decline": true}})
	if err != nil || got != vcal.PartstatDeclined {
		t.Fatalf("got %q (%v)", got, err)
	}
}

// An invite sent without a UID can be shown but not answered: the iMIP reply
// and the calendar event would both name an event that has no identity.
func TestRSVPWithoutAUIDIsRefused(t *testing.T) {
	d, _ := seedSend(t)
	f := fakeOf(d)
	msg := f.Deliver("INBOX", "nouid@example.org", "Invitation: no uid", "come")
	msg.From = "Boss <boss@example.org>"
	f.Deliver("INBOX", "x", "", "") // keep the folder non-empty for the sync
	uidless := strings.Replace(testInviteICS, "UID:meet-1@example.org\n", "", 1)
	msg.Attach("2", "text/calendar", "invite.ics", []byte(uidless))
	if _, err := d.primaryAccount().Reconciler.SyncAll(context.Background(), d.Mirrored); err != nil {
		t.Fatal(err)
	}
	resp := ask(t, d, []string{"rsvp"}, map[string]any{"positional": d.primaryAccount().messageID("INBOX", msg.UID), "accept": true})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}

// seedGraphInvite adds a Microsoft 365 account "work" whose Inbox holds an
// invite to me@work.test, with Respond recorded into the returned slice.
func seedGraphInvite(t *testing.T) (*Daemon, *davsync.Fake, string, *[]string) {
	t.Helper()
	d, f, _ := seedInviteDAV(t)
	mail := mailsync.NewFake("INBOX")
	ics := strings.Replace(testInviteICS, "me@example.com", "me@work.test", 1)
	msg := mail.Deliver("INBOX", "meet-1@example.org", "Invitation: Design review", "please come")
	msg.From = "Boss <boss@example.org>"
	msg.Attach("2", "text/calendar", "invite.ics", []byte(ics))
	r := &mailsync.Reconciler{Account: "work", Mirror: d.Mirror, Driver: mail}
	acct := NewAccount("work", r, &mailsync.Writer{Account: "work", Mirror: d.Mirror, Driver: mail, Mirrored: []string{"INBOX"}},
		[]string{"INBOX"}, nil)
	acct.From = compose.Address{Addr: "me@work.test"}
	acct.Graph = true
	var answered []string
	acct.Respond = func(_ context.Context, href, partstat string) error {
		answered = append(answered, href+" "+partstat)
		return nil
	}
	d.Others = append(d.Others, acct)
	if _, err := r.SyncAll(context.Background(), []string{"INBOX"}); err != nil {
		t.Fatal(err)
	}
	return d, f, acct.messageID("INBOX", msg.UID), &answered
}

// A Microsoft 365 invite is already on the calendar when it arrives: the card
// shows the answer that event holds, and an RSVP goes to Exchange through
// Respond — no iMIP mail leaves and no second copy is written.
func TestRSVPOnMicrosoft365AnswersTheEventExchangeMade(t *testing.T) {
	d, f, id, answered := seedGraphInvite(t)
	ctx := context.Background()
	onCalendar := strings.Replace(strings.Replace(testInviteICS, "METHOD:REQUEST\n", "", 1),
		"ATTENDEE;RSVP=TRUE:mailto:me@example.com", "ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@work.test", 1)
	f.Deliver("https://sogo.example.org/work/", "/work/meet-1.ics", onCalendar)
	if _, err := d.DAV.SyncKinds(ctx); err != nil {
		t.Fatal(err)
	}

	card := mustAsk(t, d, []string{"invite", "show"}, map[string]any{"positional": id}).Data.(*inviteCard)
	if card.Response != vcal.PartstatNeedsAction || card.Calendar != "Work" {
		t.Fatalf("card = %+v", card)
	}

	resp := mustAsk(t, d, []string{"rsvp"}, map[string]any{"positional": id, "accept": true})
	if out := resp.Data.(sent); out.State != "answered" || out.Recipients[0] != "boss@example.org" {
		t.Fatalf("resp = %+v", out)
	}
	if len(*answered) != 1 || (*answered)[0] != "/work/meet-1.ics ACCEPTED" {
		t.Fatalf("respond = %v", *answered)
	}
	if n := f.CallCount("Put"); n != 0 {
		t.Errorf("wrote the event %d times; Exchange already has it", n)
	}
	rows, err := d.Outbox.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("an iMIP reply went into the outbox: %+v", rows)
	}
}

// Not synced yet, or deleted in Outlook: nothing to answer on Exchange.
func TestRSVPOnMicrosoft365WithoutTheEventIsNotFound(t *testing.T) {
	d, _, id, answered := seedGraphInvite(t)
	if resp := ask(t, d, []string{"rsvp"}, map[string]any{"positional": id, "accept": true}); resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(*answered) != 0 {
		t.Fatalf("answered %v", *answered)
	}
}
