package module

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestPlatformDestroyStep_Execute(t *testing.T) {
	var deleted []string
	driver := &mockResourceDriver{
		resourceType: "test.container",
		deleteFunc: func(_ context.Context, name string) error {
			deleted = append(deleted, name)
			return nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "web-app", ProviderType: "test.container", Status: platform.ResourceStatusActive},
		{Name: "worker", ProviderType: "test.container", Status: platform.ResourceStatusActive},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	if step.Name() != "destroy-step" {
		t.Errorf("expected name %q, got %q", "destroy-step", step.Name())
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(deleted) != 2 {
		t.Errorf("expected 2 deletes, got %d", len(deleted))
	}

	summary, ok := result.Output["destroy_summary"].(map[string]any)
	if !ok {
		t.Fatal("expected destroy_summary in output")
	}
	if summary["destroyed_count"] != 2 {
		t.Errorf("expected destroyed_count 2, got %v", summary["destroyed_count"])
	}
	if summary["failed_count"] != 0 {
		t.Errorf("expected failed_count 0, got %v", summary["failed_count"])
	}
}

func TestPlatformDestroyStep_PartialFailure(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		deleteFunc: func(_ context.Context, name string) error {
			if name == "sticky-resource" {
				return fmt.Errorf("resource is protected")
			}
			return nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "normal-resource", ProviderType: "test.container"},
		{Name: "sticky-resource", ProviderType: "test.container"},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
}

func TestPlatformDestroyStep_MissingDriver(t *testing.T) {
	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{},
	}

	resources := []*platform.ResourceOutput{
		{Name: "db", ProviderType: "unknown.type"},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
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

func TestPlatformDestroyStep_MissingProvider(t *testing.T) {
	pc := NewPipelineContext(map[string]any{
		"applied_resources": []*platform.ResourceOutput{},
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
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

func TestPlatformDestroyStep_MissingResources(t *testing.T) {
	provider := &mockProvider{name: "test"}
	pc := NewPipelineContext(map[string]any{
		"provider": provider,
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
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

func TestPlatformDestroyStepFactory_MissingProviderService(t *testing.T) {
	factory := NewPlatformDestroyStepFactory()
	_, err := factory("destroy-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing provider_service")
	}
}

func TestPlatformDestroyStep_CustomResourcesFrom(t *testing.T) {
	driver := &mockResourceDriver{resourceType: "test.container"}
	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "svc", ProviderType: "test.container"},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":     provider,
		"my_resources": resources,
	}, nil)

	factory := NewPlatformDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider_service": "provider",
		"resources_from":   "my_resources",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	summary := result.Output["destroy_summary"].(map[string]any)
	if summary["destroyed_count"] != 1 {
		t.Errorf("expected destroyed_count 1, got %v", summary["destroyed_count"])
	}
}
