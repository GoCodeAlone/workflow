package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// refreshOutputsCmdFakeProvider is the IaCProvider stub used by the
// refresh-outputs subcommand tests. It only needs to answer ResourceDriver →
// fakeResourceDriver.Read with canned outputs; everything else can be a
// safe no-op because the subcommand never calls them on the read-only path.
type refreshOutputsCmdFakeProvider struct {
	readOutputs map[string]map[string]any
}

func (f *refreshOutputsCmdFakeProvider) Name() string    { return "fake-refresh-outputs" }
func (f *refreshOutputsCmdFakeProvider) Version() string { return "0.0.0" }
func (f *refreshOutputsCmdFakeProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (f *refreshOutputsCmdFakeProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (f *refreshOutputsCmdFakeProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return &interfaces.IaCPlan{}, nil
}
func (f *refreshOutputsCmdFakeProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return &interfaces.ApplyResult{}, nil
}
func (f *refreshOutputsCmdFakeProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &refreshOutputsCmdFakeDriver{outputs: f.readOutputs}, nil
}
func (f *refreshOutputsCmdFakeProvider) SupportedCanonicalKeys() []string { return nil }
func (f *refreshOutputsCmdFakeProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *refreshOutputsCmdFakeProvider) Close() error { return nil }

// refreshOutputsCmdFakeDriver answers Read with canned outputs keyed by
// ProviderID. All other ResourceDriver methods panic to make accidental
// non-Read use loud during testing.
type refreshOutputsCmdFakeDriver struct {
	outputs map[string]map[string]any
}

func (d *refreshOutputsCmdFakeDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *refreshOutputsCmdFakeDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       ref.Type,
		ProviderID: ref.ProviderID,
		Outputs:    d.outputs[ref.ProviderID],
	}, nil
}
func (d *refreshOutputsCmdFakeDriver) Update(context.Context, interfaces.ResourceRef, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *refreshOutputsCmdFakeDriver) Delete(context.Context, interfaces.ResourceRef) error {
	panic("not used")
}
func (d *refreshOutputsCmdFakeDriver) Diff(context.Context, interfaces.ResourceSpec, *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	// W-3b ComputePlan now dispatches Diff per resource for v2 plugins;
	// returning (nil, nil) classifies as "no change" so this fake stays
	// safe for refresh-outputs tests that only exercise Read.
	return nil, nil
}
func (d *refreshOutputsCmdFakeDriver) HealthCheck(context.Context, interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	panic("not used")
}
func (d *refreshOutputsCmdFakeDriver) Scale(context.Context, interfaces.ResourceRef, int) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *refreshOutputsCmdFakeDriver) SensitiveKeys() []string { return nil }

// TestRefreshOutputs_CommandHelp verifies that --help prints the standard
// FlagSet usage banner naming the subcommand. The banner is what T2.7's
// runtime-launch validation will exercise via the built binary.
func TestRefreshOutputs_CommandHelp(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runInfraRefreshOutputs([]string{"--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
	if !strings.Contains(out, "Usage of infra refresh-outputs") {
		t.Errorf("help output missing 'Usage of infra refresh-outputs'; got: %s", out)
	}
}

// TestRefreshOutputs_PersistsNewFieldsToState exercises the end-to-end
// persists-new-output path: pre-populate a filesystem state backend with a
// stale VPC entry, swap the IaCProvider loader for a stub that returns an
// extra "id" field on Read, run the subcommand, and verify the field has
// been written back to state.
func TestRefreshOutputs_PersistsNewFieldsToState(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "infra.yaml")
	cfg := `modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: fake-provider
  - name: state-store
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: coredump-staging-vpc
    type: infra.vpc
    config:
      provider: cloud-provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pre-populate state with a stale VPC entry (no "id" output yet).
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore: %v", err)
	}
	stale := interfaces.ResourceState{
		ID:          "coredump-staging-vpc",
		Name:        "coredump-staging-vpc",
		Type:        "infra.vpc",
		Provider:    "fake-provider",
		ProviderRef: "cloud-provider",
		ProviderID:  "uuid-1",
		Outputs:     map[string]any{"ip_range": "10.0.0.0/16"},
	}
	if err := store.SaveResource(context.Background(), stale); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Swap the provider loader so the test never touches a real cloud.
	orig := resolveIaCProvider
	defer func() { resolveIaCProvider = orig }()
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return &refreshOutputsCmdFakeProvider{
			readOutputs: map[string]map[string]any{
				"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"},
			},
		}, nil, nil
	}

	if _, err := captureStdout(t, func() error {
		return runInfraRefreshOutputs([]string{"-c", cfgPath, "--env", "staging"})
	}); err != nil {
		t.Fatalf("runInfraRefreshOutputs: %v", err)
	}

	refreshed, err := loadCurrentState(cfgPath, "")
	if err != nil {
		t.Fatalf("loadCurrentState: %v", err)
	}
	byName := make(map[string]interfaces.ResourceState, len(refreshed))
	for _, s := range refreshed {
		byName[s.Name] = s
	}
	got := byName["coredump-staging-vpc"]
	if id, _ := got.Outputs["id"].(string); id != "uuid-1" {
		t.Errorf("expected 'id' field after refresh, got %v", got.Outputs)
	}
}

// TestRefreshOutputs_NonComparableOutputs_DoesNotPanic regression-tests
// the Outputs-changed check against real-world Outputs that contain slices
// and nested maps. A naive `lv != v` on `any` values panics with
// "comparing uncomparable type" the moment a slice or map is involved —
// reflect.DeepEqual is the correct tool. Covers both the unchanged path
// (live == persisted) and the changed path (live grows a new field) so
// neither code direction can regress to the panic.
func TestRefreshOutputs_NonComparableOutputs_DoesNotPanic(t *testing.T) {
	for _, tc := range []struct {
		name         string
		liveOutputs  map[string]any
		expectUpdate bool
	}{
		{
			name: "unchanged-with-slice-and-nested-map",
			liveOutputs: map[string]any{
				"subnet_ids": []string{"a", "b"},
				"tags":       map[string]any{"env": "staging"},
			},
			expectUpdate: false,
		},
		{
			name: "added-field-with-slice-and-nested-map",
			liveOutputs: map[string]any{
				"subnet_ids": []string{"a", "b"},
				"tags":       map[string]any{"env": "staging"},
				"id":         "uuid-1",
			},
			expectUpdate: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stateDir := filepath.Join(dir, "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(dir, "infra.yaml")
			cfg := `modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: fake-provider
  - name: state-store
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: vpc-1
    type: infra.vpc
    config:
      provider: cloud-provider
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}

			store, err := resolveStateStore(cfgPath, "")
			if err != nil {
				t.Fatalf("resolveStateStore: %v", err)
			}
			persisted := interfaces.ResourceState{
				ID:          "vpc-1",
				Name:        "vpc-1",
				Type:        "infra.vpc",
				Provider:    "fake-provider",
				ProviderRef: "cloud-provider",
				ProviderID:  "uuid-1",
				Outputs: map[string]any{
					"subnet_ids": []string{"a", "b"},
					"tags":       map[string]any{"env": "staging"},
				},
			}
			if err := store.SaveResource(context.Background(), persisted); err != nil {
				t.Fatalf("seed state: %v", err)
			}

			orig := resolveIaCProvider
			defer func() { resolveIaCProvider = orig }()
			liveOutputs := tc.liveOutputs
			resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
				return &refreshOutputsCmdFakeProvider{
					readOutputs: map[string]map[string]any{"uuid-1": liveOutputs},
				}, nil, nil
			}

			// The critical assertion: this call must NOT panic with
			// "comparing uncomparable type".
			if _, err := captureStdout(t, func() error {
				return runInfraRefreshOutputs([]string{"-c", cfgPath})
			}); err != nil {
				t.Fatalf("runInfraRefreshOutputs panicked or errored: %v", err)
			}

			refreshed, err := loadCurrentState(cfgPath, "")
			if err != nil {
				t.Fatalf("loadCurrentState: %v", err)
			}
			var got interfaces.ResourceState
			for _, s := range refreshed {
				if s.Name == "vpc-1" {
					got = s
					break
				}
			}
			if tc.expectUpdate {
				if _, ok := got.Outputs["id"]; !ok {
					t.Errorf("expected new 'id' field after refresh; got Outputs=%v", got.Outputs)
				}
			}
			// Whether updated or not, post-state must still carry the
			// nested values intact.
			if _, ok := got.Outputs["subnet_ids"]; !ok {
				t.Errorf("expected subnet_ids preserved; got Outputs=%v", got.Outputs)
			}
		})
	}
}

// TestRefreshOutputs_NoProviderConfigured_ReturnsLiteralError pins the
// exact stderr line T2.7 asserts against. The wording is load-bearing —
// changing it breaks the runtime-launch-validation gate.
func TestRefreshOutputs_NoProviderConfigured_ReturnsLiteralError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	// Config has no iac.provider modules.
	cfg := `modules:
  - name: state-store
    type: iac.state
    config:
      backend: filesystem
      directory: ` + dir + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runInfraRefreshOutputs([]string{"-c", cfgPath, "--env", "staging"})
	if err == nil {
		t.Fatal("expected error when no iac.provider configured")
	}
	want := `refresh-outputs: provider not configured for env "staging"`
	if err.Error() != want {
		t.Errorf("error mismatch:\n got: %q\nwant: %q", err.Error(), want)
	}
}
