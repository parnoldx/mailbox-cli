package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Client is one connection to the Daemon: NDJSON requests out, replies and
// pushes in. It is the whole of the wire format on the Go side, so a caller
// that wants a reply does not have to know that pushes arrive on the same
// connection without an id (the C++ and QML clients speak it for themselves).
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	sc   *bufio.Scanner
	n    int
}

// maxLine is the longest line accepted. A reply carrying a mail body or an
// attachment's base64 outruns bufio's 64K default.
const maxLine = 1 << 20

// Dial connects to the socket. wait, when positive, keeps trying until it
// passes rather than failing on the first refusal: a caller that has just
// enabled the socket unit may arrive before systemd has bound it.
func Dial(socket string, wait time.Duration) (*Client, error) {
	deadline := time.Now().Add(wait)
	for {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			sc := bufio.NewScanner(conn)
			sc.Buffer(make([]byte, 0, 64*1024), maxLine)
			return &Client{conn: conn, enc: json.NewEncoder(conn), sc: sc}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no daemon at %s: %w", socket, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *Client) Close() error { return c.conn.Close() }

// Deadline bounds every later read and write; the zero time lifts it. A watch
// sets it to its --timeout, which makes the deadline arrive as a read error
// rather than needing a timer of its own.
func (c *Client) Deadline(t time.Time) error { return c.conn.SetDeadline(t) }

// Send writes one request and does not wait for the reply.
func (c *Client) Send(req Request) error { return c.enc.Encode(req) }

// Next decodes one line into v. A line that is not JSON is skipped, so a Push
// arriving between a request and its reply does not end a read; io.EOF is the
// connection closing.
func (c *Client) Next(v any) error {
	for c.sc.Scan() {
		if err := json.Unmarshal(c.sc.Bytes(), v); err != nil {
			continue
		}
		return nil
	}
	if err := c.sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// Do sends one command and waits for its reply. Pushes arriving on the same
// connection carry no id and are skipped.
func (c *Client) Do(cmd []string, args map[string]any) (Response, error) {
	c.n++
	id := strconv.Itoa(c.n)
	if err := c.Send(Request{ID: id, Cmd: cmd, Args: args}); err != nil {
		return Response{}, err
	}
	for {
		var resp Response
		err := c.Next(&resp)
		if err != nil {
			if err == io.EOF {
				err = fmt.Errorf("the daemon closed the connection")
			}
			return Response{}, err
		}
		if resp.ID == id {
			return resp, nil
		}
	}
}

// Pushes reads pushes off their own connection. They are on a second one
// because a reply and a push arriving interleaved on the same socket is a
// state machine nobody here needs.
func Pushes(socket string) (<-chan Push, func(), error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan Push, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		for sc.Scan() {
			var p Push
			if err := json.Unmarshal(sc.Bytes(), &p); err != nil || p.Event == "" {
				continue
			}
			select {
			case out <- p:
			default: // a caller that cannot keep up misses a line of progress
			}
		}
	}()
	return out, func() { conn.Close() }, nil
}
