package main

import (
	"strings"
	"testing"
)

func TestRunRegistryContainer_UnknownSubcommand(t *testing.T) {
	err := runRegistry([]string{"does-not-exist"})
	if err == nil {
		t.Fatal("want error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error should mention subcommand, got: %v", err)
	}
}

func TestRunRegistryContainer_NoArgs(t *testing.T) {
	err := runRegistry([]string{})
	if err == nil {
		t.Fatal("want error when no subcommand given")
	}
}

func TestRunRegistryContainer_LoginRoutes(t *testing.T) {
	// login with no config — should fail gracefully (no panic).
	err := runRegistry([]string{"login"})
	// May succeed or fail depending on environment; we just verify no panic.
	_ = err
}

func TestRunRegistryContainer_LogoutRoutes(t *testing.T) {
	err := runRegistry([]string{"logout"})
	_ = err
}
