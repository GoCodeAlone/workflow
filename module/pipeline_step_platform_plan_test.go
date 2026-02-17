package module

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

// mockProvider implements platform.Provider for testing pipeline steps.
type mockProvider struct {
	name         string
	version      string
	capabilities []platform.CapabilityType
	mapFunc      func(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error)
	drivers      map[string]*mockResourceDriver
}

func (m *mockProvider) Name() string    { return m.name }
func (m *mockProvider) Version() string { return m.version }
func (m *mockProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (m *mockProvider) Capabilities() []platform.CapabilityType { return m.capabilities }
func (m *mockProvider) MapCapability(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	if m.mapFunc != nil {
		return m.mapFunc(ctx, decl, pctx)
	}
	return nil, fmt.Errorf("MapCapability not implemented")
}
func (m *mockProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	if d, ok := m.drivers[resourceType]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("resource driver %q not found", resourceType)
}
func (m *mockProvider) CredentialBroker() platform.CredentialBroker { return nil }
func (m *mockProvider) StateStore() platform.StateStore             { return nil }
func (m *mockProvider) Healthy(_ context.Context) error             { return nil }
func (m *mockProvider) Close() error                                { return nil }

// mockResourceDriver implements platform.ResourceDriver for testing.
type mockResourceDriver struct {
	resourceType string
	createFunc   func(ctx context.Context, name string, props map[string]any) (*platform.ResourceOutput, error)
	readFunc     func(ctx context.Context, name string) (*platform.ResourceOutput, error)
	updateFunc   func(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error)
	deleteFunc   func(ctx context.Context, name string) error
	diffFunc     func(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error)
}

func (d *mockResourceDriver) ResourceType() string { return d.resourceType }
func (d *mockResourceDriver) Create(ctx context.Context, name string, props map[string]any) (*platform.ResourceOutput, error) {
	if d.createFunc != nil {
		return d.createFunc(ctx, name, props)
	}
	return &platform.ResourceOutput{Name: name, ProviderType: d.resourceType, Status: platform.ResourceStatusActive, Properties: props}, nil
}
func (d *mockResourceDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	if d.readFunc != nil {
		return d.readFunc(ctx, name)
	}
	return &platform.ResourceOutput{Name: name, ProviderType: d.resourceType, Status: platform.ResourceStatusActive}, nil
}
func (d *mockResourceDriver) Update(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error) {
	if d.updateFunc != nil {
		return d.updateFunc(ctx, name, current, desired)
	}
	return &platform.ResourceOutput{Name: name, ProviderType: d.resourceType, Status: platform.ResourceStatusActive, Properties: desired}, nil
}
func (d *mockResourceDriver) Delete(ctx context.Context, name string) error {
	if d.deleteFunc != nil {
		return d.deleteFunc(ctx, name)
	}
	return nil
}
func (d *mockResourceDriver) HealthCheck(_ context.Context, _ string) (*platform.HealthStatus, error) {
	return &platform.HealthStatus{Status: "healthy"}, nil
}
func (d *mockResourceDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, fmt.Errorf("not scalable")
}
func (d *mockResourceDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	if d.diffFunc != nil {
		return d.diffFunc(ctx, name, desired)
	}
	return nil, nil
}

func TestPlatformPlanStep_Execute(t *testing.T) {
	provider := &mockProvider{
		name:    "test-provider",
		version: "1.0.0",
		mapFunc: func(_ context.Context, decl platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
			return []platform.ResourcePlan{
				{
					ResourceType: "test.resource",
					Name:         decl.Name + "-resource",
					Properties:   decl.Properties,
				},
			}, nil
		},
	}

	declarations := []platform.CapabilityDeclaration{
		{
			Name: "web-server",
			Type: "container_runtime",
			Tier: platform.TierApplication,
			Properties: map[string]any{
				"replicas": 3,
				"memory":   "512Mi",
			},
		},
		{
			Name: "cache",
			Type: "cache",
			Tier: platform.TierApplication,
			Properties: map[string]any{
				"engine": "redis",
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":  provider,
		"resources": declarations,
	}, nil)

	factory := NewPlatformPlanStepFactory()
	step, err := factory("plan-step", map[string]any{
		"provider_service": "provider",
		"resources_from":   "resources",
		"context_org":      "acme",
		"context_env":      "production",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	if step.Name() != "plan-step" {
		t.Errorf("expected name %q, got %q", "plan-step", step.Name())
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	plan, ok := result.Output["platform_plan"].(*platform.Plan)
	if !ok {
		t.Fatal("expected platform_plan in output")
	}

	if len(plan.Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(plan.Actions))
	}

	if plan.Provider != "test-provider" {
		t.Errorf("expected provider %q, got %q", "test-provider", plan.Provider)
	}

	if plan.Status != "pending" {
		t.Errorf("expected status %q, got %q", "pending", plan.Status)
	}

	summary, ok := result.Output["plan_summary"].(map[string]any)
	if !ok {
		t.Fatal("expected plan_summary in output")
	}
	if summary["action_count"] != 2 {
		t.Errorf("expected action_count 2, got %v", summary["action_count"])
	}
}

func TestPlatformPlanStep_MissingProvider(t *testing.T) {
	pc := NewPipelineContext(map[string]any{
		"resources": []platform.CapabilityDeclaration{},
	}, nil)

	factory := NewPlatformPlanStepFactory()
	step, err := factory("plan-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestPlatformPlanStep_MissingResources(t *testing.T) {
	provider := &mockProvider{name: "test"}
	pc := NewPipelineContext(map[string]any{
		"provider": provider,
	}, nil)

	factory := NewPlatformPlanStepFactory()
	step, err := factory("plan-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing resources")
	}
}

func TestPlatformPlanStep_MapCapabilityError(t *testing.T) {
	provider := &mockProvider{
		name: "test-provider",
		mapFunc: func(_ context.Context, _ platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
			return nil, fmt.Errorf("unsupported capability")
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider": provider,
		"resources": []platform.CapabilityDeclaration{
			{Name: "db", Type: "database"},
		},
	}, nil)

	factory := NewPlatformPlanStepFactory()
	step, err := factory("plan-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from MapCapability failure")
	}
}

func TestPlatformPlanStepFactory_MissingProviderService(t *testing.T) {
	factory := NewPlatformPlanStepFactory()
	_, err := factory("plan-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing provider_service")
	}
}

func TestPlatformPlanStep_DryRun(t *testing.T) {
	provider := &mockProvider{
		name: "test-provider",
		mapFunc: func(_ context.Context, decl platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
			return []platform.ResourcePlan{
				{ResourceType: "test.resource", Name: decl.Name, Properties: decl.Properties},
			}, nil
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider": provider,
		"resources": []platform.CapabilityDeclaration{
			{Name: "svc", Type: "container_runtime", Properties: map[string]any{}},
		},
	}, nil)

	factory := NewPlatformPlanStepFactory()
	step, err := factory("plan-step", map[string]any{
		"provider_service": "provider",
		"dry_run":          true,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	plan := result.Output["platform_plan"].(*platform.Plan)
	if !plan.DryRun {
		t.Error("expected DryRun to be true")
	}
}
