package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunScaffoldCmd_DockerfileSubcommand (Task 9): `wfctl scaffold dockerfile`
// still generates a hardened Dockerfile.prebuilt (the legacy behavior, now
// reached via the dockerfile subcommand).
func TestRunScaffoldCmd_DockerfileSubcommand(t *testing.T) {
	out := t.TempDir()
	if err := runScaffoldCmd([]string{"dockerfile", "--output", out}); err != nil {
		t.Fatalf("scaffold dockerfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "Dockerfile.prebuilt")); os.IsNotExist(err) {
		t.Fatalf("Dockerfile.prebuilt not generated in %s", out)
	}
}

// TestRunScaffoldCmd_PrimaryDelegatesToAssemble (Task 9): `wfctl scaffold` with
// assemble flags (no subcommand) is the primary capability-driven path — it
// delegates to the assemble logic, which requires --set/--out. Invoking without
// --set surfaces the assemble requirement (proving delegation, not a dockerfile
// fallback).
func TestRunScaffoldCmd_PrimaryDelegatesToAssemble(t *testing.T) {
	err := runScaffoldCmd([]string{"--out", t.TempDir()})
	if err == nil {
		t.Fatal("expected assemble requirement error without --set")
	}
	if !strings.Contains(err.Error(), "set") {
		t.Fatalf("primary path must delegate to assemble (mention --set); got: %v", err)
	}
}

// TestRunScaffoldCmd_BareUsage (Task 9): bare `wfctl scaffold` prints usage +
// errors (it is no longer a dockerfile-only command; the user must pick a
// subcommand or pass assemble flags).
func TestRunScaffoldCmd_BareUsage(t *testing.T) {
	err := runScaffoldCmd([]string{})
	if err == nil {
		t.Fatal("expected usage error for bare scaffold")
	}
}

// TestRunScaffoldCmd_Help (Task 9): -h lists the modes.
func TestRunScaffoldCmd_Help(t *testing.T) {
	if err := runScaffoldCmd([]string{"-h"}); err != nil {
		t.Fatalf("scaffold -h: %v", err)
	}
}
