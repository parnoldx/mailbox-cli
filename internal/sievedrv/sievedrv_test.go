package sievedrv

import (
	"context"
	"net"
	"testing"
	"time"
)

// A server that accepts the connection and then never speaks must not park
// the caller forever: greeting, STARTTLS, authentication and every command
// carry one deadline, because the library sets none of its own.
func TestASilentServerIsNotWaitedOnForever(t *testing.T) {
	old := commandTimeout
	commandTimeout = 100 * time.Millisecond
	defer func() { commandTimeout = old }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	silent := make(chan net.Conn, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			silent <- c // held open, never speaking, closed on test exit
		}
	}()
	defer func() {
		if c := <-silent; c != nil {
			c.Close()
		}
	}()

	d := New(Config{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Username: "u", Password: "p",
	})
	start := time.Now()
	if _, _, err := d.Scripts(context.Background()); err == nil {
		t.Fatal("a silent server must be an error")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("waited %s on a silent server, the deadline did not bite", took)
	}
}
