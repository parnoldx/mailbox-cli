.pragma library

// The callout palette, in one place. The editor's toolbar swatches, the labels
// that mark a block as a callout, and the CSS in LexxyEditor all read it from
// here.
//
// The other half of the palette is internal/htmlmd/callout.go, which draws the
// same three boxes as an HTML table in the mail that goes out. It cannot import
// this file, so the two lists are kept in step by hand, and a test in
// internal/htmlmd fails when they drift (callouts_test.go).
var kinds = [
    { kind: "note", label: "Note", bg: "#dbeafe", fg: "#1e3a8a" },
    { kind: "tip", label: "Tip", bg: "#dcfce7", fg: "#14532d" },
    { kind: "warning", label: "Warning", bg: "#fef3c7", fg: "#78350f" }
]

// labels is the same kinds as a lookup, which is how a block's first line is
// tested against them.
function labels() {
    var out = {}
    kinds.forEach(function (k) { out[k.kind] = 1 })
    return out
}
