# Retro: contract-diff extension for verify-capabilities

**PR:** #773 — feat(sdk+wfctl): contract-diff extension for verify-capabilities (workflow#767)
**Merged:** 2026-05-24
**Branch:** feat/767-contract-diff
**Design:** docs/plans/2026-05-24-contract-diff-design.md
**Plan:** docs/plans/2026-05-24-contract-diff.md
**Related ADRs:** decisions/0042-verify-capabilities-iac-namespace.md

## Adversarial-review findings, scored

The design doc's "Revision history" section records findings across 4 design cycles and 3 plan cycles. Adversarial reports were not committed as files; findings are reconstructed from the design's inline revision history and the in-progress.jsonl skill-activation log.

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design cycle 1 | Hardcoded namespace `workflow.iac.v1.*` — wrong; actual proto package is `workflow.plugin.external.iac.*` | Critical | Resolved upfront — cycle 2 switched to programmatic derivation via `pb.IaCProviderRequired_ServiceDesc.ServiceName`; codified in ADR 0042 |
| design cycle 1 | Duplicates `registeredIaCServices`/`iacServiceRequired` helpers without citing or reusing | Critical | Resolved upfront — design §5 explicitly requires reuse; plan Task 4 names `serviceNamesFromRegistry` (different name to avoid package-main clash) |
| design cycle 2 | Set-equal diff would force lockstep JSON + Go updates on every optional-interface addition | Important | Resolved upfront — design §3 switched to directional diff (missing=FAIL, extra=WARN) before plan was written |
| design cycle 2 | `IaCStateBackend` inclusion ambiguity between wire-level (`iacServices`) and name-level (`iacStateBackends`) | Important | Resolved upfront — field docstring documents orthogonality; sweep populates both consistently |
| design cycle 2 | Embedded manifest path (`sdk.WithManifestProvider`) doesn't surface `iacServices` on the wire | Important | Resolved upfront — design §Non-goals documents this as out-of-scope; disk plugin.json is authoritative |
| design cycle 2 | Sweep-target plugins may not be on workflow v0.62.0+ when sweep PRs open | Important | Resolved upfront — design §Assumptions #3 requires per-plugin pre-flight version check; sweep deferred to post-merge |
| design cycle 4 | #765 PR used inline-spawn pattern (not adapter); cycle-3 design had adapter-based hypothesis | Important | Resolved upfront — cycle 4 re-audited and corrected; plan Task 4 reflects actual `pbClient.GetContractRegistry` call site |
| design cycle 4 | `iac_contract_filter.go` refactor proposed but package-main direct call works; no new file needed | Minor | Resolved upfront — dropped before scope-lock; plan documents this decision in implementer notes |
| plan cycle 1 (adversarial) | `registeredIaCServices` name clash — `deploy_providers.go:350` already defines this in `package main` | Critical | Resolved upfront — new helper named `serviceNamesFromRegistry` unconditionally in Task 4 |
| plan cycle 1 (adversarial) | Task 5 fixture `go.sum` not committed — `buildFixtureBinaryForVerify` uses `-mod=readonly` which fails without it | Critical | Resolved upfront — Step 2 added explicit `go.sum` check; Task 5 commit step added sanity check |
| plan cycle 1 (adversarial) | Client-side namespace filter absent — old-SDK binaries would produce WARN-spam on every infra service | Important | Resolved upfront — Task 4 §D adds `iacPrefix` derivation + `strings.HasPrefix` filter in `serviceNamesFromRegistry` |
| plan cycle 1 (adversarial) | No dedup test for both-top-level-AND-nested manifest input | Important | Resolved upfront — `TestPluginManifest_IaCServices_DeduplicatesAcrossTopLevelAndNested` added to Task 1 |
| plan cycle 2 (adversarial) | `iac-missing-service` fixture embeds `UnimplementedIaCProviderFinalizerServer` — satisfies interface via mustEmbed sentinel, secretly registering the service and producing a false PASS | Critical | Resolved upfront — embed removed; critical comment added explaining gRPC interface-satisfaction semantics |
| plan cycle 2 (adversarial) | Final verification 3b bash pipeline `$?` reads `tee` exit not wfctl exit — smoke-test silently passes regardless of actual behavior | Critical | Resolved upfront — restructured to redirect to file + capture `WFCTL_EXIT=$?` before any pipeline |

## Gate misses

No gate misses this PR. All downstream issues were caught by the gate they were assigned to, or were genuinely novel and not in any gate's bug-class scope.

Neither CI nor code review surfaced any issues not already caught by adversarial-design-review or adversarial-plan-review. The PR's five CI run categories (CI, cross-plugin-build-test, CodeQL, Benchmark, OSV-Scanner) all passed on first push. No human or Copilot review comments were posted. No follow-up fix commits were needed post-push.

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | 4 design adversarial cycles (skill invoked as adversarial-design-review --phase=design) |
| adversarial-design-review (design) | yes | Cycles 1–4 per in-progress.jsonl; cycle 1 caught 2 Critical (wrong namespace + duplicate helpers); cycles 2–4 caught 7 Important |
| writing-plans | yes | Two writing-plans invocations (after cycle 3 initial pass; revised again post cycle 4 replan against actual shipped code) |
| adversarial-design-review (plan) | yes | Cycles 1–3 per in-progress.jsonl; caught 2 Critical (name clash + go.sum absent) + 3 Important |
| alignment-check | yes | Two cycles; PASS cycle 2 |
| scope-lock | yes | Locked 2026-05-24T13:25:37Z |
| subagent-driven-development | yes | Single implementer agent dispatched for Tasks 1–5 |
| finishing-a-development-branch | no | PR opened directly; finishing-a-development-branch skill not invoked. No gate miss — the PR description and scope were fully handled inline |
| pr-monitoring | yes | This session |
| post-merge-retrospective | yes | This document |

`finishing-a-development-branch` was the one gate not fired. Given the PR was manually pushed and opened by the implementer sub-agent directly, not via a finishing session, the step was absorbed into the implementation. No downstream issue resulted.

## What worked

- **Design adversarial caught 2 Critical on cycle 1** — wrong proto namespace and duplicate helper without cite. Both would have caused either a hard compile error (wrong namespace) or a runtime duplicate-symbol error (same-named function in `package main`). Caught before a single line of production code was written.
- **Plan adversarial caught 2 more Criticals** — the `iac-missing-service` fixture's false-PASS scenario (embed semantics) and the bash smoke-test `$?` pipeline bug. Both would have shipped silently: tests would pass locally while being logically wrong.
- **Cycle-4 design replan against ACTUAL shipped code** — the design was revised after #765 shipped using a different code path than cycle-3 had hypothesized. The cycle-4 verification step (`git grep -n GetContractRegistry`) audited all 4 existing consumers and confirmed rebinding was safe. This prevented shipping code based on a stale codebase snapshot.
- **ADR 0042 + proto-derived prefix** — both filter sites (SDK bridge + wfctl client) derive the namespace from the same proto descriptor constant, making the filter impossible to drift from the .proto package declaration. A design pattern worth reapplying to any future namespace-filter work.

## What didn't

- **Adversarial review reports not committed as files** — the in-progress.jsonl records skill invocations but the actual finding tables from each adversarial review cycle were not persisted to `docs/plans/`. This retro had to reconstruct findings from the design doc's inline revision history. The pattern of prior PRs (e.g., `2026-05-10-strict-contracts-force-cutover-design.adversarial-review-1.md`) commits each adversarial report as a standalone file. For this PR, those files are absent. Future sessions should ensure adversarial-design-review and adversarial-plan-review output files land in `docs/plans/` as `<base>.adversarial-review-N.md` before the next gate proceeds.
- **Copilot reviewer settled window (10 min) elapsed with zero review comments** — consistent with Copilot not reviewing this PR in the session window. No correction needed for this PR, but it's a reminder that the Copilot reviewer gate provides no coverage on PRs where it doesn't engage within the settle window.

## Plugin-level follow-ups

**Adversarial review report files not persisted**: this is the second PR in the sequence (after #769 verify-capabilities) where adversarial findings are recorded inside the design doc rather than as separate committed files. If this recurs across a third PR, the `adversarial-design-review` skill invocation step should explicitly require committing a `<base>.adversarial-review-N.md` file before returning. This is a process gap, not a finding-quality gap — the findings were sound, just not persisted in the canonical location. Prior retros on strict-contracts and plugin-conformance-compat have these files; the gap appeared in the Layer 3 extension PRs where iteration speed was prioritized.
