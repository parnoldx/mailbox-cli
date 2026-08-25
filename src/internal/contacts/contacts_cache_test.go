package contacts

import (
	"path/filepath"
	"testing"

	"mailbox/src/internal/vobject"
)

func TestCacheRoundTrip(t *testing.T) {
	oldPath := cachePathFunc
	defer func() { cachePathFunc = oldPath }()
	dir := t.TempDir()
	cachePathFunc = func() (string, error) { return filepath.Join(dir, "c.json"), nil }

	want := []entry{{
		href:  "/a.vcf",
		etag:  "e1",
		props: []vobject.Prop{{Name: "UID", Value: "u1"}, {Name: "FN", Value: "A B"}},
	}}
	saveCache(want)

	got, ok := loadCache()
	if !ok || len(got) != 1 || got[0].href != want[0].href ||
		got[0].etag != want[0].etag || len(got[0].props) != 2 ||
		got[0].props[0].Name != "UID" {
		t.Fatalf("round trip mismatch: ok=%v got=%+v", ok, got)
	}
}
