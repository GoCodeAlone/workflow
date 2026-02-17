package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// pluginsBaseDir is the local directory under which all plugins are stored.
// All plugin-related files must remain within this directory.
var pluginsBaseDir = filepath.Join("data", "plugins")

// CompositeRegistry combines a local registry with a remote registry,
// searching both and allowing installation from the remote into local.
type CompositeRegistry struct {
	local  *LocalRegistry
	remote *RemoteRegistry
}

// NewCompositeRegistry creates a composite registry from a local and remote registry.
func NewCompositeRegistry(local *LocalRegistry, remote *RemoteRegistry) *CompositeRegistry {
	return &CompositeRegistry{
		local:  local,
		remote: remote,
	}
}

// Register delegates to the local registry.
func (c *CompositeRegistry) Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error {
	return c.local.Register(manifest, component, sourceDir)
}

// Unregister delegates to the local registry.
func (c *CompositeRegistry) Unregister(name string) error {
	return c.local.Unregister(name)
}

// Get delegates to the local registry.
func (c *CompositeRegistry) Get(name string) (*PluginEntry, bool) {
	return c.local.Get(name)
}

// List delegates to the local registry.
func (c *CompositeRegistry) List() []*PluginEntry {
	return c.local.List()
}

// CheckDependencies delegates to the local registry.
func (c *CompositeRegistry) CheckDependencies(manifest *PluginManifest) error {
	return c.local.CheckDependencies(manifest)
}

// Search checks local first, then remote, merging results with no duplicates.
func (c *CompositeRegistry) Search(ctx context.Context, query string) ([]*PluginManifest, error) {
	seen := make(map[string]bool)
	var results []*PluginManifest

	// Local results first
	for _, entry := range c.local.List() {
		if matchesQuery(entry.Manifest, query) {
			results = append(results, entry.Manifest)
			seen[entry.Manifest.Name] = true
		}
	}

	// Remote results (skip duplicates)
	if c.remote != nil {
		remoteResults, err := c.remote.Search(ctx, query)
		if err != nil {
			// Remote search failed — return local results only.
			// This is intentional: local results are still valid even when the
			// remote registry is unreachable, so we swallow the error.
			return results, nil //nolint:nilerr // intentionally returning nil; local results are sufficient
		}
		for _, m := range remoteResults {
			if !seen[m.Name] {
				results = append(results, m)
				seen[m.Name] = true
			}
		}
	}

	return results, nil
}

// Install downloads a plugin from the remote registry and registers it locally.
func (c *CompositeRegistry) Install(ctx context.Context, name, version string) error {
	if c.remote == nil {
		return fmt.Errorf("no remote registry configured")
	}

	// Check if already installed
	if entry, ok := c.local.Get(name); ok {
		if entry.Manifest.Version == version {
			return fmt.Errorf("plugin %s@%s is already installed", name, version)
		}
	}

	// Get manifest from remote
	manifest, err := c.remote.GetManifest(ctx, name, version)
	if err != nil {
		return fmt.Errorf("get manifest for %s@%s: %w", name, version, err)
	}

	// Download plugin archive
	reader, err := c.remote.Download(ctx, name, version)
	if err != nil {
	// Resolve and validate local plugin directory to prevent directory traversal.
	absBaseDir, err := filepath.Abs(pluginsBaseDir)
	if err != nil {
		return fmt.Errorf("resolve plugins base directory: %w", err)
	}
	pluginDir := filepath.Join(absBaseDir, name)
	absPluginDir, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin directory: %w", err)
	}
	if !strings.HasPrefix(absPluginDir, absBaseDir+string(os.PathSeparator)) && absPluginDir != absBaseDir {
		return fmt.Errorf("invalid plugin name %q", name)
	}

		return fmt.Errorf("download %s@%s: %w", name, version, err)
	}
	defer reader.Close()

	// Save to a local directory
	if err := os.MkdirAll(absPluginDir, 0750); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	// Save the archive
	archivePath := filepath.Join(absPluginDir, fmt.Sprintf("%s-%s.tar.gz", name, version))
	f, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		return fmt.Errorf("save archive: %w", err)
	}
	f.Close()

	// Save manifest
	manifestPath := filepath.Join(absPluginDir, "plugin.json")
	if err := SaveManifest(manifestPath, manifest); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	// Register in local registry (without a component — will be loaded separately)
	if err := c.local.Register(manifest, nil, absPluginDir); err != nil {
		return fmt.Errorf("register installed plugin: %w", err)
	}

	return nil
}

// Local returns the underlying local registry.
func (c *CompositeRegistry) Local() *LocalRegistry {
	return c.local
}

// Remote returns the underlying remote registry.
func (c *CompositeRegistry) Remote() *RemoteRegistry {
	return c.remote
}

// matchesQuery checks if a manifest matches a search query by name, description, or tags.
func matchesQuery(m *PluginManifest, query string) bool {
	if query == "" {
		return true
	}
	lower := func(s string) string {
		result := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			result[i] = c
		}
		return string(result)
	}
	q := lower(query)
	if contains(lower(m.Name), q) {
		return true
	}
	if contains(lower(m.Description), q) {
		return true
	}
	for _, tag := range m.Tags {
		if contains(lower(tag), q) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
