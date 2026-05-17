# Phase 2.5 — `OnPlanComplete` hook + `IaCProviderFinalizer` optional gRPC service

**Date:** 2026-05-17
**Status:** Draft (autonomous brainstorming output)
**Related:** workflow#695, workflow#640 (Phase 2), workflow PR #694 (v0.54.0 cascade), DO PR #120 (deferred v2 opt-out)
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

**Picked: deferred-closure pattern with `loopReached` flag** at function entry of `applyPlanWithEnvProviderAndHooks`.

`OnPlanComplete` fires on every exit path AFTER `loopReached` is set (which happens just before the `for i := range plan.Actions` loop opens). Setup-phase early-exits (lines 167, 180 — JIT-env-provider error, deferred-postcondition panic) do NOT fire OnPlanComplete because no cloud-side work has happened.

```go
func applyPlanWithEnvProviderAndHooks(...) (*interfaces.ApplyResult, error) {
    // ... existing setup ...
    var loopReached bool
    defer func() {
        if !loopReached || hooks.OnPlanComplete == nil {
            return
        }
        if hookErr := hooks.OnPlanComplete(ctx); hookErr != nil {
            // Append to result.Errors so caller sees both per-action
            // outcomes AND finalize-side failure.
            result.Errors = append(result.Errors, interfaces.ActionError{
                Resource: "<plan-finalize>",
                Action:   "finalize",
                Error:    fmt.Errorf("plan finalize: %w", hookErr).Error(),
            })
            // The outer named-return `err` is overwritten below if it's
            // currently nil — finalize failure should surface to caller.
            // If err is already non-nil (per-action driver error or hook
            // error from inside the loop), the existing err wins.
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

The deferred closure correctly handles all 4 post-loop exit paths (success at line 255, hook-fail returns at 229/240/249). For pre-loop returns at 167/180, `loopReached` is false → defer no-ops.

### E. DO migration scope

**Picked: minimal — add FinalizeApply server-side + flip v2 declare; keep `DOProvider.Apply` wrapper.**

`DOProvider.Apply` becomes dead code under v2 dispatch (wfctl bypasses it). But it's still callable by:
- Legacy v1 in-process embedders (theoretical; nobody runs this today per Phase 1 inventory)
- The wfctl-side cascade itself if a future workflow tag re-routes through it

Deletion = separate Phase 3 cleanup PR; keep current PR small + reversible.

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
  // error is the wire form of the finalize-side error, if any.
  // Empty string = success. Non-empty = wrapped + surfaced to wfctl's
  // result.Errors as an ActionError with Resource="<plan-finalize>".
  string error = 1;
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

```go
// typedIaCAdapter satisfies a new optional interface ApplyFinalizer for
// callers that build the OnPlanComplete hook from the adapter:
type ApplyFinalizer interface {
    FinalizeApply(ctx context.Context, planID string) error
}

func (a *typedIaCAdapter) FinalizeApply(ctx context.Context, planID string) error {
    if !a.hasFinalizer { return nil } // not registered = no-op
    resp, err := a.finalizerClient.FinalizeApply(ctx, &pb.FinalizeApplyRequest{PlanId: planID})
    if err != nil { return fmt.Errorf("gRPC: %w", err) }
    if resp.Error != "" { return fmt.Errorf("plugin finalize: %s", resp.Error) }
    return nil
}

// In the v2 dispatch caller (likely cmd/wfctl/infra_apply_v2_loader.go):
hooks := wfctlhelpers.ApplyPlanHooks{
    OnResourceApplied: ...,
    OnResourceDeleted: ...,
    OnPlanComplete: func(ctx context.Context) error {
        if fin, ok := adapter.(ApplyFinalizer); ok {
            return fin.FinalizeApply(ctx, plan.ID)
        }
        return nil
    },
}
```

### Plugin SDK (workflow/plugin/external/sdk/iacserver.go)

Add to the optional-server registration block:
```go
if fin, ok := provider.(pb.IaCProviderFinalizerServer); ok {
    pb.RegisterIaCProviderFinalizerServer(s, fin)
}
```

### DO plugin (workflow-plugin-digitalocean/internal/iacserver.go)

```go
// FinalizeApply implements pb.IaCProviderFinalizerServer for the v2
// dispatch path. Calls the same DOProvider.FlushDeferredUpdates() that
// DOProvider.Apply called post-loop in the v1 dispatch path.
// Per workflow#695 (Phase 2.5).
func (s *doIaCServer) FinalizeApply(ctx context.Context, _ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
    if err := s.provider.FlushDeferredUpdates(ctx); err != nil {
        return &pb.FinalizeApplyResponse{Error: err.Error()}, nil
    }
    return &pb.FinalizeApplyResponse{}, nil
}

// Capabilities flips to v2:
func (s *doIaCServer) Capabilities(...) (...) {
    return &pb.CapabilitiesResponse{
        Capabilities:       out,
        ComputePlanVersion: "v2",
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

## Assumptions (load-bearing)

1. **DO is the only plugin needing FinalizeApply today.** aws/gcp/azure have no post-Apply-loop work in their existing provider wrappers (verified by Phase 1 inventory — only DO uses the canonical-delegate `wfctlhelpers.ApplyPlan` wrapper pattern that runs deferred-flush).
2. **No new resource types depend on Phase 2.5.** The change extends the contract; it doesn't gate any feature. DO continues to function on workflow v0.54.0 + DO v1.2.0 (v1-dispatched) indefinitely.
3. **`DOProvider.FlushDeferredUpdates` is idempotent and safe to call after partial-failure plans.** Per `internal/provider.go:300` it's called regardless of per-action errors in the current v1 wrapper; v2 path preserves this semantic.
4. **ContractRegistry can advertise the new optional service** without a separate registration RPC. Verified by reading existing IaCProviderEnumerator/DriftDetector/CredentialRevoker patterns — registration happens at plugin server start; wfctl reads via existing handle-open negotiation.
5. **Hook signature `func(ctx context.Context) error` is sufficient for the foreseeable hook implementations.** No identified use case today needs `*ApplyResult` (DO's flush is driver-internal-state-only).
6. **workflow v0.55.0 is backward compatible.** Plugins not declaring v2 (DO v1.2.0, older tags) continue using v1 dispatch path with no behavior change. Adding the optional service doesn't break the IaCProviderRequired contract.
7. **The `loopReached` flag pattern correctly distinguishes "no cloud work happened" from "loop ran".** Pre-loop setup failures at lines 167/180 do NOT need finalize; the flag captures that.
8. **DO v1.3.0 ships AFTER workflow v0.55.0 tag is published** (matched-pair sequencing per Phase 2 precedent). Reverse order would have DO declaring v2 + needing FinalizeApply RPC against a workflow that doesn't know about it.

## Self-challenge findings (cleaned up before adversarial review)

1. **"Laziest plausible solution?"** Keep DO on v1 dispatch forever. Cost: DO never gets v2 hooks; Phase 2.3 compensation work can't apply to DO. Not viable long-term but it IS the no-op path. Phase 2.5 = the minimum work to unblock DO.
2. **"Most fragile assumption?"** Assumption 5 (hook signature stays sufficient). If a future plugin needs `*ApplyResult` in finalize, we'd need a new hook field (`OnPlanCompleteWithResult`?) — the existing minimal signature can't evolve without breaking changes. Mitigated by: this is the established pattern (OnResourceApplied/Deleted both have fixed signatures); future additions add new hook fields rather than mutating existing ones.
3. **"YAGNI sweep?"** Considered + rejected: HookKind enum for multi-stage finalize, pre-loop SetUp hook, per-resource-type finalize. None needed today. Minimal scope holds.
4. **"What fails first under partial failure?"** Plan with 5 actions where action 3 driver fails. Per Phase 2 deferred-closure invariant, loop continues best-effort; all 5 get ActionOutcomes; finalize fires AFTER loop. DO's FlushDeferredUpdates iterates driver-queued state — drivers that did succeed (actions 1, 2, 4, 5) get their queued updates flushed; driver from action 3 had no successful Create so no queued state. Correct semantics.
5. **"Repo pattern conflict?"** Follows IaCProviderEnumerator optional-service precedent (`plugin/external/proto/iac.proto`). Matches ApplyPlanHooks struct extension pattern (PR #694 added `OnResourceApplied/OnResourceDeleted`; PR #695 adds `OnPlanComplete`). No conflict.

## Rollback

**For workflow v0.55.0:** revert PR cleanly. ApplyPlanHooks loses OnPlanComplete field (callers stop passing it — Go zero-value compatibility). gRPC service removal: existing v1.2.0 plugins don't depend on it; v1.3.0 DO declares v2 + needs it, so reverting workflow forces DO to also revert v1.3.0 → v1.2.0. **Cut workflow v0.55.1** reverting whichever commit broke. Per ADR 0040 matched-pair rollback.

**For DO v1.3.0:** revert PR. iacserver.go removes ComputePlanVersion="v2" line + FinalizeApply impl; go.mod drops to workflow v0.54.0; plugin.json drops to v1.2.0; minEng drops to 0.54.0. **Cut DO v1.3.1** restoring v1.2.0 state. DO consumers continue on workflow v0.54.0 + DO v1.2.0 (v1-dispatched).

If both ship cleanly but a downstream consumer (BMW deploy) reveals a regression, cascade rollback: DO v1.3.1 (revert v2 declare + FinalizeApply) + workflow v0.55.1 (optional — only if engine-side change is the issue).

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
