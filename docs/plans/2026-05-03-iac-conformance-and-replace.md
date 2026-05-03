# IaC Root-Cause Fixes + Provider Conformance Suite Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor wfctl IaC semantics to honor `NeedsReplace`, add per-module JIT secret resolution, ship a provider conformance suite + DO-targeted codemod, and migrate the DO plugin to the new contract — closing the 8 root-cause gaps surfaced by core-dump's self-hosted-PG deploy iteration.

**Architecture:** Per-plugin `computePlanVersion` opt-in (DO migrates to v2; AWS/GCP/Azure stay v1 until activated). New `wfctlhelpers.ApplyPlan` shared helper handles all 4 plan actions (create/update/replace/delete) with `upsertSupporter` hook + `ApplyResult.ReplaceIDMap` for cascade ID propagation. Conformance suite at `iac/conformance/` runs scenarios against any `IaCProvider` implementation; DO smoke gate runs on every relevant PR.

**Tech Stack:** Go 1.23+, `golang.org/x/tools/go/analysis/passes` for codemod, `google.golang.org/protobuf` for plan.json, GitHub Actions for CI.

**Base branch:** main (workflow), main (workflow-plugin-digitalocean), main (core-dump)

---

## Scope Manifest

**PR Count:** 12
**Tasks:** 71 (counting T3.6a–f as 6 separate tasks and T7.2–T7.12 as 11 scenario tasks; rev5 net: rev3=72; +T3.1.5 (formerly T3.0.5; T1.5 split into W-1 portion + W-3a portion); +T3.0.4 (NEW field declarations); −T9.1 + −T9.2 (rev4 W-9 ProviderPlanner drop); = 71)

**Revision history:**
- rev1 (commit 9ea390b) — initial draft, FAILED plan-phase adversarial review (3 Critical + 7 Important + 5 Minor)
- rev2 (commit b29b297) — addressed cycle-1 findings; FAILED cycle-2 review with 3 new Critical + 7 Important
- rev3 (this revision) — addresses cycle-2 findings (user override unlocked unbounded revision per "keep up autonomously … take as many review/correction cycles as needed"). Specifically:
  - **W-3 split into W-3a (foundation: T3.0, T3.1–T3.5) + W-3b (refactor + dispatch: T3.6a–f, T3.7, T3.9, T3.10)** — addresses 16-task PR-size finding; W-3a ships zero runtime change so reviewer cost is bounded.
  - **T3.6a no longer creates a skipped TDD harness** — sub-task only changes the function signature + updates in-package tests. The new failing TDD test (Replace emission) is co-committed with the implementation in T3.6e (clean red→green in one commit). No `t.Skip("dispatch lands later")` ever lands in main.
  - **W-9 sequenced strictly after W-4** in the dependency graph — both modify `interfaces/iac_provider.go` (W-4 adds `ProviderValidator`, W-9 adds `ProviderPlanner`); strict ordering eliminates the merge-conflict race.
  - **W-1 ↔ W-5 file overlap on `cmd/wfctl/infra.go::runInfraPlan` documented explicitly**; W-5 must rebase on W-1's tip before applying.
  - **T1.5 in-process apply path replaced with typed-error postcondition design**: new sentinel `iac/inputsnapshot.ErrEnvVarChanged`; unconditional postcondition re-fingerprint at end-of-apply (drift detection independent of error path); new TDD test `TestApply_InProcess_PlanStaleDiagnostic_NamesChangedKeys` exercises the STAGING_PG_PASSWORD-style fixture matching the design's motivating bug.
  - **T3.6b `--no-provider` escape hatch dropped**; failure to load plugin at plan time fails loudly with explicit error.
  - **TP1 codemod-report attached as GitHub Actions artifact** (90-day retention, established precedent), with ADR cross-reference if any non-trivial migration decision falls out.
  - **T7.14 leak scrubber dedups by checking-then-commenting on existing open `conformance-leak-incident` issue**; new issue created only if none open.
  - **TC1.5 dry-run pinned to `wfctl-conformance@gocodealone.dev`** with pre-flight budget check; conformance budget cap bumped from $5/mo to $25/mo to accommodate ~$1 per ad-hoc dry-run.
  - **T9.3 commit message corrected** to "against this PR's workflow head" + YAML comment explains replace direction.
  - **T7.13 budget check cached per-PR-base-SHA** via Actions concurrency-group; calibration plan documented in conformance runbook (revisit after 30 days).
  - **T9.1 ProviderPlanner downgraded to "interface definition only"** with explicit `// Reserved for future Tofu/Pulumi-style adapter; not consumed by core wfctl in this PR" doc; no claim that `ComputePlan` / `ApplyPlan` delegate to it.
  - Minor cleanups: Task Conventions stub-language reworded; `WFCTL_DIFFCACHE` CI behavior pinned (CI workflows in this repo set `:memory:`); T2.7 picks one literal stderr line; T8.3/T8.4 marker notes added; T2.2 doc-verification line added.
- rev4 (this revision) — addresses cycle-3 findings:
  - **T1.5 split into T1.5 (W-1) + T3.0.5 (W-3a)** — eliminates the rev3 ghost-stub anti-pattern. T1.5 (W-1) ships only the typed-error sentinel + persisted-`--plan` path (no helper-package dependency). T3.0.5 (W-3a) ships the in-process postcondition wiring as the FIRST task in W-3a after T3.0, depending on T3.1's `wfctlhelpers.ApplyPlan` body. No stub `wfctlhelpers.ApplyPlan` ever ships in W-1.
  - **`plan.InputNames` references corrected to derive from `plan.InputSnapshot` map keys** — rev3's pseudo-code was uncompilable; rev4 uses `keys(plan.InputSnapshot)` consistently.
  - **W-9's ProviderPlanner interface DROPPED entirely** per cycle-3 YAGNI finding. W-9 now only ships T9.3 (cross-plugin-build CI gate) + brief docs. ProviderPlanner ships when the first concrete consumer arrives (a future Tofu/Pulumi adapter design). T9.1 + T9.2 removed.
  - **Field-add sites corrected (rev4 intent; rev5 implementation moved fields to W-3a)**: see rev5 changelog for the actual field-declaration site (T1.1 declares `IaCPlan` + `PlanAction` fields + `DriftEntry` only; T3.0.4 declares `ApplyResult.InitialInputSnapshot` + `InputDriftReport`; T3.4 declares + populates `ApplyResult.ReplaceIDMap`).
  - **W-9 → P-DO edge dropped from sequencing rule #8**; rule #4 keeps `W-3b → W-4 → W-9` only. P-DO can draft after W-7 + W-8 with no W-9 dep (T9.3's CI gate runs on every plugin PR, not as a P-DO dep).
  - **Third-party action `marocchino/sticky-pull-request-comment@v2` replaced with `actions/github-script@v7`** in TP1 codemod-report.yml — keeps PR-comment behavior with first-party supply chain.
  - **T7.14 dedup helper now requires BOTH `conformance-leak-incident` AND `auto-filed-leak` labels** — guards against operator-filed issues with the same primary label.
  - **T1.5 + T3.0.5 deferred postcondition wrapped in `recover()`** — on panic, set `result.InputDriftReport = nil` + log warning. New TDD test `TestApply_Postcondition_PanicDoesNotCorruptResult`.
  - **TC1.5 budget cap `$25/mo` gains second-channel alert at `$15/mo`** (Slack + GitHub issue dedup'd), with operator signoff (`jon@langevin.me`) recorded in `docs/conformance-runbook.md` § "Budget approval".
  - **T7.13 cache backend specified**: `actions/cache@v4` with key `budget-${{ github.event.pull_request.base.sha }}` — restores 1h TTL across runner ephemeral filesystems.
  - **W-3b sub-task ordering documented**: T3.6e (binding TDD test+impl) MUST land before T3.6c/d are considered "complete coverage"; cherry-pick rule documented inline.
  - **T9.3 trigger gains `paths` filter** — runs only on IaC-touching PRs, saves ~5min on doc-only PRs.
  - **T9.3 commit message + rollback note minor wording fixed** per cycle-3 minor finding.
- rev5 (this revision) — addresses cycle-4 findings:
  - **Critical fixes:**
    - `envProviderTolerantOfUnset` invocation bug fixed: factory is now CALLED (`envProviderTolerantOfUnset(plan.InputSnapshot)`) returning the closure. New `inputsnapshot.PreservedFingerprint` constant added to `iac/inputsnapshot/snapshot.go` (T1.2). `ComputeDrift` (T1.5) special-cases keys whose applySnap fingerprint equals `PreservedFingerprint` (skips drift detection). Cross-function contract documented at all 3 call sites (T1.2 closure, T1.5 ComputeDrift, T3.1.5 postcondition).
    - rev4 changelog line 41 contradiction with task content fixed. The actual rev5 implementation: T1.1 declares only IaCPlan + PlanAction fields + the `DriftEntry` type. ApplyResult field additions move to W-3a: T3.0.4 (NEW) declares `InitialInputSnapshot` + `InputDriftReport`; T3.4 declares `ReplaceIDMap` (in same commit that populates it). No "field declared in W-1 but populated in W-3a" surface area.
    - **T3.0.5 renamed to T3.1.5** — dot-suffix-after-its-prefix convention restored. T3.1.5 sits between T3.1 and T3.2 both numerically and sequentially. T3.0.4 is the new field-declaration task that ships BETWEEN T3.0 and T3.1 (numerically AND sequentially).
  - **Important fixes:**
    - `replaceIDMap` → `ReplaceIDMap` swept everywhere except function-parameter names (where lowercase is conventional). Verified via `grep "\\breplaceIDMap\\b" docs/plans/...`.
    - `actions/github-script` invocation gains `github.paginate(github.rest.issues.listComments, {...})` for >30-comment PRs; pinned to commit SHA per workflow security policy (even first-party actions).
    - T7.13 cache key fixed: discrete `id: hour` step added before the cache step; cache key references `steps.hour.outputs.value` correctly.
    - ApplyResult field additions moved out of W-1 into W-3a (Option 1 from cycle-4 review) — eliminates the "declared early, populated later" surface area concern.
    - ADR added: `decisions/0001-providerplanner-deferred-to-first-consumer.md` records the cycle-4 reasoning for dropping W-9's ProviderPlanner. Cited from the W-9 section.
    - T7.14 BOTH-labels dedup gains operator runbook note: do not remove `auto-filed-leak` label during triage; close the issue instead.
    - T7.13 YAML gains inline comment explaining `actions/cache@v4` post-step write-back semantics.
  - **Minor fixes:**
    - rev4 changelog grouped by Critical/Important/Minor (this revision).
    - T3.1.5 (formerly T3.0.5) intro cross-references T1.5 (W-1) for the persisted-plan-path counterpart.
    - T3.6e rollback wording de-magicked: explains commit-stack semantics instead of "PERSISTS across the revert."
    - Scope manifest task count recomputed: 71 tasks (rev3 was 72; rev4 split T1.5 into T1.5+T3.0.5 = +1; W-9 dropped T9.1+T9.2 = -2; rev5 adds T3.0.4 = +1; net: 72-1=71).
    - Revision history count format unified ("3C+7I+5M" shorthand).
    - All `T9.3` references swept to `T9.1` (rev4 renumber).
- rev6 (this revision) — addresses cycle-5 findings:
  - **Critical fixes:**
    - **ADR relocated to `docs/adr/006-providerplanner-deferred-to-first-consumer.md`** with repo-native Nygard-3 format (`# ADR NNN: Title` + `## Status` / `## Context` / `## Decision` / `## Consequences`). Verified: `docs/adr/` exists in workflow repo with 5 prior ADRs (`001-yaml-driven-config.md` through `005-field-contracts.md`); the next free number is `006`. The rev5 location (`decisions/0001-...`) was the `recording-decisions` skill default and conflicted with repo precedent.
    - **ADR `Decided by` attribution corrected to honest provenance**: `Decided by: Claude (autonomous-pipeline rev3-rev6; user did not explicitly approve — open question, see plan §W-9 § "Pending user ratification")`. Added `Status: Provisional — awaiting user ratification` until the user explicitly approves or rejects on next interaction. The cycle-5 reviewer correctly caught that rev5 ghost-wrote the user's name on a contested decision.
    - **T1.1 pre-step verification removed; verified at plan-revision time.** Verified `grep -rn "type ApplyResult\b" interfaces/` returns `interfaces/iac_state.go:65: type ApplyResult struct {` — the assumption holds. T1.1, T3.0.4, T3.4 all correctly target `interfaces/iac_state.go`. The runtime pre-step in T1.1 was a verification-class mismatch (plan-author-time fact deferred to implementer); rev6 removes it.
  - **Important fixes:**
    - **All 3 ApplyResult fields consolidated into T3.0.4** (cycle-5 Option 4): `InitialInputSnapshot`, `InputDriftReport`, `ReplaceIDMap`. T3.4 only POPULATES `ReplaceIDMap` (no separate field-add step + no separate `interfaces/iac_state.go` Modify). Single commit modifies `interfaces/iac_state.go` in W-3a (T3.0.4); cleaner Git history; no rebase fragility.
    - **`PreservedFingerprint` unexported to `preservedFingerprint`**; `NewTolerantEnvProvider` is the only sanctioned access path. External callers cannot inject the sentinel to bypass drift detection. T1.2's `Compute` and T1.5's `ComputeDrift` both reference the unexported constant via in-package access. Test changes: `TestCompute_PreservesSentinel` becomes a same-package test.
    - **ComputeDrift sentinel-honoring branch test added to T1.5** (`compute_drift_test.go::TestComputeDrift_PreservedSentinelSkipsDrift`) — closes the cross-PR test gap (cycle-5 Important on the branch first being tested in T3.1.5 in W-3a).
    - **T9.1 commit boundary made explicit**: ADR + cross-plugin-build workflow ship in ONE commit (`ci(iac): cross-plugin build gate + ADR for ProviderPlanner deferral`). Single PR W-9, single commit, both artifacts atomic.
    - **W-3a rebase strategy documented**: T3.0.4 is the ONLY commit in W-3a that modifies `interfaces/iac_state.go` (rev6 consolidation eliminates T3.4's separate Modify); `git rebase -i` during force-push review uses `pick` to preserve commit boundaries.
    - **Smoke-gate cost corrected from $0.005/PR to $0.0005/PR** (10x error propagated through cycles 1-5; recomputed: $4/mo Droplet ÷ 30d ÷ 24h ÷ 60m × 5min = $0.000463 ≈ $0.0005). Monthly-burn estimate recomputed: 600 PRs × $0.0005 = $0.30/mo (was $3/mo). Soft-alert threshold ($15) and hard-stop ($25) remain reasonable headroom for ad-hoc dry-runs.
  - **Minor fixes:**
    - rev4 changelog block converted to grouped Critical/Important/Minor format (matches rev5/rev6 convention).
    - `rev6 (if needed)` placeholder removed (no pre-emptive ghost-writing).
    - T3.4 commit message trimmed to ≤72 chars: `feat(iac): doReplace populates ApplyResult.ReplaceIDMap`.
    - T7.13 cache YAML inline comment specifies the cache path explicitly.
    - "T9.3 references swept" wording clarified to "active T9.3 references swept; historical changelog references retained".
- rev7 (if cycle-6 review surfaces findings) — further revisions per cycle-6 review.

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
| 2 | W-2 wfctl infra refresh-outputs + cheap apply-time refresh | T2.1, T2.2, T2.3, T2.5, T2.6, T2.7 | feat/iac-refresh-outputs |
| 3 | **W-3a** Replace foundation: manifest field + helper package + drift postcondition + diff cache (no runtime change for v1 plugins) | T3.0, T3.0.4, T3.1, T3.1.5, T3.2 – T3.5 | feat/iac-replace-foundation |
| 4 | **W-3b** Replace dispatch: ComputePlan refactor + apply-path branching + runtime validation | T3.6a – T3.6f, T3.7, T3.9, T3.10 | feat/iac-replace-dispatch |
| 5 | W-4 Provider.ValidatePlan + R-A10 align rule | T4.1, T4.2, T4.4, T4.5 | feat/iac-validate-plan |
| 6 | W-5 Per-module JIT secret resolution + ProviderID propagation | T5.1, T5.2, T5.3, T5.4, T5.5, T5.7 | feat/iac-jit-secrets |
| 7 | W-6 --allow-replace flag + partial-cascade discovery | T6.1, T6.2, T6.4 | feat/iac-allow-replace |
| 8 | W-7 iac/conformance/ package + DO smoke gate | T7.1 – T7.14 | feat/iac-conformance-suite |
| 9 | W-8 cmd/iac-codemod/ tooling | T8.1 – T8.7 | feat/iac-codemod |
| 10 | W-9 Cross-plugin build verification (CI gate only) | T9.1 | feat/iac-cross-plugin-build |
| 11 | P-DO DigitalOcean plugin migration to v2 | TP1 – TP5 | feat/iac-v2-migration |
| 12 | C-1 core-dump staging-PG cutover to nyc1 | TC1, TC1.5, TC2 | fix/staging-pg-nyc1-cutover |

**Status:** Draft

---

## Task Conventions

- Each task lists **Files**, **Steps**, **Verification**, and (for runtime-affecting tasks) a **Rollback** note.
- TDD: Step 1 = write failing test; Step 2 = verify failure; Step 3 = implement; Step 4 = verify pass; Step 5 = commit.
- Commit messages follow conventional commits (`feat(iac): ...`, `fix(plugin): ...`).
- Conformance scenarios are referenced by name in earlier PRs' verification sections only. The actual scenario stub + body both ship in W-7 (no ghost-stub Go file ships in any earlier PR). This is the rev2/rev3 fix for the stub-then-fill anti-pattern.
- All workflow tasks run from `/Users/jon/workspace/workflow/_worktrees/<branch>/`. P-DO from `/Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/<branch>/`. C-1 from `/Users/jon/workspace/core-dump/_worktrees/<branch>/`.

---

## PR W-1: IaCPlan schema + plan-stale diagnostic

**Goal:** Apply detects plan-vs-apply input drift with per-key diagnostic; per-action ResolvedConfigHash enables per-resource error scoping.

### Task T1.1: Define `IaCPlan.SchemaVersion` + `IaCPlan.InputSnapshot` + `PlanAction.ResolvedConfigHash` + `DriftEntry` type

**Files:**
- Modify: `interfaces/iac_state.go`
- Test: `interfaces/iac_state_test.go` (create)

**Note (rev5/rev6 — addresses cycle-4 Important on declared-but-unpopulated fields):** This task adds ONLY `IaCPlan` + `PlanAction` fields + the standalone `DriftEntry` type — all of which are populated by W-1 itself (T1.3, T1.4, T1.5). `ApplyResult` field additions (`InitialInputSnapshot`, `InputDriftReport`, `ReplaceIDMap`) move to W-3a where they are populated by the same PR that declares them: `T3.0.4` adds all three fields (rev6 consolidation per cycle-5 Option 4); `T3.1`/`T3.1.5`/`T3.4` populate them.

**Pre-condition verified at plan-revision time (rev6 — replaces rev5's runtime grep):** verified `grep -rn "type ApplyResult\b" interfaces/` returns `interfaces/iac_state.go:65: type ApplyResult struct {`. The assumption that `ApplyResult` is declared in `interfaces/iac_state.go` holds for T1.1, T3.0.4, and T3.4. No runtime pre-step needed; the plan author has done the fact-check.

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

Add adjacent standalone type:
```go
// DriftEntry names a single env-var whose fingerprint changed between plan-time
// and apply-time. Used by both the persisted-`--plan` path (cmd/wfctl/infra.go,
// wired in T1.5) and the in-process apply path (wfctlhelpers.ApplyPlan, wired
// in T3.1.5 — both via inputsnapshot.FormatStaleError).
type DriftEntry struct {
    Name             string `json:"name"`
    PlanFingerprint  string `json:"plan_fingerprint"`
    ApplyFingerprint string `json:"apply_fingerprint"`
}
```

**Step 4: Run tests to verify pass**

Run: `cd interfaces && go test -v`
Expected: PASS — 3 tests in iac_state_test.go (SchemaVersion + InputSnapshot + ResolvedConfigHash). DriftEntry is a standalone type — no test required at declaration site; T1.5 covers it via the formatter test.

**Step 5: Commit**

```bash
git add interfaces/iac_state.go interfaces/iac_state_test.go
git commit -m "feat(iac): add IaCPlan.SchemaVersion + InputSnapshot + PlanAction.ResolvedConfigHash + DriftEntry type"
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

**Step 3: Implement (rev5 — adds PreservedFingerprint sentinel + NewTolerantEnvProvider for the in-process apply postcondition contract)**

```go
// iac/inputsnapshot/snapshot.go
// Package inputsnapshot computes plan-time env-var fingerprints for the
// plan-stale diagnostic. Fingerprints are 16 hex chars (64 bits of preimage
// resistance); plan.json is treated as semi-sensitive and gitignored.
package inputsnapshot

import (
    "crypto/sha256"
    "encoding/hex"
    "os"
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
        if val == preservedFingerprint {
            // Sentinel from NewTolerantEnvProvider — pass through unhashed
            // so ComputeDrift recognizes the preservation signal. (rev6 —
            // unexported per cycle-5; in-package access only.)
            out[name] = preservedFingerprint
            continue
        }
        sum := sha256.Sum256([]byte(val))
        out[name] = hex.EncodeToString(sum[:])[:16]
    }
    return out
}

// Snapshot is an alias for Compute that reads slightly more naturally at
// the in-process apply postcondition call site (T3.1.5).
func Snapshot(names []string, provider func(string) (string, bool)) map[string]string {
    return Compute(names, provider)
}

// OSEnvProvider is the canonical env-provider closure that reads from
// process env via os.LookupEnv. Used by start-of-apply InputSnapshot capture.
func OSEnvProvider(name string) (string, bool) { return os.LookupEnv(name) }

// preservedFingerprint is a sentinel value indicating an env-var was set at
// plan time but is unset at apply time (sub-action cleanup is the canonical
// case). ComputeDrift (T1.5) skips drift detection for keys whose applySnap
// value is this sentinel. UNEXPORTED (rev6 — addresses cycle-5 Important on
// external-bypass channel): NewTolerantEnvProvider is the only sanctioned
// way to inject the sentinel; external callers cannot defeat drift detection.
//
// Cross-function contract:
// - Compute (this file, in-package) passes the sentinel through unhashed.
// - NewTolerantEnvProvider (this file) returns the sentinel for plan-time-set
//   but apply-time-unset vars (in-package access to the constant).
// - ComputeDrift (compute_drift.go, T1.5, same package) honors the sentinel
//   by skipping drift detection for that key.
const preservedFingerprint = "__plan_time_preserved__"

// NewTolerantEnvProvider returns an EnvProvider closure used by the
// in-process apply postcondition (T3.1.5). When a var was set at plan time
// (present in planSnapshot) but is now unset (sub-action cleanup), the
// closure returns the in-package preservedFingerprint sentinel so
// ComputeDrift suppresses the (false-positive) drift entry. For vars
// genuinely unset at both times, returns ("", false) → Compute drops the
// key from the resulting map.
//
// This is the ONLY sanctioned way to inject the preservation sentinel.
// Direct callers of Compute with a custom env-provider cannot construct
// the sentinel value because it is unexported.
func NewTolerantEnvProvider(planSnapshot map[string]string) func(name string) (string, bool) {
    return func(name string) (string, bool) {
        if val, ok := os.LookupEnv(name); ok {
            return val, true
        }
        if _, wasInPlan := planSnapshot[name]; wasInPlan {
            return preservedFingerprint, true
        }
        return "", false
    }
}
```

**Step 4: Add tests for the sentinel + tolerant-provider behavior**

Append to `iac/inputsnapshot/snapshot_test.go`:
```go
func TestNewTolerantEnvProvider_UnsetButPlanned_ReturnsSentinel(t *testing.T) {
    os.Unsetenv("STAGING_PG_PASSWORD")
    plan := map[string]string{"STAGING_PG_PASSWORD": "deadbeef00000000"}
    provider := NewTolerantEnvProvider(plan)
    val, ok := provider("STAGING_PG_PASSWORD")
    if !ok || val != preservedFingerprint {
        t.Errorf("expected (preservedFingerprint, true) for plan-time-set unset-now var; got (%q, %v)", val, ok)
    }
}

func TestCompute_PreservesSentinel(t *testing.T) {
    snap := Compute([]string{"FOO"}, func(name string) (string, bool) {
        return preservedFingerprint, true
    })
    if snap["FOO"] != preservedFingerprint {
        t.Errorf("Compute should pass sentinel through unhashed; got %q", snap["FOO"])
    }
}
```

**Step 5: Run tests to verify pass**

Run: `cd iac/inputsnapshot && go test -v`
Expected: PASS — 6 tests (4 from rev1 + 2 sentinel tests)

**Step 6: Commit**

```bash
git add iac/inputsnapshot/
git commit -m "feat(iac): add inputsnapshot.Compute + Snapshot + NewTolerantEnvProvider + PreservedFingerprint sentinel"
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

In `platform/differ.go::ComputePlan`, after building each `PlanAction`, set `ResolvedConfigHash: configHash(spec.Config)` (the existing `configHash` function at `platform/differ.go:103` — same package, unexported, in-package call). For `update` actions, compute against the post-substitution Config (config has already been substituted by caller; see `cmd/wfctl/infra.go:281,327,375` `config.ExpandEnvInMapPreservingKeys` calls before ComputePlan).

**ResolvedConfigHash semantics (rev2 — clarified per finding):**

`ResolvedConfigHash` carries the hash of the **post-substitution Config at the time the plan was computed**. For the in-process `wfctl infra apply` path, plan-time and apply-time are within the same wfctl invocation, so this is unambiguous. For the persisted `--plan plan.json` path, the hash is frozen at plan-write time; apply-time recomputation may differ if env vars changed in the interim — that drift is the plan-stale signal that T1.5 reports. After W-5 forbids JIT-style persisted plans, the persisted path is restricted to env-var-only substitution, so ResolvedConfigHash differences cleanly map to env-var changes (which T1.5 names per-key).

**Step 4: Run test to verify pass**

Run: `cd platform && go test -v`
Expected: PASS

**Step 5: Commit**

```bash
git add platform/differ.go platform/differ_test.go
git commit -m "feat(iac): ComputePlan sets PlanAction.ResolvedConfigHash"
```

### Task T1.5: Apply diagnostic — typed-error sentinel + persisted `--plan` path (in-process path moves to T3.1.5)

**Note (rev4/rev5):** Per cycle-3 Critical 1, T1.5 was creating a circular dependency by trying to wire the in-process postcondition into `iac/wfctlhelpers/apply.go` from W-1 (which would require either a ghost-stub of `wfctlhelpers.ApplyPlan` shipping in W-1, or a forward dependency on W-3a). rev4 splits T1.5: this task ships ONLY the typed-error sentinel + the persisted-`--plan` path (`cmd/wfctl/infra.go:1071`) — both of which depend solely on `IaCPlan.InputSnapshot` from T1.1, no helper-package dep. The in-process apply path moves to **T3.1.5 (W-3a)** as the task after T3.1, where `wfctlhelpers.ApplyPlan` actually exists.

**Files:**
- Create: `iac/inputsnapshot/errors.go` (typed sentinel `ErrEnvVarChanged`)
- Create: `iac/inputsnapshot/diagnostic.go` (shared `FormatStaleError` formatter)
- Modify: `cmd/wfctl/infra.go:1071` (persisted `--plan` path — wrap with `ErrEnvVarChanged`)
- Test: `cmd/wfctl/infra_apply_plan_test.go` (extend — persisted-plan path TDD)

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

**Step 1: Write failing test for the persisted-`--plan` path**

In `cmd/wfctl/infra_apply_plan_test.go` (extend):
```go
func TestApply_PlanStaleDiagnostic_NamesChangedKeys_Persisted(t *testing.T) {
    t.Setenv("STAGING_PG_PASSWORD", "old-value")
    planFile := writePlanWithInputSnapshot(t, map[string]string{
        "STAGING_PG_PASSWORD": fingerprint("old-value"),
    })
    t.Setenv("STAGING_PG_PASSWORD", "new-value")
    err := runInfraApplyForTest(planFile)
    if err == nil {
        t.Fatal("expected plan-stale error")
    }
    if !errors.Is(err, inputsnapshot.ErrEnvVarChanged) {
        t.Errorf("expected sentinel ErrEnvVarChanged; got %v", err)
    }
    if !strings.Contains(err.Error(), "STAGING_PG_PASSWORD") {
        t.Errorf("error should name the changed key; got: %s", err.Error())
    }
}
```

**Step 2: Run test → FAIL** (sentinel doesn't exist; `cmd/wfctl/infra.go:1071` raises a bare error string).

**Step 3: Implement shared formatter + sentinel + persisted-`--plan` wiring**

In `iac/inputsnapshot/`:
1. `errors.go`: `var ErrEnvVarChanged = errors.New("plan stale: env-var changed since plan")`.
2. `diagnostic.go`: `func FormatStaleError(driftReport []interfaces.DriftEntry) string` — canonical formatter. (Note: `DriftEntry` lives in the `interfaces` package per rev4 T1.1; this formatter imports `interfaces`.) Output:
   ```
   error: plan stale: %d input(s) changed since plan
     %s: fingerprint %s (plan) → %s (apply)
     ...
     hint: ensure all env vars referenced by infra.yaml are exported to both Plan and Apply steps
   ```
3. `compute_drift.go`: `func ComputeDrift(planSnap, applySnap map[string]string) []interfaces.DriftEntry` — pure function that compares two snapshots and produces drift entries. Iterates over `planSnap` keys (the plan-time names; rev4 fix per cycle-3 Critical 2 — derive names from map keys, NOT from a phantom `plan.InputNames` field). **rev5: honors `inputsnapshot.PreservedFingerprint` sentinel** — if `applySnap[k] == PreservedFingerprint`, skip drift detection for that key (sub-action cleanup case). Concrete implementation: see T3.1.5's "Step 3" code block where `ComputeDrift` is fully spec'd alongside the postcondition that consumes it. Cross-function contract documented at all 3 call sites: `Compute` passes the sentinel through; `NewTolerantEnvProvider` returns the sentinel for plan-time-set apply-time-unset vars; `ComputeDrift` honors it.

In `cmd/wfctl/infra.go` near line 1071, before raising "plan stale":
1. Re-compute apply-time InputSnapshot using `keys(plan.InputSnapshot)` as the name list (rev4 — derives from existing field; no `InputNames` field needed).
2. `drift := inputsnapshot.ComputeDrift(plan.InputSnapshot, applyTimeSnap)`.
3. Wrap result: `return fmt.Errorf("%w: %s", inputsnapshot.ErrEnvVarChanged, inputsnapshot.FormatStaleError(drift))`.

**Step 3.5 (rev6 — addresses cycle-5 Important on cross-PR test gap):** Add a same-package unit test for `ComputeDrift`'s sentinel-honoring branch.

In `iac/inputsnapshot/compute_drift_test.go`:
```go
func TestComputeDrift_PreservedSentinelSkipsDrift(t *testing.T) {
    planSnap := map[string]string{"FOO": "abcdef0000000000"}
    applySnap := map[string]string{"FOO": preservedFingerprint}
    drift := ComputeDrift(planSnap, applySnap)
    if len(drift) != 0 {
        t.Errorf("preserved-sentinel should suppress drift; got %+v", drift)
    }
}

func TestComputeDrift_DifferentFingerprint_ReportsDrift(t *testing.T) {
    planSnap := map[string]string{"FOO": "abcdef0000000000"}
    applySnap := map[string]string{"FOO": "deadbeef00000000"}
    drift := ComputeDrift(planSnap, applySnap)
    if len(drift) != 1 || drift[0].Name != "FOO" {
        t.Errorf("differing fingerprints should produce one drift entry; got %+v", drift)
    }
}

func TestComputeDrift_KeyMissingInApplySnap_ReportsDrift(t *testing.T) {
    planSnap := map[string]string{"FOO": "abcdef0000000000"}
    applySnap := map[string]string{} // FOO missing entirely
    drift := ComputeDrift(planSnap, applySnap)
    if len(drift) != 1 || drift[0].ApplyFingerprint != "(unset)" {
        t.Errorf("missing key should produce drift with (unset) fingerprint; got %+v", drift)
    }
}
```

This closes the cross-PR test gap: the sentinel-honoring branch is now first-tested in W-1 (this same task), not in W-3a's T3.1.5. T3.1.5 still tests the integration (postcondition + closure + ComputeDrift end-to-end) but the unit-level branch coverage lives here.

**Step 4: Run test → PASS** (3 ComputeDrift unit tests + the persisted-plan-path test).

**Step 5: Commit**

```bash
git add iac/inputsnapshot/errors.go iac/inputsnapshot/diagnostic.go iac/inputsnapshot/compute_drift.go iac/inputsnapshot/compute_drift_test.go cmd/wfctl/infra.go cmd/wfctl/infra_apply_plan_test.go
git commit -m "feat(iac): typed ErrEnvVarChanged sentinel + plan-stale diagnostic + ComputeDrift sentinel-honoring"
```

**Rollback (T1.5):** revert commit; persisted-`--plan` path returns to bare `error: plan stale: config hash mismatch` (existing behavior). In-process apply path is unaffected (T1.5 doesn't touch it; T3.1.5 in W-3a does).

**Cross-PR note:** T1.5 only ships the typed sentinel + persisted-plan path. The in-process apply path's drift postcondition lives in **T3.1.5** as the task after T3.1 in W-3a. This split is the rev4 fix for cycle-3 Critical 1 (no ghost-stub of `wfctlhelpers.ApplyPlan` in W-1).

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

Create `cmd/wfctl/infra_refresh_outputs.go` with `runInfraRefreshOutputs(args []string) error`. Register in `infraCommands` map (or wherever subcommands dispatch). Use `iac/refreshoutputs`. Raise the literal error from T2.7 verbatim if no provider is configured: `fmt.Errorf("refresh-outputs: provider not configured for env %q", env)`.

**Step 4: Run test to verify pass**

**Step 5 (rev3 — addresses doc-verification minor):** if this task touches `docs/WFCTL.md` (it doesn't directly, but T2.6 will), the doc verification (`mdformat --check + markdown-link-check`) is owned by T2.6 — confirm no doc edits here.

**Step 6: Commit**

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

### Task T2.5: Concurrency stress test for refresh

> **Note (rev2):** prior T2.4 (stub conformance scenario) removed — W-7 owns BOTH the stub creation and the body for every conformance scenario. PR-W-2's verification section cites `Scenario_OutputsRefreshDetectsNewFields` by name with the caveat "implemented in W-7 (T7.X)" — no ghost-stub Go file ships in W-2.

**Files:**
- Test: `iac/refreshoutputs/refresh_concurrency_test.go`

**Step 1-5:** Test with 100 fake resources, concurrency=8; verify no deadlock + all states refreshed; verify Read called exactly once per resource.

### Task T2.6: Document `refresh-outputs` in `docs/WFCTL.md`

**Files:**
- Modify: `docs/WFCTL.md`

**Step 1-5:** Add `infra refresh-outputs` section to command reference. **Verification:** run `mdformat --check docs/WFCTL.md && find docs -name "*.md" -exec markdown-link-check {} +` — exit 0 + no broken anchors. Commit: `docs(wfctl): document infra refresh-outputs subcommand`.

### Task T2.7: Runtime-launch-validation

**Files:** none new

**Step 1:** Build wfctl: `go build -o /tmp/wfctl ./cmd/wfctl`
**Step 2:** Run `/tmp/wfctl infra refresh-outputs --help`; expect help text printed (exit 0; usage line `wfctl infra refresh-outputs [-c CONFIG] [--env ENV] [--concurrency N]`).
**Step 3:** Against a fake state JSON (no real cloud), run `/tmp/wfctl infra refresh-outputs -c <fake.yaml> --env staging`; expect exit 1 with the EXACT literal stderr line: `error: refresh-outputs: provider not configured for env "staging"` (no panic; no stack trace). The implementation in T2.2 raises this string verbatim via `fmt.Errorf("refresh-outputs: provider not configured for env %q", env)` — no environment-specific suffix to keep the assertion stable across local + CI invocations.

**Verification (PR W-2):**
- `go test ./iac/refreshoutputs/... ./cmd/wfctl/... -count=1 -race`
- Manual: smoke against the staging-PG state if available.

**Rollback (PR W-2):** revert commits. `WFCTL_REFRESH_OUTPUTS` env var is opt-in (default off), so post-revert behavior matches pre-W-2 exactly.

---

## PR W-3a: Replace foundation — manifest field + helper package + diff cache (no runtime change)

**Goal:** Land all the additive scaffolding W-3b needs without touching wfctl's actual apply path. After W-3a merges, `wfctl infra apply` behaves exactly as before — no plugin reads `iacProvider.computePlanVersion` (T3.0) and no caller invokes `wfctlhelpers.ApplyPlan` (T3.1–T3.4) or the diff cache (T3.5). W-3b is the binding behavior-change gate.

**Why split:** rev3 splits the original W-3 (16 tasks) into W-3a (foundation, 6 tasks) + W-3b (refactor + dispatch, 10 tasks) per cycle-2 review's PR-size finding. W-3a is reviewable in ~1500 lines with zero runtime risk; W-3b carries all the runtime-affecting changes.

**Rebase strategy (rev6 — addresses cycle-5 Important on multi-commit `interfaces/iac_state.go` modifications):** rev6 consolidation makes T3.0.4 the ONLY task in W-3a that modifies `interfaces/iac_state.go` (T3.4 was rev5's secondary modifier; rev6 moved its field-add into T3.0.4). If W-3a force-pushes during review (which it WILL across cycles, per the rev1-rev5 plan history), `git rebase -i` uses `pick` to preserve per-task commit boundaries. The TDD-per-task verification depends on per-commit boundaries; do NOT `squash` during interactive rebase. Reviewers checking individual commits via `gh pr view <N> --json commits` see the field additions land cleanly in T3.0.4 + the populator commits land in T3.1/T3.1.5/T3.4 against the pre-existing fields.

**Note (rev2/rev3):** T3.0 hoists the `iacProvider.computePlanVersion` plugin-manifest field schema into W-3a so T3.7 (in W-3b) can read the manifest directly with no transitional env-var (`WFCTL_USE_V2_APPLY`) in main. Plugins that don't set the field default to v1 (legacy `provider.Apply`); plugins that set `v2` route through `wfctlhelpers.ApplyPlan` once W-3b lands. W-9 retains only the optional `ProviderPlanner` interface + cross-plugin build verification — no env-var to remove.

### Task T3.0: Add `iacProvider.computePlanVersion` to plugin.json schema

**Files:**
- Modify: `plugin/sdk/manifest.go` (add `IaCProvider.ComputePlanVersion string \`json:"computePlanVersion,omitempty"\`` field; values: `""` (= v1, default), `"v1"`, `"v2"`)
- Modify: `plugin/sdk/manifest_schema.json` (add `computePlanVersion` enum: ["v1","v2"]; default v1 when omitted)
- Test: `plugin/sdk/manifest_test.go` (extend)

**Step 1: Write failing test**

```go
func TestManifest_IaCProvider_ComputePlanVersion(t *testing.T) {
    cases := map[string]struct{ in string; want string; wantErr bool }{
        "default-v1":  {`{"name":"x","iacProvider":{}}`, "v1", false},
        "explicit-v1": {`{"name":"x","iacProvider":{"computePlanVersion":"v1"}}`, "v1", false},
        "explicit-v2": {`{"name":"x","iacProvider":{"computePlanVersion":"v2"}}`, "v2", false},
        "rejected":    {`{"name":"x","iacProvider":{"computePlanVersion":"v3"}}`, "", true},
    }
    for name, c := range cases {
        t.Run(name, func(t *testing.T) {
            m, err := ParseManifest([]byte(c.in))
            if (err != nil) != c.wantErr { t.Fatalf("err=%v wantErr=%v", err, c.wantErr) }
            if !c.wantErr && m.IaCProvider.EffectiveComputePlanVersion() != c.want {
                t.Errorf("got %q want %q", m.IaCProvider.EffectiveComputePlanVersion(), c.want)
            }
        })
    }
}
```

**Step 2: Run test → FAIL** (field doesn't exist).

**Step 3: Implement** — add field + `EffectiveComputePlanVersion()` accessor that returns `"v1"` when the raw value is `""`. Update schema.

**Step 4: Run test → PASS.**

**Step 5: Commit**

```bash
git add plugin/sdk/manifest.go plugin/sdk/manifest_schema.json plugin/sdk/manifest_test.go
git commit -m "feat(iac): plugin manifest gains iacProvider.computePlanVersion (default v1)"
```

**Rollback (T3.0):** revert commit; field is `omitempty` and unread by anything until T3.7 lands in same PR — no behavioral change for existing v1 plugins.

### Task T3.0.4: Declare all 3 ApplyResult W-3a fields (`InitialInputSnapshot` + `InputDriftReport` + `ReplaceIDMap`) on `interfaces/iac_state.go`

**Files:**
- Modify: `interfaces/iac_state.go` (add 3 fields to `ApplyResult` — same package as T1.1's IaCPlan/PlanAction additions; rev6 consolidates per cycle-5 Option 4)
- Test: `interfaces/iac_state_test.go` (extend with roundtrip tests)

**Note (rev5/rev6 — addresses cycle-4 Important + cycle-5 Option 4):** This task lands in W-3a, the same PR that populates the fields. T3.1 reads `InitialInputSnapshot` at apply entry; T3.1.5 populates `InputDriftReport` via deferred postcondition; T3.4 populates `ReplaceIDMap` via doReplace. **rev6 consolidation:** all three field additions ship in this one task / one commit, so `interfaces/iac_state.go` is modified exactly ONCE in W-3a. T3.4 only POPULATES the field (no separate Modify of `interfaces/iac_state.go`); cleaner Git history; cleaner rebase story across review rounds.

**Step 1: Write failing tests**

Append to `interfaces/iac_state_test.go`:
```go
func TestApplyResult_InputDriftReport_RoundTrip(t *testing.T) {
    r := ApplyResult{InputDriftReport: []DriftEntry{
        {Name: "STAGING_PG_PASSWORD", PlanFingerprint: "abc", ApplyFingerprint: "def"},
    }}
    data, _ := json.Marshal(r)
    var got ApplyResult
    json.Unmarshal(data, &got)
    if len(got.InputDriftReport) != 1 || got.InputDriftReport[0].Name != "STAGING_PG_PASSWORD" {
        t.Errorf("InputDriftReport roundtrip failed: %+v", got)
    }
}

func TestApplyResult_InitialInputSnapshot_RoundTrip(t *testing.T) {
    r := ApplyResult{InitialInputSnapshot: map[string]string{"FOO": "fp1234"}}
    data, _ := json.Marshal(r)
    var got ApplyResult
    json.Unmarshal(data, &got)
    if got.InitialInputSnapshot["FOO"] != "fp1234" {
        t.Errorf("InitialInputSnapshot roundtrip failed: %+v", got)
    }
}

func TestApplyResult_ReplaceIDMap_RoundTrip(t *testing.T) {
    r := ApplyResult{ReplaceIDMap: map[string]string{"vpc": "new-uuid"}}
    data, _ := json.Marshal(r)
    var got ApplyResult
    json.Unmarshal(data, &got)
    if got.ReplaceIDMap["vpc"] != "new-uuid" {
        t.Errorf("ReplaceIDMap roundtrip failed: %+v", got)
    }
}
```

**Step 2: Run tests → FAIL** (fields don't exist).

**Step 3: Add fields to `type ApplyResult struct` in `interfaces/iac_state.go`:**
```go
// InitialInputSnapshot captures env-var fingerprints at start of apply.
// Populated by wfctlhelpers.ApplyPlan (T3.1) at apply entry.
// Used by the deferred postcondition (T3.1.5) to compute drift report.
InitialInputSnapshot map[string]string `json:"initial_input_snapshot,omitempty"`

// InputDriftReport names env-vars whose fingerprint changed between plan and apply.
// Populated by the deferred postcondition (T3.1.5) regardless of apply success/error path.
// Empty (or nil) means no drift detected.
InputDriftReport []DriftEntry `json:"input_drift_report,omitempty"`

// ReplaceIDMap propagates new ProviderIDs from Replace actions to dependent
// resources whose Apply runs later in the same plan.
// Populated by doReplace (T3.4); consumed by JIT substitution (T5.2/T5.3 in W-5).
ReplaceIDMap map[string]string `json:"replace_id_map,omitempty"`
```

**Step 4: Run tests → PASS** (6 tests in iac_state_test.go: 3 from T1.1 + 3 here).

**Step 5: Commit**

```bash
git add interfaces/iac_state.go interfaces/iac_state_test.go
git commit -m "feat(iac): add ApplyResult.InitialInputSnapshot + InputDriftReport + ReplaceIDMap fields"
```

**Rollback (T3.0.4):** revert commit; T3.1 + T3.1.5 + T3.4 fail to compile (they reference these fields). Reverting T3.0.4 + T3.1 + T3.1.5 + T3.4 as a unit returns ApplyResult to its pre-W-3a shape.

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

### Task T3.1.5: In-process apply drift postcondition (rev4/rev5 — split out of T1.5 to eliminate the W-1↔W-3a cycle; renamed from T3.0.5 per cycle-4 finding)

**See also:** **T1.5 (W-1)** for the persisted-`--plan`-path counterpart. T1.5 wraps the existing `cmd/wfctl/infra.go:1071` plan-stale check with `inputsnapshot.ErrEnvVarChanged`; T3.1.5 adds the unconditional postcondition for the in-process apply path. Both call the same `inputsnapshot.FormatStaleError` formatter and use the same `interfaces.DriftEntry` type.

**Files:**
- Modify: `iac/wfctlhelpers/apply.go` (the ApplyPlan body created in T3.1; T3.1.5 lands AFTER T3.1 — see Note below)
- Modify: `cmd/wfctl/infra_apply.go` (in-process apply caller)
- Test: `cmd/wfctl/infra_apply_in_process_test.go` (NEW)
- Test: `iac/wfctlhelpers/apply_postcondition_test.go` (NEW — exercises the deferred postcondition + panic-recover + env-unset tolerance)

**Sequencing within W-3a (rev5 — addresses cycle-4 Critical 3):** T3.1.5 runs sequentially after T3.1 (which creates the helper package + `ApplyPlan` body). The dot-suffix-after-its-prefix convention matches TC1.5 (lands between TC1 and TC2). The in-PR commit order is: T3.0 → T3.0.4 → T3.1 → T3.1.5 → T3.2 → T3.3 → T3.4 → T3.5.

**Step 1: Write failing tests**

`cmd/wfctl/infra_apply_in_process_test.go`:
```go
func TestApply_InProcess_PlanStaleDiagnostic_NamesChangedKeys(t *testing.T) {
    t.Setenv("STAGING_PG_PASSWORD", "old-value")
    plan := buildInMemoryPlan(t, fixturePath, capturedEnv())
    t.Setenv("STAGING_PG_PASSWORD", "new-value")
    result, err := wfctlhelpers.ApplyPlan(ctx, fakeProvider, plan)
    _ = err // apply may succeed or fail; drift detection is independent
    if len(result.InputDriftReport) != 1 {
        t.Fatalf("expected 1 drift entry, got %d", len(result.InputDriftReport))
    }
    if result.InputDriftReport[0].Name != "STAGING_PG_PASSWORD" {
        t.Errorf("expected STAGING_PG_PASSWORD in drift report; got %s", result.InputDriftReport[0].Name)
    }
}
```

`iac/wfctlhelpers/apply_postcondition_test.go`:
```go
func TestApply_Postcondition_PanicDoesNotCorruptResult(t *testing.T) {
    panickyEnv := func(name string) (string, bool) { panic("env-provider closure freed") }
    plan := &interfaces.IaCPlan{InputSnapshot: map[string]string{"FOO": "abcdef"}}
    result, err := ApplyPlanWithEnvProvider(ctx, fakeProvider, plan, panickyEnv)
    if err != nil { t.Fatalf("apply should not surface postcondition panic: %v", err) }
    if result.InputDriftReport != nil {
        t.Errorf("on postcondition panic, drift report should be nil; got %+v", result.InputDriftReport)
    }
}

func TestApply_Postcondition_FingerprintAfterEnvUnset_NoFalsePositive(t *testing.T) {
    // Sub-action unsets env-var for credential cleanup; postcondition must
    // not flag this as drift (the original VALUE is what counts; an unset
    // post-apply is not a "changed env" in the operator's mental model).
    t.Setenv("STAGING_PG_PASSWORD", "value")
    plan := buildInMemoryPlanWithEnv(t, fixturePath, "STAGING_PG_PASSWORD")
    fakeApplyThatUnsetsEnv := func(...) { os.Unsetenv("STAGING_PG_PASSWORD") }
    result, _ := wfctlhelpers.ApplyPlan(ctx, fakeApplyThatUnsetsEnv, plan)
    if len(result.InputDriftReport) > 0 {
        t.Errorf("post-apply env-unset must not trigger drift false-positive; got: %+v", result.InputDriftReport)
    }
}
```

**Step 2: Run tests → FAIL** (postcondition not implemented).

**Step 3: Implement the deferred postcondition in `wfctlhelpers.ApplyPlan` (panic-safe + unset-tolerant; rev5 — fixes cycle-4 Critical 1)**

```go
// iac/wfctlhelpers/apply.go (added by T3.1.5)
func ApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
    inputNames := keys(plan.InputSnapshot)
    result := &interfaces.ApplyResult{
        InitialInputSnapshot: inputsnapshot.Snapshot(inputNames, inputsnapshot.OSEnvProvider),
    }

    // rev4+rev5 deferred postcondition — runs unconditionally, panic-safe.
    defer func() {
        defer func() {
            if r := recover(); r != nil {
                // Panic in postcondition (e.g., env-provider closure freed).
                // Do NOT corrupt the apply result; clear drift report + log.
                result.InputDriftReport = nil
                log.Printf("warning: input-drift postcondition panicked: %v", r)
            }
        }()
        // rev5 — INVOKE the closure factory (not pass it as a value).
        // Returns a closure that yields preservedFingerprint for vars that were
        // set at plan-time but are unset now (sub-action cleanup).
        tolerantProvider := inputsnapshot.NewTolerantEnvProvider(plan.InputSnapshot)
        applyTimeSnap := inputsnapshot.Snapshot(inputNames, tolerantProvider)
        result.InputDriftReport = inputsnapshot.ComputeDrift(plan.InputSnapshot, applyTimeSnap)
    }()

    // ... rest of ApplyPlan body (delegates to doCreate/doUpdate/doReplace/doDelete) ...
    return result, nil
}
```

The factory + sentinel are added in T1.2 (`iac/inputsnapshot/snapshot.go`):
```go
// (rev6 — sentinel canonical declaration is in T1.2 above; UNEXPORTED as
// preservedFingerprint per cycle-5 Important on external-bypass channel.
// This block is duplicate exposition for T3.1.5's reading flow; T1.2's
// snapshot.go is the source of truth.)

// NewTolerantEnvProvider returns an EnvProvider closure that preserves
// fingerprints for vars set at plan-time but unset at apply-time. Used by
// the in-process apply postcondition (T3.1.5).
//
// Cross-function contract: when this provider is the source for Snapshot,
// the resulting map's preservedFingerprint values MUST be honored by
// ComputeDrift (T1.5 / iac/inputsnapshot/compute_drift.go) — drift on a
// preserved key is suppressed, NOT reported as DriftEntry. NewTolerantEnvProvider
// is the only sanctioned sentinel injector since the constant is unexported.
func NewTolerantEnvProvider(planSnapshot map[string]string) func(name string) (string, bool) {
    return func(name string) (string, bool) {
        if val, ok := os.LookupEnv(name); ok {
            return val, true
        }
        // Var was set at plan time, now unset — sub-action cleanup case.
        if _, wasInPlan := planSnapshot[name]; wasInPlan {
            return preservedFingerprint, true // Sentinel, NOT a real value.
        }
        return "", false
    }
}
```

And T1.5's `ComputeDrift` MUST honor the sentinel (rev5 cross-function contract):
```go
// iac/inputsnapshot/compute_drift.go (T1.5 — UPDATED in rev5 to honor preservedFingerprint)
func ComputeDrift(planSnap, applySnap map[string]string) []interfaces.DriftEntry {
    var drift []interfaces.DriftEntry
    for name, planFP := range planSnap {
        applyFP, present := applySnap[name]
        if !present {
            // Key dropped from applySnap entirely — could be a missing env or
            // a closure that returned (_, false). Either way, treat as drift
            // (operator-facing "var was set but unset since" is genuine drift
            // for the persisted-`--plan` path). The in-process path uses
            // NewTolerantEnvProvider which returns preservedFingerprint
            // instead of dropping the key — see below.
            drift = append(drift, interfaces.DriftEntry{
                Name: name, PlanFingerprint: planFP, ApplyFingerprint: "(unset)",
            })
            continue
        }
        if applyFP == preservedFingerprint {
            continue // Sentinel — sub-action cleanup unset; not real drift.
        }
        if applyFP != planFP {
            drift = append(drift, interfaces.DriftEntry{
                Name: name, PlanFingerprint: planFP, ApplyFingerprint: applyFP,
            })
        }
    }
    return drift
}
```

**Cross-function contract documented in 3 places (rev5/rev6):**
- `iac/inputsnapshot/snapshot.go::preservedFingerprint` doc comment (the sentinel definition; UNEXPORTED per rev6)
- `iac/inputsnapshot/snapshot.go::NewTolerantEnvProvider` doc comment (cites the contract; sole sanctioned injector)
- `iac/inputsnapshot/compute_drift.go::ComputeDrift` body comment (honors the sentinel via in-package access)

In `cmd/wfctl/infra_apply.go`:
4. After `wfctlhelpers.ApplyPlan` returns, if `result.InputDriftReport` is non-empty, print `inputsnapshot.FormatStaleError(result.InputDriftReport)` to stderr as a warning (or wrap as `ErrEnvVarChanged` if the apply itself failed).

**Step 4: Run tests → PASS** (3 tests across both files).

**Step 5: Commit**

```bash
git add iac/wfctlhelpers/apply.go cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_in_process_test.go iac/wfctlhelpers/apply_postcondition_test.go
git commit -m "feat(iac): in-process apply unconditional drift postcondition (panic-safe + tolerant of mid-apply env unset)"
```

**Rollback (T3.1.5):** revert commit; in-process apply path stops emitting `InputDriftReport` (field stays declared on `ApplyResult` from T3.0.4 but is never populated by core wfctl). Persisted-`--plan` path (T1.5 in W-1) is unaffected — that path computes drift inline at the cmd/wfctl level. Operators relying on the in-process diagnostic see the original raw error from sub-actions; no regression beyond pre-rev4 behavior.

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

### Task T3.4: Implement `doReplace` populating `ApplyResult.ReplaceIDMap` (field declared in T3.0.4)

**Files:**
- Modify: `iac/wfctlhelpers/apply.go` (populate the field via doReplace)
- Test: `iac/wfctlhelpers/apply_replace_test.go`

**Note (rev6 — addresses cycle-5 Important on rebase fragility + Option 4 consolidation):** `ApplyResult.ReplaceIDMap` is declared in T3.0.4 (the single point of `interfaces/iac_state.go` modification within W-3a). T3.4 only POPULATES the field — no separate Modify of `interfaces/iac_state.go`, no rebase fragility from N commits modifying the same file in serial.

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

**Step 2-5: Implement `doReplace`:**
1. Call `Delete(ctx, refFromCurrent(action))`
2. On success, call `Create(ctx, action.Resource)`
3. On Create success: lazy-init the map if nil (`if result.ReplaceIDMap == nil { result.ReplaceIDMap = map[string]string{} }`), then `result.ReplaceIDMap[action.Resource.Name] = newOutput.ProviderID`. Field name uses exported `ReplaceIDMap` (capital R). Will be consumed by JIT substitution in W-5.

For this PR, `ReplaceIDMap` is populated by `doReplace`; the field declaration lives in T3.0.4 (rev6 consolidation). W-5 wires the consumer in T5.2/T5.3.

Commit: `feat(iac): doReplace populates ApplyResult.ReplaceIDMap`

**Rollback (T3.4):** revert commit; un-merged W-3 means Replace actions never emit (no plugin sets `iacProvider.computePlanVersion: v2` until P-DO ships, and v1 plugins never reach `doReplace`). Existing apply paths unchanged. For mid-replace failures during the W-3 development cycle: the `ApplyResult.ReplaceIDMap` state checkpoint allows operators to inspect post-Delete pre-Create state in the apply log; manual cloud restoration is the recovery path for the in-flight resource (out of scope for this task — same recovery semantics as today's bare delete + manual recreate). Production exposure for v2 plugins is gated by P-DO + C-1 cutover; rollback at C-1 reverts wfctl-lock pin, returning operators to the v1 path with no Replace emission.

### Task T3.5: Add diff cache at `iac/diffcache/` keyed by `(plugin-version, type, providerID, sha-config, sha-outputs)`

**Files:**
- Create: `iac/diffcache/cache.go`
- Create: `iac/diffcache/cache_filesystem.go` (file-backed implementation)
- Create: `iac/diffcache/cache_inmemory.go` (in-memory fallback, used when CACHE_DIR=":memory:")
- Test: `iac/diffcache/cache_test.go`
- Test: `iac/diffcache/cache_corruption_test.go`
- Test: `iac/diffcache/cache_eviction_test.go`

**Step 1: Write failing tests**

```go
// cache_test.go — basic Get/Put roundtrip
// cache_corruption_test.go — Put a JSON file, then truncate it; next Get returns (_, false) and silently re-Diffs (no crash, no error log spam)
// cache_eviction_test.go — Put 1000 entries with cap=200; verify oldest 800 are evicted (LRU)
```

**Step 2: Run tests → FAIL** (cache doesn't exist).

**Step 3: Implement**

```go
// iac/diffcache/cache.go
type Cache interface {
    Get(key Key) (DiffResult, bool)
    Put(key Key, val DiffResult)
}
type Key struct {
    PluginVersion string // e.g., "do@v0.10.0"
    Type          string // e.g., "infra.vpc"
    ProviderID    string // resource ID; empty for net-new
    SHAConfig     string // sha256 hex of canonical-marshal(spec.Config)
    SHAOutputs    string // sha256 hex of canonical-marshal(currentState.Outputs); empty for net-new
}
```

**Lifecycle constraints (rev2 — addressing missing-failure-modes finding):**

1. **Storage location**: file-backed at `~/.cache/wfctl/diff/<sha256-of-key>.json`. Operators can opt out by setting `WFCTL_DIFFCACHE=disabled` (returns no-op cache); set `WFCTL_DIFFCACHE=:memory:` for in-memory only (CI default — see #4).
2. **Size cap with LRU eviction**: max 1024 entries OR max 64 MiB on-disk (whichever hit first). Implemented as a per-Put scan of mtimes; if cap exceeded, evict oldest 10% in one pass (amortized cost). Reference: stdlib `container/list` for LRU index loaded lazily on first Put.
3. **Corruption recovery**: on Get, if the file fails to parse (truncated, partial-write, JSON syntax error, schema-version mismatch from a wfctl downgrade), silently delete the corrupt file and return `(_, false)` — caller re-Diffs and re-Puts. NO error returned, NO log spam (single info-level log on first corruption per process to aid diagnosis).
4. **CI ephemerality (load-bearing — design §"Top doubts #2")**: the cache is process-local optimization, NOT a CI-time correctness mechanism. CI runners are ephemeral and will hit the cache cold every run; the cache MUST NOT be relied on for correctness or reproducibility. Document this in the cache package godoc + in `docs/WFCTL.md` § "diff cache". **rev3:** all CI workflows in this repo set `WFCTL_DIFFCACHE=:memory:` explicitly in `.github/workflows/test.yml` and downstream workflow files — no filesystem writes in containerized runners. (rev2's "MAY set" hedge is replaced with this concrete behavior.)
5. **Schema version**: `Cache` JSON files include a `schemaVersion: 1` field. Future schema bumps trigger silent eviction-on-mismatch (same path as corruption recovery #3). Plan-version downgrades that load older cache files: same — silently re-Diff.
6. **Cross-plugin downgrade**: `PluginVersion` is part of the key, so a plugin downgrade naturally invalidates entries (cache key miss). Old entries persist on disk until LRU evicts; the size cap (#2) bounds disk waste.

**Step 4: Run tests → PASS** (basic, corruption, eviction).

**Step 5: Commit**

```bash
git add iac/diffcache/cache.go iac/diffcache/cache_filesystem.go iac/diffcache/cache_inmemory.go iac/diffcache/cache_test.go iac/diffcache/cache_corruption_test.go iac/diffcache/cache_eviction_test.go
git commit -m "feat(iac): add diff cache with LRU eviction + corruption recovery"
```

**Rollback (T3.5):** revert commits. T3.6f (which consumes the cache) becomes a no-op (cache lookup returns false 100% of the time). ComputePlan correctness is unaffected — the cache is purely an amortization optimization.

## PR W-3b: Replace dispatch — ComputePlan refactor + apply-path branching (runtime change)

**Goal:** `ComputePlan` calls `Diff` per resource; emits `replace` action when `NeedsReplace=true` or any `FieldChange.ForceNew=true`. Apply path routes v2 plugins through `wfctlhelpers.ApplyPlan` (the helper package landed in W-3a). This is the binding runtime-affecting PR.

**Branch:** `feat/iac-replace-dispatch` based off W-3a's merge commit. Cannot draft until W-3a merges — both PRs touch `platform/differ.go` (W-3a doesn't, but T3.6e does heavily) and the helper package (W-3a creates, W-3b consumes).

**Sequencing constraint within W-3b (rev4 — addresses cycle-3 cherry-pick risk):**
- T3.6e (the binding TDD test+impl for Diff dispatch) MUST land BEFORE T3.6c/d are considered "complete coverage." T3.6c/d are mechanical signature-threading commits that don't add behavior; they are reviewable in isolation but their value is unlocked only when T3.6e ships the dispatch.
- **Cherry-pick rule:** any partial-merge cherry-pick that includes T3.6c or T3.6d MUST also include T3.6e (and vice versa). The W-3b PR ships as a single squash-merge by default; if a reviewer requests a partial split, the splitter MUST honor this rule.
- Reviewers checking out W-3b at any commit between T3.6a and T3.6e see ComputePlan with the legacy ConfigHash-only behavior + threaded provider arg (no Replace action emitted yet). At T3.6e the behavior change is binding.

### Task T3.6a: Refactor `platform.ComputePlan` signature + in-package tests

**Files:**
- Modify: `platform/differ.go` (signature change only — Diff dispatch lands in T3.6e in the SAME commit as the failing test)
- Modify: `platform/differ_test.go` (update existing 7 in-package tests to construct a no-op fake provider)
- Create: `platform/fake_provider_test.go` (no-op fake provider for in-package tests; concrete TDD harness comes in T3.6e)

**Step 1: Write a passing in-package test** that constructs the no-op fake provider and asserts ComputePlan signature compiles + returns the same actions as the legacy ConfigHash-only path.

**Step 2: Implement signature**

```go
// before
func ComputePlan(desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error)
// after
func ComputePlan(ctx context.Context, p interfaces.IaCProvider, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error)
```

The body still uses the legacy ConfigHash compare (no Diff dispatch yet); `p` is accepted but unused in this commit. This keeps the signature change reviewable in isolation — the Diff dispatch + its TDD red→green cycle land together in T3.6e.

Update the 7 in-package tests in `platform/differ_test.go` to pass the no-op fake provider.

**Step 3: Verify all in-package tests pass.** Run: `cd platform && go test ./... -count=1`. Expected: PASS (no skipped tests; no `t.Skip` lines anywhere — rev3 fix for the cycle-2 self-contradiction).

**Step 4: Commit**

```bash
git add platform/differ.go platform/differ_test.go platform/fake_provider_test.go
git commit -m "refactor(iac): ComputePlan signature accepts ctx+provider (no behavior change)"
```

**Rollback (T3.6a):** revert commit; signature returns to (desired, current); cascading revert through T3.6b–f required. Pure-additive interface change — re-application is safe.

### Task T3.6b: Thread provider through `cmd/wfctl/infra.go` plan path (fail-loud on plugin-load failure)

**Files:**
- Modify: `cmd/wfctl/infra.go:199` (the `runInfraPlan` call site that doesn't have a provider constructed today)
- Modify: `cmd/wfctl/infra.go:281,327,375` (env-substitution + plan-load helpers — pass provider through)
- Test: `cmd/wfctl/infra_test.go` (1 existing call site — provide a fake)
- Test: `cmd/wfctl/infra_plan_provider_load_test.go` (NEW — assert fail-loud on plugin-load failure)

**Step 1: Write failing test** — exercise `wfctl infra plan --env staging` against a fixture config; assert (a) the new plan path constructs a provider and threads it into ComputePlan, and (b) when plugin-load fails, wfctl exits non-zero with a literal error: `error: failed to load plugin "<name>": <reason>; wfctl infra plan now requires the plugin process to compute Diff (since v0.21.0)`.

**Step 2: Run test → FAIL** (provider not constructed at plan time today; the plan path skips provider load).

**Step 3: Implement** — add provider construction to the plan path (the same provider-loader that apply uses). This is a real lifecycle change: today `wfctl infra plan` runs without ever loading the gRPC plugin process; after this task, plan loads the provider so it can call Diff.

**No `--no-provider` escape hatch** (rev3 — addressed YAGNI finding from cycle 2). If plugin load fails, the command exits non-zero with the literal error message above. Rationale: `wfctl validate` exists for offline config validation; `wfctl infra plan` is a plan-correctness operation that requires provider Diff dispatch to be honest about Replace classification. A silent fall-back to ConfigHash-only would emit misleading plans that don't reflect what apply will do.

**Step 4: Run test → PASS.**

**Step 5: Commit**

```bash
git add cmd/wfctl/infra.go cmd/wfctl/infra_test.go cmd/wfctl/infra_plan_provider_load_test.go
git commit -m "feat(iac)!: wfctl infra plan now loads provider for Diff dispatch (BREAKING: fails on plugin-load error)"
```

**Rollback (T3.6b):** revert commit; plan path stops loading provider (legacy behavior); ComputePlan call falls back to a no-op fake. Operators broken by an unreachable plugin host should pin to a previous wfctl version OR use `wfctl validate` for offline checks. Document the breaking change in CHANGELOG entry created by W-3b's PR.

### Task T3.6c: Thread provider through `cmd/wfctl/infra_apply.go` (production apply path)

**Files:**
- Modify: `cmd/wfctl/infra_apply.go:328` (the production ComputePlan call site)

**Step 1: Write failing test** — extend `cmd/wfctl/infra_apply_v2_test.go` to assert provider is passed (not nil) into ComputePlan during apply.

**Step 2: Run test → FAIL.**

**Step 3: Implement** — apply already loads the provider (the legacy `provider.Apply` call needs it); thread the same handle into ComputePlan.

**Step 4: Run test → PASS.**

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_v2_test.go
git commit -m "feat(iac): wfctl infra apply threads provider into ComputePlan"
```

**Rollback (T3.6c):** revert commit; apply path passes a nil provider to ComputePlan; ComputePlan tolerates nil and falls back to ConfigHash compare. Build still succeeds. No runtime behavior change for v1 plugins.

### Task T3.6d: Update cross-package fakes in `module/infra_module_integration_test.go`

**Files:**
- Modify: `module/infra_module_integration_test.go` (4 ComputePlan call sites in cross-package integration tests)

**Step 1: Write failing test** — N/A; the existing cross-package tests fail to compile after T3.6a's signature change. The "test" here is `go test ./module/...` returning compilation errors.

**Step 2: Verify compilation failure** — `go test ./module/... -count=1` → expect `not enough arguments in call to platform.ComputePlan`.

**Step 3: Implement** — import `platform` (or the new fake-provider package) and pass an inline fake provider into each call. If platform's fake-provider type is unexported, lift it into a small public test helper at `iac/iactest/fakeprovider.go` and import that.

**Step 4: Run test → PASS** (`go test ./module/... -count=1` exits 0).

**Step 5: Commit**

```bash
git add module/infra_module_integration_test.go iac/iactest/fakeprovider.go
git commit -m "test(iac): update cross-package fakes for ComputePlan provider arg"
```

### Task T3.6e: Implement Diff dispatch + bounded concurrency in ComputePlan body (TDD red→green in one commit)

**Files:**
- Modify: `platform/differ.go` (replace ConfigHash-only path with Diff dispatch)
- Create: `platform/differ_replace_test.go` (NEW — concrete TDD harness for Replace emission; lands together with the implementation)

**Step 1: Write the failing test** in `platform/differ_replace_test.go`:

```go
package platform

import (
    "context"
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestComputePlan_NeedsReplaceEmitsReplaceAction(t *testing.T) {
    desired := []interfaces.ResourceSpec{
        {Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
    }
    current := []interfaces.ResourceState{
        {Name: "vpc", Type: "infra.vpc", ProviderID: "old", AppliedConfig: map[string]any{"region": "nyc1"}},
    }
    fp := newFakeProviderWithDiff(t, &interfaces.DiffResult{
        NeedsReplace: true,
        Changes: []interfaces.FieldChange{{Path: "region", Old: "nyc1", New: "nyc3", ForceNew: true}},
    })
    plan, err := ComputePlan(context.Background(), fp, desired, current)
    if err != nil { t.Fatal(err) }
    if len(plan.Actions) != 1 || plan.Actions[0].Action != "replace" {
        t.Errorf("expected replace action, got %+v", plan.Actions)
    }
}

func TestComputePlan_ForceNewWithoutNeedsReplace_StillEmitsReplace(t *testing.T) {
    desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}}}
    current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}
    fp := newFakeProviderWithDiff(t, &interfaces.DiffResult{
        NeedsUpdate: true,
        Changes: []interfaces.FieldChange{{Path: "region", ForceNew: true}},
    })
    plan, _ := ComputePlan(context.Background(), fp, desired, current)
    if plan.Actions[0].Action != "replace" {
        t.Errorf("ForceNew should imply replace; got %s", plan.Actions[0].Action)
    }
}
```

(`newFakeProviderWithDiff` is a small test-helper added in T3.6a's `platform/fake_provider_test.go` which already supports configurable Diff results — extend that fake here.)

**Step 2: Run test → FAIL** (ComputePlan still uses legacy ConfigHash-only path; emits update or skip, never replace).

**Step 3: Implement**

For each existing resource (not net-new):
1. Call `p.ResourceDriver(spec.Type).Diff(ctx, spec, currentOut)` — guarded by cache hit from T3.6f when present.
2. Emit:
   - `Action="replace"` if `diff.NeedsReplace || hasForceNew(diff.Changes)`
   - `Action="update"` if `diff.NeedsUpdate && !replace`
   - skip if neither (no-op)
3. Use `golang.org/x/sync/errgroup` for bounded concurrency (default 8 from `WFCTL_PLAN_DIFF_CONCURRENCY` env var; clamped 1..32).

For net-new resources (no current state), continue to emit `Action="create"` without calling Diff.

For resources removed-from-config, continue to emit `Action="delete"` (the latent bug-fix surface — see T3.10).

**Step 4: Run test → PASS.**

**Step 5: Commit (test + implementation in ONE commit — the rev3 fix for the cycle-2 self-contradiction)**

```bash
git add platform/differ.go platform/differ_replace_test.go
git commit -m "feat(iac): ComputePlan dispatches Diff per resource; emits replace action when ForceNew or NeedsReplace"
```

**Rollback (T3.6e):** revert commit; ComputePlan returns to ConfigHash-only behavior; v2 plugins lose Replace emission until re-applied. Not a structural revert — keep T3.6a/b/c/d intact since the signature is backwards-compatible. The `differ_replace_test.go` file (NEW in this commit) is deleted by the revert. The `fake_provider_test.go` file (created in T3.6a, used by both T3.6a's no-op test AND T3.6e's Replace test) PERSISTS across the revert — it provides the no-op fake that T3.6a's in-package tests still use post-revert.

### Task T3.6f: Wire diff cache from T3.5 into ComputePlan dispatch

**Files:**
- Modify: `platform/differ.go` (cache lookup before Diff call)
- Test: `platform/differ_cache_test.go`

**Step 1: Write failing test** — assert second ComputePlan invocation against unchanged inputs hits cache (counter on fake provider's `Diff` method shows 1, not 2).

**Step 2: Run test → FAIL** (ComputePlan calls Diff every time).

**Step 3: Implement** — before each `Diff(ctx, spec, current)` call, compute cache key per T3.5 (plugin-version, type, providerID, sha-config, sha-outputs); on hit, use cached result; on miss, call Diff and persist.

**Step 4: Run test → PASS.**

**Step 5: Commit**

```bash
git add platform/differ.go platform/differ_cache_test.go
git commit -m "perf(iac): ComputePlan consults diffcache before invoking provider.Diff"
```

**T3.6 verification (after T3.6a–f all merged):**

Per writing-plans verification table, "Plugin / extension → load into host + exercise representative call". Unit tests are necessary but not sufficient for a refactor that places the gRPC roundtrip in the plan critical path. The conformance scenario `Scenario_DiffSurvivesGRPCRoundTrip` (T7.6 in W-7) is the binding gRPC verification. W-3 ships before W-7, so W-3's runtime-launch-validation in T3.9 MUST exercise a real gRPC-loaded plugin (per `plugin/sdk/iaclint/` precedent), not just unit tests.

### Task T3.7: Wire `cmd/wfctl/infra_apply.go` to use `wfctlhelpers.ApplyPlan` for v2 plugins (manifest-driven, no env-var)

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (the apply path — branch on manifest field)
- Test: `cmd/wfctl/infra_apply_v2_test.go`

**Step 1: Write failing test** — given a fixture provider whose loaded manifest sets `iacProvider.computePlanVersion: "v2"`, assert `wfctlhelpers.ApplyPlan` is invoked, NOT the legacy `provider.Apply`. Given a v1 manifest, assert legacy path.

**Step 2: Run test → FAIL.**

**Step 3: Implement**

Read the loaded plugin's manifest (the `plugin/sdk/manifest.go` types from T3.0). Branch:

```go
// pseudo-code
if provider.Manifest().IaCProvider.EffectiveComputePlanVersion() == "v2" {
    return wfctlhelpers.ApplyPlan(ctx, provider, plan)
}
return provider.Apply(ctx, plan) // legacy v1 path
```

NO env-var. NO operator-flippable gate. The v1/v2 routing is plugin-author-controlled via plugin.json. (This is the rev2 fix for the env-var-placeholder window — there is no transitional flag to misuse.)

**Step 4: Run test → PASS.**

**Step 5: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_v2_test.go
git commit -m "feat(iac): apply path branches on plugin manifest's iacProvider.computePlanVersion"
```

**Rollback (T3.7):** revert commit; apply path always calls `provider.Apply` (legacy v1 path) regardless of manifest. Plugins that set `v2` lose the new behavior but don't break.

### Task T3.9: Runtime-launch-validation (gRPC-loaded plugin)

**Files:** new fixture in `internal/testdata/stub-provider/` (test-only)

**Step 1:** Build wfctl: `go build -o /tmp/wfctl ./cmd/wfctl`
**Step 2:** Build a real gRPC-loaded stub provider plugin in `internal/testdata/stub-provider/` whose `plugin.json` sets `iacProvider.computePlanVersion: "v2"`. The plugin returns a deterministic `Diff` result with `NeedsReplace: true` for resources whose Config differs from a baseline.
**Step 3:** Run `/tmp/wfctl infra apply -c /tmp/stub-config.yaml --env test` against a state where the stub provider plugin is registered. Expect:
   - ComputePlan calls Diff over the loaded gRPC plugin (not in-process).
   - Plan contains an action with `Action: "replace"`.
   - Apply invokes Delete + Create on the stub (asserted via the stub's stderr log).
**Step 4:** Confirm typed-slice Outputs survive the structpb roundtrip (per design §strict-gRPC-contracts), exercising `Scenario_DiffSurvivesGRPCRoundTrip`'s motivating bug class.

**Rollback (T3.9):** revert commits. Stub plugin in `internal/testdata/` is test-only, not shipped to operators.

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
- `go test ./iac/wfctlhelpers/... ./iac/diffcache/... ./platform/... ./cmd/wfctl/... ./plugin/sdk/... -count=1 -race`
- Manual (T3.9 runtime-launch-validation): build wfctl, build the in-tree gRPC stub provider with `iacProvider.computePlanVersion: "v2"`, run `wfctl infra apply` against it; confirm Replace action behavior surfaces (Delete + Create on the stub) and typed-slice Outputs survive structpb roundtrip.
- Conformance scenarios listed in this PR (`Scenario_NeedsReplaceTriggersReplaceAction`, `Scenario_DeleteActionInApplyInvokesDriverDelete`, `Scenario_DiffSurvivesGRPCRoundTrip`, `Scenario_UpsertOnAlreadyExists`) are implemented in W-7 — this PR ships the behavior they test, W-7 ships the assertion harness.

**Rollback (PR W-3):** revert commits. Plugins that haven't set `iacProvider.computePlanVersion: "v2"` (i.e., everything except P-DO post-merge) default to v1 and take the legacy `provider.Apply` path; no behavior change for un-migrated providers. Reverting T3.0 alone (the manifest field schema) is safe because `EffectiveComputePlanVersion()` returns v1 on absent field — but the entire PR reverts cleanly as a unit.

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

### Task T4.4: Documentation

> **Note (rev2):** prior T4.3 (stub `Scenario_CrossResourceConstraintRejection`) removed — W-7 owns BOTH stub and body.

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

### Task T5.7: Runtime-launch-validation

> **Note (rev2):** prior T5.6 (stub `Scenario_InfraOutputCrossModuleResolution`) removed — W-7 owns BOTH stub and body.

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

### Task T6.4: Documentation + verification

> **Note (rev2):** prior T6.3 (stubs for `Scenario_ProtectedReplaceWithoutOverride`, `Scenario_ProtectedReplaceWithOverride`) removed — W-7 owns BOTH stubs and bodies.

**Verification (doc):** `mdformat --check docs/WFCTL.md && find docs -name "*.md" -exec markdown-link-check {} +` — exit 0 + no broken anchors.

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

11 scenarios, one per task. **rev3:** each task creates BOTH the scenario file (the stub-pattern was removed in earlier rev2 cleanup) AND the body, plus a sibling self-test in `iac/conformance/scenarios_test.go` exercising the scenario against an in-tree fake provider. Format per scenario:

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
- Create: `.github/workflows/conformance-smoke.yml` (workflow repo) + sister workflow in workflow-plugin-digitalocean (added in P-DO/TP5; this task adds the workflow-repo trigger only)
- Create: `.github/workflows/conformance-budget-check.yml` (kill-switch — runs every smoke job's pre-step)

**Step 1-5:**

**Creds source (rev2 — finding resolved):**
DigitalOcean does not currently issue API tokens via GitHub OIDC (verified via DO docs as of 2026-05-03). Use a long-lived API token stored in GitHub Actions repository secret `DO_CONFORMANCE_API_TOKEN`, scoped to a dedicated DigitalOcean account `wfctl-conformance@gocodealone.dev` that holds NO production resources. Token rotation: quarterly via a tracked task; rotation procedure documented in `docs/conformance-runbook.md` (added in this task).

**Cost cap kill-switch (rev3 — addressed cycle-2 finding on overhead + calibration):**
Pre-step in every smoke job runs `conformance-budget-check.yml` which:
1. Calls DO billing API (`GET /v2/customers/my/balance` + `GET /v2/customers/my/billing_history`) using the conformance account token.
2. Computes current-month spend on the conformance account.
3. If spend > $25/month (rev3 cap — bumped from $5 per TC1.5 finding to accommodate ad-hoc cascade dry-runs), the job aborts BEFORE provisioning anything, with a step output explaining the abort + a GitHub issue auto-filed against the workflow repo (label: `conformance-budget-exceeded`, dedup'd via the same script as T7.14).
4. If spend ≤ $25/month, the job proceeds; per-PR estimated cost is ~$0.0005 (rev6 cost correction per cycle-5; was incorrectly stated as $0.005 in cycles 1-5). Math: Droplet s-1vcpu-512mb-10gb at $4/mo ÷ 30 days ÷ 24 hrs ÷ 60 min = $0.0000926/min × ~5 min lifetime ≈ $0.000463 ≈ **$0.0005/PR** (sub-tenth-cent). Re-verify the Droplet's per-month price against current DO pricing before T7.13 ships.

**Overhead reduction (rev3+rev4 — caching per-PR-base-SHA via actions/cache@v4):**

```yaml
# In conformance-budget-check.yml (rev5 — fixed step ordering):
concurrency:
  group: ${{ github.event.pull_request.base.sha }}-budget-check
  cancel-in-progress: false
steps:
  # rev5 — discrete first step computes the hour-bucket as a step output,
  # so the cache step (next) can reference it via steps.hour.outputs.value.
  - id: hour
    run: echo "value=$(date -u +%Y%m%d%H)" >> $GITHUB_OUTPUT
  - uses: actions/cache@v4
    id: budget-cache
    # rev5 inline note — actions/cache@v4 does post-step write-back automatically:
    # if cache-hit is false, the action records the path's contents at job-end
    # and uploads under this key for the next run on the same key. No explicit
    # upload-cache step is needed.
    with:
      key: budget-${{ github.event.pull_request.base.sha }}-${{ steps.hour.outputs.value }}
      path: /tmp/budget-result.json
  - if: steps.budget-cache.outputs.cache-hit != 'true'
    run: |
      curl -sH "Authorization: Bearer $DO_CONFORMANCE_API_TOKEN" \
        "https://api.digitalocean.com/v2/customers/my/balance" > /tmp/budget-result.json
  - run: |
      SPEND=$(jq -r '.month_to_date_usage' /tmp/budget-result.json)
      if (( $(echo "$SPEND > 25" | bc -l) )); then
        echo "::error::Conformance budget exceeded: $SPEND > 25"
        exit 1
      fi
      if (( $(echo "$SPEND > 15" | bc -l) )); then
        # rev4 — second-channel alert at 60% of cap (uses dedup helper).
        bash .github/workflows/scripts/file-or-comment-leak-issue.sh "$SPEND" "Spend approaching cap"
      fi
```

The cache is keyed on PR-base-SHA + hour, providing 1-hour TTL with effectively unlimited cache backend (GitHub Actions cache supports up to 10GB per repo). Concurrency-group prevents duplicate API calls within the same hour for the same PR series. This collapses N jobs/PR into 1 budget-API call/PR-base-SHA/hour, well within DO's 5000/hour rate limit even at scale.

**Calibration plan (rev3):** the $25/mo threshold is an estimate; recalibrate after 30 days of operational data. Documented in `docs/conformance-runbook.md` (created in this task) under § "Budget calibration"; tracked as a recurring task (re-evaluate the cap on the 30th day after T7.13 ships).

**Budget approval (rev4 — addresses cycle-3 Important 7):** the $25/mo cap was approved by `jon@langevin.me` on 2026-05-03 (this design pass). Recorded in `docs/conformance-runbook.md` § "Budget approval" with: who approved, date, hard-stop threshold ($25/mo), soft-alert threshold ($15/mo), alert channels (GitHub issue dedup'd via T7.14 helper; Slack `#wfctl-ops` if configured). The runbook is the source of truth; changes to the cap require a new entry in the runbook + an updated PR.

**Smoke job:**
- Triggers on PRs touching `iac/`, `platform/`, or `cmd/wfctl/infra*` in workflow repo (and any change in workflow-plugin-digitalocean repo).
- Runs `Scenario_NeedsReplaceTriggersReplaceAction` against a real DO Droplet (s-1vcpu-512mb-10gb, region nyc1).
- `t.Cleanup` force-deletes the resource on test exit (success OR failure path).
- Outer-job `always()` cleanup step: `doctl compute droplet list --tag-name conformance-pr-${PR_NUMBER} | xargs doctl compute droplet delete -f` to catch resources orphaned by panicking tests.
- Hourly safety scrubber (separate scheduled workflow `conformance-leak-scrubber.yml` — listed as T7.14 below): deletes any conformance-tagged resource older than 1 hour.

### Task T7.14: Conformance leak scrubber + balance alarm (with dedup)

**Files:**
- Create: `.github/workflows/conformance-leak-scrubber.yml` (cron `0 * * * *`)
- Create: `.github/workflows/scripts/file-or-comment-leak-issue.sh` (dedup helper)

**Step 1-5:** Hourly job: list DO resources tagged `conformance-pr-*` older than 1h; delete each; collect counts.

**Dedup logic (rev3+rev4 — addressed cycle-2 + cycle-3 findings):**

```bash
# scripts/file-or-comment-leak-issue.sh
#!/usr/bin/env bash
set -euo pipefail
SCRUBBED_COUNT="$1"
SCRUBBED_DETAILS="$2"
PRIMARY_LABEL="conformance-leak-incident"
HELPER_LABEL="auto-filed-leak"  # rev4 — only the helper sets this
# rev4 — match issues that have BOTH labels (operator-filed issues with only the
# primary label are skipped, preserving manual investigation context)
EXISTING=$(gh issue list --label "$PRIMARY_LABEL" --label "$HELPER_LABEL" --state open --json number --jq '.[0].number // empty')
if [ -n "$EXISTING" ]; then
  gh issue comment "$EXISTING" --body "Scrubber run at $(date -u): scrubbed $SCRUBBED_COUNT resources. Details: $SCRUBBED_DETAILS"
else
  gh issue create --label "$PRIMARY_LABEL" --label "$HELPER_LABEL" \
    --title "Conformance leak: $SCRUBBED_COUNT resources scrubbed" \
    --body "First leak detected at $(date -u). Scrubbed $SCRUBBED_COUNT resources. Details: $SCRUBBED_DETAILS"
fi
```

The workflow only runs the helper if `SCRUBBED_COUNT > 0`. The helper checks for an existing OPEN issue with BOTH labels (`conformance-leak-incident` AND `auto-filed-leak`); if present, appends a comment; if absent, creates a new issue with both labels. Operators who file issues manually with only the primary label (for human investigation) are not appended to. Operators close the auto-filed issue after investigating; next leak opens a fresh auto-filed issue with full history visible via the closed-issue trail.

**Operator runbook note (rev5 — addresses cycle-4 Important on label-removal failure mode):** add to `docs/conformance-runbook.md` (created in T7.13) under § "Helper conventions":
> **Do not remove the `auto-filed-leak` label from helper-filed issues during triage.** It is the dedup key; removing it causes the next leak to file a NEW issue rather than appending. To stop the helper from dedup'ing onto a particular issue, **close the issue** (the helper queries `--state open`).
>
> If you need a cleaner postmortem swimlane, file a new issue with only the primary label `conformance-leak-incident` (NOT `auto-filed-leak`), link to the auto-filed issue, then close the auto-filed one. The next leak will re-open the dedup chain on a fresh helper-filed issue, leaving your postmortem issue undisturbed.

Same workflow also checks balance API; if balance > $25/mo (rev3 cap — bumped from $5 per TC1.5 finding) OR consecutive scrub events > 3/day, file an issue with label `conformance-budget-incident` (also via the dedup helper, scoped to that label).

Commit: `ci(conformance): hourly leak scrubber + balance incident filing with dedup-by-existing-issue`

(Continuing T7.13:)

Commit: `ci(conformance): DO smoke gate w/ pre-flight budget kill-switch + always() cleanup`

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

**Step 1-5:** Detects `func (p *XProvider) Plan(...)` body matching configHash compare pattern; replaces with `return wfctlhelpers.Plan(ctx, p, desired, current)`. Aborts (with informative report) if body has out-of-template logic. Golden-file test: input source → expected output diff. Honors `// wfctl:skip-iac-codemod` marker (rev3 — added per cycle-2 minor finding so an implementer reading T8.3 alone sees the marker convention).

Commit: `feat(codemod): refactor-plan mode (canonical pattern detection + rewrite); honors // wfctl:skip-iac-codemod marker`

### Task T8.4: Implement `refactor-apply` mode (with informative reports)

**Files:**
- Create: `cmd/iac-codemod/refactor_apply.go`
- Test: `cmd/iac-codemod/refactor_apply_test.go`

**Step 1-5:** Detects switch-on-action in Apply. For canonical patterns: rewrites to `return wfctlhelpers.ApplyPlan(ctx, p, plan)`. For non-canonical idioms (descriptions in design §W-8):
- DO upsert recovery → emit suggested wfctlhelpers.upsertSupporter hook patch
- AWS update+replace collapse → emit "manual port required" finding with line numbers
- Custom error wrapping → emit extension-point hook + sample patch

Output `codemod-report.md` with per-file findings + suggested handling. Honors `// wfctl:skip-iac-codemod` marker (rev3 — added per cycle-2 minor finding).

Commit: `feat(codemod): refactor-apply with informative non-canonical idiom reports; honors // wfctl:skip-iac-codemod marker`

### Task T8.5: Implement `add-validate-plan` mode

**Files:**
- Create: `cmd/iac-codemod/add_validate_plan.go`
- Test: golden-file

**Step 1-5:** Detect providers missing `ValidatePlan`; insert no-op stub. Skip if marker `// wfctl:skip-iac-codemod` present.

> **Marker convention (rev2):** ALL iac-codemod modes (`refactor-plan`, `refactor-apply`, `add-validate-plan`, `lint`) honor a single marker: `// wfctl:skip-iac-codemod` (not `// wfctl:skip-codemod` or `// wfctl:skip-plan-codemod`). This is unified across T8.3, T8.4, T8.5, T8.6 to prevent the silent-no-op surface of mismatched markers. Each mode also surfaces a list of skipped sites in its report.

Commit: `feat(codemod): add-validate-plan mode (no-op stub injection); honors // wfctl:skip-iac-codemod marker`

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

## PR W-9: Cross-plugin build verification (CI gate only)

**Goal:** Verify the W-1 + W-3a + W-3b + W-4 interface changes don't break AWS/GCP/Azure plugins (which stay un-migrated at v1).

> **Note (rev4 — cycle-3 YAGNI fix; rev5 — ADR added per cycle-4):** rev3's `ProviderPlanner` interface was speculative — no concrete consumer existed in the plan series. Cycle-3 review flagged it as YAGNI; rev4 drops it entirely. The interface ships when the first concrete adapter (Tofu/Pulumi-style) lands with its own design discussion. W-9 is now a single-task CI-only PR (T9.3 is renumbered to T9.1).
>
> **Decision record (rev5):** see `decisions/0001-providerplanner-deferred-to-first-consumer.md` for the recorded reasoning. The user-mandate "don't defer fixes" refers to the 8 root-cause issues from the design pass, not to speculative future-interface scaffolding. ProviderPlanner ships when the first concrete consumer (Tofu/Pulumi adapter) arrives with its own design discussion. The conformance-suite + cross-plugin-build CI gate (W-9 T9.1) provide the regression net for any future interface evolution.

### Task T9.1: Cross-plugin build verification (AWS/GCP/Azure stay-on-v1 compile gate) + ADR for ProviderPlanner deferral

**Files:**
- Create: `.github/workflows/cross-plugin-build-test.yml` (workflow repo)
- Create: `docs/adr/006-providerplanner-deferred-to-first-consumer.md` (rev6 — relocated from `decisions/0001-...` per cycle-5 Critical 1; the workflow repo's pre-existing ADR convention is `docs/adr/NNN-...md` 3-digit Nygard-3 format with 5 prior ADRs `001-yaml-driven-config.md` through `005-field-contracts.md`)
- (No source changes in AWS/GCP/Azure repos; this is a verification gate)

**Step 0 (rev5/rev6): Create the ADR** at `docs/adr/006-providerplanner-deferred-to-first-consumer.md` using the repo-native Nygard-3 format (matches `docs/adr/005-field-contracts.md` precedent):

```markdown
# ADR 006: ProviderPlanner Deferred to First Consumer

## Status
Provisional — awaiting user ratification. See plan §W-9 Pending user ratification (the cycle-5 review correctly caught that prior revisions ghost-wrote user approval; rev6 honestly attributes the deferral to the autonomous-pipeline cycle-3 → cycle-5 reasoning. User can override on next interaction; if no override, the deferral stands.)

## Context

Surfaced during the autonomous design pass for `docs/plans/2026-05-03-iac-conformance-and-replace-design.md` (the 8 root-cause issues from core-dump's self-hosted-PG deploy iteration). Rev1–rev3 of the implementation plan included an optional `ProviderPlanner` interface in W-9, intended to preserve a Tofu/Pulumi-style extension hook without locking the IaCProvider interface to `platform.ComputePlan`'s `driver.Diff` dispatch.

Cycle-3 adversarial review flagged the interface as YAGNI (no concrete consumer in the plan series). Rev3 downgraded the interface to "definition only — reserved for future adapter; not consumed by core wfctl." Cycle-4 review noted that the downgraded form is the worst of both worlds: an interface name lives in `interfaces/iac_provider.go` that no caller exercises and that a future adapter design may wish to define differently. Cycle-4 also raised the concern that dropping the interface contradicts the user-mandate "don't defer any fixes."

Cycle-5 review correctly caught that rev5's earlier ADR draft attributed the decision to "Decided by: Jon Langevin" without explicit user consent. Rev6 corrects the attribution and surfaces the deferral as a Provisional decision pending user ratification.

**Decided by:** Claude (autonomous-pipeline rev3-rev6). User-mandate disambiguation: the "8 root-cause issues" list does NOT include ProviderPlanner; the user said "don't defer fixes" referring to those concrete issues, not to speculative future-interface scaffolding. The autonomous-pipeline's reading is documented here for the user to ratify or override.

## Decision

The optional `ProviderPlanner` interface is deferred. It does NOT ship in this plan series. W-9 in the implementation plan ships only as a CI-only PR (cross-plugin-build gate + this ADR).

## Consequences

**Positive:** Avoids landing a speculative interface in `interfaces/iac_provider.go` with no exerciser. Future Tofu/Pulumi adapter author defines the interface freshly with concrete consumer + design discussion + tests in one PR (estimated cost: ~5 tasks). The cross-plugin-build CI gate (this same PR) provides the regression net for any future interface evolution.

**Negative:** Future adapter PR carries the cost of defining the interface. If a second-design discovery surfaces a different shape than ProviderPlanner would have had, the adapter PR's design discussion handles it; the deferral preserves optionality.

**If user reverses the deferral:** file ADR 007 superseding this one; W-9 expands to include the ProviderPlanner interface definition + a single TDD test verifying type-assertion compiles (same as rev1-rev3 had). Estimated cost: ~30 minutes of plan revision + ~1 hour implementation in W-9.
```

**Step 1-5: Implement the cross-plugin build CI gate** (per the YAML below in this same task).

**Commit boundary (rev6 — addresses cycle-5 Important on T9.1 commit ordering):** Step 0 (the ADR) and Steps 1-5 (the workflow file) ship as ONE commit:

```bash
git add docs/adr/006-providerplanner-deferred-to-first-consumer.md .github/workflows/cross-plugin-build-test.yml
git commit -m "ci(iac): cross-plugin build gate + ADR 006 for ProviderPlanner deferral"
```

Single PR, single commit, both artifacts atomic. The ADR is the rationale for why W-9 ships ONLY the CI gate (and not the ProviderPlanner interface) — they belong together.

**Step 1: Write failing CI workflow** — workflow check that clones AWS, GCP, Azure plugin repos at their main branches, runs `go build ./...` against THIS PR's workflow head via a `go.mod` replace directive. Initially fails because the workflow doesn't exist.

**Step 2: Run on draft PR → expect FAIL** (workflow doesn't exist).

**Step 3: Implement** — per-plugin matrix:
```yaml
name: cross-plugin-build-test
on:
  pull_request:
    paths:
      # rev4 — cycle-3 minor: only run on IaC-touching PRs to save ~5min/docs-PR
      - 'interfaces/**'
      - 'iac/**'
      - 'platform/**'
      - 'plugin/sdk/**'
      - '.github/workflows/cross-plugin-build-test.yml'
jobs:
  cross-plugin-build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        plugin: [workflow-plugin-aws, workflow-plugin-gcp, workflow-plugin-azure]
    steps:
      - uses: actions/checkout@v4
        with: { path: workflow }
      - uses: actions/checkout@v4
        with:
          repository: GoCodeAlone/${{ matrix.plugin }}
          path: ${{ matrix.plugin }}
      - uses: actions/setup-go@v5
        with: { go-version-file: workflow/go.mod }
      # The replace directive points the plugin's go.mod at THIS PR's checkout
      # of workflow (../workflow), NOT at workflow main. The gate exercises
      # whether the PR's interface changes break AWS/GCP/Azure compilation —
      # which is precisely the per-PR signal we want.
      - run: |
          cd ${{ matrix.plugin }}
          go mod edit -replace github.com/GoCodeAlone/workflow=../workflow
          go mod tidy
          go build ./...
          # NOTE: not `go test` — those plugins have their own CI; we just verify compile-compat
```

**Step 4: Run on draft PR → expect PASS for all 3** (per the design's assumption that W-1 + W-3a + W-3b + W-4 introduce only backwards-compatible interface additions).

**Step 5: Commit (rev6 — single commit per the boundary section above; ADR + workflow file in one commit):** see the `git add ... && git commit` invocation in the "Commit boundary" subsection above (combined ADR + workflow file).

**Future-required-method note (rev3):** if any future W-* PR adds a REQUIRED method to `IaCProvider` (not optional via type-assertion like `ProviderValidator`), this gate becomes blocking. Workflow CI MUST call out such a change as a release-note item; the plugin authors must update their interface implementations before the corresponding workflow release tags. T9.1's gate will turn red, surfacing the breakage at PR time.

**Verification (PR W-9):** CI cross-plugin-build-test job green on this PR.

**Rollback (PR W-9):** revert commit; CI workflow disappears; per-plugin breakage surfaces in those repos' own CI on next dependency bump (later, but not silently undetected forever).

---

## PR P-DO: DigitalOcean plugin migration to v2

**Goal:** DO plugin opts into computePlanVersion: v2; runs codemod; hand-ports upsert recovery to wfctlhelpers.ApplyPlan upsertSupporter hook; implements ValidatePlan for DO region constraints; adds conformance test.

### Task TP1: Run codemod against DO + upload report as GitHub Actions artifact

**Files:** `.github/workflows/codemod-report.yml` (new — CI workflow that runs codemod on each PR commit and uploads the report)

**Step 1:** Create `.github/workflows/codemod-report.yml`:
```yaml
name: codemod-report
on: pull_request
jobs:
  codemod-report:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: 'go.mod' }
      - run: |
          go run github.com/GoCodeAlone/workflow/cmd/iac-codemod refactor-apply -dry-run . > /tmp/codemod-report.md
          # Also a short PR-comment summary
          head -30 /tmp/codemod-report.md > /tmp/codemod-summary.md
          echo "" >> /tmp/codemod-summary.md
          echo "_Full report (90-day retention) attached as workflow artifact._" >> /tmp/codemod-summary.md
      - uses: actions/upload-artifact@v4
        with:
          name: codemod-report-${{ github.event.pull_request.number }}
          path: /tmp/codemod-report.md
          retention-days: 90
      - name: Comment summary on PR (first-party action SHA-pinned per workflow security policy)
        # rev5 — pin to commit SHA even though first-party. Tag mutability is a known risk
        # category covered by the workflow security-policy precedent. Renovate config tracks
        # upstream releases via .github/renovate.json.
        uses: actions/github-script@60a0d83039c74a4aee543508d2ffcb1c3799cdea  # v7.0.1
        with:
          script: |
            const fs = require('fs');
            const summary = fs.readFileSync('/tmp/codemod-summary.md', 'utf8');
            const marker = '<!-- codemod-report-sticky -->';
            const body = `${marker}\n${summary}`;
            // rev5 — paginate to handle PRs with >30 comments (default page size).
            // github.paginate auto-iterates all pages.
            const allComments = await github.paginate(github.rest.issues.listComments, {
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: context.issue.number,
              per_page: 100,
            });
            const existing = allComments.find(c => c.body && c.body.startsWith(marker));
            if (existing) {
              await github.rest.issues.updateComment({
                owner: context.repo.owner,
                repo: context.repo.repo,
                comment_id: existing.id,
                body,
              });
            } else {
              await github.rest.issues.createComment({
                owner: context.repo.owner,
                repo: context.repo.repo,
                issue_number: context.issue.number,
                body,
              });
            }
```

**rev4 — supply-chain fix:** rev3's `marocchino/sticky-pull-request-comment@v2` was a third-party action pinned to a mutable major-version tag, violating the workflow security policy (third-party actions must be SHA-pinned). rev4 replaces it with `actions/github-script@v7` (first-party) running an inline JS implementation of the same sticky-comment behavior. No supply-chain dependency outside the GoCodeAlone-controlled chain.

**Step 2:** Run the workflow once (push the PR-DO branch); confirm `/tmp/codemod-report.md` is uploaded as an artifact AND a sticky PR comment summarizes the top 30 lines with a link to the artifact.

**Step 3:** Review `/tmp/codemod-report.md`. Expect: report identifies upsert recovery as non-canonical + suggests upsertSupporter hook patch + lists per-driver findings.

**Step 4:** If the migration includes a non-trivial decision (e.g., choosing to keep DO's upsert recovery vs. canonicalizing it), record that decision as `decisions/<NNNN>-do-iac-v2-migration.md` per `recording-decisions/SKILL.md` and cite the codemod artifact URL in the ADR.

**Rationale (rev3 — addressed cycle-2 finding):** rev2's "embed report in PR body" had no precedent in workflow-plugin-* repos and ran into PR-body length limits, mutability (descriptions can be edited after merge), no per-finding back-link, and unspecified embedding mechanism. CI artifacts are an established GitHub Actions pattern with 90-day retention, immutable once uploaded, and provide a permanent download URL surfaced from the PR Checks tab. The sticky PR comment gives drive-by reviewers the top-30-line summary without requiring a download.

Commit (the workflow file): `ci(plugin): codemod-report workflow uploads dry-run output as artifact + sticky PR comment summary`

**Rollback (TP1):** revert the workflow file. Codemod can still be run manually; no breaking change.

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

### Task TC1.5: Dry-run cascade replace against ephemeral DO project (on conformance account, with budget pre-check)

> **Note (rev2/rev3 — finding-resolved):** TC2 below applies a 4-resource cascade replace against live `coredump-staging` for the FIRST PRODUCTION USE of W-3a + W-3b + W-5 + W-6 + P-DO together. Without a dry run we have no way to verify cascade order, JIT secret resolution under cascade, or `--allow-replace` partial discovery before unwrapping `protected: true` on staging.

**Account ownership (rev3):** TC1.5 runs against the SAME `wfctl-conformance@gocodealone.dev` DO account used by T7.13's smoke gate (NOT a personal account). The conformance account budget cap is $25/mo (rev3 — bumped from $5/mo) which accommodates one ad-hoc cascade dry-run per major release cycle (~$1 of resources held for ~30 min) plus the per-PR smoke gate baseline (rev6 corrected: ~$0.0005/PR × ~600 PRs/mo ≈ $0.30/mo).

**Pre-flight budget check (rev3):** before provisioning anything, run the same `conformance-budget-check.yml` workflow as T7.13 against the conformance account. If the dry-run would push spend past $25/mo, abort with a clear message and a tracked task to either bump the cap or wait for the billing cycle reset.

**Files:**
- Create: `_scratch/tc1-5-cascade-dryrun/infra.yaml` (mirror of staging infra, region nyc1, in an ephemeral DO project named `coredump-cascade-dryrun-${date}`, owned by `wfctl-conformance@gocodealone.dev`)
- Create: `_scratch/tc1-5-cascade-dryrun/.wfctl-lock.yaml` (mirror of TC1's pinned versions)

**Step 1:** From the ephemeral worktree, `wfctl infra plan -c _scratch/tc1-5-cascade-dryrun/infra.yaml --env dryrun -o /tmp/dryrun-plan.json`. Verify plan shape:
- 4 resources marked `replace`: `core-dump-vpc`, `coredump-staging-pg-data`, `coredump-staging-pg`, `coredump-staging-pg-fw`
- Cascade order: VPC first, then dependents (Volume → Droplet → Firewall + App)
- App's `vpc_ref` shows JIT placeholder (`${core-dump-vpc.id}` → "to be resolved at apply time")

**Step 2:** Apply the plan against the ephemeral project: `wfctl infra apply --plan /tmp/dryrun-plan.json --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw -c _scratch/tc1-5-cascade-dryrun/infra.yaml --env dryrun`. Verify:
- Cascade succeeds without partial-failure mid-step
- App's `vpc_ref` is resolved to the new VPC ID
- All 4 resources visible in DO console under the ephemeral project
- `wfctl infra apply` re-run shows zero diff

**Step 3:** Tear down: `wfctl infra destroy --env dryrun -c _scratch/tc1-5-cascade-dryrun/infra.yaml`. Confirm DO project empty.

**Step 4:** Commit a brief notes file (`_scratch/tc1-5-cascade-dryrun/RESULTS.md`) summarizing observed cascade order + any surprises. NOT a full commit; this is decision-input for whether TC2 is safe to run. If anything surprises, fix the underlying bug (file as a workflow or DO-plugin issue) BEFORE proceeding to TC2.

**Step 5:** Delete `_scratch/tc1-5-cascade-dryrun/` from the working tree once TC2 succeeds (artifact, not a permanent file).

**Rollback (TC1.5):** dry-run is in an ephemeral DO project; no production state touched. If dry-run fails, do NOT proceed to TC2 — file the bug, return to W-3/W-5/W-6/P-DO for the fix.

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
3. **AWS/GCP/Azure plugins still build** — owned by W-9/T9.1 (cross-plugin-build-test CI gate; rev4 renumbered from T9.3); manual confirmation: `cd workflow-plugin-aws && go build ./...` exit 0; same for gcp, azure
4. **wfctl regression suite** — `cd workflow && go test ./... -count=1 -race`
5. **codemod advisory reports filed** — issues open against AWS/GCP/Azure plugin repos with the lint-only output (TP1 attached the DO report to the P-DO PR; per-non-DO advisory issues filed manually after W-8 lands)

Sequencing dependency graph (must merge in this order; alignment-check enforces):

```
W-1 → W-2 → W-3a → W-3b → W-4 → W-5 → W-6 → W-7 → W-8 → P-DO → C-1
                                       ↘
                                        W-9 (CI-only; can merge anytime after W-3b; not on P-DO critical path)
```

**rev4 sequencing changes (per cycle-3 finding):**
- W-9 dropped its ProviderPlanner interface (YAGNI per cycle-3 Important 5; ADR `decisions/0001-providerplanner-deferred-to-first-consumer.md` records the cycle-4 reasoning); W-9 is now a single CI-only PR (T9.1 = cross-plugin-build gate). It can merge anytime after W-3b lands (the gate exercises whether W-3b's interface changes break AWS/GCP/Azure compilation). It is NOT on P-DO's critical path — T9.1's CI gate runs on every plugin PR irrespective of W-9's merge status.
- W-4 and W-9 no longer share a file (rev3's W-4↔W-9 race was on `interfaces/iac_provider.go` for `ProviderValidator` vs `ProviderPlanner` — with `ProviderPlanner` dropped in rev4, that overlap disappears).

**Drafting (rev3 — addresses cycle-2 file-overlap finding):**

The graph above is the strict merge order. The "parallel drafting" claim from rev1/rev2 was over-optimistic; rev3 enumerates exactly which file-level overlaps prevent concurrent drafting:

| Pair | Shared file or function | Resolution |
|---|---|---|
| W-1 / W-5 | `cmd/wfctl/infra.go::runInfraPlan` (T1.3, T1.6 vs. T5.4) | W-5 must rebase on W-1's tip before drafting; serial-only on this function |
| ~~W-4 / W-9~~ | ~~`interfaces/iac_provider.go`~~ | **No longer overlapping (rev4):** W-9 dropped ProviderPlanner; W-9 is CI-only and doesn't modify Go source |
| W-1 / W-3a | `iac/wfctlhelpers/` package (T1.5 wires drift-postcondition; T3.1 implements helper body) | W-1 ships interface stub; W-3a fills body — same as standard Go interface-defined-first pattern |
| W-2 / W-1 | Both add fields to `IaCPlan` schema (T1.1 adds `SchemaVersion` etc; T2.1 adds refresh-outputs metadata) | Strict order W-1 → W-2 |
| W-3a / W-3b | `iac/wfctlhelpers/` package | Strict order W-3a → W-3b (W-3b's T3.7 + T3.6e consume W-3a's helper) |
| W-7 / W-3b, W-4, W-5, W-6 | conformance scenarios reference symbols from each | W-7 drafts AFTER all four merge |

**Correct drafting/merge sequencing (rev3):**

1. **W-1 + W-2 must serialize** — W-2 needs the SchemaVersion field from W-1 (T1.1). No concurrent-draft window.
2. **W-3a must merge before W-3b can draft** — W-3b's T3.6e + T3.7 consume W-3a's helper package (T3.1) and manifest field (T3.0). Strict order.
3. **W-3b must merge before W-4 can draft** — W-3b's runtime branching is the cliff for any later runtime-affecting change.
4. **W-4 can draft after W-3b merges** (no `interfaces/iac_provider.go` overlap with W-9 anymore — rev4 dropped W-9's interface mod).
5. **W-5 + W-6 can draft + review concurrently after W-3b merges** (independent of W-4 — neither uses `ProviderValidator`): W-5 modifies `cmd/wfctl/infra_apply.go::runInfraApply` and `iac/wfctlhelpers/apply.go`; W-6 modifies `cmd/wfctl/flags.go` and `cmd/wfctl/infra_apply.go::flagParsing` (different functions). Coordinate via `cmd/wfctl/infra_apply.go` if both edit the dispatch branch. (rev4 — addresses cycle-3 minor on W-5/W-6 vs W-4 dep clarity.)
6. **W-7 must merge after W-3b + W-4 + W-5 + W-6** (so all referenced scenario symbols exist).
7. **W-8 can draft + review concurrently with W-7** but merges after W-7 (W-8's lint-mode tests reference some W-7 conformance harness types).
8. **W-9 (CI-only) can draft + review + merge anytime after W-3b** — not on P-DO's critical path.
9. **P-DO can draft after W-7 + W-8 merge** (uses codemod from W-8, conformance from W-7). W-9 is informational; T9.1's CI gate runs on every plugin PR.
10. **C-1 must merge after P-DO ships v0.10.0.**

This matches the dependency graph; the "parallel drafting" claim has been replaced with concrete file-level overlap rules.
