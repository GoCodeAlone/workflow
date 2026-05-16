# V2 Action Lifecycle Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the IaC plugin gRPC contract with per-action outcome evidence (`ApplyResult.actions` + `ActionStatus` enum) so wfctl-side v2 hooks fire correctly, shipped as a HARD-CUTOVER coordinated 5-repo cascade per ADR 0024 + 0040.

**Architecture:** workflow PR adds proto extension + Go interfaces + decoder + engine-side population. 4 plugin PRs declare `compute_plan_version="v2"` in CapabilitiesResponse so wfctl routes through v2 dispatch (`wfctlhelpers.ApplyPlanWithHooks`) for all 4. Engine-side dispatch populates Actions uniformly; no per-plugin custom Apply loops needed (Pattern B collapsed per cycle-1 review). Plugins' existing `IaCProvider.Apply` impls become dead code post-cutover (kept in-tree for Phase 2 minimization).

**Tech Stack:** Protobuf (iac.proto + regenerated iac.pb.go), Go modules (workflow + 4 plugin repos), GoReleaser, ADR 0024 + 0040 binding constraints (NO compat shim, NO graceful proto fallback).

**Base branch:** `main` per repo (workflow + 4 plugins each)

---

## Scope Manifest

**PR Count:** 5
**Tasks:** 11
**Estimated Lines of Change:** ~500 (workflow ~400 incl proto+pb.go regen; per-plugin ~5-10 LOC × 4 plugins; smoke + memory ~50)

**Out of scope:**
- Phase 2.1: manifest validation gate at `cmd/wfctl/deploy_providers.go::findIaCPluginDir` (deferred per design — separate workflow-side PR; either implements new `plugin/external/manifest` package OR ships lightweight `computePlanVersion ∈ {v1,v2}` check)
- Phase 2.3: engine-side compensation logic + `ACTION_STATUS_COMPENSATED` / `COMPENSATION_FAILED` enum value emission (tags 4+5 reserved in proto comment; defined-without-emitting is dead code per cycle-2 review)
- Phase 2.5: delete dead-code `IaCProvider.Apply` impls on aws/gcp/azure (unreachable post-Phase-2 cutover; kept in Phase 2 to minimize blast radius)
- Phase 3: codemod-driven canonical-form bump for DO (was relevant pre-cycle-1; collapsed in cycle-1 — DO now also routes via v2 dispatch)
- Phase 5: remove `wfctlhelpers.ApplyPlan` (Phase 4 already shipped via #691; Phase 5 gates on Phase 2+3 being clean — now only Phase 2 dependency since Phase 3 collapsed)
- Per-action `output_keys` field on ActionResult (dropped per cycle-2 reviewer's Option — hook firing requires only action_index + status; per-resource outputs already in ApplyResult.resources)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat: workflow v0.54.0 — ApplyResult.Actions + ActionStatus enum + engine population | Task 1, Task 2, Task 3, Task 4, Task 5 | `feat/v2-phase2-grpc-contract` (in workflow) |
| 2 | feat: declare ComputePlanVersion v2 + bump workflow v0.54.0 pin; release v1.2.0 | Task 6 | `feat/v2-capabilities-v2` (in workflow-plugin-aws) |
| 3 | feat: declare ComputePlanVersion v2 + bump workflow v0.54.0 pin; release v1.2.0 | Task 7 | `feat/v2-capabilities-v2` (in workflow-plugin-gcp) |
| 4 | feat: declare ComputePlanVersion v2 + bump workflow v0.54.0 pin; release v1.2.0 | Task 8 | `feat/v2-capabilities-v2` (in workflow-plugin-azure) |
| 5 | feat: declare ComputePlanVersion v2 + bump workflow v0.54.0 pin; release v1.2.0 | Task 9 | `feat/v2-capabilities-v2` (in workflow-plugin-digitalocean) |

**Tasks 10 + 11 are non-PR coordination steps** (cross-plugin smoke verification + memory update) executed by team-lead after all 5 PRs ship — they don't create their own PR; they're operational close-out.

**Status:** Draft

---

## Pre-dispatch setup (team-lead, ONCE before any task starts)

Per the cloud-SDK plugin sweep precedent — team-lead actions BEFORE dispatching implementers:

1. Verify ADR 0024 + 0040 are still binding (read decisions/0024 + decisions/0040; confirm no override in flight).
2. **File `workflow#640-phase-2.1` follow-up tracking issue** (moved from Task 6 per cycle-1 plan-review I-2 — this is a team-lead action, not an implementer task; the tracking issue body references the Phase 2 PR which doesn't exist yet at implementer-time). Issue body: "Phase 2.1 follow-up to #640 Phase 2 (PRs land via Phase 2 cascade) — add manifest validation gate at cmd/wfctl/deploy_providers.go::findIaCPluginDir per Phase 1 Assumption 8. Three implementation options recorded in Phase 2 design doc (full pluginmanifest package; reuse existing schema/; lightweight computePlanVersion enum check). Pick at design time."

---

## Universal per-plugin pattern (PR 2-5 — applies to Tasks 6, 7, 8, 9)

For tasks 6-9, each follows the same 5-step pattern. Repo-specific details inline per task.

**Files per plugin PR:**
- Modify: `go.mod` (workflow pin v0.53.1 → v0.54.0)
- Modify: `go.sum` (auto)
- Modify: `plugin.json` (minEngineVersion → "0.54.0")
- Modify: `internal/server.go` OR equivalent gRPC handler file (add `ComputePlanVersion: "v2"` to CapabilitiesResponse)
- Tag: `v1.2.0`

**Step 1: Branch + ff-pull**

```bash
cd /Users/jon/workspace/<plugin-repo-name>
git fetch origin
git checkout -b feat/v2-capabilities-v2 origin/main
git pull --ff-only origin main
```

**Step 2: Bump workflow pin + declare ComputePlanVersion v2**

Edit `go.mod`:
```
require (
    github.com/GoCodeAlone/workflow v0.54.0   # was v0.53.1
    ...
)
```

Edit **`internal/iacserver.go`** (verified file location across all 4 plugins via cycle-1 plan-review). The Capabilities method signature is identical across all 4 plugins:

```go
// Existing pattern (verified per-plugin — receiver varies: awsIaCServer / gcpIaCServer / azureIaCServer / doIaCServer):
func (s *<plugin>IaCServer) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
    // ... existing IaCCapabilityDeclaration build into `out` ...
    return &pb.CapabilitiesResponse{Capabilities: out}, nil  // CURRENT
}
```

**Change ONLY the return-statement struct literal** to add ComputePlanVersion field:

```go
return &pb.CapabilitiesResponse{
    Capabilities:       out,
    ComputePlanVersion: "v2",  // NEW Phase 2: declare v2 dispatch per ADR 0040
}, nil
```

Do NOT rewrite the function body, receiver type, parameter types, or parameter names.

**Step 3: Tidy + build + test**

```bash
go mod tidy
go build ./...
go test ./... -race
```

Expected: all green.

**Step 4: Update plugin.json**

```json
{
  ...
  "minEngineVersion": "0.54.0",
  ...
}
```

**Step 5: Commit + PR + admin-merge + tag v1.2.0 + verify release**

```bash
git add go.mod go.sum plugin.json internal/iacserver.go
git commit -m "feat: declare ComputePlanVersion v2 + bump workflow v0.53.1 → v0.54.0; release v1.2.0"
git push -u origin feat/v2-capabilities-v2
gh pr create --base main --head feat/v2-capabilities-v2 \
  --title "feat: declare ComputePlanVersion v2 + bump workflow v0.54.0 pin; release v1.2.0" \
  --body "Phase 2 of workflow#640 hard-cutover per ADR 0024 + 0040. Plugin declares ComputePlanVersion=\"v2\" in CapabilitiesResponse → wfctl routes through wfctlhelpers.ApplyPlanWithHooks (v2 dispatch path) → engine populates ApplyResult.Actions per dispatch. Existing IaCProvider.Apply impl becomes dead code post-cutover (kept for Phase 2 minimization; Phase 2.5 cleanup may delete).

Coordinated with workflow PR (workflow v0.54.0 must be tagged + released BEFORE this plugin PR's go.mod bump resolves). Cascading rollback: if this plugin's v1.2.0 fails downstream consumer, cut v1.2.1 reverting workflow pin + Capabilities declaration."
```

After CI green + Copilot settle (~10 min) + admin-merge:

```bash
gh pr merge <N> --squash --admin --delete-branch
git checkout main && git pull
git tag v1.2.0
git push origin v1.2.0
gh release list --repo GoCodeAlone/<plugin-repo-name> --json tagName,isDraft,isLatest --limit 1
```

Defensive draft-edit if drafted: `gh release edit v1.2.0 --repo GoCodeAlone/<plugin-repo-name> --draft=false --latest`.

**Rollback (per-plugin):** if downstream consumer breaks, cut `v1.2.1` re-pinning workflow → v0.53.1 + reverting ComputePlanVersion="v2" → "". Per ADR 0040, plugins on v0.53.x pins permanently incompatible with workflow v0.54.0+; v1.2.1 rollback re-establishes v0.53.x compat.

---

## Tasks

### Task 1: workflow — extend iac.proto + REGENERATE iac.pb.go in same commit (cycle-1 I-1 bundled)

**Files:**
- Modify: `plugin/external/proto/iac.proto` (add ActionStatus enum + ActionResult message; add `repeated ActionResult actions = 7;` to ApplyResult)
- Modify: `plugin/external/proto/iac.pb.go` (regenerated; bundled in same commit per cycle-1 plan-review I-1 — splitting proto+regen creates broken intermediate commit that fails CI)

**Step 1: Edit iac.proto — add ActionStatus enum + ActionResult message**

Add the following BEFORE `message ApplyResult` (line 295):

```protobuf
// ActionStatus categorizes per-action outcomes for wfctl-side hook dispatch.
// Per workflow#640 Phase 2 + ADR 0040 invariants 1-2. Tags 4+5 reserved
// for Phase 2.3 ACTION_STATUS_COMPENSATED + ACTION_STATUS_COMPENSATION_FAILED
// when engine-side compensation lands.
enum ActionStatus {
  ACTION_STATUS_UNSPECIFIED = 0;  // wfctl REJECTS this on receipt
  ACTION_STATUS_SUCCESS = 1;
  ACTION_STATUS_ERROR = 2;
  ACTION_STATUS_DELETE_FAILED = 3;
  // 4 + 5 reserved (Phase 2.3 compensation)
}

// ActionResult is the per-action outcome surfacing for Phase 2 v2 hooks.
// Per ADR 0040 invariant 1. output_keys field DROPPED per cycle-2 review
// (hook firing only needs action_index + status; per-resource outputs
// already in ApplyResult.resources).
message ActionResult {
  uint32 action_index = 1;
  ActionStatus status = 2;
  string error = 3;
}
```

Modify ApplyResult (existing at line 295) to add the new field at tag 7:

```protobuf
message ApplyResult {
  string plan_id = 1;
  repeated ResourceOutput resources = 2;
  repeated ActionError errors = 3;
  map<string, string> initial_input_snapshot = 4;
  repeated DriftEntry input_drift_report = 5;
  map<string, string> replace_id_map = 6;
  repeated ActionResult actions = 7;  // NEW Phase 2 (workflow#640)
}
```

**Step 2: Verify proto syntax**

Run: `protoc --proto_path=plugin/external/proto --descriptor_set_out=/dev/null plugin/external/proto/iac.proto`
Expected: exit 0, no syntax errors.

**Step 3: Regenerate iac.pb.go (bundled in same commit per cycle-1 I-1)**

```bash
grep -rn 'protoc.*iac.proto\|//go:generate.*proto' Makefile scripts/ plugin/external/proto/ 2>&1 | head -5
# Run whichever regen command the repo uses (make proto / go generate / etc.)
make proto  # OR go generate ./plugin/external/proto/...
```

**Step 4: Verify generated symbols + build**

```bash
grep -n 'ActionStatus\|ActionResult\|GetActions' plugin/external/proto/iac.pb.go | head -10
GOWORK=off go build ./plugin/external/proto/...
```

Expected: lines matching `type ActionResult struct`, `type ApplyResult struct { ... Actions []*ActionResult ...`, `func (x *ApplyResult) GetActions() []*ActionResult`, enum constants for ActionStatus; build exit 0.

**Step 5: Commit (BUNDLED — proto + regen atomic)**

```bash
git add plugin/external/proto/iac.proto plugin/external/proto/iac.pb.go
git commit -m "feat(proto): extend ApplyResult with Actions field + ActionStatus enum; regenerate iac.pb.go (Phase 2)"
```

**Verification (internal-logic refactor + proto regen):** protoc clean; build clean; grep confirms new symbol presence.

**Rollback:** revert commit (regen + proto edit roll back together).

---

### Task 2: workflow — extend interfaces.ApplyResult + ActionOutcome + ActionStatus Go types

**Files:**
- Modify: `interfaces/iac.go` (add ActionStatus enum + ActionOutcome struct; extend ApplyResult.Actions field)

**Step 1: Edit interfaces/iac.go — add types**

Find the existing `type ApplyResult struct` and add Actions field. Add ActionStatus + ActionOutcome above:

```go
// ActionStatus mirrors pb.ActionStatus for type-safe Go boundary use.
// Per workflow#640 Phase 2 + ADR 0040.
type ActionStatus uint8

const (
    ActionStatusUnspecified  ActionStatus = iota
    ActionStatusSuccess
    ActionStatusError
    ActionStatusDeleteFailed
    // Future Phase 2.3: ActionStatusCompensated, ActionStatusCompensationFailed
)

// ActionOutcome mirrors pb.ActionResult.
type ActionOutcome struct {
    ActionIndex uint32
    Status      ActionStatus
    Error       string
}

// ApplyResult — Actions field added Phase 2.
type ApplyResult struct {
    PlanID               string
    Resources            []ResourceOutput
    Errors               []ActionError
    InitialInputSnapshot map[string]string
    InputDriftReport     []DriftEntry
    ReplaceIDMap         map[string]string
    Actions              []ActionOutcome  // NEW Phase 2
}
```

**Step 2: Build verify**

```bash
GOWORK=off go build ./interfaces/...
```

Expected: exit 0.

**Step 3: Commit**

```bash
git add interfaces/iac.go
git commit -m "feat(interfaces): add ActionStatus + ActionOutcome; extend ApplyResult.Actions (Phase 2)"
```

**Verification (internal-logic refactor class):** build green. No callers break (Actions field is new; existing callers ignore it until Task 5 populates).

**Rollback:** revert commit; remove Actions field + types.

---

### Task 3: workflow — extend applyResultFromPB to decode + reject UNSPECIFIED

**Files:**
- Modify: `cmd/wfctl/iac_typed_adapter.go:1177` (extend applyResultFromPB)

**Step 1: Write failing test**

Add to `cmd/wfctl/iac_typed_adapter_test.go`:

```go
func TestApplyResultFromPB_DecodesActions(t *testing.T) {
    pbResult := &pb.ApplyResult{
        PlanId: "plan-1",
        Actions: []*pb.ActionResult{
            {ActionIndex: 0, Status: pb.ActionStatus_ACTION_STATUS_SUCCESS},
            {ActionIndex: 1, Status: pb.ActionStatus_ACTION_STATUS_DELETE_FAILED, Error: "AWS API error"},
        },
    }
    got, err := applyResultFromPB(pbResult)
    if err != nil { t.Fatalf("err: %v", err) }
    if len(got.Actions) != 2 { t.Fatalf("expected 2 actions, got %d", len(got.Actions)) }
    if got.Actions[0].Status != interfaces.ActionStatusSuccess { t.Errorf("action 0: want Success, got %v", got.Actions[0].Status) }
    if got.Actions[1].Status != interfaces.ActionStatusDeleteFailed { t.Errorf("action 1: want DeleteFailed, got %v", got.Actions[1].Status) }
    if got.Actions[1].Error != "AWS API error" { t.Errorf("action 1 error mismatch") }
}

func TestApplyResultFromPB_RejectsUNSPECIFIED(t *testing.T) {
    pbResult := &pb.ApplyResult{
        Actions: []*pb.ActionResult{
            {ActionIndex: 0, Status: pb.ActionStatus_ACTION_STATUS_UNSPECIFIED},
        },
    }
    _, err := applyResultFromPB(pbResult)
    if err == nil { t.Fatal("expected error on UNSPECIFIED status, got nil") }
    if !strings.Contains(err.Error(), "UNSPECIFIED") { t.Errorf("error should mention UNSPECIFIED: %v", err) }
}
```

**Step 2: Run test → expected FAIL**

```bash
GOWORK=off go test ./cmd/wfctl/ -run 'TestApplyResultFromPB_DecodesActions|TestApplyResultFromPB_RejectsUNSPECIFIED' -v
```

Expected: FAIL — `got.Actions` is empty / nil.

**Step 3: Implement**

In `cmd/wfctl/iac_typed_adapter.go`, find `func applyResultFromPB(r *pb.ApplyResult) (*interfaces.ApplyResult, error)` (line 1177). Add after existing field decoding, before `return result, nil`:

```go
// Phase 2: decode per-action outcomes (workflow#640).
actions := make([]interfaces.ActionOutcome, 0, len(r.GetActions()))
for _, a := range r.GetActions() {
    if a.GetStatus() == pb.ActionStatus_ACTION_STATUS_UNSPECIFIED {
        return nil, fmt.Errorf("plugin returned ActionResult with UNSPECIFIED status at action_index=%d (Phase 2 contract violation per ADR 0041)", a.GetActionIndex())
    }
    actions = append(actions, interfaces.ActionOutcome{
        ActionIndex: a.GetActionIndex(),
        Status:      mapPBActionStatusToInterface(a.GetStatus()),
        Error:       a.GetError(),
    })
}
result.Actions = actions
```

Add helper at file end:

```go
func mapPBActionStatusToInterface(s pb.ActionStatus) interfaces.ActionStatus {
    switch s {
    case pb.ActionStatus_ACTION_STATUS_SUCCESS:
        return interfaces.ActionStatusSuccess
    case pb.ActionStatus_ACTION_STATUS_ERROR:
        return interfaces.ActionStatusError
    case pb.ActionStatus_ACTION_STATUS_DELETE_FAILED:
        return interfaces.ActionStatusDeleteFailed
    default:
        return interfaces.ActionStatusUnspecified
    }
}
```

**Step 4: Run tests → expected PASS**

```bash
GOWORK=off go test ./cmd/wfctl/ -run 'TestApplyResultFromPB_DecodesActions|TestApplyResultFromPB_RejectsUNSPECIFIED' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/iac_typed_adapter.go cmd/wfctl/iac_typed_adapter_test.go
git commit -m "feat(wfctl): applyResultFromPB decodes ActionResult; rejects UNSPECIFIED (Phase 2)"
```

**Verification (internal-logic refactor):** tests PASS; existing applyResultFromPB tests still PASS (no regression on existing fields).

**Rollback:** revert commit.

---

### Task 4: workflow — engine-side ApplyPlanWithHooks populates result.Actions + length-validation assert

**Files:**
- Modify: `iac/wfctlhelpers/apply.go:118` (applyPlanWithEnvProviderAndHooks — add Actions append per dispatch + post-loop length assert)
- Modify: `iac/wfctlhelpers/apply_hooks_test.go` (add test verifying Actions populated)

**Step 1: Write failing tests**

Add to `iac/wfctlhelpers/apply_hooks_test.go` — use the REAL fakeProvider API (verified via grep in cycle-1 plan-review C-2: `type fakeProvider struct { driver *fakeDriver; driverErr error }`; `newFakeProvider() *fakeProvider`; single driver, NOT drivers map):

```go
func TestApplyPlanWithHooks_PopulatesActions_CleanSuccess(t *testing.T) {
    p := newFakeProvider()  // single-driver fakeProvider from apply_test.go
    plan := &interfaces.IaCPlan{
        ID: "plan-1",
        Actions: []interfaces.PlanAction{
            {Resource: interfaces.ResourceRef{Type: "test.resource", Name: "r1"}, Action: "create"},
            {Resource: interfaces.ResourceRef{Type: "test.resource", Name: "r2"}, Action: "create"},
        },
    }
    result, err := ApplyPlanWithHooks(context.Background(), p, plan, ApplyPlanHooks{})
    if err != nil { t.Fatalf("err: %v", err) }
    if len(result.Actions) != 2 { t.Fatalf("expected 2 ActionOutcomes, got %d", len(result.Actions)) }
    for i, a := range result.Actions {
        if a.ActionIndex != uint32(i) { t.Errorf("action %d index mismatch: %d", i, a.ActionIndex) }
        if a.Status != interfaces.ActionStatusSuccess { t.Errorf("action %d status: %v", i, a.Status) }
    }
}

// CRITICAL test per cycle-1 plan-review C-1: pre-dispatch continue paths
// (driver-resolve error at apply.go:234) MUST still produce ActionOutcome
// entries so the post-loop length assert doesn't false-fail.
func TestApplyPlanWithHooks_PopulatesActions_PreDispatchDriverError(t *testing.T) {
    p := &fakeProvider{driverErr: errors.New("driver resolution failed")}
    plan := &interfaces.IaCPlan{
        ID: "plan-1",
        Actions: []interfaces.PlanAction{
            {Resource: interfaces.ResourceRef{Type: "unknown.resource", Name: "r1"}, Action: "create"},
        },
    }
    result, err := ApplyPlanWithHooks(context.Background(), p, plan, ApplyPlanHooks{})
    // Expect best-effort: no top-level error; result.Actions has 1 entry with
    // Status==Error (since driver resolution failed pre-dispatch).
    if err != nil { t.Fatalf("expected no top-level err on driver-resolve failure, got: %v", err) }
    if len(result.Actions) != 1 { t.Fatalf("expected 1 ActionOutcome (length-assert invariant), got %d", len(result.Actions)) }
    if result.Actions[0].Status != interfaces.ActionStatusError {
        t.Errorf("driver-resolve-error action status: want Error, got %v", result.Actions[0].Status)
    }
}
```

**Step 2: Run → expected FAIL**

```bash
GOWORK=off go test ./iac/wfctlhelpers/ -run 'TestApplyPlanWithHooks_PopulatesActions' -v
```

Expected: FAIL — `result.Actions` empty (both tests fail).

**Step 3: Implement engine-side population in applyPlanWithEnvProviderAndHooks (covers ALL continue paths per cycle-1 plan-review C-1)**

In `iac/wfctlhelpers/apply.go`, the dispatch loop has multiple `continue` exits (verified: lines 224 jit-error, 234 driver-resolve-error, 261 dispatchAction-error, 287, 313). **EVERY continue path must append an ActionOutcome** OR the post-loop length-assert false-fails on legitimate plans with errors.

Cleanest implementation: deferred closure inside the loop body that records the ActionOutcome on every exit path:

```go
for i := range plan.Actions {
    action := plan.Actions[i]
    var dispatchErr error
    var loopErr error // captures the actual error of this iteration

    // Deferred closure: runs on EVERY exit from this iteration (continue or fall-through).
    // Guarantees 1-to-1 correspondence between plan.Actions and result.Actions
    // regardless of which continue/branch the code took.
    func() {
        defer func() {
            status := mapDispatchErrToStatus(loopErr, action.Action)
            errStr := ""
            if loopErr != nil { errStr = loopErr.Error() }
            result.Actions = append(result.Actions, interfaces.ActionOutcome{
                ActionIndex: uint32(i),
                Status:      status,
                Error:       errStr,
            })
        }()

        // ctx cancellation check
        if err := ctx.Err(); err != nil { loopErr = err; return }

        // Existing JIT substitution at apply.go:217
        resolved, err := jitsubst.ResolveSpec(action.Resource, result.ReplaceIDMap, syncedOutputs, os.LookupEnv)
        if err != nil {
            // ... existing result.Errors append for JIT error ...
            loopErr = fmt.Errorf("jit substitution: %w", err)
            return
        }

        // Existing driver resolution at apply.go:228
        d, err := p.ResourceDriver(action.Resource.Type)
        if err != nil {
            // ... existing result.Errors append for driver-resolve error ...
            loopErr = err
            return
        }

        // Existing dispatchAction call at apply.go:251
        if err := dispatchAction(ctx, d, resolved, result, actionHooks, deleteHookActive); err != nil {
            // ... existing result.Errors handling ...
            loopErr = err
            return
        }
        // Success path — loopErr stays nil; deferred closure records ActionStatusSuccess.
    }()
}
```

The implementer should RESTRUCTURE the existing loop body to fit this shape — the deferred closure pattern preserves the existing best-effort continue-on-error semantics while guaranteeing the ActionOutcome append on every path.

Add helper at file end:

```go
// mapDispatchErrToStatus returns the ActionStatus per workflow#640 Phase 2.
// Compensation paths (Phase 2.3) reserved; today only 3 reachable statuses.
func mapDispatchErrToStatus(err error, actionType string) interfaces.ActionStatus {
    if err == nil {
        return interfaces.ActionStatusSuccess
    }
    if actionType == "delete" {
        return interfaces.ActionStatusDeleteFailed
    }
    return interfaces.ActionStatusError
}
```

After the dispatch loop completes (just before `return result, nil` at function end), add the length-validation assert:

```go
// Engine-side invariant: len(result.Actions) must equal len(plan.Actions).
// Per workflow#640 Phase 2 + ADR 0041 (length validation lives engine-side,
// not in applyResultFromPB which is on v1 dispatch path).
if len(result.Actions) != len(plan.Actions) {
    return result, fmt.Errorf("internal: ApplyPlanWithHooks produced %d ActionOutcomes for %d plan actions (engine invariant violation)", len(result.Actions), len(plan.Actions))
}
```

**Step 4: Run tests → expected PASS**

```bash
GOWORK=off go test ./iac/wfctlhelpers/ -race
```

Expected: all PASS, including the new test + existing tests (no regression).

**Step 5: Commit**

```bash
git add iac/wfctlhelpers/apply.go iac/wfctlhelpers/apply_hooks_test.go
git commit -m "feat(engine): applyPlanWithEnvProviderAndHooks populates result.Actions + length assert (Phase 2)"
```

**Verification (internal-logic refactor):** tests PASS; race-detector clean.

**Rollback:** revert commit.

---

### Task 5: workflow — cut v0.54.0 tag from main HEAD (team-lead action post-PR1 merge)

**Files:** none (git tag only)

**Step 1: Verify PR 1 merged + Tasks 1-6 all complete**

```bash
cd /Users/jon/workspace/workflow
git fetch origin
git log origin/main --oneline -8 | grep -E 'Phase 2|ApplyResult.Actions|ActionStatus'
```

Expected: 5+ commits from Tasks 1-5 visible (Task 6 is a GitHub issue, no commit; Task 7 is the tag).

**Step 2: Tag v0.54.0 from main HEAD**

```bash
git checkout main && git pull
git tag v0.54.0
git push origin v0.54.0
```

**Step 3: Verify release publishes**

```bash
sleep 60
gh release list --repo GoCodeAlone/workflow --json tagName,isDraft,isLatest --limit 3
```

Expected: v0.54.0 in list. Defensive draft-edit if drafted: `gh release edit v0.54.0 --repo GoCodeAlone/workflow --draft=false --latest`.

**Verification (build pipeline + version pin update class — runtime-launch-validation triggered):**
- `gh release view v0.54.0 --repo GoCodeAlone/workflow --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`
- Plugin PRs (Tasks 8-11) can now resolve workflow v0.54.0 via Go proxy (proxy reads git tags immediately)

**Rollback:** if v0.54.0 has critical issue, cut v0.54.1 reverting whichever commit broke; old v0.54.0 tag stays in Go proxy (immutable) but `latest` resolves to v0.54.1. Per ADR 0040 matched-pair rollback: if revert needed before plugins migrate, plugins stay on v0.53.x pin.

---

### Task 6: workflow-plugin-aws — declare ComputePlanVersion v2; release v1.2.0

**Repo:** `/Users/jon/workspace/workflow-plugin-aws`

Apply the **Universal per-plugin pattern** at top of plan.

**Files specifics:**
- `internal/server.go` OR equivalent (grep `func.*Capabilities` to locate) — add `ComputePlanVersion: "v2"`

**Verification:**
- `go build ./... && go test ./... -race` PASS
- Post-release: `gh release view v1.2.0 --repo GoCodeAlone/workflow-plugin-aws --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`

**Rollback:** cut v1.2.1 re-pinning workflow → v0.53.1 + removing ComputePlanVersion="v2".

---

### Task 7: workflow-plugin-gcp — declare ComputePlanVersion v2; release v1.2.0

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp`

Apply the **Universal per-plugin pattern**. Same as Task 8. Tag v1.2.0.

**Rollback:** v1.2.1 re-pin v0.53.1.

---

### Task 8: workflow-plugin-azure — declare ComputePlanVersion v2; release v1.2.0

**Repo:** `/Users/jon/workspace/workflow-plugin-azure`

Apply the **Universal per-plugin pattern**. Same as Task 8. Tag v1.2.0.

**Note:** azure's release.yml uses `[self-hosted, Linux, X64]` runner (intentional infra per sweep precedent); confirm runners online before tag push: `gh api /orgs/GoCodeAlone/actions/runners --jq '.runners | map(select(.status=="online")) | length'` → ≥1.

**Rollback:** v1.2.1 re-pin v0.53.1.

---

### Task 9: workflow-plugin-digitalocean — declare ComputePlanVersion v2; release v1.2.0

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean`

Apply the **Universal per-plugin pattern**. Same as Task 8. Tag v1.2.0.

**Note:** DO already routes through wfctlhelpers.ApplyPlan via canonical delegate pattern, but Phase 2 still requires the ComputePlanVersion="v2" Capabilities declaration to flag the v2 dispatch path explicitly. DO's existing DOProvider.Apply impl becomes equally dead post-cutover (wfctl skips provider.Apply → dispatches via provider.ResourceDriver instead).

**Rollback:** v1.2.1 re-pin v0.53.1.

---

### Task 10: cross-plugin smoke verification (team-lead, post all 5 PRs merged)

**Files:** none (operational verification)

**Step 1: install each v1.2.0 plugin against wfctl v0.54.0**

For each of aws/gcp/azure/DO:

```bash
wfctl plugin install github.com/GoCodeAlone/workflow-plugin-<name>@v1.2.0
```

Expected: each install succeeds; binary cached locally.

**Step 2: verify v2 dispatch path taken**

Run a representative apply for each plugin against a known IaC config. Verify via debug logging or wfctl flag that:
- wfctl chose v2 dispatch (calls `applyV2ApplyPlanWithHooksFn`, NOT `provider.Apply(ctx, &plan)`)
- ApplyResult.Actions populated with len == len(plan.Actions)
- For successful applies, all ActionOutcomes have Status == ActionStatusSuccess
- For applies with delete actions that fail, the failed delete shows Status == ActionStatusDeleteFailed (test fixture if real apply doesn't exercise this)

**Step 3: capture transcript**

Save dispatch decision + Actions array per plugin to `docs/runtime-validation/2026-05-16-phase2-cross-plugin-smoke.md`:

```bash
mkdir -p docs/runtime-validation
# Capture commands + output per plugin into the file
git add docs/runtime-validation/2026-05-16-phase2-cross-plugin-smoke.md
git commit -m "docs: Phase 2 cross-plugin smoke validation transcript"
```

**Verification (operator-run; NOT a CI gate per design Testing section):**
- 4 plugins all show v2 dispatch + populated Actions
- Smoke doc committed for audit trail

**Rollback (if smoke fails for plugin X):** cut plugin X v1.2.1 hotfix; OR if workflow-side bug, cut workflow v0.54.1 reverting whichever commit broke. Per ADR 0040 matched-pair rollback.

---

### Task 11: memory update + close Phase 2 + flag followups (team-lead)

**Files:**
- Modify: `/Users/jon/.claude/projects/-Users-jon-workspace/memory/project_cloud_sdk_extraction_complete.md`
- Modify: `/Users/jon/.claude/projects/-Users-jon-workspace/memory/MEMORY.md`

**Step 1: Update completion file**

Append to `project_cloud_sdk_extraction_complete.md`'s #640 entry: "Phase 2 SHIPPED 2026-05-16/17 — workflow v0.54.0 + aws v1.2.0 + gcp v1.2.0 + azure v1.2.0 + DO v1.2.0 coordinated cascade per ADR 0024 + 0040. All 4 plugins declare ComputePlanVersion='v2'; wfctl routes through v2 dispatch path; ApplyResult.Actions populated per dispatch + length-validation assert engine-side. Followups: Phase 2.1 (manifest validation gate — tracking issue filed in Task 6); Phase 2.3 (compensation enum values + engine logic); Phase 2.5 (delete dead-code IaCProvider.Apply impls on aws/gcp/azure); Phase 5 (remove wfctlhelpers.ApplyPlan — gates only on Phase 2 now that Phase 3 collapsed in Phase 2 cycle-1)."

**Step 2: Update MEMORY.md index entry**

Update the Cloud-SDK Extraction completion line to include Phase 2 ✓.

**Step 3: Commit memory updates**

(Memory files are operator-side, not in git; just save the edits.)

**Verification (documentation class):** memory files updated; followup tracking issue (Task 6) referenced from completion entry.

**Rollback:** revert memory edits if Phase 2 itself reverts.

---

## Out of scope (per design — separate future passes)

- Phase 2.1: manifest validation gate at deploy_providers.go (tracked via Task 6 GitHub issue)
- Phase 2.3: engine-side compensation logic + ACTION_STATUS_COMPENSATED/COMPENSATION_FAILED enum value emission (tags 4+5 reserved in Phase 2 proto comment)
- Phase 2.5: delete dead-code IaCProvider.Apply impls on aws/gcp/azure (unreachable post-Phase-2 cutover; kept in Phase 2 to minimize blast radius)
- Phase 3: codemod-driven canonical-form bump for DO (cycle-1 collapsed this — DO now also routes via v2 dispatch)
- Phase 5: remove wfctlhelpers.ApplyPlan (gates only on Phase 2)
- Per-action `output_keys` field on ActionResult (dropped per cycle-2 — hook firing only needs action_index + status)
