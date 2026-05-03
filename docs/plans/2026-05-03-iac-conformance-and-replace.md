# IaC Root-Cause Fixes + Provider Conformance Suite Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor wfctl IaC semantics to honor `NeedsReplace`, add per-module JIT secret resolution, ship a provider conformance suite + DO-targeted codemod, and migrate the DO plugin to the new contract — closing the 8 root-cause gaps surfaced by core-dump's self-hosted-PG deploy iteration.

**Architecture:** Per-plugin `computePlanVersion` opt-in (DO migrates to v2; AWS/GCP/Azure stay v1 until activated). New `wfctlhelpers.ApplyPlan` shared helper handles all 4 plan actions (create/update/replace/delete) with `upsertSupporter` hook + `replaceIDMap` for cascade ID propagation. Conformance suite at `iac/conformance/` runs scenarios against any `IaCProvider` implementation; DO smoke gate runs on every relevant PR.

**Tech Stack:** Go 1.23+, `golang.org/x/tools/go/analysis/passes` for codemod, `google.golang.org/protobuf` for plan.json, GitHub Actions for CI.

**Base branch:** main (workflow), main (workflow-plugin-digitalocean), main (core-dump)

---

## Scope Manifest

**PR Count:** 11
**Tasks:** 64
**Estimated Lines of Change:** ~5500 (informational; not enforced)

**Out of scope:**
- AWS / GCP / Azure plugin migrations (deferred to plugin-activation work; advisory codemod-lint reports filed as issues only)
- Removing `apply --refresh` flag (workflow PR #519); complementary to new `refresh-outputs`
- Removing `IaCProvider.Plan()` interface method (preserved for v1 plugins; consolidation deferred)
- Plan/apply two-step UX with persisted `plan.json` for cascade-replace or JIT-resolved plans (apply-without-`--plan` is the canonical path; persisted plans error at plan time when JIT is required)
- DO Managed Postgres support for AGE (DO product limitation; orthogonal)
- Zero-downtime cascade replace (current cascade has dependent unavailability window)
- Cross-team protected-replace coordination workflow (design surfaces information; coordination is organizational)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | W-1 IaCPlan schema + plan-stale diagnostic | T1.1 – T1.6 | feat/iac-plan-schema-diagnostic |
| 2 | W-2 wfctl infra refresh-outputs + cheap apply-time refresh | T2.1 – T2.7 | feat/iac-refresh-outputs |
| 3 | W-3 Replace action + ApplyPlan helper + delete-via-Apply fix | T3.1 – T3.10 | feat/iac-replace-action |
| 4 | W-4 Provider.ValidatePlan + R-A10 align rule | T4.1 – T4.5 | feat/iac-validate-plan |
| 5 | W-5 Per-module JIT secret resolution + ProviderID propagation | T5.1 – T5.7 | feat/iac-jit-secrets |
| 6 | W-6 --allow-replace flag + partial-cascade discovery | T6.1 – T6.4 | feat/iac-allow-replace |
| 7 | W-7 iac/conformance/ package + DO smoke gate | T7.1 – T7.13 | feat/iac-conformance-suite |
| 8 | W-8 cmd/iac-codemod/ tooling | T8.1 – T8.7 | feat/iac-codemod |
| 9 | W-9 plugin.json computePlanVersion + ProviderPlanner | T9.1 – T9.4 | feat/iac-compute-plan-version |
| 10 | P-DO DigitalOcean plugin migration to v2 | TP1 – TP5 | feat/iac-v2-migration |
| 11 | C-1 core-dump staging-PG cutover to nyc1 | TC1 – TC2 | fix/staging-pg-nyc1-cutover |

**Status:** Draft

---

## Task Conventions

- Each task lists **Files**, **Steps**, **Verification**, and (for runtime-affecting tasks) a **Rollback** note.
- TDD: Step 1 = write failing test; Step 2 = verify failure; Step 3 = implement; Step 4 = verify pass; Step 5 = commit.
- Commit messages follow conventional commits (`feat(iac): ...`, `fix(plugin): ...`).
- Conformance scenario references use the names defined in W-7. Tasks before W-7 stub the assertion as `t.Skip("scenario added in W-7")` and W-7 fills the body.
- All workflow tasks run from `/Users/jon/workspace/workflow/_worktrees/<branch>/`. P-DO from `/Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/<branch>/`. C-1 from `/Users/jon/workspace/core-dump/_worktrees/<branch>/`.

---

## PR W-1: IaCPlan schema + plan-stale diagnostic

**Goal:** Apply detects plan-vs-apply input drift with per-key diagnostic; per-action ResolvedConfigHash enables per-resource error scoping.

### Task T1.1: Define `IaCPlan.SchemaVersion` + `IaCPlan.InputSnapshot` + `PlanAction.ResolvedConfigHash`

**Files:**
- Modify: `interfaces/iac_state.go`
- Test: `interfaces/iac_state_test.go` (create)

**Step 1: Write failing test for schema fields**

```go
// interfaces/iac_state_test.go
package interfaces

import (
    "encoding/json"
    "testing"
)

func TestIaCPlan_SchemaVersionField(t *testing.T) {
    p := IaCPlan{SchemaVersion: 2}
    data, err := json.Marshal(p)
    if err != nil { t.Fatal(err) }
    var got IaCPlan
    if err := json.Unmarshal(data, &got); err != nil { t.Fatal(err) }
    if got.SchemaVersion != 2 {
        t.Errorf("SchemaVersion roundtrip: got %d want 2", got.SchemaVersion)
    }
}

func TestIaCPlan_InputSnapshotField(t *testing.T) {
    p := IaCPlan{InputSnapshot: map[string]string{"FOO": "deadbeefcafebabe"}}
    data, _ := json.Marshal(p)
    var got IaCPlan
    json.Unmarshal(data, &got)
    if got.InputSnapshot["FOO"] != "deadbeefcafebabe" {
        t.Errorf("InputSnapshot roundtrip failed: %v", got.InputSnapshot)
    }
}

func TestPlanAction_ResolvedConfigHashField(t *testing.T) {
    a := PlanAction{Action: "create", ResolvedConfigHash: "sha256:abc"}
    data, _ := json.Marshal(a)
    var got PlanAction
    json.Unmarshal(data, &got)
    if got.ResolvedConfigHash != "sha256:abc" {
        t.Errorf("ResolvedConfigHash: got %q", got.ResolvedConfigHash)
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd interfaces && go test -run TestIaCPlan_SchemaVersionField -v`
Expected: FAIL with `unknown field SchemaVersion in struct literal of type interfaces.IaCPlan`

**Step 3: Add fields to interfaces/iac_state.go**

Add to `type IaCPlan struct`:
```go
// SchemaVersion is bumped when on-disk plan format changes (W-5 sets to 2 when JIT is required).
SchemaVersion int `json:"schema_version,omitempty"`

// InputSnapshot records every env var name read during ${VAR} substitution
// at plan time, mapped to a 16-hex-char (64-bit) sha256 prefix of the value.
// Apply re-computes inputs and prints diagnostic on mismatch.
InputSnapshot map[string]string `json:"input_snapshot,omitempty"`
```

Add to `type PlanAction struct`:
```go
// ResolvedConfigHash is the SHA-256 of POST-substitution Resource.Config.
// Apply re-computes per-action and surfaces per-resource diagnostic on mismatch.
ResolvedConfigHash string `json:"resolved_config_hash,omitempty"`
```

**Step 4: Run tests to verify pass**

Run: `cd interfaces && go test -v`
Expected: PASS — 3 tests in iac_state_test.go

**Step 5: Commit**

```bash
git add interfaces/iac_state.go interfaces/iac_state_test.go
git commit -m "feat(iac): add IaCPlan.SchemaVersion + InputSnapshot + PlanAction.ResolvedConfigHash"
```

### Task T1.2: Implement `inputSnapshot.Compute(envProvider)` helper

**Files:**
- Create: `iac/inputsnapshot/snapshot.go`
- Test: `iac/inputsnapshot/snapshot_test.go`

**Step 1: Write failing test**

```go
// iac/inputsnapshot/snapshot_test.go
package inputsnapshot

import "testing"

func TestCompute_FingerprintIs16HexChars(t *testing.T) {
    snap := Compute([]string{"FOO"}, func(k string) (string, bool) {
        return "the-value", true
    })
    if got := snap["FOO"]; len(got) != 16 {
        t.Errorf("fingerprint len = %d, want 16; got %q", len(got), got)
    }
}

func TestCompute_DeterministicAcrossRuns(t *testing.T) {
    env := func(k string) (string, bool) { return "v", true }
    a := Compute([]string{"FOO"}, env)
    b := Compute([]string{"FOO"}, env)
    if a["FOO"] != b["FOO"] {
        t.Errorf("non-deterministic: %q vs %q", a["FOO"], b["FOO"])
    }
}

func TestCompute_DifferentValuesDifferentFingerprints(t *testing.T) {
    env1 := func(k string) (string, bool) { return "value-one", true }
    env2 := func(k string) (string, bool) { return "value-two", true }
    a := Compute([]string{"FOO"}, env1)
    b := Compute([]string{"FOO"}, env2)
    if a["FOO"] == b["FOO"] {
        t.Errorf("fingerprints should differ: %q == %q", a["FOO"], b["FOO"])
    }
}

func TestCompute_MissingEnvVarOmitted(t *testing.T) {
    snap := Compute([]string{"NOT_SET"}, func(k string) (string, bool) {
        return "", false
    })
    if _, ok := snap["NOT_SET"]; ok {
        t.Errorf("missing env should be omitted, got %q", snap["NOT_SET"])
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd iac/inputsnapshot && go test -v`
Expected: FAIL with package not found

**Step 3: Implement**

```go
// iac/inputsnapshot/snapshot.go
// Package inputsnapshot computes plan-time env-var fingerprints for the
// plan-stale diagnostic. Fingerprints are 16 hex chars (64 bits of preimage
// resistance); plan.json is treated as semi-sensitive and gitignored.
package inputsnapshot

import (
    "crypto/sha256"
    "encoding/hex"
)

// Compute returns a map of env-var name → 16-hex-char sha256 prefix of the value.
// Variables that aren't set (lookup returns ok=false) are omitted from the snapshot.
func Compute(varNames []string, lookup func(string) (string, bool)) map[string]string {
    out := make(map[string]string)
    for _, name := range varNames {
        val, ok := lookup(name)
        if !ok {
            continue
        }
        sum := sha256.Sum256([]byte(val))
        out[name] = hex.EncodeToString(sum[:])[:16]
    }
    return out
}
```

**Step 4: Run tests to verify pass**

Run: `cd iac/inputsnapshot && go test -v`
Expected: PASS — 4 tests

**Step 5: Commit**

```bash
git add iac/inputsnapshot/
git commit -m "feat(iac): add inputsnapshot.Compute for plan-stale diagnostic fingerprints"
```

### Task T1.3: Wire InputSnapshot into `wfctl infra plan` output

**Files:**
- Modify: `cmd/wfctl/infra.go` (find `runInfraPlan` or equivalent; the function that writes plan.json)
- Test: `cmd/wfctl/infra_plan_inputsnapshot_test.go` (create)

**Step 1: Write failing test**

```go
// cmd/wfctl/infra_plan_inputsnapshot_test.go
package main

import (
    "encoding/json"
    "os"
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestPlanWritesInputSnapshot(t *testing.T) {
    t.Setenv("STAGING_DB_PASSWORD", "secret-value")
    cfgYAML := `
infra:
  modules:
    - name: app
      type: infra.container_service
      config:
        env_vars:
          DATABASE_URL: "postgres://user:${STAGING_DB_PASSWORD}@host:5432/db"
`
    cfgFile := writeTestConfig(t, cfgYAML)
    planFile := t.TempDir() + "/plan.json"
    err := runInfraPlanForTest(cfgFile, planFile)
    if err != nil { t.Fatal(err) }
    data, _ := os.ReadFile(planFile)
    var plan interfaces.IaCPlan
    json.Unmarshal(data, &plan)
    if plan.InputSnapshot["STAGING_DB_PASSWORD"] == "" {
        t.Errorf("plan.InputSnapshot missing STAGING_DB_PASSWORD; got %v", plan.InputSnapshot)
    }
    if len(plan.InputSnapshot["STAGING_DB_PASSWORD"]) != 16 {
        t.Errorf("fingerprint should be 16 hex chars, got %d", len(plan.InputSnapshot["STAGING_DB_PASSWORD"]))
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd cmd/wfctl && go test -run TestPlanWritesInputSnapshot -v`
Expected: FAIL — InputSnapshot empty

**Step 3: Implement**

In `cmd/wfctl/infra.go` (find `runInfraPlan`), after computing the plan but before writing to disk:

1. Walk all module.Config values; extract env-var names referenced in `${VAR}` patterns (use existing `os.Expand`-compatible regex).
2. Call `inputsnapshot.Compute(referencedNames, os.LookupEnv)`.
3. Set `plan.InputSnapshot` and `plan.SchemaVersion = 1` (W-5 will bump to 2).

Reference: existing `config.ExpandEnvInMap` already does the substitution; extract the variable names by scanning before substitution.

**Step 4: Run tests to verify pass**

Run: `cd cmd/wfctl && go test -run TestPlanWritesInputSnapshot -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/infra.go cmd/wfctl/infra_plan_inputsnapshot_test.go
git commit -m "feat(iac): wfctl infra plan writes InputSnapshot to plan.json"
```

### Task T1.4: Wire ResolvedConfigHash into PlanAction emission

**Files:**
- Modify: `platform/differ.go` (ComputePlan)
- Modify: `cmd/wfctl/infra.go` (or wherever ResolvedConfigHash should be computed)
- Test: `platform/differ_test.go` (extend)

**Step 1: Write failing test**

```go
// platform/differ_test.go (add)
func TestComputePlan_PerActionResolvedConfigHash(t *testing.T) {
    desired := []interfaces.ResourceSpec{
        {Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
    }
    plan, err := ComputePlan(context.Background(), nil, desired, nil)
    if err != nil { t.Fatal(err) }
    if len(plan.Actions) != 1 || plan.Actions[0].ResolvedConfigHash == "" {
        t.Errorf("expected ResolvedConfigHash on create action, got %+v", plan.Actions)
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd platform && go test -run TestComputePlan_PerActionResolvedConfigHash -v`
Expected: FAIL

**Step 3: Implement**

In `platform/differ.go::ComputePlan`, after building each `PlanAction`, set `ResolvedConfigHash: configHash(spec.Config)` (the existing `configHash` function). For `update` actions, compute against the post-substitution Config (config has already been substituted by caller).

**Step 4: Run test to verify pass**

Run: `cd platform && go test -v`
Expected: PASS

**Step 5: Commit**

```bash
git add platform/differ.go platform/differ_test.go
git commit -m "feat(iac): ComputePlan sets PlanAction.ResolvedConfigHash"
```

### Task T1.5: Apply diagnostic — print per-resource + per-key drift on plan-stale

**Files:**
- Modify: `cmd/wfctl/infra.go:1071` (where "plan stale" is raised today)
- Test: `cmd/wfctl/infra_apply_plan_test.go` (extend)

**Step 1: Write failing test**

```go
// cmd/wfctl/infra_apply_plan_test.go (add)
func TestApply_PlanStaleDiagnostic_NamesChangedKeys(t *testing.T) {
    t.Setenv("STAGING_DB_PASSWORD", "old-value")
    planFile := writePlanWithInputSnapshot(t, map[string]string{
        "STAGING_DB_PASSWORD": "fingerprint-of-old-value-1234",
    })
    // Apply with a different value
    t.Setenv("STAGING_DB_PASSWORD", "new-value")
    err := runInfraApplyForTest(planFile)
    if err == nil {
        t.Fatal("expected plan-stale error")
    }
    msg := err.Error()
    if !strings.Contains(msg, "STAGING_DB_PASSWORD") {
        t.Errorf("error should name the changed key; got: %s", msg)
    }
    if !strings.Contains(msg, "plan stale") {
        t.Errorf("error should preserve 'plan stale' marker; got: %s", msg)
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd cmd/wfctl && go test -run TestApply_PlanStaleDiagnostic -v`
Expected: FAIL — error doesn't name the key

**Step 3: Implement**

In `cmd/wfctl/infra.go` near line 1071, before raising "plan stale":
1. Re-compute apply-time InputSnapshot using same name list as plan.json.
2. Compare key-by-key against `plan.InputSnapshot`.
3. Build a list of `(name, planFingerprint, applyFingerprint)` for differing keys.
4. Format error:
   ```
   error: plan stale: %d input(s) changed since plan
     %s: fingerprint %s (plan) → %s (apply)
     ...
     hint: ensure all env vars referenced by infra.yaml are exported to both Plan and Apply steps
   ```

**Step 4: Run test to verify pass**

Run: `cd cmd/wfctl && go test -run TestApply_PlanStaleDiagnostic -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/infra.go cmd/wfctl/infra_apply_plan_test.go
git commit -m "feat(iac): plan-stale error names changed env-var keys"
```

### Task T1.6: Add `plan.json` gitignore-validate warning at `wfctl infra plan` time

**Files:**
- Modify: `cmd/wfctl/infra.go` (runInfraPlan)
- Test: `cmd/wfctl/infra_plan_gitignore_test.go` (create)

**Step 1: Write failing test**

```go
// cmd/wfctl/infra_plan_gitignore_test.go
func TestPlan_WarnsOnMissingGitignoreEntry(t *testing.T) {
    repo := t.TempDir()
    os.WriteFile(repo+"/.gitignore", []byte("# empty\n"), 0644)
    out, _ := captureStderr(t, func() { runInfraPlanWithCwd(repo, "plan.json") })
    if !strings.Contains(out, "plan.json") || !strings.Contains(out, "gitignore") {
        t.Errorf("expected gitignore warning, got: %s", out)
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd cmd/wfctl && go test -run TestPlan_WarnsOnMissingGitignoreEntry -v`
Expected: FAIL

**Step 3: Implement**

In `runInfraPlan`, before writing plan.json, check `.gitignore` for `plan.json` or `**/plan.json` entry; emit warning to stderr if absent.

**Step 4: Run test to verify pass**

Run: `cd cmd/wfctl && go test -run TestPlan_WarnsOnMissingGitignoreEntry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/infra.go cmd/wfctl/infra_plan_gitignore_test.go
git commit -m "feat(iac): wfctl infra plan warns when plan.json not in .gitignore"
```

**Verification (PR W-1):**
- `go test ./interfaces/... ./iac/inputsnapshot/... ./platform/... ./cmd/wfctl/...`
- Manual: build wfctl; run `wfctl infra plan -o plan.json` against a config with `${VAR}` references; inspect plan.json contains `input_snapshot` map.

**Rollback (PR W-1):** revert commits; existing wfctl plans without `SchemaVersion`/`InputSnapshot`/`ResolvedConfigHash` continue to apply (fields are `omitempty`; readers tolerate absence).

---

## PR W-2: wfctl infra refresh-outputs + cheap apply-time refresh

**Goal:** Refresh state outputs without invoking Update/Replace. Apply does cheap pre-step Read; standalone command for emergency state recovery.

### Task T2.1: Define `RefreshOutputs(ctx, provider, states, opts) ([]ResourceState, error)` helper

**Files:**
- Create: `iac/refreshoutputs/refresh.go`
- Test: `iac/refreshoutputs/refresh_test.go`

**Step 1: Write failing test**

```go
// iac/refreshoutputs/refresh_test.go
package refreshoutputs

import (
    "context"
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestRefreshOutputs_ReadsEachResource_PersistsChangedOnly(t *testing.T) {
    states := []interfaces.ResourceState{
        {Name: "vpc-1", Type: "infra.vpc", ProviderID: "uuid-1", Outputs: map[string]any{"ip_range": "10.0.0.0/16"}},
        {Name: "vpc-2", Type: "infra.vpc", ProviderID: "uuid-2", Outputs: map[string]any{"ip_range": "10.1.0.0/16"}},
    }
    fakeProvider := &fakeIaCProvider{readOutputs: map[string]map[string]any{
        "uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"}, // new "id" field
        "uuid-2": {"ip_range": "10.1.0.0/16"},                  // unchanged
    }}
    refreshed, err := Refresh(context.Background(), fakeProvider, states, Options{Concurrency: 2})
    if err != nil { t.Fatal(err) }
    if refreshed[0].Outputs["id"] != "uuid-1" {
        t.Errorf("vpc-1 should have new 'id' output: %v", refreshed[0].Outputs)
    }
    if !mapsEqual(refreshed[1].Outputs, states[1].Outputs) {
        t.Errorf("vpc-2 should be unchanged: %v vs %v", refreshed[1].Outputs, states[1].Outputs)
    }
}

func TestRefreshOutputs_PartialFailure_ReturnsError(t *testing.T) {
    states := []interfaces.ResourceState{
        {Name: "vpc-1", Type: "infra.vpc", ProviderID: "uuid-1"},
    }
    fakeProvider := &fakeIaCProvider{readErr: errors.New("network failure")}
    _, err := Refresh(context.Background(), fakeProvider, states, Options{Concurrency: 1})
    if err == nil {
        t.Errorf("expected error on Read failure")
    }
    if !strings.Contains(err.Error(), "could not refresh") {
        t.Errorf("error should mention 'could not refresh'; got: %v", err)
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd iac/refreshoutputs && go test -v`
Expected: FAIL — package missing

**Step 3: Implement**

```go
// iac/refreshoutputs/refresh.go
// Package refreshoutputs implements read-only state refresh — reads current
// Outputs from providers and updates state when fields differ. Never invokes
// Update or Replace at the cloud level.
package refreshoutputs

import (
    "context"
    "fmt"
    "sync"
    "github.com/GoCodeAlone/workflow/interfaces"
)

type Options struct {
    Concurrency int // default 8
}

func Refresh(ctx context.Context, p interfaces.IaCProvider, states []interfaces.ResourceState, opts Options) ([]interfaces.ResourceState, error) {
    if opts.Concurrency < 1 { opts.Concurrency = 8 }
    sem := make(chan struct{}, opts.Concurrency)
    out := make([]interfaces.ResourceState, len(states))
    copy(out, states)
    errs := make([]error, len(states))
    var wg sync.WaitGroup
    for i := range states {
        i := i
        sem <- struct{}{}
        wg.Add(1)
        go func() {
            defer wg.Done()
            defer func() { <-sem }()
            d, err := p.ResourceDriver(states[i].Type)
            if err != nil { errs[i] = err; return }
            ref := interfaces.ResourceRef{Name: states[i].Name, Type: states[i].Type, ProviderID: states[i].ProviderID}
            live, err := d.Read(ctx, ref)
            if err != nil { errs[i] = fmt.Errorf("could not refresh %q: %w", states[i].Name, err); return }
            if !equalMaps(live.Outputs, states[i].Outputs) {
                out[i].Outputs = cloneMap(live.Outputs)
            }
        }()
    }
    wg.Wait()
    for _, e := range errs {
        if e != nil { return nil, e }
    }
    return out, nil
}

// equalMaps + cloneMap helpers (omitted — implement as standard map deep-equal/copy)
```

**Step 4: Run tests to verify pass**

Run: `cd iac/refreshoutputs && go test -v`
Expected: PASS

**Step 5: Commit**

```bash
git add iac/refreshoutputs/
git commit -m "feat(iac): add refreshoutputs.Refresh — read-only state output refresh"
```

### Task T2.2: Add `wfctl infra refresh-outputs` subcommand

**Files:**
- Create: `cmd/wfctl/infra_refresh_outputs.go`
- Modify: `cmd/wfctl/main.go` or wherever subcommands register
- Test: `cmd/wfctl/infra_refresh_outputs_test.go`

**Step 1: Write failing test**

```go
// cmd/wfctl/infra_refresh_outputs_test.go
func TestRefreshOutputs_CommandHelp(t *testing.T) {
    out, _ := captureStdout(t, func() { wfctlMain([]string{"infra", "refresh-outputs", "--help"}) })
    if !strings.Contains(out, "Usage of infra refresh-outputs") {
        t.Errorf("help output missing; got: %s", out)
    }
}

func TestRefreshOutputs_PersistsNewFieldsToState(t *testing.T) {
    state := setupStateWithStaleVPC(t)
    cfgFile := writeTestConfigWithVPC(t)
    err := runInfraRefreshOutputs([]string{"-c", cfgFile, "--env", "staging"})
    if err != nil { t.Fatal(err) }
    refreshed := loadState(t)
    if refreshed["coredump-staging-vpc"].Outputs["id"] == "" {
        t.Errorf("expected 'id' field after refresh, got %v", refreshed["coredump-staging-vpc"].Outputs)
    }
}
```

**Step 2: Run test to verify failure**

Expected: FAIL — subcommand not registered.

**Step 3: Implement**

Create `cmd/wfctl/infra_refresh_outputs.go` with `runInfraRefreshOutputs(args []string) error`. Register in `infraCommands` map (or wherever subcommands dispatch). Use `iac/refreshoutputs`.

**Step 4: Run test to verify pass**

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_refresh_outputs.go cmd/wfctl/main.go cmd/wfctl/infra_refresh_outputs_test.go
git commit -m "feat(iac): add wfctl infra refresh-outputs subcommand"
```

### Task T2.3: Wire cheap apply-time refresh as opt-in pre-step

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` or `cmd/wfctl/infra.go::runInfraApply`
- Test: `cmd/wfctl/infra_apply_refresh_pre_test.go`

**Step 1: Write failing test**

```go
func TestApply_PreStepRefresh_OptInViaEnvVar(t *testing.T) {
    t.Setenv("WFCTL_REFRESH_OUTPUTS", "1")
    state := setupStateWithStaleVPC(t)
    cfgFile := writeTestConfigWithVPC(t)
    err := runInfraApply([]string{"-c", cfgFile, "--env", "staging"})
    if err != nil { t.Fatal(err) }
    refreshed := loadState(t)
    if refreshed["coredump-staging-vpc"].Outputs["id"] == "" {
        t.Errorf("apply pre-refresh should populate 'id'; got %v", refreshed["coredump-staging-vpc"].Outputs)
    }
}

func TestApply_PreStepRefresh_DisabledByDefault(t *testing.T) {
    // env not set
    state := setupStateWithStaleVPC(t)
    cfgFile := writeTestConfigWithVPC(t)
    runInfraApply([]string{"-c", cfgFile, "--env", "staging"})
    refreshed := loadState(t)
    if _, ok := refreshed["coredump-staging-vpc"].Outputs["id"]; ok {
        t.Errorf("default-off pre-refresh should not populate 'id'")
    }
}
```

**Step 2: Run test to verify failure**

Run: `cd cmd/wfctl && go test -run TestApply_PreStepRefresh -v`
Expected: FAIL

**Step 3: Implement**

In `runInfraApply`, after loading state but before computing the plan: if `os.Getenv("WFCTL_REFRESH_OUTPUTS") != ""`, call `refreshoutputs.Refresh(ctx, provider, states, Options{...})`. Persist refreshed states. On Read failure: abort apply with the same error from T2.1.

Add `--skip-refresh` flag that overrides the env var (apply proceeds without refresh).

**Step 4: Run test to verify pass**

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_refresh_pre_test.go
git commit -m "feat(iac): apply-time refresh-outputs pre-step (opt-in via WFCTL_REFRESH_OUTPUTS)"
```

**Rollback (T2.3):** unset `WFCTL_REFRESH_OUTPUTS` or pass `--skip-refresh` to disable. Reverting commit removes the code path entirely.

### Task T2.4: Conformance scenario stub `Scenario_OutputsRefreshDetectsNewFields`

**Files:**
- Create: `iac/conformance/scenario_outputs_refresh.go` (stub — body added in W-7)

**Step 1-5:** Create file with `func Scenario_OutputsRefreshDetectsNewFields(t *testing.T, cfg Config) { t.Skip("filled in W-7") }`. Commit: `chore(conformance): stub Scenario_OutputsRefreshDetectsNewFields`.

### Task T2.5: Concurrency stress test for refresh

**Files:**
- Test: `iac/refreshoutputs/refresh_concurrency_test.go`

**Step 1-5:** Test with 100 fake resources, concurrency=8; verify no deadlock + all states refreshed; verify Read called exactly once per resource.

### Task T2.6: Document `refresh-outputs` in `docs/WFCTL.md`

**Files:**
- Modify: `docs/WFCTL.md`

**Step 1-5:** Add `infra refresh-outputs` section to command reference. No test (doc-only). Commit: `docs(wfctl): document infra refresh-outputs subcommand`.

### Task T2.7: Runtime-launch-validation

**Files:** none new

**Step 1:** Build wfctl: `go build -o /tmp/wfctl ./cmd/wfctl`
**Step 2:** Run `/tmp/wfctl infra refresh-outputs --help`; expect help text printed.
**Step 3:** Against a fake state JSON (no real cloud), run `/tmp/wfctl infra refresh-outputs -c <fake.yaml> --env staging`; expect non-zero exit with clear "no provider configured for tests" error rather than a panic.

**Verification (PR W-2):**
- `go test ./iac/refreshoutputs/... ./cmd/wfctl/... -count=1 -race`
- Manual: smoke against the staging-PG state if available.

**Rollback (PR W-2):** revert commits. `WFCTL_REFRESH_OUTPUTS` env var is opt-in (default off), so post-revert behavior matches pre-W-2 exactly.

---

## PR W-3: Replace action + ApplyPlan helper + delete-via-Apply fix

**Goal:** `ComputePlan` calls `Diff` per resource; emits `replace` action when `NeedsReplace=true` or any `FieldChange.ForceNew=true`. New `wfctlhelpers.ApplyPlan` handles all 4 actions including the latent delete-via-Apply bug fix.

### Task T3.1: Create `iac/wfctlhelpers/` package skeleton + ApplyPlan signature

**Files:**
- Create: `iac/wfctlhelpers/apply.go`
- Test: `iac/wfctlhelpers/apply_test.go`

**Step 1: Write failing test**

```go
// iac/wfctlhelpers/apply_test.go
package wfctlhelpers

import (
    "context"
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestApplyPlan_HandlesAllFourActions(t *testing.T) {
    // Build a plan with one of each action type
    plan := &interfaces.IaCPlan{
        Actions: []interfaces.PlanAction{
            {Action: "create", Resource: spec("a", "infra.vpc")},
            {Action: "update", Resource: spec("b", "infra.vpc"), Current: state("b")},
            {Action: "replace", Resource: spec("c", "infra.vpc"), Current: state("c")},
            {Action: "delete", Resource: spec("d", "infra.vpc"), Current: state("d")},
        },
    }
    fp := &fakeProvider{}
    result, err := ApplyPlan(context.Background(), fp, plan)
    if err != nil { t.Fatal(err) }
    if len(result.Errors) != 0 {
        t.Errorf("expected no errors, got %v", result.Errors)
    }
    if !fp.calledCreate || !fp.calledUpdate || !fp.calledReplace || !fp.calledDelete {
        t.Errorf("not all action types invoked: %+v", fp)
    }
}
```

**Step 2: Run test to verify failure**

Expected: FAIL — package missing.

**Step 3: Implement**

```go
// iac/wfctlhelpers/apply.go
package wfctlhelpers

import (
    "context"
    "fmt"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func ApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
    result := &interfaces.ApplyResult{PlanID: plan.ID}
    for _, action := range plan.Actions {
        d, err := p.ResourceDriver(action.Resource.Type)
        if err != nil {
            result.Errors = append(result.Errors, interfaces.ActionError{
                Resource: action.Resource.Name, Action: action.Action, Error: err.Error(),
            })
            continue
        }
        if err := dispatchAction(ctx, d, action, result); err != nil {
            result.Errors = append(result.Errors, interfaces.ActionError{
                Resource: action.Resource.Name, Action: action.Action, Error: err.Error(),
            })
        }
    }
    return result, nil
}

func dispatchAction(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
    switch action.Action {
    case "create":
        return doCreate(ctx, d, action, result)
    case "update":
        return doUpdate(ctx, d, action, result)
    case "replace":
        return doReplace(ctx, d, action, result)
    case "delete":
        return doDelete(ctx, d, action)
    default:
        return fmt.Errorf("unknown action %q", action.Action)
    }
}

// doCreate, doUpdate, doReplace, doDelete — see subsequent tasks
```

**Step 4: Run test to verify pass (skeleton-only — body in next tasks)**

For now, write minimal stubs in doCreate/doUpdate/doReplace/doDelete that just call the driver method and append to result.

**Step 5: Commit**

```bash
git add iac/wfctlhelpers/
git commit -m "feat(iac): add wfctlhelpers.ApplyPlan skeleton (4-action dispatch)"
```

### Task T3.2: Implement `doCreate` with `upsertSupporter` recovery

**Files:**
- Modify: `iac/wfctlhelpers/apply.go`
- Modify: `interfaces/iac_resource_driver.go` (add `UpsertSupporter` interface)
- Test: `iac/wfctlhelpers/apply_create_test.go`

**Step 1: Write failing test**

```go
func TestApplyPlan_Create_UpsertOnAlreadyExists(t *testing.T) {
    fakeDriver := &fakeDriverWithUpsert{
        createErr: interfaces.ErrResourceAlreadyExists,
        readResult: &interfaces.ResourceOutput{ProviderID: "found-uuid"},
    }
    fp := &fakeProvider{driver: fakeDriver}
    plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
        {Action: "create", Resource: spec("a", "infra.vpc")},
    }}
    result, _ := ApplyPlan(context.Background(), fp, plan)
    if len(result.Errors) > 0 {
        t.Errorf("upsert should recover; got errors: %v", result.Errors)
    }
    if !fakeDriver.updateCalled {
        t.Errorf("upsert path should call Update after Read; updateCalled=%v", fakeDriver.updateCalled)
    }
}
```

**Step 2: Run test to verify failure**

**Step 3: Implement**

In `interfaces/iac_resource_driver.go` add:
```go
// UpsertSupporter is an optional interface implemented by ResourceDrivers
// that support locating a resource by name alone (empty ProviderID) in their
// Read method. Used by wfctlhelpers.ApplyPlan to recover from
// ErrResourceAlreadyExists during Create by Reading + Updating instead.
type UpsertSupporter interface {
    SupportsUpsert() bool
}
```

In `iac/wfctlhelpers/apply.go`:
```go
func doCreate(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
    out, err := d.Create(ctx, action.Resource)
    if errors.Is(err, interfaces.ErrResourceAlreadyExists) {
        us, ok := d.(interfaces.UpsertSupporter)
        if !ok || !us.SupportsUpsert() {
            return err // no recovery available
        }
        ref := interfaces.ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
        existing, readErr := d.Read(ctx, ref)
        if readErr != nil {
            return fmt.Errorf("upsert: read after conflict: %w", errors.Join(err, readErr))
        }
        if existing.ProviderID == "" {
            return fmt.Errorf("upsert: resource %q found by name but ProviderID is empty: %w", ref.Name, err)
        }
        ref.ProviderID = existing.ProviderID
        out, err = d.Update(ctx, ref, action.Resource)
    }
    if err == nil && out != nil {
        result.Resources = append(result.Resources, *out)
    }
    return err
}
```

**Step 4: Run test to verify pass**

**Step 5: Commit**

```bash
git add iac/wfctlhelpers/apply.go iac/wfctlhelpers/apply_create_test.go interfaces/iac_resource_driver.go
git commit -m "feat(iac): doCreate honors UpsertSupporter for ErrResourceAlreadyExists recovery"
```

### Task T3.3: Implement `doUpdate` + `doDelete`

**Files:**
- Modify: `iac/wfctlhelpers/apply.go`
- Test: `iac/wfctlhelpers/apply_update_delete_test.go`

**Step 1-5:** TDD per template. Tests:
- `TestApplyPlan_Update_PassesProviderID`
- `TestApplyPlan_Delete_InvokesDriverDelete` (the latent bug fix — verify driver.Delete is called, not skipped)

Commit: `feat(iac): doUpdate + doDelete actions`

### Task T3.4: Implement `doReplace` with replaceIDMap propagation

**Files:**
- Modify: `iac/wfctlhelpers/apply.go`
- Test: `iac/wfctlhelpers/apply_replace_test.go`

**Step 1: Write failing test**

```go
func TestApplyPlan_Replace_DeletesThenCreates_PropagatesNewID(t *testing.T) {
    var deleteCalled, createCalled bool
    fakeDriver := &fakeDriver{
        deleteFn: func(ref interfaces.ResourceRef) error {
            deleteCalled = true
            return nil
        },
        createFn: func(spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
            createCalled = true
            if !deleteCalled { t.Errorf("create should run AFTER delete") }
            return &interfaces.ResourceOutput{ProviderID: "new-uuid"}, nil
        },
    }
    fp := &fakeProvider{driver: fakeDriver}
    plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
        {Action: "replace", Resource: spec("a", "infra.vpc"), Current: stateWithID("a", "old-uuid")},
    }}
    result, _ := ApplyPlan(context.Background(), fp, plan)
    if !deleteCalled || !createCalled {
        t.Errorf("replace should call Delete + Create; delete=%v create=%v", deleteCalled, createCalled)
    }
    if len(result.Resources) != 1 || result.Resources[0].ProviderID != "new-uuid" {
        t.Errorf("expected new ProviderID in result, got %+v", result.Resources)
    }
}
```

**Step 2-5:** Implement `doReplace`:
1. Call `Delete(ctx, refFromCurrent(action))`
2. On success, call `Create(ctx, action.Resource)`
3. On Create success, write `result.replaceIDMap[action.Resource.Name] = newOutput.ProviderID` (will be threaded through to dependent Apply actions in W-5)

For now (this PR), `replaceIDMap` is part of `ApplyResult` but not yet read by anything. W-5 wires the consumer.

Commit: `feat(iac): doReplace deletes-then-creates with ProviderID propagation hook`

### Task T3.5: Add diff cache at `iac/diffcache/` keyed by `(plugin-version, type, providerID, sha-config, sha-outputs)`

**Files:**
- Create: `iac/diffcache/cache.go`
- Test: `iac/diffcache/cache_test.go`

**Step 1-5:** TDD. Cache backed by `~/.cache/wfctl/diff/` JSON files keyed by SHA-256 of the tuple. `Get(key) (DiffResult, bool)` + `Put(key, DiffResult)`. Cache miss returns `(_, false)`.

Commit: `feat(iac): add diff cache to amortize plan-time gRPC roundtrips`

### Task T3.6: Refactor `platform.ComputePlan` to call provider.Diff per resource (with concurrency + cache)

**Files:**
- Modify: `platform/differ.go`
- Test: `platform/differ_replace_test.go`

**Step 1: Write failing test**

```go
func TestComputePlan_NeedsReplaceEmitsReplaceAction(t *testing.T) {
    desired := []interfaces.ResourceSpec{
        {Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
    }
    current := []interfaces.ResourceState{
        {Name: "vpc", Type: "infra.vpc", ProviderID: "old", AppliedConfig: map[string]any{"region": "nyc1"}},
    }
    fp := &fakeProvider{
        diffResult: &interfaces.DiffResult{
            NeedsReplace: true,
            Changes: []interfaces.FieldChange{
                {Path: "region", Old: "nyc1", New: "nyc3", ForceNew: true},
            },
        },
    }
    plan, err := ComputePlan(context.Background(), fp, desired, current)
    if err != nil { t.Fatal(err) }
    if len(plan.Actions) != 1 || plan.Actions[0].Action != "replace" {
        t.Errorf("expected replace action, got %+v", plan.Actions)
    }
}

func TestComputePlan_ForceNewWithoutNeedsReplace_StillEmitsReplace(t *testing.T) {
    fp := &fakeProvider{diffResult: &interfaces.DiffResult{
        NeedsUpdate: true, // !NeedsReplace, but ForceNew set
        Changes: []interfaces.FieldChange{{Path: "region", ForceNew: true}},
    }}
    plan, _ := ComputePlan(context.Background(), fp, /*desired*/, /*current*/)
    if plan.Actions[0].Action != "replace" {
        t.Errorf("ForceNew should imply replace; got %s", plan.Actions[0].Action)
    }
}
```

**Step 2: Run test to verify failure**

**Step 3: Implement**

Update `ComputePlan` signature: `func ComputePlan(ctx context.Context, p IaCProvider, desired []ResourceSpec, current []ResourceState) (IaCPlan, error)`.

For each existing resource (not new), call `p.ResourceDriver(spec.Type).Diff(ctx, spec, currentOut)` (with optional cache hit). Emit:
- `Action="replace"` if `diff.NeedsReplace || hasForceNew(diff.Changes)`
- `Action="update"` if `diff.NeedsUpdate && !replace`
- skip if neither

Use `golang.org/x/sync/errgroup` for bounded concurrency (default 8 from `WFCTL_PLAN_DIFF_CONCURRENCY`).

Update all callers of `ComputePlan` to thread the provider.

**Step 4: Run test to verify pass**

**Step 5: Commit**

```bash
git add platform/differ.go platform/differ_replace_test.go cmd/wfctl/...
git commit -m "feat(iac): ComputePlan calls Diff per resource; emits replace action"
```

### Task T3.7: Wire `cmd/wfctl/infra_apply.go` to use `wfctlhelpers.ApplyPlan` for v2 plugins

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (the apply path)
- Test: `cmd/wfctl/infra_apply_v2_test.go`

**Step 1-5:** Add a branch: if `provider.computePlanVersion == "v2"` (read from manifest — implemented in W-9; for this task, accept any provider via a placeholder check that returns true), call `wfctlhelpers.ApplyPlan(ctx, provider, plan)`. Otherwise call `provider.Apply(ctx, plan)` (legacy path).

For this PR, the v2 check is `os.Getenv("WFCTL_USE_V2_APPLY") != ""` (placeholder). W-9 replaces with manifest read.

Commit: `feat(iac): apply path branches between v1 (provider.Apply) + v2 (wfctlhelpers.ApplyPlan)`

### Task T3.8: Conformance scenarios (stubs filled by W-7)

**Files:**
- Create stubs: `iac/conformance/scenario_replace.go`, `scenario_delete.go`, `scenario_diff_grpc.go`, `scenario_upsert.go`

**Step 1-5:** Each is a `func Scenario_X(t *testing.T, cfg Config) { t.Skip("filled in W-7") }`. Commit: `chore(conformance): stub W-3 scenarios`

### Task T3.9: Runtime-launch-validation

**Files:** none new

**Step 1:** Build wfctl: `go build -o /tmp/wfctl ./cmd/wfctl`
**Step 2:** Build a stub provider plugin in `internal/testdata/stub-provider/` that exercises Diff (test-only).
**Step 3:** Run `WFCTL_USE_V2_APPLY=1 /tmp/wfctl infra apply --plan <fake-plan>` against the stub; expect Replace action invokes Delete+Create on the stub.

**Rollback (T3.9):** unset `WFCTL_USE_V2_APPLY`. Reverting commits removes the v2 branch entirely.

### Task T3.10: PR description "Bugs incidentally fixed"

**Files:** PR description text (not in repo)

When opening the PR, body includes:
```
## Bugs incidentally fixed by this PR

1. **Delete-via-Apply state leakage** — Today: ComputePlan emits delete actions but DOProvider.Apply has no `case "delete"` (falls through to `default: unknown action`). wfctl prunes state regardless. Result: cloud resource leaks. This PR adds `case "delete"` to wfctlhelpers.ApplyPlan, fixing the leakage. Operators relying on the (broken) skip behavior may see different outcomes.

2. **ForceNew silently downgraded to Update** (issue C from design) — fixed by ComputePlan emitting `replace` when `NeedsReplace=true` or any `FieldChange.ForceNew=true`.
```

Commit (final PR commit): `docs(pr): note bugs incidentally fixed by W-3`

**Verification (PR W-3):**
- `go test ./iac/wfctlhelpers/... ./iac/diffcache/... ./platform/... ./cmd/wfctl/... -count=1 -race`
- Manual: opt into `WFCTL_USE_V2_APPLY=1`, run apply against test fixture; confirm replace action behavior.

**Rollback (PR W-3):** revert commits. v1 plugins (default, until W-9 adds `computePlanVersion`) take the legacy path; no behavior change for un-migrated providers.

---

## PR W-4: Provider.ValidatePlan + R-A10 align rule

**Goal:** Provider-side cross-resource constraint validation surfaces at plan time, not at API call time.

### Task T4.1: Add `ValidatePlan` to IaCProvider interface (optional)

**Files:**
- Modify: `interfaces/iac_provider.go`
- Test: `interfaces/iac_provider_test.go`

**Step 1-5:** TDD. Define optional interface:

```go
type ProviderValidator interface {
    ValidatePlan(plan *IaCPlan) []Diagnostic
}

type Diagnostic struct {
    Severity DiagnosticSeverity // Error | Warning | Info
    Resource string             // resource name (or empty for plan-level)
    Field    string             // field path (e.g. "vpc_ref")
    Message  string
}

type DiagnosticSeverity int
const (
    DiagnosticInfo DiagnosticSeverity = iota
    DiagnosticWarning
    DiagnosticError
)
```

NOT required on `IaCProvider`; consumers use type-assertion.

Commit: `feat(iac): add ProviderValidator optional interface + Diagnostic type`

### Task T4.2: Implement R-A10 align rule

**Files:**
- Modify: `cmd/wfctl/infra_align_rules.go` (add `checkRA10_provider_validate_plan`)
- Modify: `cmd/wfctl/infra_align.go` (dispatch)
- Test: `cmd/wfctl/infra_align_ra10_test.go`

**Step 1-5:** TDD. Rule iterates providers; type-asserts `ProviderValidator`; calls `ValidatePlan(plan)`; surfaces each Diagnostic as an AlignFinding (Error → FAIL strict).

Commit: `feat(iac): R-A10 align rule — provider.ValidatePlan dispatch`

### Task T4.3: Conformance scenario stub `Scenario_CrossResourceConstraintRejection`

Same template as T2.4. Commit: `chore(conformance): stub Scenario_CrossResourceConstraintRejection`

### Task T4.4: Documentation

**Files:** Modify `docs/WFCTL.md` (R-A10 entry); modify `DOCUMENTATION.md` (ProviderValidator section).

**Step 1-5:** Doc-only. Commit: `docs(iac): document ProviderValidator + R-A10 align rule`

### Task T4.5: Verification

**Files:** none

**Step 1:** Run `go test ./interfaces/... ./cmd/wfctl/...`. Expected: PASS.
**Step 2:** Build wfctl. Run `wfctl infra align --help`; expect existing help.
**Step 3:** Set up a fixture provider implementing `ProviderValidator` returning a fatal Diagnostic; run `wfctl infra align --strict`; expect non-zero exit + diagnostic in output.

**Verification (PR W-4):** all tests pass; manual rule-trigger smoke.

**Rollback (PR W-4):** revert commits. ProviderValidator is optional; no provider implements it pre-PR; no behavior change.

---

## PR W-5: Per-module JIT secret resolution + ProviderID propagation

**Goal:** Downstream modules see upstream outputs at apply time. ProviderID from W-3 cascade Replace propagates into still-referencing resources.

### Task T5.1: Define resolveJITSubstitutions helper

**Files:**
- Create: `iac/jitsubst/jitsubst.go`
- Test: `iac/jitsubst/jitsubst_test.go`

**Step 1-5:** TDD. Function signature:

```go
func ResolveSpec(spec ResourceSpec, replaceIDMap map[string]string, syncedOutputs map[string]map[string]any, envLookup func(string)(string,bool)) (ResourceSpec, error)
```

Resolves `${VAR}` against env, `${MODULE.field}` against syncedOutputs, `${MODULE.id}` against replaceIDMap.

Commit: `feat(iac): jitsubst.ResolveSpec for per-module deferred substitution`

### Task T5.2: Wire JIT substitution into wfctlhelpers.ApplyPlan

**Files:**
- Modify: `iac/wfctlhelpers/apply.go`
- Test: `iac/wfctlhelpers/apply_jit_test.go`

**Step 1-5:** TDD scenario: 2-action plan (create A, create B); B's config references `${A.id}`. Verify B's Create receives substituted Config with A's actual ProviderID.

Commit: `feat(iac): ApplyPlan resolves JIT substitutions per action`

### Task T5.3: Wire JIT substitution into Replace action

**Files:**
- Modify: `iac/wfctlhelpers/apply.go::doReplace`
- Test: `iac/wfctlhelpers/apply_replace_cascade_test.go`

**Step 1-5:** TDD scenario: cascade replace where dependent's `vpc_ref` references the replaced parent's id; verify dependent's Create gets the new ProviderID.

Commit: `feat(iac): ApplyPlan replace cascade propagates new ProviderID`

### Task T5.4: Bump plan SchemaVersion to 2 when plan requires JIT

**Files:**
- Modify: `cmd/wfctl/infra.go::runInfraPlan` (or wherever plan is built)
- Test: `cmd/wfctl/infra_plan_schema_test.go`

**Step 1-5:** TDD. If any module's config contains `${MODULE.field}` references (not just `${VAR}` env-vars), set `plan.SchemaVersion = 2`. Older wfctl loading the plan rejects with clear message.

Commit: `feat(iac): plan SchemaVersion=2 when JIT substitution required`

### Task T5.5: Reject persisted JIT-style plans at `wfctl infra plan -o file` time

**Files:**
- Modify: `cmd/wfctl/infra.go::runInfraPlan`
- Test: per task

**Step 1-5:** When plan would have `SchemaVersion=2` AND output destination is a file path (`-o`), error with: `"this plan requires JIT resolution; persisted plan.json is not supported. Run 'wfctl infra apply' directly without -o/--plan."` Stdout-only is fine (operator can preview).

Commit: `feat(iac): reject persisted JIT-style plans (canonical path is apply-without-plan)`

### Task T5.6: Conformance scenario stub `Scenario_InfraOutputCrossModuleResolution`

Same template. Commit: `chore(conformance): stub Scenario_InfraOutputCrossModuleResolution`

### Task T5.7: Runtime-launch-validation

**Files:** none

**Step 1:** Build wfctl.
**Step 2:** Run apply against fixture with `${A.id}` reference; verify success + log shows JIT resolution.
**Step 3:** Run `wfctl infra plan -o /tmp/p.json` against JIT-required config; expect error mentioning canonical path.

**Rollback (PR W-5):** revert commits. SchemaVersion stays at 1 for plans that don't use `${MODULE.field}` patterns; existing flows unchanged.

---

## PR W-6: --allow-replace flag + partial-cascade discovery

**Goal:** Per-resource opt-in for protected-resource Replace. Plan emits ALL blockers in one pass with copy-paste flag value.

### Task T6.1: Add `--allow-replace` flag to apply

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (flag registration + check)
- Test: `cmd/wfctl/infra_apply_allow_replace_test.go`

**Step 1-5:** TDD. Flag is comma-separated list (e.g. `--allow-replace=vpc-1,vpc-2`). Apply checks each replace/delete action against allow-list; protected resource not in list → error.

Commit: `feat(iac): --allow-replace flag for per-resource protected-replace opt-in`

### Task T6.2: Batch-discover protected blockers in ComputePlan

**Files:**
- Modify: `platform/differ.go::ComputePlan` (or apply-time check; pick one — apply is cleaner since plan output already shows all actions)
- Modify: `cmd/wfctl/infra_apply.go`
- Test: `cmd/wfctl/infra_apply_batch_blockers_test.go`

**Step 1-5:** TDD. Plan with 5 protected resources requiring replace → error message lists ALL 5 + pre-formatted `--allow-replace=name1,name2,name3,name4,name5` for copy-paste.

Commit: `feat(iac): apply batch-reports protected-replace blockers with copy-paste flag`

### Task T6.3: Conformance scenario stubs

**Files:** Stubs for `Scenario_ProtectedReplaceWithoutOverride`, `Scenario_ProtectedReplaceWithOverride`. Commit: `chore(conformance): stub W-6 scenarios`

### Task T6.4: Documentation + verification

Update `docs/WFCTL.md` flag section. Run tests.

Commit: `docs(wfctl): document --allow-replace flag`

**Rollback (PR W-6):** revert commits. Flag is opt-in; absence preserves existing behavior.

---

## PR W-7: iac/conformance/ package + DO smoke gate

**Goal:** Public conformance suite + per-PR smoke gate for active providers (DO).

### Task T7.1: Define `conformance.Run(t, Config)` entry point

**Files:**
- Create: `iac/conformance/scenarios.go`
- Test: `iac/conformance/scenarios_test.go`

**Step 1-5:** TDD using a fake provider that satisfies all scenarios. Define:

```go
type Config struct {
    Provider func() interfaces.IaCProvider
    SkipScenarios map[string]string
    SmokeOnly bool
    LiveCloud bool
}
func Run(t *testing.T, cfg Config) {
    scenarios := allScenarios()
    for _, s := range scenarios {
        if cfg.SmokeOnly && !s.Smoke { continue }
        if !cfg.LiveCloud && s.RequiresCloud { continue }
        if reason, ok := cfg.SkipScenarios[s.Name]; ok { t.Skipf("skipped: %s", reason); continue }
        t.Run(s.Name, func(t *testing.T) { s.Run(t, cfg) })
    }
}
```

Commit: `feat(conformance): add iac/conformance scenarios.Run entry point`

### Task T7.2-T7.12: Implement each scenario

11 scenarios, one per task. Each fills the body of a stub created in earlier W-tasks. Format per scenario:

**Scenarios** (Smoke=true means runs on every PR for active providers):

| Scenario | Smoke | RequiresCloud | Tests |
|---|---|---|---|
| `Scenario_NeedsReplaceTriggersReplaceAction` | yes (DO) | yes | NeedsReplace=true → action=replace |
| `Scenario_DeleteActionInApplyInvokesDriverDelete` | no | yes | delete action invokes driver.Delete |
| `Scenario_DiffSurvivesGRPCRoundTrip` | no | no (uses remoteResourceDriver wrapper + in-memory grpc) | typed Outputs survive structpb roundtrip |
| `Scenario_OutputsRefreshDetectsNewFields` | no | yes | refresh-outputs picks up new field |
| `Scenario_PlanStaleDiagnostic` | no | no | error names changed env-var key |
| `Scenario_CrossResourceConstraintRejection` | no | no | provider's ValidatePlan diagnostic surfaces in align |
| `Scenario_InfraOutputCrossModuleResolution` | no | yes (smoke variant: mock-only) | Module B sees A's output during own create |
| `Scenario_ProtectedReplaceWithoutOverride` | no | no | plan FAILS with hint |
| `Scenario_ProtectedReplaceWithOverride` | no | yes | plan SUCCEEDS with --allow-replace |
| `Scenario_OutputsConsistencyAcrossReadCycles` | no | yes | Read-after-Read returns identical Outputs |
| `Scenario_ReplaceCascadePreservesDependents` | no | yes | dependent gets new parent ID |
| `Scenario_UpsertOnAlreadyExists` | no | yes | upsertSupporter recovery works |

For each scenario:
1. Test: in-tree self-test using fake provider
2. Implementation: black-box assertion against `cfg.Provider()`
3. Commit: `feat(conformance): scenario_<name>`

### Task T7.13: DO smoke gate CI workflow

**Files:**
- Create: `.github/workflows/conformance-smoke.yml`
- Reference: workflow-plugin-digitalocean has its own CI; this workflow runs in workflow repo against DO plugin source

**Step 1-5:** Smoke gate runs on PRs touching `iac/`, `platform/`, or `cmd/wfctl/infra*`. Spins up DO Droplet (s-1vcpu-512mb-10gb), runs `Scenario_NeedsReplaceTriggersReplaceAction`, t.Cleanup force-deletes resource. Cost cap: ≤$0.01/PR/DO. Per-PR ephemeral OIDC creds (DO API token via GitHub OIDC if available; else short-lived secret).

Commit: `ci(conformance): DO smoke gate runs Scenario_NeedsReplaceTriggersReplaceAction per PR`

**Verification (PR W-7):** `go test ./iac/conformance/... -count=1 -race` (in-tree self-tests pass against fake provider). Manual: trigger CI workflow on a draft PR; verify smoke gate runs + cleans up.

**Rollback (PR W-7):** revert commits. iac/conformance is import-only; no provider imports it pre-W-7. CI workflow is new file; deleting unblocks.

---

## PR W-8: cmd/iac-codemod/ tooling

**Goal:** AST-based codemod for refactor-plan, refactor-apply, add-validate-plan, lint. Dry-run default; informative reports describe non-canonical idioms + suggested handling.

### Task T8.1: Skeleton `cmd/iac-codemod/main.go`

**Files:**
- Create: `cmd/iac-codemod/main.go`

**Step 1-5:** Subcommand dispatcher with `refactor-plan`, `refactor-apply`, `add-validate-plan`, `lint` modes. `-dry-run` flag default true; `-fix` opts into mutation.

Commit: `feat(codemod): scaffold cmd/iac-codemod with 4-mode subcommand dispatcher`

### Task T8.2: Implement `lint` mode

**Files:**
- Create: `cmd/iac-codemod/lint.go`
- Test: `cmd/iac-codemod/lint_test.go`

**Step 1-5:** AST-based static checks:
- `AssertPlanDelegatesToHelper`
- `AssertApplyDelegatesToHelper`
- `AssertDiffSetsNeedsReplaceForForceNew`
- `AssertProviderImplementsValidatePlan`

Use `golang.org/x/tools/go/analysis/passes` framework.

Commit: `feat(codemod): lint mode with 4 static-check assertions`

### Task T8.3: Implement `refactor-plan` mode

**Files:**
- Create: `cmd/iac-codemod/refactor_plan.go`
- Test: `cmd/iac-codemod/refactor_plan_test.go` (golden-file)

**Step 1-5:** Detects `func (p *XProvider) Plan(...)` body matching configHash compare pattern; replaces with `return wfctlhelpers.Plan(ctx, p, desired, current)`. Aborts (with informative report) if body has out-of-template logic. Golden-file test: input source → expected output diff.

Commit: `feat(codemod): refactor-plan mode (canonical pattern detection + rewrite)`

### Task T8.4: Implement `refactor-apply` mode (with informative reports)

**Files:**
- Create: `cmd/iac-codemod/refactor_apply.go`
- Test: `cmd/iac-codemod/refactor_apply_test.go`

**Step 1-5:** Detects switch-on-action in Apply. For canonical patterns: rewrites to `return wfctlhelpers.ApplyPlan(ctx, p, plan)`. For non-canonical idioms (descriptions in design §W-8):
- DO upsert recovery → emit suggested wfctlhelpers.upsertSupporter hook patch
- AWS update+replace collapse → emit "manual port required" finding with line numbers
- Custom error wrapping → emit extension-point hook + sample patch

Output `codemod-report.md` with per-file findings + suggested handling.

Commit: `feat(codemod): refactor-apply with informative non-canonical idiom reports`

### Task T8.5: Implement `add-validate-plan` mode

**Files:**
- Create: `cmd/iac-codemod/add_validate_plan.go`
- Test: golden-file

**Step 1-5:** Detect providers missing `ValidatePlan`; insert no-op stub. Skip if marker `// wfctl:skip-codemod` present.

Commit: `feat(codemod): add-validate-plan mode (no-op stub injection)`

### Task T8.6: Workspace migration runner Makefile

**Files:**
- Modify: `Makefile`

**Step 1-5:** Add `migrate-providers` target as in design §W-8. Include `lint -dry-run` against AWS/GCP/Azure (advisory-only).

Commit: `chore(make): add migrate-providers target for workspace-wide codemod`

### Task T8.7: Verification

**Files:** none

**Step 1:** Build `go build -o /tmp/iac-codemod ./cmd/iac-codemod`
**Step 2:** Run `/tmp/iac-codemod lint -dry-run /Users/jon/workspace/workflow-plugin-digitalocean/`. Expect: report listing DO plugin's current state.
**Step 3:** Run `/tmp/iac-codemod refactor-apply -dry-run /Users/jon/workspace/workflow-plugin-digitalocean/`. Expect: report identifying upsert recovery as non-canonical with suggested handling.

**Verification (PR W-8):** all tests pass; manual run produces expected report shape.

**Rollback (PR W-8):** revert commits. New binary, no consumers; deletion is clean.

---

## PR W-9: plugin.json computePlanVersion + ProviderPlanner interface

**Goal:** Per-plugin opt-in for the v2 ComputePlan + ApplyPlan path. Optional ProviderPlanner for plugins needing custom Plan even at v2.

### Task T9.1: Add `iacProvider.computePlanVersion` to plugin.json schema

**Files:**
- Modify: `schema/plugin_manifest.json` (or wherever plugin.json schema lives)
- Test: schema-validation test

**Step 1-5:** Field type: enum `["v1", "v2"]`, default `"v1"` if unset. Doc: "Which IaC compute-plan contract this plugin satisfies. v2 = wfctl uses platform.ComputePlan + driver.Diff dispatch. v1 = wfctl calls provider.Plan() per legacy."

Commit: `feat(plugin): plugin.json iacProvider.computePlanVersion field`

### Task T9.2: Add `ProviderPlanner` optional interface

**Files:**
- Modify: `interfaces/iac_provider.go`
- Test: `interfaces/iac_provider_planner_test.go`

**Step 1-5:** Define optional interface as in design §W-9. Test: mock provider implementing it; verify type-assertion works.

Commit: `feat(iac): add optional ProviderPlanner interface`

### Task T9.3: Replace placeholder check in apply with manifest read

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (the `WFCTL_USE_V2_APPLY` placeholder from T3.7)
- Test: `cmd/wfctl/infra_apply_v2_manifest_test.go`

**Step 1-5:** Read `iacProvider.computePlanVersion` from loaded plugin manifest; route accordingly. Remove env-var placeholder.

Commit: `feat(iac): apply path reads computePlanVersion from plugin manifest`

### Task T9.4: Documentation

**Files:** Modify `DOCUMENTATION.md` (computePlanVersion + ProviderPlanner section).

Commit: `docs(iac): document computePlanVersion + ProviderPlanner`

**Verification (PR W-9):** all tests pass.

**Rollback (PR W-9):** revert commits. Plugins without the field default to v1 (legacy path); manual revert preserves v1 behavior for any plugin that opted into v2.

---

## PR P-DO: DigitalOcean plugin migration to v2

**Goal:** DO plugin opts into computePlanVersion: v2; runs codemod; hand-ports upsert recovery to wfctlhelpers.ApplyPlan upsertSupporter hook; implements ValidatePlan for DO region constraints; adds conformance test.

### Task TP1: Run codemod against DO + review report

**Files:** none yet (review-only)

**Step 1:** From `/Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/feat-iac-v2-migration/`:
```bash
go run github.com/GoCodeAlone/workflow/cmd/iac-codemod refactor-apply -dry-run . > codemod-report.md
```
**Step 2:** Review `codemod-report.md`. Expect: report identifies upsert recovery as non-canonical + suggests upsertSupporter hook patch.
**Step 3:** Commit the report for posterity:
```bash
git add codemod-report.md
git commit -m "chore(migration): codemod report for DO v2 opt-in"
```

### Task TP2: Hand-port upsert recovery to upsertSupporter hook

**Files:**
- Modify: `internal/provider.go` (collapse Apply to wfctlhelpers.ApplyPlan call)
- Verify: drivers that need upsert recovery (VPC, Database, Firewall) implement `SupportsUpsert() bool`

**Step 1: Write failing test**

```go
// internal/provider_v2_test.go
func TestApply_DelegatesToWfctlhelpers(t *testing.T) {
    p := NewDOProvider()
    plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
        {Action: "create", Resource: spec("vpc-1", "infra.vpc")},
    }}
    // With a mock client that returns ErrResourceAlreadyExists then a Read
    // hit, expect ApplyPlan recovers via upsertSupporter
    result, _ := p.Apply(context.Background(), plan)
    if len(result.Errors) > 0 {
        t.Errorf("expected upsert recovery; got errors: %v", result.Errors)
    }
}
```

**Step 2-5:** Replace `DOProvider.Apply` body with `return wfctlhelpers.ApplyPlan(ctx, p, plan)`. Verify VPC/Database/Firewall drivers implement `SupportsUpsert()` (DO already has these per current code).

Commit: `refactor(provider): collapse Apply to wfctlhelpers.ApplyPlan`

### Task TP3: Implement `ValidatePlan` for DO region constraints

**Files:**
- Modify: `internal/provider.go` (add `ValidatePlan` method)
- Test: `internal/provider_validate_plan_test.go`

**Step 1-5:** TDD. Implement constraints:
- App Platform `region: nyc` → vpc_ref must reference VPC with `region: nyc1`
- App Platform `region: sfo` → vpc_ref must be `sfo2` or `sfo3`
- (other constraints discoverable from DO docs as additions)

Commit: `feat(provider): ValidatePlan for App Platform region-VPC constraints`

### Task TP4: Set `iacProvider.computePlanVersion: v2` in plugin.json

**Files:**
- Modify: `plugin.json`
- Test: existing manifest test

**Step 1-5:** Set field. Verify manifest validates. Run wfctl-strict-contracts CI locally.

Commit: `feat(plugin): opt into computePlanVersion: v2`

### Task TP5: Add conformance test + bump version

**Files:**
- Create: `internal/provider_conformance_test.go`
- Modify: `plugin.json` (bump version), `CHANGELOG.md`

**Step 1-5:**
```go
//go:build conformance

package internal

import (
    "testing"
    "github.com/GoCodeAlone/workflow/iac/conformance"
)

func TestConformance(t *testing.T) {
    conformance.Run(t, conformance.Config{
        Provider: func() interfaces.IaCProvider { return NewDOProvider() },
        SmokeOnly: testing.Short(),
        LiveCloud: os.Getenv("CONFORMANCE_LIVE_CLOUD") == "1",
    })
}
```

Run smoke locally if creds present. Bump plugin.json version. Add CHANGELOG entry.

Commit: `feat(provider): add conformance test + bump to v0.10.0`

**Verification (PR P-DO):**
- Local: `GOWORK=off go test ./internal/... -count=1 -race`
- CI: smoke gate runs on this PR (gates on the new computePlanVersion: v2)
- Runtime: `wfctl infra apply` against a test DO config in nyc1 with the v2-tagged plugin; expect Replace action behavior surfaces correctly when triggered.

**Rollback (PR P-DO):** revert plugin.json `computePlanVersion` change → wfctl falls back to v1 (provider.Apply); revert ApplyPlan refactor → restore the legacy switch. Plugin re-released as v0.10.x → wfctl-lock bump in core-dump.

---

## PR C-1: core-dump staging-PG cutover to nyc1

**Goal:** Bump wfctl + plugin pins; revert tactical workarounds in deploy.yml; complete the staging-PG migration to nyc1 (the original blocker).

### Task TC1: Bump wfctl + plugin pins; revert tactical workarounds

**Files:**
- Modify: `.wfctl-lock.yaml` (plugin v0.10.0)
- Modify: `.github/workflows/deploy.yml` (revert Capture STAGING_VPC_UUID hack; remove --skip-refresh; revert Plan env-block STAGING_PG_PASSWORD addition)
- Modify: `infra.yaml` (restore secrets.generate STAGING_VPC_UUID via infra_output: core-dump-vpc.id; drop the urn-parsing capture)

**Step 1-5:** Per-file changes. Verify locally with `wfctl infra plan`. Commit: `fix(infra): cut over to wfctl v0.21.0 + plugin v0.10.0; revert tactical workarounds`

### Task TC2: Move staging infra to nyc1

**Files:**
- Modify: `infra.yaml` (region nyc3 → nyc1 for VPC, Volume, Droplet)
- Modify: `infra.yaml` (drop `protected: true` from VPC, Volume, Droplet, Firewall — temporarily, for cutover)

**Step 1: Apply with --allow-replace** (uses W-6's flag):
```bash
wfctl infra apply --env staging --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw -c infra.yaml
```
Expect: cascade replace; new resources in nyc1; App vpc_ref auto-updates via W-3+W-5 cascade.

**Step 2: Verify /healthz green**:
```bash
curl https://coredump-staging.ondigitalocean.app/healthz
```
Expect: 200.

**Step 3: Re-add `protected: true` post-cutover**

**Step 4: Final apply**:
```bash
wfctl infra apply --env staging -c infra.yaml
```
Expect: no plan changes (resources match desired); state-only update for `protected: true` annotation.

**Step 5: Commit**

```bash
git add infra.yaml
git commit -m "fix(infra): staging PG infra in nyc1; cascade replace via --allow-replace"
```

**Verification (PR C-1):**
- staging /healthz returns 200
- `wfctl infra apply` re-run shows zero diff
- DO console: VPC + Volume + Droplet visible in nyc1; old nyc3 resources gone

**Rollback (PR C-1):** revert .wfctl-lock.yaml + deploy.yml + infra.yaml in one commit. Re-apply will restore prior config (with re-cascade replace if state already moved). Mid-rollback: cloud resources may be in-flight; complete rollback may require manual cloud cleanup.

---

## Final verification (cross-PR)

After all 11 PRs merge:

1. **W-7 conformance suite green for DO** — `cd workflow-plugin-digitalocean && go test -tags=conformance ./...`
2. **C-1 staging /healthz green** — `curl https://coredump-staging.ondigitalocean.app/healthz` returns 200
3. **AWS/GCP/Azure plugins still build** — `cd workflow-plugin-aws && go build ./...` exit 0; same for gcp, azure
4. **wfctl regression suite** — `cd workflow && go test ./... -count=1 -race`
5. **codemod advisory reports filed** — issues open against AWS/GCP/Azure plugin repos with the lint-only output

Sequencing dependency graph (must merge in this order; alignment-check enforces):

```
W-1 → W-2 → W-3 → W-4
                 ↘
                  W-5 → W-6 → W-7 → W-8 → W-9 → P-DO → C-1
```

W-1..W-6 + W-8 + W-9 can be drafted + reviewed in parallel but merged in order. W-7 must merge after W-3..W-6 so all scenarios are testable. W-9 must merge before P-DO. P-DO must merge before C-1.
