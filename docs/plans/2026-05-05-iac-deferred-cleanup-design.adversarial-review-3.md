# Adversarial Review #3 (out-of-cycle, targeted) — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Reviewer:** adversarial-design-review skill (Claude Opus 4.7)
**Target:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` @ `9f9da276` (rev3)
**Prior review:** `2026-05-05-iac-deferred-cleanup-design.adversarial-review-2.md` @ `4c6ed67`
**Date:** 2026-05-05
**Cycle:** out-of-cycle hotfix verification (skill rule: max 2 cycles; this is bounded + targeted to verify the rev3 hotfix closure of 2 NEW Criticals N1+N2 + N5 only; not a full bug-class scan)
**Verdict:** **PASS** — N1 + N2 + N5 closed; one minor stale-stdout-sketch documentation nit (Important, not Critical, no execution-blocking risk). No NEW Critical regressions introduced by rev3.

---

## Targeted scope

This review verifies ONLY:
1. N1 closure (REQUIRED `EnumerateByTag` → opt-in `Enumerator` interface)
2. N2 closure (persisted-plan `--plan` flow → direct apply for TC2)
3. N5 closure (W-Diagnose-540 silent-skip → fail-CI `t.Errorf`)
4. No NEW Critical regressions introduced by rev3
5. Implementation feasibility re-check on 2 plan-literal surfaces

It does NOT re-scan all bug classes. The standard skill discipline of max-2-cycles applies; this 3rd pass is justified only because rev2's verdict was FAIL on 2 NEW Criticals that came from rev2's own closure work, not from rev1.

---

## N1 closure verification — `Enumerator` opt-in interface

**Status: CLOSED.** Every required edit landed cleanly:

| Required edit | rev3 evidence | Verified |
|---|---|---|
| Interface code block matches workspace precedent | rev3 lines 135-143: `type Enumerator interface { EnumerateByTag(ctx context.Context, tag string) ([]ResourceRef, error) }` with leading comment matching the ProviderValidator/Planner doc-comment shape | `git show origin/main:interfaces/iac_provider.go` lines 52-62 confirm `ProviderPlanner` is opt-in via type assertion. Pattern matches verbatim. |
| Type-assertion-at-use-site code block shown | rev3 lines 146-153: `if enum, ok := provider.(interfaces.Enumerator); ok { ... } else { log.Printf("cleanup: provider %s does not implement Enumerator; skipping", ...) }` | Present and correct. |
| "Plugin coordination" section drops 3 stub PRs | rev3 line 157: "only DO plugin needs the implementation in this cycle (rolled into DO-Plugin PR — see updated DO-Plugin §Files below). AWS/GCP/Azure stubs are NOT required by this design." | Verified. |
| DO-Plugin §Files adds EnumerateByTag impl | rev3 line 172: "Modify: `internal/provider.go` — implement opt-in `interfaces.Enumerator.EnumerateByTag(ctx, tag) ([]interfaces.ResourceRef, error)` using `godo.Tags.List(ctx, name, ...)` ..." + line 173 adds `internal/provider_enumerator_test.go` | Verified. |
| §Rollback W-Cleanup drops plugin-stub revert | rev3 line 296: "**No plugin-stub revert required** because Enumerator is opt-in (per N1 fix) — plugins that haven't implemented it are unaffected by the revert." | Verified. |
| §Top doubts #1 reflects opt-in tradeoff | rev3 lines 304: explicit visible-skip-stdout-vs-leak tradeoff, AWS/GCP/Azure cycle independence, "the opposite (required interface) would have forced 3 plugin-stub PRs as a chicken-and-egg blocker" | Verified. |

Workspace-precedent verification: `git show origin/main:interfaces/iac_provider.go` confirms `ProviderPlanner` opt-in interface at lines 52-62. The `Enumerator` design literally apes that comment shape ("optional interface for providers that ..."), which is exactly the precedent-aligned fix.

Symbol-uniqueness check: `git grep "Enumerator\|EnumerateByTag" $(git rev-parse origin/main) -- '*.go'` returns ZERO HITS. Symbol space is clean.

**No new failure mode introduced**: if all 4 plugins fail to implement Enumerator, the cleanup subcommand becomes a visible no-op (prints `skipped <provider>: no Enumerator interface` for each), which is strictly better than rev2's chicken-and-egg cross-plugin gate failure. Operator behavior change: must implement Enumerator on each plugin before cleanup is useful for that provider; until then, manual cleanup. Acceptable per rev3 §Top doubt #1.

---

## N2 closure verification — direct apply for TC2

**Status: CLOSED.** Every required edit landed cleanly:

| Required edit | rev3 evidence | Verified |
|---|---|---|
| Pre-flight no longer writes `-o /tmp/tc2-plan.json` | rev3 line 203: `wfctl infra plan -c infra.yaml --env staging` — stdout-only preview; no `-o` flag | Verified. |
| "Why no `-o`" rationale cites JIT-rejection guard at `cmd/wfctl/infra.go:292` | rev3 line 206: "the persisted-plan flow at `cmd/wfctl/infra.go:292` rejects plans containing `${MODULE.field}` JIT refs (verified). Any future infra.yaml edit adding such a ref ... would halt this procedure mid-flight. Direct apply (without `--plan`) avoids the JIT-rejection precondition entirely." | Verified — `git show origin/main:cmd/wfctl/infra.go` lines 285-300 confirm the guard at line 292 with literal error: `"this plan requires JIT resolution; persisted plan.json is not supported. Run 'wfctl infra apply' directly without -o/--plan."` Plan citation is precise. |
| Cascade command literal no longer has `--plan ...` flag | rev3 lines 209-212: `wfctl infra apply -c infra.yaml --env staging --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw` — no `--plan` flag | Verified. |
| §Top doubts #3 reflects direct-apply tradeoff | rev3 line 308: "TC2 cascade uses direct apply (no `--plan` file) per N2 fix. Trade-off: no operator-side persisted-plan audit trail. Mitigation: PR body captures pre/post resource IDs (operator runs `wfctl infra plan -c infra.yaml --env staging` first as stdout-only preview; copies the cascade summary into PR body before running apply)." | Verified. |

**No safety regressions**:
- W-3a/b drift postcondition + `ApplyResult.ReplaceIDMap` apply identically under direct apply (rev3 §Pre-flight line 206 explicitly notes this).
- W-6 `--allow-replace=<csv>` per-resource opt-in is still required (line 211 of literal command).
- Audit-trail loss (no persisted plan.json file artifact) is mitigated by PR body capturing pre/post resource IDs (rev3 §Top doubts #3).
- The recovery-table at lines 226-234 still applies; nothing in those flows assumed a persisted plan.

---

## N5 closure verification — W-Diagnose-540 fail-CI test shape

**Status: CLOSED.** Every required edit landed cleanly:

| Required edit | rev3 evidence | Verified |
|---|---|---|
| §Files explicitly says `t.Errorf("BUG: ...")` | rev3 line 114: "if validation does NOT return an error, call `t.Errorf("BUG: extra iacProvider key not rejected — see workflow#540; schema declares additionalProperties:false but library accepts extra keys")`" | Verified. |
| §Behavior says "FAIL-CI from the moment it lands" | rev3 line 117: "This PR is FAIL-CI from the moment it lands. CI red is the visible 'needs fix' signal — a passing test reporting a bug is a lie." | Verified. |
| Alt-shape (`t.Skip`) documented as fallback | rev3 line 117 (continued): "If red CI on main is operationally unacceptable, alternative shape is `t.Skip("BUG: ...; see workflow#540")` with a runbook entry to track the skip — but the fail-CI shape is the recommended option." | Verified. |

§Top doubts #2 also re-frames around the new shape (rev3 line 306: "W-Diagnose-540 explicitly fails CI from the moment it lands (red CI on workflow main is the visible 'needs fix' signal per N5 fix). The fix follow-up PR turns CI green.")

**Phase 3 inclusion gate updated** (rev3 line 119): "If only diagnostic lands → include diagnostic in v0.21.2, defer fix to v0.21.3." Honest about the fail-CI being released into a tag.

---

## Hidden-stale-reference grep — one minor finding

`grep -nE "REQUIRED.*Enumerator|stub PR|plugin-stub|3 plugin|--plan |--plan$|/tmp/tc2-plan|tc2-plan\.json|ErrNotSupported"` on rev3 surfaces only:

- Line 155, 296, 304: ALL acceptable — they negate the old design ("No '3 plugin-stub PR' scope creep", "No plugin-stub revert required because ...", "the opposite (required interface) would have forced 3 plugin-stub PRs ...")
- Line 206, 308: ALL acceptable — they cite "no `--plan`" / "Direct apply (without `--plan`)" / "TC2 cascade uses direct apply (no `--plan` file)"
- **Line 216 — STALE STDOUT SKETCH**: rev3 §Expected stdout (sketch) opens with `Loading plan from /tmp/tc2-plan.json ...` — this is leftover from the rev2 persisted-plan flow and contradicts the cascade command literal at lines 209-212 (which has no `--plan`). The actual `wfctl infra apply` (without `--plan`) does NOT print "Loading plan from ...". This is a documentation inconsistency.

**Severity classification**: Important (not Critical). The OPERATOR-RUNNABLE command (lines 209-212) is correct; only the example stdout is stale. An implementer reading the doc will notice the mismatch when running the actual command. No execution-blocking risk; no safety regression. Recommend a one-line fix in §TC2 Execution to drop the "Loading plan from ..." line from the stdout sketch (or replace with a comment noting "stdout sketch updated for direct apply"). Does NOT warrant a 4th cycle.

---

## Implementation feasibility check (2 plan-literal-vs-reality surfaces)

### Surface 1: W-541 `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name`

`git show origin/main:config/secrets_config.go` confirms (re-verified, no change since cycle 2):
- `SecretGen.Key string` at line 14 (rev3 plan literal `cfg.Secrets.Generate[i].Key` — VERIFIED)
- `SecretsConfig.Generate []SecretGen` at line 33 (path resolution VERIFIED)
- `SecretEntry.Name string` at line 47 (rev3 plan literal `cfg.Secrets.Entries[i].Name` — VERIFIED)
- `SecretsConfig.Entries []SecretEntry` at line 26 (path resolution VERIFIED)
- `cfg.Secrets.Requires` does NOT exist anywhere in the file (rev1 speculation correctly dropped — VERIFIED)

### Surface 2: W-Cleanup `Enumerator` symbol uniqueness on origin/main

`git grep "Enumerator\|EnumerateByTag" $(git rev-parse origin/main) -- '*.go'` returns ZERO HITS. Symbol namespace is clean for the new opt-in interface.

`git show origin/main:interfaces/iac_provider.go` lines 52-62 confirm `ProviderPlanner` opt-in interface as the precedent. The new `Enumerator` interface is structurally identical (single method, ctx + scalar args, returns slice + error, opt-in via type assertion).

Both feasibility checks PASS.

---

## NEW Critical findings introduced by rev3

**ZERO.** No new Critical findings.

The only stale-content finding (line 216 stdout sketch) is documentation-only with no execution-blocking risk. Classified Important nit, recommended for cleanup but not a blocker.

---

## Final verdict

**PASS** — N1 CLOSED, N2 CLOSED, N5 CLOSED. Zero NEW Critical regressions introduced by rev3.

Per autonomous mandate, this closing verification clears the design for `writing-plans` + execution.

One non-blocking nit recommended for fixup before plan execution: line 216's stale "Loading plan from /tmp/tc2-plan.json ..." stdout sketch should be edited to match the new direct-apply flow (one-line edit). The implementer of the TC2 PR will catch this when running the literal command if it isn't fixed during plan-writing.

The cycle-1 + cycle-2 + rev3 transcript demonstrates a healthy iteration:
- rev1 → cycle 1: 5 Critical + 6 Important findings
- rev2 → cycle 2: closed all 11 cycle-1 findings; introduced 2 NEW Critical (N1, N2) + 3 Important (N3, N4, N5) + 1 Important regression (R1)
- rev3 → cycle 3 (this review): closes N1, N2, N5 cleanly; N3 (file-overlap rebase rule) added at line 331; N4 (`ErrNotSupported` typo) evaporated when N1 changed scope; R1 (3-stub scope creep) evaporated when N1 dropped the stubs.

Recommendation: proceed to writing-plans + dispatch.
