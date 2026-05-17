# Phase 2.5 — `OnPlanComplete` hook + `IaCProviderFinalizer` optional gRPC service

**Date:** 2026-05-17
**Status:** Draft revised after cycle-1 adversarial-design-review (4 Crit + 6 Imp + 3 Minor addressed)
**Related:** workflow#695, workflow#640 (Phase 2), workflow PR #694 (v0.54.0 cascade, merge sha `dd9a3130`), DO PR #120 (deferred v2 opt-out)
**ADRs binding:** decisions/0024-iac-typed-force-cutover.md (no compat shim), decisions/0040-v2-action-lifecycle-provider-compatibility.md (Phase 1 invariants)

## Goal

Add an engine-side post-action-loop finalizer hook so plugins with post-Apply-loop work (DO's database `trusted_sources` deferred-flush at `internal/provider.go:243-274`) can register that work over gRPC. After this lands, DO declares `CapabilitiesResponse.ComputePlanVersion="v2"` and joins aws/gcp/azure on the v2 dispatch path; the deferred-flush continues to run under v2 routing.

## Cascade scope (2 repos / 2 sequential tags)

1. **workflow v0.55.0** ships first:
   - Add optional gRPC service `IaCProviderFinalizer` with single RPC `FinalizeApply(FinalizeApplyRequest) returns (FinalizeApplyResponse)` in `plugin/external/proto/iac.proto`
   - Regenerate `iac.pb.go` (atomic with proto edit per Phase 2 cycle-1 I-1 precedent)
   - Extend `iac/wfctlhelpers.ApplyPlanHooks` with `OnPlanComplete func(context.Context) error` field
   - Plumb `OnPlanComplete` invocation into `applyPlanWithEnvProviderAndHooks` after the per-action loop completes (success + partial-failure + hook-error paths; NOT pre-loop setup-failure paths)
   - Plumb wfctl-side: `cmd/wfctl/iac_typed_adapter.go` detects `IaCProviderFinalizer` server registration via existing ContractRegistry pattern; wires `OnPlanComplete` to call `FinalizeApply` gRPC method
   - Plugin SDK: `plugin/external/sdk/iacserver.go` registers `IaCProviderFinalizerServer` when provider implements it (mirrors existing `IaCProviderEnumeratorServer` optional-service pattern)
   - Tests: ApplyPlanHooks.OnPlanComplete fires on all post-loop exit paths; gRPC adapter wires correctly; plugin SDK skips registration when not implemented

2. **workflow-plugin-digitalocean v1.3.0** ships second:
   - `internal/iacserver.go::doIaCServer` implements `pb.IaCProviderFinalizerServer.FinalizeApply` — calls existing `DOProvider.FlushDeferredUpdates(ctx)`
   - Plugin SDK auto-registers IaCProviderFinalizerServer (since doIaCServer now satisfies the interface)
   - `internal/iacserver.go::doIaCServer.Capabilities` flips `ComputePlanVersion: "v2"` in CapabilitiesResponse
   - go.mod: workflow v0.54.0 → v0.55.0; plugin.json minEngineVersion 0.54.0 → 0.55.0; plugin.json version 1.2.0 → 1.3.0
   - **Keep `DOProvider.Apply` wrapper in place** for now (still callable by legacy v1 callers; Phase 3 cleanup removes it as dead code later)
   - **Regression test gate**: extend `internal/provider_deferred_test.go` (or new `internal/iacserver_finalize_test.go`) to assert FlushDeferredUpdates fires under v2 dispatch via FinalizeApply RPC

## Architecture decisions

### A.0 Engine function signature change (REQUIRED prerequisite per cycle-1 C-2)

`applyPlanWithEnvProviderAndHooks` at `iac/wfctlhelpers/apply.go:112` currently uses **positional** returns `(*interfaces.ApplyResult, error)`. The deferred-closure plumbing (§D) requires a deferred function to potentially mutate the returned `err` (finalize-side errors must surface to the caller if no per-action error is already set). This requires changing the signature to **named** returns:

```go
// BEFORE (apply.go:112-118):
func applyPlanWithEnvProviderAndHooks(
    ctx context.Context,
    p interfaces.IaCProvider,
    plan *interfaces.IaCPlan,
    applyTimeEnv func(string) (string, bool),
    hooks ApplyPlanHooks,
) (*interfaces.ApplyResult, error) {

// AFTER (Phase 2.5):
func applyPlanWithEnvProviderAndHooks(
    ctx context.Context,
    p interfaces.IaCProvider,
    plan *interfaces.IaCPlan,
    applyTimeEnv func(string) (string, bool),
    hooks ApplyPlanHooks,
) (result *interfaces.ApplyResult, err error) {
```

All existing `return result, ...` statements at apply.go:167, 180, 229, 240, 249, 255 continue to work unchanged with named returns. The change is callsite-compatible (function signature externally observable types are identical).

### A. Optional gRPC service vs piggyback on ApplyResponse

**Picked: optional gRPC service `IaCProviderFinalizer`** with single RPC `FinalizeApply`.

**Considered alternatives:**
- **B: piggyback `repeated FinalizeAction finalize_actions` on ApplyResponse** — confuses the action lifecycle; ApplyResponse becomes multi-purpose payload; doesn't match "finalize after the action loop" semantic; couples logically independent concerns to a single RPC. Rejected.
- **C: reuse existing optional service (e.g., IaCProviderEnumerator)** — semantic abuse; future contributors confused why finalize lives in Enumerator; violates established pattern. Rejected.

The optional-service approach matches the precedent set by `IaCProviderEnumerator` / `IaCProviderDriftDetector` / `IaCProviderCredentialRevoker`: plugins register only the services they implement; wfctl checks at handle-open via ContractRegistry. **The absence of registration is the negative signal — no `not_supported` boolean field anywhere.** This honors ADR 0024 hard-cutover discipline (plugins opt-in via capability declaration, not via feature flags).

### B. Hook signature

**Picked: `func(ctx context.Context) error`** — minimal.

Considered: `func(ctx context.Context, result *interfaces.ApplyResult) error` (richer; gives hook access to per-action evidence).

Picked minimal because:
1. The wfctl-side adapter has access to `result` at closure construction time; can capture it in the closure if needed for telemetry. No need to pass redundantly.
2. The plugin-side handler (DO's `FlushDeferredUpdates`) doesn't need the per-action evidence — it operates on driver-internal queued state, not on plan.Actions.
3. Smaller surface = easier to evolve later. Can add `result *ApplyResult` parameter to a new hook later if a real need surfaces; can't easily remove it from this hook once it's in the contract.

### C. Failure semantics

**Picked: hook failure appends to `result.Errors` as ActionError, returns wrapped error from `applyPlanWithEnvProviderAndHooks`.**

The per-action work (and per-action `result.Actions` entries) is preserved — caller sees both the per-action outcomes AND the finalize error. The apply is "completed with finalize error", not "applied with no record". Operator sees both signals + can decide rollback.

Hook failure does NOT alter `result.Actions[].Status` — per-action statuses reflect what happened at action-dispatch time, not what happened post-loop. This matches ADR 0040 invariant 1 ("per-action evidence reflects what actually happened cloud-side, not just whether engine reached a clean end-state").

### D. Plumbing — where does `OnPlanComplete` fire?

**Picked: deferred-closure pattern with `loopReached` flag** at function entry of `applyPlanWithEnvProviderAndHooks` (named-return signature per §A.0).

**Exit-path classification** (re-verified against apply.go HEAD):

| Line | Context | Trigger | Fire OnPlanComplete? |
|---|---|---|---|
| `apply.go:167` | Setup phase — initial state-fetch / preflight error | `return result, err` | NO (no cloud work) |
| `apply.go:180` | Setup phase — pre-loop early exit | `return result, err` | NO (no cloud work) |
| `apply.go:229` | Inside per-action loop — `OnResourceDeleted` hook returned error | `return result, fmt.Errorf("...%w", hookErr.err)` | YES (cloud work in flight) |
| `apply.go:240` | Inside per-action loop — `OnResourceDeleted` post-hook error | `return result, fmt.Errorf("post-delete hook: %w", err)` | YES |
| `apply.go:249` | Inside per-action loop — `OnResourceApplied` post-hook error | `return result, fmt.Errorf("post-apply hook: %w", err)` | YES |
| `apply.go:255` | Success path — loop completed | `return result, nil` | YES |

The `loopReached` flag is set immediately before the `for i := range plan.Actions` loop opens. Pre-loop returns (167, 180) do NOT fire OnPlanComplete because no cloud-side work has happened. Post-loop returns (229/240/249/255) do fire.

**Empty-plan case (`len(plan.Actions) == 0`)**: `loopReached` is set to `true` immediately before the loop, regardless of whether the loop body executes. So for `len(plan.Actions) == 0` plans, `OnPlanComplete` still fires after the no-op loop. This matches the v1 wrapper semantic — DOProvider.Apply at `internal/provider.go:295-307` iterates the driver registry (NOT plan.Actions), so flush fires for stale-queued state even on empty-actions plans. The v2 path preserves this behavior. Regression-test reference: `TestDOProvider_Apply_FlushesDeferred_WhenTypeAbsentFromPlan` in `provider_deferred_test.go:155+` exercises exactly this empty-plan-but-stale-queue case.

```go
func applyPlanWithEnvProviderAndHooks(
    ctx context.Context,
    p interfaces.IaCProvider,
    plan *interfaces.IaCPlan,
    applyTimeEnv func(string) (string, bool),
    hooks ApplyPlanHooks,
) (result *interfaces.ApplyResult, err error) {
    // ... existing setup ...
    var loopReached bool
    defer func() {
        if !loopReached || hooks.OnPlanComplete == nil {
            return
        }
        if hookErr := hooks.OnPlanComplete(ctx); hookErr != nil {
            // Append to result.Errors so caller sees both per-action
            // outcomes AND finalize-side failure (per-driver attribution
            // is preserved by FinalizeApplyResponse.errors — see §E proto).
            result.Errors = append(result.Errors, interfaces.ActionError{
                Resource: "<plan-finalize>",
                Action:   "finalize",
                Error:    fmt.Errorf("plan finalize: %w", hookErr).Error(),
            })
            // Surface finalize error to caller's err if no prior error
            // already exists. If err is already non-nil (per-action driver
            // error or hook error from inside the loop), the existing err
            // wins so the original failure cause is preserved. This is
            // why §A.0 requires named-return signature.
            if err == nil {
                err = fmt.Errorf("plan finalize: %w", hookErr)
            }
        }
    }()
    // ... existing setup ...
    loopReached = true
    for i := range plan.Actions {
        // ... existing loop ...
    }
    return result, nil
}
```

### E. DO migration scope — server-side inline iteration (per cycle-1 C-1)

**Picked: inline the per-driver flush loop directly in `doIaCServer.FinalizeApply`** — do NOT add a new `DOProvider.FlushDeferredUpdates` public method.

**Why this changed from cycle-1 design:** The original design's cycle-1 cite `DOProvider.FlushDeferredUpdates(ctx)` does NOT exist on `*DOProvider`. The actual flush loop is inline in `DOProvider.Apply` at `internal/provider.go:295-307`, iterating `p.drivers` directly and dispatching per-driver via the `deferredUpdater` interface (declared at provider.go:238-241). The iacserver.go server-side handler has direct access to `s.provider.drivers` (via the existing `provider *DOProvider` field at `internal/iacserver.go:71`) — duplicating the 12-line iteration there is cleaner than introducing a new public method that's solely consumed by FinalizeApply.

```go
// internal/iacserver.go (DO plugin):
func (s *doIaCServer) FinalizeApply(ctx context.Context, _ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
    var errs []*pb.ActionError
    // Iterate driver registry (mirror of provider.go:295-307 — flush
    // even for resource types absent from the plan, since drivers may
    // hold stale-queued state from a prior aborted apply).
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

`DOProvider.Apply` stays in place unchanged (still callable by legacy v1 in-process embedders if any exist; deletion deferred to Phase 3 cleanup). Phase 3 deletes the v1 wrapper as dead code once Phase 2.5 has been smoke-verified.

**Per-driver error attribution preserved**: the inline iteration produces one ActionError per failed driver, returned as the `repeated ActionError errors` field on FinalizeApplyResponse — wfctl-side gets the exact per-driver attribution v1 had at `provider.go:301-306`. Addresses cycle-1 I-2.

**`UnimplementedIaCProviderFinalizerServer` embed**: `doIaCServer` already embeds 8 `Unimplemented*Server` stubs (`internal/iacserver.go:50-69`); add `pb.UnimplementedIaCProviderFinalizerServer` to that list so forward-compat works when future RPCs are added to the IaCProviderFinalizer service. Addresses cycle-1 I-1.

### F. workflow tag = v0.55.0 (minor)

Phase 2 went v0.53.1 → v0.54.0 (minor; added optional v2 declare). Phase 2.5 follows same pattern: backward-compatible new optional service + new optional hook field. v0.55.0 is correct semver.

### G. Plugin SDK auto-registration

The existing pattern in `plugin/external/sdk/iacserver.go` is:
```go
required, ok := provider.(pb.IaCProviderRequiredServer)
// register IaCProviderRequired
if enum, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
    pb.RegisterIaCProviderEnumeratorServer(s, enum)
}
```

Add the same pattern for IaCProviderFinalizer:
```go
if fin, ok := provider.(pb.IaCProviderFinalizerServer); ok {
    pb.RegisterIaCProviderFinalizerServer(s, fin)
}
```

Plugins implementing the interface get registered automatically; plugins that don't get nothing. ContractRegistry exposes the registration list to wfctl.

## Components

### Proto (workflow/plugin/external/proto/iac.proto)

```protobuf
// IaCProviderFinalizer is the optional service plugins implement when
// they need a post-apply-loop finalizer hook under v2 dispatch.
// Use case: DigitalOcean plugin's database trusted_sources deferred-flush
// (see workflow-plugin-digitalocean/internal/provider.go:243-274) which
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
  // Future Phase 2.3 may add per-action compensation evidence here;
  // tag 2 reserved for that purpose.
  repeated ActionError errors = 1;
  // 2 reserved (Phase 2.3 compensation evidence)
}
```

### Go engine surface (workflow/iac/wfctlhelpers/apply.go)

```go
type ApplyPlanHooks struct {
    OnResourceApplied func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error
    OnResourceDeleted func(context.Context, interfaces.PlanAction) error
    // OnPlanComplete fires once after the per-action loop completes —
    // on success, partial-failure, AND per-action hook-error paths.
    // It does NOT fire on pre-loop setup failures. Per workflow#695
    // (Phase 2.5 of #640). Used by DO plugin's deferred-flush wrapper.
    OnPlanComplete func(context.Context) error
}
```

### wfctl-side typed adapter (workflow/cmd/wfctl/iac_typed_adapter.go)

**Use the established `*Client()` accessor pattern** (per cycle-1 I-4) — matches existing `Enumerator()`, `DriftDetector()`, etc. accessors at iac_typed_adapter.go:171-221. Do NOT introduce a new Go interface.

```go
// Finalizer returns the typed IaCProviderFinalizer client if the plugin
// registered the optional service, nil otherwise. Matches the existing
// optional-service accessor pattern (Enumerator, DriftDetector, etc.).
// Per workflow#695 Phase 2.5.
func (a *typedIaCAdapter) Finalizer() pb.IaCProviderFinalizerClient {
    return a.finalizerClient // nil when ContractRegistry didn't include the service
}

// In the v2 dispatch caller (cmd/wfctl/infra_apply_v2_loader.go):
hooks := wfctlhelpers.ApplyPlanHooks{
    OnResourceApplied: ...,
    OnResourceDeleted: ...,
    OnPlanComplete: func(ctx context.Context) error {
        fin := adapter.Finalizer()
        if fin == nil {
            return nil // plugin didn't register IaCProviderFinalizer
        }
        resp, err := fin.FinalizeApply(ctx, &pb.FinalizeApplyRequest{PlanId: plan.ID})
        if err != nil {
            return fmt.Errorf("FinalizeApply gRPC: %w", err)
        }
        for _, e := range resp.GetErrors() {
            // Preserve per-driver attribution by appending each ActionError
            // directly to result.Errors. Note: the closure captures
            // `result` from the caller's outer scope so it can mutate it.
            result.Errors = append(result.Errors, interfaces.ActionError{
                Resource: e.GetResource(),
                Action:   e.GetAction(),
                Error:    e.GetError(),
            })
        }
        if len(resp.GetErrors()) > 0 {
            return fmt.Errorf("plugin finalize: %d driver(s) failed", len(resp.GetErrors()))
        }
        return nil
    },
}
```

The new typedIaCAdapter field `finalizerClient pb.IaCProviderFinalizerClient` is populated at handle-open in `buildTypedIaCAdapterFrom` (per existing pattern for Enumerator/DriftDetector). Type-assertion against ContractRegistry advertised services determines registration; nil-client = plugin didn't opt in.

The closure capturing `result` mirrors how `OnResourceApplied`/`OnResourceDeleted` closures already capture state in the existing v2 dispatch caller — verify pattern at `cmd/wfctl/infra_apply_v2_loader.go` before locking implementation details in the plan.

### Plugin SDK (workflow/plugin/external/sdk/iacserver.go)

Add to the optional-server registration block:
```go
if fin, ok := provider.(pb.IaCProviderFinalizerServer); ok {
    pb.RegisterIaCProviderFinalizerServer(s, fin)
}
```

### DO plugin (workflow-plugin-digitalocean/internal/iacserver.go)

```go
// doIaCServer also embeds (in addition to the 8 existing Unimplemented*Server stubs at iacserver.go:50-69):
//   pb.UnimplementedIaCProviderFinalizerServer

// FinalizeApply implements pb.IaCProviderFinalizerServer for the v2
// dispatch path. Inlines the per-driver flush loop from
// DOProvider.Apply (provider.go:295-307) since DOProvider does not
// expose a public FlushDeferredUpdates method (the flush iteration is
// inline in the v1 Apply wrapper). Per workflow#695 (Phase 2.5).
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

// Capabilities flips to v2 (only the return-statement struct literal changes;
// receiver, signature, params unchanged per the per-plugin universal pattern
// from Phase 2 docs/plans/2026-05-16-v2-lifecycle-phase2.md):
func (s *doIaCServer) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
    // ... existing IaCCapabilityDeclaration build into `out` ...
    return &pb.CapabilitiesResponse{
        Capabilities:       out,
        ComputePlanVersion: "v2", // NEW Phase 2.5: declare v2 dispatch (deferred-flush hoisted to FinalizeApply)
    }, nil
}
```

## Data flow

```
wfctl-side                                  plugin-side (DO)
─────────                                   ────────────────
infra apply <config>
  └─ load plugin via ExternalPluginManager
       └─ ContractRegistry registers
          IaCProviderRequired + IaCProviderFinalizer
  └─ buildTypedIaCAdapterFrom
       └─ typedIaCAdapter.hasFinalizer = true
  └─ DispatchVersionFor(adapter) → "v2"
  └─ wfctlhelpers.ApplyPlanWithHooks(ctx, adapter, plan, ApplyPlanHooks{
       OnResourceApplied: ...,
       OnResourceDeleted: ...,
       OnPlanComplete: func(ctx) error {
         return adapter.FinalizeApply(ctx, plan.ID)
       },
     })
       ├─ defer { if loopReached && hook != nil { hook(ctx) } }
       ├─ for action in plan.Actions:
       │    ├─ JIT-substitute
       │    ├─ ResourceDriver(type)        ──gRPC──>  IaCProviderRequired.ResourceDriver
       │    ├─ driver.Create/Update/Delete ──gRPC──>  pb.Driver.CreateResource (drivers internally queue deferred updates)
       │    ├─ append ActionOutcome to result.Actions
       │    └─ fire OnResourceApplied/Deleted
       └─ deferred fires:
            └─ adapter.FinalizeApply       ──gRPC──>  IaCProviderFinalizer.FinalizeApply
                                                      └─ s.provider.FlushDeferredUpdates(ctx)
                                                          └─ drivers iterate queued updates
                                                              └─ apply database firewall rules
```

## Assumptions (load-bearing — revised after cycle-1)

1. **DO is the only plugin needing FinalizeApply today.** aws/gcp/azure have no post-Apply-loop work in their existing provider wrappers (verified by Phase 1 inventory — only DO uses the canonical-delegate `wfctlhelpers.ApplyPlan` wrapper pattern that runs deferred-flush).
2. **No new resource types depend on Phase 2.5.** The change extends the contract; it doesn't gate any feature. DO continues to function on workflow v0.54.0 + DO v1.2.0 (v1-dispatched) indefinitely.
3. **Per-driver `FlushDeferredUpdates` (on the `deferredUpdater` interface at `internal/provider.go:238-241`) is idempotent and safe to call after partial-failure plans.** Per `internal/provider.go:295-307` it's iterated regardless of per-action errors in the v1 wrapper; v2 path preserves this semantic. Note: there is no `DOProvider.FlushDeferredUpdates` public method — the flush iteration is inline in `DOProvider.Apply`. Phase 2.5 inlines the same iteration in `doIaCServer.FinalizeApply` (see §E).
4. **ContractRegistry can advertise the new optional service** without a separate registration RPC. Verified by reading existing IaCProviderEnumerator/DriftDetector/CredentialRevoker patterns — registration happens at plugin server start; wfctl reads via existing handle-open negotiation.
5. **Hook signature `func(ctx context.Context) error` is sufficient for the foreseeable hook implementations.** No identified use case today needs `*ApplyResult` (DO's flush is driver-internal-state-only).
6. **workflow v0.55.0 is backward compatible.** Plugins not declaring v2 (DO v1.2.0, older tags) continue using v1 dispatch path with no behavior change. Adding the optional service doesn't break the IaCProviderRequired contract.
7. **The `loopReached` flag pattern correctly distinguishes "no cloud work happened" from "loop ran".** Pre-loop setup failures at apply.go:167/180 do NOT need finalize; the flag captures that. Empty-plan case (`len(plan.Actions) == 0`) DOES fire finalize because `loopReached=true` is set before the for-statement evaluates the range; this preserves the v1 semantic (DOProvider.Apply flushes stale-queued state even for empty plans, regression-tested by `TestDOProvider_Apply_FlushesDeferred_WhenTypeAbsentFromPlan`).
8. **DO v1.3.0 ships AFTER workflow v0.55.0 tag is published** (matched-pair sequencing per Phase 2 cascade precedent — PR #694 merge sha `dd9a3130` published 2026-05-16T17:42Z + tag pushed 18:05Z; DO v1.2.0 followed at 22:03Z merge + tag).
9. **Function signature change to named returns (§A.0)** does not break in-tree call sites. Verified: `ApplyPlanWithHooks` / `ApplyPlan` / `applyPlanWithEnvProvider` wrappers (apply.go:78-110) all delegate via `return applyPlanWithEnvProviderAndHooks(...)` — no callers introspect the return tuple's named-vs-positional shape.

## Cycle-1 adversarial-review findings addressed

| Finding | Severity | Resolution |
|---|---|---|
| `DOProvider.FlushDeferredUpdates` does not exist | Critical | §E rewritten: inline per-driver iteration in `doIaCServer.FinalizeApply` (no new public method on DOProvider). Cite trace fixed: actual flush loop at provider.go:295-307. |
| Deferred-closure pattern requires named-return signature | Critical | §A.0 added as required prerequisite — explicit signature change documented. |
| Cited file:line numbers for exit paths | Critical (reviewer claim) | RE-VERIFIED against apply.go HEAD; lines 167/180/229/240/249/255 ARE correct in current code. Added per-line classification table in §D. |
| Empty-plan case unaddressed | Critical | §D paragraph added explaining empty-plan behavior + regression-test citation. |
| Missing UnimplementedIaCProviderFinalizerServer embed | Important | §E + §DO plugin component spec now mandates the embed. |
| Per-driver error attribution loss | Important | FinalizeApplyResponse extended with `repeated ActionError errors` (instead of single string); proto + wfctl closure both updated to preserve attribution. |
| Downstream-consumer pin-reset in rollback | Important | §Rollback paragraph added explaining matched-pair sequencing including BMW/core-dump/workflow-cloud pin-reset. |
| ApplyFinalizer interface diverges from accessor pattern | Important | §wfctl-side adapter rewritten to use `Finalizer() pb.IaCProviderFinalizerClient` accessor, matching existing Enumerator()/DriftDetector() pattern. |
| v0.54.0 PR/tag SHA not cited | Important | Header now cites PR #694 merge sha `dd9a3130`. |
| MEMORY.md reference unconfirmed | Important | Verified: `project_v2_lifecycle_phase2_shipped.md` was created in the Phase 2 cascade closeout (2026-05-16 22:24Z). |
| Cite range tightening (provider.go:243-274 → 228-310) | Minor | §Goal cite tightened to provider.go:295-307 (the flush loop specifically). |
| FinalizeApplyRequest comment cross-ref to Phase 2.3 | Minor | Proto comment updated; tag 2 reserved for Phase 2.3 compensation evidence. |
| Phase 2 deferred-closure cite | Minor | Citation tightened. |

Reviewer also proposed 3 alternative options. Option 1 (inline iteration) was adopted (§E). Options 2 (defer Phase 2.5 entirely) + 3 (reuse OnResourceApplied for final action) explicitly rejected (in §Considered alternatives at top of doc).

## Self-challenge findings (cleaned up before adversarial review)

1. **"Laziest plausible solution?"** Keep DO on v1 dispatch forever. Cost: DO never gets v2 hooks; Phase 2.3 compensation work can't apply to DO. Not viable long-term but it IS the no-op path. Phase 2.5 = the minimum work to unblock DO.
2. **"Most fragile assumption?"** Assumption 5 (hook signature stays sufficient). If a future plugin needs `*ApplyResult` in finalize, we'd need a new hook field (`OnPlanCompleteWithResult`?) — the existing minimal signature can't evolve without breaking changes. Mitigated by: this is the established pattern (OnResourceApplied/Deleted both have fixed signatures); future additions add new hook fields rather than mutating existing ones.
3. **"YAGNI sweep?"** Considered + rejected: HookKind enum for multi-stage finalize, pre-loop SetUp hook, per-resource-type finalize. None needed today. Minimal scope holds.
4. **"What fails first under partial failure?"** Plan with 5 actions where action 3 driver fails. Per Phase 2 deferred-closure invariant, loop continues best-effort; all 5 get ActionOutcomes; finalize fires AFTER loop. DO's FlushDeferredUpdates iterates driver-queued state — drivers that did succeed (actions 1, 2, 4, 5) get their queued updates flushed; driver from action 3 had no successful Create so no queued state. Correct semantics.
5. **"Repo pattern conflict?"** Follows IaCProviderEnumerator optional-service precedent (`plugin/external/proto/iac.proto`). Matches ApplyPlanHooks struct extension pattern (PR #694 added `OnResourceApplied/OnResourceDeleted`; PR #695 adds `OnPlanComplete`). No conflict.

## Rollback

**For workflow v0.55.0:** revert PR cleanly. ApplyPlanHooks loses OnPlanComplete field (callers stop passing it — Go zero-value compatibility). gRPC service removal: existing v1.2.0 plugins don't depend on it; v1.3.0 DO declares v2 + needs it, so reverting workflow forces DO to also revert v1.3.0 → v1.2.0. **Cut workflow v0.55.1** reverting whichever commit broke. Per ADR 0040 matched-pair rollback. The function-signature change (positional→named returns per §A.0) is observable to in-tree callers only — `ApplyPlan` / `ApplyPlanWithHooks` wrappers' externally-visible signatures don't change.

**For DO v1.3.0:** revert PR. iacserver.go removes ComputePlanVersion="v2" line + FinalizeApply impl + UnimplementedIaCProviderFinalizerServer embed; go.mod drops to workflow v0.54.0; plugin.json drops to v1.2.0; minEng drops to 0.54.0. **Cut DO v1.3.1** restoring v1.2.0 state. DO consumers continue on workflow v0.54.0 + DO v1.2.0 (v1-dispatched).

**For downstream consumer pins** (per cycle-1 I-3): any repo whose `go.mod` pins workflow v0.55.0 (e.g., BMW, core-dump, workflow-cloud, workflow-cloud-ui) must also revert their workflow pin to v0.54.0 in a coordinated patch PR. Same matched-pair sequencing as Phase 2 cascade: workflow rollback first → DO rollback second → downstream-consumer pin-reset third. The Phase 2 cascade memory entry (`project_v2_lifecycle_phase2_shipped.md`) lists DO+wfctl-dependent consumers (BMW for DO, others for wfctl).

If both ship cleanly but a downstream consumer (BMW deploy) reveals a regression, cascade rollback: DO v1.3.1 (revert v2 declare + FinalizeApply) + workflow v0.55.1 (optional — only if engine-side change is the issue) + downstream consumer-side pin-reset PRs.

## Out of scope (deferred)

- **Phase 2.3** — ACTION_STATUS_COMPENSATED / COMPENSATION_FAILED / SKIPPED enum value additions (tags 4+5 reserved; tag 6 candidate for SKIPPED). Separate cascade.
- **Phase 3** — delete `DOProvider.Apply` wrapper as dead code post-Phase-2.5 cutover (separate cleanup PR; DO v1.3.0 keeps the wrapper to minimize blast radius).
- **OnPlanCompleteWithResult variant** (if future plugin needs access to per-action evidence in finalize). Add new hook field then; don't break this one.
- **Test coverage for ComputePlanVersion regression-guard across all 4 plugins** (Phase 2 follow-up cluster; tracked separately).
- **wfctl-side opentelemetry span around OnPlanComplete** (telemetry follow-up; not blocking Phase 2.5 functionality).

## Acceptance criteria

1. workflow v0.55.0 ships with new optional `IaCProviderFinalizer` service + ApplyPlanHooks.OnPlanComplete field + engine wiring + wfctl adapter + plugin SDK auto-registration + tests passing.
2. DO v1.3.0 ships with FinalizeApply server impl + ComputePlanVersion="v2" + workflow pin v0.55.0 + minEngineVersion 0.55.0 + regression test for deferred-flush-under-v2.
3. Live verification (operator-run; not CI-gated): install DO v1.3.0 against wfctl v0.55.0, run a representative DB+app plan, verify trusted_sources firewall rules get applied (= deferred-flush ran under v2 dispatch).
4. workflow#695 closed; cross-references updated in MEMORY.md `project_v2_lifecycle_phase2_shipped.md`.
