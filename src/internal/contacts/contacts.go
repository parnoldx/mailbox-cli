// Package contacts: CardDAV over raw REPORT/PUT (replaces the caldav lib usage).
package contacts

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/dav"
	"mailbox/src/internal/format"
	"mailbox/src/internal/vobject"
)

const query = `<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
  <d:prop>
    <d:getetag/>
    <c:address-data/>
  </d:prop>
</c:addressbook-query>`

type Contacts struct {
	acct   *config.Account
	client *dav.Client
	URL    string
}

func New(acct *config.Account) (*Contacts, error) {
	if acct.KontakteURL == "" {
		return nil, fmt.Errorf("missing MAILBOX_CARDDAV_KONTAKTE")
	}
	return &Contacts{acct: acct, client: dav.New(acct.Email, acct.Password), URL: acct.KontakteURL}, nil
}

func uidMatches(full, query string) bool {
	if query == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(full), strings.ToLower(query))
}

func first(props []vobject.Prop, name string) string { return vobject.First(props, name) }

func emails(props []vobject.Prop) []string {
	var out []string
	for _, p := range props {
		if p.Name == "EMAIL" && p.Value != "" {
			out = append(out, p.Value)
		}
	}
	return out
}

func allOf(props []vobject.Prop, name string) []string {
	var out []string
	for _, p := range props {
		if p.Name == name && p.Value != "" {
			out = append(out, p.Value)
		}
	}
	return out
}

// displayName falls back to N ("Family;Given") when FN is empty.
func displayName(props []vobject.Prop) string {
	if fn := first(props, "FN"); fn != "" {
		return fn
	}
	parts := strings.SplitN(first(props, "N"), ";", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[1] + " " + parts[0]
	}
	return parts[0]
}

// fmtAdr renders an ADR value (po;ext;street;locality;region;postal;country) as one line.
func fmtAdr(a string) string {
	fields := strings.Split(a, ";")
	for len(fields) < 7 {
		fields = append(fields, "")
	}
	parts := []string{fields[2], fields[5] + " " + fields[3], fields[6]}
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

func revKey(props []vobject.Prop) string {
	r := strings.NewReplacer("-", "", ":", "")
	return r.Replace(first(props, "REV"))
}

func revDate(props []vobject.Prop) string {
	rev := first(props, "REV")
	if len(rev) >= 10 {
		return rev[:10]
	}
	return rev
}

func row(props []vobject.Prop, detail bool) *format.OM {
	mails := emails(props)
	email := ""
	if len(mails) > 0 {
		email = mails[0]
	}
	if !detail {
		return format.NewOM("id", shortID(first(props, "UID")), "name", displayName(props), "email", email, "updated", revDate(props))
	}
	row := format.NewOM(
		"id", shortID(first(props, "UID")),
		"name", displayName(props),
		"email", email,
		"emails", strings.Join(mails, ", "),
		"updated", revDate(props),
	)
	if tels := allOf(props, "TEL"); len(tels) > 0 {
		row.Set("tel", strings.Join(tels, ", "))
	}
	if bday := first(props, "BDAY"); bday != "" {
		row.Set("bday", bday)
	}
	if adrs := allOf(props, "ADR"); len(adrs) > 0 {
		var lines []string
		for _, a := range adrs {
			if s := fmtAdr(a); s != "" {
				lines = append(lines, s)
			}
		}
		if len(lines) > 0 {
			row.Set("address", strings.Join(lines, " | "))
		}
	}
	if note := first(props, "NOTE"); note != "" {
		row.Set("note", note)
	}
	return row
}

func nFromName(name string) string {
	parts := strings.SplitN(strings.TrimSpace(name), " ", 2)
	if len(parts) == 2 {
		return vobject.Escape(parts[1]) + ";" + vobject.Escape(parts[0]) + ";;;"
	}
	return vobject.Escape(name) + ";;;;"
}

type entry struct {
	href  string
	etag  string
	props []vobject.Prop
}

// ponytail: one JSON file, no TTL; edits from other clients show up only
// after an explicit `contacts refresh` — per-entry revalidation if that bites.
var (
	cachePathFunc = defaultCachePath
)

func defaultCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mailbox", "contacts.json"), nil
}

type cacheEntry struct {
	Href  string         `json:"href"`
	Etag  string         `json:"etag"`
	Props []vobject.Prop `json:"props"`
}

type cacheDoc struct {
	Fetched time.Time     `json:"fetched"`
	Entries []*cacheEntry `json:"entries"`
}

func loadCache() ([]entry, bool) {
	path, err := cachePathFunc()
	if err != nil {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc cacheDoc
	if json.Unmarshal(raw, &doc) != nil || doc.Fetched.IsZero() {
		return nil, false
	}
	out := make([]entry, 0, len(doc.Entries))
	for _, ce := range doc.Entries {
		out = append(out, entry{href: ce.Href, etag: ce.Etag, props: ce.Props})
	}
	return out, true
}

func saveCache(entries []entry) {
	path, err := cachePathFunc()
	if err != nil {
		return
	}
	doc := cacheDoc{Fetched: time.Now(), Entries: make([]*cacheEntry, 0, len(entries))}
	for _, e := range entries {
		doc.Entries = append(doc.Entries, &cacheEntry{Href: e.href, Etag: e.etag, Props: e.props})
	}
	blob, err := json.Marshal(doc)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		os.WriteFile(path, blob, 0o600)
	}
}

func invalidateCache() {
	if path, err := cachePathFunc(); err == nil {
		os.Remove(path)
	}
}

// Refresh discards the cache and re-reads the address book.
func (c *Contacts) Refresh() (int, error) {
	invalidateCache()
	es, err := c.entries()
	if err != nil {
		return 0, err
	}
	return len(es), nil
}

func (c *Contacts) entries() ([]entry, error) {
	if es, ok := loadCache(); ok {
		return es, nil
	}
	raw, status, err := c.client.Report(c.URL, query, "1")
	if err != nil {
		return nil, err
	}
	if status != 200 && status != 207 {
		return nil, fmt.Errorf("CardDAV report failed: %d", status)
	}
	var out []entry
	for _, resp := range dav.ParseMultistatus(raw, "address-data") {
		props := vobject.ParseLines(resp.Data)
		hasUID := false
		for _, p := range props {
			if p.Name == "UID" {
				hasUID = true
				break
			}
		}
		if !hasUID {
			continue
		}
		out = append(out, entry{href: resp.Href, etag: resp.Etag, props: props})
	}
	latest := map[string]entry{}
	for _, e := range out {
		uid := first(e.props, "UID")
		prev, ok := latest[uid]
		if !ok || revKey(e.props) >= revKey(prev.props) {
			latest[uid] = e
		}
	}
	values := make([]entry, 0, len(latest))
	for _, v := range latest {
		values = append(values, v)
	}
	saveCache(values)
	return values, nil
}

func setNamed(props []vobject.Prop, name, value string) []vobject.Prop {
	for i := range props {
		if props[i].Name == name {
			if value == "" {
				props = append(props[:i], props[i+1:]...)
			} else {
				props[i].Value = value
			}
			return props
		}
	}
	if value == "" {
		return props
	}
	insertAt := len(props)
	for i, p := range props {
		if p.Name == "END" {
			insertAt = i
			break
		}
	}
	props = append(props, vobject.Prop{})
	copy(props[insertAt+1:], props[insertAt:])
	props[insertAt] = vobject.Prop{Name: name, Value: value}
	return props
}

func setPrimaryEmail(props []vobject.Prop, email string) []vobject.Prop {
	for i := range props {
		if props[i].Name == "EMAIL" {
			props[i].Value = email
			return props
		}
	}
	insertAt := len(props)
	for i, p := range props {
		if p.Name == "END" {
			insertAt = i
			break
		}
	}
	props = append(props, vobject.Prop{})
	copy(props[insertAt+1:], props[insertAt:])
	props[insertAt] = vobject.Prop{Name: "EMAIL", Params: ";TYPE=INTERNET", Value: email}
	return props
}

func (c *Contacts) List() ([]*format.OM, error) {
	entries, err := c.entries()
	if err != nil {
		return nil, err
	}
	rows := make([]*format.OM, 0, len(entries))
	for _, e := range entries {
		if first(e.props, "FN") == "" && len(emails(e.props)) == 0 {
			continue
		}
		rows = append(rows, row(e.props, false))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ki := strings.ToLower(strOr(rows[i].Get("name"))) + "|" + strings.ToLower(strOr(rows[i].Get("email")))
		kj := strings.ToLower(strOr(rows[j].Get("name"))) + "|" + strings.ToLower(strOr(rows[j].Get("email")))
		return ki < kj
	})
	return rows, nil
}

// Search filters List rows by a case-insensitive substring over name, email, and id.
func (c *Contacts) Search(q string) ([]*format.OM, error) {
	rows, err := c.List()
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(q)
	out := make([]*format.OM, 0, len(rows))
	for _, r := range rows {
		haystack := strings.ToLower(strOr(r.Get("name")) + "\n" + strOr(r.Get("email")) + "\n" + strOr(r.Get("id")))
		if strings.Contains(haystack, needle) {
			out = append(out, r)
		}
	}
	return out, nil
}

func strOr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func (c *Contacts) Show(uid string) (*format.OM, error) {
	e, err := c.lookup(uid)
	if err != nil {
		return nil, err
	}
	return row(e.props, true), nil
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := fmt.Sprintf("%x", b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

func (c *Contacts) Add(name, email, note string) (string, error) {
	uid := newUUID()
	props := []vobject.Prop{
		{Name: "BEGIN", Value: "VCARD"},
		{Name: "VERSION", Value: "3.0"},
		{Name: "UID", Value: uid},
		{Name: "FN", Value: name},
		{Name: "N", Value: nFromName(name)},
		{Name: "EMAIL", Params: ";TYPE=INTERNET", Value: email},
	}
	if note != "" {
		props = append(props, vobject.Prop{Name: "NOTE", Value: note})
	}
	props = append(props, vobject.Prop{Name: "END", Value: "VCARD"})
	href := joinURL(baseOf(c.URL), uid+".vcf")
	status, err := c.client.Put(href, vobject.Serialize(props),
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	if err != nil || (status != 200 && status != 201 && status != 204) {
		return "", fmt.Errorf("CardDAV put failed: %d", status)
	}
	invalidateCache()
	return uid, nil
}

func baseOf(u string) string {
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// joinURL resolves an href against the collection base like python's urljoin.
func joinURL(base, path string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base + path
	}
	ref, err := url.Parse(path)
	if err != nil {
		return base + path
	}
	return parsed.ResolveReference(ref).String()
}

func (c *Contacts) Update(uid string, name, email, note *string) (*format.OM, error) {
	e, err := c.lookup(uid)
	if err != nil {
		return nil, err
	}
	props := e.props
	if name != nil {
		props = setNamed(props, "FN", *name)
		props = setNamed(props, "N", nFromName(*name))
	}
	if email != nil {
		props = setPrimaryEmail(props, *email)
	}
	if note != nil {
		props = setNamed(props, "NOTE", *note)
	}
	headers := map[string]string{"Content-Type": "text/vcard; charset=utf-8"}
	putURL := e.href
	if !strings.HasPrefix(putURL, "http://") && !strings.HasPrefix(putURL, "https://") {
		putURL = joinURL(baseOf(c.URL), strings.TrimPrefix(e.href, "/"))
	}
	if e.etag != "" {
		headers["If-Match"] = e.etag
	}
	status, err := c.client.Put(putURL, vobject.Serialize(props), headers)
	if err != nil || (status != 200 && status != 201 && status != 204) {
		return nil, fmt.Errorf("CardDAV put failed: %d", status)
	}
	invalidateCache()
	return row(props, true), nil
}

func (c *Contacts) lookup(uid string) (entry, error) {
	var scored []entry
	for _, e := range mustEntries(c.entries()) {
		full := first(e.props, "UID")
		if uidMatches(full, uid) {
			scored = append(scored, e)
		}
	}
	if len(scored) == 0 {
		return entry{}, fmt.Errorf("contact not found: %s", uid)
	}
	unique := map[string]entry{}
	order := []string{}
	for _, e := range scored {
		full := first(e.props, "UID")
		if _, seen := unique[full]; !seen {
			order = append(order, full)
		}
		unique[full] = e
	}
	if len(unique) > 1 {
		sort.Strings(order)
		return entry{}, fmt.Errorf("ambiguous contact id %q, matches:\n%s", uid, strings.Join(order, "\n"))
	}
	return unique[order[0]], nil
}

func mustEntries(entries []entry, err error) []entry {
	if err != nil {
		return nil
	}
	return entries
}

func shortID(uid string) string {
	if len(uid) > 8 {
		return uid[:8]
	}
	return uid
}
