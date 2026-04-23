package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fakes ──────────────────────────────────────────────────────────────────────

// applyCapture is a minimal IaCProvider that records Plan and Apply calls.
type applyCapture struct {
	mu          sync.Mutex
	planCalled  bool
	applyCalled bool
	planSpecs   []interfaces.ResourceSpec
	appliedPlan *interfaces.IaCPlan
}

func (f *applyCapture) Name() string                                         { return "fake" }
func (f *applyCapture) Version() string                                      { return "0.0.0" }
func (f *applyCapture) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *applyCapture) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *applyCapture) Plan(_ context.Context, desired []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planCalled = true
	f.planSpecs = append(f.planSpecs, desired...)
	return nil, nil
}
func (f *applyCapture) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCalled = true
	f.appliedPlan = plan
	return &interfaces.ApplyResult{}, nil
}
func (f *applyCapture) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *applyCapture) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *applyCapture) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *applyCapture) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *applyCapture) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *applyCapture) ResourceDriver(_ string) (interfaces.ResourceDriver, error) { return nil, nil }
func (f *applyCapture) SupportedCanonicalKeys() []string                           { return nil }
func (f *applyCapture) Close() error                                               { return nil }

// ── TestApplyInfraModules_DirectPath ───────────────────────────────────────────

// TestApplyInfraModules_DirectPath verifies that applyInfraModules:
//  1. Loads the IaCProvider for the declared iac.provider module.
//  2. Computes a plan (via platform.ComputePlan — no current state → all creates).
//  3. Calls provider.Apply with the computed plan.
//  4. Correctly handles two infra.* modules that reference the same provider.
func TestApplyInfraModules_DirectPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "test-token"

  - name: bmw-db
    type: infra.database
    config:
      provider: do-provider
      engine: postgres
      size: s

  - name: bmw-app
    type: infra.container_service
    config:
      provider: do-provider
      image: registry.example.com/bmw:latest
      http_port: 8080
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fake := &applyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != "fake-cloud" {
			t.Errorf("unexpected provider type %q", providerType)
		}
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// Apply must have been called.
	if !fake.applyCalled {
		t.Fatal("provider.Apply was not called")
	}

	// Plan must contain exactly 2 create actions (no current state → all creates).
	if fake.appliedPlan == nil {
		t.Fatal("appliedPlan is nil")
	}
	if got := len(fake.appliedPlan.Actions); got != 2 {
		t.Errorf("plan actions: want 2, got %d", got)
	}
	actions := map[string]string{}
	for _, a := range fake.appliedPlan.Actions {
		actions[a.Resource.Name] = a.Action
	}
	for _, name := range []string{"bmw-db", "bmw-app"} {
		if actions[name] != "create" {
			t.Errorf("action for %q: want create, got %q", name, actions[name])
		}
	}
}

// TestApplyWithProvider_NoChanges verifies that when the current state already
// matches the desired spec (identical config hash), Apply is NOT called.
// It exercises the no-op branch of applyWithProvider directly by injecting a
// ResourceState whose ConfigHash matches the hash platform.ComputePlan computes
// for the spec's Config map.
func TestApplyWithProvider_NoChanges(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "my-db",
		Type:   "infra.database",
		Config: map[string]any{"engine": "postgres"},
	}

	// Reproduce the hash that platform.ComputePlan computes via configHash:
	//   sha256(json.Marshal(spec.Config)) in hex.
	cfgData, err := json.Marshal(spec.Config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	cfgHash := fmt.Sprintf("%x", sha256.Sum256(cfgData))

	current := []interfaces.ResourceState{{
		Name:       spec.Name,
		Type:       spec.Type,
		ConfigHash: cfgHash,
	}}

	fake := &applyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := applyWithProvider(context.Background(), "fake-cloud", nil, []interfaces.ResourceSpec{spec}, current); err != nil {
		t.Fatalf("applyWithProvider: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.applyCalled {
		t.Error("provider.Apply should NOT be called when current state matches desired spec")
	}
}

// TestApplyInfraModules_MissingProvider verifies that a helpful error is returned
// when an infra.* module references a provider module that doesn't exist.
func TestApplyInfraModules_MissingProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: my-db
    type: infra.database
    config:
      provider: nonexistent-provider
      engine: postgres
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := applyInfraModules(context.Background(), cfgPath, "")
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
	if msg := err.Error(); msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

// TestHasInfraModules verifies detection of infra.* vs platform.* configs.
func TestHasInfraModules(t *testing.T) {
	dir := t.TempDir()

	withInfra := filepath.Join(dir, "with-infra.yaml")
	if err := os.WriteFile(withInfra, []byte(`
modules:
  - name: db
    type: infra.database
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	legacyOnly := filepath.Join(dir, "legacy.yaml")
	if err := os.WriteFile(legacyOnly, []byte(`
modules:
  - name: app
    type: platform.do_app
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if !hasInfraModules(withInfra) {
		t.Error("hasInfraModules: want true for infra.* config, got false")
	}
	if hasInfraModules(legacyOnly) {
		t.Error("hasInfraModules: want false for platform.* config, got true")
	}
}
