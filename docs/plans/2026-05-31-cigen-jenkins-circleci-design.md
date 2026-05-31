# cigen: config-derived Jenkins + CircleCI — Design

**Status:** Approved (autonomous — user pre-authorized full-pipeline execution)
**Date:** 2026-05-31
**Issue:** https://github.com/GoCodeAlone/workflow/issues/804
**Repos touched:** workflow (cigen + wfctl), workflow-plugin-ci-generator (rewire), workflow-scenarios (proof)
**Adversarial review:** cycle 1 = FAIL (1C/3I/3m, all resolved); cycle 2 = FAIL
(1C/2I/1m new, all resolved this revision): C2 Jenkins Multibranch requirement +
header comment + accepted standard-pipeline consequence; I4/I5 precise PR2 test
rewrite (replace `_TemplateUnchanged` with `_CigenMarkers`, delete
`staticGenerator`/`registerTestGenerator`, unit-test `validateRelativeOutputPath`
directly, integration_test.go drives `ExecuteCIGenerate` for jenkins+circleci);
m4 Jenkins-no-PR-comment recorded in Non-Goals. cycle 3 = PASS (converged; lone
Minor — integration_test.go mock→real ExecuteCIGenerate — applied).

## Problem

`workflow/cigen` does config-derived `analyze → CIPlan → render` for **GitHub
Actions** and **GitLab CI** only. Jenkins and CircleCI are still served by
**legacy text/template generators** that ignore the CIPlan entirely:

- In `workflow-plugin-ci-generator/internal/platforms/{jenkins,circleci}.go`,
  the templates hardcode `go test ./...`, `go build ./...`, `docker build/push`,
  `wfctl deploy --image $REGISTRY_IMAGE` — none of which is derived from the
  app's secrets union, phases, migrations, smoke, or plugin-install needs.
- `wfctl ci generate --platform jenkins|circleci` is **unsupported** today
  (`cmd/wfctl/ci.go` only switches on `github_actions`/`gitlab_ci`; the wizard's
  `platformOptions` lists only those two).

So the same CIPlan that produces correct GHA/GitLab output cannot produce
Jenkins/CircleCI output, and two of four platforms emit non-config-derived CI.

## Goals

1. `cigen.RenderJenkins(*CIPlan)` + `cigen.RenderCircleCI(*CIPlan)` — mechanical
   emitters from the **existing** CIPlan, structurally mirroring the GHA/GitLab
   renderers (plan / per-phase apply with secret env + plan-guard + migrations /
   smoke). No `Analyze` change; no new CIPlan fields.
2. `wfctl ci generate --platform jenkins|circleci` routes through cigen (render
   switch + wizard `platformOptions` + usage text).
3. `workflow-plugin-ci-generator` routes `step.ci_generate` jenkins/circleci
   through cigen (extend the existing GHA/GitLab cigen branch) and **removes the
   legacy template generators**.
4. Golden/structural tests for both renderers (parity with `render_gha_test.go`
   / `render_gitlab_test.go`).
5. **Proof in workflow-scenarios**: a scenario that *runs* `wfctl ci generate
   --platform jenkins` and `--platform circleci` against a real config and
   asserts the output is config-derived (secret env wiring, `wfctl migrations
   up`, smoke, plan-guard) and free of the legacy hardcoded stages.

## Non-Goals

- No `Analyze` change, no new CIPlan field, no docker-build/image/deploy stage.
  **The config-derived renderers emit the same job set as GHA/GitLab — plan /
  apply / smoke — which deliberately does NOT include `docker build`/`wfctl
  deploy`.** Those legacy stages are exactly the non-config-derived surface this
  issue retires. (CIPlan.Build exists but is unused by all renderers today; that
  stays true — see ADR.)
- No change to GHA/GitLab renderers' output.
- No new wfctl `ci` subcommands; `--from-plan`, `--diff`, `--write` all work
  unchanged for the new platforms because they sit above the render switch.
- No live Jenkins/CircleCI execution in the proof (no Jenkins server); the proof
  is generate-and-assert-output, same posture as scenario 77.
- No Jenkins PR-comment of the plan (GHA-only feature; Jenkins has no native
  equivalent — plan goes to the build log). Accepted delta, not a regression.
- No fix for the GitLab renderer's pre-existing missing plan-guard/scoped-secrets
  (tracked as a Follow-up; #804 is jenkins/circleci-scoped).

## Global Design Guidance

`Guidance: no docs/design-guidance.md in workflow; canon = CLAUDE.md (cigen
conventions, "config-derived" principle) + the cigen renderer precedent
(render_gha.go / render_gitlab.go) + ADR series in decisions/.`

| guidance | design response |
|---|---|
| Config-derived over templated | Both new renderers read only CIPlan; zero hardcoded app commands. |
| Mirror existing renderer precedent | Job structure, secret-scoping (`phase.Scoped`), plan-guard, `migrationsUpCommand` are reused verbatim from the GHA/GitLab renderers — no new patterns. |
| `wfctl migrations up` is the real runner | Both renderers call the shared `migrationsUpCommand` helper (already correct: `wfctl migrations up … --format json`, never `wfctl ci run --phase migrate`). |
| DRY across renderers | Shared helpers (`migrationsUpCommand`, secret-source branching on `phase.Scoped`) are reused, not re-implemented per platform. |
| Record non-trivial trade-offs | ADR: "config-derived renderers omit the legacy docker-build/deploy stages." |

## Approach Options

| option | summary | trade-off |
|---|---|---|
| **Recommended: mirror GHA/GitLab job set, drop legacy docker stages** | Jenkins declarative pipeline + CircleCI 2.1, emitting plan/apply/smoke from CIPlan, secret env wiring per platform idiom. | Smallest, most consistent; all four platforms render the same logical plan. Behavior change for anyone who relied on the legacy Jenkins `docker build`/`wfctl deploy` — but those were never config-derived and are the issue's explicit target. |
| Keep a docker-build stage in Jenkins/CircleCI (gated on `CIPlan.Build`) | Preserve the legacy build/deploy behavior, config-gate it. | Requires teaching the GHA/GitLab renderers the same (for parity) OR diverging the four renderers — both out of scope and contradict "no Analyze change / mechanical emit". Rejected. |
| Two separate cross-repo features (renderers now, plugin later) | Ship cigen renderers + wfctl first, defer plugin rewire. | The issue's acceptance #2 explicitly requires the plugin rewire + template removal; deferring leaves the issue half-done. Rejected — done as one cascade. |

## Design

### Renderer output mapping (CIPlan → platform)

**Authoritative precedent is `render_gha.go`**, NOT GitLab. The GHA renderer is
the only existing renderer that implements **plan-guard** (`writeApplyJob` lines
202–211) and **per-phase secret scoping** (branch on `phase.Scoped`, line 181).
`render_gitlab.go` omits both — that is a pre-existing GitLab gap (see Backport /
Follow-up below), not a precedent to copy. Both new renderers implement the full
GHA feature set: plan / per-phase apply (scoped secrets + plan-guard + last-phase
migrations) / smoke.

Logical job/stage set (same as GHA):

- **plan** (one per phase): `wfctl infra plan --config <phase.ConfigPath>`,
  gated on PR / merge-request / `changeRequest()`.
- **apply** (one per phase):
  - secret env sourced by branching on `phase.Scoped` (NOT `len`): scoped phase
    uses `phase.Secrets`; unscoped falls back to `p.Secrets`.
  - **plan-guard** (when `p.PlanGuard`): `wfctl infra plan … | tee`, grep for
    replace/destroy, `exit 1` — no `|| true`. (Carried from GHA.)
  - **migrations** (last phase only, when `p.Migrations != nil`): the shared
    `migrationsUpCommand(configPath, p.Migrations.Env)`.
  - `wfctl infra apply --config <phase.ConfigPath> --auto-approve`.
  - apply gated to the default branch (+ manual dispatch where supported).
- **smoke** (when `p.Smoke != nil`): `curl --fail --max-time 30 <Smoke.URL>`,
  ordered after the last apply.
- **plugin install** (when `p.PluginInstall`): `wfctl plugin install --config
  <phase.ConfigPath>` before plan/apply.
- wfctl is installed/pinned per platform idiom using `p.WfctlVersion`.

#### Jenkins declarative structure (the real structural difference — I1)

A declarative Jenkinsfile has a **single linear `stages {}` block** — there is no
GHA/GitLab independent-job graph and no `needs:`. The PR-vs-push split and
multi-phase ordering map onto **sequential stages gated by `when`**, with
**per-stage `environment {}`** for per-phase secret scoping:

```
// Generated by wfctl ci generate. Requires a Jenkins Multibranch Pipeline job:
//   plan stages gate on changeRequest() (PR branches); apply/smoke gate on the
//   default branch. In a standard single-branch job, changeRequest() is always
//   false and plan stages will not run.
// Required Jenkins credentials: SECRET_ONE, SECRET_TWO, APP_DB_URL
pipeline {
  agent { label 'linux' }
  stages {
    stage('Plan <phase>')  { when { changeRequest() } steps { sh 'wfctl infra plan --config <cfg> ...' } }   // one per phase
    stage('Apply <phase>') {                                                                                  // one per phase, in order
      when { branch '<default>' }
      environment { SECRET = credentials('SECRET'); ... }   // per-phase scoped secrets
      steps {
        sh '<plan-guard grep, exit 1>'                       // when PlanGuard
        sh '<migrationsUpCommand>'                           // last phase only, when Migrations
        sh 'wfctl infra apply --config <cfg> --auto-approve'
      }
    }
    stage('Smoke') { when { branch '<default>' } steps { sh "curl --fail --max-time 30 '<url>'" } }            // when Smoke
  }
}
```

**Multibranch requirement (C2):** this structure is correct **only in a Jenkins
Multibranch Pipeline** — `changeRequest()` is true on PR branches and false on the
default branch (plan runs on PRs, apply runs on main). In a standard single-branch
job pointed at main, `changeRequest()` is always false and plan stages silently
no-op. The renderer therefore emits the `// Requires a Jenkins Multibranch
Pipeline` header comment above (mirroring the legacy template's same
`changeRequest()` assumption, now made explicit). Manual "Build Now" on the
Multibranch main branch satisfies `branch '<default>'`, so no separate dispatch
gate is needed. Multi-phase chaining is **implicit stage ordering** (prereq apply
stage precedes deploy apply stage); no `needs` keyword exists or is needed. Each
apply stage's own `environment {}` gives the per-phase secret scope GHA gets via
per-job `env:`.

**Lost feature vs GHA (m4):** GHA's plan job posts the plan as a PR comment
(`actions/github-script`). Jenkins has no native equivalent; the Jenkins plan
output goes to the build log only. This is an accepted delta (recorded in
Non-Goals), not a regression of config-derived behavior.

#### CircleCI structure

CircleCI 2.1 DOES have independent jobs + a `workflows:` graph with `requires:`
(closest to GHA). plan/apply/smoke are jobs; the `workflows` block orders them
with `requires:` and `filters.branches`. Multi-phase = `apply-prereq` →
`apply-deploy` via `requires:`.

**Platform-specific secret idiom** (the real per-platform difference):

- **Jenkins** (declarative): `environment { NAME = credentials('NAME') }` inside
  each apply stage. `credentials('NAME')` binds a credential **pre-created in the
  Jenkins credential store with id `NAME`** — unlike GHA `${{secrets.NAME}}` /
  GitLab auto-injected vars, an absent credential fails the build at runtime. To
  surface this operator precondition (I3), the Jenkins renderer emits a header
  comment `// Required Jenkins credentials: NAME1, NAME2, ...` listing every
  secret the file binds, plus the existing `Warnings` for non-`^[A-Z0-9_]+$`
  names. No plaintext secret value is ever written.
- **CircleCI**: project-level env vars are auto-injected into every job (like
  GitLab), so the renderer does **not** re-declare `NAME: $NAME` no-ops; it only
  references them. (Mirrors `render_gitlab.go`'s `NoRedundantSecretVars` rule.
  CircleCI *contexts* are opt-in and orthogonal — a user adds `context:` manually
  if needed, same as GitLab.)

### New/changed files

**PR1 — workflow** (`feat/cigen-jenkins-circleci-804`):
- `cigen/render_jenkins.go` — `RenderJenkins(*CIPlan) (map[string]string, error)`
  → `{"Jenkinsfile": …}`.
- `cigen/render_circleci.go` — `RenderCircleCI(*CIPlan) (map[string]string,
  error)` → `{".circleci/config.yml": …}`.
- `cigen/render_jenkins_test.go`, `cigen/render_circleci_test.go` — reuse the
  shared `richCIPlan()` helper; assert:
  - **Jenkins**: structural greps — `pipeline {`, per-phase `stage('Apply …')`,
    `environment {`, `credentials('APP_DB_URL')` (and other secret names), the
    `// Required Jenkins credentials:` header lists every secret, `wfctl
    migrations up`, plan-guard grep + `exit 1`, smoke `curl`, two-phase ordering
    (Apply prereq stage appears before Apply deploy stage), nil-plan error, a
    **single-phase** plan renders without panic.
  - **CircleCI**: `yaml.Unmarshal` succeeds AND structural assertions —
    `version: 2.1`, `workflows:` present, job names under workflows include plan
    + apply variants, `requires:` references match job names (NOT GHA `needs:`),
    no redundant `NAME: $NAME` secret re-declares, `wfctl migrations up`, smoke,
    plan-guard, two-phase `requires:` chain, nil-plan error, single-phase case.
  - **Both**: **absence** of legacy `go test ./...` / `wfctl deploy --image` /
    `docker build` / `docker push` (proves the docker-stage drop, ADR 0044).
- `cmd/wfctl/ci.go` — add `case "jenkins"` / `case "circleci"` to the render
  switch (line ~160) AND to the legacy `generateCIFiles` switch (line ~288);
  update BOTH the usage text (lines ~52/73/104) and the two
  `"unsupported platform %q (supported: github_actions, gitlab_ci)"` error
  strings (lines ~166, ~294) to list all four platforms.
- `cmd/wfctl/ci_wizard.go` — add `jenkins`, `circleci` to `platformOptions`.
- `DOCUMENTATION.md` / `docs/WFCTL.md` — note four-platform support.
- Version bump → **v0.68.0** (minor: new public renderers + CLI platforms).

**PR2 — workflow-plugin-ci-generator** (`feat/cigen-jenkins-circleci-804`):
- `go.mod` — bump `github.com/GoCodeAlone/workflow` v0.67.0 → **v0.68.0**; `go
  mod tidy`.
- `internal/generator.go` — extend the cigen branch to all four platforms
  (`case PlatformGitHubActions, PlatformGitLabCI, PlatformJenkins,
  PlatformCircleCI:` with a 4-way render switch); delete the `registry` map, the
  `Generator` interface, and the legacy `default:` branch.
- **Delete the entire `internal/platforms/` package** — after rewire all four
  constructors are unused. `github_actions.go`/`gitlab_ci.go` are no longer called
  from the production path (`generator.go` routes GHA/GitLab through cigen since
  #18); their own `*_test.go` files are the only remaining referees. PR2 deletes
  all four generators **and their tests** — this is intentional (retiring all four
  template generators, not only jenkins/circleci), a slightly broader-than-#804
  cleanup justified because leaving two dead files + dead tests is worse.
- **Test rewrite (I4/I5) — explicit, because PR2 deletes `registry`/`Generator`/
  `platforms.Options`:**
  - `internal/generator_test.go`:
    - **Replace** `TestExecuteCIGenerateJenkins_TemplateUnchanged` (line ~267) and
      `TestExecuteCIGenerateCircleCI_TemplateUnchanged` (~306) with
      `..._CigenMarkers` tests mirroring the existing GHA/GitLab marker tests
      (~66/~127): assert the written file is config-derived (secret wiring,
      `wfctl migrations up`, smoke, plan-guard) and free of `go test`/`wfctl
      deploy --image`.
    - **Delete** the `staticGenerator` struct (~411) and `registerTestGenerator`
      helper (~421) — they use the deleted `registry`/`Generator`/
      `platforms.Options`. `TestExecuteCIGenerateRejectsUnsafeGeneratedPath`
      (~356) and `TestExecuteCIGenerateSortsFilesWritten` (~377) used them to
      inject paths via the registry seam; rewrite both to test the surviving
      package functions **directly**: a focused unit test of
      `validateRelativeOutputPath` (preserves the path-safety guarantee without
      the registry seam) and a sort assertion over a real multi-file cigen render
      (preserves the file-ordering guarantee).
  - `integration_test.go`: this is the **plugin-path proof for acceptance #2** —
    it must drive `ExecuteCIGenerate` end-to-end. NOTE: the current
    `TestIntegration_CIGenerateWithInput` (circleci, line ~140) uses
    `wftest.MockStep`, which cannot write/assert real file content — PR2 must
    **replace the mock with a direct `ExecuteCIGenerate` call** that writes to a
    temp dir, and add the jenkins equivalent, so both assert the written
    `Jenkinsfile` / `.circleci/config.yml` are config-derived (secret wiring,
    `wfctl migrations up`, smoke, plan-guard) and free of legacy `go test`/`wfctl
    deploy --image`.
- Version bump plugin → **v0.2.0** (behavior change: jenkins/circleci now
  config-derived).

**PR3 — workflow-scenarios** (`feat/cigen-jenkins-circleci-proof-804`):
- `scenarios/97-ci-generate-jenkins-circleci/` — `scenario.yaml`, `README.md`,
  `config/app.yaml` (real config: secrets, `ci.migrations`,
  `infra.container_service` with health_check+PRIMARY domain → smoke, a
  `protected: true` module → plan-guard, an `infra.*`/`iac.*` module →
  plugin-install), `test/run.sh`.
- `test/run.sh`: locate wfctl (`WFCTL_BIN`/`$WORKFLOW_REPO/bin/wfctl`); run
  `wfctl ci generate --platform jenkins --config <cfg> --output-dir <tmp>
  --write` and `--platform circleci`; assert the generated `Jenkinsfile` and
  `.circleci/config.yml` each contain: a config secret name, `wfctl migrations
  up`, the smoke URL, plan-guard grep, `wfctl infra apply`; and do NOT contain
  `go test ./...` / `wfctl deploy --image`. Additionally run `wfctl validate` on
  a small `step.ci_generate` config with `platform: jenkins` and `circleci` to
  prove the plugin-step config shape is accepted (config-shape half of acceptance
  #2; the behavior half is PR2's `integration_test.go`). Skip cleanly if wfctl
  absent.
- Register in `scenarios.json` (next free id **97**).

### Cross-repo sequencing (hard dependency)

PR2 imports `cigen.RenderJenkins`/`RenderCircleCI`, which exist only after PR1
merges **and** workflow is tagged v0.68.0. So:

1. PR1 merges to workflow/main → tag **v0.68.0**.
2. PR2 bumps the plugin's go.mod to v0.68.0 (now resolvable) → merges → plugin
   v0.2.0.
3. PR3 builds wfctl from workflow main (post-PR1) → scenario passes in CI.

For **honest local proof now** (demonstration-fidelity): build wfctl from the
PR1 branch and run scenario 97 against it (`WFCTL_BIN=<that binary>`), capturing
the real generated Jenkinsfile/CircleCI output as evidence in the PR. The proof
executes the real `wfctl ci generate`, not a reimplementation.

## Security Review

- **Secrets:** the renderers emit secret *references*, never values. Jenkins uses
  `credentials('NAME')` (Jenkins credential store), CircleCI references
  auto-injected project env vars — neither writes a secret value into the file.
  Same posture as GHA (`${{ secrets.NAME }}`) / GitLab (auto-injected).
- **Plan-guard carried from GHA:** the destructive-plan guard (`exit 1`, no
  `|| true`) is implemented in both new renderers, modeled on the **GHA**
  renderer (the GitLab renderer lacks it — see Follow-up). A protected resource
  still blocks apply in Jenkins/CircleCI output.
- **GitLab plan-guard gap (pre-existing, out of scope):** `render_gitlab.go`
  emits no plan-guard and no scoped-secret branch. This PR does **not** fix that
  (it is #804-orthogonal) but records it as a follow-up so GitLab reaches parity
  later. The new renderers do NOT inherit GitLab's gap.
- **No new network/exec surface:** renderers are pure string builders; the plugin
  path already writes files under a validated relative output dir
  (`validateRelativeOutputPath`).
- **Path safety:** config paths come from CIPlan (already relativized by Analyze
  / aliased by the plugin); the renderers embed them verbatim like GHA/GitLab.

## Infrastructure Impact

None at deploy time. This generates CI config files; it does not create/destroy
cloud resources. The *generated* pipelines run `wfctl infra apply` — but that is
the user's pipeline, unchanged in intent from the GHA/GitLab output. Version
pins (workflow v0.68.0; plugin v0.2.0) are runtime-component bumps → version-skew
audit + rollback notes apply (see Rollback).

## Multi-Component Validation

- **cigen ↔ wfctl:** `wfctl ci generate --platform jenkins|circleci` exercises
  the real Analyze→Render path end-to-end (PR1 golden tests + PR3 scenario run a
  real binary, not a mock).
- **cigen ↔ plugin:** `step.ci_generate` for jenkins/circleci routes through the
  same `cigen.Render*`; PR2 integration test asserts config-derived output from
  the plugin entry point.
- **Real boundary in the proof:** PR3 runs the built wfctl and asserts on the
  actual emitted files (existence + behavior), not on the config — the
  Existence/runtime-validity discipline (autodev #55) applied here.
- **Acceptance-coverage split (I2):** acceptance #1 (`wfctl ci generate
  --platform jenkins|circleci`, CLI) is proven by PR1 golden tests + the PR3
  scenario behavior run. Acceptance #2 (`step.ci_generate` plugin route) is
  proven by **PR2's `integration_test.go`**, which drives `ExecuteCIGenerate`
  (the real plugin entry point) for both platforms and asserts config-derived
  output — a distinct code path from the CLI. The PR3 scenario additionally
  includes a `wfctl validate` check that a `step.ci_generate` config with
  `platform: jenkins|circleci` is accepted (config-shape proof, like scenario
  77). No acceptance criterion is left unproven.

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | Dropping the legacy docker-build/deploy stages is acceptable | A Jenkins/CircleCI user may rely on the old build/deploy | Documented behavior change + plugin minor bump v0.2.0 + ADR; GHA/GitLab never emitted these, so this is parity, not loss of a config-derived feature. |
| A2 | `credentials('NAME')` is the right Jenkins secret idiom for arbitrary secret names | Unlike GHA/GitLab, `credentials('NAME')` requires a Jenkins credential **pre-created with id `NAME`**; an absent one fails the build at runtime (harder to debug) | Emit string credentials by default (matches env-var usage); the Jenkins renderer emits a `// Required Jenkins credentials: …` header listing every bound secret so the operator knows what to pre-create; existing `Warnings` for non-`^[A-Z0-9_]+$` names retained. |
| A3 | workflow can be tagged v0.68.0 before the plugin bump | Tag/release cadence | PR1 merge + tag is a prerequisite gate for PR2; sequenced explicitly. |
| A4 | `github_actions.go`/`gitlab_ci.go` in the plugin are no longer on the production path | They might be referenced indirectly | Verified: `generator.go` registry holds only jenkins/circleci; GHA/GitLab route through cigen since #18; only their own `*_test.go` reference the dead constructors. Whole-package deletion (incl. those tests) is intentional. |
| A5 | CircleCI auto-injects project env vars into jobs (like GitLab) | Contexts vs project env differences | Reference-only (no redeclare) is the safe subset; if a user uses CircleCI contexts they add `context:` manually — same as GitLab's model. |

## Rollback

- **PR1 (workflow):** revert the PR; `RenderJenkins`/`RenderCircleCI` are
  additive (new files + additive switch cases) — reverting restores three-of-four
  platform support with no migration. Do not tag v0.68.0 if reverted.
- **PR2 (plugin):** revert restores the template generators and the v0.67.0 pin.
  Because PR2 deletes the `platforms` package, rollback = `git revert` (restores
  the files). Rollback note per task.
- **PR3 (scenarios):** revert removes scenario 97; no runtime impact.
- **Version pins:** workflow v0.68.0 / plugin v0.2.0 — to roll back, pin plugin
  to v0.1.6 + workflow consumers to v0.67.0 and rebuild. Version-skew audit at
  finish: plugin's workflow pin must equal the freshly tagged v0.68.0 (no lag).

## Backport (2026-05-31, post-execution — manifest scope unchanged)

Three existence-check discoveries at execution (all the `Existence /
runtime-validity` class from autodev #55; no manifest scope change):
- **Scenario id 97 → 100.** Plan guessed "next free id 97"; 97–99 were already
  taken. Used actual next-free 100.
- **Scenario config `app.yaml` → `deploy.yaml`.** The scenarios CI strict-`wfctl
  validate`s every `config/app.yaml`; an infra/cigen-input config has no app
  entry point and fails. Renamed to `config/deploy.yaml` (matches `wfctl ci
  generate -c deploy.yaml`; excluded from the app-validate glob, like scenarios
  88/93). The local run.sh only ran `ci generate` (cigen analyze, no entry-point
  requirement), so it didn't catch this.
- **Plugin version 0.2.0 → 0.3.0.** Plan's Task 7 read plugin.json (0.1.6) and
  bumped to 0.2.0, but `v0.2.0` was already a released tag (PR #18; plugin.json
  had drifted below it). Bumped to v0.3.0 (PR #22). Lesson: check `git
  ls-remote --tags`, not just the version file, before choosing a release version.

## Follow-ups (out of scope for #804)

- **GitLab plan-guard + scoped-secret parity:** `render_gitlab.go` lacks both the
  destructive-plan guard and the `phase.Scoped` secret branch that GHA (and now
  Jenkins/CircleCI) implement. File a follow-up issue to bring GitLab to parity.
  Not fixed here to keep #804 scoped to jenkins/circleci.

## Self-Challenge

- **Simplest alternative:** just add the 2 renderers + wfctl switch, skip the
  plugin rewire. Rejected — issue acceptance #2 mandates the plugin rewire +
  template removal; skipping leaves the issue half-done.
- **Most fragile assumption:** A1 (dropping docker stages). Mitigated by ADR +
  the fact that config-derived parity with GHA/GitLab is the explicit ask.
- **YAGNI sweep:** no new CIPlan field, no docker stage, no new subcommand, no
  per-platform Analyze branch — all rejected as surface the issue didn't ask for.
