package graphdrv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeGraph is a scripted Graph: enough of mail folders, messages, events and
// contacts, with deltas that page and a change log they are read from, to drive
// the drivers through what the real one does. Ids are made up and every
// address is example.com.
type fakeGraph struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	folders  []fakeFolder
	messages map[string]*fakeMessage
	events   map[string]map[string]any
	// instances are a series' occurrences and exceptions, by master id.
	instances map[string][]map[string]any
	contacts  map[string]map[string]any
	// log is every change after the start, which a delta link reads from.
	log []fakeChange
	// gone makes the next delta of a collection a 410.
	gone map[string]bool
	// pageSize makes every first delta page, to exercise nextLink.
	pageSize int
	next     int
	// calls is every write, as "METHOD /path body".
	calls []string
}

type fakeFolder struct{ id, name, parent, wellKnown string }

type fakeMessage struct {
	id, folder string
	read       bool
	flagged    bool
	categories []string
	mime       string
}

type fakeChange struct {
	collection, id string
	removed        bool
}

func newFakeGraph(t *testing.T) (*fakeGraph, *Client) {
	f := &fakeGraph{
		t: t, messages: map[string]*fakeMessage{}, events: map[string]map[string]any{},
		instances: map[string][]map[string]any{}, contacts: map[string]map[string]any{},
		gone: map[string]bool{}, pageSize: 2,
	}
	f.folders = []fakeFolder{
		{id: "f-inbox", name: "Posteingang", wellKnown: "inbox"},
		{id: "f-sent", name: "Gesendete Elemente", wellKnown: "sentitems"},
		{id: "f-drafts", name: "Entwürfe", wellKnown: "drafts"},
		{id: "f-trash", name: "Gelöschte Elemente", wellKnown: "deleteditems"},
		{id: "f-outbox", name: "Postausgang", wellKnown: "outbox"},
		{id: "f-projects", name: "Projekte", parent: "f-inbox"},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	c := &Client{Base: f.srv.URL + "/v1.0", HTTP: f.srv.Client(),
		Token: func(context.Context) (string, error) { return "token", nil }}
	return f, c
}

func (f *fakeGraph) newID(prefix string) string {
	f.next++
	return fmt.Sprintf("%s-%d", prefix, f.next)
}

// deliver puts a message in a folder, as mail arriving.
func (f *fakeGraph) deliver(folder, subject, messageID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.newID("m")
	f.messages[id] = &fakeMessage{id: id, folder: folder, mime: fmt.Sprintf(
		"From: Sender <sender@example.com>\r\nTo: me@example.com\r\nSubject: %s\r\nMessage-ID: <%s>\r\n"+
			"Date: Sat, 26 Sep 2026 10:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody of %s\r\n",
		subject, messageID, subject)}
	f.log = append(f.log, fakeChange{collection: folder, id: id})
	return id
}

func (f *fakeGraph) change(collection, id string, removed bool) {
	f.log = append(f.log, fakeChange{collection: collection, id: id, removed: removed})
}

func (f *fakeGraph) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGraph) fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	f.writeJSON(w, map[string]any{"error": map[string]string{"code": code, "message": code}})
}

func (f *fakeGraph) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v1.0")
	body, _ := io.ReadAll(r.Body)
	if r.Method != http.MethodGet {
		f.calls = append(f.calls, strings.TrimSpace(r.Method+" "+path+" "+string(body)))
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/$batch":
		f.batch(w, body)
	case path == "/me":
		f.writeJSON(w, map[string]string{"mail": "me@example.com"})
	case path == "/delta":
		f.delta(w, r.URL.Query())
	case strings.HasPrefix(path, "/me/mailFolders"):
		f.mailFolders(w, r, parts, body)
	case path == "/me/messages" && r.Method == http.MethodPost:
		raw, _ := base64.StdEncoding.DecodeString(string(body))
		id := f.newID("m")
		f.messages[id] = &fakeMessage{id: id, folder: "f-drafts", mime: string(raw), read: true}
		f.writeJSON(w, f.messageJSON(f.messages[id]))
	case path == "/me/sendMail":
		w.WriteHeader(http.StatusAccepted)
	case strings.HasPrefix(path, "/me/messages/"):
		f.message(w, r, parts, body)
	case strings.HasPrefix(path, "/me/calendars"):
		f.calendars(w, r, parts, body)
	case strings.HasPrefix(path, "/me/events/"):
		f.event(w, r, parts, body)
	case strings.HasPrefix(path, "/me/contactFolders") || strings.HasPrefix(path, "/me/contacts"):
		f.contactRoutes(w, r, parts, body)
	default:
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
	}
}

func (f *fakeGraph) mailFolders(w http.ResponseWriter, r *http.Request, parts []string, body []byte) {
	// /me/mailFolders, /me/mailFolders/{id|wellknown}, .../childFolders, .../messages/delta
	if len(parts) == 2 {
		if r.Method == http.MethodPost {
			f.newFolder(w, "", body)
			return
		}
		f.folderList(w, "")
		return
	}
	id := parts[2]
	for _, fo := range f.folders {
		if fo.wellKnown == id {
			id = fo.id
		}
	}
	switch {
	case len(parts) == 3:
		for _, fo := range f.folders {
			if fo.id == id {
				f.writeJSON(w, map[string]string{"id": fo.id})
				return
			}
		}
		f.fail(w, http.StatusNotFound, "ErrorFolderNotFound")
	case parts[3] == "childFolders" && r.Method == http.MethodPost:
		f.newFolder(w, id, body)
	case parts[3] == "childFolders":
		f.folderList(w, id)
	case parts[3] == "messages" && len(parts) == 5 && parts[4] == "delta":
		if f.gone[id] {
			delete(f.gone, id)
			f.fail(w, http.StatusGone, "SyncStateNotFound")
			return
		}
		var items []map[string]any
		for _, m := range f.sortedMessages() {
			if m.folder == id {
				items = append(items, f.messageJSON(m))
			}
		}
		f.page(w, id, items, 0)
	default:
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
	}
}

func (f *fakeGraph) newFolder(w http.ResponseWriter, parent string, body []byte) {
	var in struct {
		DisplayName string `json:"displayName"`
	}
	_ = json.Unmarshal(body, &in)
	fo := fakeFolder{id: f.newID("f"), name: in.DisplayName, parent: parent}
	f.folders = append(f.folders, fo)
	f.writeJSON(w, map[string]string{"id": fo.id, "displayName": fo.name})
}

func (f *fakeGraph) folderList(w http.ResponseWriter, parent string) {
	var out []map[string]any
	for _, fo := range f.folders {
		if fo.parent != parent {
			continue
		}
		children := 0
		for _, c := range f.folders {
			if c.parent == fo.id {
				children++
			}
		}
		out = append(out, map[string]any{"id": fo.id, "displayName": fo.name, "childFolderCount": children})
	}
	f.writeJSON(w, map[string]any{"value": out})
}

func (f *fakeGraph) sortedMessages() []*fakeMessage {
	var out []*fakeMessage
	for _, m := range f.messages {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b *fakeMessage) int { return strings.Compare(a.id, b.id) })
	return out
}

func (f *fakeGraph) messageJSON(m *fakeMessage) map[string]any {
	status := "notFlagged"
	if m.flagged {
		status = "flagged"
	}
	cats := m.categories
	if cats == nil {
		cats = []string{}
	}
	return map[string]any{"id": m.id, "isRead": m.read, "isDraft": m.folder == "f-drafts",
		"flag": map[string]string{"flagStatus": status}, "categories": cats}
}

// page answers a first delta: pageSize items and a nextLink, then the rest and
// a deltaLink at the current end of the log.
func (f *fakeGraph) page(w http.ResponseWriter, collection string, items []map[string]any, offset int) {
	end := min(offset+f.pageSize, len(items))
	resp := map[string]any{"value": items[offset:end]}
	if end < len(items) {
		resp["@odata.nextLink"] = fmt.Sprintf("%s/v1.0/delta?c=%s&first=1&offset=%d", f.srv.URL, collection, end)
	} else {
		resp["@odata.deltaLink"] = fmt.Sprintf("%s/v1.0/delta?c=%s&seq=%d", f.srv.URL, collection, len(f.log))
	}
	f.writeJSON(w, resp)
}

// delta answers a nextLink or a deltaLink.
func (f *fakeGraph) delta(w http.ResponseWriter, q url.Values) {
	c := q.Get("c")
	if q.Get("first") != "" {
		offset, _ := strconv.Atoi(q.Get("offset"))
		f.page(w, c, f.initial(c), offset)
		return
	}
	if f.gone[c] {
		delete(f.gone, c)
		f.fail(w, http.StatusGone, "SyncStateNotFound")
		return
	}
	seq, _ := strconv.Atoi(q.Get("seq"))
	var items []map[string]any
	for _, ch := range f.log[seq:] {
		if ch.collection != c {
			continue
		}
		items = append(items, f.itemOf(ch))
	}
	f.writeJSON(w, map[string]any{"value": items,
		"@odata.deltaLink": fmt.Sprintf("%s/v1.0/delta?c=%s&seq=%d", f.srv.URL, c, len(f.log))})
}

// initial is everything a first delta of a collection returns.
func (f *fakeGraph) initial(c string) []map[string]any {
	var items []map[string]any
	for _, m := range f.sortedMessages() {
		if m.folder == c {
			items = append(items, f.messageJSON(m))
		}
	}
	for _, id := range sortedKeys(f.events) {
		e := f.events[id]
		if e["calendar"] != c {
			continue
		}
		if e["type"] == "seriesMaster" {
			for _, in := range f.instances[id] {
				items = append(items, in)
			}
			continue
		}
		items = append(items, e)
	}
	for _, id := range sortedKeys(f.contacts) {
		if f.contacts[id]["folder"] == c {
			items = append(items, f.contacts[id])
		}
	}
	return items
}

func (f *fakeGraph) itemOf(ch fakeChange) map[string]any {
	if ch.removed {
		return map[string]any{"id": ch.id, "@removed": map[string]string{"reason": "deleted"}}
	}
	if m, ok := f.messages[ch.id]; ok {
		return f.messageJSON(m)
	}
	if e, ok := f.events[ch.id]; ok {
		return e
	}
	for _, list := range f.instances {
		for _, in := range list {
			if in["id"] == ch.id {
				return in
			}
		}
	}
	if c, ok := f.contacts[ch.id]; ok {
		return c
	}
	return map[string]any{"id": ch.id, "@removed": map[string]string{"reason": "deleted"}}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (f *fakeGraph) batch(w http.ResponseWriter, body []byte) {
	var in struct {
		Requests []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"requests"`
	}
	_ = json.Unmarshal(body, &in)
	var out []map[string]any
	for _, r := range in.Requests {
		u, _ := url.Parse(r.URL)
		id, _ := url.PathUnescape(strings.TrimPrefix(u.Path, "/me/messages/"))
		m, ok := f.messages[id]
		if !ok {
			out = append(out, map[string]any{"id": r.ID, "status": 404, "body": map[string]any{}})
			continue
		}
		out = append(out, map[string]any{"id": r.ID, "status": 200, "body": f.envelopeJSON(m)})
	}
	f.writeJSON(w, map[string]any{"responses": out})
}

// envelopeJSON is a message as $select'ed for its envelope, read out of its MIME.
func (f *fakeGraph) envelopeJSON(m *fakeMessage) map[string]any {
	out := f.messageJSON(m)
	var headers []map[string]string
	subject, msgid := "", ""
	head, _, _ := strings.Cut(m.mime, "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		name, value, _ := strings.Cut(l, ": ")
		headers = append(headers, map[string]string{"name": name, "value": value})
		switch name {
		case "Subject":
			subject = value
		case "Message-ID":
			msgid = value
		}
	}
	out["subject"] = subject
	out["internetMessageId"] = msgid
	out["from"] = map[string]any{"emailAddress": map[string]string{"name": "Sender", "address": "sender@example.com"}}
	out["toRecipients"] = []any{map[string]any{"emailAddress": map[string]string{"address": "me@example.com"}}}
	out["sentDateTime"] = "2026-09-26T10:00:00Z"
	out["receivedDateTime"] = "2026-09-26T10:00:05Z"
	out["internetMessageHeaders"] = headers
	out["singleValueExtendedProperties"] = []any{map[string]string{"id": "Integer 0xe08", "value": strconv.Itoa(len(m.mime))}}
	return out
}

func (f *fakeGraph) message(w http.ResponseWriter, r *http.Request, parts []string, body []byte) {
	id, _ := url.PathUnescape(parts[2])
	m, ok := f.messages[id]
	if !ok {
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
		return
	}
	switch {
	case len(parts) == 4 && parts[3] == "$value":
		_, _ = io.WriteString(w, m.mime)
	case len(parts) == 4 && parts[3] == "move":
		var in struct {
			DestinationID string `json:"destinationId"`
		}
		_ = json.Unmarshal(body, &in)
		f.change(m.folder, m.id, true)
		m.folder = in.DestinationID
		f.change(m.folder, m.id, false)
		f.writeJSON(w, f.messageJSON(m))
	case r.Method == http.MethodPatch:
		var in map[string]json.RawMessage
		_ = json.Unmarshal(body, &in)
		if v, ok := in["isRead"]; ok {
			m.read = string(v) == "true"
		}
		if v, ok := in["flag"]; ok {
			m.flagged = strings.Contains(string(v), `"flagged"`)
		}
		if v, ok := in["categories"]; ok {
			m.categories = nil
			_ = json.Unmarshal(v, &m.categories)
		}
		f.change(m.folder, m.id, false)
		f.writeJSON(w, f.messageJSON(m))
	default:
		f.writeJSON(w, f.messageJSON(m))
	}
}

func (f *fakeGraph) calendars(w http.ResponseWriter, r *http.Request, parts []string, body []byte) {
	if len(parts) == 2 {
		f.writeJSON(w, map[string]any{"value": []any{
			map[string]any{"id": "cal-1", "name": "Kalender", "hexColor": "#1e90ff", "isDefaultCalendar": true},
			map[string]any{"id": "cal-2", "name": "Feiertage", "hexColor": "", "isDefaultCalendar": false},
		}})
		return
	}
	cal := parts[2]
	switch {
	case len(parts) == 5 && parts[3] == "calendarView" && parts[4] == "delta":
		f.page(w, cal, f.initial(cal), 0)
	case len(parts) == 4 && parts[3] == "events" && r.Method == http.MethodPost:
		var e map[string]any
		_ = json.Unmarshal(body, &e)
		id := f.newID("ev")
		e["id"], e["iCalUId"], e["changeKey"], e["calendar"] = id, "ICALUID-"+id, "ck-"+id, cal
		e["type"] = "singleInstance"
		if e["recurrence"] != nil {
			e["type"] = "seriesMaster"
		}
		f.events[id] = e
		f.change(cal, id, false)
		f.writeJSON(w, e)
	default:
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
	}
}

func (f *fakeGraph) event(w http.ResponseWriter, r *http.Request, parts []string, body []byte) {
	id, _ := url.PathUnescape(parts[2])
	e, ok := f.events[id]
	if !ok {
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
		return
	}
	switch {
	case len(parts) == 4 && parts[3] == "instances":
		f.writeJSON(w, map[string]any{"value": f.instances[id]})
	case r.Method == http.MethodPatch:
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		for k, v := range in {
			e[k] = v
		}
		e["changeKey"] = fmt.Sprintf("%v+", e["changeKey"])
		f.change(fmt.Sprint(e["calendar"]), id, false)
		f.writeJSON(w, e)
	case r.Method == http.MethodDelete:
		delete(f.events, id)
		f.change(fmt.Sprint(e["calendar"]), id, true)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.writeJSON(w, e)
	}
}

func (f *fakeGraph) contactRoutes(w http.ResponseWriter, r *http.Request, parts []string, body []byte) {
	switch {
	case len(parts) == 2 && parts[1] == "contacts":
		// /me/contacts?$top=1: any contact in the default folder.
		for _, id := range sortedKeys(f.contacts) {
			if f.contacts[id]["folder"] == "cf-default" {
				f.writeJSON(w, map[string]any{"value": []any{map[string]string{"parentFolderId": "cf-default"}}})
				return
			}
		}
		f.writeJSON(w, map[string]any{"value": []any{}})
	case len(parts) == 2:
		f.writeJSON(w, map[string]any{"value": []any{map[string]string{"id": "cf-clients", "displayName": "Kunden"}}})
	case parts[1] == "contactFolders" && len(parts) == 5 && parts[4] == "delta":
		if f.gone[parts[2]] {
			delete(f.gone, parts[2])
			f.fail(w, http.StatusGone, "SyncStateNotFound")
			return
		}
		f.page(w, parts[2], f.initial(parts[2]), 0)
	case parts[1] == "contactFolders" && len(parts) == 4 && r.Method == http.MethodPost:
		var c map[string]any
		_ = json.Unmarshal(body, &c)
		id := f.newID("c")
		c["id"], c["changeKey"], c["folder"] = id, "ck-"+id, parts[2]
		f.contacts[id] = c
		f.change(parts[2], id, false)
		f.writeJSON(w, c)
	case parts[1] == "contacts" && len(parts) == 3:
		id, _ := url.PathUnescape(parts[2])
		c, ok := f.contacts[id]
		if !ok {
			f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			for k, v := range in {
				c[k] = v
			}
			f.change(fmt.Sprint(c["folder"]), id, false)
		case http.MethodDelete:
			delete(f.contacts, id)
			f.change(fmt.Sprint(c["folder"]), id, true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f.writeJSON(w, c)
	default:
		f.fail(w, http.StatusNotFound, "ErrorItemNotFound")
	}
}

// writes are the calls that changed something, for a test to read.
func (f *fakeGraph) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}
