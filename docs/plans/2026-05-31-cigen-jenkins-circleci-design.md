# cigen: config-derived Jenkins + CircleCI — Design

**Status:** Approved (autonomous — user pre-authorized full-pipeline execution)
**Date:** 2026-05-31
**Issue:** https://github.com/GoCodeAlone/workflow/issues/804
**Repos touched:** workflow (cigen + wfctl), workflow-plugin-ci-generator (rewire), workflow-scenarios (proof)

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

Both renderers mirror `render_gha.go` / `render_gitlab.go` exactly:

- **plan job** (one per phase): `wfctl infra plan --config <phase.ConfigPath>`,
  triggered on PR / merge-request.
- **apply job** (one per phase, chained via `needs`/`requires` when multi-phase):
  - secret env block sourced by branching on `phase.Scoped` (NOT `len`): scoped
    phase uses `phase.Secrets`; unscoped falls back to `p.Secrets`.
  - **plan-guard** (when `p.PlanGuard`): `wfctl infra plan … | tee`, grep for
    replace/destroy, `exit 1` — no `|| true`.
  - **migrations** (last phase only, when `p.Migrations != nil`): the shared
    `migrationsUpCommand(configPath, p.Migrations.Env)`.
  - `wfctl infra apply --config <phase.ConfigPath> --auto-approve`.
  - apply runs only on the default branch (+ manual dispatch where the platform
    supports it).
- **smoke job** (when `p.Smoke != nil`): `curl --fail --max-time 30 <Smoke.URL>`,
  needs the last apply.
- **plugin install** (when `p.PluginInstall`): `wfctl plugin install --config
  <phase.ConfigPath>` before plan/apply.
- wfctl is installed/pinned per platform idiom using `p.WfctlVersion`.

**Platform-specific secret idiom** (the one real per-platform difference):

- **Jenkins** (declarative): `environment { NAME = credentials('NAME') }` inside
  the apply stage — the idiomatic Jenkins secret binding. No plaintext.
- **CircleCI**: project-level env vars are auto-injected into every job (like
  GitLab), so the renderer does **not** re-declare `NAME: $NAME` no-ops; it only
  references them. (Mirrors `render_gitlab.go`'s `NoRedundantSecretVars` rule.)

### New/changed files

**PR1 — workflow** (`feat/cigen-jenkins-circleci-804`):
- `cigen/render_jenkins.go` — `RenderJenkins(*CIPlan) (map[string]string, error)`
  → `{"Jenkinsfile": …}`.
- `cigen/render_circleci.go` — `RenderCircleCI(*CIPlan) (map[string]string,
  error)` → `{".circleci/config.yml": …}`.
- `cigen/render_jenkins_test.go`, `cigen/render_circleci_test.go` — reuse the
  shared `richCIPlan()` helper; assert: valid syntax (YAML for CircleCI; Jenkins
  structural greps), secret wiring present, `wfctl migrations up` present, smoke
  present, plan-guard present, two-phase chaining, nil-plan error, and **absence**
  of legacy `go test ./...` / `wfctl deploy --image` / `docker build`.
- `cmd/wfctl/ci.go` — add `case "jenkins"` / `case "circleci"` to the render
  switch (line ~160) and the legacy `generateCIFiles` switch; update usage text.
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
  constructors are unused. (NOTE: `github_actions.go`/`gitlab_ci.go` were already
  dead post-#18; only their own tests referenced them. Removing the whole package
  is the honest cleanup, flagged for adversarial review as a slightly broader-
  than-jenkins/circleci deletion.)
- Update `internal/generator_test.go` / `integration_test.go` to assert the four
  platforms render config-derived output via cigen (drop template-generator tests).
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
  `go test ./...` / `wfctl deploy --image`. Skip cleanly if wfctl absent.
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
- **Plan-guard preserved:** the destructive-plan guard (`exit 1`, no `|| true`)
  is carried into both new renderers — a protected resource still blocks apply.
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

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | Dropping the legacy docker-build/deploy stages is acceptable | A Jenkins/CircleCI user may rely on the old build/deploy | Documented behavior change + plugin minor bump v0.2.0 + ADR; GHA/GitLab never emitted these, so this is parity, not loss of a config-derived feature. |
| A2 | `credentials('NAME')` is the right Jenkins secret idiom for arbitrary secret names | Some names need a configured Jenkins credential id; binding may differ for files vs strings | Emit string credentials by default (matches env-var usage); a `Warnings` note already exists for non-`^[A-Z0-9_]+$` names. |
| A3 | workflow can be tagged v0.68.0 before the plugin bump | Tag/release cadence | PR1 merge + tag is a prerequisite gate for PR2; sequenced explicitly. |
| A4 | `github_actions.go`/`gitlab_ci.go` in the plugin are already dead | They might be referenced indirectly | Verified: `generator.go` registry holds only jenkins/circleci; GHA/GitLab go through cigen since #18. Confirmed by grep this session. |
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

## Self-Challenge

- **Simplest alternative:** just add the 2 renderers + wfctl switch, skip the
  plugin rewire. Rejected — issue acceptance #2 mandates the plugin rewire +
  template removal; skipping leaves the issue half-done.
- **Most fragile assumption:** A1 (dropping docker stages). Mitigated by ADR +
  the fact that config-derived parity with GHA/GitLab is the explicit ask.
- **YAGNI sweep:** no new CIPlan field, no docker stage, no new subcommand, no
  per-platform Analyze branch — all rejected as surface the issue didn't ask for.
