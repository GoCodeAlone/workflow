package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestApply_V2_LoaderSeam_JITResolution_EndToEnd is W-5 T5.7 Step 2's
// runtime-launch-validation: it exercises the full v2 dispatch chain
// for a JIT-substitution scenario through the same applyInfraModules
// entrypoint runInfraApply uses, then asserts the dependent module's
// driver call receives the post-substitution Config (parent's
// freshly-minted ProviderID swapped in for ${parent.id}).
//
// Why loader-seam (not real gRPC binary): same precedent as T3.9 (see
// docs/adr/007-t3-9-runtime-validation-via-loader-seam.md). A
// stub-provider plugin would add an out-of-process gRPC dependency
// solely for this one assertion; the loader seam exercises every
// in-process code path between cmd/wfctl and the driver — config
// parse, state load, provider load, ComputePlan dispatch,
// wfctlhelpers.ApplyPlan, jitsubst.ResolveSpec — the only mocked
// boundary is the cloud API the driver would have called.
//
// The scenario:
//
//   - Two infra.vpc modules in declaration order: parent + dependent.
//   - parent has no JIT refs.
//   - dependent's Config carries env_vars.PARENT_VPC_UUID = ${parent.id}.
//   - Empty state, so ComputePlan emits two Create actions.
//   - At apply: parent's Create succeeds, returning the canonical
//     stub ProviderID. ApplyPlan folds {id: <ProviderID>} into
//     syncedOutputs. The next iteration's pre-dispatch ResolveSpec
//     swaps ${parent.id} for the ProviderID in dependent's Config
//     before driver.Create.
//
// Driver assertion: dependent's recorded Config[env_vars][PARENT_VPC_UUID]
// == parent's ProviderID — proves end-to-end JIT substitution through
// the production binary code path.
func TestApply_V2_LoaderSeam_JITResolution_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}

	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgYAML := `name: t5-7-jit-loader-seam
modules:
  - name: state
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: stub
    type: iac.provider
    config:
      provider: stub
  - name: parent
    type: infra.vpc
    config:
      provider: stub
      region: nyc1
  - name: dependent
    type: infra.vpc
    config:
      provider: stub
      region: nyc1
      env_vars:
        PARENT_VPC_UUID: "${parent.id}"
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stub := &jitLoaderStubProvider{
		driver: &jitLoaderStubDriver{
			seenCreate: make(map[string]map[string]any),
			providerID: map[string]string{
				"parent":    "vpc-parent-binary-uuid",
				"dependent": "vpc-dependent-binary-uuid",
			},
		},
	}
	origLoader := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return stub, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origLoader })

	if _, err := applyInfraModules(t.Context(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	// Both Creates must have run.
	if len(stub.driver.seenCreate) != 2 {
		t.Fatalf("expected 2 Create dispatches; got %d (seen: %+v)",
			len(stub.driver.seenCreate), stub.driver.seenCreate)
	}

	// Dependent's Create must have received the post-substitution Config.
	depCfg := stub.driver.seenCreate["dependent"]
	if depCfg == nil {
		t.Fatalf("driver did not receive dependent's Config; seenCreate: %+v",
			stub.driver.seenCreate)
	}
	envVars, ok := depCfg["env_vars"].(map[string]any)
	if !ok {
		t.Fatalf("dependent.Config[env_vars] not a map: %T (%+v)", depCfg["env_vars"], depCfg)
	}
	if got, want := envVars["PARENT_VPC_UUID"], "vpc-parent-binary-uuid"; got != want {
		t.Errorf("dependent.Config[env_vars][PARENT_VPC_UUID] post-JIT: got %q want %q\n(full env_vars: %+v)",
			got, want, envVars)
	}
}

// jitLoaderStubProvider is the W-5-T5.7-flavored loader-seam provider
// — declares ComputePlanVersion v2 to route through wfctlhelpers.ApplyPlan,
// returns the same jitLoaderStubDriver for every resource type so a
// single instance records every Create across the plan. Mirrors the
// v2LoaderStubProvider pattern from infra_apply_v2_loader_test.go but
// owns its own driver type so JIT-specific assertions (per-name Config
// recording, configurable per-name ProviderID) don't entangle with the
// T3.9 NeedsReplace-flavored stub.
type jitLoaderStubProvider struct {
	driver *jitLoaderStubDriver
}

var (
	_ interfaces.IaCProvider                  = (*jitLoaderStubProvider)(nil)
	_ wfctlhelpers.ComputePlanVersionDeclarer = (*jitLoaderStubProvider)(nil)
)

func (p *jitLoaderStubProvider) Name() string                                         { return "stub" }
func (p *jitLoaderStubProvider) Version() string                                      { return "0.0.1-jit-loader-seam" }
func (p *jitLoaderStubProvider) ComputePlanVersion() string                           { return "v2" }
func (p *jitLoaderStubProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *jitLoaderStubProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *jitLoaderStubProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, errors.New("v2 path must route through wfctlhelpers.ApplyPlan, not provider.Apply")
}
func (p *jitLoaderStubProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}
func (p *jitLoaderStubProvider) SupportedCanonicalKeys() []string { return nil }
func (p *jitLoaderStubProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *jitLoaderStubProvider) Close() error { return nil }

// jitLoaderStubDriver records every spec.Config seen on Create per
// resource Name, and returns a per-name ProviderID (mints "stub-id-"
// + spec.Name when the providerID map has no override). The Diff
// method is a no-op since this test exercises only the empty-state
// → Create path; if a future revision needs Update/Replace cascades,
// extend Diff to honor a NeedsReplace toggle.
type jitLoaderStubDriver struct {
	mu         sync.Mutex
	seenCreate map[string]map[string]any
	providerID map[string]string
}

var _ interfaces.ResourceDriver = (*jitLoaderStubDriver)(nil)

func (d *jitLoaderStubDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.mu.Lock()
	d.seenCreate[spec.Name] = spec.Config
	id, ok := d.providerID[spec.Name]
	d.mu.Unlock()
	if !ok {
		id = "stub-id-" + spec.Name
	}
	return &interfaces.ResourceOutput{
		Name: spec.Name, Type: spec.Type, ProviderID: id, Status: "active",
	}, nil
}
func (d *jitLoaderStubDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID, Status: "active"}, nil
}
func (d *jitLoaderStubDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}
func (d *jitLoaderStubDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *jitLoaderStubDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *jitLoaderStubDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: true}, nil
}
func (d *jitLoaderStubDriver) Scale(_ context.Context, ref interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}
func (d *jitLoaderStubDriver) SensitiveKeys() []string { return nil }
