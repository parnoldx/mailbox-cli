package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"mailbox/internal/daemon"
)

// runPickupList reads the Pickups the Daemon is still holding: the mail is
// already read and already on the clipboard when it was collected, so this is
// how the code or the link is found a second time — after the clipboard has
// been overwritten by the next thing copied, which is what happens to a link
// you were not ready to paste.
func runPickupList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{Cmd: []string{"pickup", "list"}},
		in.JSON(), printPickups, stdout, stderr)
}

// runPickupCopy hands one Pickup over again: the code, or the link, back on the
// clipboard and said out loud. Copied, never followed, exactly as on arrival.
func runPickupCopy(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		Cmd:  []string{"pickup", "copy"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printPickupCopied, stdout, stderr)
}

// printPickups lists what is held and what each one carries. A code is shown as
// itself — it is short and there is nothing to protect it from, since reading
// it is the whole errand — while a link shows its host and never its token.
func printPickups(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(stdout, resp.Data)
	if !ok {
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing has been collected")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n",
			str(m["id"]), truncate(str(m["from"]), 30), arrival(str(m["arrived"])), held(m))
	}
	tw.Flush()
}

// printPickupCopied confirms the hand-over by naming the same readable half the
// table would: the host of a link, or the code itself.
func printPickupCopied(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		return
	}
	fmt.Fprintf(stdout, "copied\t%s\n", held(m))
}

// held is what a caller is being offered: the code, or the link's host.
func held(m map[string]any) string {
	if code := str(m["code"]); code != "" {
		return code
	}
	if host := str(m["host"]); host != "" {
		return host
	}
	// No host means the link would not parse. Show it rather than nothing:
	// a token you cannot decode still tells you something is there.
	if link := str(m["link"]); link != "" {
		return truncate(link, 50)
	}
	return "?"
}

// arrival is the held-for term: how long ago the mail landed, which is the
// clock the expiry runs on. A stamp that will not parse is printed as it came
// rather than as "0 seconds" — a wrong age reads as a fresh code.
func arrival(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return since(t) + " ago"
}
