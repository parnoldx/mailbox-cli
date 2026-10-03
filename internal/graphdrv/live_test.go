//go:build live

// The live half of the Graph driver, against the Microsoft 365 account in the
// config. It needs the account signed in first (`mailbox setup`):
//
//	MAILBOX_GRAPH_ACCOUNT=work go test -tags live ./internal/graphdrv/ -v
//
// It reads the Inbox and the calendars, and writes only to what it makes: a
// folder called mailbox-selftest, which it deletes, and one event tomorrow,
// which it deletes. MAILBOX_GRAPH_SEND=1 also sends one mail to the account
// itself.
package graphdrv

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/mirror"
	"mailbox/internal/routing"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
)

const scratchFolder = "mailbox-selftest"

func liveAccount(t *testing.T) (*Client, *Store, config.Account, string) {
	t.Helper()
	name := os.Getenv("MAILBOX_GRAPH_ACCOUNT")
	if name == "" {
		t.Skip("MAILBOX_GRAPH_ACCOUNT names the Microsoft 365 account to test against")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a, ok := cfg.Secondary[name]
	if !ok || !a.Graph() {
		t.Fatalf("accounts.%s is not a Microsoft 365 account", name)
	}
	tokens, err := config.GraphTokenPath(name)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return NewClient(NewAuth(a.Tenant, a.ClientID, tokens)), s, a, name
}

func TestLiveMail(t *testing.T) {
	c, s, a, _ := liveAccount(t)
	ctx := context.Background()
	me, err := c.Me(ctx)
	if err != nil || !strings.EqualFold(me, a.Email) {
		t.Fatalf("signed in as %q (%v), configured %s", me, err, a.Email)
	}
	mail := NewMail(c, s)
	folders, err := mail.Folders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate: folders %v", folders)
	for _, want := range []string{"INBOX", "Sent", "Drafts"} {
		if !slices.Contains(folders, want) {
			t.Errorf("no %s", want)
		}
	}
	st, err := mail.Status(ctx, []string{"INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate: first delta of INBOX: %+v", st[0])
	again, err := mail.Status(ctx, []string{"INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].UIDValidity != st[0].UIDValidity || again[0].NumMessages < st[0].NumMessages-1 {
		t.Errorf("the stored delta link did not carry on: %+v then %+v", st[0], again[0])
	}
	uids, _ := mail.AllUIDs(ctx, "INBOX")
	if len(uids) == 0 {
		t.Skip("an empty Inbox has nothing to read")
	}
	last := uids[max(0, len(uids)-3):]
	envs, err := mail.FetchEnvelopes(ctx, "INBOX", last)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		t.Logf("gate: %d %q from %s, %d bytes, refs %d, flags %v", e.UID, e.Subject, e.From, e.Size, len(e.References), e.Flags)
		if e.MessageID == "" || e.Size == 0 || e.InternalDate.IsZero() {
			t.Errorf("envelope %+v", e)
		}
	}
	bodies, err := mail.FetchBodies(ctx, "INBOX", last[len(last)-1:])
	if err != nil || len(bodies) != 1 || bodies[0].Plain+bodies[0].HTML == "" {
		t.Fatalf("body %+v, %v", bodies, err)
	}
}

func TestLiveScratchFolderWrites(t *testing.T) {
	c, s, a, _ := liveAccount(t)
	ctx := context.Background()
	mail := NewMail(c, s)
	if err := mail.CreateFolder(ctx, scratchFolder); err != nil {
		t.Fatal(err)
	}
	f, _, _ := s.folder(scratchFolder)
	t.Cleanup(func() {
		_ = c.do(context.Background(), request{method: http.MethodDelete, path: "/me/mailFolders/" + url.PathEscape(f.GraphID)}, nil)
	})
	if err := mail.CreateFolder(ctx, scratchFolder+"/inner"); err != nil {
		t.Fatal(err)
	}
	if _, err := mail.Status(ctx, []string{scratchFolder, scratchFolder + "/inner"}); err != nil {
		t.Fatal(err)
	}
	raw := "From: " + a.Email + "\r\nTo: " + a.Email + "\r\nSubject: mailbox selftest\r\nMessage-ID: <selftest-" +
		time.Now().Format("20060102150405") + "@example.com>\r\n\r\nnothing to see\r\n"
	uid, err := mail.Append(ctx, scratchFolder, []string{`\Seen`}, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, err := mail.StoreFlags(ctx, scratchFolder, []uint32{uid}, []string{`\Flagged`, "$bubbled", "bubble-20260927T0800"}, nil)
	if err != nil || len(got) != 1 || !slices.Contains(got[0].Flags, "$bubbled") || !slices.Contains(got[0].Flags, `\Flagged`) {
		t.Fatalf("keywords as categories: %+v, %v", got, err)
	}
	t.Logf("gate: flags after store %v", got[0].Flags)
	moved, err := mail.Move(ctx, scratchFolder, []uint32{uid}, scratchFolder+"/inner")
	if err != nil || moved[uid] == 0 {
		t.Fatalf("move %v, %v", moved, err)
	}
	st, err := mail.Status(ctx, []string{scratchFolder, scratchFolder + "/inner"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate: after the move %+v", st)
	inner, _ := mail.AllUIDs(ctx, scratchFolder+"/inner")
	if !slices.Equal(inner, []uint32{moved[uid]}) {
		t.Errorf("the moved message is %v in inner, want [%d]: the immutable id did not hold", inner, moved[uid])
	}
}

// TestLiveRoutingRules writes a Routing as inbox rules, reads it back and
// puts whatever ours was before back in place. The Routing Boxes are made for
// the run and deleted after — the same contract as the scratch folder: the
// account is left with nothing the test made. Rules without the mailbox:
// prefix are never touched (ADR-0032).
func TestLiveRoutingRules(t *testing.T) {
	c, s, _, _ := liveAccount(t)
	ctx := context.Background()
	mail := NewMail(c, s)
	before, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Rules back first, boxes last — LIFO cleanup puts the record back before
	// the folders a rule still names go away.
	boxes := []string{
		routing.BoxScreener, routing.BoxFeed, routing.BoxPaperTrail, routing.BoxBlock,
	}
	for _, name := range boxes {
		if err := mail.CreateFolder(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, name := range boxes {
			if f, ok, _ := s.folder(name); ok {
				_ = c.do(context.Background(), request{
					method: http.MethodDelete, path: "/me/mailFolders/" + url.PathEscape(f.GraphID),
				}, nil)
			}
		}
	})
	t.Cleanup(func() { _ = mail.SetRules(ctx, before) })

	l := routing.New()
	if _, err := l.Set("mailbox-selftest@example.com", routing.Feed); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Set("@selftest.example", routing.PaperTrail); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Set("me-selftest@example.com", routing.Inbox); err != nil {
		t.Fatal(err)
	}
	if err := mail.SetRules(ctx, l.Rules()); err != nil {
		t.Fatalf("SetRules: %v — the rules engine refused a rule this program writes", err)
	}
	got, err := mail.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	back := routing.ListsFromRules(got)
	if back.Of("mailbox-selftest@example.com") != routing.Feed ||
		back.Of("anything@selftest.example") != routing.PaperTrail {
		t.Errorf("rules read back as %+v, decisions do not match", got)
	}
	if !routing.HasCatchAll(got) {
		t.Errorf("the catch-all did not survive the write: %+v", got)
	}
	t.Logf("gate: %d rules in force, catch-all present", len(got))
}

func TestLiveSendToSelf(t *testing.T) {
	if os.Getenv("MAILBOX_GRAPH_SEND") != "1" {
		t.Skip("MAILBOX_GRAPH_SEND=1 sends one mail to the account itself")
	}
	c, s, a, _ := liveAccount(t)
	ctx := context.Background()
	mail := NewMail(c, s)
	if _, err := mail.Folders(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := mail.Status(ctx, []string{"Sent"})
	raw := "From: " + a.Email + "\r\nTo: " + a.Email + "\r\nSubject: mailbox selftest send\r\n\r\nsent by the live test\r\n"
	if err := mail.Send(ctx, a.Email, []string{a.Email}, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		time.Sleep(5 * time.Second)
		after, err := mail.Status(ctx, []string{"Sent"})
		if err == nil && after[0].NumMessages > before[0].NumMessages {
			t.Logf("gate: the copy is in Sent Items after %ds, filed by Graph", (i+1)*5)
			return
		}
	}
	t.Fatal("no copy in Sent Items after a minute")
}

func TestLiveCalendarRoundTrip(t *testing.T) {
	c, s, _, name := liveAccount(t)
	ctx := context.Background()
	d := NewDAV(c, s, name)
	cols, err := d.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cal davsync.Collection
	for _, col := range cols {
		t.Logf("gate: %s %s %s", col.Kind, col.Name, col.Color)
		if col.Kind == "events" && col.Name == name {
			cal = col
		}
	}
	if cal.URL == "" {
		t.Fatal("no default calendar")
	}
	ch, err := d.Sync(ctx, cal.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate: first sync of %s: %d objects", cal.Name, len(ch.Items))

	uid := vcal.NewUID()
	tomorrow := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	raw, err := vcal.NewEvent(uid, vcal.EventEdit{Summary: "mailbox selftest", Start: tomorrow,
		Repeat: "FREQ=DAILY;COUNT=3", Alarms: []int{10}})
	if err != nil {
		t.Fatal(err)
	}
	href := davsync.Href(mirror.Collection{URL: cal.URL}, uid)
	abs := strings.TrimSuffix(c.Base, d.basePath()) + href
	if _, err := d.Put(ctx, abs, raw, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Delete(context.Background(), abs, "") })
	id, _ := s.objectID(href)

	// Something we do not model, set the way Outlook would.
	if err := c.do(ctx, request{method: http.MethodPatch, path: "/me/events/" + url.PathEscape(id),
		body: jsonBody(map[string]any{"categories": []string{"Blue category"}})}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := d.MultiGet(ctx, cal.URL, []string{href})
	if err != nil || len(got) != 1 {
		t.Fatalf("read back %v, %v", got, err)
	}
	edited, err := vcal.SetEvent(got[0].Data, vcal.EventEdit{Summary: "mailbox selftest, edited"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, abs, edited, got[0].ETag); err != nil {
		t.Fatal(err)
	}
	var now graphEvent
	var cats struct {
		Categories []string `json:"categories"`
	}
	_ = c.do(ctx, request{method: http.MethodGet, path: "/me/events/" + url.PathEscape(id) + "?" + eventSelect, prefer: []string{utc}}, &now)
	_ = c.do(ctx, request{method: http.MethodGet, path: "/me/events/" + url.PathEscape(id) + "?$select=categories"}, &cats)
	if now.Subject != "mailbox selftest, edited" || !slices.Contains(cats.Categories, "Blue category") || now.Recurrence == nil {
		t.Fatalf("after the edit: %q, categories %v, recurrence %v", now.Subject, cats.Categories, now.Recurrence)
	}

	next, err := d.Sync(ctx, cal.URL, ch.Token)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range next.Items {
		if it.Href == href {
			p, _ := vcal.Parse(it.Data, time.Local)
			t.Logf("gate: came back as %q, uid %s, rule %s", p.Summary, p.UID, p.Repeat)
			if p.UID != uid || !p.Recurring {
				t.Errorf("came back as %+v", p)
			}
			return
		}
	}
	t.Errorf("the event did not come back under %s in %d changes", href, len(next.Items))
}

// A Teams meeting minted on the live account comes back with a join link. No
// event is made and nobody is invited; the meeting is taken down again, so the
// account is left as it was found.
func TestLiveMeetMakesALink(t *testing.T) {
	c, _, _, _ := liveAccount(t)
	ctx := context.Background()
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	link, err := c.Meet(ctx, "mailbox meet selftest", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gate: join link %s", link)
	if !strings.HasPrefix(link, "https://") {
		t.Errorf("join link = %q", link)
	}
	// Graph hands the id back beside the link, so the meeting is ours to take
	// down again. Best effort: a leftover meeting costs nothing but clutter.
	var list struct {
		Value []struct {
			ID         string `json:"id"`
			JoinWebURL string `json:"joinWebUrl"`
		} `json:"value"`
	}
	if err := c.do(ctx, request{method: http.MethodGet, path: "/me/onlineMeetings"}, &list); err != nil {
		return
	}
	for _, m := range list.Value {
		if m.JoinWebURL == link {
			_ = c.do(ctx, request{method: http.MethodDelete,
				path: "/me/onlineMeetings/" + url.PathEscape(m.ID)}, nil)
		}
	}
}

// A Teams meeting made here comes back with the join link Graph minted for
// it. No attendees: this must not send invitations from a test account.
func TestLiveTeamsEventGetsAJoinLink(t *testing.T) {
	c, s, _, name := liveAccount(t)
	ctx := context.Background()
	d := NewDAV(c, s, name)
	cols, err := d.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cal davsync.Collection
	for _, col := range cols {
		if col.Kind == "events" && col.Name == name {
			cal = col
		}
	}
	if cal.URL == "" {
		t.Fatal("no default calendar")
	}
	uid := vcal.NewUID()
	tomorrow := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	raw, err := vcal.NewEvent(uid, vcal.EventEdit{Summary: "mailbox teams selftest", Start: tomorrow, Teams: true})
	if err != nil {
		t.Fatal(err)
	}
	href := davsync.Href(mirror.Collection{URL: cal.URL}, uid)
	abs := strings.TrimSuffix(c.Base, d.basePath()) + href
	if _, err := d.Put(ctx, abs, raw, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Delete(context.Background(), abs, "") })

	got, err := d.MultiGet(ctx, cal.URL, []string{href})
	if err != nil || len(got) != 1 {
		t.Fatalf("read back %v, %v", got, err)
	}
	p, err := vcal.Parse(got[0].Data, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Teams {
		t.Errorf("the event did not come back a Teams meeting: %+v", p)
	}
	// The join link is what an agenda line wants off the meeting; without it
	// the marker bought nothing.
	if p.URL == "" {
		t.Errorf("no join link came back: %+v", p)
	}
}
