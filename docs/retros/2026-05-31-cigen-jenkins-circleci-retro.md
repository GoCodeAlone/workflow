# Retro: cigen config-derived Jenkins + CircleCI (#804)

**Issue:** https://github.com/GoCodeAlone/workflow/issues/804
**Merged:** 2026-05-31
**PRs:** workflow #810 (v0.68.0) · workflow-plugin-ci-generator #21 + #22 (v0.3.0) · workflow-scenarios #47 (scenario 100)
**Design:** docs/plans/2026-05-31-cigen-jenkins-circleci-design.md (adversarial PASS @ cycle 3)
**Plan:** docs/plans/2026-05-31-cigen-jenkins-circleci.md (plan-phase PASS @ cycle 2; alignment PASS; scope locked)
**ADR:** decisions/0044-cigen-renderers-omit-legacy-build-deploy-stages.md

## What shipped

`cigen.RenderJenkins` + `cigen.RenderCircleCI` (config-derived emitters from the
existing CIPlan, mirroring the GHA job set) wired into `wfctl ci generate`
(4 platforms) and the ci-generator plugin (all 4 routed through cigen; the entire
`internal/platforms` template package retired). Proof: workflow-scenarios
scenario 100 runs the real `wfctl ci generate` and asserts config-derived output
(22/22). v0.68.0 / plugin v0.3.0.

## Adversarial-review findings, scored

| Phase | Finding | Sev | Outcome |
|---|---|---|---|
| design c1 | "mirror GitLab" wrong — GitLab lacks plan-guard/scoped-secrets | Critical | Resolved upfront — GHA named authoritative; GitLab gap recorded as follow-up |
| design c1 | Jenkins declarative multi-phase structure undefined | Important | Resolved upfront — concrete `when`/per-stage-`environment{}` sketch added |
| design c1 | proof didn't cover plugin path (acceptance #2) | Important | Resolved upfront — PR2 integration test designated |
| design c1 | Jenkins `credentials('NAME')` operator burden | Important | Resolved upfront — required-credentials header comment |
| design c2 | Jenkins `when{changeRequest()}` only works in a Multibranch job | Critical | Resolved upfront — Multibranch header comment + accepted consequence |
| design c2 | generator_test/integration_test file split + helper deletions | Important×2 | Resolved upfront — precise test-rewrite spec |
| plan c1 | Jenkins credentials test sort-fragility | Critical | Resolved upfront — per-credential assertions added |
| plan c1 | Task 6/7 broken-CI window | Critical | Resolved upfront — "no push until Task 7 green" note |
| plan c1 | release→module-proxy timing before `go get @v0.68.0` | Important | **Prescient** — the I4 poll-gate is exactly what unblocked PR2 (module resolved ~30s post-tag) |
| plan c1 | test-quality gaps (CircleCI secret, Task3 trivial, testdata richness) | Important×3 | Resolved upfront |

Design converged in 3 cycles, plan in 2. Every code PR's two-stage review caught
a real issue (below).

## Gate misses

Two real misses slipped to CI/runtime — both **existence-check** misses, the exact
class autodev #55 (shipped earlier this same session) targets:

| Issue | Gate that missed | Why it slipped | Fix |
|---|---|---|---|
| Scenario `config/app.yaml` failed the scenarios-CI strict `wfctl validate` (no entry point) | plan / local run.sh | The local proof ran only `ci generate` (cigen analyze — no entry-point requirement), never `wfctl validate`; the repo CI validates **every** `config/app.yaml` | Renamed to `config/deploy.yaml` (infra config, correctly excluded from the app-validate glob) |
| plugin `v0.2.0` already released (PR #18) — bump collided with an existing tag | plan (Task 7 guessed `0.1.6→0.2.0` from plugin.json) | Plan read the version *file* (0.1.6) but never checked existing *tags*; plugin.json had drifted below the released tag | Bumped to `v0.3.0` (PR #22) |

Both are the **Existence / runtime-validity** bug-class verbatim: assert the
artifact you mutate/emit actually validates against the real consumer, and that
the tag/artifact you create does not already exist. The class was added to
`adversarial-design-review` in #55 the same day but was **not yet active** for
this design's reviews (it merged into the skill in parallel). Had it been live,
both misses were one `wfctl validate` / one `git ls-remote --tags` at design time.

## What worked

- **The I4 release-availability poll-gate was prescient** — the plan-phase
  adversarial review flagged the tag→module-proxy race; the bash poll-loop
  ([[feedback_ci_wait_use_bash_poll_loop]]) confirmed v0.68.0 on the proxy in
  ~30s and unblocked PR2 cleanly. No "version not found" failure.
- **Two-stage code review earned its keep on every PR**: PR1 → `jenkinsCredentialUnion`
  used by the circleci renderer (renamed `secretUnion`); PR2 → `minEngineVersion`
  still 0.67.0 (a 0.67.0 consumer would fail plugin load) + stale doc comment.
- **Demonstration-fidelity**: scenario 100 ran the real `wfctl ci generate`
  (22 assertions), not a reimplementation; honest evidence committed.
- **Cross-repo sequencing held**: PR1 merge → tag → module proxy → PR2 dep bump →
  PR3, with no premature step.

## Plugin-level follow-ups (autodev)

The two gate misses are a **trend with the autodev #55 retro's own evidence**
(required_secrets sweep + smart-CI gen). Recommendation already shipped: the
`Existence / runtime-validity` bug-class (autodev v6.2.2). This retro is the third
data point — when that class is live in the skill, a design that (a) generates a
config a CI consumer will `validate`, or (b) creates a version tag, must show the
`validate`/`ls-remote` check at design time. No new plugin change needed; the
existing class covers both. (If a *fourth* occurrence appears with the class
live and still missed, escalate to a hard pre-flight in `finishing-a-development-branch`.)

## Project guidance updates

| File | Change | Reason |
|---|---|---|
| (none) | no change | No durable cross-design lesson beyond what ADR 0044 + the #55 bug-class already capture. GitLab plan-guard/scoped-secret parity is filed as a code follow-up, not guidance. |
