// Package prompt provides reusable terminal UI widgets built on
// charm.land/bubbletea/v2 and charm.land/bubbles/v2.
//
// Every public constructor checks whether stdin is an interactive terminal
// before starting a bubbletea program. If stdin is not a terminal the
// constructor returns (zero, ErrNotInteractive) immediately so callers in
// CI / pipe mode can detect the condition and fall back to non-interactive
// paths without any risk of hanging.
package prompt

import (
	"errors"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// ErrNotInteractive is returned by all constructors when stdin is not a terminal.
var ErrNotInteractive = errors.New("prompt: stdin is not a terminal")

// ErrInterrupted is returned when the user aborts an interactive prompt.
var ErrInterrupted = errors.New("prompt: interrupted")

// Item is a selectable entry for MultiSelect.
type Item struct {
	Label       string
	Preselected bool
}

// isTTY reports whether os.Stdin is an interactive terminal.
func isTTY() bool {
	return CanPrompt()
}

// CanPrompt reports whether prompts can safely read input and render output.
func CanPrompt() bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	_, ok := outputWriter()
	return ok
}

func outputWriter() (io.Writer, bool) {
	return chooseOutputWriter(
		isatty.IsTerminal(os.Stderr.Fd()),
		isatty.IsTerminal(os.Stdout.Fd()),
		os.Stderr,
		os.Stdout,
	)
}

func chooseOutputWriter(stderrTTY, stdoutTTY bool, stderr, stdout io.Writer) (io.Writer, bool) {
	switch {
	case stderrTTY:
		return stderr, true
	case stdoutTTY:
		return stdout, true
	default:
		return nil, false
	}
}
