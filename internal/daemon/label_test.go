package daemon

import (
	"strings"
	"testing"
)

func labels(t *testing.T, d *Daemon, cmd string, args map[string]any) []labelRow {
	t.Helper()
	resp := mustAsk(t, d, []string{"label", cmd}, args)
	rows, ok := resp.Data.([]labelRow)
	if !ok {
		t.Fatalf("label %s returned %T", cmd, resp.Data)
	}
	return rows
}

// A label in use needs no creating: the mail carrying the keyword is what says
// it exists, so a rebuilt mirror brings every used label back with the mail.
func TestALabelExistsBecauseMailCarriesIt(t *testing.T) {
	d := seed(t)
	if rows := labels(t, d, "list", nil); len(rows) != 0 {
		t.Fatalf("labels = %+v", rows)
	}

	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7"}, "name": "learn"})

	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "learn" || rows[0].Count != 1 {
		t.Fatalf("labels = %+v", rows)
	}

	// And the keyword is really on the message, beside the flags it had.
	got := view(t, d, "7")
	if !strings.Contains(strings.Join(flagsOf(t, d, "INBOX", 7), " "), "learn") {
		t.Errorf("the keyword is not on the message: %v", got)
	}
}

// Removing takes the keyword off the mail. The name stays on the list, because
// a label you have used before is one you will use again.
func TestRemovingALabelLeavesTheNameBehind(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7"}, "name": "learn"})
	mustAsk(t, d, []string{"label", "remove"},
		map[string]any{"positional": []any{"7"}, "name": "learn"})

	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "learn" || rows[0].Count != 0 {
		t.Fatalf("labels = %+v", rows)
	}
	if strings.Contains(strings.Join(flagsOf(t, d, "INBOX", 7), " "), "learn") {
		t.Error("the keyword is still on the message")
	}
}

// create is only for a label with nothing on it yet, and it must not invent
// mail to hang it on.
func TestCreateRemembersAnEmptyLabel(t *testing.T) {
	d := seed(t)
	rows := labels(t, d, "create", map[string]any{"name": "school"})
	if len(rows) != 1 || rows[0].Label != "school" {
		t.Fatalf("create gave %+v", rows)
	}
	all := labels(t, d, "list", nil)
	if len(all) != 1 || all[0].Count != 0 {
		t.Fatalf("labels = %+v", all)
	}
}

// The server's own keywords are not labels anybody chose, and listing them as
// such buries the two or three that are. Three namespaces are the server's: the
// system flags, the registered $-keywords of RFC 5788, and the spam-training
// names every provider uses without anybody having standardised them.
func TestSystemKeywordsAreNotLabels(t *testing.T) {
	d := seed(t)
	// The seed marks INBOX:7 \Seen.
	if rows := labels(t, d, "list", nil); len(rows) != 0 {
		t.Fatalf("labels = %+v", rows)
	}

	tx, err := d.Mirror.Begin(d.Account)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetFlags("INBOX", 7, []string{
		`\Seen`, "$Forwarded", "$purchases", "NonJunk", "Junk", "learn",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "learn" {
		t.Fatalf("labels = %+v, want only the one somebody chose", rows)
	}
}

// A label with a space in it would reach IMAP as two keywords, so it is one
// word by the time it gets there.
func TestALabelIsOneWord(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7"}, "name": "zu lesen"})
	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "zu-lesen" {
		t.Fatalf("labels = %+v", rows)
	}
}

// view is a listing of the mail carrying one label, wherever it is.
func TestLabelViewListsTheMailCarryingIt(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7", "Screener:42"}, "name": "learn"})

	resp := mustAsk(t, d, []string{"label", "view"}, map[string]any{"name": "learn"})
	got := resp.Data.([]row)
	if len(got) != 2 {
		t.Fatalf("label view gave %+v", got)
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["7"] || !ids["Screener:42"] {
		t.Errorf("ids = %v", ids)
	}
}

// A listing says what its mail carries, so a label is visible without opening
// anything — and it is the Thread's label, not only the newest Message's.
func TestAListingCarriesTheLabels(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7"}, "name": "learn"})

	resp := mustAsk(t, d, []string{"box", "view"}, map[string]any{"positional": "INBOX"})
	for _, r := range resp.Data.([]row) {
		if r.ID == "7" {
			if len(r.Labels) != 1 || r.Labels[0] != "learn" {
				t.Fatalf("labels = %v", r.Labels)
			}
			return
		}
	}
	t.Fatal("7 is not in the listing")
}

// A rename takes the keyword with it: the same mail, under the new name, and
// the old name gone from the list.
func TestRenamingALabelMovesTheKeyword(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7"}, "name": "learn"})
	mustAsk(t, d, []string{"label", "rename"},
		map[string]any{"name": "learn", "to": "lernen"})

	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "lernen" || rows[0].Count != 1 {
		t.Fatalf("labels = %+v", rows)
	}
	flags := strings.Join(flagsOf(t, d, "INBOX", 7), " ")
	if !strings.Contains(flags, "lernen") || strings.Contains(flags, "learn ") {
		t.Errorf("flags = %s", flags)
	}
}

// Deleting takes the keyword off every message carrying it, wherever they sit,
// and does not touch the mail itself.
func TestDeletingALabelTakesItOffEverything(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "add"},
		map[string]any{"positional": []any{"7", "Screener:42"}, "name": "learn"})
	mustAsk(t, d, []string{"label", "delete"}, map[string]any{"name": "learn"})

	if rows := labels(t, d, "list", nil); len(rows) != 0 {
		t.Fatalf("labels = %+v", rows)
	}
	for _, place := range []struct {
		folder string
		uid    uint32
	}{{"INBOX", 7}, {"INBOX/Screener", 42}} {
		if flags := strings.Join(flagsOf(t, d, place.folder, place.uid), " "); strings.Contains(flags, "learn") {
			t.Errorf("%s:%d still carries it: %s", place.folder, place.uid, flags)
		}
	}
	// The mail is still there — a label came off, nothing was thrown away.
	view(t, d, "7")
}

// A label nobody has applied yet is only a remembered name, and renaming or
// deleting one has no mail to walk.
func TestRenamingAndDeletingAnUnusedLabel(t *testing.T) {
	d := seed(t)
	mustAsk(t, d, []string{"label", "create"}, map[string]any{"name": "school"})
	mustAsk(t, d, []string{"label", "rename"},
		map[string]any{"name": "school", "to": "schule"})

	rows := labels(t, d, "list", nil)
	if len(rows) != 1 || rows[0].Label != "schule" || rows[0].Count != 0 {
		t.Fatalf("labels = %+v", rows)
	}
	mustAsk(t, d, []string{"label", "delete"}, map[string]any{"name": "schule"})
	if rows := labels(t, d, "list", nil); len(rows) != 0 {
		t.Fatalf("labels = %+v", rows)
	}
}

func flagsOf(t *testing.T, d *Daemon, folder string, uid uint32) []string {
	t.Helper()
	row, err := d.Mirror.Row(d.Account, folder, uid)
	if err != nil {
		t.Fatal(err)
	}
	return row.Placement.Flags
}
