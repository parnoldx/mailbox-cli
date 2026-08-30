package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// todoVerb lists a task list and changes it. Listing is answered from the
// Mirror; adding and completing wait for the server, so an exit code of 0 means
// it has happened (ADR-0004).
func todoVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		render := printTodos
		if verb != "list" {
			render = printTodo
		}
		return request(daemon.Request{
			ID: "1", Cmd: []string{"todo", verb},
			Args: map[string]any{
				"positional": in.Text(), "list": in.Str("list"), "all": in.Bool("all"),
				"due": in.Str("due"), "title": in.Str("title"),
				"priority": in.Str("priority"),
			},
		}, in.JSON(), render, stdout, stderr)
	}
}

// habitVerb lists and ticks off habits. A habit is a fact about a day, so the
// ones that change something take --date.
func habitVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		return request(daemon.Request{
			ID: "1", Cmd: []string{"habit", verb},
			Args: map[string]any{
				"positional": in.Text(), "date": in.Str("date"), "days": in.Str("days"),
				"color": in.Str("color"), "icon": in.Str("icon"),
			},
		}, in.JSON(), printHabits, stdout, stderr)
	}
}

func printTodos(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing on the list")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		if done, _ := m["done"].(bool); done {
			mark = "✓"
		} else if over, _ := m["overdue"].(bool); over {
			mark = "!"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\t%v\t%v\t%v\n", mark, m["id"], str(m["due"]),
			priorityMark(str(m["priority"])), truncate(str(m["summary"]), 48), str(m["list"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

func printTodo(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if state := str(m["state"]); state != "" {
		fmt.Fprintf(stdout, "%s %v %s\n", state, m["id"], str(m["summary"]))
		return
	}
	state := "open"
	if done, _ := m["done"].(bool); done {
		state = "done"
	}
	due := ""
	if v := str(m["due"]); v != "" {
		due = "  due " + v
	}
	if v := str(m["priority"]); v != "" {
		state = v + " priority, " + state
	}
	fmt.Fprintf(stdout, "%v  %s%s  (%s, %s)\n", m["id"], str(m["summary"]), due, str(m["list"]), state)
}

// priorityMark is the column a priority gets in a list. Only the two ends are
// worth a mark: everything is medium, so saying so on every row says nothing.
func priorityMark(priority string) string {
	switch priority {
	case "high":
		return "↑"
	case "low":
		return "↓"
	}
	return ""
}

func printHabits(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "no habits yet — add one with: mailbox habit add NAME")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mark := " "
		switch {
		case boolOf(m["done"]):
			mark = "✓"
		case !boolOf(m["due"]):
			mark = "–" // not due today, which is not the same as missed
		}
		streak := ""
		if n, ok := m["streak"].(float64); ok && n > 0 {
			streak = fmt.Sprintf("%d in a row", int(n))
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\t%v\n", mark, truncate(str(m["name"]), 32),
			strings.Join(strs(asAny(m["days"])), ","), streak)
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}
