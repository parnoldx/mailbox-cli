// Package davdrv implements davsync.Driver over CalDAV and CardDAV.
//
// The requests are written here rather than taken from a library. The one thing
// this program needs from these protocols — sync-collection with the object
// data in the same round trip — is what go-webdav's CalDAV client does not
// have, and re-encoding a parsed calendar to store it would throw away the
// record itself (ADR-0010). What is left is four XML documents (ADR-0015).
package davdrv

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mailbox/internal/sync/davsync"
)

// Config is what it takes to reach one DAV server.
type Config struct {
	// Endpoint is the server root, or any URL on it. Discovery starts here and
	// asks the server for everything else.
	Endpoint string
	Username string
	Password string
}

// Client talks to one DAV server as one user.
type Client struct {
	cfg  Config
	http *http.Client
	// static, when set, is what Collections answers instead of discovering.
	static []davsync.Collection
}

// New returns a Client. Nothing is requested until it is asked for.
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

// Collections implements davsync.Driver: current-user-principal, then the
// calendar and address book homes, then what is in them. Three round trips at
// startup, and never a URL anybody typed (ADR-0010).
func (c *Client) Collections(ctx context.Context) ([]davsync.Collection, error) {
	if len(c.static) > 0 {
		return append([]davsync.Collection(nil), c.static...), nil
	}
	principal, err := c.principal(ctx)
	if err != nil {
		return nil, err
	}
	calendars, cards, err := c.homes(ctx, principal)
	if err != nil {
		return nil, err
	}
	var out []davsync.Collection
	for _, home := range []string{calendars, cards} {
		if home == "" {
			continue
		}
		found, err := c.list(ctx, home)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// EnsureCalendar returns the collection with this display name, creating it if
// the server does not have one. It is used for exactly one collection — the
// habits calendar — because that is the only thing this program keeps that has
// no home of its own anywhere (ADR-0018).
func (c *Client) EnsureCalendar(ctx context.Context, displayName string, comps []string) (davsync.Collection, error) {
	if found, ok, err := c.named(ctx, displayName); err != nil || ok {
		return found, err
	}
	principal, err := c.principal(ctx)
	if err != nil {
		return davsync.Collection{}, err
	}
	home, _, err := c.homes(ctx, principal)
	if err != nil {
		return davsync.Collection{}, err
	}
	if home == "" {
		return davsync.Collection{}, fmt.Errorf("this account has no calendar home to create %q in", displayName)
	}
	href := strings.TrimSuffix(home, "/") + "/" + slug(displayName) + "/"
	if err := c.MkCalendar(ctx, href, displayName, comps); err != nil {
		return davsync.Collection{}, err
	}
	if found, ok, err := c.named(ctx, displayName); err != nil || ok {
		return found, err
	}
	// Created, but the server does not list it yet. What we asked for is still
	// the truth about where it is.
	return davsync.Collection{Kind: "events", URL: href, Name: displayName}, nil
}

func (c *Client) named(ctx context.Context, displayName string) (davsync.Collection, bool, error) {
	cols, err := c.Collections(ctx)
	if err != nil {
		return davsync.Collection{}, false, err
	}
	for _, col := range cols {
		if strings.EqualFold(col.Name, displayName) {
			return col, true, nil
		}
	}
	return davsync.Collection{}, false, nil
}

// slug is the last path segment a new collection gets. It is not the name: the
// name is a property, and a server is free to keep the two apart.
func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "collection"
	}
	return out
}

// principal asks the server who we are. Everything else hangs off the answer.
func (c *Client) principal(ctx context.Context) (string, error) {
	body := `<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`
	ms, err := c.propfind(ctx, c.cfg.Endpoint, "0", body)
	if err != nil {
		return "", fmt.Errorf("current-user-principal: %w", err)
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if href := strings.TrimSpace(ps.Prop.CurrentUserPrincipal.Href); href != "" {
				return c.resolve(href), nil
			}
		}
	}
	return "", fmt.Errorf("%s did not say who we are", c.cfg.Endpoint)
}

// homes finds the calendar and address book homes for a principal. A server
// that has only one of them answers with only one.
func (c *Client) homes(ctx context.Context, principal string) (calendars, cards string, err error) {
	body := `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav">
	  <d:prop><c:calendar-home-set/><card:addressbook-home-set/></d:prop></d:propfind>`
	ms, err := c.propfind(ctx, principal, "0", body)
	if err != nil {
		return "", "", fmt.Errorf("home sets: %w", err)
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if href := strings.TrimSpace(ps.Prop.CalendarHome.Href); href != "" && calendars == "" {
				calendars = c.resolve(href)
			}
			if href := strings.TrimSpace(ps.Prop.AddressBookHome.Href); href != "" && cards == "" {
				cards = c.resolve(href)
			}
		}
	}
	if calendars == "" && cards == "" {
		return "", "", fmt.Errorf("%s has no calendar or address book home", principal)
	}
	return calendars, cards, nil
}

// list enumerates one home. A collection that holds neither events, tasks nor
// contacts — a scheduling inbox, the home itself — is not something a caller
// can be shown, so it is dropped here.
func (c *Client) list(ctx context.Context, home string) ([]davsync.Collection, error) {
	body := `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="http://apple.com/ns/ical/">
	  <d:prop>
	    <d:resourcetype/><d:displayname/>
	    <c:supported-calendar-component-set/>
	    <a:calendar-color/>
	  </d:prop></d:propfind>`
	ms, err := c.propfind(ctx, home, "1", body)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", home, err)
	}
	var out []davsync.Collection
	for _, r := range ms.Responses {
		href := c.resolve(strings.TrimSpace(r.Href))
		if href == "" || sameURL(href, home) {
			continue
		}
		for _, ps := range r.Propstats {
			if !strings.Contains(ps.Status, " 200 ") {
				continue
			}
			kind := kindOf(ps.Prop)
			if kind == "" {
				continue
			}
			name := strings.TrimSpace(ps.Prop.DisplayName)
			if name == "" {
				name = lastSegment(href)
			}
			out = append(out, davsync.Collection{
				Kind: kind, URL: href, Name: name,
				Color: strings.TrimSpace(ps.Prop.Color),
			})
		}
	}
	return out, nil
}

// kindOf decides what a collection holds. A calendar that says it supports
// VTODO and not VEVENT is a task list; one that says nothing is a calendar,
// because that is what the RFC's default means.
func kindOf(p prop) string {
	isCalendar, isAddressBook, isScheduling := false, false, false
	for _, t := range p.ResourceType.Types {
		switch {
		case t.Space == "urn:ietf:params:xml:ns:caldav" && t.Local == "calendar":
			isCalendar = true
		case t.Space == "urn:ietf:params:xml:ns:carddav" && t.Local == "addressbook":
			isAddressBook = true
		case strings.HasPrefix(t.Local, "schedule-"):
			isScheduling = true
		}
	}
	if isScheduling {
		return ""
	}
	if isAddressBook {
		return "cards"
	}
	if !isCalendar {
		return ""
	}
	events, todos := false, false
	for _, comp := range p.SupportedComponents.Comps {
		switch strings.ToUpper(comp.Name) {
		case "VEVENT":
			events = true
		case "VTODO":
			todos = true
		}
	}
	switch {
	case todos && !events:
		return "tasks"
	default:
		return "events"
	}
}

// Sync implements davsync.Driver with one sync-collection REPORT, asking for
// the object data in the same request. An empty token returns everything.
func (c *Client) Sync(ctx context.Context, collection, token string) (davsync.Changes, error) {
	data := dataElement(collection)
	body := fmt.Sprintf(`<d:sync-collection xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav">
	  <d:sync-token>%s</d:sync-token>
	  <d:sync-level>1</d:sync-level>
	  <d:prop><d:getetag/>%s</d:prop>
	</d:sync-collection>`, xmlEscape(token), data)

	ms, err := c.report(ctx, collection, "1", body)
	if err != nil {
		return davsync.Changes{}, err
	}
	out := davsync.Changes{Token: strings.TrimSpace(ms.SyncToken)}
	for _, r := range ms.Responses {
		href := strings.TrimSpace(r.Href)
		if href == "" || sameURL(c.resolve(href), collection) {
			continue
		}
		// A deleted object is a response that carries a status and no props.
		if r.Status != "" && !strings.Contains(r.Status, " 200 ") {
			out.Items = append(out.Items, davsync.Change{Href: pathOf(href), Deleted: true})
			continue
		}
		it := davsync.Change{Href: pathOf(href)}
		for _, ps := range r.Propstats {
			if !strings.Contains(ps.Status, " 200 ") {
				continue
			}
			it.ETag = strings.TrimSpace(ps.Prop.ETag)
			it.Data = objectData(ps.Prop)
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

// MultiGet implements davsync.Driver. It is only needed for a server that
// reports a change without the object, but that is most of them under some
// conditions, and one extra round trip for a batch is the cheapest possible
// answer to it.
func (c *Client) MultiGet(ctx context.Context, collection string, hrefs []string) ([]davsync.Change, error) {
	if len(hrefs) == 0 {
		return nil, nil
	}
	var out []davsync.Change
	// A multiget with a thousand hrefs is a request some servers refuse, so it
	// goes in batches.
	const batch = 100
	for start := 0; start < len(hrefs); start += batch {
		end := start + batch
		if end > len(hrefs) {
			end = len(hrefs)
		}
		var refs strings.Builder
		for _, href := range hrefs[start:end] {
			fmt.Fprintf(&refs, "<d:href>%s</d:href>", xmlEscape(href))
		}
		name, ns := "calendar-multiget", "urn:ietf:params:xml:ns:caldav"
		if isAddressBook(collection) {
			name, ns = "addressbook-multiget", "urn:ietf:params:xml:ns:carddav"
		}
		body := fmt.Sprintf(`<x:%s xmlns:d="DAV:" xmlns:x="%s" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav">
		  <d:prop><d:getetag/>%s</d:prop>%s</x:%s>`, name, ns, dataElement(collection), refs.String(), name)

		ms, err := c.report(ctx, collection, "1", body)
		if err != nil {
			return nil, err
		}
		for _, r := range ms.Responses {
			for _, ps := range r.Propstats {
				if !strings.Contains(ps.Status, " 200 ") {
					continue
				}
				out = append(out, davsync.Change{
					Href: pathOf(strings.TrimSpace(r.Href)),
					ETag: strings.TrimSpace(ps.Prop.ETag),
					Data: objectData(ps.Prop),
				})
			}
		}
	}
	return out, nil
}

// Put writes one object. ifMatch is the ETag the caller believes it is
// replacing — empty to create, and "*" to replace whatever is there. The
// returned ETag is what the server says the object is now; a server that does
// not return one leaves the caller to read it back (ADR-0004).
//
// Like every other write in this program it is tried once. A PUT whose ack was
// lost has still happened, and repeating it would overwrite whatever the other
// client did in between (ADR-0017).
func (c *Client) Put(ctx context.Context, href, data, ifMatch string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "PUT", href, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	req.Header.Set("Content-Type", contentTypeFor(href, data))
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return resp.Header.Get("ETag"), nil
	case http.StatusPreconditionFailed:
		// Somebody else changed it first. That is not our write to force.
		return "", fmt.Errorf("PUT %s: the object changed on the server since it was read", href)
	default:
		return "", fmt.Errorf("PUT %s: %s: %s", href, resp.Status, snippet(body))
	}
}

// Delete removes one object.
func (c *Client) Delete(ctx context.Context, href, ifMatch string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", href, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound:
		// Already gone is the state that was asked for.
		return nil
	case http.StatusPreconditionFailed:
		return fmt.Errorf("DELETE %s: the object changed on the server since it was read", href)
	default:
		return fmt.Errorf("DELETE %s: %s: %s", href, resp.Status, snippet(body))
	}
}

// MkCalendar creates a collection. It is used for the one collection this
// program owns rather than finds: the habits calendar, which has no home
// anywhere else (ADR-0018).
func (c *Client) MkCalendar(ctx context.Context, href, displayName string, comps []string) error {
	var set strings.Builder
	for _, comp := range comps {
		fmt.Fprintf(&set, `<c:comp name="%s"/>`, xmlEscape(comp))
	}
	body := fmt.Sprintf(`<c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
	  <d:set><d:prop>
	    <d:displayname>%s</d:displayname>
	    <c:supported-calendar-component-set>%s</c:supported-calendar-component-set>
	  </d:prop></d:set></c:mkcalendar>`, xmlEscape(displayName), set.String())

	req, err := http.NewRequestWithContext(ctx, "MKCALENDAR", href, strings.NewReader(xmlHeader+body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent,
		http.StatusMethodNotAllowed, http.StatusConflict:
		// The last two mean it is already there, which is what was wanted.
		return nil
	default:
		return fmt.Errorf("MKCALENDAR %s: %s: %s", href, resp.Status, snippet(payload))
	}
}

// DeleteCollection removes a whole collection. Only a collection this program
// created is ever passed to it.
func (c *Client) DeleteCollection(ctx context.Context, href string) error {
	return c.Delete(ctx, href, "")
}

// contentTypeFor names what is being written. A vCard sent as text/calendar is
// refused by a server that checks, and stored wrong by one that does not.
func contentTypeFor(href, data string) string {
	if strings.Contains(data, "BEGIN:VCARD") || isAddressBook(href) {
		return "text/vcard; charset=utf-8"
	}
	return "text/calendar; charset=utf-8"
}

// dataElement asks for the right kind of payload for the collection.
func dataElement(collection string) string {
	if isAddressBook(collection) {
		return `<card:address-data/>`
	}
	return `<c:calendar-data/>`
}

// isAddressBook guesses from the URL, which is the only thing the driver has
// per request. Asking for both payload types is harmless where it guesses
// wrong, because objectData takes whichever came back.
func isAddressBook(collection string) bool {
	return strings.Contains(collection, "carddav") || strings.Contains(collection, "addressbook")
}

func objectData(p prop) string {
	if s := strings.TrimSpace(p.CalendarData); s != "" {
		return s
	}
	return strings.TrimSpace(p.AddressData)
}

func (c *Client) propfind(ctx context.Context, url, depth, body string) (*multistatus, error) {
	return c.do(ctx, "PROPFIND", url, depth, body)
}

func (c *Client) report(ctx context.Context, url, depth, body string) (*multistatus, error) {
	return c.do(ctx, "REPORT", url, depth, body)
}

func (c *Client) do(ctx context.Context, method, url, depth, body string) (*multistatus, error) {
	full := xmlHeader + body
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(full))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", depth)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusMultiStatus {
		// A token the server no longer knows comes back as a precondition
		// failure naming valid-sync-token. That is not an error to report to a
		// caller; it means start again (RFC 6578 §3.2).
		//
		// The element has to be there. Treating every 403 as an expired token
		// turns a permission problem into a full resync on every cycle,
		// forever, with nothing in the log to say why.
		if bytes.Contains(payload, []byte("valid-sync-token")) {
			return nil, fmt.Errorf("%s %s: %w", method, url, davsync.ErrTokenExpired)
		}
		return nil, fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, snippet(payload))
	}
	var ms multistatus
	if err := xml.Unmarshal(payload, &ms); err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	return &ms, nil
}

// resolve turns an href from a response into an absolute URL. Servers answer
// with paths, and a path stored as a collection URL would not survive being
// used as one.
func (c *Client) resolve(href string) string {
	if href == "" {
		return ""
	}
	base, err := url.Parse(c.cfg.Endpoint)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

// pathOf reduces an href to the path a server uses to name it. Servers answer
// with paths and accept them in multiget, and a write has to be able to produce
// an href in the same shape — otherwise the object we wrote and the same object
// coming back on the next sync are two rows in the Mirror.
func pathOf(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Path == "" {
		return strings.TrimSpace(href)
	}
	return u.Path
}

func sameURL(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

func lastSegment(href string) string {
	trimmed := strings.TrimSuffix(href, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

const xmlHeader = `<?xml version="1.0" encoding="utf-8"?>` + "\n"

// The XML this program reads. Only the parts it acts on are modelled: a
// multistatus carries a great deal more, and ignoring it is the point.
type multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []response `xml:"DAV: response"`
	SyncToken string     `xml:"DAV: sync-token"`
}

type response struct {
	Href      string     `xml:"DAV: href"`
	Status    string     `xml:"DAV: status"`
	Propstats []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string `xml:"DAV: status"`
	Prop   prop   `xml:"DAV: prop"`
}

type prop struct {
	DisplayName          string       `xml:"DAV: displayname"`
	ETag                 string       `xml:"DAV: getetag"`
	ResourceType         resourceType `xml:"DAV: resourcetype"`
	CurrentUserPrincipal hrefValue    `xml:"DAV: current-user-principal"`
	CalendarHome         hrefValue    `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	AddressBookHome      hrefValue    `xml:"urn:ietf:params:xml:ns:carddav addressbook-home-set"`
	SupportedComponents  compSet      `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	CalendarData         string       `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	AddressData          string       `xml:"urn:ietf:params:xml:ns:carddav address-data"`
	Color                string       `xml:"http://apple.com/ns/ical/ calendar-color"`
}

type hrefValue struct {
	Href string `xml:"DAV: href"`
}

type resourceType struct {
	Types []xml.Name `xml:",any"`
}

type compSet struct {
	Comps []comp `xml:"urn:ietf:params:xml:ns:caldav comp"`
}

type comp struct {
	Name string `xml:"name,attr"`
}

var _ davsync.Driver = (*Client)(nil)

// Static makes a Client that does not discover: it reports the collections it
// was told about. It is for a server that is not the account's own — a work
// calendar at another provider, with its own credentials — where there is a URL
// and nothing to enumerate from.
func Static(cfg Config, collections ...davsync.Collection) *Client {
	c := New(cfg)
	c.static = collections
	return c
}

// Set drives several DAV servers as one. The account's own server is
// discovered; the others are whatever was configured. Requests are routed by
// host, because that is the only thing a collection URL carries that says which
// server it is on.
type Set struct {
	clients []*Client
}

// NewSet groups clients. The first one is the fallback for a URL whose host
// matches none of them.
func NewSet(clients ...*Client) *Set { return &Set{clients: clients} }

// Collections implements davsync.Driver over every server.
//
// A server that does not answer is reported *with* the collections the others
// gave: the answer is real but partial, and the caller must not read "these are
// all the collections there are" out of it. Discovery prunes what has
// disappeared (davsync.Discover), and pruning on a partial answer would delete
// an unreachable server's calendars rather than wait for it to come back.
func (s *Set) Collections(ctx context.Context) ([]davsync.Collection, error) {
	var out []davsync.Collection
	var firstErr error
	for _, c := range s.clients {
		found, err := c.Collections(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, found...)
	}
	return out, firstErr
}

// Sync implements davsync.Driver against whichever server holds the collection.
func (s *Set) Sync(ctx context.Context, collection, token string) (davsync.Changes, error) {
	return s.clientFor(collection).Sync(ctx, collection, token)
}

// MultiGet implements davsync.Driver against whichever server holds the
// collection.
func (s *Set) MultiGet(ctx context.Context, collection string, hrefs []string) ([]davsync.Change, error) {
	return s.clientFor(collection).MultiGet(ctx, collection, hrefs)
}

// Put implements davsync.WriteDriver against whichever server holds the object.
func (s *Set) Put(ctx context.Context, href, data, ifMatch string) (string, error) {
	return s.clientFor(href).Put(ctx, href, data, ifMatch)
}

// Delete implements davsync.WriteDriver against whichever server holds it.
func (s *Set) Delete(ctx context.Context, href, ifMatch string) error {
	return s.clientFor(href).Delete(ctx, href, ifMatch)
}

func (s *Set) clientFor(collection string) *Client {
	want := hostOf(collection)
	for _, c := range s.clients {
		if hostOf(c.cfg.Endpoint) == want {
			return c
		}
		for _, col := range c.static {
			if hostOf(col.URL) == want {
				return c
			}
		}
	}
	return s.clients[0]
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

var _ davsync.WriteDriver = (*Set)(nil)
var _ davsync.WriteDriver = (*Client)(nil)
