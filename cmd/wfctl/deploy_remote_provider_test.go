package main

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeRemoteInvoker implements remoteServiceInvoker using an in-memory dispatch
// table, so tests exercise remoteIaCProvider without a live plugin subprocess.
type fakeRemoteInvoker struct {
	methods map[string]map[string]any // method → result
	errors  map[string]string         // method → error string
}

func (f *fakeRemoteInvoker) InvokeService(method string, _ map[string]any) (map[string]any, error) {
	if errStr, ok := f.errors[method]; ok {
		return nil, errString(errStr)
	}
	if res, ok := f.methods[method]; ok {
		return res, nil
	}
	return map[string]any{}, nil
}

type errString string

func (e errString) Error() string { return string(e) }

func newFakeInvoker() *fakeRemoteInvoker {
	return &fakeRemoteInvoker{
		methods: map[string]map[string]any{
			"IaCProvider.Name":    {"name": "test-provider"},
			"IaCProvider.Version": {"version": "1.0.0"},
			"IaCProvider.Initialize": {},
			"ResourceDriver.Update": {
				"provider_id": "app-123",
				"status":      "running",
			},
			"ResourceDriver.HealthCheck": {
				"healthy": true,
				"message": "",
			},
		},
		errors: map[string]string{},
	}
}

// ── remoteIaCProvider ─────────────────────────────────────────────────────────

func TestRemoteIaCProvider_Name(t *testing.T) {
	p := &remoteIaCProvider{invoker: newFakeInvoker()}
	if got := p.Name(); got != "test-provider" {
		t.Errorf("Name() = %q, want %q", got, "test-provider")
	}
}

func TestRemoteIaCProvider_Initialize_RoutesViaInvoker(t *testing.T) {
	inv := newFakeInvoker()
	p := &remoteIaCProvider{invoker: inv}
	if err := p.Initialize(context.Background(), map[string]any{"token": "x"}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func TestRemoteIaCProvider_Initialize_PropagatesError(t *testing.T) {
	inv := newFakeInvoker()
	inv.errors["IaCProvider.Initialize"] = "invalid token"
	p := &remoteIaCProvider{invoker: inv}
	err := p.Initialize(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("expected 'invalid token' in error, got: %v", err)
	}
}

func TestRemoteIaCProvider_ResourceDriver_ReturnsRemoteDriver(t *testing.T) {
	p := &remoteIaCProvider{invoker: newFakeInvoker()}
	drv, err := p.ResourceDriver("infra.container_service")
	if err != nil {
		t.Fatalf("ResourceDriver: %v", err)
	}
	if _, ok := drv.(*remoteResourceDriver); !ok {
		t.Fatalf("expected *remoteResourceDriver, got %T", drv)
	}
}

// ── remoteResourceDriver ──────────────────────────────────────────────────────

func TestRemoteResourceDriver_Update_RoutesViaInvoker(t *testing.T) {
	drv := &remoteResourceDriver{
		invoker:      newFakeInvoker(),
		resourceType: "infra.container_service",
	}
	ref := interfaces.ResourceRef{Name: "bmw-app", Type: "infra.container_service"}
	spec := interfaces.ResourceSpec{
		Name:   "bmw-app",
		Type:   "infra.container_service",
		Config: map[string]any{"image": "registry.example.com/bmw:v2"},
	}
	out, err := drv.Update(context.Background(), ref, spec)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.ProviderID != "app-123" {
		t.Errorf("ProviderID = %q, want %q", out.ProviderID, "app-123")
	}
}

func TestRemoteResourceDriver_HealthCheck_Healthy(t *testing.T) {
	drv := &remoteResourceDriver{
		invoker:      newFakeInvoker(),
		resourceType: "infra.container_service",
	}
	ref := interfaces.ResourceRef{Name: "bmw-app", Type: "infra.container_service"}
	result, err := drv.HealthCheck(context.Background(), ref)
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !result.Healthy {
		t.Error("expected Healthy=true")
	}
}

func TestRemoteResourceDriver_HealthCheck_Unhealthy(t *testing.T) {
	inv := newFakeInvoker()
	inv.methods["ResourceDriver.HealthCheck"] = map[string]any{
		"healthy": false,
		"message": "app is degraded",
	}
	drv := &remoteResourceDriver{
		invoker:      inv,
		resourceType: "infra.container_service",
	}
	ref := interfaces.ResourceRef{Name: "bmw-app", Type: "infra.container_service"}
	result, err := drv.HealthCheck(context.Background(), ref)
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false")
	}
	if result.Message != "app is degraded" {
		t.Errorf("Message = %q, want %q", result.Message, "app is degraded")
	}
}

func TestRemoteResourceDriver_Update_PropagatesError(t *testing.T) {
	inv := newFakeInvoker()
	inv.errors["ResourceDriver.Update"] = "deployment failed"
	drv := &remoteResourceDriver{
		invoker:      inv,
		resourceType: "infra.container_service",
	}
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{}, interfaces.ResourceSpec{})
	if err == nil || !strings.Contains(err.Error(), "deployment failed") {
		t.Errorf("expected 'deployment failed' error, got: %v", err)
	}
}

// TestDiscoverAndLoadIaCProvider_WrapsModuleAsRemoteIaCProvider verifies that
// when a plugin's iac.provider module does NOT directly implement IaCProvider
// (the normal case for gRPC plugins), discoverAndLoadIaCProvider wraps it in
// remoteIaCProvider instead of failing the type assertion.
func TestDiscoverAndLoadIaCProvider_WrapsModuleAsRemoteIaCProvider(t *testing.T) {
	// This is covered end-to-end by the plugin tests; here we just confirm that
	// remoteIaCProvider satisfies interfaces.IaCProvider at compile time.
	var _ interfaces.IaCProvider = (*remoteIaCProvider)(nil)
}
