package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/vcal"
	"mailbox/internal/vcard"
)

// contact is one card as a caller sees it.
type contact struct {
	ID           int64    `json:"id"`
	Book         string   `json:"book"`
	UID          string   `json:"uid"`
	Name         string   `json:"name"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`
	Organisation string   `json:"organisation,omitempty"`
	Note         string   `json:"note,omitempty"`
}

// handleContact searches the address books and changes them. Searching is a
// Mirror read; adding and editing block on the server (ADR-0004).
func (d *Daemon) handleContact(ctx context.Context, req Request, resp Response) Response {
	verb := req.Verb("search")
	switch verb {
	case "list", "search":
		query := req.Str("positional")
		limit := req.Int("limit", 25)
		objects, err := d.Mirror.Contacts(d.Account, query, limit)
		if err != nil {
			return resp.api(err.Error())
		}
		out := make([]contact, 0, len(objects))
		for _, o := range objects {
			out = append(out, viewContact(o))
		}
		return resp.ok(out)

	case "view":
		id, err := objectID(req, "contact")
		if err != nil {
			return resp.usage(err.Error())
		}
		o, err := d.Mirror.Object(d.Account, id)
		if errors.Is(err, mirror.ErrNotFound) {
			return resp.notFound(fmt.Sprintf("no contact %d in the mirror", id))
		}
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(viewContact(o))

	case "add":
		name := req.Str("positional")
		if strings.TrimSpace(name) == "" {
			return resp.usage("a contact needs a name")
		}
		book, err := d.pick(addressBooks, or(req.Str("book"), d.defaultAddressBook()))
		if err != nil {
			return resp.failed(err)
		}
		uid := vcal.NewUID()
		raw, err := vcard.New(uid, name, req.Strings("email"), req.Strings("phone"),
			req.Str("org"), req.Str("note"))
		if err != nil {
			return resp.usage(err.Error())
		}
		object, err := d.put(ctx, contactChanged, book, cardHref(book, uid), raw, "")
		if err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(viewContact(object))

	case "email", "phone", "update", "drop":
		return d.changeContact(ctx, verb, req, resp)
	}
	return resp.usage(fmt.Sprintf("unknown contact command %q", verb))
}

// changeContact adds an address or a number to a card, or removes the card.
func (d *Daemon) changeContact(ctx context.Context, verb string, req Request, resp Response) Response {
	object, book, err := d.load(req, "contact")
	if err != nil {
		return resp.failed(err)
	}

	if verb == "drop" {
		if err := d.remove(ctx, contactChanged, book, object); err != nil {
			return resp.api(err.Error())
		}
		return resp.ok(map[string]any{"id": object.ID, "state": "dropped", "name": object.Summary})
	}

	var raw string
	if verb == "update" {
		name := strings.TrimSpace(req.Str("name"))
		org := strings.TrimSpace(req.Str("org"))
		note := req.Str("note")
		if name == "" && org == "" && note == "" {
			return resp.usage(
				"contact update needs something to change: --name, --org or --note")
		}
		raw, err = vcard.Update(object.Raw, name, org, note)
	} else {
		// email and phone add to what is there; the vCard is the record and
		// nothing here throws away an address somebody else wrote (ADR-0010).
		value := strings.TrimSpace(req.Str("value"))
		if value == "" {
			return resp.usage(fmt.Sprintf("contact %s needs --value", verb))
		}
		if verb == "email" {
			raw, err = vcard.AddEmail(object.Raw, value)
		} else {
			raw, err = vcard.AddPhone(object.Raw, value)
		}
	}
	if err != nil {
		return resp.api(err.Error())
	}
	written, err := d.put(ctx, contactChanged, book, object.Href, raw, object.ETag)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(viewContact(written))
}

// cardHref names the file a new card goes in. A vCard is .vcf, not .ics.
func cardHref(book mirror.Collection, uid string) string {
	return strings.TrimSuffix(davsync.Href(book, uid), ".ics") + ".vcf"
}

func viewContact(o mirror.Object) contact {
	return contact{
		ID: o.ID, Book: o.Collection, UID: o.UID, Name: o.Summary,
		Emails: o.Emails, Phones: o.Phones, Organisation: o.Location, Note: o.Description,
	}
}
