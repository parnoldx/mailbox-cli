package vcal

import (
	"strings"
	"testing"
)

const meeting = `BEGIN:VCALENDAR
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

func TestParseInviteReadsTheOrganizer(t *testing.T) {
	in, err := ParseInvite(meeting, berlin)
	if err != nil {
		t.Fatal(err)
	}
	if in.UID != "meet-1@example.org" || in.Summary != "Design review" {
		t.Fatalf("invite = %+v", in)
	}
	if in.Organizer != "boss@example.org" {
		t.Errorf("organizer = %q", in.Organizer)
	}
	if in.Method != "REQUEST" {
		t.Errorf("method = %q", in.Method)
	}
	if len(in.Attendees) != 1 || in.Attendees[0] != "me@example.com" {
		t.Errorf("attendees = %v", in.Attendees)
	}
}

func TestReplyCarriesPARTSTATAndMETHOD(t *testing.T) {
	in, err := ParseInvite(meeting, berlin)
	if err != nil {
		t.Fatal(err)
	}
	ics, err := Reply(in, "me@example.com", PartstatAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ics, "METHOD:REPLY") {
		t.Errorf("no METHOD:REPLY:\n%s", ics)
	}
	if !strings.Contains(ics, "PARTSTAT=ACCEPTED") {
		t.Errorf("no PARTSTAT:\n%s", ics)
	}
	if !strings.Contains(ics, "mailto:me@example.com") {
		t.Errorf("attendee missing:\n%s", ics)
	}
	if !strings.Contains(ics, "UID:meet-1@example.org") {
		t.Errorf("UID missing:\n%s", ics)
	}
}

func TestForCalendarDropsMETHOD(t *testing.T) {
	in, _ := ParseInvite(meeting, berlin)
	ics, err := ForCalendar(in, "me@example.com", PartstatTentative)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ics, "METHOD:") {
		t.Errorf("calendar object still carries METHOD:\n%s", ics)
	}
	if !strings.Contains(ics, "PARTSTAT=TENTATIVE") {
		t.Errorf("no PARTSTAT:\n%s", ics)
	}
}

func TestParsePartstat(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"accept", PartstatAccepted},
		{"decline", PartstatDeclined},
		{"tentative", PartstatTentative},
		{"yes", PartstatAccepted},
	} {
		got, err := ParsePartstat(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParsePartstat(%q) = %q (%v), want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParsePartstat("shrug"); err == nil {
		t.Fatal("shrug was accepted")
	}
}
