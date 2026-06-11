package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
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
	fmt.Fprint(out, label+": ")
	if masked {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(value), "\r\n"), nil
	}
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(value, "\r\n"), nil
}

type inputModel struct {
	label string
	ti    textinput.Model
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
			return m, tea.Quit
		}
	}
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m *inputModel) View() tea.View {
	return tea.NewView(labelStyle.Render(m.label+": ") + m.ti.View() + "\n")
}
