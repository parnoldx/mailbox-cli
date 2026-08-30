package daemon

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/outbox"
)

// The config file is the record and the Daemon reconciles it (ADR-0021). It is
// re-read at the top of each cycle when the file has moved, and immediately
// when a client asks — `mailbox setup` asks, so the wizard does not sit for up
// to a minute looking broken.
//
// There is no file watch. TOML is written by temp-file-and-rename, so an
// inotify watch has to be on the directory rather than the file, and it sees
// two events per save with a window in between where the file is not there.

// Applied says what a reload did. Restart means the change is not one that can
// be made in place; the Daemon logs it and exits 0, and under socket activation
// the next connection starts it again on the new config.
type Applied struct {
	Changes []string
	Restart bool
	Reason  string
}

// Applier brings the Daemon into line with a config it has just read. It is
// supplied by the caller, because building an account's connections is that
// caller's job and not the socket server's.
type Applier func(cfg *config.Config) (Applied, error)

// Problem is something the program needs a human for. It is a short list on
// purpose: a daemon that cries wolf gets muted, and everything that resolves
// itself — Behind, one Box failing to sync — belongs in the log instead.
type Problem struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// reloadState is the config half of the Daemon.
type reloadState struct {
	mu       sync.Mutex
	modTime  time.Time
	size     int64
	problems map[string]string
	quit     chan struct{}
	runCtx   context.Context
}

// WatchConfig turns on config reconciliation. path is the record; apply is what
// brings this process into line with it.
func (d *Daemon) WatchConfig(path string, apply Applier) {
	d.ConfigPath = path
	d.Apply = apply
	if info, err := os.Stat(path); err == nil {
		d.reload.modTime, d.reload.size = info.ModTime(), info.Size()
	}
}

// configMoved says whether the file has changed since it was last read. mtime
// and size together, because a rewrite within the same second is common when a
// wizard writes twice.
func (d *Daemon) configMoved() bool {
	if d.ConfigPath == "" {
		return false
	}
	info, err := os.Stat(d.ConfigPath)
	if err != nil {
		return false
	}
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	if info.ModTime().Equal(d.reload.modTime) && info.Size() == d.reload.size {
		return false
	}
	d.reload.modTime, d.reload.size = info.ModTime(), info.Size()
	return true
}

// reloadConfig re-reads the file and applies what it can.
//
// A file that will not parse leaves the Daemon running on the last config that
// worked. A daemon that exits because a TOML key was misspelt turns a typo into
// missed mail, and every other decision here goes the other way: a Behind
// Mirror answers rather than failing (ADR-0001).
func (d *Daemon) reloadConfig(reason string) []string {
	if d.ConfigPath == "" || d.Apply == nil {
		return nil
	}
	cfg, err := config.LoadFrom(d.ConfigPath)
	if err != nil {
		d.logf("config (%s): %v — carrying on with the last one that worked", reason, err)
		d.setProblem("config", err.Error())
		return nil
	}
	applied, err := d.Apply(cfg)
	if err != nil {
		d.logf("config (%s): %v", reason, err)
		d.setProblem("config", err.Error())
		return nil
	}
	d.setProblem("config", "")
	for _, c := range applied.Changes {
		d.logf("config (%s): %s", reason, c)
	}
	// Say so even when there was nothing to do. A mechanism that only speaks
	// when it acts is one nobody can tell is running, and this one is meant to
	// be checked by reading the journal.
	if len(applied.Changes) == 0 && !applied.Restart {
		d.logf("config (%s): re-read, nothing to apply", reason)
	}
	if applied.Restart {
		d.logf("config (%s): %s — exiting; the next connection starts a new one",
			reason, applied.Reason)
		d.stop()
	}
	return applied.Changes
}

// stop ends the accept loop. Exiting 0 rather than re-identifying the Primary
// Account in place is the whole point of socket activation being real
// (ADR-0012).
func (d *Daemon) stop() {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	if d.reload.quit == nil {
		d.reload.quit = make(chan struct{})
	}
	select {
	case <-d.reload.quit:
	default:
		close(d.reload.quit)
	}
}

func (d *Daemon) quitting() chan struct{} {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	if d.reload.quit == nil {
		d.reload.quit = make(chan struct{})
	}
	return d.reload.quit
}

// setProblem records or clears one. An empty detail clears it. A change either
// way pushes, so a widget re-reads and shows it (ADR-0011).
func (d *Daemon) setProblem(name, detail string) {
	d.reload.mu.Lock()
	if d.reload.problems == nil {
		d.reload.problems = map[string]string{}
	}
	was, had := d.reload.problems[name]
	switch {
	case detail == "":
		delete(d.reload.problems, name)
	default:
		d.reload.problems[name] = detail
	}
	d.reload.mu.Unlock()
	if was != detail || had != (detail != "") {
		d.push(Push{Event: "problem.changed"})
	}
}

// SetProblem is setProblem for the caller that owns the config: a removal the
// Daemon kept rather than made has to be visible somewhere, or a declarative
// file and a component that may decline disagree in silence (ADR-0021).
func (d *Daemon) SetProblem(name, detail string) { d.setProblem(name, detail) }

// SetDefaults changes where a Todo and a Contact go when the caller does not
// say. They are values rather than connections, so they move without a restart.
func (d *Daemon) SetDefaults(taskList, addressBook string) {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	d.TaskList, d.AddressBook = taskList, addressBook
}

func (d *Daemon) defaultTaskList() string {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	return d.TaskList
}

func (d *Daemon) defaultAddressBook() string {
	d.reload.mu.Lock()
	defer d.reload.mu.Unlock()
	return d.AddressBook
}

// Problems is the list a caller reads after a problem.changed push: the stored
// ones, plus the Held mail, which is a fact about the Outbox rather than a
// state this process keeps.
func (d *Daemon) Problems() []Problem {
	d.reload.mu.Lock()
	out := make([]Problem, 0, len(d.reload.problems))
	for name, detail := range d.reload.problems {
		out = append(out, Problem{Name: name, Detail: detail})
	}
	d.reload.mu.Unlock()

	if d.Outbox != nil {
		if items, err := d.Outbox.List(200); err == nil {
			var held []string
			for _, it := range items {
				if it.State == outbox.Held {
					held = append(held, fmt.Sprintf("#%d %s", it.ID, it.Subject))
				}
			}
			if len(held) > 0 {
				out = append(out, Problem{
					Name: "held mail",
					Detail: fmt.Sprintf("%d at the smtp server when the daemon stopped, "+
						"and nothing resends them: %v", len(held), held),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// noteAuth turns a server refusing the password into a problem rather than a
// log line nobody reads. Every other sync failure resolves itself; this one
// does not, and mail stops arriving until a person changes something.
func (d *Daemon) noteAuth(account string, err error) {
	name := "credentials " + account
	if err == nil {
		d.setProblem(name, "")
		return
	}
	if !looksLikeAuth(err) {
		return
	}
	d.setProblem(name, fmt.Sprintf("%s refuses the password: %v", account, err))
}
