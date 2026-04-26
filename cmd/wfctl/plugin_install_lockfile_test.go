package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	// Create a fake plugin binary at the NORMALIZED path.
	// verifyWfctlLockfileChecksums calls normalizePluginName("workflow-plugin-foo") → "foo",
	// so the binary must live at plugins/foo/foo, not plugins/workflow-plugin-foo/workflow-plugin-foo.
	fooDir := filepath.Join(pluginDir, "foo")
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "foo"), []byte("wrong content"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := verifyWfctlLockfileChecksums(pluginDir, lf)
	if err == nil {
		t.Fatal("expected sha256 mismatch error, got nil")
	}
}

func TestInstallFromWfctlLockfile_UsesCurrentPlatformSHA256(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	const pluginName = "auth"
	binaryContent := []byte("#!/bin/sh\necho auth\n")
	tarball := buildPluginTarGz(t, pluginName, binaryContent, minimalPluginJSON(pluginName, "v1.2.3"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
				SHA256:  strings.Repeat("0", 64),
				Platforms: map[string]config.WfctlLockPlatform{
					currentPlatformKey(): {
						URL:    srv.URL + "/workflow-plugin-auth-" + currentPlatformKey() + ".tar.gz",
						SHA256: sha256Hex(binaryContent),
					},
				},
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	if err := installFromWfctlLockfile(pluginDir, lockPath, lf); err != nil {
		t.Fatalf("installFromWfctlLockfile should use platform checksum instead of top-level checksum: %v", err)
	}

	loaded, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load saved lockfile: %v", err)
	}
	entry := loaded.Plugins["workflow-plugin-auth"]
	if entry.SHA256 != strings.Repeat("0", 64) {
		t.Fatalf("top-level checksum should remain unchanged when current platform checksum exists: got %q", entry.SHA256)
	}
	if got := entry.Platforms[currentPlatformKey()].SHA256; got != sha256Hex(binaryContent) {
		t.Fatalf("current platform checksum = %q, want %q", got, sha256Hex(binaryContent))
	}
}

func TestVerifyWfctlLockfileChecksums_UsesCurrentPlatformSHA256(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	authDir := filepath.Join(pluginDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binaryContent := []byte("#!/bin/sh\necho auth\n")
	if err := os.WriteFile(filepath.Join(authDir, "auth"), binaryContent, 0o755); err != nil {
		t.Fatal(err)
	}

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
				SHA256:  strings.Repeat("0", 64),
				Platforms: map[string]config.WfctlLockPlatform{
					currentPlatformKey(): {
						URL:    "https://example.test/workflow-plugin-auth-" + currentPlatformKey() + ".tar.gz",
						SHA256: sha256Hex(binaryContent),
					},
				},
			},
		},
	}

	if err := verifyWfctlLockfileChecksums(pluginDir, lf); err != nil {
		t.Fatalf("verifyWfctlLockfileChecksums should use platform checksum instead of top-level checksum: %v", err)
	}
}

func TestInstallFromWfctlLockfile_PlatformSHA256IsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	const pluginName = "auth"
	binaryContent := []byte("#!/bin/sh\necho auth\n")
	tarball := buildPluginTarGz(t, pluginName, binaryContent, minimalPluginJSON(pluginName, "v1.2.3"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
				Platforms: map[string]config.WfctlLockPlatform{
					currentPlatformKey(): {
						URL:    srv.URL + "/workflow-plugin-auth-" + currentPlatformKey() + ".tar.gz",
						SHA256: strings.ToUpper(sha256Hex(binaryContent)),
					},
				},
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	if err := installFromWfctlLockfile(pluginDir, lockPath, lf); err != nil {
		t.Fatalf("installFromWfctlLockfile should accept uppercase platform checksum: %v", err)
	}
	if err := verifyWfctlLockfileChecksums(pluginDir, lf); err != nil {
		t.Fatalf("verifyWfctlLockfileChecksums should accept uppercase platform checksum: %v", err)
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
