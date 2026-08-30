package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/mirror"
)

// seedDraft puts one unsent mail in Drafts, both on the fake server and in the
// Mirror, the way a draft written in webmail arrives.
func seedDraft(t *testing.T) (*Daemon, *stubTransport) {
	t.Helper()
	d, tr := seedSend(t)
	d.Mirrored = append(d.Mirrored, "Drafts")
	d.Writer.Mirrored = d.Mirrored
	fakeOf(d).AddFolder("Drafts")
	fakeOf(d).Deliver("Drafts", "draft@example.com", "Rechnung September", "halb fertig\n")

	tx, err := d.Mirror.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, _, err := tx.UpsertMessage(mirror.Message{
		Key: "draft@example.com", Subject: "Rechnung September",
		From: "me@example.com", To: "billing@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(id, "halb fertig\n", "", "halb fertig"); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{
		Folder: "Drafts", UID: 1, MessageID: id, Flags: []string{`\Draft`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return d, tr
}

func TestDraftListAndShowReadTheDraftsBox(t *testing.T) {
	d, _ := seedDraft(t)
	rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message)
	if len(rows) != 1 || rows[0].Subject != "Rechnung September" {
		t.Fatalf("draft list = %+v", rows)
	}
	// The id printed is the qualified one, so it is unambiguous when copied.
	if rows[0].ID != "Drafts:1" {
		t.Errorf("id = %q", rows[0].ID)
	}

	// And both forms of it read the same draft, because the command has already
	// said which box this is about.
	for _, id := range []any{"Drafts:1", "1", uint32(1)} {
		got := mustAsk(t, d, []string{"draft", "show"}, map[string]any{"positional": id})
		if m := got.Data.(message); m.Subject != "Rechnung September" {
			t.Errorf("draft show %v = %+v", id, m)
		}
	}
}

// There is no in-place edit on imap. The new version is written first and the
// old one trashed second, so a crash between them leaves two drafts — which is
// recoverable — rather than none.
func TestDraftEditWritesANewVersionAndTrashesTheOld(t *testing.T) {
	d, _ := seedDraft(t)
	resp := mustAsk(t, d, []string{"draft", "edit"}, map[string]any{
		"positional": "1", "subject": "Rechnung Oktober",
	})
	got := resp.Data.(map[string]any)
	if got["state"] != "saved" || got["subject"] != "Rechnung Oktober" {
		t.Fatalf("edit gave %+v", got)
	}
	// The old uid is gone from the mirror and exactly one draft is left.
	rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message)
	if len(rows) != 1 {
		t.Fatalf("%d drafts after an edit: %+v", len(rows), rows)
	}
	if rows[0].Subject != "Rechnung Oktober" {
		t.Errorf("the edit did not land: %+v", rows[0])
	}
	// What was not named kept what the draft said.
	if !strings.Contains(rows[0].To, "billing@example.com") {
		t.Errorf("the edit lost the recipient: %q", rows[0].To)
	}
}

func TestDraftEditKeepsTheBodyItWasNotGiven(t *testing.T) {
	d, _ := seedDraft(t)
	mustAsk(t, d, []string{"draft", "edit"},
		map[string]any{"positional": "1", "to": []any{"anna@example.com"}})
	rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message)
	if len(rows) != 1 || !strings.Contains(rows[0].To, "anna@example.com") {
		t.Fatalf("drafts = %+v", rows)
	}
	body := mustAsk(t, d, []string{"draft", "show"},
		map[string]any{"positional": rows[0].ID}).Data.(message).Body
	if !strings.Contains(body, "halb fertig") {
		t.Errorf("the body was lost: %q", body)
	}
}

// Sending goes through the outbox like any other send, and the draft goes only
// once the mail is out.
func TestDraftSendDeliversAndThenClearsTheDraft(t *testing.T) {
	d, tr := seedDraft(t)
	resp := mustAsk(t, d, []string{"draft", "send"}, map[string]any{"positional": "1"})
	out := resp.Data.(sent)
	if out.State != "filed" && out.State != "sent" {
		t.Fatalf("draft send gave %+v", out)
	}
	if len(tr.sent) != 1 {
		t.Fatalf("%d mails went out", len(tr.sent))
	}
	if rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message); len(rows) != 0 {
		t.Errorf("the draft is still there: %+v", rows)
	}
}

// A draft addressed to nobody would queue a mail the outbox then keeps failing
// to send, so it is refused before it is queued at all.
func TestDraftSendNeedsARecipient(t *testing.T) {
	d, _ := seedDraft(t)
	tx, _ := d.Mirror.Begin("primary")
	id, _, _ := tx.UpsertMessage(mirror.Message{Key: "empty@example.com", Subject: "leer"})
	_ = tx.PutPlacement(mirror.Placement{Folder: "Drafts", UID: 9, MessageID: id})
	_ = tx.Commit()

	resp := ask(t, d, []string{"draft", "send"}, map[string]any{"positional": "9"})
	if resp.OK || !strings.Contains(resp.Error, "--to") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDraftDeleteTrashesIt(t *testing.T) {
	d, _ := seedDraft(t)
	mustAsk(t, d, []string{"draft", "delete"}, map[string]any{"positional": "1"})
	if rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message); len(rows) != 0 {
		t.Errorf("drafts = %+v", rows)
	}
}

// An account with no drafts box says so rather than inventing one.
func TestDraftWithNoDraftsBoxIsARefusal(t *testing.T) {
	d, _ := seedSend(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"draft", "list"}})
	if resp.OK || !strings.Contains(resp.Error, "drafts box") {
		t.Fatalf("resp = %+v", resp)
	}
}

// `compose --draft` is how a draft comes to exist here rather than in webmail.
// It is the same append an edit does, with nothing to trash afterwards.
func TestDraftSaveFilesANewDraftWithoutSending(t *testing.T) {
	d, tr := seedDraft(t)
	resp := mustAsk(t, d, []string{"draft", "save"}, map[string]any{
		"to": []any{"anna@example.com"}, "subject": "Angebot", "body": "noch nicht fertig\n",
	})
	got := resp.Data.(map[string]any)
	if got["state"] != "saved" || got["subject"] != "Angebot" {
		t.Fatalf("save gave %+v", got)
	}
	if len(tr.sent) != 0 {
		t.Fatalf("a draft went out over smtp")
	}
	rows := mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message)
	if len(rows) != 2 {
		t.Fatalf("%d drafts, want the seeded one and the new one: %+v", len(rows), rows)
	}
	var found bool
	for _, r := range rows {
		if r.Subject == "Angebot" && strings.Contains(r.To, "anna@example.com") {
			found = true
		}
	}
	if !found {
		t.Errorf("the new draft is not there: %+v", rows)
	}
}

// `reply --draft` is the same reply, written to the drafts box instead of the
// outbox — so who it answers and what thread it is in still come from the
// parent, and nothing goes out.
func TestReplyDraftFilesTheAnswerWithoutSendingIt(t *testing.T) {
	d, tr := seedDraft(t)
	resp := mustAsk(t, d, []string{"reply"}, map[string]any{
		"positional": "7", "body": "erster entwurf", "draft": true,
	})
	if got := resp.Data.(map[string]any); got["state"] != "saved" {
		t.Fatalf("reply --draft gave %+v", got)
	}
	if len(tr.sent) != 0 {
		t.Fatal("a draft went out over smtp")
	}

	var answer message
	for _, r := range mustAsk(t, d, []string{"draft", "list"}, nil).Data.([]message) {
		if r.Subject == "Re: Rechnung" {
			answer = r
		}
	}
	if answer.ID == "" {
		t.Fatal("the reply is not in drafts")
	}
	if !strings.Contains(answer.To, "billing@example.com") {
		t.Errorf("the draft answers %q", answer.To)
	}

	// And sending it from there keeps it in the thread. Losing the references
	// here would mean the two commands together started a new conversation,
	// which is not what either of them said.
	sentOut := mustAsk(t, d, []string{"draft", "send"},
		map[string]any{"positional": answer.ID}).Data.(sent)
	filed := filedCopy(t, d, sentOut.UID)
	if len(filed.References) == 0 || filed.References[len(filed.References)-1] != "plain@example.com" {
		t.Errorf("the thread was lost: references = %v", filed.References)
	}
	if len(filed.InReplyTo) != 1 || filed.InReplyTo[0] != "plain@example.com" {
		t.Errorf("in-reply-to = %v", filed.InReplyTo)
	}
}
