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
		resp.Code, resp.Error = "api", "this account has no managesieve connection"
		return resp
	}
	verb := "list"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	name := strings.TrimSpace(str(req.Args["positional"]))

	names, active, err := d.Sieve.Scripts(ctx)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	switch verb {
	case "list":
		out := []script{}
		for _, n := range names {
			out = append(out, script{
				Name: n, Active: n == active, Ours: n == routing.ScriptName,
			})
		}
		resp.OK, resp.Data = true, out
		return resp

	case "get":
		// With no name it is the active script, because "what is actually
		// running" is the question this command is usually asked.
		if name == "" {
			if active == "" {
				resp.Code, resp.Error = "not_found", "no script is active"
				return resp
			}
			name = active
		}
		body, err := d.Sieve.Script(ctx, name)
		if err != nil {
			resp.Code, resp.Error = "not_found", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, body
		return resp

	case "put":
		if name == "" {
			resp.Code, resp.Error = "usage", "sieve put needs a script name"
			return resp
		}
		content, _ := req.Args["content"].(string)
		if strings.TrimSpace(content) == "" {
			resp.Code, resp.Error = "usage", "sieve put needs a script to upload"
			return resp
		}
		// The server compiles it and refuses what it cannot, which is the check
		// that matters — an uploaded script that does not compile would
		// otherwise sit there looking fine.
		if err := d.Sieve.PutScript(ctx, name, content, false); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, putResult{Name: name, Bytes: len(content), Active: name == active}
		return resp

	case "activate":
		if name == "" {
			resp.Code, resp.Error = "usage", "sieve activate needs a script name"
			return resp
		}
		if !hasScript(names, name) {
			resp.Code, resp.Error = "not_found", fmt.Sprintf("no script called %q on the server", name)
			return resp
		}
		if err := d.Sieve.SetActive(ctx, name); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := []script{}
		for _, n := range names {
			out = append(out, script{Name: n, Active: n == name, Ours: n == routing.ScriptName})
		}
		resp.OK, resp.Data = true, out
		return resp
	}
	resp.Code, resp.Error = "usage", fmt.Sprintf("unknown sieve command %q", verb)
	return resp
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
