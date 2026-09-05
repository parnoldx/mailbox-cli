package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/bubble"
	"mailbox/internal/htmlmd"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
	"mailbox/internal/sync/mailsync"
)

// sent is what a send did. It names the Outbox row as well as the copy in Sent,
// because those are two different facts: the mail went out, and a copy of it
// was filed. A send that only managed the first is still a send.
type sent struct {
	OutboxID   int64    `json:"outbox_id"`
	MessageID  string   `json:"message_id"`
	State      string   `json:"state"`
	Subject    string   `json:"subject"`
	Recipients []string `json:"recipients"`
	// ID is the Placement id of the filed copy, in the form the read commands
	// take. Empty until the copy is filed.
	ID  string `json:"id,omitempty"`
	Box string `json:"box,omitempty"`
	UID uint32 `json:"uid,omitempty"`
	// Scheduled is set instead of ID/Box/UID when the mail is a "send later":
	// it is durable in the Outbox but has not gone to SMTP yet.
	Scheduled string `json:"scheduled,omitempty"`
}

// handleSend composes a mail, makes it durable, and sends it. The order is the
// point: nothing reaches SMTP that is not already in the Outbox, so a daemon
// that dies at any moment has either not sent it or knows that it might have
// (ADR-0004).
func (d *Daemon) handleSend(ctx context.Context, req Request, resp Response) Response {
	// Which account sends is a choice about the mail, not an id, so it is a
	// flag. Ids never need one (ADR-0005).
	acct, err := d.accountNamed(req.Str("account"))
	if err != nil {
		return resp.usage(err.Error())
	}
	if d.Outbox == nil || acct.Courier == nil {
		return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
	}
	draft, err := d.draftOf(acct, req)
	if err != nil {
		return resp.usage(err.Error())
	}
	return d.deliver(ctx, acct, draft, resp, req)
}

// handleReply answers a Message the Mirror holds. The recipients and the
// threading headers come from the parent rather than from the caller: an agent
// that has to assemble References by hand will get it wrong, and a reply that
// does not carry them starts a new conversation on every client that reads it
// (ADR-0008).
func (d *Daemon) handleReply(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	// A reply is sent by the account that received it. Answering from a
	// different address than the one that was written to is not a default
	// anybody would want.
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		return resp.usage(err.Error())
	}
	if !req.Bool("draft") && (d.Outbox == nil || acct.Courier == nil) {
		return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
	}
	parent, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	if err != nil {
		return resp.api(err.Error())
	}

	draft, err := d.draftOf(acct, req)
	if err != nil {
		return resp.usage(err.Error())
	}
	all := req.Bool("all")
	if err := d.answer(acct, &draft, parent.Message, all); err != nil {
		return resp.usage(err.Error())
	}
	// Filing it is the same reply, written to the drafts box instead of the
	// outbox. It is built here rather than by `draft save` because who to
	// answer and what thread it belongs to come from the parent, and only this
	// path has read it.
	if req.Bool("draft") {
		box, err := draftsBox(acct)
		if err != nil {
			return resp.usage(err.Error())
		}
		return d.saveDraft(ctx, acct, box, draft, resp)
	}
	resp = d.deliver(ctx, acct, draft, resp, req)
	// Answering a Message is the end of owing it a reply, so its thread does
	// not belong in a pile any more: pull the conversation's Reply Later and
	// Aside Messages back to the Inbox, the same as an incoming reply would
	// (CONTEXT.md). Only once the mail has actually gone — a queued send has
	// not been answered yet, and the cycle that drains it reclaims the thread
	// when the Sent copy lands.
	if resp.OK && acct.Primary {
		d.reclaimPiled(ctx, acct, []int64{parent.Message.ThreadID})
	}
	return resp
}

// answer fills a draft in as a reply to a Message.
func (d *Daemon) answer(a *Account, draft *compose.Draft, parent mirror.Message, all bool) error {
	if len(draft.To) == 0 {
		to, err := compose.ParseAddressList(parent.From)
		if err != nil || len(to) == 0 {
			return fmt.Errorf("cannot reply: %q is not an address to answer", parent.From)
		}
		draft.To = to
	}
	if all {
		draft.Cc = append(draft.Cc, replyAllCc(a, parent, draft.To, draft.Cc)...)
	}
	if draft.Subject == "" {
		draft.Subject = replySubject(parent.Subject)
	}
	// In-Reply-To is the parent; References is the whole chain, oldest first,
	// which is what lets a client that never saw the middle of the thread still
	// place this reply.
	draft.InReplyTo = []string{parent.Key}
	chain := parent.References
	if len(chain) == 0 {
		chain = parent.InReplyTo
	}
	draft.References = append(append([]string(nil), chain...), parent.Key)
	return nil
}

// replyAllCc is the Cc line a reply-to-all gets: everyone the parent was
// addressed to, minus ourselves, minus the people already on `to`, and minus
// whoever is on `have` already. Replying to all should not mean mailing
// yourself a copy every time, nor anyone twice.
//
// Read twice: `reply --all` builds the outgoing Cc from it, and a Message read
// carries it (reply_all) so a client can show who a reply-all would reach
// before it is sent.
func replyAllCc(a *Account, parent mirror.Message, to, have []compose.Address) []compose.Address {
	var cc []compose.Address
	for _, group := range []string{parent.To, parent.Cc} {
		list, err := compose.ParseAddressList(group)
		if err != nil {
			continue
		}
		for _, addr := range list {
			if sameAddress(addr.Addr, a.From.Addr) || containsAddress(to, addr.Addr) ||
				containsAddress(have, addr.Addr) || containsAddress(cc, addr.Addr) {
				continue
			}
			cc = append(cc, addr)
		}
	}
	return cc
}

// replySubject prefixes Re: exactly once. "Re: Re: Re:" is somebody's client
// doing this wrong three times.
func replySubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}

func sameAddress(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func containsAddress(list []compose.Address, addr string) bool {
	for _, a := range list {
		if sameAddress(a.Addr, addr) {
			return true
		}
	}
	return false
}

// draftOf reads the parts of a mail a caller supplied.
func (d *Daemon) draftOf(a *Account, req Request) (compose.Draft, error) {
	draft := compose.Draft{From: a.From, Date: time.Now()}
	if draft.From.Addr == "" {
		return draft, fmt.Errorf("account %q has no sender address configured", a.Name)
	}
	for _, f := range []struct {
		key  string
		dest *[]compose.Address
	}{{"to", &draft.To}, {"cc", &draft.Cc}, {"bcc", &draft.Bcc}} {
		for _, raw := range req.Strings(f.key) {
			list, err := compose.ParseAddressList(raw)
			if err != nil {
				return draft, err
			}
			*f.dest = append(*f.dest, list...)
		}
	}
	draft.Subject = req.Str("subject")
	draft.Body = req.Str("body")
	// The body carries an HTML twin. A caller that has real HTML (a GUI
	// composer) passes it as body_html and we send it verbatim; otherwise the
	// body is Markdown — plain prose is valid Markdown too — and we render it.
	// Either way draft.Body stays the text/plain part, untouched.
	if raw := req.Str("body_html"); raw != "" {
		draft.BodyHTML = htmlmd.StyleCallouts(raw)
		if draft.Body == "" {
			draft.Body = htmlmd.HTMLToMarkdown(draft.BodyHTML)
		}
	} else if draft.Body != "" {
		draft.BodyHTML = htmlmd.MarkdownToHTML(draft.Body)
	}
	for _, path := range req.Strings("attach") {
		// The Daemon reads the file, so the path has to mean the same thing
		// here as it did in the caller's shell: an absolute one. The CLI
		// resolves it, the same way `attachment save` resolves --output.
		if !filepath.IsAbs(path) {
			return draft, fmt.Errorf("attachment path must be absolute, got %q", path)
		}
		a, err := compose.LoadAttachment(path)
		if err != nil {
			return draft, fmt.Errorf("cannot attach %s: %w", path, err)
		}
		draft.Attachments = append(draft.Attachments, a)
	}
	return draft, nil
}

// deliver builds the mail, queues it, and hands it over. A mail SMTP would not
// take is an error the caller sees — and it is still in the Outbox, which is
// what the error says.
func (d *Daemon) deliver(ctx context.Context, a *Account, draft compose.Draft, resp Response, req Request) Response {
	// Resolved before anything is enqueued: a bad --if-no-reply flag fails as a
	// usage error rather than sending the mail and silently dropping the
	// reminder.
	watchWhen, watch, err := d.replyWatch(a, req)
	if err != nil {
		return resp.usage(err.Error())
	}
	sendAt, err := d.sendAt(req)
	if err != nil {
		return resp.usage(err.Error())
	}
	if watch && sendAt.After(time.Now()) {
		// The watch keyword is written once the mail is actually out (below);
		// a scheduled send skips that whole path today rather than firing the
		// reminder against a mail that has not gone anywhere yet.
		return resp.usage("if-no-reply and send-at cannot be combined yet")
	}
	raw, err := draft.Build()
	if err != nil {
		return resp.usage(err.Error())
	}
	id, err := d.Outbox.Enqueue(outbox.Item{
		Account: a.Name, MessageKey: draft.MessageID, From: draft.From.Addr,
		Recipients: draft.Recipients(), Subject: draft.Subject, Raw: raw,
		NotBefore: sendAt,
	})
	if err != nil {
		return resp.api(err.Error())
	}
	if sendAt.After(time.Now()) {
		// Held in the Outbox until sendAt: nothing to hand to SMTP yet. The
		// next drain that runs after that instant sends it like any other
		// queued mail (Courier.Drain filters on not_before).
		return resp.ok(sent{
			OutboxID: id, MessageID: draft.MessageID, State: "scheduled",
			Subject: draft.Subject, Recipients: draft.Recipients(),
			Scheduled: sendAt.Local().Format("2006-01-02 15:04"),
		})
	}
	it, err := a.Courier.Deliver(ctx, id)
	if err != nil {
		return resp.api(err.Error())
	}
	out := sent{
		OutboxID: it.ID, MessageID: it.MessageKey, State: string(it.State),
		Subject: it.Subject, Recipients: it.Recipients,
	}
	if it.State == outbox.Queued || it.State == outbox.Held {
		resp.Code = "api"
		resp.Error = fmt.Sprintf("not sent: %s\nit is in the outbox as #%d and will be retried", it.LastError, it.ID)
		resp.Data = out
		return resp
	}
	if it.Box != "" && it.UID != 0 {
		out.Box, out.UID = it.Box, it.UID
		out.ID = a.messageID(it.Box, it.UID)
		// The copy is a Message like any other, so the ordinary cycle mirrors
		// it — no second write path for something the reconciler already knows
		// how to read. Syncing the one Box it landed in makes the id above
		// readable now rather than at the next poll.
		d.mirrorSentCopy(ctx, a, it.Box)
		if watch {
			// The reminder is just the return-time keyword, written straight onto
			// the filed copy — same record bubble uses (ADR-0023), no move: it
			// stays in Sent and bubbleLoop brings it to the Inbox if the thread is
			// still quiet when it comes due.
			ref := mailsync.Ref{Folder: it.Box, UID: it.UID}
			if _, err := a.Writer.StoreFlags(ctx, []mailsync.Ref{ref}, []string{bubble.Keyword(watchWhen)}, nil); err != nil {
				d.logf("if-no-reply watch on outbox #%d: %v", it.ID, err)
			}
		}
	} else if it.State == outbox.Sent {
		// Sent, but the copy is not filed. The mail has gone; the next drain
		// tries the copy again. A requested watch is silently dropped rather
		// than chased across drains — a narrow edge case.
		d.logf("outbox #%d: %s", it.ID, it.LastError)
	}
	return resp.ok(out)
}

// mirrorSentCopy brings the Box the copy landed in up to date and tells the
// listeners. A failure here is not a failure of the send: the mail is gone and
// the copy is filed, and the next cycle will find it.
func (d *Daemon) mirrorSentCopy(ctx context.Context, a *Account, box string) {
	_, mirrored := a.boxNamed(box)
	if a.Reconciler == nil || !mirrored {
		// A Box the Mirror does not hold: nothing to read it back from.
		return
	}
	if _, err := a.Reconciler.Sync(ctx, box); err != nil {
		d.logf("sync %s/%s after send: %v", a.Name, box, err)
		d.kickAccount(a, "send")
		return
	}
	d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
}

// drain sends what is queued and files what is unfiled. It runs after every
// cycle, which is also what gives a mail that SMTP refused its next attempt.
func (d *Daemon) drain(ctx context.Context, a *Account) {
	if a.Courier == nil {
		return
	}
	before, err := a.Courier.Box.UnfiledFor(a.Name)
	if err != nil {
		d.logf("outbox: %v", err)
		return
	}
	n, err := a.Courier.Drain(ctx)
	if err != nil {
		d.logf("outbox: %v", err)
	}
	if n > 0 || len(before) > 0 {
		d.kickAccount(a, "outbox")
	}
}

// outboxRow is one line of `mailbox outbox`.
type outboxRow struct {
	ID         int64    `json:"id"`
	Account    string   `json:"account,omitempty"`
	State      string   `json:"state"`
	Created    string   `json:"created"`
	Subject    string   `json:"subject"`
	Recipients []string `json:"recipients"`
	Attempts   int      `json:"attempts"`
	Error      string   `json:"error,omitempty"`
	Placement  string   `json:"placement,omitempty"`
}

// handleOutbox lists the queue, or moves one row. Retrying a held mail is a
// decision only a caller can take: the daemon cannot know whether the mail it
// was cut off from went out (ADR-0017).
func (d *Daemon) handleOutbox(ctx context.Context, req Request, resp Response) Response {
	if d.Outbox == nil {
		return resp.api("this daemon has no outbox")
	}
	verb := req.Verb("list")
	switch verb {
	case "list":
		items, err := d.Outbox.List(50)
		if err != nil {
			return resp.api(err.Error())
		}
		rows := make([]outboxRow, 0, len(items))
		for _, it := range items {
			row := outboxRow{
				ID: it.ID, State: string(it.State), Subject: it.Subject, Account: it.Account,
				Recipients: it.Recipients, Attempts: it.Attempts, Error: it.LastError,
			}
			if !it.CreatedAt.IsZero() {
				row.Created = it.CreatedAt.Local().Format("2006-01-02 15:04")
			}
			if it.Box != "" && it.UID != 0 {
				if acct, err := d.accountNamed(it.Account); err == nil {
					row.Placement = acct.messageID(it.Box, it.UID)
				}
			}
			rows = append(rows, row)
		}
		return resp.ok(rows)

	case "retry":
		id, err := outboxID(req)
		if err != nil {
			return resp.usage(err.Error())
		}
		if err := d.Outbox.Retry(id); err != nil {
			return outboxFail(resp, err)
		}
		queued, err := d.Outbox.Get(id)
		if err != nil {
			return outboxFail(resp, err)
		}
		acct, err := d.accountNamed(queued.Account)
		if err != nil || acct.Courier == nil {
			return resp.api(fmt.Sprintf("#%d belongs to account %q, which cannot send", id, queued.Account))
		}
		it, err := acct.Courier.Deliver(ctx, id)
		if err != nil {
			return resp.api(err.Error())
		}
		if it.Box != "" && it.UID != 0 {
			d.mirrorSentCopy(ctx, acct, it.Box)
		}
		if it.State == outbox.Queued {
			return resp.api(fmt.Sprintf("not sent: %s", it.LastError))
		}
		return resp.ok(sent{
			OutboxID: it.ID, MessageID: it.MessageKey, State: string(it.State),
			Subject: it.Subject, Recipients: it.Recipients, Box: it.Box, UID: it.UID,
			ID: placementID(acct, it.Box, it.UID),
		})

	case "cancel":
		id, err := outboxID(req)
		if err != nil {
			return resp.usage(err.Error())
		}
		if err := d.Outbox.Cancel(id); err != nil {
			return outboxFail(resp, err)
		}
		return resp.ok(map[string]any{"id": id, "state": "cancelled"})
	}
	return resp.usage(fmt.Sprintf("unknown outbox command %q", verb))
}

func placementID(a *Account, box string, uid uint32) string {
	if box == "" || uid == 0 {
		return ""
	}
	return a.messageID(box, uid)
}

func outboxFail(resp Response, err error) Response {
	if errors.Is(err, outbox.ErrNotFound) {
		return resp.notFound(err.Error())
	}
	return resp.usage(err.Error())
}

// outboxID reads the row a retry or a cancel named. The `#` an outbox listing
// prints in front of the number is taken off, because that is the form somebody
// copies back out of it.
func outboxID(req Request) (int64, error) {
	raw := strings.TrimPrefix(req.Text("positional"), "#")
	if raw == "" {
		return 0, errors.New("no outbox id given")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("outbox id must be a number, got %q", raw)
	}
	return id, nil
}

// handleForward sends a Message on to somebody else. It is a reply's mirror
// image: a reply keeps the thread and changes the sender, a forward keeps the
// text and changes the thread — so it carries no In-Reply-To and no References,
// because the people it is going to were never in that conversation.
func (d *Daemon) handleForward(ctx context.Context, req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		return resp.usage(err.Error())
	}
	if d.Outbox == nil || acct.Courier == nil {
		return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
	}
	original, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	if err != nil {
		return resp.api(err.Error())
	}

	draft, err := d.draftOf(acct, req)
	if err != nil {
		return resp.usage(err.Error())
	}
	if len(draft.To) == 0 {
		return resp.usage("forward needs --to")
	}
	if draft.Subject == "" {
		draft.Subject = forwardSubject(original.Message.Subject)
	}
	// A forward is a plain-text quote of what was actually said: the note and
	// the whole original, verbatim. Rendering the "---- Forwarded message ----"
	// block as Markdown would only mangle it, so it stays text/plain.
	draft.Body = forwardBody(draft.Body, original.Message)
	draft.BodyHTML = ""
	return d.deliver(ctx, acct, draft, resp, req)
}

// forwardSubject prefixes Fwd: exactly once, the way replySubject does for Re:.
func forwardSubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Fwd:"
	}
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "fwd:") || strings.HasPrefix(lower, "fw:") {
		return s
	}
	return "Fwd: " + s
}

// forwardBody puts whatever the caller wrote above the mail being forwarded,
// under the header block every client writes there. The original is quoted
// whole rather than summarised: forwarding is how somebody is shown what was
// actually said.
func forwardBody(note string, m mirror.Message) string {
	var b strings.Builder
	if note = strings.TrimRight(note, "\n"); note != "" {
		b.WriteString(note)
		b.WriteString("\n\n")
	}
	b.WriteString("---------- Forwarded message ----------\n")
	for _, f := range []struct{ label, value string }{
		{"From", m.From}, {"Date", m.Date.Local().Format("2006-01-02 15:04")},
		{"Subject", m.Subject}, {"To", m.To}, {"Cc", m.Cc},
	} {
		if strings.TrimSpace(f.value) != "" {
			fmt.Fprintf(&b, "%s: %s\n", f.label, f.value)
		}
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(m.TextPlain, "\n"))
	b.WriteString("\n")
	return b.String()
}
