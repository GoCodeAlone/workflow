package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlan_WarnsOnMissingGitignoreEntry verifies that runInfraPlan emits a
// stderr warning when the plan output path is not covered by .gitignore.
// plan.json carries semi-sensitive content (env-var fingerprints, resolved
// configs) and must not land in version control by default.
func TestPlan_WarnsOnMissingGitignoreEntry(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("# empty\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	cfgPath := filepath.Join(repo, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: vpc
    type: infra.vpc
    config:
      region: nyc1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(repo, "plan.json")

	stderr, fnErr := captureStderr(t, func() error {
		return runInfraPlan([]string{"--config", cfgPath, "--output", planFile})
	})
	if fnErr != nil {
		t.Fatalf("runInfraPlan: %v", fnErr)
	}
	if !strings.Contains(stderr, "plan.json") || !strings.Contains(stderr, "gitignore") {
		t.Errorf("expected gitignore warning mentioning plan.json, got: %q", stderr)
	}
}

// TestPlan_NoWarningWhenGitignored verifies that runInfraPlan stays silent
// (no stderr warning) when the output file is already covered by .gitignore.
func TestPlan_NoWarningWhenGitignored(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("plan.json\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	cfgPath := filepath.Join(repo, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: vpc
    type: infra.vpc
    config:
      region: nyc1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(repo, "plan.json")

	stderr, fnErr := captureStderr(t, func() error {
		return runInfraPlan([]string{"--config", cfgPath, "--output", planFile})
	})
	if fnErr != nil {
		t.Fatalf("runInfraPlan: %v", fnErr)
	}
	if strings.Contains(stderr, "gitignore") {
		t.Errorf("did not expect gitignore warning when plan.json is gitignored, got: %q", stderr)
	}
}

// TestPlan_NoGitignoreFile_NoWarning verifies the warning is silent when no
// .gitignore exists in the repo (no git context — likely not a tracked repo).
func TestPlan_NoGitignoreFile_NoWarning(t *testing.T) {
	repo := t.TempDir()
	cfgPath := filepath.Join(repo, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: vpc
    type: infra.vpc
    config:
      region: nyc1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(repo, "plan.json")

	stderr, fnErr := captureStderr(t, func() error {
		return runInfraPlan([]string{"--config", cfgPath, "--output", planFile})
	})
	if fnErr != nil {
		t.Fatalf("runInfraPlan: %v", fnErr)
	}
	if strings.Contains(stderr, "gitignore") {
		t.Errorf("did not expect gitignore warning without .gitignore file, got: %q", stderr)
	}
}
