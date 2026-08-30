package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/daemon"
	"mailbox/internal/setup"
)

// check is one thing doctor looked at.
type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// runDoctor answers "why is this not working" from both ends. The local half
// opens its own connections, because a diagnostic that goes through the daemon
// cannot tell you the daemon is the problem; the daemon half reports on the
// connections it already holds, because that is what every other command
// actually uses. Neither on its own covers the failure you would be debugging.
func runDoctor(in *input, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checks := append(localChecks(ctx, in.Bool("offline")), daemonChecks(stdout)...)

	if in.JSON() {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(checks)
	} else {
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, c := range checks {
			mark := "ok"
			if !c.OK {
				mark = "FAIL"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", mark, c.Name, c.Detail)
		}
		_ = tw.Flush()
	}
	for _, c := range checks {
		if !c.OK {
			return ExitAPI
		}
	}
	return ExitOK
}

// localChecks look at this machine and, unless --offline, the servers.
func localChecks(ctx context.Context, offline bool) []check {
	out := []check{}
	path := config.Path()
	cfg, err := config.Load()
	if err != nil {
		return append(out, check{Name: "config", Detail: err.Error()})
	}
	out = append(out, check{Name: "config", OK: true, Detail: path})

	// The file holds a password, so who can read it is part of whether this is
	// set up correctly (ADR-0014).
	if info, err := os.Stat(path); err == nil {
		mode := info.Mode().Perm()
		detail := fmt.Sprintf("%04o", mode)
		if mode&0o077 != 0 {
			detail += " — it holds a password; chmod 600 it"
		}
		out = append(out, check{Name: "config mode", OK: mode&0o077 == 0, Detail: detail})
	}

	for _, p := range []struct{ name, path string }{
		{"mirror", mustPath(config.MirrorPath)},
		{"outbox", mustPath(config.OutboxPath)},
	} {
		detail := p.path + " — not written yet"
		ok := false
		if info, err := os.Stat(p.path); err == nil {
			detail = fmt.Sprintf("%s, %s", p.path, humanBytes(info.Size()))
			ok = true
		}
		// A mirror that is not there yet is a cold start, not a fault; an
		// outbox that is not there is an account that has never sent. Both are
		// reported and neither fails the check.
		_ = ok
		out = append(out, check{Name: p.name, OK: true, Detail: detail})
	}

	if offline {
		return out
	}

	a := cfg.Account
	probe := setup.Servers{}
	if boxes, err := probe.IMAP(ctx, a.IMAPHost, a.IMAPPort, a.Email, a.Password); err != nil {
		out = append(out, check{Name: "imap", Detail: err.Error()})
	} else {
		out = append(out, check{
			Name: "imap", OK: true,
			Detail: fmt.Sprintf("%s:%d, %d boxes", a.IMAPHost, a.IMAPPort, len(boxes)),
		})
	}
	if err := probe.SMTP(ctx, a.SMTPHost, a.SMTPPort, a.Email, a.Password); err != nil {
		out = append(out, check{Name: "smtp", Detail: err.Error()})
	} else {
		out = append(out, check{
			Name: "smtp", OK: true, Detail: fmt.Sprintf("%s:%d", a.SMTPHost, a.SMTPPort),
		})
	}
	if cols, err := probe.DAV(ctx, a.DAVEndpoint, a.Email, a.DAVPassword); err != nil {
		out = append(out, check{Name: "dav", Detail: err.Error()})
	} else {
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.Name)
		}
		out = append(out, check{
			Name: "dav", OK: true,
			Detail: fmt.Sprintf("%s — %s", a.DAVEndpoint, strings.Join(names, ", ")),
		})
	}
	// ManageSieve is only reachable when it is: an account without it still
	// works, it just cannot be triaged.
	addr := net.JoinHostPort(a.SieveHost, fmt.Sprint(a.SievePort))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		out = append(out, check{Name: "sieve", Detail: err.Error()})
	} else {
		conn.Close()
		out = append(out, check{Name: "sieve", OK: true, Detail: addr})
	}
	return out
}

// daemonChecks ask the daemon what it holds. Nothing here dials a server: the
// point is what the process every other command talks to actually has.
func daemonChecks(stdout io.Writer) []check {
	socket := config.SocketPath()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return []check{{
			Name: "daemon", Detail: fmt.Sprintf(
				"nothing listening at %s — start one with: mailbox daemon", socket),
		}}
	}
	conn.Close()

	var out []check
	code := request(daemon.Request{ID: "1", Cmd: []string{"status"}}, false,
		func(_, _ io.Writer, resp daemon.Response) {
			rows, _ := rowsOf(resp.Data)
			for _, r := range rows {
				m, ok := r.(map[string]any)
				if !ok {
					continue
				}
				out = append(out, check{
					Name: "account " + str(m["account"]), OK: true,
					Detail: fmt.Sprintf("%d boxes, %d in %s, watching %s",
						int(numOf(m["boxes"])), int(numOf(m["count"])), str(m["folder"]),
						strings.Join(strs(asAny(m["watched"])), ", ")),
				})
			}
			// What the Daemon says it needs a person for. It is the same list
			// `mailbox status` prints and the same one a `problem.changed`
			// push points at (ADR-0021).
			for _, p := range resp.Problems {
				out = append(out, check{Name: p.Name, Detail: p.Detail})
			}
			if resp.Mirror != nil && !resp.Mirror.Connected {
				out = append(out, check{
					Name: "mirror", Detail: "behind — the daemon cannot reach the server"})
			} else {
				out = append(out, check{Name: "mirror", OK: true, Detail: "up to date"})
			}
		}, io.Discard, io.Discard)
	if code != ExitOK {
		return append([]check{{Name: "daemon", Detail: "listening, but it will not answer status"}}, out...)
	}
	return append([]check{{Name: "daemon", OK: true, Detail: socket}}, out...)
}

func mustPath(fn func() (string, error)) string {
	p, err := fn()
	if err != nil {
		return "unknown: " + err.Error()
	}
	return p
}
