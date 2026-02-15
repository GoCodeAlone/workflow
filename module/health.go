package module

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// HealthCheckerConfig holds configuration for the HealthChecker module.
type HealthCheckerConfig struct {
	HealthPath   string        `yaml:"healthPath" json:"healthPath"`
	ReadyPath    string        `yaml:"readyPath" json:"readyPath"`
	LivePath     string        `yaml:"livePath" json:"livePath"`
	CheckTimeout time.Duration `yaml:"checkTimeout" json:"checkTimeout"`
	AutoDiscover bool          `yaml:"autoDiscover" json:"autoDiscover"`
}

// DefaultHealthCheckerConfig returns the default configuration.
func DefaultHealthCheckerConfig() HealthCheckerConfig {
	return HealthCheckerConfig{
		HealthPath:   "/healthz",
		ReadyPath:    "/readyz",
		LivePath:     "/livez",
		CheckTimeout: 5 * time.Second,
		AutoDiscover: true,
	}
}

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthCheck is a function that performs a health check.
type HealthCheck func(ctx context.Context) HealthCheckResult

// HealthCheckable is implemented by modules that can report their own health.
// The health checker auto-discovers services implementing this interface.
type HealthCheckable interface {
	HealthStatus() HealthCheckResult
}

// HealthChecker provides /health, /ready, /live HTTP endpoints.
type HealthChecker struct {
	name    string
	config  HealthCheckerConfig
	checks  map[string]HealthCheck
	mu      sync.RWMutex
	started bool
	app     modular.Application
}

// NewHealthChecker creates a new HealthChecker module.
func NewHealthChecker(name string) *HealthChecker {
	return &HealthChecker{
		name:   name,
		config: DefaultHealthCheckerConfig(),
		checks: make(map[string]HealthCheck),
	}
}

// SetConfig applies configuration to the health checker.
func (h *HealthChecker) SetConfig(cfg HealthCheckerConfig) {
	if cfg.HealthPath != "" {
		h.config.HealthPath = cfg.HealthPath
	}
	if cfg.ReadyPath != "" {
		h.config.ReadyPath = cfg.ReadyPath
	}
	if cfg.LivePath != "" {
		h.config.LivePath = cfg.LivePath
	}
	if cfg.CheckTimeout > 0 {
		h.config.CheckTimeout = cfg.CheckTimeout
	}
	h.config.AutoDiscover = cfg.AutoDiscover
}

// HealthPath returns the configured health endpoint path.
func (h *HealthChecker) HealthPath() string { return h.config.HealthPath }

// ReadyPath returns the configured readiness endpoint path.
func (h *HealthChecker) ReadyPath() string { return h.config.ReadyPath }

// LivePath returns the configured liveness endpoint path.
func (h *HealthChecker) LivePath() string { return h.config.LivePath }

// AutoDiscover returns whether auto-discovery of HealthCheckable services is enabled.
func (h *HealthChecker) AutoDiscover() bool { return h.config.AutoDiscover }

// Name returns the module name.
func (h *HealthChecker) Name() string {
	return h.name
}

// Init registers the health checker as a service.
func (h *HealthChecker) Init(app modular.Application) error {
	h.app = app
	return app.RegisterService("health.checker", h)
}

// DiscoverHealthCheckables scans the service registry for services implementing
// HealthCheckable and auto-registers them as health checks.
func (h *HealthChecker) DiscoverHealthCheckables() {
	if h.app == nil || !h.config.AutoDiscover {
		return
	}
	for name, svc := range h.app.SvcRegistry() {
		if hc, ok := svc.(HealthCheckable); ok {
			checkName := name
			checkable := hc
			h.RegisterCheck(checkName, func(_ context.Context) HealthCheckResult {
				return checkable.HealthStatus()
			})
		}
	}
}

// RegisterCheck adds a named health check function.
func (h *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// SetStarted marks the health checker as started or stopped.
func (h *HealthChecker) SetStarted(started bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.started = started
}

// HealthHandler returns an HTTP handler that runs all health checks.
func (h *HealthChecker) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		checks := make(map[string]HealthCheck, len(h.checks))
		maps.Copy(checks, h.checks)
		timeout := h.config.CheckTimeout
		h.mu.RUnlock()

		ctx := r.Context()
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		overallStatus := "healthy"
		results := make(map[string]HealthCheckResult)

		for name, check := range checks {
			result := check(ctx)
			results[name] = result
			if result.Status == "unhealthy" {
				overallStatus = "unhealthy"
			} else if result.Status == "degraded" && overallStatus == "healthy" {
				overallStatus = "degraded"
			}
		}

		resp := map[string]any{
			"status": overallStatus,
			"checks": results,
		}

		w.Header().Set("Content-Type", "application/json")
		if overallStatus == "unhealthy" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// ReadyHandler returns an HTTP handler that checks readiness.
// Returns 200 only if started AND all checks pass, else 503.
func (h *HealthChecker) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		started := h.started
		checks := make(map[string]HealthCheck, len(h.checks))
		maps.Copy(checks, h.checks)
		h.mu.RUnlock()

		if !started {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
			return
		}

		for _, check := range checks {
			result := check(r.Context())
			if result.Status != "healthy" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}
}

// LiveHandler returns an HTTP handler for liveness checks.
// Always returns 200 with {"status":"alive"}.
func (h *HealthChecker) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	}
}

// ProvidesServices returns the services provided by this module.
func (h *HealthChecker) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "health.checker",
			Description: "Health check endpoints for the workflow engine",
			Instance:    h,
		},
	}
}

// RequiresServices returns services required by this module.
func (h *HealthChecker) RequiresServices() []modular.ServiceDependency {
	return nil
}

// HealthHTTPHandler adapts an http.HandlerFunc to the HTTPHandler interface
type HealthHTTPHandler struct {
	Handler http.HandlerFunc
}

// Handle implements the HTTPHandler interface
func (h *HealthHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.Handler(w, r)
}
