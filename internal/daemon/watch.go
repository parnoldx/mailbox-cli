package daemon

import (
	"sort"
	"strings"
	"time"

	"mailbox/internal/mirror"
	"mailbox/internal/sync/davsync"
	"mailbox/internal/sync/mailsync"
)

// The events a watch reports. The mail ones name a Thread's Messages moving,
// the collection ones an Event, Todo, Habit or Contact and the Collections
// themselves, and the last two describe the watch rather than the mailbox.
//
// "new" is not an event: it is the word `--events new` selects by, and it
// arrives as a flag on an added or updated line. What is new mail is decided
// once, in watchMail, so a script and a human are told the same thing.
const (
	eventAdded   = "added"
	eventUpdated = "updated"
	eventDeleted = "deleted"
	eventResync  = "resync"
	eventNew     = "new"
	// A code mail the Daemon collected: the code is already on the clipboard
	// and the mail is already read, so this reports what was taken rather than
	// that something arrived. It replaces the `added`+new line the same mail
	// would otherwise have produced — a Pickup is never news.
	eventPickup = "pickup"

	eventObjectAdded       = "object_added"
	eventObjectUpdated     = "object_updated"
	eventObjectDeleted     = "object_deleted"
	eventCollectionAdded   = "collection_added"
	eventCollectionUpdated = "collection_updated"
	eventCollectionDeleted = "collection_deleted"
	eventCollectionResync  = "collection_resync"

	eventReady        = "ready"
	eventDisconnected = "disconnected"
)

// mailEvents and collectionEvents are the two halves an --events list can name.
// A list naming only mail events switches the collections off, the way --box
// does: both say "this watch is about mail" without a flag that says so.
var mailEvents = []string{eventAdded, eventUpdated, eventDeleted, eventResync, eventNew, eventPickup}

var collectionEvents = []string{
	eventObjectAdded, eventObjectUpdated, eventObjectDeleted,
	eventCollectionAdded, eventCollectionUpdated, eventCollectionDeleted, eventCollectionResync,
}

// watcher is one connection that asked for the feed, and what it asked for. A
// nil events map is every event; an empty boxes list is every Box.
type watcher struct {
	boxes  []string
	events map[string]bool
	// ready says the "ready" line has been sent since this watch began or since
	// the last drop. It is here rather than on the Daemon because two watches
	// started a minute apart are caught up at different moments.
	ready bool
}

// wants says whether this watch reports one Change. The two lines that describe
// the watch itself are never filtered out: a client that cannot be told the
// connection dropped is a client that silently stops working.
func (w *watcher) wants(c Change) bool {
	if c.Event == eventReady || c.Event == eventDisconnected {
		return true
	}
	if len(w.boxes) > 0 {
		if c.Box == "" {
			return false // a watch scoped to Boxes is a watch on mail
		}
		matched := false
		for _, want := range w.boxes {
			matched = matched || boxIs(want, c.Box)
		}
		if !matched {
			return false
		}
	}
	if w.events == nil {
		return true
	}
	return w.events[c.Event] || (c.New && w.events[eventNew])
}

// boxIs matches a --box value against the name a Change carries, which is the
// short one a listing prints. `inbox`, `Screener` and `INBOX/Screener` all name
// the Box somebody meant.
func boxIs(want, box string) bool {
	if strings.EqualFold(want, box) {
		return true
	}
	if strings.EqualFold(want, "inbox") {
		return strings.EqualFold(box, "INBOX")
	}
	return strings.EqualFold(strings.TrimPrefix(want, "INBOX/"), box)
}

// subscribe registers one connection's channel and answers what it will report.
// An events list is checked here rather than being quietly dropped: a watch that
// reports nothing because of a typo looks exactly like a quiet mailbox.
func (d *Daemon) subscribe(req Request, ch chan Change) Response {
	resp := Response{ID: req.ID, Mirror: d.state(domainBoth)}
	w := &watcher{boxes: req.Strings("box")}
	if names := req.Strings("events"); len(names) > 0 {
		w.events = map[string]bool{}
		for _, name := range names {
			name = strings.ToLower(strings.TrimSpace(name))
			if !known(name) {
				return resp.usage("no event called " + name + "; there are " +
					strings.Join(append(append([]string{}, mailEvents...), collectionEvents...), ", "))
			}
			w.events[name] = true
		}
	}
	d.mu.Lock()
	if d.watchers == nil {
		d.watchers = map[chan Change]*watcher{}
	}
	d.watchers[ch] = w
	d.mu.Unlock()

	// A watch started against a Mirror that is already caught up is ready now;
	// one started during a cycle is ready when that cycle ends.
	d.watchReady()
	return resp.ok(map[string]any{
		"watching": true,
		"boxes":    w.boxes,
		"events":   sortedEvents(w.events),
	})
}

func known(name string) bool {
	for _, e := range mailEvents {
		if e == name {
			return true
		}
	}
	for _, e := range collectionEvents {
		if e == name {
			return true
		}
	}
	return false
}

// sortedEvents is what the watch will report, for the reply. A nil map is every
// event, and saying so as a list is more use than saying nothing.
func sortedEvents(set map[string]bool) []string {
	if set == nil {
		return append(append([]string{}, mailEvents...), collectionEvents...)
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// unwatch drops a connection's subscription. Called when the connection closes,
// whether it ever asked for one or not.
func (d *Daemon) unwatch(ch chan Change) {
	d.mu.Lock()
	delete(d.watchers, ch)
	d.mu.Unlock()
}

// watching says whether anybody is listening. Describing a change costs a read
// per Message, so nothing builds one when the answer is nobody.
func (d *Daemon) watching() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.watchers) > 0
}

// change reports one thing that moved to every watch that asked for it. A watch
// that cannot keep up misses the line, like a widget missing a Push: the feed
// must not be able to hold up a cycle.
func (d *Daemon) change(c Change) {
	if c.At == "" {
		c.At = time.Now().Format(time.RFC3339)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch, w := range d.watchers {
		if !w.wants(c) {
			continue
		}
		select {
		case ch <- c:
		default:
		}
	}
}

// watchReady says the Mirror is caught up and the feed is live, once per watch
// per catch-up. It is called at the end of every cycle, so a watch started
// during one waits for it rather than being told the previous answer.
func (d *Daemon) watchReady() {
	st := d.state(domainBoth)
	if !st.Connected || st.Syncing || st.SyncedAt == nil {
		return
	}
	d.mu.Lock()
	var chans []chan Change
	for ch, w := range d.watchers {
		if w.ready {
			continue
		}
		w.ready = true
		chans = append(chans, ch)
	}
	d.mu.Unlock()
	d.tell(chans, Change{Event: eventReady})
}

// watchDropped says a server stopped answering. The next catch-up says "ready"
// again, which is what makes the pair worth having: between them a script knows
// the feed is behind rather than quiet.
func (d *Daemon) watchDropped() {
	d.mu.Lock()
	chans := make([]chan Change, 0, len(d.watchers))
	for ch, w := range d.watchers {
		w.ready = false
		chans = append(chans, ch)
	}
	d.mu.Unlock()
	d.tell(chans, Change{Event: eventDisconnected})
}

// tell sends one line to named channels, outside the lock.
func (d *Daemon) tell(chans []chan Change, c Change) {
	if len(chans) == 0 {
		return
	}
	c.At = time.Now().Format(time.RFC3339)
	for _, ch := range chans {
		select {
		case ch <- c:
		default:
		}
	}
}

// watchMail reports what one account's cycle did, Message by Message. It is
// given the whole cycle's outcomes rather than one Box's, because a move made
// in another client is an expunge in one Box and an append in another and only
// the pair of them says it was a move.
//
// A resynced Box is one line and not a thousand: a UIDVALIDITY change replaces
// every Placement in it, and calling that a thousand arrivals would be a lie a
// script acts on. Re-read the Box.
func (d *Daemon) watchMail(a *Account, outcomes map[string]mailsync.Outcome, pickups map[int64]string) {
	if !d.watching() {
		return
	}
	moved := map[int64]bool{}
	for _, out := range outcomes {
		for _, gone := range out.Gone {
			moved[gone.MessageID] = true
		}
	}
	for folder, out := range outcomes {
		box := shortBox(folder, a.Mirrored)
		if out.Action == mailsync.ActionResync {
			d.change(Change{Event: eventResync, Account: a.Name, Box: box})
			continue
		}
		for _, delta := range out.Added {
			if code, ok := pickups[delta.MessageID]; ok {
				// Collected, not delivered. It carries the code so a script has
				// the thing itself and not a pointer to a mail that is about to
				// be binned.
				c, _ := d.mailChange(a, eventPickup, box, folder, delta.MessageID)
				c.Code = code
				d.change(c)
				continue
			}
			c, row := d.mailChange(a, eventAdded, box, folder, delta.MessageID)
			// New mail: unseen, in a Box where unread means something, and an
			// arrival rather than a move landing. Feed and Paper Trail need no
			// rule of their own — the Routing marks them read on arrival.
			c.New = !moved[delta.MessageID] && !row.Seen() && unreadMatters(box)
			d.change(c)
		}
		for _, delta := range out.Flagged {
			c, _ := d.mailChange(a, eventUpdated, box, folder, delta.MessageID)
			d.change(c)
		}
		for _, delta := range out.Gone {
			c, _ := d.mailChange(a, eventDeleted, box, folder, delta.MessageID)
			d.change(c)
		}
	}
}

// mailChange describes one Message. A Message the Mirror no longer holds is
// still reported, by id alone: something moved, and saying so late is better
// than not saying it.
func (d *Daemon) mailChange(a *Account, event, box, folder string, id int64) (Change, mirror.Row) {
	c := Change{Event: event, Account: a.Name, Box: box, Message: id}
	row, err := d.Mirror.Changed(a.Name, folder, id)
	if err != nil {
		return c, row
	}
	c.Thread = row.Message.ThreadID
	c.Subject, c.From = row.Message.Subject, row.Message.From
	return c, row
}

// unreadMatters says whether an unseen Message in this Box is mail somebody has
// not dealt with. Your own copy in Sent, a Draft, and something on its way out
// of the mailbox are unseen and are nobody's news.
func unreadMatters(box string) bool {
	switch strings.ToLower(box) {
	case "sent", "drafts", "trash", "junk":
		return false
	}
	return true
}

// watchObjects reports what one Collection's sync did. A sync from nothing is
// one line for the same reason a resynced Box is: every object in it would look
// new.
func (d *Daemon) watchObjects(name string, out davsync.Outcome) {
	if !d.watching() {
		return
	}
	if out.Full {
		d.change(Change{Event: eventCollectionResync, Account: d.Account, Collection: name})
		return
	}
	for _, o := range out.Objects {
		event := eventObjectUpdated
		switch {
		case o.Added:
			event = eventObjectAdded
		case o.Deleted:
			event = eventObjectDeleted
		}
		d.change(Change{
			Event: event, Account: d.Account, Collection: name,
			Kind: o.Object.Kind, Object: o.Object.ID, Summary: o.Object.Summary,
		})
	}
}

// watchCollections reports the calendars, task lists and address books
// themselves coming and going, by comparing what discovery found with what the
// Mirror held before it. A rename or a recolour is an update: the Collection is
// the same one, and it is named by its display name everywhere else.
func (d *Daemon) watchCollections(before, now []mirror.Collection) {
	if !d.watching() {
		return
	}
	was := make(map[string]mirror.Collection, len(before))
	for _, c := range before {
		was[c.URL] = c
	}
	for _, c := range now {
		old, held := was[c.URL]
		delete(was, c.URL)
		switch {
		case !held:
			d.change(Change{Event: eventCollectionAdded, Account: d.Account, Collection: c.Name, Kind: c.Kind})
		case old.Name != c.Name || old.Color != c.Color:
			d.change(Change{Event: eventCollectionUpdated, Account: d.Account, Collection: c.Name, Kind: c.Kind})
		}
	}
	for _, c := range was {
		d.change(Change{Event: eventCollectionDeleted, Account: d.Account, Collection: c.Name, Kind: c.Kind})
	}
}
