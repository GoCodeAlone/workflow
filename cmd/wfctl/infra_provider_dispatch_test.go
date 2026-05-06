package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── resolveProviderDefs ────────────────────────────────────────────────────────

func TestResolveProviderDefs_BasicProviders(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "prov-a", Type: "iac.provider", Config: map[string]any{"provider": "cloud-a"}},
			{Name: "prov-b", Type: "iac.provider", Config: map[string]any{"provider": "cloud-b"}},
			{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"provider": "prov-a"}},
		},
	}

	defs, typeCounts, disabled := resolveProviderDefs(cfg, "")

	if _, ok := defs["prov-a"]; !ok {
		t.Error("expected prov-a in defs")
	}
	if _, ok := defs["prov-b"]; !ok {
		t.Error("expected prov-b in defs")
	}
	if _, ok := defs["vpc"]; ok {
		t.Error("infra.vpc should not be in defs (not an iac.provider module)")
	}
	if got := typeCounts["cloud-a"]; got != 1 {
		t.Errorf("typeCounts[cloud-a] = %d, want 1", got)
	}
	if got := typeCounts["cloud-b"]; got != 1 {
		t.Errorf("typeCounts[cloud-b] = %d, want 1", got)
	}
	if len(disabled) != 0 {
		t.Errorf("disabled should be empty, got %v", disabled)
	}
}

func TestResolveProviderDefs_EnvDisabledProvider(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "prov-a",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "cloud-a"},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": nil, // null entry disables the module
				},
			},
			{Name: "prov-b", Type: "iac.provider", Config: map[string]any{"provider": "cloud-b"}},
		},
	}

	defs, _, disabled := resolveProviderDefs(cfg, "staging")

	if _, ok := defs["prov-a"]; ok {
		t.Error("prov-a should not be in defs: it is disabled for staging")
	}
	if _, ok := disabled["prov-a"]; !ok {
		t.Error("prov-a should be in disabled set for staging")
	}
	if _, ok := defs["prov-b"]; !ok {
		t.Error("prov-b should be in defs: it has no staging override")
	}
}

func TestResolveProviderDefs_TypeCounts_SameType(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "do-1", Type: "iac.provider", Config: map[string]any{"provider": "digitalocean"}},
			{Name: "do-2", Type: "iac.provider", Config: map[string]any{"provider": "digitalocean"}},
		},
	}

	_, typeCounts, _ := resolveProviderDefs(cfg, "")

	if got := typeCounts["digitalocean"]; got != 2 {
		t.Errorf("typeCounts[digitalocean] = %d, want 2", got)
	}
}

// ── groupSpecsByProviderRef ────────────────────────────────────────────────────

func TestGroupSpecsByProviderRef_StableFirstReferenceOrder(t *testing.T) {
	defs := map[string]providerDef{
		"prov-a": {provType: "cloud-a"},
		"prov-b": {provType: "cloud-b"},
	}
	specs := []interfaces.ResourceSpec{
		{Name: "res1", Type: "infra.vpc", Config: map[string]any{"provider": "prov-b"}},
		{Name: "res2", Type: "infra.vm", Config: map[string]any{"provider": "prov-a"}},
		{Name: "res3", Type: "infra.db", Config: map[string]any{"provider": "prov-b"}},
		{Name: "res4", Type: "infra.lb", Config: map[string]any{"provider": "prov-a"}},
	}

	order, groups, err := groupSpecsByProviderRef(specs, defs, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// prov-b appears first (res1), prov-a appears second (res2).
	wantOrder := []string{"prov-b", "prov-a"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("group order = %v, want %v", order, wantOrder)
	}
	if got := len(groups["prov-a"].specs); got != 2 {
		t.Errorf("prov-a spec count = %d, want 2", got)
	}
	if got := len(groups["prov-b"].specs); got != 2 {
		t.Errorf("prov-b spec count = %d, want 2", got)
	}
}

func TestGroupSpecsByProviderRef_MissingProviderField(t *testing.T) {
	defs := map[string]providerDef{"prov-a": {provType: "cloud-a"}}
	specs := []interfaces.ResourceSpec{
		{Name: "bad-res", Type: "infra.vpc", Config: map[string]any{}},
	}

	_, _, err := groupSpecsByProviderRef(specs, defs, nil, "")
	if err == nil {
		t.Fatal("expected error for missing provider field, got nil")
	}
}

func TestGroupSpecsByProviderRef_DisabledProviderError(t *testing.T) {
	defs := map[string]providerDef{}
	disabled := map[string]struct{}{"prov-disabled": {}}
	specs := []interfaces.ResourceSpec{
		{Name: "res", Type: "infra.vpc", Config: map[string]any{"provider": "prov-disabled"}},
	}

	_, _, err := groupSpecsByProviderRef(specs, defs, disabled, "staging")
	if err == nil {
		t.Fatal("expected error for disabled provider, got nil")
	}
}

func TestGroupSpecsByProviderRef_UndeclaredProviderError(t *testing.T) {
	defs := map[string]providerDef{}
	specs := []interfaces.ResourceSpec{
		{Name: "res", Type: "infra.vpc", Config: map[string]any{"provider": "prov-missing"}},
	}

	_, _, err := groupSpecsByProviderRef(specs, defs, nil, "")
	if err == nil {
		t.Fatal("expected error for undeclared provider, got nil")
	}
}

func TestGroupSpecsByProviderRef_NilConfigTreatedAsMissingProvider(t *testing.T) {
	defs := map[string]providerDef{"prov-a": {provType: "cloud-a"}}
	specs := []interfaces.ResourceSpec{
		{Name: "nil-cfg-res", Type: "infra.vpc", Config: nil},
	}

	_, _, err := groupSpecsByProviderRef(specs, defs, nil, "")
	if err == nil {
		t.Fatal("expected error for nil Config (treated as missing provider field), got nil")
	}
}

func TestGroupSpecsByProviderRef_EmptyProvTypeError(t *testing.T) {
	defs := map[string]providerDef{
		"prov-no-type": {provType: ""}, // provider field missing from module config
	}
	specs := []interfaces.ResourceSpec{
		{Name: "res", Type: "infra.vpc", Config: map[string]any{"provider": "prov-no-type"}},
	}

	_, _, err := groupSpecsByProviderRef(specs, defs, nil, "")
	if err == nil {
		t.Fatal("expected error for empty provType, got nil")
	}
}

// ── plan-apply grouping equivalence via real entry points ─────────────────────

// capturedPlanCall records what was passed to the computeInfraPlan seam for
// one provider group, allowing plan- and apply-path behaviour to be compared.
type capturedPlanCall struct {
	provType  string
	specNames []string
}

// TestPlanApplyGroupingEquivalence_ViaRealEntryPoints exercises both
// computePlanForInfraSpecs (plan path) and applyInfraModules (apply path)
// against the same YAML config and asserts that both dispatch through
// computeInfraPlan with the same (provType, specNames) sequence per group.
//
// Because we spy on the actual package-level seams used by both functions,
// any future change that makes one path bypass the shared helpers will be
// caught here — unlike a test that re-invokes the helpers directly.
func TestPlanApplyGroupingEquivalence_ViaRealEntryPoints(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: aws-prov
    type: iac.provider
    config:
      provider: aws
  - name: gcp-prov
    type: iac.provider
    config:
      provider: gcp
  - name: bucket
    type: infra.s3_bucket
    config:
      provider: aws-prov
  - name: cluster
    type: infra.gke_cluster
    config:
      provider: gcp-prov
  - name: fn
    type: infra.function
    config:
      provider: aws-prov
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origResolve := resolveIaCProvider
	origPlan := computeInfraPlan
	t.Cleanup(func() {
		resolveIaCProvider = origResolve
		computeInfraPlan = origPlan
	})

	// currentProvType is set by the resolveIaCProvider spy immediately before
	// each group's computeInfraPlan call. Both seam calls happen sequentially
	// within the same group closure/goroutine, so this is safe without a mutex.
	var currentProvType string
	resolveIaCProvider = func(_ context.Context, pt string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		currentProvType = pt
		return &applyCapture{}, nil, nil
	}

	// ── plan path ────────────────────────────────────────────────────────────
	var planCalls []capturedPlanCall
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		planCalls = append(planCalls, capturedPlanCall{provType: currentProvType, specNames: specNames(specs)})
		return interfaces.IaCPlan{}, nil
	}

	desired, err := parseInfraResourceSpecsForEnv(cfgPath, "")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecsForEnv: %v", err)
	}
	if _, err := computePlanForInfraSpecs(context.Background(), cfgPath, "", desired, nil); err != nil {
		t.Fatalf("computePlanForInfraSpecs: %v", err)
	}

	// ── apply path ───────────────────────────────────────────────────────────
	currentProvType = ""
	var applyCalls []capturedPlanCall
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		applyCalls = append(applyCalls, capturedPlanCall{provType: currentProvType, specNames: specNames(specs)})
		return interfaces.IaCPlan{}, nil // empty plan → applyWithProviderAndStore returns immediately
	}

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	// ── assert same grouping sequence ────────────────────────────────────────
	if !reflect.DeepEqual(planCalls, applyCalls) {
		t.Errorf("plan-vs-apply computeInfraPlan call sequence diverges:\n  plan:  %v\n  apply: %v", planCalls, applyCalls)
	}
}

//nolint:unused
func specNames(specs []interfaces.ResourceSpec) []string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	return names
}
