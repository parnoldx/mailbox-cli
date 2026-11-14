package daemon

// Attachments: the one read that waits on the server, because a file is never
// held in the Mirror (ADR-0003). Listing is a Mirror read; fetching one part,
// and saving it to disk, streams it through.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mailbox/internal/mirror"
)

func (d *Daemon) handleAttachmentList(req Request, resp Response) Response {
	id := req.Str("positional")
	acct, folder, uid, _, err := d.resolveAttachmentID(id)
	if err != nil {
		return resp.usage(err.Error())
	}
	parts, err := d.partsOf(acct, folder, uid)
	if err != nil {
		return fail(resp, id, err)
	}
	return resp.ok(viewParts(acct, folder, uid, parts))
}

func (d *Daemon) handleAttachmentSave(ctx context.Context, req Request, resp Response) Response {
	acct, folder, uid, part, err := d.partOf(req.Str("positional"))
	if err != nil {
		return resp.failed(err)
	}
	out := req.Str("output")
	force := req.Bool("force")
	saved, err := d.save(ctx, acct, folder, uid, part, out, force)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(saved)
}

func (d *Daemon) handleAttachmentBytes(ctx context.Context, req Request, resp Response) Response {
	acct, folder, uid, part, err := d.partOf(req.Str("positional"))
	if err != nil {
		return resp.failed(err)
	}
	if part.Size > maxInlineBytes {
		return resp.usage(fmt.Sprintf(
			"%s is %d bytes; use `attachment save` for anything over %d",
			part.Name(), part.Size, int64(maxInlineBytes)))
	}
	if acct.Reconciler == nil {
		return resp.api("this daemon cannot fetch: no server connection")
	}
	body, err := acct.Reconciler.Driver.FetchPart(ctx, folder, uid, part.Path)
	if err != nil {
		return resp.api(err.Error())
	}
	return resp.ok(inlineBytes{
		Filename: part.Name(), MIMEType: part.MIMEType, Size: len(body),
		ContentID: part.ContentID, Base64: base64.StdEncoding.EncodeToString(body),
	})
}

// partsOf reads a Message and what it carries, both from the Mirror. Listing
// what is attached never goes to the server; only saving one does (ADR-0003).
func (d *Daemon) partsOf(a *Account, folder string, uid uint32) ([]mirror.Part, error) {
	r, err := d.Mirror.Row(a.Name, folder, uid)
	if err != nil {
		return nil, err
	}
	return d.Mirror.Parts(r.Message.ID)
}

// partOf resolves an attachment id to the one part it names. The error carries
// its own Code for resp.failed, as fail and pick would have chosen it.
func (d *Daemon) partOf(id string) (*Account, string, uint32, mirror.Part, error) {
	acct, folder, uid, index, err := d.resolveAttachmentID(id)
	if err != nil {
		return nil, "", 0, mirror.Part{}, usageErr("%s", err)
	}
	parts, err := d.partsOf(acct, folder, uid)
	if errors.Is(err, mirror.ErrNotFound) {
		return nil, "", 0, mirror.Part{}, notFoundErr("%s", noSuchMessage(id))
	}
	if err != nil {
		return nil, "", 0, mirror.Part{}, err
	}
	part, err := pick(acct, folder, uid, parts, index)
	if err != nil {
		return nil, "", 0, mirror.Part{}, usageErr("%s", err)
	}
	return acct, folder, uid, part, nil
}

// pick chooses the part a save was asked for. A Message with exactly one
// attachment can be named without an index, because that is how people refer to
// it; a Message with several cannot, and the error says what to type instead.
func pick(a *Account, folder string, uid uint32, parts []mirror.Part, index int) (mirror.Part, error) {
	if len(parts) == 0 {
		return mirror.Part{}, fmt.Errorf("%s has nothing attached", a.messageID(folder, uid))
	}
	if index == 0 {
		if len(parts) > 1 {
			ids := make([]string, 0, len(parts))
			for i := range parts {
				ids = append(ids, attachmentID(a, folder, uid, i+1))
			}
			return mirror.Part{}, fmt.Errorf("%s has %d attachments: name one of %s",
				a.messageID(folder, uid), len(parts), strings.Join(ids, ", "))
		}
		index = 1
	}
	if index < 1 || index > len(parts) {
		return mirror.Part{}, fmt.Errorf("no attachment %d on %s (it has %d)",
			index, a.messageID(folder, uid), len(parts))
	}
	return parts[index-1], nil
}

// save fetches one part and writes it to disk. The Daemon writes the file
// rather than sending the bytes back over the socket: they are the same user on
// the same machine, and a 20 MB PDF has no business being base64 in NDJSON.
func (d *Daemon) save(ctx context.Context, a *Account, folder string, uid uint32, part mirror.Part, out string, force bool) (saved, error) {
	if a.Reconciler == nil {
		return saved{}, errors.New("this daemon cannot fetch: no server connection")
	}
	if out == "" {
		return saved{}, errors.New("no output path given")
	}
	path := out
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		path = filepath.Join(out, part.Name())
	}
	if _, err := os.Stat(path); err == nil && !force {
		return saved{}, fmt.Errorf("%s exists (use --force to overwrite)", path)
	}
	body, err := a.Reconciler.Driver.FetchPart(ctx, folder, uid, part.Path)
	if err != nil {
		return saved{}, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return saved{}, err
	}
	return saved{
		Path: path, Bytes: len(body), Filename: part.Name(), MIMEType: part.MIMEType,
	}, nil
}

// saved is what an attachment save did.
type saved struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
}

// attachment is one row of a listing.
type attachment struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	MIMEType    string `json:"mime_type"`
	Disposition string `json:"disposition"`
	Size        int64  `json:"size"`
	// ContentID lets a reading pane match an inline part to an <img src="cid:">
	// in the HTML body. Empty for a plain enclosure.
	ContentID string `json:"content_id,omitempty"`
}

func viewParts(a *Account, folder string, uid uint32, parts []mirror.Part) []attachment {
	out := make([]attachment, 0, len(parts))
	for i, p := range parts {
		out = append(out, attachment{
			ID: attachmentID(a, folder, uid, i+1), Index: i + 1,
			Filename: p.Name(), MIMEType: p.MIMEType,
			Disposition: p.Disposition, Size: p.Size, ContentID: p.ContentID,
		})
	}
	return out
}

// maxInlineBytes caps `attachment bytes`. Inline images a body refers to are
// logos and banners; anything larger is a real enclosure and belongs in
// `attachment save`, which streams to disk instead of base64 down the socket.
const maxInlineBytes = 10 << 20

// inlineBytes is what `attachment bytes` returns: one part decoded and
// base64-wrapped, small enough to inline into an HTML document.
type inlineBytes struct {
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	Size      int    `json:"size"`
	ContentID string `json:"content_id,omitempty"`
	Base64    string `json:"base64"`
}
