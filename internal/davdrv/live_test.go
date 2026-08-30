//go:build live

// Live conformance run against the real DAV server. It reads and never writes.
//
//	go test -tags live ./internal/davdrv/ -v
//
// The question it answers is the one a fake cannot: does this server enumerate
// itself the way the RFCs say, and does sync-collection carry the objects in
// the same round trip?
package davdrv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"mailbox/internal/vcal"
)

type liveConfig struct {
	Account struct {
		Email       string
		Password    string
		DAVPassword string `toml:"dav_password"`
		DAVEndpoint string `toml:"dav_endpoint"`
	}
}

func loadLive(t *testing.T) liveConfig {
	t.Helper()
	path := os.Getenv("MAILBOX_CONFIG")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "mailbox", "config.toml")
	}
	var c liveConfig
	if _, err := toml.DecodeFile(path, &c); err != nil {
		t.Skipf("no live config at %s: %v", path, err)
	}
	if c.Account.Email == "" {
		t.Skip("no account in config")
	}
	if c.Account.DAVPassword == "" {
		c.Account.DAVPassword = c.Account.Password
	}
	if c.Account.DAVEndpoint == "" {
		c.Account.DAVEndpoint = "https://dav.mailbox.org/"
	}
	return c
}

func liveClient(t *testing.T) *Client {
	t.Helper()
	cfg := loadLive(t)
	return New(Config{
		Endpoint: cfg.Account.DAVEndpoint,
		Username: cfg.Account.Email, Password: cfg.Account.DAVPassword,
	})
}

func TestLiveDiscovery(t *testing.T) {
	c := liveClient(t)
	cols, err := c.Collections(context.Background())
	if err != nil {
		t.Fatalf("collections: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("the server offered no collections at all")
	}
	kinds := map[string]int{}
	for _, col := range cols {
		kinds[col.Kind]++
		t.Logf("%-8s %-28s %s", col.Kind, col.Name, col.URL)
		if col.Name == "" {
			t.Errorf("collection %s has no display name — it is what a caller has to type", col.URL)
		}
		if !strings.HasPrefix(col.URL, "http") {
			t.Errorf("collection url is not absolute: %q", col.URL)
		}
	}
	// This is what discovery is for: the address book is found by asking, not
	// by the URL that has been quietly wrong in the old config for years.
	if kinds["events"] == 0 {
		t.Error("no calendar was discovered")
	}
	if kinds["cards"] == 0 {
		t.Log("note: no address book on this endpoint")
	}
}

func TestLiveSyncCollection(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	cols, err := c.Collections(ctx)
	if err != nil {
		t.Fatalf("collections: %v", err)
	}
	var cal string
	for _, col := range cols {
		if col.Kind == "events" {
			cal = col.URL
			t.Logf("syncing %q", col.Name)
			break
		}
	}
	if cal == "" {
		t.Skip("no calendar to sync")
	}

	first, err := c.Sync(ctx, cal, "")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.Token == "" {
		t.Fatal("no sync token: every later sync would be a full one")
	}
	t.Logf("gate: %d objects and a token in one request", len(first.Items))

	withData := 0
	for _, it := range first.Items {
		if it.Data != "" {
			withData++
		}
	}
	if len(first.Items) > 0 && withData == 0 {
		t.Error("the server sent no calendar-data: every sync would need a second round trip")
	}

	// The projection has to survive real calendar entries, not just the ones a
	// test would write.
	parsed, recurring := 0, 0
	for _, it := range first.Items {
		if it.Data == "" {
			continue
		}
		p, err := vcal.Parse(it.Data, time.Local)
		if err != nil {
			t.Errorf("%s does not parse: %v", it.Href, err)
			continue
		}
		parsed++
		if p.Recurring {
			recurring++
		}
		if p.Kind == vcal.KindEvent && p.Start.IsZero() {
			t.Errorf("%s (%q) has no start", it.Href, p.Summary)
		}
	}
	t.Logf("gate: %d objects projected, %d of them repeating", parsed, recurring)

	// A second sync with the token has nothing to say, which is the whole point
	// of the token.
	again, err := c.Sync(ctx, cal, first.Token)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(again.Items) != 0 {
		t.Logf("note: the second sync reported %d changes (something moved, or this server always reports)", len(again.Items))
	}
	if again.Token == "" {
		t.Error("the second sync returned no token")
	}
	t.Logf("gate: an unchanged collection costs one request and %d objects", len(again.Items))
}

func TestLiveMultiGet(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	cols, err := c.Collections(ctx)
	if err != nil {
		t.Fatalf("collections: %v", err)
	}
	var cal string
	for _, col := range cols {
		if col.Kind == "events" {
			cal = col.URL
			break
		}
	}
	if cal == "" {
		t.Skip("no calendar")
	}
	changes, err := c.Sync(ctx, cal, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(changes.Items) == 0 {
		t.Skip("the calendar is empty")
	}
	hrefs := []string{changes.Items[0].Href}
	got, err := c.MultiGet(ctx, cal, hrefs)
	if err != nil {
		t.Fatalf("multiget: %v", err)
	}
	if len(got) != 1 || got[0].Data == "" {
		t.Fatalf("multiget returned %+v", got)
	}
	t.Logf("gate: multiget returned %d bytes for %s", len(got[0].Data), got[0].Href)
}
