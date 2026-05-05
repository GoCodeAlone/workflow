# Adversarial Review #1 — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Reviewer:** adversarial-design-review skill (Claude Opus 4.7)
**Target:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` @ `1a23197`
**Date:** 2026-05-05
**Cycle:** 1 of 2
**Verdict:** **FAIL** — 5 Critical findings, 6 Important findings

---

## Summary

The design captures a sensible 3-phase clustering, but it ships several factual errors against ground-truth-on-`origin/main`, contradicts already-merged operator decisions in `core-dump#190` (TC1.5 SKIPPED, TC2 separate-PR), and inherits unverified speculation that an implementer would discover at code-write time. Most defects are localized and revisable, but two (the v0.10.0 tag-cut gap and the assumption-9 wrong protected-resource list) are foundational enough to require a rev2 before plan execution proceeds.

---

## Critical findings

### C-1: PR #190 is on branch `feat/c1-staging-pg-cutover` but TC2 is explicitly slated as a SEPARATE PR

The design (§PR Grouping table, §Phase 3 step 9) puts TC1 + TC2 into a single bag — `C-1-Close | core-dump | feat/c1-staging-pg-cutover (existing PR #190 + new TC2 commits) | TC1 + TC2`. But PR #190's body (verified via `gh pr view 190 -R GoCodeAlone/core-dump`) states verbatim:

> **TC2 ⏭️ NEXT PR** — Production touch (cascade-replace 4 protected resources on coredump-staging via `--allow-replace`). Will land in a separate PR after this one merges so the new pins are live before the cutover.

> Will branch a new `feat/c2-staging-pg-cutover-nyc1` from main once TC1 lands.

This is a directly-contradicted operator decision. The design's `C-1-Close` bag misrepresents what's already committed-to in PR #190. **An implementer reading the design will try to extend `feat/c1-staging-pg-cutover` after merge and find no branch to push to** (because TC1 will have already landed and the working branch will be deleted by admin-merge-with-delete-branch). They'd have to reverse-engineer the actual sequence from the PR comments.

**Fix:** Split the C-1-Close row into two rows: (a) C-1-TC1 = existing PR #190, branch `feat/c1-staging-pg-cutover`; (b) C-1-TC2 = NEW PR, branch `feat/c2-staging-pg-cutover-nyc1` (cut from `main` AFTER TC1 merges).

### C-2: Workflow-plugin-digitalocean v0.10.0 is unreleased; the design plans to skip it and cut v0.10.1 directly

Verified via `git -C /Users/jon/workspace/workflow-plugin-digitalocean tag -l`: latest tag is **v0.9.2**. Verified via `git log v0.9.2..origin/main`: a single un-tagged commit `6797d9e feat(provider): migrate to v2 IaC dispatch (W-7/W-8 conformance + ApplyPlan) (#61)`.

But `core-dump` PR #190 (TC1) pins `workflow-plugin-digitalocean v0.10.0` and the deferred-cleanup design's Phase 3 step 3 reads "Cut `workflow-plugin-digitalocean v0.10.1` at DO main HEAD" — implying v0.10.0 has already been cut. **It hasn't.** Either:
- (a) v0.10.0 must be cut as a prerequisite before TC1 can pass CI (its lock-file checksum for v0.10.0 won't validate without a real release), and the deferred-cleanup design must add this step explicitly; OR
- (b) the design intends to roll #61 + #62 + #63 + #61 into a single v0.10.1 cut, and the design needs to say so AND the TC1 pin needs to bump to v0.10.1 before merge.

The design says neither. **The plan as written cannot execute** — TC1 will fail lock-file verification on push because the plugin pin references a tag that does not exist.

**Fix:** Either insert a "Cut v0.10.0 at HEAD before TC1 lands" step at the start of Phase 1, OR change Phase 3 step 3 to "Cut v0.10.1 (rolls #61 + #62 + #63 since v0.9.2)" AND add a TC1 amendment to bump the pin from v0.10.0 → v0.10.1 BEFORE PR #190 merges.

### C-3: Assumption #9 names the wrong protected resources

Design §Assumptions #9: *"The 4 protected resources in coredump-staging are: VPC + Database + Redis + App (or similar shape). Names will be discovered at TC2 time from infra.yaml."*

Verified against PR #190 body and the conformance plan §TC2 (`docs/plans/2026-05-03-iac-conformance-and-replace.md` line 2839): the 4 protected resources are **already known and named**: `core-dump-vpc`, `coredump-staging-pg-data`, `coredump-staging-pg`, `coredump-staging-pg-fw`. PR #190 even pre-formats the apply command:

```
wfctl infra apply --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw -c infra.yaml --env staging
```

**The design's "VPC + Database + Redis + App" shape is wrong.** There is no Redis in coredump-staging (verified by reading `/Users/jon/workspace/core-dump/infra.yaml`: 7 modules total, none `infra.cache`). The four resources are: VPC + database connection pool + database + database firewall. Two database-supporting resources, no app, no Redis.

This matters because it telegraphs that the design author hasn't reviewed PR #190 thoroughly. An implementer following assumption-9 will discover at TC2 time that "App is already replaced naturally via Apply (not Replace) since its config didn't change in TC2 scope" — which IS true, but only because the protected resources are storage-tier, not application-tier. The blast-radius reasoning differs (database-tier replace = 5-15min downtime per resource; app-tier replace = ~30s blue-green).

**Fix:** Rewrite assumption #9 with the correct resource names AND the operator-known fact that this is a database-tier replace, not an app-tier replace. Update the rollback story in §Rollback to reflect database-tier blast radius.

### C-4: TC1.5 omission appears to be a gap, but is actually the correct call — the design just doesn't acknowledge the operator decision

Design §Phase 3 jumps from step 6 (admin-merge TC1) to step 7 (execute TC2). The conformance plan rev10 (`docs/plans/2026-05-03-iac-conformance-and-replace.md` line 2826) makes TC1.5 a hard gate before TC2.

But verified via PR #190 body: *"TC1.5 ⏭️ SKIPPED (operator decision) — Per jon@langevin.me 2026-05-05: dedicated DO conformance account adds tracking + billing overhead the team does not want to absorb. Staging IS the test environment; W-6 `--allow-replace=<names>` per-resource opt-in + W-3a/b unit-tested cascade is the safety belt."*

The deferred-cleanup design's omission of TC1.5 is **correct** (the operator officially skipped it), but the design doesn't document this decision OR the safety-belt rationale that replaces it. An implementer reading just this design (without PR #190 context) would either (a) re-introduce TC1.5 unprompted, OR (b) execute TC2 without knowing the safety analysis that justifies skipping it.

**Fix:** Add an explicit "TC1.5 SKIPPED (operator decision per PR #190; safety-belt = W-6 `--allow-replace` + W-3a/b unit-tested cascade)" note in §Phase 3 between step 6 and step 7. Reference workflow#542 as the upstream block that made the dry-run unrunnable.

### C-5: `cfg.Secrets.Requires` does not exist as a field on `SecretsConfig` — the design's W-541 spec hedges around a non-existent field

Design §Phase 1: *"`buildAlignContext` populates `ctx.secretKeys` from `cfg.Secrets.Generate` (and `cfg.Secrets.Requires` if present) alongside existing `ctx.secretGens` population."*

Verified via `/Users/jon/workspace/workflow/_worktrees/iac-conformance-design/config/secrets_config.go` — `SecretsConfig` struct (lines 21-33) has fields: `DefaultStore`, `Entries`, `Provider`, `Config`, `Rotation`, `Generate`. **No `Requires` field exists.** Verified via `grep -rn "Secrets.Requires\|Requires.*SecretsConfig"`: zero hits.

The phrase "(and `cfg.Secrets.Requires` if present)" is dead conditional that an implementer will write defensively (`if cfg.Secrets.Requires != nil { ... }`) and burn ~20 minutes proving doesn't compile. The actual issue #541 also speculates "and `cfg.Secrets.Requires` if the latter exists" — both inherit the speculation without anyone verifying it.

The honest fix is: populate `ctx.secretKeys` from `cfg.Secrets.Generate[i].Key` for each entry. That's the entire fix. There IS a separate `SecretEntry.Name` field on `cfg.Secrets.Entries[i]` that ALSO names declared secrets — and if R-A4 is going to be canonical, it should consult `cfg.Secrets.Entries[].Name` too. **The design considered neither**: it speculated about a field that doesn't exist while ignoring the field that does.

**Fix:** Rewrite §Phase 1 W-541 spec to (a) populate from `cfg.Secrets.Generate[i].Key`, (b) ALSO populate from `cfg.Secrets.Entries[i].Name`, (c) drop the Requires speculation. Add a test for the Entries case alongside the Generate case.

---

## Important findings

### I-1: §W-Cleanup §"Discovery question" decides on an interface name (`ResourceLister`) without checking if it overlaps with the existing `ResourceDriver` interface or the existing state-store `ListResources` method

The design proposes `ResourceLister.ListByTag(ctx, tag) ([]ResourceRef, error)` on `IaCProvider`. Verified via `grep -rn "ListResources" cmd/wfctl/`: there is already a `StateStore.ListResources(ctx context.Context) ([]interfaces.ResourceState, error)` method (`infra_state_store.go:21`). Naming a NEW provider-side method `ListByTag` while a state-side method `ListResources` exists creates two competing "list" verbs in the wfctl codebase that an operator will conflate.

Also: tag-based listing across providers will hit the AWS/GCP/Azure tag-API rate limits much faster than DO's. The design's "plugins that don't implement it are skipped with a log line" is silent-degradation: a multi-provider deployment will quietly leak resources from any provider that hasn't implemented the optional interface. The design should require ALL providers to implement it OR fail-loud at startup if a configured provider doesn't.

**Fix:** Either (a) commit to ALL providers implementing it (and adding a smoke test for each provider during W-Cleanup PR cycle); OR (b) make the absence-of-impl a startup error rather than a silent skip; OR (c) name the method differently to avoid `ListResources`-vs-`ListByTag` confusion (e.g. `EnumerateByTag`).

### I-2: §Phase 2 parallelism §Mitigation "rebase-as-needed at merge time" is too soft for the actual file-overlap risk

Design §Top-doubts #1: *"Mitigation: each PR's file scope is disjoint by design (see "Files" sections); rebase-as-needed at merge time."*

Cross-checking the Files sections:
- W-Precision touches `plugin/external/convert.go` + `cmd/iac-codemod/lint.go` + `plugin/sdk/manifest.go`
- W-Cleanup touches `cmd/wfctl/main.go` (subcommand registration) + `docs/WFCTL.md` + `cmd/wfctl/infra_cleanup.go` (new) + `.github/workflows/conformance-smoke.yml` + `docs/conformance-runbook.md`
- W-Refactor touches `cmd/wfctl/deploy_providers.go` + `docs/adr/010-...md` (new)

`docs/WFCTL.md` is at risk: W-Refactor's ADR may also bump WFCTL.md (per workflow's CLAUDE.md doc-maintenance table). `cmd/wfctl/main.go` is at risk only if W-Refactor renames any wfctl command (unlikely but the design doesn't promise it doesn't). The actual collision-risk is small but not zero.

**Fix:** Either (a) explicitly serialize: W-Precision → W-Cleanup → W-Refactor (each rebased on previous merge); OR (b) document the explicit collision-detection step in the agents' workflow ("before final push, agent runs `git fetch origin main && git rebase origin/main` and resolves WFCTL.md conflicts in favor of additive sections"). The current "rebase as needed" is too soft to act on.

### I-3: §#540 schema additionalProperties — the actual schema ALREADY has `additionalProperties:false` on `iacProvider`

Verified via `git -C /Users/jon/workspace/workflow show origin/main:plugin/sdk/manifest_schema.json`: the schema explicitly declares `"additionalProperties": false` on the `iacProvider` sub-object (line 16). Issue #540 claims this is "not enforced" — but the schema declaration IS there.

The actual bug must be one of: (a) the jsonschema library version is older than draft-2020-12 enforcement; (b) the validator is being called with a "lax" mode flag; (c) the draft URL on line 2 doesn't match the library's parser version. The design's mitigation ("Likely fix: explicit `Strict: true` flag on validator, OR upgrading the library") is two different fixes for two different root causes — without diagnosis first, the implementer will guess and the wrong guess will silently fail.

The design's assumption "MANY plugin manifests in the registry may have extra keys that suddenly fail validation" is also overstated. Looking at the schema: only the `iacProvider` sub-object is strict; the root object permits additional properties. If the strict-on-iacProvider path was already enforcing, P-DO PR #61 would not have reached merge. The most likely root cause is library-version-too-old (santhosh-tekuri/jsonschema/v6 may have changed default behavior between minor versions).

**Fix:** Phase the W-Precision PR's #540 work as: (1) write a failing test that proves current behavior is lax; (2) bisect the library version where strictness was honored; (3) either pin the right version OR upgrade. Don't pre-commit to a "strict flag" that may not exist in the API.

### I-4: §Phase 1 estimate "5-line additive fix" doesn't account for the new test file

§Components: *"Test: `cmd/wfctl/infra_align_test.go` — new test pinning R-A4 success on top-level `secrets.generate` case."*

Verified via reading `cmd/wfctl/infra_align_test.go:421-448` (test `TestInfraAlign_RA4_SecretsGenerate_DoesNotFire`): the existing test covers the MODULE-form `secrets.generate` case. The new test for the TOP-LEVEL `secrets:` block case is approximately 25-30 lines (yaml setup + assertion), bringing the actual PR diff closer to ~35 lines (5 prod + 30 test). That's still small, but "5-line additive fix" undersells the work and the Copilot review surface area.

**Fix:** Update §Phase 1 §Behavior to read "Production change ~5 lines; test addition ~30 lines; total PR diff ~35 lines."

### I-5: Workflow#542 (DO_CONFORMANCE_API_TOKEN provisioning) is not in the 9-item list but IS open

Verified via `gh issue list -R GoCodeAlone/workflow --state open`: issue #542 is open and was filed at the same time as PR #190 (2026-05-05). It blocks W-7 conformance smoke gate from doing anything (the gate is currently a silent kill-switch no-op). The design's premise of "9 deferred work items" excludes this issue without rationale.

The design needs to either: (a) explain why #542 is out-of-scope (e.g., "operator deferred — token provisioning is a manual ops task, not an engineering task"), OR (b) include it as item #10 in Phase 2 with the W-Refactor cluster (since both are operator-toggle work).

**Fix:** Add a §Excluded Items section listing #542 + the rationale. Don't leave a 542-shaped hole next to a "9 deferred items" claim.

### I-6: §Rollback for W-Cleanup says "conformance-smoke.yml falls back to T7.14 leak scrubber (the existing safety net)"

Verified via `grep -rn "T7.14"`: T7.14 is the leak-scrubber task in the conformance plan. The conformance-smoke.yml + conformance-leak-scrubber.yml + dedup helper landed in W-7 (PR #535). The design's rollback note assumes both files exist on origin/main.

This IS true (verified via `git ls-tree origin/main .github/workflows/`), but the rollback statement is vague: "falls back to T7.14 leak scrubber" doesn't specify whether the existing conformance-smoke.yml is left untouched (in which case "fall back" is misleading — it never moves forward) OR whether reverting W-Cleanup means also reverting the conformance-smoke.yml edit (in which case the test infra is left in a half-modified state).

**Fix:** Rewrite the rollback to explicitly say "revert both the new `infra_cleanup.go` AND the conformance-smoke.yml edit; the leak-scrubber hourly job continues to clean up orphans."

---

## Bug-class scan transcript

| Class | Finding/Clean | Notes |
|---|---|---|
| Unstated assumptions | **Findings: C-2, C-3, C-5** | Tag v0.10.0 cut, protected-resource list, Requires field — all unwritten/wrong. |
| Repo-precedent conflicts | **Findings: I-1, C-1** | ListResources vs ListByTag naming; PR #190 explicit branch decision contradicted. |
| YAGNI violations | **Clean** | All 9 items map to filed issues with operator/automation justification. |
| Missing failure modes | **Findings: I-1, I-3** | Optional ResourceLister silent-skip leaks resources; #540 mitigation assumes wrong root cause. |
| Security / privacy at architecture level | **Clean** | No secret-flow changes; PR-cycle bound; no new privileged paths. Only minor: I-1's silent-skip could mask a tag-based resource leak (security-adjacent). |
| Rollback story | **Findings: I-6** | W-Cleanup rollback under-specified; otherwise per-PR revert paths look complete. |
| Simpler alternative not considered | **Findings: C-5** | The simpler R-A4 fix is "use `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name`" — no Requires speculation. Also: simpler alternative to the W-Cleanup interface = "extend each provider's existing per-resource Delete to take an optional tag-filter" without new interface. |
| User-intent drift | **Clean (mostly)** | The user asked to address all deferred work + close TC1/TC2. Design covers items 1-9 + closes C-1. Missing #542 (I-5) is an explicit gap, not drift. |

---

## Per-attack-prompt-question evaluation

1. **Phase 1 critical-path estimate "5-line additive fix"** — PARTIAL. Production code IS ~5 lines of additive iteration (verified). But the new test is ~30 lines (I-4). And the spec hedges around a non-existent `cfg.Secrets.Requires` field (C-5).

2. **Phase 2 parallelism risk** — Important finding (I-2). Mitigation is too soft; specific overlap files identified.

3. **#540 blast radius** — Important finding (I-3). Schema actually IS strict on iacProvider; the bug is library/version, not schema. Wrong root-cause framing risks wrong fix.

4. **#536 interface design** — Important finding (I-1). Naming overlaps with existing `ListResources`; silent-skip on missing impl is a leak risk; "list+delete in one call" alternative not considered.

5. **TC2 staging blast radius** — Critical finding (C-3). Design names wrong resources. Verified resources are database-tier (5-15min downtime), not app-tier; design says "VPC + Database + Redis + App" which has no Redis in this app.

6. **Plan-literal vs reality recurring defect class** — NOT proactively named. The design has only "Top doubts" (3 self-challenges) and no explicit "where will plan-literal-vs-reality bite this PR" section. Given the recurrence pattern (W-4, W-5, W-9, W-7, P-DO, T8.2 all hit this), the design SHOULD enumerate likely surfaces (e.g., "W-Cleanup's `wfctl infra cleanup --tag` command spec must be checked against the actual flag-parser library"; "W-Refactor's `wfctlhelpers.Plan` reference must be verified against the actual function signature on origin/main").

7. **ADR 010 timing** — Acceptable as scoped (low-cost, captures already-applied pattern, no future work blocked). Design could be more explicit that ADR-010 is documenting a SHIPPED pattern (W-7's bypass-cfg.Provider() pattern in conformance), not introducing a new one.

8. **Copilot-cycle budget** — UNADDRESSED. Design assumes 1-5 rounds per PR but W-8 took 12. 4 PRs × worst-case 12 = 48 review cycles. The serialization tradeoff (lower throughput, easier reviewer attention) is not discussed. Recommend: keep parallel for W-Precision + DO-Plugin (low Copilot surface); serialize W-Cleanup + W-Refactor (new feature + interface refactor have higher Copilot-cycle risk).

9. **TC2 production touch authorization** — Acknowledged as pre-authorized but the actual command + expected output + recovery procedure is one bullet (Phase 3 step 7). Critical finding C-3 also applies (wrong resource list). Recommend: dedicate a "§TC2 Execution" subsection with literal command, expected stdout sample, expected /healthz response, recovery commands for each of 4 partial-failure modes.

10. **Workflow tag-bumping cycle** — Tradeoff not considered. v0.21.1 then v0.21.2 = 2 GoReleaser CI runs (~10 min each = 20 min wall-clock + 2 cache flushes for downstream consumers). Combining (e.g., merge W-541 to a non-main branch, accumulate Phase 2 PRs there, cut single v0.21.1 at the end) trades wall-clock for blocked-on-each-other risk: if Phase 2 has any PR that fails CI, ALL Phase 2 PRs block. Recommend: keep two-tag plan as-is (the parallelism gain is worth 10 min) but document why.

---

## Options the author may not have considered

### Option 1 — Cut v0.10.0 explicitly + insert as Phase 0

Add a new Phase 0 (before Phase 1): "Cut workflow-plugin-digitalocean v0.10.0 at HEAD." This unblocks PR #190 from a phantom tag. Cost: ~10 min (one GoReleaser run). Benefit: deterministic execution.

### Option 2 — Defer #536 (W-Cleanup) entirely

The W-Cleanup work introduces a new interface (`ResourceLister`) and adds rollback risk to the conformance-smoke.yml. The current state (T7.14 leak scrubber as hourly safety net) is operationally adequate. Filing #536 doesn't BLOCK anything; it's a cleanup-quality enhancement. Defer to v0.22.x and shrink Phase 2 from 4 PRs to 3.

Tradeoff: user mandate is "address ALL deferred items." If this option is taken, document the explicit deferral rationale ("optional cleanup; existing safety net adequate; defer to next minor cycle to focus this cycle on blocker-resolution").

### Option 3 — Move #540 (W-Precision schema enforcement) to a separate diagnostic-first PR

The design bundles #540 into W-Precision but admits in §Top-doubts #2 that it has unknown-blast-radius until investigated. Splitting it: ship W-Precision as #537 + #539 only; file a separate W-540-diagnostic PR that adds a one-line "schema accepts iacProvider key 'foo' silently" failing test that proves the bug; then the fix lands in a follow-up PR with known-bounded blast radius. Two PRs instead of one but each is small + reviewable.

### Option 4 — Verify the v0.10.x tag situation BEFORE writing more design

The design's biggest risk is C-2 (phantom tag). Before any other revision, the author should verify the actual DO plugin tag state and align with PR #190's actual pin assumptions. If v0.10.0 truly hasn't been cut and TC1's pin is to-be-bumped, the design's whole tag sequencing changes.

---

## Verdict + next step

**FAIL** — 5 Critical findings (C-1 contradicts merged PR; C-2 references unreleased tag; C-3 wrong protected-resource list; C-4 missing operator-decision documentation; C-5 hedges around non-existent field) plus 6 Important findings.

Per the autonomous mandate, this is cycle 1 of 2. Recommended rev2 changes:
1. Apply Option 1 (Phase 0 = cut v0.10.0).
2. Apply C-1 fix (split C-1-Close into TC1 + TC2 rows; correct branch names).
3. Apply C-3 fix (correct 4 protected resources; database-tier blast radius).
4. Apply C-4 fix (document TC1.5 SKIPPED + safety-belt rationale).
5. Apply C-5 fix (simplify W-541 spec; remove Requires speculation; add Entries[].Name).
6. Apply I-3 fix (#540 phased as diagnose-first).
7. Apply I-5 fix (document #542 as out-of-scope or include as item #10).
8. Add §Plan-literal-vs-reality surfaces section (verify against origin/main, not against this worktree which is 11-commits-behind).

If rev2 lands all of the Critical fixes, the next cycle should PASS. If rev2 misses any Critical, escalate to user per the autonomous-mandate cycle-2 escalation rule.
