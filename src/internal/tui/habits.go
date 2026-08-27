package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/terminal"
)

type habitPicker struct {
	habits      []Recording
	cursor      int
	completable bool
	confirmed   string
	status      string
}

func newHabitPicker(habits []Recording, completable bool) *habitPicker {
	return &habitPicker{habits: habits, completable: completable}
}

func (p *habitPicker) setHabits(habits []Recording) {
	onID := ""
	if selected := p.selected(); selected != nil {
		onID = selected.ID
	}
	p.habits = habits
	p.cursor = min(p.cursor, max(len(habits)-1, 0))
	for i, habit := range habits {
		if habit.ID == onID {
			p.cursor = i
			break
		}
	}
	p.confirmed = ""
}

func (p *habitPicker) selected() *Recording {
	if p.cursor < 0 || p.cursor >= len(p.habits) {
		return nil
	}
	return &p.habits[p.cursor]
}

func (p *habitPicker) moveCursor(msg tea.KeyPressMsg) {
	p.cursor = stepListCursor(p.cursor, len(p.habits), msg)
	p.confirmed = ""
}

func (p *habitPicker) draw(base string, width, height int) string {
	contentWidth := modalContentWidth(width)
	visible := modalContentRows(height)
	if p.status != "" {
		visible = max(visible-2, 1)
	}

	var rows []string
	start, end := modalListWindow(len(p.habits), p.cursor, visible)
	for i := start; i < end; i++ {
		habit := p.habits[i]
		done := p.completable && habit.Done()
		marker := habitMarkerStyle(habit.Color).Render(habitMarker(done))
		if !p.completable {
			marker = habitMarkerStyle(habit.Color).Render("·")
		}
		label := truncateToWidth(habitLabel(habit), max(contentWidth-4, 1))
		labelStyle := lipgloss.NewStyle().Foreground(colorBright)
		prefix := "  "
		if done {
			labelStyle = styleMuted
		}
		if i == p.cursor {
			labelStyle = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
			prefix = "› "
		}
		rows = append(rows, prefix+marker+" "+labelStyle.Render(label))
	}

	body := strings.Join(rows, "\n")
	if len(p.habits) == 0 {
		body = styleMuted.Render("No habits yet")
	}
	if p.status != "" {
		body += "\n\n" + styleMuted.Render(truncateToWidth(terminal.SanitizeLine(p.status), contentWidth))
	}
	return overlayModal(base, modalFrame("Habits", body, width), width, height)
}

func (p *habitPicker) helpBindings() []helpBinding {
	bindings := []helpBinding{{"↑↓", "choose"}}
	if selected := p.selected(); selected != nil {
		if p.completable {
			doneLabel := "mark done"
			if selected.Done() {
				doneLabel = "clear"
			}
			bindings = append(bindings, helpBinding{"enter", doneLabel})
		}
		deleteLabel := "delete"
		if p.confirmed == selected.ID {
			deleteLabel = "press x again to delete"
		}
		bindings = append(bindings, helpBinding{"e", "edit"}, helpBinding{"x", deleteLabel})
	}
	return append(bindings, helpBinding{"a", "new habit"}, helpBinding{"esc", "close"})
}
