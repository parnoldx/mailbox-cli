package mirror

import "testing"

// Name is what the Daemon joins to the directory a caller asked to save into,
// and the filename is the sender's. It must never be more than one path
// element: a mail can name its attachment ../../.ssh/authorized_keys.
func TestAPartsDiskNameIsOnePathElement(t *testing.T) {
	for _, tc := range []struct{ filename, want string }{
		{"../../.ssh/authorized_keys", "authorized_keys"},
		{"/etc/passwd", "passwd"},
		{`..\..\evil.exe`, "evil.exe"},
		{"..", "part-1.pdf"},
		{"/", "part-1.pdf"},
		{"", "part-1.pdf"},
		{"Rechnung.pdf", "Rechnung.pdf"},
	} {
		p := Part{Path: "1", Filename: tc.filename, MIMEType: "application/pdf"}
		if got := p.Name(); got != tc.want {
			t.Errorf("Name(%q) = %q, want %q", tc.filename, got, tc.want)
		}
	}
}

// The fallback name comes from the MIME type, which is the server's too: a type
// that carries a separator must not reintroduce one.
func TestAFallbackPartNameTakesNoSeparatorsFromItsType(t *testing.T) {
	p := Part{Path: "2.1", MIMEType: "text/../../evil"}
	if got := p.Name(); got != "part-2-1.evil" {
		t.Fatalf("Name() = %q", got)
	}
}
