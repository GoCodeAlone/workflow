package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

func TestInstallFromWfctlLockfile_SHA256MismatchFails(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a lockfile with a non-empty sha256.
	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-foo": {
				Version: "v1.0.0",
				Source:  "github.com/GoCodeAlone/workflow-plugin-foo",
				SHA256:  "expected-sha256-that-wont-match",
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	// Create a fake plugin binary with wrong content.
	fooDir := filepath.Join(pluginDir, "workflow-plugin-foo")
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "workflow-plugin-foo"), []byte("wrong content"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := verifyWfctlLockfileChecksums(pluginDir, lf)
	if err == nil {
		t.Fatal("expected sha256 mismatch error, got nil")
	}
}

func TestInstallFromWfctlLockfile_SHA256EmptySkipsVerification(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-foo": {
				Version: "v1.0.0",
				Source:  "github.com/GoCodeAlone/workflow-plugin-foo",
				SHA256:  "", // empty — skip verification
			},
		},
	}

	// No plugin binary present; should succeed because sha256 is empty.
	err := verifyWfctlLockfileChecksums(pluginDir, lf)
	if err != nil {
		t.Fatalf("expected no error when sha256 is empty, got: %v", err)
	}
}

// TestVerifyWfctlLockfileChecksums_EmptyEntryAlwaysPasses verifies that when a
// plugin's top-level sha256 field is empty, the check is skipped entirely.
// Alpha.1 lockfiles often have empty sha256 (no real download performed), so
// this must be a no-op rather than a hard failure.
func TestVerifyWfctlLockfileChecksums_EmptyEntryAlwaysPasses(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-foo": {
				Version: "v1.0.0",
				SHA256:  "", // empty — skip verification; no binary needed
			},
		},
	}

	// No plugin binary present; should succeed because sha256 is empty.
	err := verifyWfctlLockfileChecksums(pluginDir, lf)
	if err != nil {
		t.Fatalf("expected no error when sha256 is empty, got: %v", err)
	}
}
