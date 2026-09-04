// Package daemon serves the command surface on a unix socket and owns the only
// connections to the servers.
package daemon

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Request is one line in from a client.
type Request struct {
	ID   string         `json:"id"`
	Cmd  []string       `json:"cmd"`
	Args map[string]any `json:"args,omitempty"`
}

// Reading one request's arguments. They arrive over the socket as JSON, so a
// number is a float64 and a repeatable flag may be a list of anys — the four
// accessors below are the only place that has to know it, and a handler that
// wants a limit asks for a limit.

// Str is one string argument, empty when it was not given.
func (r Request) Str(key string) string {
	s, _ := r.Args[key].(string)
	return s
}

// Bool is one switch.
func (r Request) Bool(key string) bool {
	b, _ := r.Args[key].(bool)
	return b
}

// Int is one number, or fallback when none was given. Every number a command
// takes is a limit or a count of days, and neither means anything at zero or
// below, so a value that is not positive is treated as absent.
func (r Request) Int(key string, fallback int) int {
	v, ok := r.Args[key].(float64)
	if !ok || v <= 0 {
		return fallback
	}
	return int(v)
}

// Text is one argument as the caller wrote it, whatever JSON shape it arrived
// in. An id sent as the number 12 and the same id sent as "12" are the same id,
// and a handler reading one should not have to know which spelling a client
// chose — the CLI sends strings, a GUI over the socket sends numbers.
func (r Request) Text(key string) string {
	switch v := r.Args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	}
	return ""
}

// Strings is a repeatable flag, or the one value it was given once. Blanks are
// dropped: a flag given and left empty is the caller not giving it.
func (r Request) Strings(key string) []string {
	switch t := r.Args[key].(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Verb is the subcommand, or fallback when the caller named only the group.
// Every group has a default — `todo` is `todo list`, `event` is `event view` —
// because the bare noun is what somebody types when they want to see the thing
// (ADR-0020).
func (r Request) Verb(fallback string) string {
	if len(r.Cmd) > 1 {
		return r.Cmd[1]
	}
	return fallback
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

// The four ways a handler ends. Every one of them takes the Response it was
// given — which already carries the id and the MirrorState — and returns it
// finished, so a handler's last line is the whole of its answer.
//
// The Codes are the ones the CLI turns into exit codes: `usage` is a mistake in
// what was typed, `not_found` is an id the Mirror does not hold (which is
// ordinary against a Mirror that may be Behind), and `api` is everything a
// server or the disk did. They are spelled here and nowhere else.
func (r Response) ok(data any) Response {
	r.OK, r.Data = true, data
	return r
}

func (r Response) usage(msg string) Response {
	r.Code, r.Error = "usage", msg
	return r
}

func (r Response) notFound(msg string) Response {
	r.Code, r.Error = "not_found", msg
	return r
}

func (r Response) api(msg string) Response {
	r.Code, r.Error = "api", msg
	return r
}

// codedError is an error that already knows which Code the reply should carry.
// A helper several calls below a handler can tell "you typed something wrong"
// apart from "that id is gone" without every caller having to work it out
// again from the wording.
type codedError struct {
	code string
	msg  string
}

func (e codedError) Error() string { return e.msg }

// usageErr is a mistake in what the caller typed; notFoundErr is an id that
// parsed but names nothing the Mirror holds, which is ordinary against a Mirror
// that may be Behind. Anything else is left as a plain error and comes out as
// `api`.
func usageErr(format string, a ...any) error {
	return codedError{"usage", fmt.Sprintf(format, a...)}
}

func notFoundErr(format string, a ...any) error {
	return codedError{"not_found", fmt.Sprintf(format, a...)}
}

// failed answers with whatever went wrong. An error that named its own Code
// keeps it; everything else is something a server or the disk did.
func (r Response) failed(err error) Response {
	var c codedError
	if errors.As(err, &c) {
		r.Code, r.Error = c.code, c.msg
		return r
	}
	return r.api(err.Error())
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
