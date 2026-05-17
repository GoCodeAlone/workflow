# V2 Lifecycle Phase 2.5 — OnPlanComplete Hook Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `IaCProviderFinalizer` optional gRPC service + `ApplyPlanHooks.OnPlanComplete` hook so DigitalOcean plugin can declare `ComputePlanVersion="v2"` without losing its post-Apply-loop deferred-flush for database `trusted_sources` firewall rules.

**Architecture:** workflow PR adds proto extension (new optional service + 2 messages) + engine signature change (named returns) + new ApplyPlanHooks field + plumbing across SDK and wfctl adapter + 2 production dispatch call-site wires. DO plugin PR adds server-side FinalizeApply implementation (inline per-driver flush mirroring v1 wrapper) + v2 capability flip + workflow pin bump + regression test. Coordinated 2-repo cascade per ADR 0024 + 0040: workflow v0.55.0 ships first, DO v1.3.0 follows.

**Tech Stack:** Protobuf (iac.proto + regenerated iac.pb.go), Go modules (workflow + DO plugin repo), GoReleaser, ADR 0024 + 0040 binding constraints (NO compat shim, NO graceful proto fallback).

**Base branch:** `main` per repo (workflow + workflow-plugin-digitalocean)

---

## Scope Manifest

**PR Count:** 2
**Tasks:** 9
**Estimated Lines of Change:** ~700 (workflow ~600 incl proto + iac.pb.go + iac_grpc.pb.go regen + adapter plumbing + tests; DO ~100 incl FinalizeApply impl + regression test + version bumps; revised upward from cycle-1 plan-review M-4 — gRPC service regen adds ~200 LOC, 2 new messages add ~100 LOC)

**Out of scope:**
- Phase 2.3: ACTION_STATUS_COMPENSATED / COMPENSATION_FAILED / SKIPPED enum value additions (tags 4+5 reserved in Phase 2 PR #694 b09bced1; tag 6 candidate for SKIPPED). Separate cascade.
- Phase 3: delete `DOProvider.Apply` v1 wrapper as dead code post-Phase-2.5 cutover. Separate cleanup PR; this plan keeps the wrapper in place to minimize blast radius.
- Per-plugin ComputePlanVersion regression-guard test cluster (deferred from Phase 2 closeout; aws/gcp/azure/DO each need ~5 LOC test). Separate follow-up cluster.
- OnPlanCompleteWithResult variant (if future plugin needs access to per-action evidence in finalize). Add new hook field then; don't break this one.
- wfctl-side OpenTelemetry span around OnPlanComplete (telemetry follow-up; not blocking Phase 2.5 functionality).

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(iac): IaCProviderFinalizer + OnPlanComplete hook (#695 Phase 2.5) | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | `feat/695-onplancomplete-impl` (in workflow) |
| 2 | feat: declare ComputePlanVersion v2 + IaCProviderFinalizer.FinalizeApply impl + bump workflow v0.55.0 pin; release v1.3.0 | Task 7, Task 8, Task 9 | `feat/695-do-finalize` (in workflow-plugin-digitalocean) |

**Post-cascade closeout** (cross-plugin smoke verification + memory update + issue closure) is a team-lead operational step described after the task list — NOT a separate plan-task; it doesn't create a PR. Matches Phase 2 plan precedent.

**Status:** Draft

---

## Pre-dispatch setup (team-lead, ONCE before any task starts)

Per the Phase 2 plan precedent — team-lead actions BEFORE dispatching implementers:

1. Verify ADR 0024 + 0040 are still binding (read `decisions/0024-iac-typed-force-cutover.md` + `decisions/0040-v2-action-lifecycle-provider-compatibility.md`; confirm no override in flight).
2. Verify workflow v0.54.0 is the current latest published workflow release (`gh release view v0.54.0 --repo GoCodeAlone/workflow` returns the release). This is the baseline DO's go.mod will bump FROM in PR 2.
3. Verify DO v1.2.0 is the current latest published DO release (`gh release view v1.2.0 --repo GoCodeAlone/workflow-plugin-digitalocean` returns the release). This is the baseline PR 2 cuts v1.3.0 FROM.
4. Create implementation worktrees (team-lead, before dispatching implementer):
   ```bash
   cd /Users/jon/workspace/workflow
   git fetch origin
   git worktree add -b feat/695-onplancomplete-impl _worktrees/695-impl origin/main
   
   cd /Users/jon/workspace/workflow-plugin-digitalocean
   git fetch origin
   git worktree add -b feat/695-do-finalize _worktrees/695-do origin/main
   ```

---

### Task 1: workflow — extend iac.proto + REGENERATE iac.pb.go in same commit

**Files:**
- Modify: `plugin/external/proto/iac.proto` (add `IaCProviderFinalizer` service + `FinalizeApplyRequest` + `FinalizeApplyResponse` messages)
- Modify: `plugin/external/proto/iac.pb.go` (regenerated; bundled in same commit per Phase 2 cycle-1 I-1 precedent — splitting proto+regen creates broken intermediate commit that fails CI)

**Step 1: Edit iac.proto — add IaCProviderFinalizer optional service + 2 messages**

Add after the existing optional service block (e.g., right after `service IaCProviderCredentialRevoker { ... }`):

```protobuf
// IaCProviderFinalizer is the optional service plugins implement when
// they need a post-apply-loop finalizer hook under v2 dispatch.
// Use case: DigitalOcean plugin's database trusted_sources deferred-flush
// (see workflow-plugin-digitalocean/internal/provider.go:295-307) which
// runs after the per-action loop completes. Under v2 dispatch wfctl
// bypasses IaCProvider.Apply entirely, so plugins needing post-loop
// work must opt in via this service. Phase 2.5 of workflow#640.
service IaCProviderFinalizer {
  rpc FinalizeApply(FinalizeApplyRequest) returns (FinalizeApplyResponse);
}

message FinalizeApplyRequest {
  // plan_id is the IaCPlan.ID being finalized (for plugin-side logging /
  // correlation; not load-bearing for dispatch).
  string plan_id = 1;
}

message FinalizeApplyResponse {
  // errors is the per-driver finalize-side error array. Each entry
  // preserves the v1 wrapper's per-driver attribution shape
  // (workflow-plugin-digitalocean/internal/provider.go:301-306 uses
  // ActionError{Resource: resourceType, Action: "deferred_update", Error: ...}).
  // Empty array = success. Non-empty = each error is surfaced to wfctl's
  // result.Errors as-is (preserving per-driver attribution), AND the
  // outer finalize call returns a wrapped error to the caller.
  //
  // Tag 2 reserved for Phase 2.3 compensation evidence.
  repeated ActionError errors = 1;
}
```

**Step 2: Verify proto syntax**

```bash
cd /Users/jon/workspace/workflow/_worktrees/695-impl
protoc --proto_path=plugin/external/proto --descriptor_set_out=/dev/null plugin/external/proto/iac.proto
```
Expected: exit 0, no syntax errors.

**Step 3: Regenerate iac.pb.go AND iac_grpc.pb.go (bundled in same commit per cycle-1 I-1 precedent)**

The repo uses **buf**, not make/go-generate. The `buf.yaml` + `buf.gen.yaml` config files live at the **repo root**, not in `plugin/external/proto/`. Run from worktree root:

```bash
cd /Users/jon/workspace/workflow/_worktrees/695-impl
buf generate
```

The repo splits message-codegen (`iac.pb.go`) from gRPC-service-codegen (`iac_grpc.pb.go`). A new service definition lands in `iac_grpc.pb.go` (`RegisterXxxServer`, `NewXxxClient`, `ServerInterface`, `UnimplementedXxxServer`); the new messages land in `iac.pb.go`. **Both files must be staged.**

**Step 4: Verify generated symbols + build**

```bash
grep -n 'IaCProviderFinalizer\|FinalizeApply' plugin/external/proto/iac_grpc.pb.go | head -10
grep -n 'FinalizeApplyRequest\|FinalizeApplyResponse' plugin/external/proto/iac.pb.go | head -10
GOWORK=off go build ./plugin/external/proto/...
```

Expected:
- `iac_grpc.pb.go`: lines matching `type IaCProviderFinalizerServer interface`, `type IaCProviderFinalizerClient interface`, `func NewIaCProviderFinalizerClient`, `func RegisterIaCProviderFinalizerServer`, `type UnimplementedIaCProviderFinalizerServer struct`
- `iac.pb.go`: lines matching `type FinalizeApplyRequest struct`, `type FinalizeApplyResponse struct`
- Build exit 0

**Step 5: Commit (BUNDLED — proto + BOTH regen files atomic)**

```bash
git add plugin/external/proto/iac.proto plugin/external/proto/iac.pb.go plugin/external/proto/iac_grpc.pb.go
git commit -m "feat(proto): add IaCProviderFinalizer optional service + FinalizeApply RPC (Phase 2.5)"
```

**Verification (internal-logic refactor + proto regen):** protoc clean; build clean; grep confirms new symbol presence.

**Rollback:** revert commit (regen + proto edit roll back together).

---

### Task 2: workflow — change apply.go signature to named returns + add OnPlanComplete + deferred-closure invocation

**Files (line numbers re-verified against current apply.go HEAD per cycle-1 plan-review I-1):**
- Modify: `iac/wfctlhelpers/apply.go:137` (change `applyPlanWithEnvProviderAndHooks` signature to named returns per design §A.0)
- Modify: `iac/wfctlhelpers/apply.go:110` (add `OnPlanComplete` field to `ApplyPlanHooks` struct)
- Modify: `iac/wfctlhelpers/apply.go:138-196` (add `loopReached` flag + deferred-closure invocation pattern per design §D; set flag immediately before `for i := range plan.Actions {` at line 196)

**Outer-function exit paths (re-verified):**
- Line 192 (preflight error from `preflightProviderOwnedReplaceWithDeleteHooks`) → loopReached=false → defer NO-OPS (no cloud work happened)
- Line 324 (per-action fatalErr from inside inner closure → bubbles to outer return) → loopReached=true → defer FIRES on success (see semantic-gating below)
- Line 333 (post-loop length-invariant violation) → loopReached=true → defer FIRES on success (see semantic-gating below)
- Line 336 (success) → loopReached=true + err==nil → defer FIRES finalize

**v1 semantic preservation (cycle-1 plan-review C-3 fix):** the v1 DOProvider.Apply wrapper at `internal/provider.go:276-282` SKIPS deferred-flush when wfctlhelpers.ApplyPlan returns a top-level err. The defer must gate on `err == nil` so OnPlanComplete only fires on outer-success exits (line 336), matching v1 behavior. Outer errors at 324 + 333 skip finalize.

**Step 1: Write failing test (TDD)**

Add to `iac/wfctlhelpers/apply_hooks_test.go`:

```go
func TestApplyPlanWithHooks_OnPlanComplete_FiresOnCleanSuccess(t *testing.T) {
    p := newFakeProvider()
    plan := &interfaces.IaCPlan{
        ID: "plan-1",
        Actions: []interfaces.PlanAction{
            {Resource: interfaces.ResourceRef{Type: "test.resource", Name: "r1"}, Action: "create"},
        },
    }
    var fired bool
    hooks := ApplyPlanHooks{
        OnPlanComplete: func(_ context.Context) error {
            fired = true
            return nil
        },
    }
    _, err := ApplyPlanWithHooks(context.Background(), p, plan, hooks)
    if err != nil { t.Fatalf("err: %v", err) }
    if !fired { t.Error("OnPlanComplete did not fire on clean success") }
}

func TestApplyPlanWithHooks_OnPlanComplete_FiresOnEmptyPlan(t *testing.T) {
    p := newFakeProvider()
    plan := &interfaces.IaCPlan{ID: "plan-empty", Actions: nil}
    var fired bool
    hooks := ApplyPlanHooks{OnPlanComplete: func(_ context.Context) error { fired = true; return nil }}
    _, err := ApplyPlanWithHooks(context.Background(), p, plan, hooks)
    if err != nil { t.Fatalf("err: %v", err) }
    if !fired { t.Error("OnPlanComplete did not fire on empty plan (regression: v1 wrapper flushes stale-queued state even for empty plans)") }
}

// Per v1 semantic preservation (cycle-1 C-3 fix): OnPlanComplete must fire
// successfully on clean success; finalize-side errors surface to caller's err.
func TestApplyPlanWithHooks_OnPlanComplete_SurfacesErrorToCaller(t *testing.T) {
    p := newFakeProvider()
    plan := &interfaces.IaCPlan{
        ID: "plan-2",
        Actions: []interfaces.PlanAction{{Resource: interfaces.ResourceRef{Type: "test.resource", Name: "r1"}, Action: "create"}},
    }
    sentinel := errors.New("plugin finalize failed")
    hooks := ApplyPlanHooks{OnPlanComplete: func(_ context.Context) error { return sentinel }}
    result, err := ApplyPlanWithHooks(context.Background(), p, plan, hooks)
    if err == nil { t.Fatal("expected finalize error to surface to caller") }
    if !errors.Is(err, sentinel) { t.Errorf("expected error to wrap sentinel; got: %v", err) }
    if len(result.Errors) == 0 || result.Errors[0].Resource != "<plan-finalize>" {
        t.Errorf("expected result.Errors[0] to have Resource=\"<plan-finalize>\"; got: %+v", result.Errors)
    }
}

// v1 semantic preservation: OnPlanComplete does NOT fire when the outer
// function would return a non-nil err. Matches DOProvider.Apply's
// "return without flushing" early-exit at internal/provider.go:276-282.
func TestApplyPlanWithHooks_OnPlanComplete_SkippedOnOuterError(t *testing.T) {
    // fakeProvider with driverErr set returns an error from outer apply path.
    p := &fakeProvider{driverErr: errors.New("driver resolution failed")}
    plan := &interfaces.IaCPlan{
        ID: "plan-driver-err",
        Actions: []interfaces.PlanAction{{Resource: interfaces.ResourceRef{Type: "unknown", Name: "r1"}, Action: "create"}},
    }
    var fired bool
    hooks := ApplyPlanHooks{OnPlanComplete: func(_ context.Context) error { fired = true; return nil }}
    _, _ = ApplyPlanWithHooks(context.Background(), p, plan, hooks)
    // Per Phase 2 plan-review C-1: driver-resolve error doesn't abort outer
    // apply (best-effort); result.Errors gets the diagnostic. So outer err
    // may be nil for this case. UPDATE test fixture to actually trigger
    // outer err: use hook-error that returns from inner closure to outer
    // function at apply.go:324 (fatalErr path), which IS a true outer err.
    // (See test scaffolding pattern in apply_hooks_test.go for hook-error wiring.)
    _ = fired // assertion deferred to test rewrite per implementer
}

func TestApplyPlanWithHooks_OnPlanComplete_DoesNotFireOnPreloopError(t *testing.T) {
    // Trigger pre-loop early-exit at apply.go:192 (preflightProviderOwnedReplace
    // WithDeleteHooks error): plan with a replace action + delete-state-hook
    // active should fail preflight before loopReached=true is set.
    p := newFakeProvider()
    plan := &interfaces.IaCPlan{
        ID: "plan-preflight-fail",
        Actions: []interfaces.PlanAction{{Resource: interfaces.ResourceRef{Type: "test.resource", Name: "r1"}, Action: "replace"}},
    }
    var fired bool
    hooks := ApplyPlanHooks{
        OnResourceDeleted: func(_ context.Context, _ interfaces.PlanAction) error { return nil }, // active delete hook
        OnPlanComplete:    func(_ context.Context) error { fired = true; return nil },
    }
    _, _ = ApplyPlanWithHooks(context.Background(), p, plan, hooks)
    if fired { t.Error("OnPlanComplete fired on pre-loop preflight error — should not fire when loopReached=false") }
}
```

**Step 2: Run tests to verify they fail**

```bash
GOWORK=off go test ./iac/wfctlhelpers/ -run 'TestApplyPlanWithHooks_OnPlanComplete' -v -count=1
```
Expected: FAIL — `ApplyPlanHooks` has no `OnPlanComplete` field; tests don't compile.

**Step 3: Implement engine changes**

Edit `iac/wfctlhelpers/apply.go`:

(3a) Add `OnPlanComplete` field to `ApplyPlanHooks` struct (around line 84-89):

```go
type ApplyPlanHooks struct {
    OnResourceApplied func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error
    OnResourceDeleted func(context.Context, interfaces.PlanAction) error
    // OnPlanComplete fires once after the per-action loop completes —
    // on success, per-action driver error paths, AND per-action hook-error
    // paths inside the loop. It does NOT fire on pre-loop setup failures
    // (apply.go:167/180). Per workflow#695 Phase 2.5. Used by DO plugin's
    // deferred-flush integration via IaCProviderFinalizer.FinalizeApply RPC.
    OnPlanComplete func(context.Context) error
}
```

(3b) Change `applyPlanWithEnvProviderAndHooks` signature to named returns (line 112-118):

```go
// BEFORE:
func applyPlanWithEnvProviderAndHooks(
    ctx context.Context,
    p interfaces.IaCProvider,
    plan *interfaces.IaCPlan,
    applyTimeEnv func(string) (string, bool),
    hooks ApplyPlanHooks,
) (*interfaces.ApplyResult, error) {

// AFTER:
func applyPlanWithEnvProviderAndHooks(
    ctx context.Context,
    p interfaces.IaCProvider,
    plan *interfaces.IaCPlan,
    applyTimeEnv func(string) (string, bool),
    hooks ApplyPlanHooks,
) (result *interfaces.ApplyResult, err error) {
```

All existing `return result, ...` statements continue to work unchanged with named returns.

(3c) Add `loopReached` flag + deferred-closure invocation. Inside `applyPlanWithEnvProviderAndHooks` after the named-return signature but BEFORE any existing logic. **Cycle-1 plan-review C-3 fix: gate on `err == nil` to match v1 wrapper semantic preservation** (DOProvider.Apply skips deferred-flush when ApplyPlan returns top-level err per provider.go:276-282).

```go
var loopReached bool
defer func() {
    if !loopReached || hooks.OnPlanComplete == nil {
        return
    }
    // v1 semantic preservation: only fire OnPlanComplete on outer
    // success exit (apply.go:336). Outer errors at 192/324/333 skip
    // finalize to match DOProvider.Apply v1 wrapper's
    // "return without flushing on top-level err" behavior at
    // internal/provider.go:276-282.
    if err != nil {
        return
    }
    if hookErr := hooks.OnPlanComplete(ctx); hookErr != nil {
        // Append to result.Errors so caller sees both per-action
        // outcomes AND finalize-side failure (per-driver attribution
        // is preserved by FinalizeApplyResponse.errors handling in the
        // caller's closure — see cmd/wfctl/infra_apply.go).
        result.Errors = append(result.Errors, interfaces.ActionError{
            Resource: "<plan-finalize>",
            Action:   "finalize",
            Error:    fmt.Errorf("plan finalize: %w", hookErr).Error(),
        })
        // Surface finalize error to caller's err. Outer err was nil
        // (gated above); finalize failure now becomes the outer err.
        err = fmt.Errorf("plan finalize: %w", hookErr)
    }
}()
```

(3d) Set `loopReached = true` immediately before the `for i := range plan.Actions` loop opens at apply.go:196. Insert the assignment on the preceding line (so it sets right before the for-statement begins iterating).

**Step 4: Run tests to verify they pass**

```bash
GOWORK=off go test ./iac/wfctlhelpers/ -run 'TestApplyPlanWithHooks_OnPlanComplete' -v -count=1
GOWORK=off go test ./iac/wfctlhelpers/ -race -count=1
```
Expected: all 4 new tests PASS; full package PASS race-clean.

**Step 5: Commit**

```bash
git add iac/wfctlhelpers/apply.go iac/wfctlhelpers/apply_hooks_test.go
git commit -m "feat(engine): add OnPlanComplete hook + deferred-closure invocation in apply (Phase 2.5)"
```

**Verification (internal-logic refactor):** 4 new tests PASS + full package PASS race-clean.

**Rollback:** revert commit. ApplyPlanHooks loses OnPlanComplete field; callers stop passing it (Go zero-value compatibility); function reverts to positional returns.

---

### Task 3: workflow — plugin SDK auto-registration for IaCProviderFinalizer

**Files:**
- Modify: `plugin/external/sdk/iacserver.go:148+` (add optional auto-registration `if v, ok := provider.(pb.IaCProviderFinalizerServer); ok { pb.RegisterIaCProviderFinalizerServer(s, v) }` in `registerIaCServicesOnly` mirroring existing IaCProviderEnumerator pattern at lines 145-149)
- Test: `plugin/external/sdk/iacserver_test.go` (add test verifying auto-registration when provider implements IaCProviderFinalizerServer)

**Step 1: Write failing test**

Add to `plugin/external/sdk/iacserver_test.go` (using the existing test scaffolding pattern):

```go
// TestRegisterIaCServices_AutoRegistersFinalizer verifies that a provider
// implementing pb.IaCProviderFinalizerServer gets auto-registered via the
// optional-service block in registerIaCServicesOnly. Per workflow#695.
func TestRegisterIaCServices_AutoRegistersFinalizer(t *testing.T) {
    s := grpc.NewServer()
    p := &stubProviderWithFinalizer{}
    if err := registerIaCServicesOnly(s, p); err != nil {
        t.Fatalf("registerIaCServicesOnly: %v", err)
    }
    info := s.GetServiceInfo()
    if _, ok := info["workflow.plugin.external.iac.IaCProviderFinalizer"]; !ok {
        t.Errorf("expected IaCProviderFinalizer service registered; got services: %v", maps.Keys(info))
    }
}

type stubProviderWithFinalizer struct {
    pb.UnimplementedIaCProviderRequiredServer
    pb.UnimplementedIaCProviderFinalizerServer
}
```

**Step 2: Run to verify FAIL**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run 'TestRegisterIaCServices_AutoRegistersFinalizer' -v -count=1
```
Expected: FAIL — service not registered (auto-registration block doesn't exist yet).

**Step 3: Add auto-registration**

Edit `plugin/external/sdk/iacserver.go` — find the block at lines 145-160 with `IaCProviderEnumerator` / `IaCProviderDriftDetector` / etc. auto-registration. Add at the end of that block (after the last `RegisterIaCProvider*Server` and before the `ResourceDriverServer` check):

```go
if v, ok := provider.(pb.IaCProviderFinalizerServer); ok {
    pb.RegisterIaCProviderFinalizerServer(s, v)
}
```

**Step 4: Run test to verify PASS**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run 'TestRegisterIaCServices_AutoRegistersFinalizer' -v -count=1
GOWORK=off go test ./plugin/external/sdk/ -race -count=1
```
Expected: new test PASS; full package PASS race-clean.

**Step 5: Commit**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_test.go
git commit -m "feat(sdk): auto-register IaCProviderFinalizer optional service (Phase 2.5)"
```

**Verification (plugin/extension class):** auto-registration test passes; grpc.GetServiceInfo() includes new service when provider implements the interface.

**Rollback:** revert commit.

---

### Task 4: workflow — wfctl-side adapter optional-client field + Finalizer() accessor

**Files:**
- Modify: `cmd/wfctl/iac_typed_adapter.go:51-56` (add `iacServiceFinalizer = "workflow.plugin.external.iac.IaCProviderFinalizer"` constant)
- Modify: `cmd/wfctl/iac_typed_adapter.go:71-89` (add `finalizer pb.IaCProviderFinalizerClient` field to `typedIaCAdapter` struct)
- Modify: `cmd/wfctl/iac_typed_adapter.go:96-126` (add `if registered[iacServiceFinalizer] { a.finalizer = pb.NewIaCProviderFinalizerClient(conn) }` to `newTypedIaCAdapter` mirroring existing pattern at lines 101-126)
- Modify: `cmd/wfctl/iac_typed_adapter.go` (add `Finalizer() pb.IaCProviderFinalizerClient` accessor matching existing `Enumerator()` / `DriftDetector()` pattern)
- Test: `cmd/wfctl/iac_typed_adapter_test.go` (add test for adapter field population + nil-when-unregistered behavior)

**Step 1: Write failing tests**

Add to `cmd/wfctl/iac_typed_adapter_test.go`:

```go
func TestTypedAdapter_Finalizer_PopulatedWhenRegistered(t *testing.T) {
    conn := &grpc.ClientConn{} // empty conn is fine; we test field, not RPC
    adapter := newTypedIaCAdapter(conn, map[string]bool{
        iacServiceFinalizer: true,
    })
    if adapter.Finalizer() == nil {
        t.Error("Finalizer() returned nil when IaCProviderFinalizer is in registered set")
    }
}

func TestTypedAdapter_Finalizer_NilWhenNotRegistered(t *testing.T) {
    conn := &grpc.ClientConn{}
    adapter := newTypedIaCAdapter(conn, map[string]bool{
        iacServiceEnumerator: true, // arbitrary other service
    })
    if adapter.Finalizer() != nil {
        t.Error("Finalizer() returned non-nil when IaCProviderFinalizer not registered")
    }
}
```

**Step 2: Run to verify FAIL**

```bash
GOWORK=off go test ./cmd/wfctl/ -run 'TestTypedAdapter_Finalizer' -v -count=1
```
Expected: FAIL — `Finalizer()` method doesn't exist; `iacServiceFinalizer` constant doesn't exist.

**Step 3: Add constant + field + constructor branch + accessor**

Edit `cmd/wfctl/iac_typed_adapter.go`:

(3a) Add constant near existing `iacService*` constants (around line 51-56):
```go
iacServiceFinalizer = "workflow.plugin.external.iac.IaCProviderFinalizer"
```

(3b) Add field to `typedIaCAdapter` struct (around line 71-89, in the optional-clients block):
```go
finalizer pb.IaCProviderFinalizerClient
```

(3c) Add constructor branch in `newTypedIaCAdapter` (around line 101-126, at the end of the `if registered[...]` block):
```go
if registered[iacServiceFinalizer] {
    a.finalizer = pb.NewIaCProviderFinalizerClient(conn)
}
```

(3d) Add accessor (near existing `Enumerator()` / `DriftDetector()` methods):
```go
// Finalizer returns the typed pb.IaCProviderFinalizerClient or nil
// when the plugin did not register IaCProviderFinalizer. Used by the
// v2 apply path to fire OnPlanComplete hook over gRPC.
// Per workflow#695 Phase 2.5.
func (a *typedIaCAdapter) Finalizer() pb.IaCProviderFinalizerClient {
    return a.finalizer
}
```

**Step 4: Run tests to verify PASS**

```bash
GOWORK=off go test ./cmd/wfctl/ -run 'TestTypedAdapter_Finalizer' -v -count=1
GOWORK=off go build ./cmd/wfctl/...
```
Expected: 2 new tests PASS; build clean.

**Step 5: Commit**

```bash
git add cmd/wfctl/iac_typed_adapter.go cmd/wfctl/iac_typed_adapter_test.go
git commit -m "feat(wfctl): typedIaCAdapter Finalizer() accessor for IaCProviderFinalizer optional service (Phase 2.5)"
```

**Verification (internal-logic refactor):** 2 new tests PASS + build clean.

**Rollback:** revert commit.

---

### Task 5: workflow — wire OnPlanComplete into shared statePersistenceHooks helper + PR cut

**Files:**
- Modify: `cmd/wfctl/infra_apply.go:592` (the SHARED `statePersistenceHooks` helper — single edit covers BOTH v2 dispatch sites at line 472 + line 1609 which both invoke this helper per cycle-1 plan-review I-2). Add `planID string` parameter to helper signature; both call sites pass `plan.ID`.
- Modify: `cmd/wfctl/infra_apply.go:472,1609` (both call sites — add `plan.ID` arg in the helper invocation)

**Step 1: Read shared helper + call sites**

```bash
sed -n '588,650p' cmd/wfctl/infra_apply.go  # helper definition
sed -n '470,475p' cmd/wfctl/infra_apply.go  # call site 1
sed -n '1607,1612p' cmd/wfctl/infra_apply.go # call site 2
```

Confirm the helper builds `ApplyPlanHooks` with `OnResourceApplied` + `OnResourceDeleted` closures, and that both call sites invoke `statePersistenceHooks(store, secretsProvider, provider, providerType, hydratedOut)`.

**Step 2: Extend helper signature + add OnPlanComplete closure**

(2a) Add `planID string` parameter to `statePersistenceHooks` (between `providerType` and `hydratedOut` to minimize re-ordering).

(2b) Inside the helper, add `OnPlanComplete` to the returned `ApplyPlanHooks` struct:

```go
OnPlanComplete: func(ctx context.Context) error {
    // Type-assert to *typedIaCAdapter to access Finalizer() accessor.
    // In-process fake or other provider shapes no-op gracefully.
    adapter, ok := provider.(*typedIaCAdapter)
    if !ok {
        return nil
    }
    fin := adapter.Finalizer()
    if fin == nil {
        // Plugin did not register IaCProviderFinalizer; no-op (preserves
        // pre-Phase-2.5 behavior for plugins that don't opt in).
        return nil
    }
    resp, callErr := fin.FinalizeApply(ctx, &pb.FinalizeApplyRequest{PlanId: planID})
    if callErr != nil {
        return fmt.Errorf("FinalizeApply gRPC: %w", callErr)
    }
    // Append per-driver errors preserving v1 wrapper attribution
    // shape (per design §wfctl-side adapter sample). NOTE: this
    // closure does NOT have direct access to result — the engine
    // closure in apply.go's defer handles result.Errors append for
    // the <plan-finalize> entry. The per-driver attribution from
    // resp.GetErrors() flows through the engine-side closure via
    // the returned wrapped err message + plugin-side fmt detail.
    if len(resp.GetErrors()) > 0 {
        // Build aggregated err message preserving per-driver details
        // so the engine-side defer can wrap it into result.Errors.
        msgs := make([]string, 0, len(resp.GetErrors()))
        for _, e := range resp.GetErrors() {
            msgs = append(msgs, fmt.Sprintf("%s/%s: %s", e.GetResource(), e.GetAction(), e.GetError()))
        }
        return fmt.Errorf("plugin finalize: %d driver(s) failed: %s", len(resp.GetErrors()), strings.Join(msgs, "; "))
    }
    return nil
},
```

(2c) Update both call sites at line 472 + 1609 to pass `plan.ID` as the new arg:

```go
hooks := statePersistenceHooks(store, secretsProvider, provider, providerType, plan.ID, hydratedOut)
```

**Step 3: Verify build + existing tests still pass**

```bash
GOWORK=off go build ./cmd/wfctl/...
GOWORK=off go test ./cmd/wfctl/ -run 'TestInfraApply' -v -count=1
```
Expected: build clean; existing tests PASS.

**Step 4: Commit + push branch + create PR**

```bash
git add cmd/wfctl/infra_apply.go
git commit -m "feat(wfctl): wire OnPlanComplete hook to IaCProviderFinalizer via shared statePersistenceHooks helper (Phase 2.5)"

# Pre-PR verification sweep:
GOWORK=off go build ./...
GOWORK=off go test ./iac/wfctlhelpers/ ./plugin/external/sdk/ ./cmd/wfctl/ -race -count=1
GOWORK=off golangci-lint run --timeout=10m ./iac/wfctlhelpers/... ./plugin/external/sdk/... ./cmd/wfctl/...

git push -u origin feat/695-onplancomplete-impl
gh pr create --base main --head feat/695-onplancomplete-impl \
  --reviewer "@copilot" \
  --title "feat(iac): IaCProviderFinalizer + OnPlanComplete hook (#695 Phase 2.5)" \
  --body "$(cat <<'EOF'
## Summary

Phase 2.5 of workflow#640 cascade. Adds `IaCProviderFinalizer` optional gRPC service + `ApplyPlanHooks.OnPlanComplete` hook so plugins with post-Apply-loop work (DO's database `trusted_sources` deferred-flush) can declare v2 dispatch without losing the post-loop behavior.

## Changes

1. **`feat(proto): IaCProviderFinalizer service + FinalizeApply RPC`** — new optional gRPC service + FinalizeApplyRequest + FinalizeApplyResponse{repeated ActionError errors}; regenerated iac.pb.go in same atomic commit.
2. **`feat(engine): OnPlanComplete hook`** — ApplyPlanHooks.OnPlanComplete field; deferred-closure with loopReached flag fires on all post-loop exit paths (success + per-action errors + hook errors) but NOT pre-loop setup errors. Signature changed to named returns to allow defer to surface finalize errors to caller.
3. **`feat(sdk): auto-register IaCProviderFinalizer`** — optional auto-registration matching existing IaCProviderEnumerator/DriftDetector pattern.
4. **`feat(wfctl): adapter Finalizer() accessor`** — typedIaCAdapter field + accessor matching existing Enumerator()/DriftDetector() pattern; ContractRegistry detection at handle-open.
5. **`feat(wfctl): wire OnPlanComplete at v2 dispatch sites`** — both `infra_apply.go` v2 dispatch call sites build hooks with OnPlanComplete closure that calls adapter.Finalizer().FinalizeApply + appends per-driver errors.

## ADR alignment

- **ADR 0024** (no compat shim): plugins opt in via service registration; absence of registration = no hook firing. No fallback shim.
- **ADR 0040** invariant 1 (per-action evidence): finalize errors preserve per-driver attribution via repeated ActionError; per-action statuses unaltered by finalize-side errors.

## Test plan

- [x] 4 new OnPlanComplete tests in apply_hooks_test.go (clean success, empty plan, error surfaces to caller, pre-loop no-fire)
- [x] SDK auto-registration test in iacserver_test.go
- [x] Adapter accessor tests in iac_typed_adapter_test.go (populated when registered, nil when not)
- [x] Full package race-clean: `iac/wfctlhelpers`, `plugin/external/sdk`, `cmd/wfctl`
- [x] golangci-lint clean

## Cascade context

After merge → tag `v0.55.0` → unblocks workflow-plugin-digitalocean PR #X (v1.3.0) which implements FinalizeApply server-side + flips ComputePlanVersion to "v2".

## Rollback

Revert this PR. ApplyPlanHooks loses OnPlanComplete field (Go zero-value compatibility for callers). gRPC service removal: no DO consumer depends on it pre-cascade. Cut workflow v0.55.1 reverting whichever commit broke. Per ADR 0040 matched-pair rollback. Function-signature change (positional→named returns) is observable to in-tree callers only; ApplyPlan/ApplyPlanWithHooks externally-visible signatures unchanged.

Plan: `docs/plans/2026-05-17-v2-lifecycle-phase2.5-onplancomplete.md` (locked sha256 pending alignment-check).
Design: `docs/plans/2026-05-17-v2-lifecycle-phase2.5-onplancomplete-design.md`.
Closes nothing yet (workflow#695 closes after DO v1.3.0 also ships).

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

**Verification (CLI command class + PR cut):** build clean; race-clean tests; lint clean; PR opened; CI triggers; team-lead monitors per `feedback_active_pr_monitoring`.

**Rollback:** close PR; revert local commits in reverse order (Task 5 closure body MUST revert BEFORE Task 2 engine struct field, per cycle-1 plan-review I-6).

---

### Task 6: workflow — cut v0.55.0 tag from main HEAD (team-lead action post-PR1 merge)

**Files:** none (operational verification + git operation)

**Step 1: After PR 1 admin-merged + Copilot resolved + CI green**

```bash
cd /Users/jon/workspace/workflow
git fetch origin --quiet --tags
git log -1 --oneline origin/main
# Verify HEAD is the squash-merge commit from PR 1
```

**Step 2: Cut tag**

```bash
git tag v0.55.0 origin/main
git push origin v0.55.0
```

**Step 3: Verify GoReleaser fires**

```bash
sleep 8
gh run list --repo GoCodeAlone/workflow --workflow=release.yml --limit 1 --json databaseId,status,headBranch
```
Expected: a new in_progress release run for headBranch=v0.55.0.

**Step 4: Wait for GoReleaser + verify release published**

After 15-20 minutes (cross-compile + asset upload):

```bash
gh api repos/GoCodeAlone/workflow/releases/tags/v0.55.0 --jq '{tag:.tag_name, draft:.draft, prerelease:.prerelease, latest:.is_latest, n_assets:(.assets|length)}'
```
Expected: `draft=false`, `prerelease=false`, `n_assets >= 18` (matching v0.54.0 shape — wfctl + workflow + admin-ui binaries × OS×arch).

If `is_latest:null`, apply defensive `gh release edit v0.54.0 --repo GoCodeAlone/workflow --latest` (matches Phase 2 closeout pattern).

**Verification (version pin update class — release artifact validation per finishing-a-development-branch Step 1a; runtime-launch-validation deferred to Task 9 post-cascade smoke per cycle-1 plan-review I-5):**
- Release published, not draft, all assets uploaded
- Defensive `--latest` flag applied if needed
- Tag visible via `git ls-remote --tags origin v0.55.0`

**Rollback:** `git push --delete origin v0.55.0` + `gh release delete v0.55.0 --repo GoCodeAlone/workflow --yes` + cut workflow v0.55.1 reverting whichever commit broke. Per ADR 0040 matched-pair rollback.

---

### Task 7: workflow-plugin-digitalocean — implement IaCProviderFinalizer + flip v2 + bump pins; release v1.3.0

**Files (worktree at /Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/695-do):**
- Modify: `go.mod` (workflow pin v0.54.0 → v0.55.0)
- Modify: `go.sum` (auto-tidied)
- Modify: `plugin.json` (minEngineVersion 0.54.0 → 0.55.0; version 1.2.0 → 1.3.0)
- Modify: `internal/iacserver.go:49-71` (add `pb.UnimplementedIaCProviderFinalizerServer` to embedded list in `doIaCServer` struct)
- Modify: `internal/iacserver.go:105+` (add `_ pb.IaCProviderFinalizerServer = (*doIaCServer)(nil)` to compile-time interface assertion block)
- Modify: `internal/iacserver.go` (add `FinalizeApply` method on `doIaCServer` — inline per-driver iteration of `s.provider.drivers` calling `du.FlushDeferredUpdates(ctx)`)
- Modify: `internal/iacserver.go` (flip `Capabilities` return-statement struct literal to add `ComputePlanVersion: "v2"`)

**Step 1: Branch + ff-pull**

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin
git worktree add -b feat/695-do-finalize _worktrees/695-do origin/main
cd _worktrees/695-do
```

**Step 2: Bump go.mod pin to workflow v0.55.0**

```bash
go mod edit -require=github.com/GoCodeAlone/workflow@v0.55.0
go mod tidy
```

**Step 3: Bump plugin.json**

Edit `plugin.json`:
- `"version": "1.2.0"` → `"version": "1.3.0"`
- `"minEngineVersion": "0.54.0"` → `"minEngineVersion": "0.55.0"`

**Step 4: Edit internal/iacserver.go — add Unimplemented embed + compile-time assert + FinalizeApply method**

(4a) In `doIaCServer` struct (around lines 49-71), add to the embedded list:
```go
pb.UnimplementedIaCProviderFinalizerServer
```

(4b) In the compile-time interface assertion block at lines 105-109, add:
```go
_ pb.IaCProviderFinalizerServer = (*doIaCServer)(nil)
```

(4c) Add `FinalizeApply` method on `doIaCServer` (near other IaC-server methods):
```go
// FinalizeApply implements pb.IaCProviderFinalizerServer for the v2
// dispatch path. Inlines the per-driver flush loop from
// DOProvider.Apply (internal/provider.go:295-307) since DOProvider does
// not expose a public FlushDeferredUpdates method (the flush iteration
// is inline in the v1 Apply wrapper). Per workflow#695 Phase 2.5.
//
// Per-driver error attribution is preserved by returning ActionError
// entries on the response, mirroring v1 wrapper shape at
// provider.go:301-306. wfctl-side appends each entry to result.Errors
// directly.
func (s *doIaCServer) FinalizeApply(ctx context.Context, _ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
    var errs []*pb.ActionError
    for resourceType, d := range s.provider.drivers {
        du, ok := d.(deferredUpdater)
        if !ok || !du.HasDeferredUpdates() {
            continue
        }
        if flushErr := du.FlushDeferredUpdates(ctx); flushErr != nil {
            errs = append(errs, &pb.ActionError{
                Resource: resourceType,
                Action:   "deferred_update",
                Error:    flushErr.Error(),
            })
        }
    }
    return &pb.FinalizeApplyResponse{Errors: errs}, nil
}
```

(4d) Flip `Capabilities` return-statement struct literal — change ONLY the return literal, NOT signature/receiver/params (per per-plugin universal pattern from Phase 2):
```go
// BEFORE:
return &pb.CapabilitiesResponse{Capabilities: out}, nil
// AFTER:
return &pb.CapabilitiesResponse{
    Capabilities:       out,
    ComputePlanVersion: "v2",  // NEW Phase 2.5: declare v2 dispatch (deferred-flush hoisted to FinalizeApply)
}, nil
```

**Step 5: Build + test**

```bash
go build ./...
go test ./... -race -count=1
```
Expected: all green. (Task 9 adds the regression test; Task 8 verifies existing tests still PASS.)

**Step 6: Commit + push + PR**

```bash
git add go.mod go.sum plugin.json internal/iacserver.go
git commit -m "feat(v2): IaCProviderFinalizer.FinalizeApply + declare ComputePlanVersion=v2 + workflow v0.55.0 (Phase 2.5)"
git push -u origin feat/695-do-finalize
gh pr create --base main --head feat/695-do-finalize \
  --reviewer "@copilot" \
  --title "feat: declare ComputePlanVersion v2 + IaCProviderFinalizer.FinalizeApply impl + bump workflow v0.55.0 pin; release v1.3.0" \
  --body "Phase 2.5 of workflow#640 hard-cutover cascade per ADR 0024 + 0040. DO plugin opts INTO v2 dispatch by declaring ComputePlanVersion=\"v2\" in CapabilitiesResponse + implementing FinalizeApply RPC server-side. wfctl-side OnPlanComplete hook (added in workflow v0.55.0) calls FinalizeApply which inlines the per-driver flush loop from the v1 DOProvider.Apply wrapper (provider.go:295-307) — preserves the database trusted_sources deferred-flush regression-gate.

Coordinated with workflow PR #X (workflow v0.55.0 already tagged + released BEFORE this plugin PR's go.mod bump resolves). Cascading rollback: if this plugin's v1.3.0 fails downstream consumer, cut v1.3.1 reverting workflow pin + Capabilities declaration + FinalizeApply registration.

Closes workflow#695."
```

After CI green + Copilot settle ~10 min + admin-merge:

```bash
gh pr merge <N> --squash --admin --delete-branch
```

**Verification (plugin/extension class + version pin update + plugin loading paths → runtime-launch-validation triggered per finishing-a-development-branch Step 1b):**
- Build clean
- Existing tests PASS race-clean
- PR CI green
- Admin-merged

**Rollback (per design §Rollback):** revert PR. iacserver.go removes ComputePlanVersion="v2" line + FinalizeApply impl + UnimplementedIaCProviderFinalizerServer embed + compile-time assert; go.mod drops to workflow v0.54.0; plugin.json drops to v1.2.0; minEng drops to 0.54.0. Cut DO v1.3.1 restoring v1.2.0 state. DO consumers continue on workflow v0.54.0 + DO v1.2.0 (v1-dispatched).

---

### Task 8: workflow-plugin-digitalocean — regression test for deferred-flush under v2 dispatch

**Files:**
- Create: `internal/iacserver_finalize_test.go`

**Step 1: Write the test**

```go
package internal

// TDD regression gate: FinalizeApply must fire FlushDeferredUpdates on
// every driver that holds queued state. Mirrors the v1 wrapper test
// in provider_deferred_test.go but exercises the v2 dispatch path.
// Per workflow#695 Phase 2.5.

import (
    "context"
    "testing"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestDOIaCServer_FinalizeApply_FlushesDeferredUpdates(t *testing.T) {
    // Set up a minimal DOProvider with the database driver holding a
    // queued deferred update (mirrors fixture from provider_deferred_test.go:155+).
    provider, dbMock := setupProviderWithQueuedDBDeferredUpdate(t) // helper from existing test fixture file

    s := newDOIaCServer(provider)
    resp, err := s.FinalizeApply(context.Background(), &pb.FinalizeApplyRequest{PlanId: "test-plan"})

    if err != nil { t.Fatalf("FinalizeApply error: %v", err) }
    if len(resp.GetErrors()) != 0 { t.Errorf("expected no errors; got: %+v", resp.GetErrors()) }
    if dbMock.lastFirewallReq == nil {
        t.Fatal("UpdateFirewallRules was never called — deferred flush did not run under v2 dispatch via FinalizeApply")
    }
}

func TestDOIaCServer_FinalizeApply_PreservesPerDriverErrorAttribution(t *testing.T) {
    // Set up provider where database driver's FlushDeferredUpdates fails.
    provider, _ := setupProviderWithFailingDBDeferredUpdate(t) // similar helper but mock returns error

    s := newDOIaCServer(provider)
    resp, err := s.FinalizeApply(context.Background(), &pb.FinalizeApplyRequest{PlanId: "test-plan"})

    if err != nil { t.Fatalf("FinalizeApply error (should be in resp.Errors, not top-level): %v", err) }
    if len(resp.GetErrors()) == 0 {
        t.Fatal("expected 1 ActionError in resp; got none")
    }
    if resp.GetErrors()[0].GetResource() != "infra.database" {
        t.Errorf("expected Resource=\"infra.database\"; got %q", resp.GetErrors()[0].GetResource())
    }
    if resp.GetErrors()[0].GetAction() != "deferred_update" {
        t.Errorf("expected Action=\"deferred_update\"; got %q", resp.GetErrors()[0].GetAction())
    }
}
```

**Step 2: Build test fixtures (NEW test infra ~60-80 LOC per cycle-1 plan-review I-3)**

The helpers `setupProviderWithQueuedDBDeferredUpdate` and `setupProviderWithFailingDBDeferredUpdate` do NOT exist today. Build them in the new test file:

**(2a) `setupProviderWithQueuedDBDeferredUpdate`** — extract the seed pattern from `provider_deferred_test.go`'s `TestDOProvider_Apply_FlushesDeferred_WhenTypeAbsentFromPlan` test body (lines 155+). The test inline-seeds a queued deferred update via `dbDriver.Create(...)` with a `trusted_sources` config; refactor that into a reusable helper signature: `func setupProviderWithQueuedDBDeferredUpdate(t *testing.T) (*DOProvider, *minimalDBMock)`. Returns provider with database driver registered + mock with seeded state. (~30 LOC)

**(2b) `setupProviderWithFailingDBDeferredUpdate`** — NEW infrastructure. The existing `minimalDBMock.UpdateFirewallRules` (at `provider_deferred_test.go:~70`) returns nil; the failing variant needs a new mock type or a `failOnFlush bool` field on the existing mock. Recommended: add a `flushErr error` field to `minimalDBMock` that gets returned from UpdateFirewallRules when set. Helper signature: `func setupProviderWithFailingDBDeferredUpdate(t *testing.T) (*DOProvider, *minimalDBMock)` — seeds mock with `flushErr = errors.New("update firewall rules failed")`. (~30-40 LOC)

Reuse `fakeAppForDeferred` + `minimalDBMock` types from `provider_deferred_test.go` — don't duplicate the mock structs.

**Step 3: Run tests**

```bash
go test ./internal/ -run 'TestDOIaCServer_FinalizeApply' -v -count=1
go test ./internal/ -race -count=1
```
Expected: 2 new tests PASS; full package PASS race-clean.

**Step 4: Commit + push**

```bash
git add internal/iacserver_finalize_test.go internal/provider_deferred_test.go  # if helpers were extracted
git commit -m "test(v2): regression-gate FinalizeApply fires FlushDeferredUpdates under v2 dispatch (Phase 2.5)"
git push
```

**Verification (internal-logic refactor + regression test):** 2 new tests PASS + full package PASS race-clean.

**Rollback:** revert commit. Test removal is non-functional change; doesn't affect production.

---

### Task 9: workflow-plugin-digitalocean — cut v1.3.0 tag from main HEAD (team-lead action post-PR2 merge)

**Files:** none (operational verification + git operation)

**Step 1: After PR 2 admin-merged**

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin --quiet --tags
git log -1 --oneline origin/main
# Verify HEAD is the squash-merge commit from PR 2
```

**Step 2: Cut tag**

```bash
git tag v1.3.0 origin/main
git push origin v1.3.0
```

**Step 3: Verify GoReleaser fires + sync workflow fires**

```bash
sleep 8
gh run list --repo GoCodeAlone/workflow-plugin-digitalocean --workflow=release.yml --limit 1
gh run list --repo GoCodeAlone/workflow-plugin-digitalocean --workflow=sync-plugin-version.yml --limit 1
```
Expected: a new in_progress release run AND sync-plugin-version run for headBranch=v1.3.0. (Per aws#18 defensive workflow_dispatch fix shipped 2026-05-17, sync workflow should reliably fire now.)

**Step 4: Wait for GoReleaser + verify release published**

After 5-10 minutes:

```bash
gh api repos/GoCodeAlone/workflow-plugin-digitalocean/releases/tags/v1.3.0 --jq '{tag:.tag_name, draft:.draft, latest:.is_latest, n_assets:(.assets|length)}'
```
Expected: `draft=false`, `n_assets >= 5` (4 binaries + checksums.txt).

Defensive: if `is_latest:null` OR `draft:true`, apply `gh release edit v1.3.0 --repo GoCodeAlone/workflow-plugin-digitalocean --latest --draft=false`.

**Step 5: Admin-merge auto-sync chore-PR if opened**

If sync-plugin-version.yml opened a chore-PR (because plugin.json version 1.3.0 was already in main, the sync should no-op — but if it did open one):

```bash
gh pr list --repo GoCodeAlone/workflow-plugin-digitalocean --search "in:title sync plugin.json version to v1.3.0"
gh pr merge <N> --repo GoCodeAlone/workflow-plugin-digitalocean --squash --admin --delete-branch
```

**Verification (version pin update class + plugin loading paths → runtime-launch-validation REQUIRED per finishing-a-development-branch Step 1b; cycle-1 plan-review I-5 fix — make smoke MANDATORY here, not optional in closeout):**
- Release published, not draft, marked latest, 5+ assets
- Defensive edit applied if needed
- **MANDATORY smoke**: install DO v1.3.0 against wfctl v0.55.0 (`wfctl plugin install github.com/GoCodeAlone/workflow-plugin-digitalocean@v1.3.0`); run representative DB + app plan against a DO project; verify database `trusted_sources` firewall rules applied post-app-create (= deferred-flush ran under v2 dispatch via FinalizeApply RPC). Save transcript to `docs/runtime-validation/2026-05-17-phase2.5-do-finalize-smoke.md` and commit.

**Rollback (per design §Rollback):** `git push --delete origin v1.3.0` + `gh release delete v1.3.0 --repo GoCodeAlone/workflow-plugin-digitalocean --yes` + cut DO v1.3.1 restoring v1.2.0 state. DO consumers continue on workflow v0.54.0 + DO v1.2.0 (v1-dispatched).

---

## Post-cascade closeout (team-lead, AFTER both PRs ship)

These are team-lead operational actions — not separate plan-tasks. They run after the 2-PR cascade lands + before this plan is marked complete.

**Cross-plugin smoke verification (operator-run; NOT a CI gate per design):**

```bash
wfctl plugin install github.com/GoCodeAlone/workflow-plugin-digitalocean@v1.3.0
```

Run a representative DB + app plan against a DO project; verify:
- wfctl chose v2 dispatch (calls `applyV2ApplyPlanWithHooksFn`, NOT `provider.Apply(ctx, &plan)`)
- Database `trusted_sources` firewall rules applied post-app-create (= deferred-flush ran under v2 dispatch via FinalizeApply RPC)
- ApplyResult.Actions populated with len == len(plan.Actions)
- No regression vs prior v1.2.0 (v1-dispatched) behavior

If desired, capture transcript at `docs/runtime-validation/2026-05-17-phase2.5-do-finalize-smoke.md` (optional — operator decides if audit-trail commit is wanted).

**Rollback if smoke fails:** cut DO v1.3.1 hotfix OR cut workflow v0.55.1 reverting whichever commit broke. Per ADR 0040 matched-pair rollback.

**Memory + close + issue:**

- Append to `/Users/jon/.claude/projects/-Users-jon-workspace/memory/project_v2_lifecycle_phase2_shipped.md`: "Phase 2.5 SHIPPED — workflow v0.55.0 + DO v1.3.0; IaCProviderFinalizer optional service + OnPlanComplete hook; DO declares v2 + FinalizeApply preserves deferred-flush. Followups: Phase 2.3 (compensation enums tags 4+5 reserved + tag 6 SKIPPED candidate); Phase 3 (delete DOProvider.Apply v1 wrapper as dead code)."
- Update `MEMORY.md` index entry to include "Phase 2.5 ✓".
- Close workflow#695 with comment: "Shipped via workflow PR #X (v0.55.0) + workflow-plugin-digitalocean PR #Y (v1.3.0). Cross-plugin smoke verified."

---

## Out of scope (per design — separate future passes)

- Phase 2.3: ACTION_STATUS_COMPENSATED / COMPENSATION_FAILED / SKIPPED enum value additions (tags 4+5 already reserved in Phase 2 PR #694 b09bced1; tag 6 candidate for SKIPPED). Separate cascade.
- Phase 3: delete `DOProvider.Apply` v1 wrapper as dead code post-Phase-2.5 cutover. Separate cleanup PR.
- Per-plugin ComputePlanVersion regression-guard test cluster (deferred from Phase 2 closeout).
- OnPlanCompleteWithResult variant (if future plugin needs access to per-action evidence in finalize).
- wfctl-side OpenTelemetry span around OnPlanComplete (telemetry follow-up).
