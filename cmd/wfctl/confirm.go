package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
)

func isPromptCancelled(err error) bool {
	return errors.Is(err, prompt.ErrCancelled)
}

func confirmAction(question string, def bool, out io.Writer, confirm func(string, bool) (bool, error)) (bool, error) {
	if confirm == nil {
		confirm = prompt.Confirm
	}
	ok, err := confirm(question, def)
	if err != nil {
		if isPromptCancelled(err) || errors.Is(err, prompt.ErrNotInteractive) {
			if out != nil {
				fmt.Fprintln(out, "Cancelled.")
			}
			return false, nil
		}
		return false, err
	}
	if !ok && out != nil {
		fmt.Fprintln(out, "Cancelled.")
	}
	return ok, nil
}
