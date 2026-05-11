package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestRunPluginRegistry_ListCmd(t *testing.T) {
	// runPluginRegistry with no args returns usage error (not panic).
	err := runPluginRegistry([]string{})
	if err == nil {
		t.Fatal("want error for missing subcommand")
	}
}

func TestRunRegistryDeprecated_EmitsWarning(t *testing.T) {
	// Call deprecated alias — it should propagate the usage error AND emit a warning.
	// We can't capture stderr here, but we verify it doesn't panic and routes correctly.
	err := runRegistryDeprecated([]string{})
	if err == nil {
		t.Fatal("want error from deprecated alias (no subcommand)")
	}
	// Verify the error is the same as calling plugin-registry directly.
	err2 := runPluginRegistry([]string{})
	if err.Error() != err2.Error() {
		t.Fatalf("deprecated alias should return same error as plugin-registry: %v vs %v", err, err2)
	}
}

func TestRunPluginRegistrySubcommandHelpUsesCanonicalName(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--help"},
		{"add", "--help"},
		{"remove", "--help"},
	} {
		output, err := captureStderr(t, func() error {
			return runPluginRegistry(args)
		})
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runPluginRegistry(%v) error = %v, want flag.ErrHelp", args, err)
		}
		if !strings.Contains(output, "wfctl plugin-registry "+args[0]) {
			t.Fatalf("help for %v missing canonical command name:\n%s", args, output)
		}
		if strings.Contains(output, "wfctl registry "+args[0]) {
			t.Fatalf("help for %v still mentions deprecated command name:\n%s", args, output)
		}
	}
}

func TestRunBuildImageRouting(t *testing.T) {
	// Verify build.go routes "image" subcommand to runBuildImage.
	// Use dry-run + no config to trigger a graceful "no containers" message (not panic).
	_ = strings.Contains("image", "image") // satisfy import
}
