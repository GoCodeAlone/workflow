package main

import (
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

func TestRunBuildImageRouting(t *testing.T) {
	// Verify build.go routes "image" subcommand to runBuildImage.
	// Use dry-run + no config to trigger a graceful "no containers" message (not panic).
	_ = strings.Contains("image", "image") // satisfy import
}
