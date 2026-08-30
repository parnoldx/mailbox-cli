package setup

import (
	"context"
	"fmt"
	"strings"

	"mailbox/internal/routing"
)

// RoutingBoxes are the Boxes the Routing needs on the Primary Account. A fresh
// account has none of them, and until it does every `mailbox route` call is
// refused with the Box named (ADR-0019) — a `fileinto` into a Box that is not
// there files nowhere.
//
// Setup creates them because setup is the only place a human is present to be
// asked. ADR-0019's rule is unchanged: a routing decision still never creates a
// Box.
var RoutingBoxes = []string{
	routing.BoxScreener,
	routing.BoxFeed,
	routing.BoxPaperTrail,
	routing.BoxAside,
	routing.BoxBlock,
}

// BoxMaker creates a Box on the Primary Account.
type BoxMaker interface {
	CreateFolder(ctx context.Context, name string) error
}

// SieveOps is the part of ManageSieve this needs.
type SieveOps interface {
	Scripts(ctx context.Context) (names []string, active string, err error)
	Script(ctx context.Context, name string) (string, error)
	PutScript(ctx context.Context, name, content string, activate bool) error
}

// Bootstrap is what the account looked like afterwards.
type Bootstrap struct {
	// Created are the Boxes that were not there.
	Created []string
	// Wrote says an empty Routing script was put up. A script already on the
	// account is never rewritten here: it holds decisions.
	Wrote bool
	// Activated says ours was made the active script, which only happens on an
	// account running nothing at all.
	Activated bool
	// Active is the script the server runs, if any.
	Active string
	// Unreachable says the active script neither is ours nor includes it, so
	// the Routing would be stored and never run. Setup reports it and does not
	// fix it: switching somebody's filtering off to switch ours on is not a
	// thing a wizard does.
	Unreachable bool
}

// MissingBoxes are the Routing Boxes the account has not got.
func MissingBoxes(have []string) []string {
	known := map[string]bool{}
	for _, h := range have {
		known[strings.ToUpper(h)] = true
	}
	var out []string
	for _, want := range RoutingBoxes {
		if !known[strings.ToUpper(want)] {
			out = append(out, want)
		}
	}
	return out
}

// EnsureRouting creates the missing Boxes and, if the account has no Routing
// script at all, puts an empty one up.
func EnsureRouting(ctx context.Context, mk BoxMaker, sv SieveOps, have []string) (Bootstrap, error) {
	var b Bootstrap
	for _, name := range MissingBoxes(have) {
		if err := mk.CreateFolder(ctx, name); err != nil {
			return b, err
		}
		b.Created = append(b.Created, name)
	}
	if sv == nil {
		return b, nil
	}

	names, active, err := sv.Scripts(ctx)
	if err != nil {
		return b, fmt.Errorf("sieve: %w", err)
	}
	b.Active = active
	ours := false
	for _, n := range names {
		if n == routing.ScriptName {
			ours = true
		}
	}
	if !ours {
		// An empty script: no sender has been decided about yet. Activated only
		// on an account running nothing, because activating deactivates
		// whatever was running and that is somebody else's filtering.
		if err := sv.PutScript(ctx, routing.ScriptName, routing.New().Script(), active == ""); err != nil {
			return b, fmt.Errorf("sieve: %w", err)
		}
		b.Wrote = true
		if active == "" {
			b.Activated = true
			b.Active = routing.ScriptName
			return b, nil
		}
	}

	// Reachability, not activity: the Routing runs when ours is active or when
	// the active script includes it, and the second is the ordinary case on an
	// account whose webmail wrote the first one (ADR-0019).
	if b.Active == "" || b.Active == routing.ScriptName {
		return b, nil
	}
	src, err := sv.Script(ctx, b.Active)
	if err != nil {
		return b, fmt.Errorf("sieve: read %s: %w", b.Active, err)
	}
	b.Unreachable = !routing.Includes(src, routing.ScriptName)
	return b, nil
}
