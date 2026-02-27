package main

import (
	"testing"
)

func TestRunMCP_Usage(t *testing.T) {
	// Passing -h should return an error from flag parsing (ExitOnError calls os.Exit,
	// but we use ContinueOnError in runMCP so it returns an error instead).
	err := runMCP([]string{"-h"})
	if err == nil {
		t.Fatal("expected error from -h flag")
	}
}

func TestRunMCP_UnknownFlag(t *testing.T) {
	err := runMCP([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}
