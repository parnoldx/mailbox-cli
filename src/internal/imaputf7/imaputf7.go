// IMAP modified UTF-7 (RFC 3501).
package imaputf7

import (
	"encoding/base64"
	"encoding/binary"
	"regexp"
	"strings"
	"unicode/utf16"
)

func Encode(name string) string {
	var out strings.Builder
	var buf []rune

	flush := func() {
		if len(buf) == 0 {
			return
		}
		u16 := utf16.Encode(buf)
		raw := make([]byte, 2*len(u16))
		for i, v := range u16 {
			binary.BigEndian.PutUint16(raw[2*i:], v)
		}
		enc := base64.StdEncoding.EncodeToString(raw)
		enc = strings.TrimRight(enc, "=")
		enc = strings.ReplaceAll(enc, "/", ",")
		out.WriteString("&" + enc + "-")
		buf = buf[:0]
	}

	for _, ch := range name {
		if ch >= 0x20 && ch <= 0x7E {
			flush()
			if ch == '&' {
				out.WriteString("&-")
			} else {
				out.WriteRune(ch)
			}
		} else {
			buf = append(buf, ch)
		}
	}
	flush()
	return out.String()
}

var chunkRe = regexp.MustCompile(`&([^-]*)-`)

func Decode(name string) string {
	bad := false
	result := chunkRe.ReplaceAllStringFunc(name, func(m string) string {
		sub := chunkRe.FindStringSubmatch(m)
		chunk := sub[1]
		if chunk == "" {
			return "&"
		}
		pad := (4 - len(chunk)%4) % 4
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(chunk, ",", "/") + strings.Repeat("=", pad))
		if err != nil {
			bad = true
			return m
		}
		u16 := make([]uint16, 0, len(raw)/2)
		for i := 0; i+1 < len(raw); i += 2 {
			u16 = append(u16, binary.BigEndian.Uint16(raw[i:]))
		}
		return string(utf16.Decode(u16))
	})
	if bad {
		return name
	}
	return result
}

func Quote(name string) string {
	encoded := Encode(name)
	escaped := strings.ReplaceAll(encoded, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
