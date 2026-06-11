package prompt

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Input prompts the user for a single-line text value. When masked is true
// the input is displayed as asterisks (suitable for passwords/tokens).
//
// Returns ("", ErrNotInteractive) when stdin is not a terminal.
func Input(label string, masked bool) (string, error) {
	return InputWithSuggestions(label, masked, nil)
}

// InputWithSuggestions prompts the user for a single-line text value and shows
// completion suggestions when suggestions is non-empty. Tab accepts the current
// suggestion using the underlying bubbles textinput behavior.
func InputWithSuggestions(label string, masked bool, suggestions []string) (string, error) {
	if !isTTY() {
		return "", ErrNotInteractive
	}
	out, _ := outputWriter()
	ti := textinput.New()
	ti.Focus()
	if masked {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '*'
	}
	if len(suggestions) > 0 {
		ti.ShowSuggestions = true
		ti.SetSuggestions(suggestions)
	}
	finalModel, err := tea.NewProgram(&inputModel{label: label, ti: ti}, tea.WithOutput(out)).Run()
	if err != nil {
		return "", fmt.Errorf("prompt input: %w", err)
	}
	m, ok := finalModel.(*inputModel)
	if !ok {
		return "", fmt.Errorf("prompt: unexpected input model %T", finalModel)
	}
	if m.interrupted {
		return "", ErrCancelled
	}
	return m.ti.Value(), nil
}

type inputModel struct {
	label       string
	ti          textinput.Model
	quit        bool
	interrupted bool
}

var labelStyle = lipgloss.NewStyle().Bold(true)

func (m *inputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		switch kp.String() {
		case "ctrl+c", "esc":
			m.quit = true
			m.interrupted = true
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		}
	}
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m *inputModel) View() tea.View {
	return tea.NewView(labelStyle.Render(m.label+": ") + m.ti.View() + "\n")
}
