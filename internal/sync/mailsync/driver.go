// Package mailsync reconciles a Mirror against an IMAP server using CONDSTORE,
// without QRESYNC (ADR-0006).
package mailsync

import (
	"context"
	"time"
)

// FolderStatus is what one LIST-STATUS row says about a folder. Detecting
// change costs one of these per folder per cycle, and nothing per message.
type FolderStatus struct {
	Name          string
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
	NumMessages   uint32
}

// Envelope is a message's headers, without its text. Fetched for every message
// in a folder on a resync, which is why it is separate from Body.
type Envelope struct {
	UID       uint32
	MessageID string // RFC822 Message-ID, without angle brackets; may be empty
	Date      time.Time
	Subject   string
	From      string
	To        string
	Cc        string
	// InReplyTo and References are what a Thread is built from, and the only
	// reason the envelope fetch asks for a header section as well (ADR-0008).
	// Both are Message-IDs without angle brackets, References oldest first.
	InReplyTo    []string
	References   []string
	Flags        []string
	InternalDate time.Time
	Size         int64
}

// Body is a message's text parts, and the metadata of everything else it
// carries. Attachment bytes are never fetched here (ADR-0003); naming one
// specifically is what fetches it.
type Body struct {
	UID   uint32
	Plain string
	HTML  string
	Parts []PartInfo
}

// PartInfo is one part that is not mirrored text: what it is, what it is
// called, and how big it is. Enough to answer `attachment list` from the
// Mirror, and enough to fetch the bytes later by path.
type PartInfo struct {
	Path        string // the IMAP part path, "2" or "1.3"
	MIMEType    string // "application/pdf"
	Filename    string
	Disposition string // "attachment", "inline", or empty
	Size        int64
	// ContentID is the part's Content-ID with the angle brackets stripped, so
	// an HTML body's <img src="cid:..."> can be matched to it. Empty when the
	// part carried none.
	ContentID string
}

// FlagUpdate is one message whose flags changed since a modseq.
type FlagUpdate struct {
	UID   uint32
	Flags []string
}

// EventKind distinguishes the unsolicited things a server says while idling.
type EventKind int

const (
	// EventChanged means something happened in the watched folder and the
	// reconciler should run a cycle. Pushes carry no data (ADR-0011) and
	// neither does this.
	EventChanged EventKind = iota
	// EventExpunge is a message leaving the folder, reported by the driver as a
	// uid — it resolves the sequence number go-imap hands it using the seq->uid
	// map it keeps for the selected folder.
	EventExpunge
)

// Event is something the server said without being asked.
type Event struct {
	Kind EventKind
	UID  uint32
}

// Driver is everything the reconciler needs from a server. It exists so the
// reconciler can be driven through states no real server can be asked for on
// demand: see fake.go.
type Driver interface {
	// Folders lists the selectable folders on the account.
	Folders(ctx context.Context) ([]string, error)
	// Status returns one row per named folder, in one round trip where the
	// server supports LIST-STATUS.
	Status(ctx context.Context, folders []string) ([]FolderStatus, error)
	// ChangedFlags returns messages whose flags changed after sinceModSeq.
	ChangedFlags(ctx context.Context, folder string, sinceModSeq uint64) ([]FlagUpdate, error)
	// FetchEnvelopes returns headers for uids, without text.
	FetchEnvelopes(ctx context.Context, folder string, uids []uint32) ([]Envelope, error)
	// FetchBodies returns the text parts of uids.
	FetchBodies(ctx context.Context, folder string, uids []uint32) ([]Body, error)
	// StoreFlags adds and removes flags on uids and returns the flags the
	// server reports afterwards. Writing what the ack says rather than what we
	// asked for is what makes the next read true (ADR-0004).
	StoreFlags(ctx context.Context, folder string, uids []uint32, add, remove []string) ([]FlagUpdate, error)
	// Move moves uids to dest and returns each source uid mapped to the uid it
	// now has there. A server without UIDPLUS answers with an empty map, and
	// then only the source side of the move is known (ADR-0004).
	//
	// Unlike every other method here this one is not retried on a dropped
	// connection: a MOVE whose ack was lost has still happened (ADR-0017).
	Move(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error)
	// FetchPart returns the decoded bytes of one part, by the path the Mirror
	// recorded. This is the one read that goes to the server by design: the
	// Mirror holds an Attachment's name and size but never its bytes
	// (ADR-0003).
	FetchPart(ctx context.Context, folder string, uid uint32, path string) ([]byte, error)
	// Append files a message we composed into a Box and returns the uid the
	// server gave it — 0 from a server without UIDPLUS, which leaves the copy
	// to the next cycle to find (ADR-0004).
	//
	// Like Move, this is not retried on a dropped connection. An APPEND whose
	// ack was lost has still filed the message, and doing it again leaves two
	// copies of the same mail in Sent (ADR-0017).
	Append(ctx context.Context, folder string, flags []string, raw []byte) (uint32, error)
	// AllUIDs returns every uid in a folder, for the expunge diff.
	AllUIDs(ctx context.Context, folder string) ([]uint32, error)
	// Watch idles on a folder, sending events until ctx is done.
	Watch(ctx context.Context, folder string, events chan<- Event) error
	// Close releases the connections.
	Close() error
}
