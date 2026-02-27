package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// mockRegistrySource — in-process RegistrySource backed by a map of manifests.
// Shared with plugin_install_e2e_test.go which uses this type via the manifests field.
// ---------------------------------------------------------------------------

type mockRegistrySource struct {
	name      string
	manifests map[string]*RegistryManifest
	listErr   error
	fetchErr  map[string]error
}

func (m *mockRegistrySource) Name() string { return m.name }

func (m *mockRegistrySource) ListPlugins() ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	names := make([]string, 0, len(m.manifests))
	for k := range m.manifests {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

func (m *mockRegistrySource) FetchManifest(name string) (*RegistryManifest, error) {
	if m.fetchErr != nil {
		if err, ok := m.fetchErr[name]; ok && err != nil {
			return nil, err
		}
	}
	manifest, ok := m.manifests[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found in registry %s", name, m.name)
	}
	return manifest, nil
}

func (m *mockRegistrySource) SearchPlugins(query string) ([]PluginSearchResult, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var results []PluginSearchResult
	for _, manifest := range m.manifests {
		if matchesRegistryQuery(manifest, query) {
			results = append(results, PluginSearchResult{
				PluginSummary: PluginSummary{
					Name:        manifest.Name,
					Version:     manifest.Version,
					Description: manifest.Description,
					Tier:        manifest.Tier,
				},
				Source: m.name,
			})
		}
	}
	// Sort for determinism
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

// ---------------------------------------------------------------------------
// Registry config tests
// ---------------------------------------------------------------------------

func TestDefaultRegistryConfig(t *testing.T) {
	cfg := DefaultRegistryConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	r := cfg.Registries[0]
	if r.Name != "default" {
		t.Errorf("name: got %q, want %q", r.Name, "default")
	}
	if r.Type != "github" {
		t.Errorf("type: got %q, want %q", r.Type, "github")
	}
	if r.Owner != registryOwner {
		t.Errorf("owner: got %q, want %q", r.Owner, registryOwner)
	}
	if r.Repo != registryRepo {
		t.Errorf("repo: got %q, want %q", r.Repo, registryRepo)
	}
	if r.Branch != registryBranch {
		t.Errorf("branch: got %q, want %q", r.Branch, registryBranch)
	}
	if r.Priority != 0 {
		t.Errorf("priority: got %d, want 0", r.Priority)
	}
}

func TestLoadRegistryConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `registries:
  - name: my-org
    type: github
    owner: my-org
    repo: my-plugins
    branch: stable
    priority: 1
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0640); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRegistryConfig: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	r := cfg.Registries[0]
	if r.Name != "my-org" {
		t.Errorf("name: got %q, want %q", r.Name, "my-org")
	}
	if r.Owner != "my-org" {
		t.Errorf("owner: got %q, want %q", r.Owner, "my-org")
	}
	if r.Repo != "my-plugins" {
		t.Errorf("repo: got %q, want %q", r.Repo, "my-plugins")
	}
	if r.Branch != "stable" {
		t.Errorf("branch: got %q, want %q", r.Branch, "stable")
	}
	if r.Priority != 1 {
		t.Errorf("priority: got %d, want 1", r.Priority)
	}
}

func TestLoadRegistryConfigDefault(t *testing.T) {
	// Provide a path that does not exist — should fall back to default.
	cfg, err := LoadRegistryConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadRegistryConfig: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (default), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Owner != registryOwner {
		t.Errorf("owner: got %q, want %q", cfg.Registries[0].Owner, registryOwner)
	}
}

func TestSaveAndLoadRegistryConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "wfctl", "config.yaml")

	original := &RegistryConfig{
		Registries: []RegistrySourceConfig{
			{Name: "primary", Type: "github", Owner: "acme", Repo: "plugins", Branch: "main", Priority: 0},
			{Name: "secondary", Type: "github", Owner: "acme", Repo: "more-plugins", Branch: "dev", Priority: 5},
		},
	}

	if err := SaveRegistryConfig(cfgPath, original); err != nil {
		t.Fatalf("SaveRegistryConfig: %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	loaded, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRegistryConfig: %v", err)
	}
	if len(loaded.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(loaded.Registries))
	}
	for i, want := range original.Registries {
		got := loaded.Registries[i]
		if got.Name != want.Name {
			t.Errorf("[%d] name: got %q, want %q", i, got.Name, want.Name)
		}
		if got.Owner != want.Owner {
			t.Errorf("[%d] owner: got %q, want %q", i, got.Owner, want.Owner)
		}
		if got.Repo != want.Repo {
			t.Errorf("[%d] repo: got %q, want %q", i, got.Repo, want.Repo)
		}
		if got.Branch != want.Branch {
			t.Errorf("[%d] branch: got %q, want %q", i, got.Branch, want.Branch)
		}
		if got.Priority != want.Priority {
			t.Errorf("[%d] priority: got %d, want %d", i, got.Priority, want.Priority)
		}
	}
}

// ---------------------------------------------------------------------------
// Mock registry source tests
// ---------------------------------------------------------------------------

func TestMockRegistrySource(t *testing.T) {
	src := &mockRegistrySource{
		name: "test",
		manifests: map[string]*RegistryManifest{
			"alpha": {Name: "alpha", Version: "1.0.0", Description: "Alpha plugin", Tier: "core"},
			"beta":  {Name: "beta", Version: "2.0.0", Description: "Beta plugin", Tier: "community"},
		},
	}

	if src.Name() != "test" {
		t.Errorf("Name: got %q, want %q", src.Name(), "test")
	}

	// ListPlugins
	names, err := src.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(names))
	}

	// FetchManifest success
	m, err := src.FetchManifest("alpha")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Name != "alpha" {
		t.Errorf("name: got %q, want %q", m.Name, "alpha")
	}

	// FetchManifest not found
	_, err = src.FetchManifest("nonexistent")
	if err == nil {
		t.Error("expected error for missing plugin")
	}

	// SearchPlugins — empty query returns all
	results, err := src.SearchPlugins("")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// SearchPlugins — filtered query
	results, err = src.SearchPlugins("alpha")
	if err != nil {
		t.Fatalf("SearchPlugins(alpha): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "alpha" {
		t.Errorf("result name: got %q, want %q", results[0].Name, "alpha")
	}
	if results[0].Source != "test" {
		t.Errorf("source: got %q, want %q", results[0].Source, "test")
	}
}

// ---------------------------------------------------------------------------
// MultiRegistry tests
// ---------------------------------------------------------------------------

func TestMultiRegistryFetchPriority(t *testing.T) {
	// Source A has "shared-plugin" version 1.0.0 (higher priority — listed first).
	// Source B has "shared-plugin" version 2.0.0 (lower priority).
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{
			"shared-plugin": {Name: "shared-plugin", Version: "1.0.0"},
		},
	}
	srcB := &mockRegistrySource{
		name: "secondary",
		manifests: map[string]*RegistryManifest{
			"shared-plugin": {Name: "shared-plugin", Version: "2.0.0"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	manifest, source, err := mr.FetchManifest("shared-plugin")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if source != "primary" {
		t.Errorf("source: got %q, want %q", source, "primary")
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("version: got %q, want %q (higher-priority source should win)", manifest.Version, "1.0.0")
	}
}

func TestMultiRegistryFetchFallback(t *testing.T) {
	// Source A errors for "unique-plugin", source B has it.
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{},
		fetchErr: map[string]error{
			"unique-plugin": fmt.Errorf("not found"),
		},
	}
	srcB := &mockRegistrySource{
		name: "secondary",
		manifests: map[string]*RegistryManifest{
			"unique-plugin": {Name: "unique-plugin", Version: "3.0.0"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	manifest, source, err := mr.FetchManifest("unique-plugin")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if source != "secondary" {
		t.Errorf("source: got %q, want %q", source, "secondary")
	}
	if manifest.Version != "3.0.0" {
		t.Errorf("version: got %q, want %q", manifest.Version, "3.0.0")
	}
}

func TestMultiRegistryFetchNotFound(t *testing.T) {
	srcA := &mockRegistrySource{
		name:      "primary",
		manifests: map[string]*RegistryManifest{},
	}
	mr := NewMultiRegistryFromSources(srcA)
	_, _, err := mr.FetchManifest("does-not-exist")
	if err == nil {
		t.Fatal("expected error when plugin not found in any registry")
	}
}

func TestMultiRegistrySearchDedup(t *testing.T) {
	// Both sources have "dup-plugin". Result should only appear once with the
	// higher-priority source's metadata.
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{
			"dup-plugin": {Name: "dup-plugin", Version: "1.0.0", Description: "from primary", Tier: "core"},
		},
	}
	srcB := &mockRegistrySource{
		name: "secondary",
		manifests: map[string]*RegistryManifest{
			"dup-plugin": {Name: "dup-plugin", Version: "9.9.9", Description: "from secondary", Tier: "community"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	results, err := mr.SearchPlugins("")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(results))
	}
	if results[0].Source != "primary" {
		t.Errorf("source: got %q, want %q (higher-priority source should win)", results[0].Source, "primary")
	}
	if results[0].Version != "1.0.0" {
		t.Errorf("version: got %q, want %q", results[0].Version, "1.0.0")
	}
}

func TestMultiRegistrySearchMerge(t *testing.T) {
	// Each source has a distinct plugin. Both should appear in results.
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{
			"plugin-a": {Name: "plugin-a", Version: "1.0.0", Description: "A plugin"},
		},
	}
	srcB := &mockRegistrySource{
		name: "secondary",
		manifests: map[string]*RegistryManifest{
			"plugin-b": {Name: "plugin-b", Version: "2.0.0", Description: "B plugin"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	results, err := mr.SearchPlugins("")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["plugin-a"] {
		t.Error("expected plugin-a in results")
	}
	if !names["plugin-b"] {
		t.Error("expected plugin-b in results")
	}
}

func TestMultiRegistrySearchFilteredQuery(t *testing.T) {
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{
			"cache-plugin": {Name: "cache-plugin", Version: "1.0.0", Description: "Redis cache integration"},
			"auth-plugin":  {Name: "auth-plugin", Version: "1.0.0", Description: "Authentication plugin"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA)
	results, err := mr.SearchPlugins("cache")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for query %q, got %d", "cache", len(results))
	}
	if results[0].Name != "cache-plugin" {
		t.Errorf("result name: got %q, want %q", results[0].Name, "cache-plugin")
	}
}

func TestMultiRegistryListDedup(t *testing.T) {
	// Both sources share "shared" and each has a unique plugin.
	srcA := &mockRegistrySource{
		name: "primary",
		manifests: map[string]*RegistryManifest{
			"shared":   {Name: "shared"},
			"only-in-a": {Name: "only-in-a"},
		},
	}
	srcB := &mockRegistrySource{
		name: "secondary",
		manifests: map[string]*RegistryManifest{
			"shared":   {Name: "shared"},
			"only-in-b": {Name: "only-in-b"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	names, err := mr.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 deduplicated plugins, got %d: %v", len(names), names)
	}
	seen := map[string]int{}
	for _, n := range names {
		seen[n]++
	}
	for _, name := range []string{"shared", "only-in-a", "only-in-b"} {
		if seen[name] != 1 {
			t.Errorf("expected %q exactly once, got %d times", name, seen[name])
		}
	}
}

func TestMultiRegistryListSkipsFailedSources(t *testing.T) {
	srcA := &mockRegistrySource{
		name:    "broken",
		listErr: fmt.Errorf("network error"),
	}
	srcB := &mockRegistrySource{
		name: "working",
		manifests: map[string]*RegistryManifest{
			"good-plugin": {Name: "good-plugin"},
		},
	}

	mr := NewMultiRegistryFromSources(srcA, srcB)
	names, err := mr.ListPlugins()
	// MultiRegistry swallows per-source errors; overall call should succeed.
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(names) != 1 || names[0] != "good-plugin" {
		t.Errorf("expected [good-plugin], got %v", names)
	}
}

// ---------------------------------------------------------------------------
// ValidateManifest tests
// ---------------------------------------------------------------------------

func validManifest() *RegistryManifest {
	return &RegistryManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "Test Author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: "https://example.com/plugin-linux-amd64.tar.gz"},
		},
	}
}

func TestValidateManifest_Valid(t *testing.T) {
	m := validManifest()
	errs := ValidateManifest(m, ValidationOptions{})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateManifest_RequiredFields(t *testing.T) {
	m := &RegistryManifest{} // all empty
	errs := ValidateManifest(m, ValidationOptions{})

	requiredFields := []string{"name", "version", "author", "description", "type", "tier", "license"}
	errFields := map[string]bool{}
	for _, e := range errs {
		errFields[e.Field] = true
	}
	for _, field := range requiredFields {
		if !errFields[field] {
			t.Errorf("expected validation error for field %q, but none found", field)
		}
	}
}

func TestValidateManifest_InvalidEnums(t *testing.T) {
	m := validManifest()
	m.Type = "invalid-type"
	m.Tier = "invalid-tier"

	errs := ValidateManifest(m, ValidationOptions{})
	errFields := map[string]bool{}
	for _, e := range errs {
		errFields[e.Field] = true
	}
	if !errFields["type"] {
		t.Error("expected validation error for invalid type")
	}
	if !errFields["tier"] {
		t.Error("expected validation error for invalid tier")
	}
}

func TestValidateManifest_SemverFormat(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		{"1.0.0", false},
		{"0.2.18", false},
		{"10.100.1000", false},
		{"1.0.0-beta", false}, // semverRegex uses prefix match so this passes
		{"1.0", true},
		{"abc", true},
		{"", true}, // empty triggers "required" error, not format error
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			m := validManifest()
			m.Version = tt.version
			errs := ValidateManifest(m, ValidationOptions{})
			hasVersionErr := false
			for _, e := range errs {
				if e.Field == "version" {
					hasVersionErr = true
					break
				}
			}
			if tt.wantErr && !hasVersionErr {
				t.Errorf("version %q: expected validation error, got none", tt.version)
			}
			if !tt.wantErr && hasVersionErr {
				t.Errorf("version %q: unexpected validation error", tt.version)
			}
		})
	}
}

func TestValidateManifest_DownloadValidation(t *testing.T) {
	t.Run("invalid OS", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].OS = "freebsd"
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads[0].os" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for invalid OS")
		}
	})

	t.Run("invalid arch", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].Arch = "386"
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads[0].arch" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for invalid arch")
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].URL = ""
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads[0].url" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for missing download URL")
		}
	})

	t.Run("external type without downloads", func(t *testing.T) {
		m := validManifest()
		m.Type = "external"
		m.Downloads = nil
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads" {
				found = true
			}
		}
		if !found {
			t.Error("expected error: external plugins must have downloads")
		}
	})

	t.Run("builtin type without downloads is ok", func(t *testing.T) {
		m := validManifest()
		m.Type = "builtin"
		m.Downloads = nil
		errs := ValidateManifest(m, ValidationOptions{})
		for _, e := range errs {
			if e.Field == "downloads" {
				t.Errorf("unexpected downloads error for builtin type: %s", e.Message)
			}
		}
	})
}

func TestValidateManifest_EngineVersionCompat(t *testing.T) {
	t.Run("engine too old", func(t *testing.T) {
		m := validManifest()
		m.MinEngineVersion = "2.0.0"
		errs := ValidateManifest(m, ValidationOptions{EngineVersion: "1.9.0"})
		found := false
		for _, e := range errs {
			if e.Field == "minEngineVersion" {
				found = true
			}
		}
		if !found {
			t.Error("expected error: engine version too old")
		}
	})

	t.Run("engine meets minimum", func(t *testing.T) {
		m := validManifest()
		m.MinEngineVersion = "1.0.0"
		errs := ValidateManifest(m, ValidationOptions{EngineVersion: "1.0.0"})
		for _, e := range errs {
			if e.Field == "minEngineVersion" {
				t.Errorf("unexpected error: %s", e.Message)
			}
		}
	})

	t.Run("engine exceeds minimum", func(t *testing.T) {
		m := validManifest()
		m.MinEngineVersion = "1.0.0"
		errs := ValidateManifest(m, ValidationOptions{EngineVersion: "2.0.0"})
		for _, e := range errs {
			if e.Field == "minEngineVersion" {
				t.Errorf("unexpected error: %s", e.Message)
			}
		}
	})

	t.Run("no engine version in opts — no check", func(t *testing.T) {
		m := validManifest()
		m.MinEngineVersion = "99.0.0"
		errs := ValidateManifest(m, ValidationOptions{}) // EngineVersion is empty
		for _, e := range errs {
			if e.Field == "minEngineVersion" {
				t.Errorf("unexpected error when EngineVersion not set: %s", e.Message)
			}
		}
	})
}

func TestValidateManifest_SHA256Format(t *testing.T) {
	t.Run("valid sha256", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].SHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
		errs := ValidateManifest(m, ValidationOptions{})
		for _, e := range errs {
			if e.Field == "downloads[0].sha256" {
				t.Errorf("unexpected sha256 error: %s", e.Message)
			}
		}
	})

	t.Run("invalid sha256 — too short", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].SHA256 = "abc123"
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads[0].sha256" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for invalid sha256 format")
		}
	})

	t.Run("invalid sha256 — non-hex characters", func(t *testing.T) {
		m := validManifest()
		// 64 chars but contains non-hex (z)
		m.Downloads[0].SHA256 = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		errs := ValidateManifest(m, ValidationOptions{})
		found := false
		for _, e := range errs {
			if e.Field == "downloads[0].sha256" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for non-hex sha256")
		}
	})

	t.Run("empty sha256 is allowed", func(t *testing.T) {
		m := validManifest()
		m.Downloads[0].SHA256 = ""
		errs := ValidateManifest(m, ValidationOptions{})
		for _, e := range errs {
			if e.Field == "downloads[0].sha256" {
				t.Errorf("unexpected sha256 error for empty value: %s", e.Message)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// compareSemver tests
// ---------------------------------------------------------------------------

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal
		{"0.0.0", "0.0.0", 0},
		{"1.0.0", "1.0.0", 0},
		{"1.2.3", "1.2.3", 0},
		// a < b
		{"0.0.0", "0.0.1", -1},
		{"0.0.9", "0.1.0", -1},
		{"0.9.9", "1.0.0", -1},
		{"1.0.0", "2.0.0", -1},
		{"1.9.9", "2.0.0", -1},
		{"0.2.17", "0.2.18", -1},
		// a > b
		{"0.0.1", "0.0.0", 1},
		{"0.1.0", "0.0.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"0.2.18", "0.2.17", 1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tt.a, tt.b), func(t *testing.T) {
			got := compareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadRegistryConfig — branch default fill-in
// ---------------------------------------------------------------------------

func TestLoadRegistryConfigFillsDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// branch omitted — should default to "main"
	yaml := `registries:
  - name: no-branch
    type: github
    owner: acme
    repo: plugins
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0640); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRegistryConfig: %v", err)
	}
	if cfg.Registries[0].Branch != "main" {
		t.Errorf("branch: got %q, want %q", cfg.Registries[0].Branch, "main")
	}
}

func TestLoadRegistryConfigFillsDefaultType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// type omitted — should default to "github"
	yaml := `registries:
  - name: no-type
    owner: acme
    repo: plugins
    branch: main
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0640); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRegistryConfig: %v", err)
	}
	if cfg.Registries[0].Type != "github" {
		t.Errorf("type: got %q, want %q", cfg.Registries[0].Type, "github")
	}
}

// ---------------------------------------------------------------------------
// NewMultiRegistry priority sorting
// ---------------------------------------------------------------------------

func TestNewMultiRegistryPriorityOrder(t *testing.T) {
	// Build a RegistryConfig with two entries; higher numeric priority = lower
	// precedence. NewMultiRegistry should sort by Priority ascending.
	cfg := &RegistryConfig{
		Registries: []RegistrySourceConfig{
			{Name: "low-prio", Type: "github", Owner: "acme", Repo: "b", Branch: "main", Priority: 10},
			{Name: "high-prio", Type: "github", Owner: "acme", Repo: "a", Branch: "main", Priority: 0},
		},
	}

	mr := NewMultiRegistry(cfg)
	sources := mr.Sources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Name() != "high-prio" {
		t.Errorf("first source: got %q, want %q", sources[0].Name(), "high-prio")
	}
	if sources[1].Name() != "low-prio" {
		t.Errorf("second source: got %q, want %q", sources[1].Name(), "low-prio")
	}
}
