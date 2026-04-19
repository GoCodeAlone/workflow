package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestGenerateSBOM_DryRun_PrintsPlan(t *testing.T) {
	sec := &config.CIBuildSecurity{SBOM: true}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	if err := GenerateSBOM(t.Context(), "registry.example.com/myapp:latest", sec, os.Stdout); err != nil {
		t.Fatalf("GenerateSBOM dry-run: %v", err)
	}
}

func TestGenerateSBOM_SBOMFalse_NoOp(t *testing.T) {
	sec := &config.CIBuildSecurity{SBOM: false}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	if err := GenerateSBOM(t.Context(), "registry.example.com/myapp:latest", sec, os.Stdout); err != nil {
		t.Fatalf("SBOM=false should be a no-op: %v", err)
	}
}

func TestGenerateSBOM_NilSecurity_NoOp(t *testing.T) {
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	if err := GenerateSBOM(t.Context(), "registry.example.com/myapp:latest", nil, os.Stdout); err != nil {
		t.Fatalf("nil security should be a no-op: %v", err)
	}
}

func TestAttachSBOM_DryRun(t *testing.T) {
	dir := t.TempDir()
	sbomPath := filepath.Join(dir, "sbom.json")
	if err := os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX"}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	if err := AttachSBOM(t.Context(), "registry.example.com/myapp:latest", sbomPath, os.Stdout); err != nil {
		t.Fatalf("AttachSBOM dry-run: %v", err)
	}
}

func TestSBOMFilePath_Naming(t *testing.T) {
	// sbomFilePath should produce a stable, deterministic name.
	path := sbomFilePath("registry.example.com/myapp:v1.2.3")
	if path == "" {
		t.Fatal("sbomFilePath should return non-empty string")
	}
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("SBOM path should end in .json, got %q", path)
	}
}
