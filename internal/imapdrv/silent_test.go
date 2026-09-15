package imapdrv

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// A server that greets, accepts LOGIN and then goes silent — the shape that
// froze a real account for six hours on 2026-09-15: one APPEND answered by
// nothing, its Wait parked on a socket that never speaks again. cmdCap must
// make the Driver give up instead.
func TestAppendGivesUpWhenServerGoesSilent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go serveSilent(ln, t)
	oldCap := cmdCap
	cmdCap = 500 * time.Millisecond
	defer func() { cmdCap = oldCap }()

	d, err := Dial(Config{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Username: "u", Password: "p",
		TLS: &tls.Config{InsecureSkipVerify: true}, // a self-signed fake server
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer d.Close()

	start := time.Now()
	if _, err := d.Append(t.Context(), "Sent", nil, []byte("Subject: x\r\n\r\nbody")); err == nil {
		t.Fatal("append against a silent server returned no error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("append parked %v instead of giving up at cmdCap", elapsed)
	}
}

// serveSilent speaks just enough IMAP for Dial to get through — greeting,
// LOGIN, CAPABILITY — and then goes silent on APPEND: no continuation, no
// tagged response, nothing. That silence is the whole point of this server.
func serveSilent(ln net.Listener, t *testing.T) {
	cert := selfSigned(t)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			defer tc.Close()
			if _, err := tc.Write([]byte("* OK [CAPABILITY IMAP4rev1] fake ready\r\n")); err != nil {
				return
			}
			buf := make([]byte, 4096)
			for {
				n, err := tc.Read(buf)
				if err != nil {
					return
				}
				fields := strings.Fields(string(buf[:n]))
				if len(fields) < 2 {
					continue
				}
				switch fields[1] {
				case "LOGIN":
					tc.Write([]byte(fields[0] + " OK logged in\r\n"))
				case "CAPABILITY":
					tc.Write([]byte("* CAPABILITY IMAP4rev1\r\n" + fields[0] + " OK done\r\n"))
					// APPEND gets silence — no continuation, no reply.
				}
			}
		}()
	}
}

// selfSigned is the one certificate the fake server needs.
func selfSigned(t *testing.T) tls.Certificate {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
