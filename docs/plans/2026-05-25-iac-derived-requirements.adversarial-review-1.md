### Adversarial Review Report

**Phase:** plan
**Artifact:** `docs/plans/2026-05-25-iac-derived-requirements.md`
**Status:** FAIL

**Findings (Critical):**
- [under-decomposition / hidden serial dependencies] `docs/plans/2026-05-25-iac-derived-requirements.md:17-36,717-787`: The scope manifest claims 5 PRs, but PR 5 spans four provider repositories and Task 10 also includes workflow release, provider version bumps, observability version bumps, and consumer app edits. That is not one independently reviewable PR and it will confuse scope-lock and execution. Recommendation: split PR grouping by repository and make release/version-bump work explicit follow-up PRs/tasks.

**Findings (Important):**
- [user-intent drift / missing failure mode] `docs/plans/2026-05-25-iac-derived-requirements.md:299-355` vs `docs/plans/2026-05-25-iac-derived-requirements-design.md:215-226`: The design includes optional strict-proto external-plugin requirement discovery for config-aware plugins, but the plan only implements an in-process Go interface and static manifest v2. That misses the composable plugin requirement path the user asked for. Recommendation: add a task for `IaCRequirementDiscovery` proto service, SDK registration, contract tests, and wfctl collection.
- [design drift] `docs/plans/2026-05-25-iac-derived-requirements.md:142-170` vs `docs/plans/2026-05-25-iac-derived-requirements-design.md:122-149,186-192`: The plan's proto sketch drops `source`, `resource_type_hint`, `environment`, accepted/rejected diagnostics, and ordered notes that the design says are part of the typed exchange. It replaces rejected diagnostics with plain warnings. Recommendation: model these as strict proto fields/messages unless a specific field has a documented reason to defer.
- [verification-class mismatch] `docs/plans/2026-05-25-iac-derived-requirements.md:536-550`: The Workflow CLI representative invocation uses `--provider digitalocean` before provider plugins implement the mapper. This can fail for reasons unrelated to Workflow CLI correctness. Recommendation: verify Workflow CLI with a fake/in-process mapper fixture first, then add provider-backed smoke tests only after provider mapper PRs.
- [missing rollback wiring / hidden dependencies] `docs/plans/2026-05-25-iac-derived-requirements.md:789-840`: Task 10 says to merge, tag, bump plugin minimums, remove consumer `/metrics` reliance, and commit separately per repo, but it is assigned to the provider mapper PR. This mixes release operations with code changes and has no clear rollback for a partially released Workflow tag. Recommendation: split release, plugin min-version bumps, and consumer migrations into separate tasks with explicit prereqs and no shared PR row.

**Findings (Minor):**
- [repo-precedent conflict] `docs/plans/2026-05-25-iac-derived-requirements.md:597-618`: The editor test command is guessed as `npm test -- serialization.satisfies`; the plan should inspect the repo's package scripts and use the exact Vitest/Jest command. Recommendation: add an exploration step or replace with the verified command after checking `package.json`.
- [verification gap] `docs/plans/2026-05-25-iac-derived-requirements.md:180-188`: The proto generation step uses `go run github.com/bufbuild/buf/cmd/buf@latest`, which is convenient but not pinned. Recommendation: either use existing repo release conventions for Buf or document that this is a tool invocation only and verify generated headers/CI with committed output.

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Assumes provider plugins can be updated and released inside one PR row despite living in separate repos. |
| Repo-precedent conflicts | Finding | The plan's PR manifest is repo-local, but several tasks are cross-repo operations that cannot share one branch/PR. |
| YAGNI violations | Clean | The explicit derive command and provider mapper are justified by the user ask and prior design. |
| Missing failure modes | Finding | Partial release/version-bump failure is not decomposed or rollback-safe. |
| Security / privacy at architecture level | Clean | Secret handling is explicitly tested and rejects plaintext secret-like generated config. |
| Rollback story | Finding | Rollback notes exist per task, but the release/version-bump task has no safe rollback sequence for partially published tags. |
| Simpler alternative not considered | Clean | Manifest-only and apply-time derivation were considered and rejected in the design. |
| User-intent drift | Finding | Missing external-plugin requirement discovery weakens the Go-interface-without-hard-dependency requirement for out-of-process plugins. |
| Over-decomposition / under-decomposition | Finding | Provider and release work is under-decomposed across repos. |
| Verification-class mismatch | Finding | Workflow CLI smoke depends on provider plugin behavior before provider mapper implementation exists. |
| Hidden serial dependencies | Finding | Observability/plugin/provider tasks depend on the Workflow release tag but are grouped as if one PR can contain them. |
| Missing rollback wiring | Finding | Release and consumer migration rollback are not executable steps. |

**Options the author may not have considered:**
1. Split Workflow into two PRs, editor into one PR, observability into one PR, each provider into its own PR, then one release/version-bump wave. This increases PR count but matches actual repository boundaries and keeps failures revertible.
2. Implement only Workflow core plus a fake mapper in the first wave, then release provider mappers incrementally. This makes `wfctl infra derive` testable without blocking on every provider, but it delays real cross-cloud proof until follow-up PRs.

**Verdict reasoning:** The design direction is still sound, but the implementation plan is not executable as written. The largest issue is the scope manifest: it describes one provider PR and one release task where the work actually crosses at least six repositories plus consumer migrations. The strict-proto surface also regressed from the design by omitting dynamic external-plugin requirement discovery and typed mapper diagnostics. Revise the plan before alignment-check or execution.
