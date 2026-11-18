package daemon

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// canned serves one connection on path: every line it is given, then it hangs
// up.
func canned(t *testing.T, path string, lines ...any) {
	t.Helper()
	ln, err := Listen(path, false)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		enc := json.NewEncoder(conn)
		for _, l := range lines {
			_ = enc.Encode(l)
		}
		// The request has to be read, or the client's write blocks on a full
		// socket buffer and never sees the reply.
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
	}()
	t.Cleanup(func() { ln.Close() })
}

// A push and a reply share one connection, and only the reply carries the id
// that was asked for.
func TestDoSkipsThePushesOnItsWayToTheReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	canned(t, path,
		Push{Event: "mail.changed", Box: "INBOX"},
		Response{ID: "1", OK: true, Data: "the answer"},
	)
	c, err := Dial(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.Do([]string{"status"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Data != "the answer" {
		t.Fatalf("Data = %v", resp.Data)
	}
}

// `attachment bytes` answers with the base64 of up to maxInlineBytes, and the
// whole reply is one line, so the client's line cap has to fit it. The two
// used to disagree: the daemon accepted the request and the client refused
// the answer as a too-long token.
func TestTheClientAcceptsTheBiggestInlineAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	body := make([]byte, maxInlineBytes)
	for i := range body {
		body[i] = 'A'
	}
	reply := Response{ID: "1", OK: true,
		Data: inlineBytes{Filename: "big.png", MIMEType: "image/png", Base64: base64.StdEncoding.EncodeToString(body)}}
	canned(t, path, reply)
	c, err := Dial(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Do([]string{"attachment", "bytes"}, nil); err != nil {
		t.Fatalf("the biggest inline attachment must survive the wire: %v", err)
	}
}

// A wizard that has just enabled the socket unit arrives before systemd has
// bound it, so a wait is a retry and not a failure.
func TestDialWaitsForASocketThatIsNotThereYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	if _, err := Dial(path, 0); err == nil {
		t.Fatal("Dial answered on a socket nobody bound")
	}

	// The socket appears while a Dial that was asked to wait is still trying.
	bound := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		ln, err := net.Listen("unix", path)
		if err != nil {
			return
		}
		close(bound)
		if conn, err := ln.Accept(); err == nil {
			conn.Close()
		}
		ln.Close()
	}()

	c, err := Dial(path, 3*time.Second)
	if err != nil {
		t.Fatalf("Dial gave up before the socket appeared: %v", err)
	}
	c.Close()
	<-bound
}

func TestPushesCarriesOnlyWhatHasAnEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.sock")
	canned(t, path,
		Response{ID: "1", OK: true}, // a reply on the wrong connection: no Event
		Push{Event: "mail.changed", Box: "INBOX"},
		Push{Event: "calendar.changed", Account: "primary"},
	)
	out, stop, err := Pushes(path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	for _, want := range []string{"mail.changed", "calendar.changed"} {
		select {
		case p := <-out:
			if p.Event != want {
				t.Fatalf("Event = %q, want %q", p.Event, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no push, want %q", want)
		}
	}
}
