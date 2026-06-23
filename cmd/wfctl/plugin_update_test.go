package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestUpdateManifestVersion_UpdatesVersion(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
  - name: workflow-plugin-bar
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-bar
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateManifestVersion("workflow-plugin-foo", "v1.5.0", manifestPath, lockPath); err != nil {
		t.Fatalf("updateManifestVersion: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-foo" {
			if p.Version != "v1.5.0" {
				t.Errorf("version = %q, want v1.5.0", p.Version)
			}
			return
		}
	}
	t.Error("workflow-plugin-foo not found in manifest")
}

func TestUpdateManifestVersion_OtherPluginsUnchanged(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
  - name: workflow-plugin-bar
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-bar
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateManifestVersion("workflow-plugin-foo", "v1.5.0", manifestPath, lockPath); err != nil {
		t.Fatalf("updateManifestVersion: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-bar" && p.Version != "v2.0.0" {
			t.Errorf("workflow-plugin-bar version changed unexpectedly: %q", p.Version)
		}
	}
}

func TestUpdateManifestVersion_NotInManifestReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	err := updateManifestVersion("workflow-plugin-nonexistent", "v1.5.0", manifestPath, lockPath)
	if err == nil {
		t.Fatal("expected error when plugin not in manifest")
	}
}

func TestPluginUpdateGlobalNamedUsesGlobalDir(t *testing.T) {
	cwd := chdirTemp(t)
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "portfolio", "1.0.0")
	writeInstalledPlugin(t, filepath.Join(cwd, "data", "plugins"), "portfolio", "1.0.0")

	tarball := buildPluginTarGz(t, "portfolio", []byte("#!/bin/sh\necho portfolio v2\n"), minimalPluginJSON("portfolio", "v2.0.0"))
	checksum := sha256Hex(tarball)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/portfolio/manifest.json":
			writeHTTPJSON(t, w, testRegistryManifestForRequest(r, "portfolio", "v2.0.0", "/download/portfolio.tar.gz", checksum))
		case "/download/portfolio.tar.gz":
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runPluginUpdate([]string{"-g", "-config", writeTestRegistryConfig(t, srv.URL), "portfolio"}); err != nil {
		t.Fatalf("runPluginUpdate -g portfolio: %v", err)
	}
	if got := readInstalledVersion(filepath.Join(global, "portfolio")); got != "v2.0.0" {
		t.Fatalf("global portfolio version = %q, want v2.0.0", got)
	}
	if got := readInstalledVersion(filepath.Join(cwd, "data", "plugins", "portfolio")); got != "1.0.0" {
		t.Fatalf("project portfolio version = %q, want unchanged 1.0.0", got)
	}
}

func TestPluginUpdateGlobalAllUpdatesInstalledPlugins(t *testing.T) {
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "portfolio", "1.0.0")
	writeInstalledPlugin(t, global, "moderation", "1.0.0")

	portfolioTarball := buildPluginTarGz(t, "portfolio", []byte("#!/bin/sh\necho portfolio v2\n"), minimalPluginJSON("portfolio", "v2.0.0"))
	moderationTarball := buildPluginTarGz(t, "moderation", []byte("#!/bin/sh\necho moderation v2\n"), minimalPluginJSON("moderation", "v2.0.0"))
	portfolioChecksum := sha256Hex(portfolioTarball)
	moderationChecksum := sha256Hex(moderationTarball)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/portfolio/manifest.json":
			writeHTTPJSON(t, w, testRegistryManifestForRequest(r, "portfolio", "v2.0.0", "/download/portfolio.tar.gz", portfolioChecksum))
		case "/plugins/moderation/manifest.json":
			writeHTTPJSON(t, w, testRegistryManifestForRequest(r, "moderation", "v2.0.0", "/download/moderation.tar.gz", moderationChecksum))
		case "/download/portfolio.tar.gz":
			_, _ = w.Write(portfolioTarball)
		case "/download/moderation.tar.gz":
			_, _ = w.Write(moderationTarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runPluginUpdate([]string{"-g", "-config", writeTestRegistryConfig(t, srv.URL), "--all"}); err != nil {
		t.Fatalf("runPluginUpdate -g --all: %v", err)
	}
	for _, name := range []string{"portfolio", "moderation"} {
		if got := readInstalledVersion(filepath.Join(global, name)); got != "v2.0.0" {
			t.Fatalf("%s version = %q, want v2.0.0", name, got)
		}
	}
}

func TestPluginUpdateGlobalAlreadyLatestPrintsVersion(t *testing.T) {
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "portfolio", "v2.0.0")

	tarball := buildPluginTarGz(t, "portfolio", []byte("#!/bin/sh\necho portfolio v2\n"), minimalPluginJSON("portfolio", "v2.0.0"))
	manifest := testRegistryManifest("portfolio", "v2.0.0", "unused", sha256Hex(tarball))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/portfolio/manifest.json" {
			writeHTTPJSON(t, w, manifest)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	out, err := captureStdout(t, func() error {
		return runPluginUpdate([]string{"-g", "-config", writeTestRegistryConfig(t, srv.URL), "portfolio"})
	})
	if err != nil {
		t.Fatalf("runPluginUpdate already latest: %v", err)
	}
	if !strings.Contains(out, "already at latest version (v2.0.0)") {
		t.Fatalf("output = %q, want already latest version", out)
	}
}
