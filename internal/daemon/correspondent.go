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
	verb := req.Verb("search")
	if verb != "search" {
		return resp.usage(fmt.Sprintf("unknown correspondent command %q", verb))
	}
	query := req.Str("positional")
	limit := req.Int("limit", 6)
	hits, err := d.Mirror.SearchCorrespondents(d.Account, query, limit)
	if err != nil {
		return resp.api(err.Error())
	}
	out := make([]correspondentHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, correspondentHit{Name: h.Name, Email: h.Email})
	}
	return resp.ok(out)
}
