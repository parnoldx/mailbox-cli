package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
)

type inviteCard struct {
	Summary   string `json:"summary,omitempty"`
	Organizer string `json:"organizer,omitempty"`
	Start     string `json:"start,omitempty"`
	UID       string `json:"uid,omitempty"`
}

func isCalendarPart(p mirror.Part) bool {
	kind := strings.ToLower(p.MIMEType)
	if strings.Contains(kind, "calendar") || strings.HasSuffix(kind, "/ics") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(p.Filename), ".ics")
}

func (d *Daemon) calendarPart(messageID int64) (mirror.Part, error) {
	parts, err := d.Mirror.Parts(messageID)
	if err != nil {
		return mirror.Part{}, err
	}
	for _, p := range parts {
		if isCalendarPart(p) {
			return p, nil
		}
	}
	return mirror.Part{}, errors.New("this message is not a meeting invite")
}

func (d *Daemon) loadInvite(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64) (vcal.Invite, error) {
	part, err := d.calendarPart(messageID)
	if err != nil {
		return vcal.Invite{}, err
	}
	if acct.Reconciler == nil {
		return vcal.Invite{}, errors.New("this daemon cannot fetch: no server connection")
	}
	body, err := acct.Reconciler.Driver.FetchPart(ctx, folder, uid, part.Path)
	if err != nil {
		return vcal.Invite{}, err
	}
	return vcal.ParseInvite(string(body), time.Local)
}

func (d *Daemon) inviteCardOf(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64) *inviteCard {
	in, err := d.loadInvite(ctx, acct, folder, uid, messageID)
	if err != nil {
		part, perr := d.calendarPart(messageID)
		if perr != nil {
			return nil
		}
		return &inviteCard{Summary: part.Filename}
	}
	card := &inviteCard{Summary: in.Summary, Organizer: in.Organizer, UID: in.UID}
	if !in.Start.IsZero() {
		card.Start = in.Start.Format(time.RFC3339)
	}
	return card
}

func (d *Daemon) handleRSVP(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		return resp.usage(err.Error())
	}
	partstat, err := rsvpPartstat(req)
	if err != nil {
		return resp.usage(err.Error())
	}
	if d.Outbox == nil || acct.Courier == nil {
		return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
	}
	row, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	if err != nil {
		return resp.api(err.Error())
	}
	in, err := d.loadInvite(ctx, acct, folder, uid, row.Message.ID)
	if err != nil {
		return resp.usage(err.Error())
	}
	if in.Organizer == "" {
		return resp.usage("this invite has no organizer to reply to")
	}

	ics, err := vcal.Reply(in, acct.From.Addr, partstat)
	if err != nil {
		return resp.api(err.Error())
	}
	word := rsvpWord(partstat)
	draft := compose.Draft{
		From:           acct.From,
		To:             []compose.Address{{Addr: in.Organizer}},
		Subject:        word + ": " + in.Summary,
		Body:           strings.ToUpper(partstat) + ": " + in.Summary + "\n",
		InReplyTo:      []string{row.Message.Key},
		References:     append(append([]string{}, row.Message.References...), row.Message.Key),
		CalendarMethod: "REPLY",
		CalendarICS:    []byte(ics),
	}
	resp = d.deliver(ctx, acct, draft, resp, req)
	if !resp.OK {
		return resp
	}

	if partstat != vcal.PartstatDeclined && d.DAVWriter != nil {
		if err := d.storeInvite(ctx, req, in, acct.From.Addr, partstat); err != nil && d.Log != nil {
			d.Log.Printf("rsvp: calendar: %v", err)
		}
	}
	return resp
}

func (d *Daemon) storeInvite(ctx context.Context, req Request, in vcal.Invite, attendee, partstat string) error {
	raw, err := vcal.ForCalendar(in, attendee, partstat)
	if err != nil {
		return err
	}
	if existing, err := d.Mirror.ObjectByUID(d.Account, in.UID); err == nil {
		col, err := d.collectionOf(existing)
		if err != nil {
			return err
		}
		_, err = d.put(ctx, eventChanged, col, existing.Href, raw, existing.ETag)
		return err
	}
	col, err := d.pick(calendars, req.Str("calendar"))
	if err != nil {
		return err
	}
	_, err = d.put(ctx, eventChanged, col, davsync.Href(col, in.UID), raw, "")
	return err
}

func rsvpPartstat(req Request) (string, error) {
	var got []string
	for _, w := range []string{"accept", "decline", "tentative"} {
		if req.Bool(w) {
			got = append(got, w)
		}
	}
	if len(got) == 0 {
		return vcal.ParsePartstat(req.Str("status"))
	}
	if len(got) > 1 {
		return "", fmt.Errorf("rsvp takes one of --accept, --decline, --tentative")
	}
	return vcal.ParsePartstat(got[0])
}

func rsvpWord(partstat string) string {
	switch partstat {
	case vcal.PartstatAccepted:
		return "Accepted"
	case vcal.PartstatDeclined:
		return "Declined"
	case vcal.PartstatTentative:
		return "Tentative"
	}
	return partstat
}
