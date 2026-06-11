package prompt

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const tableSelectColumnWidth = 4

// TableColumn describes a column in a table-based prompt.
type TableColumn struct {
	Title string
	Width int
}

// TableItem is a selectable table row. Cells must match the number of caller
// supplied columns; the prompt adds its own selection marker column.
type TableItem struct {
	Cells       []string
	Preselected bool
}

// TableMultiSelect presents a table-shaped interactive multi-choice list.
// Returns selected row indices.
//
// Returns (nil, ErrNotInteractive) when stdin is not a terminal.
func TableMultiSelect(title string, columns []TableColumn, items []TableItem) ([]int, error) {
	if !isTTY() {
		return nil, ErrNotInteractive
	}
	out, _ := outputWriter()
	finalModel, err := tea.NewProgram(newTableMultiSelectModel(title, columns, items), tea.WithOutput(out)).Run()
	if err != nil {
		return nil, err
	}
	m, ok := finalModel.(*tableMultiSelectModel)
	if !ok {
		return nil, fmt.Errorf("prompt: unexpected table multiselect model %T", finalModel)
	}
	if m.interrupted {
		return nil, ErrInterrupted
	}
	return m.selectedIndexes(), nil
}

type tableMultiSelectModel struct {
	title       string
	columns     []TableColumn
	items       []TableItem
	selected    map[int]bool
	cursor      int
	quit        bool
	interrupted bool
}

func newTableMultiSelectModel(title string, columns []TableColumn, items []TableItem) *tableMultiSelectModel {
	selected := make(map[int]bool, len(items))
	for i, item := range items {
		if item.Preselected {
			selected[i] = true
		}
	}
	m := &tableMultiSelectModel{
		title:    title,
		columns:  columns,
		items:    items,
		selected: selected,
	}
	return m
}

func (m *tableMultiSelectModel) Init() tea.Cmd { return nil }

func (m *tableMultiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.quit = true
			m.interrupted = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ", "space":
			if len(m.items) == 0 {
				return m, nil
			}
			if m.selected[m.cursor] {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = true
			}
			return m, nil
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *tableMultiSelectModel) selectedIndexes() []int {
	selected := make([]int, 0, len(m.selected))
	for i := range m.items {
		if m.selected[i] {
			selected = append(selected, i)
		}
	}
	return selected
}

func (m *tableMultiSelectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	b.WriteString(m.headerRow())
	b.WriteString("\n")
	b.WriteString(m.separatorRow())
	b.WriteString("\n")
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		mark := "[ ]"
		if m.selected[i] {
			mark = "[x]"
		}
		b.WriteString(cursor)
		b.WriteString(padTableCell(mark, tableSelectColumnWidth))
		for colIdx, col := range m.columns {
			cell := ""
			if colIdx < len(item.Cells) {
				cell = item.Cells[colIdx]
			}
			b.WriteString(" ")
			b.WriteString(padTableCell(cell, col.Width))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n\n(up/down move, space toggles, enter confirms, esc cancels)\n")
	return tea.NewView(b.String())
}

func (m *tableMultiSelectModel) headerRow() string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(padTableCell("Pick", tableSelectColumnWidth))
	for _, col := range m.columns {
		b.WriteString(" ")
		b.WriteString(padTableCell(col.Title, col.Width))
	}
	return b.String()
}

func (m *tableMultiSelectModel) separatorRow() string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(strings.Repeat("-", tableSelectColumnWidth))
	for _, col := range m.columns {
		b.WriteString(" ")
		b.WriteString(strings.Repeat("-", max(col.Width, 1)))
	}
	return b.String()
}

func padTableCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > width {
		if width <= 1 {
			return s[:width]
		}
		return s[:width-1] + "~"
	}
	return s + strings.Repeat(" ", width-len(s))
}
