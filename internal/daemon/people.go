package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Colleague is one person in a Microsoft 365 directory: the basic profile
// Graph carries for the members of the groups the account belongs to, which
// is the staff rather than every account in the tenant.
type Colleague struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// colleagueHit is one directory row as the socket answers it, shaped like a
// correspondent so the panel ranks the two the same way.
type colleagueHit struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// handlePeople searches the people in the signed-in account's Microsoft 365
// groups: the colleagues a meeting is actually made with, not the tenant's
// rooms and service accounts. The rows come from the account's cached
// Directory, so a keystroke is a slice scan and only the first search after a
// refresh waits on Graph.
func (d *Daemon) handlePeople(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("search")
	if verb != "search" {
		return resp.usage(fmt.Sprintf("unknown people command %q", verb))
	}
	query := req.Str("positional")
	limit := req.Int("limit", 20)
	if limit <= 0 {
		limit = 20
	}

	var rows []Colleague
	var firstErr error
	asked := false
	for _, a := range d.accounts() {
		if a.Directory == nil {
			continue
		}
		asked = true
		got, err := d.cachedDirectory(ctx, a)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		rows = append(rows, got...)
	}
	if !asked {
		return resp.usage("no Microsoft 365 account has a directory")
	}
	// A directory that could not be read answers with the last list it had;
	// only having nothing at all to say is an error worth reporting.
	if len(rows) == 0 && firstErr != nil {
		return resp.failed(firstErr)
	}
	out := make([]colleagueHit, 0, limit)
	for _, c := range matchColleagues(rows, query, limit) {
		out = append(out, colleagueHit{Name: c.Name, Email: c.Email})
	}
	return resp.ok(out)
}

// cachedDirectory is the account's org list, refreshed when DirectoryEvery
// has passed. A failed refresh is logged and the stale list answers: a
// colleague who was there an hour ago is still the right person to invite
// when Graph hiccups.
func (d *Daemon) cachedDirectory(ctx context.Context, a *Account) ([]Colleague, error) {
	every := d.DirectoryEvery
	if every <= 0 {
		every = time.Hour
	}
	a.directoryMu.Lock()
	defer a.directoryMu.Unlock()
	if !a.directoryAt.IsZero() && time.Since(a.directoryAt) < every {
		return a.directory, nil
	}
	rows, err := a.Directory(ctx)
	if err != nil {
		d.logf("people: %s: %v", a.Name, err)
		return a.directory, err
	}
	a.directory, a.directoryAt = rows, time.Now()
	return rows, nil
}

// matchColleagues is the picker's search: every word typed has to appear in
// the name or the address, a name that starts with the words sorts first, and
// the rest follows alphabetically. Empty words match everybody, so an empty
// box lists the directory.
func matchColleagues(rows []Colleague, query string, limit int) []Colleague {
	terms := strings.Fields(strings.ToLower(query))
	hit := make([]Colleague, 0, len(rows))
	for _, c := range rows {
		haystack := strings.ToLower(c.Name + " " + c.Email)
		ok := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				ok = false
				break
			}
		}
		if ok {
			hit = append(hit, c)
		}
	}
	prefix := strings.ToLower(strings.TrimSpace(query))
	sort.SliceStable(hit, func(i, j int) bool {
		iStart := prefix != "" && strings.HasPrefix(strings.ToLower(hit[i].Name), prefix)
		jStart := prefix != "" && strings.HasPrefix(strings.ToLower(hit[j].Name), prefix)
		if iStart != jStart {
			return iStart
		}
		return strings.ToLower(hit[i].Name) < strings.ToLower(hit[j].Name)
	})
	if limit > 0 && len(hit) > limit {
		hit = hit[:limit]
	}
	return hit
}
