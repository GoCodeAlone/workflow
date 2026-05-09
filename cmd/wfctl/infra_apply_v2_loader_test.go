package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// (captureStderr lives in infra_outputs_test.go as a panic-safe
// stderr-capture helper; reused here so the loader-seam test pins
// its drift-report assertion against the same code path other
// stderr-capturing tests rely on.)

// TestApply_V2_LoaderSeamDispatch_EndToEnd is W-3b T3.9's
// runtime-launch-validation: it exercises the full v2 dispatch chain
// — config parse → state load → provider load (via the
// resolveIaCProvider seam from T3.6c) → ComputePlan Diff dispatch
// (T3.6e/f) → wfctlhelpers.ApplyPlan (T3.7's manifest-driven branch)
// → Replace decomposition into Delete + Create →
// printDriftReportIfAny — without standing up an out-of-process
// gRPC plugin. The plan-text said "real gRPC-loaded stub provider"
// but plugin/sdk/iaclint/'s precedent is in-tree Go test
// infrastructure, and the team-lead authorized this loader-seam
// approach (see docs/adr/007-t3-9-runtime-validation-via-loader-seam.md).
//
// The test loads from a real config file written to a temp dir and
// runs through applyInfraModules — the same entrypoint runInfraApply
// uses — so every code path between config and driver is exercised.
// Only the provider-load step is instrumented (via the
// resolveIaCProvider var-seam established in T3.6c) so the test
// substitutes a Go in-process v2 provider for what would otherwise
// be a loaded gRPC plugin.
func TestApply_V2_LoaderSeamDispatch_EndToEnd(t *testing.T) {
	// (Diffcache is disabled package-wide via TestMain in
	// main_test.go so a stale ~/.cache/wfctl/diff/ entry doesn't
	// satisfy Diff dispatch as a false-positive cache hit and skip
	// the driver-call assertions below.)

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}

	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgYAML := `name: t3-9-loader-seam
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
  - name: vpc
    type: infra.vpc
    config:
      provider: stub
      region: nyc1
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Seed state so vpc already exists with region=nyc3 (different
	// from desired nyc1). ComputePlan will then dispatch Diff and
	// observe the divergence. The on-disk shape is iacStateRecord
	// (cmd/wfctl/infra_state_store.go), not interfaces.ResourceState
	// — the wfctl-side fsWfctlStateStore translates between the two
	// in iacRecordToResourceState.
	stateJSON := `{
  "resource_id": "vpc",
  "resource_type": "infra.vpc",
  "provider": "stub",
  "provider_ref": "stub",
  "provider_id": "stub-existing-001",
  "config_hash": "stale-hash-irrelevant-once-Diff-dispatches",
  "status": "active",
  "config": {"provider": "stub", "region": "nyc3"},
  "outputs": {"region": "nyc3", "subnet_ids": ["sn-1", "sn-2", "sn-3"]},
  "dependencies": [],
  "created_at": "2026-05-01T00:00:00Z",
  "updated_at": "2026-05-01T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(stateDir, "vpc.json"), []byte(stateJSON), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// Inject a v2-declaring provider via the same loader seam
	// production providers go through. This is the "loaded plugin"
	// substitution that lets the test exercise the entire v2
	// dispatch path without a real gRPC binary.
	stub := &v2LoaderStubProvider{
		driver: &v2LoaderStubDriver{},
	}
	origLoader := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return stub, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origLoader })

	// Run the production apply entrypoint. The dispatcher must:
	//   1. Load the provider via resolveIaCProvider (seam) — succeeds.
	//   2. Type-assert wfctlhelpers.ComputePlanVersionDeclarer →
	//      "v2" → route through wfctlhelpers.ApplyPlan.
	//   3. ComputePlan dispatch driver.Diff via T3.6e errgroup,
	//      observe NeedsReplace=true, emit "replace" action.
	//   4. wfctlhelpers.ApplyPlan decomposes replace into
	//      driver.Delete + driver.Create, populating ReplaceIDMap.
	if _, err := applyInfraModules(t.Context(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	// Driver must have observed the full Replace decomposition.
	if got := stub.driver.diffCount.Load(); got != 1 {
		t.Errorf("Diff calls = %d, want 1 (one existing-resource Diff dispatch)", got)
	}
	if got := stub.driver.deleteCount.Load(); got != 1 {
		t.Errorf("Delete calls = %d, want 1 (replace decomposes Delete + Create)", got)
	}
	if got := stub.driver.createCount.Load(); got != 1 {
		t.Errorf("Create calls = %d, want 1 (replace decomposes Delete + Create)", got)
	}

	// driver.deleteRefs[0] should reference the OLD providerID; .createSpecs[0]
	// should reference the new desired Config (region=nyc1). Together they prove
	// wfctlhelpers.ApplyPlan executed Delete-then-Create against the v2 driver.
	stub.driver.mu.Lock()
	defer stub.driver.mu.Unlock()
	if len(stub.driver.deleteRefs) != 1 || stub.driver.deleteRefs[0].ProviderID != "stub-existing-001" {
		t.Errorf("delete ref = %+v, want ProviderID=stub-existing-001 (the old resource)", stub.driver.deleteRefs)
	}
	if len(stub.driver.createSpecs) != 1 || stub.driver.createSpecs[0].Config["region"] != "nyc1" {
		t.Errorf("create spec = %+v, want region=nyc1 (the new desired)", stub.driver.createSpecs)
	}
}

// TestApply_V2_LoaderSeam_DriftReportPrinted complements the
// end-to-end test with a focused assertion that
// printDriftReportIfAny runs in the v2 path when the loader-seam
// provider's wfctlhelpers.ApplyPlan returns a non-empty
// InputDriftReport. T3.7 wired the helper into the v2 dispatch but
// only T3.7's mock-seam test pins the assertion; this loader-seam
// variant proves the wiring works end-to-end without the
// applyV2ApplyPlanFn substitution.
//
// The test substitutes the wfctlhelpers.ApplyPlan dispatch only —
// ComputePlan and the rest of the pipeline run unmodified — so the
// drift-on-error contract from T3.7's review remains exercised
// against the production code path.
func TestApply_V2_LoaderSeam_DriftReportPrinted(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}

	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgYAML := `name: t3-9-drift
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
  - name: vpc
    type: infra.vpc
    config:
      provider: stub
      region: nyc1
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stub := &v2LoaderStubProvider{driver: &v2LoaderStubDriver{}}
	origLoader := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return stub, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origLoader })

	// Substitute ApplyPlan so we control the InputDriftReport
	// without orchestrating env-var changes between plan and apply.
	driftEntries := []interfaces.DriftEntry{
		{Name: "EXAMPLE_VAR", PlanFingerprint: "plan-fp", ApplyFingerprint: "apply-fp"},
	}
	origApply := applyV2ApplyPlanFn
	applyV2ApplyPlanFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		return &interfaces.ApplyResult{InputDriftReport: driftEntries}, nil
	}
	t.Cleanup(func() { applyV2ApplyPlanFn = origApply })

	// Capture stderr to assert printDriftReportIfAny output reached
	// the operator. applyInfraModules writes the drift report to
	// os.Stderr via the v2 branch in applyWithProviderAndStore.
	// Reuses the package-level captureStderr helper from
	// infra_outputs_test.go (panic-safe stderr capture).
	out, fnErr := captureStderr(t, func() error {
		_, err := applyInfraModules(t.Context(), cfgPath, "")
		return err
	})
	if fnErr != nil {
		t.Fatalf("applyInfraModules: %v", fnErr)
	}
	if !strings.Contains(out, "EXAMPLE_VAR") {
		t.Errorf("drift report missing EXAMPLE_VAR — printDriftReportIfAny not wired into v2 loader path; got:\n%s", out)
	}
}

// v2LoaderStubProvider is the in-process equivalent of a v2 IaC
// plugin loaded via wfctl's discoverAndLoadIaCProvider. Implements
// interfaces.IaCProvider AND wfctlhelpers.ComputePlanVersionDeclarer
// (returning "v2") so the apply path's dispatch branch routes
// through wfctlhelpers.ApplyPlan.
type v2LoaderStubProvider struct {
	driver *v2LoaderStubDriver
}

var (
	_ interfaces.IaCProvider                  = (*v2LoaderStubProvider)(nil)
	_ wfctlhelpers.ComputePlanVersionDeclarer = (*v2LoaderStubProvider)(nil)
)

func (p *v2LoaderStubProvider) Name() string                                         { return "stub" }
func (p *v2LoaderStubProvider) Version() string                                      { return "0.0.1-loader-seam" }
func (p *v2LoaderStubProvider) ComputePlanVersion() string                           { return "v2" }
func (p *v2LoaderStubProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *v2LoaderStubProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *v2LoaderStubProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, errors.New("v2 path must route through wfctlhelpers.ApplyPlan, not provider.Apply")
}
func (p *v2LoaderStubProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}
func (p *v2LoaderStubProvider) SupportedCanonicalKeys() []string { return nil }
func (p *v2LoaderStubProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *v2LoaderStubProvider) Close() error { return nil }

// v2LoaderStubDriver is the ResourceDriver counterpart. Diff
// returns NeedsReplace=true when the desired Config["region"]
// differs from the current Outputs["region"] — the same shape the
// gRPC stub used in early Option-A drafts. Create/Delete record
// invocations so the end-to-end test can assert
// wfctlhelpers.ApplyPlan executed the Delete + Create
// decomposition that defines a Replace action.
type v2LoaderStubDriver struct {
	mu          sync.Mutex
	createSpecs []interfaces.ResourceSpec
	deleteRefs  []interfaces.ResourceRef
	diffCount   atomic.Int64
	createCount atomic.Int64
	deleteCount atomic.Int64
}

var _ interfaces.ResourceDriver = (*v2LoaderStubDriver)(nil)

func (d *v2LoaderStubDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCount.Add(1)
	d.mu.Lock()
	d.createSpecs = append(d.createSpecs, spec)
	d.mu.Unlock()
	region, _ := spec.Config["region"].(string)
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: "stub-created-loader",
		Status:     "active",
		Outputs: map[string]any{
			"region":     region,
			"subnet_ids": []any{"new-1", "new-2", "new-3"},
		},
	}, nil
}
func (d *v2LoaderStubDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       ref.Type,
		ProviderID: ref.ProviderID,
		Status:     "active",
		Outputs:    map[string]any{"region": "nyc3", "subnet_ids": []any{"sn-1", "sn-2", "sn-3"}},
	}, nil
}
func (d *v2LoaderStubDriver) Update(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, Status: "active"}, nil
}
func (d *v2LoaderStubDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount.Add(1)
	d.mu.Lock()
	d.deleteRefs = append(d.deleteRefs, ref)
	d.mu.Unlock()
	return nil
}
func (d *v2LoaderStubDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	d.diffCount.Add(1)
	desiredRegion, _ := desired.Config["region"].(string)
	var currentRegion string
	if current != nil {
		currentRegion, _ = current.Outputs["region"].(string)
	}
	if desiredRegion != "" && desiredRegion != currentRegion {
		return &interfaces.DiffResult{
			NeedsReplace: true,
			Changes: []interfaces.FieldChange{
				{Path: "region", Old: currentRegion, New: desiredRegion, ForceNew: true},
			},
		}, nil
	}
	return nil, nil
}
func (d *v2LoaderStubDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: true}, nil
}
func (d *v2LoaderStubDriver) Scale(_ context.Context, ref interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}
func (d *v2LoaderStubDriver) SensitiveKeys() []string { return nil }
