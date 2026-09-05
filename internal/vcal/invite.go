package vcal

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

const (
	PartstatAccepted  = "ACCEPTED"
	PartstatDeclined  = "DECLINED"
	PartstatTentative = "TENTATIVE"
)

// Invite is a meeting request as a mail carries it: the VEVENT plus who
// organised it, so an RSVP knows who to reply to.
type Invite struct {
	UID       string
	Summary   string
	Location  string
	Organizer string // bare address
	Attendees []string
	Start     time.Time
	End       time.Time
	AllDay    bool
	Method    string
	Raw       string
}

// ParseInvite reads a text/calendar part. A METHOD:CANCEL is still an invite
// (the reply is a decline); a VEVENT-less payload is refused.
func ParseInvite(raw string, loc *time.Location) (Invite, error) {
	cal, err := decode(raw)
	if err != nil {
		return Invite{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	master, kind := primary(cal)
	if master == nil || kind != KindEvent {
		return Invite{}, fmt.Errorf("this calendar part is not a meeting")
	}
	in := Invite{Raw: raw}
	if m := cal.Props.Get("METHOD"); m != nil {
		in.Method = strings.ToUpper(m.Value)
	}
	in.UID, _ = master.Props.Text(ical.PropUID)
	in.Summary, _ = master.Props.Text(ical.PropSummary)
	in.Location, _ = master.Props.Text(ical.PropLocation)
	in.Start, in.AllDay = timeOf(master, ical.PropDateTimeStart, loc)
	in.End, _ = endOf(master, in.Start, in.AllDay, loc)
	if org := master.Props.Get(ical.PropOrganizer); org != nil {
		in.Organizer = mailtoAddr(org.Value)
	}
	for _, p := range master.Props[ical.PropAttendee] {
		if a := mailtoAddr(p.Value); a != "" {
			in.Attendees = append(in.Attendees, a)
		}
	}
	if in.UID == "" {
		return Invite{}, fmt.Errorf("this meeting has no UID")
	}
	return in, nil
}

// Reply builds a METHOD:REPLY calendar for iMIP. attendee is the address that
// is answering; partstat is ACCEPTED, DECLINED or TENTATIVE.
func Reply(in Invite, attendee, partstat string) (string, error) {
	partstat = strings.ToUpper(strings.TrimSpace(partstat))
	switch partstat {
	case PartstatAccepted, PartstatDeclined, PartstatTentative:
	default:
		return "", fmt.Errorf("unknown RSVP %q: accept, decline or tentative", partstat)
	}
	attendee = mailtoAddr(attendee)
	if attendee == "" {
		return "", fmt.Errorf("an RSVP needs an attendee address")
	}

	cal := newCalendar()
	cal.Props.SetText("METHOD", "REPLY")
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, in.UID)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	if in.Summary != "" {
		ev.Props.SetText(ical.PropSummary, in.Summary)
	}
	if in.Location != "" {
		ev.Props.SetText(ical.PropLocation, in.Location)
	}
	if !in.Start.IsZero() {
		end := in.End
		if end.IsZero() {
			end = defaultEnd(in.Start, in.AllDay)
		}
		setWhen(ev, in.Start, end, in.AllDay)
	}
	if in.Organizer != "" {
		org := ical.NewProp(ical.PropOrganizer)
		org.Value = "mailto:" + in.Organizer
		ev.Props.Set(org)
	}
	att := ical.NewProp(ical.PropAttendee)
	att.Params.Set("PARTSTAT", partstat)
	att.Value = "mailto:" + attendee
	ev.Props.Set(att)
	cal.Children = append(cal.Children, ev)
	return encode(cal)
}

// ForCalendar is the VEVENT to store on a calendar after accepting or
// tentatively accepting. METHOD is stripped — a calendar object is not an
// iMIP message — and the attendee carries the PARTSTAT.
func ForCalendar(in Invite, attendee, partstat string) (string, error) {
	ics, err := Reply(in, attendee, partstat)
	if err != nil {
		return "", err
	}
	cal, err := decode(ics)
	if err != nil {
		return "", err
	}
	cal.Props.Del("METHOD")
	return encode(cal)
}

// ParsePartstat reads the CLI word.
func ParsePartstat(word string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "accept", "accepted", "yes":
		return PartstatAccepted, nil
	case "decline", "declined", "no":
		return PartstatDeclined, nil
	case "tentative", "maybe":
		return PartstatTentative, nil
	case "":
		return "", fmt.Errorf("rsvp needs --accept, --decline or --tentative")
	default:
		return "", fmt.Errorf("unknown RSVP %q: accept, decline or tentative", word)
	}
}

func mailtoAddr(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "MAILTO:")
	v = strings.TrimPrefix(v, "mailto:")
	return strings.ToLower(strings.TrimSpace(v))
}
