package module

import (
	"fmt"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// RegionDeployConfig describes a deployment region within a MultiRegionModule.
type RegionDeployConfig struct {
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Endpoint    string            `json:"endpoint"`
	Priority    string            `json:"priority"` // primary, secondary, dr
	HealthCheck HealthCheckConfig `json:"healthCheck"`
}

// HealthCheckConfig describes health check settings for a region.
type HealthCheckConfig struct {
	Interval  int    `json:"interval"`  // seconds between checks
	Timeout   int    `json:"timeout"`   // seconds before timeout
	Path      string `json:"path"`      // HTTP path for health check
	Threshold int    `json:"threshold"` // failures before marking degraded
}

// RegionHealth tracks the current health state of a region.
type RegionHealth struct {
	Name    string `json:"name"`
	Status  string `json:"status"`  // healthy, degraded, failed, recovering
	Latency int    `json:"latency"` // mock latency in ms
}

// MultiRegionState tracks current multi-region deployment state.
type MultiRegionState struct {
	Regions       []RegionHealth `json:"regions"`
	ActiveRegion  string         `json:"activeRegion"`
	PrimaryRegion string         `json:"primaryRegion"`
	Weights       map[string]int `json:"weights"`
	Status        string         `json:"status"` // initializing, active, failing-over, degraded
}

// multiRegionBackend is the interface provider backends implement.
type multiRegionBackend interface {
	deploy(m *MultiRegionModule, region string) error
	status(m *MultiRegionModule) (*MultiRegionState, error)
	failover(m *MultiRegionModule, from, to string) error
	promote(m *MultiRegionModule, region string) error
	setWeight(m *MultiRegionModule, region string, weight int) error
	sync(m *MultiRegionModule) error
	checkHealth(m *MultiRegionModule) ([]RegionHealth, error)
}

// MultiRegionModule manages region-aware deployments with failover and traffic routing.
// Config:
//
//	provider: mock
//	regions:  list of region definitions (name, provider, endpoint, priority, health_check)
//	primary:  primary region name
type MultiRegionModule struct {
	mu      sync.RWMutex
	name    string
	config  map[string]any
	state   *MultiRegionState
	backend multiRegionBackend
}

// NewMultiRegionModule creates a new MultiRegionModule.
func NewMultiRegionModule(name string, cfg map[string]any) *MultiRegionModule {
	return &MultiRegionModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *MultiRegionModule) Name() string { return m.name }

// Init initialises the backend and sets up initial state.
func (m *MultiRegionModule) Init(app modular.Application) error {
	regions := m.regionConfigs()
	if len(regions) == 0 {
		return fmt.Errorf("platform.region %q: at least one region is required", m.name)
	}

	primary := m.primaryRegion(regions)
	if primary == "" {
		return fmt.Errorf("platform.region %q: no primary region configured (set priority=primary on one region)", m.name)
	}

	weights := make(map[string]int, len(regions))
	for _, r := range regions {
		weights[r.Name] = 100 / len(regions)
	}
	// Primary gets any remainder
	weights[primary] += 100 - (100/len(regions))*len(regions)

	healths := make([]RegionHealth, 0, len(regions))
	for _, r := range regions {
		healths = append(healths, RegionHealth{Name: r.Name, Status: "healthy", Latency: 0})
	}

	m.state = &MultiRegionState{
		Regions:       healths,
		ActiveRegion:  primary,
		PrimaryRegion: primary,
		Weights:       weights,
		Status:        "initializing",
	}

	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	switch providerType {
	case "mock":
		m.backend = &mockMultiRegionBackend{}
	default:
		return fmt.Errorf("platform.region %q: unsupported provider %q", m.name, providerType)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *MultiRegionModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "MultiRegion: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil.
func (m *MultiRegionModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Deploy deploys to the specified region.
func (m *MultiRegionModule) Deploy(region string) error {
	return m.backend.deploy(m, region)
}

// Status returns the current multi-region state.
func (m *MultiRegionModule) Status() (*MultiRegionState, error) {
	return m.backend.status(m)
}

// Failover triggers failover from one region to another.
func (m *MultiRegionModule) Failover(from, to string) error {
	return m.backend.failover(m, from, to)
}

// Promote promotes a region to primary.
func (m *MultiRegionModule) Promote(region string) error {
	return m.backend.promote(m, region)
}

// SetWeight adjusts the traffic weight for a region.
func (m *MultiRegionModule) SetWeight(region string, weight int) error {
	return m.backend.setWeight(m, region, weight)
}

// Sync synchronises state/config across all regions.
func (m *MultiRegionModule) Sync() error {
	return m.backend.sync(m)
}

// CheckHealth checks health across all regions.
func (m *MultiRegionModule) CheckHealth() ([]RegionHealth, error) {
	return m.backend.checkHealth(m)
}

// Weights returns current traffic routing weights.
func (m *MultiRegionModule) Weights() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]int, len(m.state.Weights))
	for k, v := range m.state.Weights {
		result[k] = v
	}
	return result
}

// regionConfigs parses the regions config list.
func (m *MultiRegionModule) regionConfigs() []RegionDeployConfig {
	raw, ok := m.config["regions"].([]any)
	if !ok {
		return nil
	}
	var result []RegionDeployConfig
	for _, item := range raw {
		r, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := r["name"].(string)
		provider, _ := r["provider"].(string)
		endpoint, _ := r["endpoint"].(string)
		priority, _ := r["priority"].(string)
		cfg := RegionDeployConfig{
			Name:     name,
			Provider: provider,
			Endpoint: endpoint,
			Priority: priority,
		}
		if hc, ok := r["health_check"].(map[string]any); ok {
			interval, _ := intFromAny(hc["interval"])
			timeout, _ := intFromAny(hc["timeout"])
			path, _ := hc["path"].(string)
			threshold, _ := intFromAny(hc["threshold"])
			cfg.HealthCheck = HealthCheckConfig{
				Interval:  interval,
				Timeout:   timeout,
				Path:      path,
				Threshold: threshold,
			}
		}
		result = append(result, cfg)
	}
	return result
}

// primaryRegion returns the name of the region marked as primary.
func (m *MultiRegionModule) primaryRegion(regions []RegionDeployConfig) string {
	for _, r := range regions {
		if r.Priority == "primary" {
			return r.Name
		}
	}
	return ""
}

// regionExists checks whether the given region name is configured.
func (m *MultiRegionModule) regionExists(name string) bool {
	for _, r := range m.regionConfigs() {
		if r.Name == name {
			return true
		}
	}
	return false
}

// regionHealth returns the RegionHealth for a given region name (or nil).
func (m *MultiRegionModule) regionHealthFor(name string) *RegionHealth {
	for i := range m.state.Regions {
		if m.state.Regions[i].Name == name {
			return &m.state.Regions[i]
		}
	}
	return nil
}

// ─── mock backend ─────────────────────────────────────────────────────────────

type mockMultiRegionBackend struct{}

func (b *mockMultiRegionBackend) deploy(m *MultiRegionModule, region string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.regionExists(region) {
		return fmt.Errorf("platform.region %q: region %q not configured", m.name, region)
	}
	// Mark region as healthy on successful deploy
	if h := m.regionHealthFor(region); h != nil {
		h.Status = "healthy"
		h.Latency = 10
	}
	m.state.Status = "active"
	return nil
}

func (b *mockMultiRegionBackend) status(m *MultiRegionModule) (*MultiRegionState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, nil
}

func (b *mockMultiRegionBackend) failover(m *MultiRegionModule, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.regionExists(from) {
		return fmt.Errorf("platform.region %q: source region %q not configured", m.name, from)
	}
	if !m.regionExists(to) {
		return fmt.Errorf("platform.region %q: target region %q not configured", m.name, to)
	}

	// Transition: source → failed, target → recovering → healthy
	if h := m.regionHealthFor(from); h != nil {
		h.Status = "failed"
	}
	if h := m.regionHealthFor(to); h != nil {
		h.Status = "recovering"
	}

	m.state.Status = "failing-over"
	m.state.ActiveRegion = to

	// Complete recovery
	if h := m.regionHealthFor(to); h != nil {
		h.Status = "healthy"
	}
	m.state.Status = "active"

	return nil
}

func (b *mockMultiRegionBackend) promote(m *MultiRegionModule, region string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.regionExists(region) {
		return fmt.Errorf("platform.region %q: region %q not configured", m.name, region)
	}
	m.state.PrimaryRegion = region
	m.state.ActiveRegion = region
	return nil
}

func (b *mockMultiRegionBackend) setWeight(m *MultiRegionModule, region string, weight int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.regionExists(region) {
		return fmt.Errorf("platform.region %q: region %q not configured", m.name, region)
	}
	if weight < 0 || weight > 100 {
		return fmt.Errorf("platform.region %q: weight %d out of range [0, 100]", m.name, weight)
	}
	m.state.Weights[region] = weight
	return nil
}

func (b *mockMultiRegionBackend) sync(m *MultiRegionModule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mock: mark all healthy regions as synced (noop — state is in-memory)
	return nil
}

func (b *mockMultiRegionBackend) checkHealth(m *MultiRegionModule) ([]RegionHealth, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mock: all non-failed regions report healthy
	for i := range m.state.Regions {
		if m.state.Regions[i].Status != "failed" {
			m.state.Regions[i].Status = "healthy"
			m.state.Regions[i].Latency = 5 + i*3 // mock varying latency
		}
	}
	result := make([]RegionHealth, len(m.state.Regions))
	copy(result, m.state.Regions)
	return result, nil
}
