package terminal

import (
	"regexp"
	"strings"
	"unicode"
)

var ansiRe = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|[\\]PX^].*?(?:\x07|\x1b\\\\)|[@-Z\\\\-_])")

func inRanges(r rune, ranges []rune) bool {
	for i := 0; i+1 < len(ranges); i += 2 {
		if r >= ranges[i] && r <= ranges[i+1] {
			return true
		}
	}
	return false
}

var bidi = []rune{
	0x061C, 0x061C,
	0x200E, 0x200F,
	0x202A, 0x202E,
	0x2066, 0x2069,
}

var invisible = []rune{
	0x00AD, 0x00AD,
	0x034F, 0x034F,
	0x180E, 0x180E,
	0x200B, 0x200B,
	0xFEFF, 0xFEFF,
	0x2060, 0x2064,
	0x206A, 0x206F,
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F)
}

const zwj = '‍'
const zwnj = '‌'

const maxMarks = 8

func SanitizeLine(text string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(SanitizeText(text))
}

type walkState struct {
	src      []rune
	out      []rune
	diverged bool
	base     rune
	hasBase  bool
	marks    int
	joiner   rune
	joinerAt int
	hasJoinr bool
}

func (w *walkState) write(ch rune) {
	if w.diverged {
		w.out = append(w.out, ch)
	}
}

func (w *walkState) diverge(i int) {
	if !w.diverged {
		w.out = append([]rune{}, w.src[:i]...)
		w.diverged = true
	}
}

func (w *walkState) keep(ch rune) {
	if w.hasJoinr {
		if isBase(w.base, w.hasBase) {
			w.write(w.joiner)
		} else {
			w.diverge(w.joinerAt)
		}
		w.hasJoinr = false
	}
	w.write(ch)
	if !isCombiningMark(ch) {
		w.base = ch
		w.hasBase = true
	}
}

func (w *walkState) drop(i int) {
	if w.hasJoinr {
		i = w.joinerAt
	}
	w.diverge(i)
}

func (w *walkState) hold(i int, ch rune) {
	if w.hasJoinr || !isBase(w.base, w.hasBase) {
		w.drop(i)
	} else {
		w.joiner = ch
		w.joinerAt = i
		w.hasJoinr = true
	}
}

func (w *walkState) finish() string {
	if w.hasJoinr {
		w.hasJoinr = false
		w.diverge(w.joinerAt)
	}
	if !w.diverged {
		return string(w.src)
	}
	return string(w.out)
}

func isBase(ch rune, has bool) bool {
	if !has {
		return false
	}
	if ch < 0x80 || unicode.IsSpace(ch) || isCombiningMark(ch) ||
		unicode.Is(unicode.Cf, ch) || unicode.Is(unicode.P, ch) {
		return false
	}
	return true
}

func isCombiningMark(ch rune) bool {
	if ch < 0x300 {
		return false
	}
	return unicode.Is(unicode.Mn, ch) || unicode.Is(unicode.Me, ch) || unicode.Is(unicode.Mc, ch)
}

func SanitizeText(text string) string {
	stripped := ansiRe.ReplaceAllString(text, "")
	src := []rune(stripped)
	w := &walkState{src: src}
	for i, ch := range src {
		o := ch
		switch {
		case ch == '\n' || ch == '\t':
			w.keep(ch)
			w.marks = 0
		case isControl(o) || inRanges(o, bidi) || inRanges(o, invisible):
			w.drop(i)
		case ch == zwj || ch == zwnj:
			w.hold(i, ch)
		case isCombiningMark(ch):
			if w.marks < maxMarks {
				w.keep(ch)
				w.marks++
			} else {
				w.drop(i)
			}
		default:
			w.keep(ch)
			if !unicode.Is(unicode.Cf, ch) {
				w.marks = 0
			}
		}
	}
	return w.finish()
}
