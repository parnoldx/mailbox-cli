package sieve

import (
	"fmt"
	"log"
	"time"
)

// Mailbox is the IMAP surface the watcher needs (implemented by *mail.Mail).
type Mailbox interface {
	ListFolders() ([]string, error)
	CreateFolder(name string) error
	UIDSenders(folder string) (map[string]string, error)
	SeenAndDelete(folder, uid string) error
}

// Watcher polls the routing folders and reports arrivals as movements,
// like the original standalone helper's watch loop (OUT detection was disabled
// there and stays disabled).
type Watcher struct {
	MB       Mailbox
	Interval time.Duration
	state    map[string]map[string]string // folder -> uid -> from
}

// EnsureFolders creates missing routing folders on startup.
func (w *Watcher) EnsureFolders() error {
	existing := map[string]bool{}
	names, err := w.MB.ListFolders()
	if err != nil {
		return fmt.Errorf("failed to list folders: %w", err)
	}
	for _, n := range names {
		existing[n] = true
	}
	for _, folder := range RuleFolders {
		if existing[folder] {
			continue
		}
		log.Printf("sieve: creating missing folder: %s", folder)
		if err := w.MB.CreateFolder(folder); err != nil {
			log.Printf("sieve: warning: cannot create %s: %v", folder, err)
		}
	}
	return nil
}

// Snapshot fetches the initial per-folder state so Poll only reports new mail.
func (w *Watcher) Snapshot() error {
	w.state = map[string]map[string]string{}
	for _, folder := range RuleFolders {
		senders, err := w.MB.UIDSenders(folder)
		if err != nil {
			log.Printf("sieve: warning: cannot read %s: %v", folder, err)
			continue
		}
		w.state[folder] = senders
	}
	return nil
}

// Poll scans all folders once and returns arrivals since the last poll.
func (w *Watcher) Poll() ([]Movement, error) {
	var movements []Movement
	for _, folder := range RuleFolders {
		current, err := w.MB.UIDSenders(folder)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", folder, err)
		}
		old := w.state[folder]

		if folder == FolderBlock {
			movements = append(movements, w.handleBlock(current, old)...)
			// Block messages were deleted; resync so the diff below is clean.
			current, err = w.MB.UIDSenders(folder)
			if err != nil {
				return nil, fmt.Errorf("cannot re-read %s: %w", folder, err)
			}
		} else {
			for uid, from := range current {
				if _, seen := old[uid]; !seen {
					movements = append(movements, Movement{Folder: folder, Address: from})
				}
			}
		}
		w.state[folder] = current
	}
	return movements, nil
}

// handleBlock blacklists new arrivals in Block and deletes them (mark \Seen +
// \Deleted, then expunge — done server-side by SeenAndDelete).
func (w *Watcher) handleBlock(current, old map[string]string) []Movement {
	var movements []Movement
	for uid, from := range current {
		if _, seen := old[uid]; seen {
			continue
		}
		movements = append(movements, Movement{Folder: FolderBlock, Address: from})
		if err := w.MB.SeenAndDelete(FolderBlock, uid); err != nil {
			log.Printf("sieve: cannot delete %s %s: %v", FolderBlock, uid, err)
		} else {
			log.Printf("sieve: blacklisted and deleted %s (uid %s)", from, uid)
		}
	}
	return movements
}
