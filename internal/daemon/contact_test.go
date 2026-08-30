package daemon

import (
	"context"
	"strings"
	"testing"

	"mailbox/internal/sync/davsync"
)

const (
	testBookURL   = "https://dav.example.org/carddav/kontakte/"
	testGlobalURL = "https://dav.example.org/carddav/global/"
)

func card(uid, name, email, phone string) string {
	return "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + name +
		"\r\nEMAIL:" + email + "\r\nTEL:" + phone + "\r\nEND:VCARD\r\n"
}

// seedContacts adds an address book with two people in it to a task-seeded
// Daemon.
func seedContacts(t *testing.T, books ...davsync.Collection) (*Daemon, *davsync.Fake) {
	t.Helper()
	d, f, _ := seedTasks(t)
	f.AddCollection(davsync.Collection{Kind: "cards", URL: testBookURL, Name: "Kontakte"})
	for _, b := range books {
		f.AddCollection(b)
	}
	f.Deliver(testBookURL, "/carddav/kontakte/kaethe.vcf",
		card("kaethe-1", "Käthe Groß", "kaethe@example.com", "+49 170 1234567"))
	f.Deliver(testBookURL, "/carddav/kontakte/hans.vcf",
		card("hans-1", "Hans Meier", "hans@example.org", "+49 30 999"))
	if _, err := d.DAV.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DAV.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d, f
}

func contacts(t *testing.T, d *Daemon, args map[string]any) []contact {
	t.Helper()
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "search"}, Args: args})
	if !resp.OK {
		t.Fatalf("contact search: %s (%s)", resp.Error, resp.Code)
	}
	out, ok := resp.Data.([]contact)
	if !ok {
		t.Fatalf("contact search returned %T", resp.Data)
	}
	return out
}

func TestContactsAreFoundByAnyPartOfTheCard(t *testing.T) {
	d, _ := seedContacts(t)

	// The projection is what makes this a Mirror read: no card is parsed here.
	for _, query := range []string{"Käthe", "kaethe@example.com", "1234567", "groß"} {
		got := contacts(t, d, map[string]any{"positional": query})
		if len(got) != 1 || got[0].Name != "Käthe Groß" {
			t.Fatalf("%q found %+v", query, got)
		}
	}
	// Terms are ANDed, so two halves of one person find that person and two
	// halves of two people find nobody.
	if got := contacts(t, d, map[string]any{"positional": "Käthe example.com"}); len(got) != 1 {
		t.Fatalf("both terms should match one card: %+v", got)
	}
	if got := contacts(t, d, map[string]any{"positional": "Käthe Hans"}); len(got) != 0 {
		t.Fatalf("nobody is both: %+v", got)
	}
	// An empty query is the whole book.
	if got := contacts(t, d, nil); len(got) != 2 {
		t.Fatalf("all = %+v", got)
	}
}

func TestAddingAContactWritesACardTheServerKeeps(t *testing.T) {
	d, f := seedContacts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "add"},
		Args: map[string]any{
			"positional": "Marie Kurz", "email": []string{"marie@example.com"},
			"phone": []string{"+49 40 555"}, "org": "Kurz & Co",
		}})
	if !resp.OK {
		t.Fatalf("contact add: %s (%s)", resp.Error, resp.Code)
	}
	added := resp.Data.(contact)
	if added.Name != "Marie Kurz" || len(added.Emails) != 1 || added.Organisation != "Kurz & Co" {
		t.Fatalf("added = %+v", added)
	}
	// Written as a vCard under a .vcf href, which is what a CardDAV server and
	// every other client expect.
	changes, err := f.Sync(context.Background(), testBookURL, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range changes.Items {
		if strings.Contains(it.Data, "Marie Kurz") {
			found = true
			if !strings.HasSuffix(it.Href, ".vcf") {
				t.Errorf("href = %q", it.Href)
			}
			if !strings.Contains(it.Data, "BEGIN:VCARD") {
				t.Errorf("what was written is not a vCard:\n%s", it.Data)
			}
		}
	}
	if !found {
		t.Fatal("the card is not on the server")
	}
	// And it is findable straight away, with no cycle in between.
	if got := contacts(t, d, map[string]any{"positional": "marie@example.com"}); len(got) != 1 {
		t.Fatalf("search after add = %+v", got)
	}
}

func TestAPhoneNumberWithSpacesIsOneNumber(t *testing.T) {
	d, _ := seedContacts(t)
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "add"},
		Args: map[string]any{"positional": "Marie Kurz", "phone": []string{"+49 40 555 6677"}}})
	if !resp.OK {
		t.Fatalf("contact add: %s", resp.Error)
	}
	// Stored space-separated, this came back as four phone numbers.
	if got := resp.Data.(contact); len(got.Phones) != 1 || got.Phones[0] != "+49 40 555 6677" {
		t.Fatalf("phones = %q", got.Phones)
	}
	found := contacts(t, d, map[string]any{"positional": "555 6677"})
	if len(found) != 1 || len(found[0].Phones) != 1 {
		t.Fatalf("search = %+v", found)
	}
}

func TestAnAddressCanBeAddedToSomebodyWeAlreadyHave(t *testing.T) {
	d, _ := seedContacts(t)
	who := contacts(t, d, map[string]any{"positional": "Hans"})[0]

	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "email"},
		Args: map[string]any{"positional": who.ID, "value": "hans.meier@arbeit.example"}})
	if !resp.OK {
		t.Fatalf("contact email: %s (%s)", resp.Error, resp.Code)
	}
	got := resp.Data.(contact)
	if len(got.Emails) != 2 {
		t.Fatalf("emails = %v", got.Emails)
	}
	// The old address still finds him: an edit adds, it does not replace.
	if found := contacts(t, d, map[string]any{"positional": "hans@example.org"}); len(found) != 1 {
		t.Fatalf("search = %+v", found)
	}
}

func TestSeveralAddressBooksHaveToBeNamed(t *testing.T) {
	d, _ := seedContacts(t, davsync.Collection{Kind: "cards", URL: testGlobalURL, Name: "Globales Adressbuch"})
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "add"},
		Args: map[string]any{"positional": "Marie Kurz"}})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(resp.Error, "Kontakte") || !strings.Contains(resp.Error, "Globales") {
		t.Fatalf("error = %q", resp.Error)
	}
	d.AddressBook = "Kontakte"
	resp = d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "add"},
		Args: map[string]any{"positional": "Marie Kurz"}})
	if !resp.OK {
		t.Fatalf("with a default book: %s", resp.Error)
	}
	if got := resp.Data.(contact); got.Book != "Kontakte" {
		t.Fatalf("added to %q", got.Book)
	}
}

func TestDroppingAContactRemovesIt(t *testing.T) {
	d, _ := seedContacts(t)
	who := contacts(t, d, map[string]any{"positional": "Hans"})[0]
	resp := d.handle(context.Background(), Request{ID: "1", Cmd: []string{"contact", "drop"},
		Args: map[string]any{"positional": who.ID}})
	if !resp.OK {
		t.Fatalf("contact drop: %s", resp.Error)
	}
	if got := contacts(t, d, map[string]any{"positional": "Hans"}); len(got) != 0 {
		t.Fatalf("still there: %+v", got)
	}
}

// An update changes what the card says. Addresses and numbers are not touched:
// somebody fixing a spelling should not lose the email they were writing to.
func TestContactUpdateChangesOnlyWhatItWasGiven(t *testing.T) {
	d, _ := seedContacts(t)
	added := mustAsk(t, d, []string{"contact", "add"}, map[string]any{
		"positional": "Anna Beispiel", "email": []any{"anna@example.com"},
	}).Data.(contact)

	mustAsk(t, d, []string{"contact", "update"},
		map[string]any{"positional": added.ID, "org": "Beispiel GmbH"})

	got := mustAsk(t, d, []string{"contact", "view"},
		map[string]any{"positional": added.ID}).Data.(contact)
	if got.Organisation != "Beispiel GmbH" {
		t.Errorf("org = %q", got.Organisation)
	}
	if got.Name != "Anna Beispiel" {
		t.Errorf("the update changed the name to %q", got.Name)
	}
	if len(got.Emails) != 1 || got.Emails[0] != "anna@example.com" {
		t.Errorf("the update lost the address: %v", got.Emails)
	}
}

// An update that names nothing is a mistake, not a write that reports success
// and changed nothing.
func TestContactUpdateNeedsSomethingToChange(t *testing.T) {
	d, _ := seedContacts(t)
	added := mustAsk(t, d, []string{"contact", "add"},
		map[string]any{"positional": "Anna Beispiel"}).Data.(contact)
	resp := ask(t, d, []string{"contact", "update"}, map[string]any{"positional": added.ID})
	if resp.OK || resp.Code != "usage" {
		t.Fatalf("resp = %+v", resp)
	}
}
