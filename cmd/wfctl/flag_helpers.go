package main

import (
	"fmt"
	"strings"
)

// checkTrailingFlags returns an error if any flag (starting with '-') appears
// after the first positional argument in args. A token immediately following a
// flag token (its value) is not counted as a positional argument.
func checkTrailingFlags(args []string) error {
	seenPositional := false
	prevWasFlag := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if seenPositional {
				return fmt.Errorf("flags must come before arguments (got %s after positional arg). Reorder so all flags precede the name argument", arg)
			}
			// Only treat as value-bearing flag if it doesn't use = syntax
			prevWasFlag = !strings.Contains(arg, "=")
		} else {
			if !prevWasFlag {
				seenPositional = true
			}
			prevWasFlag = false
		}
	}
	return nil
}
