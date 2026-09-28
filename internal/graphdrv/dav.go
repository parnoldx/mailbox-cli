package graphdrv

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mailbox/internal/sync/davsync"
)

// DAV is a Graph account's calendars and contacts behind davsync's driver
// interface. Everything crosses as iCalendar and vCard, so the Mirror, vcal,
// vcard and every command that edits an event or a contact do not know this is
// not a DAV server (ADR-0029).
//
// An object is named by an href, as on DAV: the collection's path and a key —
// the event's iCalUId, the UID we gave an event we made, or a hash of a
// contact's id — and the Store maps it to the Graph id.
type DAV struct {
	c *Client
	s *Store
	// Account names the default calendar and address book; the others are
	// "account/name". The default calendar is where a work invite belongs, and
	// the account's name is what the invite routing already maps its address to.
	Account string

	loc      *time.Location
	zoneName string
	now      func() time.Time
}

// NewDAV serves one account's calendars and contacts from c.
func NewDAV(c *Client, s *Store, account string) *DAV {
	loc, name := zone()
	return &DAV{c: c, s: s, Account: account, loc: loc, zoneName: name, now: time.Now}
}

// Owns says whether a collection or object URL is on Graph, which is how
// davdrv.Set routes it here.
func (d *DAV) Owns(raw string) bool {
	return hostOf(raw) == hostOf(d.c.Base)
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

// basePath is the path Graph's root sits under, "/v1.0". Hrefs carry it, as a
// DAV server's hrefs carry its own root; API paths do not.
func (d *DAV) basePath() string {
	u, err := url.Parse(d.c.Base)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(u.Path, "/")
}

// apiPath turns a collection URL, an object URL or an href into the API path
// under the root and the href the Mirror knows it by.
func (d *DAV) apiPath(raw string) (api, href string) {
	href = raw
	if u, err := url.Parse(raw); err == nil {
		href = u.EscapedPath()
	}
	return strings.TrimPrefix(href, d.basePath()), href
}

// split takes an object href apart into its collection's API path and its key.
func (d *DAV) split(raw string) (collection, key, href string) {
	api, href := d.apiPath(raw)
	i := strings.LastIndex(api, "/")
	if i < 0 {
		return api, "", href
	}
	key = api[i+1:]
	key = strings.TrimSuffix(strings.TrimSuffix(key, ".ics"), ".vcf")
	return api[:i], key, href
}

func isCalendar(api string) bool { return strings.HasPrefix(api, "/me/calendars/") }

// Collections implements davsync.Driver.
func (d *DAV) Collections(ctx context.Context) ([]davsync.Collection, error) {
	var out []davsync.Collection
	cals, _, err := all[struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		HexColor  string `json:"hexColor"`
		IsDefault bool   `json:"isDefaultCalendar"`
	}](ctx, d.c, "/me/calendars?$select=id,name,hexColor,isDefaultCalendar&$top=100")
	if err != nil {
		return nil, fmt.Errorf("calendars: %w", err)
	}
	for _, c := range cals {
		name := d.Account + "/" + c.Name
		if c.IsDefault {
			name = d.Account
		}
		out = append(out, davsync.Collection{Kind: "events", URL: d.c.Base + "/me/calendars/" + url.PathEscape(c.ID), Name: name, Color: c.HexColor})
	}

	// The default Contacts folder is not in /me/contactFolders, which lists what
	// is inside it, and it has no well-known name to ask for. Its id is on any
	// contact in it.
	// ponytail: an account with no contact in the default folder shows no
	// default address book until it has one; the folders inside it still show.
	var first page[struct {
		ParentFolderID string `json:"parentFolderId"`
	}]
	if err := d.c.do(ctx, request{method: http.MethodGet, path: "/me/contacts?$top=1&$select=parentFolderId"}, &first); err != nil {
		return nil, fmt.Errorf("contacts: %w", err)
	}
	seen := map[string]bool{}
	if len(first.Value) > 0 && first.Value[0].ParentFolderID != "" {
		id := first.Value[0].ParentFolderID
		seen[id] = true
		out = append(out, davsync.Collection{Kind: "cards", URL: d.c.Base + "/me/contactFolders/" + url.PathEscape(id), Name: d.Account})
	}
	folders, _, err := all[struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}](ctx, d.c, "/me/contactFolders?$select=id,displayName&$top=100")
	if err != nil {
		return nil, fmt.Errorf("contact folders: %w", err)
	}
	for _, f := range folders {
		if seen[f.ID] {
			continue
		}
		out = append(out, davsync.Collection{Kind: "cards", URL: d.c.Base + "/me/contactFolders/" + url.PathEscape(f.ID), Name: d.Account + "/" + f.DisplayName})
	}
	return out, nil
}

// window is the stretch of a calendar a delta covers. calendarView's delta is
// fixed to the window it started with, so the token carries it, and a window
// whose start has fallen too far behind is started again.
const (
	windowBack    = 60 * 24 * time.Hour
	windowForward = 540 * 24 * time.Hour
	windowStale   = 30 * 24 * time.Hour
)

type calToken struct {
	From, To time.Time
	Link     string
}

func parseCalToken(tok string) (calToken, bool) {
	parts := strings.SplitN(tok, "|", 3)
	if len(parts) != 3 {
		return calToken{}, false
	}
	from, err1 := strconv.ParseInt(parts[0], 10, 64)
	to, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return calToken{}, false
	}
	return calToken{From: time.Unix(from, 0).UTC(), To: time.Unix(to, 0).UTC(), Link: parts[2]}, true
}

func (t calToken) String() string {
	return fmt.Sprintf("%d|%d|%s", t.From.Unix(), t.To.Unix(), t.Link)
}

// Sync implements davsync.Driver: a delta link is a sync token.
func (d *DAV) Sync(ctx context.Context, collection, token string) (davsync.Changes, error) {
	api, _ := d.apiPath(collection)
	if isCalendar(api) {
		return d.syncCalendar(ctx, api, token)
	}
	return d.syncContacts(ctx, api, token)
}

func (d *DAV) syncContacts(ctx context.Context, api, token string) (davsync.Changes, error) {
	link := token
	if link == "" {
		link = api + "/contacts/delta?" + contactSelect
	}
	items, next, err := all[graphContact](ctx, d.c, link, "odata.maxpagesize=200")
	if err != nil {
		if expired(err) && token != "" {
			return davsync.Changes{}, davsync.ErrTokenExpired
		}
		return davsync.Changes{}, err
	}
	out := davsync.Changes{Token: next}
	for _, g := range items {
		if g.Removed != nil {
			if href, ok := d.s.objectHref(g.ID); ok {
				out.Items = append(out.Items, davsync.Change{Href: href, Deleted: true})
				_ = d.s.dropObject(g.ID)
			}
			continue
		}
		ch, err := d.contactChange(api, g)
		if err != nil {
			return davsync.Changes{}, err
		}
		out.Items = append(out.Items, ch)
	}
	return out, nil
}

func (d *DAV) contactChange(api string, g graphContact) (davsync.Change, error) {
	href, ok := d.s.objectHref(g.ID)
	if !ok {
		sum := sha1.Sum([]byte(g.ID))
		href = d.basePath() + api + "/" + hex.EncodeToString(sum[:16]) + ".vcf"
		if err := d.s.putObject(href, g.ID); err != nil {
			return davsync.Change{}, err
		}
	}
	_, key, _ := d.split(href)
	data, err := cardOf(key, g)
	if err != nil {
		return davsync.Change{}, err
	}
	return davsync.Change{Href: href, ETag: g.ChangeKey, Data: data}, nil
}

// utc is how calendar times are asked for, so they need no zone table.
const utc = `outlook.timezone="UTC"`

func (d *DAV) syncCalendar(ctx context.Context, api, token string) (davsync.Changes, error) {
	tok, ok := parseCalToken(token)
	if token != "" && (!ok || d.now().Sub(tok.From) > windowBack+windowStale) {
		return davsync.Changes{}, davsync.ErrTokenExpired
	}
	if token == "" {
		day := d.now().UTC().Truncate(24 * time.Hour)
		tok = calToken{From: day.Add(-windowBack), To: day.Add(windowForward)}
		tok.Link = api + "/calendarView/delta?startDateTime=" + tok.From.Format(time.RFC3339) +
			"&endDateTime=" + tok.To.Format(time.RFC3339)
	}
	items, next, err := all[graphEvent](ctx, d.c, tok.Link, "odata.maxpagesize=100", utc)
	if err != nil {
		if expired(err) && token != "" {
			return davsync.Changes{}, davsync.ErrTokenExpired
		}
		return davsync.Changes{}, err
	}
	byHref := map[string]davsync.Change{}
	var order []string
	add := func(ch davsync.Change) {
		if _, had := byHref[ch.Href]; !had {
			order = append(order, ch.Href)
		}
		byHref[ch.Href] = ch
	}
	dirty := map[string]bool{}
	for _, g := range items {
		switch {
		case g.Removed != nil:
			if href, ok := d.s.objectHref(g.ID); ok {
				add(davsync.Change{Href: href, Deleted: true})
				_ = d.s.dropObject(g.ID)
			} else if m, ok := d.s.masterOf(g.ID); ok {
				dirty[m] = true
			}
		case g.Type == "seriesMaster":
			dirty[g.ID] = true
		case g.SeriesMasterID != "":
			if err := d.s.setMaster(g.ID, g.SeriesMasterID); err != nil {
				return davsync.Changes{}, err
			}
			dirty[g.SeriesMasterID] = true
		default:
			if g.Start.DateTime == "" {
				// A delta that named the event without describing it.
				if err := d.c.do(ctx, request{method: http.MethodGet, path: "/me/events/" + url.PathEscape(g.ID) + "?" + eventSelect, prefer: []string{utc}}, &g); err != nil {
					if notFound(err) {
						continue
					}
					return davsync.Changes{}, err
				}
			}
			ch, err := d.singleChange(api, g)
			if err != nil {
				return davsync.Changes{}, err
			}
			add(ch)
		}
	}
	for master := range dirty {
		ch, err := d.seriesChange(ctx, api, master, tok.From, tok.To)
		if err != nil {
			return davsync.Changes{}, err
		}
		if ch.Href != "" {
			add(ch)
		}
	}
	tok.Link = next
	out := davsync.Changes{Token: tok.String()}
	for _, href := range order {
		out.Items = append(out.Items, byHref[href])
	}
	return out, nil
}

// eventHref is the href an event is known by: the one it already has, or one
// named after its iCalUId — the invite's UID, so an RSVP finds it — or, for an
// event with none, a hash of its id.
func (d *DAV) eventHref(api string, g graphEvent) (string, error) {
	if href, ok := d.s.objectHref(g.ID); ok {
		return href, nil
	}
	key := g.ICalUID
	if key == "" || strings.ContainsAny(key, "/?#% ") {
		sum := sha1.Sum([]byte(g.ID))
		key = hex.EncodeToString(sum[:16])
	}
	href := d.basePath() + api + "/" + key + ".ics"
	return href, d.s.putObject(href, g.ID)
}

func (d *DAV) singleChange(api string, g graphEvent) (davsync.Change, error) {
	href, err := d.eventHref(api, g)
	if err != nil {
		return davsync.Change{}, err
	}
	_, key, _ := d.split(href)
	data, err := singleICal(key, g, d.loc)
	if err != nil {
		return davsync.Change{}, err
	}
	return davsync.Change{Href: href, ETag: g.ChangeKey, Data: data}, nil
}

// seriesChange reads a repeating event and its instances in the window and
// makes one object of them. A master that is gone is a deletion.
func (d *DAV) seriesChange(ctx context.Context, api, master string, from, to time.Time) (davsync.Change, error) {
	var m graphEvent
	err := d.c.do(ctx, request{method: http.MethodGet, path: "/me/events/" + url.PathEscape(master) + "?" + eventSelect, prefer: []string{utc}}, &m)
	if notFound(err) {
		if href, ok := d.s.objectHref(master); ok {
			_ = d.s.dropObject(master)
			return davsync.Change{Href: href, Deleted: true}, nil
		}
		return davsync.Change{}, nil
	}
	if err != nil {
		return davsync.Change{}, err
	}
	m.ID = master
	instances, _, err := all[graphEvent](ctx, d.c, "/me/events/"+url.PathEscape(master)+"/instances?startDateTime="+
		from.Format(time.RFC3339)+"&endDateTime="+to.Format(time.RFC3339)+
		"&$select=type,originalStart,start,end,subject,location,isCancelled&$top=200", utc)
	if err != nil {
		return davsync.Change{}, err
	}
	href, err := d.eventHref(api, m)
	if err != nil {
		return davsync.Change{}, err
	}
	_, key, _ := d.split(href)
	data, err := seriesICal(key, m, instances, from, to, d.loc)
	if err != nil {
		return davsync.Change{}, err
	}
	return davsync.Change{Href: href, ETag: m.ChangeKey, Data: data}, nil
}

// MultiGet implements davsync.Driver: each object read again by its Graph id.
func (d *DAV) MultiGet(ctx context.Context, collection string, hrefs []string) ([]davsync.Change, error) {
	api, _ := d.apiPath(collection)
	var out []davsync.Change
	for _, h := range hrefs {
		_, _, href := d.split(h)
		id, ok := d.s.objectID(href)
		if !ok {
			continue
		}
		var ch davsync.Change
		var err error
		if isCalendar(api) {
			ch, err = d.currentEvent(ctx, api, id)
		} else {
			var g graphContact
			if err = d.c.do(ctx, request{method: http.MethodGet, path: "/me/contacts/" + url.PathEscape(id) + "?" + contactSelect}, &g); err == nil {
				g.ID = id
				ch, err = d.contactChange(api, g)
			}
		}
		if notFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}

// currentEvent is what the server has for an event now, as the object the
// Mirror would hold.
func (d *DAV) currentEvent(ctx context.Context, api, id string) (davsync.Change, error) {
	var g graphEvent
	if err := d.c.do(ctx, request{method: http.MethodGet, path: "/me/events/" + url.PathEscape(id) + "?" + eventSelect, prefer: []string{utc}}, &g); err != nil {
		return davsync.Change{}, err
	}
	g.ID = id
	if g.Type == "seriesMaster" {
		day := d.now().UTC().Truncate(24 * time.Hour)
		return d.seriesChange(ctx, api, id, day.Add(-windowBack), day.Add(windowForward))
	}
	return d.singleChange(api, g)
}

// Put implements davsync.WriteDriver. A new object is created and its href
// mapped to the id Graph gave it; an existing one is sent only the fields that
// differ from what the server has now. The If-Match is not sent: Graph's
// changeKey is not an ETag it checks on a PATCH.
//
// ponytail: a concurrent edit in Outlook between our read and our PATCH is
// overwritten field by field rather than refused.
func (d *DAV) Put(ctx context.Context, raw, data, ifMatch string) (string, error) {
	collection, _, href := d.split(raw)
	id, known := d.s.objectID(href)
	if isCalendar(collection) {
		return d.putEvent(ctx, collection, href, id, known, data)
	}
	return d.putContact(ctx, collection, href, id, known, data)
}

func (d *DAV) putEvent(ctx context.Context, collection, href, id string, known bool, data string) (string, error) {
	want, err := eventFields(data, d.loc, d.zoneName)
	if err != nil {
		return "", err
	}
	var g graphEvent
	if !known {
		if want["recurrence"] == nil {
			delete(want, "recurrence")
		}
		if err := d.c.do(ctx, request{method: http.MethodPost, path: collection + "/events", body: jsonBody(want)}, &g); err != nil {
			return "", err
		}
		return g.ChangeKey, d.s.putObject(href, g.ID)
	}
	now, err := d.currentEvent(ctx, collection, id)
	if err != nil {
		return "", err
	}
	have, err := eventFields(now.Data, d.loc, d.zoneName)
	if err != nil {
		return "", err
	}
	patch := changed(have, want)
	if len(patch) == 0 {
		return now.ETag, nil
	}
	if err := d.c.do(ctx, request{method: http.MethodPatch, path: "/me/events/" + url.PathEscape(id), body: jsonBody(patch)}, &g); err != nil {
		return "", err
	}
	return g.ChangeKey, nil
}

func (d *DAV) putContact(ctx context.Context, collection, href, id string, known bool, data string) (string, error) {
	var now graphContact
	if known {
		if err := d.c.do(ctx, request{method: http.MethodGet, path: "/me/contacts/" + url.PathEscape(id) + "?" + contactSelect}, &now); err != nil {
			return "", err
		}
	}
	want, err := contactFields(data, now)
	if err != nil {
		return "", err
	}
	var g graphContact
	if !known {
		if err := d.c.do(ctx, request{method: http.MethodPost, path: collection + "/contacts", body: jsonBody(want)}, &g); err != nil {
			return "", err
		}
		return g.ChangeKey, d.s.putObject(href, g.ID)
	}
	current, err := cardOf("", now)
	if err != nil {
		return "", err
	}
	have, err := contactFields(current, now)
	if err != nil {
		return "", err
	}
	patch := changed(have, want)
	if len(patch) == 0 {
		return now.ChangeKey, nil
	}
	if err := d.c.do(ctx, request{method: http.MethodPatch, path: "/me/contacts/" + url.PathEscape(id), body: jsonBody(patch)}, &g); err != nil {
		return "", err
	}
	return g.ChangeKey, nil
}

// changed is the fields of want that differ from have.
func changed(have, want map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range want {
		if string(jsonBody(v)) != string(jsonBody(have[k])) {
			out[k] = v
		}
	}
	return out
}

// Delete implements davsync.WriteDriver. Already gone is done.
func (d *DAV) Delete(ctx context.Context, raw, ifMatch string) error {
	collection, _, href := d.split(raw)
	id, ok := d.s.objectID(href)
	if !ok {
		return fmt.Errorf("no object at %s", href)
	}
	path := "/me/contacts/" + url.PathEscape(id)
	if isCalendar(collection) {
		path = "/me/events/" + url.PathEscape(id)
	}
	if err := d.c.do(ctx, request{method: http.MethodDelete, path: path}, nil); err != nil && !notFound(err) {
		return err
	}
	return d.s.dropObject(id)
}

var _ davsync.WriteDriver = (*DAV)(nil)
