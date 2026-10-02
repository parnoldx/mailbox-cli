package daemon

import (
	"testing"
)

// An id a listing prints is an id the reader accepts, naming the same
// Placement: every Box of every account, through every rule that shortens one.
func TestEveryPrintedIDReadsBackToItsPlacement(t *testing.T) {
	d := New("primary", nil, nil, []string{
		"INBOX", "INBOX/Screener", "INBOX/Screener/Block", "INBOX/Paper Trail",
		"INBOX/Feed", "Archive", "Archive:2024", "Projects/Alpha",
		// Both spellings exist, so neither is shortened.
		"INBOX/Sent", "Sent",
	}, nil, nil)
	gmx := NewAccount("gmx", nil, nil, []string{"INBOX", "Drafts", "INBOX/Feed", "Spam"}, nil)
	d.StartAccount(gmx)

	for _, a := range d.accounts() {
		for _, folder := range a.Mirrored {
			id := a.messageID(folder, 42)
			got, f, uid, err := d.resolveID(id)
			if err != nil || got != a || f != folder || uid != 42 {
				t.Errorf("%s %s:42 printed %q, read back as %s %s:%d (%v)", a.Name, folder, id, nameOf(got), f, uid, err)
			}
			attID := attachmentID(a, folder, 42, 3)
			got, f, uid, index, err := d.resolveAttachmentID(attID)
			if err != nil || got != a || f != folder || uid != 42 || index != 3 {
				t.Errorf("%s %s:42#3 printed %q, read back as %s %s:%d:%d (%v)", a.Name, folder, attID, nameOf(got), f, uid, index, err)
			}
		}
	}
}

// A draft command says which account it is about, and a bare uid there is in
// that account's Drafts — but an id naming another account still wins.
func TestADraftIDIsReadAgainstItsOwnDefaults(t *testing.T) {
	d := New("primary", nil, nil, []string{"INBOX", "INBOX/Drafts"}, nil, nil)
	gmx := NewAccount("gmx", nil, nil, []string{"INBOX", "Drafts"}, nil)
	d.StartAccount(gmx)

	for _, tc := range []struct {
		id     string
		home   *Account
		want   *Account
		folder string
	}{
		{"7", gmx, gmx, "Drafts"},
		{"7", d.Primary, d.Primary, "INBOX/Drafts"},
		{"gmx/7", d.Primary, gmx, "Drafts"},
		{"Drafts:7", d.Primary, d.Primary, "INBOX/Drafts"},
		{"inbox:7", gmx, gmx, "INBOX"},
	} {
		a, f, uid, err := d.resolveIDOn(tc.id, tc.home, draftsBox)
		if err != nil || a != tc.want || f != tc.folder || uid != 7 {
			t.Errorf("%q from %s = %s %s:%d (%v), want %s %s:7", tc.id, tc.home.Name, nameOf(a), f, uid, err, tc.want.Name, tc.folder)
		}
	}

	none := NewAccount("bare", nil, nil, []string{"INBOX"}, nil)
	if _, _, _, err := d.resolveIDOn("7", none, draftsBox); err == nil {
		t.Error("a bare uid on an account with no Drafts was read anyway")
	}
	if _, _, _, err := d.resolveIDOn("inbox:7", none, draftsBox); err != nil {
		t.Errorf("a Box named outright needs no Drafts: %v", err)
	}
}

func nameOf(a *Account) string {
	if a == nil {
		return "<no account>"
	}
	return a.Name
}
