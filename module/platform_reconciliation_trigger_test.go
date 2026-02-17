package module

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// -- Mock implementations for trigger tests --

type mockTriggerProvider struct {
	name    string
	store   platform.StateStore
	drivers map[string]platform.ResourceDriver
}

func (p *mockTriggerProvider) Name() string    { return p.name }
func (p *mockTriggerProvider) Version() string { return "1.0.0" }
func (p *mockTriggerProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *mockTriggerProvider) Capabilities() []platform.CapabilityType { return nil }
func (p *mockTriggerProvider) MapCapability(_ context.Context, _ platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	return nil, nil
}
func (p *mockTriggerProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	d, ok := p.drivers[resourceType]
	if !ok {
		return nil, &platform.ResourceDriverNotFoundError{ResourceType: resourceType, Provider: p.name}
	}
	return d, nil
}
func (p *mockTriggerProvider) CredentialBroker() platform.CredentialBroker { return nil }
func (p *mockTriggerProvider) StateStore() platform.StateStore             { return p.store }
func (p *mockTriggerProvider) Healthy(_ context.Context) error             { return nil }
func (p *mockTriggerProvider) Close() error                                { return nil }

type mockTriggerStateStore struct {
	resources    []*platform.ResourceOutput
	dependencies map[string][]platform.DependencyRef
}

func newMockTriggerStateStore() *mockTriggerStateStore {
	return &mockTriggerStateStore{
		dependencies: make(map[string][]platform.DependencyRef),
	}
}

func (s *mockTriggerStateStore) SaveResource(_ context.Context, _ string, _ *platform.ResourceOutput) error {
	return nil
}
func (s *mockTriggerStateStore) GetResource(_ context.Context, _, _ string) (*platform.ResourceOutput, error) {
	return nil, &platform.ResourceNotFoundError{}
}
func (s *mockTriggerStateStore) ListResources(_ context.Context, _ string) ([]*platform.ResourceOutput, error) {
	return s.resources, nil
}
func (s *mockTriggerStateStore) DeleteResource(_ context.Context, _, _ string) error { return nil }
func (s *mockTriggerStateStore) SavePlan(_ context.Context, _ *platform.Plan) error  { return nil }
func (s *mockTriggerStateStore) GetPlan(_ context.Context, _ string) (*platform.Plan, error) {
	return nil, &platform.ResourceNotFoundError{}
}
func (s *mockTriggerStateStore) ListPlans(_ context.Context, _ string, _ int) ([]*platform.Plan, error) {
	return nil, nil
}
func (s *mockTriggerStateStore) Lock(_ context.Context, _ string, _ time.Duration) (platform.LockHandle, error) {
	return &mockTriggerLock{}, nil
}
func (s *mockTriggerStateStore) Dependencies(_ context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error) {
	key := contextPath + "/" + resourceName
	return s.dependencies[key], nil
}
func (s *mockTriggerStateStore) AddDependency(_ context.Context, dep platform.DependencyRef) error {
	key := dep.SourceContext + "/" + dep.SourceResource
	s.dependencies[key] = append(s.dependencies[key], dep)
	return nil
}

type mockTriggerLock struct{}

func (l *mockTriggerLock) Unlock(_ context.Context) error                   { return nil }
func (l *mockTriggerLock) Refresh(_ context.Context, _ time.Duration) error { return nil }

func TestReconciliationTrigger_Name(t *testing.T) {
	t.Parallel()
	trigger := NewReconciliationTrigger()
	if trigger.Name() != ReconciliationTriggerName {
		t.Errorf("Name() = %q, want %q", trigger.Name(), ReconciliationTriggerName)
	}
}

func TestReconciliationTrigger_StartAndStop(t *testing.T) {
	t.Parallel()

	store := newMockTriggerStateStore()
	provider := &mockTriggerProvider{
		name:    "mock",
		store:   store,
		drivers: map[string]platform.ResourceDriver{},
	}

	trigger := NewReconciliationTrigger()
	trigger.interval = 50 * time.Millisecond
	trigger.provider = provider
	trigger.store = store
	trigger.ctxPath = "test/reconciliation"

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Let the reconciler run a couple of cycles.
	time.Sleep(150 * time.Millisecond)

	stopCtx := context.Background()
	if err := trigger.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestReconciliationTrigger_StartWithoutProvider(t *testing.T) {
	t.Parallel()

	trigger := NewReconciliationTrigger()
	trigger.interval = time.Minute
	trigger.ctxPath = "test/no-provider"
	// provider is nil — Start should be a no-op

	err := trigger.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when starting without provider (no-op), got: %v", err)
	}
}

func TestReconciliationTrigger_StartWithoutStore(t *testing.T) {
	t.Parallel()

	provider := &mockTriggerProvider{
		name: "mock",
	}

	trigger := NewReconciliationTrigger()
	trigger.interval = time.Minute
	trigger.provider = provider
	trigger.ctxPath = "test/no-store"
	// store is nil — Start should be a no-op

	err := trigger.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when starting without store (no-op), got: %v", err)
	}
}

func TestReconciliationTrigger_Dependencies(t *testing.T) {
	t.Parallel()
	trigger := NewReconciliationTrigger()
	deps := trigger.Dependencies()
	if deps != nil {
		t.Errorf("Dependencies() = %v, want nil", deps)
	}
}
