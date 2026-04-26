package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPluginLock_FromManifest(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Version string `yaml:"version"`
			Source  string `yaml:"source"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	entry, ok := parsed.Plugins["workflow-plugin-foo"]
	if !ok {
		t.Fatalf("plugin 'workflow-plugin-foo' not found in lockfile; got: %v", parsed.Plugins)
	}
	if entry.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", entry.Version)
	}
	if entry.Source != "github.com/GoCodeAlone/workflow-plugin-foo" {
		t.Errorf("source = %q, want github.com/GoCodeAlone/workflow-plugin-foo", entry.Source)
	}
}

func TestPluginLock_FromManifest_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	// Pre-populate lockfile with sha256 for an existing plugin.
	existingLock := `version: 1
generated_at: "2026-01-01T00:00:00Z"
plugins:
  workflow-plugin-foo:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
    sha256: existing-sha256
`
	if err := os.WriteFile(lockPath, []byte(existingLock), 0o600); err != nil {
		t.Fatalf("write existing lockfile: %v", err)
	}

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
  - name: workflow-plugin-bar
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-bar
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Version string `yaml:"version"`
			SHA256  string `yaml:"sha256"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	foo := parsed.Plugins["workflow-plugin-foo"]
	if foo.SHA256 != "existing-sha256" {
		t.Errorf("existing sha256 not preserved: got %q, want existing-sha256", foo.SHA256)
	}
	if _, ok := parsed.Plugins["workflow-plugin-bar"]; !ok {
		t.Error("new plugin workflow-plugin-bar not added")
	}
}

func TestPluginLock_FromManifest_PopulatesPlatformURLsAndSHA256FromRegistry(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plugins/workflow-plugin-foo/manifest.json" {
			http.NotFound(w, r)
			return
		}
		manifest := RegistryManifest{
			Name:       "workflow-plugin-foo",
			Version:    "v1.2.3",
			Repository: "github.com/GoCodeAlone/workflow-plugin-foo",
			Downloads: []PluginDownload{
				{OS: "linux", Arch: "amd64", URL: "https://example.test/foo-linux-amd64.tar.gz", SHA256: "archive-sha-linux"},
				{OS: "darwin", Arch: "arm64", URL: "https://example.test/foo-darwin-arm64.tar.gz", SHA256: "archive-sha-darwin"},
			},
		}
		data, _ := json.Marshal(manifest)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
	}))
	defer srv.Close()

	registryConfig := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(registryConfig), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Platforms map[string]struct {
				URL    string `yaml:"url"`
				SHA256 string `yaml:"sha256"`
			} `yaml:"platforms"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	platforms := parsed.Plugins["workflow-plugin-foo"].Platforms
	if got := platforms["linux-amd64"].URL; got != "https://example.test/foo-linux-amd64.tar.gz" {
		t.Fatalf("linux-amd64 URL = %q, want registry URL", got)
	}
	if got := platforms["darwin-arm64"].URL; got != "https://example.test/foo-darwin-arm64.tar.gz" {
		t.Fatalf("darwin-arm64 URL = %q, want registry URL", got)
	}
	if got := platforms["linux-amd64"].SHA256; got != "archive-sha-linux" {
		t.Fatalf("linux-amd64 SHA256 = %q, want registry archive checksum", got)
	}
	if got := platforms["darwin-arm64"].SHA256; got != "archive-sha-darwin" {
		t.Fatalf("darwin-arm64 SHA256 = %q, want registry archive checksum", got)
	}
}

func TestPluginLock_FromManifest_DoesNotUseHomeOrDefaultRegistry(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("plugin lock should not query registry config outside the manifest or lockfile directory: %s", r.URL.Path)
	}))
	defer srv.Close()

	homeConfigPath := filepath.Join(homeDir, ".config", "wfctl", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(homeConfigPath), 0o750); err != nil {
		t.Fatalf("create home config dir: %v", err)
	}
	homeRegistryConfig := "registries:\n  - name: home\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(homeConfigPath, []byte(homeRegistryConfig), 0o600); err != nil {
		t.Fatalf("write home registry config: %v", err)
	}
	t.Setenv("HOME", homeDir)

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Platforms map[string]struct {
				URL string `yaml:"url"`
			} `yaml:"platforms"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	if platforms := parsed.Plugins["workflow-plugin-foo"].Platforms; len(platforms) != 0 {
		t.Fatalf("platforms = %v, want no registry enrichment without project-local registry config", platforms)
	}
}

func TestPluginLock_FromManifest_SkipsRegistryPlatformURLsForVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plugins/workflow-plugin-foo/manifest.json" {
			http.NotFound(w, r)
			return
		}
		manifest := RegistryManifest{
			Name:    "workflow-plugin-foo",
			Version: "v1.2.2",
			Downloads: []PluginDownload{
				{OS: "linux", Arch: "amd64", URL: "https://cdn.example.test/releases/v1.2.2/foo-linux-amd64.tar.gz"},
			},
		}
		data, _ := json.Marshal(manifest)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
	}))
	defer srv.Close()

	registryConfig := "registries:\n  - name: test\n    type: static\n    url: " + srv.URL + "\n    priority: 0\n"
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(registryConfig), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Platforms map[string]struct {
				URL string `yaml:"url"`
			} `yaml:"platforms"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	if platforms := parsed.Plugins["workflow-plugin-foo"].Platforms; len(platforms) != 0 {
		t.Fatalf("platforms = %v, want no registry enrichment when manifest version mismatches requested version", platforms)
	}
}
