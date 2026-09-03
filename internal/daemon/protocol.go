// Package daemon serves the command surface on a unix socket and owns the only
// connections to the servers.
package daemon

import "time"

// Request is one line in from a client.
type Request struct {
	ID   string         `json:"id"`
	Cmd  []string       `json:"cmd"`
	Args map[string]any `json:"args,omitempty"`
}

// MirrorState says how fresh the answer is. It rides on every reply, because a
// caller has to be able to reason about staleness on every call — and a Behind
// Mirror is not an error, it is a Mirror that says so (ADR-0001).
//
// It describes the data this reply answered from, not the Mirror in general.
// Mail and the collections are brought up to date by different loops, minutes
// or hours apart, so one number for both would be wrong for one of them: a
// caller reading the agenda is owed the age of the agenda.
type MirrorState struct {
	SyncedAt  *time.Time `json:"synced_at"`
	Behind    bool       `json:"behind"`
	Connected bool       `json:"connected"`
	// Syncing says a cycle for this data is running right now. It is what makes
	// a Behind answer actionable rather than only honest: re-reading in a moment
	// will say something different, and re-reading immediately will not.
	Syncing bool `json:"syncing,omitempty"`
}

// Response is one line out.
type Response struct {
	ID     string       `json:"id"`
	OK     bool         `json:"ok"`
	Data   any          `json:"data,omitempty"`
	Mirror *MirrorState `json:"mirror,omitempty"`
	Code   string       `json:"code,omitempty"`
	Error  string       `json:"error,omitempty"`
	// Problems are the things this program needs a human for. Only `status`
	// carries them: they are a property of the Daemon rather than of one
	// answer, and a caller that has been pushed `problem.changed` re-reads
	// status to find out what they are (ADR-0011).
	Problems []Problem `json:"problems,omitempty"`
	// Inferred is the recent routing decisions this Daemon read out of a drag
	// out of the Screener rather than from a `mailbox route` command. Only
	// `status` carries it: an inference has no human in the loop, so a wrong one
	// has to be visible somewhere it will be seen (supersedes ADR-0019).
	Inferred []InferredDecision `json:"inferred,omitempty"`
}

// InferredDecision is one routing decision the Daemon made from a Screener move
// it observed, kept for `status` so a mistaken drag is not silent.
type InferredDecision struct {
	At      string `json:"at"`
	Address string `json:"address"`
	To      string `json:"to"`
	// Moved is how many waiting Screener mails swept to the destination with it.
	Moved int `json:"moved"`
}

// Push is an unsolicited line. It names what moved and says nothing about what
// it now holds: a widget that receives one re-reads (ADR-0011).
type Push struct {
	Event   string `json:"event"`
	Account string `json:"account"`
	Box     string `json:"box,omitempty"`
}
