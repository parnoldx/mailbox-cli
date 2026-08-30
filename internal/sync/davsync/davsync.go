// Package davsync reconciles the Mirror against CalDAV and CardDAV collections
// using RFC 6578 sync-collection. All three servers this program talks to
// answer it, so there is one algorithm and no ctag fallback (ADR-0010).
package davsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/vcal"
	"mailbox/internal/vcard"
)

// Collection is a collection as the server describes it. It is discovered by
// enumerating the server, never by a URL typed into a config file.
type Collection struct {
	Kind  string // "events" | "tasks" | "cards"
	URL   string
	Name  string
	Color string
}

// Change is one object in a sync answer. Data may be empty when the server
// reported the change without the object, which is what MultiGet is for.
type Change struct {
	Href    string
	ETag    string
	Data    string
	Deleted bool
}

// Changes is one sync-collection answer: what moved, and the token that means
// "everything up to here".
type Changes struct {
	Token string
	Items []Change
}

// ErrTokenExpired is what a driver returns when the server no longer recognises
// the token it gave us. It is not a failure: it means start again from nothing,
// which is exactly what an empty token asks for.
var ErrTokenExpired = errors.New("sync token expired")

// Driver is everything the reconciler needs from a DAV server.
type Driver interface {
	// Collections enumerates what the account has.
	Collections(ctx context.Context) ([]Collection, error)
	// Sync runs sync-collection. An empty token returns everything, together
	// with the token that describes it, in one request.
	Sync(ctx context.Context, url, token string) (Changes, error)
	// MultiGet fetches objects the sync answer named but did not carry.
	MultiGet(ctx context.Context, collectionURL string, hrefs []string) ([]Change, error)
}

// Outcome is what one collection's sync did.
type Outcome struct {
	Changed int
	Deleted int
	// Full says this was a sync from nothing, either the first one or because
	// the server refused our token.
	Full bool
}

// Reconciler brings one account's collections up to date. Like the mail
// reconciler it is the only writer of the state it owns.
type Reconciler struct {
	Account string
	Mirror  *mirror.Mirror
	Driver  Driver
	// Location is the timezone a floating date-time means. Calendars are full
	// of them and there is no right answer on the server side.
	Location *time.Location
	// OnCollection, if set, is called as each collection finishes.
	OnCollection func(name string, out Outcome, err error)
	// Exclude are the display names this machine does not mirror, from the
	// config. Discovery skips them on the way in and drops them if they are
	// already held. It is read under a lock because the config can change
	// while a cycle is running (ADR-0021).
	Exclude   []string
	excludeMu sync.RWMutex
}

// SetExclude replaces the exclude list on a running Reconciler.
func (r *Reconciler) SetExclude(names []string) {
	r.excludeMu.Lock()
	defer r.excludeMu.Unlock()
	r.Exclude = append([]string(nil), names...)
}

// Discover asks the servers what collections exist and records them. A
// collection that has disappeared, or that this machine has excluded, is
// dropped with its objects.
//
// The prune is skipped when the answer was partial — some servers answered and
// some did not. Dropping an unreachable server's calendars because it could not
// be asked would turn a network problem into data loss, and the next answer
// would refetch every object it ever held.
func (r *Reconciler) Discover(ctx context.Context) ([]mirror.Collection, error) {
	found, err := r.Driver.Collections(ctx)
	partial := false
	if err != nil {
		if len(found) == 0 {
			return nil, fmt.Errorf("discover: %w", err)
		}
		partial = true
	}
	keep := make(map[string]bool, len(found))
	for _, c := range found {
		if r.excluded(c.Name) {
			continue
		}
		keep[c.URL] = true
		if _, err := r.Mirror.PutCollection(mirror.Collection{
			Account: r.Account, Kind: c.Kind, URL: c.URL, Name: c.Name, Color: c.Color,
		}); err != nil {
			return nil, err
		}
	}
	known, err := r.Mirror.Collections(r.Account, "")
	if err != nil {
		return nil, err
	}
	for _, c := range known {
		if keep[c.URL] {
			continue
		}
		// An excluded collection is dropped whether or not the answer was
		// partial: that decision came from the config, not from a server.
		if partial && !r.excluded(c.Name) {
			continue
		}
		if err := r.Mirror.ForgetCollection(r.Account, c.URL); err != nil {
			return nil, err
		}
	}
	return r.Mirror.Collections(r.Account, "")
}

// excluded says whether this machine mirrors a Collection. The list is matched
// on the display name, which is what discovery matches on, and it lives in the
// config because the Mirror is rebuilt (ADR-0013) and would forget it.
func (r *Reconciler) excluded(name string) bool {
	r.excludeMu.RLock()
	defer r.excludeMu.RUnlock()
	for _, e := range r.Exclude {
		if strings.EqualFold(strings.TrimSpace(e), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// SyncAll reconciles every known collection. One that fails does not stop the
// others: a Mirror that is Behind on one calendar and current on the rest is a
// better answer than one that gives up.
func (r *Reconciler) SyncAll(ctx context.Context) (map[string]Outcome, error) {
	return r.SyncKinds(ctx)
}

// SyncKinds reconciles the collections of the kinds named, or all of them when
// none are. Calendars and task lists move all day and address books almost
// never, so they do not deserve the same cadence (ADR-0010).
func (r *Reconciler) SyncKinds(ctx context.Context, kinds ...string) (map[string]Outcome, error) {
	cols, err := r.Mirror.Collections(r.Account, "")
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 {
		want := map[string]bool{}
		for _, k := range kinds {
			want[k] = true
		}
		var keep []mirror.Collection
		for _, c := range cols {
			if want[c.Kind] {
				keep = append(keep, c)
			}
		}
		cols = keep
	}
	out := make(map[string]Outcome, len(cols))
	var firstErr error
	for _, c := range cols {
		o, err := r.Sync(ctx, c)
		if r.OnCollection != nil {
			r.OnCollection(c.Name, o, err)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", c.Name, err)
			}
			continue
		}
		out[c.Name] = o
	}
	return out, firstErr
}

// Sync reconciles one collection.
//
// There is no journal here and none is needed. The token and the objects it
// describes are written in one transaction, so a crash anywhere leaves the old
// token with the old objects — and asking again from the old token returns the
// same changes. The mail side needs a journal because a modseq can be advanced
// over a half-fetched folder; here that state cannot be constructed.
func (r *Reconciler) Sync(ctx context.Context, c mirror.Collection) (Outcome, error) {
	full := c.SyncToken == ""
	changes, err := r.Driver.Sync(ctx, c.URL, c.SyncToken)
	if errors.Is(err, ErrTokenExpired) {
		// The server has forgotten what our token meant. Everything we hold may
		// be wrong, so it goes and comes back.
		full = true
		changes, err = r.Driver.Sync(ctx, c.URL, "")
	}
	if err != nil {
		return Outcome{}, err
	}

	// Anything the server named without sending is fetched in one more request.
	var missing []string
	for _, it := range changes.Items {
		if !it.Deleted && it.Data == "" {
			missing = append(missing, it.Href)
		}
	}
	if len(missing) > 0 {
		fetched, err := r.Driver.MultiGet(ctx, c.URL, missing)
		if err != nil {
			return Outcome{}, err
		}
		byHref := map[string]Change{}
		for _, f := range fetched {
			byHref[f.Href] = f
		}
		for i, it := range changes.Items {
			if f, ok := byHref[it.Href]; ok {
				changes.Items[i].Data, changes.Items[i].ETag = f.Data, f.ETag
			}
		}
	}

	tx, err := r.Mirror.Begin(r.Account)
	if err != nil {
		return Outcome{}, err
	}
	defer tx.Rollback()

	out := Outcome{Full: full}
	if full {
		if err := tx.ClearCollection(c.ID); err != nil {
			return Outcome{}, err
		}
	}
	for _, it := range changes.Items {
		if it.Deleted {
			if err := tx.DeleteObject(c.ID, it.Href); err != nil {
				return Outcome{}, err
			}
			out.Deleted++
			continue
		}
		if it.Data == "" {
			// Named, not deleted, and not fetchable: leave what we have rather
			// than storing an empty record.
			continue
		}
		if err := tx.PutObject(r.Project(c, it)); err != nil {
			return Outcome{}, err
		}
		out.Changed++
	}
	if err := tx.SetSyncToken(c.ID, changes.Token); err != nil {
		return Outcome{}, err
	}
	return out, tx.Commit()
}

// Project turns a raw object into the row the Mirror stores. An object we
// cannot parse is stored anyway, with an empty projection: the raw text is the
// record, and a projection we got wrong is repaired by one resync (ADR-0010).
func (r *Reconciler) Project(c mirror.Collection, it Change) mirror.Object {
	o := mirror.Object{
		CollectionID: c.ID, Href: it.Href, ETag: it.ETag, Raw: it.Data,
	}
	if c.Kind == "cards" {
		card, err := vcard.Parse(it.Data)
		if err != nil {
			return o
		}
		o.Kind = "card"
		o.UID, o.Summary = card.UID, card.Name
		o.Emails, o.Phones = card.Emails, card.Phones
		o.Location, o.Description = card.Organisation, card.Note
		o.Status = card.Kind
		return o
	}
	p, err := vcal.Parse(it.Data, r.Location)
	if err != nil {
		return o
	}
	o.Kind = string(p.Kind)
	o.UID, o.Summary, o.Location, o.Description = p.UID, p.Summary, p.Location, p.Description
	o.Status, o.Start, o.End = p.Status, p.Start, p.End
	o.Due, o.DueAllDay, o.Priority = p.Due, p.DueAllDay, p.Priority
	o.Completed = p.Completed
	o.AllDay, o.Recurring, o.RepeatsUntil = p.AllDay, p.Recurring, p.RepeatsUntil
	return o
}
