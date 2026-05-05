# IaC Deferred-Work Cleanup + C-1 Wrap-Up Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship 8 PRs across workflow + workflow-plugin-digitalocean + core-dump that close 9 deferred issues from the IaC conformance plan, then unblock + execute core-dump's C-1 staging-PG cutover (TC1 + TC2).

**Architecture:** 3-phase blocker-first sequencing (per design rev3 commit f112d206). Phase 1 ships the workflow#541 align-rule fix (5-line additive change) → cuts v0.21.1, unblocking core-dump PR #190. Phase 2 ships 4 parallel workflow PRs (precision fixes, diagnostic-first schema test, cleanup subcommand with opt-in Enumerator interface, deploy_providers refactor + ADR) plus 1 DO-plugin PR (ctx threading + Plan canonicalization + Enumerator impl). Phase 3 cuts workflow v0.21.2 + DO v0.10.1, bumps core-dump pins, merges TC1, then branches a fresh PR for TC2 cascade-replace of 4 database-tier resources on coredump-staging.

**Tech Stack:** Go 1.22+ on `github.com/GoCodeAlone/{workflow, workflow-plugin-digitalocean}`; YAML configs + GoReleaser CI; `github.com/santhosh-tekuri/jsonschema/v6` for plugin manifest validation; `github.com/digitalocean/godo` for DO API; `golang.org/x/tools/go/analysis` for the W-Precision codemod analyzer fix.

**Base branch:** `main` (per repo)

---

## Scope Manifest

**PR Count:** 9
**Tasks:** 28 (includes per-PR `PR + Copilot cycle + admin-merge` orchestration tasks; implementation work is ~21 tasks)
**Estimated Lines of Change:** ~700 (informational; not enforced)

**Out of scope:**
- workflow#542 (`DO_CONFORMANCE_API_TOKEN` provisioning) — operator-side ops work, deferred-not-required per user direction (2026-05-05). Stays open as tracking issue.
- TC1.5 dry-run on conformance DO account — operator decision per PR #190 body + user direction. Safety belt = W-6 `--allow-replace` + W-3a/b unit tests + workflow#541 fix + git-revert rollback + /healthz verification.
- AWS / GCP / Azure plugin Enumerator implementations — opt-in interface (per N1 closure). Each plugin lands its impl on its own cycle; until then, `wfctl infra cleanup --tag` prints `skipped <provider>: no Enumerator interface`.
- The W-Diagnose-540 fix follow-up (the actual library bisection + version bump or config flag toggle) — this plan ships only the failing-CI test that captures the bug; the fix lands in a separate follow-up PR after diagnosis identifies root cause.

**PR Grouping:**

| PR # | Title | Tasks | Branch | Dependencies |
|------|-------|-------|--------|--------------|
| 1 | W-541: R-A4 align rule consults top-level secrets.generate + entries | Task 1.1, Task 1.2 | feat/r-a4-toplevel-secrets | — (Phase 1 leader) |
| 2 | W-Precision: convert.go silent-drop + iac-codemod accumulator pattern | Task 2.1, Task 2.2 | feat/iac-precision-fixes | Phase 2 Set A; independent |
| 3 | W-Diagnose-540: failing-CI test for plugin SDK manifest schema strictness | Task 3.1 | test/manifest-schema-strict-diagnostic | Phase 2 Set A; independent |
| 4 | W-Cleanup: wfctl infra cleanup --tag subcommand + opt-in Enumerator interface | Task 4.1, Task 4.2, Task 4.3, Task 4.4 | feat/wfctl-infra-cleanup | Phase 2 Set B; independent |
| 5 | W-Refactor: deploy_providers.remoteIaCProvider canonicalization + ADR 010 | Task 5.1, Task 5.2 | refactor/remote-iac-provider | Phase 2 Set B; rebases on PR 4 if WFCTL.md overlap |
| 6 | DO-Plugin (#62 + #63 only): Initialize ctx + Plan canonicalization | Task 6.1, 6.2, 6.3 | fix/initialize-ctx-and-plan-canonical | Phase 2 Set A; independent |
| 6b | DO-Plugin (Enumerator impl, follow-up): EnumerateByTag using godo.Tags.Get | Task 6b.1, 6b.2 | feat/do-enumerator-impl (NEW from DO main post-PR-6-merge) | **BLOCKS on PR 4 Task 4.1 reaching workflow main** |
| 7 | C-1 TC1: pin bumps to v0.21.2 + v0.10.1 (amend existing PR #190) | Task 7.1, Task 7.2 | feat/c1-staging-pg-cutover (existing) | Blocks on Coordination 2 + Coordination 3 |
| 8 | C-1 TC2: cascade-replace 4 protected resources to nyc1 | Task 8.1, Task 8.2, Task 8.3 | feat/c2-staging-pg-cutover-nyc1 (NEW from main post-TC1-merge) | Blocks on PR 7 merge |

**Coordination tasks** (orchestrator-side; NOT part of any PR branch):

| Coord | Action | Repo | Triggers after |
|-------|--------|------|----------------|
| Coord-1 | Cut workflow v0.21.1 tag | GoCodeAlone/workflow | PR 1 merges |
| Coord-2 | Cut workflow-plugin-digitalocean v0.10.1 tag | GoCodeAlone/workflow-plugin-digitalocean | PR 6b merges |
| Coord-3 | Cut workflow v0.21.2 tag | GoCodeAlone/workflow | PRs 2, 3, 4, 5 ALL merged |

**Hard gate**: Task 7.1 starts ONLY after both Coord-2 AND Coord-3 complete (Release CI green + binaries live).

**Status:** Locked (alignment-check PASS at commit 497d1ed5, 2026-05-05)

---

## PR 1: W-541 — R-A4 align rule consults top-level secrets.generate + entries

**Repo:** `GoCodeAlone/workflow`
**Branch:** `feat/r-a4-toplevel-secrets`
**Issue:** workflow#541
**Tag-cut after merge:** v0.21.1
**Why first:** blocks core-dump PR #190 from clean-merging. Currently TC1's revert of the env-var stopgap means `wfctl infra align --strict` fails because R-A4 doesn't see top-level `secrets.generate` keys.

### Task 1.1: Modify `buildAlignContext` to populate `ctx.secretKeys` from top-level `cfg.Secrets.Generate[i].Key` + `cfg.Secrets.Entries[i].Name`

**Files:**
- Modify: `cmd/wfctl/infra_align_rules.go:45-68` (the `buildAlignContext` function and its top-level secrets handling at lines 47-49)

**Step 1: Verify current behavior + field names against origin/main**

Run: `git -C /Users/jon/workspace/workflow show origin/main:cmd/wfctl/infra_align_rules.go | grep -n -A 3 "secretGens\|cfg.Secrets" | head -20`

Confirm: `buildAlignContext` has an existing branch that populates `ctx.secretGens = cfg.Secrets.Generate` but does NOT populate `ctx.secretKeys` from the top-level `cfg.Secrets`. The module-form switch arm IS the only path that populates `ctx.secretKeys`.

Then verify field NAMES (not line numbers — line drift is a recurring failure mode):

```
git -C /Users/jon/workspace/workflow show origin/main:config/secrets_config.go | grep -n "^	Generate \|^	Entries \|type Secret"
git -C /Users/jon/workspace/workflow show origin/main:config/secrets_config.go | grep -n "^	Key \|^	Name "
```

Confirm: `SecretGen` struct has a `Key string` field; `SecretEntry` struct has a `Name string` field. Drop reliance on line numbers.

**Step 2: Write the failing test**

Add to `cmd/wfctl/infra_align_test.go` (after the existing `TestInfraAlign_RA4_SecretsGenerate_DoesNotFire` block at line 421-448):

```go
func TestInfraAlign_RA4_TopLevelSecretsGenerate_DoesNotFire(t *testing.T) {
    yaml := `
appName: test
secrets:
  generate:
    - key: STAGING_PG_PASSWORD
      type: random_hex
      bytes: 32
modules:
  - name: app
    type: container
    config:
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_PG_PASSWORD}@host:5432/db"
`
    findings := runAlignOnYAML(t, yaml, true /* strict */)
    for _, f := range findings {
        if f.Rule == "R-A4" {
            t.Errorf("unexpected R-A4 finding for top-level secrets.generate key: %v", f)
        }
    }
}

func TestInfraAlign_RA4_TopLevelSecretsEntries_DoesNotFire(t *testing.T) {
    yaml := `
appName: test
secrets:
  entries:
    - name: STAGING_PG_PASSWORD
      store: vault
modules:
  - name: app
    type: container
    config:
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_PG_PASSWORD}@host:5432/db"
`
    findings := runAlignOnYAML(t, yaml, true /* strict */)
    for _, f := range findings {
        if f.Rule == "R-A4" {
            t.Errorf("unexpected R-A4 finding for top-level secrets.entries name: %v", f)
        }
    }
}
```

(If the helper `runAlignOnYAML` doesn't exist, look for the equivalent in the existing test file — likely it's a pattern of `parseYAML + buildAlignContext + checkRA4` you can mirror from line 421-448's existing test.)

**Step 3: Run tests to verify they fail**

Run: `cd /Users/jon/workspace/workflow/_worktrees/feat-iac-deferred-cleanup-w541 && GOWORK=off go test ./cmd/wfctl/ -run "TestInfraAlign_RA4_TopLevel" -v`

Expected: FAIL — both tests report "unexpected R-A4 finding for top-level secrets..."

**Step 4: Implement the fix**

In `cmd/wfctl/infra_align_rules.go`, modify the existing top-level-secrets block (around line 47-49):

```go
// existing:
if cfg.Secrets != nil {
    ctx.secretGens = cfg.Secrets.Generate
    // ADD these lines:
    for _, gen := range cfg.Secrets.Generate {
        ctx.secretKeys[gen.Key] = struct{}{}
    }
    for _, entry := range cfg.Secrets.Entries {
        ctx.secretKeys[entry.Name] = struct{}{}
    }
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /Users/jon/workspace/workflow/_worktrees/feat-iac-deferred-cleanup-w541 && GOWORK=off go test ./cmd/wfctl/ -run "TestInfraAlign_RA4" -v`

Expected: PASS — both new tests + the existing module-form test all pass.

**Step 6: Run full align test suite to verify no regressions**

Run: `GOWORK=off go test ./cmd/wfctl/ -run "TestInfraAlign" -v -count=1 -race`

Expected: all tests pass.

**Step 7: Commit**

```bash
git add cmd/wfctl/infra_align_rules.go cmd/wfctl/infra_align_test.go
git commit -m "fix(wfctl): R-A4 align rule consults top-level secrets.generate + entries (workflow#541)"
```

### Task 1.2: PR + Copilot cycle + admin-merge

**Files:** none (orchestration task)

**Step 1: Push branch + open PR**

```bash
git push -u origin feat/r-a4-toplevel-secrets
gh pr create --base main --head feat/r-a4-toplevel-secrets \
  --title "fix(wfctl): R-A4 align rule consults top-level secrets.generate + entries (workflow#541)" \
  --body "[paste PR body — see below]"
gh api -X POST /repos/GoCodeAlone/workflow/pulls/<N>/requested_reviewers \
  -f 'reviewers[]=copilot-pull-request-reviewer[bot]'
```

PR body should reference workflow#541 + cite the design at `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` + note that this unblocks core-dump PR #190.

**Step 2: Copilot review cycle**

Wait ~10 min for Copilot settle. For each finding: verify against actual file content (workspace memory `feedback_copilot_ghost_flags_verify_file_content`); fix real findings via additional commits; reply to false positives with empirical evidence. Resolve threads via GraphQL `resolveReviewThread`.

**Step 3: Admin-merge once Copilot resolved + non-Lint CI green**

```bash
gh pr merge <N> --squash --admin --delete-branch --repo GoCodeAlone/workflow
```

Expected: PR merged. Capture squash SHA for Phase 3 task 7.0a.

**Step 4: Verify**

Run: `git -C /Users/jon/workspace/workflow fetch origin main && git log --oneline origin/main | head -3`

Expected: the squash-merge commit appears on main.

**Rollback:** revert commit on workflow main; fallback is the env-var stopgap (re-add `STAGING_PG_PASSWORD` and `STAGING_VPC_UUID` env exports to deploy.yml's align step in core-dump).

---

## PR 2: W-Precision — convert.go silent-drop + iac-codemod accumulator pattern

**Repo:** `GoCodeAlone/workflow`
**Branch:** `feat/iac-precision-fixes`
**Issues:** workflow#537 + workflow#539

### Task 2.1: workflow#537 — `mapToStruct` propagates structpb.NewStruct error

**Files:**
- Modify: `plugin/external/convert.go:20-30` (the `mapToStruct` function)
- Test: `plugin/external/convert_test.go` (add new test or new file if not present)

**Step 1: Verify current behavior**

Read `plugin/external/convert.go:20-30` on origin/main. Confirm the silent-drop pattern:

```go
func mapToStruct(m map[string]any) *structpb.Struct {
    if m == nil { return nil }
    s, err := structpb.NewStruct(m)
    if err != nil {
        return &structpb.Struct{}  // BUG: silent drop
    }
    return s
}
```

Identify all callers: `git -C /Users/jon/workspace/workflow grep -n "mapToStruct" origin/main -- '*.go'`. Confirm they're already in error-return paths (gRPC handlers).

**Step 2: Write the failing test**

Add to `plugin/external/convert_test.go`:

```go
func TestMapToStruct_PropagatesErrorOnUnrepresentableType(t *testing.T) {
    // chan is not representable in structpb (per google.golang.org/protobuf/types/known/structpb).
    m := map[string]any{
        "ok_key":  "value",
        "bad_key": make(chan int),
    }
    s, err := mapToStructWithError(m)  // new signature; see Step 4
    if err == nil {
        t.Fatal("expected error from structpb.NewStruct on chan, got nil")
    }
    if s != nil {
        t.Errorf("expected nil struct on error, got %v", s)
    }
}
```

**Step 3: Run test to verify it fails**

Run: `GOWORK=off go test ./plugin/external/ -run "TestMapToStruct_PropagatesError" -v`

Expected: FAIL with `undefined: mapToStructWithError`.

**Step 4: Implement the fix**

Refactor `mapToStruct` to propagate the error. Two approaches:
- Option A: change signature to `(m) (*structpb.Struct, error)`; update all callers. Cleanest but larger blast radius.
- Option B: keep `mapToStruct` as-is for backwards compat AND add `mapToStructWithError` that propagates; deprecate the silent-drop variant.

**Decision: Option A** (per workspace mandate "build the right way; refactor where needed"). Update callers in same commit.

```go
// plugin/external/convert.go
func mapToStruct(m map[string]any) (*structpb.Struct, error) {
    if m == nil { return nil, nil }
    return structpb.NewStruct(m)
}
```

For each caller, propagate the error to the gRPC response. Specific locations TBD by `grep` in Step 1.

**Step 5: Run tests to verify they pass + no regressions**

Run: `GOWORK=off go test ./plugin/external/... ./plugin/sdk/... ./plugin/sdkgrpc/... -count=1 -race`

Expected: all tests pass (including the new one and any updated callers).

**Step 6: Commit**

```bash
git add plugin/external/convert.go plugin/external/convert_test.go [+ caller files]
git commit -m "fix(plugin): mapToStruct propagates structpb.NewStruct error (workflow#537)"
```

### Task 2.2: workflow#539 — iac-codemod analyzer recognizes accumulator pattern

**Files:**
- Modify: `cmd/iac-codemod/lint.go` (the `AssertDiffSetsNeedsReplaceForForceNew` analyzer)
- Test: `cmd/iac-codemod/lint_test.go` (add golden-file fixture for accumulator pattern)

**Step 1: Verify current analyzer code + the false-positive case from VolumeDriver**

Run: `grep -n "AssertDiffSetsNeedsReplace\|needsReplace" cmd/iac-codemod/lint.go | head -20`

Read the analyzer's AST visitor. Confirm it currently checks only direct `result.NeedsReplace = true` patterns, not the accumulator form `acc := false; ... acc = true; ... result.NeedsReplace = acc`.

Reference the false-positive on workflow-plugin-digitalocean's VolumeDriver: per workflow#539, the pattern is at line 232 of that file.

**Step 2: Write the failing test (golden-file pattern)**

Add to `cmd/iac-codemod/testdata/lint/accumulator-pattern.go` (new file):

```go
package fakedriver

import "github.com/GoCodeAlone/workflow/interfaces"

type VolumeDriver struct{}

func (d *VolumeDriver) Diff(_ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
    needsReplace := false
    // ... ForceNew field check elided ...
    if /* ForceNew field changed */ true {
        needsReplace = true
    }
    return &interfaces.DiffResult{NeedsReplace: needsReplace}, nil
}
```

Add golden-file expectation: this fixture should produce ZERO findings from `AssertDiffSetsNeedsReplaceForForceNew`.

In `lint_test.go`:

```go
func TestAssertDiffSetsNeedsReplaceForForceNew_AccumulatorPatternIsClean(t *testing.T) {
    findings := runAnalyzer(t, "AssertDiffSetsNeedsReplaceForForceNew",
        "testdata/lint/accumulator-pattern.go")
    if len(findings) != 0 {
        t.Errorf("expected 0 findings on accumulator pattern, got %d: %v",
            len(findings), findings)
    }
}
```

**Step 3: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/iac-codemod/ -run "AccumulatorPatternIsClean" -v`

Expected: FAIL with "expected 0 findings on accumulator pattern, got 1".

**Step 4: Implement the analyzer fix**

In `lint.go`'s `AssertDiffSetsNeedsReplaceForForceNew`, extend the AST visitor:
1. When inspecting a `Diff` function body, scan for `var <name> bool` or `<name> := false` declarations.
2. Track which local `bool` variables are later assigned `true` inside conditional blocks.
3. When checking `result.NeedsReplace = X`, accept either a literal `true` OR a reference to a tracked accumulator variable.

```go
// Pseudocode for the new logic; implementer should use go/analysis pass APIs:
func checkDiffBody(pass *analysis.Pass, fn *ast.FuncDecl) {
    boolAccumulators := collectBoolLocalVars(fn.Body)
    for _, stmt := range fn.Body.List {
        if assign, ok := stmt.(*ast.AssignStmt); ok {
            if isResultNeedsReplaceLHS(assign.Lhs) {
                rhs := assign.Rhs[0]
                if isLiteralTrue(rhs) || isTrackedAccumulator(rhs, boolAccumulators) {
                    return // canonical pattern detected; no finding
                }
            }
        }
    }
    pass.Reportf(fn.Pos(), "Diff does not set NeedsReplace for ForceNew fields")
}
```

**Step 5: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/iac-codemod/ -count=1 -race`

Expected: all tests pass; the new accumulator-pattern test reports 0 findings.

**Step 6: Re-run codemod against DO plugin to verify the fix in practice**

Run: `GOWORK=off go build -o /tmp/iac-codemod ./cmd/iac-codemod && /tmp/iac-codemod lint -dry-run /Users/jon/workspace/workflow-plugin-digitalocean/`

Expected: NO finding for `internal/drivers/volume.go` `Diff` (it should be clean now).

**Step 7: Commit**

```bash
git add cmd/iac-codemod/lint.go cmd/iac-codemod/lint_test.go cmd/iac-codemod/testdata/lint/accumulator-pattern.go
git commit -m "fix(codemod): AssertDiffSetsNeedsReplaceForForceNew recognizes accumulator pattern (workflow#539)"
```

### Task 2.3: PR + Copilot cycle + admin-merge

(Same orchestration shape as Task 1.2.) PR title: `fix(workflow): convert.go silent-drop + iac-codemod accumulator pattern (workflow#537, #539)`. Body cites both issues + design doc.

**Rollback:** revert each commit independently. Tests still pass at CI; no behavior change for unaffected paths.

---

## PR 3: W-Diagnose-540 — failing-CI test for plugin SDK manifest schema strictness

**Repo:** `GoCodeAlone/workflow`
**Branch:** `test/manifest-schema-strict-diagnostic`
**Issue:** workflow#540

### Task 3.1: Add failing-CI test that captures `additionalProperties:false` lax-validation bug

**Files:**
- Test: `plugin/sdk/manifest_test.go` (modify existing; add new test fn)

**Step 1: Verify current behavior**

Run: `git -C /Users/jon/workspace/workflow show origin/main:plugin/sdk/manifest_schema.json | head -20`

Confirm: `"additionalProperties": false` IS declared on the `iacProvider` sub-object (per design rev3 §I-3).

Then verify the lax-validation behavior empirically: write a quick Go script (or use `wfctl plugin validate`) to load a manifest with extra `iacProvider.bogusKey` and confirm validation accepts it (it should, per the bug).

**Step 2: Write the test using `t.Skip` alt-shape**

Per adversarial-plan-review-1 finding I-5: ship the alt-shape (`t.Skip` with runbook entry) instead of the failing-CI shape. This avoids:
- Red CI on workflow main blocking other PRs.
- Trade-off significant enough to require ADR per `superpowers:recording-decisions`.
- Future contributors reverting the test to "fix CI" without understanding intent.

Add to `plugin/sdk/manifest_test.go`:

```go
func TestManifest_iacProviderAdditionalPropertiesFalse_IsEnforced(t *testing.T) {
    // Diagnostic test for workflow#540.
    // Schema declares additionalProperties:false on iacProvider sub-object,
    // but the jsonschema library currently accepts extra keys silently.
    //
    // SHAPE: t.Skip + workflow#540 link. Once the fix lands (separate PR),
    // remove the t.Skip line + the test will assert the validator REJECTS
    // the bogus key. Until then, this test is a documented BUG marker that
    // future contributors can grep for.
    t.Skip("BUG: workflow#540 — extra iacProvider key not rejected; " +
        "schema declares additionalProperties:false but library accepts extra keys. " +
        "Diagnostic test pending fix follow-up PR.")

    manifest := []byte(`{
        "name": "test-plugin",
        "version": "0.0.0",
        "iacProvider": {
            "computePlanVersion": "v2",
            "bogusKeyThatShouldBeRejected": "value"
        }
    }`)
    err := ValidateManifest(manifest)  // or whichever validator function is used
    if err == nil {
        t.Errorf("BUG: extra iacProvider key not rejected — see workflow#540")
    }
}
```

(If `ValidateManifest` doesn't exist, look for the equivalent function — likely in `plugin/sdk/manifest.go`. Test should call whatever's already used by `wfctl plugin validate`.)

**Step 3: Run the test to verify it SKIPS (not fails)**

Run: `GOWORK=off go test ./plugin/sdk/ -run "iacProviderAdditionalPropertiesFalse" -v`

Expected: `--- SKIP: TestManifest_iacProviderAdditionalPropertiesFalse_IsEnforced (0.00s)` with the SKIP message naming workflow#540.

**Step 4: Add runbook entry naming the active SKIPs**

Add to `docs/test-skips.md` (create if not present):

```markdown
# Active test skips

| Test | Issue | Plan to remove |
|------|-------|----------------|
| TestManifest_iacProviderAdditionalPropertiesFalse_IsEnforced | workflow#540 | Remove `t.Skip` line in fix follow-up PR |
```

**Step 5: Commit (test + runbook entry; no implementation change)**

```bash
git add plugin/sdk/manifest_test.go docs/test-skips.md
git commit -m "test(plugin): diagnostic test (skipped) for manifest schema strictness (workflow#540)"
```

### Task 3.2: PR + Copilot cycle + admin-merge

(Same orchestration shape as Task 1.2.) PR title: `test(plugin): diagnostic test for manifest schema strictness (workflow#540)`. Body notes:

- This PR ships the diagnostic test in `t.Skip` shape (alt-shape per design rev3 §Behavior).
- CI stays green; the SKIP shows up in `go test -v` output as a reminder.
- Fix follow-up PR removes the `t.Skip` line + the test asserts the validator REJECTS the bogus key.
- `docs/test-skips.md` runbook entry tracks the active SKIP.

**Rollback:** revert commit; runbook entry stays as a TODO.

---

## PR 4: W-Cleanup — `wfctl infra cleanup --tag` subcommand + opt-in Enumerator interface

**Repo:** `GoCodeAlone/workflow`
**Branch:** `feat/wfctl-infra-cleanup`
**Issue:** workflow#536

### Task 4.1: Add opt-in `Enumerator` interface

**Pre-DM team-lead** before this task per workspace policy (new public interface in `interfaces/iac_provider.go` is a cross-package contract change; same trigger as Task 4.4 + Task 7.1).

**Files:**
- Modify: `interfaces/iac_provider.go` (add new optional interface)

**Step 1: Verify Enumerator namespace is clean on origin/main**

Run: `git -C /Users/jon/workspace/workflow grep -n "Enumerator\|EnumerateByTag" origin/main -- '*.go'`

Expected: zero hits.

**Step 2: Write the failing test**

Add to `interfaces/iac_provider_test.go` (or new `interfaces/iac_enumerator_test.go`):

```go
package interfaces_test

import (
    "context"
    "testing"

    "github.com/GoCodeAlone/workflow/interfaces"
)

type fakeEnumerator struct{}

func (f *fakeEnumerator) EnumerateByTag(ctx context.Context, tag string) ([]interfaces.ResourceRef, error) {
    return []interfaces.ResourceRef{{Name: "test", Type: "infra.compute"}}, nil
}

func TestEnumeratorInterfaceCompiles(t *testing.T) {
    var _ interfaces.Enumerator = (*fakeEnumerator)(nil)
}
```

**Step 3: Run test to verify it FAILS**

Run: `GOWORK=off go test ./interfaces/ -run "EnumeratorInterfaceCompiles" -v`

Expected: FAIL with `undefined: interfaces.Enumerator`.

**Step 4: Implement the interface**

Add to `interfaces/iac_provider.go` (after the existing `ProviderPlanner` interface block, ~line 62):

```go
// Enumerator is an optional interface for providers that can list
// resources by tag across the cloud account. Used by `wfctl infra
// cleanup --tag <name>`. Providers without a tag-query API simply do
// not implement it; the cleanup subcommand skips them with a structured
// log line so operators see the explicit skip in stdout.
//
// Plugins implementing this interface are accepted by the loader; the
// implementation is not yet exercised by core code outside cleanup.
type Enumerator interface {
    EnumerateByTag(ctx context.Context, tag string) ([]ResourceRef, error)
}
```

**Step 5: Run test to verify it PASSES**

Run: `GOWORK=off go test ./interfaces/ -count=1 -race`

Expected: all interface tests pass; the new `Enumerator` compiles.

**Step 6: Commit**

```bash
git add interfaces/iac_provider.go interfaces/iac_enumerator_test.go
git commit -m "feat(interfaces): add opt-in Enumerator interface for tag-based resource listing (workflow#536)"
```

### Task 4.2: Implement `wfctl infra cleanup --tag` subcommand

**Files:**
- Create: `cmd/wfctl/infra_cleanup.go`
- Test: `cmd/wfctl/infra_cleanup_test.go`
- Modify: `cmd/wfctl/main.go` (subcommand registration)

**Step 1: Write the failing test**

Add to `cmd/wfctl/infra_cleanup_test.go`:

```go
func TestInfraCleanup_DryRun_ListsResourcesWithoutDeleting(t *testing.T) {
    // Set up fake provider that implements Enumerator.
    fp := &fakeEnumeratingProvider{
        resources: []interfaces.ResourceRef{
            {Name: "vpc-1", Type: "infra.vpc"},
            {Name: "db-1", Type: "infra.database"},
        },
    }
    // Run cleanup with --dry-run.
    out, err := runInfraCleanup(t, fp, "--tag", "test-tag", "--dry-run")
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    // Verify stdout lists the 2 resources.
    if !strings.Contains(out, "vpc-1") { t.Errorf("missing vpc-1 in: %s", out) }
    if !strings.Contains(out, "db-1") { t.Errorf("missing db-1 in: %s", out) }
    // Verify driver Delete was NOT called.
    if fp.deleteCallCount != 0 {
        t.Errorf("dry-run should not invoke Delete; got %d calls", fp.deleteCallCount)
    }
}

func TestInfraCleanup_LiveMode_DeletesResources(t *testing.T) {
    // Same fixture; run WITHOUT --dry-run.
    fp := &fakeEnumeratingProvider{...}
    _, err := runInfraCleanup(t, fp, "--tag", "test-tag")
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if fp.deleteCallCount != 2 {
        t.Errorf("expected 2 Delete calls, got %d", fp.deleteCallCount)
    }
}

func TestInfraCleanup_NonEnumeratorProvider_SkipsWithLog(t *testing.T) {
    // Provider does NOT implement Enumerator.
    fp := &fakeNonEnumeratingProvider{}
    out, err := runInfraCleanup(t, fp, "--tag", "test-tag")
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if !strings.Contains(out, "skipped") || !strings.Contains(out, "no Enumerator") {
        t.Errorf("expected skip log; got: %s", out)
    }
}

func TestInfraCleanup_PartialFailure_NonZeroExit(t *testing.T) {
    // 2 resources; Delete on resource 2 fails.
    fp := &fakeEnumeratingProvider{
        resources: []interfaces.ResourceRef{...},
        deleteErrors: map[int]error{1: errors.New("simulated failure")},
    }
    _, err := runInfraCleanup(t, fp, "--tag", "test-tag")
    if err == nil { t.Error("expected non-nil error on partial failure") }
}
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl/ -run "TestInfraCleanup" -v`

Expected: FAIL with `undefined: runInfraCleanup`.

**Step 3: Implement the subcommand using the canonical `runInfra*` dispatcher signature**

Verify the canonical pattern against origin/main first:

```
git -C /Users/jon/workspace/workflow show origin/main:cmd/wfctl/infra.go | grep -n "^func runInfra" | head -10
```

Expected: all sibling sub-functions (`runInfra`, `runInfraPlan`, `runInfraApply`, etc.) take `args []string` and return `error`. They get providers/stdout via package-level state set up in `runInfra` itself.

Create `cmd/wfctl/infra_cleanup.go` matching the precedent:

```go
package main

import (
    "context"
    "errors"
    "flag"
    "fmt"

    "github.com/GoCodeAlone/workflow/interfaces"
)

func runInfraCleanup(args []string) error {
    // Use the same package-level helpers other runInfra* functions use:
    // - load config + providers via the same path runInfraPlan / runInfraApply use
    // - emit to os.Stdout / os.Stderr per the canonical pattern
    fs := flag.NewFlagSet("infra cleanup", flag.ContinueOnError)
    fs.SetOutput(os.Stderr)
    fs.Usage = func() { usage(os.Stderr) }

    tag := fs.String("tag", "", "tag to match resources for cleanup (required)")
    dryRun := fs.Bool("dry-run", true, "preview only; do not delete resources")
    fix := fs.Bool("fix", false, "actually delete resources (overrides -dry-run)")

    if err := fs.Parse(args); err != nil { return err }
    if *tag == "" { return errors.New("infra cleanup: --tag is required") }
    if !*fix { *dryRun = true }

    // Load providers via the same helper runInfraPlan/runInfraApply use.
    // (Verify the actual helper name against origin/main; likely loadIaCProviders
    // or similar — see cmd/wfctl/infra.go for the canonical pattern.)
    ctx := context.Background()
    providers, err := loadIaCProvidersForCleanup() // canonical helper; replace name per actual
    if err != nil { return err }

    var totalErrs []error
    for _, p := range providers {
        enum, ok := p.(interfaces.Enumerator)
        if !ok {
            fmt.Fprintf(os.Stdout, "skipped %s: no Enumerator interface\n", p.Name())
            continue
        }
        refs, err := enum.EnumerateByTag(ctx, *tag)
        if err != nil {
            fmt.Fprintf(os.Stderr, "%s: enumeration failed: %v\n", p.Name(), err)
            totalErrs = append(totalErrs, err)
            continue
        }
        for _, ref := range refs {
            if *dryRun {
                fmt.Fprintf(os.Stdout, "[dry-run] would delete %s/%s (provider: %s)\n", ref.Type, ref.Name, p.Name())
                continue
            }
            // Live mode: get driver + delete
            drv, err := p.ResourceDriver(ref.Type)
            if err != nil {
                totalErrs = append(totalErrs, fmt.Errorf("%s: resolve driver: %w", ref.Name, err))
                continue
            }
            if err := drv.Delete(ctx, ref); err != nil {
                totalErrs = append(totalErrs, fmt.Errorf("%s: delete: %w", ref.Name, err))
                continue
            }
            fmt.Fprintf(os.Stdout, "deleted %s/%s (provider: %s)\n", ref.Type, ref.Name, p.Name())
        }
    }
    if len(totalErrs) > 0 {
        return errors.Join(totalErrs...)
    }
    return nil
}
```

(The implementer must verify the actual provider-loading helper name in `cmd/wfctl/infra.go` on origin/main. The canonical pattern matches the existing `runInfraPlan` / `runInfraApply` precedent — package-level state, args-only signature, no DI.)

**Step 4: Wire subcommand into `cmd/wfctl/main.go`**

In the `infra` subcommand dispatcher, add a `case "cleanup":` arm calling `runInfraCleanup`.

**Step 5: Run tests to verify pass**

Run: `GOWORK=off go test ./cmd/wfctl/ -run "TestInfraCleanup" -v -count=1 -race`

Expected: all 4 tests pass.

**Step 6: Smoke-test the binary**

Run:
```
GOWORK=off go build -o /tmp/wfctl ./cmd/wfctl
/tmp/wfctl infra cleanup --help
```

Expected: help text shows `--tag` (required), `--dry-run` (default true), `--fix` (opt into mutation).

**Step 7: Commit**

```bash
git add cmd/wfctl/infra_cleanup.go cmd/wfctl/infra_cleanup_test.go cmd/wfctl/main.go
git commit -m "feat(wfctl): infra cleanup --tag subcommand using opt-in Enumerator interface (workflow#536)"
```

### Task 4.3: Update `docs/WFCTL.md` + `docs/conformance-runbook.md`

**Files:**
- Modify: `docs/WFCTL.md` (add `infra cleanup --tag` to command reference)
- Modify: `docs/conformance-runbook.md` (update "Known follow-ups" — close the wfctl-cleanup item)

**Step 1: Add command-reference entry**

In `docs/WFCTL.md`, add after the existing `infra` subcommand entries:

```markdown
### `wfctl infra cleanup --tag <name>`

List + delete resources matching `<name>` tag across all loaded providers.

**Flags:**
- `--tag <name>` (required): tag to match.
- `--dry-run` (default `true`): preview only.
- `--fix`: opt into deletion (overrides `--dry-run`).

**Behavior:** Calls `EnumerateByTag` on each provider that implements the optional `interfaces.Enumerator`. For matched resources in `--fix` mode, calls each provider's `ResourceDriver(type).Delete(ref)`. Returns non-zero on partial failure with structured stderr.

**Limitations:** Providers without `Enumerator` are skipped with `skipped <provider>: no Enumerator interface` to stdout. AWS/GCP/Azure plugins do not yet implement `Enumerator`; only DO does as of workflow v0.21.2 + workflow-plugin-digitalocean v0.10.1.
```

**Step 2: Update runbook**

In `docs/conformance-runbook.md` "Known follow-ups" section, mark the wfctl-cleanup item as CLOSED with link to this PR.

**Step 3: Commit**

```bash
git add docs/WFCTL.md docs/conformance-runbook.md
git commit -m "docs(wfctl): add infra cleanup --tag command reference + close runbook follow-up"
```

### Task 4.4: Update `.github/workflows/conformance-smoke.yml` to use new subcommand

**Files:**
- Modify: `.github/workflows/conformance-smoke.yml:106-118` (the cleanup-stub stub)

**Pre-DM team-lead** before this task per workspace policy (`.github/workflows/` touch).

**Step 1: Replace stub with new subcommand**

In `conformance-smoke.yml`, replace:

```yaml
# Old stub:
- name: Cleanup smoke resources
  if: always()
  run: |
    echo "::warning::wfctl infra cleanup --tag is not yet implemented; relying on T7.14 leak scrubber."
```

With:

```yaml
- name: Cleanup smoke resources
  if: always()
  env:
    DO_TOKEN: ${{ secrets.DO_CONFORMANCE_API_TOKEN }}
  run: |
    if [ -z "$DO_TOKEN" ]; then
      echo "::notice::DO_CONFORMANCE_API_TOKEN not provisioned (workflow#542 deferred); cleanup gate is wired but inactive. Hourly leak-scrubber catches orphans in the meantime."
      exit 0
    fi
    wfctl infra cleanup --tag "conformance-pr-${{ github.event.pull_request.number }}" --fix
```

**Important note for PR 4 body**: explicitly call out: "Smoke-gate cleanup wiring activates only after workflow#542 (DO_CONFORMANCE_API_TOKEN provisioning) lands. Until then, the smoke gate emits a `::notice::` indicating the wiring is in place but inactive. Unit tests in Task 4.2 ARE the integration verification; Task 4.4 is wiring-only."

**Step 2: Validate YAML**

Run: `python3 -c 'import yaml; yaml.safe_load(open(".github/workflows/conformance-smoke.yml"))'`

Expected: clean parse.

**Step 3: Commit**

```bash
git add .github/workflows/conformance-smoke.yml
git commit -m "ci(conformance): use wfctl infra cleanup --tag in smoke gate (workflow#536)"
```

### Task 4.5: PR + Copilot cycle + admin-merge

(Same orchestration shape as Task 1.2.) PR title: `feat(wfctl): infra cleanup --tag subcommand + opt-in Enumerator interface (workflow#536)`. Body: cite #536, design rev3, note opt-in semantics + visible-skip behavior + future plugin-impl roadmap.

**Rollback:** revert all commits on this branch (subcommand registration in main.go, infra_cleanup.go, conformance-smoke.yml edit, WFCTL.md, runbook update, Enumerator interface). The leak-scrubber hourly job continues to clean orphans. **No plugin-stub revert required** because Enumerator is opt-in. DO plugin's Enumerator impl is a separate PR (PR 6b Task 6b.1); if PR 4 is reverted, DO plugin's impl simply doesn't get used (no compile break).

---

## PR 5: W-Refactor — deploy_providers.remoteIaCProvider canonicalization + ADR 010

**Repo:** `GoCodeAlone/workflow`
**Branch:** `refactor/remote-iac-provider`

### Task 5.1: Refactor `cmd/wfctl/deploy_providers.go remoteIaCProvider` to use canonical wfctlhelpers dispatch

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go` (the `remoteIaCProvider` type + its Plan/Apply methods)

**Step 1: Verify current non-canonical pattern**

Run: `grep -n "remoteIaCProvider\|func.*Plan\|func.*Apply" cmd/wfctl/deploy_providers.go | head -20`

Read the current implementations of Plan + Apply on `remoteIaCProvider`. Confirm they don't currently delegate to `wfctlhelpers.Plan` or `wfctlhelpers.ApplyPlan` (per W-8 lint smoke surfaced).

**Step 2: Verify canonical helper signatures on origin/main**

Run:
```
git -C /Users/jon/workspace/workflow show origin/main:iac/wfctlhelpers/plan.go | head -20
git -C /Users/jon/workspace/workflow show origin/main:iac/wfctlhelpers/apply.go | head -20
```

Confirm the exact function signatures `wfctlhelpers.Plan(ctx, p, desired, current)` and `wfctlhelpers.ApplyPlan(ctx, p, plan)` (or whatever the actual signatures are; rev3 plan-literal-vs-reality discipline applies).

**Step 3: Write the regression test**

Existing tests in `cmd/wfctl/deploy_providers_*_test.go` should all pass after refactor. Add one new test pinning canonical delegation:

```go
func TestRemoteIaCProvider_PlanDelegatesToHelper(t *testing.T) {
    // Set up fake remote driver; call Plan; verify it returned via wfctlhelpers.Plan path.
    // (Implementation depends on how to mock this; likely a behavioral assertion via
    // the existing fake-driver harness in deploy_providers_remote_driver_test.go.)
}
```

**Step 4: Implement the refactor**

Replace the body of `remoteIaCProvider.Plan` with:

```go
func (p *remoteIaCProvider) Plan(ctx context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
    return wfctlhelpers.Plan(ctx, p, desired, current)  // verify exact signature
}
```

And similarly for Apply:

```go
func (p *remoteIaCProvider) Apply(ctx context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
    return wfctlhelpers.ApplyPlan(ctx, p, plan)  // verify exact signature
}
```

**Step 5: Run tests + verify codemod lint passes**

Run:
```
GOWORK=off go test ./cmd/wfctl/... -count=1 -race
GOWORK=off go build -o /tmp/iac-codemod ./cmd/iac-codemod
/tmp/iac-codemod lint -dry-run /Users/jon/workspace/workflow/_worktrees/feat-iac-deferred-cleanup-w5refactor/cmd/wfctl/
```

Expected: all tests pass; lint reports zero findings on `deploy_providers.go`.

**Step 6: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_canonical_test.go
git commit -m "refactor(wfctl): remoteIaCProvider delegates Plan/Apply to wfctlhelpers"
```

### Task 5.2: Add ADR 010 — Platform-vs-provider conformance scenarios

**Files:**
- Create: `docs/adr/010-platform-vs-provider-conformance-scenarios.md`

**Step 1: Verify ADR 010 namespace is unused**

Run: `ls docs/adr/ | grep "010"`

Expected: zero matches (per session memory: latest ADR is 009 ProviderPlanner inclusion).

**Step 2: Write the ADR**

Create `docs/adr/010-platform-vs-provider-conformance-scenarios.md`:

```markdown
# ADR 010: Platform-vs-Provider Conformance Scenario Classification

**Status:** Accepted
**Date:** 2026-05-05
**Context:** W-7 (workflow PR #535) shipped 12 conformance scenarios in `iac/conformance/`. During implementation, 4 scenarios diverged from the typical pattern of asserting against `cfg.Provider()`; instead they exercise platform-shared surfaces directly.

## Decision

We codify TWO classes of conformance scenario:

**Provider-level scenarios** (8 of 12): assert against `cfg.Provider()`. Exercise per-provider behavior (Diff, Apply, ResourceDriver lookup, etc.). Scenarios:
- Scenario_NeedsReplaceTriggersReplaceAction
- Scenario_DeleteActionInApplyInvokesDriverDelete
- Scenario_DiffSurvivesGRPCRoundTrip
- Scenario_OutputsRefreshDetectsNewFields
- Scenario_CrossResourceConstraintRejection
- Scenario_OutputsConsistencyAcrossReadCycles
- Scenario_ReplaceCascadePreservesDependents
- Scenario_UpsertOnAlreadyExists

**Platform-level scenarios** (4 of 12): bypass `cfg.Provider()`; exercise platform-shared surfaces (`inputsnapshot`, `jitsubst`, `wfctlhelpers`). cfg.Provider is required by Run's validateConfig precondition but intentionally NOT invoked. Scenarios:
- Scenario_PlanStaleDiagnostic — exercises `inputsnapshot.NewStaleError`
- Scenario_InfraOutputCrossModuleResolution — exercises `jitsubst.ResolveSpec`
- Scenario_ProtectedReplaceWithoutOverride — exercises `wfctlhelpers.ValidateAllowReplaceProtected`
- Scenario_ProtectedReplaceWithOverride — exercises `wfctlhelpers.ValidateAllowReplaceProtected`

Each platform-level scenario carries a body comment naming the platform surface it exercises and explaining why cfg.Provider is unused.

## Rationale

Some root-cause issues from the IaC conformance plan (e.g. plan-stale-diagnostic, JIT secret resolution, --allow-replace gate) live at the platform layer (cross-provider-shared code), not at the per-provider layer. Conformance scenarios for those issues SHOULD test the platform surface directly — wrapping in `cfg.Provider()` calls would be vestigial.

The 4-of-12 ratio is acceptable; the boundary is non-arbitrary (each platform-level scenario tests code that lives in `iac/` or `iac/wfctlhelpers/`, not in any specific provider).

## Consequences

- Future contributors adding conformance scenarios must classify their scenario at design time and document the choice in the body comment.
- The Run dispatcher does not need code changes for this classification; it already validates `cfg.Provider != nil` regardless. Platform-level scenarios pass a NoopProvider to satisfy validateConfig.
- If future iteration wants typed enforcement, a `Scenario.Platform bool` field could be added and consulted by Run for richer reporting. Out of scope for this ADR.

## Alternatives Considered

- **Bypass validateConfig for platform-level scenarios.** Rejected: the precondition is universal; making it conditional adds complexity without benefit.
- **Move platform-level scenarios out of conformance/ into a separate package.** Rejected: scenarios are conceptually about provider-conformance to the IaC contract; platform-shared surfaces are part of that contract.

## References

- Workflow PR #535 (W-7) — original implementation
- Workflow PR #538 (W-8) — codemod that surfaced the pattern in lint reports
- IaC conformance plan: `docs/plans/2026-05-03-iac-conformance-and-replace.md`
- Deferred cleanup design: `docs/plans/2026-05-05-iac-deferred-cleanup-design.md`
```

**Step 3: Commit**

```bash
git add docs/adr/010-platform-vs-provider-conformance-scenarios.md
git commit -m "docs(adr): add ADR 010 — platform-vs-provider conformance scenario classification"
```

### Task 5.3: PR + Copilot cycle + admin-merge

(Same orchestration.) PR title: `refactor(wfctl): remoteIaCProvider canonicalization + ADR 010`. Body: cite W-8 lint surface + design rev3 + the per-attack `feedback_proper_fixes_over_workarounds` rationale.

**Rollback:** revert each commit independently. The refactor is interface-equivalent (existing tests still pass). ADR 010 is doc-only.

---

## PR 6: DO-Plugin (#62 + #63 only) — Initialize ctx + Plan canonicalization

**Repo:** `GoCodeAlone/workflow-plugin-digitalocean`
**Branch:** `fix/initialize-ctx-and-plan-canonical`
**Issues:** workflow-plugin-digitalocean#62 + workflow-plugin-digitalocean#63
**Sequencing:** Phase 2 Set A; independent of PR 4 (Enumerator impl moved to PR 6b)
**Tag-cut after merge:** none (PR 6b's Enumerator impl rolls into v0.10.1)

### Task 6.1: workflow-plugin-digitalocean#62 — Thread ctx through DOProvider.Initialize to godo client

**Files:**
- Modify: `internal/provider.go` (the `DOProvider.Initialize` method)

**Step 1: Verify current behavior**

Run: `grep -n "Initialize\|context.Background\|godo.NewClient" /Users/jon/workspace/workflow-plugin-digitalocean/internal/provider.go`

Confirm: `Initialize` accepts `ctx` but constructs godo client with `context.Background()`.

**Step 2: Write the failing test**

In `internal/provider_test.go`:

```go
func TestDOProvider_Initialize_ThreadsCtxToGodoClient(t *testing.T) {
    // Use a context with a known value or deadline; verify godo client receives it.
    type ctxKey string
    ctx := context.WithValue(context.Background(), ctxKey("marker"), "test-value")
    p := New()
    err := p.Initialize(ctx, map[string]any{"token": "fake-token"})
    if err != nil { t.Fatalf("init: %v", err) }
    // Verify ctx is observable by mocking godo's HTTP transport (or by checking
    // that subsequent ctx-cancel propagates — which is the actual behavior we care about).
    cancelCtx, cancel := context.WithCancel(ctx)
    cancel()
    // After cancel, any operation taking the godo client should observe ctx.Err().
    // Concrete assertion: implementation-dependent; document the test's verification approach.
}
```

(If a direct ctx-threading test is too brittle, alternative: test cancellation propagation — Initialize then call any method (e.g. EnumerateByTag) with a cancelled context and verify it returns ctx.Err().)

**Step 3: Implement the fix**

In `internal/provider.go`, change the godo client construction:

```go
// Old:
client := godo.NewFromToken(token)

// New (thread ctx via httpClient with ctx-aware transport):
hc := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
client := godo.NewClient(hc)
```

(Verify `oauth2` import is available; if not, fall back to a custom http.Client with a Transport that wraps requests in ctx.)

**Step 4: Run tests**

Run: `cd /Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/feat-do-plugin-cleanup && GOWORK=off go test ./internal/... -count=1 -race`

Expected: all tests pass.

**Step 5: Commit**

```bash
git add internal/provider.go internal/provider_test.go
git commit -m "fix(provider): thread ctx to godo client in Initialize (workflow-plugin-digitalocean#62)"
```

### Task 6.2: workflow-plugin-digitalocean#63 — Refactor DOProvider.Plan to platform.ComputePlan

**Files:**
- Modify: `internal/provider.go` (the `DOProvider.Plan` method)

**Step 1: Verify platform.ComputePlan signature on workflow origin/main**

Run: `git -C /Users/jon/workspace/workflow show origin/main:platform/differ.go | grep -A 5 "^func ComputePlan"`

Confirm the exact signature. Per design rev3 §Plan-literal-vs-reality #4: must be done at start of this task.

**Step 2: Write the regression test**

Existing tests should still pass after refactor (interface-equivalent). Add one new test pinning canonical delegation:

```go
func TestDOProvider_Plan_DelegatesToCanonicalHelper(t *testing.T) {
    // Plan with empty current state; verify the result shape matches what
    // platform.ComputePlan would produce.
    // (Likely a behavioral assertion using existing fake driver harness.)
}
```

**Step 3: Implement the refactor**

Replace `DOProvider.Plan` body with:

```go
func (p *DOProvider) Plan(ctx context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
    return platform.ComputePlan(ctx, p, desired, current)
}
```

(Adjust signature exactly per Step 1 verification.)

**Step 4: Run tests + verify codemod lint clean**

Run:
```
GOWORK=off go test ./internal/... -count=1 -race
# Then run the W-8 codemod against the worktree:
GOWORK=off go build -o /tmp/iac-codemod /Users/jon/workspace/workflow/cmd/iac-codemod
/tmp/iac-codemod lint -dry-run /Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/feat-do-plugin-cleanup/
```

Expected: tests pass; lint reports zero findings on `internal/provider.go::Plan`.

**Step 5: Commit**

```bash
git add internal/provider.go internal/provider_canonical_plan_test.go
git commit -m "refactor(provider): collapse Plan to platform.ComputePlan (workflow-plugin-digitalocean#63)"
```

### Task 6.3: PR 6 PR + Copilot cycle + admin-merge

(Same orchestration shape as Task 1.2.) PR title: `fix/refactor(provider): Initialize ctx + Plan canonicalization (#62, #63)`. Body cites both issues + design rev3.

**Rollback:** revert each commit independently. v0.10.0 behavior continues.

---

## PR 6b: DO-Plugin Enumerator impl (FOLLOW-UP after PR 4)

**Repo:** `GoCodeAlone/workflow-plugin-digitalocean`
**Branch:** `feat/do-enumerator-impl` (NEW from DO main post-PR-6-merge)
**Issue:** workflow#536 (DO-side Enumerator impl)
**Sequencing:** **BLOCKS on PR 4 Task 4.1 reaching workflow main** (need `interfaces.Enumerator` to exist before DO can satisfy it). Runs AFTER PR 6 merges + AFTER PR 4 Task 4.1's Enumerator interface lands on workflow main.
**Tag-cut after merge:** v0.10.1

### Task 6b.1: Implement opt-in Enumerator interface

**Files:**
- Modify: `internal/provider.go` (add `EnumerateByTag` method to DOProvider)
- Modify: `go.mod` (bump workflow dep to a ref that includes the Enumerator interface)
- Test: `internal/provider_enumerator_test.go`

**Step 1: Verify Enumerator interface is on workflow main**

Run: `git -C /Users/jon/workspace/workflow show origin/main:interfaces/iac_provider.go | grep -A 10 "^type Enumerator"`

Verify the interface signature is what PR 4 Task 4.1 landed. If the grep returns empty, PR 4 hasn't merged yet — this task is BLOCKED. STOP and DM team-lead.

**Step 1b: Bump DO-Plugin go.mod to a workflow ref containing Enumerator**

Run: `cd _worktrees/feat-do-enumerator-impl && go mod edit -require=github.com/GoCodeAlone/workflow@<workflow-main-SHA-with-Enumerator> && go mod tidy`

(Alternatively: bump to a workflow tag that includes Enumerator, if Coord-3 has already cut v0.21.2. If not yet, use the post-PR-4 main HEAD SHA.)

**Step 2: Write the failing test**

In `internal/provider_enumerator_test.go`:

```go
func TestDOProvider_ImplementsEnumerator(t *testing.T) {
    var _ interfaces.Enumerator = (*DOProvider)(nil)
}

func TestDOProvider_EnumerateByTag_ReturnsTaggedResources(t *testing.T) {
    // Mock godo.Tags.List to return 2 resources tagged "test-tag".
    // Verify EnumerateByTag returns both as ResourceRef.
    // (Implementation detail: requires mocking godo client.)
}
```

**Step 3: Implement EnumerateByTag**

In `internal/provider.go`:

```go
// EnumerateByTag implements interfaces.Enumerator.
// Lists DO resources matching the given tag via godo.Tags.List API.
func (p *DOProvider) EnumerateByTag(ctx context.Context, tag string) ([]interfaces.ResourceRef, error) {
    tagDetails, _, err := p.client.Tags.Get(ctx, tag)
    if err != nil { return nil, fmt.Errorf("DO: get tag %q: %w", tag, err) }
    var refs []interfaces.ResourceRef
    // tagDetails.Resources contains droplets, volumes, dbs, etc.
    for _, dropletRes := range tagDetails.Resources.Droplets.LastTagged {
        refs = append(refs, interfaces.ResourceRef{
            Name: dropletRes.URN(),
            Type: "infra.compute",
        })
    }
    // Similarly for Volumes, Databases, etc.
    return refs, nil
}
```

(Reference godo.Tags API at `github.com/digitalocean/godo` for the exact data model; the above is sketch — implementer should verify.)

**Step 4: Run tests**

Run: `GOWORK=off go test ./internal/... -count=1 -race`

Expected: all tests pass.

**Step 5: Commit**

```bash
git add internal/provider.go internal/provider_enumerator_test.go
git commit -m "feat(provider): implement Enumerator interface for tag-based resource listing"
```

### Task 6b.2: PR 6b PR + Copilot cycle + admin-merge

(Same orchestration.) PR title: `feat(provider): implement opt-in Enumerator interface (workflow#536 follow-up)`. Body: cite workflow#536 + design rev3 + workflow PR 4 (which lands the interface).

**Rollback:** revert PR 6b commit; v0.10.0 behavior continues. The cleanup subcommand will still work (opt-in interface; type-assertion at use site sees `ok=false` when Enumerator-impl is reverted).

---

## PR 7: C-1 TC1 — Pin bumps to v0.21.2 + v0.10.1 (amend existing PR #190)

**Repo:** `GoCodeAlone/core-dump`
**Branch:** `feat/c1-staging-pg-cutover` (existing, PR #190)
**Pre-DM team-lead** before this task per workspace policy (`.github/workflows/` touch).

### Task 7.0a: Cut workflow v0.21.1 (after PR 1 merges)

**Files:** none (operator-side tag cut)

**Step 1: Verify PR 1 has merged + capture squash SHA**

Run: `gh pr view <PR-1-number> --repo GoCodeAlone/workflow --json mergedAt,mergeCommit --jq '{mergedAt, sha: .mergeCommit.oid}'`

Expected: `mergedAt` non-null; `sha` is the squash-merge SHA.

**Step 2: Cut + push tag**

Run:
```
cd /Users/jon/workspace/workflow
git fetch origin main
git tag -a v0.21.1 <PR-1-squash-SHA> -m "v0.21.1 — R-A4 align rule consults top-level secrets (workflow#541)"
git push origin v0.21.1
```

**Step 3: Verify Release CI starts**

Run: `gh run list --repo GoCodeAlone/workflow --branch v0.21.1 --limit 3`

Expected: a `Release` workflow run is `in_progress`.

**Step 4: Wait for Release CI green + binary live**

Poll: `gh release view v0.21.1 --repo GoCodeAlone/workflow --json assets --jq '.assets | length'` until non-zero.

Expected: assets array contains the wfctl binaries (~2-3 min after push).

**Step 5: If Release CI fails — escalate**

If after ~10 min the assets array is still empty: capture failed run via `gh run view --log-failed`; DM team-lead with the run ID + failed-step name + workflow file path; do NOT proceed to Phase 2. Recovery is typically a config drift in `.goreleaser.yml` or upstream tag-resolution issue (see workspace memory `feedback_go_sum_mismatch_check_compare_first`). The team-lead will dispatch a fix PR or restart the run as appropriate.

### Task 7.0b: Cut DO plugin v0.10.1 (after PR 6b merges)

(Identical shape to 7.0a INCLUDING Step 5 escalation but on `workflow-plugin-digitalocean` repo, tag `v0.10.1`, body: cites #62, #63, and #536 Enumerator impl.)

### Task 7.0c: Cut workflow v0.21.2 (after PRs 2-5 merge)

(Identical shape INCLUDING Step 5 escalation; tag body cites W-Precision, W-Diagnose-540, W-Cleanup, W-Refactor.)

**Tasks 7.0a/b/c clarification (per adversarial-plan-review-1 finding C-2)**: these tasks are orchestrator-side coordination, NOT commits on PR 7's `feat/c1-staging-pg-cutover` branch. They appear in the §Coordination tasks table at the top of the plan. Task 7.1 starts ONLY after BOTH Coord-2 (DO v0.10.1) AND Coord-3 (workflow v0.21.2) complete with binaries live.

### Task 7.1: Bump core-dump pins to v0.21.2 + v0.10.1

**Files:**
- Modify: `wfctl.yaml` (DO plugin version)
- Modify: `.wfctl-lock.yaml` (DO plugin version)
- Modify: `.github/workflows/deploy.yml`, `bootstrap.yml`, `drift-recovery.yml`, `registry-retention.yml`, `teardown.yml`, `image-launch-ci.yml` (setup-wfctl version → v0.21.2)

**Step 1: cd to PR #190 worktree, fetch + ff**

Run:
```
cd /Users/jon/workspace/core-dump/_worktrees/feat-c1-staging-pg-cutover
git pull --ff-only origin feat/c1-staging-pg-cutover
```

**Step 2: Bump pins**

Edit each file:
- `wfctl.yaml`: change DO plugin version to `v0.10.1`
- `.wfctl-lock.yaml`: same; refresh checksums via `wfctl plugin lock --update` (verify the command exists; if not, manual)
- All 6 GH workflows: change `with: { version: v0.21.0 }` to `with: { version: v0.21.2 }`

**Step 3: Local validation**

Run: `wfctl validate infra.yaml --env staging` (with v0.21.2 binary; build from /Users/jon/workspace/workflow main if needed)

Expected: validation succeeds WITHOUT requiring the env-var stopgap on align (proves W-541 fix is live in v0.21.2).

**Step 4: Commit**

```bash
git add wfctl.yaml .wfctl-lock.yaml .github/workflows/*.yml
git commit -m "fix(infra): bump pins to wfctl v0.21.2 + DO plugin v0.10.1 (TC1 follow-up)"
```

**Step 5: Force-push to PR #190 branch**

```bash
git push origin feat/c1-staging-pg-cutover
```

(Note: this is a follow-up commit, NOT a force-push of history. Standard push to existing PR branch.)

### Task 7.2: Re-run CI + admin-merge PR #190

**Files:** none (orchestration)

**Step 1: Wait for CI on the new commit**

Poll: `gh pr view 190 --repo GoCodeAlone/core-dump --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.conclusion==null and .status!="COMPLETED")] | length'` until 0.

**Step 2: Verify Copilot has reviewed the new commit**

Per workspace memory `feedback_check_review_comments_before_merge`: read each new inline comment if Copilot found anything. If clean, proceed.

**Step 3: Admin-merge**

```bash
gh pr merge 190 --squash --admin --delete-branch --repo GoCodeAlone/core-dump
```

Expected: PR #190 merged; branch deleted.

**Step 4: Verify post-merge**

Run: `gh pr view 190 --repo GoCodeAlone/core-dump --json state,mergedAt,mergeCommit --jq '{state, mergedAt, sha: .mergeCommit.oid}'`

Expected: state=MERGED.

**Rollback (TC1):** git-revertible; deploy pipeline reverts to env-var stopgap form. Plan §C-1 documents this.

**Runtime-launch-validation (per finishing-a-development-branch Step 1b)**: deploy pipeline IS runtime-affecting. Verification: the next `core-dump deploy.yml` workflow run on staging branch should succeed without env-var stopgap on align step (proves the bump is live + W-541 fix is exercised end-to-end).

---

## PR 8: C-1 TC2 — Cascade-replace 4 protected resources to nyc1

**Repo:** `GoCodeAlone/core-dump`
**Branch:** `feat/c2-staging-pg-cutover-nyc1` (NEW, branched from main AFTER TC1 lands)

### Task 8.1: Branch + run TC2 plan preview

**Files:** none yet (planning task)

**Step 1: Branch from current core-dump main**

Run:
```
cd /Users/jon/workspace/core-dump
git fetch origin main
git worktree add _worktrees/feat-c2-staging-pg-cutover-nyc1 -b feat/c2-staging-pg-cutover-nyc1 origin/main
cd _worktrees/feat-c2-staging-pg-cutover-nyc1
```

**Step 2: Run TC2 plan (stdout-only preview)**

Run:
```
wfctl infra plan -c infra.yaml --env staging
```

Expected: stdout shows the cascade plan: 4 protected resources will be replaced + N dependents recreated.

**Step 3: Capture plan output for PR body**

Save the stdout to a file (NOT committed) for PR body inclusion. Confirm the 4 resources are exactly:
- core-dump-vpc
- coredump-staging-pg-data
- coredump-staging-pg
- coredump-staging-pg-fw

If the plan output diverges from this list, STOP and DM team-lead.

### Task 8.2: Execute TC2 cascade-replace + post-cutover /healthz verification

**Files:** none (operator runtime task)

**Step 1: Pre-flight check**

Confirm coredump-staging is operationally OK to be down for ~30-60 min. (Operator decision; staging only, no production impact per design rev3 §TC2 Execution.)

**Step 2: Execute cascade-replace**

Run (literal command per design rev3 §TC2 Execution):
```
wfctl infra apply -c infra.yaml --env staging \
  --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw
```

Expected: actual stdout format will be whatever `wfctl infra apply --allow-replace=...` emits in v0.21.2. The plan does NOT pre-specify a sketch (per adversarial-plan-review-1 finding C-3 — sketches that aren't verified against actual binary behavior are a credibility-erosion class). The implementer captures the FULL stdout transcript verbatim into the TC2 cutover doc (see Step 4 below). Operator-side verification: the transcript MUST show 4 Delete+Create pairs corresponding to the 4 protected resources, plus an `ApplyResult.ReplaceIDMap`-style summary mapping pre→post resource IDs.

Capture the full transcript including the pre/post resource IDs from the ReplaceIDMap output.

**Step 3: Post-cutover /healthz verification**

Run:
```
sleep 30
for i in $(seq 1 30); do
  status=$(curl -s -o /dev/null -w "%{http_code}" https://staging.coredump.<host>/healthz)
  echo "[$(date)] healthz: $status"
  [ "$status" = "200" ] && break
  sleep 10
done
```

Expected: status reaches 200 within ~5 min.

If /healthz never reaches 200: DO NOT commit; DM team-lead. Recovery procedures per design rev3 §TC2 Execution table.

**Step 4: Commit a TC2 marker**

Even though TC2 doesn't change repo files (the apply mutates cloud state, not source), commit a marker file or transcript for audit trail:

```bash
mkdir -p docs/cutovers
cat > docs/cutovers/2026-05-05-tc2-staging-pg-nyc1.md <<EOF
# TC2 Cutover: coredump-staging PG → nyc1

**Date:** $(date -u +%Y-%m-%dT%H:%M:%SZ)
**Operator:** jon@langevin.me
**Allow-replace targets:** core-dump-vpc, coredump-staging-pg-data, coredump-staging-pg, coredump-staging-pg-fw

## Pre/post resource IDs

[paste from ReplaceIDMap output]

## /healthz verification

[paste curl transcript]

## Apply transcript

[paste full wfctl infra apply output]
EOF
git add docs/cutovers/2026-05-05-tc2-staging-pg-nyc1.md
git commit -m "docs(cutovers): TC2 — coredump-staging-pg → nyc1 cascade-replace transcript"
```

### Task 8.3: PR + admin-merge

**Step 1: Push branch + open PR**

```bash
git push -u origin feat/c2-staging-pg-cutover-nyc1
gh pr create --base main --head feat/c2-staging-pg-cutover-nyc1 \
  --title "C-1 TC2: cascade-replace 4 protected resources to nyc1" \
  --body "[Body includes cutover doc + plan output + apply transcript + healthz]"
gh api -X POST /repos/GoCodeAlone/core-dump/pulls/<N>/requested_reviewers \
  -f 'reviewers[]=copilot-pull-request-reviewer[bot]'
```

**Step 2: Copilot review cycle (likely zero findings — doc-only PR)**

Wait ~10 min. Address any Copilot inline comments.

**Step 3: Admin-merge**

```bash
gh pr merge <N> --squash --admin --delete-branch --repo GoCodeAlone/core-dump
```

**Rollback (TC2):** if cascade succeeds but post-cutover issue surfaces, execute git-revert of TC2 commit + manual cleanup of nyc1 resources + restart from pre-TC2 state per design rev3 §Rollback. The drift-postcondition + ApplyResult.ReplaceIDMap protections preserve partial state.

**Runtime-launch-validation**: TC2 IS the runtime touch. Verification IS the /healthz 200 in Step 3 above.

---

## Plan-literal-vs-reality verification (mandatory pre-step per task)

Per design rev3 §Plan-literal-vs-reality surfaces, every task author runs the relevant `git show origin/main:<file>` checks BEFORE writing code. The 7 risky surfaces:

1. **Task 1.1** — `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name` field names. Verify against `git show origin/main:config/secrets_config.go`.
2. **Task 4.1** — `Enumerator` and `EnumerateByTag` symbol uniqueness. `git -C /Users/jon/workspace/workflow grep -n "Enumerator\|EnumerateByTag" origin/main -- '*.go'` must return zero hits.
3. **Task 5.1** — `wfctlhelpers.Plan` and `wfctlhelpers.ApplyPlan` signatures. Verify against `git show origin/main:iac/wfctlhelpers/{plan,apply}.go`.
4. **Task 6.2** — `platform.ComputePlan` signature. Verify against `git show origin/main:platform/differ.go`.
5. **Task 3.1** — `plugin/sdk/manifest_test.go` exists; verify the validator function name (`ValidateManifest` is a guess; check actual).
6. **Task 7.1** — `setup-wfctl` action `version: vX.Y.Z` literal. Verify against the `with:` block in any existing core-dump workflow already using setup-wfctl@v1.
7. **Task 8.2** — `wfctl infra apply --allow-replace=<csv>` flag name. Verify against `cmd/wfctl/infra_apply.go`.

Implementer agents must, as the FIRST step of any task, run these verifications. The "verify against origin/main" pattern is the closure of the recurring plan-literal-vs-reality defect class observed across W-1 through W-9.

---

## Pipeline expectation

Standalone background agents per cluster (proven across W-7 / W-8 / P-DO / C-1). Each agent operates in its own worktree, self-paces via ScheduleWakeup + Monitor + bash watchdog, handles Copilot review cycle independently, and admin-merges per workspace memory `feedback_admin_override_pr_merge` once Copilot resolved + non-Lint CI green.

**Phase 1**: PR 1 (W-541) ships; team-lead cuts v0.21.1 (Task 7.0a).

**Phase 2 parallel sets**:
- Set A (low Copilot surface): PR 2 (W-Precision) + PR 3 (W-Diagnose-540) + PR 6 (DO-Plugin)
- Set B (higher Copilot surface): PR 4 (W-Cleanup) + PR 5 (W-Refactor); per N3 hard rule, W-Refactor pre-flight checks `docs/WFCTL.md` against W-Cleanup's merge.

**Phase 3 sequential**:
1. Cut DO plugin v0.10.1 (Task 7.0b) after PR 6 merges
2. Cut workflow v0.21.2 (Task 7.0c) after PRs 2-5 merge
3. PR 7 (TC1 amend): bump pins, re-run CI, admin-merge
4. PR 8 (TC2 NEW): branch from main, run TC2 cascade + /healthz verify, open + admin-merge

**Coordination via TaskList**: each agent claims tasks via TaskUpdate, following the W-7/W-8/P-DO/C-1 pattern.
