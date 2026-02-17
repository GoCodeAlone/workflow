package module

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/featureflag"
	"github.com/GoCodeAlone/workflow/featureflag/generic"
)

// FeatureFlagModuleConfig holds the configuration for the feature flag module.
type FeatureFlagModuleConfig struct {
	Provider   string `yaml:"provider" default:"generic"`
	CacheTTL   string `yaml:"cache_ttl" default:"30s"`
	SSEEnabled bool   `yaml:"sse_enabled" default:"true"`
	DBPath     string `yaml:"db_path" default:"data/featureflags.db"`
}

// FeatureFlagModule wraps a featureflag.Service as a modular.Module.
// It initializes the configured provider and makes the service available
// in the modular service registry.
type FeatureFlagModule struct {
	name    string
	config  FeatureFlagModuleConfig
	service *featureflag.Service
	store   *generic.Store
}

// NewFeatureFlagModule creates a new feature flag module with the given name and config.
func NewFeatureFlagModule(name string, cfg FeatureFlagModuleConfig) (*FeatureFlagModule, error) {
	cacheTTL, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		cacheTTL = 30 * time.Second
	}

	var provider featureflag.Provider
	var store *generic.Store

	switch cfg.Provider {
	case "generic", "":
		dbPath := cfg.DBPath
		if dbPath == "" {
			dbPath = "data/featureflags.db"
		}
		// Ensure parent directory exists for the SQLite file.
		if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				return nil, fmt.Errorf("feature flag module %q: failed to create db directory %q: %w", name, dir, mkErr)
			}
		}
		store, err = generic.NewStore(dbPath)
		if err != nil {
			return nil, fmt.Errorf("feature flag module %q: failed to open store: %w", name, err)
		}
		provider = generic.NewProvider(store, slog.Default())
	default:
		return nil, fmt.Errorf("feature flag module %q: unsupported provider %q (supported: generic)", name, cfg.Provider)
	}

	cache := featureflag.NewFlagCache(cacheTTL)
	service := featureflag.NewService(provider, cache, slog.Default())

	return &FeatureFlagModule{
		name:    name,
		config:  cfg,
		service: service,
		store:   store,
	}, nil
}

// Name implements modular.Module.
func (m *FeatureFlagModule) Name() string { return m.name }

// Init implements modular.Module.
func (m *FeatureFlagModule) Init(_ modular.Application) error { return nil }

// ProvidesServices implements modular.Module. The service is registered under
// the module name so other modules (and the engine) can look it up.
func (m *FeatureFlagModule) ProvidesServices() []modular.ServiceProvider {
	providers := []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Feature flag service: " + m.name,
			Instance:    m.service,
		},
	}
	// Also register the store if it's a generic provider so the admin API can use it
	if m.store != nil {
		providers = append(providers, modular.ServiceProvider{
			Name:        m.name + ".store",
			Description: "Feature flag store: " + m.name,
			Instance:    m.store,
		})
	}
	return providers
}

// RequiresServices implements modular.Module.
func (m *FeatureFlagModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Service returns the underlying FF service for direct use (e.g. step factories).
func (m *FeatureFlagModule) Service() *featureflag.Service {
	return m.service
}

// Store returns the underlying generic store, or nil if using a non-generic provider.
func (m *FeatureFlagModule) Store() *generic.Store {
	return m.store
}

// SSEEnabled returns whether SSE streaming is enabled for this module.
func (m *FeatureFlagModule) SSEEnabled() bool {
	return m.config.SSEEnabled
}
