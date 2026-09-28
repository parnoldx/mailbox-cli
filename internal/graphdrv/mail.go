package graphdrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"mailbox/internal/imapdrv"
	"mailbox/internal/sync/mailsync"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
)

// Mail is a Graph mailbox behind the mailsync.Driver interface, so the
// reconciler, the writer and every command above them treat it as one more
// IMAP account (ADR-0029).
//
// A Graph delta link is a sync token, not a modseq, so Status runs each
// folder's delta and folds it into the Store, which then answers the IMAP-shaped
// questions: UIDNEXT, HIGHESTMODSEQ, which uids changed since a modseq, and
// every uid there is.
type Mail struct {
	c *Client
	s *Store
}

// NewMail serves one account's mail from c, keeping its uid map in s.
func NewMail(c *Client, s *Store) *Mail { return &Mail{c: c, s: s} }

// wellKnown are the folders Graph names the same in every language. A folder's
// display name is localised — "Posteingang" — so these are recognised by id and
// given the names the rest of the program uses for them. An empty name is a
// folder that is not mail anybody reads.
var wellKnown = []struct{ graph, name string }{
	{"inbox", "INBOX"},
	{"sentitems", "Sent"},
	{"drafts", "Drafts"},
	{"deleteditems", "Trash"},
	{"junkemail", "Junk"},
	{"archive", "Archive"},
	{"outbox", ""},
	{"conversationhistory", ""},
	{"syncissues", ""},
}

type graphFolder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ChildFolderCount int    `json:"childFolderCount"`
}

// Folders implements mailsync.Driver. Children are named under their parent
// with a slash, so a folder inside the Inbox is INBOX/Aside exactly as it is on
// the Primary's server.
func (m *Mail) Folders(ctx context.Context) ([]string, error) {
	canonical := map[string]string{}
	for _, wk := range wellKnown {
		var f graphFolder
		err := m.c.do(ctx, request{method: http.MethodGet, path: "/me/mailFolders/" + wk.graph + "?$select=id"}, &f)
		if notFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("folder %s: %w", wk.graph, err)
		}
		canonical[f.ID] = wk.name
	}
	var out []string
	var walk func(path, parent string) error
	walk = func(path, parent string) error {
		folders, _, err := all[graphFolder](ctx, m.c, path+"?$select=id,displayName,childFolderCount&$top=100")
		if err != nil {
			return err
		}
		for _, f := range folders {
			name, known := canonical[f.ID]
			switch {
			case known && name == "":
				continue
			case known:
			case parent == "":
				name = f.DisplayName
			default:
				name = parent + "/" + f.DisplayName
			}
			if err := m.s.setFolder(name, f.ID); err != nil {
				return err
			}
			out = append(out, name)
			if f.ChildFolderCount > 0 {
				if err := walk("/me/mailFolders/"+url.PathEscape(f.ID)+"/childFolders", name); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk("/me/mailFolders", ""); err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	return out, nil
}

// SentFolder is where Graph files what it sends: Sent Items, whatever it is
// called in this mailbox's language.
func (m *Mail) SentFolder(ctx context.Context) (string, error) { return "Sent", nil }

// CreateFolder makes a folder, under its parent when the name has one.
func (m *Mail) CreateFolder(ctx context.Context, name string) error {
	if _, ok, _ := m.s.folder(name); !ok {
		if _, err := m.Folders(ctx); err != nil {
			return err
		}
	}
	if _, ok, _ := m.s.folder(name); ok {
		return nil
	}
	path := "/me/mailFolders"
	leaf := name
	if i := strings.LastIndex(name, "/"); i > 0 {
		parent, ok, err := m.s.folder(name[:i])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no folder %q to make %q in", name[:i], name[i+1:])
		}
		path = "/me/mailFolders/" + url.PathEscape(parent.GraphID) + "/childFolders"
		leaf = name[i+1:]
	}
	var f graphFolder
	if err := m.c.do(ctx, request{method: http.MethodPost, path: path, body: jsonBody(map[string]string{"displayName": leaf})}, &f); err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	return m.s.setFolder(name, f.ID)
}

// graphMessage is a message as Graph describes it; a delta item carries only
// some of it, and a removal only the id.
type graphMessage struct {
	ID      string `json:"id"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
	IsRead  bool `json:"isRead"`
	IsDraft bool `json:"isDraft"`
	Flag    struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
	Categories        []string    `json:"categories"`
	InternetMessageID string      `json:"internetMessageId"`
	Subject           string      `json:"subject"`
	From              *recipient  `json:"from"`
	To                []recipient `json:"toRecipients"`
	Cc                []recipient `json:"ccRecipients"`
	SentDateTime      time.Time   `json:"sentDateTime"`
	ReceivedDateTime  time.Time   `json:"receivedDateTime"`
	Headers           []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Extended []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"singleValueExtendedProperties"`
}

type recipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (r recipient) String() string {
	if r.EmailAddress.Name != "" && r.EmailAddress.Name != r.EmailAddress.Address {
		return fmt.Sprintf("%s <%s>", r.EmailAddress.Name, r.EmailAddress.Address)
	}
	return r.EmailAddress.Address
}

func joinRecipients(rs []recipient) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, ", ")
}

func (g graphMessage) header(name string) string {
	for _, h := range g.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// flags is what a message looks like as IMAP flags. A category is a keyword
// when it could be one — `$bubbled`, `bubble-20260927T0800` — and is otherwise
// Outlook's own ("Red category") and kept out of the Mirror.
func (g graphMessage) flags() []string {
	var out []string
	if g.IsRead {
		out = append(out, `\Seen`)
	}
	if g.IsDraft {
		out = append(out, `\Draft`)
	}
	if g.Flag.FlagStatus == "flagged" {
		out = append(out, `\Flagged`)
	}
	for _, c := range g.Categories {
		if keyword(c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func keyword(c string) bool {
	return c != "" && !strings.HasPrefix(c, `\`) && !strings.ContainsAny(c, " ()%*\"\\]{")
}

// Status implements mailsync.Driver: each folder's delta is run and folded in,
// then the Store answers. An unchanged folder is one request.
func (m *Mail) Status(ctx context.Context, folders []string) ([]mailsync.FolderStatus, error) {
	out := make([]mailsync.FolderStatus, 0, len(folders))
	var firstErr error
	for _, name := range folders {
		if err := m.syncFolder(ctx, name); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", name, err)
			}
			continue
		}
		st, err := m.s.status(name)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if len(out) == 0 {
		return nil, firstErr
	}
	// One folder failing leaves the rest to be reconciled: the reconciler
	// reports a folder with no status as not found, which is what it is here.
	return out, nil
}

func (m *Mail) syncFolder(ctx context.Context, name string) error {
	f, ok, err := m.s.folder(name)
	if err != nil {
		return err
	}
	if !ok {
		if _, err := m.Folders(ctx); err != nil {
			return err
		}
		if f, ok, err = m.s.folder(name); err != nil || !ok {
			return fmt.Errorf("folder %q not found on server", name)
		}
	}
	link := f.Delta
	if link == "" {
		link = "/me/mailFolders/" + url.PathEscape(f.GraphID) + "/messages/delta?$select=isRead,isDraft,flag,categories"
	}
	items, next, err := all[graphMessage](ctx, m.c, link, "odata.maxpagesize=200")
	if expired(err) && f.Delta != "" {
		// Graph has forgotten where we were. Every uid starts over, under a new
		// UIDVALIDITY, which is the reconciler's cue to resync this folder.
		if err := m.s.resetFolder(name, f.GraphID); err != nil {
			return err
		}
		return m.syncFolder(ctx, name)
	}
	if err != nil {
		return err
	}
	seen := make([]seenMessage, 0, len(items))
	for _, it := range items {
		seen = append(seen, seenMessage{GraphID: it.ID, Removed: it.Removed != nil, Flags: it.flags(), Categories: it.Categories})
	}
	return m.s.applyDelta(name, seen, next)
}

// ChangedFlags implements mailsync.Driver from the Store.
func (m *Mail) ChangedFlags(ctx context.Context, folder string, since uint64) ([]mailsync.FlagUpdate, error) {
	return m.s.changed(folder, since)
}

// AllUIDs implements mailsync.Driver from the Store.
func (m *Mail) AllUIDs(ctx context.Context, folder string) ([]uint32, error) {
	return m.s.uids(folder)
}

// envelopeSelect is every property an Envelope is made of. The size is an
// extended MAPI property: Graph has no plain one.
const envelopeSelect = "?$select=internetMessageId,subject,from,toRecipients,ccRecipients,sentDateTime," +
	"receivedDateTime,isRead,isDraft,flag,categories,internetMessageHeaders" +
	"&$expand=singleValueExtendedProperties($filter=id%20eq%20'Integer%200x0E08')"

// FetchEnvelopes implements mailsync.Driver, twenty messages to a $batch.
func (m *Mail) FetchEnvelopes(ctx context.Context, folder string, uids []uint32) ([]mailsync.Envelope, error) {
	out := make([]mailsync.Envelope, 0, len(uids))
	for start := 0; start < len(uids); start += 20 {
		chunk := uids[start:min(start+20, len(uids))]
		paths := map[string]uint32{}
		var reqs []string
		for _, uid := range chunk {
			id, _, err := m.s.message(folder, uid)
			if err != nil {
				continue
			}
			key := strconv.FormatUint(uint64(uid), 10)
			paths[key] = uid
			reqs = append(reqs, key, "/me/messages/"+url.PathEscape(id)+envelopeSelect)
		}
		got, err := m.batch(ctx, reqs)
		if err != nil {
			return nil, err
		}
		for key, g := range got {
			out = append(out, envelopeOf(paths[key], g))
		}
	}
	slices.SortFunc(out, func(a, b mailsync.Envelope) int { return int(a.UID) - int(b.UID) })
	return out, nil
}

func envelopeOf(uid uint32, g graphMessage) mailsync.Envelope {
	e := mailsync.Envelope{
		UID:          uid,
		MessageID:    strings.Trim(g.InternetMessageID, "<>"),
		Date:         g.SentDateTime,
		Subject:      g.Subject,
		To:           joinRecipients(g.To),
		Cc:           joinRecipients(g.Cc),
		InReplyTo:    imapdrv.MessageIDs(g.header("In-Reply-To")),
		References:   imapdrv.MessageIDs(g.header("References")),
		Flags:        g.flags(),
		InternalDate: g.ReceivedDateTime,

		ListUnsubscribe:     g.header("List-Unsubscribe"),
		ListUnsubscribePost: g.header("List-Unsubscribe-Post"),
	}
	if e.Date.IsZero() {
		e.Date = g.ReceivedDateTime
	}
	if g.From != nil {
		e.From = g.From.String()
	}
	for _, p := range g.Extended {
		if strings.EqualFold(p.ID, "Integer 0xe08") {
			e.Size, _ = strconv.ParseInt(p.Value, 10, 64)
		}
	}
	return e
}

// batch runs GETs through /$batch: pairs of (key, path). A message that has gone
// since the delta named it is left out; one that was throttled is asked for
// again after the wait Graph named.
func (m *Mail) batch(ctx context.Context, pairs []string) (map[string]graphMessage, error) {
	out := map[string]graphMessage{}
	type sub struct {
		ID      string            `json:"id"`
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	pending := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		pending[pairs[i]] = pairs[i+1]
	}
	for attempt := 0; len(pending) > 0; attempt++ {
		var body struct {
			Requests []sub `json:"requests"`
		}
		for key, path := range pending {
			body.Requests = append(body.Requests, sub{ID: key, Method: http.MethodGet, URL: path,
				Headers: map[string]string{"Prefer": `IdType="ImmutableId"`}})
		}
		var resp struct {
			Responses []struct {
				ID      string            `json:"id"`
				Status  int               `json:"status"`
				Headers map[string]string `json:"headers"`
				Body    graphMessage      `json:"body"`
			} `json:"responses"`
		}
		if err := m.c.do(ctx, request{method: http.MethodPost, path: "/$batch", body: jsonBody(body)}, &resp); err != nil {
			return nil, err
		}
		wait := time.Duration(0)
		for _, r := range resp.Responses {
			switch {
			case r.Status < 300:
				out[r.ID] = r.Body
				delete(pending, r.ID)
			case r.Status == http.StatusNotFound:
				delete(pending, r.ID)
			case (r.Status == http.StatusTooManyRequests || r.Status == http.StatusServiceUnavailable) && attempt < 3:
				wait = max(wait, retryAfter(r.Headers["Retry-After"]))
			default:
				return nil, &APIError{Status: r.Status, Code: "batch", Message: "fetching message " + pending[r.ID]}
			}
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return out, nil
}

// mime is a message's full MIME, which is what the body and the parts are read
// out of: Graph's own body is one rendering, not the parts the mail was sent in.
func (m *Mail) mime(ctx context.Context, folder string, uid uint32) ([]byte, error) {
	id, _, err := m.s.message(folder, uid)
	if err != nil {
		return nil, err
	}
	return m.c.raw(ctx, request{method: http.MethodGet, path: "/me/messages/" + url.PathEscape(id) + "/$value"})
}

// FetchBodies implements mailsync.Driver, four messages at a time. A message
// gone since the delta named it is left out, as a vanished uid is on IMAP.
func (m *Mail) FetchBodies(ctx context.Context, folder string, uids []uint32) ([]mailsync.Body, error) {
	// ponytail: one GET per body, four in flight; a cold start of a big folder
	// takes minutes. $batch cannot carry $value, so this is the floor short of
	// JSON bodies, which would lose the MIME parts.
	out := make([]mailsync.Body, len(uids))
	errs := make([]error, len(uids))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, uid := range uids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			raw, err := m.mime(ctx, folder, uid)
			if notFound(err) {
				return
			}
			if err != nil {
				errs[i] = err
				return
			}
			b := parseBody(raw)
			b.UID = uid
			out[i] = b
		}()
	}
	wg.Wait()
	var bodies []mailsync.Body
	for i := range out {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if out[i].UID != 0 {
			bodies = append(bodies, out[i])
		}
	}
	return bodies, nil
}

// FetchPart implements mailsync.Driver: the MIME again, walked to the path the
// Mirror recorded.
func (m *Mail) FetchPart(ctx context.Context, folder string, uid uint32, path string) ([]byte, error) {
	raw, err := m.mime(ctx, folder, uid)
	if err != nil {
		return nil, err
	}
	var out []byte
	found := false
	walkParts(raw, func(p string, ent *message.Entity) {
		if found || p != path {
			return
		}
		out, _ = io.ReadAll(ent.Body)
		found = true
	})
	if !found {
		return nil, fmt.Errorf("no part %s in message %d", path, uid)
	}
	return out, nil
}

// parseBody reads a message's text and the metadata of every other part, the
// way the IMAP driver does from BODYSTRUCTURE.
func parseBody(raw []byte) mailsync.Body {
	var b mailsync.Body
	walkParts(raw, func(path string, ent *message.Entity) {
		mt, params, _ := ent.Header.ContentType()
		disp, dparams, _ := ent.Header.ContentDisposition()
		disp = strings.ToLower(disp)
		if disp != "attachment" && (mt == "text/plain" || mt == "text/html") {
			text, _ := io.ReadAll(ent.Body)
			if mt == "text/plain" {
				b.Plain += string(text)
			} else {
				b.HTML += string(text)
			}
			return
		}
		n, _ := io.Copy(io.Discard, ent.Body)
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		b.Parts = append(b.Parts, mailsync.PartInfo{
			Path: path, MIMEType: mt, Filename: name, Disposition: disp, Size: n,
			ContentID: strings.Trim(ent.Header.Get("Content-Id"), "<> "),
		})
	})
	return b
}

// walkParts calls fn for every leaf part with its IMAP part path: one-based,
// and "1" for a message that is not multipart.
func walkParts(raw []byte, fn func(path string, ent *message.Entity)) {
	// An unknown charset or encoding still returns the entity, and its bytes
	// are better than nothing, which is what the IMAP driver keeps too.
	e, _ := message.Read(bytes.NewReader(raw))
	if e == nil {
		return
	}
	_ = e.Walk(func(path []int, ent *message.Entity, err error) error {
		if ent == nil || ent.MultipartReader() != nil {
			return nil
		}
		fn(imapPath(path), ent)
		return nil
	})
}

func imapPath(path []int) string {
	if len(path) == 0 {
		return "1"
	}
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = strconv.Itoa(n + 1)
	}
	return strings.Join(parts, ".")
}

// StoreFlags implements mailsync.Driver. \Seen is isRead, \Flagged is the
// follow-up flag, and any other keyword is an Outlook category — which is how
// a bubble's timer lives on the server where both Daemons can see it
// (ADR-0031). The categories Outlook put there are kept.
func (m *Mail) StoreFlags(ctx context.Context, folder string, uids []uint32, add, remove []string) ([]mailsync.FlagUpdate, error) {
	var out []mailsync.FlagUpdate
	for _, uid := range uids {
		id, cats, err := m.s.message(folder, uid)
		if err != nil {
			continue
		}
		patch := map[string]any{}
		newCats := slices.Clone(cats)
		for _, f := range add {
			switch f {
			case `\Seen`:
				patch["isRead"] = true
			case `\Flagged`:
				patch["flag"] = map[string]string{"flagStatus": "flagged"}
			default:
				if keyword(f) && !slices.Contains(newCats, f) {
					newCats = append(newCats, f)
				}
			}
		}
		for _, f := range remove {
			switch f {
			case `\Seen`:
				patch["isRead"] = false
			case `\Flagged`:
				patch["flag"] = map[string]string{"flagStatus": "notFlagged"}
			default:
				newCats = slices.DeleteFunc(newCats, func(c string) bool { return c == f })
			}
		}
		if !slices.Equal(cats, newCats) {
			patch["categories"] = newCats
		}
		var g graphMessage
		if len(patch) == 0 {
			err = m.c.do(ctx, request{method: http.MethodGet, path: "/me/messages/" + url.PathEscape(id) + "?$select=isRead,isDraft,flag,categories"}, &g)
		} else {
			err = m.c.do(ctx, request{method: http.MethodPatch, path: "/me/messages/" + url.PathEscape(id), body: jsonBody(patch)}, &g)
		}
		if err != nil {
			return out, err
		}
		if err := m.s.setFlags(folder, uid, g.flags(), g.Categories); err != nil {
			return out, err
		}
		out = append(out, mailsync.FlagUpdate{UID: uid, Flags: g.flags()})
	}
	return out, nil
}

// Move implements mailsync.Driver. An immutable id survives the move, so the
// message keeps its identity and gets its uid in the destination here, the
// UIDPLUS case: the caller knows where it went without waiting for a cycle.
// Tried once, like every move (ADR-0017).
func (m *Mail) Move(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	to, ok, err := m.s.folder(dest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no folder %q", dest)
	}
	out := map[uint32]uint32{}
	for _, uid := range uids {
		id, cats, err := m.s.message(folder, uid)
		if err != nil {
			continue
		}
		var g graphMessage
		if err := m.c.do(ctx, request{method: http.MethodPost, path: "/me/messages/" + url.PathEscape(id) + "/move",
			body: jsonBody(map[string]string{"destinationId": to.GraphID})}, &g); err != nil {
			return out, err
		}
		if g.ID == "" {
			g.ID = id
		}
		if err := m.s.drop(folder, uid); err != nil {
			return out, err
		}
		newUID, err := m.s.place(dest, g.ID, g.flags(), cats)
		if err != nil {
			return out, err
		}
		out[uid] = newUID
	}
	return out, nil
}

// Append implements mailsync.Driver: Graph takes a message as base64 MIME and
// makes it a draft, which is then moved where it was meant to go. Nothing here
// files a sent copy — Graph does that itself — so this is how a draft is saved.
func (m *Mail) Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error) {
	var g graphMessage
	if err := m.c.do(ctx, request{method: http.MethodPost, path: "/me/messages",
		body: []byte(base64.StdEncoding.EncodeToString(raw)), contentType: "text/plain"}, &g); err != nil {
		return 0, err
	}
	f, ok, err := m.s.folder(folder)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("no folder %q", folder)
	}
	drafts, _, _ := m.s.folder("Drafts")
	if f.GraphID != drafts.GraphID {
		if err := m.c.do(ctx, request{method: http.MethodPost, path: "/me/messages/" + url.PathEscape(g.ID) + "/move",
			body: jsonBody(map[string]string{"destinationId": f.GraphID})}, &g); err != nil {
			return 0, err
		}
	}
	return m.s.place(folder, g.ID, g.flags(), g.Categories)
}

// Watch implements mailsync.Driver. Graph pushes only to a public HTTPS
// webhook, which a laptop does not have, so nothing is watched: the Daemon's
// minute poll is how new mail on this account is found (ADR-0029).
func (m *Mail) Watch(ctx context.Context, folder string, events chan<- mailsync.Event) error {
	<-ctx.Done()
	return nil
}

// Close implements mailsync.Driver. The HTTP client holds nothing worth closing.
func (m *Mail) Close() error { return nil }

var _ mailsync.Driver = (*Mail)(nil)
