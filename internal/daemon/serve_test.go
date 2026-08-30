package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/mailsync"
)

// seed builds a Mirror holding one Message in each of INBOX and Screener, so a
// read can be tested without a server or a reconciler.
func seed(t *testing.T) *Daemon {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	tx, err := m.Begin("primary")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	plain, _, err := tx.UpsertMessage(mirror.Message{
		Key: "plain@example.com", Date: time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC),
		Subject: "Rechnung", From: "billing@example.com", To: "me@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(plain, "the text\n", "", "the text"); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutParts(plain, []mirror.Part{
		{Path: "2", MIMEType: "application/pdf", Filename: "rechnung.pdf", Disposition: "attachment", Size: 13},
		{Path: "3", MIMEType: "image/png", Disposition: "inline", Size: 44},
	}); err != nil {
		t.Fatal(err)
	}

	// The same Message in two Boxes is one Message with two Placements.
	for _, p := range []mirror.Placement{
		{Folder: "INBOX", UID: 7, MessageID: plain, Flags: []string{`\Seen`}, Size: 120},
		{Folder: "INBOX/Sent", UID: 3, MessageID: plain, Size: 120},
	} {
		if err := tx.PutPlacement(p); err != nil {
			t.Fatal(err)
		}
	}

	// A reply to the first message, so the seed has a Thread in it.
	reply, _, err := tx.UpsertMessage(mirror.Message{
		Key: "reply@example.com", Subject: "Re: Rechnung", From: "me@example.com",
		References: []string{"plain@example.com"},
		Date:       time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(reply, "schon bezahlt\n", "", "schon bezahlt"); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX/Sent", UID: 4, MessageID: reply}); err != nil {
		t.Fatal(err)
	}

	html, _, err := tx.UpsertMessage(mirror.Message{Key: "html@example.com", Subject: "Newsletter"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetBody(html, "", "<p>Hello <b>you</b></p>", "Hello you"); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutPlacement(mirror.Placement{Folder: "INBOX/Screener", UID: 42, MessageID: html}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	mirrored := []string{"INBOX", "INBOX/Screener", "INBOX/Sent", "Archive", "Junk"}

	// A server holding the same messages, so a write has something to ack and a
	// fetch has something to fetch.
	f := mailsync.NewFake("INBOX")
	for _, name := range []string{"INBOX/Screener", "INBOX/Sent", "Archive", "Junk", "Trash"} {
		f.AddFolder(name)
	}
	f.Folder("INBOX").UIDNext = 7
	f.Deliver("INBOX", "plain@example.com", "Rechnung", "the text\n").
		Attach("2", "application/pdf", "rechnung.pdf", []byte("%PDF-1.4 fake"))
	f.Folder("INBOX/Screener").UIDNext = 42
	f.Deliver("INBOX/Screener", "html@example.com", "Newsletter", "")
	r := &mailsync.Reconciler{Account: "primary", Mirror: m, Driver: f}
	return New("primary", m, r, mirrored, nil, nil)
}

// fakeOf reaches the scripted server behind a seeded Daemon.
func fakeOf(d *Daemon) *mailsync.Fake { return d.Reconciler.Driver.(*mailsync.Fake) }

func view(t *testing.T, d *Daemon, id string) map[string]any {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"}, Args: map[string]any{"positional": id}})
	if !resp.OK {
		t.Fatalf("message view %q: %s (%s)", id, resp.Error, resp.Code)
	}
	data, ok := resp.Data.(message)
	if !ok {
		t.Fatalf("message view %q returned %T", id, resp.Data)
	}
	return map[string]any{
		"id": data.ID, "body": data.Body, "format": data.BodyFormat,
		"subject": data.Subject, "places": data.Placements,
	}
}

func TestMessageViewBareUIDMeansInbox(t *testing.T) {
	d := seed(t)
	got := view(t, d, "7")
	if got["subject"] != "Rechnung" {
		t.Fatalf("subject = %v", got["subject"])
	}
	if got["body"] != "the text\n" || got["format"] != "plain" {
		t.Fatalf("body = %q (%v)", got["body"], got["format"])
	}
	// Every Box the Message sits in, so a caller can find its other Placement.
	places, _ := got["places"].([]string)
	if len(places) != 2 || places[0] != "7" || places[1] != "Sent:3" {
		t.Fatalf("placements = %v", places)
	}
}

// An HTML-only message is read as Markdown: a caller of this CLI should never
// have to parse HTML to find out what a mail says.
func TestMessageViewRendersHTMLOnlyAsMarkdown(t *testing.T) {
	d := seed(t)
	got := view(t, d, "INBOX/Screener:42")
	if got["format"] != "markdown" {
		t.Fatalf("format = %v", got["format"])
	}
	if body, _ := got["body"].(string); !strings.Contains(body, "**you**") {
		t.Fatalf("body = %q", body)
	}
}

// An expunged uid is an ordinary thing to ask a Mirror that may be Behind for.
func TestMessageViewUnknownUIDIsNotFound(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"message", "view"}, Args: map[string]any{"positional": "999"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestParseMessageID(t *testing.T) {
	known := []string{"INBOX", "INBOX/Screener"}
	for _, tc := range []struct {
		in     string
		folder string
		uid    uint32
		bad    bool
	}{
		{in: "7", folder: "INBOX", uid: 7},
		{in: "INBOX/Screener:42", folder: "INBOX/Screener", uid: 42},
		// The short form is what a listing prints, so it is the one most ids
		// come back in.
		{in: "Screener:42", folder: "INBOX/Screener", uid: 42},
		{in: "screener:42", folder: "INBOX/Screener", uid: 42},
		{in: "inbox/screener:42", folder: "INBOX/Screener", uid: 42},
		{in: "inbox:1", folder: "INBOX", uid: 1},
		{in: "INBOX/Screener", bad: true},
		{in: "", bad: true},
		{in: "0", bad: true},
	} {
		folder, uid, err := parseMessageID(tc.in, known)
		if tc.bad {
			if err == nil {
				t.Errorf("parseMessageID(%q) = %s:%d, want error", tc.in, folder, uid)
			}
			continue
		}
		if err != nil || folder != tc.folder || uid != tc.uid {
			t.Errorf("parseMessageID(%q) = %s:%d, %v; want %s:%d", tc.in, folder, uid, err, tc.folder, tc.uid)
		}
	}
}

func write(t *testing.T, d *Daemon, cmd []string, args map[string]any) []change {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: cmd, Args: args})
	if !resp.OK {
		t.Fatalf("%v: %s (%s)", cmd, resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]change)
	if !ok {
		t.Fatalf("%v returned %T", cmd, resp.Data)
	}
	return out
}

// A write blocks on the server and the Mirror is true straight afterwards: the
// next read sees the change without waiting for a cycle (ADR-0004).
func TestSeenIsVisibleToTheNextRead(t *testing.T) {
	d := seed(t)
	// Seeded \Seen, so start from unseen to make the change visible.
	got := write(t, d, []string{"unseen"}, map[string]any{"positional": []any{"7"}})
	if len(got) != 1 || got[0].Seen {
		t.Fatalf("unseen returned %+v", got)
	}
	// A flag change is not a move, even when it leaves the Message with no
	// flags at all: a reader that cannot tell the two apart prints a move.
	if got[0].Moved || got[0].NewID != "" {
		t.Errorf("flag change reported as a move: %+v", got[0])
	}
	row, err := d.Mirror.Row("primary", "INBOX", 7)
	if err != nil || row.Seen() {
		t.Fatalf("row = %+v, err %v; want unseen", row.Placement, err)
	}

	got = write(t, d, []string{"seen"}, map[string]any{"positional": []any{"7"}})
	if len(got) != 1 || !got[0].Seen || got[0].ID != "7" {
		t.Fatalf("seen returned %+v", got)
	}
}

// Ids may name any mirrored Box, and one command may span several.
func TestWriteAcceptsIdsFromSeveralBoxes(t *testing.T) {
	d := seed(t)
	got := write(t, d, []string{"seen"}, map[string]any{"positional": []any{"7", "INBOX/Screener:42"}})
	if len(got) != 2 {
		t.Fatalf("got %d changes, want 2", len(got))
	}
}

func TestMoveReportsTheNewID(t *testing.T) {
	d := seed(t)
	got := write(t, d, []string{"move"}, map[string]any{"positional": []any{"7"}, "to": "archive"})
	if len(got) != 1 || got[0].Box != "Archive" || got[0].NewID == "" {
		t.Fatalf("move returned %+v", got)
	}
	if _, err := d.Mirror.Row("primary", "INBOX", 7); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("still in INBOX: %v", err)
	}
}

// Trash is not a mirrored Box, so trashing takes the Message out of the Mirror.
func TestTrashLeavesTheMirror(t *testing.T) {
	d := seed(t)
	if got := write(t, d, []string{"trash"}, map[string]any{"positional": []any{"7"}}); got[0].Box != "Trash" {
		t.Fatalf("trash returned %+v", got)
	}
	if _, err := d.Mirror.Row("primary", "INBOX", 7); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("still in INBOX: %v", err)
	}
	if _, err := d.Mirror.Row("primary", "Trash", 1); !errors.Is(err, mirror.ErrNotFound) {
		t.Errorf("trash placement written into the mirror: %v", err)
	}
}

// A binned message should not sit in anyone's unread count, so trash forces
// \Seen on the way out even when the message arrived unread.
func TestTrashMarksItRead(t *testing.T) {
	d := seed(t)
	f := fakeOf(d)
	if flags := f.Folder("INBOX/Screener").Msgs[0].Flags; hasFlag(flags, `\Seen`) {
		t.Fatalf("seed message already \\Seen: %v", flags)
	}
	if got := write(t, d, []string{"trash"}, map[string]any{"positional": []any{"Screener:42"}}); got[0].Box != "Trash" {
		t.Fatalf("trash returned %+v", got)
	}
	moved := f.Folder("Trash").Msgs
	if len(moved) != 1 || !hasFlag(moved[0].Flags, `\Seen`) {
		t.Fatalf("trashed message is not \\Seen: %+v", moved)
	}
}

// An id that named nothing in the Mirror is caught before the server is
// touched, and the reply matches what the read commands say for the same id
// rather than a folder-shaped "INBOX: not found" from the IMAP layer.
func TestTrashOfAnUnknownIDIsNotFound(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"trash"}, Args: map[string]any{"positional": []any{"99999"}}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Error != "99999: no such message — moved or deleted since it was listed" {
		t.Fatalf("error = %q", resp.Error)
	}
}

// The same guard covers the flag writes, and it names the offending id even
// when an earlier id in the same command was fine.
func TestSeenOfAnUnknownIDNamesThatID(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"seen"}, Args: map[string]any{"positional": []any{"7", "99999"}}})
	if resp.OK || resp.Code != "not_found" || resp.Error != "99999: no such message — moved or deleted since it was listed" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestMoveWithoutADestinationIsAUsageError(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"move"}, Args: map[string]any{"positional": []any{"7"}}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}

func searchFor(t *testing.T, d *Daemon, args map[string]any) []hit {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"search"}, Args: args})
	if !resp.OK {
		t.Fatalf("search %v: %s (%s)", args, resp.Error, resp.Code)
	}
	hits, ok := resp.Data.([]hit)
	if !ok {
		t.Fatalf("search returned %T", resp.Data)
	}
	return hits
}

// A search result carries the id to read it with and where it is, so the next
// command needs nothing else.
func TestSearchReturnsReadableIDs(t *testing.T) {
	d := seed(t)
	hits := searchFor(t, d, map[string]any{"positional": "text"})
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].ID != "7" || hits[0].Box != "INBOX" || hits[0].Snippet == "" {
		t.Errorf("hit = %+v", hits[0])
	}
}

// An HTML-only Message is searchable by the text a reader would see, not by
// its markup.
func TestSearchMatchesRenderedTextNotMarkup(t *testing.T) {
	d := seed(t)
	hits := searchFor(t, d, map[string]any{"positional": "Hello"})
	if len(hits) != 1 || hits[0].ID != "Screener:42" {
		t.Fatalf("hits = %+v", hits)
	}
	if got := searchFor(t, d, map[string]any{"positional": "b"}); len(got) != 0 {
		t.Errorf("matched on a tag name: %+v", got)
	}
}

func TestSearchInABox(t *testing.T) {
	d := seed(t)
	if got := searchFor(t, d, map[string]any{"positional": "Hello", "in": "inbox"}); len(got) != 0 {
		t.Errorf("--in inbox matched a message in the screener: %+v", got)
	}
}

func TestSearchWithNothingToLookForIsAUsageError(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"search"}, Args: map[string]any{"positional": "  "}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}

func threadOf(t *testing.T, d *Daemon, id string) []message {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"thread", "view"}, Args: map[string]any{"positional": id}})
	if !resp.OK {
		t.Fatalf("thread %s: %s (%s)", id, resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]message)
	if !ok {
		t.Fatalf("thread returned %T", resp.Data)
	}
	return out
}

// The conversation reads the same from either end of it, and crosses Boxes —
// which is the thing IMAP THREAD cannot do (ADR-0008).
func TestThreadReadsFromEitherEnd(t *testing.T) {
	d := seed(t)
	for _, id := range []string{"7", "INBOX/Sent:4"} {
		got := threadOf(t, d, id)
		if len(got) != 2 {
			t.Fatalf("thread from %s has %d messages, want 2", id, len(got))
		}
		if got[0].ID != "7" || got[1].ID != "Sent:4" {
			t.Errorf("thread from %s = %s, %s; want 7, Sent:4 oldest first", id, got[0].ID, got[1].ID)
		}
		if got[1].Body == "" {
			t.Errorf("thread from %s: the reply has no text", id)
		}
	}
}

// A Message nothing links to is a Thread of one, not an error.
func TestThreadOfALoneMessage(t *testing.T) {
	d := seed(t)
	if got := threadOf(t, d, "INBOX/Screener:42"); len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
}

func TestThreadOfAnUnknownIDIsNotFound(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"thread", "view"}, Args: map[string]any{"positional": "999"}})
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("resp = %+v", resp)
	}
}

func attachments(t *testing.T, d *Daemon, id string) []attachment {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"attachment", "list"}, Args: map[string]any{"positional": id}})
	if !resp.OK {
		t.Fatalf("attachment list %s: %s (%s)", id, resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]attachment)
	if !ok {
		t.Fatalf("attachment list returned %T", resp.Data)
	}
	return out
}

// Listing what a Message carries is a Mirror read: names and sizes are held,
// bytes are not (ADR-0003).
func TestAttachmentListComesFromTheMirror(t *testing.T) {
	d := seed(t)
	got := attachments(t, d, "7")
	if len(got) != 2 {
		t.Fatalf("got %d attachments, want 2: %+v", len(got), got)
	}
	if got[0].ID != "7:1" || got[0].Filename != "rechnung.pdf" || got[0].Size != 13 {
		t.Errorf("first attachment = %+v", got[0])
	}
	// A part with no filename still has to be nameable on disk.
	if got[1].Filename != "part-3.png" {
		t.Errorf("unnamed part = %q, want part-3.png", got[1].Filename)
	}
	if fakeOf(d).CallCount("FetchPart") != 0 {
		t.Errorf("listing went to the server")
	}
}

func TestAttachmentSaveWritesTheFile(t *testing.T) {
	d := seed(t)
	dir := t.TempDir()
	resp := d.handle(context.Background(), Request{
		ID: "1", Cmd: []string{"attachment", "save"},
		Args: map[string]any{"positional": "7:1", "output": dir},
	})
	if !resp.OK {
		t.Fatalf("save: %s (%s)", resp.Error, resp.Code)
	}
	got, ok := resp.Data.(saved)
	if !ok {
		t.Fatalf("save returned %T", resp.Data)
	}
	want := filepath.Join(dir, "rechnung.pdf")
	if got.Path != want {
		t.Fatalf("wrote %s, want %s", got.Path, want)
	}
	body, err := os.ReadFile(want)
	if err != nil || string(body) != "%PDF-1.4 fake" {
		t.Fatalf("file = %q, err %v", body, err)
	}

	// A second save must not silently overwrite the first.
	again := d.handle(context.Background(), Request{
		ID: "1", Cmd: []string{"attachment", "save"},
		Args: map[string]any{"positional": "7:1", "output": dir},
	})
	if again.OK {
		t.Error("save overwrote an existing file without --force")
	}
	forced := d.handle(context.Background(), Request{
		ID: "1", Cmd: []string{"attachment", "save"},
		Args: map[string]any{"positional": "7:1", "output": dir, "force": true},
	})
	if !forced.OK {
		t.Errorf("--force: %s", forced.Error)
	}
}

// A Message with several attachments cannot be named without saying which, and
// the error says what to type.
func TestAttachmentSaveNeedsAnIndexWhenThereAreSeveral(t *testing.T) {
	d := seed(t)
	resp := d.handle(context.Background(), Request{
		ID: "1", Cmd: []string{"attachment", "save"},
		Args: map[string]any{"positional": "7", "output": t.TempDir()},
	})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(resp.Error, "7:1") {
		t.Errorf("error does not say what to type: %s", resp.Error)
	}
}

func TestAttachmentListOfAMessageWithNone(t *testing.T) {
	d := seed(t)
	if got := attachments(t, d, "INBOX/Screener:42"); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestParseAttachmentID(t *testing.T) {
	known := []string{"INBOX", "INBOX/Screener"}
	for _, tc := range []struct {
		in     string
		folder string
		uid    uint32
		index  int
	}{
		{in: "7", folder: "INBOX", uid: 7, index: 0},
		{in: "7:2", folder: "INBOX", uid: 7, index: 2},
		{in: "INBOX/Screener:42", folder: "INBOX/Screener", uid: 42, index: 0},
		{in: "INBOX/Screener:42:3", folder: "INBOX/Screener", uid: 42, index: 3},
		{in: "Screener:42:3", folder: "INBOX/Screener", uid: 42, index: 3},
	} {
		folder, uid, index, err := parseAttachmentID(tc.in, known)
		if err != nil || folder != tc.folder || uid != tc.uid || index != tc.index {
			t.Errorf("parseAttachmentID(%q) = %s:%d:%d, %v; want %s:%d:%d",
				tc.in, folder, uid, index, err, tc.folder, tc.uid, tc.index)
		}
	}
}

// A listing follows the way mail moves rather than the alphabet, and it is the
// eight boxes mail moves through — not the fifty-odd archive folders behind
// them, which is what an account actually looks like.
func TestBoxListIsTheRoutingBoxesInTheOrderMailMovesThrough(t *testing.T) {
	d := seed(t)
	rows := mustAsk(t, d, []string{"box", "list"}, nil).Data.([]boxRow)

	var got []string
	for _, r := range rows {
		got = append(got, r.Box)
	}
	// The seed holds INBOX, INBOX/Screener, INBOX/Sent, Archive and Junk.
	want := []string{"INBOX", "Screener", "Sent", "Junk"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("boxes = %v, want %v", got, want)
	}
	// The name in the row is the one that reads it back: no INBOX/ prefix.
	if rows[1].Folder != "INBOX/Screener" {
		t.Errorf("Screener is folder %q", rows[1].Folder)
	}
	// Driven by what is mirrored and not by what has mail in it, so a box that
	// has gone quiet is a row saying so.
	if rows[3].Box != "Junk" || rows[3].Count != 0 {
		t.Errorf("the empty junk box is %+v", rows[3])
	}
}

// --archive is everything, with the boxes mail moves through still first: the
// archive tree is where you go looking for one thing, not where you browse.
func TestBoxListArchiveAddsTheRestAfterThem(t *testing.T) {
	d := seed(t)
	rows := mustAsk(t, d, []string{"box", "list"}, map[string]any{"archive": true}).Data.([]boxRow)

	var got []string
	for _, r := range rows {
		got = append(got, r.Box)
	}
	want := []string{"INBOX", "Screener", "Sent", "Junk", "Archive"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("boxes = %v, want %v", got, want)
	}
	if len(rows) != len(d.Mirrored) {
		t.Errorf("%d rows for %d mirrored boxes", len(rows), len(d.Mirrored))
	}
}

// The block pile is where a blocked sender's waiting mail went so a mistake can
// still be found. Worth having; not worth a line in every listing.
func TestBoxListLeavesTheBlockPileOutUnlessAsked(t *testing.T) {
	d := seed(t)
	d.Mirrored = append(d.Mirrored, "INBOX/Screener/Block")

	for _, c := range []struct {
		archive bool
		want    bool
	}{{false, false}, {true, true}} {
		rows := mustAsk(t, d, []string{"box", "list"},
			map[string]any{"archive": c.archive}).Data.([]boxRow)
		found := false
		for _, r := range rows {
			found = found || r.Box == "Screener/Block"
		}
		if found != c.want {
			t.Errorf("--archive=%v: block pile listed = %v", c.archive, found)
		}
	}
}

// spam and trash are the same IMAP move to two different boxes. Neither
// blocks the sender: that decision is the routing's.
func TestSpamMovesToJunkAndNotToTrash(t *testing.T) {
	d := seed(t)
	resp := mustAsk(t, d, []string{"spam"}, map[string]any{"positional": []any{"7"}})
	got := resp.Data.([]change)
	if len(got) != 1 || !got[0].Moved {
		t.Fatalf("spam gave %+v", got)
	}
	if got[0].Box != "Junk" {
		t.Errorf("spam filed into %q, want Junk", got[0].Box)
	}
}

// An account with nowhere to put spam says so rather than inventing a box or
// quietly using Trash, which would be a different and worse thing to do.
func TestSpamWithNoJunkBoxIsARefusal(t *testing.T) {
	d := seed(t)
	d.Mirrored = []string{"INBOX"}
	resp := ask(t, d, []string{"spam"}, map[string]any{"positional": []any{"7"}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(resp.Error, "junk") {
		t.Errorf("error = %q", resp.Error)
	}
}
