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
	// Mark repo as a git worktree so warnIfPlanNotGitignored activates;
	// .git can be a directory or file (git-worktree pointer) — empty dir is fine.
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
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
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
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

// TestPlan_NoGitignoreFile_NoWarning verifies the warning is silent when
// the repo IS a git worktree but contains no .gitignore yet (a fresh repo
// is more likely to have an unrelated unconfigured tree than a hostile
// "leak my plan" intent).
func TestPlan_NoGitignoreFile_NoWarning(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
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
		t.Errorf("did not expect gitignore warning without .gitignore file, got: %q", stderr)
	}
}

// TestPlan_NoGitWorktree_NoWarning verifies that runInfraPlan stays silent
// when the plan output path is not inside any git worktree — operators
// running plan in /tmp or other untracked locations should not be nagged
// about an unrelated parent .gitignore that happens to live above them.
func TestPlan_NoGitWorktree_NoWarning(t *testing.T) {
	repo := t.TempDir() // intentionally NO .git marker
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
		t.Errorf("did not expect gitignore warning outside any git worktree, got: %q", stderr)
	}
}

// TestGitignoreCovers_ScanError_Propagates verifies that when the underlying
// bufio.Scanner fails (e.g. a single line over bufio.MaxScanTokenSize), the
// helper returns the error to the caller rather than silently treating
// scan-failure as either covered or not-covered. The caller (warnIfPlanNotGitignored)
// then surfaces this as an operator-visible "could not scan" warning.
func TestGitignoreCovers_ScanError_Propagates(t *testing.T) {
	// One contiguous 70 KiB line — well over bufio.MaxScanTokenSize (64 KiB)
	// so Scanner.Scan returns false and Scanner.Err returns the long-line error.
	huge := make([]byte, 70*1024)
	for i := range huge {
		huge[i] = 'x'
	}
	covered, err := gitignoreCovers(huge, "plan.json", "/tmp/plan.json", "/tmp")
	if err == nil {
		t.Fatal("expected non-nil scan error for oversized line; got nil")
	}
	if covered {
		t.Errorf("oversized line should not report covered=true; got %v", covered)
	}
}
