package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/daemon"
)

// runWatch prints changes as they happen, one JSON object per line, until it is
// interrupted. It is the one command that does not go through request(): every
// other one asks a question and leaves, and this one subscribes and stays
// (ADR-0027).
func runWatch(in *input, stdout, stderr io.Writer) int {
	w := &watch{
		req:  daemon.Request{ID: "1", Cmd: []string{"watch"}, Args: map[string]any{}},
		once: in.Bool("exit-on-first"),
		out:  stdout, err: stderr,
	}
	sync, async := in.Str("run-sync"), in.Str("run-async")
	switch {
	case sync != "" && async != "":
		fmt.Fprintln(stderr, "pass --run-sync or --run-async, not both: one waits for the command and the other does not")
		return ExitUsage
	case sync != "":
		w.cmd, w.wait = sync, true
	case async != "":
		w.cmd = async
	}
	if s := in.Str("timeout"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			fmt.Fprintf(stderr, "--timeout wants a duration like 30m, not %q\n", s)
			return ExitUsage
		}
		w.deadline = time.Now().Add(d)
	}
	if boxes := in.List("box"); len(boxes) > 0 {
		w.req.Args["box"] = boxes
	}
	if events := in.List("events"); len(events) > 0 {
		w.req.Args["events"] = events
	}
	return w.run()
}

// watch is one run of the command: what was asked for, and where it goes.
type watch struct {
	req      daemon.Request
	cmd      string // the shell command a change drives, if any
	wait     bool   // --run-sync: one at a time
	once     bool
	deadline time.Time
	out, err io.Writer
	// dialled says the socket has answered at least once. Before that a missing
	// daemon is a setup problem and the watch fails; after it, it is a daemon
	// being restarted and the watch waits.
	dialled bool
}

// redialEvery is how long to wait before dialling again. Under socket
// activation the daemon comes back when something connects, so this is the
// thing that brings it back.
const redialEvery = 2 * time.Second

func (w *watch) run() int {
	for {
		conn, err := net.Dial("unix", config.SocketPath())
		if err != nil {
			if !w.dialled {
				fmt.Fprintf(w.err, "no daemon listening at %s\n", config.SocketPath())
				fmt.Fprintf(w.err, "start one with: mailbox daemon\n")
				return ExitDaemon
			}
			if !w.pause() {
				return ExitOK
			}
			continue
		}
		code, done := w.stream(conn)
		conn.Close()
		if done {
			return code
		}
		w.report(daemon.Change{Event: "disconnected", At: time.Now().Format(time.RFC3339)})
		if !w.pause() {
			return ExitOK
		}
	}
}

// pause waits before the next dial, and says whether the watch is still within
// its --timeout.
func (w *watch) pause() bool {
	if !w.deadline.IsZero() && time.Now().Add(redialEvery).After(w.deadline) {
		return false
	}
	time.Sleep(redialEvery)
	return true
}

// stream subscribes on one connection and reports what arrives. It returns the
// exit code and whether the watch is over: a connection that simply dropped is
// not, because the daemon is allowed to restart under a watch.
func (w *watch) stream(conn net.Conn) (int, bool) {
	if !w.deadline.IsZero() {
		_ = conn.SetDeadline(w.deadline)
	}
	if err := json.NewEncoder(conn).Encode(w.req); err != nil {
		fmt.Fprintf(w.err, "write: %v\n", err)
		return ExitAPI, true
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		// A reply and a change arrive on the same connection, and a reply is
		// the one carrying an id. The only reply this command ever gets is the
		// answer to its own subscription.
		var line struct {
			daemon.Change
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		if line.ID != "" {
			if !line.OK {
				fmt.Fprintln(w.err, line.Error)
				return codeToExit(line.Code), true
			}
			w.dialled = true
			continue
		}
		if line.Event == "" {
			continue // a Push, for the widgets
		}
		w.report(line.Change)
		if w.once && line.Event != "ready" && line.Event != "disconnected" {
			return ExitOK, true
		}
	}
	// The deadline is --timeout arriving, which is an ordinary end to a watch.
	if os.IsTimeout(sc.Err()) {
		return ExitOK, true
	}
	return ExitOK, false
}

// report prints one change, or runs the command instead. The two lines that
// describe the watch itself are always printed and never drive a command: a
// script asked to handle changes should not be run for "the connection came
// back".
func (w *watch) report(c daemon.Change) {
	if w.cmd == "" || c.Event == "ready" || c.Event == "disconnected" {
		enc := json.NewEncoder(w.out)
		_ = enc.Encode(c)
		return
	}
	w.exec(c)
}

// exec runs the command for one change, with the change on its stdin and in its
// environment. --run-sync waits here, which is what makes it ordered; without
// it the command is spawned and the watch reads on, so two can overlap.
func (w *watch) exec(c daemon.Change) {
	line, _ := json.Marshal(c)
	cmd := exec.Command("sh", "-c", w.cmd)
	cmd.Stdin = bytes.NewReader(append(line, '\n'))
	cmd.Stdout, cmd.Stderr = w.out, w.err
	cmd.Env = append(os.Environ(), changeEnv(c)...)
	if w.wait {
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(w.err, "%s: %v\n", w.cmd, err)
		}
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w.err, "%s: %v\n", w.cmd, err)
		return
	}
	go func() { _ = cmd.Wait() }() // reaped, not waited for
}

// changeEnv is the change as environment variables, for a one-liner that would
// otherwise need a JSON parser. Only what this change has is set, so a script
// can tell an empty subject from a calendar line that has none.
func changeEnv(c daemon.Change) []string {
	pairs := []struct{ key, val string }{
		{"MAILBOX_EVENT", c.Event},
		{"MAILBOX_ACCOUNT", c.Account},
		{"MAILBOX_BOX", c.Box},
		{"MAILBOX_SUBJECT", c.Subject},
		{"MAILBOX_FROM", c.From},
		{"MAILBOX_COLLECTION", c.Collection},
		{"MAILBOX_KIND", c.Kind},
		{"MAILBOX_SUMMARY", c.Summary},
		{"MAILBOX_CODE", c.Code},
	}
	for _, n := range []struct {
		key string
		val int64
	}{
		{"MAILBOX_THREAD", c.Thread}, {"MAILBOX_MESSAGE", c.Message}, {"MAILBOX_OBJECT", c.Object},
	} {
		if n.val != 0 {
			pairs = append(pairs, struct{ key, val string }{n.key, strconv.FormatInt(n.val, 10)})
		}
	}
	out := make([]string, 0, len(pairs)+1)
	for _, p := range pairs {
		if p.val != "" {
			out = append(out, p.key+"="+p.val)
		}
	}
	if c.New {
		out = append(out, "MAILBOX_NEW=1")
	}
	return out
}
