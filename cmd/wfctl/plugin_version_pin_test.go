package main

import (
	"bytes"
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

// TestInstallPluginFromManifest_NoDoubleVInSuccessMessage verifies that when a
// manifest's Version field already starts with "v" (e.g. "v0.6.1"), the success
// line printed by installPluginFromManifest reads "Installed X v0.6.1 ..." not
// "Installed X vv0.6.1 ...".
func TestInstallPluginFromManifest_NoDoubleVInSuccessMessage(t *testing.T) {
	const pluginName = "payments"
	const version = "v0.6.1" // manifest stores version WITH v prefix

	binaryContent := []byte("#!/bin/sh\necho payments\n")
	tarball := buildPluginTarGz(t, pluginName, binaryContent, minimalPluginJSON(pluginName, version))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/"+pluginName+"/manifest.json" {
			m := RegistryManifest{
				Name:        pluginName,
				Version:     version, // "v0.6.1" — already has v prefix
				Author:      "tester",
				Description: "test",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{OS: runtime.GOOS, Arch: runtime.GOARCH,
						URL: "http://" + r.Host + "/dl/" + pluginName + ".tar.gz"},
				},
			}
			data, _ := json.Marshal(m)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck
			return
		}
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Write(tarball) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	regCfg := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	regCfgPath := filepath.Join(cfgDir, "registry.yaml")
	if err := os.WriteFile(regCfgPath, []byte(regCfg), 0600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	origWD, _ := os.Getwd()
	_ = os.Chdir(t.TempDir())
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Capture stdout to check the success message.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	pluginsDir := t.TempDir()
	installErr := runPluginInstall([]string{
		"--config", regCfgPath,
		"--plugin-dir", pluginsDir,
		"--skip-checksum", // test server is not a GitHub URL; skip integrity check
		pluginName,
	})

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if installErr != nil {
		t.Fatalf("runPluginInstall: %v", installErr)
	}
	if strings.Contains(output, "vv") {
		t.Errorf("success message contains double-v: %q", output)
	}
	if !strings.Contains(output, "v0.6.1") {
		t.Errorf("success message should contain v0.6.1: %q", output)
	}
}

// TestPinManifestToVersion_VPrefixMismatchSameVersion verifies that when the
// registry manifest stores a version without a "v" prefix (e.g. "0.6.1") but the
// user requests the same version with a "v" prefix (e.g. "@v0.6.1"), pinManifestToVersion
// treats them as equal and makes no changes — preventing the double-v bug where
// the fallback replacement would turn ".../v0.6.1/..." into ".../vv0.6.1/...".
func TestPinManifestToVersion_VPrefixMismatchSameVersion(t *testing.T) {
	origURL := "https://github.com/owner/repo/releases/download/v0.6.1/plugin-linux-amd64.tar.gz"
	manifest := &RegistryManifest{
		Name:    "auth",
		Version: "0.6.1", // registry stores without v prefix
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: origURL, SHA256: "checksum"},
		},
	}

	// User passes @v0.6.1 — same version, just different prefix convention.
	pinManifestToVersion(manifest, "v0.6.1")

	// Version should NOT have been changed (they're the same version).
	// More importantly: URL must not contain "vv0.6.1".
	if strings.Contains(manifest.Downloads[0].URL, "vv0.6.1") {
		t.Errorf("double-v bug: URL contains %q: %s", "vv0.6.1", manifest.Downloads[0].URL)
	}
	if manifest.Downloads[0].URL != origURL {
		t.Errorf("URL should be unchanged for same version: got %q, want %q",
			manifest.Downloads[0].URL, origURL)
	}
	// SHA256 should NOT be cleared because no rewrite happened.
	if manifest.Downloads[0].SHA256 == "" {
		t.Error("SHA256 should not be cleared when version is unchanged")
	}
}

// TestPinManifestToVersion_CrossPrefixPin verifies that when the manifest stores
// a version without "v" (e.g. "0.5.0") and the URL has "v0.5.0", pinning to a
// new version (e.g. "v0.6.1") correctly rewrites the URL to "v0.6.1" without
// introducing a double-v or losing the prefix.
func TestPinManifestToVersion_CrossPrefixPin(t *testing.T) {
	manifest := &RegistryManifest{
		Name:    "auth",
		Version: "0.5.0", // registry stores without v prefix
		Downloads: []PluginDownload{
			{
				OS:   "linux",
				Arch: "amd64",
				// URL uses the standard GitHub release tag format (with v prefix).
				URL:    "https://github.com/owner/repo/releases/download/v0.5.0/auth-linux-amd64.tar.gz",
				SHA256: "oldchecksum",
			},
			{
				OS:   "darwin",
				Arch: "arm64",
				URL:  "https://github.com/owner/repo/releases/download/v0.5.0/auth-darwin-arm64.tar.gz",
			},
		},
	}

	pinManifestToVersion(manifest, "v0.6.1")

	if manifest.Version != "v0.6.1" {
		t.Errorf("manifest.Version: got %q, want %q", manifest.Version, "v0.6.1")
	}
	for i, dl := range manifest.Downloads {
		if strings.Contains(dl.URL, "vv0.6.1") {
			t.Errorf("download[%d]: double-v bug in URL: %s", i, dl.URL)
		}
		if !strings.Contains(dl.URL, "v0.6.1") {
			t.Errorf("download[%d]: URL should contain v0.6.1: %s", i, dl.URL)
		}
		if strings.Contains(dl.URL, "0.5.0") {
			t.Errorf("download[%d]: URL still contains old version 0.5.0: %s", i, dl.URL)
		}
		if dl.SHA256 != "" {
			t.Errorf("download[%d]: SHA256 should be cleared after version pin, got %q", i, dl.SHA256)
		}
	}
}

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

func TestPinManifestToVersion_RewritesVersionedArchiveFilenames(t *testing.T) {
	manifest := &RegistryManifest{
		Name:    "workflow-plugin-auth",
		Version: "0.1.4",
		Downloads: []PluginDownload{
			{
				OS:   "linux",
				Arch: "amd64",
				URL:  "https://github.com/GoCodeAlone/workflow-plugin-auth/releases/download/v0.1.4/workflow-plugin-auth_0.1.4_linux_amd64.tar.gz",
			},
			{
				OS:   "darwin",
				Arch: "arm64",
				URL:  "https://github.com/GoCodeAlone/workflow-plugin-auth/releases/download/v0.1.4/workflow-plugin-auth_0.1.4_darwin_arm64.tar.gz",
			},
		},
	}

	pinManifestToVersion(manifest, "v0.1.5")

	for i, dl := range manifest.Downloads {
		if !strings.Contains(dl.URL, "/releases/download/v0.1.5/") {
			t.Errorf("download[%d].URL: want release tag v0.1.5 in %q", i, dl.URL)
		}
		if !strings.Contains(dl.URL, "workflow-plugin-auth_0.1.5_") {
			t.Errorf("download[%d].URL: want archive filename version 0.1.5 in %q", i, dl.URL)
		}
		if strings.Contains(dl.URL, "0.1.4") {
			t.Errorf("download[%d].URL: old version remained in %q", i, dl.URL)
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
		"--skip-checksum", // test server is not a GitHub URL; skip integrity check
		pluginName,        // no @version
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
		"--skip-checksum", // test server is not a GitHub URL; skip integrity check
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

// TestRunPluginInstall_FullNameNotNormalizedBeforeLookup verifies that when
// runPluginInstall is given "workflow-plugin-auth@v0.1.2" the full name
// "workflow-plugin-auth" is sent to the registry rather than the normalized
// "auth". This prevents collisions where a builtin "auth" entry would be
// returned instead of the external workflow-plugin-auth plugin.
//
// The test server exposes two manifest paths:
//   - /plugins/workflow-plugin-auth/manifest.json → external auth v0.1.2
//   - /plugins/auth/manifest.json                 → builtin auth v0.3.51 (wrong one)
//
// Only the external v0.1.2 tarball exists; hitting the builtin path would
// fail with "no download for linux/amd64" because it has no Downloads entry.
func TestRunPluginInstall_FullNameNotNormalizedBeforeLookup(t *testing.T) {
	const externalVersion = "v0.1.2"
	const builtinVersion = "v0.3.51"

	binaryContent := []byte("#!/bin/sh\necho auth\n")
	// Tarball uses the normalized name "auth" as the binary name (as GoReleaser would).
	tarball := buildPluginTarGz(t, "auth", binaryContent, minimalPluginJSON("auth", externalVersion))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/workflow-plugin-auth/manifest.json":
			// The correct external plugin.
			m := RegistryManifest{
				Name:        "workflow-plugin-auth",
				Version:     externalVersion,
				Author:      "tester",
				Description: "external auth plugin",
				Type:        "external",
				Tier:        "community",
				License:     "MIT",
				Downloads: []PluginDownload{
					{
						OS:   runtime.GOOS,
						Arch: runtime.GOARCH,
						URL:  "http://" + r.Host + "/releases/download/" + externalVersion + "/auth.tar.gz",
					},
				},
			}
			data, _ := json.Marshal(m)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck

		case "/plugins/auth/manifest.json":
			// The builtin plugin — no Downloads, so installing it would fail.
			m := RegistryManifest{
				Name:        "auth",
				Version:     builtinVersion,
				Author:      "engine",
				Description: "builtin auth module",
				Type:        "builtin",
				Tier:        "core",
				License:     "MIT",
				// No Downloads — installing this would return "no download for OS/arch".
			}
			data, _ := json.Marshal(m)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck

		case "/releases/download/" + externalVersion + "/auth.tar.gz":
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
	if err := runPluginInstall([]string{
		"--config", regCfgPath,
		"--plugin-dir", pluginsDir,
		"--skip-checksum", // test server is not a GitHub URL; skip integrity check
		"workflow-plugin-auth@" + externalVersion,
	}); err != nil {
		t.Fatalf("runPluginInstall workflow-plugin-auth: %v", err)
	}

	// Plugin should be installed under the normalized name "auth".
	pjPath := filepath.Join(pluginsDir, "auth", "plugin.json")
	data, err := os.ReadFile(pjPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v — did the builtin path get hit instead?", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if pj.Version != externalVersion {
		t.Errorf("installed version: got %q, want %q (builtin %q would indicate name-collision bug)",
			pj.Version, externalVersion, builtinVersion)
	}
}
