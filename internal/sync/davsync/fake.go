package davsync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Fake is a DAV server a test can drive through the states a real one only
// reaches by accident: a token it has forgotten, a change reported without its
// data, a collection that disappears.
type Fake struct {
	mu          sync.Mutex
	collections []Collection
	objects     map[string]map[string]*fakeObject // url -> href -> object
	// version counts every change, and a token is a version. That is what a
	// real sync token is: an opaque "everything up to here".
	version int
	// Expire makes the next Sync reject the token it is given, the way a server
	// does when its change log has rolled over.
	Expire bool
	// Detached makes the server report changes without their data, so the
	// reconciler has to ask for them separately.
	Detached bool
	// SilentPut makes a write return no ETag, like a server that rewrites what
	// it is given and does not say what it ended up with.
	SilentPut bool
	// PutErr makes the next write fail, once.
	PutErr error
	// Calls records operations in order.
	Calls []string
}

type fakeObject struct {
	etag    string
	data    string
	version int
	deleted bool
}

// NewFake returns a server with one calendar.
func NewFake(name, url string) *Fake {
	return &Fake{
		collections: []Collection{{Kind: "events", URL: url, Name: name, Color: "#3355ff"}},
		objects:     map[string]map[string]*fakeObject{url: {}},
	}
}

// AddCollection adds another collection.
func (f *Fake) AddCollection(c Collection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collections = append(f.collections, c)
	f.objects[c.URL] = map[string]*fakeObject{}
}

// RemoveCollection takes one away, the way deleting a calendar in webmail does.
func (f *Fake) RemoveCollection(url string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keep []Collection
	for _, c := range f.collections {
		if c.URL != url {
			keep = append(keep, c)
		}
	}
	f.collections = keep
	delete(f.objects, url)
}

// Deliver creates or replaces an object, as another client would. This is the
// fixture verb; Put is the WriteDriver method this program writes through.
func (f *Fake) Deliver(url, href, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	f.objects[url][href] = &fakeObject{
		etag: fmt.Sprintf(`"%d"`, f.version), data: data, version: f.version,
	}
}

// Remove takes an object away, as another client would. This is the fixture
// verb; Delete is the WriteDriver method this program writes through.
func (f *Fake) Remove(url, href string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	if o, ok := f.objects[url][href]; ok {
		o.deleted, o.version, o.data = true, f.version, ""
	}
}

// Collections implements Driver.
func (f *Fake) Collections(ctx context.Context) ([]Collection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "Collections")
	return append([]Collection(nil), f.collections...), nil
}

// Sync implements Driver. An empty token returns everything that still exists;
// a token returns what has changed since, deletions included.
func (f *Fake) Sync(ctx context.Context, url, token string) (Changes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "Sync")
	if token != "" && f.Expire {
		f.Expire = false
		return Changes{}, ErrTokenExpired
	}
	objects, ok := f.objects[url]
	if !ok {
		return Changes{}, fmt.Errorf("no such collection %q", url)
	}
	since := 0
	if token != "" {
		if _, err := fmt.Sscanf(token, "v%d", &since); err != nil {
			return Changes{}, ErrTokenExpired
		}
	}
	var hrefs []string
	for href := range objects {
		hrefs = append(hrefs, href)
	}
	sort.Strings(hrefs)

	out := Changes{Token: fmt.Sprintf("v%d", f.version)}
	for _, href := range hrefs {
		o := objects[href]
		if o.version <= since {
			continue
		}
		if o.deleted {
			if since == 0 {
				continue // never seen it, so it is not news that it is gone
			}
			out.Items = append(out.Items, Change{Href: href, Deleted: true})
			continue
		}
		it := Change{Href: href, ETag: o.etag}
		if !f.Detached {
			it.Data = o.data
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

// MultiGet implements Driver.
func (f *Fake) MultiGet(ctx context.Context, url string, hrefs []string) ([]Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "MultiGet")
	var out []Change
	for _, href := range hrefs {
		o, ok := f.objects[url][href]
		if !ok || o.deleted {
			continue
		}
		out = append(out, Change{Href: href, ETag: o.etag, Data: o.data})
	}
	return out, nil
}

// Put implements WriteDriver. It checks If-Match, because a server that does
// not is a server where two clients silently overwrite each other — and the
// reason the write path carries an ETag at all.
func (f *Fake) Put(ctx context.Context, href, data, ifMatch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "Put")
	if f.PutErr != nil {
		err := f.PutErr
		f.PutErr = nil
		return "", err
	}
	url, key, err := f.locate(href)
	if err != nil {
		return "", err
	}
	existing, found := f.objects[url][key]
	if ifMatch != "" && ifMatch != "*" {
		if !found || existing.etag != ifMatch {
			return "", fmt.Errorf("precondition failed: %s has changed", href)
		}
	}
	f.version++
	etag := fmt.Sprintf(`"%d"`, f.version)
	f.objects[url][key] = &fakeObject{etag: etag, data: data, version: f.version}
	if f.SilentPut {
		return "", nil // a server that does not say what the object now is
	}
	return etag, nil
}

// Delete implements WriteDriver.
func (f *Fake) Delete(ctx context.Context, href, ifMatch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "Delete")
	url, key, err := f.locate(href)
	if err != nil {
		return err
	}
	o, found := f.objects[url][key]
	if !found {
		return nil // already gone is the state that was asked for
	}
	if ifMatch != "" && ifMatch != "*" && o.etag != ifMatch {
		return fmt.Errorf("precondition failed: %s has changed", href)
	}
	f.version++
	o.deleted, o.version, o.data = true, f.version, ""
	return nil
}

// locate finds which collection an href belongs to, by the same rule a server
// does: the collection whose path it sits under.
func (f *Fake) locate(href string) (url, key string, err error) {
	path := href
	if i := strings.Index(href, "://"); i >= 0 {
		if j := strings.Index(href[i+3:], "/"); j >= 0 {
			path = href[i+3+j:]
		}
	}
	for colURL := range f.objects {
		base := colURL
		if i := strings.Index(base, "://"); i >= 0 {
			if j := strings.Index(base[i+3:], "/"); j >= 0 {
				base = base[i+3+j:]
			}
		}
		base = strings.TrimSuffix(base, "/")
		if strings.HasPrefix(path, base+"/") {
			return colURL, path, nil
		}
	}
	return "", "", fmt.Errorf("no collection holds %s", href)
}

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
