package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
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
	// Response is how this account answered, as the event on its calendar
	// says: ACCEPTED, TENTATIVE, DECLINED, NEEDS-ACTION, or empty when the
	// calendar has no answer of ours.
	Response string `json:"response,omitempty"`
	// Calendar is the unique target when an invite is clearly to one of our
	// addresses. Empty plus Calendars means the caller has to pick.
	Calendar  string   `json:"calendar,omitempty"`
	Calendars []string `json:"calendars,omitempty"`
	// Day is that calendar day: the invite itself, marked proposed, and
	// whatever else is already on it. A reply can see where it fits.
	Day []dayBlock `json:"day,omitempty"`
}

// dayBlock is one thing on the invite's day. Proposed is the invite itself,
// drawn from the mail rather than from a copy the calendar may already hold.
type dayBlock struct {
	Summary  string `json:"summary"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	AllDay   bool   `json:"all_day,omitempty"`
	Color    string `json:"color,omitempty"`
	Calendar string `json:"calendar,omitempty"`
	Proposed bool   `json:"proposed,omitempty"`
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

func (d *Daemon) loadInvite(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64, key string) (vcal.Invite, error) {
	d.invitesMu.Lock()
	in, ok := d.invites[key]
	d.invitesMu.Unlock()
	if ok {
		return in, nil
	}
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
	in, err = vcal.ParseInvite(string(body), time.Local)
	if err != nil {
		return vcal.Invite{}, err
	}
	d.invitesMu.Lock()
	if d.invites == nil {
		d.invites = make(map[string]vcal.Invite)
	}
	d.invites[key] = in
	d.invitesMu.Unlock()
	return in, nil
}

func (d *Daemon) inviteCardOf(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64, key, msgTo string) *inviteCard {
	in, err := d.loadInvite(ctx, acct, folder, uid, messageID, key)
	if err != nil {
		// A card with only the filename is still worth showing, but why the
		// fetch failed is not guessable from outside: log it.
		d.logf("invite: %s: %v", key, err)
		part, perr := d.calendarPart(messageID)
		if perr != nil {
			return nil
		}
		card := &inviteCard{Summary: part.Filename}
		if card.Calendar, card.Calendars, err = d.inviteTarget(vcal.Invite{}, msgTo, acct); err != nil {
			d.logf("invite: %v", err)
			card.Calendar, card.Calendars = "", nil
		}
		return card
	}
	card := &inviteCard{
		Summary: in.Summary, Organizer: in.Organizer, Location: in.Location,
		AllDay: in.AllDay, UID: in.UID,
	}
	card.Response = d.inviteAnswer(in.UID, acct)
	if !in.Start.IsZero() {
		card.Start = in.Start.Format(time.RFC3339)
	}
	if !in.End.IsZero() {
		card.End = in.End.Format(time.RFC3339)
	}
	if card.Calendar, card.Calendars, err = d.inviteTarget(in, msgTo, acct); err != nil {
		// A calendar list that cannot be read is logged rather than shown as
		// "no calendars": the RSVP itself fails loudly when it needs one.
		d.logf("invite: %v", err)
		card.Calendar, card.Calendars = "", nil
	}
	card.Day = d.inviteDay(in, acct)
	return card
}

// inviteDay is the invite's calendar day. The invite is one block, at the
// time the mail names, even when the calendar already has that UID — a moved
// copy on the calendar is not what this mail is asking about. Declined and
// cancelled entries are not on the day: they do not take the time.
func (d *Daemon) inviteDay(in vcal.Invite, acct *Account) []dayBlock {
	if in.Start.IsZero() || d.Mirror == nil || d.Primary == nil {
		return nil
	}
	day := startOfDay(in.Start)
	blocks := []dayBlock{}
	objects, err := d.Mirror.ObjectsIn(d.Primary.Name, "events", day, day.AddDate(0, 0, 1), "")
	if err != nil {
		d.logf("invite day: %v", err)
		return []dayBlock{proposedBlock(in)}
	}
	occ, err := expand(objects, day, day.AddDate(0, 0, 1))
	if err != nil {
		d.logf("invite day: %v", err)
		return []dayBlock{proposedBlock(in)}
	}
	raw := make(map[int64]string, len(objects))
	for _, o := range objects {
		raw[o.ID] = o.Raw
	}
	colors := map[string]string{}
	if cols, err := d.eventCalendars(); err == nil {
		for _, c := range cols {
			colors[c.Name] = c.Color
		}
	}
	want := day.Format("2006-01-02")
	addrs := d.ourAddrs(acct)
	for _, o := range occ {
		if o.Date != want || strings.EqualFold(o.Status, "CANCELLED") {
			continue
		}
		if in.UID != "" && o.UID == in.UID {
			continue
		}
		if vcal.AnswerOf(raw[o.ID], addrs...) == vcal.PartstatDeclined {
			continue
		}
		blocks = append(blocks, dayBlock{
			Summary: o.Summary, Start: o.Start, End: o.End, AllDay: o.AllDay,
			Color: colors[o.Calendar], Calendar: o.Calendar,
		})
	}
	blocks = append(blocks, proposedBlock(in))
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].AllDay != blocks[j].AllDay {
			return blocks[i].AllDay
		}
		if blocks[i].Start == blocks[j].Start {
			return blocks[i].Summary < blocks[j].Summary
		}
		return blocks[i].Start < blocks[j].Start
	})
	return blocks
}

func proposedBlock(in vcal.Invite) dayBlock {
	end := in.End
	if !end.After(in.Start) {
		if in.AllDay {
			end = in.Start.AddDate(0, 0, 1)
		} else {
			end = in.Start.Add(time.Hour)
		}
	}
	summary := in.Summary
	if summary == "" {
		summary = "Meeting"
	}
	return dayBlock{
		Summary: summary, AllDay: in.AllDay, Proposed: true,
		Start: in.Start.Format(time.RFC3339), End: end.Format(time.RFC3339),
	}
}

// ourAddrs is every address an invite or a calendar object might name us by.
func (d *Daemon) ourAddrs(acct *Account) []string {
	addrs := []string{}
	if d.Primary != nil {
		addrs = append(addrs, d.Primary.From.Addr)
	}
	if acct != nil {
		addrs = append(addrs, acct.From.Addr)
	}
	for a := range d.CalendarEmail {
		addrs = append(addrs, a)
	}
	return addrs
}

// hasCalendarPart is the mirror-only check behind HasInvite: it marks a
// Message whose card `invite show` can fetch. It never touches the server, so
// reading a Thread does not wait on one.
func (d *Daemon) hasCalendarPart(acct *Account, messageID int64) bool {
	if acct == nil {
		return false
	}
	_, err := d.calendarPart(messageID)
	return err == nil
}

func (d *Daemon) withInvite(ctx context.Context, acct *Account, folder string, uid uint32, messageID int64, key, msgTo string, m message) message {
	if card := d.inviteCardOf(ctx, acct, folder, uid, messageID, key, msgTo); card != nil {
		m.Invite = card
	}
	return m
}

// inviteAnswer is this account's PARTSTAT on the invite's event, read from the
// Mirror: a CalDAV event carries it once an RSVP stored it, a Microsoft 365
// one from the moment Exchange put it on the calendar.
func (d *Daemon) inviteAnswer(uid string, acct *Account) string {
	if uid == "" {
		return ""
	}
	o, err := d.Mirror.ObjectByUID(d.Primary.Name, uid)
	if err != nil {
		return ""
	}
	return vcal.AnswerOf(o.Raw, d.ourAddrs(acct)...)
}

// inviteTarget decides which calendar an RSVP writes to. An invite to the
// account's own address goes on that account's CalDAV; an invite to a mapped
// work address goes on that calendar; anything else returns no name and the
// list a chooser offers. A calendar list that cannot be read is an error,
// not an empty list — an empty list reads as "no calendars", which is a lie
// about the account.
func (d *Daemon) inviteTarget(in vcal.Invite, msgTo string, acct *Account) (string, []string, error) {
	open, err := d.eventCalendars()
	if err != nil {
		return "", nil, err
	}
	names := make([]string, 0, len(open))
	for _, c := range open {
		names = append(names, c.Name)
	}
	if len(open) == 0 {
		return "", nil, nil
	}
	if len(open) == 1 {
		return open[0].Name, names, nil
	}

	matched := d.matchedInviteEmails(in, msgTo, acct)
	if len(matched) != 1 {
		return "", names, nil
	}
	mapped, ok := d.lookupCalendarEmail(matched[0])
	if !ok {
		return "", names, nil
	}
	if mapped == "" {
		home := d.calendarsOnHost(open, d.DAVHost)
		if len(home) == 1 {
			return home[0].Name, names, nil
		}
		return "", names, nil
	}
	for _, c := range open {
		if strings.EqualFold(c.Name, mapped) {
			return c.Name, names, nil
		}
	}
	return "", names, nil
}

func (d *Daemon) eventCalendars() ([]mirror.Collection, error) {
	all, err := d.Mirror.Collections(d.Primary.Name, calendars.kind)
	if err != nil {
		return nil, err
	}
	out := make([]mirror.Collection, 0, len(all))
	for _, c := range all {
		if c.Name == habit.CalendarName {
			continue
		}
		out = append(out, c)
	}
	return out, nil
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
	if d.Primary.From.Addr != "" && strings.EqualFold(d.Primary.From.Addr, addr) {
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
	row, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	if err != nil {
		return resp.api(err.Error())
	}
	in, err := d.loadInvite(ctx, acct, folder, uid, row.Message.ID, row.Message.Key)
	if err != nil {
		return resp.usage(err.Error())
	}
	if in.Organizer == "" {
		return resp.usage("this invite has no organizer to reply to")
	}
	if in.UID == "" {
		// A reply names the event it answers; an invite sent without one
		// cannot be answered without inventing an identity it never had.
		return resp.usage("this invite has no UID, so it cannot be answered")
	}
	if acct.Graph {
		return d.respondOnExchange(ctx, acct, in, partstat, resp)
	}
	if d.Outbox == nil || acct.Courier == nil {
		return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
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

// respondOnExchange answers a Microsoft 365 invite on the event Exchange
// already put on the calendar; Exchange sends the reply to the organizer. The
// event is found by the invite's UID, which is the href graphdrv gave it.
func (d *Daemon) respondOnExchange(ctx context.Context, acct *Account, in vcal.Invite, partstat string, resp Response) Response {
	if acct.Respond == nil {
		return resp.api(fmt.Sprintf("account %q cannot answer invites: no calendar connection", acct.Name))
	}
	o, err := d.Mirror.ObjectByUID(d.Primary.Name, in.UID)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(fmt.Sprintf("%q is not on the %s calendar yet", in.Summary, acct.Name))
	}
	if err != nil {
		return resp.api(err.Error())
	}
	if err := acct.Respond(ctx, o.Href, partstat); err != nil {
		return resp.failed(err)
	}
	// The answer is on the server; the next cycle brings it into the Mirror,
	// where the card reads it.
	select {
	case d.davTrigger <- davKick{reason: "rsvp", kinds: []string{calendars.kind}}:
	default:
	}
	return resp.ok(sent{State: "answered", Subject: rsvpWord(partstat) + ": " + in.Summary, Recipients: []string{in.Organizer}})
}

func (d *Daemon) storeInvite(ctx context.Context, req Request, in vcal.Invite, acct *Account, msgTo, attendee, partstat string) error {
	raw, err := vcal.ForCalendar(in, attendee, partstat)
	if err != nil {
		return err
	}
	if existing, err := d.Mirror.ObjectByUID(d.Primary.Name, in.UID); err == nil {
		col, err := d.collectionOf(existing)
		if err != nil {
			return err
		}
		_, err = d.put(ctx, eventChanged, col, existing.Href, raw, existing.ETag)
		return err
	}
	name := req.Str("calendar")
	if name == "" {
		if name, _, err = d.inviteTarget(in, msgTo, acct); err != nil {
			return err
		}
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
