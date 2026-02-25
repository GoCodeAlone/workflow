// Package marketplace provides a plugin that registers pipeline step factories
// for interacting with the workflow plugin marketplace registry.
// Steps: step.marketplace_search, step.marketplace_detail, step.marketplace_install,
// step.marketplace_installed, step.marketplace_uninstall, step.marketplace_update.
package marketplace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers marketplace pipeline steps.
type Plugin struct {
	plugin.BaseEnginePlugin
	registry module.MarketplaceRegistry
}

// New creates a new marketplace plugin using the default local registry.
// The registry is backed by the data/plugins/ directory.
func New() *Plugin {
	return NewWithRegistry(newLocalRegistry("data/plugins"))
}

// NewWithRegistry creates a marketplace plugin with a custom registry.
func NewWithRegistry(registry module.MarketplaceRegistry) *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "marketplace",
				PluginVersion:     "1.0.0",
				PluginDescription: "Plugin marketplace steps for searching, installing, and managing workflow plugins",
			},
			Manifest: plugin.PluginManifest{
				Name:        "marketplace",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Plugin marketplace steps for searching, installing, and managing workflow plugins",
				Tier:        plugin.TierCore,
				StepTypes: []string{
					"step.marketplace_search",
					"step.marketplace_detail",
					"step.marketplace_install",
					"step.marketplace_installed",
					"step.marketplace_uninstall",
					"step.marketplace_update",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "marketplace", Role: "provider", Priority: 50},
				},
			},
		},
		registry: registry,
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "marketplace",
			Description: "Plugin marketplace operations: search, install, uninstall, update",
		},
	}
}

// StepFactories returns the marketplace step factories.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.marketplace_search":    wrapFactory(module.NewMarketplaceSearchStepFactory(p.registry)),
		"step.marketplace_detail":    wrapFactory(module.NewMarketplaceDetailStepFactory(p.registry)),
		"step.marketplace_install":   wrapFactory(module.NewMarketplaceInstallStepFactory(p.registry)),
		"step.marketplace_installed": wrapFactory(module.NewMarketplaceInstalledStepFactory(p.registry)),
		"step.marketplace_uninstall": wrapFactory(module.NewMarketplaceUninstallStepFactory(p.registry)),
		"step.marketplace_update":    wrapFactory(module.NewMarketplaceUpdateStepFactory(p.registry)),
	}
}

func wrapFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}

// ─── local registry ────────────────────────────────────────────────────────

// localRegistry is a file-system-backed implementation of MarketplaceRegistry.
// It manages plugins under a base directory (e.g., data/plugins/).
// For the bundled catalog it uses an in-memory seed so tests and demos
// work without a real network registry.
type localRegistry struct {
	baseDir string
	catalog []module.MarketplaceEntry
}

func newLocalRegistry(baseDir string) *localRegistry {
	return &localRegistry{
		baseDir: baseDir,
		catalog: defaultCatalog(),
	}
}

func (r *localRegistry) Search(query, category string, tags []string) ([]module.MarketplaceEntry, error) {
	var results []module.MarketplaceEntry
	installed := r.installedSet()
	for _, e := range r.catalog {
		if query != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(query)) &&
			!strings.Contains(strings.ToLower(e.Description), strings.ToLower(query)) {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		if len(tags) > 0 {
			matched := false
			for _, want := range tags {
				for _, have := range e.Tags {
					if have == want {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				continue
			}
		}
		e.Installed = installed[e.Name]
		results = append(results, e)
	}
	return results, nil
}

func (r *localRegistry) Detail(name string) (*module.MarketplaceEntry, error) {
	installed := r.installedSet()
	for _, e := range r.catalog {
		if e.Name == name {
			e.Installed = installed[name]
			if e.Installed {
				e.InstalledAt = r.installedAt(name)
			}
			return &e, nil
		}
	}
	return nil, fmt.Errorf("plugin %q not found in catalog", name)
}

func (r *localRegistry) Install(name string) error {
	if _, err := r.Detail(name); err != nil {
		return err
	}
	pluginDir := filepath.Join(r.baseDir, name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin dir %s: %w", pluginDir, err)
	}
	// Write a sentinel file to mark installation
	marker := filepath.Join(pluginDir, ".installed")
	return os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

func (r *localRegistry) Uninstall(name string) error {
	installed := r.installedSet()
	if !installed[name] {
		return fmt.Errorf("plugin %q is not installed", name)
	}
	return os.RemoveAll(filepath.Join(r.baseDir, name))
}

func (r *localRegistry) Update(name string) (*module.MarketplaceEntry, error) {
	installed := r.installedSet()
	if !installed[name] {
		return nil, fmt.Errorf("plugin %q is not installed", name)
	}
	// Re-install to simulate an update
	if err := r.Install(name); err != nil {
		return nil, fmt.Errorf("update failed: %w", err)
	}
	return r.Detail(name)
}

func (r *localRegistry) ListInstalled() ([]module.MarketplaceEntry, error) {
	installed := r.installedSet()
	var result []module.MarketplaceEntry
	for _, e := range r.catalog {
		if installed[e.Name] {
			e.Installed = true
			e.InstalledAt = r.installedAt(e.Name)
			result = append(result, e)
		}
	}
	return result, nil
}

func (r *localRegistry) installedSet() map[string]bool {
	set := make(map[string]bool)
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return set
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker := filepath.Join(r.baseDir, entry.Name(), ".installed")
		if _, err := os.Stat(marker); err == nil {
			set[entry.Name()] = true
		}
	}
	return set
}

func (r *localRegistry) installedAt(name string) string {
	marker := filepath.Join(r.baseDir, name, ".installed")
	data, err := os.ReadFile(marker)
	if err != nil {
		return ""
	}
	return string(data)
}

// defaultCatalog returns a built-in set of community plugins for demo/test use.
func defaultCatalog() []module.MarketplaceEntry {
	return []module.MarketplaceEntry{
		{
			Name:        "auth-oidc",
			Version:     "1.2.0",
			Description: "OpenID Connect authentication provider for workflow",
			Author:      "GoCodeAlone",
			Category:    "auth",
			Tags:        []string{"auth", "oidc", "sso"},
			Downloads:   4200,
			Rating:      4.8,
		},
		{
			Name:        "storage-s3",
			Version:     "2.0.1",
			Description: "AWS S3 blob storage backend",
			Author:      "GoCodeAlone",
			Category:    "storage",
			Tags:        []string{"storage", "aws", "s3"},
			Downloads:   8900,
			Rating:      4.9,
		},
		{
			Name:        "messaging-kafka",
			Version:     "1.0.3",
			Description: "Apache Kafka messaging integration",
			Author:      "GoCodeAlone",
			Category:    "messaging",
			Tags:        []string{"messaging", "kafka", "streaming"},
			Downloads:   3100,
			Rating:      4.6,
		},
		{
			Name:        "observability-otel",
			Version:     "0.9.0",
			Description: "OpenTelemetry tracing and metrics export",
			Author:      "GoCodeAlone",
			Category:    "observability",
			Tags:        []string{"observability", "otel", "tracing", "metrics"},
			Downloads:   2700,
			Rating:      4.5,
		},
		{
			Name:        "cicd-github-actions",
			Version:     "1.1.0",
			Description: "GitHub Actions CI/CD pipeline trigger integration",
			Author:      "GoCodeAlone",
			Category:    "cicd",
			Tags:        []string{"cicd", "github", "actions"},
			Downloads:   1850,
			Rating:      4.4,
		},
		{
			Name:        "ai-openai",
			Version:     "0.5.0",
			Description: "OpenAI GPT integration for AI-assisted workflow steps",
			Author:      "GoCodeAlone",
			Category:    "ai",
			Tags:        []string{"ai", "openai", "gpt", "llm"},
			Downloads:   5600,
			Rating:      4.7,
		},
	}
}
