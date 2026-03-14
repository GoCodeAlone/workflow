package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================
// Test 6: StaticRegistrySource
// ============================================================

// buildStaticRegistryServer creates a test HTTP server that serves:
//   - GET /index.json → the provided index entries
//   - GET /plugins/<name>/manifest.json → the manifest for that plugin (if present)
//
// It returns the server and a cleanup function.
func buildStaticRegistryServer(t *testing.T, index []staticIndexEntry, manifests map[string]*RegistryManifest) *httptest.Server {
	t.Helper()
	indexData, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(indexData) //nolint:errcheck
		default:
			// Try to match /plugins/<name>/manifest.json
			var pluginName string
			if _, err := splitPluginManifestPath(r.URL.Path, &pluginName); err == nil {
				if m, ok := manifests[pluginName]; ok {
					data, _ := json.Marshal(m)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write(data) //nolint:errcheck
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	return srv
}

// splitPluginManifestPath parses /plugins/<name>/manifest.json and extracts
// the plugin name. Returns an error if the path does not match.
func splitPluginManifestPath(path string, name *string) (string, error) {
	// path: /plugins/<name>/manifest.json
	const prefix = "/plugins/"
	const suffix = "/manifest.json"
	if len(path) <= len(prefix)+len(suffix) {
		return "", errNotPluginPath
	}
	if path[:len(prefix)] != prefix || path[len(path)-len(suffix):] != suffix {
		return "", errNotPluginPath
	}
	*name = path[len(prefix) : len(path)-len(suffix)]
	if *name == "" {
		return "", errNotPluginPath
	}
	return *name, nil
}

// errNotPluginPath is a sentinel used by splitPluginManifestPath.
var errNotPluginPath = errSentinel("not a plugin manifest path")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// ---------------------------------------------------------------------------

// TestStaticRegistrySource_FetchManifest verifies that FetchManifest fetches
// the correct manifest from the static server.
func TestStaticRegistrySource_FetchManifest(t *testing.T) {
	manifests := map[string]*RegistryManifest{
		"alpha": {
			Name:        "alpha",
			Version:     "1.0.0",
			Author:      "tester",
			Description: "Alpha plugin",
			Type:        "external",
			Tier:        "community",
			License:     "MIT",
		},
	}

	srv := buildStaticRegistryServer(t, nil, manifests)
	defer srv.Close()

	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test-static",
		URL:  srv.URL,
	})

	m, err := src.FetchManifest("alpha")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Name != "alpha" {
		t.Errorf("name: got %q, want %q", m.Name, "alpha")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version: got %q, want %q", m.Version, "1.0.0")
	}
}

// TestStaticRegistrySource_FetchManifest_NotFound verifies that fetching a
// non-existent plugin returns an error.
func TestStaticRegistrySource_FetchManifest_NotFound(t *testing.T) {
	srv := buildStaticRegistryServer(t, nil, map[string]*RegistryManifest{})
	defer srv.Close()

	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test-static",
		URL:  srv.URL,
	})

	_, err := src.FetchManifest("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing plugin, got nil")
	}
}

// TestStaticRegistrySource_ListPlugins verifies that ListPlugins returns
// all plugin names from the index.
func TestStaticRegistrySource_ListPlugins(t *testing.T) {
	index := []staticIndexEntry{
		{Name: "alpha", Version: "1.0.0", Description: "Alpha", Tier: "core"},
		{Name: "beta", Version: "2.0.0", Description: "Beta", Tier: "community"},
		{Name: "gamma", Version: "3.0.0", Description: "Gamma", Tier: "enterprise"},
	}

	srv := buildStaticRegistryServer(t, index, nil)
	defer srv.Close()

	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test-static",
		URL:  srv.URL,
	})

	names, err := src.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 plugins, got %d: %v", len(names), names)
	}

	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !nameSet[want] {
			t.Errorf("expected %q in list, not found", want)
		}
	}
}

// TestStaticRegistrySource_SearchPlugins_AllWithEmptyQuery verifies that an
// empty query returns all index entries.
func TestStaticRegistrySource_SearchPlugins_AllWithEmptyQuery(t *testing.T) {
	index := []staticIndexEntry{
		{Name: "alpha", Version: "1.0.0", Description: "Alpha plugin", Tier: "core"},
		{Name: "beta", Version: "2.0.0", Description: "Beta plugin", Tier: "community"},
	}

	srv := buildStaticRegistryServer(t, index, nil)
	defer srv.Close()

	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test-static",
		URL:  srv.URL,
	})

	results, err := src.SearchPlugins("")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for empty query, got %d", len(results))
	}
}

// TestStaticRegistrySource_SearchPlugins_Filtering verifies that search
// filtering by name and description works correctly.
func TestStaticRegistrySource_SearchPlugins_Filtering(t *testing.T) {
	index := []staticIndexEntry{
		{Name: "cache-plugin", Version: "1.0.0", Description: "Redis cache integration", Tier: "community"},
		{Name: "auth-plugin", Version: "2.0.0", Description: "Authentication and authorization", Tier: "core"},
		{Name: "logger", Version: "1.0.0", Description: "Log aggregation plugin", Tier: "community"},
	}

	srv := buildStaticRegistryServer(t, index, nil)
	defer srv.Close()

	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test-static",
		URL:  srv.URL,
	})

	tests := []struct {
		query       string
		wantCount   int
		wantPlugins []string
	}{
		{query: "cache", wantCount: 1, wantPlugins: []string{"cache-plugin"}},
		{query: "auth", wantCount: 1, wantPlugins: []string{"auth-plugin"}},
		// "logger" has description "Log aggregation plugin" so it also matches "plugin".
		{query: "plugin", wantCount: 3, wantPlugins: []string{"cache-plugin", "auth-plugin", "logger"}},
		{query: "log", wantCount: 1, wantPlugins: []string{"logger"}},
		{query: "CACHE", wantCount: 1, wantPlugins: []string{"cache-plugin"}}, // case-insensitive
		{query: "nonexistent", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run("query="+tt.query, func(t *testing.T) {
			results, err := src.SearchPlugins(tt.query)
			if err != nil {
				t.Fatalf("SearchPlugins(%q): %v", tt.query, err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("SearchPlugins(%q): got %d results, want %d: %v",
					tt.query, len(results), tt.wantCount, results)
				return
			}
			if len(tt.wantPlugins) > 0 {
				resultNames := map[string]bool{}
				for _, r := range results {
					resultNames[r.Name] = true
				}
				for _, want := range tt.wantPlugins {
					if !resultNames[want] {
						t.Errorf("SearchPlugins(%q): expected %q in results", tt.query, want)
					}
				}
			}
		})
	}
}

// TestStaticRegistrySource_SearchPlugins_SourceName verifies that search results
// include the correct Source name.
func TestStaticRegistrySource_SearchPlugins_SourceName(t *testing.T) {
	index := []staticIndexEntry{
		{Name: "myplugin", Version: "1.0.0", Description: "My plugin"},
	}

	srv := buildStaticRegistryServer(t, index, nil)
	defer srv.Close()

	const registryName = "my-static-registry"
	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: registryName,
		URL:  srv.URL,
	})

	results, err := src.SearchPlugins("")
	if err != nil {
		t.Fatalf("SearchPlugins: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Source != registryName {
		t.Errorf("source: got %q, want %q", results[0].Source, registryName)
	}
}

// TestStaticRegistrySource_TrailingSlashStripped verifies that a trailing slash
// in the base URL is stripped and doesn't cause double-slash in URLs.
func TestStaticRegistrySource_TrailingSlashStripped(t *testing.T) {
	index := []staticIndexEntry{
		{Name: "slash-plugin", Version: "1.0.0"},
	}

	srv := buildStaticRegistryServer(t, index, nil)
	defer srv.Close()

	// Pass URL with trailing slash.
	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "test",
		URL:  srv.URL + "/",
	})

	names, err := src.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins with trailing-slash URL: %v", err)
	}
	if len(names) != 1 || names[0] != "slash-plugin" {
		t.Errorf("expected [slash-plugin], got %v", names)
	}
}

// TestStaticRegistrySource_Name verifies that the registry name is returned correctly.
func TestStaticRegistrySource_Name(t *testing.T) {
	src, _ := NewStaticRegistrySource(RegistrySourceConfig{
		Name: "my-registry",
		URL:  "https://example.com",
	})
	if src.Name() != "my-registry" {
		t.Errorf("Name: got %q, want %q", src.Name(), "my-registry")
	}
}
