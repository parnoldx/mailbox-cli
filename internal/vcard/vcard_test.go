package vcard

import (
	"strings"
	"testing"
)

const real = `BEGIN:VCARD
VERSION:3.0
UID:abc-123
FN:Käthe Groß
N:Groß;Käthe;;;
ORG:Beispiel GmbH
EMAIL;TYPE=WORK:kaethe@example.com
EMAIL;TYPE=HOME:kaethe@privat.example
TEL;TYPE=CELL:+49 170 1234567
NOTE:Kennt sich mit Dach aus.
END:VCARD
`

func TestParseTakesEverythingWorthSearching(t *testing.T) {
	c, err := Parse(real)
	if err != nil {
		t.Fatal(err)
	}
	if c.UID != "abc-123" || c.Name != "Käthe Groß" || c.Organisation != "Beispiel GmbH" {
		t.Fatalf("contact = %+v", c)
	}
	if len(c.Emails) != 2 || c.Emails[0] != "kaethe@example.com" {
		t.Fatalf("emails = %v", c.Emails)
	}
	if len(c.Phones) != 1 || c.Phones[0] != "+49 170 1234567" {
		t.Fatalf("phones = %v", c.Phones)
	}
	if !strings.Contains(c.Note, "Dach") {
		t.Fatalf("note = %q", c.Note)
	}
}

func TestACardWithNoFormattedNameStillHasOne(t *testing.T) {
	c, err := Parse("BEGIN:VCARD\nVERSION:3.0\nUID:x\nN:Meier;Hans;;;\nEND:VCARD\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Hans Meier" {
		t.Fatalf("name = %q — an export with no FN is not a nameless person", c.Name)
	}
}

func TestNewCardComesBackAsWhatWasPutIn(t *testing.T) {
	raw, err := New("uid-1", "Käthe Groß", []string{"kaethe@example.com", ""}, []string{"+49 170 1234567"},
		"Beispiel GmbH", "kennt sich aus")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatalf("what we wrote does not parse: %v\n%s", err, raw)
	}
	if c.Name != "Käthe Groß" || c.UID != "uid-1" {
		t.Fatalf("contact = %+v", c)
	}
	// An empty address given by a caller is not an address.
	if len(c.Emails) != 1 || len(c.Phones) != 1 {
		t.Fatalf("emails=%v phones=%v", c.Emails, c.Phones)
	}
	if _, err := New("uid-2", "  ", nil, nil, "", ""); err == nil {
		t.Fatal("a contact with no name is not a contact")
	}
}

func TestEditingKeepsWhatItDidNotTouch(t *testing.T) {
	raw, err := AddEmail(real, "kaethe@neu.example")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Emails) != 3 {
		t.Fatalf("emails = %v", c.Emails)
	}
	// The note and the organisation belong to whoever wrote them.
	if c.Organisation != "Beispiel GmbH" || !strings.Contains(c.Note, "Dach") {
		t.Fatalf("an edit dropped fields it was not asked about: %+v", c)
	}
	raw, err = AddPhone(raw, "+49 30 999")
	if err != nil {
		t.Fatal(err)
	}
	c, _ = Parse(raw)
	if len(c.Phones) != 2 {
		t.Fatalf("phones = %v", c.Phones)
	}
}
