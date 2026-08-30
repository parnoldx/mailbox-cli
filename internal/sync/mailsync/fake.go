package mailsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-message/mail"
)

// Fake is a Driver that can be driven through states no real server can be
// asked for on demand — a UIDVALIDITY change that keeps its messages, a modseq
// that goes backwards, a count that disagrees with the uid set. The list it
// covers is in docs/DESIGN.md.
type Fake struct {
	mu      sync.Mutex
	folders map[string]*FakeFolder

	// Hook runs before each operation, named by method. A test uses it to
	// mutate server state at an exact point in a cycle — "between detect and
	// fetch" is Hook checking for op == "FetchEnvelopes".
	Hook func(op string)
	// Fail makes the named operation return an error, once.
	Fail map[string]error
	// Calls records operations in order, so a test can assert that a body was
	// never refetched.
	Calls []string
	// NoUIDPlus makes Move answer without COPYUID, like a server that cannot
	// say where the message landed.
	NoUIDPlus bool

	events chan Event
}

// FakeFolder is one server-side folder.
type FakeFolder struct {
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
	Msgs          []*FakeMsg
	// Count, when non-nil, is reported instead of len(Msgs): a server whose
	// STATUS disagrees with what UID SEARCH returns.
	Count *uint32
}

// FakeMsg is one server-side message.
// FakePart is one attachment on a fake message: its metadata, and the bytes a
// fetch of that path returns.
type FakePart struct {
	PartInfo
	Bytes []byte
}

type FakeMsg struct {
	UID        uint32
	MessageID  string
	Subject    string
	Date       time.Time
	From       string
	To         string
	Cc         string
	InReplyTo  []string
	References []string
	Flags      []string
	Plain      string
	Parts      []FakePart
	ModSeq     uint64
}

// Attach adds a file to a fake message, the way a sender would.
func (m *FakeMsg) Attach(path, mimeType, filename string, body []byte) *FakeMsg {
	m.Parts = append(m.Parts, FakePart{
		PartInfo: PartInfo{
			Path: path, MIMEType: mimeType, Filename: filename,
			Disposition: "attachment", Size: int64(len(body)),
		},
		Bytes: body,
	})
	return m
}

// Reply marks a message as answering another, the way a mail client would.
func (m *FakeMsg) Reply(to *FakeMsg) *FakeMsg {
	m.InReplyTo = []string{to.MessageID}
	m.References = append(append([]string(nil), to.References...), to.MessageID)
	return m
}

// NewFake returns an empty server with one folder.
func NewFake(folder string) *Fake {
	return &Fake{
		folders: map[string]*FakeFolder{
			folder: {UIDValidity: 1000, UIDNext: 1, HighestModSeq: 1},
		},
		Fail:   map[string]error{},
		events: make(chan Event, 64),
	}
}

// AddFolder adds another folder to the server.
func (f *Fake) AddFolder(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.folders[name] = &FakeFolder{UIDValidity: 1000, UIDNext: 1, HighestModSeq: 1}
}

// Folders implements Driver.
func (f *Fake) Folders(ctx context.Context) ([]string, error) {
	if err := f.op("Folders"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.folders))
	for name := range f.folders {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// Folder exposes a folder for a test to inspect or mutate directly.
func (f *Fake) Folder(name string) *FakeFolder {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.folders[name]
}

func (f *Fake) op(name string) error {
	if f.Hook != nil {
		f.Hook(name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, name)
	if err, ok := f.Fail[name]; ok {
		delete(f.Fail, name)
		return err
	}
	return nil
}

// Deliver delivers a message into a folder, as the server would on receiving
// it. This is the fixture verb; Append is the Driver method that files a
// message we sent.
func (f *Fake) Deliver(folder, messageID, subject, plain string) *FakeMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	fo.HighestModSeq++
	m := &FakeMsg{
		UID: fo.UIDNext, MessageID: messageID, Subject: subject,
		Plain: plain, ModSeq: fo.HighestModSeq,
	}
	fo.UIDNext++
	fo.Msgs = append(fo.Msgs, m)
	return m
}

// SetFlags changes a message's flags, as another client would.
func (f *Fake) SetFlags(folder string, uid uint32, flags ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	fo.HighestModSeq++
	for _, m := range fo.Msgs {
		if m.UID == uid {
			m.Flags = flags
			m.ModSeq = fo.HighestModSeq
		}
	}
}

// Expunge removes a message. It deliberately does not bump HighestModSeq,
// because without QRESYNC that is exactly what a real server's expunge looks
// like to us: invisible except in the count.
func (f *Fake) Expunge(folder string, uid uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	var keep []*FakeMsg
	for _, m := range fo.Msgs {
		if m.UID != uid {
			keep = append(keep, m)
		}
	}
	fo.Msgs = keep
}

// Renumber changes a folder's UIDVALIDITY while keeping its messages, giving
// them fresh uids from 1. This is the migration case: the thing the re-map
// protection in ADR-0006 exists for, and the thing DELETE+CREATE on a real
// server cannot produce.
func (f *Fake) Renumber(folder string, newValidity uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	fo.UIDValidity = newValidity
	fo.UIDNext = 1
	for _, m := range fo.Msgs {
		m.UID = fo.UIDNext
		fo.UIDNext++
	}
}

// Emit sends a watch event to whoever is watching.
func (f *Fake) Emit(e Event) { f.events <- e }

func (f *Fake) status(folder string) (FolderStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fo, ok := f.folders[folder]
	if !ok {
		return FolderStatus{}, fmt.Errorf("no such folder %q", folder)
	}
	n := uint32(len(fo.Msgs))
	if fo.Count != nil {
		n = *fo.Count
	}
	return FolderStatus{
		Name: folder, UIDValidity: fo.UIDValidity, UIDNext: fo.UIDNext,
		HighestModSeq: fo.HighestModSeq, NumMessages: n,
	}, nil
}

// Status implements Driver.
func (f *Fake) Status(ctx context.Context, folders []string) ([]FolderStatus, error) {
	if err := f.op("Status"); err != nil {
		return nil, err
	}
	var out []FolderStatus
	for _, name := range folders {
		st, err := f.status(name)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// ChangedFlags implements Driver.
func (f *Fake) ChangedFlags(ctx context.Context, folder string, since uint64) ([]FlagUpdate, error) {
	if err := f.op("ChangedFlags"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FlagUpdate
	for _, m := range f.folders[folder].Msgs {
		if m.ModSeq > since {
			out = append(out, FlagUpdate{UID: m.UID, Flags: m.Flags})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, nil
}

// FetchEnvelopes implements Driver.
func (f *Fake) FetchEnvelopes(ctx context.Context, folder string, uids []uint32) ([]Envelope, error) {
	if err := f.op("FetchEnvelopes"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []Envelope
	for _, m := range f.folders[folder].Msgs {
		if want[m.UID] {
			out = append(out, Envelope{
				UID: m.UID, MessageID: m.MessageID, Subject: m.Subject, Date: m.Date,
				From: m.From, To: m.To, Cc: m.Cc,
				InReplyTo: m.InReplyTo, References: m.References,
				Flags: m.Flags, Size: int64(len(m.Plain)),
			})
		}
	}
	return out, nil
}

// FetchBodies implements Driver.
// FetchPart implements Driver: the one read that goes to the server.
func (f *Fake) FetchPart(ctx context.Context, folder string, uid uint32, path string) ([]byte, error) {
	if err := f.op("FetchPart"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	if fo == nil {
		return nil, fmt.Errorf("no such folder %q", folder)
	}
	for _, m := range fo.Msgs {
		if m.UID != uid {
			continue
		}
		for _, p := range m.Parts {
			if p.Path == path {
				return p.Bytes, nil
			}
		}
		return nil, fmt.Errorf("no part %s in message %d", path, uid)
	}
	return nil, fmt.Errorf("no message %d in %s", uid, folder)
}

func (f *Fake) FetchBodies(ctx context.Context, folder string, uids []uint32) ([]Body, error) {
	if err := f.op("FetchBodies"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []Body
	for _, m := range f.folders[folder].Msgs {
		if want[m.UID] {
			b := Body{UID: m.UID, Plain: m.Plain}
			for _, p := range m.Parts {
				b.Parts = append(b.Parts, p.PartInfo)
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// StoreFlags implements Driver: the server's flags become what was asked for,
// and what it reports back is what the Mirror will store.
func (f *Fake) StoreFlags(ctx context.Context, folder string, uids []uint32, add, remove []string) ([]FlagUpdate, error) {
	if err := f.op("StoreFlags"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	if fo == nil {
		return nil, fmt.Errorf("no such folder %q", folder)
	}
	fo.HighestModSeq++
	var out []FlagUpdate
	for _, uid := range uids {
		for _, m := range fo.Msgs {
			if m.UID != uid {
				continue
			}
			m.Flags = withFlags(m.Flags, add, remove)
			m.ModSeq = fo.HighestModSeq
			out = append(out, FlagUpdate{UID: m.UID, Flags: append([]string(nil), m.Flags...)})
		}
	}
	return out, nil
}

// Move implements Driver. NoUIDPlus makes it answer like a server that does not
// report COPYUID, which leaves the destination uid unknown.
func (f *Fake) Move(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	if err := f.op("Move"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	src, dst := f.folders[folder], f.folders[dest]
	if src == nil || dst == nil {
		return nil, fmt.Errorf("no such folder %q", dest)
	}
	out := map[uint32]uint32{}
	for _, uid := range uids {
		var keep []*FakeMsg
		for _, m := range src.Msgs {
			if m.UID != uid {
				keep = append(keep, m)
				continue
			}
			dst.HighestModSeq++
			m.UID, m.ModSeq = dst.UIDNext, dst.HighestModSeq
			dst.UIDNext++
			dst.Msgs = append(dst.Msgs, m)
			if !f.NoUIDPlus {
				out[uid] = m.UID
			}
		}
		src.Msgs = keep
	}
	return out, nil
}

// withFlags applies an add/remove pair the way a STORE does.
func withFlags(have, add, remove []string) []string {
	out := append([]string(nil), have...)
	for _, a := range add {
		if !hasFlag(out, a) {
			out = append(out, a)
		}
	}
	var kept []string
	for _, f := range out {
		if !hasFlag(remove, f) {
			kept = append(kept, f)
		}
	}
	return kept
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// Append implements Driver: it files a message we composed, the way a server
// does. The bytes are parsed rather than taken on trust, because the point of
// filing a sent mail is that the next cycle mirrors it like any other mail — if
// what we wrote is not a message, this is where that shows up.
func (f *Fake) Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error) {
	if err := f.op("Append"); err != nil {
		return 0, err
	}
	parsed, err := parseMessage(raw)
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fo := f.folders[folder]
	if fo == nil {
		return 0, fmt.Errorf("no such folder %q", folder)
	}
	fo.HighestModSeq++
	parsed.UID, parsed.ModSeq, parsed.Flags = fo.UIDNext, fo.HighestModSeq, flags
	fo.UIDNext++
	fo.Msgs = append(fo.Msgs, parsed)
	if f.NoUIDPlus {
		return 0, nil // no UIDPLUS: the caller does not learn where it landed
	}
	return parsed.UID, nil
}

// parseMessage reads composed RFC 5322 bytes back into a fake server message.
func parseMessage(raw []byte) (*FakeMsg, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("append: %w", err)
	}
	m := &FakeMsg{}
	id, _ := r.Header.MessageID()
	m.MessageID = strings.Trim(id, "<>")
	m.Subject, _ = r.Header.Subject()
	m.Date, _ = r.Header.Date()
	m.From = addrsOf(r.Header, "From")
	m.To = addrsOf(r.Header, "To")
	m.Cc = addrsOf(r.Header, "Cc")
	m.InReplyTo, _ = r.Header.MsgIDList("In-Reply-To")
	m.References, _ = r.Header.MsgIDList("References")
	for i := 1; ; i++ {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("append: %w", err)
		}
		body, err := io.ReadAll(p.Body)
		if err != nil {
			return nil, err
		}
		switch h := p.Header.(type) {
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			kind, _, _ := h.ContentType()
			m.Parts = append(m.Parts, FakePart{
				PartInfo: PartInfo{
					Path: fmt.Sprintf("%d", i), MIMEType: kind, Filename: name,
					Disposition: "attachment", Size: int64(len(body)),
				},
				Bytes: body,
			})
		default:
			m.Plain += string(body)
		}
	}
	return m, nil
}

func addrsOf(h mail.Header, key string) string {
	list, err := h.AddressList(key)
	if err != nil {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.String())
	}
	return strings.Join(out, ", ")
}

// AllUIDs implements Driver.
func (f *Fake) AllUIDs(ctx context.Context, folder string) ([]uint32, error) {
	if err := f.op("AllUIDs"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uint32
	for _, m := range f.folders[folder].Msgs {
		out = append(out, m.UID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Watch implements Driver.
func (f *Fake) Watch(ctx context.Context, folder string, events chan<- Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-f.events:
			select {
			case events <- e:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// Close implements Driver.
func (f *Fake) Close() error { return nil }

// CallCount returns how many times an operation ran.
func (f *Fake) CallCount(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c == op {
			n++
		}
	}
	return n
}
