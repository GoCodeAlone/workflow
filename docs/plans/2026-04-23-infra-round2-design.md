---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 179868b
  - repo: workflow
    commit: 8c76651
  - repo: workflow
    commit: 8faadf9
  - repo: workflow
    commit: 76497e5
  - repo: workflow
    commit: fc4c3e7
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - rg -n "ResolveForEnv|ConfigHash|ResolveSizing|Close\\(\\).*warning|plan-vs-apply" config platform cmd/wfctl
    - GOWORK=off go test ./interfaces ./config ./platform ./cmd/wfctl -run 'Test(Migration|Tenant|Canonical|BuildHook|PluginCLI|ScaffoldDockerfile|ResolveForEnv|ConfigHash|ApplyInfraModules|Diagnostic|Troubleshoot|ProviderID|ValidateProviderID|PluginInstall|ParseChecksums|Audit|WfctlManifest|WfctlLockfile|PluginLock|PluginAdd|PluginRemove|MigratePlugins|InfraOutputs)' -count=1
  result: pass
supersedes: []
superseded_by: []
---

# Infra Round 2: Display-vs-Behavior Contract Fixes — Design

**Status:** Approved (autonomous pipeline, 2026-04-23)

**Goal:** Ship workflow v0.18.7 closing display-vs-behavior contract violations in the infra apply pipeline (env-override name not applied; configHash determinism drift; ResolveSizing never called; Close() errors ignored) + a plan-vs-apply equivalence test harness that catches this class of divergence going forward. BMW staging deploy is blocked on this.

**Why:** The v0.18.6 release closed the state-persistence / destroy-regression / bootstrap-provider-leak gaps. Round-2 audit surfaced three silent contract violations where `wfctl infra plan` output describes one thing and `wfctl infra apply` does another. Worst example: plan shows resource `bmw-vpc` will be created as `bmw-staging-vpc` (env override), but apply creates it as `bmw-vpc` (raw module name). Users can't tell from plan what apply will do. These must be closed before BMW re-deploys + before any multi-env deployment can work (prod would collide with staging on the raw names).

## Architecture

Five surgical fixes in wfctl core + one test harness. No plugin changes needed; DO plugin v0.7.4 stays current.

1. **`ResolveForEnv` lifts `Config["name"]` into `ResolvedModule.Name`** — the config-layer fix for the env-override bug. After this, `ResourceSpec.Name` carries the env-resolved name, drivers honor it implicitly, plan and apply converge.

2. **`platform.configHash` sorts map keys deterministically** — matches the DO plugin's existing sorted-key hash. Eliminates spurious "update" detection on second apply with unchanged config.

3. **`applyInfraModules` invokes `provider.ResolveSizing(...)` before plan** — the interface method exists but has zero callers. Sizing resolves `Size: "m"` into concrete instance types before plan comparison. Without it, plan and apply may disagree on expected instance type.

4. **Provider `Close()` errors are logged as warnings** — `_ = closer.Close() //nolint:errcheck` becomes `if err := closer.Close(); err != nil { fmt.Printf("warning: provider shutdown: %v\n", err) }`. Plugin subprocess leaks now surface.

5. **Plan-vs-apply equivalence test harness** (`cmd/wfctl/infra_plan_apply_equivalence_test.go`): loads a BMW-shaped infra.yaml with env overrides, runs plan to capture intended resource names, runs apply against a recording fake provider that captures the spec.Name / spec.Type / spec.Config["name"] passed to `Apply`, asserts the recorded spec.Name matches what plan displayed. Regression gate for Bug #32 and its cousins.

6. **No full dry-run mode in v0.18.7** — Task #33 (structured API-call dry-run via new IaCProvider.DryRun interface method) remains a follow-up; significant interface change. The test harness above gives us 80% of the value without an interface expansion, using the existing `fakeIaCProvider` already in test code.

## Components

### 1. ResolveForEnv name-override lift

File: `config/module_resolve_env.go` (function `ResolveForEnv` at line ~18).

After merging envRes.Config into the resolved Config map, add:

```go
if n, ok := resolved.Config["name"].(string); ok && n != "" {
    resolved.Name = n
    delete(resolved.Config, "name") // config.name is now redundant; identity lives in ResolvedModule.Name
}
```

Remove `name` from Config so downstream doesn't see the override key (drivers would otherwise need to know to look for it). Identity is now exclusively in `ResolvedModule.Name`.

Test (`config/module_resolve_env_test.go`):

```go
func TestResolveForEnv_LiftsConfigNameIntoIdentity(t *testing.T) {
    m := &ModuleConfig{
        Name: "bmw-vpc", Type: "infra.vpc",
        Config: map[string]any{"cidr": "10.0.0.0/24"},
        Environments: map[string]EnvOverride{
            "staging": {Config: map[string]any{"name": "bmw-staging-vpc"}},
        },
    }
    resolved, ok := m.ResolveForEnv("staging")
    if !ok { t.Fatal("ResolveForEnv returned !ok") }
    if resolved.Name != "bmw-staging-vpc" {
        t.Errorf("Name = %q, want bmw-staging-vpc", resolved.Name)
    }
    if _, present := resolved.Config["name"]; present {
        t.Error("name should be stripped from Config after lift")
    }
}
```

### 2. configHash determinism

File: `platform/differ.go` (function `configHash` at line ~91).

Sort map keys before JSON-marshaling (match the DO plugin's existing pattern):

```go
func configHash(cfg map[string]any) string {
    keys := make([]string, 0, len(cfg))
    for k := range cfg { keys = append(keys, k) }
    sort.Strings(keys)
    ordered := make([]struct {
        K string
        V any
    }, len(keys))
    for i, k := range keys { ordered[i] = struct{K string; V any}{K: k, V: cfg[k]} }
    data, _ := json.Marshal(ordered)
    sum := sha256.Sum256(data)
    return fmt.Sprintf("%x", sum)
}
```

Test: `TestConfigHash_Stable_AcrossMapIterationOrder` — hash same map 100 times, assert all 100 hashes are identical.

### 3. ResolveSizing invocation

File: `cmd/wfctl/infra_apply.go`, in `applyInfraModules` at line 120 loop.

For each spec before adding to the provider group, if `spec.Size != ""`:

```go
sizing, err := provider.ResolveSizing(spec.Type, spec.Size, spec.Hints)
if err != nil {
    return fmt.Errorf("%s/%s: resolve sizing: %w", spec.Type, spec.Name, err)
}
if sizing != nil {
    if spec.Config == nil { spec.Config = map[string]any{} }
    spec.Config["instance_type"] = sizing.InstanceType
    for k, v := range sizing.Specs { spec.Config[k] = v }
}
```

This populates the concrete sizing into spec.Config so plan shows "instance_type: s-1vcpu-2gb" and apply uses the same value.

(If provider returns `nil, nil` — no resolution needed, spec unchanged.)

### 4. Close() error logging

File: `cmd/wfctl/infra_apply.go`, `cmd/wfctl/infra_destroy.go`, `cmd/wfctl/infra_status_drift.go`, `cmd/wfctl/infra_bootstrap.go`.

Replace every `defer closer.Close() //nolint:errcheck` with:

```go
defer func() {
    if err := closer.Close(); err != nil {
        fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", providerType, err)
    }
}()
```

### 5. Plan-vs-apply equivalence test harness

File: `cmd/wfctl/infra_plan_apply_equivalence_test.go` (new).

A recording fake `*fakeIaCProvider` already exists in tests (the one impl-digitalocean-2 built for v0.7.4). Replicate the pattern at the wfctl layer. Structure:

```go
// recordingProvider captures every ResourceSpec passed to Apply.
type recordingProvider struct {
    interfaces.IaCProvider
    applied []interfaces.ResourceSpec
}
func (r *recordingProvider) Apply(ctx context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
    for _, action := range plan.Actions {
        r.applied = append(r.applied, action.Resource)
    }
    // Return zero-value Result so apply succeeds without cloud calls.
    return &interfaces.ApplyResult{Resources: nil}, nil
}

func TestPlanApplyEquivalence_EnvOverrideNames(t *testing.T) {
    cfgFile := writeTestConfig(t, /* BMW-shaped config with environments.staging.config.name overrides */)
    
    // Render plan — capture displayed resource names via planResourcesForEnv.
    planned, err := planResourcesForEnv(cfgFile, "staging")
    if err != nil { t.Fatalf("plan: %v", err) }
    plannedNames := map[string]bool{}
    for _, rm := range planned { plannedNames[rm.Name] = true }
    // Expect: {bmw-staging-vpc, bmw-staging-firewall, bmw-staging-db, bmw-staging-app}
    
    // Run apply via a recording provider; capture actual spec.Name values.
    rp := &recordingProvider{}
    // ... invoke applyInfraModules with rp injected ...
    actualNames := map[string]bool{}
    for _, s := range rp.applied { actualNames[s.Name] = true }
    
    if !reflect.DeepEqual(plannedNames, actualNames) {
        t.Errorf("plan-vs-apply name divergence:\n  plan: %v\n  apply: %v", plannedNames, actualNames)
    }
}
```

This test would FAIL today (plan shows env-override names, apply uses module names) and PASS after fix #1. Going forward it's a regression gate for any future env-resolution / spec-construction drift.

## Data flow

```
infra.yaml + --env staging
  → LoadFromFile → []ModuleConfig
  → For each module: ResolveForEnv("staging")
      → (new) if Config["name"] exists, lift to ResolvedModule.Name; strip from Config
  → planResourcesForEnv returns []*ResolvedModule with corrected Name
  → specs built from ResolvedModule: ResourceSpec{Name: rm.Name, Type, Config}
  → plan renderer formats specs (uses spec.Name)
  → apply path:
      → applyWithProviderAndStore(ctx, provider, specs, current, store)
      → (new) for each spec: provider.ResolveSizing → merge into spec.Config
      → platform.ComputePlan(specs, current)
          → (new) configHash is deterministic
      → provider.Apply(ctx, plan)  # spec.Name is now env-resolved
      → state saved with Name = env-resolved name
      → (new) closer.Close() errors logged
```

## Testing

- `config/module_resolve_env_test.go`: `TestResolveForEnv_LiftsConfigNameIntoIdentity` + `TestResolveForEnv_PreservesNameWhenNoOverride` + `TestResolveForEnv_EmptyNameFieldIgnored`
- `platform/differ_test.go`: `TestConfigHash_Stable_AcrossMapIterationOrder` (loops 100 iterations, asserts 1 unique hash)
- `cmd/wfctl/infra_apply_test.go`: `TestApplyInfraModules_CallsResolveSizing_ForEachSpec` (recording fake provider counts ResolveSizing calls)
- `cmd/wfctl/infra_apply_test.go`: `TestApplyWithProvider_LogsCloseError` (uses a closer that returns error, asserts stderr output)
- `cmd/wfctl/infra_plan_apply_equivalence_test.go` (new file): full end-to-end harness above
- `cmd/wfctl/infra_env_wire_test.go`: add `TestPlanResourcesForEnv_UsesEnvOverrideNames` asserting ResolvedModule.Name matches env override

## Deferred (explicit non-goals)

- **Task #33 (full dry-run mode)**: structured API-call capture via a new IaCProvider.DryRun interface method. Requires interface expansion + DO plugin implementation. Defer. The equivalence harness in this release catches the same class of bug at the spec layer without the interface change.
- **Critical #3 (multi-provider state contamination)**: needs `ResourceState.Provider` field + provider-scoped filtering. Non-trivial; not blocking BMW (single-provider).
- **Important #5 (plan-vs-applied-config divergence when provider mutates config)**: document the current semantics as "plan reflects spec; applied state may differ if provider derives values"; file as doc follow-up.
- **Minor #7/#8/#9**: env var expansion timing, DependsOn destroy order, destroy error UX. Low impact for BMW. File as follow-ups.
- **Tasks #28, #29, #30**: remain open follow-ups.

## Rollout phases

1. Phase A: v0.18.7 PR on workflow (all 5 components + tests). Branch `feat/v0.18.7-plan-apply-equivalence`.
2. Phase B: merge + tag v0.18.7.
3. Phase C: update BMW setup-wfctl pin from v0.18.6 → v0.18.7.
4. Phase D: re-trigger bmw-staging deploy (cluster is already torn down). Apply creates resources with correct env-override names (`bmw-staging-vpc` etc.).
5. Phase E: playwright verify staging.
6. Phase F: auto-promote to prod + /healthz green.

Phase A is the critical path. Phases B-F serialize on it.

## Success criteria

- `planResourcesForEnv(cfg, "staging").Name` returns `bmw-staging-vpc` for a module named `bmw-vpc` with env override.
- `configHash(m1) == configHash(m2)` when m1 and m2 are deeply equal maps with different key iteration orders.
- `wfctl infra apply --env staging` against BMW's infra.yaml creates DO resources named `bmw-staging-vpc`, `bmw-staging-firewall`, `bmw-staging-db`, `bmw-staging` (app).
- `TestPlanApplyEquivalence_EnvOverrideNames` passes.
- BMW `/healthz` green in staging + prod post-deploy.
