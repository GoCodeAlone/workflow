package prompt

import (
	"fmt"
	"strconv"
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
	out, _ := outputWriter()
	finalModel, err := tea.NewProgram(newMultiSelectModel(title, items), tea.WithOutput(out)).Run()
	if err != nil {
		return nil, err
	}
	m, ok := finalModel.(*multiSelectModel)
	if !ok {
		return nil, fmt.Errorf("prompt: unexpected multiselect model %T", finalModel)
	}
	if m.interrupted {
		return nil, ErrInterrupted
	}
	return m.selectedIndexes(), nil
}

func parseIndexSelection(line string, count int) ([]int, error) {
	seen := make(map[int]bool)
	var out []int
	for _, raw := range strings.Split(line, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		startRaw, endRaw, hasRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(startRaw))
		if err != nil {
			return nil, fmt.Errorf("invalid selection %q", part)
		}
		end := start
		if hasRange {
			end, err = strconv.Atoi(strings.TrimSpace(endRaw))
			if err != nil {
				return nil, fmt.Errorf("invalid selection %q", part)
			}
		}
		if start < 1 || end < start || end > count {
			return nil, fmt.Errorf("selection %q out of range 1-%d", part, count)
		}
		for n := start; n <= end; n++ {
			idx := n - 1
			if !seen[idx] {
				seen[idx] = true
				out = append(out, idx)
			}
		}
	}
	return out, nil
}

type multiSelectModel struct {
	title       string
	items       []Item
	cursor      int
	selected    map[int]bool
	quit        bool
	interrupted bool
}

var (
	checkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	uncheckStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func newMultiSelectModel(title string, items []Item) *multiSelectModel {
	selected := make(map[int]bool, len(items))
	for i, item := range items {
		if item.Preselected {
			selected[i] = true
		}
	}
	return &multiSelectModel{
		title:    title,
		items:    items,
		selected: selected,
	}
}

func (m *multiSelectModel) Init() tea.Cmd { return nil }

func (m *multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *multiSelectModel) selectedIndexes() []int {
	selected := make([]int, 0, len(m.selected))
	for i := range m.items {
		if m.selected[i] {
			selected = append(selected, i)
		}
	}
	return selected
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
	b.WriteString("\n(up/down move, space toggles, enter confirms, esc cancels)\n")
	return tea.NewView(b.String())
}
