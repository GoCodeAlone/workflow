package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCIValidate_ValidConfig(t *testing.T) {
	f := writeTempConfig(t, "modules:\n  - name: server\n    type: http.server\n    config:\n      address: \":8080\"\n")
	err := runCIValidate([]string{f})
	if err != nil {
		t.Fatalf("expected valid config to pass ci validate: %v", err)
	}
}

func TestRunCIValidate_MissingFile(t *testing.T) {
	err := runCIValidate([]string{"/nonexistent/path.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestRunCIValidate_NoArgs(t *testing.T) {
	err := runCIValidate([]string{})
	if err == nil {
		t.Fatal("expected error when no files provided")
	}
}

func TestRunCIValidate_ImmutableSections(t *testing.T) {
	f := writeTempConfig(t, "modules:\n  - name: server\n    type: http.server\n    config:\n      port: 8080\n")
	// Require workflows section — should fail since it's missing
	err := runCIValidate([]string{"--immutable-sections=workflows", f})
	if err == nil {
		t.Fatal("expected failure when required workflows section is missing")
	}
}

func TestRunCIValidate_JSONFormat(t *testing.T) {
	f := writeTempConfig(t, "modules:\n  - name: server\n    type: http.server\n    config:\n      address: \":8080\"\n")
	err := runCIValidate([]string{"--format=json", f})
	if err != nil {
		t.Fatalf("expected json format to succeed: %v", err)
	}
}

// writeTempConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return f
}
