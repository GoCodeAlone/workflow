package module

import (
	"context"
	"fmt"
	"sync"
)

// RegionRouterInterface defines the interface for routing requests across regions.
// Implementations can use latency-based, geographic, or weighted routing.
type RegionRouterInterface interface {
	// RouteRequest selects the best region for the given context.
	RouteRequest(ctx context.Context) (RegionDeployConfig, error)
	// Failover triggers a failover from one region to another.
	Failover(from, to string) error
	// Weights returns the current traffic routing weights per region.
	Weights() map[string]int
}

// RegionFailoverState represents the failover state machine state.
type RegionFailoverState string

const (
	RegionStateHealthy    RegionFailoverState = "healthy"
	RegionStateDegraded   RegionFailoverState = "degraded"
	RegionStateFailed     RegionFailoverState = "failed"
	RegionStateRecovering RegionFailoverState = "recovering"
)

// Transitions: healthy → degraded → failed → recovering → healthy

// MultiRegionRoutingModule manages region routing for a tenant deployment.
// It wraps a MultiRegionModule and provides routing logic (latency or geo).
// Config:
//
//	module:  name of the platform.region module to route through
//	mode:    latency | geo (default: latency)
type MultiRegionRoutingModule struct {
	mu      sync.RWMutex
	name    string
	config  map[string]any
	states  map[string]RegionFailoverState
	weights map[string]int
	regions []RegionDeployConfig
}

// NewMultiRegionRoutingModule creates a new routing module.
func NewMultiRegionRoutingModule(name string, cfg map[string]any) *MultiRegionRoutingModule {
	return &MultiRegionRoutingModule{
		name:    name,
		config:  cfg,
		states:  make(map[string]RegionFailoverState),
		weights: make(map[string]int),
	}
}

// Name returns the module name.
func (r *MultiRegionRoutingModule) Name() string { return r.name }

// SetRegions configures the regions available for routing.
func (r *MultiRegionRoutingModule) SetRegions(regions []RegionDeployConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.regions = regions
	for _, reg := range regions {
		if _, ok := r.states[reg.Name]; !ok {
			r.states[reg.Name] = RegionStateHealthy
		}
		if _, ok := r.weights[reg.Name]; !ok {
			if len(regions) > 0 {
				r.weights[reg.Name] = 100 / len(regions)
			}
		}
	}
}

// RouteRequest selects the best region based on the configured routing mode.
func (r *MultiRegionRoutingModule) RouteRequest(ctx context.Context) (RegionDeployConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mode, _ := r.config["mode"].(string)
	if mode == "" {
		mode = "latency"
	}

	switch mode {
	case "latency":
		return r.latencyRoute()
	case "geo":
		return r.geoRoute(ctx)
	default:
		return RegionDeployConfig{}, fmt.Errorf("region_router %q: unsupported mode %q", r.name, mode)
	}
}

// latencyRoute returns the healthy region with the highest weight (mock: highest weight = lowest latency).
func (r *MultiRegionRoutingModule) latencyRoute() (RegionDeployConfig, error) {
	var best *RegionDeployConfig
	bestWeight := -1

	for i := range r.regions {
		reg := &r.regions[i]
		state := r.states[reg.Name]
		if state == RegionStateFailed {
			continue
		}
		w := r.weights[reg.Name]
		if w > bestWeight {
			bestWeight = w
			best = reg
		}
	}

	if best == nil {
		return RegionDeployConfig{}, fmt.Errorf("region_router %q: no healthy region available", r.name)
	}
	return *best, nil
}

// geoRoute returns the first healthy primary region (mock: ignores actual geo).
func (r *MultiRegionRoutingModule) geoRoute(_ context.Context) (RegionDeployConfig, error) {
	// Mock geo routing: prefer primary, then secondary, then dr
	priority := []string{"primary", "secondary", "dr"}
	for _, p := range priority {
		for i := range r.regions {
			reg := &r.regions[i]
			if reg.Priority == p && r.states[reg.Name] != RegionStateFailed {
				return *reg, nil
			}
		}
	}
	return RegionDeployConfig{}, fmt.Errorf("region_router %q: no healthy region available for geo routing", r.name)
}

// Failover transitions a region through the failover state machine.
// Transitions: healthy/degraded → failed (from), recovering → healthy (to).
func (r *MultiRegionRoutingModule) Failover(from, to string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.states[from]; !ok {
		return fmt.Errorf("region_router %q: source region %q not found", r.name, from)
	}
	if _, ok := r.states[to]; !ok {
		return fmt.Errorf("region_router %q: target region %q not found", r.name, to)
	}

	r.states[from] = RegionStateFailed
	r.states[to] = RegionStateRecovering
	// Complete recovery immediately (mock)
	r.states[to] = RegionStateHealthy
	// Shift weights
	r.weights[from] = 0
	r.weights[to] = 100

	return nil
}

// Weights returns the current traffic routing weights.
func (r *MultiRegionRoutingModule) Weights() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]int, len(r.weights))
	for k, v := range r.weights {
		result[k] = v
	}
	return result
}

// SetState directly sets the failover state for a region (for testing / external control).
func (r *MultiRegionRoutingModule) SetState(region string, state RegionFailoverState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.states[region]; !ok {
		return fmt.Errorf("region_router %q: region %q not found", r.name, region)
	}
	r.states[region] = state
	return nil
}

// State returns the failover state for a region.
func (r *MultiRegionRoutingModule) State(region string) (RegionFailoverState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.states[region]
	return s, ok
}
