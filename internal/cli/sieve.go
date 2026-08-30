package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// runBoxList says what Boxes there are and how much of each is held. It lives
// here rather than in cli.go because it is the listing `sieve` and `spam`
// arrived with, and because it is the one read that answers about the Mirror
// rather than out of it.
func runBoxList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"box", "list"},
		Args: map[string]any{"unread": in.Bool("unread"), "archive": in.Bool("archive")},
	}, in.JSON(), printBoxes, stdout, stderr)
}

// printBoxes prints one line per Box: the name that reads it, what is in it,
// and a mark for the ones the daemon watches rather than polls.
func printBoxes(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "no boxes match")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		if boolOf(m["watched"]) {
			mark = "*"
		}
		count := fmt.Sprintf("%d", int(numOf(m["count"])))
		if unseen := int(numOf(m["unseen"])); unseen > 0 {
			count = fmt.Sprintf("%d (%d new)", int(numOf(m["count"])), unseen)
		}
		// The count leads and the name trails: a box name can be sixty
		// characters deep and padding every row out to the longest one makes
		// the numbers unreadable. Names are never truncated — a name that
		// cannot be copied cannot be used.
		fmt.Fprintf(tw, "%s\t%v\t%v\n", mark, count, str(m["box"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

func runSieveList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{ID: "1", Cmd: []string{"sieve", "list"}},
		in.JSON(), printScripts, stdout, stderr)
}

// runSieveGet writes the script itself and nothing else, so it can be
// redirected into a file and put back unchanged.
func runSieveGet(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"sieve", "get"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), func(stdout, stderr io.Writer, resp daemon.Response) {
		body, ok := resp.Data.(string)
		if !ok {
			encodeJSON(stdout, resp.Data)
			return
		}
		fmt.Fprint(stdout, body)
		if !strings.HasSuffix(body, "\n") {
			fmt.Fprintln(stdout)
		}
	}, stdout, stderr)
}

// runSievePut reads the script here rather than at the daemon: a path on the
// command line is a path in this shell, and `-` is a pipe only this process has.
func runSievePut(in *input, stdout, stderr io.Writer) int {
	if len(in.Words) < 2 {
		fmt.Fprint(stderr, "sieve put needs a name and a file: mailbox sieve put NAME FILE\n")
		return ExitUsage
	}
	name, path := in.Words[0], in.Words[1]
	var (
		content []byte
		err     error
	)
	if path == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "cannot read %s: %v\n", path, err)
		return ExitUsage
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"sieve", "put"},
		Args: map[string]any{"positional": name, "content": string(content)},
	}, in.JSON(), func(stdout, stderr io.Writer, resp daemon.Response) {
		m, ok := resp.Data.(map[string]any)
		if !ok {
			encodeJSON(stdout, resp.Data)
			return
		}
		fmt.Fprintf(stdout, "stored %s, %d bytes\n", str(m["name"]), int(numOf(m["bytes"])))
		if !boolOf(m["active"]) {
			fmt.Fprintf(stderr, "notice: %s is not the active script — nothing changed until it is\n", str(m["name"]))
		}
	}, stdout, stderr)
}

func runSieveActivate(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"sieve", "activate"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printScripts, stdout, stderr)
}

// printScripts marks the active script and the one the routing owns, because
// those are the two facts that decide whether editing one is safe.
func printScripts(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "no scripts on the server")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		if boolOf(m["active"]) {
			mark = "*"
		}
		note := ""
		if boolOf(m["ours"]) {
			note = "the routing's own"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\n", mark, str(m["name"]), note)
	}
	_ = tw.Flush()
}
