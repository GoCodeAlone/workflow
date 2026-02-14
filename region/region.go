package region

import (
	"context"
	"fmt"
	"sync"
)

type contextKey string

const (
	// RegionKey is the context key for the active region.
	RegionKey contextKey = "region"
)

// RegionFromContext extracts the region from the context.
func RegionFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(RegionKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithRegion returns a context with the region set.
func ContextWithRegion(ctx context.Context, region string) context.Context {
	return context.WithValue(ctx, RegionKey, region)
}

// RegionConfig describes a deployment region.
type RegionConfig struct {
	// Name is the unique identifier for this region (e.g. "us-east-1").
	Name string
	// Endpoint is the base URL for this region's API.
	Endpoint string
	// Primary indicates whether this is the primary region.
	Primary bool
	// AllowedDataClasses lists the data classification levels this region may store.
	AllowedDataClasses []string
}

// DataResidencyRule maps a tenant to the regions where its data may reside.
type DataResidencyRule struct {
	TenantID       string
	AllowedRegions []string
	DataClass      string // e.g. "pii", "general", "restricted"
}

// DataResidencyEnforcer validates that data operations respect tenant
// residency requirements.
type DataResidencyEnforcer struct {
	mu    sync.RWMutex
	rules map[string]DataResidencyRule // tenantID -> rule
}

// NewDataResidencyEnforcer creates a new enforcer.
func NewDataResidencyEnforcer() *DataResidencyEnforcer {
	return &DataResidencyEnforcer{
		rules: make(map[string]DataResidencyRule),
	}
}

// SetRule configures the residency rule for a tenant.
func (e *DataResidencyEnforcer) SetRule(rule DataResidencyRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules[rule.TenantID] = rule
}

// GetRule returns the residency rule for a tenant.
func (e *DataResidencyEnforcer) GetRule(tenantID string) (DataResidencyRule, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.rules[tenantID]
	return r, ok
}

// RemoveRule removes a tenant's residency rule.
func (e *DataResidencyEnforcer) RemoveRule(tenantID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rules, tenantID)
}

// Check validates that a region is allowed for a given tenant.
// Returns nil if the region is permitted or no rule is configured.
func (e *DataResidencyEnforcer) Check(tenantID, region string) error {
	e.mu.RLock()
	rule, ok := e.rules[tenantID]
	e.mu.RUnlock()

	if !ok {
		// No rule means no restriction
		return nil
	}

	for _, allowed := range rule.AllowedRegions {
		if allowed == region {
			return nil
		}
	}

	return fmt.Errorf("region %q not allowed for tenant %s (allowed: %v)", region, tenantID, rule.AllowedRegions)
}

// CheckDataClass validates that a region supports the required data classification.
func (e *DataResidencyEnforcer) CheckDataClass(tenantID string, regionCfg RegionConfig) error {
	e.mu.RLock()
	rule, ok := e.rules[tenantID]
	e.mu.RUnlock()

	if !ok || rule.DataClass == "" {
		return nil
	}

	for _, dc := range regionCfg.AllowedDataClasses {
		if dc == rule.DataClass {
			return nil
		}
	}

	return fmt.Errorf("region %q does not support data class %q for tenant %s", regionCfg.Name, rule.DataClass, tenantID)
}

// RegionRouter routes requests to the appropriate region based on tenant
// residency rules and region availability.
type RegionRouter struct {
	mu       sync.RWMutex
	regions  map[string]RegionConfig
	enforcer *DataResidencyEnforcer
	primary  string
}

// NewRegionRouter creates a new region router.
func NewRegionRouter(enforcer *DataResidencyEnforcer) *RegionRouter {
	return &RegionRouter{
		regions:  make(map[string]RegionConfig),
		enforcer: enforcer,
	}
}

// AddRegion registers a region.
func (r *RegionRouter) AddRegion(cfg RegionConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.regions[cfg.Name] = cfg
	if cfg.Primary {
		r.primary = cfg.Name
	}
}

// RemoveRegion unregisters a region.
func (r *RegionRouter) RemoveRegion(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.primary == name {
		r.primary = ""
	}
	delete(r.regions, name)
}

// Route returns the best region for a tenant request.
// It checks residency rules first, then falls back to the primary region.
func (r *RegionRouter) Route(tenantID string) (RegionConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.regions) == 0 {
		return RegionConfig{}, fmt.Errorf("no regions configured")
	}

	// Check if there's a residency rule for this tenant
	if r.enforcer != nil {
		rule, hasRule := r.enforcer.GetRule(tenantID)
		if hasRule && len(rule.AllowedRegions) > 0 {
			// Return the first allowed region that exists
			for _, allowed := range rule.AllowedRegions {
				if cfg, ok := r.regions[allowed]; ok {
					return cfg, nil
				}
			}
			return RegionConfig{}, fmt.Errorf("none of the allowed regions for tenant %s are available", tenantID)
		}
	}

	// Fall back to primary region
	if r.primary != "" {
		if cfg, ok := r.regions[r.primary]; ok {
			return cfg, nil
		}
	}

	// Return any available region
	for _, cfg := range r.regions {
		return cfg, nil
	}

	return RegionConfig{}, fmt.Errorf("no available region for tenant %s", tenantID)
}

// GetRegion returns a specific region's config.
func (r *RegionRouter) GetRegion(name string) (RegionConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.regions[name]
	return cfg, ok
}

// Regions returns all configured regions.
func (r *RegionRouter) Regions() []RegionConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]RegionConfig, 0, len(r.regions))
	for _, cfg := range r.regions {
		result = append(result, cfg)
	}
	return result
}

// PrimaryRegion returns the primary region name.
func (r *RegionRouter) PrimaryRegion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.primary
}
