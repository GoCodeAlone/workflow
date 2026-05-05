# Adversarial Plan Review #1 — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Reviewer:** adversarial-design-review skill (Claude Opus 4.7), plan-phase cycle 1
**Target plan:** `docs/plans/2026-05-05-iac-deferred-cleanup.md` @ `cff1df2`
**Inherited design:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` @ `f112d206` (rev3, design-phase PASS at adversarial review #3)
**Date:** 2026-05-05
**Cycle:** 1 of max 2 plan-phase cycles
**Verdict:** **FAIL** — 4 Critical findings, 5 Important findings, 3 Nit findings. Multiple defects that will trip implementer agents at runtime, including a stale stdout sketch flagged but not fixed in design rev3, a Task 7.0a/b/c sequencing-vs-PR-Grouping inconsistency that is materially misleading, a hidden serial dep (PR 6 Task 6.3 → PR 4 Task 4.1) that the parallel-set declaration glosses over, an inherited line-number drift on `SecretEntry.Name` (design says line 47; actual is line 44), and a CLI dispatcher signature mismatch where `runInfraCleanup` cannot be wired into `runInfra` as specified.

---

## Findings

### CRITICAL

#### C-1. PR 6 Task 6.3 has a hidden serial dependency on PR 4 Task 4.1 that contradicts the "Set A vs Set B parallel" claim

**Location:** plan §Pipeline expectation lines 1442-1444 + Task 6.3 Step 1 line 1092.

**Defect.** The §Pipeline expectation block declares:

> Set A (low Copilot surface): PR 2 (W-Precision) + PR 3 (W-Diagnose-540) + **PR 6 (DO-Plugin)**
> Set B (higher Copilot surface): PR 4 (W-Cleanup) + PR 5 (W-Refactor)

implying PR 6 can run **concurrently with PR 4**. But Task 6.3 Step 1 (line 1092) explicitly says:

> Note: this task depends on PR 4 having merged AND workflow tag v0.21.2 being cut OR DO-Plugin's go.mod referencing workflow main HEAD. The implementer should bump DO-Plugin's go.mod to a workflow ref that includes the Enumerator interface BEFORE writing this task's code.

**This is a hard serial dependency from PR 6 → PR 4**, not a soft one. If the DO-Plugin agent starts Set A in parallel with the W-Cleanup agent in Set B, Task 6.3 cannot complete until PR 4 lands at least the Enumerator interface (Task 4.1) on origin/main. The `go.mod` bump to a workflow main HEAD that doesn't yet contain `interfaces.Enumerator` would compile-break `(*DOProvider)(nil)` against `interfaces.Enumerator`.

Two collisions:
1. The §PR Grouping table (line 28-39) shows PR 6 next to PRs 2/3/4/5 with no dependency annotation.
2. The §Pipeline expectation (Phase 2 parallel sets) explicitly puts PR 6 in Set A — implying it does NOT block on PR 4 in Set B.

The DO-Plugin implementer agent following the literal plan will start Task 6.1 + 6.2 (which are independent of PR 4), then hit Task 6.3, attempt to bump go.mod to a workflow commit that does include Enumerator (which only exists post-PR-4-merge), and stall — or worse, write Task 6.3 against a guessed signature, then thrash when PR 4 actually lands with a different signature.

**Severity reasoning.** This is a Critical because (a) it's a sequencing defect, not a comment defect — the agent is given conflicting instructions; (b) it's a recurring class (workspace memory `feedback_per_agent_worktree_per_task_pr` and `feedback_implementer_taskclaim_before_work` both call out hidden serial deps as a high-cost failure mode); and (c) the plan literal in Task 6.3 itself acknowledges the dep ("implementer should bump go.mod ... BEFORE writing this task's code") which contradicts the §Pipeline-expectation parallel claim.

**Fix options.**
- **Option A (preferred):** move PR 6 Task 6.3 to a separate sub-PR (PR 6b) that runs AFTER PR 4 Task 4.1's Enumerator interface lands on workflow main. PR 6 (a) keeps Tasks 6.1 + 6.2 only; runs in Set A. PR 6b (Enumerator impl) runs after PR 4 merges, before workflow v0.21.2 tag-cut so it rolls into DO v0.10.1. This matches the design rev3 §Top doubt #1 phrasing: "DO-Plugin PR can land independently of W-Cleanup PR; the type-assertion at use site means `wfctl infra cleanup --tag` works against DO once both PRs land in any order" — that statement is true if Enumerator-impl is detached from PR 6's other tasks.
- **Option B:** rewrite §Pipeline expectation to put PR 6 in Set B as a co-blocker on PR 4's Enumerator interface; explicitly declare "PR 6 starts Tasks 6.1 + 6.2 in parallel with all others; Task 6.3 is gated on PR 4 Task 4.1 reaching origin/main and triggers a go.mod bump in PR 6's branch."
- **Option C:** move Enumerator interface declaration into Phase 1 (alongside W-541 in v0.21.1) so Task 4.1 is unblocked even when PR 4 is the cleanup-subcommand work. This keeps Set A truly parallel.

---

#### C-2. Tag-cut Task 7.0a/b/c are misclassified as PR 7 work; the §PR Grouping table is materially misleading

**Location:** plan §PR Grouping table line 37 + Task 7.0a/b/c lines 1162-1200.

**Defect.** The §PR Grouping table puts Tasks 7.0a, 7.0b, 7.0c into **PR 7**. PR 7's branch is `feat/c1-staging-pg-cutover` (an existing core-dump PR #190). But:

- Task 7.0a operates on the workflow repo (cuts tag v0.21.1 from workflow main).
- Task 7.0b operates on the workflow-plugin-digitalocean repo (cuts tag v0.10.1 from DO main).
- Task 7.0c operates on the workflow repo (cuts tag v0.21.2 from workflow main).
- Task 7.1 + 7.2 operate on core-dump (the actual PR 7).

Tag-cuts happen on different repos with different tag-author SHAs and have no associated PR branch. They are coordination operations performed by the team-lead, not commits on `feat/c1-staging-pg-cutover`. Putting them in PR 7 is wrong on its face.

Worse, the temporal ordering is hidden in the §Pipeline expectation block at the bottom (lines 1440-1450), but **the §PR Grouping table — which an implementer agent reads first to know what's in scope — implies the tag-cuts are part of the PR 7 commit history**. An agent that "claims PR 7" might assume Tasks 7.0a/b/c are commits to push to `feat/c1-staging-pg-cutover` rather than orchestrator-side coordination.

Also: Task 7.0c says "after PRs 2-5 merge". But Task 7.0a happens after PR 1 merges (Phase 1). The temporal sequence is:
1. PR 1 merges → Task 7.0a (cut v0.21.1)
2. PRs 2, 3, 4, 5 merge (Phase 2 workflow) → Task 7.0c (cut v0.21.2)
3. PR 6 merges (Phase 2 DO-Plugin) → Task 7.0b (cut v0.10.1)
4. Task 7.1 (bumps pins) requires BOTH 7.0b AND 7.0c — meaning 7.0b and 7.0c can run in either order but Task 7.1 blocks on the LAST of the two. Plan does not state this gate explicitly.

**Severity reasoning.** Critical because (a) misclassification is misleading to implementer agents; (b) the cross-task gate ("Task 7.1 blocks on both 7.0b and 7.0c") is unstated, which is the textbook missing-failure-mode class; (c) workspace memory `feedback_team_lead_active_orchestration` mandates the team-lead clearly distinguish orchestrator coordination from implementer work.

**Fix options.**
- **Option A:** rename Tasks 7.0a/b/c to "Coordination 1/2/3" and put them in a §Coordination section AFTER the §PR Grouping table, distinct from per-PR task lists. Add an explicit gate: "Coordination 3 (workflow v0.21.2 tag-cut) AND Coordination 2 (DO plugin v0.10.1 tag-cut) MUST both complete before Task 7.1 starts."
- **Option B:** redraw the §PR Grouping table to add a "Coordination" row between Phase 2 and Phase 3 that lists 7.0a/b/c separately with their gating relationships.

---

#### C-3. Inherited stale stdout-sketch from design rev3 (recommended-for-fixup-but-not-applied)

**Location:** plan §PR 8 Task 8.2 Step 2 lines 1331-1341 + design rev3 line 216.

**Defect.** Adversarial design review #3 explicitly flagged a stale `Loading plan from /tmp/tc2-plan.json ...` line in design rev3 §TC2 Execution and recommended a "one-line fix in §TC2 Execution to drop the 'Loading plan from ...' line from the stdout sketch (or replace with a comment noting 'stdout sketch updated for direct apply')."

Design rev3 was PASSed with this nit unfixed; review #3 line 128 said "The implementer of the TC2 PR will catch this when running the literal command if it isn't fixed during plan-writing."

**Plan @ cff1df2 inherited the stale sketch** at lines 1331-1341 of plan but does NOT show the stale "Loading plan from /tmp/..." line (it's been silently truncated). Inspecting the plan's stdout sketch:

```
Loading config from infra.yaml (env: staging) ...
Replace cascade: 4 protected resources will be replaced + N dependents recreated.
Allow-list verified: 4/4 protected resources opted-in.
[1/4] core-dump-vpc: Delete + Create ...
...
```

The plan's sketch starts with "Loading config from infra.yaml" — different from design rev3's stale "Loading plan from /tmp/tc2-plan.json". So the plan AUTHOR did fix the sketch. **However, it's not clear the operator-runnable command produces this exact stdout** — the plan does not cite an `origin/main:cmd/wfctl/infra_apply.go` line where this format string is emitted. Verification step is missing.

The design adversarial-review #3 nit was "fix during plan-writing"; the plan partially-fixed it (replaced the leading line) but did not VERIFY the resulting sketch matches actual `wfctl infra apply --allow-replace` output. Section §Plan-literal-vs-reality at line 1430 lists `wfctl infra apply --allow-replace=<csv>` flag name verification but NOT stdout-sketch verification. Inconsistency between plan claim ("Expected output (sketch)") and unverified literal.

**Severity reasoning.** Critical because (a) Task 8.2 is the ONLY production-touching task in the entire plan and inheriting a documentation defect from design rev3 leaves the operator second-guessing actual output during a 4-resource cascade-replace on staging; (b) workspace memory `feedback_local_image_launch_validation` mandates "any deploy-touching PR requires `docker compose up + curl /healthz` locally before push" — by the same principle, any production-touching command's expected output must be verified against actual behavior, not "sketched".

**Fix options.**
- **Option A:** before Task 8.2, run `wfctl infra apply --help` against the v0.21.2 binary on a no-op config to capture actual stdout format; replace sketch with verified strings.
- **Option B:** drop the "Expected output (sketch)" block entirely; instead say "Capture actual stdout and verify against pre/post resource ID changes."
- **Option C:** add the sketch verification to §Plan-literal-vs-reality #7 explicitly: "Verify `wfctl infra apply --allow-replace` actual stdout format by running against a fixture; replace sketch with verified strings."

---

#### C-4. `runInfraCleanup` signature mismatch with `runInfra` dispatcher precedent

**Location:** plan Task 4.2 Step 3 (line 632) + Step 4 (line 685).

**Defect.** Step 3 implements:

```go
func runInfraCleanup(ctx context.Context, args []string, stdout, stderr io.Writer, providers []interfaces.IaCProvider) error
```

Step 4 says: "Wire subcommand into `cmd/wfctl/main.go` ... In the `infra` subcommand dispatcher, add a `case 'cleanup':` arm calling `runInfraCleanup`."

But the actual dispatcher pattern on origin/main is:

```
$ git show origin/main:cmd/wfctl/main.go | grep "infra\":"
86:    "infra":           runInfra,

$ git show origin/main:cmd/wfctl/infra.go | grep -E "^func runInfra"
57:func runInfra(args []string) error {
200:func runInfraPlan(args []string) error {
```

All sibling sub-functions take `args []string` and return `error`. They do NOT take `(ctx, args, stdout, stderr, providers)`. The cleanup subcommand cannot be wired into the existing dispatcher pattern as the plan literal specifies — the implementer would need to either (a) change the signature to `func runInfraCleanup(args []string) error` and read providers/stdout from package-level state, or (b) introduce a new dispatcher pattern incompatible with the existing one.

**Severity reasoning.** Critical because (a) the plan-literal-vs-reality verification step at line 1422 lists 7 surfaces but NOT this one; (b) an implementer agent compiling the file as written will fail at the wiring step and need to invent a non-canonical pattern; (c) the design rev3 §W-Cleanup §Files lists `cmd/wfctl/main.go — register subcommand` without specifying the signature.

**Fix options.**
- **Option A:** rewrite Step 3 with the canonical signature `func runInfraCleanup(args []string) error`; pass providers/stdout/stderr via the package-level config that other `runInfra*` functions already use. Cite the precedent file:line.
- **Option B:** add Task 4.0a "Surface the canonical dispatcher signature pattern by reading `git show origin/main:cmd/wfctl/infra.go:200-275` and document the conventions runInfra* functions follow."
- **Option C:** keep the proposed (ctx, args, stdout, stderr, providers) signature but add a Task 4.1.5 to refactor the existing dispatcher pattern to use it for ALL `runInfra*` functions — significant scope creep, NOT recommended.

---

### IMPORTANT

#### I-1. Inherited line-number drift: design rev3 says SecretEntry.Name is at line 47; actual is line 44

**Location:** design rev3 line 88 + plan Task 1.1 Step 1 (line 60-61).

**Defect.** Design rev3 §Phase 1 line 88 says:

> `SecretEntry` has a `Name` field (at line 47)

Plan Task 1.1 Step 1 inherits this:

> Also confirm `cfg.Secrets.Generate` has a `Key` field (at `config/secrets_config.go:14` per design rev3) and `cfg.Secrets.Entries` has a `Name` field (at line 47).

Verifying against `git show origin/main:config/secrets_config.go`:

- `SecretGen.Key` at line 14 ✓
- `SecretEntry.Name` at line 44 ✗ (NOT line 47)
- `SecretsConfig.Entries` field declaration at line 26 (not 47)

The "line 47" was likely copied from design rev1/2 without reverification. Three lines off — small but it's the recurring class workspace memory `feedback_check_versions_actively` mandates against ("never guess, fetch current"). This is a plan-literal-vs-reality defect that the §Plan-literal-vs-reality verification block exists specifically to catch.

**Severity reasoning.** Important (not Critical) because (a) the implementer agent following Step 1 will run `git show origin/main:config/secrets_config.go` and see the truth; (b) the line-number drift is a self-correcting class — the agent finds the field by name, not by line. But it's also a credibility-erosion class: if the design+plan are sloppy on line numbers, implementers grow distrust on other literals (e.g. the "line 232" claim in Task 2.2's VolumeDriver reference, which I did NOT verify — but a wary implementer might re-verify EVERY line number, slowing them).

**Fix options.**
- **Option A:** change Task 1.1 Step 1 to "verify the field NAMES (not line numbers)" — drop the line numbers entirely.
- **Option B:** re-verify all line-number citations in plan and design via `git show origin/main:<file>` and update.

---

#### I-2. Task 4.4 smoke.yml edit ships a no-op gate; plan acknowledges but does not address

**Location:** plan Task 4.4 Step 1 lines 770-779 + plan Out-of-scope line 22.

**Defect.** Task 4.4 modifies `.github/workflows/conformance-smoke.yml` to call `wfctl infra cleanup --tag "conformance-pr-${{ github.event.pull_request.number }}" --fix` with `DO_TOKEN: ${{ secrets.DO_CONFORMANCE_API_TOKEN }}`. But the plan §Out-of-scope (line 22) explicitly says workflow#542 (DO_CONFORMANCE_API_TOKEN provisioning) is deferred-not-required.

So `${{ secrets.DO_CONFORMANCE_API_TOKEN }}` is unset at runtime. The script's first branch is:

```
if [ -z "$DO_TOKEN" ]; then
  echo "::warning::DO_CONFORMANCE_API_TOKEN unset; skipping cleanup. Hourly leak-scrubber will catch orphans."
  exit 0
fi
```

So the cleanup gate is a perpetual silent no-op until #542 lands. Functionally equivalent to the existing stub at smoke.yml:106-118. The plan ships a **gate that is guaranteed not to exercise the new subcommand on production CI** — meaning workflow#536's primary integration point (the smoke gate) is still untested in CI even after PR 4 merges.

The plan does not flag this as a known limitation in the PR 4 body or note it in the §Plan-literal-vs-reality verification. The team-lead reading the §Out-of-scope block + Task 4.4 has to manually bridge the gap.

**Severity reasoning.** Important because (a) it's not a functional defect — the no-op behavior is correct; (b) but it's a hidden semantic regression: the user reading PR 4 thinks "cleanup is now wired to the smoke gate" when actually "cleanup is a no-op in the smoke gate until #542 lands"; (c) workspace memory `feedback_proper_fixes_over_workarounds` calls out exactly this class — visible-pending-fix beats invisible-no-op.

**Fix options.**
- **Option A:** Task 4.4 explicitly emits a `::notice::` (not `::warning::`) line in the no-op branch saying "wfctl infra cleanup --tag wired but DO_CONFORMANCE_API_TOKEN not provisioned (workflow#542); will activate on TOKEN provision." Add to PR 4 body the explicit note "smoke-gate cleanup activates only after workflow#542 (TOKEN provisioning) lands."
- **Option B:** drop Task 4.4 from PR 4 entirely; defer smoke.yml edit to a follow-up PR after #542 lands. PR 4 becomes a pure subcommand+interface PR that ships green CI without integration test.
- **Option C:** add a unit-test fixture in PR 4 that exercises `runInfraCleanup` end-to-end with a fake provider — proving the integration logic is correct independent of the CI gate. Plan Task 4.2 Steps 1-5 already do this; plan should add an explicit reference: "the unit tests in Task 4.2 ARE the integration verification; the smoke.yml edit in Task 4.4 is wiring-only (will activate when #542 lands)."

---

#### I-3. Pre-DM-team-lead trigger missing on Task 4.1 (interface addition)

**Location:** plan Task 4.1 (lines 473-543) + workspace pre-DM policy.

**Defect.** Workspace policy requires pre-DM team-lead before tasks that:
- Add new public interfaces
- Modify `.github/workflows/`
- Touch deploy/runtime infra

Task 4.1 ADDS a new public interface `interfaces.Enumerator` to `interfaces/iac_provider.go`. This is a design-level contract change that other plugins must implement. Plan Task 4.4 explicitly says "Pre-DM team-lead before this task per workspace policy (`.github/workflows/` touch)" — and Task 7.1 also says it. But Task 4.1 has NO such pre-DM directive despite the equivalent gravity (new interface = cross-plugin contract).

For comparison: workspace memory `feedback_brief_implementers_auth_pattern` shows pre-DM-team-lead is the standard when an interface change crosses cluster boundaries. Adding `Enumerator` crosses workflow ↔ all-IaC-plugins.

**Severity reasoning.** Important (not Critical) because the design itself is sound (opt-in pattern, precedent-aligned per ProviderPlanner) — the team-lead pre-DM is procedural, not blocking. But the plan should mirror its own §Task 4.4 / §Task 7.1 discipline.

**Fix options.**
- **Option A:** prepend "**Pre-DM team-lead** before this task per workspace policy (new public interface in `interfaces/iac_provider.go`)." to Task 4.1.

---

#### I-4. Task 7.0a/b/c lack explicit retry/escalation step on Release CI failure

**Location:** plan Task 7.0a Step 4 (line 1188-1192).

**Defect.** Step 4 polls:

```
gh release view v0.21.1 --repo GoCodeAlone/workflow --json assets --jq '.assets | length'
```

until non-zero, with "~2-3 min after push" expected. But there is NO step for "what to do if Release CI fails" (e.g. GoReleaser config drift, GitHub Actions runner outage, signing-cert renewal failure).

Recent workspace history (`project_p3_core_dump_deploy_pipeline_shipped` memory) shows GoReleaser CI has flake-and-fix history (4 upstream bugs surfaced, including buildx stopgap). The plan assumes Release CI is "reliable" (design rev3 Assumption 2) but doesn't budget for flake.

If 7.0a fails:
- Phase 1 hangs because Phase 2 cannot proceed without v0.21.1
- TC1 (Phase 3 Task 7.1) cannot proceed without v0.21.2
- The whole 8-PR plan stalls

**Severity reasoning.** Important because the failure cascade is real but the recovery path is documented elsewhere (workspace memory has runbooks). Adding an explicit escalation step (3-line addition) makes the plan operationally complete.

**Fix options.**
- **Option A:** add Step 5 to each 7.0a/b/c: "If Release CI fails: capture the failed run ID via `gh run view --log-failed`; DM team-lead with run ID + workflow file path; do NOT proceed to next phase. Recovery: typically a config drift in `.goreleaser.yml` or upstream tag-resolution issue (see workspace memory `feedback_go_sum_mismatch_check_compare_first`)."
- **Option B:** acknowledge in §Pipeline expectation that Phase 1 / Phase 2 / Phase 3 transitions block on tag-cut Release CI green; budget accordingly.

---

#### I-5. Task 3.1 ships a failing-CI test without escalating to ADR despite documented "trade-off"

**Location:** plan Task 3.2 lines 452-463 + design rev3 §Top doubt #2 lines 305-306.

**Defect.** Task 3.1 commits a `t.Errorf`-shape failing test to workflow main, and Task 3.2 PR body explicitly notes:

> Test FAILS CI on workflow main from the moment it lands.
> Red CI is the visible "needs fix" signal per design rev3 §W-Diagnose-540 §Behavior.

Design rev3 §Top doubt #2 (line 306) calls this "trade-off: red CI on workflow main until fix lands; this is acceptable per `feedback_proper_fixes_over_workarounds` (visible-pending-fix beats invisible-bug)."

Workspace policy `superpowers:recording-decisions` says: "Use when the design or plan makes a non-trivial trade-off that future contributors will need context for — records an Architecture Decision Record (ADR)..."

A "ship a failing test that turns workflow main CI red" is exactly such a trade-off:
- Future contributors who see red CI may revert it without understanding the intent.
- Other PRs blocked behind W-Diagnose-540 may be force-merged with `--admin` to avoid the red.
- The fix follow-up PR's reviewer needs to know "the test was intentionally red, not accidentally."

The plan creates ADR 010 (Task 5.2) for the platform-vs-provider classification. It does NOT create an ADR for the failing-CI-on-main shape, despite the latter being arguably more impactful (it changes CI semantics, not just code organization).

**Severity reasoning.** Important — recording-decisions skill should fire here per its trigger ("non-trivial trade-off that future contributors will need context for"). The plan acknowledges the trade-off in PR body but the PR body is ephemeral; future contributors searching the workflow repo for "why is this CI red" need a durable durable artifact.

**Fix options.**
- **Option A:** add Task 3.1.5 "Create ADR 011 — Failing-CI-on-Main as Diagnostic Signal" documenting the rationale + the alt-shape (`t.Skip`) + when it's acceptable to flip to alt-shape.
- **Option B:** flip W-Diagnose-540 to the alt-shape (`t.Skip("BUG: ...; see workflow#540")` with runbook entry) — this avoids the red-CI-on-main problem entirely; design rev3 already documents this as the fallback.
- **Option C:** consolidate ADR 010 to cover BOTH platform-vs-provider AND failing-CI-shape decisions; rename to "ADR 010: Conformance + Diagnostic Test Conventions."

---

### NIT

#### N-1. Plan §Plan-literal-vs-reality block has 7 surfaces; per-task Step 1 verifications duplicate the same content

**Location:** plan §Plan-literal-vs-reality (lines 1422-1432) + each task's Step 1.

**Observation.** The plan duplicates verification content. E.g. surface #1 in §Plan-literal-vs-reality says "verify `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name` field names" — and Task 1.1 Step 1 ALSO says this. Surface #6 says "verify `setup-wfctl` action `version: vX.Y.Z` literal" — Task 7.1 Step 2 ALSO does this.

Duplicating per-task verification with a global block is OK if the global block is the canonical reference and tasks are self-contained snippets. But the plan repeats the same instructions in two places — risking divergence on rev2 if only one place is updated.

**Severity reasoning.** Nit — not blocking; just maintenance overhead.

**Fix options.**
- **Option A:** keep the per-task Step 1 inline (self-contained); drop the §Plan-literal-vs-reality global block.
- **Option B:** keep the global block as a checklist; per-task Step 1 says "Run §Plan-literal-vs-reality verification #N for this task before proceeding."
- **Option C:** leave duplication; add a note: "If divergence is found, the per-task verification is canonical; update §Plan-literal-vs-reality to match."

---

#### N-2. Task 4.2 Step 3 implementation has 4 sub-tests + a 50+ line implementation in one task — borderline over-decomposition

**Location:** plan Task 4.2 (lines 546-708).

**Observation.** Counting Task 4.2's content:
- Step 1: 4 test functions (TestInfraCleanup_DryRun, _LiveMode, _NonEnumerator, _PartialFailure) — ~60 lines of test code
- Step 3: ~50 lines of implementation + helper struct
- Step 4: dispatcher wiring (1 line edit, but with the C-4 issue above, possibly more)
- Step 6: smoke-test the binary
- Step 7: commit

Estimated implementation time: 30-60 minutes for a competent implementer including test-debug cycles. Task granularity guideline (per workspace `superpowers:executing-plans`) is "tasks too big (30+ min)? finding."

Mitigation: the 4 tests are tightly related (all exercising the same subcommand). Splitting would create artificial seams.

**Severity reasoning.** Nit — borderline. The task is large but cohesive.

**Fix options.**
- **Option A:** split into Task 4.2a (subcommand impl + dry-run + non-Enumerator tests) + Task 4.2b (live-mode + partial-failure tests).
- **Option B:** leave as-is; flag in spec for the implementer agent to budget 45 min.

---

#### N-3. Task 8.2 references "design rev3 §Rollback table" indirectly; implementer agent must remember to consult

**Location:** plan Task 8.2 Step 3 line 1360.

**Observation.** Step 3 says: "If /healthz never reaches 200: DO NOT commit; DM team-lead. Recovery procedures per design rev3 §TC2 Execution table." But the design rev3 §TC2 Execution recovery table (lines 226-234) is not inlined in the plan. An implementer in the middle of a partial-cascade-replace failure has to context-switch to the design doc.

**Severity reasoning.** Nit — workspace memory `feedback_brief_implementers_auth_pattern` mandates inlining critical context, not deferring. But the plan's §PR 8 Task 8.2 is operator-driven, so the operator (team-lead) is presumably already familiar with the design.

**Fix options.**
- **Option A:** inline the 5-row recovery table from design rev3 §TC2 Execution into plan Task 8.2 Step 3.
- **Option B:** add the table reference more visibly (e.g. boxed callout) to ensure it's not missed.
- **Option C:** leave as-is; the team-lead is the operator and has the design open.

---

## Bug-class scan transcript

### Plan-phase classes

| Class | Run? | Result |
|---|---|---|
| Over/under-decomposition | Yes | N-2 (Task 4.2 borderline 30-60 min); other tasks reasonable. PR 4 has 4 tasks, PR 6 has 3, PR 8 has 3 — all reasonable. |
| Verification-class mismatch | Yes | C-3 (Task 8.2 production touch — sketch unverified); C-4 (runInfraCleanup signature contradicts dispatcher pattern); Task 4.2 Step 6 includes binary smoke-test + --help check ✓. Task 1.1 Step 6 runs full align test suite ✓. Task 3.1 has FAIL-test verification but inherits design's red-CI-on-main trade-off (I-5). PR 7 Task 7.2 Step 1+2 has CI poll + Copilot check (deploy pipeline runtime touch — design rev3 calls out runtime-launch-validation Step 4 with deploy.yml test). |
| Hidden serial dependencies | Yes | C-1 (PR 6 Task 6.3 → PR 4 Task 4.1 hidden serial); C-2 (Task 7.0b + 7.0c → Task 7.1 gate unstated). |
| Missing rollback wiring | Yes | Each PR has a §Rollback section. PR 8's rollback is via "git revert + manual cleanup of nyc1 resources + restart from pre-TC2 state" — high-cost but documented. ✓ |

### Inherited design-phase classes

| Class | Run? | Result |
|---|---|---|
| Unstated assumptions | Yes | Design rev3 lists 11 explicit assumptions. Plan inherits without restating. ✓ for transparency; - for plan being self-contained. |
| Repo-precedent conflicts | Yes | C-4 (CLI dispatcher signature mismatch). Otherwise: ProviderPlanner precedent is correctly applied to Enumerator (design rev3 + adversarial review #3 verified). |
| YAGNI violations | Yes | None visible. Each PR has a single purpose. The opt-in interface design is precedent-aligned, not speculative. |
| Missing failure modes | Yes | I-4 (tag-cut Release CI failure unhandled); C-2 (cross-task gate unstated); C-3 (production stdout sketch unverified). |
| Security/privacy | Yes | DO_CONFORMANCE_API_TOKEN handled via GH secrets ✓. No hardcoded credentials. TC2 runs against staging only ✓. |
| Rollback story | Yes | Per-PR rollback is documented (see Plan-phase Missing rollback wiring above). N-3 nit: rollback table inlining for Task 8.2. |
| Simpler alternative | Yes | None significantly simpler. The opt-in interface is the simpler-than-required alternative. |
| User-intent drift | Yes | User asked: "address all of the deferred work, and then execute on that plan, get all the deferred issues addressed." Plan covers 9 deferred issues + TC1 + TC2. ✓ Note: workflow#542 is OUT OF SCOPE (deferred-not-required per user direction 2026-05-05) — this is intentional per design rev3. |

---

## NEW Critical findings introduced by plan-phase

C-1, C-2, C-3, C-4 are plan-phase findings (not inherited from design phase that already PASSed at adversarial review #3). C-3 is partially inherited (the stale stdout sketch was flagged in design review #3 as "Important nit, recommended for fixup before plan execution") but the plan author did partial fixup without verification — promoting the remaining issue to Critical.

---

## Final verdict

**FAIL.** 4 Critical findings require remediation before plan execution:

1. **C-1**: PR 6 Task 6.3 hidden serial dep on PR 4 Task 4.1 — sequencing fix required.
2. **C-2**: Tasks 7.0a/b/c misclassified as PR 7 work; cross-task gate unstated.
3. **C-3**: Task 8.2 stdout sketch unverified against actual `wfctl infra apply` output.
4. **C-4**: `runInfraCleanup` signature contradicts existing `runInfra*` dispatcher pattern.

5 Important + 3 Nit findings should be addressed but do not block.

**Recommended path:** plan author revises (cycle 2 of 2). Apply Option A or equivalent for each Critical:
- C-1: split PR 6 into 6a (Tasks 6.1+6.2, runs in Set A) + 6b (Task 6.3, gated on PR 4 merge). OR move Enumerator interface to Phase 1 (Option C).
- C-2: rename Tasks 7.0a/b/c to "Coordination 1/2/3" + add §Coordination section + add explicit "Task 7.1 blocks on 7.0b AND 7.0c" gate.
- C-3: re-verify stdout sketch against actual binary output OR drop sketch in favor of "capture actual output."
- C-4: rewrite `runInfraCleanup` signature to match canonical `func runInfra*(args []string) error` dispatcher pattern.

After cycle-2 revision, re-run plan-phase adversarial review (cycle 2 of 2). On PASS, hand off to alignment-check.
