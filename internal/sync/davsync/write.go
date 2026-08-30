package davsync

import (
	"context"
	"net/url"
	"strings"

	"mailbox/internal/mirror"
)

// WriteDriver is a Driver that can also change things. It is separate from
// Driver because everything that reads is answerable offline and everything
// here is not: a write blocks on the server and updates the Mirror from the
// ack (ADR-0004).
type WriteDriver interface {
	Driver
	// Put writes an object and returns the ETag the server now has for it.
	// ifMatch is the ETag being replaced, empty to create. A server that
	// returns no ETag leaves the object to be read back.
	Put(ctx context.Context, href, data, ifMatch string) (string, error)
	// Delete removes an object.
	Delete(ctx context.Context, href, ifMatch string) error
}

// Writer is the write-through path for collections. It shares the driver with
// the Reconciler, so a write and a sync never interleave on one object.
type Writer struct {
	Account string
	Mirror  *mirror.Mirror
	Driver  WriteDriver
	// Project turns raw text into the row to store. It is the Reconciler's, so
	// an object written here and the same object arriving on the next sync
	// project identically — a write that stored a different projection would
	// make the Mirror disagree with itself until the next cycle.
	Reconciler *Reconciler
}

// Put writes an object to the server and records what the server ended up with.
//
// The Mirror stores what came back rather than what was sent. A server is
// entitled to rewrite what it is given — adding its own DTSTAMP, normalising a
// timezone — and a Mirror holding the version we hoped for is a Mirror that
// disagrees with every other client (ADR-0004).
func (w *Writer) Put(ctx context.Context, c mirror.Collection, href, raw, ifMatch string) (mirror.Object, error) {
	// The request goes to the absolute URL, because that is what says which
	// server this is; the Mirror stores the path the server names it by, which
	// is the shape the next sync will report it in.
	etag, err := w.Driver.Put(ctx, w.absolute(c, href), raw, ifMatch)
	if err != nil {
		return mirror.Object{}, err
	}
	stored := Change{Href: href, ETag: etag, Data: raw}
	// No ETag means we do not know what the server has, so we ask. One extra
	// request on a write is cheaper than a Mirror that has to be believed.
	if etag == "" {
		if got, err := w.Driver.MultiGet(ctx, c.URL, []string{href}); err == nil && len(got) == 1 && got[0].Data != "" {
			stored = got[0]
			stored.Href = href
		}
	}
	object := w.Reconciler.Project(c, stored)
	tx, err := w.Mirror.Begin(w.Account)
	if err != nil {
		return mirror.Object{}, err
	}
	defer tx.Rollback()
	if err := tx.PutObject(object); err != nil {
		return mirror.Object{}, err
	}
	if err := tx.Commit(); err != nil {
		return mirror.Object{}, err
	}
	return w.Mirror.ObjectByHref(w.Account, c.ID, href)
}

// Delete removes an object from the server and from the Mirror, in that order.
func (w *Writer) Delete(ctx context.Context, c mirror.Collection, o mirror.Object) error {
	if err := w.Driver.Delete(ctx, w.absolute(c, o.Href), o.ETag); err != nil {
		return err
	}
	tx, err := w.Mirror.Begin(w.Account)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.DeleteObject(c.ID, o.Href); err != nil {
		return err
	}
	return tx.Commit()
}

// Href names the file a new object goes in, as a path — the shape a server
// reports its own objects in. The UID names the file, which is what every
// client does and the only convention under which a second write to the same
// object lands on the same URL.
func Href(c mirror.Collection, uid string) string {
	base := strings.TrimSuffix(c.URL, "/")
	if u, err := url.Parse(base); err == nil && u.Path != "" {
		base = strings.TrimSuffix(u.Path, "/")
	}
	return base + "/" + uid + ".ics"
}

// absolute resolves an href the server gave us — usually a path — against the
// collection it came from.
func (w *Writer) absolute(c mirror.Collection, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	base := strings.TrimSuffix(c.URL, "/")
	i := strings.Index(base, "://")
	if i < 0 {
		return href
	}
	root := base[:i+3]
	if j := strings.Index(base[i+3:], "/"); j >= 0 {
		root = base[:i+3+j]
	}
	if strings.HasPrefix(href, "/") {
		return root + href
	}
	return base + "/" + href
}

// AbsoluteHref is absolute, for a caller that has a collection and an href and
// needs the URL the server knows them by.
func (w *Writer) AbsoluteHref(c mirror.Collection, href string) string {
	return w.absolute(c, href)
}
