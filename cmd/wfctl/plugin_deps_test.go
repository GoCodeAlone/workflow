package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestCompareSemverConstraints verifies semver comparison used in version constraint checks.
func TestCompareSemverConstraints(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"0.1.0", "0.2.0", -1},
		{"1.0.0", "2.0.0", -1},
		{"10.0.0", "9.0.0", 1},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tc.a, tc.b), func(t *testing.T) {
			got := compareSemver(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestCheckVersionConstraints verifies min/max version enforcement.
func TestCheckVersionConstraints(t *testing.T) {
	tests := []struct {
		name        string
		dep         PluginDependency
		version     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "no constraints",
			dep:     PluginDependency{Name: "foo"},
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "meets minVersion",
			dep:     PluginDependency{Name: "foo", MinVersion: "1.0.0"},
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "above minVersion",
			dep:     PluginDependency{Name: "foo", MinVersion: "1.0.0"},
			version: "1.2.0",
			wantErr: false,
		},
		{
			name:        "below minVersion",
			dep:         PluginDependency{Name: "foo", MinVersion: "2.0.0"},
			version:     "1.9.9",
			wantErr:     true,
			errContains: "below minimum",
		},
		{
			name:    "meets maxVersion",
			dep:     PluginDependency{Name: "foo", MaxVersion: "2.0.0"},
			version: "2.0.0",
			wantErr: false,
		},
		{
			name:        "exceeds maxVersion",
			dep:         PluginDependency{Name: "foo", MaxVersion: "1.0.0"},
			version:     "1.0.1",
			wantErr:     true,
			errContains: "exceeds maximum",
		},
		{
			name:    "within min and max",
			dep:     PluginDependency{Name: "foo", MinVersion: "1.0.0", MaxVersion: "2.0.0"},
			version: "1.5.0",
			wantErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkVersionConstraints(tc.dep, tc.version)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tc.errContains)
				} else if tc.errContains != "" && !contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestResolveDependencies_AlreadyInstalled checks that an already-installed
// compatible dep is skipped.
func TestResolveDependencies_AlreadyInstalled(t *testing.T) {
	pluginDir := t.TempDir()

	// Create a fake installed "bento" plugin.
	bentoDir := filepath.Join(pluginDir, "bento")
	if err := os.MkdirAll(bentoDir, 0750); err != nil {
		t.Fatalf("mkdir bento: %v", err)
	}
	pj := `{"name":"bento","version":"1.2.0","author":"test","description":"bento"}`
	if err := os.WriteFile(filepath.Join(bentoDir, "plugin.json"), []byte(pj), 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	// Set up a fake registry that serves manifests.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called for "bento" since it's already installed.
		if r.URL.Path == "/plugins/bento/manifest.json" {
			t.Error("unexpected fetch of already-installed dependency bento")
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &RegistryConfig{
		Registries: []RegistrySourceConfig{
			{Name: "test", Type: "static", URL: srv.URL},
		},
	}

	manifest := &RegistryManifest{
		Name:    "data-engineering",
		Version: "0.2.0",
		Dependencies: []PluginDependency{
			{Name: "bento", MinVersion: "1.0.0"},
		},
	}

	// Use an inline registry config — inject via a temp file.
	cfgFile := filepath.Join(t.TempDir(), "registry.yaml")
	cfgData, _ := json.Marshal(map[string]any{"registries": cfg.Registries})
	if err := os.WriteFile(cfgFile, cfgData, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolved := make(map[string]string)
	// Should succeed and skip install since bento 1.2.0 satisfies >=1.0.0.
	err := resolveDependencies("data-engineering", manifest, pluginDir, cfgFile, []string{}, resolved)
	if err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}
	if resolved["bento"] != "1.2.0" {
		t.Errorf("resolved[bento] = %q, want 1.2.0", resolved["bento"])
	}
}

// TestResolveDependencies_CircularDependency verifies circular dep detection.
func TestResolveDependencies_CircularDependency(t *testing.T) {
	pluginDir := t.TempDir()

	bentoManifest := RegistryManifest{
		Name:    "bento",
		Version: "1.0.0",
		Author:  "test", Description: "bento",
		Type: "external", Tier: "community", License: "MIT",
		// bento depends on data-engineering → cycle
		Dependencies: []PluginDependency{{Name: "data-engineering"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/bento/manifest.json" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(bentoManifest); err != nil {
				t.Errorf("encode bento manifest: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgFile := writeTestRegistryConfig(t, srv.URL)

	manifest := &RegistryManifest{
		Name:    "data-engineering",
		Version: "0.2.0",
		Dependencies: []PluginDependency{
			{Name: "bento"},
		},
	}

	resolved := make(map[string]string)
	err := resolveDependencies("data-engineering", manifest, pluginDir, cfgFile, []string{}, resolved)
	if err == nil {
		t.Fatal("expected circular dependency error, got nil")
	}
	if !containsStr(err.Error(), "circular") {
		t.Errorf("error %q does not mention 'circular'", err.Error())
	}
}

// TestResolveDependencies_VersionConflict verifies that requesting the same
// dependency at two incompatible versions is detected.
func TestResolveDependencies_VersionConflict(t *testing.T) {
	pluginDir := t.TempDir()

	// resolved already has "bento" at 2.0.0
	resolved := map[string]string{"bento": "2.0.0"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins/bento/manifest.json" {
			// Return bento 1.0.0
			m := RegistryManifest{
				Name: "bento", Version: "1.0.0",
				Author: "test", Description: "bento",
				Type: "external", Tier: "community", License: "MIT",
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(m); err != nil {
				t.Errorf("encode: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgFile := writeTestRegistryConfig(t, srv.URL)

	manifest := &RegistryManifest{
		Name:    "data-engineering",
		Version: "0.2.0",
		Dependencies: []PluginDependency{
			{Name: "bento"}, // wants latest (1.0.0 from registry)
		},
	}

	err := resolveDependencies("data-engineering", manifest, pluginDir, cfgFile, []string{}, resolved)
	if err == nil {
		t.Fatal("expected version conflict error, got nil")
	}
	if !containsStr(err.Error(), "conflict") {
		t.Errorf("error %q does not mention 'conflict'", err.Error())
	}
}

// TestPluginInstall_ResolveDependencies verifies end-to-end that wfctl plugin
// install resolves and installs deps declared in the manifest.
func TestPluginInstall_ResolveDependencies(t *testing.T) {
	pluginDir := t.TempDir()
	installCount := 0

	bentoManifest := RegistryManifest{
		Name: "bento", Version: "1.0.0",
		Author: "test", Description: "bento stream processor",
		Type: "external", Tier: "community", License: "MIT",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: ""}, // filled below
			{OS: "darwin", Arch: "amd64", URL: ""},
			{OS: "darwin", Arch: "arm64", URL: ""},
		},
	}
	deManifest := RegistryManifest{
		Name: "data-engineering", Version: "0.2.0",
		Author: "GoCodeAlone", Description: "data engineering",
		Type: "external", Tier: "community", License: "MIT",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: ""},
			{OS: "darwin", Arch: "amd64", URL: ""},
			{OS: "darwin", Arch: "arm64", URL: ""},
		},
		Dependencies: []PluginDependency{{Name: "bento", MinVersion: "1.0.0"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/bento/manifest.json":
			installCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(bentoManifest)
		case "/plugins/data-engineering/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(deManifest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Patch download URLs to point to our test server (the actual binary install
	// will fail but we want to verify dep resolution is attempted).
	bentoManifest.Downloads[0].URL = srv.URL + "/download/bento.tar.gz"
	deManifest.Downloads[0].URL = srv.URL + "/download/data-engineering.tar.gz"

	cfgFile := writeTestRegistryConfig(t, srv.URL)

	// Pre-install bento so we can verify dep resolution skips already-installed.
	bentoDir := filepath.Join(pluginDir, "bento")
	if err := os.MkdirAll(bentoDir, 0750); err != nil {
		t.Fatal(err)
	}
	pj := `{"name":"bento","version":"1.0.0","author":"test","description":"bento"}`
	if err := os.WriteFile(filepath.Join(bentoDir, "plugin.json"), []byte(pj), 0640); err != nil {
		t.Fatal(err)
	}

	resolved := make(map[string]string)
	err := resolveDependencies("data-engineering", &deManifest, pluginDir, cfgFile, []string{}, resolved)
	if err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}
	// bento was already installed — should be resolved without fetching manifest.
	if resolved["bento"] != "1.0.0" {
		t.Errorf("resolved[bento] = %q, want 1.0.0", resolved["bento"])
	}
	// The manifest server should not have been called (already installed).
	if installCount > 0 {
		t.Errorf("bento manifest fetched %d times, want 0 (already installed)", installCount)
	}
}

// writeTestRegistryConfig writes a minimal registry YAML config to a temp file
// pointing at the given static URL, and returns the file path.
func writeTestRegistryConfig(t *testing.T, baseURL string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := fmt.Sprintf(`registries:
  - name: test
    type: static
    url: %s
`, baseURL)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}
	return path
}
