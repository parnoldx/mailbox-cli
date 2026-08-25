package mail

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mailbox/src/internal/config"
)

func fakeAccount() *config.Account {
	return &config.Account{
		Email: "user@mailbox.org", Password: "x",
		IMAPHost: "localhost", IMAPPort: 993,
		DisplayName: "Test User", SMTPHost: "localhost", SMTPPort: 465,
	}
}

func TestBuildOutgoingPlain(t *testing.T) {
	m := &Mail{Acct: fakeAccount()}
	raw, msgid, err := m.BuildOutgoing(&Outgoing{
		To: []string{"dest@example.com"}, Subject: "Hi", Body: "line one\nline two",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"From: Test User <user@mailbox.org>",
		"To: dest@example.com",
		"Subject: Hi",
		"Message-ID: " + msgid,
		"text/plain; charset=utf-8",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q missing from:\n%s", want, text)
		}
	}
	parsed := ParseMessage(raw)
	var plainPart *Part
	var walk func(p *Part)
	walk = func(p *Part) {
		if plainPart != nil {
			return
		}
		if p.CType == "text/plain" {
			plainPart = p
			return
		}
		for _, c := range p.Children {
			walk(c)
		}
	}
	walk(parsed)
	if plainPart == nil {
		t.Fatal("no text/plain part")
	}
	if got := plainPart.DecodeText(); strings.TrimSpace(got) != "line one\nline two" {
		t.Fatalf("body %q", got)
	}
}

func TestBuildOutgoingHTMLAndAttachments(t *testing.T) {
	m := &Mail{Acct: fakeAccount()}
	raw, _, err := m.BuildOutgoing(&Outgoing{
		To:      []string{"d@e.com"},
		Subject: "S",
		Body:    "**bold**",
		Attachments: []OutAttachment{
			{Name: "a.txt", Data: []byte("hello"), ContentType: "text/plain"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "multipart/mixed") || !strings.Contains(text, "filename=\"a.txt\"") {
		t.Fatalf("missing structure:\n%s", text)
	}
	if !strings.Contains(text, "<strong>bold</strong>") {
		t.Fatalf("html part missing:\n%s", text)
	}
}

func TestStripBccAndRecipients(t *testing.T) {
	m := &Mail{Acct: fakeAccount()}
	raw, _, err := m.BuildOutgoing(&Outgoing{
		To: []string{"to@e.com"}, Cc: []string{"cc@e.com"},
		Bcc: []string{"bcc@e.com", "two@e.com"}, Subject: "s", Body: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if headerValueOf(raw, "Bcc") == "" {
		t.Fatal("draft should keep Bcc header")
	}
	wire := stripBcc(raw)
	if headerValueOf(wire, "Bcc") != "" {
		t.Fatal("wire copy must drop Bcc")
	}
	rcpts := recipientsOf(raw, nil)
	want := map[string]bool{"to@e.com": true, "cc@e.com": true, "bcc@e.com": true, "two@e.com": true}
	if len(rcpts) != len(want) {
		t.Fatalf("rcpts %v", rcpts)
	}
	for _, r := range rcpts {
		if !want[r] {
			t.Fatalf("unexpected rcpt %q", r)
		}
	}
}

func TestReplySubjectGetsRePrefix(t *testing.T) {
	orig := &ThreadMessage{
		Folder: "INBOX", UID: "1",
		From: "a@b.c", MessageID: "<m@x>", References: "",
		Subject: "Hello",
	}
	m := &Mail{Acct: fakeAccount()}
	m.FullHook = func(folder, uid string) (*ThreadMessage, error) { return orig, nil }
	raw, _, err := m.BuildOutgoing(&Outgoing{ReplyTo: &[2]string{"INBOX", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if headerValueOf(raw, "Subject") != "Re: Hello" {
		t.Fatalf("subject %q", headerValueOf(raw, "Subject"))
	}
	if headerValueOf(raw, "In-Reply-To") != "<m@x>" {
		t.Fatal("in-reply-to")
	}
	if headerValueOf(raw, "References") != "<m@x>" {
		t.Fatal("references")
	}
}

func TestUploadToTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/report.pdf" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.Copy(io.Discard, r.Body)
		w.Write([]byte("https://transfer.example/abc/report.pdf\n"))
	}))
	defer srv.Close()

	old := transferURL
	transferURL = srv.URL + "/"
	defer func() { transferURL = old }()

	url, err := UploadToTransfer("report.pdf", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	want := "https://transfer.example/abc/report.pdf"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}

func TestUploadToTransferError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too big", http.StatusRequestEntityTooLarge)
	}))
	defer srv.Close()
	old := transferURL
	transferURL = srv.URL + "/"
	defer func() { transferURL = old }()

	if _, err := UploadToTransfer("x.bin", []byte("d")); err == nil {
		t.Fatal("want error on non-200")
	}
}
