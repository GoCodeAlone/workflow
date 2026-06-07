package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsGenerateHelpIncludesAPIDocFlags(t *testing.T) {
	out, err := captureStderr(t, func() error {
		return runDocsGenerate([]string{"--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
	for _, want := range []string{"--source", "--out", "--module", "--version", "--packages", "--registry", "--subjects"} {
		if !strings.Contains(out, strings.TrimPrefix(want, "--")) {
			t.Fatalf("help output missing %s:\n%s", want, out)
		}
	}
}

func TestDocsGenerateWorkflowPackages(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	_, err = captureStdout(t, func() error {
		return runDocsGenerate([]string{
			"--source", repoRoot,
			"--out", outDir,
			"--module", "github.com/GoCodeAlone/workflow",
			"--version", "v0.75.0",
			"--packages", "plugin,plugin/sdk,plugin/external/sdk",
		})
	})
	if err != nil {
		t.Fatalf("docs generate: %v", err)
	}

	metaPath := filepath.Join(outDir, "versions.json")
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var meta struct {
		SchemaVersion int `json:"schemaVersion"`
		Subject       string
		Versions      map[string][]string `json:"versions"`
		Packages      []struct {
			Subject    string `json:"subject"`
			ImportPath string `json:"importPath"`
			Version    string `json:"version"`
			Path       string `json:"path"`
			Synopsis   string `json:"synopsis"`
		} `json:"packages"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatalf("parse versions.json: %v", err)
	}
	if meta.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", meta.SchemaVersion)
	}
	if got := meta.Versions["workflow"]; len(got) != 1 || got[0] != "v0.75.0" {
		t.Fatalf("workflow versions = %v, want [v0.75.0]", got)
	}
	if len(meta.Packages) != 3 {
		t.Fatalf("packages = %d, want 3 (%+v)", len(meta.Packages), meta.Packages)
	}
	if len(meta.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", meta.Warnings)
	}

	docPath := filepath.Join(outDir, "workflow", "latest", "plugin", "index.md")
	rawDoc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read generated plugin doc: %v", err)
	}
	doc := string(rawDoc)
	for _, want := range []string{
		"# package plugin",
		"Import path: `github.com/GoCodeAlone/workflow/plugin`",
		"Version: `v0.75.0`",
		"https://github.com/GoCodeAlone/workflow/tree/v0.75.0/plugin",
		"## Types",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("generated doc missing %q:\n%s", want, doc)
		}
	}
}
