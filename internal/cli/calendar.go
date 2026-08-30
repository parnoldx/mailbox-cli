package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"mailbox/internal/daemon"
)

// runAgenda asks what is on. The window is the question: a repeating event has
// no finite list of instances, so the daemon expands the rule over the days
// that were asked for.
func runAgenda(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"agenda"},
		Args: map[string]any{
			"days": in.Int("days"), "from": in.Str("from"),
			"calendar": in.Str("calendar"), "limit": in.Int("limit"),
		},
	}, in.JSON(), printAgenda, stdout, stderr)
}

// runCalendarList lists the collections the Mirror holds.
func runCalendarList(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"calendar", "list"},
		Args: map[string]any{"kind": in.Str("kind")},
	}, in.JSON(), printCalendars, stdout, stderr)
}

// runEventView reads one entry whole, with the next few times it happens.
func runEventView(in *input, stdout, stderr io.Writer) int {
	return request(daemon.Request{
		ID: "1", Cmd: []string{"event", "view"},
		Args: map[string]any{"positional": in.First()},
	}, in.JSON(), printEvent, stdout, stderr)
}

// printAgenda prints the days, each with what is on it. The id comes first,
// because the next thing a caller does is read one of them.
func printAgenda(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nothing in that window")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	day := ""
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if d := str(m["date"]); d != day {
			day = d
			if day != "" {
				_ = tw.Flush()
				fmt.Fprintf(stdout, "%s\n", dayHeading(day))
			}
		}
		mark := ""
		if rec, _ := m["recurring"].(bool); rec {
			mark = " ↻"
		}
		fmt.Fprintf(tw, "  %v\t%v\t%v%s\t%v\n", m["id"], str(m["time"]),
			truncate(str(m["summary"]), 44), mark, str(m["calendar"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// dayHeading writes the weekday out, because "is that a Saturday" is the second
// question anybody asks about a date.
func dayHeading(date string) string {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	today := time.Now().Local().Format("2006-01-02")
	suffix := ""
	switch date {
	case today:
		suffix = "  (today)"
	case time.Now().Local().AddDate(0, 0, 1).Format("2006-01-02"):
		suffix = "  (tomorrow)"
	}
	return t.Format("Mon 2006-01-02") + suffix
}

func printCalendars(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(resp.Data)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "no calendars in the mirror yet")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		count := ""
		if n, ok := m["count"].(float64); ok {
			count = fmt.Sprintf("%d entries", int(n))
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n", str(m["name"]), str(m["kind"]), count, str(m["synced_at"]))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

// minutes says the reminders the way somebody sets them: so long before it
// starts, rather than as a signed duration.
func minutes(v any) string {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, n := range list {
		f, ok := n.(float64)
		if !ok {
			continue
		}
		m := int(f)
		switch {
		case m == 0:
			out = append(out, "at the start")
		case m%60 == 0:
			out = append(out, fmt.Sprintf("%dh before", m/60))
		default:
			out = append(out, fmt.Sprintf("%dm before", m))
		}
	}
	return strings.Join(out, ", ")
}

func printEvent(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := resp.Data.(map[string]any)
	if !ok {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp.Data)
		return
	}
	fmt.Fprintf(stdout, "%-10s %s\n", "Summary:", str(m["summary"]))
	for _, f := range []struct{ label, key string }{
		{"Calendar", "calendar"}, {"Location", "location"}, {"Status", "status"},
		{"Link", "url"}, {"Repeats", "repeat"},
	} {
		if v := str(m[f.key]); v != "" {
			fmt.Fprintf(stdout, "%-10s %s\n", f.label+":", v)
		}
	}
	if alarms := minutes(m["alarms"]); alarms != "" {
		fmt.Fprintf(stdout, "%-10s %s\n", "Reminds:", alarms)
	}
	if next, ok := m["next"].([]any); ok && len(next) > 0 {
		fmt.Fprintln(stdout, "Next:")
		for _, n := range next {
			o, ok := n.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(stdout, "  %s  %s\n", dayHeading(str(o["date"])), str(o["time"]))
		}
	}
	if desc := str(m["description"]); desc != "" {
		fmt.Fprintf(stdout, "\n%s\n", desc)
	}
	behindNotice(stderr, resp)
}
