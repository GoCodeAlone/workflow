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
	"os"

	"github.com/mattn/go-isatty"
)

// ErrNotInteractive is returned by all constructors when stdin is not a terminal.
var ErrNotInteractive = errors.New("prompt: stdin is not a terminal")

// Item is a selectable entry for MultiSelect.
type Item struct {
	Label       string
	Preselected bool
}

// isTTY reports whether os.Stdin is an interactive terminal.
func isTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}
