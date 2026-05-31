# cigen Jenkins + CircleCI Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add config-derived `cigen.RenderJenkins` / `cigen.RenderCircleCI`, wire them into `wfctl ci generate` and the ci-generator plugin (retiring the legacy template generators), and prove it via a workflow-scenarios behavior scenario.

**Architecture:** Two new mechanical renderers in `workflow/cigen` that emit the same plan/apply/smoke job set as the GHA renderer (authoritative precedent) from the existing CIPlan — Jenkins declarative pipeline + CircleCI 2.1. `wfctl ci generate` + the plugin route all four platforms through cigen. A workflow-scenarios scenario runs the real `wfctl ci generate` and asserts config-derived output. Cross-repo: workflow tags v0.68.0 → plugin bumps the pin + rewires → scenario builds wfctl from workflow main.

**Tech Stack:** Go (stdlib `strings`/`fmt` string builders, `gopkg.in/yaml.v3` in tests); bash scenario harness.

**Base branch:** main (each repo)

---

## Scope Manifest

**PR Count:** 3
**Tasks:** 9
**Estimated Lines of Change:** ~700 (informational; not enforced)

**Out of scope:**
- Any `cigen.Analyze` change or new CIPlan field (renderers consume the existing plan).
- Any docker-build/push/`wfctl deploy --image` stage (ADR 0044 — the legacy non-config-derived stages are retired, not ported).
- Any change to GHA/GitLab renderer output.
- Fixing the GitLab renderer's pre-existing missing plan-guard/scoped-secrets (Follow-up).
- Jenkins PR-comment of the plan (no native equivalent; plan goes to build log).
- Live Jenkins/CircleCI server execution in the proof (generate-and-assert posture).

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(cigen): config-derived Jenkins + CircleCI renderers + wfctl four-platform support (#804) | Task 1, Task 2, Task 3, Task 4 | workflow: feat/cigen-jenkins-circleci-804 |
| 2 | feat: route jenkins/circleci through cigen, retire template generators (#804) | Task 5, Task 6, Task 7 | workflow-plugin-ci-generator: feat/cigen-jenkins-circleci-804 |
| 3 | test(scenario-97): config-derived jenkins/circleci CI generation proof (#804) | Task 8, Task 9 | workflow-scenarios: feat/cigen-jenkins-circleci-proof-804 |

**Status:** Locked 2026-05-31T22:52:06Z

---

## Project Design Guidance

`Guidance: no docs/design-guidance.md in workflow; canon = CLAUDE.md cigen
conventions + render_gha.go precedent + ADR 0044.` Mapping:
- Config-derived over templated → renderers read only CIPlan (Tasks 1,2); legacy
  templates deleted (Task 6).
- Mirror GHA precedent → Tasks 1,2 reuse `migrationsUpCommand` + `phase.Scoped`
  branch + plan-guard from `render_gha.go`.
- `wfctl migrations up` real runner → both renderers call `migrationsUpCommand`
  (asserted absent of `wfctl ci run --phase migrate` in tests).
- Record trade-offs → ADR 0044 (committed with the design).

**Cross-repo execution order (hard gate):** PR1 merges to workflow/main AND is
tagged **v0.68.0** before PR2's `go.mod` bump resolves. PR3 builds wfctl from the
PR1 branch/main. Execute PR1 fully (merge + tag) before PR2; PR3 after PR1.

---

### Task 1: cigen.RenderJenkins

**Files:**
- Create: `cigen/render_jenkins.go`
- Create: `cigen/render_jenkins_test.go`

**Step 1: Write the failing test** (`cigen/render_jenkins_test.go`, `package cigen_test`, reuse the shared `richCIPlan()` from `render_gha_test.go`):

```go
package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestRenderJenkins_ConfigDerived(t *testing.T) {
	files, err := cigen.RenderJenkins(richCIPlan())
	if err != nil {
		t.Fatalf("RenderJenkins: %v", err)
	}
	content, ok := files["Jenkinsfile"]
	if !ok {
		t.Fatal("expected Jenkinsfile in output")
	}
	must := []string{
		"pipeline {",
		"// Requires a Jenkins Multibranch Pipeline",   // C2 header
		"// Required Jenkins credentials: APP_DB_URL, SECRET_ONE, SECRET_TWO", // union, SORTED — renderer MUST sort
		"stage('Apply prereq')",
		"stage('Apply deploy')",
		"environment {",
		"when { changeRequest() }",                       // plan gate
		"when { branch 'main' }",                          // apply gate
		"wfctl migrations up",                             // real runner
		"--format json",
		"curl --fail --max-time 30 'https://myapp.example.com/healthz'", // smoke
		"wfctl infra apply --config 'deploy.yaml' --auto-approve",
	}
	for _, m := range must {
		if !strings.Contains(content, m) {
			t.Errorf("Jenkinsfile missing %q\n---\n%s", m, content)
		}
	}
	// Each secret wired individually (robust against header-sort-order regressions):
	// richCIPlan phases are NOT Scoped, so both apply stages use the p.Secrets union.
	for _, name := range []string{"APP_DB_URL", "SECRET_ONE", "SECRET_TWO"} {
		if !strings.Contains(content, "credentials('"+name+"')") {
			t.Errorf("expected credentials('%s') binding", name)
		}
	}
	// plan-guard present
	if !strings.Contains(content, "exit 1") {
		t.Error("expected plan-guard exit 1")
	}
	// legacy non-config-derived stages must be ABSENT (ADR 0044)
	for _, banned := range []string{"go test ./...", "wfctl deploy --image", "docker build", "docker push", "wfctl ci run --phase migrate"} {
		if strings.Contains(content, banned) {
			t.Errorf("Jenkinsfile must NOT contain legacy %q", banned)
		}
	}
	// apply-prereq stage appears before apply-deploy stage (ordering)
	if strings.Index(content, "stage('Apply prereq')") > strings.Index(content, "stage('Apply deploy')") {
		t.Error("expected Apply prereq stage before Apply deploy stage")
	}
}

func TestRenderJenkins_NilPlan(t *testing.T) {
	if _, err := cigen.RenderJenkins(nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRenderJenkins_SinglePhase(t *testing.T) {
	p := richCIPlan()
	p.Phases = []cigen.DeployPhase{{Name: "deploy", ConfigPath: "deploy.yaml"}}
	files, err := cigen.RenderJenkins(p)
	if err != nil {
		t.Fatalf("RenderJenkins single-phase: %v", err)
	}
	if !strings.Contains(files["Jenkinsfile"], "stage('Apply deploy')") {
		t.Error("expected single Apply deploy stage")
	}
}
```

**Step 2: Run → FAIL** (`undefined: cigen.RenderJenkins`).
Run: `cd /Users/jon/workspace/workflow && GOWORK=off go test ./cigen/ -run TestRenderJenkins 2>&1 | tail`
Expected: compile error / FAIL.

**Step 3: Implement** (`cigen/render_jenkins.go`) — declarative pipeline, single
linear `stages{}`, per-phase plan stage gated on `changeRequest()`, per-phase
apply stage gated on `branch '<default>'` with per-stage `environment{}` scoped
secrets (branch on `phase.Scoped`), plan-guard (when `p.PlanGuard`), migrations on
last phase via shared `migrationsUpCommand`, smoke stage. Header comments:
Multibranch requirement + sorted union of required credentials. Install wfctl via
`go install …@<version>` inside each stage's steps; pipeline-level `environment {
PATH = "${HOME}/go/bin:${PATH}" }`. Reuse `migrationsUpCommand` (already in
`render_gha.go`). Sort the credentials union for determinism. (Full code authored
in execution to satisfy the assertions above; mirror `render_gha.go`'s
`writeApplyJob` secret/plan-guard/migrations logic.)

**Step 4: Run → PASS**
Run: `GOWORK=off go test ./cigen/ -run TestRenderJenkins -v 2>&1 | tail -20`
Expected: `PASS` (all three tests).

**Step 5: Commit**
```bash
git add cigen/render_jenkins.go cigen/render_jenkins_test.go
git commit -m "feat(cigen): config-derived RenderJenkins (#804)"
```
Rollback: revert commit — additive new files, no migration.

---

### Task 2: cigen.RenderCircleCI

**Files:**
- Create: `cigen/render_circleci.go`
- Create: `cigen/render_circleci_test.go`

**Step 1: Write the failing test** (mirror `render_gitlab_test.go` structural depth + YAML validity):

```go
package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
	"gopkg.in/yaml.v3"
)

func TestRenderCircleCI_ValidYAMLAndStructure(t *testing.T) {
	files, err := cigen.RenderCircleCI(richCIPlan())
	if err != nil {
		t.Fatalf("RenderCircleCI: %v", err)
	}
	content := files[".circleci/config.yml"]
	var parsed any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, content)
	}
	must := []string{
		"version: 2.1",
		"workflows:",
		"plan-prereq", "plan-deploy",
		"apply-prereq", "apply-deploy",
		"requires:",                      // CircleCI graph keyword (NOT needs:)
		"wfctl migrations up", "--format json",
		"wfctl infra apply --config 'deploy.yaml' --auto-approve",
		"curl --fail --max-time 30 'https://myapp.example.com/healthz'",
	}
	for _, m := range must {
		if !strings.Contains(content, m) {
			t.Errorf(".circleci/config.yml missing %q\n---\n%s", m, content)
		}
	}
	if strings.Contains(content, "needs:") {
		t.Error("CircleCI uses requires:, not GHA needs:")
	}
	// Positive secret-wiring: each secret name must appear (referenced by an apply
	// job's run/env), so a renderer that emits NO secret wiring fails this.
	for _, s := range richCIPlan().Secrets {
		if !strings.Contains(content, s.Name) {
			t.Errorf("expected secret %s referenced in output", s.Name)
		}
		// CircleCI auto-injects project env vars; no redundant NAME: $NAME re-declare.
		if strings.Contains(content, "  "+s.Name+": $"+s.Name) {
			t.Errorf("redundant secret re-declare for %s", s.Name)
		}
	}
	if !strings.Contains(content, "exit 1") {
		t.Error("expected plan-guard exit 1")
	}
	for _, banned := range []string{"go test ./...", "wfctl deploy --image", "docker build", "wfctl ci run --phase migrate"} {
		if strings.Contains(content, banned) {
			t.Errorf("must NOT contain legacy %q", banned)
		}
	}
}

func TestRenderCircleCI_NilPlan(t *testing.T) {
	if _, err := cigen.RenderCircleCI(nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRenderCircleCI_SinglePhase(t *testing.T) {
	p := richCIPlan()
	p.Phases = []cigen.DeployPhase{{Name: "deploy", ConfigPath: "deploy.yaml"}}
	files, err := cigen.RenderCircleCI(p)
	if err != nil {
		t.Fatalf("single-phase: %v", err)
	}
	if !strings.Contains(files[".circleci/config.yml"], "apply:") && !strings.Contains(files[".circleci/config.yml"], "apply-deploy") {
		t.Error("expected an apply job for single phase")
	}
}
```

**Step 2: Run → FAIL** (`undefined: cigen.RenderCircleCI`).
Run: `GOWORK=off go test ./cigen/ -run TestRenderCircleCI 2>&1 | tail`

**Step 3: Implement** (`cigen/render_circleci.go`) — CircleCI 2.1: `jobs:` with
per-phase `plan[-name]` (run `wfctl infra plan`) and `apply[-name]` (plan-guard
when PlanGuard, migrations on last phase, `wfctl infra apply`), optional `smoke`
job; `workflows:` graph ordering plan jobs (filter to non-default branches) and
apply jobs (`requires:` previous apply, filter to default branch), smoke requires
last apply. Reference auto-injected project env vars (no `NAME: $NAME` redeclare,
per GitLab precedent). Reuse `migrationsUpCommand`. (Full code authored in
execution to satisfy the YAML + structural assertions.)

**Step 4: Run → PASS**
Run: `GOWORK=off go test ./cigen/ -run TestRenderCircleCI -v 2>&1 | tail -20`
Expected: `PASS`.

**Step 5: Commit**
```bash
git add cigen/render_circleci.go cigen/render_circleci_test.go
git commit -m "feat(cigen): config-derived RenderCircleCI (#804)"
```
Rollback: revert commit — additive.

---

### Task 3: Wire jenkins/circleci into wfctl ci generate

**Files:**
- Modify: `cmd/wfctl/ci.go` (render switch ~160; legacy `generateCIFiles` switch ~288; usage text ~52/73/104; two `unsupported platform` error strings ~166/~294)
- Modify: `cmd/wfctl/ci_wizard.go` (`platformOptions` ~51)
- Modify/extend: `cmd/wfctl/ci_test.go` (assert the four-platform switch)

**Step 1: Write/extend the failing test** — add a wfctl-level test that
`generateCIFiles` (or the render switch helper) returns config-derived output for
`jenkins` and `circleci`:

```go
func TestGenerateCIFiles_JenkinsCircleCI(t *testing.T) {
	// Purpose: verify the platform SWITCH dispatches to the cigen renderers.
	// Content quality is gated by the cigen unit tests (Tasks 1+2), not here.
	cases := map[string]string{"jenkins": "pipeline {", "circleci": "version: 2.1"}
	for plat, marker := range cases {
		files, err := generateCIFiles(ciOptions{Platform: plat, InfraConfig: "infra.yaml"})
		if err != nil {
			t.Fatalf("%s: %v", plat, err)
		}
		joined := ""
		for _, c := range files {
			joined += c
		}
		if !strings.Contains(joined, marker) {
			t.Errorf("%s: expected marker %q in output", plat, marker)
		}
	}
}
```

**Step 2: Run → FAIL** (`unsupported platform "jenkins"`).
Run: `GOWORK=off go test ./cmd/wfctl/ -run TestGenerateCIFiles_JenkinsCircleCI 2>&1 | tail`

**Step 3: Implement** — add `case "jenkins": files, renderErr = cigen.RenderJenkins(plan)`
and `case "circleci": files, renderErr = cigen.RenderCircleCI(plan)` to the render
switch (~160); add `case "jenkins": return generateJenkins(opts)` / `circleci` to
`generateCIFiles` (~288) with `generateJenkins`/`generateCircleCI` helpers
(mirror `generateGitHubActions`); update BOTH `unsupported platform` error strings
and all usage text (~52/73/104) to `github_actions, gitlab_ci, jenkins, circleci`;
add `"jenkins", "circleci"` to `platformOptions` in `ci_wizard.go`.

**Step 4: Run → PASS + full cigen/wfctl suite + lint**
Run: `GOWORK=off go test ./cmd/wfctl/ ./cigen/ 2>&1 | tail -5`
Expected: `ok`.
Run: `GOWORK=off golangci-lint run --new-from-rev=origin/main ./cigen/... ./cmd/wfctl/... 2>&1 | tail`
Expected: exit 0.

**Step 5: Runtime-launch validation (CLI change class)** — build wfctl and run it:
```bash
GOWORK=off go build -o /tmp/wfctl-804 ./cmd/wfctl
cd $(mktemp -d) && cp /Users/jon/workspace/workflow/example/*.yaml . 2>/dev/null; \
  /tmp/wfctl-804 ci generate --platform jenkins --config <an example config> --output-dir out --write && \
  grep -q "pipeline {" out/Jenkinsfile && echo "JENKINS OK"
/tmp/wfctl-804 ci generate --platform circleci --config <same> --output-dir out --write && \
  grep -q "version: 2.1" out/.circleci/config.yml && echo "CIRCLE OK"
```
Use a real, rich config: `example/api-server-config.yaml` (or, once Task 8 has
authored it, `scenarios/97-…/config/app.yaml`). Replace `<an example config>`
with that path.
Expected: `JENKINS OK` + `CIRCLE OK`; capture transcript for the PR. (Keep
`/tmp/wfctl-804` — Tasks 8/9 reuse it as `WFCTL_BIN`.)

**Step 6: Commit**
```bash
git add cmd/wfctl/ci.go cmd/wfctl/ci_wizard.go cmd/wfctl/ci_test.go
git commit -m "feat(wfctl): ci generate --platform jenkins|circleci via cigen (#804)"
```
Rollback: revert commit — restores three-platform support, additive switch cases.

---

### Task 4: Docs + workflow version bump → v0.68.0

**Files:**
- Modify: `DOCUMENTATION.md` / `docs/WFCTL.md` (note four-platform `ci generate`)
- Modify: the workflow version source (the file `wfctl --version` / release reads — confirm at execution: `sdk.ResolveBuildVersion` ldflag vs a VERSION constant; if version is ldflag-injected at release time, no file bump is needed — the tag drives it).

**Step 1: Update docs** — add jenkins/circleci to the `wfctl ci generate
--platform` reference in `docs/WFCTL.md` and the cigen section of `DOCUMENTATION.md`.

**Step 2: Verify version mechanism** —
Run: `cd /Users/jon/workspace/workflow && grep -rn "0.67.0\|var version\|ResolveBuildVersion" cmd/wfctl/*.go sdk/ 2>/dev/null | head`
If the version is ldflag-injected by the release workflow (no in-repo constant),
the tag `v0.68.0` is the only bump needed (done at finish/merge). If an in-repo
constant exists, bump it to `0.68.0`.

**Step 3: Verify docs render** — `grep -n "jenkins" docs/WFCTL.md DOCUMENTATION.md`
Expected: jenkins/circleci listed under `ci generate` platforms.

**Step 4: Commit**
```bash
git add DOCUMENTATION.md docs/WFCTL.md
git commit -m "docs: wfctl ci generate four-platform support (#804)"
```
Rollback: revert commit (docs-only). Version-pin rollback: do not push tag
v0.68.0 if PR1 reverted.

> After PR1 review + CI green + merge: tag **v0.68.0** on workflow main (the
> prerequisite gate for PR2). Version-skew note: nothing else in workflow lags.
>
> **I4 — release-availability gate (MANDATORY before Task 5):** pushing `v0.68.0`
> triggers the workflow `release.yml` action; the Go module proxy only serves
> `@v0.68.0` after the release run completes. Before Task 5's `go get`, wait via a
> bash poll-loop ([[feedback_ci_wait_use_bash_poll_loop]]):
> ```bash
> # 1) wait for the release workflow run to finish
> until gh run list --repo GoCodeAlone/workflow --workflow=release.yml --branch main \
>   --limit 1 --json status,conclusion -q '.[0].status' | grep -q completed; do sleep 30; done
> # 2) confirm the proxy serves the version (retry — proxy lags the release slightly)
> for i in $(seq 1 20); do
>   GOWORK=off GOPROXY=https://proxy.golang.org go list -m github.com/GoCodeAlone/workflow@v0.68.0 \
>     2>/dev/null && break || sleep 30
> done
> ```
> Only proceed to Task 5 once `go list -m …@v0.68.0` succeeds.

---

### Task 5: Plugin — bump workflow dependency to v0.68.0

**Files:**
- Modify: `go.mod` (workflow `v0.67.0` → `v0.68.0`), `go.sum`

**Pre-req:** workflow **v0.68.0 must be tagged** (Task 4 close-out) so the module
resolves.

**Step 1: Bump + tidy**
```bash
cd /Users/jon/workspace/workflow-plugin-ci-generator
go get github.com/GoCodeAlone/workflow@v0.68.0
GOWORK=off go mod tidy
```
**Step 2: Verify it resolves + `cigen.RenderJenkins` is visible**
Run: `GOWORK=off go build ./... 2>&1 | tail` (will still fail until Task 6 rewires
the calls; at minimum the module must download — confirm `go list -m
github.com/GoCodeAlone/workflow` prints `v0.68.0`).
Expected: `…/workflow v0.68.0`.

**Step 3: Commit**
```bash
git add go.mod go.sum
git commit -m "chore: bump workflow to v0.68.0 for cigen jenkins/circleci (#804)"
```
Rollback: `go get …workflow@v0.67.0 && go mod tidy`; revert commit.

---

### Task 6: Plugin — route jenkins/circleci through cigen, delete platforms package

**Files:**
- Modify: `internal/generator.go` (4-way cigen switch; delete `registry` map, `Generator` interface, the legacy `default:` template branch, the `platforms` import)
- Delete: `internal/platforms/` (jenkins.go, circleci.go, github_actions.go, gitlab_ci.go, options.go + all `*_test.go`)

**Step 1: Rewire `generator.go`** — change the platform switch (~77) so all four
route through cigen:
```go
switch platform {
case PlatformGitHubActions, PlatformGitLabCI, PlatformJenkins, PlatformCircleCI:
    // ... existing analyze/from-plan block builds `plan` ...
    switch platform {
    case PlatformGitHubActions:
        files, err = cigen.RenderGitHubActions(plan)
    case PlatformGitLabCI:
        files, err = cigen.RenderGitLabCI(plan)
    case PlatformJenkins:
        files, err = cigen.RenderJenkins(plan)
    case PlatformCircleCI:
        files, err = cigen.RenderCircleCI(plan)
    }
    // ... err handling ...
}
```
Delete the `default:` template branch, the `registry` var (~35), the `Generator`
interface (~27), and the `platforms` import. `knownPlatforms` stays (validates the
platform string).

**Step 2: Delete the platforms package**
```bash
git rm -r internal/platforms/
```

**Step 3: Build → expect test-file compile errors (fixed in Task 7)**
Run: `GOWORK=off go build ./... 2>&1 | tail`
Expected: `internal/` builds; `go vet`/test compile fails only in `*_test.go`
(handled next). Production build clean.

**Step 4: Commit (LOCAL ONLY — do NOT push yet)**
```bash
git add internal/generator.go && git rm -r internal/platforms/
git commit -m "feat: route all four platforms through cigen, delete template generators (#804)"
```
> **C2 — broken-CI window:** between this commit and Task 7 the `*_test.go` files
> reference the deleted `registry`/`Generator`/`platforms.Options` and will NOT
> compile. Do NOT push the PR2 branch to remote until Task 7 makes `go test ./...`
> green — `finishing-a-development-branch` pushes the whole branch once, so CI only
> ever sees the Task-7-complete state.

Rollback: `git revert` restores `internal/platforms/` and the registry.

---

### Task 7: Plugin — rewrite tests + plugin version bump → v0.2.0

**Files:**
- Modify: `internal/generator_test.go` (replace the two `_TemplateUnchanged` tests with `_CigenMarkers`; delete `staticGenerator` + `registerTestGenerator`; rewrite the path-safety + sort tests to call package functions directly)
- Modify: `integration_test.go` (replace the `wftest.MockStep` circleci test with a real `ExecuteCIGenerate` call; add jenkins)
- Modify: `plugin.json` (`0.1.6` → `0.2.0`)

**Step 0 (I3 — confirm testdata richness):** the new `_CigenMarkers` tests rely on
the same `testdataConfig` the GHA/GitLab marker tests use. Confirm that config
yields migrations + secrets BEFORE authoring the jenkins/circleci equivalents:
Run: `cd /Users/jon/workspace/workflow-plugin-ci-generator && GOWORK=off go test ./internal/ -run 'TestExecuteCIGenerateGitHubActions_CigenMarkers' -v 2>&1 | tail`
Expected: PASS (proves the shared testdata config → a plan with Migrations +
Secrets; the same plan feeds RenderJenkins/RenderCircleCI, so their markers will
render). If it FAILS or the config lacks an `infra.*` module, enrich the testdata
config first.

**Step 1: Rewrite `internal/generator_test.go`** —
- Replace `TestExecuteCIGenerateJenkins_TemplateUnchanged` (~267) and
  `TestExecuteCIGenerateCircleCI_TemplateUnchanged` (~306) with
  `TestExecuteCIGenerateJenkins_CigenMarkers` /
  `TestExecuteCIGenerateCircleCI_CigenMarkers` that call `ExecuteCIGenerate` for
  the platform, read the written file, and assert config-derived markers (secret
  env, `wfctl migrations up`, smoke, plan-guard) + **absence** of `go test ./...`
  / `wfctl deploy --image`. Mirror the existing GHA/GitLab `_CigenMarkers` tests
  (~66/~127).
- Delete `staticGenerator` (~411) + `registerTestGenerator` (~421).
- `TestExecuteCIGenerateRejectsUnsafeGeneratedPath` → a direct unit test of
  `validateRelativeOutputPath` (`generator.go:193`, same package) asserting
  `../escape` and absolute paths error and `Jenkinsfile` is accepted.
- `TestExecuteCIGenerateSortsFilesWritten` → assert the `FilesWritten` slice is
  sorted on a real `ExecuteCIGenerate` render that writes ≥1 file (use a config
  yielding a deterministic set; or assert `sort.StringsAreSorted` over the output).

**Step 2: Rewrite the circleci integration test** (`integration_test.go` ~140) —
replace the `wftest.MockStep` with a real `ExecuteCIGenerate` call writing to
`t.TempDir()`, asserting the `.circleci/config.yml` is config-derived; add the
jenkins equivalent. This is the **acceptance-#2 plugin-path proof**.

**Step 3: Run the suite → PASS**
Run: `cd /Users/jon/workspace/workflow-plugin-ci-generator && GOWORK=off go test ./... 2>&1 | tail -15`
Expected: `ok` for `internal` + root integration package; no reference to
`internal/platforms`.
Run: `GOWORK=off golangci-lint run --new-from-rev=origin/main ./... 2>&1 | tail`
Expected: exit 0.

**Step 4: Plugin-load runtime validation (plugin change class)** — build the
plugin binary in the wfctl-discoverable layout + drive a representative call:
```bash
GOWORK=off go build -o /tmp/ci-gen-plugin/ci-generator/ci-generator ./cmd/plugin
cp plugin.json /tmp/ci-gen-plugin/ci-generator/
# representative: run the plugin's ExecuteCIGenerate via the integration test (already real)
GOWORK=off go test ./... -run Integration 2>&1 | tail
```
Expected: integration tests pass (real plugin entry point renders config-derived
jenkins/circleci). Capture transcript for the PR.

**Step 5: Bump plugin version**
Edit `plugin.json` version `0.1.6` → `0.2.0`.

**Step 6: Commit**
```bash
git add internal/generator_test.go integration_test.go plugin.json
git commit -m "test+chore: cigen-route tests for jenkins/circleci, plugin v0.2.0 (#804)"
```
Rollback: revert commits; pin plugin back to v0.1.6 + workflow v0.67.0.

> After PR2 review + CI green + merge: tag the plugin **v0.2.0**.

---

### Task 8: workflow-scenarios — scenario 97 files + registration

**Files:**
- Create: `scenarios/97-ci-generate-jenkins-circleci/scenario.yaml`
- Create: `scenarios/97-ci-generate-jenkins-circleci/README.md`
- Create: `scenarios/97-ci-generate-jenkins-circleci/config/app.yaml` (real config: a `secrets.entries` block; an `infra.container_service` with `health_check.http_path` + a PRIMARY `domains` entry → smoke; a `ci.migrations` entry with `database.env` → migrations; a `protected: true` module → plan-guard; an `infra.*` module → plugin-install)
- Create: `scenarios/97-ci-generate-jenkins-circleci/config/step-ci-generate.yaml` (a small pipeline with `step.ci_generate` `platform: jenkins` and `platform: circleci` — for the `wfctl validate` config-shape check)
- Create: `scenarios/97-ci-generate-jenkins-circleci/test/run.sh` (executable)
- Modify: `scenarios.json` (register id 97)

**Step 0 (m1 — schema accepts jenkins/circleci):** the `step.ci_generate` config
shape check only works if the step schema accepts `platform: jenkins|circleci`.
The legacy plugin registry already supported both platforms, so the schema is
expected to accept them, but confirm:
Run: `WFCTL_BIN=/tmp/wfctl-804; $WFCTL_BIN get-step-schema step.ci_generate 2>/dev/null | grep -A6 -i platform || echo "no enum constraint"`
Expected: `platform` has no enum, OR the enum includes jenkins/circleci. If the
schema enumerates only github_actions/gitlab_ci, drop the validate sub-check
(Step 2 item 4) and note it — the behavior proof (PR2 integration test) still
covers acceptance #2.

**Step 1: Author `config/app.yaml`** — a minimal but real workflow config that
`cigen.Analyze` derives a rich plan from. Required for each derivation (per
`cigen/analyze.go`): smoke ⇐ an `infra.container_service` module with
`health_check.http_path` + a `domains` entry `type: PRIMARY`; migrations ⇐ a
`ci.migrations[]` entry with `database.env`; plan-guard ⇐ any module with
`protected: true`; plugin-install ⇐ any `infra.*`/`iac.*`/`plugin.*` module;
secrets ⇐ a `secrets.entries` block. Validate it parses: `python3 -c "import yaml;
yaml.safe_load(open('.../config/app.yaml'))"`. After authoring, dry-run the
analyzer to confirm the plan is rich: `$WFCTL_BIN ci plan --config
.../config/app.yaml` shows migrations + secrets + smoke + plan_guard.

**Step 2: Author `test/run.sh`** (mirror scenario 77's wfctl-locate + PASS/FAIL
harness) that:
1. Locates wfctl (`WFCTL_BIN`/`$WORKFLOW_REPO/bin/wfctl`/`which wfctl`); `skip` if absent.
2. `wfctl ci generate --platform jenkins --config config/app.yaml --output-dir $TMP/j --write` then asserts `$TMP/j/Jenkinsfile` contains a config secret name, `wfctl migrations up`, the smoke URL, plan-guard `exit 1`, `wfctl infra apply`, and does NOT contain `go test ./...` / `wfctl deploy --image`.
3. Same for `--platform circleci` → `$TMP/c/.circleci/config.yml` (+ assert `version: 2.1`, `requires:`).
4. `wfctl validate --skip-unknown-types config/step-ci-generate.yaml` passes (config-shape half of acceptance #2).
5. Prints `Results: N passed, M failed` and exits non-zero on any FAIL.

**Step 3: Author `scenario.yaml` + `README.md`** (category C, status testable,
tags ci/jenkins/circleci/cigen/config-derived). **Register in `scenarios.json`**
with id `97-ci-generate-jenkins-circleci`.

**Step 4: Lint the harness** — `bash -n scenarios/97-ci-generate-jenkins-circleci/test/run.sh`
Expected: no syntax error. Confirm registration: `python3 -c "import json;
print('97-ci-generate-jenkins-circleci' in open('scenarios.json').read())"` → `True`.

**Step 5: Commit**
```bash
git add scenarios/97-ci-generate-jenkins-circleci/ scenarios.json
git commit -m "test(scenario-97): config-derived jenkins/circleci CI generation (#804)"
```
Rollback: revert commit (scenario-only).

---

### Task 9: workflow-scenarios — run the scenario, capture honest evidence

**Files:**
- Create: `scenarios/97-ci-generate-jenkins-circleci/test/artifacts/last-run.log` (the real run output)

**Step 1: Build wfctl from the PR1 branch** (or reuse `/tmp/wfctl-804` from Task 3):
```bash
cd /Users/jon/workspace/workflow && git checkout feat/cigen-jenkins-circleci-804 && \
  GOWORK=off go build -o /tmp/wfctl-804 ./cmd/wfctl
```
**Step 2: Run scenario 97 against the real binary** (demonstration-fidelity —
real `wfctl ci generate`, not a reimplementation):
```bash
cd /Users/jon/workspace/workflow-scenarios && \
  WFCTL_BIN=/tmp/wfctl-804 bash scenarios/97-ci-generate-jenkins-circleci/test/run.sh \
  | tee scenarios/97-ci-generate-jenkins-circleci/test/artifacts/last-run.log
```
Expected: `Results: N passed, 0 failed` — the gate is **0 failed** (the exact
passed count = however many assertions the authored `run.sh` contains; do not
hard-code a target). If any FAIL → fix the renderer (Task 1/2) or the scenario,
re-run. The log is the honest evidence pasted into the PR3 body.

**Step 3: Commit the evidence**
```bash
git add scenarios/97-ci-generate-jenkins-circleci/test/artifacts/last-run.log
git commit -m "test(scenario-97): captured real wfctl ci generate evidence (#804)"
```
Rollback: revert commit.

---

## Verification summary (change-class mapping)

| Task | Change class | Verification | Expected |
|---|---|---|---|
| 1 | Go code (renderer) | `go test ./cigen/ -run TestRenderJenkins` | PASS (config-derived markers, no legacy stages) |
| 2 | Go code (renderer) | `go test ./cigen/ -run TestRenderCircleCI` | PASS (valid YAML + structure) |
| 3 | CLI command | `go test ./cmd/wfctl/` + build + `wfctl ci generate --platform jenkins/circleci` run | help/usage lists 4 platforms; real run writes Jenkinsfile/.circleci |
| 4 | Docs + version pin | grep docs; confirm version mechanism | jenkins/circleci documented; v0.68.0 tag drives release |
| 5 | Version pin | `go list -m …workflow` | v0.68.0 resolves |
| 6 | Go code (rewire+delete) | `go build ./...` | production builds; platforms package gone |
| 7 | Plugin + tests | `go test ./...` + plugin-load run + golangci-lint | config-derived jenkins/circleci via ExecuteCIGenerate; v0.2.0 |
| 8 | Test scenario | `bash -n run.sh` + registration check | valid harness; registered id 97 |
| 9 | Multi-component proof | run scenario vs real wfctl | `N passed, 0 failed`; evidence committed |

## Multi-Component / Integration proof

The real boundaries: (a) cigen↔wfctl — Task 3 builds + runs the real wfctl
(`ci generate --platform jenkins/circleci`); Task 9 runs the scenario against that
binary. (b) cigen↔plugin — Task 7's `integration_test.go` drives the real
`ExecuteCIGenerate` for both platforms and asserts on written files (acceptance
#2). No mock-only validation on either boundary.
