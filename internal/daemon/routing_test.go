package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/mailsync"
)

// fakeSieve is a ManageSieve server that holds scripts in a map. It is enough
// to drive every decision this slice makes: the Routing is one script, and what
// a real server adds is a compiler, which is what the live test is for.
type fakeSieve struct {
	scripts map[string]string
	active  string
	puts    int
	fail    error
}

func newFakeSieve() *fakeSieve {
	return &fakeSieve{scripts: map[string]string{}}
}

func (f *fakeSieve) Scripts(ctx context.Context) ([]string, string, error) {
	if f.fail != nil {
		return nil, "", f.fail
	}
	var names []string
	for n := range f.scripts {
		names = append(names, n)
	}
	return names, f.active, nil
}

func (f *fakeSieve) SetActive(ctx context.Context, name string) error {
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.scripts[name]; !ok {
		return fmt.Errorf("no script %q", name)
	}
	f.active = name
	return nil
}

func (f *fakeSieve) Script(ctx context.Context, name string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	return f.scripts[name], nil
}

func (f *fakeSieve) PutScript(ctx context.Context, name, content string, activate bool) error {
	if f.fail != nil {
		return f.fail
	}
	f.puts++
	f.scripts[name] = content
	if activate {
		f.active = name
	}
	return nil
}

// screenerBoxes is the Primary Account's set: the Screener and the three Boxes
// a decision can send mail to, plus the two hand-tended piles.
var screenerBoxes = []string{
	"INBOX", routing.BoxScreener, routing.BoxFeed, routing.BoxPaperTrail,
	routing.BoxBlock, routing.BoxAside, routing.BoxReplyLater,
}

// seedScreener builds a Primary Account with mail from three senders waiting in
// the Screener — one of whom wrote twice — and a Sieve server holding the
// script the account was using before this program owned it.
func seedScreener(t *testing.T) (*Daemon, *fakeSieve) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	f := mailsync.NewFake("INBOX")
	for _, name := range screenerBoxes[1:] {
		f.AddFolder(name)
	}
	f.AddFolder("Trash")
	f.Folder(routing.BoxScreener).UIDNext = 10

	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	when := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	for _, mail := range []struct {
		key, subject, from string
	}{
		{"a1@example.com", "Newsletter #41", "Beispiel News <news@example.com>"},
		{"b1@example.com", "Ihre Rechnung", "Rechnungen <bills@example.com>"},
		{"a2@example.com", "Newsletter #42", "Beispiel News <news@example.com>"},
		{"c1@example.com", "Gewinnspiel", "spam@example.net"},
	} {
		when = when.Add(time.Hour)
		id, _, err := tx.UpsertMessage(mirror.Message{
			Key: mail.key, Date: when, Subject: mail.subject, From: mail.from,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SetBody(id, mail.subject, "", mail.subject); err != nil {
			t.Fatal(err)
		}
		msg := f.Deliver(routing.BoxScreener, mail.key, mail.subject, mail.subject)
		msg.From, msg.Date = mail.from, when
		if err := tx.PutPlacement(mirror.Placement{
			Folder: routing.BoxScreener, UID: msg.UID, MessageID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	r := &mailsync.Reconciler{Account: "primary", Mirror: m, Driver: f}
	d := New("primary", m, r, screenerBoxes, nil, nil)
	sieve := newFakeSieve()
	sieve.scripts[routing.ScriptName] = routing.New().Script()
	sieve.active = routing.ScriptName
	d.Sieve = sieve
	if err := d.refreshRouting(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d, sieve
}

func ask(t *testing.T, d *Daemon, cmd []string, args map[string]any) Response {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	return d.handle(context.Background(), Request{ID: "1", Cmd: cmd, Args: args})
}

func mustAsk(t *testing.T, d *Daemon, cmd []string, args map[string]any) Response {
	t.Helper()
	resp := ask(t, d, cmd, args)
	if !resp.OK {
		t.Fatalf("%v: %s (%s)", cmd, resp.Error, resp.Code)
	}
	return resp
}

// Gate 1. The Screener is a list of decisions owed, and a decision is owed per
// sender: two mails from one address are one thing to decide.
func TestScreenerIsOneLinePerSender(t *testing.T) {
	d, _ := seedScreener(t)
	resp := mustAsk(t, d, []string{"screener"}, nil)
	got, ok := resp.Data.([]waiting)
	if !ok {
		t.Fatalf("screener returned %T", resp.Data)
	}
	if len(got) != 3 {
		t.Fatalf("%d senders, want 3: %+v", len(got), got)
	}
	// Newest first: the last mail seeded was the spam one.
	if got[0].Address != "spam@example.net" {
		t.Errorf("first sender is %q, want the newest", got[0].Address)
	}
	var news *waiting
	for i := range got {
		if got[i].Address == "news@example.com" {
			news = &got[i]
		}
	}
	if news == nil {
		t.Fatalf("news@example.com is not in the screener: %+v", got)
	}
	if news.Count != 2 {
		t.Errorf("news@example.com has %d mails, want 2", news.Count)
	}
	if news.Name != "Beispiel News" {
		t.Errorf("name = %q", news.Name)
	}
	// The id reads the newest of them, so a decision that needs the mail read
	// does not need a search first.
	if news.Subject != "Newsletter #42" || news.ID != "Screener:12" {
		t.Errorf("newest = %q under %q", news.Subject, news.ID)
	}
}

// Gate 2. A decision is one command: the script that files the sender's next
// mail, and the mail already waiting, which moves with it.
func TestDecidingRoutesAndMovesWhatIsWaiting(t *testing.T) {
	d, sieve := seedScreener(t)
	resp := mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{routing.BoxScreener + ":10"}, "to": "feed",
	})
	got, ok := resp.Data.([]decision)
	if !ok || len(got) != 1 {
		t.Fatalf("route returned %T %+v", resp.Data, resp.Data)
	}
	// The sender came from the Message, which is what a caller has in hand.
	if got[0].Address != "news@example.com" || !got[0].Changed {
		t.Fatalf("decision = %+v", got[0])
	}
	// Both of that sender's mails moved, and only theirs.
	if len(got[0].Moved) != 2 {
		t.Fatalf("moved %v, want both of them", got[0].Moved)
	}
	if !strings.HasPrefix(got[0].Moved[0], "Feed:") {
		t.Errorf("moved to %q", got[0].Moved[0])
	}

	// The script the server now runs files their next mail into the Feed.
	if l := routing.Parse(sieve.scripts[routing.ScriptName]); l.Of("news@example.com") != routing.Feed {
		t.Errorf("the script does not route them:\n%s", sieve.scripts[routing.ScriptName])
	}
	// And the Mirror answers the same question offline, with no second read of
	// the server.
	routes, err := d.Mirror.Routing("primary")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Address != "news@example.com" || routes[0].Box != routing.BoxFeed {
		t.Errorf("mirror holds %+v", routes)
	}
	// The Screener is down to the two senders still owed a decision.
	rows, err := d.Mirror.Rows("primary", routing.BoxScreener, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("%d mails left in the screener, want 2", len(rows))
	}
}

// Gate 3. Blocking discards what comes next and keeps what is already here: the
// script never sees another mail from them, and the pile that is already in the
// Screener goes somewhere a mistake can be found again.
func TestBlockingDiscardsLaterAndPilesUpWhatIsHere(t *testing.T) {
	d, sieve := seedScreener(t)
	resp := mustAsk(t, d, []string{"route"}, map[string]any{
		"positional": []any{"spam@example.net"}, "to": "block",
	})
	got := resp.Data.([]decision)
	if got[0].Box != "" {
		t.Errorf("a blocked sender files into %q; their mail is discarded", got[0].Box)
	}
	if len(got[0].Moved) != 1 || !strings.HasPrefix(got[0].Moved[0], "Screener/Block:") {
		t.Errorf("moved = %v, want it in %s", got[0].Moved, routing.BoxBlock)
	}
	script := sieve.scripts[routing.ScriptName]
	if !strings.Contains(script, "discard") {
		t.Errorf("the script does not discard:\n%s", script)
	}
	if routing.Parse(script).Of("spam@example.net") != routing.Block {
		t.Errorf("the sender is not blocked:\n%s", script)
	}
}

// Gate 4. A sender has one destination. Deciding again moves them rather than
// listing them twice, which the script could not act on anyway: the first rule
// that matches wins and the second would be text nothing reaches.
func TestASecondDecisionReplacesTheFirst(t *testing.T) {
	d, sieve := seedScreener(t)
	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})
	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "inbox"})
	l := routing.Parse(sieve.scripts[routing.ScriptName])
	if l.Count() != 1 || l.Of("news@example.com") != routing.Inbox {
		t.Errorf("after two decisions the script says %v", l.All())
	}
	routes, _ := d.Mirror.Routing("primary")
	if len(routes) != 1 || routes[0].To != "inbox" {
		t.Errorf("the mirror says %+v", routes)
	}

	// Forgetting a sender takes them off every list, which puts their next mail
	// back in the Screener.
	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "screener"})
	if routing.Parse(sieve.scripts[routing.ScriptName]).Count() != 0 {
		t.Errorf("forgetting left the sender on a list:\n%s", sieve.scripts[routing.ScriptName])
	}
}

// Gate 5. Nothing is written until the whole decision can be carried out. A
// destination Box that is not there is a Sieve rule that quietly does nothing:
// fileinto into a missing Box files nowhere and the mail lands in the Inbox
// looking as though the decision was never made.
func TestAMissingBoxIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	d, sieve := seedScreener(t)
	d.Mirrored = []string{"INBOX", routing.BoxScreener}
	before := sieve.scripts[routing.ScriptName]

	resp := ask(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})
	if resp.OK {
		t.Fatal("routing into a box that is not there was accepted")
	}
	if !strings.Contains(resp.Error, routing.BoxFeed) {
		t.Errorf("the error does not name the box: %s", resp.Error)
	}
	if sieve.scripts[routing.ScriptName] != before || sieve.puts != 0 {
		t.Error("the script was written anyway")
	}
	// The mail is still in the Screener, still owed a decision.
	rows, _ := d.Mirror.Rows("primary", routing.BoxScreener, 50)
	if len(rows) != 4 {
		t.Errorf("%d mails left in the screener, want all 4", len(rows))
	}
}

// Gate 6. Activating a script deactivates the one that was running, and that
// one is somebody's webmail rules. A decision that would do that is refused and
// says how to make the Routing reachable instead.
func TestAnActiveScriptThatIgnoresOursIsRefused(t *testing.T) {
	d, sieve := seedScreener(t)
	sieve.scripts["Open-Xchange"] = "# their rules\nfileinto \"Archive/gmx\";\n"
	sieve.active = "Open-Xchange"

	resp := ask(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})
	if resp.OK {
		t.Fatal("the decision went ahead over somebody else's script")
	}
	if !strings.Contains(resp.Error, "Open-Xchange") || !strings.Contains(resp.Error, "include") {
		t.Errorf("the error does not name it or say how to fix it: %s", resp.Error)
	}
	if sieve.active != "Open-Xchange" || sieve.puts != 0 {
		t.Errorf("active = %q after %d puts", sieve.active, sieve.puts)
	}
}

// The same script with `include "logic";` at the end is the real account's
// arrangement: the webmail filter editor owns the active script and ours runs
// from inside it. The Routing is in force, and it is written without touching
// which script is active.
func TestAnActiveScriptThatIncludesOursIsLeftAlone(t *testing.T) {
	d, sieve := seedScreener(t)
	sieve.scripts["Open-Xchange"] = "# their rules\nfileinto \"Archive/gmx\";\n\ninclude \"logic\";\n"
	sieve.active = "Open-Xchange"

	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})
	if sieve.active != "Open-Xchange" {
		t.Fatalf("the active script was changed to %q", sieve.active)
	}
	if routing.Parse(sieve.scripts[routing.ScriptName]).Of("news@example.com") != routing.Feed {
		t.Errorf("the routing script was not written")
	}
	// And the Mirror says the Routing is in force, because it is: the active
	// script reaches it.
	resp := mustAsk(t, d, []string{"route"}, nil)
	if view := resp.Data.(routingView); !view.Active {
		t.Error("a routing reached by an include reads as not in force")
	}
}

// A script that is stored and unreachable routes nothing, and a caller has to
// be able to see that without the server being reachable.
func TestARoutingNothingReachesReadsAsNotInForce(t *testing.T) {
	d, sieve := seedScreener(t)
	sieve.scripts["Open-Xchange"] = "# their rules\n"
	sieve.active = "Open-Xchange"
	if err := d.refreshRouting(context.Background()); err != nil {
		t.Fatal(err)
	}
	resp := mustAsk(t, d, []string{"route"}, nil)
	if view := resp.Data.(routingView); view.Active {
		t.Error("an unreachable routing reads as in force")
	}
}

// Gate 7. Reading the Routing never touches the server: it is the Mirror's copy
// of the script, and it answers with the network down like every other read
// (ADR-0001).
func TestReadingTheRoutingNeverAsksTheServer(t *testing.T) {
	d, sieve := seedScreener(t)
	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})

	sieve.fail = context.DeadlineExceeded
	resp := mustAsk(t, d, []string{"route"}, map[string]any{"script": true})
	view, ok := resp.Data.(routingView)
	if !ok {
		t.Fatalf("route returned %T", resp.Data)
	}
	if len(view.Routes) != 1 || view.Routes[0].Address != "news@example.com" {
		t.Errorf("routes = %+v", view.Routes)
	}
	if !view.Active {
		t.Error("the script is the active one and the read says otherwise")
	}
	// The script itself is held too, so "what does the routing actually say" is
	// answerable without the server.
	if !strings.Contains(view.Script, "news@example.com") {
		t.Errorf("the mirror does not hold the script:\n%s", view.Script)
	}
	// The screener listing is a Mirror read as well.
	mustAsk(t, d, []string{"screener"}, nil)
}

// Gate 8. Aside is not a Destination. Routing a sender there would mean "always
// read this later", which is a Feed; putting one mail there is what Aside is.
func TestAsideIsAPileAndNotARoute(t *testing.T) {
	d, sieve := seedScreener(t)
	resp := ask(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "aside"})
	if resp.OK {
		t.Fatal("aside was accepted as a routing destination")
	}
	if !strings.Contains(resp.Error, "mailbox aside") {
		t.Errorf("the error does not say what to do instead: %s", resp.Error)
	}
	if sieve.puts != 0 {
		t.Error("the script was written anyway")
	}

	// The verb that does exist moves one mail there and back.
	resp = mustAsk(t, d, []string{"aside"}, map[string]any{"positional": []any{routing.BoxScreener + ":10"}})
	moved := resp.Data.([]change)
	if len(moved) != 1 || !strings.HasPrefix(moved[0].NewID, "Aside:") {
		t.Fatalf("aside gave %+v", moved)
	}
	back := mustAsk(t, d, []string{"aside", "done"}, map[string]any{"positional": []any{moved[0].NewID}})
	returned := back.Data.([]change)
	if len(returned) != 1 || strings.Contains(returned[0].NewID, "/") {
		t.Fatalf("aside done gave %+v, want it back in the inbox", returned)
	}
}

// Reply Later is the second hand-tended pile: not a Destination, and moved one
// mail at a time the same way Aside is.
func TestReplyLaterIsAPileAndNotARoute(t *testing.T) {
	d, sieve := seedScreener(t)
	resp := ask(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "reply later"})
	if resp.OK {
		t.Fatal("reply later was accepted as a routing destination")
	}
	if !strings.Contains(resp.Error, "mailbox reply-later") {
		t.Errorf("the error does not say what to do instead: %s", resp.Error)
	}
	if sieve.puts != 0 {
		t.Error("the script was written anyway")
	}

	// The verb that does exist moves one mail there and back.
	resp = mustAsk(t, d, []string{"reply-later"}, map[string]any{"positional": []any{routing.BoxScreener + ":10"}})
	moved := resp.Data.([]change)
	if len(moved) != 1 || !strings.HasPrefix(moved[0].NewID, "Reply Later:") {
		t.Fatalf("reply-later gave %+v", moved)
	}
	back := mustAsk(t, d, []string{"reply-later", "done"}, map[string]any{"positional": []any{moved[0].NewID}})
	returned := back.Data.([]change)
	if len(returned) != 1 || strings.Contains(returned[0].NewID, "/") {
		t.Fatalf("reply-later done gave %+v, want it back in the inbox", returned)
	}
}

// The account's own script is read as it stands, not overwritten on sight: the
// decisions in it are the ones somebody already made.
func TestTheScriptOnTheAccountIsAdoptedNotReplaced(t *testing.T) {
	d, sieve := seedScreener(t)
	sieve.scripts[routing.ScriptName] = `require ["fileinto"];
if header :contains "From" ["anna@example.com"]
{
  fileinto "INBOX";
  stop;
}
fileinto "INBOX/Screener";`
	if err := d.refreshRouting(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes, _ := d.Mirror.Routing("primary")
	if len(routes) != 1 || routes[0].Address != "anna@example.com" || routes[0].To != "inbox" {
		t.Fatalf("the mirror read %+v out of the old script", routes)
	}
	// A new decision keeps the old one and converts the script on the way.
	mustAsk(t, d, []string{"route"}, map[string]any{"positional": []any{"news@example.com"}, "to": "feed"})
	l := routing.Parse(sieve.scripts[routing.ScriptName])
	if l.Of("anna@example.com") != routing.Inbox || l.Of("news@example.com") != routing.Feed {
		t.Errorf("after the first write the script says %v", l.All())
	}
}
