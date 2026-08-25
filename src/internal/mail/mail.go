package mail

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"mailbox/src/internal/config"
	"mailbox/src/internal/folders"
	"mailbox/src/internal/htmlmd"
	"mailbox/src/internal/ids"
	"mailbox/src/internal/imapclient"
)

const (
	MaxThreadMessages = 100
	MaxThreadBytes    = 8 * 1024 * 1024
	PreviewBytes      = 8192
	PreviewChars      = 120
)

var copyUIDRe = regexp.MustCompile(`(?i)COPYUID\s+\d+\s+\S+\s+(\d+)`)
var appendUIDRe = regexp.MustCompile(`(?i)APPENDUID\s+\d+\s+(\d+)`)
var uidMetaRe = regexp.MustCompile(`\bUID (\d+)`)
var flagsRe = regexp.MustCompile(`FLAGS \(([^)]*)\)`)
var htmlLeadRe = regexp.MustCompile(`(?is)\A\s*(?:<!doctype\s+html|<html|<head|<body|<div|<table|<p[\s>])`)

type Attachment struct {
	Index       int
	Name        string
	ContentType string
	Size        int
	part        *Part
}

func (a Attachment) Payload() []byte {
	if a.part == nil {
		return nil
	}
	return a.part.Decoded
}

type Envelope struct {
	Folder  string
	UID     string
	Date    string
	From    string
	Subject string
	Flags   []string
	Preview string
	To      string
}

func (e *Envelope) ID() string { return ids.FormatMessageID(e.Folder, e.UID) }

func (e *Envelope) FromShort() string { return ShortFrom(e.From) }

func (e *Envelope) Summary() string {
	if e.Preview != "" {
		return e.Preview
	}
	return e.Subject
}

type SearchQuery struct {
	Text    string
	From    string
	To      string
	Subject string
}

func NewSearchQuery(text string) SearchQuery { return SearchQuery{Text: text} }

func (q SearchQuery) Empty() bool {
	return q.Text == "" && q.From == "" && q.To == "" && q.Subject == ""
}

type Outgoing struct {
	To          []string
	Subject     string
	Body        string
	HTML        string
	Cc          []string
	Bcc         []string
	Attachments []OutAttachment
	ReplyTo     *[2]string
	InReplyTo   string
	References  string
}

type Listing struct {
	Items     []*Envelope
	Truncated bool
}

type ThreadMessage struct {
	Folder     string
	UID        string
	From       string
	To         string
	Date       string
	Subject    string
	MessageID  string
	InReplyTo  string
	References string
	ReplyTo    string
	Body       string
	BodyHTML   string
	BodyState  string
	Attachments []Attachment
	Cc         string
	Bcc        string
}

func (m *ThreadMessage) ID() string { return ids.FormatMessageID(m.Folder, m.UID) }

type ThreadWalk struct {
	Messages  []*ThreadMessage
	Truncated bool
	Notice    string
}

// SMTPSender is injectable for tests.
type SMTPSender func(acct *config.Account, raw []byte, rcpts []string, wire []byte) error

type Mail struct {
	Acct     *config.Account
	c        *imapclient.Client
	SMTPSend SMTPSender
	// FullHook overrides full-message fetch (tests).
	FullHook func(folder, uid string) (*ThreadMessage, error)
}

func New(account *config.Account) *Mail {
	return &Mail{Acct: account, SMTPSend: SendMessage}
}

func (m *Mail) Connect() error {
	c, err := imapclient.Dial(m.Acct.IMAPHost, m.Acct.IMAPPort, m.Acct.Email, m.Acct.Password)
	if err != nil {
		return err
	}
	m.c = c
	return nil
}

func (m *Mail) Close() {
	if m.c != nil {
		m.c.Logout()
		m.c = nil
	}
}

func (m *Mail) client() (*imapclient.Client, error) {
	if m.c == nil {
		if err := m.Connect(); err != nil {
			return nil, err
		}
	}
	return m.c, nil
}

func (m *Mail) Select(folder string, readonly bool) error {
	c, err := m.client()
	if err != nil {
		return err
	}
	cmd := "SELECT"
	if readonly {
		cmd = "EXAMINE"
	}
	resp, err := c.Command(cmd, imapclient.QuoteString(folder))
	if err != nil {
		return err
	}
	if resp.Status != "OK" {
		return fmt.Errorf("cannot select %s", folder)
	}
	return nil
}

func (m *Mail) ListFolders() ([]string, error) {
	c, err := m.client()
	if err != nil {
		return nil, err
	}
	resp, err := c.Command("LIST", `""`, `"*"`)
	if err != nil {
		return nil, err
	}
	if resp.Status != "OK" {
		return nil, fmt.Errorf("imap list failed")
	}
	var names []string
	for _, line := range resp.Lines {
		encoded := ListFolderName(line)
		names = append(names, utf7Decode(encoded))
	}
	return names, nil
}

func (m *Mail) SearchFolders() ([]string, error) {
	allNames, err := m.ListFolders()
	if err != nil {
		return nil, err
	}
	var routing, archive []string
	for _, name := range allNames {
		if name == folders.BLOCK || strings.HasPrefix(name, folders.BLOCK+"/") {
			continue
		}
		if folders.IsArchive(name) {
			archive = append(archive, name)
			continue
		}
		for _, root := range folders.SearchRoots {
			if name == root || strings.HasPrefix(name, root+"/") {
				routing = append(routing, name)
				break
			}
		}
	}
	seenRouting := map[string]bool{}
	for _, r := range routing {
		seenRouting[r] = true
	}
	for _, root := range folders.RoutingFolders {
		if !seenRouting[root] {
			routing = append(routing, root)
			seenRouting[root] = true
		}
	}
	out := append(routing, archive...)
	if len(out) == 0 {
		return append([]string{}, folders.SearchRoots...), nil
	}
	return out, nil
}

func (m *Mail) ListMessages(folder string, unread bool, limit *int) (*Listing, error) {
	resolved, err := folders.ResolveFolder(folder, nil)
	if err != nil {
		return nil, err
	}
	if err := m.Select(resolved, true); err != nil {
		return nil, err
	}
	criterion := "ALL"
	if unread {
		criterion = "UNSEEN"
	}
	uids, err := m.uidSearch(criterion)
	if err != nil {
		return nil, err
	}
	truncated := false
	if limit != nil {
		truncated = len(uids) > *limit
		if *limit <= 0 {
			t := len(uids) > 0
			return &Listing{Truncated: t}, nil
		}
		if len(uids) > *limit {
			uids = uids[len(uids)-*limit:]
		}
	}
	reverse(uids)
	envs, err := m.envelopes(resolved, uids)
	if err != nil {
		return nil, err
	}
	return &Listing{Items: envs, Truncated: truncated}, nil
}

func (m *Mail) CountMessages(folder string, unread bool) (int, error) {
	resolved, err := folders.ResolveFolder(folder, nil)
	if err != nil {
		return 0, err
	}
	if err := m.Select(resolved, true); err != nil {
		return 0, err
	}
	criterion := "ALL"
	if unread {
		criterion = "UNSEEN"
	}
	uids, err := m.uidSearch(criterion)
	if err != nil {
		return 0, err
	}
	return len(uids), nil
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func (m *Mail) uidSearch(args ...any) ([]string, error) {
	c, err := m.client()
	if err != nil {
		return nil, err
	}
	parts := append([]any{"UID", "SEARCH"}, args...)
	resp, err := c.Command(parts...)
	if err != nil {
		return nil, err
	}
	if resp.Status != "OK" || len(resp.Lines) == 0 {
		return nil, fmt.Errorf("search failed")
	}
	fields := strings.Fields(strings.TrimPrefix(resp.Lines[0], "SEARCH"))
	return fields, nil
}

func (m *Mail) Search(query SearchQuery, limit int, folder string) (*Listing, error) {
	scope, err := m.searchScope(folder)
	if err != nil {
		return nil, err
	}
	required := folder != ""
	truncated := false
	var found []*Envelope
	if len(scope) <= 2 {
		for _, name := range scope {
			listing, err := m.searchFolder(name, query, limit, required)
			if err != nil {
				return nil, err
			}
			found = append(found, listing.Items...)
			truncated = truncated || listing.Truncated
		}
	} else {
		type result struct {
			items     []*Envelope
			truncated bool
			err       error
		}
		results := make([]result, len(scope))
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for i, name := range scope {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				chunkMail := New(m.Acct)
				items, trunc, err := chunkMail.searchFolderQuiet(name, query, limit, required)
				chunkMail.Close()
				results[i] = result{items, trunc, err}
			}(i, name)
		}
		wg.Wait()
		for _, r := range results {
			if r.err != nil {
				return nil, r.err
			}
			found = append(found, r.items...)
			truncated = truncated || r.truncated
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Date > found[j].Date })
	if limit <= 0 {
		return &Listing{Truncated: truncated || len(found) > 0}, nil
	}
	truncated = truncated || len(found) > limit
	if len(found) > limit {
		found = found[:limit]
	}
	return &Listing{Items: found, Truncated: truncated}, nil
}

func (m *Mail) searchScope(folder string) ([]string, error) {
	names, err := m.SearchFolders()
	if err != nil {
		return nil, err
	}
	return ScopedSearchFolders(names, folder)
}

func (m *Mail) searchFolder(folder string, query SearchQuery, limit int, required bool) (*Listing, error) {
	err := m.Select(folder, true)
	if err != nil {
		if required {
			return nil, err
		}
		fmt.Fprintf(stderr(), "cannot select %s\n", folder)
		return &Listing{}, nil
	}
	uids, err := m.searchUIDs(query)
	if err != nil {
		return nil, err
	}
	reverse(uids)
	if limit <= 0 {
		return &Listing{Truncated: len(uids) > 0}, nil
	}
	truncated := len(uids) > limit
	if len(uids) > limit {
		uids = uids[:limit]
	}
	envs, err := m.envelopes(folder, uids)
	if err != nil {
		return nil, err
	}
	return &Listing{Items: envs, Truncated: truncated}, nil
}

func (m *Mail) searchFolderQuiet(folder string, query SearchQuery, limit int, required bool) ([]*Envelope, bool, error) {
	listing, err := m.searchFolder(folder, query, limit, required)
	if err != nil {
		return nil, false, err
	}
	return listing.Items, listing.Truncated, nil
}

func imapQuoteAtom(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

func (m *Mail) searchUIDs(query SearchQuery) ([]string, error) {
	var extra []string
	if query.From != "" {
		extra = append(extra, "FROM", imapQuoteAtom(query.From))
	}
	if query.To != "" {
		extra = append(extra, "TO", imapQuoteAtom(query.To))
	}
	if query.Subject != "" {
		extra = append(extra, "SUBJECT", imapQuoteAtom(query.Subject))
	}
	if query.Text == "" {
		if len(extra) == 0 {
			return nil, nil
		}
		args := append([]any{"CHARSET", "UTF-8"}, toAny(extra)...)
		uids, err := m.uidSearch(args...)
		if err != nil {
			uids, err = m.uidSearch(toAny(extra)...)
			if err != nil {
				return nil, nil
			}
		}
		return uids, nil
	}
	quoted := imapQuoteAtom(query.Text)
	if hasNonASCII(query.Text) && len(extra) == 0 {
		uids, err := m.uidSearch("CHARSET", "UTF-8", "TEXT", imapclient.Literal(query.Text))
		if err != nil {
			return nil, nil
		}
		return uids, nil
	}
	args := append(toAny(extra), "TEXT", quoted)
	uids, err := m.uidSearch(append([]any{"CHARSET", "UTF-8"}, args...)...)
	if err != nil && len(extra) == 0 {
		uids, err = m.uidSearch("CHARSET", "UTF-8", "OR", "SUBJECT", quoted, "FROM", quoted)
	}
	if err != nil {
		uids, err = m.uidSearch(args...)
		if err != nil {
			return nil, nil
		}
	}
	return uids, nil
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// fetchRecord pairs FETCH metadata text with its literal body bytes.
type fetchRecord struct {
	meta string
	body []byte
}

func splitFetchChunks(chunks []imapclient.Chunk) []fetchRecord {
	var records []fetchRecord
	metaParts := []string{}
	flush := func(body []byte) {
		meta := strings.Join(metaParts, "")
		metaParts = nil
		records = append(records, fetchRecord{meta: meta, body: body})
	}
	for _, ch := range chunks {
		if ch.Bytes != nil {
			flush(ch.Bytes)
		} else {
			metaParts = append(metaParts, ch.Text)
		}
	}
	return records
}

func (m *Mail) envelopes(folder string, uids []string) ([]*Envelope, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	c, err := m.client()
	if err != nil {
		return nil, err
	}
	resp, err := c.Command("UID", "FETCH",
		strings.Join(uids, ","),
		fmt.Sprintf("(FLAGS UID BODY.PEEK[]<0.%d>)", PreviewBytes))
	if err != nil {
		return nil, err
	}
	if resp.Status != "OK" {
		return nil, fmt.Errorf("fetch failed")
	}
	byUID := map[string]*Envelope{}
	for _, rec := range splitFetchChunks(resp.Chunks) {
		m := uidMetaRe.FindStringSubmatch(rec.meta)
		if m == nil {
			continue
		}
		parsed := ParseMessage(rec.body)
		byUID[m[1]] = &Envelope{
			Folder:  folder,
			UID:     m[1],
			Date:    FmtDate(parsed.HeaderGet("Date")),
			From:    DecodeHeader(parsed.HeaderGet("From")),
			Subject: DecodeHeader(parsed.HeaderGet("Subject")),
			Flags:   parseFlags(rec.meta),
			Preview: previewFromParsed(parsed),
			To:      DecodeHeader(parsed.HeaderGet("To")),
		}
	}
	var out []*Envelope
	for _, uid := range uids {
		if e, ok := byUID[uid]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func parseFlags(meta string) []string {
	m := flagsRe.FindStringSubmatch(meta)
	if m == nil {
		return nil
	}
	var names []string
	for _, tok := range strings.Fields(m[1]) {
		names = append(names, strings.TrimPrefix(tok, "\\"))
	}
	for i, n := range names {
		names[i] = strings.ToLower(n)
	}
	return names
}

func (m *Mail) Thread(folder, uid string) (*ThreadWalk, error) {
	return m.ThreadLimits(folder, uid, MaxThreadMessages, MaxThreadBytes)
}

func (m *Mail) ThreadLimits(folder, uid string, maxMessages, maxBytes int) (*ThreadWalk, error) {
	if err := m.Select(folder, true); err != nil {
		return nil, err
	}
	root, err := m.full(folder, uid)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*ThreadMessage{root.ID(): root}
	queue := []*ThreadMessage{root}
	searched := map[string]bool{}
	retained := len(root.Body)
	truncated := false
	var notices []string
	noticeSeen := map[string]bool{}
	addNotice := func(n string) {
		if !noticeSeen[n] {
			notices = append(notices, n)
			noticeSeen[n] = true
		}
	}
	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		stop := false
		for _, msgid := range relatedMessageIDs(current) {
			if searched[msgid] {
				continue
			}
			searched[msgid] = true
			quoted := imapQuoteAtom(msgid)
			extraSet := map[string]bool{}
			c, _ := m.client()
			for _, header := range []string{"Message-ID", "In-Reply-To", "References"} {
				resp, err := c.Command("UID", "SEARCH", "HEADER", header, quoted)
				if err != nil || resp.Status != "OK" || len(resp.Lines) == 0 {
					continue
				}
				for _, f := range strings.Fields(resp.Lines[0]) {
					if f != "SEARCH" {
						extraSet[f] = true
					}
				}
			}
			for otherUID := range extraSet {
				key := folder + ":" + otherUID
				if _, ok := byKey[key]; ok {
					continue
				}
				if len(byKey) >= maxMessages {
					truncated = true
					addNotice(fmt.Sprintf("thread exceeds %d messages", maxMessages))
					queue = nil
					stop = true
					break
				}
				other, err := m.full(folder, otherUID)
				if err != nil {
					other = stubMessage(folder, otherUID, "failed")
					truncated = true
					addNotice(fmt.Sprintf("cannot fetch %s:%s", folder, otherUID))
					byKey[key] = other
					continue
				}
				size := len(other.Body)
				if retained+size > maxBytes {
					other.Body = ""
					other.BodyHTML = ""
					other.BodyState = "over_limit"
					truncated = true
					addNotice(fmt.Sprintf("thread exceeds %d bytes", maxBytes))
				} else {
					retained += size
				}
				byKey[key] = other
				queue = append(queue, other)
			}
			if stop {
				break
			}
		}
	}
	messages := make([]*ThreadMessage, 0, len(byKey))
	for _, msg := range byKey {
		messages = append(messages, msg)
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].Date < messages[j].Date })
	return &ThreadWalk{Messages: messages, Truncated: truncated, Notice: strings.Join(notices, "; ")}, nil
}

func stubMessage(folder, uid, state string) *ThreadMessage {
	return &ThreadMessage{Folder: folder, UID: uid, BodyState: state}
}

func relatedMessageIDs(msg *ThreadMessage) []string {
	var ids []string
	seen := map[string]bool{}
	for _, raw := range []string{msg.InReplyTo, msg.References, msg.MessageID} {
		for _, tok := range strings.Fields(raw) {
			if tok != "" && !seen[tok] {
				ids = append(ids, tok)
				seen[tok] = true
			}
		}
	}
	return ids
}

func (m *Mail) Screen(folder, uid, dest string) (string, error) {
	if err := m.Select(folder, false); err != nil {
		return "", err
	}
	msgid := m.peekMessageID(uid)
	destQuoted := imapclient.QuoteString(dest)
	c, _ := m.client()
	resp, err := c.Command("UID", "MOVE", uid, destQuoted)
	if err != nil || resp.Status != "OK" {
		resp, err = c.Command("UID", "COPY", uid, destQuoted)
		if err != nil || resp.Status != "OK" {
			return "", fmt.Errorf("cannot move %s:%s to %s", folder, uid, dest)
		}
		st, err := c.Command("UID", "STORE", uid, "+FLAGS", `(\Deleted)`)
		if err != nil || st.Status != "OK" {
			return "", fmt.Errorf("cannot flag %s:%s deleted", folder, uid)
		}
		st, err = c.Command("UID", "EXPUNGE", uid)
		if err != nil || st.Status != "OK" {
			return "", fmt.Errorf("cannot expunge %s:%s", folder, uid)
		}
	}
	newUID := uidplusFromResponse(resp)
	if newUID != "" {
		return ids.FormatMessageID(dest, newUID), nil
	}
	if msgid != "" {
		if found := m.uidByMessageID(dest, msgid); found != "" {
			return ids.FormatMessageID(dest, found), nil
		}
	}
	return "", fmt.Errorf("moved %s:%s to %s but destination uid is unknown", folder, uid, dest)
}

func uidplusFromResponse(resp *imapclient.Response) string {
	text := resp.Text
	for _, ch := range resp.Chunks {
		text += " " + ch.String()
	}
	for _, line := range resp.Lines {
		text += " " + line
	}
	if m := copyUIDRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if m := appendUIDRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func (m *Mail) peekMessageID(uid string) string {
	c, err := m.client()
	if err != nil {
		return ""
	}
	resp, err := c.Command("UID", "FETCH", uid, "(BODY.PEEK[HEADER.FIELDS (MESSAGE-ID)])")
	if err != nil || resp.Status != "OK" {
		return ""
	}
	recs := splitFetchChunks(resp.Chunks)
	if len(recs) == 0 {
		return ""
	}
	parsed := ParseMessage(recs[0].body)
	return DecodeHeader(parsed.HeaderGet("Message-ID"))
}

func (m *Mail) uidByMessageID(folder, msgid string) string {
	if msgid == "" {
		return ""
	}
	if err := m.Select(folder, true); err != nil {
		return ""
	}
	uids, err := m.uidSearch("HEADER", "Message-ID", imapQuoteAtom(msgid))
	if err != nil || len(uids) == 0 {
		return ""
	}
	return uids[len(uids)-1]
}

func (m *Mail) full(folder, uid string) (*ThreadMessage, error) {
	if m.FullHook != nil {
		return m.FullHook(folder, uid)
	}
	if err := m.Select(folder, true); err != nil {
		return nil, err
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", uid, "(BODY.PEEK[])")
	if err != nil || resp.Status != "OK" {
		return nil, fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	recs := splitFetchChunks(resp.Chunks)
	if len(recs) == 0 || recs[0].body == nil {
		return nil, fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	return threadFromParsed(folder, uid, ParseMessage(recs[0].body)), nil
}

func (m *Mail) Message(folder, uid string) (*ThreadMessage, error) {
	return m.full(folder, uid)
}

func (m *Mail) SetSeen(folder, uid string, seen bool) (string, error) {
	if err := m.Select(folder, false); err != nil {
		return "", err
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", uid, "(FLAGS)")
	if err != nil || resp.Status != "OK" || len(resp.Chunks) == 0 {
		return "", fmt.Errorf("cannot fetch %s:%s", folder, uid)
	}
	op := "+FLAGS"
	if !seen {
		op = "-FLAGS"
	}
	st, err := c.Command("UID", "STORE", uid, op, `(\Seen)`)
	if err != nil || st.Status != "OK" {
		return "", fmt.Errorf("cannot flag %s:%s", folder, uid)
	}
	return ids.FormatMessageID(folder, uid), nil
}

func (m *Mail) Attachment(folder, uid string, index int) (*Attachment, []byte, error) {
	msg, err := m.full(folder, uid)
	if err != nil {
		return nil, nil, err
	}
	if len(msg.Attachments) == 0 {
		return nil, nil, fmt.Errorf("%s has no attachments", msg.ID())
	}
	if index < 1 || index > len(msg.Attachments) {
		return nil, nil, fmt.Errorf("attachment index %d out of range 1..%d", index, len(msg.Attachments))
	}
	att := msg.Attachments[index-1]
	return &att, att.Payload(), nil
}

func (m *Mail) AppendBytes(folder, flags string, raw []byte, msgid string) (string, error) {
	c, _ := m.client()
	// ponytail: Dovecot hangs on a bare flag atom here; RFC 3501 APPEND wants a flag-list
	if !strings.HasPrefix(flags, "(") {
		flags = "(" + flags + ")"
	}
	resp, err := c.Command("APPEND", imapclient.QuoteString(folder), flags, imapclient.Literal(raw))
	if err != nil || resp.Status != "OK" {
		return "", fmt.Errorf("cannot append to %s", folder)
	}
	newUID := uidplusFromResponse(resp)
	if newUID != "" {
		return ids.FormatMessageID(folder, newUID), nil
	}
	found := m.uidByMessageID(folder, msgid)
	if found == "" {
		return "", fmt.Errorf("saved to %s but destination uid is unknown", folder)
	}
	return ids.FormatMessageID(folder, found), nil
}

func (m *Mail) purge(folder, uid string) error {
	if err := m.Select(folder, false); err != nil {
		return err
	}
	c, _ := m.client()
	st, err := c.Command("UID", "STORE", uid, "+FLAGS", `(\Deleted)`)
	if err != nil || st.Status != "OK" {
		return fmt.Errorf("cannot flag %s:%s deleted", folder, uid)
	}
	st, err = c.Command("UID", "EXPUNGE", uid)
	if err != nil || st.Status != "OK" {
		return fmt.Errorf("cannot expunge %s:%s", folder, uid)
	}
	return nil
}

func ScopedSearchFolders(names []string, folder string) ([]string, error) {
	if folder == "" {
		return names, nil
	}
	resolved, err := folders.ResolveFolder(folder, names)
	if err != nil {
		return nil, err
	}
	if folders.IsArchive(resolved) {
		var scoped []string
		for _, n := range names {
			if n == resolved || strings.HasPrefix(n, resolved+"/") {
				scoped = append(scoped, n)
			}
		}
		if scoped == nil {
			scoped = []string{resolved}
		}
		return scoped, nil
	}
	return []string{resolved}, nil
}

// --- sieve service ops (mailbox serve) ---

func (m *Mail) CreateFolder(name string) error {
	c, err := m.client()
	if err != nil {
		return err
	}
	resp, err := c.Command("CREATE", imapclient.QuoteString(name))
	if err != nil || resp.Status != "OK" {
		return fmt.Errorf("cannot create %s", name)
	}
	return nil
}

// UIDSenders maps uid -> From address for every message in the folder.
func (m *Mail) UIDSenders(folder string) (map[string]string, error) {
	if err := m.Select(folder, true); err != nil {
		return nil, err
	}
	uids, err := m.uidSearch("ALL")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	c, _ := m.client()
	resp, err := c.Command("UID", "FETCH", strings.Join(uids, ","), "(UID BODY.PEEK[HEADER.FIELDS (FROM)])")
	if err != nil || resp.Status != "OK" {
		return nil, fmt.Errorf("cannot fetch senders in %s", folder)
	}
	for _, rec := range splitFetchChunks(resp.Chunks) {
		mm := uidMetaRe.FindStringSubmatch(rec.meta)
		if mm == nil || rec.body == nil {
			continue
		}
		from := DecodeHeader(ParseMessage(rec.body).HeaderGet("From"))
		if addr := firstAddress(from); addr != "" {
			out[mm[1]] = addr
		}
	}
	return out, nil
}

// SeenAndDelete marks the message \Seen + \Deleted and expunges it.
func (m *Mail) SeenAndDelete(folder, uid string) error {
	if err := m.Select(folder, false); err != nil {
		return err
	}
	c, _ := m.client()
	st, err := c.Command("UID", "STORE", uid, "+FLAGS", `(\Seen \Deleted)`)
	if err != nil || st.Status != "OK" {
		return fmt.Errorf("cannot flag %s:%s deleted", folder, uid)
	}
	st, err = c.Command("UID", "EXPUNGE", uid)
	if err != nil || st.Status != "OK" {
		return fmt.Errorf("cannot expunge %s:%s", folder, uid)
	}
	return nil
}

// firstAddress extracts the addr-spec from a From header value.
func firstAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if i := strings.LastIndex(value, "<"); i >= 0 {
		if j := strings.Index(value[i:], ">"); j > 1 {
			return value[i+1 : i+j]
		}
	}
	return value
}

var _ = htmlmd.HTMLToMarkdown
