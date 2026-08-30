package mailsync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"mailbox/internal/mirror"
)

const box = "INBOX"

func setup(t *testing.T) (*Reconciler, *Fake, *mirror.Mirror) {
	t.Helper()
	m, err := mirror.Open(filepath.Join(t.TempDir(), "mirror.db"))
	if err != nil {
		t.Fatalf("open mirror: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	f := NewFake(box)
	return &Reconciler{Account: "primary", Mirror: m, Driver: f}, f, m
}

func runSync(t *testing.T, r *Reconciler) Outcome {
	t.Helper()
	out, err := r.Sync(context.Background(), box)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return out
}

func rows(t *testing.T, m *mirror.Mirror) []mirror.Row {
	t.Helper()
	got, err := m.Rows("primary", box, 100)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	return got
}

// Gate 1: cold start from an empty Mirror produces the correct folder.
func TestGate1_ColdStart(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	f.Deliver(box, "b@x", "second", "world")

	out := runSync(t, r)
	if out.Action != ActionResync {
		t.Errorf("action = %v, want resync", out.Action)
	}
	got := rows(t, m)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if out.BodiesFetched != 2 {
		t.Errorf("bodies fetched = %d, want 2", out.BodiesFetched)
	}
	for _, row := range got {
		if row.BodyState != "mirrored" || row.TextPlain == "" {
			t.Errorf("uid %d not mirrored: state=%q text=%q", row.UID, row.BodyState, row.TextPlain)
		}
	}

	// A second cycle with nothing changed must do no work at all.
	if out := runSync(t, r); out.Action != ActionNone {
		t.Errorf("second cycle action = %v, want none", out.Action)
	}
}

// Gate 2: a message delivered while watching appears on the next cycle. The
// watch only says "something changed" — pushes carry no data (ADR-0011) — so
// the assertion is that the cycle it triggers picks the message up.
func TestGate2_NewMessageArrives(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	runSync(t, r)

	f.Deliver(box, "b@x", "second", "world")
	out := runSync(t, r)
	if out.Action != ActionIncremental {
		t.Errorf("action = %v, want incremental", out.Action)
	}
	if out.NewMessages != 1 {
		t.Errorf("new messages = %d, want 1", out.NewMessages)
	}
	if got := rows(t, m); len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
}

// Gate 3: a flag changed by another client converges, without refetching a body.
func TestGate3_FlagConverges(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	runSync(t, r)
	before := f.CallCount("FetchBodies")

	f.SetFlags(box, 1, `\Seen`)
	out := runSync(t, r)
	if out.FlagsChanged != 1 {
		t.Errorf("flags changed = %d, want 1", out.FlagsChanged)
	}
	if got := f.CallCount("FetchBodies"); got != before {
		t.Errorf("refetched a body for a flag change (%d -> %d)", before, got)
	}
	got := rows(t, m)
	if len(got) != 1 || !got[0].Seen() {
		t.Errorf("flag did not converge: %+v", got)
	}
}

// Gate 4: a message expunged while the Daemon was stopped is gone after
// restart. The fake's Expunge deliberately does not bump the modseq, so only
// the count reveals it — which is the whole reason the diff path exists.
func TestGate4_ExpungeWhileStopped(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	f.Deliver(box, "b@x", "second", "world")
	runSync(t, r)

	f.Expunge(box, 1) // "while the daemon was stopped"

	out := runSync(t, r)
	if out.Expunged != 1 {
		t.Errorf("expunged = %d, want 1", out.Expunged)
	}
	got := rows(t, m)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].UID != 2 {
		t.Errorf("wrong message survived: uid %d", got[0].UID)
	}
}

// Gate 5: a UIDVALIDITY change forces a clean resync. This is the migration
// case the real server cannot be asked for — the uids change but the messages
// are still there — so the Mirror must re-map them by Message-ID and must not
// refetch bodies it already holds (ADR-0006).
func TestGate5_UIDValidityChange(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	f.Deliver(box, "b@x", "second", "world")
	runSync(t, r)
	bodiesBefore := f.CallCount("FetchBodies")

	f.Renumber(box, 2000)

	out := runSync(t, r)
	if out.Action != ActionResync {
		t.Fatalf("action = %v, want resync", out.Action)
	}
	if out.Remapped != 2 {
		t.Errorf("remapped = %d, want 2 — messages were not recognised", out.Remapped)
	}
	if got := f.CallCount("FetchBodies"); got != bodiesBefore {
		t.Errorf("refetched bodies across a UIDVALIDITY change (%d -> %d)", bodiesBefore, got)
	}
	got := rows(t, m)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	for _, row := range got {
		if row.TextPlain == "" {
			t.Errorf("uid %d lost its body across the resync", row.UID)
		}
	}
}

// A UIDVALIDITY change that lands between detect and fetch: the plan was made
// against one incarnation, the fetch lands on the next. The cycle must not
// commit a mixture of the two.
func TestUIDValidityChangeMidCycle(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	runSync(t, r)

	f.Deliver(box, "b@x", "second", "world")
	once := false
	f.Hook = func(op string) {
		if op == "FetchEnvelopes" && !once {
			once = true
			f.Renumber(box, 3000)
		}
	}
	if _, err := r.Sync(context.Background(), box); err != nil {
		t.Fatalf("sync: %v", err)
	}
	f.Hook = nil

	// Whatever that cycle managed, the next one must land on the new
	// incarnation and leave the Mirror agreeing with the server.
	runSync(t, r)
	local, err := m.Folder("primary", box)
	if err != nil {
		t.Fatal(err)
	}
	if local.UIDValidity != 3000 {
		t.Errorf("uidvalidity = %d, want 3000", local.UIDValidity)
	}
	remote, _ := f.status(box)
	if local.Count != int(remote.NumMessages) {
		t.Errorf("count = %d, server says %d", local.Count, remote.NumMessages)
	}
}

// A connection dropped after the envelopes were fetched and before the commit
// must leave the Mirror untouched and the intent standing, so Resume redoes it.
func TestCrashBeforeCommit(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")

	f.Fail["FetchBodies"] = errors.New("connection reset")
	if _, err := r.Sync(context.Background(), box); err == nil {
		t.Fatal("sync succeeded, want an error")
	}
	if got := rows(t, m); len(got) != 0 {
		t.Errorf("partial commit: %d rows, want 0", len(got))
	}
	intents, err := m.Intents("primary")
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].Folder != box {
		t.Fatalf("intent did not survive: %+v", intents)
	}

	if err := r.Resume(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := rows(t, m); len(got) != 1 {
		t.Errorf("resume did not finish the job: %d rows, want 1", len(got))
	}
	if intents, _ := m.Intents("primary"); len(intents) != 0 {
		t.Errorf("intent still standing after resume: %+v", intents)
	}
}

// A server whose STATUS count disagrees with the uid set it returns must not
// leave the Mirror stuck: the diff runs against what UID SEARCH actually says.
func TestCountDisagreesWithUIDSet(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "first", "hello")
	f.Deliver(box, "b@x", "second", "world")
	runSync(t, r)

	wrong := uint32(99)
	f.Folder(box).Count = &wrong

	if _, err := r.Sync(context.Background(), box); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := rows(t, m); len(got) != 2 {
		t.Errorf("got %d rows, want 2 — the diff trusted the count over the uid set", len(got))
	}
}

// Messages with no Message-ID, and two sharing one, must not collide into a
// single row (ADR-0007's synthetic-key path).
func TestMissingAndDuplicateMessageIDs(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "", "no id", "one")
	f.Deliver(box, "", "also no id", "two")
	f.Deliver(box, "dup@x", "dup a", "three")
	f.Deliver(box, "dup@x", "dup b", "four")

	runSync(t, r)
	got := rows(t, m)
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4 — messages collided", len(got))
	}
	seen := map[string]bool{}
	for _, row := range got {
		if seen[row.Key] {
			t.Errorf("duplicate message key %q", row.Key)
		}
		seen[row.Key] = true
	}
}

// SyncAll must detect every folder in one pass. Looping Sync would work, and
// would quietly cost one LIST-STATUS round trip per folder — turning the
// O(folders) design into O(folders) round trips, which is the thing ADR-0006
// exists to avoid.
func TestSyncAllUsesOneDetectionPass(t *testing.T) {
	r, f, m := setup(t)
	for _, b := range []string{"INBOX/Feed", "INBOX/Screener", "Archive"} {
		f.AddFolder(b)
	}
	f.Deliver(box, "a@x", "in inbox", "one")
	f.Deliver("INBOX/Feed", "b@x", "in feed", "two")

	folders, err := f.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := f.CallCount("Status")

	outcomes, err := r.SyncAll(context.Background(), folders)
	if err != nil {
		t.Fatalf("sync all: %v", err)
	}
	if got := f.CallCount("Status") - before; got != 1 {
		t.Errorf("detection ran %d times for %d folders, want 1", got, len(folders))
	}
	if len(outcomes) != len(folders) {
		t.Errorf("got %d outcomes, want %d", len(outcomes), len(folders))
	}

	// Both folders with mail must be mirrored, not just the first.
	if rows, _ := m.Rows("primary", box, 10); len(rows) != 1 {
		t.Errorf("INBOX: %d rows, want 1", len(rows))
	}
	if rows, _ := m.Rows("primary", "INBOX/Feed", 10); len(rows) != 1 {
		t.Errorf("INBOX/Feed: %d rows, want 1 — an unwatched box was not mirrored", len(rows))
	}
}

// One failing folder must not stop the rest: the Mirror may be Behind on one
// Box and current on the others.
func TestSyncAllSurvivesOneBadFolder(t *testing.T) {
	r, f, m := setup(t)
	f.AddFolder("INBOX/Feed")
	f.Deliver(box, "a@x", "in inbox", "one")
	f.Deliver("INBOX/Feed", "b@x", "in feed", "two")

	// The first folder reconciled fails its body fetch; the other must survive.
	f.Fail["FetchBodies"] = errors.New("connection reset")

	folders, _ := f.Folders(context.Background())
	outcomes, err := r.SyncAll(context.Background(), folders)
	if err == nil {
		t.Error("want an error reporting the failed folder")
	}
	if len(outcomes) != 1 {
		t.Fatalf("got %d successful outcomes, want 1", len(outcomes))
	}
	total := 0
	for _, b := range folders {
		rows, _ := m.Rows("primary", b, 10)
		total += len(rows)
	}
	if total != 1 {
		t.Errorf("mirrored %d messages, want 1 — the good folder did not survive", total)
	}
}

// What the reconciler writes is what Search reads: a synced Message is findable
// by its subject and by its text, without a second indexing step.
func TestSyncedMessagesAreSearchable(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "Rechnung Mai", "Ihre Rechnung liegt bereit")
	f.Deliver(box, "b@x", "Urlaub", "wir fahren weg")
	runSync(t, r)

	for _, tc := range []struct {
		query string
		want  int
	}{
		{"Rechnung", 1}, {"bereit", 1}, {"fahren", 1}, {"Rechnung fahren", 0},
	} {
		hits, err := m.Search("primary", mirror.Query{Text: tc.query})
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		if len(hits) != tc.want {
			t.Errorf("search %q: %d hits, want %d", tc.query, len(hits), tc.want)
		}
	}
}

// Threads are built as mail is mirrored, from References and In-Reply-To, and
// the whole conversation is readable from any Message in it (ADR-0008).
func TestSyncBuildsThreads(t *testing.T) {
	r, f, m := setup(t)
	parent := f.Deliver(box, "a@x", "Angebot", "hier ist das Angebot")
	f.Deliver(box, "b@x", "Re: Angebot", "danke").Reply(parent)
	runSync(t, r)

	got := rows(t, m)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0].ThreadID != got[1].ThreadID {
		t.Fatalf("reply and parent are in different threads: %d, %d", got[0].ThreadID, got[1].ThreadID)
	}
	thread, err := m.Thread("primary", got[0].ThreadID)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if len(thread) != 2 {
		t.Errorf("thread has %d messages, want 2", len(thread))
	}
}

// What a Message carries is recorded while its text is fetched, so a listing is
// a Mirror read and only the bytes ever go to the server (ADR-0003).
func TestSyncRecordsAttachmentMetadataButNotBytes(t *testing.T) {
	r, f, m := setup(t)
	f.Deliver(box, "a@x", "Rechnung", "siehe Anhang").
		Attach("2", "application/pdf", "rechnung.pdf", []byte("%PDF-1.4 fake"))
	runSync(t, r)

	got := rows(t, m)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	parts, err := m.Parts(got[0].Message.ID)
	if err != nil {
		t.Fatalf("parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d parts, want 1: %+v", len(parts), parts)
	}
	if parts[0].Filename != "rechnung.pdf" || parts[0].MIMEType != "application/pdf" || parts[0].Size != 13 {
		t.Errorf("part = %+v", parts[0])
	}
	if n := f.CallCount("FetchPart"); n != 0 {
		t.Errorf("the sync fetched %d attachment bodies, want 0", n)
	}
}

// The cold start's order is the caller's: the Inbox first, so useful answers
// exist in seconds rather than after the whole mirror. The server answers
// LIST-STATUS in whatever order it likes, and that does not get to undo it.
func TestSyncAllFollowsTheOrderItWasAskedFor(t *testing.T) {
	r, f, _ := setup(t)
	for _, name := range []string{"Archive", "INBOX/Screener"} {
		f.AddFolder(name)
	}
	var order []string
	r.OnFolder = func(folder string, _ Outcome, _ error) { order = append(order, folder) }
	want := []string{"INBOX", "INBOX/Screener", "Archive"}
	if _, err := r.SyncAll(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("synced %v, asked for %v", order, want)
		}
	}
}
