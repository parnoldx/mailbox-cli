package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const prefs = `
user_pref("mail.server.server2.hostname", "imap.mailbox.org");
user_pref("mail.server.server2.port", 993);
user_pref("mail.server.server2.type", "imap");
user_pref("mail.server.server2.userName", "user@mailbox.org");
user_pref("mail.server.server3.type", "none");
user_pref("mail.server.server3.hostname", "Local Folders");
user_pref("mail.identity.id2.fullName", "Test User");
user_pref("calendar.registry.aaa.name", "Kalender");
user_pref("calendar.registry.aaa.type", "caldav");
user_pref("calendar.registry.aaa.uri", "https://dav.mailbox.org/caldav/KAL/");
user_pref("calendar.registry.bbb.name", "Aufgaben");
user_pref("calendar.registry.bbb.type", "caldav");
user_pref("calendar.registry.bbb.uri", "https://dav.mailbox.org/caldav/TODO/");
user_pref("calendar.registry.ccc.name", "Privat");
user_pref("calendar.registry.ccc.type", "storage");
user_pref("calendar.registry.ccc.disabled", true);
user_pref("ldap_2.servers.Kontakte.carddav.url", "https://dav.mailbox.org/carddav/32/");
user_pref("ldap_2.servers.Kontakte.description", "Kontakte");
user_pref("ldap_2.servers.Kontakte.dirType", 102);
user_pref("ldap_2.servers.pab.description", "Personal Address Book");
user_pref("ldap_2.servers.pab.dirType", 101);
`

func TestParseIMAPAndCalDAV(t *testing.T) {
	acc := accountFromPrefs(ParsePrefs(prefs))
	if acc.Email != "user@mailbox.org" || acc.IMAPHost != "imap.mailbox.org" || acc.IMAPPort != 993 {
		t.Fatalf("acc=%+v", acc)
	}
	if acc.DisplayName != "Test User" {
		t.Fatalf("name %q", acc.DisplayName)
	}
	if acc.KalenderURL != "https://dav.mailbox.org/caldav/KAL/" || acc.AufgabenURL != "https://dav.mailbox.org/caldav/TODO/" {
		t.Fatalf("caldav %+v", acc)
	}
	if acc.KontakteURL != "https://dav.mailbox.org/carddav/32/" {
		t.Fatalf("carddav %q", acc.KontakteURL)
	}
}

func TestLaterIMAPServerDoesNotReplaceMailboxOrg(t *testing.T) {
	extra := `
user_pref("mail.server.server9.type", "imap");
user_pref("mail.server.server9.hostname", "imap.gmail.com");
user_pref("mail.server.server9.userName", "other@gmail.com");
user_pref("mail.server.server9.port", 993);
`
	acc := accountFromPrefs(ParsePrefs(prefs + extra))
	if acc.IMAPHost != "imap.mailbox.org" || acc.Email != "user@mailbox.org" || acc.IMAPPort != 993 {
		t.Fatalf("acc=%+v", acc)
	}
}

func TestPickLoginBlobsPrefersIMAPAndDAVSeparately(t *testing.T) {
	imapBlob, davBlob := pickLoginBlobs([]loginEntry{
		{Hostname: "imap://imap.mailbox.org", EncryptedPassword: "imap-enc"},
		{Hostname: "smtp://smtp.mailbox.org", EncryptedPassword: "smtp-enc"},
		{Hostname: "https://dav.mailbox.org", EncryptedPassword: "dav-enc"},
	})
	if imapBlob != "imap-enc" {
		t.Fatalf("imap blob %q", imapBlob)
	}
	if davBlob != "dav-enc" {
		t.Fatalf("dav blob %q", davBlob)
	}
}

func TestPickLoginBlobsIMAPOnlyLeavesDAVEmpty(t *testing.T) {
	imapBlob, davBlob := pickLoginBlobs([]loginEntry{
		{Hostname: "imap://imap.mailbox.org", EncryptedPassword: "imap-enc"},
	})
	if imapBlob != "imap-enc" || davBlob != "" {
		t.Fatalf("imap=%q dav=%q", imapBlob, davBlob)
	}
}

func TestListProfilesNewestFirst(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "Profiles", "old.default")
	newp := filepath.Join(home, "Profiles", "new.default")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newp, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPrefs := filepath.Join(old, "prefs.js")
	newPrefs := filepath.Join(newp, "prefs.js")
	if err := os.WriteFile(oldPrefs, []byte("user_pref(\"x\", 1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPrefs, []byte("user_pref(\"x\", 2);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(oldPrefs, time.Unix(1, 0), time.Unix(1, 0))
	_ = os.Chtimes(newPrefs, time.Unix(2, 0), time.Unix(2, 0))
	got := ListProfiles(home)
	if len(got) != 2 || got[0] != newp || got[1] != old {
		t.Fatalf("%q", got)
	}
}

func TestDisplayNameMatchesMailboxIdentity(t *testing.T) {
	extra := `
user_pref("mail.identity.id2.useremail", "user@mailbox.org");
user_pref("mail.identity.id9.fullName", "Gmail Me");
user_pref("mail.identity.id9.useremail", "other@gmail.com");
`
	acc := accountFromPrefs(ParsePrefs(prefs + extra))
	if acc.Email != "user@mailbox.org" {
		t.Fatalf("email %q", acc.Email)
	}
	if acc.DisplayName != "Test User" {
		t.Fatalf("name %q", acc.DisplayName)
	}
}
