package daemon

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"mailbox/internal/pickup"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// handedOver records what a Pickup put on the clipboard and what it told the
// desktop, in place of actually running wl-copy and notify-send.
type handedOver struct{ calls [][]string }

func (h *handedOver) install(t *testing.T) {
	t.Helper()
	was := run
	run = func(name string, args ...string) error {
		h.calls = append(h.calls, append([]string{name}, args...))
		return nil
	}
	t.Cleanup(func() { run = was })
}

func (h *handedOver) arg(cmd string) []string {
	for _, c := range h.calls {
		if c[0] == cmd {
			return c[1:]
		}
	}
	return nil
}

// deliverScreener puts one mail in the Screener as it would arrive — with a
// body, and dated now, since a Pickup is by definition mail that has just
// landed — and runs the cycle over it.
func deliverScreener(t *testing.T, d *Daemon, key, subject, from, body string) map[string]mailsync.Outcome {
	t.Helper()
	ctx := context.Background()
	a := d.primaryAccount()
	// One sync first, so the Boxes the seed wrote straight into the Mirror are
	// settled. Without it the delivery below lands in the same pass that first
	// reads every folder, and a resync reports no arrivals — which is exactly
	// what collectPickups relies on to not fire on a Mirror rebuild.
	if _, err := a.Reconciler.SyncAll(ctx, d.Mirrored); err != nil {
		t.Fatal(err)
	}
	m := fakeOf(d).Deliver(routing.BoxScreener, key, subject, body)
	m.From, m.Date = from, time.Now()
	out, err := a.Reconciler.SyncAll(ctx, d.Mirrored)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Gate 1. A code mail is collected rather than delivered: the code lands on the
// clipboard, a notification says so, and the mail is left read and flagged.
func TestPickupCopiesTheCodeAndQuietensTheMail(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)

	out := deliverScreener(t, d, "otp@example.com", "Your verification code",
		"Example Login <no-reply@example.com>",
		"Hi,\n\nYour code is 481920.\n\nIt expires in 10 minutes.\n")
	got := d.collectPickups(context.Background(), d.primaryAccount(), out)

	if len(got) != 1 {
		t.Fatalf("collectPickups found %d pickups, want 1", len(got))
	}
	for _, code := range got {
		if code != "481920" {
			t.Errorf("code = %q, want 481920", code)
		}
	}
	if c := h.arg("wl-copy"); len(c) != 1 || c[0] != "481920" {
		t.Errorf("clipboard got %v, want [481920]", c)
	}
	if n := h.arg("notify-send"); len(n) == 0 || !strings.Contains(strings.Join(n, " "), "481920") {
		t.Errorf("notification %v does not carry the code", n)
	}

	rows := rowsIn(t, d, routing.BoxScreener)
	var found bool
	for _, r := range rows {
		if r.Message.Subject != "Your verification code" {
			continue
		}
		found = true
		if !r.Seen() {
			t.Error("a collected Pickup is still unread; it would raise a notification of its own")
		}
		if !hasFlag(r.Placement.Flags, pickup.Keyword) {
			t.Errorf("flags %v carry no %s", r.Placement.Flags, pickup.Keyword)
		}
	}
	if !found {
		t.Fatal("the pickup mail is not in the Screener")
	}
}

// Gate 2. The Screener never asks for a decision about a Pickup's sender: a
// login form you used once is not somebody to route.
func TestPickupIsNotAScreenerDecision(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)

	out := deliverScreener(t, d, "otp2@example.com", "Your login code",
		"Auth <auth@example.net>", "Your code is 771034.\n")
	if got := d.collectPickups(context.Background(), d.primaryAccount(), out); len(got) != 1 {
		t.Fatalf("collectPickups found %d pickups, want 1", len(got))
	}

	resp := mustAsk(t, d, []string{"screener"}, map[string]any{})
	got, ok := resp.Data.([]waiting)
	if !ok {
		t.Fatalf("screener returned %T", resp.Data)
	}
	for _, w := range got {
		if strings.Contains(w.Address, "auth@example.net") {
			t.Fatalf("the Screener is waiting on %s, which is a Pickup sender", w.Address)
		}
	}
}

// Gate 3. Ordinary mail is untouched, including mail whose body is full of
// code-shaped numbers. This is the whole reason detection gates on the subject.
func TestOrdinaryMailIsNotAPickup(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)

	out := deliverScreener(t, d, "order@example.com", "Vielen Dank für deine Bestellung",
		"Shop <shop@example.com>",
		"Bestellnummer:\n\n  2818304  \n\nDein Rabattcode: SPAR20\n")
	if got := d.collectPickups(context.Background(), d.primaryAccount(), out); len(got) != 0 {
		t.Fatalf("collectPickups took %v out of an order confirmation", got)
	}
	if len(h.calls) != 0 {
		t.Fatalf("an order confirmation reached the desktop: %v", h.calls)
	}
	for _, r := range rowsIn(t, d, routing.BoxScreener) {
		if r.Message.Subject == "Vielen Dank für deine Bestellung" && r.Seen() {
			t.Error("an order confirmation was marked read")
		}
	}
}

// Gate 4. A Pickup is binned once its window has passed, and not before. The
// scan reads the arrival instant rather than a stored deadline, so shortening
// the window applies to mail that is already flagged.
func TestPickupIsBinnedWhenItExpires(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)
	ctx := context.Background()

	out := deliverScreener(t, d, "otp3@example.com", "Security code",
		"Bank <no-reply@bank.example>", "Passcode: 9F4KQ2\n")
	if got := d.collectPickups(ctx, d.primaryAccount(), out); len(got) != 1 {
		t.Fatalf("collectPickups found %d pickups, want 1", len(got))
	}

	// Still inside the window: nothing moves.
	d.PickupExpiry = time.Hour
	d.binExpiredPickups(ctx, d.primaryAccount())
	if !inScreener(t, d, "Security code") {
		t.Fatal("a Pickup was binned while it was still current")
	}

	// Past it: gone to Trash, where a code you turn out to still need is
	// recoverable.
	d.PickupExpiry = time.Nanosecond
	d.binExpiredPickups(ctx, d.primaryAccount())
	if inScreener(t, d, "Security code") {
		t.Fatal("an expired Pickup is still in the Screener")
	}
}

// Gate 5. A watch is told `pickup` with the code on it, and never `added`+new:
// a widget that reacts to new mail must not raise a second alert for a mail
// that has already been dealt with.
func TestWatchReportsAPickupRatherThanNewMail(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)
	ctx := context.Background()

	ch := make(chan Change, 16)
	d.subscribe(Request{ID: "1"}, ch)

	out := deliverScreener(t, d, "otp4@example.com", "Your one-time code",
		"Example <no-reply@example.org>", "Your code is 224466.\n")
	pickups := d.collectPickups(ctx, d.primaryAccount(), out)
	d.watchMail(d.primaryAccount(), out, pickups)
	close(ch)

	var saw bool
	for c := range ch {
		if c.Subject != "Your one-time code" {
			continue
		}
		if c.New {
			t.Error("a Pickup was reported as new mail")
		}
		if c.Event == eventPickup {
			saw = true
			if c.Code != "224466" {
				t.Errorf("pickup line carries code %q, want 224466", c.Code)
			}
		}
	}
	if !saw {
		t.Fatal("no pickup line was reported")
	}
}

// Gate 6. A registration or magic link is the same errand as a code: the URL
// itself goes on the clipboard, and the notification names the host rather than
// the token.
func TestPickupCopiesARegistrationLink(t *testing.T) {
	d, _ := seedScreener(t)
	var h handedOver
	h.install(t)

	out := deliverScreener(t, d, "reg@example.com", "Bitte E-Mail-Adresse bestätigen",
		"Elster <portal@example.de>",
		"Guten Tag,\n\nzum Aktivieren:\nhttps://portal.example.de/eportal/auth/Registrierung?t=9f2ad91c4b\n")
	got := d.collectPickups(context.Background(), d.primaryAccount(), out)
	if len(got) != 1 {
		t.Fatalf("collectPickups found %d pickups, want 1", len(got))
	}

	want := "https://portal.example.de/eportal/auth/Registrierung?t=9f2ad91c4b"
	if c := h.arg("wl-copy"); len(c) != 1 || c[0] != want {
		t.Errorf("clipboard got %v, want [%s]", c, want)
	}
	n := strings.Join(h.arg("notify-send"), " ")
	if !strings.Contains(n, "portal.example.de") {
		t.Errorf("notification %q does not name the host", n)
	}
	if strings.Contains(n, "9f2ad91c4b") {
		t.Errorf("notification %q spells out the token; the host is the readable part", n)
	}
}

func inScreener(t *testing.T, d *Daemon, subject string) bool {
	t.Helper()
	for _, r := range rowsIn(t, d, routing.BoxScreener) {
		if r.Message.Subject == subject {
			return true
		}
	}
	return false
}

// The VPS Daemon (ADR-0025) mirrors the same account with no clipboard on it.
// Whichever Daemon marks the mail read takes it off the other, so the one that
// cannot show the code must leave it exactly as it found it.
func TestADaemonWithNoClipboardLeavesThePickupForTheDesktop(t *testing.T) {
	d, _ := seedScreener(t)
	was := run
	run = func(name string, args ...string) error { return exec.ErrNotFound }
	t.Cleanup(func() { run = was })

	out := deliverScreener(t, d, "otp5@example.com", "Your one-time code",
		"Example <no-reply@example.org>", "Your code is 224466.\n")
	if got := d.collectPickups(context.Background(), d.primaryAccount(), out); len(got) != 0 {
		t.Fatalf("collectPickups took %v with nowhere to hand it over", got)
	}
	if !inScreener(t, d, "Your one-time code") {
		t.Fatal("the Pickup was quietened by a Daemon that could not show it")
	}
}
