package htmlmd

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The palette has two halves: the callouts below, which draw the box as an HTML
// table in the mail that goes out, and gui/qml/Callouts.js, which paints the
// editor and the toolbar swatches. Go cannot import the JS, so the two are kept
// in step by hand and this holds them to it: a colour that changes on one side
// only is a note that looks different in the editor from the one that arrives.
func TestTheEditorPaletteMatchesTheOneTheMailCarries(t *testing.T) {
	path := filepath.Join("..", "..", "gui", "qml", "Callouts.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the GUI is not in this checkout: %v", err)
	}
	entry := regexp.MustCompile(`\{\s*kind:\s*"(\w+)",\s*label:\s*"([^"]*)",\s*bg:\s*"(#[0-9a-fA-F]{6})",\s*fg:\s*"(#[0-9a-fA-F]{6})"\s*\}`)
	found := entry.FindAllStringSubmatch(string(raw), -1)
	if len(found) != len(callouts) {
		t.Fatalf("%s holds %d callouts, this side draws %d", path, len(found), len(callouts))
	}
	for i, m := range found {
		want := callouts[i]
		if m[1] != want.kind || m[2] != want.label || m[3] != want.bg || m[4] != want.fg {
			t.Errorf("%s entry %d is %v, this side draws %+v", path, i,
				m[1:], want)
		}
	}
}
