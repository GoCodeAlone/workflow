package module

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestDriftCheckStep_NoDrift(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		diffFunc: func(_ context.Context, _ string, _ map[string]any) ([]platform.DiffEntry, error) {
			return nil, nil // no drift
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "web-app", ProviderType: "test.container", Properties: map[string]any{"replicas": 3}},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	if step.Name() != "drift-check" {
		t.Errorf("expected name %q, got %q", "drift-check", step.Name())
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	reports, ok := result.Output["drift_reports"].([]DriftReport)
	if !ok {
		t.Fatal("expected drift_reports in output")
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if reports[0].Drifted {
		t.Error("expected no drift")
	}

	summary := result.Output["drift_summary"].(map[string]any)
	if summary["drift_detected"] != false {
		t.Error("expected drift_detected false")
	}
}

func TestDriftCheckStep_DriftDetected(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		diffFunc: func(_ context.Context, _ string, _ map[string]any) ([]platform.DiffEntry, error) {
			return []platform.DiffEntry{
				{Path: "properties.replicas", OldValue: 3, NewValue: 1},
			}, nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "web-app", ProviderType: "test.container", Properties: map[string]any{"replicas": 3}},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	reports := result.Output["drift_reports"].([]DriftReport)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if !reports[0].Drifted {
		t.Error("expected drift to be detected")
	}
	if len(reports[0].Diffs) != 1 {
		t.Errorf("expected 1 diff entry, got %d", len(reports[0].Diffs))
	}

	summary := result.Output["drift_summary"].(map[string]any)
	if summary["drift_detected"] != true {
		t.Error("expected drift_detected true")
	}
	if summary["drifted_count"] != 1 {
		t.Errorf("expected drifted_count 1, got %v", summary["drifted_count"])
	}
}

func TestDriftCheckStep_MultipleResources(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		diffFunc: func(_ context.Context, name string, _ map[string]any) ([]platform.DiffEntry, error) {
			if name == "drifted-svc" {
				return []platform.DiffEntry{
					{Path: "properties.memory", OldValue: "512Mi", NewValue: "256Mi"},
				}, nil
			}
			return nil, nil
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "stable-svc", ProviderType: "test.container", Properties: map[string]any{"memory": "512Mi"}},
		{Name: "drifted-svc", ProviderType: "test.container", Properties: map[string]any{"memory": "512Mi"}},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	reports := result.Output["drift_reports"].([]DriftReport)
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}

	summary := result.Output["drift_summary"].(map[string]any)
	if summary["drifted_count"] != 1 {
		t.Errorf("expected drifted_count 1, got %v", summary["drifted_count"])
	}
}

func TestDriftCheckStep_DiffError(t *testing.T) {
	driver := &mockResourceDriver{
		resourceType: "test.container",
		diffFunc: func(_ context.Context, _ string, _ map[string]any) ([]platform.DiffEntry, error) {
			return nil, fmt.Errorf("provider unreachable")
		},
	}

	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{"test.container": driver},
	}

	resources := []*platform.ResourceOutput{
		{Name: "svc", ProviderType: "test.container", Properties: map[string]any{}},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute should not fail on individual resource errors: %v", err)
	}

	reports := result.Output["drift_reports"].([]DriftReport)
	if reports[0].Error == "" {
		t.Error("expected error in drift report")
	}
}

func TestDriftCheckStep_MissingDriver(t *testing.T) {
	provider := &mockProvider{
		name:    "test-provider",
		drivers: map[string]*mockResourceDriver{},
	}

	resources := []*platform.ResourceOutput{
		{Name: "db", ProviderType: "unknown.type", Properties: map[string]any{}},
	}

	pc := NewPipelineContext(map[string]any{
		"provider":          provider,
		"applied_resources": resources,
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
		"provider_service": "provider",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute should not fail on missing driver: %v", err)
	}

	reports := result.Output["drift_reports"].([]DriftReport)
	if reports[0].Error == "" {
		t.Error("expected error in drift report for missing driver")
	}
}

func TestDriftCheckStepFactory_MissingProviderService(t *testing.T) {
	factory := NewDriftCheckStepFactory()
	_, err := factory("drift-check", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing provider_service")
	}
}

func TestDriftCheckStep_MissingProvider(t *testing.T) {
	pc := NewPipelineContext(map[string]any{
		"applied_resources": []*platform.ResourceOutput{},
	}, nil)

	factory := NewDriftCheckStepFactory()
	step, err := factory("drift-check", map[string]any{
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
