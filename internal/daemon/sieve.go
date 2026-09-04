package daemon

import (
	"context"
	"fmt"
	"strings"

	"mailbox/internal/routing"
)

// script is one stored Sieve script, and whether it is the one the server runs.
type script struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// Ours marks the script the Routing owns. Editing that one by hand is how
	// a caller takes triage away from `mailbox route`, so it is worth saying.
	Ours bool `json:"ours"`
}

// handleSieve is raw access to the scripts on the server, for the cases the
// Routing does not cover: reading what is actually there, and putting a script
// back when something outside this program has left the server wrong.
//
// It is deliberately not guarded. `mailbox route` owns one script and says so
// in every listing; a caller reaching for `sieve put` has left the paved road
// on purpose, and a --force flag on the way out would only be in the way of
// the one job this command exists for.
func (d *Daemon) handleSieve(ctx context.Context, req Request, resp Response) Response {
	if d.Sieve == nil {
		return resp.api("this account has no managesieve connection")
	}
	verb := req.Verb("list")
	name := strings.TrimSpace(req.Str("positional"))

	names, active, err := d.Sieve.Scripts(ctx)
	if err != nil {
		return resp.api(err.Error())
	}

	switch verb {
	case "list":
		out := []script{}
		for _, n := range names {
			out = append(out, script{
				Name: n, Active: n == active, Ours: n == routing.ScriptName,
			})
		}
		return resp.ok(out)

	case "get":
		// With no name it is the active script, because "what is actually
		// running" is the question this command is usually asked.
		if name == "" {
			if active == "" {
				return resp.notFound("no script is active")
			}
			name = active
		}
		body, err := d.Sieve.Script(ctx, name)
		if err != nil {
			return resp.notFound(err.Error())
		}
		return resp.ok(body)

	case "put":
		if name == "" {
			return resp.usage("sieve put needs a script name")
		}
		content := req.Str("content")
		if strings.TrimSpace(content) == "" {
			return resp.usage("sieve put needs a script to upload")
		}
		// The server compiles it and refuses what it cannot, which is the check
		// that matters — an uploaded script that does not compile would
		// otherwise sit there looking fine.
		if err := d.Sieve.PutScript(ctx, name, content, false); err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(putResult{Name: name, Bytes: len(content), Active: name == active})

	case "activate":
		if name == "" {
			return resp.usage("sieve activate needs a script name")
		}
		if !hasScript(names, name) {
			return resp.notFound(fmt.Sprintf("no script called %q on the server", name))
		}
		if err := d.Sieve.SetActive(ctx, name); err != nil {
			return resp.api(err.Error())
		}
		out := []script{}
		for _, n := range names {
			out = append(out, script{Name: n, Active: n == name, Ours: n == routing.ScriptName})
		}
		return resp.ok(out)
	}
	return resp.usage(fmt.Sprintf("unknown sieve command %q", verb))
}

// putResult says what landed, and whether it is what the server is running —
// uploading over the active script changes behaviour immediately, and uploading
// beside it changes nothing until somebody activates it.
type putResult struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	Active bool   `json:"active"`
}

func hasScript(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
