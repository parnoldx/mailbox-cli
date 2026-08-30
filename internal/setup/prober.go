package setup

import (
	"context"

	"mailbox/internal/davdrv"
	"mailbox/internal/imapdrv"
	"mailbox/internal/sievedrv"
	"mailbox/internal/smtpdrv"
	"mailbox/internal/sync/davsync"
)

// Servers is the Prober that talks to the real ones. Each probe connects,
// asks, and disconnects: the wizard runs before there is a daemon to own any
// of these connections (ADR-0012).
type Servers struct{}

// IMAP implements Prober.
func (Servers) IMAP(ctx context.Context, host string, port int, user, password string) ([]imapdrv.Box, error) {
	drv, err := imapdrv.Dial(imapdrv.Config{Host: host, Port: port, Username: user, Password: password})
	if err != nil {
		return nil, err
	}
	defer drv.Close()
	return drv.Boxes(ctx)
}

// SMTP implements Prober.
func (Servers) SMTP(ctx context.Context, host string, port int, user, password string) error {
	return smtpdrv.New(smtpdrv.Config{Host: host, Port: port, Username: user, Password: password}).Check(ctx)
}

// DAV implements Prober.
func (Servers) DAV(ctx context.Context, endpoint, user, password string) ([]davsync.Collection, error) {
	return davdrv.New(davdrv.Config{Endpoint: endpoint, Username: user, Password: password}).Collections(ctx)
}

// Routing implements Prober. It creates the Boxes a fresh account has not got
// and puts an empty Routing script up if there is none — the last thing here
// that talks to a server, and the only one that changes anything on it.
//
// A script already called `logic` is never rewritten: it holds the decisions.
func (Servers) Routing(ctx context.Context, a Answers, boxes []string) (Bootstrap, error) {
	drv, err := imapdrv.Dial(imapdrv.Config{
		Host: a.IMAPHost, Port: a.IMAPPort, Username: a.Email, Password: a.Password,
	})
	if err != nil {
		return Bootstrap{}, err
	}
	defer drv.Close()
	sieve := sievedrv.New(sievedrv.Config{
		Host: a.IMAPHost, Port: 4190, Username: a.Email, Password: a.Password,
	})
	return EnsureRouting(ctx, drv, sieve, boxes)
}
