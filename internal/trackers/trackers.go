// Package trackers names the open-tracking pixels an HTML mail carries.
//
// The stored HTML is left alone (ADR-0003): this is a projection, used to drop
// the images from Markdown and to tell a reader who is watching.
package trackers

import (
	"regexp"
	"strings"
)

// Identify returns the service that owns url, or empty when it is not a known
// tracker. Matching is a substring test against the lowercased URL, the same
// shape HEY and Simplify Gmail use.
func Identify(url string) string {
	if url == "" {
		return ""
	}
	lower := strings.ToLower(url)
	for _, s := range services {
		for _, p := range s.patterns {
			if strings.Contains(lower, p) {
				return s.name
			}
		}
	}
	return ""
}

// InHTML reports the named services whose pixels appear in html, plus
// "tracker" when a 1×1 image is not on the list. Order is first-seen.
func InHTML(html string) []string {
	if html == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, src := range imgSrcs(html) {
		if name := Identify(src); name != "" {
			add(name)
			continue
		}
		if isPixel(src, html) {
			add("tracker")
		}
	}
	return out
}

var imgRe = regexp.MustCompile(`(?is)<img\b([^>]*)>`)
var srcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
var dimRe = regexp.MustCompile(`(?i)\b(width|height)\s*=\s*["']?(0|1)(px)?["']?`)

func imgSrcs(html string) []string {
	var out []string
	for _, m := range imgRe.FindAllStringSubmatch(html, -1) {
		attr := m[1]
		sm := srcRe.FindStringSubmatch(attr)
		if sm == nil {
			continue
		}
		src := strings.Trim(sm[1], `"'`)
		out = append(out, src)
	}
	return out
}

// isPixel is the generic 1×1 heuristic, used only when Identify did not name
// a service. It looks at the tag that contains src, not at CSS.
func isPixel(src, html string) bool {
	lowerSrc := strings.ToLower(src)
	for _, m := range imgRe.FindAllStringSubmatch(html, -1) {
		attr := m[1]
		sm := srcRe.FindStringSubmatch(attr)
		if sm == nil {
			continue
		}
		if strings.ToLower(strings.Trim(sm[1], `"'`)) != lowerSrc {
			continue
		}
		w := dimRe.FindAllStringSubmatch(attr, -1)
		hasW, hasH := false, false
		for _, d := range w {
			switch strings.ToLower(d[1]) {
			case "width":
				hasW = true
			case "height":
				hasH = true
			}
		}
		if hasW && hasH {
			return true
		}
	}
	return false
}
