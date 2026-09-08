package daemon

import (
	"context"
	"net/url"
	"os/exec"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/pickup"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// DefaultPickupExpiry is how long a Pickup sits before the Daemon bins it. A
// login code is usually dead in five to ten minutes; fifteen leaves room for a
// fumbled login without leaving the mail around long enough to be read as mail.
const DefaultPickupExpiry = 15 * time.Minute

// pickupWindow is how recent an arrival has to be to be considered at all. It
// is the cheapest half of the detection and the one that needs no regex: you
// are standing at a login form, so a code that arrived an hour ago is not the
// one you are waiting for, whatever its subject says. It also means a resync,
// which re-adds every message in a Box, cannot fire a notification for mail
// from 2019.
const pickupWindow = 15 * time.Minute

// collectPickups takes the codes out of this cycle's new mail: for each Pickup
// it copies the code to the clipboard, raises a notification, marks the mail
// read and flags it, and reports it so watchMail says `pickup` instead of
// `added`+new.
//
// It runs before watchMail so that a code mail never reaches the Screener or
// the Inbox as news. That is the point of the feature rather than a detail of
// it: the mail is not something to decide about, it is something to collect,
// and the sender behind it is one you will never hear from again.
func (d *Daemon) collectPickups(ctx context.Context, a *Account, outcomes map[string]mailsync.Outcome) map[int64]string {
	if a.Writer == nil {
		return nil
	}
	found := map[int64]string{}
	cutoff := time.Now().Add(-pickupWindow)
	for folder, out := range outcomes {
		// Only where mail lands unbidden. The Feed and the Paper Trail are
		// skimmed, not answered, and nothing routed there is waiting on you;
		// Sent, Drafts and Trash are not arrivals at all.
		if !pickupBox(shortBox(folder, a.Mirrored)) {
			continue
		}
		// ActionResync re-adds a whole Box. The window below rejects all of it
		// anyway, but skipping saves reading a thousand bodies to prove it.
		if out.Action == mailsync.ActionResync {
			continue
		}
		for _, delta := range out.Added {
			row, err := d.Mirror.Changed(a.Name, folder, delta.MessageID)
			if err != nil || row.Seen() {
				continue
			}
			if arrivedAt(row).Before(cutoff) {
				continue
			}
			code, link := pickup.Find(row.Message.Subject, row.Message.TextPlain)
			if code == "" && link == "" {
				// A subject that read like a Pickup but carried nothing to
				// collect. Logged rather than dropped: the phrase list was
				// built on an archive with almost no true positives in it —
				// they get deleted — so this line is how it gets tuned against
				// mail that actually arrives.
				if pickup.Candidate(row.Message.Subject) {
					d.logf("pickup: subject matched but nothing to collect: %q", row.Message.Subject)
				}
				continue
			}
			ref := mailsync.Ref{Folder: folder, UID: row.Placement.UID}
			taken, err := d.takePickup(ctx, a, ref, row, code, link)
			if err != nil {
				// The code is on the clipboard but the mail is still unread on
				// the server: next cycle sees it again, which is the duplicate
				// notification takePickup trades for never losing a code.
				d.logf("pickup %s: %v", row.Message.Subject, err)
				continue
			}
			if !taken {
				// Left as it arrived, so it is ordinary new mail to whoever is
				// watching this Daemon rather than a code already collected.
				continue
			}
			found[delta.MessageID] = code
		}
	}
	return found
}

// takePickup is what happens to one Pickup, in the order that matters: hand the
// code over first, then quieten the mail. Flagging is last because a failure
// there costs a duplicate notification next cycle, while doing it first would
// cost the code entirely. Reports whether this Daemon took the mail at all.
func (d *Daemon) takePickup(ctx context.Context, a *Account, ref mailsync.Ref, row mirror.Row, code, link string) (bool, error) {
	if !d.handOver(row.Message.From, code, link) {
		// Nowhere to hand it to, so this Daemon has no business quietening it.
		// That is the whole of the VPS question (ADR-0025): two Daemons mirror
		// this account and whichever quietens first takes the mail off the
		// other, so the one that cannot show a code leaves it alone and the
		// desktop collects it on its next cycle. No flag to set and no display
		// to sniff for: a clipboard that is not there says so by failing.
		return false, nil
	}
	// \Seen and the keyword in one round trip. \Seen is what stops the widget
	// counting it and the phone buzzing for it; the keyword is what the expiry
	// scan and the Screener listing read.
	_, err := a.Writer.StoreFlags(ctx, []mailsync.Ref{ref}, []string{`\Seen`, pickup.Keyword}, nil)
	return true, err
}

// handOver puts the thing you are waiting for on the clipboard and says so.
// A code if the mail carried one, otherwise the link — a registration or magic
// link is the same errand as a code, and pasting a URL into the bar beats
// opening a mail client to find it.
//
// Copied, never followed. Opening the link here would log you in, or confirm an
// address, from a notification you had not read yet; that decision stays yours,
// and it is one keystroke away once the URL is on the clipboard.
//
// Reports whether the clipboard took it, which is the caller's licence to mark
// the mail read. The notification is not part of that answer: a desktop missing
// notify-send still has the code where you need it, and half a hand-over beats
// leaving the mail for a Daemon that may be the one without notify-send too.
func (d *Daemon) handOver(from, code, link string) bool {
	copied, body := code, code
	if code == "" {
		// A link on its own. The notification shows the host rather than the
		// URL: a magic link is a screenful of opaque token, and the one thing
		// worth reading before pasting it is who it logs you in to.
		copied, body = link, "link copied · "+hostOf(link)
	} else if link != "" {
		body += " · login link in the mail"
	}
	if err := run("wl-copy", copied); err != nil {
		d.logf("pickup: no clipboard: %v", err)
		return false
	}
	who := routing.NameOf(from)
	if who == "" {
		who = routing.AddressOf(from)
	}
	title := "Code from " + who
	if code == "" {
		title = "Link from " + who
	}
	if err := run("notify-send", "-a", "mailbox", "-u", "critical", title, body); err != nil {
		d.logf("pickup: no notification: %v", err)
	}
	d.logf("pickup from %s: code %q link %q", who, code, link)
	return true
}

// hostOf is the host a link points at, for a notification that has no room for
// the token behind it. A URL that will not parse is shown as it is rather than
// swallowed: something is better than an empty notification.
func hostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return link
	}
	return u.Host
}

// run is a desktop side effect that is allowed to be unavailable. A var so a
// test can watch what was handed over without a clipboard or a notification
// daemon anywhere near it.
var run = func(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return err
	}
	return exec.Command(name, args...).Run()
}

// pickupLoop bins Pickups whose window has passed. Like bubbleLoop it scans by
// wall clock rather than sleeping on a per-message timer, so a Daemon that was
// down when one came due bins it on the first tick after startup (ADR-0023's
// reasoning, and the reason the arrival instant is the only state).
func (d *Daemon) pickupLoop(ctx context.Context) {
	a := d.primaryAccount()
	if a == nil || a.Writer == nil {
		return
	}
	every := d.PollEvery
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		d.binExpiredPickups(ctx, a)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// binExpiredPickups is one pickupLoop tick. The mail is already \Seen, so this
// only moves it: to Trash, where a code you turned out to still need is
// recoverable for as long as the server keeps it — the same bargain `block`
// makes, and for the same reason.
func (d *Daemon) binExpiredPickups(ctx context.Context, a *Account) {
	held, err := d.Mirror.Pickups(a.Name)
	if err != nil {
		d.logf("pickup scan: %v", err)
		return
	}
	cutoff := time.Now().Add(-d.pickupExpiry())
	var refs []mailsync.Ref
	boxes := map[string]bool{}
	for _, p := range held {
		// A Pickup with no instant at all — no internaldate and no Date:
		// header — is binned rather than kept: it is already read and already
		// collected, and the one outcome to avoid here is a $pickup that lives
		// forever because nothing can say how old it is.
		if !p.InternalDate.IsZero() && p.InternalDate.After(cutoff) {
			continue
		}
		refs = append(refs, mailsync.Ref{Folder: p.Folder, UID: p.UID})
		boxes[shortBox(p.Folder, a.Mirrored)] = true
	}
	if len(refs) == 0 {
		return
	}
	if _, err := a.Writer.Move(ctx, refs, "Trash"); err != nil {
		d.logf("pickup bin: %v", err)
		return
	}
	for box := range boxes {
		d.push(Push{Event: "mail.changed", Account: a.Name, Box: box})
	}
	d.logf("pickup: binned %d expired", len(refs))
}

// pickupExpiry is the configured window, or the default.
func (d *Daemon) pickupExpiry() time.Duration {
	if d.PickupExpiry > 0 {
		return d.PickupExpiry
	}
	return DefaultPickupExpiry
}

// pickupBox says whether mail landing in this Box can be a Pickup. Only the two
// Boxes mail arrives in unasked: a code is always something you just triggered,
// so it reaches the Inbox if the sender is known and the Screener if not. The
// argument is the short name a listing prints, which is what boxIs matches.
func pickupBox(box string) bool {
	return boxIs(routing.BoxInbox, box) || boxIs(routing.BoxScreener, box)
}

// arrivedAt is when a Message landed, preferring the server's own instant over
// the Date: header, which a sender writes and can get wrong.
func arrivedAt(row mirror.Row) time.Time {
	if !row.Placement.InternalDate.IsZero() {
		return row.Placement.InternalDate
	}
	return row.Message.Date
}
