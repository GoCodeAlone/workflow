package infra

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// InfraConfig represents the infrastructure block in a workflow config.
type InfraConfig struct {
	Resources []ResourceConfig `json:"resources" yaml:"resources"`
}

// ResourceConfig describes a single infrastructure resource to provision.
type ResourceConfig struct {
	Name     string         `json:"name" yaml:"name"`
	Type     string         `json:"type" yaml:"type"`         // "database", "cache", "queue", "storage"
	Provider string         `json:"provider" yaml:"provider"` // "sqlite", "postgres", "redis", "memory", "s3"
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// ProvisionPlan describes what would be created, modified, or deleted.
type ProvisionPlan struct {
	Create  []ResourceConfig `json:"create"`
	Update  []ResourceConfig `json:"update"`
	Delete  []ResourceConfig `json:"delete"`
	Current []ResourceConfig `json:"current"` // existing resources
}

// ProvisionedResource tracks a live provisioned resource.
type ProvisionedResource struct {
	Config    ResourceConfig `json:"config"`
	Status    string         `json:"status"` // "provisioned", "pending", "failed", "destroying"
	CreatedAt time.Time      `json:"created_at"`
	Error     string         `json:"error,omitempty"`
}

// validResourceTypes enumerates the supported resource types.
var validResourceTypes = map[string]bool{
	"database": true,
	"cache":    true,
	"queue":    true,
	"storage":  true,
}

// validProviders maps resource types to their allowed providers.
var validProviders = map[string]map[string]bool{
	"database": {"sqlite": true, "postgres": true, "memory": true},
	"cache":    {"redis": true, "memory": true},
	"queue":    {"memory": true, "nats": true, "kafka": true},
	"storage":  {"memory": true, "s3": true, "filesystem": true},
}

// Provisioner manages infrastructure resources declared in workflow configs.
type Provisioner struct {
	mu        sync.RWMutex
	resources map[string]*ProvisionedResource
	logger    *slog.Logger
}

// NewProvisioner creates a new Provisioner.
func NewProvisioner(logger *slog.Logger) *Provisioner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provisioner{
		resources: make(map[string]*ProvisionedResource),
		logger:    logger,
	}
}

// Plan computes the diff between the current state and the desired InfraConfig.
func (p *Provisioner) Plan(desired InfraConfig) (*ProvisionPlan, error) {
	if err := validateInfraConfig(desired); err != nil {
		return nil, fmt.Errorf("invalid infra config: %w", err)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	plan := &ProvisionPlan{
		Create:  make([]ResourceConfig, 0),
		Update:  make([]ResourceConfig, 0),
		Delete:  make([]ResourceConfig, 0),
		Current: make([]ResourceConfig, 0),
	}

	// Build map of desired resources keyed by name.
	desiredMap := make(map[string]ResourceConfig, len(desired.Resources))
	for _, r := range desired.Resources {
		desiredMap[r.Name] = r
	}

	// Collect current resource configs.
	for _, res := range p.resources {
		plan.Current = append(plan.Current, res.Config)
	}

	// Determine creates and updates.
	for _, d := range desired.Resources {
		existing, exists := p.resources[d.Name]
		if !exists {
			plan.Create = append(plan.Create, d)
		} else if resourceDiffers(existing.Config, d) {
			plan.Update = append(plan.Update, d)
		}
	}

	// Determine deletes: resources that exist but are not in desired.
	for name, res := range p.resources {
		if _, wanted := desiredMap[name]; !wanted {
			plan.Delete = append(plan.Delete, res.Config)
		}
	}

	// Sort for deterministic output.
	sortResources(plan.Create)
	sortResources(plan.Update)
	sortResources(plan.Delete)
	sortResources(plan.Current)

	return plan, nil
}

// Apply executes a provision plan, creating, updating, and deleting resources.
// For now this performs mock/local provisioning (no real cloud resources).
func (p *Provisioner) Apply(ctx context.Context, plan *ProvisionPlan) error {
	if plan == nil {
		return fmt.Errorf("provision plan is nil")
	}

	// Process deletes first.
	for _, rc := range plan.Delete {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled during delete: %w", err)
		}
		if err := p.destroyResource(rc.Name); err != nil {
			return fmt.Errorf("failed to delete resource %q: %w", rc.Name, err)
		}
	}

	// Process creates.
	for _, rc := range plan.Create {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled during create: %w", err)
		}
		if err := p.provisionResource(rc); err != nil {
			return fmt.Errorf("failed to create resource %q: %w", rc.Name, err)
		}
	}

	// Process updates (destroy then re-provision).
	for _, rc := range plan.Update {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled during update: %w", err)
		}
		if err := p.destroyResource(rc.Name); err != nil {
			return fmt.Errorf("failed to destroy resource %q for update: %w", rc.Name, err)
		}
		if err := p.provisionResource(rc); err != nil {
			return fmt.Errorf("failed to re-create resource %q for update: %w", rc.Name, err)
		}
	}

	p.logger.Info("provision plan applied",
		"created", len(plan.Create),
		"updated", len(plan.Update),
		"deleted", len(plan.Delete),
	)
	return nil
}

// Destroy tears down a single named resource.
func (p *Provisioner) Destroy(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}
	return p.destroyResource(name)
}

// Status returns the current state of all provisioned resources.
func (p *Provisioner) Status() map[string]*ProvisionedResource {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*ProvisionedResource, len(p.resources))
	for k, v := range p.resources {
		copied := *v
		result[k] = &copied
	}
	return result
}

// ParseConfig parses a raw map (from YAML) into an InfraConfig.
func ParseConfig(raw map[string]any) (*InfraConfig, error) {
	if raw == nil {
		return nil, fmt.Errorf("config is nil")
	}

	resourcesRaw, ok := raw["resources"]
	if !ok {
		return &InfraConfig{Resources: nil}, nil
	}

	resourcesList, ok := resourcesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("resources must be a list")
	}

	cfg := &InfraConfig{
		Resources: make([]ResourceConfig, 0, len(resourcesList)),
	}

	for i, item := range resourcesList {
		resMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("resource at index %d is not a map", i)
		}

		rc := ResourceConfig{
			Config: make(map[string]any),
		}

		if name, ok := resMap["name"].(string); ok {
			rc.Name = name
		} else {
			return nil, fmt.Errorf("resource at index %d missing 'name'", i)
		}

		if typ, ok := resMap["type"].(string); ok {
			rc.Type = typ
		} else {
			return nil, fmt.Errorf("resource %q missing 'type'", rc.Name)
		}

		if provider, ok := resMap["provider"].(string); ok {
			rc.Provider = provider
		} else {
			return nil, fmt.Errorf("resource %q missing 'provider'", rc.Name)
		}

		if cfgMap, ok := resMap["config"].(map[string]any); ok {
			rc.Config = cfgMap
		}

		cfg.Resources = append(cfg.Resources, rc)
	}

	return cfg, nil
}

// provisionResource creates a resource in the internal store.
func (p *Provisioner) provisionResource(rc ResourceConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.resources[rc.Name]; exists {
		return fmt.Errorf("resource %q already exists", rc.Name)
	}

	p.resources[rc.Name] = &ProvisionedResource{
		Config:    rc,
		Status:    "provisioned",
		CreatedAt: time.Now(),
	}

	p.logger.Info("resource provisioned", "name", rc.Name, "type", rc.Type, "provider", rc.Provider)
	return nil
}

// destroyResource removes a resource from the internal store.
func (p *Provisioner) destroyResource(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	res, exists := p.resources[name]
	if !exists {
		return fmt.Errorf("resource %q not found", name)
	}

	res.Status = "destroying"
	delete(p.resources, name)

	p.logger.Info("resource destroyed", "name", name)
	return nil
}

// validateInfraConfig checks that all resources have valid types and providers.
func validateInfraConfig(cfg InfraConfig) error {
	seen := make(map[string]bool)
	for _, r := range cfg.Resources {
		if r.Name == "" {
			return fmt.Errorf("resource name is required")
		}
		if seen[r.Name] {
			return fmt.Errorf("duplicate resource name %q", r.Name)
		}
		seen[r.Name] = true

		if !validResourceTypes[r.Type] {
			return fmt.Errorf("unsupported resource type %q for resource %q", r.Type, r.Name)
		}
		providers, ok := validProviders[r.Type]
		if !ok || !providers[r.Provider] {
			return fmt.Errorf("unsupported provider %q for resource type %q (resource %q)", r.Provider, r.Type, r.Name)
		}
	}
	return nil
}

// resourceDiffers returns true if the existing config differs from the desired config.
func resourceDiffers(existing, desired ResourceConfig) bool {
	if existing.Type != desired.Type || existing.Provider != desired.Provider {
		return true
	}
	// Compare config maps: if lengths differ, they differ.
	if len(existing.Config) != len(desired.Config) {
		return true
	}
	for k, v := range existing.Config {
		if dv, ok := desired.Config[k]; !ok || fmt.Sprintf("%v", v) != fmt.Sprintf("%v", dv) {
			return true
		}
	}
	return false
}

// sortResources sorts a slice of ResourceConfig by name for deterministic output.
func sortResources(resources []ResourceConfig) {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})
}
