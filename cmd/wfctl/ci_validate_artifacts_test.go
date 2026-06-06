package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCIValidatePlatformArtifactRejectsInvalidGitHubActions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.yml")
	if err := os.WriteFile(path, []byte("stages:\n  - plan\n"), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	err := runCIValidate([]string{"--platform", "github_actions", path})
	if err == nil {
		t.Fatal("expected invalid GitHub Actions artifact to fail")
	}
	if !strings.Contains(err.Error(), "ci validate") {
		t.Fatalf("expected ci validate error, got %v", err)
	}
}
