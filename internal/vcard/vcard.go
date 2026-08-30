// Package vcard reads and writes the contacts. Like the calendar side, the raw
// card is the record and everything here is a projection of it (ADR-0010).
package vcard

import (
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"
)

// Contact is what the Mirror stores beside a card so that searching and listing
// never have to parse vCard.
type Contact struct {
	UID          string
	Name         string
	Organisation string
	Title        string
	Note         string
	Emails       []string
	Phones       []string
	// Kind is "individual" or "group"; a group has members and no address of
	// its own, and a caller looking for a person should not be given one.
	Kind string
}

// Parse projects one card. A card that will not parse still belongs in the
// Mirror — the raw is the record — so callers store it either way.
func Parse(raw string) (Contact, error) {
	card, err := vcard.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		return Contact{}, fmt.Errorf("vcard: %w", err)
	}
	c := Contact{
		UID:          card.Value(vcard.FieldUID),
		Name:         strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)),
		Organisation: card.PreferredValue(vcard.FieldOrganization),
		Title:        card.PreferredValue(vcard.FieldTitle),
		Note:         card.Value(vcard.FieldNote),
		Kind:         strings.ToLower(string(card.Kind())),
	}
	if c.Name == "" {
		// A card with no FN is not unusual in an export. The structured name is
		// what it meant.
		if n := card.Name(); n != nil {
			c.Name = strings.TrimSpace(strings.Join(nonEmpty(n.GivenName, n.FamilyName), " "))
		}
	}
	c.Emails = values(card, vcard.FieldEmail)
	c.Phones = values(card, vcard.FieldTelephone)
	return c, nil
}

// New builds a card. The UID is ours and names the file it lives in.
func New(uid, name string, emails, phones []string, organisation, note string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("a contact needs a name")
	}
	card := vcard.Card{}
	card.SetValue(vcard.FieldFormattedName, name)
	card.SetName(&vcard.Name{FamilyName: familyOf(name), GivenName: givenOf(name)})
	card.SetValue(vcard.FieldUID, uid)
	for _, e := range emails {
		if e = strings.TrimSpace(e); e != "" {
			card.AddValue(vcard.FieldEmail, e)
		}
	}
	for _, p := range phones {
		if p = strings.TrimSpace(p); p != "" {
			card.AddValue(vcard.FieldTelephone, p)
		}
	}
	if organisation != "" {
		card.SetValue(vcard.FieldOrganization, organisation)
	}
	if note != "" {
		card.SetValue(vcard.FieldNote, note)
	}
	return encode(card)
}

// Edit changes fields on an existing card and leaves everything else — photos,
// addresses, X- properties somebody else's client wrote — exactly as it was.
func Edit(raw string, fn func(vcard.Card)) (string, error) {
	card, err := vcard.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		return "", fmt.Errorf("vcard: %w", err)
	}
	fn(card)
	return encode(card)
}

// AddEmail and AddPhone are the two edits worth naming, because they are what
// an agent does after reading a mail.
func AddEmail(raw, email string) (string, error) {
	return Edit(raw, func(c vcard.Card) { c.AddValue(vcard.FieldEmail, email) })
}

func AddPhone(raw, phone string) (string, error) {
	return Edit(raw, func(c vcard.Card) { c.AddValue(vcard.FieldTelephone, phone) })
}

func encode(card vcard.Card) (string, error) {
	// Version 4 is what a server that cares wants, and go-vcard fills in the
	// required fields on the way.
	vcard.ToV4(card)
	var b strings.Builder
	if err := vcard.NewEncoder(&b).Encode(card); err != nil {
		return "", err
	}
	return b.String(), nil
}

func values(card vcard.Card, field string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range card.Values(field) {
		v = strings.TrimSpace(strings.TrimPrefix(v, "tel:"))
		v = strings.TrimSpace(strings.TrimPrefix(v, "mailto:"))
		key := strings.ToLower(v)
		if v == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// givenOf and familyOf split a written name for the structured field. It is a
// guess, and it is only ever a guess — which is why the formatted name is the
// one that is displayed.
func givenOf(name string) string {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name
	}
	return strings.Join(fields[:len(fields)-1], " ")
}

func familyOf(name string) string {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return ""
	}
	return fields[len(fields)-1]
}

// Update is the edit that changes what a contact already says rather than
// adding to it: the name, the organisation, the note. An empty field is one the
// caller did not name and is left as it was — a caller fixing a spelling should
// not have to retype an address to keep it.
func Update(raw, name, organisation, note string) (string, error) {
	return Edit(raw, func(c vcard.Card) {
		if name != "" {
			c.SetValue(vcard.FieldFormattedName, name)
			c.SetName(&vcard.Name{GivenName: givenOf(name), FamilyName: familyOf(name)})
		}
		if organisation != "" {
			c.SetValue(vcard.FieldOrganization, organisation)
		}
		if note != "" {
			c.SetValue(vcard.FieldNote, note)
		}
	})
}
