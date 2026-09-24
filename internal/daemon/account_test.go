package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/bubble"
	compose "mailbox/internal/message"
	"mailbox/internal/mirror"
	"mailbox/internal/outbox"
	"mailbox/internal/sync/mailsync"
)

// twoAccounts builds a Daemon with the Primary Account of seed() and a
// Secondary called "gmx", each with its own scripted server.
func twoAccounts(t *testing.T) (*Daemon, *mailsync.Fake, *stubTransport) {
	t.Helper()
	d, primaryTransport := seedSend(t)
	_ = primaryTransport

	second := mailsync.NewFake("INBOX")
	second.AddFolder("Sent")
	second.AddFolder("INBOX/Aside")
	second.AddFolder("INBOX/Reply Later")
	// One newer and one older than the Primary's Inbox mail (2026-08-29 09:00),
	// so a merged listing has to interleave them.
	one := second.Deliver("INBOX", "gmx-one@x", "Rechnung von GMX", "zahlen bitte")
	one.From, one.Date = "buchhaltung@gmx.de", time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	second.Deliver("INBOX", "gmx-two@x", "Newsletter", "nicht wichtig").Date = time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

	r := &mailsync.Reconciler{Account: "gmx", Mirror: d.Mirror, Driver: second}
	mirrored := []string{"INBOX", "Sent", "INBOX/Aside", "INBOX/Reply Later"}
	acct := NewAccount("gmx", r,
		&mailsync.Writer{Account: "gmx", Mirror: d.Mirror, Driver: second, Mirrored: mirrored},
		mirrored, []string{"INBOX"})
	acct.From = compose.Address{Name: "Peter", Addr: "peter@gmx.de"}
	acct.Color = "orange"
	transport := &stubTransport{}
	acct.Courier = &outbox.Courier{
		Box: d.Outbox, Account: "gmx", Transport: transport, Filer: second, SentBox: "Sent",
	}
	d.Others = append(d.Others, acct)

	// Fill the Mirror for the second account, the way its first cycle would.
	if _, err := r.SyncAll(context.Background(), mirrored); err != nil {
		t.Fatal(err)
	}
	return d, second, transport
}

func TestAnUnqualifiedIdStillMeansThePrimary(t *testing.T) {
	d, _, _ := twoAccounts(t)
	// Every id that worked when there was one account works verbatim
	// (ADR-0005).
	got := view(t, d, "7")
	if got["subject"] != "Rechnung" {
		t.Fatalf("message 7 = %v", got)
	}
	if got["id"] != "7" {
		t.Fatalf("the primary's ids are never prefixed, got %q", got["id"])
	}
}

func TestASecondaryAccountIsReadUnderItsOwnName(t *testing.T) {
	d, _, _ := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"box", "view"},
		Args: map[string]any{"positional": "gmx/INBOX"}})
	if !resp.OK {
		t.Fatalf("box view: %s (%s)", resp.Error, resp.Code)
	}
	rows := resp.Data.([]row)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "gmx/") {
			t.Fatalf("id = %q, want it qualified", r.ID)
		}
	}
	// And the id it printed reads the Message.
	got := view(t, d, rows[0].ID)
	if got["subject"] == "" || !strings.HasPrefix(got["id"].(string), "gmx/") {
		t.Fatalf("message = %v", got)
	}

	// A bare uid of a secondary means the primary, and there is no such
	// message there.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"},
		Args: map[string]any{"positional": "1"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
	// An account nobody has heard of says so.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"},
		Args: map[string]any{"positional": "web/INBOX:1"}})
	if resp.OK || !strings.Contains(resp.Error, "web/INBOX") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestABoxNameWithASlashIsNotAnAccount(t *testing.T) {
	d, _, _ := twoAccounts(t)
	// INBOX/Screener is a Box on the primary, not the Screener account.
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"},
		Args: map[string]any{"positional": "INBOX/Screener:42"}})
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	if got := resp.Data.(message); got.Box != "INBOX/Screener" || got.ID != "Screener:42" {
		t.Fatalf("message = %+v", got)
	}
}

func TestSearchCoversEveryAccountAndSaysWhichIsWhich(t *testing.T) {
	d, _, _ := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"search"},
		Args: map[string]any{"positional": "rechnung"}})
	if !resp.OK {
		t.Fatalf("search: %s (%s)", resp.Error, resp.Code)
	}
	hits := resp.Data.([]hit)
	var qualified, plain int
	for _, h := range hits {
		if strings.HasPrefix(h.ID, "gmx/") {
			qualified++
		} else {
			plain++
		}
	}
	if qualified != 1 || plain == 0 {
		t.Fatalf("hits = %+v", hits)
	}
	// Newest first across accounts, because two accounts' relevance scores are
	// not comparable.
	for i := 1; i < len(hits); i++ {
		if hits[i-1].Date < hits[i].Date {
			t.Fatalf("out of order: %+v", hits)
		}
	}

	// One account can be asked for on its own.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"search"},
		Args: map[string]any{"positional": "rechnung", "in": "gmx"}})
	hits = resp.Data.([]hit)
	if len(hits) != 1 || !strings.HasPrefix(hits[0].ID, "gmx/") {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestNothingMatchedIsAnEmptyListNotNothing(t *testing.T) {
	d, _, _ := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"search"},
		Args: map[string]any{"positional": "kaertcher-gibt-es-nicht"}})
	if !resp.OK {
		t.Fatalf("search: %s", resp.Error)
	}
	hits, ok := resp.Data.([]hit)
	if !ok || hits == nil {
		t.Fatalf("data = %#v — a caller reading this over the socket gets null and has to guess", resp.Data)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestAWriteGoesToTheAccountTheIdNames(t *testing.T) {
	d, second, _ := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"seen"},
		Args: map[string]any{"positional": []string{"gmx/1"}}})
	if !resp.OK {
		t.Fatalf("seen: %s (%s)", resp.Error, resp.Code)
	}
	changes := resp.Data.([]change)
	if len(changes) != 1 || changes[0].ID != "gmx/1" || !changes[0].Seen {
		t.Fatalf("changes = %+v", changes)
	}
	// The primary's server was not touched.
	if fakeOf(d).CallCount("StoreFlags") != 0 {
		t.Fatal("the write went to the wrong account's server")
	}
	if second.CallCount("StoreFlags") != 1 {
		t.Fatalf("the second account saw %d stores", second.CallCount("StoreFlags"))
	}

	// Ids from two accounts in one command is a mistake, not two commands.
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"seen"},
		Args: map[string]any{"positional": []string{"7", "gmx/1"}}})
	if resp.OK || !strings.Contains(resp.Error, "different accounts") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSendUsesTheAccountItWasAskedFor(t *testing.T) {
	d, _, gmx := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"send"},
		Args: map[string]any{
			"account": "gmx", "to": []string{"kaethe@example.com"},
			"subject": "von gmx", "body": "hallo",
		}})
	if !resp.OK {
		t.Fatalf("send: %s (%s)", resp.Error, resp.Code)
	}
	out := resp.Data.(sent)
	if !strings.HasPrefix(out.ID, "gmx/") {
		t.Fatalf("the copy was filed as %q", out.ID)
	}
	if gmx.count() != 1 {
		t.Fatalf("the second account's smtp server saw %d mails", gmx.count())
	}
	// It went out as that account, not as the primary.
	if !strings.Contains(string(gmx.sent[0]), "peter@gmx.de") {
		t.Fatalf("sent as:\n%s", gmx.sent[0])
	}

	// And the outbox says whose it was.
	list := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"outbox", "list"}})
	rows := list.Data.([]outboxRow)
	if len(rows) != 1 || rows[0].Account != "gmx" {
		t.Fatalf("outbox = %+v", rows)
	}
}

func TestAReplyIsSentByTheAccountThatReceivedIt(t *testing.T) {
	d, second, gmx := twoAccounts(t)
	// Find the gmx message to answer.
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"box", "view"},
		Args: map[string]any{"positional": "gmx/INBOX"}})
	rows := resp.Data.([]row)
	var id string
	for _, r := range rows {
		if strings.Contains(r.Subject, "Rechnung") {
			id = r.ID
		}
	}
	if id == "" {
		t.Fatalf("rows = %+v", rows)
	}

	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"reply"},
		Args: map[string]any{"positional": id, "body": "ist bezahlt"}})
	if !resp.OK {
		t.Fatalf("reply: %s (%s)", resp.Error, resp.Code)
	}
	if gmx.count() != 1 {
		t.Fatalf("the reply went out on %d of the gmx account's connections", gmx.count())
	}
	if !strings.Contains(string(gmx.sent[0]), "peter@gmx.de") {
		t.Fatalf("the reply was sent as somebody else:\n%s", gmx.sent[0])
	}
	// Filed in that account's Sent box, and mirrored under its name.
	out := resp.Data.(sent)
	if !strings.HasPrefix(out.ID, "gmx/Sent:") {
		t.Fatalf("copy = %q", out.ID)
	}
	_ = second
}

func TestStatusReportsEveryAccount(t *testing.T) {
	d, _, _ := twoAccounts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"status"}})
	if !resp.OK {
		t.Fatalf("status: %s", resp.Error)
	}
	rows := resp.Data.([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("status = %+v", rows)
	}
	if rows[0]["account"] != "primary" || rows[0]["primary"] != true {
		t.Fatalf("the primary is first: %+v", rows[0])
	}
	if rows[1]["account"] != "gmx" || rows[1]["primary"] != false {
		t.Fatalf("status = %+v", rows[1])
	}
}

// The Mirror is one file with an account column, not one file per account.
func TestOneMirrorHoldsBothAccounts(t *testing.T) {
	d, _, _ := twoAccounts(t)
	m, err := mirror.Open(filepath.Join(t.TempDir(), "unused.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	primary, err := d.Mirror.Rows("primary", "INBOX", 50)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := d.Mirror.Rows("gmx", "INBOX", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(primary) == 0 || len(secondary) != 2 {
		t.Fatalf("primary=%d secondary=%d", len(primary), len(secondary))
	}
	// The same uid in two accounts is two different Messages.
	if primary[0].UID == secondary[0].UID && primary[0].Message.Key == secondary[0].Message.Key {
		t.Fatal("the accounts are sharing rows")
	}
	_ = time.Now
}

// boxRows runs box view and returns its rows.
func boxRows(t *testing.T, d *Daemon, args map[string]any) []row {
	t.Helper()
	return mustAsk(t, d, []string{"box", "view"}, args).Data.([]row)
}

func idsOf(rows []row) string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID + "@" + r.Account
	}
	return strings.Join(ids, " ")
}

// A listing spans accounts, an id names one: the Inbox is both accounts',
// newest first, each row carrying its own account.
func TestTheInboxListsEveryAccountNewestFirst(t *testing.T) {
	d, _, _ := twoAccounts(t)
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "inbox"})); got != "gmx/1@gmx 7@ gmx/2@gmx" {
		t.Fatalf("merged inbox = %s", got)
	}
	// The limit is on the merged listing.
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "inbox", "limit": 2.0})); got != "gmx/1@gmx 7@" {
		t.Fatalf("limit 2 = %s", got)
	}
	// Naming an account narrows, the Primary included.
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "gmx"})); got != "gmx/1@gmx gmx/2@gmx" {
		t.Fatalf("gmx = %s", got)
	}
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "primary"})); got != "7@" {
		t.Fatalf("primary = %s", got)
	}
	// A Box only one account has is that account's, as it always was.
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "Screener"})); got != "Screener:42@" {
		t.Fatalf("screener = %s", got)
	}
}

// The piles work on a Secondary: bubble sets the keyword on its server, the
// return brings the thread back there, and the Primary is untouched.
func TestASecondaryHasThePiles(t *testing.T) {
	d, second, _ := twoAccounts(t)
	ctx := context.Background()
	gmx := d.Others[0]

	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1).Format("2006-01-02")
	mustAsk(t, d, []string{"bubble"}, map[string]any{"positional": "gmx/1", "on": yesterday})
	if msgs := second.Folder("INBOX/Aside").Msgs; len(msgs) != 1 || bubble.KeywordOf(msgs[0].Flags) == "" {
		t.Fatalf("gmx Aside = %+v, want the thread with its keyword", msgs)
	}
	if got := idsOf(boxRows(t, d, map[string]any{"positional": "primary"})); got != "7@" {
		t.Fatalf("the primary moved: %s", got)
	}

	d.returnDue(ctx, gmx)
	if len(second.Folder("INBOX/Aside").Msgs) != 0 {
		t.Fatal("the bubbled thread did not come back to the gmx Inbox")
	}

	mustAsk(t, d, []string{"aside"}, map[string]any{"positional": "gmx/2"})
	if len(second.Folder("INBOX/Aside").Msgs) != 1 {
		t.Fatal("aside did not move on the gmx server")
	}
}

// A server that takes the STORE and keeps no keyword would make a bubble that
// never returns. It is refused, and nothing is moved.
func TestABubbleTheServerWillNotKeepIsRefused(t *testing.T) {
	d, second, _ := twoAccounts(t)
	second.NoKeywords = true
	resp := ask(t, d, []string{"bubble"}, map[string]any{"positional": "gmx/1", "tomorrow": true})
	if resp.OK || !strings.Contains(resp.Error, "keywords") {
		t.Fatalf("resp = %+v, want a refusal naming keywords", resp)
	}
	if len(second.Folder("INBOX/Aside").Msgs) != 0 {
		t.Fatal("the thread was set aside with no way back")
	}
}

func TestAccountListNamesEveryAccountAndItsColour(t *testing.T) {
	d, _, _ := twoAccounts(t)
	d.Color = "accent"
	got := mustAsk(t, d, []string{"account", "list"}, nil).Data.([]accountRow)
	want := []accountRow{
		{Name: "primary", Email: "me@example.com", Primary: true, Color: "accent"},
		{Name: "gmx", Email: "peter@gmx.de", Color: "orange"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("accounts = %+v", got)
	}
}
