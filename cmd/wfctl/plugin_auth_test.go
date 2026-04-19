package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPrivateAuth_SetsGitConfig(t *testing.T) {
	t.Setenv("TEST_PRIVATE_TOKEN", "secret123")

	cleanup, err := applyPrivateAuth("TEST_PRIVATE_TOKEN", "github.com")
	if err != nil {
		t.Fatalf("applyPrivateAuth: %v", err)
	}
	defer cleanup()

	// GOPRIVATE should include github.com.
	gp := os.Getenv("GOPRIVATE")
	if !strings.Contains(gp, "github.com") {
		t.Errorf("GOPRIVATE should contain github.com, got %q", gp)
	}
}

func TestApplyPrivateAuth_MissingToken(t *testing.T) {
	_, err := applyPrivateAuth("NO_SUCH_TOKEN_VAR_XYZ", "github.com")
	if err == nil {
		t.Fatal("want error for missing token env var")
	}
	if !strings.Contains(err.Error(), "NO_SUCH_TOKEN_VAR_XYZ") {
		t.Errorf("error should mention env var, got: %v", err)
	}
}

func TestApplyPrivateAuth_CleanupRestoresGOPRIVATE(t *testing.T) {
	t.Setenv("TEST_PRIVATE_TOKEN", "tok")
	orig := os.Getenv("GOPRIVATE")

	cleanup, err := applyPrivateAuth("TEST_PRIVATE_TOKEN", "github.com")
	if err != nil {
		t.Fatalf("applyPrivateAuth: %v", err)
	}
	cleanup()

	if got := os.Getenv("GOPRIVATE"); got != orig {
		t.Errorf("GOPRIVATE not restored: want %q got %q", orig, got)
	}
}

func TestInstallFromConfig_WithAuth_SkipsInstalledPrivate(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	// Pre-install the private plugin.
	fakeInstalledPlugin(t, pluginDir, "workflow-plugin-payments", "1.0.0")

	content := `
requires:
  plugins:
    - name: workflow-plugin-payments
      source: github.com/MyOrg/workflow-plugin-payments
      auth:
        env: RELEASES_TOKEN
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("RELEASES_TOKEN", "tok")

	// Already installed → should skip without touching git config.
	if err := installFromWorkflowConfig(cfgPath, pluginDir, ""); err != nil {
		t.Fatalf("installFromWorkflowConfig with auth: %v", err)
	}
}

func TestInstallFromConfig_WithAuth_ErrorsOnMissingToken(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	// Plugin NOT pre-installed → will try to install → auth check fires.

	content := `
requires:
  plugins:
    - name: workflow-plugin-secret
      source: github.com/MyOrg/workflow-plugin-secret
      auth:
        env: MISSING_PRIVATE_TOKEN
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := installFromWorkflowConfig(cfgPath, pluginDir, "")
	if err == nil {
		t.Fatal("want error when auth token is missing")
	}
}
