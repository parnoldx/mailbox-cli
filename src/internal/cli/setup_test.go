package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailbox/src/internal/dav"
	"mailbox/src/internal/format"
)

func TestNamedHref(t *testing.T) {
	rows := []dav.Response{
		{Href: "/caldav/", Data: "root"},
		{Href: "/caldav/KAL/", Data: "Kalender"},
		{Href: "/caldav/TODO/", Data: "Aufgaben"},
	}
	base := "https://dav.mailbox.org/caldav/"
	if u := namedHref(rows, base, "Kalender"); u != "https://dav.mailbox.org/caldav/KAL/" {
		t.Fatalf("kalender %q", u)
	}
	if u := namedHref(rows, base, "Aufgaben"); u != "https://dav.mailbox.org/caldav/TODO/" {
		t.Fatalf("aufgaben %q", u)
	}
	if u := namedHref(rows, base, "Nope"); u != "" {
		t.Fatalf("missing %q", u)
	}
}

func TestYesNoDefault(t *testing.T) {
	w := scripted("\n")
	if !w.yesNo("ok?", true) {
		t.Fatal("enter should keep default true")
	}
	w = scripted("n\n")
	if w.yesNo("ok?", true) {
		t.Fatal("n should be false")
	}
}

func TestCollectManualAppPasswords(t *testing.T) {
	defer func() { discoverDAV = discoverDAVLive }()
	discoverDAV = func(string, string) (string, string, string) {
		return "https://dav.mailbox.org/caldav/KAL/", "https://dav.mailbox.org/caldav/TODO/", "https://dav.mailbox.org/carddav/32/"
	}
	w := scripted("user@mailbox.org\ny\nimap-secret\ndav-secret\n")
	vals, err := w.collectManual()
	if err != nil {
		t.Fatal(err)
	}
	if vals["MAILBOX_EMAIL"] != "user@mailbox.org" {
		t.Fatalf("email %q", vals["MAILBOX_EMAIL"])
	}
	if vals["MAILBOX_PASSWORD"] != "imap-secret" || vals["MAILBOX_DAV_PASSWORD"] != "dav-secret" {
		t.Fatalf("passwords %+v", vals)
	}
	if vals["MAILBOX_CALDAV_KALENDER"] != "https://dav.mailbox.org/caldav/KAL/" {
		t.Fatalf("kalender %q", vals["MAILBOX_CALDAV_KALENDER"])
	}
}

func TestCollectManualOnePassword(t *testing.T) {
	defer func() { discoverDAV = discoverDAVLive }()
	discoverDAV = func(string, string) (string, string, string) {
		return "https://dav.mailbox.org/caldav/KAL/", "https://dav.mailbox.org/caldav/TODO/", "https://dav.mailbox.org/carddav/32/"
	}
	w := scripted("user@mailbox.org\nn\nsecret\n")
	vals, err := w.collectManual()
	if err != nil {
		t.Fatal(err)
	}
	if vals["MAILBOX_PASSWORD"] != "secret" {
		t.Fatalf("password %q", vals["MAILBOX_PASSWORD"])
	}
	if _, ok := vals["MAILBOX_DAV_PASSWORD"]; ok {
		t.Fatal("same password should omit DAV")
	}
}

func TestCollectThunderbirdPinsProfile(t *testing.T) {
	home := t.TempDir()
	prof := filepath.Join(home, "Profiles", "abcd.default")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := `user_pref("mail.server.server2.hostname", "imap.mailbox.org");
user_pref("mail.server.server2.port", 993);
user_pref("mail.server.server2.type", "imap");
user_pref("mail.server.server2.userName", "user@mailbox.org");
user_pref("calendar.registry.aaa.name", "Kalender");
user_pref("calendar.registry.aaa.type", "caldav");
user_pref("calendar.registry.aaa.uri", "https://dav.mailbox.org/caldav/KAL/");
user_pref("calendar.registry.bbb.name", "Aufgaben");
user_pref("calendar.registry.bbb.type", "caldav");
user_pref("calendar.registry.bbb.uri", "https://dav.mailbox.org/caldav/TODO/");
user_pref("ldap_2.servers.Kontakte.carddav.url", "https://dav.mailbox.org/carddav/32/");
user_pref("ldap_2.servers.Kontakte.description", "Kontakte");
user_pref("ldap_2.servers.Kontakte.dirType", 102);
`
	if err := os.WriteFile(filepath.Join(prof, "prefs.js"), []byte(prefs), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILBOX_TB_HOME", home)
	w := scripted("y\nn\nimap-pw\n")
	vals, err := w.collectThunderbird([]string{prof})
	if err != nil {
		t.Fatal(err)
	}
	if vals["MAILBOX_TB_PROFILE"] != prof {
		t.Fatalf("profile %q", vals["MAILBOX_TB_PROFILE"])
	}
	if vals["MAILBOX_PASSWORD"] != "imap-pw" {
		t.Fatalf("password %+v", vals)
	}
}

func TestSetupInteractiveOffForJSON(t *testing.T) {
	if setupInteractive(&format.Output{JSON: true}) {
		t.Fatal("json should skip wizard")
	}
}

func scripted(input string) *wizard {
	return &wizard{in: strings.NewReader(input), out: &bytes.Buffer{}, br: bufio.NewReader(strings.NewReader(input))}
}
