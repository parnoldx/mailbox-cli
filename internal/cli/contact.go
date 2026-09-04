package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"mailbox/internal/daemon"
)

// contactVerb searches the address books and changes them. The search is a
// Mirror read and never waits on a network; adding waits for the server.
func contactVerb(verb string) func(*input, io.Writer, io.Writer) int {
	return func(in *input, stdout, stderr io.Writer) int {
		render := printContacts
		if verb != "search" && verb != "list" {
			render = printContact
		}
		return request(daemon.Request{
			ID: "1", Cmd: []string{"contact", verb},
			Args: map[string]any{
				"positional": in.Text(), "limit": in.Int("limit"), "book": in.Str("book"),
				"org": in.Str("org"), "note": in.Str("note"), "value": in.Str("value"),
				"email": in.List("email"), "phone": in.List("phone"),
			},
		}, in.JSON(), render, stdout, stderr)
	}
}

func printContacts(stdout, stderr io.Writer, resp daemon.Response) {
	rows, ok := rowsOf(stdout, resp.Data)
	if !ok {
		return
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "nobody matched")
		return
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		emails := strs(asAny(m["emails"]))
		phones := strs(asAny(m["phones"]))
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n", m["id"], truncate(str(m["name"]), 28),
			truncate(strings.Join(emails, " "), 34), truncate(strings.Join(phones, " "), 24))
	}
	_ = tw.Flush()
	behindNotice(stderr, resp)
}

func printContact(stdout, stderr io.Writer, resp daemon.Response) {
	m, ok := fieldsOf(stdout, resp.Data)
	if !ok {
		return
	}
	if state := str(m["state"]); state != "" {
		fmt.Fprintf(stdout, "%s %v %s\n", state, m["id"], str(m["name"]))
		return
	}
	fmt.Fprintf(stdout, "%-8s %s\n", "Name:", str(m["name"]))
	for _, f := range []struct{ label, key string }{{"Org", "organisation"}, {"Book", "book"}} {
		if v := str(m[f.key]); v != "" {
			fmt.Fprintf(stdout, "%-8s %s\n", f.label+":", v)
		}
	}
	for _, e := range strs(asAny(m["emails"])) {
		fmt.Fprintf(stdout, "%-8s %s\n", "Mail:", e)
	}
	for _, p := range strs(asAny(m["phones"])) {
		fmt.Fprintf(stdout, "%-8s %s\n", "Phone:", p)
	}
	if note := str(m["note"]); note != "" {
		fmt.Fprintf(stdout, "\n%s\n", note)
	}
	behindNotice(stderr, resp)
}
