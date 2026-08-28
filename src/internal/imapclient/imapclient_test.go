package imapclient

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

// dialFake wires a client to an in-memory server and returns everything the
// server read off the wire once the exchange finishes.
func dialFake(t *testing.T, serve func(c net.Conn, br *bufio.Reader, seen *[]string)) (*Client, func() []string) {
	t.Helper()
	client, server := net.Pipe()
	var seen []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(server, bufio.NewReader(server), &seen)
	}()
	c := &Client{conn: client, br: bufio.NewReader(client)}
	return c, func() []string {
		client.Close()
		<-done
		return seen
	}
}

func TestLiteralCommandSendsNoTrailingBlankLine(t *testing.T) {
	c, finish := dialFake(t, func(conn net.Conn, br *bufio.Reader, seen *[]string) {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			*seen = append(*seen, line)
			if strings.HasSuffix(strings.TrimRight(line, "\r\n"), "{5}") {
				conn.Write([]byte("+ ready\r\n"))
				literal, err := br.ReadString('\n')
				if err != nil {
					return
				}
				*seen = append(*seen, literal)
				conn.Write([]byte("* SEARCH 7\r\na1 OK done\r\n"))
			}
		}
	})

	resp, err := c.Command("UID", "SEARCH", "TEXT", Literal("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" {
		t.Fatalf("status = %q, want OK", resp.Status)
	}
	lines := finish()
	want := []string{"a1 UID SEARCH TEXT {5}\r\n", "hello\r\n"}
	if len(lines) != len(want) {
		t.Fatalf("server saw %q, want exactly %q", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestPlainCommandStillTerminated(t *testing.T) {
	c, finish := dialFake(t, func(conn net.Conn, br *bufio.Reader, seen *[]string) {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		*seen = append(*seen, line)
		conn.Write([]byte("a1 OK done\r\n"))
	})

	if _, err := c.Command("NOOP"); err != nil {
		t.Fatal(err)
	}
	lines := finish()
	if len(lines) != 1 || lines[0] != "a1 NOOP\r\n" {
		t.Fatalf("server saw %q, want [\"a1 NOOP\\r\\n\"]", lines)
	}
}
