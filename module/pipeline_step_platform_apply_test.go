package module

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestPlatformApplyStep_Execute(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		createFunc: func(_ context.Context, name string, props map[string]any) (*platform.ResourceOutput, error) {
			return &platform.ResourceOutput{
				Name:         name,
				ProviderType: "test.container",
				Status:       platform.ResourceStatusActive,
				Properties:   props,
			}, nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	plan := &platform.Plan{
		ID:       "plan-1",
		Provider: "test-provider",
		Actions: []platform.PlanAction{
			{
				Action:       "create",
				ResourceName: "web-app",
				ResourceType: "test.container",
				After:        map[string]any{"replicas": 3},
			},
			{
				Action:       "create",
				ResourceName: "worker",
				ResourceType: "test.container",
				After:        map[string]any{"replicas": 1},
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":      provider,
		"platform_plan": plan,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	if step.Name() != "apply-step" {
		t.Errorf("expected name %q, got %q", "apply-step", step.Name())
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	applied, ok := result.Output["applied_resources"].([]*platform.ResourceOutput)
	if !ok {
		t.Fatal("expected applied_resources in output")
	}
	if len(applied) != 2 {
		t.Errorf("expected 2 applied resources, got %d", len(applied))
	}

	summary, ok := result.Output["apply_summary"].(map[string]any)
	if !ok {
		t.Fatal("expected apply_summary in output")
	}
	if summary["applied_count"] != 2 {
		t.Errorf("expected applied_count 2, got %v", summary["applied_count"])
	}
	if summary["failed_count"] != 0 {
		t.Errorf("expected failed_count 0, got %v", summary["failed_count"])
	}
}

func TestPlatformApplyStep_UpdateAction(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		updateFunc: func(_ context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
			return &platform.ResourceOutput{
				Name:         name,
				ProviderType: "test.container",
				Status:       platform.ResourceStatusActive,
				Properties:   desired,
			}, nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	plan := &platform.Plan{
		ID: "plan-update",
		Actions: []platform.PlanAction{
			{
				Action:       "update",
				ResourceName: "web-app",
				ResourceType: "test.container",
				Before:       map[string]any{"replicas": 1},
				After:        map[string]any{"replicas": 3},
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":      provider,
		"platform_plan": plan,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	applied := result.Output["applied_resources"].([]*platform.ResourceOutput)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied resource, got %d", len(applied))
	}
	if applied[0].Properties["replicas"] != 3 {
		t.Errorf("expected replicas 3, got %v", applied[0].Properties["replicas"])
	}
}

func TestPlatformApplyStep_DriverError(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		createFunc: func(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
			return nil, fmt.Errorf("create failed: quota exceeded")
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	plan := &platform.Plan{
		ID: "plan-fail",
		Actions: []platform.PlanAction{
			{
				Action:       "create",
				ResourceName: "web-app",
				ResourceType: "test.container",
				After:        map[string]any{"replicas": 100},
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":      provider,
		"platform_plan": plan,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from driver failure")
	}
}

func TestPlatformApplyStep_MissingDriver(t *testing.T) {
	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{},
	}

	plan := &platform.Plan{
		ID: "plan-no-driver",
		Actions: []platform.PlanAction{
			{
				Action:       "create",
				ResourceName: "db",
				ResourceType: "unknown.type",
				After:        map[string]any{},
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":      provider,
		"platform_plan": plan,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing driver")
	}
}

func TestPlatformApplyStep_MissingPlan(t *testing.T) {
	provider := &mockProvider{name: "test"}
	pc := NewPipelineContext(map[string]any{
		"provider": provider,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing plan")
	}
}

func TestPlatformApplyStepFactory_MissingProviderService(t *testing.T) {
	factory := NewPlatformApplyStepFactory()
	_, err := factory("apply-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing provider_service")
	}
}

func TestPlatformApplyStep_CustomPlanFrom(t *testing.T) {
	driver := &mockResourceDriver{resourceType: "test.container"}
	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	plan := &platform.Plan{
		ID: "custom-plan",
		Actions: []platform.PlanAction{
			{Action: "create", ResourceName: "svc", ResourceType: "test.container", After: map[string]any{}},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":    provider,
		"custom_plan": plan,
	}, nil)

	factory := NewPlatformApplyStepFactory()
	step, err := factory("apply-step", map[string]any{
		"provider_service": "provider",
		"plan_from":        "custom_plan",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	applied := result.Output["applied_resources"].([]*platform.ResourceOutput)
	if len(applied) != 1 {
		t.Errorf("expected 1 applied resource, got %d", len(applied))
	}
}
