package daemon

import (
	"context"
	"fmt"
	"strings"
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
		out := []message{}
		for _, r := range rows {
			out = append(out, viewMessage(acct, r.Placement.Folder, r, nil))
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
