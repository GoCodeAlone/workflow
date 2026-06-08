package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Select presents an interactive single-choice list to the user and returns
// the index of the chosen item.
//
// Returns (0, ErrNotInteractive) when stdin is not a terminal.
func Select(title string, opts []string) (int, error) {
	if !isTTY() {
		return 0, ErrNotInteractive
	}
	out, _ := outputWriter()
	fmt.Fprintln(out, title)
	for i, opt := range opts {
		fmt.Fprintf(out, "  %d. %s\n", i+1, opt)
	}
	fmt.Fprint(out, "Choose [1]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(opts) {
		return 0, fmt.Errorf("invalid selection %q", line)
	}
	return idx - 1, nil
}

// selectModel is the bubbletea model for single selection.
type selectModel struct {
	title  string
	opts   []string
	cursor int
	quit   bool
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	normalStyle   = lipgloss.NewStyle()
)

func (m *selectModel) Init() tea.Cmd { return nil }

func (m *selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.opts)-1 {
				m.cursor++
			}
		case "enter", " ":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *selectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	for i, opt := range m.opts {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
			b.WriteString(selectedStyle.Render(cursor+opt) + "\n")
		} else {
			b.WriteString(normalStyle.Render(cursor+opt) + "\n")
		}
	}
	return tea.NewView(b.String())
}
