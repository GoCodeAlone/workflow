package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// ResolveTag walks entries in order, returning the first non-empty resolved value.
// Each entry is tried in sequence:
//  1. If Env is set, os.Getenv is checked — non-empty value wins.
//  2. If Command is set, it is run via sh -c — non-empty trimmed stdout wins.
//
// If no entry resolves, fallback is returned.
func ResolveTag(entries []config.TagFromEntry, fallback string) string {
	for _, e := range entries {
		if e.Env != "" {
			if v := os.Getenv(e.Env); v != "" {
				return v
			}
		}
		if e.Command != "" {
			out, err := exec.Command("sh", "-c", e.Command).Output() //nolint:gosec
			if err == nil && len(out) > 0 {
				if v := strings.TrimSpace(string(out)); v != "" {
					return v
				}
			}
		}
	}
	return fallback
}
