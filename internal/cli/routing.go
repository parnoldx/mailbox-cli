package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// runScreener lists who is waiting for a decision, one line per sender. The
// Screener holds mail from senders nothing has been decided about, and what is
// owed there is a decision per sender rather than a read per mail.
func runScreener(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"screener"}, Args: map[string]any{"limit": in.Int("limit")},
	}, in.JSON(), printScreener, stdout, stderr)
}

func printScreener(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing is waiting for a decision")
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		count := fmt.Sprintf("%d", int(numOf(m["count"])))
		if unread := int(numOf(m["unread"])); unread > 0 && unread != int(numOf(m["count"])) {
			count = fmt.Sprintf("%d (%d new)", int(numOf(m["count"])), unread)
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\n",
			truncate(str(m["address"]), 34), count, str(m["newest"]),
			truncate(str(m["subject"]), 38), str(m["id"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// runRouteList prints the decisions already made. The sieve script on the
// server is the record; what is listed here is a projection of it (ADR-0019).
func runRouteList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"route"}, Args: map[string]any{"script": in.Bool("script")},
	}, in.JSON(), printRouting, stdout, stderr)
}

// runRouteSet decides where a sender's mail goes. A target is a message id or
// an address: whoever has just read something in the Screener has its id and
// not its sender's address.
func runRouteSet(in *input, stdout, stderr io.Writer) int {
	to := in.Str("to")
	if to == "" {
		fmt.Fprint(stderr, "route set needs a destination: --to inbox|feed|paper|block|screener\n")
		return ExitUsage
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"route"},
		Args: map[string]any{"positional": in.Words, "to": to},
	}, in.JSON(), printDecisions, stdout, stderr)
}

func printRouting(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if active, _ := m["active"].(bool); !active {
		fmt.Fprintln(stderr, "notice: the routing script is not the active one — nothing below is in force")
	}
	rows, _ := m["routes"].([]any)
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nobody is routed anywhere: every sender lands in the screener")
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		box := str(row["box"])
		if box == "" {
			box = "discarded"
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\n", str(row["to"]), str(row["address"]), box)
	}
	_ = tw.Flush()
	if script := str(m["script"]); script != "" {
		fmt.Fprintf(stdout, "\n%s", script)
	}
	behindNotice(stderr, resp)
}

// printDecisions says what a decision did to the sender and to the mail that
// was already here, because those are two different things and only one of
// them is visible in a box listing afterwards.
func printDecisions(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		what := "-> " + str(m["to"])
		if changed, _ := m["changed"].(bool); !changed {
			what = "already " + str(m["to"])
		}
		moved := ""
		switch ids := strs(asAny(m["moved"])); len(ids) {
		case 0:
		case 1:
			moved = "moved " + ids[0]
		default:
			moved = fmt.Sprintf("moved %d, from %s", len(ids), ids[0])
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\n", str(m["address"]), what, moved)
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// asideVerb puts mail in the read-later pile, and takes it back out. It is a
// move and not a route: the Routing decides about senders, and what to read
// later is decided one conversation at a time — the daemon moves the whole
// thread of the id it is given.
func asideVerb(done bool) func(*input, io.Writer, io.Writer) int {
	return pileVerb("aside", done)
}

// replyLaterVerb puts mail in the reply-later pile, and takes it back out. Like
// aside it is a move and not a route: "I owe this a reply" is decided one
// conversation at a time, and the whole thread moves with the id.
func replyLaterVerb(done bool) func(*input, io.Writer, io.Writer) int {
	return pileVerb("reply-later", done)
}

// pileVerb is the shared body of the hand-tended piles: a Move into the named
// pile, or — with `done` — back out to the Inbox.
func pileVerb(name string, done bool) func(*input, io.Writer, io.Writer) int {
	cmd := []string{name}
	if done {
		cmd = append(cmd, "done")
	}
	return func(in *input, stdout, stderr io.Writer) int {
		return request(daemon.Request{
			ID: "1", Cmd: cmd, Args: map[string]any{"positional": in.Words},
		}, in.JSON(), printChanges, stdout, stderr)
	}
}

func encodeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
