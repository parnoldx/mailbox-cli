package mail

import (
	"encoding/base64"
	"testing"
)

func TestDecodeCTEPaddedBase64(t *testing.T) {
	raw := []byte("%PDF-1.7") // 8 bytes → StdEncoding pads with =
	enc := base64.StdEncoding.EncodeToString(raw)
	if enc[len(enc)-1] != '=' {
		t.Fatalf("fixture not padded: %q", enc)
	}
	got := decodeCTE("base64", []byte(enc+"\n"))
	if string(got) != string(raw) {
		t.Fatalf("padded base64: got %q want %q", got, raw)
	}
}

func TestDecodeCTEUnpaddedBase64(t *testing.T) {
	raw := []byte("hello")
	enc := base64.RawStdEncoding.EncodeToString(raw)
	got := decodeCTE("base64", []byte(enc))
	if string(got) != string(raw) {
		t.Fatalf("unpadded base64: got %q want %q", got, raw)
	}
}

