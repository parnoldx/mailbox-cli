package imaputf7

import (
	"strings"
	"testing"
)

func TestASCIIRoundtrip(t *testing.T) {
	if Encode("INBOX/Paper Trail") != "INBOX/Paper Trail" {
		t.Fatal("encode ascii")
	}
	if Decode("INBOX/Paper Trail") != "INBOX/Paper Trail" {
		t.Fatal("decode ascii")
	}
}

func TestQuoteSpaces(t *testing.T) {
	if Quote("INBOX/Paper Trail") != `"INBOX/Paper Trail"` {
		t.Fatalf("got %s", Quote("INBOX/Paper Trail"))
	}
}

func TestUmlaut(t *testing.T) {
	if !strings.Contains(Decode(Encode("Homöopathie")), "ö") {
		t.Fatal("umlaut roundtrip failed")
	}
}
