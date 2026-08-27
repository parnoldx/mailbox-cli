package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// ANSI-16 so a terminal retint restyles the TUI. Same palette as hey-cli.
var (
	colorPrimary  color.Color = lipgloss.BrightBlue
	colorBright   color.Color = lipgloss.BrightWhite
	colorAlert    color.Color = lipgloss.Red
	colorPositive color.Color = lipgloss.Green
	colorLink     color.Color = lipgloss.BrightCyan
	colorError    color.Color = lipgloss.Red
	colorChrome   color.Color = lipgloss.Blue
	colorActive   color.Color = lipgloss.Yellow
	colorOnAccent color.Color = lipgloss.Black
)

var styleMuted = lipgloss.NewStyle().Faint(true)

func applyTheme() {
	if os.Getenv("NO_COLOR") != "" {
		nc := lipgloss.NoColor{}
		colorPrimary, colorBright, colorLink, colorError = nc, nc, nc, nc
		colorAlert, colorPositive, colorChrome, colorActive, colorOnAccent = nc, nc, nc, nc, nc
	}
}

type styles struct {
	title     lipgloss.Style
	pill      lipgloss.Style
	entryFrom lipgloss.Style
	entryDate lipgloss.Style
	entryBody lipgloss.Style
	separator lipgloss.Style
	helpKey   lipgloss.Style
	helpDesc  lipgloss.Style
	helpSep   lipgloss.Style
}

func newStyles() styles {
	return styles{
		title:     lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
		pill:      lipgloss.NewStyle().Foreground(colorOnAccent).Background(colorPrimary).Bold(true).Padding(0, 1),
		entryFrom: lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
		entryDate: styleMuted,
		entryBody: lipgloss.NewStyle(),
		separator: lipgloss.NewStyle().Foreground(colorChrome),
		helpKey:   lipgloss.NewStyle().Foreground(colorChrome).Bold(true),
		helpDesc:  lipgloss.NewStyle().Foreground(colorChrome),
		helpSep:   lipgloss.NewStyle().Foreground(colorChrome),
	}
}

func cursorStyles() (marker, text lipgloss.Style) {
	marker = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	return marker, marker
}

func errorView(errMsg string, width int) string {
	border := lipgloss.NewStyle().Foreground(colorError)
	errStyle := lipgloss.NewStyle().Foreground(colorError).Bold(true)
	maxInner := min(width-4, 60)
	if maxInner <= 0 {
		return errStyle.Render("Error: " + errMsg)
	}
	lines := wrapText(errMsg, maxInner)
	innerWidth := 0
	for _, l := range lines {
		if len(l) > innerWidth {
			innerWidth = len(l)
		}
	}
	top := border.Render("╭─ Error " + strings.Repeat("─", max(innerWidth-6, 0)) + "╮")
	bot := border.Render("╰" + strings.Repeat("─", innerWidth+2) + "╯")
	var mid strings.Builder
	for _, l := range lines {
		pad := strings.Repeat(" ", innerWidth-len(l))
		mid.WriteString(border.Render("│") + " " + errStyle.Render(l) + pad + " " + border.Render("│") + "\n")
	}
	hint := styleMuted.Render("  Press ctrl+c ctrl+c to quit")
	return top + "\n" + mid.String() + bot + "\n\n" + hint
}

func wrapText(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > maxWidth {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	return append(lines, line)
}

func errorNotice(what string, err error) string {
	return what + ": " + err.Error()
}

var heyColors = map[string]color.Color{
	"blue":   lipgloss.Blue,
	"red":    lipgloss.Red,
	"gold":   lipgloss.BrightYellow,
	"green":  lipgloss.Green,
	"teal":   lipgloss.Cyan,
	"purple": lipgloss.Magenta,
	"pink":   lipgloss.BrightMagenta,
	"brown":  lipgloss.Yellow,
	"black":  lipgloss.White,
}

func habitMarkerStyle(habitColor string) lipgloss.Style {
	if slot, ok := heyColors[habitColor]; ok {
		return lipgloss.NewStyle().Foreground(slot).Bold(true)
	}
	if habitColor == "" {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(colorAlert).Bold(true)
}

func habitMarker(done bool) string {
	if done {
		return "●"
	}
	return "○"
}

func habitEmoji(icon string) string {
	return strings.TrimSpace(icon)
}

func habitLabel(habit Recording) string {
	name := habit.Title
	if emoji := habitEmoji(habit.Icon); emoji != "" {
		return emoji + " " + name
	}
	return name
}
