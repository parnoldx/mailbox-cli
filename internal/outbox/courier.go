package outbox

import (
	"context"
	"fmt"
	"log"
)

// Transport hands a mail to a submission server. It is an interface so the
// queue can be driven through a refusing, an unreachable and a silent server
// without one existing.
type Transport interface {
	Send(ctx context.Context, from string, to []string, raw []byte) error
}

// Filer puts the copy of a sent mail into a Box. This is IMAP APPEND, and its
// ack carries the uid the copy now has (ADR-0004).
type Filer interface {
	Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error)
}

// Courier moves mail out of the queue: SMTP first, then the copy in Sent.
//
// The two halves are not the same kind of operation and are not retried the
// same way. SMTP is tried once per attempt and its outcome is recorded before
// anything else happens (ADR-0017). Filing is idempotent enough to retry — the
// worst case is a second copy in Sent, and the state machine only lets it run
// against a mail that has already been sent.
type Courier struct {
	Box       *Outbox
	Transport Transport
	Filer     Filer
	// Account is whose mail this Courier carries. Empty means all of it, which
	// is what a single-account setup has.
	Account string
	// SentBox is where the copy goes. Empty means do not file: a send is still
	// a send without a copy of it.
	SentBox string
	Log     *log.Logger
}

// Deliver sends one queued mail and files its copy. The Item it returns is the
// row as it now stands, whether that is filed, sent-but-unfiled, or back in the
// queue with the reason.
func (c *Courier) Deliver(ctx context.Context, id int64) (Item, error) {
	it, err := c.Box.Get(id)
	if err != nil {
		return Item{}, err
	}
	switch it.State {
	case Queued:
		if err := c.Box.Claim(id); err != nil {
			return it, err
		}
		if err := c.Transport.Send(ctx, it.From, it.Recipients, it.Raw); err != nil {
			// SMTP said no, so it was not accepted and may be tried again.
			if rerr := c.Box.Requeue(id, err); rerr != nil {
				return it, rerr
			}
			return c.Box.Get(id)
		}
		// Written before the copy is filed, and before anything else can fail:
		// from here the mail exists in the world and must never be sent twice.
		if err := c.Box.MarkSent(id); err != nil {
			return it, err
		}
	case Sent:
		// Sent but never filed. Only the copy is outstanding.
	default:
		return it, fmt.Errorf("#%d is %s, not queued", id, it.State)
	}

	return c.file(ctx, id)
}

// file puts the copy in Sent. A copy that cannot be filed is not a failed send:
// the mail has gone, so the error is recorded against the row and the next
// drain tries the copy again.
func (c *Courier) file(ctx context.Context, id int64) (Item, error) {
	it, err := c.Box.Get(id)
	if err != nil {
		return Item{}, err
	}
	if c.SentBox == "" || c.Filer == nil {
		return it, nil
	}
	uid, err := c.Filer.Append(ctx, c.SentBox, []string{`\Seen`}, it.Raw)
	if err != nil {
		if nerr := c.Box.NoteError(id, fmt.Errorf("sent, but the copy could not be filed: %w", err)); nerr != nil {
			return it, nerr
		}
		return c.Box.Get(id)
	}
	if err := c.Box.MarkFiled(id, c.SentBox, uid); err != nil {
		return it, err
	}
	return c.Box.Get(id)
}

// Recover is run once at startup. It holds every mail a dead process left at
// the SMTP server, because nobody here can tell whether it went out. It is not
// per-account: a held mail is held whoever was sending it.
func (c *Courier) Recover() ([]Item, error) {
	held, err := c.Box.HoldInterrupted()
	if err != nil {
		return nil, err
	}
	for _, it := range held {
		c.logf("outbox #%d (%q) was interrupted mid-send and is held: `mailbox outbox retry %d` to send it again, `mailbox outbox cancel %d` to drop it",
			it.ID, it.Subject, it.ID, it.ID)
	}
	return held, nil
}

// Drain sends everything queued and files every copy still outstanding. It
// stops sending at the first failure: a queue that keeps trying every mail
// against a server that is down turns one outage into a hundred retries, and
// the next drain will find them all anyway.
func (c *Courier) Drain(ctx context.Context) (sent int, err error) {
	unfiled, err := c.Box.UnfiledFor(c.Account)
	if err != nil {
		return 0, err
	}
	for _, it := range unfiled {
		if _, err := c.file(ctx, it.ID); err != nil {
			c.logf("outbox #%d: %v", it.ID, err)
		}
	}

	pending, err := c.Box.PendingFor(c.Account)
	if err != nil {
		return 0, err
	}
	for _, it := range pending {
		out, derr := c.Deliver(ctx, it.ID)
		if derr != nil {
			return sent, derr
		}
		if out.State == Queued {
			// Still queued means SMTP refused it; the reason is on the row.
			c.logf("outbox #%d: %s", out.ID, out.LastError)
			return sent, nil
		}
		sent++
	}
	return sent, nil
}

func (c *Courier) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log.Printf(format, args...)
	}
}
