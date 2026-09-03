package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

func newReconcilerFor(d *Daemon) *mailsync.Reconciler {
	return &mailsync.Reconciler{Account: "primary", Mirror: d.Mirror, Driver: fakeOf(d)}
}

// dragOut moves a Screener message to another folder the way a second client
// (an iPhone, webmail) does: a MOVE this Daemon did not issue.
func dragOut(t *testing.T, d *Daemon, from string, uid uint32, to string) {
	t.Helper()
	if _, err := fakeOf(d).Move(context.Background(), from, []uint32{uid}, to); err != nil {
		t.Fatal(err)
	}
}

// baseline brings every mirrored folder to a synced state, so the next cycle is
// an incremental one that can carry Added/Gone deltas.
func baseline(t *testing.T, d *Daemon) {
	t.Helper()
	d.cycle(context.Background(), d.primaryAccount(), "baseline")
}

// Gate 1. Screener→Inbox from a second client, no command run, rewrites the
// script so that sender's next mail hits the Inbox, and sweeps their other
// waiting Screener mail with it.
func TestScreenerToInboxIsInferredAsADecision(t *testing.T) {
	d, sieve := seedScreener(t)
	ctx := context.Background()
	baseline(t, d)

	// news@example.com wrote twice — Screener:10 and Screener:12.
	dragOut(t, d, routing.BoxScreener, 10, routing.BoxInbox)
	d.cycle(ctx, d.primaryAccount(), "after drag")

	if l := routing.Parse(sieve.scripts[routing.ScriptName]); l.Of("news@example.com") != routing.Inbox {
		t.Fatalf("the drag did not route the sender to the Inbox:\n%s", sieve.scripts[routing.ScriptName])
	}
	routes, _ := d.Mirror.Routing("primary")
	if len(routes) != 1 || routes[0].Address != "news@example.com" || routes[0].To != "inbox" {
		t.Errorf("the mirror holds %+v", routes)
	}
	// The sender's other Screener mail followed it.
	if rows, _ := d.Mirror.Rows("primary", routing.BoxScreener, 50); len(rows) != 2 {
		t.Errorf("%d mails left in the screener, want 2 (the two other senders)", len(rows))
	}
	inbox, _ := d.Mirror.Rows("primary", "INBOX", 50)
	n := 0
	for _, r := range inbox {
		if strings.Contains(r.From, "news@example.com") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("the inbox holds %d of the sender's mails, want both", n)
	}
}

// Gate 2. The destination folder names the destination — Feed, Paper Trail and
// Screener/Block each write their own decision — and a drag anywhere else
// writes nothing.
func TestScreenerDragDestinationNamesTheDecision(t *testing.T) {
	cases := []struct {
		folder string
		want   routing.Destination
	}{
		{routing.BoxFeed, routing.Feed},
		{routing.BoxPaperTrail, routing.PaperTrail},
		{routing.BoxBlock, routing.Block},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			d, sieve := seedScreener(t)
			baseline(t, d)
			dragOut(t, d, routing.BoxScreener, 11, tc.folder) // bills@example.com
			d.cycle(context.Background(), d.primaryAccount(), "drag")
			if got := routing.Parse(sieve.scripts[routing.ScriptName]).Of("bills@example.com"); got != tc.want {
				t.Fatalf("drag to %s decided %q, want %q", tc.folder, got, tc.want)
			}
		})
	}

	// Screener→Archive is a plain move: no decision.
	d, sieve := seedScreener(t)
	f := fakeOf(d)
	f.AddFolder("Archive")
	a := d.primaryAccount()
	d.Mirrored = append(d.Mirrored, "Archive")
	d.Writer.Mirrored = d.Mirrored
	baseline(t, d)
	before := sieve.puts
	dragOut(t, d, routing.BoxScreener, 11, "Archive")
	d.cycle(context.Background(), a, "drag to archive")
	if sieve.puts != before {
		t.Errorf("a drag into Archive rewrote the script")
	}
	if routes, _ := d.Mirror.Routing("primary"); len(routes) != 0 {
		t.Errorf("a drag into Archive left a decision: %+v", routes)
	}
}

// Gate 3. A `mailbox route` decision followed by its move syncing does not
// produce a second, redundant script write — the inference re-derives the same
// entry and Set reports no change.
func TestACommandDecisionDoesNotEchoAsAnInference(t *testing.T) {
	d, sieve := seedScreener(t)
	ctx := context.Background()
	baseline(t, d)

	mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{"news@example.com"}, "to": "feed",
	})
	puts := sieve.puts
	if puts == 0 {
		t.Fatal("mailbox route did not write the script")
	}

	// The cycles that follow re-observe the moved mail in the Feed. That is the
	// write-through path's own move coming back, not a fresh decision.
	d.cycle(ctx, d.primaryAccount(), "after route")
	d.cycle(ctx, d.primaryAccount(), "and again")
	if sieve.puts != puts {
		t.Fatalf("the script was rewritten %d extra times — an echo loop", sieve.puts-puts)
	}
	if len(d.RecentInferred()) != 0 {
		t.Errorf("the command decision was re-recorded as an inference: %+v", d.RecentInferred())
	}
}

// Gate 4. A message moved into the Screener from a decision folder un-decides
// that sender — their next mail is owed a decision again.
func TestMovingIntoTheScreenerUnDecidesTheSender(t *testing.T) {
	d, sieve := seedScreener(t)
	ctx := context.Background()
	baseline(t, d)

	// Decide the sender first, by command.
	mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{"news@example.com"}, "to": "inbox",
	})
	if routing.Parse(sieve.scripts[routing.ScriptName]).Of("news@example.com") != routing.Inbox {
		t.Fatal("precondition: the sender is not routed to the inbox")
	}
	// Now a message of theirs is dragged from the Inbox back into the Screener.
	d.cycle(ctx, d.primaryAccount(), "settle")
	inbox, _ := d.Mirror.Rows("primary", "INBOX", 50)
	var uid uint32
	for _, r := range inbox {
		if strings.Contains(r.From, "news@example.com") {
			uid = r.Placement.UID
		}
	}
	if uid == 0 {
		t.Fatal("no inbox mail from the sender to drag back")
	}
	dragOut(t, d, "INBOX", uid, routing.BoxScreener)
	d.cycle(ctx, d.primaryAccount(), "drag back")

	if routing.Parse(sieve.scripts[routing.ScriptName]).Count() != 0 {
		t.Errorf("the sender is still on a list after being dragged back to the screener:\n%s",
			sieve.scripts[routing.ScriptName])
	}
}

// Gate 5. A second, always-on Daemon does the inference while the first is
// stopped: they share only the Mirror and the server.
func TestScreenerInferenceRunsOnASecondDaemon(t *testing.T) {
	d, sieve := seedScreener(t)
	ctx := context.Background()
	baseline(t, d)

	vps := New("primary", d.Mirror,
		newReconcilerFor(d), d.Mirrored, nil, nil)
	vps.Sieve = sieve

	dragOut(t, d, routing.BoxScreener, 10, routing.BoxFeed)
	vps.cycle(ctx, vps.primaryAccount(), "vps sees the drag")

	if routing.Parse(sieve.scripts[routing.ScriptName]).Of("news@example.com") != routing.Feed {
		t.Fatalf("the VPS Daemon did not infer the decision from the shared server state")
	}
}

// Gate 6. Every inferred decision is recorded for `status`, and logged.
func TestInferredDecisionsShowUpInStatus(t *testing.T) {
	d, _ := seedScreener(t)
	ctx := context.Background()
	baseline(t, d)
	dragOut(t, d, routing.BoxScreener, 10, routing.BoxFeed)
	d.cycle(ctx, d.primaryAccount(), "drag")

	got := d.RecentInferred()
	if len(got) != 1 || got[0].Address != "news@example.com" || got[0].To != "feed" {
		t.Fatalf("recent inferred = %+v", got)
	}
	// And it rides on the status reply.
	resp := mustAsk(t, d, []string{"status"}, nil)
	if len(resp.Inferred) != 1 {
		t.Errorf("status carries %d inferred decisions, want 1", len(resp.Inferred))
	}
}

// An active script that does not reach ours refuses an inferred decision, the
// same way it refuses a command one: a drag must not switch somebody else's
// filtering off.
func TestInferenceIsRefusedWhenTheRoutingIsUnreachable(t *testing.T) {
	d, sieve := seedScreener(t)
	ctx := context.Background()
	sieve.scripts["Open-Xchange"] = "# their rules\n"
	sieve.active = "Open-Xchange"
	baseline(t, d)

	dragOut(t, d, routing.BoxScreener, 10, routing.BoxFeed)
	d.cycle(ctx, d.primaryAccount(), "drag")

	if sieve.puts != 0 {
		t.Errorf("an inferred decision was written into an unreachable routing")
	}
	if len(d.RecentInferred()) != 0 {
		t.Errorf("an inferred decision was recorded despite the routing being unreachable")
	}
}
