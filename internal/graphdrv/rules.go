// The Routing on a Graph account is inbox rules (ADR-0032). This file speaks
// /me/mailFolders/inbox/messageRules: our rules carry the `mailbox:` prefix,
// are replaced whole on every write, and rules without the prefix are never
// read and never written — a rule a human wrote in Outlook is theirs.
package graphdrv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"mailbox/internal/routing"
)

// graphRule is one messageRule as Graph describes it. Graph v1.0's predicates
// have no senderDomains test — that is Exchange PowerShell's — so a domain
// decision is written as senderContains "@example.com": a substring match,
// which `x@example.com.evil.net` would also catch, as `address :domain :is`
// never would.
type graphRule struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName"`
	Sequence    int    `json:"sequence"`
	IsEnabled   bool   `json:"isEnabled"`
	Conditions  struct {
		FromAddresses  []recipient `json:"fromAddresses,omitempty"`
		SenderContains []string    `json:"senderContains,omitempty"`
	} `json:"conditions"`
	Actions struct {
		MoveToFolder        string `json:"moveToFolder,omitempty"`
		MarkAsRead          bool   `json:"markAsRead,omitempty"`
		Delete              bool   `json:"delete,omitempty"`
		StopProcessingRules bool   `json:"stopProcessingRules"`
	} `json:"actions"`
}

// Rules reads the Routing back off the server: our rules only, with folder ids
// resolved back to the Box names the rest of the program speaks, in sequence
// order — the order the server runs them and the order first-match-wins reads
// them in.
//
// A rule of ours that cannot be read back whole is kept with its Destination
// empty rather than skipped: dropping it silently would let the next write
// decide about senders nobody decided about, and Unreadable is how a caller
// refuses that. A condition or action this program does not model makes a rule
// unreadable by the same mark, so an edit made outside this program is never
// silently overwritten with a routing that forgot it.
func (m *Mail) Rules(ctx context.Context) ([]routing.Rule, error) {
	rules, _, err := all[graphRule](ctx, m.c, "/me/mailFolders/inbox/messageRules")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Sequence < rules[j].Sequence })
	out := make([]routing.Rule, 0, len(rules))
	for _, g := range rules {
		if !strings.HasPrefix(g.DisplayName, routing.RuleName) {
			continue
		}
		out = append(out, m.ruleOf(ctx, g))
	}
	return out, nil
}

// ruleOf reads one of our rules into a routing.Rule, empty Destination when it
// cannot be read whole — a rule switched off in Outlook, a move target this
// program does not know, or a domain condition it does not model. (Other edits
// made outside this program — an extra condition, a forward — are not detected;
// the rule is then rewritten without them, which is what "replaced whole"
// means, and why rules are not edited outside it.)
func (m *Mail) ruleOf(ctx context.Context, g graphRule) routing.Rule {
	r := routing.Rule{Name: g.DisplayName}
	for _, rcpt := range g.Conditions.FromAddresses {
		r.From = append(r.From, rcpt.EmailAddress.Address)
	}
	domainsOK := true
	for _, s := range g.Conditions.SenderContains {
		if strings.HasPrefix(s, "@") && len(s) > 1 {
			r.Domains = append(r.Domains, s[1:])
		} else {
			domainsOK = false
		}
	}
	if !g.IsEnabled {
		return r
	}
	switch {
	case len(g.Conditions.FromAddresses) == 0 && len(g.Conditions.SenderContains) == 0:
		// No conditions: the catch-all. It is ours only if it files into
		// the Screener, where undecided senders wait.
		if name, ok, err := m.boxOf(ctx, g.Actions.MoveToFolder); err == nil &&
			ok && strings.EqualFold(name, routing.BoxScreener) {
			r.CatchAll = true
			r.Dest = routing.None
		}
		return r
	case g.Actions.Delete:
		r.Dest = routing.Block
	default:
		if name, ok, err := m.boxOf(ctx, g.Actions.MoveToFolder); err != nil || !ok {
			return r
		} else if d, ok2 := destOf(name); ok2 {
			r.Dest = d
		}
	}
	// A rule whose domain conditions were not all ours to read is not read
	// whole, whatever its action says.
	if !domainsOK {
		r.Dest = ""
	}
	return r
}

// boxOf is the Box name a Graph folder id is known by here, refreshing the
// folder map once when the id is not in it — a folder made or renamed in
// Outlook is not a Box that is gone.
func (m *Mail) boxOf(ctx context.Context, id string) (string, bool, error) {
	if name, ok, err := m.s.folderName(id); err != nil || ok {
		return name, ok, err
	}
	if _, err := m.Folders(ctx); err != nil {
		return "", false, err
	}
	return m.s.folderName(id)
}

// destOf is the Destination a Box name is filed under, empty when the box is
// not one the Routing files into.
func destOf(box string) (routing.Destination, bool) {
	for _, s := range routing.Specs() {
		if strings.EqualFold(s.Files, box) {
			return s.Dest, true
		}
	}
	return "", false
}

// SetRules replaces our rules with these. The new rules are created first and
// the old ones deleted after — the reverse order would leave an outage halfway
// through erasing the record, and a create that fails is rolled back out of
// what it had managed to create. What is on the server is never fewer decisions
// than before the call. Rules a human wrote are untouched.
//
// The move targets are resolved before anything is sent: a rule into a folder
// that is not there files nowhere, and the Sieve path refuses for the same
// reason (ADR-0019) — this path refuses too, and creates nothing.
func (m *Mail) SetRules(ctx context.Context, want []routing.Rule) error {
	old, _, err := all[graphRule](ctx, m.c, "/me/mailFolders/inbox/messageRules")
	if err != nil {
		return err
	}
	// The new rules are numbered after everything already on the server: while
	// both sets are briefly present, sequence decides the order, and two rules
	// with one number would order by nobody's promise.
	seq := 0
	for _, g := range old {
		if g.Sequence > seq {
			seq = g.Sequence
		}
	}

	built := make([]graphRule, 0, len(want))
	for _, r := range want {
		seq++
		g := graphRule{DisplayName: r.Name, Sequence: seq, IsEnabled: true}
		g.Actions.StopProcessingRules = true
		if r.CatchAll {
			box, err := m.ruleFolder(ctx, routing.BoxScreener)
			if err != nil {
				return err
			}
			g.Actions.MoveToFolder = box
		} else {
			for _, a := range r.From {
				var rcpt recipient
				rcpt.EmailAddress.Address = a
				g.Conditions.FromAddresses = append(g.Conditions.FromAddresses, rcpt)
			}
			for _, d := range r.Domains {
				g.Conditions.SenderContains = append(g.Conditions.SenderContains, "@"+d)
			}
			if r.Dest == routing.Block {
				// A blocked sender's rule is the bin: read, then Deleted Items —
				// what binBlocked does with the mail that was already here.
				g.Actions.Delete = true
				g.Actions.MarkAsRead = true
			} else {
				box := routing.Destination(r.Dest).Box()
				id, err := m.ruleFolder(ctx, box)
				if err != nil {
					return err
				}
				g.Actions.MoveToFolder = id
				if routing.Destination(r.Dest).Seen() {
					g.Actions.MarkAsRead = true
				}
			}
		}
		built = append(built, g)
	}

	// Create the new set. A rule that is refused aborts the write, and the
	// rules created so far go again — the server keeps the old set, which is
	// the record the caller still holds.
	var made []string
	for _, g := range built {
		var out graphRule
		if err := m.c.do(ctx, request{method: http.MethodPost,
			path: "/me/mailFolders/inbox/messageRules", body: jsonBody(g)}, &out); err != nil {
			// The caller may be gone — a cancelled command — but the half-set
			// it left behind is ours to remove regardless.
			rollback := context.WithoutCancel(ctx)
			rollback, cancel := context.WithTimeout(rollback, 30*time.Second)
			defer cancel()
			if rbErr := m.deleteRules(rollback, made); rbErr != nil {
				return fmt.Errorf("create rule %s: %w (and rolling back the %d rules created failed: %v — delete them before deciding again)",
					g.DisplayName, err, len(made), rbErr)
			}
			return fmt.Errorf("create rule %s: %w", g.DisplayName, err)
		}
		made = append(made, out.ID)
	}

	// The new set is whole. Now — and only now — the old one goes. A delete
	// that fails is an error, not a shrug: old and new sets running side by
	// side is two Routings, and the caller must not be told the write won.
	if err := m.deleteRules(ctx, ours(old)); err != nil {
		return fmt.Errorf("the new rules are in, but deleting the old ones failed: %w — "+
			"run the same decision again to clear the leftovers", err)
	}
	return nil
}

// ours is every rule with the mailbox: prefix.
func ours(rules []graphRule) (ids []string) {
	for _, g := range rules {
		if strings.HasPrefix(g.DisplayName, routing.RuleName) {
			ids = append(ids, g.ID)
		}
	}
	return ids
}

// deleteRules deletes by id; a rule that is already gone is fine, one that
// refuses is the caller's problem.
func (m *Mail) deleteRules(ctx context.Context, ids []string) error {
	var failed error
	for _, id := range ids {
		if id == "" {
			continue
		}
		path := "/me/mailFolders/inbox/messageRules/" + url.PathEscape(id)
		if err := m.c.do(ctx, request{method: http.MethodDelete, path: path}, nil); err != nil && !notFound(err) {
			failed = errors.Join(failed, err)
		}
	}
	return failed
}

// ruleFolder is a Box's folder id, looked up — never created. A Routing
// decision does not make Boxes (ADR-0019); a missing one is refused with its
// name, the way the Sieve path is.
func (m *Mail) ruleFolder(ctx context.Context, name string) (string, error) {
	row, ok, err := m.s.folder(name)
	if err != nil {
		return "", err
	}
	if !ok {
		if _, err := m.Folders(ctx); err != nil {
			return "", err
		}
		if row, ok, err = m.s.folder(name); err != nil {
			return "", err
		}
	}
	if !ok {
		return "", fmt.Errorf("no folder %q — create it before routing mail there", name)
	}
	return row.GraphID, nil
}
