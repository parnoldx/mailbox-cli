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
	verb := "list"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	name := labelName(str(req.Args["name"]))

	switch verb {
	case "list":
		names, err := d.Mirror.Labels(d.Account)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := []labelRow{}
		for _, n := range names {
			rows, err := d.Mirror.Labelled(d.Account, n, 0)
			if err != nil {
				resp.Code, resp.Error = "api", err.Error()
				return resp
			}
			out = append(out, labelRow{Label: n, Count: len(rows)})
		}
		resp.OK, resp.Data = true, out
		return resp

	case "view":
		if name == "" {
			resp.Code, resp.Error = "usage", "label view needs a label"
			return resp
		}
		limit := 50
		if v, ok := req.Args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		acct := d.primaryAccount()
		rows, err := d.Mirror.Labelled(d.Account, name, limit)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := []message{}
		for _, r := range rows {
			out = append(out, viewMessage(acct, r.Placement.Folder, r, nil))
		}
		resp.OK, resp.Data = true, out
		return resp

	case "create":
		if name == "" {
			resp.Code, resp.Error = "usage", "label create needs a name"
			return resp
		}
		if err := d.Mirror.RememberLabel(name); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		// Creating with ids is creating and applying: naming a label and the
		// mail it is for in one command is the ordinary way one comes to exist.
		if len(strList(req.Args["positional"])) == 0 {
			resp.OK, resp.Data = true, []labelRow{{Label: name}}
			return resp
		}
		return d.applyLabel(ctx, name, true, req, resp)

	case "add", "remove":
		if name == "" {
			resp.Code, resp.Error = "usage", fmt.Sprintf("label %s needs a label", verb)
			return resp
		}
		return d.applyLabel(ctx, name, verb == "add", req, resp)
	}
	resp.Code, resp.Error = "usage", fmt.Sprintf("unknown label command %q", verb)
	return resp
}

// applyLabel puts a keyword on mail or takes it off. It waits for the server
// like every other write, so an exit code of 0 means the next read sees it
// (ADR-0004).
func (d *Daemon) applyLabel(ctx context.Context, name string, add bool, req Request, resp Response) Response {
	acct, refs, err := d.refs(req)
	if err != nil {
		return refsFail(resp, err)
	}
	if add {
		// Applying a label is also how one comes to exist, so the name is
		// remembered here too rather than only by create.
		if err := d.Mirror.RememberLabel(name); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
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
