package prompt

import (
	"fmt"
	"io"
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
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	m := &confirmModel{question: question, hint: hint, def: def}
	p := tea.NewProgram(m, tea.WithOutput(io.Discard))
	result, err := p.Run()
	if err != nil {
		return false, fmt.Errorf("prompt confirm: %w", err)
	}
	fm := result.(*confirmModel)
	if fm.quit {
		return false, ErrNotInteractive
	}
	return fm.answer, nil
}

type confirmModel struct {
	question string
	hint     string
	def      bool
	answer   bool
	done     bool
	quit     bool
}

func (m *confirmModel) Init() tea.Cmd { return nil }

func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch strings.ToLower(msg.String()) {
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "y":
			m.answer = true
			m.done = true
			return m, tea.Quit
		case "n":
			m.answer = false
			m.done = true
			return m, tea.Quit
		case "enter":
			m.answer = m.def
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *confirmModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("%s [%s] ", m.question, m.hint))
}
