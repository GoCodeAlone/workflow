# V2 Action Lifecycle — Phase 2 gRPC Contract Extension + 5-Repo Hard-Cutover — Design

**Status:** Draft
**Date:** 2026-05-16
**Operator:** Jon (autonomous-mode mandate 2026-05-16)
**Tracking issue:** GoCodeAlone/workflow#640
**Phase 1 (already shipped):** PR #691 at f5ec18ef — inventory + ADR 0040 + Deprecated marker + 4 callers migrated
**ADR (will land in this design):** decisions/0041-v2-applyresponse-actions-contract.md
**Related ADRs (BINDING):** decisions/0024-iac-typed-force-cutover.md (no compat shim), decisions/0040-v2-action-lifecycle-provider-compatibility.md (5 provider expectations)

## Goal

Phase 2 of #640. Extend the `IaCProviderRequiredServer.Apply` gRPC contract with per-action outcome evidence so wfctl-side `OnResourceApplied` + `OnResourceDeleted` hooks fire correctly. Ship as a coordinated HARD-CUTOVER PR cascade across 5 repos per ADR 0024 (no compat shim, no graceful proto fallback).

**Phase 2 scope (REVISED per cycle-1 adversarial review: Pattern B collapsed — 8 deliverables, not 10):**

The cycle-1 review correctly identified that "Pattern B (custom-loop)" is architecturally broken: aws/gcp/azure don't declare `compute_plan_version="v2"` in their CapabilitiesResponse, so wfctl takes the v1 dispatch path (`provider.Apply(ctx, &plan)` direct call). Even if those plugins populate the new `Actions` wire field, wfctl never reads it on the v1 path. The intended OnResourceApplied / OnResourceDeleted hook firing CANNOT happen via Pattern B.

**Correct architecture**: have ALL 4 plugins DECLARE `compute_plan_version="v2"` in their CapabilitiesResponse. wfctl then routes through `wfctlhelpers.ApplyPlanWithHooks` for all 4 — engine-side population handles Actions UNIFORMLY (Pattern A becomes the ONLY pattern). The plugins' existing `IaCProvider.Apply` impls become unused dead code (engine dispatches via `provider.ResourceDriver(action.Resource.Type)` per action instead).

**8 deliverables:**

1. Extend `plugin/external/proto/iac.proto` `ApplyResult` message with `repeated ActionResult actions` field + new `enum ActionStatus` (workflow PR)
2. Regenerate `iac.pb.go` from updated proto (workflow PR)
3. Extend `interfaces.ApplyResult` Go struct with `Actions []ActionOutcome` field (workflow PR)
4. Update `cmd/wfctl/iac_typed_adapter.go::applyResultFromPB` to decode the new field. **REVISED per cycle-1 C-2**: function signature must thread plan-action-count for length validation. Either (a) add `expectedActionCount uint32` parameter to applyResultFromPB OR (b) move length validation to caller after applyResultFromPB returns.
5. Update `iac/wfctlhelpers/apply.go::applyPlanWithEnvProviderAndHooks` to populate `result.Actions` during dispatch (workflow PR) — the engine ALREADY iterates plan.Actions via `dispatchAction`; Phase 2 just appends ActionOutcome per iteration
6. **REVISED per cycle-1 C-3**: manifest validation gate scope. Two options: (a) implement new `plugin/external/manifest` package with `ValidateBytes(bytes []byte) error` + create `schema/plugin_manifest.json` (hidden scope, ~200 LOC). (b) Use existing `schema/` package's JSON schema infrastructure if it covers plugin.json (probe required at writing-plans). (c) DEFER manifest validation to Phase 2.1 separate PR. Decision needed at writing-plans phase. Phase 2 design now LISTS this explicitly as deliverable #6 with scope-decision sub-task.
7. **All 4 plugins (aws/gcp/azure/DO): declare `compute_plan_version="v2"` in CapabilitiesResponse** + bump workflow pin + bump minEngineVersion. ~5-line change per plugin. PRs in parallel. Plugins' existing `IaCProvider.Apply` impls become dead-code; they could be deleted in a Phase 2.5 cleanup but kept for Phase 2 to avoid blast radius.
8. Cross-plugin smoke: install each plugin into wfctl + run sample apply + verify ActionResults populated AND that v2 dispatch path was taken (not v1 fallback).

**Coordinated cutover:** workflow PR + 4 plugin PRs land in same release window. Workflow tag (v0.54.0 candidate) carries the engine change. 4 plugin tags (aws v1.2.0 / gcp v1.2.0 / azure v1.2.0 / DO v1.2.0) carry the per-plugin Apply impls. All 5 ship in lockstep.

**Out of scope for Phase 2** (deferred to Phase 3 / 5):
- Phase 3: codemod-driven canonical-form bump for plugins that delegate to wfctlhelpers.ApplyPlan (currently only DO)
- Phase 5: remove `wfctlhelpers.ApplyPlan` entirely

## Architecture

**1-design-doc + 1-ADR + 5-coordinated-PR cascade**. Per-repo single PR; coordinated release tags.

### Proto extension (definitive spec)

```protobuf
// plugin/external/proto/iac.proto (additions)

// ActionStatus categorizes per-action outcomes for wfctl-side hook dispatch.
// Per ADR 0040 invariants 1-2. (Compensation invariants 3 deferred to Phase
// 2.3 when engine-side compensation logic actually exists — cycle-2 review
// correctly flagged that defining COMPENSATED + COMPENSATION_FAILED enum
// values in Phase 2 with no reachable emission path creates dead values +
// linter exhaustiveness violations. Phase 2 scope ships only the 3 statuses
// that have engine-side emission paths today.)
enum ActionStatus {
  // Default; must NEVER be emitted by plugins. wfctl rejects ApplyResponse
  // with any action_index whose status == UNSPECIFIED (catches plugin bugs
  // where action result was forgotten).
  ACTION_STATUS_UNSPECIFIED = 0;

  // Action succeeded; wfctl fires OnResourceApplied (or OnResourceDeleted
  // for successful delete actions per ADR 0040 invariant 1).
  ACTION_STATUS_SUCCESS = 1;

  // Action failed (non-delete: create/update/replace). For create/update, no
  // resource exists; wfctl skips OnResourceApplied. ApplyResult.errors carries
  // the human-readable error (existing field).
  ACTION_STATUS_ERROR = 2;

  // Delete action failed (resource still exists in cloud); wfctl MUST NOT
  // fire OnResourceDeleted (state preserved). Distinct from ACTION_STATUS_ERROR
  // because wfctl-side downstream behavior differs per ADR 0040 invariant 2.
  ACTION_STATUS_DELETE_FAILED = 3;

  // FUTURE: ACTION_STATUS_COMPENSATED + ACTION_STATUS_COMPENSATION_FAILED
  // reserve tags 4 + 5 for Phase 2.3 when engine-side compensation lands.
  // NOT defined in Phase 2; defining-without-emitting creates dead enum
  // values per cycle-2 review.
}

// ActionResult is the per-action outcome surfacing for Phase 2 v2 hooks.
// Per ADR 0040 invariant 1.
//
// REVISED per cycle-2 review: output_keys field DROPPED. Hook firing
// (OnResourceApplied / OnResourceDeleted) requires only action_index +
// status. Per-resource outputs already live in ApplyResult.resources
// (existing aggregated field; engine-side populated). Adding a parallel
// map<string,string> with map[string]any→string conversion ambiguity
// expands proto surface without delivering new capability for hooks.
// Phase 2.2 may add a per-action outputs field if a future caller actually
// needs per-action output correlation; today no such caller exists.
message ActionResult {
  // 0-indexed position in the input plan.actions array.
  uint32 action_index = 1;

  // Per ActionStatus enum semantics above.
  ActionStatus status = 2;

  // Per-action error message. Empty unless status is ERROR / DELETE_FAILED.
  // Mirrors interfaces.ActionError.Error for backwards compat with the
  // existing ApplyResult.errors aggregation path (engine-side reconciles
  // when populating both fields).
  string error = 3;
}

// Extend existing ApplyResult message with the new field at tag 7 (non-conflicting
// with current tags 1-6).
message ApplyResult {
  string plan_id = 1;
  repeated ResourceOutput resources = 2;
  repeated ActionError errors = 3;
  map<string, string> initial_input_snapshot = 4;
  repeated DriftEntry input_drift_report = 5;
  map<string, string> replace_id_map = 6;
  // NEW for Phase 2 (workflow#640 ADR 0041): per-action outcomes for wfctl-side
  // hook dispatch. Per ADR 0024 + 0040, plugins MUST populate this field; wfctl
  // MUST validate that len(actions) == len(plan.actions) on receipt. Absent or
  // partial actions field → wfctl rejects ApplyResponse with structured error.
  repeated ActionResult actions = 7;
}
```

### Go interfaces extension (workflow side)

```go
// interfaces/iac.go additions

// ActionStatus mirrors pb.ActionStatus. Type-safe enum at the Go boundary.
type ActionStatus uint8

const (
    ActionStatusUnspecified ActionStatus = iota
    ActionStatusSuccess
    ActionStatusError
    ActionStatusDeleteFailed
    ActionStatusCompensated
    ActionStatusCompensationFailed
)

// ActionOutcome mirrors pb.ActionResult.
// REVISED per cycle-2: Outputs field dropped (see ActionResult proto comment).
type ActionOutcome struct {
    ActionIndex uint32
    Status      ActionStatus
    Error       string
}

// ApplyResult — add Actions field; preserve existing fields.
type ApplyResult struct {
    PlanID               string
    Resources            []ResourceOutput
    Errors               []ActionError
    InitialInputSnapshot map[string]string
    InputDriftReport     []DriftEntry
    ReplaceIDMap         map[string]string
    Actions              []ActionOutcome // NEW Phase 2
}
```

### wfctl-side decoder (REVISED per cycle-1 C-2)

**Cycle-1 finding:** `applyResultFromPB(r *pb.ApplyResult)` does NOT receive `plan`; the design's pseudo-code `plan.GetActions()` was a compile error.

**Fix:** length validation lives at the CALLER (which has both plan + result). applyResultFromPB only decodes + rejects UNSPECIFIED. The two-phase responsibility split:

```go
// applyResultFromPB (no signature change): just decode the new field
func applyResultFromPB(r *pb.ApplyResult) (*interfaces.ApplyResult, error) {
    // ... existing decoding ...
    actions := make([]interfaces.ActionOutcome, 0, len(r.GetActions()))
    for _, a := range r.GetActions() {
        if a.GetStatus() == pb.ActionStatus_ACTION_STATUS_UNSPECIFIED {
            return nil, fmt.Errorf("plugin returned ActionResult with UNSPECIFIED status at action_index=%d (Phase 2 contract violation per ADR 0041)", a.GetActionIndex())
        }
        actions = append(actions, interfaces.ActionOutcome{
            ActionIndex: a.GetActionIndex(),
            Status:      mapPBStatusToInterface(a.GetStatus()),
            Outputs:     a.GetOutputKeys(),
            Error:       a.GetError(),
        })
    }
    result.Actions = actions
    return result, nil
}

// At the caller (typedIaCAdapter.Apply, iac_typed_adapter.go:350) — V1 PATH ONLY:
result, err := applyResultFromPB(resp.GetResult())
if err != nil { return nil, err }
// NOTE: Phase 2 length validation does NOT live here. typedIaCAdapter.Apply
// is reached only on the V1 DISPATCH PATH (provider.Apply direct). On the
// v2 path (wfctlhelpers.ApplyPlanWithHooks), this function is never called;
// engine populates Actions per dispatch loop iteration.
//
// V1-plugin compatibility: future plugins that stay on v1 (declare empty
// computePlanVersion) will return ApplyResult with len(actions) == 0. The
// Phase 2 contract length validation MUST happen on the V2 ENGINE PATH
// only — see next subsection.
```

### Engine-side length validation (cycle-2 fix for I-2)

```go
// In iac/wfctlhelpers/apply.go::applyPlanWithEnvProviderAndHooks AFTER the
// dispatchAction loop completes:
if len(result.Actions) != len(plan.Actions) {
    // ASSERT, not return: this is an engine-internal invariant violation
    // (engine just iterated len(plan.Actions) times; if Actions count
    // differs, engine has a bug). Log + return error rather than panic.
    return result, fmt.Errorf("internal: ApplyPlanWithHooks produced %d ActionOutcomes for %d plan actions (engine invariant violation)", len(result.Actions), len(plan.Actions))
}
```

This places the validation where the engine actually iterates the plan + can guarantee 1:1 correspondence. V1-plugin path (`typedIaCAdapter.Apply` → `provider.Apply`) does NOT enforce this check because v1 plugins legitimately return empty Actions field; that's the v1 dispatch contract.

### Engine-side population (iac/wfctlhelpers/apply.go::applyPlanWithEnvProviderAndHooks)

```go
// In the dispatchAction loop, after each action (success OR error):
result.Actions = append(result.Actions, interfaces.ActionOutcome{
    ActionIndex: uint32(i),
    Status:      mapErrToStatus(dispatchErr, action.Type),
    Error:       errOrEmpty(dispatchErr),
})
```

`mapErrToStatus(err, actionType)` returns:
- `ActionStatusSuccess` if `err == nil`
- `ActionStatusDeleteFailed` if `err != nil && actionType == "delete"`
- `ActionStatusError` otherwise

Compensation status values (COMPENSATED + COMPENSATION_FAILED) are RESERVED for Phase 2.3 — cycle-2 review correctly identified that no engine path currently emits them (doCreate's upsert recovery is name-conflict workaround, not cloud-side compensation; defaultReplaceWithHooks documents no rollback attempted). Phase 2 ships only the 3 reachable statuses.

### Per-plugin implementation pattern (REVISED — single uniform pattern)

**All 4 plugins (aws/gcp/azure/DO): declare `compute_plan_version="v2"` in CapabilitiesResponse.** wfctl then routes through `wfctlhelpers.ApplyPlanWithHooks` for all 4. Engine-side dispatch via `provider.ResourceDriver(action.Resource.Type)` per action; engine-side population of `result.Actions` per dispatch.

Plugin-side change template (~5 LOC per plugin):

```go
// internal/server.go OR equivalent: where Capabilities RPC handler lives
func (s *Server) Capabilities(ctx context.Context, _ *emptypb.Empty) (*pb.CapabilitiesResponse, error) {
    return &pb.CapabilitiesResponse{
        // ... existing CapabilityDeclarations field ...
        ComputePlanVersion: "v2",  // NEW Phase 2: declare v2 dispatch
    }, nil
}
```

This is the IaCCapabilityDeclaration array stays unchanged; the addition is the `compute_plan_version` STRING field on CapabilitiesResponse (proto path `plugin/external/proto/iac.proto:382`).

**Existing `IaCProvider.Apply` impls on aws/gcp/azure become dead code post-cutover** — wfctl no longer calls them on the v2 path. Phase 2 keeps them in-tree to minimize blast radius; Phase 2.5 follow-up may delete.

### Manifest validation gate (REVISED per cycle-1 C-3)

**Cycle-1 finding:** `pluginmanifest.ValidateBytes` doesn't exist; design referenced a non-existent package. ~200 LOC hidden scope.

**Resolution:** scope-decision at writing-plans phase. Three options recorded for writing-plans evaluation:

(a) **Implement `plugin/external/manifest` package** with `ValidateBytes(bytes []byte) error` + create `schema/plugin_manifest.json` schema file. ~200 LOC of new code; adds a real-time JSON-schema-validation dependency (e.g., `github.com/santhosh-tekuri/jsonschema`).

(b) **Reuse existing `schema/` package's JSON schema infrastructure** IF it covers plugin.json — writing-plans probes this; if `schema/` already has a plugin manifest schema generator, validation hook can ride on top.

(c) **DEFER manifest validation to a Phase 2.1 follow-up PR.** Phase 2 ships without the gate; Phase 1 Assumption 8 risk remains (silent v1 fallback on manifest typos) until 2.1 closes it. Acceptable trade-off if the schema-validation library + schema-file scope is too big to fold into Phase 2's 5-repo cutover.

**Phase 2 design RECOMMENDS option (c)** — defer manifest validation to Phase 2.1. Reason: the 5-repo HARD-CUTOVER is already substantial; adding net-new package + dependency expands blast radius. Phase 2.1 is a single workflow-side PR that adds the validation gate WITHOUT cross-repo cutover risk. Phase 2.1 can also handle the typo-silent-fallback risk via a simpler check (verify computePlanVersion field value is one of {"v1", "v2"} OR empty) without requiring full JSON-schema infrastructure.

This shifts deliverable #6 from "implement manifest validation gate" to "file workflow#640-followup tracking issue for Phase 2.1 manifest validation gate."

### ResourceReplacer manifest declaration (ADR 0040 invariant 5)

Plugins declaring `ResourceReplacer` interface usage at the resource-type level via `plugin.json`:

```json
{
  "iacProvider": {
    "computePlanVersion": "v2",
    "resourceTypes": [
      {"name": "infra.compute", "drivers": ["compute"], "resourceReplacer": true},
      {"name": "infra.network", "drivers": ["network"]}
    ]
  }
}
```

wfctl-side `deploy_providers.go` reads `resourceReplacer: true` PER resource type; pre-mutation gating in `preflightProviderOwnedReplaceWithDeleteHooks` (`iac/wfctlhelpers/apply.go:166`) uses this declaration instead of runtime ResourceReplacer detection.

## Coordinated cutover sequence (REVISED per cycle-1 timing critique)

**Realistic 2-3 hour release window** — cycle-1 reviewer correctly flagged that the original "30-min atomic window" claim ignored CI + Copilot review cycles + GoReleaser windows + draft-edit defensive fixes (azure-pattern recurring). Empirical baseline from this session's plugin sweep: 15-20 min per plugin PR (CI ~10min + Copilot settle ~10min + admin-merge + tag-push + GoReleaser ~5-15min + defensive edit ~30s). 5 repos parallel = bounded by slowest path ≈ 30-45 min IF all goes clean, 1-2 hours with any per-repo retry/Copilot finding, 2-3 hours with multi-round Copilot iteration.

```
T-0:           workflow PR opened (proto + decoder + engine + Phase 2.1 followup-issue-file)
T+10-20m:      workflow PR CI + Copilot settle; admin-merge; v0.54.0 tag pushed
T+20-30m:      v0.54.0 GoReleaser verifies (defensive draft=false if needed)
T+30m:         All 4 plugin PRs opened in parallel (each with workflow pin bumped to v0.54.0 + Capabilities computePlanVersion="v2" + minEng bumped to "0.54.0")
T+45-90m:      Plugin PRs CI + Copilot review cycles (Copilot may surface findings; per-plugin iterate)
T+90-120m:     All 4 plugin PRs merged; tags pushed (aws v1.2.0 + gcp v1.2.0 + azure v1.2.0 + DO v1.2.0)
T+120-150m:    All 4 plugin GoReleaser runs verified; defensive draft=false per plugin
T+150-180m:   Cross-plugin smoke: install each plugin + run sample apply + verify v2 dispatch + Actions populated
T+180m+:       Memory update + close Phase 2 + flag Phase 2.1 + Phase 3 followups
```

**Plugin v1.2.0 semver decision (cycle-1 reviewer flagged):** plugins move from v1.1.x to v1.2.0 (MINOR bump). Decision rationale: per ADR 0040 + 0024, old plugin tags become permanently incompatible with workflow v0.54.0+ (hard-cutover). Semver convention treats this as "compatibility ratcheting via minEngineVersion" rather than "API break of the plugin's own API" — the PLUGIN's exported API (Go module + plugin.json schema) is unchanged; what changes is which workflow version the plugin requires. MINOR is the established convention for "minEngineVersion floor moved" per prior sweep (v1.1.0 from v0.51.x → v0.53.x pin was also a minor bump). Operator who wants MAJOR (v2.0.0) should override before Phase 2 ships.

**Failure modes + rollback:**

| Failure | Detection | Recovery |
|---------|-----------|----------|
| 1 of 4 plugin PRs fails CI | Pre-merge | Pause cutover; fix plugin; resume |
| 1 of 4 plugin tags fails GoReleaser | Post-tag | Defensive draft-edit; if assets missing, debug per-plugin |
| Cross-plugin smoke fails on plugin X | Post-tag | Cut plugin X v1.2.1 hotfix; OR revert workflow v0.54.0 → v0.54.1 reverting proto change (LAST RESORT — undoes hard-cutover) |
| All 4 plugins ship clean, smoke green | — | DONE; proceed to memory updates |

**ADR 0024 binding constraint:** NO graceful fallback. If workflow v0.54.0 ships but a plugin can't update for any reason, that plugin is permanently incompatible with v0.54.0+. Operator must pin workflow to v0.53.x until plugin catches up.

## Approaches considered

**Approach A (CHOSEN — REVISED PER CYCLE-1): additive proto field + ALL 4 PLUGINS DECLARE compute_plan_version="v2" → uniform engine-side population.** wfctl routes via wfctlhelpers.ApplyPlanWithHooks for all 4; engine populates ApplyResult.Actions per dispatch loop iteration. Pro: single dispatch path; engine has 1:1 plan→outcome iteration so length-validation invariant is natural; no per-plugin custom-loop code. Con: aws/gcp/azure's existing IaCProvider.Apply method becomes unreachable post-cutover (dead code; kept in-tree for Phase 2 minimization, deletable in Phase 2.5).

**Approach B (REJECTED — was previously "custom-loop population for non-canonical plugins"): plugins' own Apply loops populate Actions field via gRPC wire.** Cycle-1 review correctly killed this: hooks fire only on v2 dispatch path; custom-loop plugins take v1 path; wfctl never reads Actions even if populated. Pattern was architecturally broken.

**Approach C (REJECTED): inject per-action hooks via callback bridge over gRPC streaming.** Pro: real-time hook firing during apply. Con: massively more complex; streaming RPC requires bidirectional client/server state; out of scope; violates ADR 0024 (new dispatch path = compat shim by another name).

## Assumptions

1. **Proto wire format is forwards-compatible AT THE FIELD LEVEL** — new `actions` field at tag 7 doesn't conflict with existing fields. Verified by checking iac.proto:295 (ApplyResult tags 1-6 used). New tag 7 + reserved-tags discipline = safe.
2. **All 4 IaC plugins use the same `iac.proto` definition** — verified plan-2 + plan-1 sweep work. Plugins re-import workflow/plugin/external/proto via go module path.
3. **`wfctlhelpers.ApplyPlanWithHooks` is the engine-side path that populates `result.Actions`** — direct path: dispatchAction → action result evidence → append to result.Actions. Verified during Phase 1: ApplyPlanWithHooks is the ONLY engine entrypoint after Phase 1+4 migration.
4. **`pluginmanifest.ValidateBytes` (or equivalent) exists or can be added** — verified existence of `schema/` package with JSON schema generation. Phase 2 adds the runtime validator if not present.
5. **`extractOutputs` helper composing per-resource outputs from interfaces.ResourceOutput is straightforward** — verify during writing-plans.
6. **No plugin currently relies on a "DEFER actions field to engine" behavior** — i.e., custom-loop plugins (aws/gcp/azure) don't expect engine to fill in Actions retroactively. Verified: today they return ApplyResult without Actions; the absence is interpreted as "all aggregated in errors[] field" — Phase 2 forces them to populate explicitly.
7. **Plugins are released with semver MINOR bump (v1.1.0 → v1.2.0).** Phase 2 IS a breaking-compat change (per ADR 0024 the old plugin can't talk to new workflow), but the SEMVER convention treats this as breaking-via-minimum-engine-version, not as a major bump. Verify with operator if minor bump is too aggressive.
8. **wfctl v0.54.0 minEngineVersion enforcement happens via existing `*-engine-range` conformance CI gates per `feedback_check_versions_actively`.** Plugins ship with minEngineVersion: "0.54.0" — old workflow pin (v0.53.x) won't load v1.2.0 plugins.
9. **Test plan: every plugin PR's test suite has a NEW unit test asserting `ApplyResult.Actions` populated with len == len(plan.Actions) + correct ActionStatus per action.** Mandatory; Phase 2 ADR 0041 records this.

## Self-challenge round (top doubts)

1. **`ACTION_STATUS_UNSPECIFIED` rejection at decoder layer is strict; could break in-flight upgrade window.** If wfctl is upgraded to v0.54.0 while a stale-tagged plugin (pre-v1.2.0) is still loaded, the plugin emits no actions field → decoder reads len(actions)==0 vs plan having N actions → REJECTS. This is intentional per ADR 0024 (no graceful fallback). But it means operators MUST upgrade plugins immediately after upgrading workflow.

2. **Per-action `output_keys` semantic ambiguity** — for delete actions, what goes in output_keys? Spec says "empty for delete actions." But ResourceReplacer (delete-then-create) may surface new outputs from the recreate phase. Decision needed: which action is the replace recorded under — the delete or the create-after-delete?

3. **Manifest validation gate is a Phase 2 scope addition (not in original ADR 0040 invariants).** Including it expands Phase 2. Counter: Phase 1 Assumption 8 explicitly flagged it as Phase 2 scope; the cycle-1 reviewer accepted this.

## Tech Stack

- Protobuf (`plugin/external/proto/iac.proto` + `iac.pb.go` regen)
- Go modules (workflow + 4 plugin repos)
- GoReleaser (per-repo release tags)
- JSON schema (manifest validation)
- ALL workflow-side Go cmds need `GOWORK=off`

## Base branch

- workflow: `feat/v2-phase2-grpc-contract` off origin/main
- workflow-plugin-aws: `feat/v2-applyresult-actions` off origin/main
- workflow-plugin-gcp: same
- workflow-plugin-azure: same
- workflow-plugin-digitalocean: same

## Rollback

Per-PR rollback per Phase 1 precedent. If hard-cutover fails mid-flight:

1. **workflow v0.54.0 revertible** — cut v0.54.1 reverting iac.proto + iac.pb.go + applyResultFromPB + engine populate + manifest gate. Removes the Phase 2 hard-cutover but preserves Phase 1 (ADR 0040, godoc Deprecated marker, 4 caller migrations).
2. **Plugin tags permanent in Go proxy** — old v1.2.0 tags can't be deleted, but `latest` resolves to subsequent rollback tag (v1.2.1) re-pinning workflow → v0.53.x.
3. **Matched-pair rollback** — workflow v0.54.0 + 4 plugin v1.2.0 tags ALL roll back together if Phase 2 needs full revert. Matches plan-2 cloud-SDK matched-pair pattern.

## Decisions to record

ADR 0041 — "V2 ApplyResponse Actions contract":
- Status: Accepted
- Context: Phase 1 (ADR 0040) declared 5 provider compatibility expectations; Phase 2 implements
- Decision: extend ApplyResult proto with `repeated ActionResult actions` + new `enum ActionStatus`; wfctl decoder rejects ApplyResponse with len(actions) != len(plan.actions) OR any UNSPECIFIED status; coordinated 5-repo cutover per ADR 0024
- Consequences: workflow v0.54.0 + 4 plugin v1.2.0 release window; plugins MUST upgrade simultaneously; old plugin tags permanently incompatible with v0.54.0+; manifest validation DEFERRED to Phase 2.1 (separate workflow-side PR); ALL 4 plugins uniformly declare compute_plan_version="v2" → engine-side population (single dispatch path, no Pattern A/B bifurcation per cycle-1 collapse); aws/gcp/azure existing IaCProvider.Apply impls become dead code (kept for Phase 2; deletable in Phase 2.5); ACTION_STATUS_COMPENSATED + COMPENSATION_FAILED enum values RESERVED but not defined in Phase 2 (no engine emission path; Phase 2.3 when compensation logic lands)

## Pipeline next step

After this design lands + adversarial-design-review --phase=design PASSES → invoke `superpowers:writing-plans`. The plan body produces a Scope Manifest with 5 PRs (1 per repo) and 10 tasks (proto + regen + interfaces + decoder + engine populate + manifest gate + aws Apply + gcp Apply + azure Apply + DO pin bump + 4 plugin tag releases + cross-plugin smoke + memory update).

**Note on autonomous execution:** Phase 2's 5-repo coordinated cutover is substantively heavier than this session's accumulated context budget can sustain in a single autonomous run. After scope-lock, the plan is ready for fresh-session execution per the user mandate to recreate-team-per-task. Fresh session picks up the locked plan + dispatches subagent-driven-development.
