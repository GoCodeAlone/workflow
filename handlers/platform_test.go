package handlers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// mockPlatformProvider is a test double for platform.Provider.
type mockPlatformProvider struct {
	name         string
	version      string
	capabilities []platform.CapabilityType
	drivers      map[string]*mockResourceDriver
	stateStore   platform.StateStore
	healthy      error
	mapResult    []platform.ResourcePlan
	mapErr       error
	initErr      error
}

func (m *mockPlatformProvider) Name() string    { return m.name }
func (m *mockPlatformProvider) Version() string { return m.version }
func (m *mockPlatformProvider) Initialize(_ context.Context, _ map[string]any) error {
	return m.initErr
}
func (m *mockPlatformProvider) Capabilities() []platform.CapabilityType {
	return m.capabilities
}
func (m *mockPlatformProvider) MapCapability(_ context.Context, _ platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	return m.mapResult, m.mapErr
}
func (m *mockPlatformProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	if d, ok := m.drivers[resourceType]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("driver not found: %s", resourceType)
}
func (m *mockPlatformProvider) CredentialBroker() platform.CredentialBroker { return nil }
func (m *mockPlatformProvider) StateStore() platform.StateStore             { return m.stateStore }
func (m *mockPlatformProvider) Healthy(_ context.Context) error             { return m.healthy }
func (m *mockPlatformProvider) Close() error                                { return nil }

// mockResourceDriver is a test double for platform.ResourceDriver.
type mockResourceDriver struct {
	resourceType string
	createOutput *platform.ResourceOutput
	createErr    error
	readOutput   *platform.ResourceOutput
	readErr      error
	updateOutput *platform.ResourceOutput
	updateErr    error
	deleteErr    error
}

func (d *mockResourceDriver) ResourceType() string { return d.resourceType }
func (d *mockResourceDriver) Create(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return d.createOutput, d.createErr
}
func (d *mockResourceDriver) Read(_ context.Context, _ string) (*platform.ResourceOutput, error) {
	return d.readOutput, d.readErr
}
func (d *mockResourceDriver) Update(_ context.Context, _ string, _, _ map[string]any) (*platform.ResourceOutput, error) {
	return d.updateOutput, d.updateErr
}
func (d *mockResourceDriver) Delete(_ context.Context, _ string) error {
	return d.deleteErr
}
func (d *mockResourceDriver) HealthCheck(_ context.Context, _ string) (*platform.HealthStatus, error) {
	return &platform.HealthStatus{Status: "healthy"}, nil
}
func (d *mockResourceDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, fmt.Errorf("not scalable")
}
func (d *mockResourceDriver) Diff(_ context.Context, _ string, _ map[string]any) ([]platform.DiffEntry, error) {
	return nil, nil
}

// mockContextResolver is a test double for platform.ContextResolver.
type mockContextResolver struct {
	pctx       *platform.PlatformContext
	resolveErr error
	violations []platform.ConstraintViolation
}

func (r *mockContextResolver) ResolveContext(_ context.Context, org, env, app string, tier platform.Tier) (*platform.PlatformContext, error) {
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	if r.pctx != nil {
		return r.pctx, nil
	}
	return &platform.PlatformContext{
		Org:         org,
		Environment: env,
		Application: app,
		Tier:        tier,
	}, nil
}

func (r *mockContextResolver) PropagateOutputs(_ context.Context, _ *platform.PlatformContext, _ []*platform.ResourceOutput) error {
	return nil
}

func (r *mockContextResolver) ValidateTierBoundary(_ *platform.PlatformContext, _ []platform.CapabilityDeclaration) []platform.ConstraintViolation {
	return r.violations
}

// inMemoryStateStore is a simple in-memory state store for testing.
type inMemoryStateStore struct {
	plans     map[string]*platform.Plan
	resources map[string][]*platform.ResourceOutput
}

func newInMemoryStateStore() *inMemoryStateStore {
	return &inMemoryStateStore{
		plans:     make(map[string]*platform.Plan),
		resources: make(map[string][]*platform.ResourceOutput),
	}
}

func (s *inMemoryStateStore) SaveResource(_ context.Context, contextPath string, output *platform.ResourceOutput) error {
	s.resources[contextPath] = append(s.resources[contextPath], output)
	return nil
}

func (s *inMemoryStateStore) GetResource(_ context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	for _, r := range s.resources[contextPath] {
		if r.Name == resourceName {
			return r, nil
		}
	}
	return nil, fmt.Errorf("resource not found: %s", resourceName)
}

func (s *inMemoryStateStore) ListResources(_ context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	return s.resources[contextPath], nil
}

func (s *inMemoryStateStore) DeleteResource(_ context.Context, contextPath, resourceName string) error {
	res := s.resources[contextPath]
	for i, r := range res {
		if r.Name == resourceName {
			s.resources[contextPath] = append(res[:i], res[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *inMemoryStateStore) SavePlan(_ context.Context, plan *platform.Plan) error {
	s.plans[plan.ID] = plan
	return nil
}

func (s *inMemoryStateStore) GetPlan(_ context.Context, planID string) (*platform.Plan, error) {
	p, ok := s.plans[planID]
	if !ok {
		return nil, fmt.Errorf("plan not found: %s", planID)
	}
	return p, nil
}

func (s *inMemoryStateStore) ListPlans(_ context.Context, _ string, _ int) ([]*platform.Plan, error) {
	return nil, nil
}

func (s *inMemoryStateStore) Lock(_ context.Context, _ string, _ time.Duration) (platform.LockHandle, error) {
	return nil, nil
}

func (s *inMemoryStateStore) Dependencies(_ context.Context, _, _ string) ([]platform.DependencyRef, error) {
	return nil, nil
}

func (s *inMemoryStateStore) AddDependency(_ context.Context, _ platform.DependencyRef) error {
	return nil
}

// --- Tests ---

func TestPlatformWorkflowHandler_CanHandle(t *testing.T) {
	h := NewPlatformWorkflowHandler()

	if !h.CanHandle("platform") {
		t.Error("expected CanHandle to return true for 'platform'")
	}
	if h.CanHandle("http") {
		t.Error("expected CanHandle to return false for 'http'")
	}
	if h.CanHandle("") {
		t.Error("expected CanHandle to return false for empty string")
	}
	if h.CanHandle("platform-v2") {
		t.Error("expected CanHandle to return false for 'platform-v2'")
	}
}

func TestPlatformWorkflowHandler_ConfigureWorkflow(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	app := CreateMockApplication()

	cfg := map[string]any{
		"org":         "test-org",
		"environment": "dev",
		"provider": map[string]any{
			"name": "test-provider",
		},
		"tiers": map[string]any{
			"infrastructure": map[string]any{
				"capabilities": []any{
					map[string]any{"name": "vpc", "type": "network"},
				},
			},
		},
	}

	err := h.ConfigureWorkflow(app, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if h.config == nil {
		t.Fatal("expected config to be set")
	}
	if h.config.Org != "test-org" {
		t.Errorf("expected org 'test-org', got %q", h.config.Org)
	}
}

func TestPlatformWorkflowHandler_ConfigureWorkflow_InvalidFormat(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	app := CreateMockApplication()

	err := h.ConfigureWorkflow(app, "not-a-map")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestPlatformWorkflowHandler_ConfigureWorkflow_InvalidConfig(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	app := CreateMockApplication()

	// Missing required fields (org, environment, provider.name).
	cfg := map[string]any{
		"org": "",
	}

	err := h.ConfigureWorkflow(app, cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestPlatformWorkflowHandler_ExecutePlan(t *testing.T) {
	ss := newInMemoryStateStore()
	provider := &mockPlatformProvider{
		name:    "test",
		version: "1.0",
		mapResult: []platform.ResourcePlan{
			{ResourceType: "test.instance", Name: "web-server", Properties: map[string]any{"size": "small"}},
		},
		stateStore: ss,
	}

	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
		Tiers: platform.TiersConfig{
			Application: platform.TierConfig{
				Capabilities: []platform.CapabilityConfig{
					{Name: "web", Type: "container_runtime"},
				},
			},
		},
	}
	h.SetProvider(provider)

	result, err := h.ExecuteWorkflow(context.Background(), "platform", "plan", map[string]any{
		"tier": "application",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["action_count"] != 1 {
		t.Errorf("expected 1 action, got: %v", result["action_count"])
	}
	if result["status"] != "pending" {
		t.Errorf("expected status 'pending', got: %v", result["status"])
	}
	if result["provider"] != "test" {
		t.Errorf("expected provider 'test', got: %v", result["provider"])
	}
}

func TestPlatformWorkflowHandler_ExecuteApply(t *testing.T) {
	ss := newInMemoryStateStore()
	driver := &mockResourceDriver{
		resourceType: "test.instance",
		createOutput: &platform.ResourceOutput{
			Name:         "web-server",
			Type:         "container_runtime",
			ProviderType: "test.instance",
			Status:       platform.ResourceStatusActive,
			LastSynced:   time.Now(),
		},
	}
	provider := &mockPlatformProvider{
		name:       "test",
		version:    "1.0",
		stateStore: ss,
		drivers:    map[string]*mockResourceDriver{"test.instance": driver},
	}

	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
	}
	h.SetProvider(provider)

	// Seed a pending plan in the state store.
	plan := &platform.Plan{
		ID:     "plan-123",
		Tier:   platform.TierApplication,
		Status: "pending",
		Actions: []platform.PlanAction{
			{
				Action:       "create",
				ResourceName: "web-server",
				ResourceType: "test.instance",
				Provider:     "test",
				After:        map[string]any{"size": "small"},
			},
		},
	}
	_ = ss.SavePlan(context.Background(), plan)

	result, err := h.ExecuteWorkflow(context.Background(), "platform", "apply", map[string]any{
		"plan_id":     "plan-123",
		"approved_by": "admin@acme.com",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["status"] != "applied" {
		t.Errorf("expected status 'applied', got: %v", result["status"])
	}
	if result["resources_count"] != 1 {
		t.Errorf("expected 1 resource, got: %v", result["resources_count"])
	}
}

func TestPlatformWorkflowHandler_ExecuteApply_NoPlanID(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
	}
	h.SetProvider(&mockPlatformProvider{
		name:       "test",
		stateStore: newInMemoryStateStore(),
	})

	_, err := h.ExecuteWorkflow(context.Background(), "platform", "apply", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing plan_id")
	}
}

func TestPlatformWorkflowHandler_ExecuteDestroy(t *testing.T) {
	ss := newInMemoryStateStore()
	driver := &mockResourceDriver{
		resourceType: "test.instance",
	}
	provider := &mockPlatformProvider{
		name:       "test",
		version:    "1.0",
		stateStore: ss,
		drivers:    map[string]*mockResourceDriver{"test.instance": driver},
	}

	// Seed resources in the state store.
	_ = ss.SaveResource(context.Background(), "acme/dev", &platform.ResourceOutput{
		Name:         "web-server",
		ProviderType: "test.instance",
		Status:       platform.ResourceStatusActive,
	})

	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
	}
	h.SetProvider(provider)

	result, err := h.ExecuteWorkflow(context.Background(), "platform", "destroy", map[string]any{
		"tier": "application",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["status"] != "destroyed" {
		t.Errorf("expected status 'destroyed', got: %v", result["status"])
	}
	if result["resources_count"] != 1 {
		t.Errorf("expected 1 destroyed resource, got: %v", result["resources_count"])
	}
}

func TestPlatformWorkflowHandler_ExecuteStatus(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "prod",
		Provider:    platform.ProviderConfig{Name: "aws"},
	}
	h.SetProvider(&mockPlatformProvider{
		name:    "aws",
		version: "2.0",
	})

	result, err := h.ExecuteWorkflow(context.Background(), "platform", "status", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["org"] != "acme" {
		t.Errorf("expected org 'acme', got: %v", result["org"])
	}
	if result["provider_healthy"] != true {
		t.Errorf("expected provider_healthy true, got: %v", result["provider_healthy"])
	}
}

func TestPlatformWorkflowHandler_UnknownAction(t *testing.T) {
	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
	}

	_, err := h.ExecuteWorkflow(context.Background(), "platform", "invalid", nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestPlatformWorkflowHandler_NotConfigured(t *testing.T) {
	h := NewPlatformWorkflowHandler()

	_, err := h.ExecuteWorkflow(context.Background(), "platform", "plan", nil)
	if err == nil {
		t.Fatal("expected error when not configured")
	}
}

func TestPlatformWorkflowHandler_PlanWithContextResolver(t *testing.T) {
	ss := newInMemoryStateStore()
	provider := &mockPlatformProvider{
		name:    "test",
		version: "1.0",
		mapResult: []platform.ResourcePlan{
			{ResourceType: "test.instance", Name: "web", Properties: map[string]any{}},
		},
		stateStore: ss,
	}

	resolver := &mockContextResolver{
		pctx: &platform.PlatformContext{
			Org:         "acme",
			Environment: "staging",
			Tier:        platform.TierApplication,
		},
	}

	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "staging",
		Provider:    platform.ProviderConfig{Name: "test"},
		Tiers: platform.TiersConfig{
			Application: platform.TierConfig{
				Capabilities: []platform.CapabilityConfig{
					{Name: "web", Type: "container_runtime"},
				},
			},
		},
	}
	h.SetProvider(provider)
	h.SetContextResolver(resolver)

	result, err := h.ExecuteWorkflow(context.Background(), "platform", "plan", map[string]any{
		"tier": "application",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["context"] != "acme/staging" {
		t.Errorf("expected context 'acme/staging', got: %v", result["context"])
	}
}

func TestPlatformWorkflowHandler_TierBoundaryViolation(t *testing.T) {
	provider := &mockPlatformProvider{
		name:    "test",
		version: "1.0",
	}

	resolver := &mockContextResolver{
		violations: []platform.ConstraintViolation{
			{Message: "memory exceeds limit"},
		},
	}

	h := NewPlatformWorkflowHandler()
	h.config = &platform.PlatformConfig{
		Org:         "acme",
		Environment: "dev",
		Provider:    platform.ProviderConfig{Name: "test"},
		Tiers: platform.TiersConfig{
			Application: platform.TierConfig{
				Capabilities: []platform.CapabilityConfig{
					{Name: "web", Type: "container_runtime"},
				},
			},
		},
	}
	h.SetProvider(provider)
	h.SetContextResolver(resolver)

	_, err := h.ExecuteWorkflow(context.Background(), "platform", "plan", map[string]any{
		"tier": "application",
	})
	if err == nil {
		t.Fatal("expected error for tier boundary violation")
	}
}

func TestResolveTier(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		expected platform.Tier
	}{
		{"infrastructure string", map[string]any{"tier": "infrastructure"}, platform.TierInfrastructure},
		{"shared_primitive string", map[string]any{"tier": "shared_primitive"}, platform.TierSharedPrimitive},
		{"application string", map[string]any{"tier": "application"}, platform.TierApplication},
		{"numeric tier 1", map[string]any{"tier": float64(1)}, platform.TierInfrastructure},
		{"numeric tier 2", map[string]any{"tier": float64(2)}, platform.TierSharedPrimitive},
		{"default empty", map[string]any{}, platform.TierApplication},
		{"default unknown", map[string]any{"tier": "unknown"}, platform.TierApplication},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveTier(tt.data)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
