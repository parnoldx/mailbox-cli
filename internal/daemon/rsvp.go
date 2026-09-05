package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"mailbox/internal/habit"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
)

type inviteCard struct {
	Summary   string `json:"summary,omitempty"`
	Organizer string `json:"organizer,omitempty"`
	Location  string `json:"location,omitempty"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	AllDay    bool   `json:"all_day,omitempty"`
	UID       string `json:"uid,omitempty"`
	// Calendar is the unique target when an invite is clearly to one of our
	// addresses. Empty plus Calendars means the caller has to pick.
	Calendar  string   `json:"calendar,omitempty"`
	Calendars []string `json:"calendars,omitempty"`
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

func (d *Daemon) inviteCardOf(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64, msgTo string) *inviteCard {
	in, err := d.loadInvite(ctx, acct, folder, uid, messageID)
	if err != nil {
		part, perr := d.calendarPart(messageID)
		if perr != nil {
			return nil
		}
		card := &inviteCard{Summary: part.Filename}
		card.Calendar, card.Calendars = d.inviteTarget(vcal.Invite{}, msgTo, acct)
		return card
	}
	card := &inviteCard{
		Summary: in.Summary, Organizer: in.Organizer, Location: in.Location,
		AllDay: in.AllDay, UID: in.UID,
	}
	if !in.Start.IsZero() {
		card.Start = in.Start.Format(time.RFC3339)
	}
	if !in.End.IsZero() {
		card.End = in.End.Format(time.RFC3339)
	}
	card.Calendar, card.Calendars = d.inviteTarget(in, msgTo, acct)
	return card
}

func (d *Daemon) withInvite(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64, msgTo string, m message) message {
	if card := d.inviteCardOf(ctx, acct, folder, uid, messageID, msgTo); card != nil {
		m.Invite = card
	}
	return m
}

// inviteTarget decides which calendar an RSVP writes to. An invite to the
// account's own address goes on that account's CalDAV; an invite to a mapped
// work address goes on that calendar; anything else returns no name and the
// list a chooser offers.
func (d *Daemon) inviteTarget(in vcal.Invite, msgTo string, acct *Account) (string, []string) {
	open := d.eventCalendars()
	names := make([]string, 0, len(open))
	for _, c := range open {
		names = append(names, c.Name)
	}
	if len(open) == 0 {
		return "", nil
	}
	if len(open) == 1 {
		return open[0].Name, names
	}

	matched := d.matchedInviteEmails(in, msgTo, acct)
	if len(matched) != 1 {
		return "", names
	}
	mapped, ok := d.lookupCalendarEmail(matched[0])
	if !ok {
		return "", names
	}
	if mapped == "" {
		home := d.calendarsOnHost(open, d.DAVHost)
		if len(home) == 1 {
			return home[0].Name, names
		}
		return "", names
	}
	for _, c := range open {
		if strings.EqualFold(c.Name, mapped) {
			return c.Name, names
		}
	}
	return "", names
}

func (d *Daemon) eventCalendars() []mirror.Collection {
	all, err := d.Mirror.Collections(d.Account, calendars.kind)
	if err != nil {
		return nil
	}
	out := make([]mirror.Collection, 0, len(all))
	for _, c := range all {
		if c.Name == habit.CalendarName {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (d *Daemon) calendarsOnHost(all []mirror.Collection, host string) []mirror.Collection {
	if host == "" {
		return nil
	}
	var out []mirror.Collection
	for _, c := range all {
		if hostOfURL(c.URL) == host {
			out = append(out, c)
		}
	}
	return out
}

func (d *Daemon) matchedInviteEmails(in vcal.Invite, msgTo string, acct *Account) []string {
	seen := map[string]bool{}
	add := func(addr string) {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" {
			return
		}
		if _, ok := d.lookupCalendarEmail(addr); ok {
			seen[addr] = true
		}
	}
	for _, a := range in.Attendees {
		add(a)
	}
	for _, a := range emailsFromHeader(msgTo) {
		add(a)
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	return out
}

func (d *Daemon) lookupCalendarEmail(addr string) (string, bool) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return "", false
	}
	if d.CalendarEmail != nil {
		n, ok := d.CalendarEmail[addr]
		return n, ok
	}
	if d.From.Addr != "" && strings.EqualFold(d.From.Addr, addr) {
		return "", true
	}
	return "", false
}

func emailsFromHeader(raw string) []string {
	list, err := compose.ParseAddressList(raw)
	if err != nil || len(list) == 0 {
		if a, aerr := compose.ParseAddress(raw); aerr == nil && a.Addr != "" {
			return []string{strings.ToLower(a.Addr)}
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Addr != "" {
			out = append(out, strings.ToLower(a.Addr))
		}
	}
	return out
}

func hostOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
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
		if err := d.storeInvite(ctx, req, in, acct, row.To, acct.From.Addr, partstat); err != nil && d.Log != nil {
			d.Log.Printf("rsvp: calendar: %v", err)
		}
	}
	return resp
}

func (d *Daemon) storeInvite(ctx context.Context, req Request, in vcal.Invite, acct *Account, msgTo, attendee, partstat string) error {
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
	name := req.Str("calendar")
	if name == "" {
		name, _ = d.inviteTarget(in, msgTo, acct)
	}
	col, err := d.pick(calendars, name)
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
