package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"time"

	"mailbox/internal/daemon"
)

// Client is the wizard's connection to the Daemon. Setup is a socket client
// like every other one: after the probes it opens nothing and writes nothing
// but files, and the first sync it shows is the Daemon's own (ADR-0021).
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	sc   *bufio.Scanner
	n    int
}

// Dial connects, or says nothing is listening. A wizard that has just enabled
// the socket unit may arrive before systemd has finished binding it, so this
// retries briefly rather than failing on the first refusal.
func Dial(socket string, wait time.Duration) (*Client, error) {
	deadline := time.Now().Add(wait)
	for {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			sc := bufio.NewScanner(conn)
			sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
			return &Client{conn: conn, enc: json.NewEncoder(conn), sc: sc}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no daemon at %s: %w", socket, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *Client) Close() error { return c.conn.Close() }

// Do sends one command and waits for its reply. Pushes arriving on the same
// connection carry no id and are skipped.
func (c *Client) Do(cmd []string, args map[string]any) (daemon.Response, error) {
	c.n++
	id := strconv.Itoa(c.n)
	if err := c.enc.Encode(daemon.Request{ID: id, Cmd: cmd, Args: args}); err != nil {
		return daemon.Response{}, err
	}
	for c.sc.Scan() {
		var resp daemon.Response
		if err := json.Unmarshal(c.sc.Bytes(), &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		return resp, nil
	}
	if err := c.sc.Err(); err != nil {
		return daemon.Response{}, err
	}
	return daemon.Response{}, fmt.Errorf("the daemon closed the connection")
}

// Pushes reads pushes off their own connection. They are on a second one
// because a reply and a push arriving interleaved on the same socket is a
// state machine nothing here needs.
func Pushes(socket string) (<-chan daemon.Push, func(), error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan daemon.Push, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var p daemon.Push
			if err := json.Unmarshal(sc.Bytes(), &p); err != nil || p.Event == "" {
				continue
			}
			select {
			case out <- p:
			default: // a wizard that cannot keep up misses a line of progress
			}
		}
	}()
	return out, func() { conn.Close() }, nil
}
