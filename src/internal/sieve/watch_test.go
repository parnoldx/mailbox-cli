package sieve

import (
	"errors"
	"testing"
)

type fakeMailbox struct {
	folders   map[string]bool
	messages  map[string]map[string]string // folder -> uid -> from
	created   []string
	deleted   []string // "folder:uid" seen+deleted
	failReads bool
}

func newFakeMailbox() *fakeMailbox {
	return &fakeMailbox{
		folders:  map[string]bool{},
		messages: map[string]map[string]string{},
	}
}

func (f *fakeMailbox) ListFolders() ([]string, error) {
	var out []string
	for name := range f.folders {
		out = append(out, name)
	}
	return out, nil
}

func (f *fakeMailbox) CreateFolder(name string) error {
	f.created = append(f.created, name)
	f.folders[name] = true
	return nil
}

func (f *fakeMailbox) UIDSenders(folder string) (map[string]string, error) {
	if f.failReads {
		return nil, errors.New("connection lost")
	}
	msgs, ok := f.messages[folder]
	if !ok {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(msgs))
	for k, v := range msgs {
		out[k] = v
	}
	return out, nil
}

func (f *fakeMailbox) SeenAndDelete(folder, uid string) error {
	f.deleted = append(f.deleted, folder+":"+uid)
	delete(f.messages[folder], uid)
	return nil
}

func TestEnsureFoldersCreatesMissing(t *testing.T) {
	f := newFakeMailbox()
	w := &Watcher{MB: f}
	if err := w.EnsureFolders(); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != len(RuleFolders) {
		t.Errorf("created %v, want all %d folders", f.created, len(RuleFolders))
	}
	// Second run creates nothing.
	if err := w.EnsureFolders(); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != len(RuleFolders) {
		t.Errorf("created again: %v", f.created[len(f.created)-len(RuleFolders):])
	}
}

func TestPollDetectsArrivals(t *testing.T) {
	f := newFakeMailbox()
	f.messages = map[string]map[string]string{
		FolderInbox: {"1": "a@x.com"},
		FolderFeed:  {},
	}
	w := &Watcher{MB: f}
	if err := w.Snapshot(); err != nil {
		t.Fatal(err)
	}

	// No change -> no movements.
	movements, err := w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 0 {
		t.Fatalf("initial poll reported %+v", movements)
	}

	// New arrivals in Feed and Inbox.
	f.messages[FolderFeed]["2"] = "b@x.com"
	f.messages[FolderInbox]["3"] = "c@x.com"
	movements, err = w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, mv := range movements {
		got[mv.Folder] = mv.Address
	}
	if got[FolderFeed] != "b@x.com" || got[FolderInbox] != "c@x.com" {
		t.Fatalf("movements: %+v", movements)
	}

	// Same message again -> no duplicate.
	movements, _ = w.Poll()
	if len(movements) != 0 {
		t.Fatalf("duplicate movements: %+v", movements)
	}
}

func TestPollBlockBlacklistsAndDeletes(t *testing.T) {
	f := newFakeMailbox()
	f.messages = map[string]map[string]string{
		FolderBlock: {},
	}
	w := &Watcher{MB: f}
	if err := w.Snapshot(); err != nil {
		t.Fatal(err)
	}

	// Message lands in Block after startup.
	f.messages[FolderBlock]["7"] = "bad@x.com"
	movements, err := w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 1 || movements[0].Address != "bad@x.com" || movements[0].Folder != FolderBlock {
		t.Fatalf("movements: %+v", movements)
	}
	if len(f.deleted) != 1 || f.deleted[0] != FolderBlock+":7" {
		t.Fatalf("deleted: %v", f.deleted)
	}

	// Deleted message is gone; next poll reports nothing.
	movements, _ = w.Poll()
	if len(movements) != 0 {
		t.Fatalf("movements after delete: %+v", movements)
	}
}

func TestPollErrorPropagates(t *testing.T) {
	f := newFakeMailbox()
	w := &Watcher{MB: f}
	if err := w.Snapshot(); err != nil {
		t.Fatal(err)
	}
	f.failReads = true
	if _, err := w.Poll(); err == nil {
		t.Fatal("want error on failed read")
	}
}
