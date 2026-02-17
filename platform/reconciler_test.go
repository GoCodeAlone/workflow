package platform

import (
	"context"
	"sync"
	"testing"
	"time"
)

// -- Mock Provider for reconciler tests --

type mockReconcilerProvider struct {
	name    string
	drivers map[string]ResourceDriver
}

func (p *mockReconcilerProvider) Name() string    { return p.name }
func (p *mockReconcilerProvider) Version() string { return "1.0.0" }
func (p *mockReconcilerProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *mockReconcilerProvider) Capabilities() []CapabilityType { return nil }
func (p *mockReconcilerProvider) MapCapability(_ context.Context, _ CapabilityDeclaration, _ *PlatformContext) ([]ResourcePlan, error) {
	return nil, nil
}
func (p *mockReconcilerProvider) ResourceDriver(resourceType string) (ResourceDriver, error) {
	d, ok := p.drivers[resourceType]
	if !ok {
		return nil, &ResourceDriverNotFoundError{ResourceType: resourceType, Provider: p.name}
	}
	return d, nil
}
func (p *mockReconcilerProvider) CredentialBroker() CredentialBroker { return nil }
func (p *mockReconcilerProvider) StateStore() StateStore             { return nil }
func (p *mockReconcilerProvider) Healthy(_ context.Context) error    { return nil }
func (p *mockReconcilerProvider) Close() error                       { return nil }

// -- Mock Resource Driver --

type mockReconcilerDriver struct {
	resourceType string
	resources    map[string]*ResourceOutput
	diffs        map[string][]DiffEntry
	readErr      map[string]error
}

func (d *mockReconcilerDriver) ResourceType() string { return d.resourceType }
func (d *mockReconcilerDriver) Create(_ context.Context, _ string, _ map[string]any) (*ResourceOutput, error) {
	return nil, nil
}
func (d *mockReconcilerDriver) Read(_ context.Context, name string) (*ResourceOutput, error) {
	if err, ok := d.readErr[name]; ok {
		return nil, err
	}
	r, ok := d.resources[name]
	if !ok {
		return nil, &ResourceNotFoundError{Name: name}
	}
	return r, nil
}
func (d *mockReconcilerDriver) Update(_ context.Context, _ string, _, _ map[string]any) (*ResourceOutput, error) {
	return nil, nil
}
func (d *mockReconcilerDriver) Delete(_ context.Context, _ string) error { return nil }
func (d *mockReconcilerDriver) HealthCheck(_ context.Context, _ string) (*HealthStatus, error) {
	return nil, nil
}
func (d *mockReconcilerDriver) Scale(_ context.Context, _ string, _ map[string]any) (*ResourceOutput, error) {
	return nil, nil
}
func (d *mockReconcilerDriver) Diff(_ context.Context, name string, _ map[string]any) ([]DiffEntry, error) {
	if diffs, ok := d.diffs[name]; ok {
		return diffs, nil
	}
	return nil, nil
}

// -- Mock State Store --

type mockReconcilerStore struct {
	mu           sync.Mutex
	resources    map[string][]*ResourceOutput
	dependencies map[string][]DependencyRef
	plans        map[string]*Plan
}

func newMockReconcilerStore() *mockReconcilerStore {
	return &mockReconcilerStore{
		resources:    make(map[string][]*ResourceOutput),
		dependencies: make(map[string][]DependencyRef),
		plans:        make(map[string]*Plan),
	}
}

func (s *mockReconcilerStore) SaveResource(_ context.Context, contextPath string, output *ResourceOutput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Replace if exists, else append
	resources := s.resources[contextPath]
	for i, r := range resources {
		if r.Name == output.Name {
			resources[i] = output
			return nil
		}
	}
	s.resources[contextPath] = append(resources, output)
	return nil
}

func (s *mockReconcilerStore) GetResource(_ context.Context, contextPath, resourceName string) (*ResourceOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.resources[contextPath] {
		if r.Name == resourceName {
			return r, nil
		}
	}
	return nil, &ResourceNotFoundError{Name: resourceName}
}

func (s *mockReconcilerStore) ListResources(_ context.Context, contextPath string) ([]*ResourceOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resources[contextPath], nil
}

func (s *mockReconcilerStore) DeleteResource(_ context.Context, contextPath, resourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	resources := s.resources[contextPath]
	for i, r := range resources {
		if r.Name == resourceName {
			s.resources[contextPath] = append(resources[:i], resources[i+1:]...)
			return nil
		}
	}
	return &ResourceNotFoundError{Name: resourceName}
}

func (s *mockReconcilerStore) SavePlan(_ context.Context, plan *Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans[plan.ID] = plan
	return nil
}

func (s *mockReconcilerStore) GetPlan(_ context.Context, planID string) (*Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[planID]
	if !ok {
		return nil, &ResourceNotFoundError{Name: planID}
	}
	return p, nil
}

func (s *mockReconcilerStore) ListPlans(_ context.Context, _ string, _ int) ([]*Plan, error) {
	return nil, nil
}

func (s *mockReconcilerStore) Lock(_ context.Context, _ string, _ time.Duration) (LockHandle, error) {
	return &mockLockHandle{}, nil
}

func (s *mockReconcilerStore) Dependencies(_ context.Context, contextPath, resourceName string) ([]DependencyRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := contextPath + "/" + resourceName
	return s.dependencies[key], nil
}

func (s *mockReconcilerStore) AddDependency(_ context.Context, dep DependencyRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := dep.SourceContext + "/" + dep.SourceResource
	s.dependencies[key] = append(s.dependencies[key], dep)
	return nil
}

type mockLockHandle struct{}

func (h *mockLockHandle) Unlock(_ context.Context) error                   { return nil }
func (h *mockLockHandle) Refresh(_ context.Context, _ time.Duration) error { return nil }

// -- Tests --

func TestReconciler_DetectsChangedResources(t *testing.T) {
	t.Parallel()

	driver := &mockReconcilerDriver{
		resourceType: "aws.ecs_service",
		resources: map[string]*ResourceOutput{
			"api-svc": {
				Name:       "api-svc",
				Properties: map[string]any{"replicas": float64(2)},
				Status:     ResourceStatusActive,
			},
		},
		diffs: map[string][]DiffEntry{
			"api-svc": {
				{Path: "replicas", OldValue: float64(3), NewValue: float64(2)},
			},
		},
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.ecs_service": driver},
	}

	store := newMockReconcilerStore()
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "api-svc",
		ProviderType: "aws.ecs_service",
		Properties:   map[string]any{"replicas": float64(3)},
		Status:       ResourceStatusActive,
	})

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(result.DriftResults) != 1 {
		t.Fatalf("len(DriftResults) = %d, want 1", len(result.DriftResults))
	}
	if result.DriftResults[0].DriftType != "changed" {
		t.Errorf("DriftType = %q, want changed", result.DriftResults[0].DriftType)
	}
	if len(result.DriftResults[0].Diffs) != 1 {
		t.Errorf("len(Diffs) = %d, want 1", len(result.DriftResults[0].Diffs))
	}
}

func TestReconciler_DetectsRemovedResources(t *testing.T) {
	t.Parallel()

	driver := &mockReconcilerDriver{
		resourceType: "aws.ecs_service",
		resources:    map[string]*ResourceOutput{},
		diffs:        map[string][]DiffEntry{},
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.ecs_service": driver},
	}

	store := newMockReconcilerStore()
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "removed-svc",
		ProviderType: "aws.ecs_service",
		Properties:   map[string]any{"replicas": float64(3)},
		Status:       ResourceStatusActive,
	})

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(result.DriftResults) != 1 {
		t.Fatalf("len(DriftResults) = %d, want 1", len(result.DriftResults))
	}
	if result.DriftResults[0].DriftType != "removed" {
		t.Errorf("DriftType = %q, want removed", result.DriftResults[0].DriftType)
	}
}

func TestReconciler_NoDrift(t *testing.T) {
	t.Parallel()

	driver := &mockReconcilerDriver{
		resourceType: "aws.ecs_service",
		resources: map[string]*ResourceOutput{
			"stable-svc": {
				Name:       "stable-svc",
				Properties: map[string]any{"replicas": float64(3)},
				Status:     ResourceStatusActive,
			},
		},
		diffs: map[string][]DiffEntry{}, // no diffs
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.ecs_service": driver},
	}

	store := newMockReconcilerStore()
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "stable-svc",
		ProviderType: "aws.ecs_service",
		Properties:   map[string]any{"replicas": float64(3)},
		Status:       ResourceStatusActive,
	})

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(result.DriftResults) != 0 {
		t.Errorf("len(DriftResults) = %d, want 0", len(result.DriftResults))
	}
	if result.ResourcesChecked != 1 {
		t.Errorf("ResourcesChecked = %d, want 1", result.ResourcesChecked)
	}
}

func TestReconciler_SkipsPendingResources(t *testing.T) {
	t.Parallel()

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{},
	}

	store := newMockReconcilerStore()
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "pending-svc",
		ProviderType: "aws.ecs_service",
		Status:       ResourceStatusPending,
		Properties:   map[string]any{},
	})
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "creating-svc",
		ProviderType: "aws.ecs_service",
		Status:       ResourceStatusCreating,
		Properties:   map[string]any{},
	})

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(result.DriftResults) != 0 {
		t.Errorf("len(DriftResults) = %d, want 0 (pending/creating resources should be skipped)", len(result.DriftResults))
	}
}

func TestReconciler_CrossTierImpact(t *testing.T) {
	t.Parallel()

	// Tier 2 shared-postgres drifts
	driver := &mockReconcilerDriver{
		resourceType: "aws.rds",
		resources: map[string]*ResourceOutput{
			"shared-postgres": {
				Name:       "shared-postgres",
				Properties: map[string]any{"instance_class": "db.r6g.xlarge"},
				Status:     ResourceStatusActive,
			},
		},
		diffs: map[string][]DiffEntry{
			"shared-postgres": {
				{Path: "instance_class", OldValue: "db.r6g.large", NewValue: "db.r6g.xlarge"},
			},
		},
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.rds": driver},
	}

	store := newMockReconcilerStore()
	store.SaveResource(context.Background(), "acme/prod", &ResourceOutput{
		Name:         "shared-postgres",
		ProviderType: "aws.rds",
		Properties:   map[string]any{"instance_class": "db.r6g.large"},
		Status:       ResourceStatusActive,
	})

	// Add cross-tier dependencies: Tier 3 api-service depends on Tier 2 shared-postgres
	store.AddDependency(context.Background(), DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "shared-postgres",
		TargetContext:  "acme/prod/api",
		TargetResource: "api-service",
		Type:           "hard",
	})
	store.AddDependency(context.Background(), DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "shared-postgres",
		TargetContext:  "acme/prod/worker",
		TargetResource: "worker-service",
		Type:           "hard",
	})

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(result.DriftResults) != 1 {
		t.Fatalf("len(DriftResults) = %d, want 1", len(result.DriftResults))
	}

	if len(result.CrossTierImpacts) != 1 {
		t.Fatalf("len(CrossTierImpacts) = %d, want 1", len(result.CrossTierImpacts))
	}

	impact := result.CrossTierImpacts[0]
	if impact.SourceDrift.ResourceName != "shared-postgres" {
		t.Errorf("SourceDrift.ResourceName = %q, want shared-postgres", impact.SourceDrift.ResourceName)
	}
	if len(impact.AffectedResources) != 2 {
		t.Errorf("len(AffectedResources) = %d, want 2", len(impact.AffectedResources))
	}
}

func TestReconciler_StartAndStop(t *testing.T) {
	t.Parallel()

	driver := &mockReconcilerDriver{
		resourceType: "aws.ecs_service",
		resources:    map[string]*ResourceOutput{},
		diffs:        map[string][]DiffEntry{},
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.ecs_service": driver},
	}

	store := newMockReconcilerStore()

	reconciler := NewReconciler(provider, store, "acme/prod", 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := reconciler.Start(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}

func TestReconciler_ReconcileJSON(t *testing.T) {
	t.Parallel()

	driver := &mockReconcilerDriver{
		resourceType: "aws.ecs_service",
		resources:    map[string]*ResourceOutput{},
		diffs:        map[string][]DiffEntry{},
	}

	provider := &mockReconcilerProvider{
		name:    "aws",
		drivers: map[string]ResourceDriver{"aws.ecs_service": driver},
	}

	store := newMockReconcilerStore()

	reconciler := NewReconciler(provider, store, "acme/prod", 5*time.Minute)
	data, err := reconciler.ReconcileJSON(context.Background())
	if err != nil {
		t.Fatalf("ReconcileJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}
