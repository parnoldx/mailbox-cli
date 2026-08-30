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
	verb := "search"
	if len(req.Cmd) > 1 {
		verb = req.Cmd[1]
	}
	switch verb {
	case "list", "search":
		query, _ := req.Args["positional"].(string)
		limit := 25
		if v, ok := req.Args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		objects, err := d.Mirror.Contacts(d.Account, query, limit)
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		out := make([]contact, 0, len(objects))
		for _, o := range objects {
			out = append(out, viewContact(o))
		}
		resp.OK, resp.Data = true, out
		return resp

	case "view":
		id, err := objectID(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		o, err := d.Mirror.Object(d.Account, id)
		if errors.Is(err, mirror.ErrNotFound) {
			resp.Code, resp.Error = "not_found", fmt.Sprintf("no contact %d in the mirror", id)
			return resp
		}
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewContact(o)
		return resp

	case "add":
		name, _ := req.Args["positional"].(string)
		if strings.TrimSpace(name) == "" {
			resp.Code, resp.Error = "usage", "a contact needs a name"
			return resp
		}
		book, err := d.addressBook(req)
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		uid := vcal.NewUID()
		raw, err := vcard.New(uid, name, strList(req.Args["email"]), strList(req.Args["phone"]),
			str(req.Args["org"]), str(req.Args["note"]))
		if err != nil {
			resp.Code, resp.Error = "usage", err.Error()
			return resp
		}
		object, err := d.writeCard(ctx, book, cardHref(book, uid), raw, "")
		if err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		resp.OK, resp.Data = true, viewContact(object)
		return resp

	case "email", "phone", "update", "drop":
		return d.changeContact(ctx, verb, req, resp)
	}
	resp.Code, resp.Error = "usage", fmt.Sprintf("unknown contact command %q", verb)
	return resp
}

// changeContact adds an address or a number to a card, or removes the card.
func (d *Daemon) changeContact(ctx context.Context, verb string, req Request, resp Response) Response {
	if d.DAVWriter == nil {
		resp.Code, resp.Error = "api", "this daemon cannot write: no dav connection"
		return resp
	}
	id, err := objectID(req)
	if err != nil {
		resp.Code, resp.Error = "usage", err.Error()
		return resp
	}
	object, err := d.Mirror.Object(d.Account, id)
	if errors.Is(err, mirror.ErrNotFound) {
		resp.Code, resp.Error = "not_found", fmt.Sprintf("no contact %d in the mirror", id)
		return resp
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	book, err := d.collectionOf(object)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}

	if verb == "drop" {
		if err := d.DAVWriter.Delete(ctx, book, object); err != nil {
			resp.Code, resp.Error = "api", err.Error()
			return resp
		}
		d.push(Push{Event: "contact.changed", Account: d.Account, Box: book.Name})
		resp.OK, resp.Data = true, map[string]any{"id": id, "state": "dropped", "name": object.Summary}
		return resp
	}

	var raw string
	if verb == "update" {
		name := strings.TrimSpace(str(req.Args["name"]))
		org := strings.TrimSpace(str(req.Args["org"]))
		note := str(req.Args["note"])
		if name == "" && org == "" && note == "" {
			resp.Code, resp.Error = "usage",
				"contact update needs something to change: --name, --org or --note"
			return resp
		}
		raw, err = vcard.Update(object.Raw, name, org, note)
	} else {
		// email and phone add to what is there; the vCard is the record and
		// nothing here throws away an address somebody else wrote (ADR-0010).
		value := strings.TrimSpace(str(req.Args["value"]))
		if value == "" {
			resp.Code, resp.Error = "usage", fmt.Sprintf("contact %s needs --value", verb)
			return resp
		}
		if verb == "email" {
			raw, err = vcard.AddEmail(object.Raw, value)
		} else {
			raw, err = vcard.AddPhone(object.Raw, value)
		}
	}
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	written, err := d.writeCard(ctx, book, object.Href, raw, object.ETag)
	if err != nil {
		resp.Code, resp.Error = "api", err.Error()
		return resp
	}
	resp.OK, resp.Data = true, viewContact(written)
	return resp
}

func (d *Daemon) writeCard(ctx context.Context, book mirror.Collection, href, raw, ifMatch string) (mirror.Object, error) {
	if d.DAVWriter == nil {
		return mirror.Object{}, errors.New("this daemon cannot write: no dav connection")
	}
	object, err := d.DAVWriter.Put(ctx, book, href, raw, ifMatch)
	if err != nil {
		return mirror.Object{}, err
	}
	d.push(Push{Event: "contact.changed", Account: d.Account, Box: book.Name})
	return object, nil
}

// cardHref names the file a new card goes in. A vCard is .vcf, not .ics.
func cardHref(book mirror.Collection, uid string) string {
	return strings.TrimSuffix(davsync.Href(book, uid), ".ics") + ".vcf"
}

// addressBook picks where a new contact goes. As with task lists, one book
// needs no naming and several do — and the Global Address Book is read-only,
// so it is never the default.
func (d *Daemon) addressBook(req Request) (mirror.Collection, error) {
	name := str(req.Args["book"])
	if name == "" {
		name = d.defaultAddressBook()
	}
	books, err := d.Mirror.Collections(d.Account, "cards")
	if err != nil {
		return mirror.Collection{}, err
	}
	if len(books) == 0 {
		return mirror.Collection{}, errors.New("there are no address books on this account")
	}
	if name != "" {
		for _, c := range books {
			if strings.EqualFold(c.Name, name) {
				return c, nil
			}
		}
		return mirror.Collection{}, fmt.Errorf("no address book called %q", name)
	}
	if len(books) == 1 {
		return books[0], nil
	}
	names := make([]string, 0, len(books))
	for _, c := range books {
		names = append(names, c.Name)
	}
	return mirror.Collection{}, fmt.Errorf("there are %d address books — name one with --book: %s",
		len(books), strings.Join(names, ", "))
}

func viewContact(o mirror.Object) contact {
	return contact{
		ID: o.ID, Book: o.Collection, UID: o.UID, Name: o.Summary,
		Emails: o.Emails, Phones: o.Phones, Organisation: o.Location, Note: o.Description,
	}
}
