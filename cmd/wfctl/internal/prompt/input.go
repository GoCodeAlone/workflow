package prompt

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Input prompts the user for a single-line text value. When masked is true
// the input is displayed as asterisks (suitable for passwords/tokens).
//
// Returns ("", ErrNotInteractive) when stdin is not a terminal.
func Input(label string, masked bool) (string, error) {
	if !isTTY() {
		return "", ErrNotInteractive
	}
	ti := textinput.New()
	ti.Placeholder = label
	ti.Focus()
	if masked {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '*'
	}

	m := &inputModel{label: label, ti: ti}
	p := tea.NewProgram(m, tea.WithOutput(io.Discard))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("prompt input: %w", err)
	}
	fm := result.(*inputModel)
	if fm.quit {
		return "", ErrNotInteractive
	}
	return fm.ti.Value(), nil
}

type inputModel struct {
	label string
	ti    textinput.Model
	done  bool
	quit  bool
}

var labelStyle = lipgloss.NewStyle().Bold(true)

func (m *inputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		switch kp.String() {
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m *inputModel) View() tea.View {
	return tea.NewView(labelStyle.Render(m.label+": ") + m.ti.View() + "\n")
}
