package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
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
	for i := range sorted {
		switch sorted[i].Type {
		case "github":
			sources = append(sources, NewGitHubRegistrySource(sorted[i]))
		case "static":
			src, err := NewStaticRegistrySource(sorted[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v, skipping\n", err)
				continue
			}
			sources = append(sources, src)
		default:
			// Skip unknown types
			fmt.Fprintf(os.Stderr, "warning: unknown registry type %q for %q, skipping\n", sorted[i].Type, sorted[i].Name)
		}
	}

	return &MultiRegistry{sources: sources}
}

// NewMultiRegistryFromSources creates a multi-registry from pre-built sources (useful for testing).
func NewMultiRegistryFromSources(sources ...RegistrySource) *MultiRegistry {
	return &MultiRegistry{sources: sources}
}

// normalizePluginName strips the "workflow-plugin-" prefix from a plugin name
// so that users can refer to plugins by their short name (e.g. "authz") or
// full name (e.g. "workflow-plugin-authz") interchangeably.
func normalizePluginName(name string) string {
	return strings.TrimPrefix(name, "workflow-plugin-")
}

// debugRegistryLog is true when the WFCTL_DEBUG environment variable is non-empty.
// It enables per-source trace logging in FetchManifest to aid CI diagnostics.
var debugRegistryLog = os.Getenv("WFCTL_DEBUG") != ""

// FetchManifest tries each source in priority order, returning the first successful result.
// It first tries the original name across all sources; if the original name differs from
// its normalized form (after stripping the "workflow-plugin-" prefix) and no source
// matched the original, it retries with the normalized name as a fallback.
//
// Trying the original name first prevents name collisions where both "auth" (a builtin
// module plugin) and "workflow-plugin-auth" (an external plugin) exist in the registry —
// the caller's intent is respected rather than conflating the two.
//
// Set WFCTL_DEBUG=1 to enable per-source trace logging on stderr.
func (m *MultiRegistry) FetchManifest(name string) (*RegistryManifest, string, error) {
	// Guard against misconfigured / empty registries early so the error message
	// is actionable rather than "not found in any configured registry" with no
	// hint about why.
	if len(m.sources) == 0 {
		return nil, "", fmt.Errorf("plugin %q not found: no registry sources configured"+
			" (missing .wfctl.yaml? run `wfctl registry list` or set WFCTL_DEBUG=1)", name)
	}

	normalized := normalizePluginName(name)
	if debugRegistryLog {
		fmt.Fprintf(os.Stderr, "[wfctl debug] FetchManifest %q: %d source(s), normalized=%q\n",
			name, len(m.sources), normalized)
	}

	// Try the original name first across all sources.
	var lastErr error
	for _, src := range m.sources {
		manifest, err := src.FetchManifest(name)
		if debugRegistryLog {
			if err != nil {
				fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (original %q): %v\n", src.Name(), name, err)
			} else {
				fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (original %q): found v%s\n",
					src.Name(), name, strings.TrimPrefix(manifest.Version, "v"))
			}
		}
		if err == nil {
			return manifest, src.Name(), nil
		}
		lastErr = err
	}

	// If the original name was not found and the normalized short name differs,
	// retry with the short name. This lets callers omit the "workflow-plugin-"
	// prefix (e.g. passing "auth" resolves to the registry entry named "auth"
	// when no entry named "auth" exists under the full original name).
	if normalized != name {
		if debugRegistryLog {
			fmt.Fprintf(os.Stderr, "[wfctl debug] FetchManifest %q: original not found, retrying as normalized %q\n",
				name, normalized)
		}
		for _, src := range m.sources {
			manifest, err := src.FetchManifest(normalized)
			if debugRegistryLog {
				if err != nil {
					fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (normalized %q): %v\n", src.Name(), normalized, err)
				} else {
					fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (normalized %q): found v%s\n",
						src.Name(), normalized, strings.TrimPrefix(manifest.Version, "v"))
				}
			}
			if err == nil {
				return manifest, src.Name(), nil
			}
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("plugin %q not found in any configured registry", name)
}

// FetchVersionIndex tries each source in priority order, using the same
// original-name then normalized-name lookup order as FetchManifest.
func (m *MultiRegistry) FetchVersionIndex(name string) (*PluginVersionIndex, string, error) {
	if len(m.sources) == 0 {
		return nil, "", fmt.Errorf("plugin %q not found: no registry sources configured"+
			" (missing .wfctl.yaml? run `wfctl registry list` or set WFCTL_DEBUG=1)", name)
	}

	normalized := normalizePluginName(name)
	if debugRegistryLog {
		fmt.Fprintf(os.Stderr, "[wfctl debug] FetchVersionIndex %q: %d source(s), normalized=%q\n",
			name, len(m.sources), normalized)
	}

	var lastErr error
	for _, src := range m.sources {
		index, err := src.FetchVersionIndex(name)
		if debugRegistryLog {
			if err != nil {
				fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (original %q index): %v\n", src.Name(), name, err)
			} else {
				fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (original %q index): found %d version(s)\n",
					src.Name(), name, len(index.Versions))
			}
		}
		if err == nil {
			return index, src.Name(), nil
		}
		lastErr = err
	}

	if normalized != name {
		for _, src := range m.sources {
			index, err := src.FetchVersionIndex(normalized)
			if debugRegistryLog {
				if err != nil {
					fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (normalized %q index): %v\n", src.Name(), normalized, err)
				} else {
					fmt.Fprintf(os.Stderr, "[wfctl debug]   %s (normalized %q index): found %d version(s)\n",
						src.Name(), normalized, len(index.Versions))
				}
			}
			if err == nil {
				return index, src.Name(), nil
			}
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("plugin %q compatibility index not found in any configured registry", name)
}

func (m *MultiRegistry) FetchManifestAndVersionIndex(name string) (*RegistryManifest, *PluginVersionIndex, string, error) {
	if len(m.sources) == 0 {
		return nil, nil, "", fmt.Errorf("plugin %q not found: no registry sources configured"+
			" (missing .wfctl.yaml? run `wfctl registry list` or set WFCTL_DEBUG=1)", name)
	}
	normalized := normalizePluginName(name)
	var lastErr error
	for _, candidate := range []string{name, normalized} {
		if candidate == "" {
			continue
		}
		for _, src := range m.sources {
			manifest, err := src.FetchManifest(candidate)
			if err != nil {
				lastErr = err
				continue
			}
			index, idxErr := src.FetchVersionIndex(candidate)
			if idxErr != nil {
				return manifest, nil, src.Name(), idxErr
			}
			return manifest, index, src.Name(), nil
		}
		if candidate == normalized {
			break
		}
	}
	if lastErr != nil {
		return nil, nil, "", lastErr
	}
	return nil, nil, "", fmt.Errorf("plugin %q not found in any configured registry", name)
}

// SearchPlugins searches all sources and returns deduplicated results.
// When the same plugin appears in multiple registries, the higher-priority source wins.
// The query is normalized (stripping "workflow-plugin-" prefix) before searching.
func (m *MultiRegistry) SearchPlugins(query string) ([]PluginSearchResult, error) {
	seen := make(map[string]bool)
	var results []PluginSearchResult

	normalizedQuery := normalizePluginName(query)
	for _, src := range m.sources {
		srcResults, err := src.SearchPlugins(normalizedQuery)
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
