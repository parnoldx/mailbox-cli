package daemon

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
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
	name, names := d.inviteTarget(in, "", d.primaryAccount())
	if name != "Work" {
		t.Fatalf("got %q from %v", name, names)
	}
}

func TestInviteToAnUnknownAddressNeedsAChoice(t *testing.T) {
	d, _, _ := seedInviteDAV(t)
	in := vcal.Invite{Attendees: []string{"stranger@example.org"}}
	name, names := d.inviteTarget(in, "", d.primaryAccount())
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
}

func TestThreadViewSurfacesAnInvite(t *testing.T) {
	d, _, id := seedInvite(t)
	resp := mustAsk(t, d, []string{"thread", "view"}, map[string]any{"positional": id})
	rows, ok := resp.Data.([]message)
	if !ok || len(rows) != 1 || rows[0].Invite == nil {
		t.Fatalf("thread = %T %+v", resp.Data, resp.Data)
	}
	if rows[0].Invite.Summary != "Design review" {
		t.Errorf("summary = %q", rows[0].Invite.Summary)
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
