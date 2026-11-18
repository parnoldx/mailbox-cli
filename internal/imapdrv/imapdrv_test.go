package imapdrv

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func textPart(kind string, size uint32, disposition string) imap.BodyStructure {
	sp := &imap.BodyStructureSinglePart{
		Type: "text", Subtype: kind, Encoding: "quoted-printable",
		Params: map[string]string{"charset": "utf-8"}, Size: size,
	}
	if disposition != "" {
		sp.Extended = &imap.BodyStructureSinglePartExt{
			Disposition: &imap.BodyStructureDisposition{
				Value: disposition, Params: map[string]string{},
			},
		}
	}
	return sp
}

// A text part past maxBodyBytes is left on the server: everything downstream
// of the fetch is proportional to what arrives, so a sender who calls a
// hundred-megabyte image "text/plain" does not get a lever on the daemon's
// memory. A small part, and an oversized one beside a small one, are still
// mirrored.
func TestTextPartsSkipsWhatIsTooBigToMirror(t *testing.T) {
	big := textPart("plain", maxBodyBytes+1, "")
	ps, oversized := textParts(big)
	if len(ps) != 0 {
		t.Fatalf("%d parts of an oversized body were worth mirroring", len(ps))
	}
	if !oversized {
		t.Fatal("an oversized body was not reported as such")
	}

	small := textPart("plain", 1024, "")
	if ps, oversized = textParts(small); len(ps) != 1 || oversized {
		t.Fatalf("a small part must be mirrored: %d parts, oversized=%v", len(ps), oversized)
	}

	mixed := &imap.BodyStructureMultiPart{
		Children: []imap.BodyStructure{small, big},
	}
	if ps, oversized = textParts(mixed); len(ps) != 1 || !oversized {
		t.Fatalf("the small part beside a big one must be mirrored: %d parts, oversized=%v", len(ps), oversized)
	}

	// An attachment is skipped for what it is, not for its size, and never
	// counts as oversized: it was never text to mirror.
	if ps, oversized = textParts(textPart("plain", maxBodyBytes+1, "attachment")); len(ps) != 0 || oversized {
		t.Fatalf("an oversized attachment read as oversize text: %d parts, oversized=%v", len(ps), oversized)
	}
}
