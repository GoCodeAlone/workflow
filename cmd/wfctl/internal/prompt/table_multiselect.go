package prompt

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

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
	m := newTableMultiSelectModel(title, columns, items)
	p := tea.NewProgram(m, tea.WithOutput(out))
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("prompt table multiselect: %w", err)
	}
	fm := result.(*tableMultiSelectModel)
	if fm.quit {
		return nil, ErrInterrupted
	}
	var indices []int
	for i := range items {
		if fm.selected[i] {
			indices = append(indices, i)
		}
	}
	return indices, nil
}

type tableMultiSelectModel struct {
	title    string
	columns  []TableColumn
	items    []TableItem
	selected map[int]bool
	table    table.Model
	quit     bool
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
	m.table = table.New(
		table.WithColumns(m.tableColumns()),
		table.WithRows(m.tableRows()),
		table.WithFocused(true),
		table.WithHeight(min(max(len(items)+2, 5), 18)), //nolint:mnd
	)
	return m
}

func (m *tableMultiSelectModel) Init() tea.Cmd { return nil }

func (m *tableMultiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "ctrl+c", "q":
			m.quit = true
			return m, tea.Quit
		case " ":
			cursor := m.table.Cursor()
			if m.selected[cursor] {
				delete(m.selected, cursor)
			} else {
				m.selected[cursor] = true
			}
			m.table.SetRows(m.tableRows())
			return m, nil
		case "enter":
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *tableMultiSelectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	b.WriteString(m.table.View())
	b.WriteString("\n\n(space to toggle, enter to confirm)\n")
	return tea.NewView(b.String())
}

func (m *tableMultiSelectModel) tableColumns() []table.Column {
	cols := make([]table.Column, 0, len(m.columns)+1)
	cols = append(cols, table.Column{Title: "Set", Width: 3})
	for _, col := range m.columns {
		cols = append(cols, table.Column{Title: col.Title, Width: col.Width})
	}
	return cols
}

func (m *tableMultiSelectModel) tableRows() []table.Row {
	rows := make([]table.Row, 0, len(m.items))
	for i, item := range m.items {
		mark := " "
		if m.selected[i] {
			mark = "x"
		}
		cells := make([]string, 0, len(item.Cells)+1)
		cells = append(cells, mark)
		cells = append(cells, item.Cells...)
		rows = append(rows, table.Row(cells))
	}
	return rows
}
