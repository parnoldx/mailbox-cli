package daemon

import (
	"context"
	"fmt"
	"strings"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// handleLabel is the keyword half of filing mail. A label is an IMAP keyword,
// not a Box: that is what lets a Message carry several at once and keep all of
// them when it is moved, which is the whole difference between labelling
// something and putting it somewhere.
//
// The list is derived from the mail that carries them. Only a label created and
// not yet used has to be remembered, and the Mirror remembers it — so a rebuild
// brings back every label in use and forgets the empty ones, which is the right
// way round.
func (d *Daemon) handleLabel(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("list")
	name := labelName(req.Str("name"))

	switch verb {
	case "list":
		names, err := d.Mirror.Labels(d.Account)
		if err != nil {
			return resp.api(err.Error())
		}
		counts, err := d.Mirror.LabelCounts(d.Account)
		if err != nil {
			return resp.api(err.Error())
		}
		// Names and counts are read separately because they answer different
		// questions: a label that has been created and used by nothing is on the
		// list, at nought.
		out := []labelRow{}
		for _, n := range names {
			out = append(out, labelRow{Label: n, Count: counts[n]})
		}
		return resp.ok(out)

	case "view":
		if name == "" {
			return resp.usage("label view needs a label")
		}
		limit := req.Int("limit", 50)
		acct := d.primaryAccount()
		rows, err := d.Mirror.Labelled(d.Account, name, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		// A listing, not a read: the same row a Box listing gives, one Box at a
		// time because a label crosses them (the shape viewHits uses).
		out := []row{}
		for _, r := range rows {
			out = append(out, viewRows(acct, r.Placement.Folder, []mirror.Row{r}, nil)[0])
		}
		return resp.ok(out)

	case "create":
		if name == "" {
			return resp.usage("label create needs a name")
		}
		if err := d.Mirror.RememberLabel(name); err != nil {
			return resp.api(err.Error())
		}
		// Creating with ids is creating and applying: naming a label and the
		// mail it is for in one command is the ordinary way one comes to exist.
		if len(req.Strings("positional")) == 0 {
			return resp.ok([]labelRow{{Label: name}})
		}
		return d.applyLabel(ctx, name, true, req, resp)

	case "add", "remove":
		if name == "" {
			return resp.usage(fmt.Sprintf("label %s needs a label", verb))
		}
		return d.applyLabel(ctx, name, verb == "add", req, resp)

	case "rename":
		to := labelName(req.Str("to"))
		if name == "" || to == "" {
			return resp.usage("label rename needs both names: OLD --to NEW")
		}
		return d.sweepLabel(ctx, name, to, resp)

	case "delete":
		if name == "" {
			return resp.usage("label delete needs a label")
		}
		return d.sweepLabel(ctx, name, "", resp)
	}
	return resp.usage(fmt.Sprintf("unknown label command %q", verb))
}

// applyLabel puts a keyword on mail or takes it off. It waits for the server
// like every other write, so an exit code of 0 means the next read sees it
// (ADR-0004).
func (d *Daemon) applyLabel(ctx context.Context, name string, add bool, req Request, resp Response) Response {
	acct, refs, err := d.refs(req)
	if err != nil {
		return resp.failed(err)
	}
	if add {
		// Applying a label is also how one comes to exist, so the name is
		// remembered here too rather than only by create.
		if err := d.Mirror.RememberLabel(name); err != nil {
			return resp.api(err.Error())
		}
	}
	results, err := acct.Writer.SetLabel(ctx, refs, name, add)
	return d.wrote(acct, resp, results, err)
}

// labelSweep is how much mail a rename or a delete will take the keyword off in
// one go.
// ponytail: one pass, no paging — page the sweep if a label ever holds more.
const labelSweep = 5000

// sweepLabel renames a label onto `to`, or deletes it when `to` is empty. Both
// are the same walk: every Message carrying the keyword, restamped or stripped
// on the server, and then the remembered name brought in line.
//
// A label nobody has applied yet has no mail to walk, and is only the remembered
// name — so that path writes nothing to the server and still holds.
func (d *Daemon) sweepLabel(ctx context.Context, name, to string, resp Response) Response {
	rows, err := d.Mirror.Labelled(d.Account, name, labelSweep)
	if err != nil {
		return resp.api(err.Error())
	}
	refs := make([]mailsync.Ref, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, mailsync.Ref{Folder: r.Placement.Folder, UID: r.Placement.UID})
	}
	acct := d.primaryAccount()
	var results []mailsync.Result
	if len(refs) > 0 {
		if acct.Writer == nil {
			return resp.usage("this daemon cannot write: no server connection")
		}
		if to != "" {
			// The new keyword goes on before the old one comes off, so a failure
			// half way leaves the mail carrying both rather than neither.
			if results, err = acct.Writer.SetLabel(ctx, refs, to, true); err != nil {
				return resp.api(err.Error())
			}
		}
		if results, err = acct.Writer.SetLabel(ctx, refs, name, false); err != nil {
			return resp.api(err.Error())
		}
	}
	if to != "" {
		if err := d.Mirror.RememberLabel(to); err != nil {
			return resp.api(err.Error())
		}
	}
	if err := d.Mirror.ForgetLabel(name); err != nil {
		return resp.api(err.Error())
	}
	return d.wrote(acct, resp, results, nil)
}

// labelRow is one label in a listing: the name, and how much mail carries it.
type labelRow struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// labelName trims a label to what IMAP will take as a keyword. A space would
// make one keyword read as two, which is how a label silently becomes two
// labels nobody meant.
func labelName(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), "-")
}
