package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Confirm asks a yes/no question. def is the default answer shown in the
// prompt (used when the user presses Enter without typing y/n).
//
// Returns (false, ErrNotInteractive) when stdin is not a terminal.
func Confirm(question string, def bool) (bool, error) {
	if !isTTY() {
		return false, ErrNotInteractive
	}
	out, _ := outputWriter()
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(out, "%s [%s] ", question, hint)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid confirmation %q", strings.TrimSpace(line))
	}
}

type confirmModel struct {
	question string
	hint     string
	def      bool
	answer   bool
	quit     bool
}

func (m *confirmModel) Init() tea.Cmd { return nil }

func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch strings.ToLower(msg.String()) {
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "y":
			m.answer = true
			return m, tea.Quit
		case "n":
			m.answer = false
			return m, tea.Quit
		case "enter":
			m.answer = m.def
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *confirmModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("%s [%s] ", m.question, m.hint))
}
