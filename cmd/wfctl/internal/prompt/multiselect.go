package prompt

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// MultiSelect presents an interactive multi-choice list. Returns the indices
// of all selected items. Items can be pre-selected via Item.Preselected.
//
// Returns (nil, ErrNotInteractive) when stdin is not a terminal.
func MultiSelect(title string, items []Item) ([]int, error) {
	if !isTTY() {
		return nil, ErrNotInteractive
	}
	selected := make(map[int]bool, len(items))
	for i, it := range items {
		if it.Preselected {
			selected[i] = true
		}
	}
	m := &multiSelectModel{title: title, items: items, selected: selected}
	p := tea.NewProgram(m, tea.WithOutput(outputWriter()))
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("prompt multiselect: %w", err)
	}
	fm := result.(*multiSelectModel)
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

type multiSelectModel struct {
	title    string
	items    []Item
	cursor   int
	selected map[int]bool
	quit     bool
}

var (
	checkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	uncheckStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m *multiSelectModel) Init() tea.Cmd { return nil }

func (m *multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "ctrl+c", "q":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			if m.selected[m.cursor] {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = true
			}
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *multiSelectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		check := uncheckStyle.Render("[ ]")
		if m.selected[i] {
			check = checkStyle.Render("[x]")
		}
		b.WriteString(cursor + check + " " + it.Label + "\n")
	}
	b.WriteString("\n(space to toggle, enter to confirm)\n")
	return tea.NewView(b.String())
}
