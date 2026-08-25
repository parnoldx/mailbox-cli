// Package vobject: shared vCard / iCalendar line grammar (unfold, params, escaping).
package vobject

import (
	"regexp"
	"strings"
)

type Prop struct {
	Name   string
	Params string
	Value  string
	Group  string
}

func (p Prop) Line() string {
	prefix := ""
	if p.Group != "" {
		prefix = p.Group + "."
	}
	value := p.Value
	if p.Name != "N" {
		value = Escape(p.Value)
	}
	return prefix + p.Name + p.Params + ":" + value
}

func Unescape(value string) string {
	var out strings.Builder
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			switch next {
			case 'n', 'N':
				out.WriteRune('\n')
			default:
				out.WriteRune(next)
			}
			i++
			continue
		}
		out.WriteRune(runes[i])
	}
	return out.String()
}

func Escape(value string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		";", "\\;",
		",", "\\,",
	)
	return r.Replace(value)
}

var foldRe = regexp.MustCompile(`\n[ \t]`)

func Unfold(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return foldRe.ReplaceAllString(text, "")
}

func ParseLines(text string) []Prop {
	var props []Prop
	for _, line := range strings.Split(Unfold(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		prop, ok := ParseLine(line)
		if ok {
			props = append(props, prop)
		}
	}
	return props
}

func ParseLine(line string) (Prop, bool) {
	i := strings.Index(line, ":")
	if i < 0 {
		return Prop{}, false
	}
	meta, raw := line[:i], line[i+1:]
	group := ""
	namePart := meta
	headEnd := strings.Index(meta, ";")
	head := meta
	if headEnd >= 0 {
		head = meta[:headEnd]
	}
	if dot := strings.Index(head, "."); dot > 0 {
		group = head[:dot]
		namePart = meta[dot+1:]
	}
	var name, params string
	if semi := strings.Index(namePart, ";"); semi >= 0 {
		name = namePart[:semi]
		params = ";" + namePart[semi+1:]
	} else {
		name = namePart
	}
	if name == "" {
		return Prop{}, false
	}
	return Prop{Name: strings.ToUpper(name), Params: params, Value: Unescape(raw), Group: group}, true
}

func Serialize(props []Prop) string {
	lines := make([]string, len(props))
	for i, p := range props {
		lines[i] = p.Line()
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

func First(props []Prop, name string) string {
	for _, p := range props {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

// Component returns the props of the first NAME block (e.g. VEVENT).
func Component(text, name string) []Prop {
	props := ParseLines(text)
	var out []Prop
	in := false
	for _, p := range props {
		switch {
		case !in && p.Name == "BEGIN" && strings.EqualFold(p.Value, name):
			in = true
		case in && p.Name == "END" && strings.EqualFold(p.Value, name):
			return out
		case in:
			out = append(out, p)
		}
	}
	return out
}
