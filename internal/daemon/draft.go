package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/htmlmd"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// handleDraft is the unsent pile. A draft is ordinary mail in the Drafts Box
// carrying the \Draft flag, which is what makes one written in webmail and one
// written here the same thing.
//
// There is no in-place edit on IMAP: changing a draft appends the new version
// and trashes the old, so its uid changes and the reply says what the new one
// is (ADR-0004).
func (d *Daemon) handleDraft(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("list")
	acct := d.primaryAccount()
	if name := req.Str("account"); name != "" {
		var err error
		if acct, err = d.accountNamed(name); err != nil {
			return resp.usage(err.Error())
		}
	}
	box, err := draftsBox(acct)
	if err != nil {
		return resp.usage(err.Error())
	}

	if verb == "save" {
		// A draft written here rather than in webmail. It is the same append
		// an edit does, with nothing to trash afterwards.
		draft, err := d.draftOf(acct, req)
		if err != nil {
			return resp.usage(err.Error())
		}
		return d.saveDraft(ctx, acct, box, draft, resp)
	}

	if verb == "list" {
		limit := req.Int("limit", 25)
		rows, err := d.Mirror.Rows(acct.Name, box, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		out := []message{}
		for _, r := range rows {
			out = append(out, viewMessage(acct, box, r, nil))
		}
		return resp.ok(out)
	}

	row, err := d.draftRow(acct, box, req)
	if err != nil {
		return resp.notFound(err.Error())
	}

	switch verb {
	case "show":
		return resp.ok(viewMessage(acct, box, row, nil))

	case "delete":
		results, err := acct.Writer.Move(ctx, []mailsync.Ref{{Folder: box, UID: row.Placement.UID}}, "Trash")
		return d.wrote(acct, resp, results, err)

	case "edit", "send":
		draft, err := d.draftFrom(acct, row, req)
		if err != nil {
			return resp.usage(err.Error())
		}
		if verb == "send" {
			if d.Outbox == nil || acct.Courier == nil {
				return resp.api(fmt.Sprintf("account %q cannot send: no outbox", acct.Name))
			}
			if len(draft.To) == 0 {
				return resp.usage("this draft has nobody to send to: add --to")
			}
			out := d.deliver(ctx, acct, draft, resp, req)
			if !out.OK {
				return out
			}
			// The draft goes only once the mail is out. A send that failed
			// leaves it where it was, so nothing is lost by retrying — and a
			// failure to bin it is not a failure of the send, which has already
			// happened: the reply says the mail went either way.
			_, _ = acct.Writer.Move(ctx, []mailsync.Ref{{Folder: box, UID: row.Placement.UID}}, "Trash")
			return out
		}
		return d.replaceDraft(ctx, acct, box, row, draft, resp)
	}
	return resp.usage(fmt.Sprintf("unknown draft command %q", verb))
}

// saveDraft files a new draft and nothing else.
func (d *Daemon) saveDraft(ctx context.Context, acct *Account, box string, draft compose.Draft, resp Response) Response {
	return d.putDraft(ctx, acct, box, nil, draft, resp)
}

// replaceDraft writes the new version and trashes the old one, in that order:
// a crash between them leaves two drafts, which is recoverable, where the other
// order loses the only copy.
func (d *Daemon) replaceDraft(ctx context.Context, acct *Account, box string, old mirror.Row, draft compose.Draft, resp Response) Response {
	return d.putDraft(ctx, acct, box, &old, draft, resp)
}

func (d *Daemon) putDraft(ctx context.Context, acct *Account, box string, old *mirror.Row, draft compose.Draft, resp Response) Response {
	raw, err := draft.Build()
	if err != nil {
		return resp.usage(err.Error())
	}
	uid, err := acct.Writer.Driver.Append(ctx, box, []string{`\Draft`}, raw)
	if err != nil {
		return resp.api(err.Error())
	}
	if old != nil {
		if _, err := acct.Writer.Move(ctx, []mailsync.Ref{{Folder: box, UID: old.Placement.UID}}, "Trash"); err != nil {
			return resp.api(err.Error())
		}
	}
	// The Mirror is caught up here rather than at the next cycle, so a listing
	// straight after an edit shows the edit (ADR-0004).
	if acct.Reconciler != nil {
		if _, err := acct.Reconciler.Sync(ctx, box); err != nil && d.Log != nil {
			d.Log.Printf("draft: catching the mirror up: %v", err)
		}
	}
	d.push(Push{Event: "mail.changed", Account: acct.Name, Box: box})

	out := map[string]any{"state": "saved", "subject": draft.Subject}
	if uid != 0 {
		out["id"] = acct.messageID(box, uid)
	}
	return resp.ok(out)
}

// draftFrom reads a draft back into something sendable and applies whatever the
// caller is changing. Anything not named keeps what the draft already said,
// which is what makes an edit an edit.
func (d *Daemon) draftFrom(acct *Account, row mirror.Row, req Request) (compose.Draft, error) {
	draft := compose.Draft{From: acct.From, Date: time.Now()}
	if draft.From.Addr == "" {
		return draft, fmt.Errorf("account %q has no sender address configured", acct.Name)
	}
	for _, f := range []struct {
		key   string
		was   string
		dest  *[]compose.Address
		clear string
	}{
		{"to", row.Message.To, &draft.To, "to"},
		{"cc", row.Message.Cc, &draft.Cc, "cc"},
	} {
		given := req.Strings(f.key)
		if len(given) == 0 {
			// Kept as the draft had them. A malformed address somebody typed in
			// webmail is dropped rather than made into an error here: the point
			// of an edit is to be able to fix it.
			list, _ := compose.ParseAddressList(f.was)
			*f.dest = list
			continue
		}
		for _, raw := range given {
			list, err := compose.ParseAddressList(raw)
			if err != nil {
				return draft, err
			}
			*f.dest = append(*f.dest, list...)
		}
	}
	// A draft that is a reply carries the headers that put it in its thread.
	// Losing them here would mean `reply --draft` followed by `draft send`
	// started a new conversation, which is not what either command said.
	draft.InReplyTo = row.Message.InReplyTo
	draft.References = row.Message.References
	draft.Subject = row.Message.Subject
	if v := req.Str("subject"); v != "" {
		draft.Subject = v
	}
	draft.Body = row.Message.TextPlain
	if v := req.Str("body"); v != "" {
		draft.Body = v
	}
	if raw := req.Str("body_html"); raw != "" {
		draft.BodyHTML = raw
	} else if strings.TrimSpace(draft.Body) != "" {
		// An edited draft is sent like any other: its body is Markdown and
		// carries a rendered text/html twin (see draftOf).
		draft.BodyHTML = htmlmd.MarkdownToHTML(draft.Body)
	}
	if strings.TrimSpace(draft.Body) == "" {
		draft.Body = "\n"
	}
	return draft, nil
}

// draftRow finds the draft a caller named. An id may be bare, because `draft`
// has already said which Box this is about, or qualified the ordinary way.
func (d *Daemon) draftRow(acct *Account, box string, req Request) (mirror.Row, error) {
	id := req.Text("positional")
	if id == "" {
		return mirror.Row{}, errors.New("which draft? give the id draft list printed")
	}
	folder, uid := box, uint32(0)
	if _, rest := splitAccount(id, d.accountNames()); rest != "" {
		id = rest
	}
	if !strings.Contains(id, ":") {
		n, err := strconv.ParseUint(id, 10, 32)
		if err != nil || n == 0 {
			return mirror.Row{}, fmt.Errorf("a draft id is a uid or Drafts:uid, got %q", id)
		}
		uid = uint32(n)
	} else {
		var err error
		if folder, uid, err = parseMessageID(id, acct.Mirrored); err != nil {
			return mirror.Row{}, err
		}
	}
	row, err := d.Mirror.Row(acct.Name, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return mirror.Row{}, fmt.Errorf("no draft %s in the mirror", id)
	}
	return row, err
}

// draftsBox is where this account keeps unsent mail, found by name among the
// mirrored Boxes the way the junk box is.
func draftsBox(a *Account) (string, error) {
	if name, ok := boxNamedLeaf(a, "drafts"); ok {
		return name, nil
	}
	return "", errors.New("this account has no drafts box")
}

// boxNamedLeaf matches the last segment of a folder path, so `Drafts`,
// `INBOX/Drafts` and `INBOX.Drafts` all answer to the same name.
func boxNamedLeaf(a *Account, want string) (string, bool) {
	for _, folder := range a.Mirrored {
		leaf := folder
		if i := strings.LastIndexAny(folder, "/."); i >= 0 {
			leaf = folder[i+1:]
		}
		if strings.EqualFold(leaf, want) {
			return folder, true
		}
	}
	return "", false
}
