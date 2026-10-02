package daemon

// A Message's id, both ways: `[account/][box:]uid`, with an attachment's index
// after it. formatMessageID and parseMessageID are each other's inverse over an
// Account's Mirrored Boxes, and the round-trip test holds them to it.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"mailbox/internal/routing"
)

// resolveID reads `[account/][box:]uid`. An unqualified id means the Primary
// Account, so every id that worked when there was one account still works
// verbatim (ADR-0005), and a bare uid means its Inbox.
func (d *Daemon) resolveID(value string) (*Account, string, uint32, error) {
	return d.resolveIDOn(value, d.Primary, inbox)
}

// resolveIDOn is resolveID for a command that has already said which account
// and which Box it is about: an id naming no account is on home, and a bare
// uid is in the Box bare finds on the account the id turned out to be on.
func (d *Daemon) resolveIDOn(value string, home *Account, bare func(*Account) (string, error)) (*Account, string, uint32, error) {
	name, rest := splitAccount(value, d.accountNames())
	a := home
	if name != "" {
		var err error
		if a, err = d.accountNamed(name); err != nil {
			return nil, "", 0, err
		}
	}
	box := ""
	if !strings.Contains(rest, ":") {
		var err error
		if box, err = bare(a); err != nil {
			return nil, "", 0, err
		}
	}
	folder, uid, err := parseMessageID(rest, a.Mirrored, box)
	return a, folder, uid, err
}

// inbox is where a bare uid is, for every command but the draft ones.
func inbox(*Account) (string, error) { return "INBOX", nil }

// resolveAttachmentID reads `[account/]box:uid[:index]`.
func (d *Daemon) resolveAttachmentID(value string) (*Account, string, uint32, int, error) {
	name, rest := splitAccount(value, d.accountNames())
	a, err := d.accountNamed(name)
	if err != nil {
		return nil, "", 0, 0, err
	}
	folder, uid, index, err := parseAttachmentID(rest, a.Mirrored)
	return a, folder, uid, index, err
}

// splitAccount takes the account prefix off an id. A Box name can contain a
// slash — `INBOX/Screener` — so only a prefix that names an account counts as
// one, and everything else is part of the Box.
func splitAccount(value string, names []string) (account, rest string) {
	v := strings.TrimSpace(value)
	// The account on its own names all of it: `box view gmx` is that account's
	// Inbox, and `search --in gmx` is that account's mail.
	for _, n := range names {
		if n != "" && strings.EqualFold(n, v) {
			return v, ""
		}
	}
	i := strings.Index(v, "/")
	if i <= 0 {
		return "", v
	}
	head := v[:i]
	for _, n := range names {
		if strings.EqualFold(n, head) {
			return head, v[i+1:]
		}
	}
	return "", v
}

// qualify puts the account back on an id. The Primary Account is never written:
// an id from a one-account setup and the same id from a two-account one are the
// same string (ADR-0005).
func (a *Account) qualify(id string) string {
	if a == nil || a.Primary || a.Name == "" {
		return id
	}
	return a.Name + "/" + id
}

// messageID is the id a caller hands back to a read command.
func (a *Account) messageID(folder string, uid uint32) string {
	return a.qualify(formatMessageID(folder, uid, a.Mirrored))
}

// attachmentID names one file on one Message: the Placement id, then which
// file. Index is 1-based and matches the listing.
func attachmentID(a *Account, folder string, uid uint32, index int) string {
	return fmt.Sprintf("%s:%d", a.messageID(folder, uid), index)
}

// parseAttachmentID reads [box:]uid[:index]. The index is optional because a
// Message with one attachment is named by the Message.
func parseAttachmentID(value string, known []string) (folder string, uid uint32, index int, err error) {
	v := strings.TrimSpace(value)
	if i := strings.LastIndex(v, ":"); i >= 0 {
		if n, convErr := strconv.Atoi(v[i+1:]); convErr == nil && n > 0 {
			if f, u, mErr := parseMessageID(v[:i], known, "INBOX"); mErr == nil {
				return f, u, n, nil
			}
		}
	}
	f, u, err := parseMessageID(v, known, "INBOX")
	return f, u, 0, err
}

// parseMessageID reads the [box:]uid a listing printed back into a Placement.
// A bare uid is in bare — the Inbox, which is the Box an agent is usually
// looking at, for everything but a draft.
func parseMessageID(value string, known []string, bare string) (string, uint32, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", 0, errors.New("message id must be [box:]uid")
	}
	folder := bare
	if i := strings.LastIndex(v, ":"); i >= 0 {
		folder, v = resolveBox(strings.TrimSpace(v[:i]), known), strings.TrimSpace(v[i+1:])
	}
	uid, err := strconv.ParseUint(v, 10, 32)
	if err != nil || uid == 0 {
		return "", 0, fmt.Errorf("message id must be [box:]uid, got %q", value)
	}
	return folder, uint32(uid), nil
}

// resolveBox maps what a caller typed onto a mirrored folder name. A Box under
// the Inbox answers to its short name — `Screener`, not `INBOX/Screener` —
// because that is the name its ids are printed with. A Box named outright wins
// over one that only matches with the prefix put back, so a top-level folder is
// never shadowed by a child of the Inbox with the same name.
func resolveBox(name string, known []string) string {
	if strings.EqualFold(name, "inbox") {
		return "INBOX"
	}
	for _, k := range known {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	for _, k := range known {
		if strings.EqualFold(k, "INBOX/"+name) {
			return k
		}
	}
	// The short vocabulary `mailbox route` uses for the boxes the routing files
	// into — "paper", "trail", "feed", "block" — so a name that names a box there
	// names the same box here.
	if d, err := routing.ParseDestination(name); err == nil {
		if box := d.Box(); box != "" {
			return box
		}
	}
	return name
}

// shortBox is the name a Box is printed with. Everything under the Inbox loses
// the prefix, unless a Box of that name exists at the top level too — there the
// short form would name two Boxes, so neither gets it.
func shortBox(folder string, known []string) string {
	short, ok := strings.CutPrefix(folder, "INBOX/")
	if !ok {
		return folder
	}
	for _, k := range known {
		if !strings.EqualFold(k, folder) && strings.EqualFold(k, short) {
			return folder
		}
	}
	return short
}

// formatMessageID is the id a caller hands back to message view. The Inbox is
// implicit, because that is the Box most ids come from, and its children are
// named without it: `Screener:342`, not `INBOX/Screener:342`.
func formatMessageID(folder string, uid uint32, known []string) string {
	if folder == "INBOX" {
		return fmt.Sprintf("%d", uid)
	}
	return fmt.Sprintf("%s:%d", shortBox(folder, known), uid)
}
