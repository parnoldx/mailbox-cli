// Package cli is a socket client and nothing else. With no Daemon listening it
// fails rather than reading the Mirror itself or falling back to the network
// (ADR-0012).
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/daemon"
)

// Exit codes, matching the envelope's code field.
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitNotFound = 2
	ExitAPI      = 7
	// ExitDaemon means no Daemon is listening. Under socket activation this is
	// a setup problem, and should read like one.
	ExitDaemon = 9
)

func runBoxView(in *input, stdout, stderr io.Writer) int {
	box := in.First()
	if box == "" {
		box = "inbox"
	}
	return request(daemon.Request{
		ID:   "1",
		Cmd:  []string{"box", "view"},
		Args: map[string]any{"positional": box, "limit": in.Int("limit")},
	}, in.JSON(), printTable, stdout, stderr)
}

func runMessageView(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID:   "1",
		Cmd:  []string{"message", "view"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printMessage, stdout, stderr)
}

func runStatus(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{ID: "1", Cmd: []string{"status"}},
		in.JSON(), printStatus, stdout, stderr)
}

// runAttachmentList says what a Message carries. It is a Mirror read; saving is
// the one read that waits on the server (ADR-0003).
func runAttachmentList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"attachment", "list"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printAttachments, stdout, stderr)
}

func runAttachmentSave(in *input, stdout, stderr io.Writer) int {
	// The Daemon writes the file, so the path it is given has to mean the same
	// thing there as it does here: an absolute one. With no --output that is
	// this directory, and the file keeps the name the sender gave it.
	output := in.Str("output")
	if output == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "cannot resolve the current directory: %v\n", err)
			return ExitAPI
		}
		output = wd
	} else if !filepath.IsAbs(output) {
		abs, err := filepath.Abs(output)
		if err != nil {
			fmt.Fprintf(stderr, "cannot resolve %s: %v\n", output, err)
			return ExitAPI
		}
		output = abs
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"attachment", "save"},
		Args: map[string]any{
			"positional": in.First(), "output": output, "force": in.Bool("force"),
		},
	}, in.JSON(), printSaved, stdout, stderr)
}

func printAttachments(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing attached")
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		size := ""
		if n, ok := m["size"].(float64); ok {
			size = humanBytes(int64(n))
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n", m["id"], str(m["filename"]), str(m["mime_type"]), size)
	}
	_ = tw.Flush()
	printProblems(stdout, resp)
	behindNotice(stderr, resp)
}

// printProblems prints what the Daemon needs a human for. There are never many:
// an unloadable config, a server refusing a password, and mail that was at the
// SMTP server when the daemon stopped. Everything that resolves itself is a log
// line instead.
func printProblems(stdout io.Writer, resp daemon.Response) {
	if len(resp.Problems) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "problems")
	for _, p := range resp.Problems {
		fmt.Fprintf(stdout, "  %s: %s\n", p.Name, p.Detail)
	}
}

func printSaved(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	size := ""
	if n, ok := m["bytes"].(float64); ok {
		size = " (" + humanBytes(int64(n)) + ")"
	}
	fmt.Fprintf(stdout, "%s%s\n", str(m["path"]), size)
}

// humanBytes is for reading, not for arithmetic.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// runThread reads a whole conversation, from any Message in it.
func runThread(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"thread", "view"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printThread, stdout, stderr)
}

// printThread prints a conversation oldest first, each Message under a rule
// carrying the id that reads it on its own.
func printThread(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	for i, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "── %v  %v  %v\n", m["id"], m["date"], str(m["from"]))
		if i == 0 {
			fmt.Fprintf(stdout, "   %s\n", str(m["subject"]))
		}
		fmt.Fprintln(stdout)
		if body := str(m["body"]); body != "" {
			fmt.Fprintln(stdout, body)
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "notice: the mirror holds no messages for that thread")
	}
	behindNotice(stderr, resp)
}

// runSearch reads a query and its filters. Words before the flags are the
// query, so `mailbox search rechnung mai --in feed` reads the way it is written.
func runSearch(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID:  "1",
		Cmd: []string{"search"},
		Args: map[string]any{
			"positional": in.Text(),
			"in":         in.Str("in"), "from": in.Str("from"), "limit": in.Int("limit"),
		},
	}, in.JSON(), printHits, stdout, stderr)
}

// printHits prints one line per Message: the id to read it with, where it is,
// and the text around the match.
func printHits(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing matched")
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n", m["id"], m["date"],
			truncate(str(m["from"]), 24), truncate(str(m["subject"]), 40))
		if snip := str(m["snippet"]); snip != "" {
			fmt.Fprintf(tw, "\t\t\t%s\n", truncate(snip, 72))
		}
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// writeVerb handles the write verbs that take ids and nothing else. They block
// on the server: when one of these exits 0 the change has happened, and a read
// straight after it sees the result (ADR-0004).
func writeVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		return request(daemon.Request{
			ID: "1", Cmd: []string{verb}, Args: map[string]any{"positional": in.Words},
		}, in.JSON(), printChanges, stdout, stderr)
	}
}

func runMove(in *input, stdout, stderr io.Writer) int {
	to := in.Str("to")
	if to == "" {
		fmt.Fprint(stderr, "move needs a destination: --to BOX\n")
		return ExitUsage
	}
	return request(daemon.Request{
		ID: "1", Cmd: []string{"move"},
		Args: map[string]any{"positional": in.Words, "to": to},
	}, in.JSON(), printChanges, stdout, stderr)
}

// printChanges says what a write did, one line per Message, in the id form the
// next command would use.
func printChanges(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		moved, _ := m["moved"].(bool)
		switch {
		case moved && str(m["new_id"]) != "":
			fmt.Fprintf(tw, "%v\t-> %v\n", m["id"], str(m["new_id"]))
		case moved:
			// Moved somewhere the Mirror does not hold, or the server did not
			// say where. Either way the id it had is gone.
			fmt.Fprintf(tw, "%v\t-> %v\n", m["id"], str(m["box"]))
		default:
			flags := strings.Join(strs(asAny(m["flags"])), " ")
			if flags == "" {
				flags = "no flags"
			}
			fmt.Fprintf(tw, "%v\t%v\n", m["id"], flags)
		}
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// rowsOf reads a listing out of a reply. A reply with no data at all is an
// empty listing: the daemon builds its lists empty, but a field that is absent
// for any other reason should read the same way rather than printing "null".
func rowsOf(data any) ([]any, bool) {
	if data == nil {
		return nil, true
	}
	rows, ok := data.([]any)
	return rows, ok
}

func asAny(v any) []any {
	out, _ := v.([]any)
	return out
}

// request sends one command and prints the reply. Pushes may arrive on the same
// connection; they carry no id and are skipped here.
func request(req daemon.Request, asJSON bool, render renderer, stdout, stderr io.Writer) int {
	conn, err := net.Dial("unix", config.SocketPath())
	if err != nil {
		fmt.Fprintf(stderr, "no daemon listening at %s\n", config.SocketPath())
		fmt.Fprintf(stderr, "start one with: mailbox daemon\n")
		return ExitDaemon
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fmt.Fprintf(stderr, "write: %v\n", err)
		return ExitAPI
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var resp daemon.Response
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			continue
		}
		if resp.ID != req.ID {
			continue // a push
		}
		if asJSON {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(resp)
		} else if !resp.OK {
			fmt.Fprintf(stderr, "%s\n", resp.Error)
		} else {
			render(stdout, stderr, resp)
		}
		if resp.OK {
			return ExitOK
		}
		return codeToExit(resp.Code)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(stderr, "read: %v\n", err)
	}
	return ExitAPI
}

func codeToExit(code string) int {
	switch code {
	case "usage":
		return ExitUsage
	case "not_found":
		return ExitNotFound
	default:
		return ExitAPI
	}
}

// renderer turns a successful reply into the plain-text form of that command.
type renderer func(stdout, stderr io.Writer, resp daemon.Response)

// printMessage prints one Message: a short header block, then its text. The
// body has already been rendered and sanitised by the Daemon, so this is only
// layout.
func printMessage(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	for _, f := range []struct{ label, key string }{
		{"Date", "date"}, {"From", "from"}, {"To", "to"}, {"Subject", "subject"},
	} {
		if v := str(m[f.key]); v != "" {
			fmt.Fprintf(stdout, "%-8s %s\n", f.label+":", v)
		}
	}
	if places, ok := m["placements"].([]any); ok && len(places) > 1 {
		fmt.Fprintf(stdout, "%-8s %s\n", "Also:", strings.Join(strs(places), ", "))
	}
	fmt.Fprintln(stdout)
	switch body := str(m["body"]); {
	case body != "":
		fmt.Fprintln(stdout, body)
	case str(m["body_state"]) != "mirrored":
		fmt.Fprintln(stderr, "notice: the mirror has this message's headers but not its text yet")
	default:
		fmt.Fprintln(stderr, "notice: this message has no text parts")
	}
	behindNotice(stderr, resp)
}

// printStatus says what the Mirror holds, one line per account. With one
// account it is one line, which is what it always was.
func printStatus(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		if primary, _ := m["primary"].(bool); primary {
			mark = "*"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v boxes\t%v in %v\twatching %v\n", mark, str(m["account"]),
			numOf(m["boxes"]), numOf(m["count"]), str(m["folder"]),
			strings.Join(strs(asAny(m["watched"])), ", "))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

func numOf(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func printTable(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	// Silence on an empty listing reads like a command that failed quietly, so
	// it says so — on stderr, because it is not a result.
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing there")
		behindNotice(stderr, resp)
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		if seen, _ := m["seen"].(bool); !seen {
			mark = "*"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\t%v\t%v\n", mark, m["id"], m["date"], truncate(str(m["from"]), 28), str(m["subject"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// behindNotice mentions the Mirror only when it is Behind. A stale answer is
// still an answer, so this is a notice and not an error (ADR-0001).
//
// What it reports is the freshness of the data this command answered from, not
// of the Mirror in general: mail and the collections have separate loops, and
// "behind" with no age is a sentence a caller can do nothing with. The age says
// whether it matters, and a running cycle says whether asking again will help.
func behindNotice(stderr io.Writer, resp daemon.Response) {
	st := resp.Mirror
	if st == nil || st.Connected {
		return
	}
	msg := "notice: mirror is behind — the daemon has not reached the server yet"
	if st.SyncedAt != nil {
		msg = fmt.Sprintf("notice: mirror is behind — last reached %s ago", since(*st.SyncedAt))
	}
	if st.Syncing {
		msg += "; a sync is running"
	}
	fmt.Fprintln(stderr, msg)
}

// since is a duration at the resolution somebody reading a notice cares about.
// "41 minutes" is the useful part of 41m18.4s, and rounding down is the honest
// direction: an answer is never fresher than this says.
func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	}
	return plural(int(d.Hours()/24), "day")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func strs(vs []any) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, str(v))
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
