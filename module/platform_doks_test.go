package module_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newDOKSApp(t *testing.T) (*module.MockApplication, *module.PlatformDOKS) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformDOKS("my-cluster", map[string]any{
		"cluster_name": "staging-cluster",
		"region":       "nyc3",
		"version":      "1.29.1-do.0",
		"node_pool": map[string]any{
			"name":  "default",
			"size":  "s-2vcpu-2gb",
			"count": 3,
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module lifecycle ─────────────────────────────────────────────────────────

func TestDO_DOKS_Init(t *testing.T) {
	_, m := newDOKSApp(t)
	if m.Name() != "my-cluster" {
		t.Errorf("expected name=my-cluster, got %q", m.Name())
	}
}

func TestDO_DOKS_InitRegistersService(t *testing.T) {
	app, _ := newDOKSApp(t)
	svc, ok := app.Services["my-cluster"]
	if !ok {
		t.Fatal("expected my-cluster in service registry")
	}
	if _, ok := svc.(*module.PlatformDOKS); !ok {
		t.Fatalf("registry entry is %T, want *PlatformDOKS", svc)
	}
}

func TestDO_DOKS_Create(t *testing.T) {
	_, m := newDOKSApp(t)
	state, err := m.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
	if state.ID == "" {
		t.Error("expected non-empty ID after create")
	}
	if state.Endpoint == "" {
		t.Error("expected non-empty Endpoint after create")
	}
	if len(state.NodePools) == 0 {
		t.Error("expected at least one node pool after create")
	}
}

func TestDO_DOKS_Get(t *testing.T) {
	_, m := newDOKSApp(t)
	if _, err := m.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	state, err := m.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
}

func TestDO_DOKS_ListNodePools(t *testing.T) {
	_, m := newDOKSApp(t)
	if _, err := m.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pools, err := m.ListNodePools()
	if err != nil {
		t.Fatalf("ListNodePools: %v", err)
	}
	if len(pools) == 0 {
		t.Error("expected at least one node pool")
	}
	if pools[0].Name != "default" {
		t.Errorf("expected pool name=default, got %q", pools[0].Name)
	}
	if pools[0].Count != 3 {
		t.Errorf("expected count=3, got %d", pools[0].Count)
	}
}

func TestDO_DOKS_Delete(t *testing.T) {
	_, m := newDOKSApp(t)
	if _, err := m.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	state, err := m.Get()
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if state.Status != "deleted" {
		t.Errorf("expected status=deleted, got %q", state.Status)
	}
	if len(state.NodePools) != 0 {
		t.Errorf("expected no node pools after delete, got %d", len(state.NodePools))
	}
}

func TestDO_DOKS_DeleteIdempotent(t *testing.T) {
	_, m := newDOKSApp(t)
	if _, err := m.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Delete(); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := m.Delete(); err != nil {
		t.Errorf("second Delete should be idempotent, got: %v", err)
	}
}

func TestDO_DOKS_DefaultNodePool(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDOKS("default-cluster", map[string]any{
		"cluster_name": "default-cluster",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	state, err := m.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(state.NodePools) == 0 {
		t.Error("expected default node pool")
	}
	if state.NodePools[0].Size != "s-2vcpu-2gb" {
		t.Errorf("expected default size=s-2vcpu-2gb, got %q", state.NodePools[0].Size)
	}
}

func TestDO_DOKS_InvalidAccountRef(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDOKS("fail-cluster", map[string]any{
		"cluster_name": "fail-cluster",
		"account":      "nonexistent",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}
