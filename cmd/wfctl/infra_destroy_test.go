package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── destroyCapture ─────────────────────────────────────────────────────────────

// destroyCapture is a minimal IaCProvider that records Destroy calls.
type destroyCapture struct {
	destroyCalled bool
	destroyRefs   []interfaces.ResourceRef
	destroyErr    error
}

func (f *destroyCapture) Name() string                                         { return "fake" }
func (f *destroyCapture) Version() string                                      { return "0.0.0" }
func (f *destroyCapture) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *destroyCapture) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *destroyCapture) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *destroyCapture) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return &interfaces.ApplyResult{}, nil
}
func (f *destroyCapture) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	f.destroyCalled = true
	f.destroyRefs = append(f.destroyRefs, refs...)
	if f.destroyErr != nil {
		return nil, f.destroyErr
	}
	destroyed := make([]string, 0, len(refs))
	for _, r := range refs {
		destroyed = append(destroyed, r.Name)
	}
	return &interfaces.DestroyResult{Destroyed: destroyed}, nil
}
func (f *destroyCapture) Status(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	statuses := make([]interfaces.ResourceStatus, 0, len(refs))
	for _, r := range refs {
		statuses = append(statuses, interfaces.ResourceStatus{Name: r.Name, Type: r.Type, Status: "running"})
	}
	return statuses, nil
}
func (f *destroyCapture) DetectDrift(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	results := make([]interfaces.DriftResult, 0, len(refs))
	for _, r := range refs {
		results = append(results, interfaces.DriftResult{Name: r.Name, Type: r.Type, Drifted: false})
	}
	return results, nil
}
func (f *destroyCapture) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *destroyCapture) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *destroyCapture) ResourceDriver(_ string) (interfaces.ResourceDriver, error) { return nil, nil }
func (f *destroyCapture) SupportedCanonicalKeys() []string                           { return nil }
func (f *destroyCapture) Close() error                                               { return nil }

// ── helpers ────────────────────────────────────────────────────────────────────

// writeDestroyConfig writes a minimal infra.yaml with an infra.* module and
// filesystem state backend to dir, and pre-populates the state directory with
// the given states. Returns the config file path.
func writeDestroyConfig(t *testing.T, dir string, states []interfaces.ResourceState) string {
	t.Helper()

	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	// Write each state record in the iacStateRecord JSON format.
	st := &fsWfctlStateStore{dir: stateDir}
	for _, rs := range states {
		if err := st.SaveResource(context.Background(), rs); err != nil {
			t.Fatalf("seed state %q: %v", rs.Name, err)
		}
	}

	cfgPath := filepath.Join(dir, "infra.yaml")
	yaml := `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: test

  - name: state-backend
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `

  - name: my-app
    type: infra.container_service
    config:
      provider: do-provider
      image: registry/app:latest
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

// ── TestDestroyInfraModules ────────────────────────────────────────────────────

// TestDestroyInfraModules_DirectPath verifies that destroyInfraModules:
//  1. Loads state records from the filesystem backend.
//  2. Calls provider.Destroy with refs built from those records.
//  3. Removes the records from state after a successful destroy.
func TestDestroyInfraModules_DirectPath(t *testing.T) {
	dir := t.TempDir()
	states := []interfaces.ResourceState{
		{ID: "my-app", Name: "my-app", Type: "infra.container_service", Provider: "fake-cloud", ProviderID: "app-1"},
		{ID: "my-db", Name: "my-db", Type: "infra.database", Provider: "fake-cloud", ProviderID: "db-1"},
	}
	cfgPath := writeDestroyConfig(t, dir, states)

	fake := &destroyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := destroyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("destroyInfraModules: %v", err)
	}

	if !fake.destroyCalled {
		t.Fatal("provider.Destroy was not called")
	}
	if len(fake.destroyRefs) != 2 {
		t.Errorf("Destroy refs = %d, want 2", len(fake.destroyRefs))
	}

	// State records should be removed after destroy.
	store := &fsWfctlStateStore{dir: filepath.Join(dir, "state")}
	remaining, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("list remaining state: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 state records after destroy, got %d", len(remaining))
	}
}

// TestDestroyInfraModules_EmptyState verifies no error and no Destroy call when
// state is empty.
func TestDestroyInfraModules_EmptyState(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDestroyConfig(t, dir, nil)

	fake := &destroyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := destroyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("destroyInfraModules with empty state: %v", err)
	}
	if fake.destroyCalled {
		t.Error("provider.Destroy should NOT be called when state is empty")
	}
}

// ── TestStatusDriftInfraModules ────────────────────────────────────────────────

// TestStatusInfraModules_DirectPath verifies that statusInfraModules queries
// provider.Status for each tracked resource.
func TestStatusInfraModules_DirectPath(t *testing.T) {
	dir := t.TempDir()
	states := []interfaces.ResourceState{
		{ID: "my-app", Name: "my-app", Type: "infra.container_service", Provider: "fake-cloud"},
	}
	cfgPath := writeDestroyConfig(t, dir, states)

	fake := &destroyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := statusInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("statusInfraModules: %v", err)
	}
}

// TestDriftInfraModules_NoDrift verifies that driftInfraModules returns nil
// when the provider reports no drift.
func TestDriftInfraModules_NoDrift(t *testing.T) {
	dir := t.TempDir()
	states := []interfaces.ResourceState{
		{ID: "my-app", Name: "my-app", Type: "infra.container_service", Provider: "fake-cloud"},
	}
	cfgPath := writeDestroyConfig(t, dir, states)

	fake := &destroyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := driftInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("driftInfraModules: %v", err)
	}
}
