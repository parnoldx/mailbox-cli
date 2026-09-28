package graphdrv

import (
	"slices"
	"strings"

	"mailbox/internal/vcard"
)

// graphContact is a contact as Graph describes it.
type graphContact struct {
	ID      string `json:"id"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
	ChangeKey      string   `json:"changeKey"`
	DisplayName    string   `json:"displayName"`
	GivenName      string   `json:"givenName"`
	Surname        string   `json:"surname"`
	CompanyName    string   `json:"companyName"`
	PersonalNotes  string   `json:"personalNotes"`
	BusinessPhones []string `json:"businessPhones"`
	HomePhones     []string `json:"homePhones"`
	MobilePhone    string   `json:"mobilePhone"`
	EmailAddresses []struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddresses"`
}

const contactSelect = "$select=changeKey,displayName,givenName,surname,companyName,personalNotes," +
	"businessPhones,homePhones,mobilePhone,emailAddresses"

func (g graphContact) name() string {
	if n := strings.TrimSpace(g.DisplayName); n != "" {
		return n
	}
	if n := strings.TrimSpace(g.GivenName + " " + g.Surname); n != "" {
		return n
	}
	if len(g.EmailAddresses) > 0 {
		return g.EmailAddresses[0].Address
	}
	return "(no name)"
}

func (g graphContact) phones() []string {
	out := slices.Clone(g.BusinessPhones)
	if g.MobilePhone != "" {
		out = append(out, g.MobilePhone)
	}
	return append(out, g.HomePhones...)
}

func (g graphContact) emails() []string {
	var out []string
	for _, e := range g.EmailAddresses {
		if e.Address != "" {
			out = append(out, e.Address)
		}
	}
	return out
}

// cardOf is a Graph contact as a vCard, written by the same function a contact
// made here is, so the two read back alike.
func cardOf(uid string, g graphContact) (string, error) {
	return vcard.New(uid, g.name(), g.emails(), g.phones(), g.CompanyName, g.PersonalNotes)
}

// contactFields is what a write sends. Phones keep the kind Outlook filed them
// under — mobile stays mobile — and a new one is a business phone, Graph's
// first kind. Like eventFields, what the server has now goes through the same
// function, so only what changed is sent.
func contactFields(raw string, now graphContact) (map[string]any, error) {
	c, err := vcard.Parse(raw)
	if err != nil {
		return nil, err
	}
	emails := []map[string]string{}
	for _, e := range c.Emails {
		emails = append(emails, map[string]string{"address": e})
	}
	f := map[string]any{
		"displayName":    c.Name,
		"companyName":    c.Organisation,
		"personalNotes":  c.Note,
		"emailAddresses": emails,
	}
	fields := strings.Fields(c.Name)
	if len(fields) > 1 {
		f["givenName"], f["surname"] = strings.Join(fields[:len(fields)-1], " "), fields[len(fields)-1]
	} else {
		f["givenName"], f["surname"] = c.Name, ""
	}
	business, home, mobile := []string{}, []string{}, ""
	for _, p := range c.Phones {
		switch {
		case p == now.MobilePhone && mobile == "":
			mobile = p
		case slices.Contains(now.HomePhones, p):
			home = append(home, p)
		default:
			business = append(business, p)
		}
	}
	f["businessPhones"], f["homePhones"], f["mobilePhone"] = business, home, mobile
	return f, nil
}
