package daemon

import "fmt"

// correspondentHit is one address the mailbox has actually exchanged mail
// with — the recipient autocomplete's fallback behind the address book.
type correspondentHit struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// handleCorrespondent reads the correspondents cache: a Mirror read, kept
// warm by Tx.upsertCorrespondents as messages are mirrored, never a scan of
// raw headers or a network call.
func (d *Daemon) handleCorrespondent(req Request, resp Response) Response {
	verb := "search"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	if verb != "search" {
		resp.Code, resp.Error = "usage", fmt.Sprintf("unknown correspondent command %q", verb)
		return resp
	}
	query, _ := req.Args["positional"].(string)
	limit := 6
	if v, ok := req.Args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	hits, err := d.Mirror.SearchCorrespondents(d.Account, query, limit)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	out := make([]correspondentHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, correspondentHit{Name: h.Name, Email: h.Email})
	}
	resp.OK, resp.Data = true, out
	return resp
}
