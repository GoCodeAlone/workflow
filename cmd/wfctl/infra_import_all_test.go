package main

import (
	"strings"
	"testing"
)

// TestRunInfraImportAll_requiresProvider pins the contract: import-all without
// --provider fails fast with a clear error pointing at the missing flag.
// Mirrors the sister guard in runInfraImport which requires --name. Catches
// the regression where the dispatch core silently defaults to the empty
// provider name and falls through to a generic "no module named \"\"" error
// that doesn't help operators.
func TestRunInfraImportAll_requiresProvider(t *testing.T) {
	err := runInfraImportAll([]string{})
	if err == nil {
		t.Fatal("expected error from runInfraImportAll with no flags; got nil")
	}
	if !strings.Contains(err.Error(), "--provider") {
		t.Fatalf("error %q should mention --provider; got %v", err.Error(), err)
	}
}

// TestRunInfraImportAll_requiresType pins the second-required-flag contract:
// after --provider passes, --type must also be set. Catches the regression
// where the implementation only validates --provider and lets a missing --type
// fall through to enumerator.EnumerateAll("") which surfaces as a generic
// "resource type not supported" error from the provider plugin instead of a
// clear CLI-level error.
func TestRunInfraImportAll_requiresType(t *testing.T) {
	err := runInfraImportAll([]string{"--provider", "digitalocean"})
	if err == nil {
		t.Fatal("expected error from runInfraImportAll with --provider but no --type; got nil")
	}
	if !strings.Contains(err.Error(), "--type") {
		t.Fatalf("error %q should mention --type; got %v", err.Error(), err)
	}
}
