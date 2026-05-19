# Retro: Issue #653 Phase 1 — Remove AWS IaC Modules from Workflow Core

**PR:** [#657](https://github.com/GoCodeAlone/workflow/pull/657) — feat(#653): Phase 1 — remove AWS IaC modules from workflow core
**Merged:** 2026-05-13 (sha `1389d024`)
**Branch:** `feat/issue-653-aws-iac-cutover-v2`
**Design:** `docs/plans/2026-05-13-issue-653-phase1-aws-cutover-design.md`
**Plan:** `docs/plans/2026-05-13-issue-653-phase1-aws-cutover.md`
**Related ADRs:** none new (force-cutover pattern established by #617 retro)
**Prior retro:** `docs/retros/2026-05-13-issue-617-godo-removal-retro.md`

---

## Adversarial-review findings, scored

The adversarial-review cycles for this PR were inline in the session (not committed as
separate files). The design leveraged the established `internal/legacydo` pattern directly,
and the plan was written against the verified #617 precedent. Fewer cycles were needed as
a result.

### Design phase

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design | `cloud_account_aws.go` NOT deleted — credential resolver stays | Critical | **Resolved upfront** — design explicitly scoped this out; implementation never touched those files |
| design | `platform.dns` is generic, not AWS-only — only Route53 backend deleted | Critical | **Resolved upfront** — `platform_dns_backends.go` replaced with mock + Route53 migration stub; `platform.dns` module type and `step.dns_*` stayed |
| design | 15 step types need manual rewrite (config shape differs from `step.iac_*`) | Important | **Resolved upfront** — step types marked non-auto-fixable in modernize rule; migration guide documented `platform + state_store` keys |
| design | `infra.autoscaling_group` missing from infra plugin registration | Important | **Resolved upfront** — added to `plugins/infra/plugin.go` `infraTypes` in T3 before any tests ran |

### Plan phase

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| plan | `walkTypeNodes` already exists in `modernize` package — don't re-add | Important | **Resolved upfront** — `legacy_aws_rule.go` calls existing helper, no duplicate |
| plan | `step.network_destroy` never existed — don't include in removal | Minor | **Resolved upfront** — confirmed absent before writing step deletion list |
| plan | `RemovedInVersion` constant must live in `internal/legacyaws` to avoid import cycle | Important | **Resolved upfront** — design cited the exact import-cycle lesson from #617 plan-cycle 4; `internal/legacyaws/types.go` created as leaf package from the start |

---

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| `module/aws_absent_test.go` used `parseErr != nil; return nil` which triggers `nilerr` golangci-lint rule. Lint failed on first CI run. | adversarial-design-review --phase=plan | The plan reviewer and T6 implementer verified the `go/parser` API pattern against the #617 precedent (`godo_absent_test.go`) without running `golangci-lint` locally. The #617 precedent used the same pattern and happened to pass; the `nilerr` linter rule fires when `parseErr != nil` but `nil` is returned. | Add to plan-phase checklist: "for files derived from existing precedents, run golangci-lint on the derived file before marking the task verified — precedent files may have grandfathered lint suppressions or the linter ruleset may have changed." |
| `platform.dns` `ConfigKeys` in `type_registry.go` listed `domain` (stale from pre-Phase 1 state) instead of the actual keys `zone` and `records`. Caught by Copilot post-PR. | adversarial-design-review --phase=plan / code review (pre-PR) | The `platform.dns` config shape was verified at the module level (design correctly identified `zone` and `records`) but the `type_registry.go` ConfigKeys entry was not audited as part of T3/T6. The entry predates this PR and the design audit did not extend to wfctl metadata registry consistency. | Add to plan-phase checklist: "for every module type that is retained (not deleted), verify its `type_registry.go` ConfigKeys entry matches the actual module config struct fields." |

---

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | audit + scope-refinement comment posted on #653 before design was written |
| adversarial-design-review (design) | yes | inline — design doc revised to pin exact scope boundaries before plan was written |
| adversarial-design-review (plan) | yes | inline — plan revised to use `internal/legacyaws` leaf package from the start |
| alignment-check | yes | PASS on first run |
| scope-lock | yes | applied; hash held through all 6 tasks |
| subagent-driven-development | yes | sequential mode, 6 tasks, code review between tasks |
| finishing-a-development-branch | yes | scope-check + test pass + build verified |
| pr-monitoring | yes | 3 CI runs (1 failure fixed — lint; then 2 green runs) + Copilot 2 threads both resolved |
| post-merge-retrospective | yes | this document |

No missed activations.

---

## What worked

- **#617 precedent reuse.** The `internal/legacydo` leaf-package pattern, import-cycle lesson (plan cycle 4 from #617), and `walkTypeNodes` reuse eliminated entire bug classes before a single line was written. Time to first green CI was significantly shorter than #617.
- **Design explicitly called out `platform.dns` generic/AWS split.** The design doc's "Critical architectural finding" section prevented the most likely mistake (deleting the whole DNS module). Zero DNS-related regressions in CI.
- **AWS SDK banned CI gate shipped alongside the deletion.** The `aws-sdk-banned` job passed on first CI run. The regression surface is now machine-enforced.
- **`infra.autoscaling_group` gap caught in design review.** Adding it to `plugins/infra/plugin.go` before execution started meant T3 compiled and tested cleanly without a fix-up commit.

---

## What didn't

- **Lint not run locally before T6 commit.** The `nilerr` failure in `aws_absent_test.go` was the only CI failure. Deriving a test file from a precedent that passes lint does not guarantee the derivative passes lint — the linter ruleset evolves. One extra `golangci-lint run ./...` locally before push would have caught it.
- **wfctl metadata registry not audited against module config shapes.** `type_registry.go` ConfigKeys for `platform.dns` had a stale `domain` key that predated this PR. The design audited module deletion sites but not the metadata registry's per-type accuracy for retained types. Copilot caught it in review — correctly. This is a recurring pattern: Copilot has now surfaced registry/metadata mismatches on two consecutive PRs (#654 modernize step config shapes, #657 ConfigKeys mismatch).

---

## Plugin-level follow-ups

Two gate misses this PR; combined with the two gate misses from #617, a pattern is emerging:

1. **Lint on derived files (2nd occurrence, different lint rule):** #617 retro flagged `filepath.Glob` coverage; this retro flags `nilerr` on error-ignore pattern. Both are "used precedent file without running linter on derivative." Propose adding to `adversarial-design-review --phase=plan` checklist: *"for test files derived from existing precedents, specify `golangci-lint run ./...` as a required verification step in the task, not just `go test`."*

2. **wfctl metadata registry consistency (1st occurrence):** #657 is the first retro flagging ConfigKeys mismatch. Record as signal; wait for one more retro before adding to the adversarial-review checklist. Tentative: if #658+ surfaces another metadata-registry mismatch, add to plan-phase checklist: *"for each module type retained (not deleted), verify its `type_registry.go` and `DOCUMENTATION.md` metadata entries match the module's actual config struct fields."*
