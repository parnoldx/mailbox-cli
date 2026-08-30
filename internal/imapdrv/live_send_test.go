//go:build live

// The live half of sending: a real submission server, a real APPEND, and a
// real delivery back into the Inbox.
//
//	go test -tags live ./internal/imapdrv/ -run TestLiveSend -v
//
// It sends one mail from the account to itself, files a copy in
// INBOX/mailbox-selftest, and deletes the delivered mail again when it is done.
package imapdrv

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	compose "mailbox/internal/message"
	"mailbox/internal/smtpdrv"
)

func TestLiveSend(t *testing.T) {
	cfg := loadLive(t)
	other := dialOther(t, cfg)
	other.ensure()
	other.purge()
	t.Cleanup(other.purge)

	drv, err := Dial(Config{
		Host: "imap.mailbox.org", Port: 993,
		Username: cfg.Account.Email, Password: cfg.Account.Password,
	})
	if err != nil {
		t.Fatalf("dial driver: %v", err)
	}
	t.Cleanup(func() { drv.Close() })

	// Not valid UTF-8 and not printable: an attachment that survives because it
	// was text all along proves nothing about the transfer encoding.
	payload := []byte{0x00, 0xff, 0xfe, '%', 'P', 'D', 'F', 0x80, 0x0a, 0x1a}
	stamp := time.Now().Format("15:04:05")
	draft := compose.Draft{
		From:    compose.Address{Name: "mailbox selftest", Addr: cfg.Account.Email},
		To:      []compose.Address{{Addr: cfg.Account.Email}},
		Subject: "mailbox selftest " + stamp + " — Grüße",
		Body:    "Der Körper dieser Mail enthält Umlaute: wär, groß, Übung.\n",
		Attachments: []compose.Attachment{
			{Filename: "selftest.pdf", MIMEType: "application/pdf", Content: payload},
		},
	}
	raw, err := draft.Build()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	t.Logf("sending %d bytes as %s", len(raw), draft.MessageID)

	sender := smtpdrv.New(smtpdrv.Config{
		Host: "smtp.mailbox.org", Port: 465,
		Username: cfg.Account.Email, Password: cfg.Account.Password,
	})
	ctx := context.Background()
	if err := sender.Send(ctx, draft.From.Addr, draft.Recipients(), raw); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Gate: APPEND reports where the copy landed. Without UIDPLUS this is 0 and
	// the copy waits for a cycle to be found — worth knowing which we are on.
	uid, err := drv.Append(ctx, liveFolder, []string{`\Seen`}, raw)
	if err != nil {
		t.Fatalf("append the sent copy: %v", err)
	}
	if uid == 0 {
		t.Fatal("APPEND gave no APPENDUID: the copy cannot be named without a cycle")
	}
	t.Logf("gate: copy filed as %s:%d", liveFolder, uid)

	// The copy the server holds is the mail we composed: same text, same file.
	bodies, err := drv.FetchBodies(ctx, liveFolder, []uint32{uid})
	if err != nil || len(bodies) != 1 {
		t.Fatalf("fetch the copy: %v (%d bodies)", err, len(bodies))
	}
	if !bytes.Contains([]byte(bodies[0].Plain), []byte("Übung")) {
		t.Fatalf("the copy's text came back as %q", bodies[0].Plain)
	}
	if len(bodies[0].Parts) != 1 || bodies[0].Parts[0].Filename != "selftest.pdf" {
		t.Fatalf("the copy carries %+v", bodies[0].Parts)
	}
	got, err := drv.FetchPart(ctx, liveFolder, uid, bodies[0].Parts[0].Path)
	if err != nil {
		t.Fatalf("fetch the attachment back: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("the attachment came back as %v", got)
	}
	t.Log("gate: the filed copy is byte-identical to what was sent")

	// And the mail itself arrives, having been through a real MTA.
	delivered := waitForDelivery(t, other, draft.MessageID, 90*time.Second)
	t.Logf("gate: delivered to INBOX as uid %d", delivered)
	other.deleteFromInbox(delivered)
}

// waitForDelivery watches the Inbox for the Message-ID we minted. A mail we
// sent to ourselves is the only way to see the whole path, and the only way to
// find it again is the id we chose before it existed.
func waitForDelivery(t *testing.T, o *otherClient, messageID string, within time.Duration) imap.UID {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		var found imap.UID
		o.do("search inbox", func() error {
			if _, err := o.c.Select("INBOX", nil).Wait(); err != nil {
				return err
			}
			data, err := o.c.UIDSearch(&imap.SearchCriteria{
				Header: []imap.SearchCriteriaHeaderField{{Key: "Message-Id", Value: messageID}},
			}, &imap.SearchOptions{ReturnAll: true}).Wait()
			if err != nil {
				return err
			}
			if uids := data.AllUIDs(); len(uids) > 0 {
				found = uids[len(uids)-1]
			}
			return nil
		})
		if found != 0 {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mail never arrived within %s (message-id %s)", within, messageID)
		}
		time.Sleep(3 * time.Second)
	}
}

// deleteFromInbox takes the test's own mail back out of the Inbox. A suite that
// leaves litter in a real mailbox is one nobody runs twice.
func (o *otherClient) deleteFromInbox(uid imap.UID) {
	o.t.Helper()
	o.do("clean up inbox", func() error {
		if _, err := o.c.Select("INBOX", nil).Wait(); err != nil {
			return err
		}
		if err := o.c.Store(imap.UIDSetNum(uid), &imap.StoreFlags{
			Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
		}, nil).Close(); err != nil {
			return err
		}
		return o.c.Expunge().Close()
	})
}
