package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunScaffoldCmd_Template (Task 10): `wfctl scaffold template` scaffolds a
// project from a template — the same output the old `wfctl init` produced
// (init is folded here per design D5).
func TestRunScaffoldCmd_Template(t *testing.T) {
	out := t.TempDir()
	// Flags must precede the positional project name (Go flag parsing stops at
	// the first non-flag argument).
	if err := runScaffoldCmd([]string{"template", "-output", out, "my-api"}); err != nil {
		t.Fatalf("scaffold template: %v", err)
	}
	// api-service is the default template; main.go + workflow.yaml are rendered.
	for _, f := range []string{"main.go", "workflow.yaml", "go.mod"} {
		if _, err := os.Stat(filepath.Join(out, f)); os.IsNotExist(err) {
			t.Errorf("template did not render %s in %s", f, out)
		}
	}
}

// TestRunInit_DeprecatedAlias (Task 10, D5): `wfctl init` still works (delegates
// to the shared template core) — backward-compatible for one release.
func TestRunInit_DeprecatedAlias(t *testing.T) {
	out := t.TempDir()
	if err := runInit([]string{"-output", out, "legacy-api"}); err != nil {
		t.Fatalf("runInit deprecated alias: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "main.go")); os.IsNotExist(err) {
		t.Errorf("deprecated init did not render main.go in %s", out)
	}
}

// TestRunScaffoldTemplate_List (Task 10): -list enumerates templates.
func TestRunScaffoldTemplate_List(t *testing.T) {
	// -list writes to stdout + returns nil; just assert no error + it doesn't
	// require a project name.
	if err := runScaffoldTemplate([]string{"-list"}); err != nil {
		t.Fatalf("scaffold template -list: %v", err)
	}
}

// TestRunScaffoldTemplate_UnknownErrors (Task 10): an unknown template errors.
func TestRunScaffoldTemplate_UnknownErrors(t *testing.T) {
	// Flags must precede the positional project name (Go flag parsing stops at
	// the first non-flag argument).
	err := runScaffoldTemplate([]string{"-template", "no-such-template", "x", "-output", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("expected unknown-template error, got: %v", err)
	}
}
