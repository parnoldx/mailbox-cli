package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// runCompose writes a mail and sends it, or files it in drafts. Everything the
// Daemon needs is on the command line or on stdin: this is a CLI an agent
// drives, so there is no editor and no prompt.
func runCompose(in *input, stdout, stderr io.Writer) int {
	to := in.List("to")
	if len(to) == 0 {
		fmt.Fprint(stderr, "compose needs a recipient: --to ADDR\n")
		return ExitUsage
	}
	if in.Str("subject") == "" {
		fmt.Fprint(stderr, "compose needs a --subject\n")
		return ExitUsage
	}
	text, code := composeBody(in, stderr)
	if code != ExitOK {
		return code
	}
	paths, code := attachments(in.List("attach"), stderr)
	if code != ExitOK {
		return code
	}
	// A draft is not a send and does not go through the outbox: it is an
	// append to the drafts box, which is what `draft edit` does too.
	cmd, render := []string{"send"}, printSent
	if in.Bool("draft") {
		cmd, render = []string{"draft", "save"}, printDraftSaved
	}
	return request(daemon.Request{
		ID: "1", Cmd: cmd,
		Args: withReplyWatch(map[string]any{
			"to": to, "cc": in.List("cc"), "bcc": in.List("bcc"),
			"subject": in.Str("subject"), "body": text, "attach": paths,
			"body_html": in.Str("body-html"), "account": in.Str("account"),
		}, in),
	}, in.JSON(), render, stdout, stderr)
}

// withReplyWatch adds the "if no reply by" reminder flags — HEY's Bubble Up,
// applied to a message on its way out instead of one already sitting in the
// Inbox. --if-no-reply is the switch; the timing is the same one flag `mailbox
// bubble` takes (bubbleWhen on the daemon side), shared by send, reply,
// forward and draft send.
func withReplyWatch(args map[string]any, in *input) map[string]any {
	args["if_no_reply"] = in.Bool("if-no-reply")
	args["on"] = in.Str("on")
	args["tomorrow"] = in.Bool("tomorrow")
	args["weekend"] = in.Bool("weekend")
	args["next_week"] = in.Bool("next-week")
	return args
}

// runReply answers a Message. The recipients and the References come from the
// Daemon's copy of the parent, so a caller never assembles a thread by hand.
func runReply(in *input, stdout, stderr io.Writer) int {
	text, code := composeBody(in, stderr)
	if code != ExitOK {
		return code
	}
	paths, code := attachments(in.List("attach"), stderr)
	if code != ExitOK {
		return code
	}
	// The daemon builds the reply either way: who to answer and what thread it
	// belongs to come from the parent, so --draft changes where it lands and
	// nothing else.
	render := printSent
	if in.Bool("draft") {
		render = printDraftSaved
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"reply"},
		Args: withReplyWatch(map[string]any{
			"positional": in.First(), "all": in.Bool("all"),
			"to": in.List("to"), "cc": in.List("cc"),
			"subject": in.Str("subject"), "body": text, "attach": paths,
			"body_html": in.Str("body-html"), "draft": in.Bool("draft"),
		}, in),
	}, in.JSON(), render, stdout, stderr)
}

// outboxVerb shows the queue, or moves one mail in it.
func outboxVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		render := printOutbox
		if verb != "list" {
			render = printSent
		}
		return request(daemon.Request{
			ID: "1", Cmd: []string{"outbox", verb},
			Args: map[string]any{"positional": in.First()},
		}, in.JSON(), render, stdout, stderr)
	}
}

// composeBody is the body a send or reply carries. Without --body-html it is
// --body or stdin, as before, and the daemon renders it from Markdown. With
// --body-html the HTML is the body: --body, if given, is the plain-text twin,
// and stdin is left alone so a `--body-html` mail is not a hang on the tty.
func composeBody(in *input, stderr io.Writer) (string, int) {
	if in.Str("body-html") != "" {
		return in.Str("body"), ExitOK
	}
	return bodyText(in.Str("body"), stderr)
}

// bodyText takes the body from --body, or from stdin when it is piped in. A
// mail whose text came from a heredoc is the ordinary case for an agent.
func bodyText(body string, stderr io.Writer) (string, int) {
	if body != "" && body != "-" {
		return body, ExitOK
	}
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 && body != "-" {
		// A terminal, and no --body: there is nothing to send and nothing to
		// wait for. Waiting on a tty here would just look like a hang.
		fmt.Fprint(stderr, "no body: pass --body TEXT, or pipe the text in\n")
		return "", ExitUsage
	}
	text, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(stderr, "read body: %v\n", err)
		return "", ExitAPI
	}
	return string(text), ExitOK
}

// attachments resolves each path the way `attachment save` resolves --output:
// the Daemon reads the file, so the path has to mean the same thing there as it
// does in the caller's shell.
func attachments(paths []string, stderr io.Writer) ([]string, int) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(stderr, "cannot resolve %s: %v\n", p, err)
			return nil, ExitAPI
		}
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(stderr, "cannot attach %s: %v\n", p, err)
			return nil, ExitUsage
		}
		out = append(out, abs)
	}
	return out, ExitOK
}

// repeated is a flag that may be given more than once.
type repeated []string

func (r *repeated) String() string { return strings.Join(*r, ", ") }

func (r *repeated) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func runRSVP(in *input, stdout, stderr io.Writer) int {
	n := 0
	for _, f := range []string{"accept", "decline", "tentative"} {
		if in.Bool(f) {
			n++
		}
	}
	if n != 1 {
		fmt.Fprint(stderr, "rsvp needs one of --accept, --decline, --tentative\n")
		return ExitUsage
	}
	return request(daemon.Request{
		ID:  "1",
		Cmd: []string{"rsvp"},
		Args: map[string]any{
			"positional": in.First(),
			"accept":     in.Bool("accept"),
			"decline":    in.Bool("decline"),
			"tentative":  in.Bool("tentative"),
			"calendar":   in.Str("calendar"),
		},
	}, in.JSON(), printSent, stdout, stderr)
}

// printSent says what happened to the mail, in two facts: it went, and where
// the copy of it is.
func printSent(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := fieldsOf(stdout, resp.Data)
	if !ok {
		return
	}
	if state := str(m["state"]); state == "cancelled" {
		fmt.Fprintf(stdout, "cancelled #%v\n", m["id"])
		return
	}
	fmt.Fprintf(stdout, "sent to %s\n", strings.Join(strs(asAny(m["recipients"])), ", "))
	if id := str(m["id"]); id != "" {
		fmt.Fprintf(stdout, "filed as %s\n", id)
	} else {
		fmt.Fprintln(stderr, "notice: the copy is not filed yet — the daemon will retry it")
	}
}

func printOutbox(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(stdout, resp.Data)
	if !ok {
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "the outbox is empty")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		where := str(m["placement"])
		if where == "" {
			where = strings.Join(strs(asAny(m["recipients"])), ", ")
		}
		fmt.Fprintf(tw, "#%v\t%v\t%v\t%v\t%v\n", m["id"], m["state"], m["created"],
			truncate(str(m["subject"]), 40), truncate(where, 40))
		if e := str(m["error"]); e != "" {
			fmt.Fprintf(tw, "\t\t\t%s\n", truncate(strings.ReplaceAll(e, "\n", " "), 72))
		}
	}
	_ = tw.Flush()
}
