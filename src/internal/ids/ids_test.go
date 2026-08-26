package ids

import "testing"

func TestParseMessageID(t *testing.T) {
	cases := []struct {
		in, folder, uid string
		bad             bool
	}{
		{"36722", folders_INBOX(), "36722", false},
		{"feed:2431", "INBOX/Feed", "2431", false},
		{"trail:9", "INBOX/Paper Trail", "9", false},
		{"inbox:5", "INBOX", "5", false},
		{"Archive/Immo:4", "Archive/Immo", "4", false},
		{"drafts:12", "Drafts", "12", false},
		{"", "", "", true},
		{"abc", "", "", true},
		{"feed:x", "", "", true},
	}
	for _, c := range cases {
		folder, uid, err := ParseMessageID(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseMessageID(%q) = %q,%q want error", c.in, folder, uid)
			}
			continue
		}
		if err != nil || folder != c.folder || uid != c.uid {
			t.Errorf("ParseMessageID(%q) = %q,%q,%v want %q,%q", c.in, folder, uid, err, c.folder, c.uid)
		}
	}
}

func folders_INBOX() string { return "INBOX" }

func TestFormatRoundTrip(t *testing.T) {
	for _, id := range []string{"36722", "feed:2431", "trail:9", "Archive/Immo:4", "drafts:12"} {
		folder, uid, err := ParseMessageID(id)
		if err != nil {
			t.Fatalf("parse %q: %v", id, err)
		}
		if got := FormatMessageID(folder, uid); got != id {
			t.Errorf("round trip: %q -> %q", id, got)
		}
	}
}

func TestParseMessageIDInBareUIDUsesDefaultFolder(t *testing.T) {
	folder, uid, err := ParseMessageIDIn("920", "Drafts")
	if err != nil || folder != "Drafts" || uid != "920" {
		t.Errorf("ParseMessageIDIn(%q, Drafts) = %q,%q,%v want Drafts,920", "920", folder, uid, err)
	}
	folder, uid, err = ParseMessageIDIn("drafts:920", "Drafts")
	if err != nil || folder != "Drafts" || uid != "920" {
		t.Errorf("prefixed still parses: %q,%q,%v", folder, uid, err)
	}
	folder, uid, err = ParseMessageIDIn("inbox:920", "Drafts")
	if err != nil || folder != "INBOX" || uid != "920" {
		t.Errorf("explicit inbox is still inbox: %q,%q,%v", folder, uid, err)
	}
}

func TestParseAttachmentID(t *testing.T) {
	folder, uid, n, err := ParseAttachmentID("36722:1")
	if err != nil || folder != "INBOX" || uid != "36722" || n != 1 {
		t.Errorf("got %q %q %d %v", folder, uid, n, err)
	}
	folder, uid, n, err = ParseAttachmentID("feed:2431:2")
	if err != nil || folder != "INBOX/Feed" || uid != "2431" || n != 2 {
		t.Errorf("got %q %q %d %v", folder, uid, n, err)
	}
}

func TestInboxThreadKeyIsNotFolderColonUID(t *testing.T) {
	if FormatMessageID("INBOX", "36635") != "36635" {
		t.Fatal("inbox display id is the bare uid")
	}
	if FormatMessageID("INBOX/Feed", "12") != "feed:12" {
		t.Fatal("aliased folders use display alias, not IMAP path")
	}
}
