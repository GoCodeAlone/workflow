package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	fooRaw := pluginRawMap(t, data, "workflow-plugin-foo")
	if _, ok := fooRaw["sha256"]; ok {
		t.Fatalf("workflow-plugin-foo should not contain top-level sha256:\n%s", data)
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

func TestPluginLock_FromManifest_DropsExistingTopLevelSHA256(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	// Pre-populate lockfile with a host-specific binary sha256 from an older
	// generated lockfile. Regenerating the new-format lockfile must not preserve it.
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
	fooRaw := pluginRawMap(t, data, "workflow-plugin-foo")
	if _, ok := fooRaw["sha256"]; ok {
		t.Fatalf("workflow-plugin-foo should not contain top-level sha256:\n%s", data)
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
	if foo.SHA256 != "" {
		t.Errorf("existing top-level sha256 should not be preserved: got %q", foo.SHA256)
	}
	if _, ok := parsed.Plugins["workflow-plugin-bar"]; !ok {
		t.Error("new plugin workflow-plugin-bar not added")
	}
}

func pluginRawMap(t *testing.T, data []byte, name string) map[string]any {
	t.Helper()

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse raw lockfile: %v", err)
	}
	pluginsRaw, ok := raw["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("plugins should be a map in lockfile:\n%s", data)
	}
	pluginRaw, ok := pluginsRaw[name].(map[string]any)
	if !ok {
		t.Fatalf("%s should be a plugin map in lockfile:\n%s", name, data)
	}
	return pluginRaw
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
				{OS: "linux", Arch: "amd64", URL: "https://example.test/foo-linux-amd64.tar.gz", SHA256: sha256Hex([]byte("foo linux amd64 archive"))},
				{OS: "darwin", Arch: "arm64", URL: "https://example.test/foo-darwin-arm64.tar.gz", SHA256: sha256Hex([]byte("foo darwin arm64 archive"))},
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
	if got, want := platforms["linux-amd64"].SHA256, sha256Hex([]byte("foo linux amd64 archive")); got != want {
		t.Fatalf("linux-amd64 SHA256 = %q, want registry archive checksum", got)
	}
	if got, want := platforms["darwin-arm64"].SHA256, sha256Hex([]byte("foo darwin arm64 archive")); got != want {
		t.Fatalf("darwin-arm64 SHA256 = %q, want registry archive checksum", got)
	}
}

func TestPluginLock_FromManifest_RefreshesExistingPlatformSHA256FromRegistry(t *testing.T) {
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
				{OS: "linux", Arch: "amd64", URL: "https://example.test/fresh-linux-amd64.tar.gz", SHA256: sha256Hex([]byte("fresh foo linux amd64 archive"))},
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
	existing := `version: 1
generated_at: 2026-04-26T00:00:00Z
plugins:
  workflow-plugin-foo:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
    sha256: stale-binary-sha
    platforms:
      linux-amd64:
        url: https://example.test/stale-linux-amd64.tar.gz
        sha256: stale-platform-sha
`
	if err := os.WriteFile(lockPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing lockfile: %v", err)
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
			SHA256    string `yaml:"sha256"`
			Platforms map[string]struct {
				URL    string `yaml:"url"`
				SHA256 string `yaml:"sha256"`
			} `yaml:"platforms"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	entry := parsed.Plugins["workflow-plugin-foo"]
	if entry.SHA256 != "" {
		t.Fatalf("platform lock entry should not preserve top-level sha256, got %q", entry.SHA256)
	}
	platform := entry.Platforms["linux-amd64"]
	if platform.URL != "https://example.test/fresh-linux-amd64.tar.gz" {
		t.Fatalf("platform URL = %q, want fresh registry URL", platform.URL)
	}
	if want := sha256Hex([]byte("fresh foo linux amd64 archive")); platform.SHA256 != want {
		t.Fatalf("platform SHA256 = %q, want fresh registry archive checksum", platform.SHA256)
	}
}

func TestPluginLock_FromManifest_FailsWhenExistingPlatformsCannotBeRefreshed(t *testing.T) {
	tests := []struct {
		name     string
		manifest RegistryManifest
		status   int
	}{
		{
			name: "registry error",
			manifest: RegistryManifest{
				Name:    "workflow-plugin-foo",
				Version: "v1.2.3",
			},
			status: http.StatusInternalServerError,
		},
		{
			name: "version mismatch",
			manifest: RegistryManifest{
				Name:    "workflow-plugin-foo",
				Version: "v1.2.2",
				Downloads: []PluginDownload{
					{OS: "linux", Arch: "amd64", URL: "https://example.test/foo-linux-amd64.tar.gz", SHA256: sha256Hex([]byte("foo v1.2.2 linux amd64 archive"))},
				},
			},
			status: http.StatusOK,
		},
		{
			name: "no usable downloads",
			manifest: RegistryManifest{
				Name:    "workflow-plugin-foo",
				Version: "v1.2.3",
				Downloads: []PluginDownload{
					{OS: "linux", Arch: "", URL: "https://example.test/foo-linux-amd64.tar.gz", SHA256: sha256Hex([]byte("foo unusable archive"))},
				},
			},
			status: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			manifestPath := filepath.Join(dir, "wfctl.yaml")
			lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/plugins/workflow-plugin-foo/manifest.json" {
					http.NotFound(w, r)
					return
				}
				if tt.status != http.StatusOK {
					http.Error(w, "registry unavailable", tt.status)
					return
				}
				data, _ := json.Marshal(tt.manifest)
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
			existing := `version: 1
generated_at: 2026-04-26T00:00:00Z
plugins:
  workflow-plugin-foo:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
    platforms:
      linux-amd64:
        url: https://example.test/stale-linux-amd64.tar.gz
        sha256: ` + sha256Hex([]byte("stale foo linux amd64 archive")) + `
`
			if err := os.WriteFile(lockPath, []byte(existing), 0o600); err != nil {
				t.Fatalf("write existing lockfile: %v", err)
			}

			err := runPluginLockFromManifest(manifestPath, lockPath)
			if err == nil {
				t.Fatal("expected plugin lock to fail rather than preserve stale platform metadata")
			}
			if !strings.Contains(err.Error(), "refresh platform metadata") {
				t.Fatalf("error = %q, want clear stale platform refresh failure", err)
			}
			data, readErr := os.ReadFile(lockPath)
			if readErr != nil {
				t.Fatalf("read lockfile: %v", readErr)
			}
			if string(data) != existing {
				t.Fatalf("lockfile was rewritten after failed platform refresh:\n%s", data)
			}
		})
	}
}

func TestPluginLock_FromManifest_RejectsInvalidRegistrySHA256(t *testing.T) {
	tests := []struct {
		name   string
		sha256 string
	}{
		{name: "empty", sha256: ""},
		{name: "short", sha256: "abc123"},
		{name: "non hex", sha256: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
					Version: "v1.2.3",
					Downloads: []PluginDownload{
						{OS: "linux", Arch: "amd64", URL: "https://example.test/foo-linux-amd64.tar.gz", SHA256: tt.sha256},
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

			err := runPluginLockFromManifest(manifestPath, lockPath)
			if err == nil {
				t.Fatal("expected invalid registry sha256 to be rejected")
			}
			if !strings.Contains(err.Error(), "invalid sha256") {
				t.Fatalf("error = %q, want invalid sha256 message", err)
			}
			if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
				t.Fatalf("lockfile should not be written after invalid registry sha256; stat err=%v", statErr)
			}
		})
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
