package config

import (
	"os"
	"testing"

	"mailbox/src/internal/thunderbird"
)

func isolateEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if len(kv) > 8 && kv[:8] == "MAILBOX_" {
			i := indexByte(kv, '=')
			os.Unsetenv(kv[:i])
		}
	}
	t.Cleanup(func() {})
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func setTBError(t *testing.T) {
	t.Helper()
	LoadThunderbirdHook = func(string) (*thunderbird.Account, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { LoadThunderbirdHook = nil })
}

func TestMailAccountDoesNotNeedCalDAV(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "secret")
	defer os.Unsetenv("MAILBOX_EMAIL")
	defer os.Unsetenv("MAILBOX_PASSWORD")

	acc, err := LoadAccount(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Email != "user@mailbox.org" {
		t.Fatal("email mismatch")
	}
	if acc.KalenderURL != "" || acc.AufgabenURL != "" || acc.KontakteURL != "" {
		t.Fatal("unexpected URLs")
	}
}

func TestContactsNeedCardDAV(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "secret")
	defer os.Unsetenv("MAILBOX_EMAIL")
	defer os.Unsetenv("MAILBOX_PASSWORD")

	_, err := LoadAccount(false, true)
	if err == nil {
		t.Fatal("expected missing CARDDAV error")
	}
	me, ok := err.(*MissingCredsError)
	if !ok || !contains(me.Msg, "MAILBOX_CARDDAV_KONTAKTE") {
		t.Fatalf("err=%v", err)
	}
}

func TestContactsEnvURL(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "secret")
	os.Setenv("MAILBOX_CARDDAV_KONTAKTE", "https://dav.mailbox.org/carddav/32/")
	defer func() {
		os.Unsetenv("MAILBOX_EMAIL")
		os.Unsetenv("MAILBOX_PASSWORD")
		os.Unsetenv("MAILBOX_CARDDAV_KONTAKTE")
	}()

	acc, err := LoadAccount(false, true)
	if err != nil || acc.KontakteURL != "https://dav.mailbox.org/carddav/32/" {
		t.Fatalf("acc=%+v err=%v", acc, err)
	}
}

func TestDAVPasswordFromEnv(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "imap-secret")
	os.Setenv("MAILBOX_DAV_PASSWORD", "dav-secret")
	os.Setenv("MAILBOX_CALDAV_KALENDER", "https://dav.mailbox.org/caldav/KAL/")
	os.Setenv("MAILBOX_CALDAV_AUFGABEN", "https://dav.mailbox.org/caldav/TODO/")
	defer func() {
		os.Unsetenv("MAILBOX_EMAIL")
		os.Unsetenv("MAILBOX_PASSWORD")
		os.Unsetenv("MAILBOX_DAV_PASSWORD")
		os.Unsetenv("MAILBOX_CALDAV_KALENDER")
		os.Unsetenv("MAILBOX_CALDAV_AUFGABEN")
	}()

	acc, err := LoadAccount(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Password != "imap-secret" || acc.DAVPass() != "dav-secret" {
		t.Fatalf("password=%q dav=%q", acc.Password, acc.DAVPass())
	}
}

func TestDAVPasswordFallsBackToIMAP(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "secret")
	defer os.Unsetenv("MAILBOX_EMAIL")
	defer os.Unsetenv("MAILBOX_PASSWORD")

	acc, err := LoadAccount(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.DAVPass() != "secret" {
		t.Fatalf("dav=%q", acc.DAVPass())
	}
}

func TestDAVPasswordFromThunderbird(t *testing.T) {
	isolateEnv(t)
	LoadThunderbirdHook = func(string) (*thunderbird.Account, error) {
		return &thunderbird.Account{
			Email:       "user@mailbox.org",
			Password:    "imap-pw",
			DAVPassword: "dav-pw",
			KalenderURL: "https://dav.mailbox.org/caldav/KAL/",
			AufgabenURL: "https://dav.mailbox.org/caldav/TODO/",
		}, nil
	}
	t.Cleanup(func() { LoadThunderbirdHook = nil })

	acc, err := LoadAccount(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Password != "imap-pw" || acc.DAVPass() != "dav-pw" {
		t.Fatalf("password=%q dav=%q", acc.Password, acc.DAVPass())
	}
}

func TestEventsStillNeedCalDAV(t *testing.T) {
	isolateEnv(t)
	setTBError(t)
	os.Setenv("MAILBOX_EMAIL", "user@mailbox.org")
	os.Setenv("MAILBOX_PASSWORD", "secret")
	defer os.Unsetenv("MAILBOX_EMAIL")
	defer os.Unsetenv("MAILBOX_PASSWORD")

	_, err := LoadAccount(true, false)
	me, ok := err.(*MissingCredsError)
	if !ok || !contains(me.Msg, "MAILBOX_CALDAV_KALENDER") {
		t.Fatalf("err=%v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
