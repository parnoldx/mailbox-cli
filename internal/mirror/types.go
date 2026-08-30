package mirror

import (
	"strings"
	"time"
)

// FolderState is what the Mirror knows about one folder's synchronisation.
// A zero UIDValidity means the folder has never been synced.
type FolderState struct {
	Account       string
	Name          string
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
	Count         int
	SyncedAt      time.Time
}

// Synced reports whether this folder has ever been mirrored.
func (f FolderState) Synced() bool { return f.UIDValidity != 0 }

// Message is one email, identified within an account by its Message-ID. It
// outlives the Placements that point at it, so a move or a UIDVALIDITY reset
// does not lose the body we already paid to fetch.
type Message struct {
	ID         int64
	Key        string // RFC822 Message-ID, or "folder:uid" when absent (ADR-0007)
	Date       time.Time
	Subject    string
	From       string
	To         string
	Cc         string
	InReplyTo  []string
	References []string
	// ThreadID is the id of the oldest Message known to be in this
	// conversation. A Message on its own is its own Thread.
	ThreadID  int64
	TextPlain string
	TextHTML  string
	BodyState string // "mirrored" | "pending"
}

// Placement is where a Message currently sits: a folder, a uid, and its flags.
type Placement struct {
	Folder       string
	UID          uint32
	MessageID    int64
	Flags        []string
	InternalDate time.Time
	Size         int64
}

// Row is a Placement joined to its Message, which is what a listing shows.
type Row struct {
	Placement
	Message
}

// Seen reports whether the placement carries the \Seen flag.
func (r Row) Seen() bool {
	for _, f := range r.Placement.Flags {
		if f == `\Seen` {
			return true
		}
	}
	return false
}

// Part is one thing a Message carries that is not its mirrored text: a file, or
// an image an HTML mail refers to. The bytes are never here (ADR-0003).
type Part struct {
	MessageID   int64
	Path        string // the IMAP part path, "2" or "1.3"
	MIMEType    string
	Filename    string
	Disposition string // "attachment", "inline", or empty
	Size        int64
}

// Name is what to call this Part on disk. A part with no filename still has to
// be saveable, so it is named after its path and type.
func (p Part) Name() string {
	if p.Filename != "" {
		return p.Filename
	}
	ext := ""
	if i := strings.LastIndex(p.MIMEType, "/"); i >= 0 {
		ext = "." + p.MIMEType[i+1:]
	}
	return "part-" + strings.ReplaceAll(p.Path, ".", "-") + ext
}
