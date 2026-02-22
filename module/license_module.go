package module

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/licensing"
)

// LicenseModuleConfig holds the configuration for the license validator module.
type LicenseModuleConfig struct {
	ServerURL       string `yaml:"server_url"`
	LicenseKey      string `yaml:"license_key"`
	CacheTTL        string `yaml:"cache_ttl" default:"1h"`
	GracePeriod     string `yaml:"grace_period" default:"72h"`
	RefreshInterval string `yaml:"refresh_interval" default:"1h"`
}

// LicenseModule wraps a licensing.HTTPValidator as a modular.Module.
// It starts a background refresh on Start and exposes a status API endpoint.
type LicenseModule struct {
	name      string
	config    LicenseModuleConfig
	validator *licensing.HTTPValidator
	logger    *slog.Logger
}

// NewLicenseModule creates a new LicenseModule from a name and config map.
func NewLicenseModule(name string, cfg map[string]any) (*LicenseModule, error) {
	modCfg := LicenseModuleConfig{
		CacheTTL:        "1h",
		GracePeriod:     "72h",
		RefreshInterval: "1h",
	}

	if v, ok := cfg["server_url"].(string); ok {
		modCfg.ServerURL = v
	}
	// License key: config → env var → default empty
	if v, ok := cfg["license_key"].(string); ok && v != "" {
		modCfg.LicenseKey = v
	} else if envKey := os.Getenv("WORKFLOW_LICENSE_KEY"); envKey != "" {
		modCfg.LicenseKey = envKey
	}
	if v, ok := cfg["cache_ttl"].(string); ok && v != "" {
		modCfg.CacheTTL = v
	}
	if v, ok := cfg["grace_period"].(string); ok && v != "" {
		modCfg.GracePeriod = v
	}
	if v, ok := cfg["refresh_interval"].(string); ok && v != "" {
		modCfg.RefreshInterval = v
	}

	cacheTTL, err := time.ParseDuration(modCfg.CacheTTL)
	if err != nil {
		return nil, fmt.Errorf("license module %q: invalid cache_ttl %q: %w", name, modCfg.CacheTTL, err)
	}
	gracePeriod, err := time.ParseDuration(modCfg.GracePeriod)
	if err != nil {
		return nil, fmt.Errorf("license module %q: invalid grace_period %q: %w", name, modCfg.GracePeriod, err)
	}
	refreshInterval, err := time.ParseDuration(modCfg.RefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("license module %q: invalid refresh_interval %q: %w", name, modCfg.RefreshInterval, err)
	}

	validator := licensing.NewHTTPValidator(licensing.ValidatorConfig{
		ServerURL:       modCfg.ServerURL,
		LicenseKey:      modCfg.LicenseKey,
		CacheTTL:        cacheTTL,
		GracePeriod:     gracePeriod,
		RefreshInterval: refreshInterval,
	}, slog.Default())

	return &LicenseModule{
		name:      name,
		config:    modCfg,
		validator: validator,
		logger:    slog.Default(),
	}, nil
}

// Name implements modular.Module.
func (m *LicenseModule) Name() string { return m.name }

// Init implements modular.Module.
func (m *LicenseModule) Init(_ modular.Application) error { return nil }

// ProvidesServices implements modular.Module. The validator is registered
// under both the module name and the canonical "license-validator" name so
// other modules can look it up by either.
func (m *LicenseModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "License validator service: " + m.name,
			Instance:    m.validator,
		},
		{
			Name:        "license-validator",
			Description: "Canonical license validator service",
			Instance:    m.validator,
		},
	}
}

// RequiresServices implements modular.Module.
func (m *LicenseModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Start implements StartStopModule. It performs an initial license validation
// and starts the background refresh goroutine.
func (m *LicenseModule) Start(ctx context.Context) error {
	m.logger.Info("Starting license validator module", "name", m.name)
	if err := m.validator.Start(ctx); err != nil {
		return fmt.Errorf("license module %q: %w", m.name, err)
	}
	return nil
}

// Stop implements StartStopModule. It stops the background refresh goroutine.
func (m *LicenseModule) Stop(ctx context.Context) error {
	m.validator.Stop(ctx)
	return nil
}

// Validator returns the underlying HTTPValidator for direct use.
func (m *LicenseModule) Validator() *licensing.HTTPValidator {
	return m.validator
}

// ServeHTTP serves the GET /api/v1/license/status endpoint.
// It returns the current license info as JSON.
func (m *LicenseModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := m.validator.GetLicenseInfo()
	result, err := m.validator.Validate(r.Context(), m.config.LicenseKey)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	type statusResponse struct {
		Valid       bool                `json:"valid"`
		License     *licensing.LicenseInfo `json:"license,omitempty"`
		Error       string              `json:"error,omitempty"`
		CachedUntil time.Time           `json:"cached_until"`
	}

	resp := statusResponse{
		Valid:       result.Valid,
		CachedUntil: result.CachedUntil,
		Error:       result.Error,
	}
	if info != nil {
		resp.License = info
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
