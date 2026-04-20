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
)

// TestPinManifestToVersion_URLRewritten verifies that pinManifestToVersion
// replaces the old version string in download URLs and updates manifest.Version.
func TestPinManifestToVersion_URLRewritten(t *testing.T) {
	manifest := &RegistryManifest{
		Name:    "payments",
		Version: "v0.1.0",
		Downloads: []PluginDownload{
			{
				OS:     "linux",
				Arch:   "amd64",
				URL:    "https://github.com/owner/repo/releases/download/v0.1.0/payments-linux-amd64.tar.gz",
				SHA256: "abc123",
			},
			{
				OS:     "darwin",
				Arch:   "arm64",
				URL:    "https://github.com/owner/repo/releases/download/v0.1.0/payments-darwin-arm64.tar.gz",
				SHA256: "def456",
			},
		},
	}

	pinManifestToVersion(manifest, "v0.2.1")

	if manifest.Version != "v0.2.1" {
		t.Errorf("manifest.Version: got %q, want %q", manifest.Version, "v0.2.1")
	}
	for i, dl := range manifest.Downloads {
		if !strings.Contains(dl.URL, "v0.2.1") {
			t.Errorf("download[%d].URL: want v0.2.1 in %q", i, dl.URL)
		}
		if strings.Contains(dl.URL, "v0.1.0") {
			t.Errorf("download[%d].URL: still contains old version v0.1.0 in %q", i, dl.URL)
		}
		if dl.SHA256 != "" {
			t.Errorf("download[%d].SHA256: expected cleared after version pin, got %q", i, dl.SHA256)
		}
	}
}

// TestPinManifestToVersion_SameVersion verifies that no URL rewriting happens
// when the requested version matches the manifest version.
func TestPinManifestToVersion_SameVersion(t *testing.T) {
	origURL := "https://github.com/owner/repo/releases/download/v0.1.0/plugin.tar.gz"
	manifest := &RegistryManifest{
		Name:    "myplugin",
		Version: "v0.1.0",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: origURL, SHA256: "abc"},
		},
	}

	pinManifestToVersion(manifest, "v0.1.0")

	if manifest.Downloads[0].URL != origURL {
		t.Errorf("URL should not change when version matches: got %q", manifest.Downloads[0].URL)
	}
	if manifest.Downloads[0].SHA256 != "abc" {
		t.Errorf("SHA256 should not be cleared when version matches: got %q", manifest.Downloads[0].SHA256)
	}
}

// TestRunPluginInstall_NoVersionUsesManifest verifies that when no @version suffix
// is given, the manifest version is used as-is (existing behavior unchanged).
func TestRunPluginInstall_NoVersionUsesManifest(t *testing.T) {
	const pluginName = "payments"
	const manifestVersion = "v0.1.0"

	binaryContent := []byte("#!/bin/sh\necho payments\n")
	tarball := buildPluginTarGz(t, pluginName, binaryContent, minimalPluginJSON(pluginName, manifestVersion))

	var hitManifestVersion atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/plugins/"+pluginName+"/manifest.json":
			manifest := RegistryManifest{
				Name:        pluginName,
				Version:     manifestVersion,
				Author:      "tester",
				Description: "test payments plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:   runtime.GOOS,
						Arch: runtime.GOARCH,
						URL:  "http://" + r.Host + "/releases/download/" + manifestVersion + "/" + pluginName + ".tar.gz",
					},
				},
			}
			data, _ := json.Marshal(manifest)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck
		case strings.Contains(r.URL.Path, manifestVersion):
			hitManifestVersion.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write(tarball) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	regCfgPath := filepath.Join(cfgDir, "registry.yaml")
	if err := os.WriteFile(regCfgPath, []byte(regCfg), 0600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	pluginsDir := t.TempDir()
	// No @version suffix — should use manifest version unchanged.
	if err := runPluginInstall([]string{
		"--config", regCfgPath,
		"--plugin-dir", pluginsDir,
		pluginName, // no @version
	}); err != nil {
		t.Fatalf("runPluginInstall (no version): %v", err)
	}

	if hitManifestVersion.Load() == 0 {
		t.Error("expected download from manifest version URL, got none")
	}

	// Installed plugin.json should record the manifest version.
	pjPath := filepath.Join(pluginsDir, pluginName, "plugin.json")
	data, err := os.ReadFile(pjPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if pj.Version != manifestVersion {
		t.Errorf("installed version: got %q, want %q", pj.Version, manifestVersion)
	}
}

// TestRunPluginInstall_VersionPinHitsNewURL verifies that when name@vX.Y.Z is
// requested and the registry manifest has an older version, the installer
// rewrites download URLs to the requested version and successfully installs it.
func TestRunPluginInstall_VersionPinHitsNewURL(t *testing.T) {
	const pluginName = "payments"
	const oldVersion = "v0.1.0"
	const newVersion = "v0.2.1"

	binaryContent := []byte("#!/bin/sh\necho payments\n")
	newTarball := buildPluginTarGz(t, pluginName, binaryContent, minimalPluginJSON(pluginName, newVersion))

	var hitNewVersion atomic.Int32
	var hitOldVersion atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/plugins/"+pluginName+"/manifest.json":
			// Registry manifest with old version; download URL points to this server.
			manifest := RegistryManifest{
				Name:        pluginName,
				Version:     oldVersion,
				Author:      "tester",
				Description: "test payments plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:   runtime.GOOS,
						Arch: runtime.GOARCH,
						URL:  "http://" + r.Host + "/releases/download/" + oldVersion + "/" + pluginName + ".tar.gz",
					},
				},
			}
			data, _ := json.Marshal(manifest)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck
		case strings.Contains(r.URL.Path, newVersion):
			hitNewVersion.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write(newTarball) //nolint:errcheck
		case strings.Contains(r.URL.Path, oldVersion) && strings.Contains(r.URL.Path, "releases"):
			hitOldVersion.Add(1)
			http.NotFound(w, r) // old version doesn't exist at download server
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Write a registry config pointing at the test server.
	cfgDir := t.TempDir()
	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	regCfgPath := filepath.Join(cfgDir, "registry.yaml")
	if err := os.WriteFile(regCfgPath, []byte(regCfg), 0600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	// Run install in a temp cwd so .wfctl.yaml lockfile stays isolated.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwdDir := t.TempDir()
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	pluginsDir := t.TempDir()
	err = runPluginInstall([]string{
		"--config", regCfgPath,
		"--plugin-dir", pluginsDir,
		pluginName + "@" + newVersion,
	})
	if err != nil {
		t.Fatalf("runPluginInstall: %v", err)
	}

	// The new version URL must have been hit.
	if hitNewVersion.Load() == 0 {
		t.Error("expected request to new version URL, got none")
	}
	// The old version download URL must NOT have been hit (we switched to new).
	if hitOldVersion.Load() > 0 {
		t.Errorf("expected no request to old version download URL, but got %d", hitOldVersion.Load())
	}

	// Installed plugin.json should record the new version.
	pjPath := filepath.Join(pluginsDir, pluginName, "plugin.json")
	data, err := os.ReadFile(pjPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if pj.Version != newVersion {
		t.Errorf("installed version: got %q, want %q", pj.Version, newVersion)
	}
}

// TestRunPluginInstall_VersionPinNotFound verifies that requesting a non-existent
// version returns an error and does not silently fall back to the registry version.
func TestRunPluginInstall_VersionPinNotFound(t *testing.T) {
	const pluginName = "payments"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/"+pluginName+"/manifest.json" {
			manifest := RegistryManifest{
				Name:        pluginName,
				Version:     "v0.1.0",
				Author:      "tester",
				Description: "test payments plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:   runtime.GOOS,
						Arch: runtime.GOARCH,
						URL:  "http://" + r.Host + "/releases/download/v0.1.0/" + pluginName + ".tar.gz",
					},
				},
			}
			data, _ := json.Marshal(manifest)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck
			return
		}
		// All download URLs return 404 (simulating version not found).
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	regCfgPath := filepath.Join(cfgDir, "registry.yaml")
	if err := os.WriteFile(regCfgPath, []byte(regCfg), 0600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	pluginsDir := t.TempDir()
	err = runPluginInstall([]string{
		"--config", regCfgPath,
		"--plugin-dir", pluginsDir,
		pluginName + "@v99.99.99",
	})
	if err == nil {
		t.Fatal("expected error for non-existent version, got nil (should not silently fall back)")
	}
	// Error should mention the requested version.
	if !strings.Contains(err.Error(), "v99.99.99") {
		t.Errorf("error should mention requested version v99.99.99, got: %v", err)
	}
}
