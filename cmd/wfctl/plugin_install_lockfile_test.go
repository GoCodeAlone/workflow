package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

func TestInstallFromWfctlLockfile_UsesCurrentPlatformSHA256AsArchiveChecksum(t *testing.T) {
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
						SHA256: sha256Hex(tarball),
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
	if entry.SHA256 != "" {
		t.Fatalf("top-level checksum should be omitted when current platform checksum exists: got %q", entry.SHA256)
	}
	if got := entry.Platforms[currentPlatformKey()].SHA256; got != sha256Hex(tarball) {
		t.Fatalf("current platform checksum = %q, want archive checksum %q", got, sha256Hex(tarball))
	}
}

func TestInstallFromWfctlLockfile_PlatformSHA256MismatchFails(t *testing.T) {
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
	tarball := buildPluginTarGz(t, pluginName, []byte("#!/bin/sh\necho auth\n"), minimalPluginJSON(pluginName, "v1.2.3"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	staleLock := `version: 1
generated_at: "2026-01-01T00:00:00Z"
plugins:
  workflow-plugin-auth:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-auth
    sha256: ` + strings.Repeat("0", 64) + `
    platforms:
      ` + currentPlatformKey() + `:
        url: ` + srv.URL + `/workflow-plugin-auth-` + currentPlatformKey() + `.tar.gz
        sha256: ` + strings.Repeat("1", 64) + `
`
	if err := os.WriteFile(lockPath, []byte(staleLock), 0o600); err != nil {
		t.Fatalf("write stale lockfile: %v", err)
	}
	lf, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load stale lockfile: %v", err)
	}

	err = installFromWfctlLockfile(pluginDir, lockPath, lf)
	if err == nil {
		t.Fatal("expected platform archive checksum mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %q, want checksum mismatch detail", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginDir, pluginName, pluginName)); !os.IsNotExist(statErr) {
		t.Fatalf("plugin binary should not be installed after checksum mismatch, stat err: %v", statErr)
	}
	loaded, loadErr := config.LoadWfctlLockfile(lockPath)
	if loadErr != nil {
		t.Fatalf("load scrubbed lockfile: %v", loadErr)
	}
	if got := loaded.Plugins["workflow-plugin-auth"].SHA256; got != "" {
		t.Fatalf("top-level checksum should be scrubbed even when install fails, got %q", got)
	}
}

func TestInstallFromWfctlLockfile_UsesCachedInstallWhenLockMetadataMatches(t *testing.T) {
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

	var downloadHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloadHits.Add(1)
		http.Error(w, "cache should satisfy lockfile install", http.StatusInternalServerError)
	}))
	defer srv.Close()

	const pluginName = "auth"
	installDir := filepath.Join(pluginDir, pluginName)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, pluginName), []byte("#!/bin/sh\necho cached auth\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "plugin.json"), minimalPluginJSON(pluginName, "v1.2.3"), 0o644); err != nil {
		t.Fatal(err)
	}

	plat := config.WfctlLockPlatform{
		URL:    srv.URL + "/workflow-plugin-auth-" + currentPlatformKey() + ".tar.gz",
		SHA256: strings.Repeat("a", 64),
	}
	entry := config.WfctlLockPluginEntry{
		Version: "v1.2.3",
		Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
		Platforms: map[string]config.WfctlLockPlatform{
			currentPlatformKey(): plat,
		},
	}
	meta := lockfileInstallMetadata{
		Version:  entry.Version,
		Source:   entry.Source,
		Platform: currentPlatformKey(),
		URL:      plat.URL,
		SHA256:   plat.SHA256,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, lockfileInstallMetadataName), append(metaData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": entry,
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	if err := installFromWfctlLockfile(pluginDir, lockPath, lf); err != nil {
		t.Fatalf("installFromWfctlLockfile should reuse cached plugin matching lock metadata: %v", err)
	}
	if got := downloadHits.Load(); got != 0 {
		t.Fatalf("download endpoint was hit %d times; cached install should satisfy lockfile", got)
	}
}

func TestInstallFromWfctlLockfile_ScrubsExplicitEmptyTopLevelSHA256(t *testing.T) {
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

	lockWithEmptySHA := `version: 1
generated_at: "2026-01-01T00:00:00Z"
plugins:
  workflow-plugin-auth:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-auth
    sha256:
    platforms:
      ` + currentPlatformKey() + `:
        url: http://127.0.0.1:1/missing.tar.gz
        sha256: ` + strings.Repeat("1", 64) + `
`
	if err := os.WriteFile(lockPath, []byte(lockWithEmptySHA), 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	lf, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}

	err = installFromWfctlLockfile(pluginDir, lockPath, lf)
	if err == nil {
		t.Fatal("expected install failure from unreachable URL")
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	authRaw := pluginRawMap(t, data, "workflow-plugin-auth")
	if _, ok := authRaw["sha256"]; ok {
		t.Fatalf("explicit empty top-level sha256 should be scrubbed:\n%s", data)
	}
}

func TestInstallFromWfctlLockfile_MissingCurrentPlatformDoesNotFallbackToRegistry(t *testing.T) {
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

	var registryHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registryHits.Add(1)
		http.Error(w, "registry must not be used", http.StatusInternalServerError)
	}))
	defer srv.Close()

	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(regCfg), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	otherPlatform := "linux-amd64"
	if otherPlatform == currentPlatformKey() {
		otherPlatform = "darwin-arm64"
	}
	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
				Platforms: map[string]config.WfctlLockPlatform{
					otherPlatform: {
						URL:    "https://example.test/workflow-plugin-auth-" + otherPlatform + ".tar.gz",
						SHA256: sha256Hex([]byte("auth archive for " + otherPlatform)),
					},
				},
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	err = installFromWfctlLockfile(pluginDir, lockPath, lf)
	if err == nil {
		t.Fatal("expected missing current platform error, got nil")
	}
	if !strings.Contains(err.Error(), "missing current platform") || !strings.Contains(err.Error(), currentPlatformKey()) {
		t.Fatalf("error = %q, want clear missing current platform message for %s", err, currentPlatformKey())
	}
	if got := registryHits.Load(); got != 0 {
		t.Fatalf("registry was queried %d times; lockfile platform metadata should be authoritative", got)
	}
}

func TestInstallFromWfctlLockfile_InitialScrubSaveFailureFails(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "missing-dir", ".wfctl-lock.yaml")
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
	tarball := buildPluginTarGz(t, pluginName, []byte("#!/bin/sh\necho auth\n"), minimalPluginJSON(pluginName, "v1.2.3"))
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
						SHA256: sha256Hex(tarball),
					},
				},
			},
		},
	}

	err = installFromWfctlLockfile(pluginDir, lockPath, lf)
	if err == nil {
		t.Fatal("expected lockfile save failure, got nil")
	}
	if !strings.Contains(err.Error(), "persist scrubbed lockfile") {
		t.Fatalf("error = %q, want persist scrubbed lockfile failure", err)
	}
}

func TestInstallFromWfctlLockfile_PostInstallSaveFailureFails(t *testing.T) {
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
	tarball := buildPluginTarGz(t, pluginName, []byte("#!/bin/sh\necho auth\n"), minimalPluginJSON(pluginName, "v1.2.3"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
		if err := os.Chmod(lockPath, 0o400); err != nil {
			t.Errorf("chmod lockfile read-only: %v", err)
		}
	}))
	defer srv.Close()
	t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })

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
						SHA256: sha256Hex(tarball),
					},
				},
			},
		},
	}

	err = installFromWfctlLockfile(pluginDir, lockPath, lf)
	if err == nil {
		t.Fatal("expected post-install lockfile save failure, got nil")
	}
	if !strings.Contains(err.Error(), "persist scrubbed lockfile after installing workflow-plugin-auth") {
		t.Fatalf("error = %q, want post-install scrubbed lockfile persistence failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginDir, pluginName, pluginName)); statErr != nil {
		t.Fatalf("plugin binary should be installed before post-install save failure, stat err: %v", statErr)
	}
}

func TestInstallFromWfctlLockfile_NoPlatformMetadataDoesNotPersistTopLevelSHA256(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/workflow-plugin-auth/manifest.json":
			manifest := RegistryManifest{
				Name:        pluginName,
				Version:     "v1.2.3",
				Repository:  "github.com/GoCodeAlone/workflow-plugin-auth",
				Author:      "tester",
				Description: "test auth plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:     runtime.GOOS,
						Arch:   runtime.GOARCH,
						URL:    "http://" + r.Host + "/releases/download/v1.2.3/auth.tar.gz",
						SHA256: sha256Hex(tarball),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/releases/download/v1.2.3/auth.tar.gz":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(regCfg), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	lf := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now(),
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-auth": {
				Version: "v1.2.3",
				Source:  "github.com/GoCodeAlone/workflow-plugin-auth",
				SHA256:  strings.Repeat("0", 64),
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	if err := installFromWfctlLockfile(pluginDir, lockPath, lf); err != nil {
		t.Fatalf("installFromWfctlLockfile: %v", err)
	}

	loaded, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load saved lockfile: %v", err)
	}
	entry := loaded.Plugins["workflow-plugin-auth"]
	if entry.SHA256 != "" {
		t.Fatalf("top-level checksum should remain empty without platform metadata, got %q", entry.SHA256)
	}
	if len(entry.Platforms) != 0 {
		t.Fatalf("platform metadata should not be synthesized, got %#v", entry.Platforms)
	}
}

func TestRunPluginInstall_DoesNotRewriteNewFormatLockfile(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/workflow-plugin-auth/manifest.json":
			manifest := RegistryManifest{
				Name:        pluginName,
				Version:     "v1.2.3",
				Repository:  "github.com/GoCodeAlone/workflow-plugin-auth",
				Author:      "tester",
				Description: "test auth plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:     runtime.GOOS,
						Arch:   runtime.GOARCH,
						URL:    "http://" + r.Host + "/releases/download/v1.2.3/auth.tar.gz",
						SHA256: sha256Hex(tarball),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case "/releases/download/v1.2.3/auth.tar.gz":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(regCfg), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
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
						URL:    "https://example.test/original.tar.gz",
						SHA256: "archive-sha-from-lock",
					},
				},
			},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	if err := runPluginInstall([]string{"--plugin-dir", pluginDir, "workflow-plugin-auth@v1.2.3"}); err != nil {
		t.Fatalf("runPluginInstall: %v", err)
	}

	loaded, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	entry := loaded.Plugins["workflow-plugin-auth"]
	if entry.SHA256 != "" {
		t.Fatalf("direct install should not write host binary sha into new-format lockfile, got %q", entry.SHA256)
	}
	platform := entry.Platforms[currentPlatformKey()]
	if platform.URL != "https://example.test/original.tar.gz" || platform.SHA256 != "archive-sha-from-lock" {
		t.Fatalf("direct install should not rewrite platform lock data, got %#v", platform)
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
						SHA256: strings.ToUpper(sha256Hex(tarball)),
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
}

func TestUpdateLockfileWithChecksum_GuardSkips(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkipLockfileUpdate = true
	defer func() { installSkipLockfileUpdate = false }()
	updateLockfileWithChecksum("foo", "1.0.0", "", "", "")
	b, _ := os.ReadFile(wfctlLockPath)
	if strings.Contains(string(b), "foo") {
		t.Errorf("guard should have suppressed write; got: %s", b)
	}
}

func TestUpdateLockfileWithChecksum_NewFormatFanOut(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins:\n  bar:\n    version: 0.1.0\n    source: github.com/x/bar\n    platforms:\n      linux_amd64:\n        url: https://example.com/bar\n        sha256: deadbeef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updateLockfileWithChecksum("bar", "1.2.3", "github.com/x/bar-new", "", "feedface")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := lf.Plugins["bar"]
	if got.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", got.Version)
	}
	if got.Source != "github.com/x/bar-new" {
		t.Errorf("Source = %q, want github.com/x/bar-new", got.Source)
	}
	if len(got.Platforms) == 0 {
		t.Errorf("Platforms should be preserved; got empty")
	}
	if got.Platforms["linux_amd64"].URL != "https://example.com/bar" {
		t.Errorf("Platforms URL clobbered: %v", got.Platforms)
	}
}
