# Retro: Issue #653 Phase 2 — Strip AWS SDK from codebuild + EKS backends

**PR:** [#659](https://github.com/GoCodeAlone/workflow/pull/659) — feat(#653): Phase 2 — strip AWS SDK from codebuild + EKS backends
**Merged:** 2026-05-13 (sha `62e22870`) by @intel352
**Branch:** `feat/issue-653-phase2-aws-operational`
**Design:** `docs/plans/2026-05-13-issue-653-phase2-aws-operational-design.md`
**Plan:** `docs/plans/2026-05-13-issue-653-phase2-aws-operational.md`
**Related ADRs:** none new
**Prior retros:** `docs/retros/2026-05-13-issue-617-godo-removal-retro.md`, `docs/retros/2026-05-13-issue-653-aws-iac-removal-retro.md`

---

## Adversarial-review findings, scored

Adversarial-review cycles for this PR were inline in the session, captured in the design and plan commit history (not as separate report files).

### Design phase (2 cycles to PASS)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design | Error backend struct shape not specified (would require implementer to guess) | Critical | **Resolved upfront** — design enumerated exact method signatures for `codebuildAWSErrorBackend` and `eksErrorBackend` |
| design | `aws.*` helper usage in non-EKS backends (gke/aks) not confirmed; risk of breaking gkeBackend by removing import | Critical | **Resolved upfront** — design grepped per-backend and confirmed `aws.*` and `errors.As` were used only by eksBackend |
| design | `legacyaws` scope wrong in Files Changed (already shipped in Phase 1) | Important | **Resolved upfront** — design corrected to remove legacyaws from Files Changed; package is consumed, not added |
| design | Test audit gap — no statement on how existing eksBackend tests would behave | Important | **Resolved upfront** — design called out `TestPlatformKubernetes_EKSStubPlan` and `_EKSApplyNotImplemented` would need replacement |
| design | YAGNI on plugin-version reference (claimed v0.X.Y for workflow-plugin-aws without verifying) | Minor | **Resolved upfront** — design changed to "Phase 3, TBD" |

### Plan phase (2 cycles to PASS)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| plan | Serial dependency T3→T1+T2 not explicit (T3 step 1 verifies T1+T2 grep returns zero) | Critical | **Resolved upfront** — plan called out SERIAL DEPENDENCY block at top of T3 |
| plan | T1 Step 2 fail-mode description incorrect (would have produced wrong-shaped test) | Critical | **Resolved upfront** — plan corrected expected initial-test-run behavior |
| plan | `errors` import fate implicit (would the implementer remove it?) | Important | **Resolved upfront** — plan explicitly stated "Remove `errors` — confirmed: used only by eksBackend for errors.As" |
| plan | PR base pointing to merged Phase 1 branch instead of main | Important | **Resolved upfront** — plan changed base to `origin/main` |
| plan | Rollback notes missing on T1+T2 commit instructions | Important | **Resolved upfront** — plan added "Rollback: git revert <sha>; go mod tidy to restore SDK imports" to both |
| plan | Typo `.claire` → `.claude` in working dir path | Minor | **Resolved upfront** — corrected in plan revision |

---

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Test called `m.CreateProject()` twice — first call discarded just for the nil-check, then again to capture `.Error()`. Wasteful and reads awkwardly. (Copilot R1 on commit `9afc9560`) | adversarial-design-review (plan) | The plan listed the exact test body with the double-call pattern; reviewers OKed it without flagging the redundancy. The pattern was copied from session memory of an earlier test sketch and the plan's adversarial cycle focused on assertion correctness, not call efficiency. | Add to plan-phase checklist: "if a test calls a method twice on the same object, verify the first call's return is genuinely discarded — capture-once-and-reuse is almost always cleaner." |
| `go.mod` grep gate omitted `service/eks` (intentional, but with no inline comment); a future cleanup that removes provider/aws/ legitimate callers would leave a stale direct dep with no automated catch. (Copilot R1 on commit `ad74bab3`) | adversarial-design-review (plan) | The plan asymmetrically scoped go-file gate vs go.mod gate (the latter only adds `codebuild` because `eks` legitimately stays). Plan reviewers approved the asymmetry but did not require an inline CI-yaml comment to record the rationale, so a future maintainer would see it as a typo. | Add to plan-phase checklist: "when a CI gate intentionally omits a value that a sibling gate includes, require an inline YAML comment explaining the asymmetry." |
| Stale blank lines left in `schema/schema.go` `coreModuleTypes` slice after `step.network_*` and `step.scaling_*` deletions (Phase 1 holdover surfaced in Phase 2 diff). (Copilot R2 on commit `ad74bab3`) | adversarial-design-review (Phase 1 plan) | This was a Phase 1 deletion site, not touched by Phase 2 directly. Copilot only flagged it on PR #659 because the merge-main commit `0325df6c` widened the visible diff. The Phase 1 retro did not catch it because the Phase 1 PR diff was self-consistent and Copilot reviewed Phase 1 separately. | When deletions are split across phases, add to plan-phase checklist: "for each registered-types slice with removed entries (coreModuleTypes, infraTypes, etc.), re-run `gofmt -d` after deletions to surface incidental blank lines." |
| `parser.ParseFile` errors silently discarded in `aws_absent_test.go` — `f, _ :=` means a syntax error in a source file is invisible to the test. (Copilot R2 on commit `ad74bab3`) | adversarial-design-review (plan) | The test file was carried over from Phase 1 verbatim; Phase 2 only added entries to the `freed` slice. The error-suppression was a Phase 1 carryover not flagged in either Phase 1 or Phase 2 plan review. | When extending an existing test file, plan-phase checklist should include: "audit the existing file's error handling against linter rules even if not modifying those lines — they may have been grandfathered." |
| `eksErrorBackend.status` returns `(nil, error)` while legacy `eksBackend.status` returned `(k.state, error)` — potential nil-pointer panic if a caller used the old contract of "always non-nil state". (Copilot R2 on commit `0325df6c`) | adversarial-design-review (design) | The design specified the error-backend method shape but did not audit each method's return-value contract against the legacy implementation. The (nil, error) return-shape change happens to be safe (verified by Jon: pipeline_step_platform_k8s.go checks err before deref), but the audit was not in the design's review pass. | Add to design-phase checklist: "for each method on a backend being replaced with an error stub, audit what callers do with the non-error return when error is non-nil — preserve any non-nil-on-error contracts the legacy implementation provided." |
| Dead helpers `awsProviderFrom`, `parseStringSlice`, `safeIntToInt32` and unused `math` import left behind after eksBackend deletion. (Inferred from Jon's R2 fix in `90907cdb`) | adversarial-design-review (plan) | Plan T2 specified removing the SDK imports but did not specify auditing helpers in other files that only the deleted backend called. The implementer (this session) only removed direct imports in platform_kubernetes_kind.go itself. | Add to plan-phase checklist: "for backend-removal tasks, grep across `module/` for every helper named or referenced in the deleted code; flag helpers with zero remaining callers for cleanup in the same commit." |
| Code-review comment about wider scope than PR title — diff showed Phase 1 deletion sites because branch was based on pre-Phase-1 main. (Copilot R2 on commit `0325df6c`, resolved by Jon merging main) | finishing-a-development-branch | The plan correctly specified `origin/main` as base, but the worktree branch was created earlier and didn't get fast-forwarded after Phase 1 merged. `finishing-a-development-branch` Step 1d does scope-check against plan, not "is the branch rebased on current main?" | Add to finishing-a-development-branch: "before pushing the PR, verify `git rev-list --left-right --count origin/<base>...HEAD` shows zero left-side commits (branch is rebased), or warn the user." |

---

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | inline — task spec from prior conversation scoped the 4-file audit |
| adversarial-design-review (design) | yes | 2 cycles to PASS, 5 findings resolved upfront |
| writing-plans | yes | plan committed as `0f241c32` |
| adversarial-design-review (plan) | yes | 2 cycles to PASS, 6 findings resolved upfront |
| alignment-check | yes | PASS on first run |
| scope-lock | yes | applied; hash `5b682b574b89...` held through all 4 tasks |
| subagent-driven-development | yes | sequential, 4 tasks, all committed as planned |
| finishing-a-development-branch | yes | Step 1d scope-check PASS; Steps 1b/1c not triggered (CI-only config change, no runtime artifact) |
| pr-monitoring | yes | 1 advisory CI failure (codecov/patch, unprotected branch) + 2 Copilot review rounds with 8 inline findings; merged by user |
| post-merge-retrospective | yes | this document |

No missed activations.

---

## What worked

- **Backend-stub pattern reuse (#617 → #657 → #659).** Three retros in, the `internal/legacyaws.RemovedInVersion` + error-backend-struct pattern is now templated. Time from plan-lock to first green test was the shortest of the three PRs.
- **Scope-lock prevented Phase 2 from sprawling.** Two files were deliberately exempted (`nosql_dynamodb.go` no real SDK import; `pipeline_step_s3_upload.go` no go.mod win). Both exemptions held through execution; no implementer "while I'm here" expansion.
- **Adversarial-design-review caught struct-shape ambiguity before code was written.** The design cycle-1 finding "error backend struct shape not specified" forced the design to enumerate exact method signatures, which made T1+T2 mechanical.
- **CI gate scoped correctly to `module/`.** Recognizing that `provider/aws/` and `platform/providers/aws/drivers/` are legitimate IaC importers of `service/eks` prevented a false positive on the banned-packages CI gate. The first gate-script iteration tried `--exclude-dir=platform --exclude-dir=provider` which silently failed (grep `--exclude-dir` matches basename not prefix); switching to `module/` scoping was the right fix.

---

## What didn't

- **Worktree branch not rebased on main before push.** PR #659 was branched from a pre-Phase-1 base; when Copilot reviewed the diff it saw Phase 1 deletion sites and flagged the apparent scope mismatch. Jon merged main into the branch (`0325df6c`) to resolve. The mechanical fix is trivial, but the gate ought to catch it.
- **Test-pattern double-call.** A small but visible code-smell in the migration test (`m.CreateProject()` called twice). The plan specified the exact test body with this pattern; plan reviewers approved the assertion semantics without flagging the redundancy.
- **CI-gate asymmetry undocumented.** `service/eks` was intentionally omitted from the go.mod grep gate (still has legitimate importers); without an inline comment, a future maintainer reads it as a typo. Jon added the explanatory comment in `ad74bab3`.
- **Dead helpers left behind.** `awsProviderFrom`, `parseStringSlice`, `safeIntToInt32`, and `math` import were dead after eksBackend deletion. Jon cleaned them up in `90907cdb`. Plan T2 said "remove SDK imports" but didn't say "audit helpers that were only called by the deleted code."

---

## Plugin-level follow-ups

Pattern is firming across three retros (#617, #657, #659):

1. **Lint / line-hygiene on derived test files (3rd occurrence).** #617 caught `filepath.Glob` coverage; #657 caught `nilerr`; #659 caught silent error discard (`parser.ParseFile`) AND blank-line debris in `coreModuleTypes`. Three different surface forms of the same root cause: **inheriting a test or registry file verbatim and adding entries without auditing the existing file against current linter rules + formatter output.** Recommend extending `adversarial-design-review --phase=plan` checklist with: *"for any task that modifies an existing file with a known precedent pattern (test files, registry slices, CI grep gates), require `gofmt -d <file>` + `golangci-lint run <file>` as named verification steps. Do not assume prior precedent passes current tooling."*

2. **Dead-helper sweep after backend deletion (1st occurrence with clear signal).** #659 surfaced 3 dead helpers + 1 dead import in a single deletion task. Propose adding to plan template for "strip backend X" tasks: *"after removing the backend struct + its methods, grep `module/` for every package-private helper that the removed backend called; flag any helper with zero remaining callers as part of the same task."* Wait for one more retro before promoting to a mandatory checklist item.

3. **Branch-rebased-on-base check at PR creation (1st occurrence with clear cause).** #659's "scope mismatch" Copilot finding was entirely a stale-branch-base artifact. Propose adding to `finishing-a-development-branch` Step 1c or as a new Step 1e: *"verify `git rev-list --left-right --count <base>...HEAD` shows zero left-side commits before pushing the PR; if non-zero, prompt the user to merge or rebase before opening the PR."* This is a cheap mechanical check that prevents an entire class of review noise.

4. **Method-contract preservation in error-backend stubs (1st occurrence with concrete near-miss).** #659's `eksErrorBackend.status` returned `(nil, error)` where the legacy returned `(k.state, error)`. The change happened to be safe (verified at review time), but the design didn't audit return-value contracts. Propose adding to `adversarial-design-review --phase=design` checklist when reviewing error-backend stubs: *"for each method, compare the new error-path return values to the legacy non-nil return values; preserve any callers' reliance on non-nil-state-on-error."*

Items 1 and 3 are ready to ship into the checklist now (pattern strength is sufficient). Items 2 and 4 should wait one more retro for confirmation.
