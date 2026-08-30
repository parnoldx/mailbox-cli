// Package imapdrv implements mailsync.Driver over emersion/go-imap v2.
//
// go-imap v2 has CONDSTORE but no QRESYNC and no NOTIFY, and cannot be extended
// to add them — its command types are unexported. The reconciler is built
// around that (ADR-0006); this package's job is only to speak the protocol.
package imapdrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-message/charset"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"mailbox/internal/sync/mailsync"
)

// Config is what it takes to reach one account.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Driver holds the connections to one account.
//
// There are two, and the split is not an optimisation. RFC 3501 says STATUS
// "SHOULD NOT be used on the currently selected mailbox", because a server may
// answer from the connection's own view rather than the mailbox's current
// state — and Dovecot does exactly that: a change made elsewhere stays
// invisible to LIST-STATUS on a connection that has the folder selected. So
// detection gets a connection that never selects anything, and fetching gets
// its own.
type Driver struct {
	cfg Config

	ctlMu sync.Mutex
	ctl   *imapclient.Client // never selects a mailbox

	workMu sync.Mutex
	work   *imapclient.Client

	condStore bool
}

// Dial connects and authenticates both connections.
func Dial(cfg Config) (*Driver, error) {
	d := &Driver{cfg: cfg}
	ctl, err := d.connect(nil)
	if err != nil {
		return nil, err
	}
	work, err := d.connect(nil)
	if err != nil {
		ctl.Close()
		return nil, err
	}
	d.ctl, d.work = ctl, work
	// CONDSTORE is not turned on with ENABLE: RFC 7162 says a server enables it
	// implicitly the first time the client uses a CONDSTORE parameter, and
	// go-imap refuses `ENABLE CONDSTORE` outright ("not supported"). Advertising
	// the capability is the whole precondition — mailbox.org returns
	// HIGHESTMODSEQ from STATUS, LIST-STATUS and SELECT with no ENABLE at all.
	d.condStore = ctl.Caps().Has(imap.CapCondStore)
	return d, nil
}

func (d *Driver) connect(opts *imapclient.Options) (*imapclient.Client, error) {
	c, err := imapclient.DialTLS(fmt.Sprintf("%s:%d", d.cfg.Host, d.cfg.Port), opts)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", d.cfg.Host, err)
	}
	if err := c.Login(d.cfg.Username, d.cfg.Password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("login: %w", err)
	}
	return c, nil
}

// onWork runs fn with folder selected, redialling once if the connection has
// died. A server drops a connection whose selected folder is deleted or renamed
// by somebody else, so this is normal operation rather than a fault, and a
// daemon that cannot survive it is not one.
func (d *Driver) onWork(folder string, fn func(*imapclient.Client) error) error {
	d.workMu.Lock()
	defer d.workMu.Unlock()

	err := d.tryWork(folder, fn)
	if err == nil {
		return nil
	}
	// Reconnect and retry once on any failure rather than trying to classify
	// errors. A server drops a connection whose selected folder was deleted,
	// and Dovecot poisons one permanently once that folder is gone; both read
	// as ordinary command errors. Every operation here is a read, so retrying
	// is free of consequence.
	c, rerr := d.connect(nil)
	if rerr != nil {
		return fmt.Errorf("reconnect after %v: %w", err, rerr)
	}
	_ = d.work.Close()
	d.work = c
	return d.tryWork(folder, fn)
}

func (d *Driver) tryWork(folder string, fn func(*imapclient.Client) error) error {
	if err := d.selectFolder(folder); err != nil {
		return err
	}
	return fn(d.work)
}

// onWorkOnce is onWork without the retry, for an operation that may not be
// repeated blindly (ADR-0017).
func (d *Driver) onWorkOnce(folder string, fn func(*imapclient.Client) error) error {
	d.workMu.Lock()
	defer d.workMu.Unlock()
	return d.tryWork(folder, fn)
}

// onCtl runs fn against the detection connection, redialling once if it died.
func (d *Driver) onCtl(fn func(*imapclient.Client) error) error {
	d.ctlMu.Lock()
	defer d.ctlMu.Unlock()

	err := fn(d.ctl)
	if err == nil {
		return nil
	}
	c, rerr := d.connect(nil)
	if rerr != nil {
		return fmt.Errorf("reconnect after %v: %w", err, rerr)
	}
	_ = d.ctl.Close()
	d.ctl = c
	return fn(d.ctl)
}

// Close logs both connections out.
func (d *Driver) Close() error {
	for _, c := range []*imapclient.Client{d.ctl, d.work} {
		if c != nil {
			_ = c.Logout().Wait()
			_ = c.Close()
		}
	}
	return nil
}

// Folders implements mailsync.Driver. \Noselect entries are dropped: they are
// tree nodes rather than folders, and selecting one is an error.
func (d *Driver) Folders(ctx context.Context) ([]string, error) {
	var out []string
	err := d.onCtl(func(c *imapclient.Client) error {
		entries, err := c.List("", "*", nil).Collect()
		if err != nil {
			return err
		}
		out = nil
		for _, e := range entries {
			selectable := true
			for _, a := range e.Attrs {
				if a == imap.MailboxAttrNonExistent || a == imap.MailboxAttrNoSelect {
					selectable = false
				}
			}
			if selectable {
				out = append(out, e.Mailbox)
			}
		}
		return nil
	})
	return out, err
}

// Box is a folder as the setup wizard shows it: what it is called, what the
// server says it is for, and how much is in it. The special-use flag is what
// makes "which of these is Sent" answerable without asking a human to guess
// from names in a language the server chose.
type Box struct {
	Name       string
	SpecialUse string // "sent", "drafts", "archive", "junk", "trash", or ""
	Messages   uint32
	Unseen     uint32
}

// Boxes lists the selectable folders with their special-use flags and counts,
// in one LIST-STATUS round trip.
func (d *Driver) Boxes(ctx context.Context) ([]Box, error) {
	opts := &imap.ListOptions{
		SelectSpecialUse: false,
		ReturnStatus: &imap.StatusOptions{
			NumMessages: true, NumUnseen: true,
		},
	}
	var out []Box
	err := d.onCtl(func(c *imapclient.Client) error {
		entries, err := c.List("", "*", opts).Collect()
		if err != nil {
			return err
		}
		out = nil
		for _, e := range entries {
			box := Box{Name: e.Mailbox}
			selectable := true
			for _, a := range e.Attrs {
				switch a {
				case imap.MailboxAttrNonExistent, imap.MailboxAttrNoSelect:
					selectable = false
				case imap.MailboxAttrSent:
					box.SpecialUse = "sent"
				case imap.MailboxAttrDrafts:
					box.SpecialUse = "drafts"
				case imap.MailboxAttrArchive:
					box.SpecialUse = "archive"
				case imap.MailboxAttrJunk:
					box.SpecialUse = "junk"
				case imap.MailboxAttrTrash:
					box.SpecialUse = "trash"
				}
			}
			if !selectable {
				continue
			}
			if e.Status != nil {
				if e.Status.NumMessages != nil {
					box.Messages = *e.Status.NumMessages
				}
				if e.Status.NumUnseen != nil {
					box.Unseen = uint32(*e.Status.NumUnseen)
				}
			}
			out = append(out, box)
		}
		return nil
	})
	return out, err
}

// Status implements mailsync.Driver with one LIST-STATUS round trip. The cost
// is one row per folder and nothing per message, which is what lets this scale
// past this account (ADR-0006).
// CreateFolder makes a Box, and says nothing if it is already there. Only
// `mailbox setup` calls this: the Routing refuses a destination the account
// does not have rather than creating one (ADR-0019), because a Box appearing
// under somebody's mail is a thing to be asked about, and setup is where there
// is somebody to ask.
func (d *Driver) CreateFolder(ctx context.Context, name string) error {
	return d.onCtl(func(c *imapclient.Client) error {
		err := c.Create(name, nil).Wait()
		if err == nil {
			return nil
		}
		// ALREADYEXISTS is the answer we wanted: two clients creating the same
		// Box is not a conflict, and a server that says so has done the job.
		var status *imap.Error
		if errors.As(err, &status) && status.Code == imap.ResponseCodeAlreadyExists {
			return nil
		}
		return fmt.Errorf("create %s: %w", name, err)
	})
}

func (d *Driver) Status(ctx context.Context, folders []string) ([]mailsync.FolderStatus, error) {
	opts := &imap.ListOptions{ReturnStatus: &imap.StatusOptions{
		NumMessages: true, UIDNext: true, UIDValidity: true, NumUnseen: true,
		HighestModSeq: d.condStore,
	}}
	want := map[string]bool{}
	for _, f := range folders {
		want[f] = true
	}
	var out []mailsync.FolderStatus
	err := d.onCtl(func(c *imapclient.Client) error {
		out = nil
		if !c.Caps().Has(imap.CapListStatus) {
			var err error
			out, err = statusOneByOne(c, folders, d.condStore)
			return err
		}
		entries, err := c.List("", "*", opts).Collect()
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !want[e.Mailbox] || e.Status == nil {
				continue
			}
			out = append(out, statusOf(e.Mailbox, e.Status))
		}
		return nil
	})
	return out, err
}

// statusOneByOne is the fallback for a server without LIST-STATUS.
func statusOneByOne(c *imapclient.Client, folders []string, condStore bool) ([]mailsync.FolderStatus, error) {
	opts := &imap.StatusOptions{
		NumMessages: true, UIDNext: true, UIDValidity: true, NumUnseen: true,
		HighestModSeq: condStore,
	}
	var out []mailsync.FolderStatus
	for _, f := range folders {
		sd, err := c.Status(f, opts).Wait()
		if err != nil {
			return nil, err
		}
		out = append(out, statusOf(f, sd))
	}
	return out, nil
}

func statusOf(name string, sd *imap.StatusData) mailsync.FolderStatus {
	st := mailsync.FolderStatus{
		Name:          name,
		UIDValidity:   sd.UIDValidity,
		UIDNext:       uint32(sd.UIDNext),
		HighestModSeq: sd.HighestModSeq,
	}
	if sd.NumMessages != nil {
		st.NumMessages = *sd.NumMessages
	}
	return st
}

// selectFolder selects a folder, every single time, with no caching.
//
// Reusing a selection is a trap. A connection's view of its selected mailbox
// only advances when the server is allowed to send untagged updates, so `1:*`
// in a UID FETCH silently excludes mail delivered since the last command. Worse,
// once the mailbox has been deleted and recreated elsewhere, Dovecot answers
// "Mailbox was deleted under us" — and a UID SEARCH in that state returns an
// empty set with no error at all, which reads exactly like an empty folder.
//
// A fresh SELECT costs one round trip on a warm connection and makes both
// problems structurally impossible.
func (d *Driver) selectFolder(name string) error {
	if _, err := d.work.Select(name, &imap.SelectOptions{CondStore: d.condStore}).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", name, err)
	}
	return nil
}

// ChangedFlags implements mailsync.Driver using CONDSTORE's CHANGEDSINCE, so
// the cost is proportional to what changed rather than to the folder.
func (d *Driver) ChangedFlags(ctx context.Context, folder string, since uint64) ([]mailsync.FlagUpdate, error) {
	var out []mailsync.FlagUpdate
	err := d.onWork(folder, func(c *imapclient.Client) error {
		all := imap.UIDSet{{Start: 1, Stop: 0}} // 1:*
		msgs, err := c.Fetch(all, &imap.FetchOptions{
			UID: true, Flags: true, ChangedSince: since,
		}).Collect()
		if err != nil {
			return err
		}
		out = make([]mailsync.FlagUpdate, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, mailsync.FlagUpdate{UID: uint32(m.UID), Flags: flagStrings(m.Flags)})
		}
		return nil
	})
	return out, err
}

// FetchEnvelopes implements mailsync.Driver. Headers only: a resync pays this
// for the whole folder, so it must not drag bodies along with it.
func (d *Driver) FetchEnvelopes(ctx context.Context, folder string, uids []uint32) ([]mailsync.Envelope, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	var out []mailsync.Envelope
	err := d.onWork(folder, func(c *imapclient.Client) error {
		// ENVELOPE carries In-Reply-To but not References, so the chain costs
		// one extra header section on the same FETCH (ADR-0008).
		refs := &imap.FetchItemBodySection{
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: []string{"References"},
			Peek:         true,
		}
		msgs, err := c.Fetch(uidSet(uids), &imap.FetchOptions{
			UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true,
			BodySection: []*imap.FetchItemBodySection{refs},
		}).Collect()
		if err != nil {
			return err
		}
		out = make([]mailsync.Envelope, 0, len(msgs))
		for _, m := range msgs {
			e := mailsync.Envelope{
				UID:          uint32(m.UID),
				Flags:        flagStrings(m.Flags),
				InternalDate: m.InternalDate,
				Size:         m.RFC822Size,
			}
			if m.Envelope != nil {
				e.MessageID = strings.Trim(m.Envelope.MessageID, "<>")
				e.Date = m.Envelope.Date
				e.Subject = m.Envelope.Subject
				e.From = addrList(m.Envelope.From)
				e.To = addrList(m.Envelope.To)
				e.Cc = addrList(m.Envelope.Cc)
				e.InReplyTo = messageIDs(strings.Join(m.Envelope.InReplyTo, " "))
			}
			for _, sec := range m.BodySection {
				e.References = messageIDs(string(sec.Bytes))
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

// FetchBodies implements mailsync.Driver. It reads BODYSTRUCTURE first and then
// asks only for the text/* parts by path: attachments are never fetched, which
// is what keeps the Mirror at ~18 MB rather than ~610 MB (ADR-0003).
func (d *Driver) FetchBodies(ctx context.Context, folder string, uids []uint32) ([]mailsync.Body, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	var out []mailsync.Body
	err := d.onWork(folder, func(c *imapclient.Client) error {
		out = nil
		structs, err := c.Fetch(uidSet(uids), &imap.FetchOptions{
			// Extended, which is BODYSTRUCTURE rather than BODY: the plain form
			// carries no Content-Disposition at all, so every disposition test
			// downstream — is this part an attachment or is it the body? —
			// silently sees nothing.
			UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		}).Collect()
		if err != nil {
			return err
		}

		// Group messages by the shape of their text parts before asking for
		// them. A FETCH lists sections once and applies them to every message
		// in the set, so pooling every message's parts into one request asks
		// the server for parts×messages sections — for a folder of 260 mails
		// that is a hundred thousand of them, and it does not come back.
		// Messages share very few distinct shapes, so this is a handful of
		// round trips whatever the folder size.
		byShape := map[string][]uint32{}
		partsOf := map[string][]part{}
		byUID := map[uint32]map[string]part{}
		for _, m := range structs {
			if m.BodyStructure == nil {
				continue
			}
			ps := textParts(m.BodyStructure)
			if len(ps) == 0 {
				continue
			}
			key := shapeOf(ps)
			byShape[key] = append(byShape[key], uint32(m.UID))
			partsOf[key] = ps
			byUID[uint32(m.UID)] = map[string]part{}
			for _, p := range ps {
				byUID[uint32(m.UID)][pathKey(p.path)] = p
			}
		}

		// The metadata of everything that is not mirrored text, recorded while
		// the BODYSTRUCTURE is in hand: it is what `attachment list` answers
		// from the Mirror, and the path is how the bytes are fetched later.
		attachments := map[uint32][]mailsync.PartInfo{}
		for _, m := range structs {
			if m.BodyStructure != nil {
				attachments[uint32(m.UID)] = attachmentParts(m.BodyStructure)
			}
		}

		bodies := map[uint32]*mailsync.Body{}
		for key, shapeUIDs := range byShape {
			ps := partsOf[key]
			sections := make([]*imap.FetchItemBodySection, 0, len(ps))
			for _, p := range ps {
				sections = append(sections, p.section())
			}
			msgs, err := c.Fetch(uidSet(shapeUIDs), &imap.FetchOptions{
				UID: true, BodySection: sections,
			}).Collect()
			if err != nil {
				return err
			}
			for _, m := range msgs {
				b := bodies[uint32(m.UID)]
				if b == nil {
					b = &mailsync.Body{UID: uint32(m.UID)}
					bodies[uint32(m.UID)] = b
				}
				for _, sec := range m.BodySection {
					p, ok := byUID[uint32(m.UID)][pathKey(sec.Section.Part)]
					if !ok {
						continue
					}
					switch p.kind {
					case "plain":
						b.Plain += p.decode(sec.Bytes)
					case "html":
						b.HTML += p.decode(sec.Bytes)
					}
				}
			}
		}
		// A message with attachments and no text part at all still has parts
		// worth recording, so the Body exists either way.
		for uid, parts := range attachments {
			if bodies[uid] == nil {
				bodies[uid] = &mailsync.Body{UID: uid}
			}
			bodies[uid].Parts = parts
		}
		for _, b := range bodies {
			out = append(out, *b)
		}
		return nil
	})
	return out, err
}

// FetchPart implements mailsync.Driver. It reads the BODYSTRUCTURE again to
// learn the part's transfer encoding — the Mirror does not keep it, and
// decoding is not optional: a base64 PDF written to disk undecoded is not a
// PDF. That is one extra round trip on a command that was always going to the
// server anyway (ADR-0003).
func (d *Driver) FetchPart(ctx context.Context, folder string, uid uint32, path string) ([]byte, error) {
	var out []byte
	err := d.onWork(folder, func(c *imapclient.Client) error {
		structs, err := c.Fetch(uidSet([]uint32{uid}), &imap.FetchOptions{
			// Extended, which is BODYSTRUCTURE rather than BODY: the plain form
			// carries no Content-Disposition at all, so every disposition test
			// downstream — is this part an attachment or is it the body? —
			// silently sees nothing.
			UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		}).Collect()
		if err != nil {
			return err
		}
		if len(structs) == 0 || structs[0].BodyStructure == nil {
			return fmt.Errorf("no message %d in %s", uid, folder)
		}
		want, ok := findPart(structs[0].BodyStructure, path)
		if !ok {
			return fmt.Errorf("no part %s in message %d", path, uid)
		}
		msgs, err := c.Fetch(uidSet([]uint32{uid}), &imap.FetchOptions{
			UID: true, BodySection: []*imap.FetchItemBodySection{want.section()},
		}).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			for _, sec := range m.BodySection {
				out = want.decodeBytes(sec.Bytes)
			}
		}
		if out == nil {
			return fmt.Errorf("part %s of message %d came back empty", path, uid)
		}
		return nil
	})
	return out, err
}

// Append implements mailsync.Driver. It files a message we composed — the copy
// of a sent mail — and reports the uid UIDPLUS gave it, so the caller can name
// the copy without waiting for a cycle (ADR-0004).
//
// APPEND needs no selected mailbox, and it is deliberately not retried on a
// dropped connection: an APPEND whose ack was lost has still filed the message,
// and a second one leaves two copies of the same mail in Sent (ADR-0017).
func (d *Driver) Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error) {
	d.workMu.Lock()
	defer d.workMu.Unlock()

	cmd := d.work.Append(folder, int64(len(raw)), &imap.AppendOptions{
		Flags: imapFlags(flags), Time: time.Now(),
	})
	if _, err := cmd.Write(raw); err != nil {
		return 0, fmt.Errorf("append to %s: %w", folder, err)
	}
	if err := cmd.Close(); err != nil {
		return 0, fmt.Errorf("append to %s: %w", folder, err)
	}
	data, err := cmd.Wait()
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", folder, err)
	}
	if data == nil {
		return 0, nil // no UIDPLUS: the next cycle finds the copy
	}
	return uint32(data.UID), nil
}

// SentFolder asks the server which Box sent mail belongs in, by its special-use
// flag rather than by its name. A name is a guess that is wrong in every
// language but one, and this account answers \Sent for it.
func (d *Driver) SentFolder(ctx context.Context) (string, error) {
	var out string
	err := d.onCtl(func(c *imapclient.Client) error {
		entries, err := c.List("", "*", &imap.ListOptions{SelectSpecialUse: true}).Collect()
		if err != nil {
			return err
		}
		out = ""
		for _, e := range entries {
			for _, a := range e.Attrs {
				if a == imap.MailboxAttrSent {
					out = e.Mailbox
				}
			}
		}
		return nil
	})
	return out, err
}

// attachmentParts lists everything in a message that is not the text we mirror:
// the files, and the images an HTML mail refers to. A container is not a part
// anybody can save, so only single parts are listed.
func attachmentParts(bs imap.BodyStructure) []mailsync.PartInfo {
	var out []mailsync.PartInfo
	bs.Walk(func(path []int, p imap.BodyStructure) bool {
		sp, ok := p.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		disp := ""
		filename := ""
		if d := sp.Disposition(); d != nil {
			disp = strings.ToLower(d.Value)
			filename = d.Params["filename"]
		}
		if filename == "" {
			filename = sp.Params["name"]
		}
		// A text/plain or text/html part with no disposition is the body, and
		// the Mirror already holds it. One that is attached is a file.
		if strings.EqualFold(sp.Type, "text") && disp != "attachment" {
			kind := strings.ToLower(sp.Subtype)
			if kind == "plain" || kind == "html" {
				return true
			}
		}
		out = append(out, mailsync.PartInfo{
			Path:        pathString(path),
			MIMEType:    strings.ToLower(sp.Type + "/" + sp.Subtype),
			Filename:    filename,
			Disposition: disp,
			Size:        int64(sp.Size),
			ContentID:   strings.Trim(sp.ID, "<>"),
		})
		return true
	})
	return out
}

// findPart locates a part by the path string the Mirror recorded.
func findPart(bs imap.BodyStructure, path string) (part, bool) {
	var found part
	ok := false
	bs.Walk(func(p []int, node imap.BodyStructure) bool {
		sp, isSingle := node.(*imap.BodyStructureSinglePart)
		if !isSingle || pathString(p) != path {
			return true
		}
		found = part{path: append([]int(nil), p...), encoding: sp.Encoding}
		ok = true
		return false
	})
	return found, ok
}

// pathString renders an IMAP part path the way the protocol writes it.
func pathString(path []int) string {
	parts := make([]string, 0, len(path))
	for _, n := range path {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ".")
}

type part struct {
	path     []int
	kind     string
	encoding string
	charset  string
}

// decode turns a raw body section into text. A text part arrives in its
// transfer encoding and its own charset, so storing the bytes as they came off
// the wire puts "w=C3=A4r" in the Mirror where "wär" belongs — and every reader
// of the Mirror would then have to undo it.
func (p part) decode(raw []byte) string {
	var r io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(p.encoding) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	}
	if p.charset != "" {
		if cr, err := charset.Reader(p.charset, r); err == nil {
			r = cr
		}
	}
	b, err := io.ReadAll(r)
	if err != nil && len(b) == 0 {
		// A part we cannot decode is better kept than dropped.
		return string(raw)
	}
	return string(b)
}

// decodeBytes undoes the transfer encoding and nothing else. An attachment is
// bytes, not text: running it through a charset reader would corrupt it.
func (p part) decodeBytes(raw []byte) []byte {
	var r io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(p.encoding) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	default:
		return raw
	}
	b, err := io.ReadAll(r)
	if err != nil && len(b) == 0 {
		return raw
	}
	return b
}

// section is the FETCH item for this part. A message that is not multipart has
// no part path, and BODY[TEXT] is how you ask for its body without its headers.
func (p part) section() *imap.FetchItemBodySection {
	if len(p.path) == 0 {
		return &imap.FetchItemBodySection{Specifier: imap.PartSpecifierText, Peek: true}
	}
	return &imap.FetchItemBodySection{Part: append([]int(nil), p.path...), Peek: true}
}

// shapeOf identifies a message's MIME layout, so messages laid out the same way
// can be fetched together.
func shapeOf(ps []part) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.kind)
		b.WriteString(pathKey(p.path))
		b.WriteByte(';')
	}
	return b.String()
}

func pathKey(path []int) string {
	var b strings.Builder
	for _, n := range path {
		fmt.Fprintf(&b, "%d.", n)
	}
	return b.String()
}

// textParts returns the paths of every text/plain and text/html part, skipping
// anything marked as an attachment.
func textParts(bs imap.BodyStructure) []part {
	var out []part
	bs.Walk(func(path []int, p imap.BodyStructure) bool {
		sp, ok := p.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if disp := sp.Disposition(); disp != nil && strings.EqualFold(disp.Value, "attachment") {
			return true
		}
		if !strings.EqualFold(sp.Type, "text") {
			return true
		}
		kind := strings.ToLower(sp.Subtype)
		if kind != "plain" && kind != "html" {
			return true
		}
		out = append(out, part{
			path:     append([]int(nil), path...),
			kind:     kind,
			encoding: sp.Encoding,
			charset:  sp.Params["charset"],
		})
		return true
	})
	return out
}

// AllUIDs implements mailsync.Driver. ESEARCH returns compressed ranges, so
// even a large folder is a small reply — this is the expunge diff's cost.
func (d *Driver) AllUIDs(ctx context.Context, folder string) ([]uint32, error) {
	var out []uint32
	err := d.onWork(folder, func(c *imapclient.Client) error {
		data, err := c.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{ReturnAll: true}).Wait()
		if err != nil {
			return err
		}
		uids := data.AllUIDs()
		out = make([]uint32, 0, len(uids))
		for _, u := range uids {
			out = append(out, uint32(u))
		}
		return nil
	})
	return out, err
}

// StoreFlags implements mailsync.Driver. Adding and removing are two STOREs —
// IMAP has no combined form — and the flags are then read back, because a
// server is not obliged to put the UID in the untagged FETCH a STORE provokes
// and a flag written into the Mirror against the wrong uid is worse than a
// round trip. Flags are idempotent, so this may take the retrying path.
func (d *Driver) StoreFlags(ctx context.Context, folder string, uids []uint32, add, remove []string) ([]mailsync.FlagUpdate, error) {
	if len(uids) == 0 || (len(add) == 0 && len(remove) == 0) {
		return nil, nil
	}
	var out []mailsync.FlagUpdate
	err := d.onWork(folder, func(c *imapclient.Client) error {
		set := uidSet(uids)
		for _, step := range []struct {
			op    imap.StoreFlagsOp
			flags []string
		}{{imap.StoreFlagsAdd, add}, {imap.StoreFlagsDel, remove}} {
			if len(step.flags) == 0 {
				continue
			}
			store := &imap.StoreFlags{Op: step.op, Silent: true, Flags: imapFlags(step.flags)}
			if err := c.Store(set, store, nil).Close(); err != nil {
				return err
			}
		}
		msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return err
		}
		out = make([]mailsync.FlagUpdate, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, mailsync.FlagUpdate{UID: uint32(m.UID), Flags: flagStrings(m.Flags)})
		}
		return nil
	})
	return out, err
}

// Move implements mailsync.Driver. UIDPLUS makes the ack say where each message
// landed, so the Mirror never has to guess (ADR-0004); a server that does not
// answer with COPYUID leaves the destination to the next cycle.
//
// This is the one operation that is not retried on a dropped connection. A MOVE
// whose ack was lost has still happened, and doing it again would move whatever
// now holds that uid (ADR-0017).
func (d *Driver) Move(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	out := map[uint32]uint32{}
	err := d.onWorkOnce(folder, func(c *imapclient.Client) error {
		data, err := c.Move(uidSet(uids), dest).Wait()
		if err != nil {
			return err
		}
		if data == nil {
			return nil
		}
		src, srcOK := numsOf(data.SourceUIDs)
		dst, dstOK := numsOf(data.DestUIDs)
		if !srcOK || !dstOK || len(src) != len(dst) {
			return nil // no UIDPLUS: the destination is the next cycle's problem
		}
		for i := range src {
			out[src[i]] = dst[i]
		}
		return nil
	})
	return out, err
}

// numsOf flattens a UID set the server sent back. A dynamic set (one with a `*`
// in it) has no enumerable members and comes back not-ok.
func numsOf(set imap.NumSet) ([]uint32, bool) {
	us, ok := set.(imap.UIDSet)
	if !ok {
		return nil, false
	}
	nums, ok := us.Nums()
	if !ok {
		return nil, false
	}
	out := make([]uint32, 0, len(nums))
	for _, n := range nums {
		out = append(out, uint32(n))
	}
	return out, true
}

func imapFlags(flags []string) []imap.Flag {
	out := make([]imap.Flag, 0, len(flags))
	for _, f := range flags {
		out = append(out, imap.Flag(f))
	}
	return out
}

// Watch holds IDLE on a folder and reports that something happened. IDLE costs
// one connection per selected folder and NOTIFY is unavailable, so it is spent
// only on the folders where sub-second latency is worth it (ADR-0006).
func (d *Driver) Watch(ctx context.Context, folder string, events chan<- mailsync.Event) error {
	notify := make(chan struct{}, 1)
	nudge := func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	// A connection running IDLE may send no other command until DONE, so a
	// watcher cannot share the connection that fetches.
	c, err := d.connect(&imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			// Any unsolicited word from the server means "run a cycle". The
			// sequence numbers go-imap reports here would need a seq->uid map
			// to be actionable; a cycle finds the same facts from uids.
			Mailbox: func(*imapclient.UnilateralDataMailbox) { nudge() },
			Expunge: func(uint32) { nudge() },
			Fetch:   func(*imapclient.FetchMessageData) { nudge() },
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout().Wait(); _ = c.Close() }()

	if _, err := c.Select(folder, &imap.SelectOptions{CondStore: d.condStore}).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", folder, err)
	}
	idle, err := c.Idle()
	if err != nil {
		return fmt.Errorf("idle: %w", err)
	}
	defer func() {
		_ = idle.Close()
		_ = idle.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
			select {
			case events <- mailsync.Event{Kind: mailsync.EventChanged}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func uidSet(uids []uint32) imap.UIDSet {
	var s imap.UIDSet
	for _, u := range uids {
		s.AddNum(imap.UID(u))
	}
	return s
}

// messageIDs pulls Message-IDs out of a header value. Real mail puts all sorts
// of things in References — commas, folded lines, bare ids without brackets —
// so this takes what is inside angle brackets and, failing that, whitespace
// separated words that look like an addr-spec.
func messageIDs(value string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	rest := value
	for {
		open := strings.Index(rest, "<")
		if open < 0 {
			break
		}
		close := strings.Index(rest[open:], ">")
		if close < 0 {
			break
		}
		add(rest[open+1 : open+close])
		rest = rest[open+close+1:]
	}
	if len(out) > 0 {
		return out
	}
	// No brackets at all: fall back to anything that looks like an id.
	for _, f := range strings.Fields(value) {
		f = strings.Trim(f, ",;")
		if strings.Contains(f, "@") && !strings.EqualFold(f, "References:") {
			add(f)
		}
	}
	return out
}

func flagStrings(flags []imap.Flag) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		out = append(out, string(f))
	}
	return out
}

func addrList(addrs []imap.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Addr()))
		} else {
			parts = append(parts, a.Addr())
		}
	}
	return strings.Join(parts, ", ")
}

var _ io.Closer = (*Driver)(nil)
var _ mailsync.Driver = (*Driver)(nil)
