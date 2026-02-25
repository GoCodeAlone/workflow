package module_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newDODNSApp(t *testing.T) (*module.MockApplication, *module.PlatformDODNS) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformDODNS("prod-do-dns", map[string]any{
		"provider": "mock",
		"domain":   "example.com",
		"records": []any{
			map[string]any{"name": "api", "type": "A", "data": "10.0.0.1", "ttl": 300},
			map[string]any{"name": "www", "type": "CNAME", "data": "example.com", "ttl": 3600},
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module lifecycle ─────────────────────────────────────────────────────────

func TestDO_DNS_Init(t *testing.T) {
	_, m := newDODNSApp(t)
	if m.Name() != "prod-do-dns" {
		t.Errorf("expected name=prod-do-dns, got %q", m.Name())
	}
}

func TestDO_DNS_InitRegistersService(t *testing.T) {
	app, _ := newDODNSApp(t)
	svc, ok := app.Services["prod-do-dns"]
	if !ok {
		t.Fatal("expected prod-do-dns in service registry")
	}
	if _, ok := svc.(*module.PlatformDODNS); !ok {
		t.Fatalf("registry entry is %T, want *PlatformDODNS", svc)
	}
}

func TestDO_DNS_Plan_PendingState(t *testing.T) {
	_, m := newDODNSApp(t)
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Domain != "example.com" {
		t.Errorf("expected domain=example.com, got %q", plan.Domain)
	}
	if len(plan.Changes) == 0 {
		t.Error("expected changes in plan")
	}
	if len(plan.Records) != 2 {
		t.Errorf("expected 2 records in plan, got %d", len(plan.Records))
	}
}

func TestDO_DNS_Plan_NoopAfterApply(t *testing.T) {
	_, m := newDODNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if len(plan.Changes) == 0 || plan.Changes[0] != "no changes" {
		t.Errorf("expected 'no changes', got %v", plan.Changes)
	}
}

func TestDO_DNS_Apply(t *testing.T) {
	_, m := newDODNSApp(t)
	state, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.DomainName != "example.com" {
		t.Errorf("expected domain=example.com, got %q", state.DomainName)
	}
	if len(state.Records) != 2 {
		t.Errorf("expected 2 records after apply, got %d", len(state.Records))
	}
}

func TestDO_DNS_Status(t *testing.T) {
	_, m := newDODNSApp(t)
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

func TestDO_DNS_Destroy(t *testing.T) {
	_, m := newDODNSApp(t)
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
		t.Errorf("expected status=deleted, got %q", state.Status)
	}
	if len(state.Records) != 0 {
		t.Errorf("expected 0 records after destroy, got %d", len(state.Records))
	}
}

func TestDO_DNS_DestroyIdempotent(t *testing.T) {
	_, m := newDODNSApp(t)
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Errorf("second Destroy should be idempotent, got: %v", err)
	}
}

func TestDO_DNS_MissingDomain(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDODNS("bad-dns", map[string]any{
		"provider": "mock",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for missing domain, got nil")
	}
}

func TestDO_DNS_UnsupportedProvider(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDODNS("bad-dns", map[string]any{
		"provider": "aws",
		"domain":   "example.com",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestDO_DNS_InvalidAccountRef(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDODNS("fail-dns", map[string]any{
		"provider": "mock",
		"account":  "nonexistent",
		"domain":   "example.com",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}
