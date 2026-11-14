package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// serve handles one client: NDJSON requests in, replies and pushes out.
func (d *Daemon) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	pushes := make(chan Push, 16)
	d.mu.Lock()
	d.clients[pushes] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.clients, pushes)
		d.mu.Unlock()
	}()

	// The change feed is this connection's too, but nothing is sent down it
	// until a `watch` request asks (ADR-0027).
	changes := make(chan Change, 64)
	defer d.unwatch(changes)

	enc := json.NewEncoder(conn)
	var wmu sync.Mutex
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return enc.Encode(v)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-pushes:
				if err := write(p); err != nil {
					cancel()
					return
				}
			case c := <-changes:
				if err := write(c); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = write(Response{OK: false, Code: "usage", Error: "malformed request"})
			continue
		}
		// `watch` is the one command that is about this connection rather than
		// about the Mirror, so it is answered here where the connection's
		// channel is, and not in handle with the rest.
		if len(req.Cmd) > 0 && req.Cmd[0] == "watch" {
			if err := write(d.subscribe(req, changes)); err != nil {
				return
			}
			continue
		}
		// One goroutine per request. A send blocks in SMTP for seconds, and
		// nothing behind it on this connection should sit waiting: every read
		// is a Mirror read (WAL SQLite, already shared with the cycles), and
		// every reply carries its request's id, so replies may interleave and
		// the caller matches them up. A failed write kills the connection,
		// which is what unblocks the scan below.
		go func() {
			if err := write(d.handle(ctx, req)); err != nil {
				cancel()
				conn.Close()
			}
		}()
	}
}

// handle runs one request against the Mirror. Reads never touch the network:
// that is the whole point (ADR-0001).
func (d *Daemon) handle(ctx context.Context, req Request) Response {
	if len(req.Cmd) == 0 {
		return Response{ID: req.ID, Mirror: d.state(domainBoth), Code: "usage", Error: "no command"}
	}
	dom := domainOf(req.Cmd[0])
	// Asking for a cycle, not waiting for one: the reply below is the Mirror's
	// as it stands, and the cycle this starts is what makes the next reply
	// better. Before the reply is built, so that a caller told it is Behind is
	// also told a sync is running.
	if dom == domainDAV {
		d.nudgeDAV(nudgeKinds(req.Cmd[0])...)
	}
	resp := Response{ID: req.ID, Mirror: d.state(dom)}

	switch req.Cmd[0] {
	case "reload":
		// Not a CLI command: `mailbox setup` sends it after it writes the
		// config, so the wizard does not sit for up to a minute waiting for
		// the poll to notice (ADR-0021).
		return resp.ok(map[string]any{"changes": d.reloadConfig("asked over the socket")})
	case "outbox":
		return d.handleOutbox(ctx, req, resp)
	case "calendar":
		return d.handleCalendar(req, resp)
	case "agenda":
		return d.handleAgenda(req, resp)
	case "event":
		return d.handleEvent(ctx, req, resp)
	case "meet":
		return d.handleMeet(ctx, req, resp)
	case "todo":
		return d.handleTodo(ctx, req, resp)
	case "habit":
		return d.handleHabit(ctx, req, resp)
	case "contact":
		return d.handleContact(ctx, req, resp)
	case "correspondent":
		return d.handleCorrespondent(req, resp)
	case "screener":
		return d.handleScreener(req, resp)
	case "pickup":
		return d.handlePickup(ctx, req, resp)
	case "route":
		return d.handleRoute(ctx, req, resp)
	case "aside":
		return d.handleAside(ctx, req, resp)
	case "reply-later":
		return d.handleReplyLater(ctx, req, resp)
	case "bubble":
		return d.handleBubble(ctx, req, resp)
	case "sieve":
		return d.handleSieve(ctx, req, resp)
	case "rsvp":
		return d.handleRSVP(ctx, req, resp)
	case "unsubscribe":
		return d.handleUnsubscribe(ctx, req, resp)
	case "label":
		return d.handleLabel(ctx, req, resp)
	case "draft":
		return d.handleDraft(ctx, req, resp)
	}

	switch strings.Join(req.Cmd, " ") {
	case "box list":
		return d.handleBoxList(req, resp)
	case "box view":
		return d.handleBoxView(ctx, req, resp)
	case "message view":
		return d.handleMessageView(ctx, req, resp)
	case "attachment list":
		return d.handleAttachmentList(req, resp)
	case "attachment save":
		return d.handleAttachmentSave(ctx, req, resp)
	case "attachment bytes":
		return d.handleAttachmentBytes(ctx, req, resp)
	case "attachment fileee":
		return d.handleFileee(ctx, req, resp)
	case "thread view":
		return d.handleThreadView(ctx, req, resp)
	case "invite show":
		return d.handleInviteShow(ctx, req, resp)
	case "search":
		return d.handleSearch(ctx, req, resp)
	case "seen", "unseen":
		return d.handleSeen(ctx, req, resp)
	case "move", "trash", "spam":
		return d.handleMove(ctx, req, resp)
	case "send":
		return d.handleSend(ctx, req, resp)
	case "reply":
		return d.handleReply(ctx, req, resp)
	case "forward":
		return d.handleForward(ctx, req, resp)
	case "account list":
		return d.handleAccountList(resp)
	case "status":
		return d.handleStatus(resp)
	}

	resp.Code = "usage"
	resp.Error = fmt.Sprintf("unknown command %q", strings.Join(req.Cmd, " "))
	return resp
}

// domainOf says which loop owns the data a command answers from, so a reply can
// carry that loop's freshness rather than an average of both.
func domainOf(cmd string) domain {
	switch cmd {
	case "calendar", "agenda", "event", "todo", "habit", "contact":
		return domainDAV
	case "status":
		return domainBoth
	}
	return domainMail
}

// nudgeKinds is the collections a command reads. Events and task lists go
// together: an agenda shows both, and a Habit is an Event that a Todo is often
// standing next to.
func nudgeKinds(cmd string) []string {
	if cmd == "contact" {
		return []string{"cards"}
	}
	return []string{"events", "tasks"}
}

// noSuchMessage is what every command says for an id that parsed but matches no
// Message: the id as the caller typed it, then the reason it is usually gone,
// it was moved or expunged between the listing that printed it and now. It does
// not mention the Mirror; from the outside there is only "the mail", and the
// id is stale whether or not a sync is pending.
func noSuchMessage(id string) string {
	return fmt.Sprintf("%s: no such message: moved or deleted since it was listed", id)
}

// fail turns a Mirror read error into the right reply. An id the Mirror does
// not hold is not_found rather than a failure: it may have been expunged, and
// the Mirror may be Behind.
func fail(resp Response, id string, err error) Response {
	if errors.Is(err, mirror.ErrNotFound) {
		return resp.notFound(noSuchMessage(id))
	}
	return resp.failed(err)
}

// wrote finishes a write command: it reports what the server acked and tells
// every listener which Boxes moved. A move the server could not place, no
// UIDPLUS, also asks for a cycle, because only a cycle can find it.
func (d *Daemon) wrote(a *Account, resp Response, results []mailsync.Result, err error) Response {
	if err != nil {
		return resp.api(err.Error())
	}
	boxes := map[string]struct{}{}
	needCycle := false
	out := make([]change, 0, len(results))
	for _, r := range results {
		c := change{ID: a.messageID(r.Folder, r.UID), Box: r.Folder, Flags: r.Flags, Seen: slices.Contains(r.Flags, `\Seen`)}
		boxes[r.Folder] = struct{}{}
		fromHeld := true
		if _, held := a.boxNamed(r.Folder); !held {
			// Mail coming back out of Trash has no local row to put: only a
			// cycle can bring it into the destination's listing, so the ack is
			// not yet a Message anybody can read.
			needCycle, fromHeld = true, false
		}
		if r.NewFolder != "" {
			c.Moved, c.Box = true, r.NewFolder
			boxes[r.NewFolder] = struct{}{}
			switch {
			case !fromHeld:
				// NewID stays empty: the destination uid resolves once the
				// cycle has the Message, and an id a caller acts on before then
				// would name nothing.
			case r.NewUID != 0:
				c.NewID = a.messageID(r.NewFolder, r.NewUID)
			default:
				needCycle = true
			}
		}
		out = append(out, c)
	}
	for box := range boxes {
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	if needCycle {
		d.kickAccount(a, "write")
	}
	return resp.ok(out)
}
