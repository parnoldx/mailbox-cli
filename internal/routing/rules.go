// Rules are the Routing as a Graph account carries it: inbox rules instead of a
// Sieve script (ADR-0032). Same Lists, different serialization — the decisions
// and their order are exactly the ones Script writes, and ListsFromRules reads
// them back the way Parse reads the script.
package routing

// RuleName is the displayName prefix that marks an inbox rule as ours. A rule
// without it was written by a human in Outlook and is never read and never
// written.
const RuleName = "mailbox:"

// Rule is one server-side rule: a group of senders bound for one Destination.
// CatchAll marks the screener's catch-all — no conditions, every sender nothing
// has been decided about.
type Rule struct {
	Name    string      `json:"name"`
	Dest    Destination `json:"dest"`
	From    []string    `json:"from,omitempty"`
	Domains []string    `json:"domains,omitempty"`
	// CatchAll carries no sender list: it is the rule that files everyone else
	// into the Screener, and it is always last.
	CatchAll bool `json:"catch_all,omitempty"`
}

// Rules renders the Routing as the rules a Graph account runs, in the order
// they are to match: address rules for every Destination, then domain rules,
// then the catch-all — the same order Script writes. A list with nobody on it
// is no rule at all, as in the script.
func (l *Lists) Rules() []Rule {
	var out []Rule
	for _, domains := range []bool{false, true} {
		for _, s := range order {
			keys := l.keys(s.dest, domains)
			if len(keys) == 0 {
				continue
			}
			r := Rule{Name: ruleName(s.dest, domains), Dest: s.dest}
			for _, k := range keys {
				if domains {
					r.Domains = append(r.Domains, k[1:])
				} else {
					r.From = append(r.From, k)
				}
			}
			out = append(out, r)
		}
	}
	out = append(out, Rule{Name: RuleName + " screener", Dest: None, CatchAll: true})
	return out
}

func ruleName(d Destination, domains bool) string {
	name := RuleName + " " + string(d)
	if domains {
		name += " (domains)"
	}
	return name
}

// ListsFromRules reads the decisions back out of rules read from the server.
// A rule whose sender lists are empty carries no decision, and one whose
// Destination is not one of the four is somebody else's and skipped — the same
// forgiveness Parse reads the script with.
func ListsFromRules(rules []Rule) *Lists {
	l := New()
	for _, r := range rules {
		if r.CatchAll {
			continue
		}
		if _, ok := specOf(r.Dest); !ok {
			continue
		}
		for _, k := range r.From {
			// First match wins, as on the server: a sender two rules name is
			// matched by the earlier one.
			if _, taken := l.by[normalise(k)]; taken {
				continue
			}
			_, _ = l.Set(k, r.Dest)
		}
		for _, d := range r.Domains {
			key := "@" + normalise(d)
			if _, taken := l.by[key]; taken {
				continue
			}
			_, _ = l.Set(key, r.Dest)
		}
	}
	return l
}

// HasCatchAll says whether the screener's catch-all is among the rules. It is
// what "the Routing is in force" means on a Graph account: without it,
// undecided senders land in the Inbox and nothing is waiting for a decision.
func HasCatchAll(rules []Rule) bool {
	for _, r := range rules {
		if r.CatchAll {
			return true
		}
	}
	return false
}

// Unreadable says whether a rule was ours but could not be read back — its
// move target names a Box this program no longer knows. The rule is kept (the
// server is the record), but no decision is made on an account whose Routing
// cannot be read whole: a write would carry over only what it could read and
// quietly forget the rest.
func Unreadable(rules []Rule) bool {
	for _, r := range rules {
		if _, ok := specOf(r.Dest); !ok && !r.CatchAll {
			return true
		}
	}
	return false
}
