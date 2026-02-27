package main

import (
	"fmt"
	"os"
	"sort"
)

// MultiRegistry aggregates multiple RegistrySource instances and resolves
// plugins across them in priority order.
type MultiRegistry struct {
	sources []RegistrySource
}

// NewMultiRegistry creates a multi-registry from a config. Sources are sorted
// by priority (lowest number = highest priority).
func NewMultiRegistry(cfg *RegistryConfig) *MultiRegistry {
	// Sort by priority
	sorted := make([]RegistrySourceConfig, len(cfg.Registries))
	copy(sorted, cfg.Registries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	sources := make([]RegistrySource, 0, len(sorted))
	for _, sc := range sorted {
		switch sc.Type {
		case "github":
			sources = append(sources, NewGitHubRegistrySource(sc))
		default:
			// Skip unknown types
			fmt.Fprintf(os.Stderr, "warning: unknown registry type %q for %q, skipping\n", sc.Type, sc.Name)
		}
	}

	return &MultiRegistry{sources: sources}
}

// NewMultiRegistryFromSources creates a multi-registry from pre-built sources (useful for testing).
func NewMultiRegistryFromSources(sources ...RegistrySource) *MultiRegistry {
	return &MultiRegistry{sources: sources}
}

// FetchManifest tries each source in priority order, returning the first successful result.
func (m *MultiRegistry) FetchManifest(name string) (*RegistryManifest, string, error) {
	var lastErr error
	for _, src := range m.sources {
		manifest, err := src.FetchManifest(name)
		if err == nil {
			return manifest, src.Name(), nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("plugin %q not found in any configured registry", name)
}

// SearchPlugins searches all sources and returns deduplicated results.
// When the same plugin appears in multiple registries, the higher-priority source wins.
func (m *MultiRegistry) SearchPlugins(query string) ([]PluginSearchResult, error) {
	seen := make(map[string]bool)
	var results []PluginSearchResult

	for _, src := range m.sources {
		srcResults, err := src.SearchPlugins(query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: search failed for registry %q: %v\n", src.Name(), err)
			continue
		}
		for _, r := range srcResults {
			if !seen[r.Name] {
				results = append(results, r)
				seen[r.Name] = true
			}
		}
	}
	return results, nil
}

// ListPlugins lists all plugins from all sources, deduplicated.
func (m *MultiRegistry) ListPlugins() ([]string, error) {
	seen := make(map[string]bool)
	var names []string

	for _, src := range m.sources {
		srcNames, err := src.ListPlugins()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: list failed for registry %q: %v\n", src.Name(), err)
			continue
		}
		for _, n := range srcNames {
			if !seen[n] {
				names = append(names, n)
				seen[n] = true
			}
		}
	}
	return names, nil
}

// Sources returns the configured registry sources.
func (m *MultiRegistry) Sources() []RegistrySource {
	return m.sources
}
