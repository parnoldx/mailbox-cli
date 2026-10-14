package imapdrv

import (
	"crypto/tls"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// A pooled connection the server quietly drops must not fail a move. The
// SELECT is what fails — nothing has moved yet — so redialling and running
// the move once on the fresh connection is safe (ADR-0017 only forbids
// repeating an operation whose ack may have been lost). A MOVE that fails
// after the select ran is the other case, and there the move must NOT be
// retried: the old server answered two dials, not three.
func TestMoveSurvivesADroppedPooledConnection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dropOn    string // the command the first work connection dies on
		wantErr   bool
		wantDials int // ctl dial + work dials
	}{
		{"select fails on the dead connection, move retried on a fresh one", "SELECT", false, 3},
		{"move fails after the select ran, move not repeated", "UID COPY", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			f := &fakeIMAP{dropOn: tc.dropOn}
			cert := selfSigned(t)
			go f.serve(ln, cert)
			oldCap := cmdCap
			cmdCap = 2 * time.Second
			defer func() { cmdCap = oldCap }()

			d, err := Dial(Config{
				Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
				Username: "u", Password: "p",
				TLS: &tls.Config{InsecureSkipVerify: true},
			})
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer d.Close()

			_, err = d.Move(t.Context(), "INBOX", []uint32{5}, "Archive/Privat/Familie&Freunde")
			if (err != nil) != tc.wantErr {
				t.Errorf("move err = %v, wantErr %v", err, tc.wantErr)
			}
			f.mu.Lock()
			dials := f.dials
			f.mu.Unlock()
			if dials != tc.wantDials {
				t.Errorf("dials = %d, want %d", dials, tc.wantDials)
			}
		})
	}
}

// fakeIMAP speaks just enough IMAP for Dial and Move. The ctl connection is
// dialled first and answered with login only; the second dial is the work
// connection, which drops the socket on its dropOn command — the shape of a
// server that closed an idle pooled connection — and every later dial is
// served fully. go-imap runs a move as UID COPY + UID STORE + UID EXPUNGE,
// because the fake advertises no MOVE capability.
type fakeIMAP struct {
	mu     sync.Mutex
	dials  int
	dropOn string
}

func (f *fakeIMAP) serve(ln net.Listener, cert tls.Certificate) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn, cert)
	}
}

func (f *fakeIMAP) handle(conn net.Conn, cert tls.Certificate) {
	defer conn.Close()
	f.mu.Lock()
	f.dials++
	dial := f.dials
	f.mu.Unlock()

	tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer tc.Close()
	if _, err := tc.Write([]byte("* OK [CAPABILITY IMAP4rev1 UIDPLUS] fake ready\r\n")); err != nil {
		return
	}
	drop := dial == 2 // the work connection's first attempt
	buf := make([]byte, 4096)
	for {
		nr, err := tc.Read(buf)
		if err != nil {
			return
		}
		raw := strings.Split(strings.TrimRight(string(buf[:nr]), "\r\n"), "\r\n")
		for _, line := range raw {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			cmd := fields[1]
			if cmd == "UID" && len(fields) > 2 {
				cmd = "UID " + fields[2]
			}
			dies := drop && cmd == f.dropOn
			switch cmd {
			case "LOGIN":
				tc.Write([]byte(fields[0] + " OK logged in\r\n"))
			case "CAPABILITY":
				tc.Write([]byte("* CAPABILITY IMAP4rev1 UIDPLUS\r\n" + fields[0] + " OK done\r\n"))
			case "SELECT":
				if dies {
					return // the socket dies before the command is answered
				}
				tc.Write([]byte(fields[0] + " OK [READ-WRITE] selected\r\n"))
			case "UID COPY":
				if dies {
					return
				}
				tc.Write([]byte(fields[0] + " OK [COPYUID 1 5 9] copied\r\n"))
			case "UID STORE", "UID EXPUNGE":
				tc.Write([]byte(fields[0] + " OK " + cmd + " done\r\n"))
			}
			// anything else goes unanswered, like serveSilent
		}
	}
}
