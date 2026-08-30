package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// labelVerb is the read half: the listing of one label's mail.
func labelVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		return request(daemon.Request{
			ID: "1", Cmd: []string{"label", verb},
			Args: map[string]any{"name": in.Text(), "limit": in.Int("limit")},
		}, in.JSON(), printTable, stdout, stderr)
	}
}

// labelApply is the write half. The label is a flag and the ids are the words,
// so `mailbox label add 1 2 --to learn` reads the way it is written; add and
// remove name that flag differently because "--from learn" is what taking one
// off sounds like.
func labelApply(verb, flag string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		name := in.Str(flag)
		if name == "" {
			fmt.Fprintf(stderr, "label %s needs a label: --%s NAME\n", verb, flag)
			return ExitUsage
		}
		return request(daemon.Request{
			ID: "1", Cmd: []string{"label", verb},
			Args: map[string]any{"positional": in.Words, "name": name},
		}, in.JSON(), printChanges, stdout, stderr)
	}
}

// runLabelCreate takes the label first and any ids after it, which is the one
// place in this CLI where the leading word is not an id.
func runLabelCreate(in *input, stdout, stderr io.Writer) int {
	name, ids := in.Words[0], in.Words[1:]
	render := printLabels
	if len(ids) > 0 {
		render = printChanges
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"label", "create"},
		Args: map[string]any{"name": name, "positional": ids},
	}, in.JSON(), render, stdout, stderr)
}

func runLabelList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{ID: "1", Cmd: []string{"label", "list"}},
		in.JSON(), printLabels, stdout, stderr)
}

func printLabels(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "no labels yet — make one with: mailbox label add ID --to NAME")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		count := ""
		if n := int(numOf(m["count"])); n > 0 {
			count = fmt.Sprintf("%d", n)
		}
		fmt.Fprintf(tw, "%v\t%v\n", str(m["label"]), count)
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// runForward sends a message on. The body is a note above the original, so an
// empty one is ordinary here and is not read from stdin the way send's is.
func runForward(in *input, stdout, stderr io.Writer) int {
	if len(in.List("to")) == 0 {
		fmt.Fprint(stderr, "forward needs a recipient: --to ADDR\n")
		return ExitUsage
	}
	paths, code := attachments(in.List("attach"), stderr)
	if code != ExitOK {
		return code
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"forward"},
		Args: map[string]any{
			"positional": in.First(), "to": in.List("to"), "cc": in.List("cc"),
			"subject": in.Str("subject"), "body": in.Str("body"), "attach": paths,
		},
	}, in.JSON(), printSent, stdout, stderr)
}

// eventVerb adds, changes and removes calendar entries.
func eventVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		return request(daemon.Request{
			ID: "1", Cmd: []string{"event", verb},
			Args: map[string]any{
				"positional": in.Text(), "title": in.Str("title"),
				"start": in.Str("start"), "end": in.Str("end"),
				"calendar": in.Str("calendar"), "location": in.Str("location"),
				"notes": in.Str("notes"), "all_day": in.Bool("all-day"),
				"url": in.Str("url"), "repeat": in.Str("repeat"), "alarm": in.Str("alarm"),
			},
		}, in.JSON(), printEventChange, stdout, stderr)
	}
}

// printEventChange says what landed and where, in the id form that reads it.
func printEventChange(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	fmt.Fprintf(stdout, "%s %v  %s", str(m["state"]), m["id"], str(m["summary"]))
	if cal := str(m["calendar"]); cal != "" {
		fmt.Fprintf(stdout, "  (%s)", cal)
	}
	fmt.Fprintln(stdout)
}

// draftVerb reads and changes the unsent pile. The id may be bare — `draft` has
// already said which box this is about — so it is passed through untouched and
// the daemon resolves it against the drafts box.
func draftVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		render := printTable
		switch verb {
		case "show":
			render = printMessage
		case "edit", "send":
			render = printDraftSaved
		case "delete":
			render = printChanges
		}
		return request(daemon.Request{
			ID: "1", Cmd: []string{"draft", verb},
			Args: map[string]any{
				"positional": in.First(), "limit": in.Int("limit"),
				"to": in.List("to"), "cc": in.List("cc"),
				"subject": in.Str("subject"), "body": in.Str("body"),
			},
		}, in.JSON(), render, stdout, stderr)
	}
}

// printDraftSaved names the new id, because editing a draft on imap gives it
// one: the old id is gone the moment this returns.
func printDraftSaved(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		encodeJSON(stdout, resp.Data)
		return
	}
	if state := str(m["state"]); state == "saved" {
		if id := str(m["id"]); id != "" {
			fmt.Fprintf(stdout, "saved %s  %s\n", id, str(m["subject"]))
			return
		}
		fmt.Fprintf(stdout, "saved  %s\n", str(m["subject"]))
		fmt.Fprintln(stderr, "notice: the server did not say what id it has now — try draft list")
		return
	}
	printSent(stdout, stderr, resp)
}
