# cigen fidelity: per-phase secret scoping + migration operational flags — Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Narrow two measured fidelity gaps in cigen's GitHub Actions output: (#3) scope each apply job's `env:` to only the secrets that phase's config references; (#4) emit `wfctl migrations up … --format json` always plus `--env <env>` when unambiguously derivable.

**Architecture:** Additive `DeployPhase.{Secrets,Scoped}` fields; `Analyze` loads the prereq config from `opts.PhaseConfig` and runs the existing `deriveSecrets` per config; `render_gha` branches each apply job's `env:` source on `phase.Scoped` (else the union fallback). `deriveMigrations` populates `MigrationsSpec.Env` only when exactly one `ci.migrations[0].environments` key exists; `migrationsUpCommand` appends `--format json` unconditionally.

**Tech Stack:** Go (stdlib + `github.com/GoCodeAlone/workflow/config`). No new deps. No release.

**Base branch:** main (worktree branch `feat/cigen-phase-secret-scoping`)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 5
**Estimated Lines of Change:** ~180 (informational; not enforced)

**Out of scope:**
- Per-phase scoping for single-phase deploys (union IS the scope; no change).
- Deriving `--env` when ambiguous (≥2 `environments` keys) — omit + warn.
- Scoping when the phase-config is alias-only (no loadable file) — union fallback + warn.
- A plugin (ci-generator) release/bump — `wfctl ci generate` gets the fix directly; plugin picks it up on its next workflow-dep bump (noted, optional follow-on).
- GitLab/Jenkins/CircleCI renderers — GHA only (where phases + the measured gap live).
- `--env prod` for multisite specifically — multisite declares NO `environments:`, so `--env` stays not-derivable for it (honest; design C1).

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(cigen): per-phase secret scoping + migration --format json/--env | Task 1, Task 2, Task 3, Task 4, Task 5 | feat/cigen-phase-secret-scoping |

**Status:** Locked 2026-05-31T20:35:52Z

---

### Task 1: DeployPhase model — add Secrets + Scoped

**Files:**
- Modify: `cigen/plan.go:51-58` (DeployPhase struct)
- Test: `cigen/plan_test.go` (new — minimal struct/JSON round-trip)

**Step 1: Write the failing test**

Create `cigen/plan_test.go`:

```go
package cigen

import (
	"encoding/json"
	"testing"
)

func TestDeployPhase_ScopedSecretsJSON(t *testing.T) {
	p := DeployPhase{
		Name:       "prereq",
		ConfigPath: "deploy.prereq.yaml",
		Secrets:    []SecretRef{{Name: "DIGITALOCEAN_TOKEN"}},
		Scoped:     true,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DeployPhase
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Scoped || len(got.Secrets) != 1 || got.Secrets[0].Name != "DIGITALOCEAN_TOKEN" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	// Unscoped phase with no secrets must omit both in JSON (additive, back-compat).
	b2, _ := json.Marshal(DeployPhase{Name: "deploy", ConfigPath: "deploy.yaml"})
	if string(b2) != `{"name":"deploy","config_path":"deploy.yaml"}` {
		t.Fatalf("unexpected JSON for unscoped phase: %s", b2)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cigen/ -run TestDeployPhase_ScopedSecretsJSON -v`
Expected: FAIL (compile error — `Secrets`/`Scoped` undefined).

**Step 3: Implement**

Edit `cigen/plan.go` DeployPhase struct to:

```go
// DeployPhase is a single phase in a potentially multi-phase deploy pipeline.
type DeployPhase struct {
	// Name is the human-readable phase name (e.g. "prereq", "deploy").
	Name string `json:"name"`
	// ConfigPath is the workflow config file for this phase.
	ConfigPath string `json:"config_path"`
	// Include is an optional list of module names to include in this phase.
	Include []string `json:"include,omitempty"`
	// Secrets is the set of secrets this phase's apply job needs. Populated
	// only when Scoped is true; otherwise the renderer uses CIPlan.Secrets.
	Secrets []SecretRef `json:"secrets,omitempty"`
	// Scoped is true when per-phase secret derivation ran against a real,
	// loaded config for this phase. The renderer branches its env: source on
	// this flag — NOT on len(Secrets) — so a genuinely zero-secret scoped
	// phase emits no union, while an unscoped phase falls back to the union.
	Scoped bool `json:"scoped,omitempty"`
}
```

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./cigen/ -run TestDeployPhase_ScopedSecretsJSON -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cigen/plan.go cigen/plan_test.go
git commit -m "feat(cigen): add DeployPhase.Secrets + Scoped for per-phase scoping"
```

---

### Task 2: Analyze — per-phase secret scoping + migration env derivation

**Files:**
- Modify: `cigen/analyze.go` — `Analyze` (per-phase secrets), `derivePhases` (carry secrets/scoped), `deriveMigrations` (populate Env), `deriveWarnings` (ambiguity + unscopable notes)
- Test: `cigen/analyze_phase_test.go` (NEW — **`package cigen` internal**, NOT `cigen_test`)

**Change class:** internal logic (analyze pure function) → unit tests sufficient.

> **Why an internal test file (resolves plan-review C1/C2):** the existing `cigen/analyze_test.go` is `package cigen_test` (external). The new tests call **unexported** funcs (`deriveMigrations`, `deriveSecrets`, `deriveWarnings`) and `config.LoadFromFile`, so they MUST live in a `package cigen` (internal) file — from there the unexported symbols are reachable unqualified and the file declares its own `config`/`strings` imports. Do NOT append these to `analyze_test.go`.

**Step 1: Write the failing tests**

Create `cigen/analyze_phase_test.go`:

```go
package cigen

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestAnalyze_PerPhaseScoping_PrereqExcludesDeploySecret(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig: "testdata/multisite/deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(plan.Phases))
	}
	prereq, deploy := plan.Phases[0], plan.Phases[1]
	if !prereq.Scoped || !deploy.Scoped {
		t.Fatalf("expected both phases Scoped, got prereq=%v deploy=%v", prereq.Scoped, deploy.Scoped)
	}
	if hasSecret(prereq.Secrets, "MULTISITE_DB_URL") {
		t.Errorf("prereq phase must NOT carry the deploy-only migration secret MULTISITE_DB_URL; got %v", names(prereq.Secrets))
	}
	if !hasSecret(deploy.Secrets, "MULTISITE_DB_URL") {
		t.Errorf("deploy (last) phase must carry MULTISITE_DB_URL; got %v", names(deploy.Secrets))
	}
	// prereq genuinely needs the provider token — sanity that scoping isn't empty.
	if !hasSecret(prereq.Secrets, "DIGITALOCEAN_TOKEN") {
		t.Errorf("prereq phase should carry DIGITALOCEAN_TOKEN; got %v", names(prereq.Secrets))
	}
}

func TestAnalyze_SinglePhase_NotScoped(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Scoped {
		t.Errorf("single-phase deploy must not be Scoped (union is its scope)")
	}
}

func TestAnalyze_PhaseConfigAliasOnly_FallsBackToUnion(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig:      "/nonexistent/deploy.prereq.yaml",
		PhaseConfigAlias: "deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.Phases[0].Scoped {
		t.Errorf("alias-only/unloadable phase config must fall back (Scoped=false)")
	}
	if !containsSubstr(plan.Warnings, "per-phase secret scoping unavailable") {
		t.Errorf("expected an unscopable warning; got %v", plan.Warnings)
	}
}

func TestDeriveMigrations_SingleEnvDerived(t *testing.T) {
	cfg, err := config.LoadFromFile("testdata/migrations-one-env.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := deriveMigrations(cfg)
	if m == nil || m.Env != "prod" {
		t.Fatalf("expected Env=prod, got %+v", m)
	}
}

func TestDeriveMigrations_TwoEnvsAmbiguous(t *testing.T) {
	cfg, err := config.LoadFromFile("testdata/migrations-two-envs.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := deriveMigrations(cfg)
	if m == nil || m.Env != "" {
		t.Fatalf("expected Env empty (ambiguous), got %+v", m)
	}
	w := deriveWarnings(cfg, m, deriveSecrets(cfg, m))
	if !containsSubstr(w, "migrations environment ambiguous") {
		t.Errorf("expected ambiguity warning; got %v", w)
	}
}

// test helpers
func hasSecret(s []SecretRef, name string) bool {
	for _, r := range s {
		if r.Name == name {
			return true
		}
	}
	return false
}
func names(s []SecretRef) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, r.Name)
	}
	return out
}
func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
```

Add two fixtures. `cigen/testdata/migrations-one-env.yaml`:

```yaml
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: APP_DB_URL
      environments:
        prod:
          database:
            env: APP_DB_URL
```

`cigen/testdata/migrations-two-envs.yaml`:

```yaml
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: APP_DB_URL
      environments:
        staging:
          database:
            env: STAGING_DB_URL
        prod:
          database:
            env: PROD_DB_URL
```

(If `config.LoadFromFile` rejects a config with no `modules:`, add a minimal `modules: [{name: noop, type: http.server, config: {address: ":0"}}]` block to each fixture — verify by running the test; adjust until it loads.)

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cigen/ -run 'TestAnalyze_PerPhase|TestAnalyze_SinglePhase|TestAnalyze_PhaseConfigAlias|TestDeriveMigrations' -v`
Expected: FAIL (per-phase secrets not populated; Env not derived; warnings absent).

**Step 3: Implement**

In `cigen/analyze.go`:

(a) `deriveMigrations` — derive `Env` from a single `environments` key:

```go
func deriveMigrations(cfg *config.WorkflowConfig) *MigrationsSpec {
	if cfg.CI == nil || len(cfg.CI.Migrations) == 0 {
		return nil
	}
	m := cfg.CI.Migrations[0]
	spec := &MigrationsSpec{
		DBEnv:  m.Database.Env,
		Source: m.SourceDir,
	}
	if spec.DBEnv == "" {
		return nil
	}
	// Derive --env only when exactly one environment is declared (unambiguous).
	if len(m.Environments) == 1 {
		for envName := range m.Environments {
			spec.Env = envName
		}
	}
	return spec
}
```

(b) `Analyze` — compute per-phase secrets and pass into `derivePhases`. Replace the phases block (currently lines ~95-112) with:

```go
	primaryConfigPath := opts.ConfigPathAlias
	if primaryConfigPath == "" {
		primaryConfigPath = relativizeConfigPath(primaryPath)
	}

	// Per-phase secret scoping (multi-phase only). The prereq phase is scoped to
	// the secrets ITS config references; the deploy (last) phase keeps the
	// primary union (which already includes the migration DBEnv via deriveSecrets).
	var prereqSecrets []SecretRef
	scoped := false
	phaseConfigPath := opts.PhaseConfig
	if phaseConfigPath != "" {
		if opts.PhaseConfigAlias != "" {
			phaseConfigPath = opts.PhaseConfigAlias
		} else {
			phaseConfigPath = relativizeConfigPath(opts.PhaseConfig)
		}
		if pcfg, perr := config.LoadFromFile(opts.PhaseConfig); perr == nil {
			// deriveMigrations is primary-only: the migrating phase is the LAST
			// phase, so the prereq never gets a migration DBEnv (pass nil).
			prereqSecrets = deriveSecrets(pcfg, nil)
			scoped = true
		} else {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("per-phase secret scoping unavailable: phase config %q not loadable (%v); using union", opts.PhaseConfig, perr))
		}
	}

	plan.Phases = derivePhases(primaryConfigPath, phaseConfigPath, plan.Secrets, prereqSecrets, scoped)
```

(c) `derivePhases` — new signature carrying secrets/scoped:

```go
func derivePhases(primaryPath, phaseConfig string, primarySecrets, prereqSecrets []SecretRef, scoped bool) []DeployPhase {
	var phases []DeployPhase
	if phaseConfig != "" {
		phases = append(phases, DeployPhase{
			Name:       "prereq",
			ConfigPath: phaseConfig,
			Secrets:    prereqSecrets,
			Scoped:     scoped,
		})
	}
	deploy := DeployPhase{
		Name:       "deploy",
		ConfigPath: primaryPath,
	}
	// The deploy phase is scoped to the primary union only in the multi-phase
	// case (scoped==true means the prereq loaded). Single-phase stays unscoped:
	// the union IS its scope, so the renderer's union fallback applies unchanged.
	if scoped {
		deploy.Secrets = primarySecrets
		deploy.Scoped = true
	}
	phases = append(phases, deploy)
	return phases
}
```

(d) `deriveWarnings` — add the ambiguity warning:

```go
	// (c) migrations environment ambiguity: ≥2 declared → --env omitted.
	if cfg.CI != nil && len(cfg.CI.Migrations) > 0 {
		if n := len(cfg.CI.Migrations[0].Environments); n >= 2 {
			warnings = append(warnings,
				fmt.Sprintf("migrations environment ambiguous (%d declared); --env omitted — set it in the generated workflow", n))
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cigen/ -v`
Expected: PASS (all cigen tests, including the new ones AND the pre-existing golden tests).

**Step 5: Commit**

```bash
git add cigen/analyze.go cigen/analyze_phase_test.go cigen/testdata/migrations-one-env.yaml cigen/testdata/migrations-two-envs.yaml
git commit -m "feat(cigen): scope per-phase secrets in Analyze; derive single-env migrations --env"
```

---

### Task 3: render_gha — per-phase env block + --format json

**Files:**
- Modify: `cigen/render_gha.go` — `writeApplyJob` (~177-184), `migrationsUpCommand` (~227-233)
- Test: `cigen/render_gha_phase_test.go` (NEW — **`package cigen` internal**)

**Change class:** generator output (CI workflow content). Verification: render → parse-back YAML (`gopkg.in/yaml.v3`, a direct dep) + literal-substring asserts. **Rollback: revert the PR; `wfctl ci generate` reverts to union-scoping + bare-migrations output (additive field, JSON omitempty — old plan.json consumers unaffected).**

> **Why an internal test file (resolves plan-review C1):** `migrationsUpCommand` is unexported. The new test must be `package cigen`. The existing `render_gha_test.go` (`cigen_test`, external) helpers (`richCIPlan`, etc.) are NOT reachable from it — the new file is self-contained (parses YAML with `yaml.Unmarshal`, same lib the external tests use).
>
> **Pre-existing tests survive (verified):** `TestRenderGitHubActions_MigrationsStep` (render_gha_test.go:82) and `_WithEnv` (:128) assert with `strings.Contains(...,"wfctl migrations up --config")` / `"--env prod"` — substring checks that remain true after ` --format json` is appended. `richCIPlan()`-built plans leave `Scoped=false` → union fallback → secret-env tests unchanged. No edits to the existing external tests are required; the Task 4 full-package run confirms it.

**Step 1: Write the failing tests**

Create `cigen/render_gha_phase_test.go`:

```go
package cigen

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// jobEnv returns the rendered `env:` map for a named job, parsed from YAML.
func jobEnv(t *testing.T, yml, job string) map[string]any {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			Env map[string]any `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(yml), &doc); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, yml)
	}
	j, ok := doc.Jobs[job]
	if !ok {
		t.Fatalf("job %q not found in:\n%s", job, yml)
	}
	return j.Env
}

func TestRenderGHA_PerPhaseEnvScoping(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig: "testdata/multisite/deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	files, err := RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var yml string
	for _, c := range files {
		yml = c
	}
	prereqEnv := jobEnv(t, yml, "apply-prereq") // also asserts valid YAML
	deployEnv := jobEnv(t, yml, "apply-deploy")
	if _, ok := prereqEnv["MULTISITE_DB_URL"]; ok {
		t.Errorf("apply-prereq env must NOT contain MULTISITE_DB_URL; got %v", prereqEnv)
	}
	if _, ok := deployEnv["MULTISITE_DB_URL"]; !ok {
		t.Errorf("apply-deploy env must contain MULTISITE_DB_URL; got %v", deployEnv)
	}
}

func TestMigrationsUpCommand_AlwaysFormatJSON(t *testing.T) {
	if got := migrationsUpCommand("deploy.yaml", ""); got != "wfctl migrations up --config 'deploy.yaml' --format json" {
		t.Errorf("no-env: got %q", got)
	}
	if got := migrationsUpCommand("deploy.yaml", "prod"); got != "wfctl migrations up --config 'deploy.yaml' --env prod --format json" {
		t.Errorf("with-env: got %q", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cigen/ -run 'TestRenderGHA_PerPhaseEnvScoping|TestMigrationsUpCommand' -v`
Expected: FAIL (apply-prereq currently carries the full union incl MULTISITE_DB_URL; migrations command lacks `--format json`).

**Step 3: Implement**

In `cigen/render_gha.go` `writeApplyJob`, replace the secrets env block (currently `if len(p.Secrets) > 0 { … p.Secrets … }`) with:

```go
	// Secrets env block. Branch the SOURCE on phase.Scoped (NOT len): a scoped
	// phase uses its own subset (possibly empty → no env block); an unscoped
	// phase falls back to the plan-wide union.
	secrets := p.Secrets
	if phase.Scoped {
		secrets = phase.Secrets
	}
	if len(secrets) > 0 {
		b.WriteString("    env:\n")
		for _, s := range secrets {
			fmt.Fprintf(b, "      %s: ${{ secrets.%s }}\n", s.Name, s.Name)
		}
	}
```

And `migrationsUpCommand` — append `--format json` unconditionally (after `--env`):

```go
// migrationsUpCommand builds the `wfctl migrations up` invocation. `--env <env>`
// is included only when MigrationsSpec.Env is set; `--format json` is always
// appended (machine-readable output; matches the deployed multisite workflow).
func migrationsUpCommand(configPath, env string) string {
	cmd := fmt.Sprintf("wfctl migrations up --config '%s'", configPath)
	if env != "" {
		cmd += fmt.Sprintf(" --env %s", env)
	}
	cmd += " --format json"
	return cmd
}
```

Also update the comment block above the migrations step (render_gha.go ~208-213): the DBEnv is now in the *last phase's* scoped `env:` (still present, since the deploy phase carries the primary union) — keep the "no step-level env needed" rationale but reword "secrets union" → "the last phase's env block".

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cigen/ -v`
Expected: PASS (all cigen tests).

**Step 5: Commit**

```bash
git add cigen/render_gha.go cigen/render_gha_phase_test.go
git commit -m "feat(cigen): per-phase env block + wfctl migrations up --format json"
```

---

### Task 4: golangci-lint + full-package gate

**Files:** none (verification task)

**Change class:** Go-repo code change → lint gate before push.

**Step 1:** Run the full cigen package (not a `-run` filter — pre-existing golden tests must stay green):

Run: `GOWORK=off go test ./cigen/...`
Expected: `ok  github.com/GoCodeAlone/workflow/cigen`

**Step 2:** Build wfctl (the consumer surface) to confirm no break:

Run: `GOWORK=off go build ./cmd/wfctl`
Expected: exit 0.

**Step 3:** Lint only the changed lines:

Run: `GOWORK=off golangci-lint run --new-from-rev=origin/main ./cigen/...`
Expected: exit 0.

**Step 4: Commit** (only if lint auto-fixes or doc tweaks were needed; otherwise skip).

---

### Task 5: Regenerate multisite evidence + honest GAP.md

**Files:**
- Create: `cigen/multisite_evidence_test.go` (NEW — **`package cigen` internal** on-disk golden test)
- Regenerate: `cigen/testdata/multisite/plan.json`, `cigen/testdata/multisite/generated-infra.yml`
- Modify: `cigen/testdata/multisite/GAP.md`

**Change class:** generator output artifact (demonstration-fidelity). Verification: real binary regen + measured diff + **on-disk golden test** (resolves plan-review Important: no existing test reads these committed files; this adds one, satisfying the "verify the artifact behaves" mandate); literal output committed.

**Step 0: Write the on-disk golden test FIRST (fails against the current buggy committed evidence).**

Create `cigen/multisite_evidence_test.go`:

```go
package cigen

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMultisiteEvidence_Honest locks the committed demonstration-fidelity
// artifact. It reads the on-disk generated-infra.yml (NOT a freshly rendered
// plan) and asserts exactly the honest claims in GAP.md: apply-prereq is
// scoped (no deploy-only DB secret), apply-deploy keeps it, and the migrations
// step carries --format json but NOT --env (multisite declares no environments).
func TestMultisiteEvidence_Honest(t *testing.T) {
	b, err := os.ReadFile("testdata/multisite/generated-infra.yml")
	if err != nil {
		t.Fatalf("read committed evidence: %v", err)
	}
	yml := string(b)

	var doc struct {
		Jobs map[string]struct {
			Env map[string]any `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("committed evidence is not valid YAML: %v", err)
	}
	if _, ok := doc.Jobs["apply-prereq"].Env["MULTISITE_DB_URL"]; ok {
		t.Errorf("apply-prereq must NOT carry the deploy-only MULTISITE_DB_URL (scoping gap #3)")
	}
	if _, ok := doc.Jobs["apply-deploy"].Env["MULTISITE_DB_URL"]; !ok {
		t.Errorf("apply-deploy must carry MULTISITE_DB_URL")
	}
	if !strings.Contains(yml, "wfctl migrations up --config 'deploy.yaml' --format json") {
		t.Errorf("migrations step must emit --format json (gap #4)")
	}
	if strings.Contains(yml, "migrations up") && strings.Contains(yml, "--env ") {
		t.Errorf("multisite declares no ci.migrations.environments → --env must NOT be emitted (honest, design C1)")
	}
}
```

Run: `GOWORK=off go test ./cigen/ -run TestMultisiteEvidence_Honest -v`
Expected: FAIL (current committed `generated-infra.yml` has the union in apply-prereq incl `MULTISITE_DB_URL`, and a bare `migrations up` with no `--format json`).

**Step 1: Regenerate the evidence with the REAL CLI flags (the exact recipe documented in `GAP.md` "How this was produced" — resolves plan-review C3).** `ci plan` has no `--format` (output is JSON to `--out`, `-`=stdout); `ci generate` writes files to `--out`/`--output` dir with `--write` (there is NO `--stdout`/`--output-dir` flag). From the repo root of the worktree:

```bash
GOWORK=off go run ./cmd/wfctl ci plan \
  -c cigen/testdata/multisite/deploy.yaml \
  --phase-config cigen/testdata/multisite/deploy.prereq.yaml \
  --config-path-alias deploy.yaml \
  --phase-config-alias deploy.prereq.yaml \
  --out cigen/testdata/multisite/plan.json

GOWORK=off go run ./cmd/wfctl ci generate \
  -c cigen/testdata/multisite/deploy.yaml \
  --phase-config cigen/testdata/multisite/deploy.prereq.yaml \
  --config-path-alias deploy.yaml \
  --phase-config-alias deploy.prereq.yaml \
  --platform github_actions \
  --out cigen/testdata/multisite \
  --write
# emits cigen/testdata/multisite/.github/workflows/multisite.yml — move it:
mv cigen/testdata/multisite/.github/workflows/multisite.yml cigen/testdata/multisite/generated-infra.yml
rm -rf cigen/testdata/multisite/.github
```

(These are the flags verified in `cmd/wfctl/ci_plan.go:13-22` and `cmd/wfctl/ci.go:72-86`. Do NOT hand-edit the output to fake the shape — if the diff is wrong the implementation is wrong.)

**Step 2:** Verify the measured diff (the honest claim):

Run: `git --no-pager diff cigen/testdata/multisite/generated-infra.yml`
Expected diff content:
- `apply-prereq` `env:` block **no longer contains** `MULTISITE_DB_URL` (and any other deploy-only secrets the prereq does not reference).
- `apply-deploy` `env:` block **still contains** `MULTISITE_DB_URL`.
- The `Run migrations` step changes from `wfctl migrations up --config 'deploy.yaml'` to `wfctl migrations up --config 'deploy.yaml' --format json` — **no `--env`** (multisite declares no `environments:`).

If the diff does not match these expectations, STOP — the implementation is wrong, not the evidence. Do not edit the committed output to force the expected shape.

**Step 3:** Update `cigen/testdata/multisite/GAP.md` — HONEST (design C1):
- **Move to "now derivable / matched":** per-phase secret scoping (apply-prereq no longer carries the deploy-only DB secret) + `--format json` on the migrations step.
- **KEEP in "not derivable":** migrations `--env <env>` (requires an `environments:` block multisite does not declare) + the hash-suffixed DB secret name, GHCR image-wait, GHCR creds, GA4 step, smoke matrix, concurrency, SHA-pins.
- State the claim is exactly the measured diff — never "matches the hand-tuned `--env prod`".

**Step 4:** Re-run the package — the on-disk golden test from Step 0 now PASSES against the regenerated evidence, and the whole package stays green:

Run: `GOWORK=off go test ./cigen/...`
Expected: PASS (incl `TestMultisiteEvidence_Honest`).

**Step 5: Commit**

```bash
git add cigen/multisite_evidence_test.go cigen/testdata/multisite/plan.json cigen/testdata/multisite/generated-infra.yml cigen/testdata/multisite/GAP.md
git commit -m "test(cigen): regen multisite evidence (scoped prereq env + migrations --format json) + on-disk golden test; honest GAP.md"
```

---

## Global Design Guidance

Source: `docs/design-guidance.md` (workspace). Dogfood (improves `wfctl ci generate`); reuse over rebuild (reuses `deriveSecrets`/`MigrationsSpec.Env`/render scaffolding); secrets never logged (scoping tightens blast radius; names only); multi-component/real-consumer proof (multisite real-config regen + measured diff).

## Verification per change class

- Task 1: internal logic → unit test (JSON round-trip).
- Task 2: internal logic → unit tests (per-phase scoping, env derivation, ambiguity/unscopable warnings) + pre-existing golden tests stay green.
- Task 3: generator output → render + YAML parse-back + literal-substring asserts. Rollback note inline.
- Task 4: Go-repo gate → full-package `go test`, `go build ./cmd/wfctl`, `golangci-lint run --new-from-rev=origin/main`.
- Task 5: generator artifact (demonstration-fidelity) → real-binary regen + measured diff + honest GAP.md; literal output committed.

## Rollback

Revert the PR. `wfctl ci generate` reverts to union-scoping + bare-migrations output. `DeployPhase.{Secrets,Scoped}` are additive (`json:",omitempty"`) — old `plan.json` consumers are unaffected. No release, no migration, no state.
