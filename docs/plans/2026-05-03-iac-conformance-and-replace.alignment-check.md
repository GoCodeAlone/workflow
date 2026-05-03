# Alignment Check Report — IaC Conformance + Replace

**Design:** `docs/plans/2026-05-03-iac-conformance-and-replace-design.md` (rev2 with user Path A override)
**Plan:** `docs/plans/2026-05-03-iac-conformance-and-replace.md` (rev10 with user Option-C ratification)
**Gate:** superpowers:alignment-check (pre-subagent-driven-development)

---

### Alignment Report

**Status:** PASS

**Coverage (forward trace: design → plan):**

| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| **Issue A** (hash-mismatch diagnostic; per-key env-var fingerprint) — design §W-1 | T1.1 (IaCPlan.SchemaVersion + InputSnapshot + PlanAction.ResolvedConfigHash + DriftEntry); T1.2 (inputsnapshot.Compute + sentinel); T1.3 (wire into plan.json); T1.4 (per-action ResolvedConfigHash); T1.5 (typed ErrEnvVarChanged + persisted-plan path); T1.6 (gitignore warning); T3.0.4 + T3.1.5 (in-process drift postcondition; split out of T1.5 to avoid W-1↔W-3a circular dep) | Covered |
| **Issue B / F** (state outputs lag; refresh-outputs) — design §W-2 | T2.1 (refreshoutputs.Refresh helper); T2.2 (`wfctl infra refresh-outputs` subcommand); T2.3 (cheap apply-time refresh, opt-in WFCTL_REFRESH_OUTPUTS, --skip-refresh flag); T2.5 (concurrency stress); T2.6 (docs); T2.7 (runtime-launch-validation) | Covered |
| **Issue C** (NeedsReplace not honored; Replace action) — design §W-3 | T3.0 (manifest computePlanVersion field); T3.0.4 (3 ApplyResult fields); T3.1 (wfctlhelpers.ApplyPlan skeleton + 4-action dispatch); T3.2 (doCreate + UpsertSupporter); T3.3 (doUpdate + doDelete); T3.4 (doReplace + ReplaceIDMap); T3.5 (diff cache); T3.6a–f (ComputePlan signature + Diff dispatch + bounded concurrency + cache wiring); T3.7 (manifest-driven v1/v2 routing); T3.9 (gRPC runtime-launch-validation); T3.10 (PR description bug call-out) | Covered |
| **Issue D** (region constraints; ValidatePlan + R-A10) — design §W-4 | T4.1 (ProviderValidator interface + Diagnostic type); T4.2 (R-A10 align rule); T4.4 (docs); T4.5 (verification) | Covered |
| **Issue E** (infra_output secret resolution timing; JIT) — design §W-5 | T5.1 (jitsubst.ResolveSpec); T5.2 (wire into ApplyPlan); T5.3 (cascade Replace JIT); T5.4 (SchemaVersion=2); T5.5 (reject persisted JIT plans); T5.7 (runtime-launch-validation) | Covered |
| **Issue G** (protected-replace inflexibility; --allow-replace) — design §W-6 | T6.1 (--allow-replace flag); T6.2 (batch-discover blockers + copy-paste flag); T6.4 (docs + verification) | Covered |
| **Issue H** (provider conformance suite; smoke gate) — design §W-7 | T7.1 (conformance.Run entry); T7.2–T7.12 (12 scenarios including UpsertOnAlreadyExists added beyond design's 11; covers all 11 design scenarios); T7.13 (DO smoke gate CI + budget kill-switch + runbook); T7.14 (leak scrubber + balance alarm + dedup helper) | Covered |
| **Codemod tooling + advisory reports** — design §W-8 | T8.1 (skeleton); T8.2 (lint mode w/ 4 assertions); T8.3 (refactor-plan); T8.4 (refactor-apply with informative non-canonical reports); T8.5 (add-validate-plan); T8.6 (Makefile migrate-providers); T8.7 (verification); P-DO/TP1 attaches DO codemod report as CI artifact + sticky PR comment; final verification step 5 covers AWS/GCP/Azure advisory issue filing | Covered |
| **Per-plugin computePlanVersion opt-in** — design §W-9 | T3.0 (manifest field schema); T3.7 (manifest-driven dispatch routing) — these design-required pieces moved into W-3a/W-3b in plan; W-9 retained for ProviderPlanner + cross-plugin build | Covered |
| **Optional ProviderPlanner interface (Tofu/Pulumi-style hook)** — design §W-9 | T9.1 (ProviderPlanner interface + TDD compile test) — restored per rev10 user Option-C ratification; T9.3 (adapter author guide doc) | Covered |
| **DO plugin migration (P-DO)** — design sequencing table | TP1 (codemod report CI); TP2 (hand-port upsert recovery to upsertSupporter); TP3 (ValidatePlan for DO region constraints); TP4 (set computePlanVersion: v2); TP5 (conformance test + version bump) | Covered |
| **core-dump cutover (C-1)** — design sequencing table | TC1 (bump pins + revert tactical workarounds); TC1.5 (dry-run cascade against ephemeral DO project on conformance account with budget pre-check); TC2 (move staging to nyc1 + verify /healthz) | Covered |
| **Latent bug 1: delete-via-Apply state leakage** — design §Latent bugs | T3.3 (doDelete implementation); T3.10 (PR description call-out); conformance scenario `Scenario_DeleteActionInApplyInvokesDriverDelete` (T7.x) | Covered |
| **Latent bug 2: plan-time gRPC cost regression** — design §W-3 §Plan-time gRPC cost mitigation | T3.5 (diff cache with key per design); T3.6e (bounded concurrency via WFCTL_PLAN_DIFF_CONCURRENCY); T3.6f (cache wiring); conformance scenario `Scenario_DiffSurvivesGRPCRoundTrip` (T7.x); T3.9 gRPC-loaded stub provider runtime test | Covered |
| **Cross-plugin build CI gate (BC compatibility for AWS/GCP/Azure)** — design §Sequencing + §Assumption 9 | T9.2 (cross-plugin-build-test.yml + ADR 006 superseded + ADR 007 created) | Covered |
| **Out-of-scope items** (AWS/GCP/Azure migration deferred per Path A; persisted JIT plans; zero-downtime replace; cross-team coordination workflow; apply --refresh; AGE on DO PG) — design §Out of scope | Plan §Scope Manifest "Out of scope" list (lines 139-146) — matches design exactly | Covered |

**Scope Check (reverse trace: plan → design):**

| Plan Task | Design Requirement | Status |
|---|---|---|
| T1.1–T1.6 | Design §W-1 (Issue A — hash-mismatch diagnostic) | Justified |
| T2.1–T2.7 | Design §W-2 (Issues B + F — refresh-outputs) | Justified |
| T3.0 | Design §W-9 (per-plugin computePlanVersion opt-in; relocated into W-3a so T3.7 can read manifest in same PR) | Justified |
| T3.0.4 | Design §W-3 §Cascade replace + ProviderID propagation (ApplyResult.ReplaceIDMap) + design §W-1 (InputSnapshot wiring through ApplyResult for in-process drift) — consolidated into single struct mod per cycle-5 Option 4 | Justified |
| T3.1, T3.2, T3.3, T3.4 | Design §W-3 §wfctlhelpers.ApplyPlan shared helper (4 actions: create/update/replace/delete + upsertSupporter hook) | Justified |
| T3.1.5 | Design §W-1 (in-process drift diagnostic — split out of T1.5 to avoid W-1↔W-3a circular package dep; same design intent) | Justified |
| T3.5 | Design §W-3 §Diff cache (key includes plugin-version, type, providerID, sha-config, sha-outputs per design Minor finding) | Justified |
| T3.6a–T3.6f | Design §W-3 §ComputePlan refactor (Diff dispatch, bounded concurrency, cache wiring); split into 6 sub-tasks per cycle-2 PR-size finding (W-3 was 16 tasks; split into W-3a 6 tasks + W-3b 10 tasks) | Justified |
| T3.7 | Design §W-9 §Manifest-driven dispatch (computePlanVersion gates v1/v2 routing); design specifies no env-var | Justified |
| T3.9 | Design §Tests §runtime-launch-validation requirement for runtime-affecting changes (W-3 named explicitly) | Justified |
| T3.10 | Design §Latent bug 1 §"operators relying on the current (broken) behavior MUST be alerted via the W-3 PR description" | Justified |
| T4.1, T4.2, T4.4, T4.5 | Design §W-4 (ValidatePlan hook + R-A10 align rule) | Justified |
| T5.1–T5.7 | Design §W-5 (Per-module JIT secret resolution + ProviderID propagation; SchemaVersion bump; reject persisted JIT plans per design §Top doubts §2) | Justified |
| T6.1, T6.2, T6.4 | Design §W-6 (--allow-replace flag); design §W-5 §Partial-cascade discovery (batch-report blockers with copy-paste flag) | Justified |
| T7.1–T7.12 | Design §W-7 §Full conformance scenarios (11 listed; plan adds Scenario_UpsertOnAlreadyExists as a 12th to cover the upsertSupporter hook from T3.2 — justified by design §W-3 upsert recovery requirement) | Justified |
| T7.13 | Design §W-7 §Smoke gate (DO scope only per Path A; budget cap per design §Top doubts §3 with revised estimate) | Justified |
| T7.14 | Design §W-7 §Smoke gate cleanup (`t.Cleanup` + outer `always()` + hourly safety scrubber explicitly listed as `conformance-leak-scrubber.yml`) | Justified |
| T8.1–T8.7 | Design §W-8 (codemod tooling — 4 modes: refactor-plan, refactor-apply, add-validate-plan, lint; dry-run default; informative reports for non-canonical idioms) | Justified |
| T9.1 | Design §W-9 §Optional ProviderPlanner interface — restored per rev10 user Option-C ratification (chat reply 2026-05-03 "option C") | Justified |
| T9.2 | Design §Assumption 9 (cross-plugin BC verification — design's W-3 sequencing constraint that AWS/GCP/Azure compile-compat is verified mechanically) + ADR 007 records the user override | Justified |
| T9.3 | Design §W-9 §"reserved as extension hook for Tofu/Pulumi-style adapter plugins" — adapter author docs called for so future implementer can wire the type-assertion; restored in rev10 alongside T9.1 | Justified |
| TP1 | Design §W-8 §Two-step migration flow per plugin step 1 (codemod dry-run + report) | Justified |
| TP2 | Design §W-8 §two-step migration flow §"DO upsert recovery → emit suggested wfctlhelpers.upsertSupporter hook patch"; design §W-3 upsertSupporter | Justified |
| TP3 | Design §W-4 §Cross-provider constraint examples §DO region constraints | Justified |
| TP4 | Design §W-9 §"DO opts in alongside its codemod migration" | Justified |
| TP5 | Design §W-7 §Smoke scenario per provider; design §Tests §"P-* (per-plugin migration): conformance suite must pass" | Justified |
| TC1 | Design §C-1 (bump wfctl + plugin pins; revert tactical workarounds) | Justified |
| TC1.5 | Not explicitly in design — added per cycle-2 finding (cascade dry-run is first-production-use of W-3a+W-3b+W-5+W-6+P-DO together; needs verification before TC2 unprotects staging). Justified by design §Rollback "post-W-3 rollback in production" risk surface. | Justified (precautionary; aligned with design's risk-surface assessment) |
| TC2 | Design §C-1 (complete the staging-PG migration to nyc1) | Justified |

**Drift Items:** none

**Verdict reasoning:** Every one of the 8 root-cause issues (A through H) maps to specific tasks across W-1 through W-7. The provider conformance suite (Issue H) is implemented in W-7 with all 11 design-specified scenarios plus one extra (Scenario_UpsertOnAlreadyExists) that legitimately covers the upsertSupporter hook from T3.2. The DO migration (P-DO) and core-dump cutover (C-1) match the design's sequencing table. The plan's PR sequence W-1 → W-2 → W-3a → W-3b → W-4 → W-5 → W-6 → W-7 → W-8 → P-DO → C-1 with W-9 as a parallel CI-only branch (gated only by W-3b) matches both the design's sequencing table and the plan's bottom-of-document dependency graph. The rev10 W-9 expansion (T9.1 ProviderPlanner interface + T9.2 cross-plugin CI + ADRs + T9.3 adapter docs) is justified by user Option-C ratification recorded explicitly in plan §Open Questions and ADR 007. The plan's "Out of scope" list (AWS/GCP/Azure migration, persisted JIT plans, zero-downtime replace, cross-team coordination, apply --refresh, AGE on DO PG) matches the design's out-of-scope section verbatim. The single notable deviation — TC1.5 (cascade dry-run on conformance account before TC2 unprotects staging) — is not in the design's task list but is justified by the design's own §Rollback risk-surface text and is a defensive precaution rather than scope creep. No design requirement is uncovered; no plan task is unjustified. Plan is locked-in and ready for subagent-driven-development dispatch.
