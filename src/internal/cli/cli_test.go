package cli

import (
	"strings"
	"testing"

	"mailbox/src/internal/folders"
)

func TestHelpRoot(t *testing.T) {
	text := helpText(nil)
	for _, want := range []string{"MAIL", "box", "draft", "mailbox <command> --help"} {
		if !strings.Contains(text, want) {
			t.Fatalf("root help missing %q", want)
		}
	}
	if strings.Contains(text, "--starts-on") {
		t.Fatal("root help should not dump leaf flags")
	}
}

func TestHelpDraftGroup(t *testing.T) {
	text := helpText([]string{"draft"})
	for _, want := range []string{"list", "show", "edit", "send", "delete"} {
		if !strings.Contains(text, want) {
			t.Fatalf("draft help missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "--limit") {
		t.Fatal("group help should not dump list flags")
	}
}

func TestHelpDraftList(t *testing.T) {
	text := helpText([]string{"draft", "list"})
	if !strings.Contains(text, "--limit") || !strings.Contains(text, "page size") {
		t.Fatalf("leaf help:\n%s", text)
	}
}

func TestHelpSearchHybrid(t *testing.T) {
	text := helpText([]string{"search"})
	if !strings.Contains(text, "filters") || !strings.Contains(text, "--from") {
		t.Fatalf("search help:\n%s", text)
	}
}

func TestHelpEventGroup(t *testing.T) {
	text := helpText([]string{"event"})
	if !strings.Contains(text, "list") || !strings.Contains(text, "add") {
		t.Fatalf("event help:\n%s", text)
	}
	if strings.Contains(text, "--starts-on") {
		t.Fatal("event group should not dump list flags")
	}
}

func TestHelpUnknown(t *testing.T) {
	if helpText([]string{"nope"}) != "" {
		t.Fatal("unknown command should have empty help")
	}
}

func TestDraftIDBareUIDIsDrafts(t *testing.T) {
	folder, uid, err := draftID("920")
	if err != nil || folder != folders.DRAFTS || uid != "920" {
		t.Fatalf("draftID(920) = %q,%q,%v want Drafts,920", folder, uid, err)
	}
}

func TestDraftIDPrefixed(t *testing.T) {
	for _, in := range []string{"drafts:920", "Drafts:920"} {
		folder, uid, err := draftID(in)
		if err != nil || folder != folders.DRAFTS || uid != "920" {
			t.Fatalf("draftID(%q) = %q,%q,%v want Drafts,920", in, folder, uid, err)
		}
	}
}

func TestDraftIDRejectsOtherBox(t *testing.T) {
	for _, in := range []string{"inbox:920", "feed:9"} {
		if _, _, err := draftID(in); err == nil {
			t.Fatalf("draftID(%q) should reject a non-Drafts box", in)
		}
	}
}
