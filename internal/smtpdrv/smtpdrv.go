// Package smtpdrv hands a finished mail to the submission server. It is the
// only thing in the program that speaks SMTP, and it holds no state: a
// submission connection is cheap and an idle one gets dropped by the server
// anyway, so each send dials.
package smtpdrv

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// Config is what it takes to reach one submission server.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Sender submits mail.
type Sender struct {
	cfg Config
}

// New returns a Sender. Nothing is dialled until something is sent.
func New(cfg Config) *Sender { return &Sender{cfg: cfg} }

// Send delivers raw to the envelope recipients. It returns an error unless the
// server accepted the whole transaction — an accepted mail is one the queue may
// mark sent and never send again, so anything less than acceptance must read as
// a failure here.
func (s *Sender) Send(ctx context.Context, from string, to []string, raw []byte) error {
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	c, err := s.dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()

	auth := sasl.NewPlainClient("", s.cfg.Username, s.cfg.Password)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.SendMail(from, to, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	// QUIT is what turns "the server read our data" into "the server took
	// responsibility for it". A dropped connection before this is a mail that
	// may or may not exist, which is exactly the state the Outbox refuses to
	// guess about.
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

// dial picks the right kind of TLS for the port. 465 is implicit TLS, which is
// what mailbox.org's submission port is; 587 negotiates it with STARTTLS.
// Neither ever runs in the clear: the password goes over this connection.
func (s *Sender) dial(addr string) (*smtp.Client, error) {
	cfg := &tls.Config{ServerName: s.cfg.Host}
	var c *smtp.Client
	var err error
	if s.cfg.Port == 587 {
		c, err = smtp.DialStartTLS(addr, cfg)
	} else {
		c, err = smtp.DialTLS(addr, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	c.CommandTimeout = 60 * time.Second
	c.SubmissionTimeout = 5 * time.Minute
	return c, nil
}

// Check logs in and hangs up. It is what the setup wizard asks before writing a
// password to disk: a password that works for IMAP and not for submission is a
// failure that would otherwise surface at the worst moment, on the first send.
func (s *Sender) Check(ctx context.Context) error {
	c, err := s.dial(fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port))
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(sasl.NewPlainClient("", s.cfg.Username, s.cfg.Password)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	return c.Quit()
}
