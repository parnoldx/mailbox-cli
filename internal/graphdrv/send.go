package graphdrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/mail"
	"strings"

	"mailbox/internal/outbox"
)

// Send implements outbox.Transport through Graph's sendMail, which takes the
// MIME the Outbox already holds, so composing does not change. Graph files the
// sent copy in Sent Items itself, so the Courier for this account has no Filer.
//
// Graph reads the recipients out of the headers, not an envelope: a Bcc
// recipient, whom the composed mail rightly does not name, would be dropped. So
// every recipient the headers do not carry goes back in as a Bcc header, which
// Exchange strips before delivery.
func (m *Mail) Send(ctx context.Context, from string, to []string, raw []byte) error {
	raw = withBcc(raw, to)
	return m.c.do(ctx, request{method: http.MethodPost, path: "/me/sendMail",
		body: []byte(base64.StdEncoding.EncodeToString(raw)), contentType: "text/plain"}, nil)
}

func withBcc(raw []byte, to []string) []byte {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	named := map[string]bool{}
	for _, h := range []string{"To", "Cc", "Bcc"} {
		list, _ := msg.Header.AddressList(h)
		for _, a := range list {
			named[strings.ToLower(a.Address)] = true
		}
	}
	var hidden []string
	for _, addr := range to {
		if !named[strings.ToLower(addr)] {
			hidden = append(hidden, addr)
		}
	}
	if len(hidden) == 0 {
		return raw
	}
	return append([]byte("Bcc: "+strings.Join(hidden, ", ")+"\r\n"), raw...)
}

var _ outbox.Transport = (*Mail)(nil)
