package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type habitFormMode int

const (
	habitFormCreate habitFormMode = iota
	habitFormEdit
)

const (
	habitFieldName = iota
	habitFieldIcon
	habitFieldColor
	habitFieldDays
	habitFieldCount
)

var habitDayKeys = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
var habitDayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var habitColorNames = []string{"blue", "red", "gold", "green", "teal", "purple", "pink", "brown"}
var habitIcons = []string{"💪", "🏃", "📚", "🧘", "💧", "🎵", "✍️", "😴", "🥗", "☀️", "⭐", "🔥"}

type habitForm struct {
	mode      habitFormMode
	id        string
	name      string
	icon      int
	color     int
	days      [7]bool
	dayCursor int
	focus     int
	status    string
}

func newHabitForm(mode habitFormMode, rec Recording) *habitForm {
	f := &habitForm{mode: mode, id: rec.ID}
	if mode == habitFormCreate {
		f.days = [7]bool{true, true, true, true, true, true, true}
		return f
	}
	f.name = rec.Title
	f.icon = indexOfHabitIcon(rec.Icon)
	f.color = indexOfHabitColor(rec.Color)
	f.days = parseHabitDays(rec.Days)
	return f
}

func indexOfHabitIcon(icon string) int {
	icon = strings.TrimSpace(icon)
	for i, e := range habitIcons {
		if e == icon {
			return i
		}
	}
	return 0
}

func indexOfHabitColor(name string) int {
	for i, c := range habitColorNames {
		if c == name {
			return i
		}
	}
	return 0
}

func parseHabitDays(s string) [7]bool {
	var days [7]bool
	if strings.TrimSpace(s) == "" {
		for i := range days {
			days[i] = true
		}
		return days
	}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		for i, key := range habitDayKeys {
			if p == key {
				days[i] = true
			}
		}
	}
	return days
}

func wrapIndex(index, count int, msg tea.KeyPressMsg) int {
	switch msg.Key().Code {
	case tea.KeyLeft, tea.KeyUp:
		return (index + count - 1) % count
	case tea.KeyRight, tea.KeyDown:
		return (index + 1) % count
	}
	return index
}

func (f *habitForm) daysCSV() string {
	var parts []string
	for i, on := range f.days {
		if on {
			parts = append(parts, habitDayKeys[i])
		}
	}
	return strings.Join(parts, ",")
}

func (f *habitForm) validate() string {
	if strings.TrimSpace(f.name) == "" {
		return "Name is required"
	}
	if f.daysCSV() == "" {
		return "Pick at least one day"
	}
	return ""
}

func (f *habitForm) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case msg.Key().Code == tea.KeyTab && msg.Key().Mod == tea.ModShift:
		f.focus = (f.focus + habitFieldCount - 1) % habitFieldCount
		return nil, false
	case msg.Key().Code == tea.KeyTab || msg.Key().Code == tea.KeyEnter:
		f.focus = (f.focus + 1) % habitFieldCount
		return nil, false
	case msg.String() == "ctrl+s":
		if problem := f.validate(); problem != "" {
			f.status = problem
			return nil, false
		}
		return nil, true
	case msg.Key().Code == tea.KeyBackspace:
		if f.focus == habitFieldName && f.name != "" {
			r := []rune(f.name)
			f.name = string(r[:len(r)-1])
		}
		return nil, false
	}
	if f.focus == habitFieldName {
		f.name += insertText(msg)
		return nil, false
	}
	f.choose(msg)
	return nil, false
}

func (f *habitForm) choose(msg tea.KeyPressMsg) {
	switch f.focus {
	case habitFieldIcon:
		f.icon = wrapIndex(f.icon, len(habitIcons), msg)
	case habitFieldColor:
		f.color = wrapIndex(f.color, len(habitColorNames), msg)
	case habitFieldDays:
		switch msg.String() {
		case " ", "space":
			f.days[f.dayCursor] = !f.days[f.dayCursor]
		default:
			f.dayCursor = wrapIndex(f.dayCursor, len(f.days), msg)
		}
	}
}

func (f *habitForm) helpBindings() []helpBinding {
	bindings := []helpBinding{{"tab", "next field"}}
	switch f.focus {
	case habitFieldIcon, habitFieldColor:
		bindings = append(bindings, helpBinding{"←→", "choose"})
	case habitFieldDays:
		bindings = append(bindings, helpBinding{"←→", "day"}, helpBinding{"space", "toggle"})
	}
	return append(bindings, helpBinding{"ctrl+s", "save"}, helpBinding{"esc", "cancel"})
}

func (f *habitForm) title() string {
	if f.mode == habitFormEdit {
		return "Edit habit"
	}
	return "Create habit"
}

func (f *habitForm) view() string {
	var b strings.Builder
	name := f.name
	if f.focus == habitFieldName {
		name += "█"
	}
	f.writeField(&b, "Name", habitFieldName, name)
	f.writeField(&b, "Icon", habitFieldIcon, f.iconField())
	f.writeField(&b, "Color", habitFieldColor, f.colorField())
	f.writeField(&b, "Days", habitFieldDays, f.daysField())
	if f.status != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(colorError).Render(f.status))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (f *habitForm) writeField(b *strings.Builder, label string, field int, value string) {
	labelStyle := styleMuted
	if f.focus == field {
		labelStyle = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
	}
	fmt.Fprintf(b, "%s %s\n", labelStyle.Render(fmt.Sprintf("%6s:", label)), value)
}

func (f *habitForm) iconField() string {
	icon := habitIcons[f.icon]
	n := len(habitIcons)
	before := habitIcons[(f.icon+n-1)%n]
	after := habitIcons[(f.icon+1)%n]
	bracket := lipgloss.NewStyle().Foreground(colorActive).Bold(true)
	return fmt.Sprintf("%s %s%s%s %s",
		styleMuted.Render(before),
		bracket.Render("‹"), icon, bracket.Render("›"),
		styleMuted.Render(after))
}

func (f *habitForm) colorField() string {
	swatches := make([]string, 0, len(habitColorNames))
	for i, name := range habitColorNames {
		marker := "●"
		if i == f.color {
			marker = "◉"
		}
		swatches = append(swatches, habitMarkerStyle(name).Render(marker))
	}
	return strings.Join(swatches, " ") + "  " +
		lipgloss.NewStyle().Foreground(colorBright).Render(habitColorNames[f.color])
}

func (f *habitForm) daysField() string {
	names := make([]string, 0, len(habitDayNames))
	for day, label := range habitDayNames {
		style := styleMuted
		if f.days[day] {
			style = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
		}
		if f.focus == habitFieldDays && day == f.dayCursor {
			style = style.Underline(true)
		}
		names = append(names, style.Render(label))
	}
	return strings.Join(names, " ")
}
