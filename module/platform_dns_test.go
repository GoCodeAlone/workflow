package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// helpers

func newDNSApp(t *testing.T) (*module.MockApplication, *module.PlatformDNS) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("prod-dns", map[string]any{
		"provider": "mock",
		"zone": map[string]any{
			"name":    "example.com",
			"comment": "production zone",
		},
		"records": []any{
			map[string]any{"name": "api.example.com", "type": "A", "value": "10.0.1.50", "ttl": 300},
			map[string]any{"name": "www.example.com", "type": "CNAME", "value": "cdn.example.com", "ttl": 3600},
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module lifecycle ─────────────────────────────────────────────────────────

func TestPlatformDNS_Init(t *testing.T) {
	_, m := newDNSApp(t)
	if m.Name() != "prod-dns" {
		t.Errorf("expected name=prod-dns, got %q", m.Name())
	}
}

func TestPlatformDNS_InitRegistersService(t *testing.T) {
	app, _ := newDNSApp(t)
	svc, ok := app.Services["prod-dns"]
	if !ok {
		t.Fatal("expected prod-dns in service registry")
	}
	if _, ok := svc.(*module.PlatformDNS); !ok {
		t.Fatalf("registry entry is %T, want *PlatformDNS", svc)
	}
}

func TestPlatformDNS_Plan_CreateOnPending(t *testing.T) {
	_, m := newDNSApp(t)
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Zone.Name != "example.com" {
		t.Errorf("expected zone=example.com, got %q", plan.Zone.Name)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change")
	}
	// first change should mention creating the zone
	found := false
	for _, c := range plan.Changes {
		if c != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected non-empty changes")
	}
}

func TestPlatformDNS_Plan_ContainsRecords(t *testing.T) {
	_, m := newDNSApp(t)
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Records) != 2 {
		t.Errorf("expected 2 records in plan, got %d", len(plan.Records))
	}
}

func TestPlatformDNS_Apply_CreatesZone(t *testing.T) {
	_, m := newDNSApp(t)
	state, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.ZoneID == "" {
		t.Error("expected non-empty zoneId after apply")
	}
	if state.ZoneName != "example.com" {
		t.Errorf("expected zoneName=example.com, got %q", state.ZoneName)
	}
	if len(state.Records) != 2 {
		t.Errorf("expected 2 records after apply, got %d", len(state.Records))
	}
}

func TestPlatformDNS_Plan_NoopAfterApply(t *testing.T) {
	_, m := newDNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if len(plan.Changes) == 0 || plan.Changes[0] != "no changes" {
		t.Errorf("expected 'no changes' after apply, got %v", plan.Changes)
	}
}

func TestPlatformDNS_Status(t *testing.T) {
	_, m := newDNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	state, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
}

func TestPlatformDNS_Destroy(t *testing.T) {
	_, m := newDNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	state, err := m.Status()
	if err != nil {
		t.Fatalf("Status after destroy: %v", err)
	}
	if state.Status != "deleted" {
		t.Errorf("expected status=deleted after destroy, got %q", state.Status)
	}
	if len(state.Records) != 0 {
		t.Errorf("expected 0 records after destroy, got %d", len(state.Records))
	}
}

func TestPlatformDNS_DestroyIdempotent(t *testing.T) {
	_, m := newDNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("second Destroy: %v", err)
	}
}

// ─── invalid config ───────────────────────────────────────────────────────────

func TestPlatformDNS_Init_MissingZoneName(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("bad-dns", map[string]any{
		"provider": "mock",
		"zone":     map[string]any{},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for missing zone.name, got nil")
	}
}

func TestPlatformDNS_Init_InvalidRecordType(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("bad-dns", map[string]any{
		"provider": "mock",
		"zone":     map[string]any{"name": "example.com"},
		"records": []any{
			map[string]any{"name": "api.example.com", "type": "BADTYPE", "value": "1.2.3.4"},
		},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for invalid record type, got nil")
	}
}

func TestPlatformDNS_Init_UnsupportedProvider(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("bad-dns", map[string]any{
		"provider": "cloudflare",
		"zone":     map[string]any{"name": "example.com"},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestPlatformDNS_Init_InvalidAccountRef(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("fail-dns", map[string]any{
		"provider": "mock",
		"account":  "nonexistent",
		"zone":     map[string]any{"name": "example.com"},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// ─── record type validation ───────────────────────────────────────────────────

func TestPlatformDNS_ValidRecordTypes(t *testing.T) {
	validTypes := []string{"A", "AAAA", "CNAME", "ALIAS", "TXT", "MX", "SRV", "NS", "PTR"}
	for _, rt := range validTypes {
		app := module.NewMockApplication()
		m := module.NewPlatformDNS("dns-"+rt, map[string]any{
			"provider": "mock",
			"zone":     map[string]any{"name": "example.com"},
			"records": []any{
				map[string]any{"name": "test.example.com", "type": rt, "value": "1.2.3.4"},
			},
		})
		if err := m.Init(app); err != nil {
			t.Errorf("type %q: unexpected Init error: %v", rt, err)
		}
	}
}

// ─── Route53 stub ─────────────────────────────────────────────────────────────

func TestPlatformDNS_Route53_PlanReturnsStub(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("r53-dns", map[string]any{
		"provider": "aws",
		"zone":     map[string]any{"name": "aws.example.com"},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change from route53 stub")
	}
}

func TestPlatformDNS_Route53_ApplyNotImplemented(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDNS("r53-dns", map[string]any{
		"provider": "aws",
		"zone":     map[string]any{"name": "aws.example.com"},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.Apply(); err == nil {
		t.Error("expected error from route53 Apply stub, got nil")
	}
}

// ─── pipeline steps ───────────────────────────────────────────────────────────

func setupDNSApp(t *testing.T) (*module.MockApplication, *module.PlatformDNS) {
	t.Helper()
	return newDNSApp(t)
}

func TestDNSPlanStep(t *testing.T) {
	app, _ := setupDNSApp(t)
	factory := module.NewDNSPlanStepFactory()
	step, err := factory("plan", map[string]any{"zone": "prod-dns"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["zone"] != "prod-dns" {
		t.Errorf("expected zone=prod-dns, got %v", result.Output["zone"])
	}
	if result.Output["changes"] == nil {
		t.Error("expected changes in output")
	}
}

func TestDNSApplyStep(t *testing.T) {
	app, _ := setupDNSApp(t)
	factory := module.NewDNSApplyStepFactory()
	step, err := factory("apply", map[string]any{"zone": "prod-dns"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
	if result.Output["zoneId"] == "" {
		t.Error("expected non-empty zoneId in output")
	}
}

func TestDNSStatusStep(t *testing.T) {
	app, m := setupDNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	factory := module.NewDNSStatusStepFactory()
	step, err := factory("status", map[string]any{"zone": "prod-dns"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["zone"] != "prod-dns" {
		t.Errorf("expected zone=prod-dns, got %v", result.Output["zone"])
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
}

func TestDNSPlanStep_MissingZone(t *testing.T) {
	factory := module.NewDNSPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing zone, got nil")
	}
}

func TestDNSPlanStep_ZoneNotFound(t *testing.T) {
	factory := module.NewDNSPlanStepFactory()
	step, err := factory("plan", map[string]any{"zone": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing dns service, got nil")
	}
}

func TestDNSApplyStep_MissingZone(t *testing.T) {
	factory := module.NewDNSApplyStepFactory()
	_, err := factory("apply", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing zone, got nil")
	}
}

func TestDNSStatusStep_MissingZone(t *testing.T) {
	factory := module.NewDNSStatusStepFactory()
	_, err := factory("status", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing zone, got nil")
	}
}
