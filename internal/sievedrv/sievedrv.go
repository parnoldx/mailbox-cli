// Package sievedrv talks ManageSieve (RFC 5804). It is the fourth protocol
// this program speaks itself (ADR-0015), and the smallest: the Routing is one
// script, fetched whole and stored whole.
package sievedrv

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"

	"go.guido-berhoerster.org/managesieve"
)

// Config is where the scripts live. Port 4190 is the registered one and is what
// every provider offering ManageSieve uses.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Driver dials for each operation rather than holding a connection open. A
// routing decision happens a few times a day and the script is re-read on a
// ten-minute timer, so a connection held open all week would be idle for all of
// it — unlike the IMAP ones, which exist to be idle (ADR-0016).
type Driver struct{ cfg Config }

func New(cfg Config) *Driver {
	if cfg.Port == 0 {
		cfg.Port = 4190
	}
	return &Driver{cfg: cfg}
}

func (d *Driver) addr() string {
	return net.JoinHostPort(d.cfg.Host, strconv.Itoa(d.cfg.Port))
}

// dial connects, upgrades to TLS and authenticates. ManageSieve starts in the
// clear on 4190, so a server that does not offer STARTTLS is refused rather
// than downgraded: the password goes over this connection.
func (d *Driver) dial() (*managesieve.Client, error) {
	c, err := managesieve.Dial(d.addr())
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", d.addr(), err)
	}
	if !c.SupportsTLS() {
		c.Close()
		return nil, fmt.Errorf("%s does not offer STARTTLS: refusing to send the password in the clear", d.addr())
	}
	if err := c.StartTLS(&tls.Config{ServerName: d.cfg.Host}); err != nil {
		c.Close()
		return nil, fmt.Errorf("starttls to %s: %w", d.addr(), err)
	}
	if err := c.Authenticate(managesieve.PlainAuth("", d.cfg.Username, d.cfg.Password, d.cfg.Host)); err != nil {
		c.Close()
		return nil, fmt.Errorf("authenticate to %s: %w", d.addr(), err)
	}
	return c, nil
}

// Scripts lists the stored script names and which one is active. The daemon
// asks because a script it did not write must not be deactivated by one it did.
func (d *Driver) Scripts(ctx context.Context) (names []string, active string, err error) {
	c, err := d.dial()
	if err != nil {
		return nil, "", err
	}
	defer c.Close()
	names, active, err = c.ListScripts()
	if err != nil {
		return nil, "", fmt.Errorf("list sieve scripts: %w", err)
	}
	return names, active, nil
}

// Script fetches one script by name.
func (d *Driver) Script(ctx context.Context, name string) (string, error) {
	c, err := d.dial()
	if err != nil {
		return "", err
	}
	defer c.Close()
	body, err := c.GetScript(name)
	if err != nil {
		return "", fmt.Errorf("get sieve script %q: %w", name, err)
	}
	return body, nil
}

// PutScript stores a script and, if asked, makes it the active one. Both go
// down the same connection: a script uploaded and left inactive routes nothing,
// and finding that out on a second dial that failed is the worst way to learn
// it.
//
// The server compiles what it is given and refuses what it cannot, which is the
// check that matters — a Sieve script this program generated wrongly would
// otherwise misfile mail silently for as long as nobody looked.
func (d *Driver) PutScript(ctx context.Context, name, content string, activate bool) error {
	c, err := d.dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.PutScript(name, content); err != nil {
		return fmt.Errorf("put sieve script %q: %w", name, err)
	}
	if !activate {
		return nil
	}
	if err := c.ActivateScript(name); err != nil {
		return fmt.Errorf("activate sieve script %q: %w", name, err)
	}
	return nil
}

// SetActive makes a script already on the server the active one. Uploading and
// activating is one call because they belong together (see PutScript); this is
// the other case, where the script is already there and only the choice of
// which one runs is changing.
func (d *Driver) SetActive(ctx context.Context, name string) error {
	c, err := d.dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.ActivateScript(name); err != nil {
		return fmt.Errorf("activate sieve script %q: %w", name, err)
	}
	return nil
}
