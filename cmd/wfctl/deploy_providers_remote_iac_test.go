package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// newIaCProvider builds a remoteIaCProvider backed by the given stubInvoker.
func newIaCProvider(si *stubInvoker) *remoteIaCProvider {
	return &remoteIaCProvider{invoker: si}
}

// ── Capabilities ──────────────────────────────────────────────────────────────

func TestRemoteIaC_Capabilities(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"capabilities": []any{
			map[string]any{
				"resource_type": "infra.database",
				"tier":          float64(1),
				"operations":    []any{"create", "read", "update", "delete"},
			},
		},
	}}
	p := newIaCProvider(si)

	caps := p.Capabilities()
	if si.method != "IaCProvider.Capabilities" {
		t.Errorf("method: got %q, want IaCProvider.Capabilities", si.method)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].ResourceType != "infra.database" {
		t.Errorf("ResourceType: got %q", caps[0].ResourceType)
	}
	if caps[0].Tier != 1 {
		t.Errorf("Tier: got %d", caps[0].Tier)
	}
	if len(caps[0].Operations) != 4 {
		t.Errorf("Operations: got %v", caps[0].Operations)
	}
}

func TestRemoteIaC_Capabilities_Empty(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{}}
	p := newIaCProvider(si)
	caps := p.Capabilities()
	if len(caps) != 0 {
		t.Errorf("expected empty capabilities, got %v", caps)
	}
}

func TestRemoteIaC_Capabilities_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("rpc error")}
	p := newIaCProvider(si)
	caps := p.Capabilities()
	if len(caps) != 0 {
		t.Errorf("expected nil on error, got %v", caps)
	}
}

// ── Plan ──────────────────────────────────────────────────────────────────────

func samplePlanResponse() map[string]any {
	return map[string]any{
		"id": "plan-abc",
		"actions": []any{
			map[string]any{
				"action": "create",
				"resource": map[string]any{
					"name":   "db",
					"type":   "infra.database",
					"config": map[string]any{},
				},
			},
		},
		"created_at": time.Now().Format(time.RFC3339Nano),
	}
}

func TestRemoteIaC_Plan(t *testing.T) {
	si := &stubInvoker{resp: samplePlanResponse()}
	p := newIaCProvider(si)

	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: map[string]any{"engine": "postgres"}},
	}
	current := []interfaces.ResourceState{
		{Name: "old-db", Type: "infra.database", ProviderID: "pid-old"},
	}

	plan, err := p.Plan(context.Background(), desired, current)
	if err != nil {
		t.Fatalf("Plan: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.Plan" {
		t.Errorf("method: got %q, want IaCProvider.Plan", si.method)
	}
	// Args must include desired and current as slices
	if _, ok := si.args["desired"]; !ok {
		t.Error("missing arg key 'desired'")
	}
	if _, ok := si.args["current"]; !ok {
		t.Error("missing arg key 'current'")
	}
	if plan.ID != "plan-abc" {
		t.Errorf("plan ID: got %q", plan.ID)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "create" {
		t.Errorf("action: got %q", plan.Actions[0].Action)
	}
}

func TestRemoteIaC_Plan_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("rpc error")}
	p := newIaCProvider(si)
	_, err := p.Plan(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Apply ─────────────────────────────────────────────────────────────────────

func TestRemoteIaC_Apply(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"plan_id": "plan-abc",
		"resources": []any{
			map[string]any{
				"provider_id": "pid-123",
				"name":        "db",
				"type":        "infra.database",
				"status":      "running",
			},
		},
	}}
	p := newIaCProvider(si)

	plan := &interfaces.IaCPlan{
		ID: "plan-abc",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.database"}},
		},
	}

	result, err := p.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.Apply" {
		t.Errorf("method: got %q, want IaCProvider.Apply", si.method)
	}
	// Plan must be wrapped under "plan" key
	if _, ok := si.args["plan"]; !ok {
		t.Error("missing arg key 'plan'")
	}
	if result.PlanID != "plan-abc" {
		t.Errorf("PlanID: got %q", result.PlanID)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].Name != "db" {
		t.Errorf("resource name: got %q", result.Resources[0].Name)
	}
}

func TestRemoteIaC_Apply_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("apply failed")}
	p := newIaCProvider(si)
	_, err := p.Apply(context.Background(), &interfaces.IaCPlan{ID: "p1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Destroy ───────────────────────────────────────────────────────────────────

func TestRemoteIaC_Destroy(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"destroyed": []any{"db", "cache"},
	}}
	p := newIaCProvider(si)

	refs := []interfaces.ResourceRef{
		{Name: "db", Type: "infra.database", ProviderID: "pid-1"},
		{Name: "cache", Type: "infra.cache", ProviderID: "pid-2"},
	}

	result, err := p.Destroy(context.Background(), refs)
	if err != nil {
		t.Fatalf("Destroy: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.Destroy" {
		t.Errorf("method: got %q, want IaCProvider.Destroy", si.method)
	}
	if _, ok := si.args["refs"]; !ok {
		t.Error("missing arg key 'refs'")
	}
	if len(result.Destroyed) != 2 {
		t.Fatalf("expected 2 destroyed, got %d", len(result.Destroyed))
	}
}

func TestRemoteIaC_Destroy_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("destroy failed")}
	p := newIaCProvider(si)
	_, err := p.Destroy(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Status ────────────────────────────────────────────────────────────────────

func TestRemoteIaC_Status(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"statuses": []any{
			map[string]any{
				"name":        "db",
				"type":        "infra.database",
				"provider_id": "pid-1",
				"status":      "running",
				"outputs":     map[string]any{"endpoint": "db.example.com"},
			},
		},
	}}
	p := newIaCProvider(si)

	refs := []interfaces.ResourceRef{{Name: "db", Type: "infra.database", ProviderID: "pid-1"}}
	statuses, err := p.Status(context.Background(), refs)
	if err != nil {
		t.Fatalf("Status: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.Status" {
		t.Errorf("method: got %q, want IaCProvider.Status", si.method)
	}
	if _, ok := si.args["refs"]; !ok {
		t.Error("missing arg key 'refs'")
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Name != "db" {
		t.Errorf("Name: got %q", statuses[0].Name)
	}
	if statuses[0].Status != "running" {
		t.Errorf("Status: got %q", statuses[0].Status)
	}
}

func TestRemoteIaC_Status_Empty(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{}}
	p := newIaCProvider(si)
	statuses, err := p.Status(context.Background(), nil)
	if err != nil {
		t.Fatalf("Status: unexpected error: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected empty statuses, got %v", statuses)
	}
}

// ── DetectDrift ───────────────────────────────────────────────────────────────

func TestRemoteIaC_DetectDrift(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"drifts": []any{
			map[string]any{
				"name":     "db",
				"type":     "infra.database",
				"drifted":  true,
				"expected": map[string]any{"engine": "postgres"},
				"actual":   map[string]any{"engine": "mysql"},
				"fields":   []any{"engine"},
			},
		},
	}}
	p := newIaCProvider(si)

	refs := []interfaces.ResourceRef{{Name: "db", Type: "infra.database", ProviderID: "pid-1"}}
	drifts, err := p.DetectDrift(context.Background(), refs)
	if err != nil {
		t.Fatalf("DetectDrift: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.DetectDrift" {
		t.Errorf("method: got %q, want IaCProvider.DetectDrift", si.method)
	}
	if _, ok := si.args["refs"]; !ok {
		t.Error("missing arg key 'refs'")
	}
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(drifts))
	}
	if !drifts[0].Drifted {
		t.Error("Drifted: expected true")
	}
	if len(drifts[0].Fields) != 1 || drifts[0].Fields[0] != "engine" {
		t.Errorf("Fields: got %v", drifts[0].Fields)
	}
}

func TestRemoteIaC_DetectDrift_Empty(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{}}
	p := newIaCProvider(si)
	drifts, err := p.DetectDrift(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(drifts) != 0 {
		t.Errorf("expected empty drifts, got %v", drifts)
	}
}

// ── Import ────────────────────────────────────────────────────────────────────

func TestRemoteIaC_Import(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	si := &stubInvoker{resp: map[string]any{
		"id":          "state-xyz",
		"name":        "my-db",
		"type":        "infra.database",
		"provider":    "digitalocean",
		"provider_id": "do-db-123",
		"created_at":  now.Format(time.RFC3339),
		"updated_at":  now.Format(time.RFC3339),
	}}
	p := newIaCProvider(si)

	state, err := p.Import(context.Background(), "do-db-123", "infra.database")
	if err != nil {
		t.Fatalf("Import: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.Import" {
		t.Errorf("method: got %q, want IaCProvider.Import", si.method)
	}
	if si.args["provider_id"] != "do-db-123" {
		t.Errorf("provider_id arg: got %v", si.args["provider_id"])
	}
	if si.args["resource_type"] != "infra.database" {
		t.Errorf("resource_type arg: got %v", si.args["resource_type"])
	}
	if state.ProviderID != "do-db-123" {
		t.Errorf("ProviderID: got %q", state.ProviderID)
	}
	if state.Type != "infra.database" {
		t.Errorf("Type: got %q", state.Type)
	}
}

func TestRemoteIaC_Import_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("not found")}
	p := newIaCProvider(si)
	_, err := p.Import(context.Background(), "pid-x", "infra.database")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── ResolveSizing ─────────────────────────────────────────────────────────────

func TestRemoteIaC_ResolveSizing(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"instance_type": "db-s-1vcpu-1gb",
		"specs": map[string]any{
			"cpu":    "1",
			"memory": "1Gi",
		},
	}}
	p := newIaCProvider(si)

	hints := &interfaces.ResourceHints{CPU: "1", Memory: "1Gi"}
	sizing, err := p.ResolveSizing("infra.database", interfaces.SizeS, hints)
	if err != nil {
		t.Fatalf("ResolveSizing: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.ResolveSizing" {
		t.Errorf("method: got %q, want IaCProvider.ResolveSizing", si.method)
	}
	if si.args["resource_type"] != "infra.database" {
		t.Errorf("resource_type: got %v", si.args["resource_type"])
	}
	if si.args["size"] != "s" {
		t.Errorf("size: got %v", si.args["size"])
	}
	if _, ok := si.args["hints"]; !ok {
		t.Error("missing arg key 'hints'")
	}
	if sizing.InstanceType != "db-s-1vcpu-1gb" {
		t.Errorf("InstanceType: got %q", sizing.InstanceType)
	}
}

func TestRemoteIaC_ResolveSizing_NilHints(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"instance_type": "db-s-1vcpu-1gb",
		"specs":         map[string]any{},
	}}
	p := newIaCProvider(si)
	sizing, err := p.ResolveSizing("infra.database", interfaces.SizeM, nil)
	if err != nil {
		t.Fatalf("ResolveSizing: unexpected error: %v", err)
	}
	if sizing.InstanceType != "db-s-1vcpu-1gb" {
		t.Errorf("InstanceType: got %q", sizing.InstanceType)
	}
}

func TestRemoteIaC_ResolveSizing_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("unsupported size")}
	p := newIaCProvider(si)
	_, err := p.ResolveSizing("infra.database", interfaces.SizeXL, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Migration Repair ────────────────────────────────────────────────────────

func TestRemoteIaCProvider_RepairDirtyMigration(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"provider_job_id": "job-123",
		"status":          interfaces.MigrationRepairStatusSucceeded,
		"applied":         []any{"20260426000006"},
		"logs":            "repair complete",
	}}
	p := newIaCProvider(si)

	result, err := p.RepairDirtyMigration(context.Background(), interfaces.MigrationRepairRequest{
		AppResourceName:      "bmw-app",
		DatabaseResourceName: "bmw-db",
		JobImage:             "registry.example/workflow-migrate:sha",
		SourceDir:            "/migrations",
		ExpectedDirtyVersion: "20260426000005",
		ForceVersion:         "20260426000004",
		ThenUp:               true,
		ConfirmForce:         interfaces.MigrationRepairConfirmation,
		Env:                  map[string]string{"DATABASE_URL": "postgres://example"},
		TimeoutSeconds:       600,
	})
	if err != nil {
		t.Fatalf("RepairDirtyMigration: unexpected error: %v", err)
	}
	if si.method != "IaCProvider.RepairDirtyMigration" {
		t.Errorf("method: got %q, want IaCProvider.RepairDirtyMigration", si.method)
	}
	request, ok := si.args["request"].(map[string]any)
	if !ok {
		t.Fatalf("request arg: got %T, want map[string]any", si.args["request"])
	}
	if request["expected_dirty_version"] != "20260426000005" {
		t.Errorf("expected_dirty_version arg: got %v", request["expected_dirty_version"])
	}
	if result.ProviderJobID != "job-123" {
		t.Errorf("ProviderJobID: got %q", result.ProviderJobID)
	}
	if result.Status != interfaces.MigrationRepairStatusSucceeded {
		t.Errorf("Status: got %q", result.Status)
	}
	if len(result.Applied) != 1 || result.Applied[0] != "20260426000006" {
		t.Errorf("Applied: got %v", result.Applied)
	}
}

func TestRemoteIaCProvider_RepairDirtyMigration_DecodeError(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"status": 123,
	}}
	p := newIaCProvider(si)

	_, err := p.RepairDirtyMigration(context.Background(), interfaces.MigrationRepairRequest{})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "IaCProvider.RepairDirtyMigration: decode result") {
		t.Fatalf("error %q missing decode context", err)
	}
}
