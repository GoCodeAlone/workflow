package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── Registry interface ────────────────────────────────────────────────────

// MarketplaceEntry is a plugin entry in the marketplace registry.
type MarketplaceEntry struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Downloads   int      `json:"downloads"`
	Rating      float64  `json:"rating"`
	Installed   bool     `json:"installed"`
	InstalledAt string   `json:"installedAt,omitempty"`
}

// MarketplaceRegistry is the backend used by marketplace pipeline steps.
type MarketplaceRegistry interface {
	Search(query, category string, tags []string) ([]MarketplaceEntry, error)
	Detail(name string) (*MarketplaceEntry, error)
	Install(name string) error
	Uninstall(name string) error
	Update(name string) (*MarketplaceEntry, error)
	ListInstalled() ([]MarketplaceEntry, error)
}

// ─── step.marketplace_search ──────────────────────────────────────────────

// MarketplaceSearchStep searches the plugin registry.
type MarketplaceSearchStep struct {
	name     string
	query    string
	category string
	tags     []string
	registry MarketplaceRegistry
}

// NewMarketplaceSearchStepFactory returns a StepFactory for step.marketplace_search.
func NewMarketplaceSearchStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		query, _ := cfg["query"].(string)
		category, _ := cfg["category"].(string)
		var tags []string
		if raw, ok := cfg["tags"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}
		return &MarketplaceSearchStep{
			name:     name,
			query:    query,
			category: category,
			tags:     tags,
			registry: registry,
		}, nil
	}
}

func (s *MarketplaceSearchStep) Name() string { return s.name }

func (s *MarketplaceSearchStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	results, err := s.registry.Search(s.query, s.category, s.tags)
	if err != nil {
		return nil, fmt.Errorf("marketplace_search step %q: %w", s.name, err)
	}
	entries := make([]map[string]any, 0, len(results))
	for _, e := range results {
		entries = append(entries, entryToMap(e))
	}
	return &StepResult{Output: map[string]any{
		"results": entries,
		"count":   len(results),
		"query":   s.query,
	}}, nil
}

// ─── step.marketplace_detail ──────────────────────────────────────────────

// MarketplaceDetailStep retrieves detailed info for a named plugin.
type MarketplaceDetailStep struct {
	name       string
	pluginName string
	registry   MarketplaceRegistry
}

// NewMarketplaceDetailStepFactory returns a StepFactory for step.marketplace_detail.
func NewMarketplaceDetailStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		pluginName, _ := cfg["plugin"].(string)
		if pluginName == "" {
			return nil, fmt.Errorf("marketplace_detail step %q: 'plugin' is required", name)
		}
		return &MarketplaceDetailStep{name: name, pluginName: pluginName, registry: registry}, nil
	}
}

func (s *MarketplaceDetailStep) Name() string { return s.name }

func (s *MarketplaceDetailStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	entry, err := s.registry.Detail(s.pluginName)
	if err != nil {
		return nil, fmt.Errorf("marketplace_detail step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plugin": entryToMap(*entry),
	}}, nil
}

// ─── step.marketplace_install ─────────────────────────────────────────────

// MarketplaceInstallStep triggers installation of a named plugin.
type MarketplaceInstallStep struct {
	name       string
	pluginName string
	registry   MarketplaceRegistry
}

// NewMarketplaceInstallStepFactory returns a StepFactory for step.marketplace_install.
func NewMarketplaceInstallStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		pluginName, _ := cfg["plugin"].(string)
		if pluginName == "" {
			return nil, fmt.Errorf("marketplace_install step %q: 'plugin' is required", name)
		}
		return &MarketplaceInstallStep{name: name, pluginName: pluginName, registry: registry}, nil
	}
}

func (s *MarketplaceInstallStep) Name() string { return s.name }

func (s *MarketplaceInstallStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	if err := s.registry.Install(s.pluginName); err != nil {
		return nil, fmt.Errorf("marketplace_install step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plugin":  s.pluginName,
		"status":  "installed",
		"success": true,
	}}, nil
}

// ─── step.marketplace_installed ───────────────────────────────────────────

// MarketplaceInstalledStep lists all installed plugins.
type MarketplaceInstalledStep struct {
	name     string
	registry MarketplaceRegistry
}

// NewMarketplaceInstalledStepFactory returns a StepFactory for step.marketplace_installed.
func NewMarketplaceInstalledStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &MarketplaceInstalledStep{name: name, registry: registry}, nil
	}
}

func (s *MarketplaceInstalledStep) Name() string { return s.name }

func (s *MarketplaceInstalledStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	installed, err := s.registry.ListInstalled()
	if err != nil {
		return nil, fmt.Errorf("marketplace_installed step %q: %w", s.name, err)
	}
	entries := make([]map[string]any, 0, len(installed))
	for _, e := range installed {
		entries = append(entries, entryToMap(e))
	}
	return &StepResult{Output: map[string]any{
		"plugins": entries,
		"count":   len(installed),
	}}, nil
}

// ─── step.marketplace_uninstall ───────────────────────────────────────────

// MarketplaceUninstallStep removes an installed plugin.
type MarketplaceUninstallStep struct {
	name       string
	pluginName string
	registry   MarketplaceRegistry
}

// NewMarketplaceUninstallStepFactory returns a StepFactory for step.marketplace_uninstall.
func NewMarketplaceUninstallStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		pluginName, _ := cfg["plugin"].(string)
		if pluginName == "" {
			return nil, fmt.Errorf("marketplace_uninstall step %q: 'plugin' is required", name)
		}
		return &MarketplaceUninstallStep{name: name, pluginName: pluginName, registry: registry}, nil
	}
}

func (s *MarketplaceUninstallStep) Name() string { return s.name }

func (s *MarketplaceUninstallStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	if err := s.registry.Uninstall(s.pluginName); err != nil {
		return nil, fmt.Errorf("marketplace_uninstall step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plugin":  s.pluginName,
		"status":  "uninstalled",
		"success": true,
	}}, nil
}

// ─── step.marketplace_update ──────────────────────────────────────────────

// MarketplaceUpdateStep updates an installed plugin to its latest version.
type MarketplaceUpdateStep struct {
	name       string
	pluginName string
	registry   MarketplaceRegistry
}

// NewMarketplaceUpdateStepFactory returns a StepFactory for step.marketplace_update.
func NewMarketplaceUpdateStepFactory(registry MarketplaceRegistry) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		pluginName, _ := cfg["plugin"].(string)
		if pluginName == "" {
			return nil, fmt.Errorf("marketplace_update step %q: 'plugin' is required", name)
		}
		return &MarketplaceUpdateStep{name: name, pluginName: pluginName, registry: registry}, nil
	}
}

func (s *MarketplaceUpdateStep) Name() string { return s.name }

func (s *MarketplaceUpdateStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	entry, err := s.registry.Update(s.pluginName)
	if err != nil {
		return nil, fmt.Errorf("marketplace_update step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plugin":  entryToMap(*entry),
		"status":  "updated",
		"success": true,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────

func entryToMap(e MarketplaceEntry) map[string]any {
	return map[string]any{
		"name":        e.Name,
		"version":     e.Version,
		"description": e.Description,
		"author":      e.Author,
		"category":    e.Category,
		"tags":        e.Tags,
		"downloads":   e.Downloads,
		"rating":      e.Rating,
		"installed":   e.Installed,
		"installedAt": e.InstalledAt,
	}
}
