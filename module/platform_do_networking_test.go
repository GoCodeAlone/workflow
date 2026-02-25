package module_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newDONetworkingApp(t *testing.T) (*module.MockApplication, *module.PlatformDONetworking) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformDONetworking("staging-vpc", map[string]any{
		"provider": "mock",
		"vpc": map[string]any{
			"name":     "staging-vpc",
			"region":   "nyc3",
			"ip_range": "10.20.0.0/16",
		},
		"firewalls": []any{
			map[string]any{"name": "allow-web"},
			map[string]any{"name": "allow-db"},
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module lifecycle ─────────────────────────────────────────────────────────

func TestDO_Networking_Init(t *testing.T) {
	_, m := newDONetworkingApp(t)
	if m.Name() != "staging-vpc" {
		t.Errorf("expected name=staging-vpc, got %q", m.Name())
	}
}

func TestDO_Networking_InitRegistersService(t *testing.T) {
	app, _ := newDONetworkingApp(t)
	svc, ok := app.Services["staging-vpc"]
	if !ok {
		t.Fatal("expected staging-vpc in service registry")
	}
	if _, ok := svc.(*module.PlatformDONetworking); !ok {
		t.Fatalf("registry entry is %T, want *PlatformDONetworking", svc)
	}
}

func TestDO_Networking_Plan_PendingState(t *testing.T) {
	_, m := newDONetworkingApp(t)
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.VPC != "staging-vpc" {
		t.Errorf("expected vpc=staging-vpc, got %q", plan.VPC)
	}
	if len(plan.Changes) == 0 {
		t.Error("expected at least one change in plan")
	}
	if len(plan.Firewalls) != 2 {
		t.Errorf("expected 2 firewalls, got %d", len(plan.Firewalls))
	}
}

func TestDO_Networking_Plan_NoopAfterApply(t *testing.T) {
	_, m := newDONetworkingApp(t)
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

func TestDO_Networking_Apply(t *testing.T) {
	_, m := newDONetworkingApp(t)
	state, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.ID == "" {
		t.Error("expected non-empty VPC ID after apply")
	}
	if len(state.FirewallIDs) != 2 {
		t.Errorf("expected 2 firewall IDs, got %d", len(state.FirewallIDs))
	}
}

func TestDO_Networking_Status(t *testing.T) {
	_, m := newDONetworkingApp(t)
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

func TestDO_Networking_Destroy(t *testing.T) {
	_, m := newDONetworkingApp(t)
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
	if len(state.FirewallIDs) != 0 {
		t.Errorf("expected no firewall IDs after destroy, got %d", len(state.FirewallIDs))
	}
}

func TestDO_Networking_DestroyIdempotent(t *testing.T) {
	_, m := newDONetworkingApp(t)
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

func TestDO_Networking_UnsupportedProvider(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDONetworking("bad-vpc", map[string]any{
		"provider": "azure",
		"vpc":      map[string]any{"name": "bad"},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestDO_Networking_InvalidAccountRef(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDONetworking("fail-vpc", map[string]any{
		"provider": "mock",
		"account":  "nonexistent",
		"vpc":      map[string]any{"name": "fail"},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}
