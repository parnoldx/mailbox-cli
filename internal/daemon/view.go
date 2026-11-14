package daemon

// What a command sees: the JSON shapes the socket carries and the builders that
// turn a Mirror row into one. Keeping them together is what lets a listing, a
// message and a search hit stay the same shape across the commands that print
// them.

import (
	"slices"
	"sort"
	"strings"
	"time"

	"mailbox/internal/bubble"
	"mailbox/internal/htmlmd"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/terminal"
	"mailbox/internal/trackers"
	"mailbox/internal/unsubscribe"
)

// change is what one write did to one Message.
type change struct {
	ID string `json:"id"`
	// NewID is empty when the Message moved to a Box the Mirror does not hold,
	// when the Box it came from is one the Mirror does not hold (Trash: the
	// destination has no row for it until a cycle runs), or when the server did
	// not say where it landed.
	NewID string   `json:"new_id,omitempty"`
	Box   string   `json:"box"`
	Moved bool     `json:"moved"`
	Flags []string `json:"flags,omitempty"`
	Seen  bool     `json:"seen"`
}

// accountRow is one account as `account list` names it.
type accountRow struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Primary bool   `json:"primary"`
	Color   string `json:"color"`
}

type row struct {
	ID string `json:"id"`
	// Account is the Secondary Account this row is on, and empty for the
	// Primary, the same rule as the id's prefix, spelled out for a client
	// that paints the account rather than parsing the id.
	Account string `json:"account,omitempty"`
	UID     uint32 `json:"uid"`
	Date    string `json:"date"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Seen    bool   `json:"seen"`
	Body    string `json:"body_state"`
	// Count is how many Messages are in this Thread in all, wherever they
	// sit, the same number the reader shows, not just the part of the Thread
	// in the Box being listed. It is omitted for a Message on its own, the way
	// a mail client only badges a conversation once there is one, read the
	// whole thing with `mailbox thread` on this row's own id (ADR-0008: any
	// Message in a Thread names it).
	Count int `json:"count,omitempty"`
	// Due is the wall-clock time a bubbled thread comes back, "2006-01-02
	// 15:04", shown in the Aside listing and beside a still-floating thread in
	// the Inbox. Bubbled is set on a thread a return has just brought back:
	// `mailbox bubble` floats it to the top of the Inbox and badges it until it
	// is read.
	Due     string `json:"due,omitempty"`
	Bubbled bool   `json:"bubbled,omitempty"`
	// Labels is every label on the Thread, not just on the Message shown: a
	// label is something the conversation carries, so a listing that badged
	// only its newest mail would hide the one put on the reply before it.
	Labels []string `json:"labels,omitempty"`
	// ReadOnly marks a Message the Mirror does not hold: Trash and everything
	// under it (ADR-0003). It was read straight from the server, so the only
	// things that can act on it are the server's own move (out of Trash, or to
	// spam); a flag, a label or a pile needs a mirrored Placement it does not
	// have, and a client should not offer those. See change.NewID for what a
	// move of one reports back.
	ReadOnly bool `json:"read_only,omitempty"`
}

// message is one Message read whole: its headers, its text, and every Box it
// sits in. Text is what the Mirror holds, HTML is rendered as Markdown, so a
// caller reading this never has to parse HTML (ADR-0003 keeps attachments out).
type message struct {
	ID   string `json:"id"`
	UID  uint32 `json:"uid"`
	Box  string `json:"box"`
	Date string `json:"date"`
	From string `json:"from"`
	To   string `json:"to"`
	// Cc is the mirror's cc line, the other recipients on the message,
	// Bcc never appears on received mail: the sending server strips it.
	Cc         string   `json:"cc,omitempty"`
	Subject    string   `json:"subject"`
	Seen       bool     `json:"seen"`
	Flags      []string `json:"flags"`
	Size       int64    `json:"size"`
	MessageKey string   `json:"message_key"`
	// ReplyAll is the Cc line `reply --all` would build from this Message,
	// everyone on its To/Cc except us, so a client can show the recipients
	// before the reply goes out. See replyAllCc.
	ReplyAll   []compose.Address `json:"reply_all,omitempty"`
	Body       string            `json:"body"`
	BodyFormat string            `json:"body_format"` // "plain" | "markdown" | "none"
	// BodyHTML is the raw HTML part, untouched, for a client that renders it
	// itself (a desktop reading pane). Empty when the message had no HTML part.
	// The text `Body` above stays the canonical read (ADR-0003); this is extra.
	BodyHTML   string   `json:"body_html,omitempty"`
	BodyState  string   `json:"body_state"`
	Placements []string `json:"placements"`
	// Trackers are the named open-tracking pixels in the HTML, a projection
	// of the stored part (ADR-0003): the HTML is not rewritten.
	Trackers []string `json:"trackers,omitempty"`
	// Labels are the keywords somebody chose, out of the Flags above: the
	// server's own ones are not labels and are left out (mirror.LabelsOf).
	Labels []string `json:"labels,omitempty"`
	// HasInvite says a card is there for the asking: `invite show` fetches it
	// (cached after the first time). The card itself is not embedded, so
	// reading a Thread never waits on the server.
	HasInvite bool `json:"has_invite,omitempty"`
	// Invite is the card itself, fetched on demand, `message view` and
	// `invite show` attach it; `thread view` does not.
	Invite *inviteCard `json:"invite,omitempty"`
	// Unsubscribe is how to leave this list, when the message offers a way,
	// its own List-Unsubscribe headers, or a link its body carries. Nil means
	// no button: neither was found.
	Unsubscribe *unsubscribeInfo `json:"unsubscribe,omitempty"`
	// ReadOnly says this Message came from a Box the Mirror does not hold
	// (Trash), read from the server for this one look: no Thread to expand, no
	// other Placements, and nothing to act on. See row.ReadOnly.
	ReadOnly bool `json:"read_only,omitempty"`
}

// unsubscribeInfo is unsubscribe.Target as the client needs it: enough to
// draw the button and to know, once tapped, whether the daemon just did it
// (one_click, email) or a browser has to (link).
type unsubscribeInfo struct {
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
}

// unsubscribeTarget is unsubscribe.Of, fed from the one Message both
// message view and `unsubscribe` read it from.
func unsubscribeTarget(m mirror.Message) unsubscribe.Target {
	return unsubscribe.Of(m.ListUnsubscribe, m.ListUnsubscribePost, m.TextHTML)
}

func viewMessage(a *Account, folder string, r mirror.Row, places []mirror.Placement) message {
	body, format := renderBody(r.Message)
	m := message{
		ID: a.messageID(folder, r.Placement.UID), UID: r.Placement.UID, Box: folder,
		From: r.From, To: r.To, Cc: r.Message.Cc, Subject: r.Subject, Seen: r.Seen(),
		Flags: r.Placement.Flags, Size: r.Placement.Size, MessageKey: r.Message.Key,
		Body: body, BodyFormat: format, BodyState: r.BodyState,
		BodyHTML: r.Message.TextHTML,
		Trackers: trackers.InHTML(r.Message.TextHTML),
		Labels:   mirror.LabelsOf(r.Placement.Flags),
	}
	if t := unsubscribeTarget(r.Message); t.Kind != unsubscribe.None {
		m.Unsubscribe = &unsubscribeInfo{Kind: string(t.Kind), URL: t.URL}
	}
	if to, err := compose.ParseAddressList(r.From); err == nil {
		if cc, err := replyAllCc(a, r.Message, to, nil); err == nil {
			m.ReplyAll = cc
		}
	}
	if !r.Message.Date.IsZero() {
		m.Date = r.Message.Date.Format(time.RFC3339)
	}
	for _, p := range places {
		m.Placements = append(m.Placements, a.messageID(p.Folder, p.UID))
	}
	return m
}

// renderBody picks the text to show. A message with both parts is shown as its
// plain one: converting HTML we did not need to is how a signature turns into a
// wall of markup.
func renderBody(m mirror.Message) (body, format string) {
	if strings.TrimSpace(m.TextPlain) != "" {
		return terminal.SanitizeText(m.TextPlain), "plain"
	}
	if strings.TrimSpace(m.TextHTML) != "" {
		return terminal.SanitizeText(htmlmd.HTMLToMarkdown(m.TextHTML)), "markdown"
	}
	return "", "none"
}

// hit is one search result. It carries the Box, because a result list crosses
// all of them, and the text around the match, because an agent that has to read
// each result to find out why it matched has been given a worse answer.
type hit struct {
	row
	Box     string `json:"box"`
	Snippet string `json:"snippet"`
}

func viewHits(a *Account, hits []mirror.Hit) []hit {
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		r := viewRows(a, h.Placement.Folder, []mirror.Row{h.Row}, nil)[0]
		out = append(out, hit{
			row: r, Box: h.Placement.Folder,
			Snippet: terminal.SanitizeLine(h.Snippet),
		})
	}
	return out
}

// viewRows collapses a Box listing to one row per Thread, the way a mail
// client groups a conversation rather than repeating it, Rows comes back
// newest first, so the first Message of a Thread encountered is the one
// shown, and the ones behind it only add to its unread state.
//
// The Count badge is the whole conversation's size, from threadSizes, the
// same number the reader shows, not just the part of the Thread that is in
// this Box. threadSizes may be nil, in which case the badge falls back to the
// count within the listing.
func viewRows(a *Account, folder string, rows []mirror.Row, threadSizes map[int64]int) []row {
	out := make([]row, 0, len(rows))
	index := map[int64]int{} // Message.ThreadID -> its row's index in out
	threadOf := make([]int64, 0, len(rows))
	for _, r := range rows {
		if i, ok := index[r.ThreadID]; ok {
			out[i].Count++
			if !r.Seen() {
				out[i].Seen = false
			}
			out[i].Labels = mergeLabels(out[i].Labels, mirror.LabelsOf(r.Placement.Flags))
			continue
		}
		date := ""
		if !r.Message.Date.IsZero() {
			// .Local(): the Mirror keeps the instant the server sent, which is
			// usually UTC. Printing it raw made a listing disagree with the
			// reader by the zone offset, both on screen at once.
			date = r.Message.Date.Local().Format("2006-01-02 15:04")
		}
		index[r.ThreadID] = len(out)
		threadOf = append(threadOf, r.ThreadID)
		newRow := row{
			ID: a.messageID(folder, r.UID), Account: a.label(), UID: r.UID, Date: date, From: r.From,
			Subject: r.Subject, Seen: r.Seen(), Body: r.BodyState, Count: 1,
			Labels: mirror.LabelsOf(r.Placement.Flags),
		}
		if when, ok := bubble.Of(r.Placement.Flags); ok {
			newRow.Due = when.Format("2006-01-02 15:04")
		}
		if slices.Contains(r.Placement.Flags, bubble.Returned) {
			newRow.Bubbled = true
		}
		out = append(out, newRow)
	}
	for i := range out {
		if sz := threadSizes[threadOf[i]]; sz > out[i].Count {
			out[i].Count = sz
		}
		if out[i].Count <= 1 {
			out[i].Count = 0 // omitempty: no badge for a Message on its own
		}
		// Reading a returned thread, in any client, is what ends its bubbled
		// state: the `$bubbled` keyword stays on the server, but a read thread
		// neither floats nor carries the badge.
		out[i].Bubbled = out[i].Bubbled && !out[i].Seen
	}
	// A bubbled thread comes back with an old date and would sort low; the Inbox
	// floats it to the top, badged, the way HEY does. Other Boxes keep their
	// date order.
	if folder == routing.BoxInbox {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Bubbled && !out[j].Bubbled })
	}
	return out
}

// mergedView lists a Box from every account that has it, newest first. ok is
// false when fewer than two accounts do, and the caller lists the one account
// exactly as it always has, so a single-account setup never takes this path.
//
// Each account is asked for limit rows, since any one of them may hold all of
// the newest. Threads never cross accounts (ADR-0008), so one conversation
// reaching both addresses is two rows.
func (d *Daemon) mergedView(box string, limit int) (out []row, ok bool, err error) {
	type part struct {
		acct   *Account
		folder string
	}
	var parts []part
	for _, a := range d.accounts() {
		folder := "INBOX"
		if box != "" {
			folder = resolveBox(box, a.Mirrored)
		}
		if f, has := a.boxNamed(folder); has {
			parts = append(parts, part{a, f})
		}
	}
	if len(parts) < 2 {
		return nil, false, nil
	}
	floats := false
	for _, p := range parts {
		rows, err := d.Mirror.Rows(p.acct.Name, p.folder, limit)
		if err != nil {
			return nil, false, err
		}
		out = append(out, viewRows(p.acct, p.folder, rows, d.threadSizesFor(p.acct.Name, rows))...)
		floats = floats || p.folder == routing.BoxInbox
	}
	// The date is local "2006-01-02 15:04", which sorts as text; a row with
	// none sorts last. Stable, so a tie keeps the Primary first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	// The same float viewRows gives one account's Inbox, over the merged one.
	if floats {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Bubbled && !out[j].Bubbled })
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, true, nil
}

// mergeLabels folds a second Message's labels into a Thread's, sorted and
// without repeating one two Messages both carry.
func mergeLabels(have, add []string) []string {
	for _, name := range add {
		if !slices.Contains(have, name) {
			have = append(have, name)
		}
	}
	sort.Strings(have)
	return have
}

// threadSizesFor is the whole-conversation size of every Thread in a listing,
// keyed by Thread id, for viewRows to badge each row with.
func (d *Daemon) threadSizesFor(account string, rows []mirror.Row) map[int64]int {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ThreadID)
	}
	sizes, err := d.Mirror.ThreadSizes(account, ids)
	if err != nil {
		d.logf("thread sizes for %s: %v", account, err)
		return nil
	}
	return sizes
}
