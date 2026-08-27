package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailbox/src/internal/terminal"
)

type todoMode int

const (
	todoBrowsing todoMode = iota
	todoAdding
	todoRenaming
)

type todoPicker struct {
	todos     []Recording
	cursor    int
	mode      todoMode
	buf       string
	confirmed string
	status    string
}

func newTodoPicker(todos []Recording) *todoPicker {
	return &todoPicker{todos: todos}
}

func (p *todoPicker) setTodos(todos []Recording) {
	onID := ""
	if selected := p.selected(); selected != nil {
		onID = selected.ID
	}
	p.todos = todos
	p.cursor = min(p.cursor, max(len(todos)-1, 0))
	for i, todo := range todos {
		if todo.ID == onID {
			p.cursor = i
			break
		}
	}
	p.confirmed = ""
}

func (p *todoPicker) selected() *Recording {
	if p.cursor < 0 || p.cursor >= len(p.todos) {
		return nil
	}
	return &p.todos[p.cursor]
}

func (p *todoPicker) startAdding() {
	p.mode = todoAdding
	p.confirmed = ""
	p.status = ""
	p.buf = ""
}

func (p *todoPicker) startRenaming() {
	selected := p.selected()
	if selected == nil {
		return
	}
	p.mode = todoRenaming
	p.confirmed = ""
	p.status = ""
	p.buf = terminal.SanitizeLine(selected.Title)
}

func (p *todoPicker) stopEditing() {
	p.mode = todoBrowsing
	p.status = ""
	p.buf = ""
}

func (p *todoPicker) title() (string, bool) {
	title := strings.TrimSpace(p.buf)
	if title == "" {
		p.status = "Give the to-do a name"
		return "", false
	}
	return title, true
}

func (p *todoPicker) renamed() (Recording, string, bool) {
	selected := p.selected()
	title, ok := p.title()
	if selected == nil || !ok {
		return Recording{}, "", false
	}
	if title == selected.Title || title == terminal.SanitizeLine(selected.Title) {
		return Recording{}, "", false
	}
	return *selected, title, true
}

func (p *todoPicker) editing() bool { return p.mode != todoBrowsing }

func (p *todoPicker) moveCursor(msg tea.KeyPressMsg) {
	p.cursor = stepListCursor(p.cursor, len(p.todos), msg)
	p.confirmed = ""
}

func (p *todoPicker) draw(base string, width, height int) string {
	contentWidth := modalContentWidth(width)
	visible := modalContentRows(height)
	for _, extra := range []bool{p.editing(), p.status != ""} {
		if extra {
			visible = max(visible-2, 1)
		}
	}

	var rows []string
	start, end := modalListWindow(len(p.todos), p.cursor, visible)
	for i := start; i < end; i++ {
		todo := p.todos[i]
		done := todo.Done()
		marker, markerStyle := "□", lipgloss.NewStyle().Foreground(colorAlert).Bold(true)
		labelStyle := lipgloss.NewStyle().Foreground(colorBright)
		if done {
			marker, markerStyle, labelStyle = "■", styleMuted, styleMuted
		}
		prefix := "  "
		if i == p.cursor {
			prefix = "› "
			if !p.editing() {
				labelStyle = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
			}
		}
		title := truncateToWidth(terminal.SanitizeLine(todo.Title), max(contentWidth-4, 1))
		rows = append(rows, prefix+markerStyle.Render(marker)+" "+labelStyle.Render(title))
	}

	body := strings.Join(rows, "\n")
	if len(p.todos) == 0 {
		body = styleMuted.Render("Nothing to do this week")
	}
	if p.editing() {
		label := "New to-do: "
		if p.mode == todoRenaming {
			label = "Rename: "
		}
		body += "\n\n" + styleMuted.Render(label) + p.buf + "█"
	}
	if p.status != "" {
		body += "\n\n" + styleMuted.Render(truncateToWidth(terminal.SanitizeLine(p.status), contentWidth))
	}
	return overlayModal(base, modalFrame(todosSectionLabel, body, width), width, height)
}

func (p *todoPicker) helpBindings() []helpBinding {
	if p.editing() {
		save := "add"
		if p.mode == todoRenaming {
			save = "rename"
		}
		return []helpBinding{{"enter", save}, {"esc", "cancel"}}
	}
	bindings := []helpBinding{{"↑↓", "choose"}}
	if selected := p.selected(); selected != nil {
		doneLabel := "mark done"
		if selected.Done() {
			doneLabel = "clear"
		}
		deleteLabel := "delete"
		if p.confirmed == selected.ID {
			deleteLabel = "press x again to delete"
		}
		bindings = append(bindings,
			helpBinding{"enter", doneLabel},
			helpBinding{"e", "rename"},
			helpBinding{"x", deleteLabel})
	}
	return append(bindings, helpBinding{"a", "new to-do"}, helpBinding{"esc", "close"})
}
