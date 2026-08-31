package daemon

import (
	"context"
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"

	"mailbox/internal/htmlmd"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
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
}

// handleSend composes a mail, makes it durable, and sends it. The order is the
// point: nothing reaches SMTP that is not already in the Outbox, so a daemon
// that dies at any moment has either not sent it or knows that it might have
// (ADR-0004).
func (d *Daemon) handleSend(ctx context.Context, req Request, resp Response) Response {
	// Which account sends is a choice about the mail, not an id, so it is a
	// flag. Ids never need one (ADR-0005).
	acct, err := d.accountNamed(str(req.Args["account"]))
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if d.Outbox == nil || acct.Courier == nil {
		resp.Code, resp.Error = "api", fmt.Sprintf("account %q cannot send: no outbox", acct.Name)
		return resp
	}
	draft, err := d.draftOf(acct, req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	return d.deliver(ctx, acct, draft, resp)
}

// handleReply answers a Message the Mirror holds. The recipients and the
// threading headers come from the parent rather than from the caller: an agent
// that has to assemble References by hand will get it wrong, and a reply that
// does not carry them starts a new conversation on every client that reads it
// (ADR-0008).
func (d *Daemon) handleReply(ctx context.Context, req Request, resp Response) Response {
	id, _ := req.Args["positional"].(string)
	// A reply is sent by the account that received it. Answering from a
	// different address than the one that was written to is not a default
	// anybody would want.
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if wants, _ := req.Args["draft"].(bool); !wants && (d.Outbox == nil || acct.Courier == nil) {
		resp.Code, resp.Error = "api", fmt.Sprintf("account %q cannot send: no outbox", acct.Name)
		return resp
	}
	parent, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", noSuchMessage(id)
		return resp
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	draft, err := d.draftOf(acct, req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	all, _ := req.Args["all"].(bool)
	if err := d.answer(acct, &draft, parent.Message, all); err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	// Filing it is the same reply, written to the drafts box instead of the
	// outbox. It is built here rather than by `draft save` because who to
	// answer and what thread it belongs to come from the parent, and only this
	// path has read it.
	if wants, _ := req.Args["draft"].(bool); wants {
		box, err := draftsBox(acct)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		return d.saveDraft(ctx, acct, box, draft, resp)
	}
	return d.deliver(ctx, acct, draft, resp)
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
		// Everyone the mail was addressed to, minus ourselves and minus the
		// people already on the To line. Replying to all should not mean
		// mailing yourself a copy every time.
		var cc []compose.Address
		for _, group := range []string{parent.To, parent.Cc} {
			list, err := compose.ParseAddressList(group)
			if err != nil {
				continue
			}
			for _, addr := range list {
				if sameAddress(addr.Addr, a.From.Addr) || containsAddress(draft.To, addr.Addr) || containsAddress(cc, addr.Addr) {
					continue
				}
				cc = append(cc, addr)
			}
		}
		draft.Cc = append(draft.Cc, cc...)
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

	// Quote the parent under whatever the caller wrote — top-posted, the way
	// every mail client does it — so the reply carries its own context on a
	// client with no conversation view. Skipped when the caller already
	// included the block (a GUI that showed the quote to the user for
	// trimming): quoteMarker in the HTML is how that is recognised.
	if !strings.Contains(draft.BodyHTML, quoteMarker) {
		draft.Body = replyBody(draft.Body, parent)
		draft.BodyHTML = replyBodyHTML(draft.BodyHTML, parent)
	}
	return nil
}

// quoteMarker tags answer()'s quoted-parent <div> so the same reply, sent
// again from a GUI that already rendered the quote, is not quoted twice.
const quoteMarker = "data-mailbox-quote"

// replyAttribution is the "On <date>, <who> wrote:" line that sits above a
// quoted parent.
func replyAttribution(m mirror.Message) string {
	who := strings.TrimSpace(m.From)
	if who == "" {
		who = "the sender"
	}
	return fmt.Sprintf("On %s, %s wrote:", m.Date.Local().Format("Mon, 2 Jan 2006 at 15:04"), who)
}

// parentPlain is the parent's text, falling back to a flattened copy of its
// HTML when it never had a text/plain part.
func parentPlain(m mirror.Message) string {
	if strings.TrimSpace(m.TextPlain) != "" {
		return m.TextPlain
	}
	if strings.TrimSpace(m.TextHTML) != "" {
		return htmlmd.HTMLToMarkdown(m.TextHTML)
	}
	return ""
}

// replyBody is the text/plain half: the caller's note, then the parent with
// every line "> "-quoted under an attribution line.
func replyBody(note string, m mirror.Message) string {
	var b strings.Builder
	if note = strings.TrimRight(note, "\n"); note != "" {
		b.WriteString(note)
		b.WriteString("\n\n")
	}
	b.WriteString(replyAttribution(m))
	b.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(parentPlain(m), "\n"), "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// replyBodyHTML is the text/html half: the caller's HTML, then the same
// attribution line and the parent wrapped in a <blockquote> a client can fold.
func replyBodyHTML(note string, m mirror.Message) string {
	inner := strings.TrimSpace(m.TextHTML)
	if inner == "" {
		inner = "<pre style=\"white-space:pre-wrap\">" +
			html.EscapeString(strings.TrimRight(m.TextPlain, "\n")) + "</pre>"
	}
	return note +
		"<div " + quoteMarker + "><br><div>" + html.EscapeString(replyAttribution(m)) + "</div>" +
		"<blockquote type=\"cite\" style=\"margin:0 0 0 .8ex;border-left:2px solid #ccc;padding-left:1ex\">" +
		inner + "</blockquote></div>"
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
		for _, raw := range strList(req.Args[f.key]) {
			list, err := compose.ParseAddressList(raw)
			if err != nil {
				return draft, err
			}
			*f.dest = append(*f.dest, list...)
		}
	}
	draft.Subject, _ = req.Args["subject"].(string)
	draft.Body, _ = req.Args["body"].(string)
	// The body carries an HTML twin. A caller that has real HTML (a GUI
	// composer) passes it as body_html and we send it verbatim; otherwise the
	// body is Markdown — plain prose is valid Markdown too — and we render it.
	// Either way draft.Body stays the text/plain part, untouched.
	if raw, _ := req.Args["body_html"].(string); raw != "" {
		draft.BodyHTML = raw
		if draft.Body == "" {
			draft.Body = htmlmd.HTMLToMarkdown(raw)
		}
	} else if draft.Body != "" {
		draft.BodyHTML = htmlmd.MarkdownToHTML(draft.Body)
	}
	for _, path := range strList(req.Args["attach"]) {
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
func (d *Daemon) deliver(ctx context.Context, a *Account, draft compose.Draft, resp Response) Response {
	raw, err := draft.Build()
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	id, err := d.Outbox.Enqueue(outbox.Item{
		Account: a.Name, MessageKey: draft.MessageID, From: draft.From.Addr,
		Recipients: draft.Recipients(), Subject: draft.Subject, Raw: raw,
	})
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	it, err := a.Courier.Deliver(ctx, id)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
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
	} else if it.State == outbox.Sent {
		// Sent, but the copy is not filed. The mail has gone; the next drain
		// tries the copy again.
		d.logf("outbox #%d: %s", it.ID, it.LastError)
	}
	resp.OK, resp.Data = true, out
	return resp
}

// mirrorSentCopy brings the Box the copy landed in up to date and tells the
// listeners. A failure here is not a failure of the send: the mail is gone and
// the copy is filed, and the next cycle will find it.
func (d *Daemon) mirrorSentCopy(ctx context.Context, a *Account, box string) {
	if a.Reconciler == nil || !a.mirrors(box) {
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

func (a *Account) mirrors(box string) bool {
	for _, m := range a.Mirrored {
		if strings.EqualFold(m, box) {
			return true
		}
	}
	return false
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
		resp.Code, resp.Error = "api", "this daemon has no outbox"
		return resp
	}
	verb := "list"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	switch verb {
	case "list":
		items, err := d.Outbox.List(50)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
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
		resp.OK, resp.Data = true, rows
		return resp

	case "retry":
		id, err := outboxID(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
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
			resp.Code, resp.Error = "api", fmt.Sprintf("#%d belongs to account %q, which cannot send", id, queued.Account)
			return resp
		}
		it, err := acct.Courier.Deliver(ctx, id)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		if it.Box != "" && it.UID != 0 {
			d.mirrorSentCopy(ctx, acct, it.Box)
		}
		if it.State == outbox.Queued {
			resp.Code, resp.Error = "api", fmt.Sprintf("not sent: %s", it.LastError)
			return resp
		}
		resp.OK, resp.Data = true, sent{
			OutboxID: it.ID, MessageID: it.MessageKey, State: string(it.State),
			Subject: it.Subject, Recipients: it.Recipients, Box: it.Box, UID: it.UID,
			ID: placementID(acct, it.Box, it.UID),
		}
		return resp

	case "cancel":
		id, err := outboxID(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		if err := d.Outbox.Cancel(id); err != nil {
			return outboxFail(resp, err)
		}
		resp.OK, resp.Data = true, map[string]any{"id": id, "state": "cancelled"}
		return resp
	}
	resp.Code, resp.Error = "usage", fmt.Sprintf("unknown outbox command %q", verb)
	return resp
}

func placementID(a *Account, box string, uid uint32) string {
	if box == "" || uid == 0 {
		return ""
	}
	return a.messageID(box, uid)
}

func outboxFail(resp Response, err error) Response {
	if errors.Is(err, outbox.ErrNotFound) {
		resp.Code, resp.Error = "not_found", err.Error()
		return resp
	}
	resp.Code, resp.Error = "usage", err.Error()
	return resp
}

func outboxID(req Request) (int64, error) {
	switch v := req.Args["positional"].(type) {
	case float64:
		return int64(v), nil
	case string:
		var id int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(v), "#"), "%d", &id); err != nil || id <= 0 {
			return 0, fmt.Errorf("outbox id must be a number, got %q", v)
		}
		return id, nil
	}
	return 0, errors.New("no outbox id given")
}

// strList reads a repeatable argument, which JSON hands over as a list of any.
func strList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// handleForward sends a Message on to somebody else. It is a reply's mirror
// image: a reply keeps the thread and changes the sender, a forward keeps the
// text and changes the thread — so it carries no In-Reply-To and no References,
// because the people it is going to were never in that conversation.
func (d *Daemon) handleForward(ctx context.Context, req Request, resp Response) Response {
	id, _ := req.Args["positional"].(string)
	acct, folder, uid, err := d.resolveID(id)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if d.Outbox == nil || acct.Courier == nil {
		resp.Code, resp.Error = "api", fmt.Sprintf("account %q cannot send: no outbox", acct.Name)
		return resp
	}
	original, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", noSuchMessage(id)
		return resp
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	draft, err := d.draftOf(acct, req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	if len(draft.To) == 0 {
		resp.Code, resp.Error = "usage", "forward needs --to"
		return resp
	}
	if draft.Subject == "" {
		draft.Subject = forwardSubject(original.Message.Subject)
	}
	// A forward is a plain-text quote of what was actually said: the note and
	// the whole original, verbatim. Rendering the "---- Forwarded message ----"
	// block as Markdown would only mangle it, so it stays text/plain.
	draft.Body = forwardBody(draft.Body, original.Message)
	draft.BodyHTML = ""
	return d.deliver(ctx, acct, draft, resp)
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
