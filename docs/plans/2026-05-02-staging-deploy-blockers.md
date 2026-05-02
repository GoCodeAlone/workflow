# Staging deploy blockers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Get core-dump's deploy chain from "passes plan + align" through to "staging app responds to /healthz" by fixing three known upstream blockers and iterating on whatever surfaces, all on wfctl + workflow-plugin-{digitalocean,migrations} with no doctl/openssl/gh-secret fallback.

**Architecture:** Two independent upstream fixes that ship as new releases (workflow + workflow-plugin-migrations), each followed by a downstream version-pin bump in core-dump. After both upstream fixes land, re-run the deploy chain end-to-end and iterate on each new failure with the same upstream-first discipline. PR-A (workflow #154 — `${VAR}` preservation in env_vars submaps) and PR-B (workflow-plugin-migrations #150 + #513 — `--up-if-clean` on `up` + atlas Executor recover) are independent and may run in parallel.

**Tech Stack:** Go (workflow + workflow-plugin-migrations + workflow-plugin-digitalocean), YAML (infra.yaml + workflow YAMLs), GitHub Actions (deploy/bootstrap workflows), Cobra (workflow-migrate CLI), atlas-go (`ariga.io/atlas/sql/migrate`), DigitalOcean App Platform + managed Postgres.

---

### Task 1: PR-A1 — workflow `${VAR}` preservation in env_vars submaps (Solution A for #154)

Repo: `GoCodeAlone/workflow`. Branch: `fix/env-vars-preserve-in-plan` (fresh, off origin/main; do NOT use `design/staging-deploy-blockers`). Worktree: `/Users/jon/workspace/workflow/_worktrees/fix-env-vars-preservation/`.

Reference: `docs/plans/2026-05-02-staging-deploy-blockers-design.md` Section "Blocker 1".

**Files:**
- Modify: `config/env_expand.go` (add `ExpandEnvInMapPreservingKeys` + `ExpandEnvInValuePreservingKeys` helpers)
- Modify: `cmd/wfctl/infra.go:255,301,346,829` (switch the four call sites to the new variant with `["env_vars", "env_vars_secret", "secret_env_vars"]` preserve-list)
- Test: `config/env_expand_test.go` (new: literal-preservation tests for env_vars submaps)
- Test: `cmd/wfctl/infra_plan_env_vars_preserve_test.go` (new: integration test against a fixture with env_vars references)
- Create: `cmd/wfctl/testdata/infra-with-env-var-refs.yaml` (fixture)

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow
git fetch origin main
git worktree add _worktrees/fix-env-vars-preservation origin/main -b fix/env-vars-preserve-in-plan
cd _worktrees/fix-env-vars-preservation
```

Expected: worktree on fresh branch tracking origin/main at commit `60aa8471` (or later).

**Step 2: Write failing test for `ExpandEnvInMapPreservingKeys`**

Edit `config/env_expand_test.go` (create if absent — search `config/*_test.go` for existing patterns first). Add:

```go
func TestExpandEnvInMapPreservingKeys_PreservesEnvVarsSubmap(t *testing.T) {
	t.Setenv("MY_TOKEN", "actual-secret-value")
	t.Setenv("OTHER", "resolved-other")
	in := map[string]any{
		"name": "myapp",
		"region": "${OTHER}",  // top-level: should resolve
		"env_vars": map[string]any{
			"AUTH_TOKEN": "${MY_TOKEN}",  // inside env_vars: should PRESERVE literal
			"PORT":       "8080",          // no var ref, preserved as-is
		},
		"env_vars_secret": map[string]any{
			"DB_URL": "${OTHER}",  // inside env_vars_secret: PRESERVE
		},
	}
	out := ExpandEnvInMapPreservingKeys(in, []string{"env_vars", "env_vars_secret", "secret_env_vars"})
	if got := out["region"]; got != "resolved-other" {
		t.Errorf("top-level region: got %q, want resolved-other", got)
	}
	envVars := out["env_vars"].(map[string]any)
	if got := envVars["AUTH_TOKEN"]; got != "${MY_TOKEN}" {
		t.Errorf("env_vars.AUTH_TOKEN: got %q, want literal ${MY_TOKEN}", got)
	}
	envVarsSecret := out["env_vars_secret"].(map[string]any)
	if got := envVarsSecret["DB_URL"]; got != "${OTHER}" {
		t.Errorf("env_vars_secret.DB_URL: got %q, want literal ${OTHER}", got)
	}
}

func TestExpandEnvInMapPreservingKeys_NestedNonPreservedSubmapsStillResolve(t *testing.T) {
	t.Setenv("DEEP", "deep-value")
	in := map[string]any{
		"services": map[string]any{
			"api": map[string]any{
				"image": "${DEEP}",  // not in preserve list: should resolve
			},
		},
	}
	out := ExpandEnvInMapPreservingKeys(in, []string{"env_vars"})
	got := out["services"].(map[string]any)["api"].(map[string]any)["image"]
	if got != "deep-value" {
		t.Errorf("services.api.image: got %q, want deep-value", got)
	}
}

func TestExpandEnvInMapPreservingKeys_EmptyPreserveListEqualsExpandEnvInMap(t *testing.T) {
	t.Setenv("V", "vv")
	in := map[string]any{"k": "${V}"}
	out := ExpandEnvInMapPreservingKeys(in, []string{})
	if out["k"] != "vv" {
		t.Errorf("with empty preserve list, behavior should equal ExpandEnvInMap; got %q", out["k"])
	}
}
```

**Step 3: Run test to verify it fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./config/ -run TestExpandEnvInMapPreservingKeys -v
```

Expected: FAIL with `ExpandEnvInMapPreservingKeys undefined`.

**Step 4: Implement `ExpandEnvInMapPreservingKeys` in `config/env_expand.go`**

Append to `config/env_expand.go`:

```go
// ExpandEnvInMapPreservingKeys is like ExpandEnvInMap but when a key in
// preserveKeys is encountered, the corresponding value (and any nested
// content inside it) is left untouched — ${VAR} / $VAR references are
// preserved literally instead of being substituted from the process env.
//
// Use case: plan-time serialization of resource specs where certain
// submaps (env_vars, env_vars_secret, secret_env_vars) carry secret
// references that should resolve only at apply time. Without this,
// security-check rules see resolved literals and incorrectly flag them
// as accidentally-pasted secret values.
//
// preserveKeys is matched case-sensitively against the immediate map key
// at every depth. An empty or nil preserveKeys list makes this function
// behave identically to ExpandEnvInMap.
func ExpandEnvInMapPreservingKeys(m map[string]any, preserveKeys []string) map[string]any {
	if m == nil {
		return nil
	}
	preserve := make(map[string]struct{}, len(preserveKeys))
	for _, k := range preserveKeys {
		preserve[k] = struct{}{}
	}
	return expandEnvInMapWithPreserve(m, preserve)
}

func expandEnvInMapWithPreserve(m map[string]any, preserve map[string]struct{}) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, isPreserved := preserve[k]; isPreserved {
			// Copy the value verbatim. For maps/slices, deep-copy so callers
			// can mutate without aliasing back into the source. Strings and
			// scalars are immutable; pass through.
			out[k] = deepCopyValue(v)
			continue
		}
		out[k] = expandEnvInValueWithPreserve(v, preserve)
	}
	return out
}

func expandEnvInValueWithPreserve(v any, preserve map[string]struct{}) any {
	switch val := v.(type) {
	case string:
		return os.ExpandEnv(val)
	case map[string]any:
		return expandEnvInMapWithPreserve(val, preserve)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = expandEnvInValueWithPreserve(item, preserve)
		}
		return out
	default:
		return v
	}
}

// deepCopyValue copies a value preserving its structure. Maps and slices
// are recursively copied; scalars (string, int, bool, nil) are returned
// as-is. Used by ExpandEnvInMapPreservingKeys to insulate preserved
// subtrees from caller mutation.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = deepCopyValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return v
	}
}
```

**Step 5: Run test to verify it passes**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./config/ -run TestExpandEnvInMapPreservingKeys -v
```

Expected: all 3 cases PASS.

**Step 6: Run full config suite to verify no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./config/...
```

Expected: PASS (existing ExpandEnvInMap behavior unchanged).

**Step 7: Switch the four infra.go call sites to use the new variant**

Find each call site:
```bash
grep -n 'config.ExpandEnvInMap(m.Config\|config.ExpandEnvInMap(resolved.Config\|config.ExpandEnvInMap(m.Config)' /Users/jon/workspace/workflow/_worktrees/fix-env-vars-preservation/cmd/wfctl/infra.go
```

Expected: 4 lines (currently 255, 301, 346, 829 per the design doc; verify actual line numbers since file may have drifted).

For each, replace:
```go
config.ExpandEnvInMap(SOMETHING)
```
with:
```go
config.ExpandEnvInMapPreservingKeys(SOMETHING, infraPreserveKeys)
```

Add at the top of `cmd/wfctl/infra.go` (after imports, at package level):
```go
// infraPreserveKeys lists the submap keys whose contents should be left
// as ${VAR} literals through plan serialization. Apply-time injection
// (per the existing pattern in deploy_providers.go + driver Apply
// methods) resolves them when the plugin actually creates/updates the
// resource.
//
// Why these three keys:
//   - env_vars: App Platform service env vars that downstream consumers
//     reference in YAML as ${VAR}.
//   - env_vars_secret: canonical secret-typed env vars per
//     workflow-plugin-digitalocean's envVarsFromConfig.
//   - secret_env_vars: legacy alias for env_vars_secret kept for
//     backwards compat (same source).
//
// This preservation is the fix for core-dump#154 (R4 fired on
// env_vars["NATS_AUTH_TOKEN"] because the secret had been eagerly
// resolved into the plan output). See
// docs/plans/2026-05-02-staging-deploy-blockers-design.md.
var infraPreserveKeys = []string{"env_vars", "env_vars_secret", "secret_env_vars"}
```

**Step 8: Write failing integration test for plan serialization**

Create `cmd/wfctl/testdata/infra-with-env-var-refs.yaml`:

```yaml
infra:
  auto_bootstrap: true
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
  - name: example-app
    type: infra.container_service
    provider: digitalocean
    config:
      services:
        - name: api
          image: "${IMAGE_REF}"
          env_vars:
            DEPLOY_ENV: staging
            AUTH_TOKEN: "${AUTH_TOKEN}"
          env_vars_secret:
            DB_URL: "${DATABASE_URL}"
```

Create `cmd/wfctl/infra_plan_env_vars_preserve_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInfraResourceSpecs_PreservesEnvVarRefs(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "actual-do-token")
	t.Setenv("IMAGE_REF", "registry.example.com/api:abc123")
	t.Setenv("AUTH_TOKEN", "would-be-resolved-secret")
	t.Setenv("DATABASE_URL", "postgres://would-be-resolved")

	specs, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	var app map[string]any
	for _, s := range specs {
		if s.Name == "example-app" {
			b, _ := json.Marshal(s.Config)
			_ = json.Unmarshal(b, &app)
			break
		}
	}
	if app == nil {
		t.Fatal("example-app spec not found")
	}

	services := app["services"].([]any)
	api := services[0].(map[string]any)

	// Top-level field IS resolved.
	if got := api["image"]; got != "registry.example.com/api:abc123" {
		t.Errorf("api.image: got %q, want resolved literal", got)
	}

	// env_vars contents are PRESERVED.
	envVars := api["env_vars"].(map[string]any)
	if got := envVars["AUTH_TOKEN"]; got != "${AUTH_TOKEN}" {
		t.Errorf("env_vars.AUTH_TOKEN: got %q, want literal ${AUTH_TOKEN}", got)
	}
	if got := envVars["DEPLOY_ENV"]; got != "staging" {
		t.Errorf("env_vars.DEPLOY_ENV: got %q, want literal staging", got)
	}

	// env_vars_secret contents are PRESERVED.
	envSecret := api["env_vars_secret"].(map[string]any)
	if got := envSecret["DB_URL"]; got != "${DATABASE_URL}" {
		t.Errorf("env_vars_secret.DB_URL: got %q, want literal", got)
	}
}

// ensure the testdata file exists (catches a refactor that moves it)
func TestPlanEnvVarPreserveTestdataExists(t *testing.T) {
	if _, err := os.Stat(filepath.Join("testdata", "infra-with-env-var-refs.yaml")); err != nil {
		t.Fatalf("testdata fixture missing: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join("testdata", "infra-with-env-var-refs.yaml"))
	if !strings.Contains(string(b), "env_vars_secret") {
		t.Errorf("fixture missing env_vars_secret block — needed for preservation test")
	}
}
```

**Step 9: Run integration test to verify it passes**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestParseInfraResourceSpecs_PreservesEnvVarRefs -v
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestPlanEnvVarPreserveTestdataExists -v
```

Expected: both PASS. If parse-time substitution still happens, AUTH_TOKEN would equal "would-be-resolved-secret" instead of "${AUTH_TOKEN}".

**Step 10: Run full wfctl + config test suites — no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/... ./config/...
```

Expected: PASS across both packages.

**Step 11: Commit + push + open PR**

```bash
git add config/env_expand.go config/env_expand_test.go cmd/wfctl/infra.go cmd/wfctl/infra_plan_env_vars_preserve_test.go cmd/wfctl/testdata/infra-with-env-var-refs.yaml
git commit -m "$(cat <<'EOF'
fix(wfctl): preserve ${VAR} literals in env_vars submaps through plan

Solution A from docs/plans/2026-05-02-staging-deploy-blockers-design.md
for the diagnostic that surfaced as core-dump#154: security-check rule
R4 was incorrectly flagging env_vars[K]=${VAR_REF} as a "potential
secret literal" because parse-time env-var expansion had already
substituted the value into the plan output. R4 explicitly skips values
containing `${`, but the substitution had already happened.

Add a preserve-keys variant to ExpandEnvInMap that recursively walks
the config tree but leaves named submap contents untouched. Switch the
four cmd/wfctl/infra.go call sites to the new variant with the preserve
list ["env_vars", "env_vars_secret", "secret_env_vars"]. Apply-time
injection (already present in deploy_providers.go + driver Apply paths)
resolves them when the plugin creates/updates resources.

Side effect: plan.json now contains ${VAR} literals in env_vars
submaps. State store unaffected (state is post-apply, fully resolved).
Existing plans (from before this change, fully resolved) still apply
correctly because apply-side already does ExpandEnvInMap.

Tests:
- config/env_expand_test.go: 3 unit cases (preserved submap, nested
  non-preserved still resolves, empty-list backwards-compat).
- cmd/wfctl/infra_plan_env_vars_preserve_test.go: integration against
  a fixture with env_vars + env_vars_secret references.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push -u origin fix/env-vars-preserve-in-plan
gh pr create --title "fix(wfctl): preserve \${VAR} literals in env_vars submaps through plan" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Solution A for [core-dump#154](https://github.com/GoCodeAlone/core-dump/issues/154): security-check R4 was firing on `env_vars["NATS_AUTH_TOKEN"]` because parse-time env-var expansion substituted `${NATS_AUTH_TOKEN}` → resolved 64-char hex token into the plan output before R4's `${`-skip could fire.

This PR adds a preserve-keys variant to `ExpandEnvInMap` and switches the four `cmd/wfctl/infra.go` call sites to it with `["env_vars", "env_vars_secret", "secret_env_vars"]`. Apply-time injection already exists for these fields; this just defers the resolution to where it belongs.

Reference: [`docs/plans/2026-05-02-staging-deploy-blockers-design.md`](../blob/design/staging-deploy-blockers/docs/plans/2026-05-02-staging-deploy-blockers-design.md) (workflow repo).

## Changes

- `config/env_expand.go`: add `ExpandEnvInMapPreservingKeys(m, preserveKeys)` + helpers `expandEnvInMapWithPreserve`, `expandEnvInValueWithPreserve`, `deepCopyValue`.
- `cmd/wfctl/infra.go`: switch 4 call sites; introduce `infraPreserveKeys` package var.
- `config/env_expand_test.go`: 3 new unit cases.
- `cmd/wfctl/infra_plan_env_vars_preserve_test.go` + fixture: integration test against parse path.

## Test plan

- [x] `go test ./config/...` green
- [x] `go test ./cmd/wfctl/...` green
- [ ] CI green (note: workflow#516 lint drift will fail; pre-existing)
- [ ] After merge + release: re-run core-dump deploy; security-check passes for env_vars-with-secret-references

## Backwards compatibility

`ExpandEnvInMap` unchanged (still public). `ExpandEnvInMapPreservingKeys` is additive. Plan serialization changes shape only for env_vars/env_vars_secret/secret_env_vars submaps; other plan content unchanged. Existing apply-side consumers already handle ${VAR} in these fields via the existing ExpandEnvInMap call at apply time (per the comment at infra_env_resolve.go:88).

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If `--reviewer "Copilot"` errors with "Could not resolve user with login 'copilot'", retry via API: `gh api -X POST /repos/GoCodeAlone/workflow/pulls/<N>/requested_reviewers -f 'reviewers[]=Copilot'`.

DM team-lead on push complete with branch + commit SHA + PR URL.

**Verification class:** internal logic refactor (per matrix). Unit + integration tests green is sufficient; class doesn't require runtime-launch validation. End-to-end runtime validation happens in Task 2 after the new wfctl version pin lands in core-dump.

---

### Task 2: PR-A2 — bump core-dump wfctl pin to new workflow release containing PR-A1

Repo: `GoCodeAlone/core-dump`. Branch: `chore/bump-wfctl-pin-after-env-vars-fix`. Worktree: `/Users/jon/workspace/core-dump/_worktrees/bump-wfctl-pin-A2/`.

PREREQUISITE: PR-A1 merged + workflow release tag containing it exists. Confirm with `gh release list --repo GoCodeAlone/workflow --limit 3`. The new release will be either v0.20.4 (patch) or v0.21.0 (minor). Substitute NEW_TAG below.

**Files:**
- Modify: `.github/workflows/{deploy,bootstrap,teardown,registry-retention}.yml` (4 files; one `version: v0.20.X` line each)
- Modify: `infra.yaml` (engine compat header comment)

**Step 1: Confirm release exists + cut a new workflow release if not**

```bash
gh release list --repo GoCodeAlone/workflow --limit 3
```

If no release contains PR-A1's commit yet, the workflow release process needs to fire first. Per workspace memory's experience with v0.20.3, the release.yml workflow auto-fires on tag push. If the team-lead hasn't already cut a new tag at this point, DM team-lead and pause.

Confirm the new tag's target commit:
```bash
gh api repos/GoCodeAlone/workflow/git/refs/tags/<NEW_TAG> --jq '.object.sha'
gh api repos/GoCodeAlone/workflow/git/tags/<TAG_OBJ_SHA> --jq '{message: .message[:80], target: .object.sha}'
```

The target should be PR-A1's merge commit (or later).

**Step 2: Set up worktree**

```bash
cd /Users/jon/workspace/core-dump
git fetch origin main
git worktree add _worktrees/bump-wfctl-pin-A2 origin/main -b chore/bump-wfctl-pin-after-env-vars-fix
cd _worktrees/bump-wfctl-pin-A2
```

**Step 3: Bulk-replace version pins**

```bash
NEW_TAG=v0.20.4  # or v0.21.0 — substitute the actual new tag
for f in .github/workflows/{deploy,bootstrap,teardown,registry-retention}.yml; do
  sed -i.bak "s/version: v0\.20\.3/version: $NEW_TAG/g" "$f" && rm "$f.bak"
done
sed -i.bak "s/wfctl v0\.20\.3+/wfctl $NEW_TAG+/g" infra.yaml && rm infra.yaml.bak
```

**Step 4: Verify diff**

```bash
git diff --stat
git diff
```

Expected: 5 files; ~6 insertions; ~6 deletions; only the version-pin lines touched.

**Step 5: Commit + push + open PR**

```bash
git add .github/workflows/ infra.yaml
git commit -m "chore: bump wfctl pin to <NEW_TAG> for env_vars preservation fix

Picks up workflow PR <PR-A1-NUMBER>: ${VAR} preservation in env_vars
submaps through plan serialization. Resolves core-dump#154 R4 false
positive on NATS_AUTH_TOKEN.

See workflow/docs/plans/2026-05-02-staging-deploy-blockers-design.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
"
git push -u origin chore/bump-wfctl-pin-after-env-vars-fix
gh pr create --title "chore: bump wfctl pin to <NEW_TAG>" --reviewer "Copilot" --body "Picks up workflow PR <PR-A1-NUMBER> (env_vars preservation fix for #154). Pure version-pin bump."
```

**Step 6: Per workspace memory feedback_version_bump_immediate_merge — admin-merge in same turn**

Once CI is green, admin-merge with `--squash --admin --delete-branch`. No Copilot review wait required for version-bump PRs. (Lint may fail on workflow#516 drift — that's downstream of the workflow main-branch lint failures, not relevant to core-dump CI; only core-dump CI matters here.)

**Step 7: Post-merge verification — re-run Deploy + observe security-check**

```bash
# Wait for CI on the new main commit to finish; then Deploy auto-fires via workflow_run
gh run list --repo GoCodeAlone/core-dump --workflow Deploy --branch main --limit 1
# Watch the new Deploy run
gh run watch <deploy-run-id> --repo GoCodeAlone/core-dump
```

Expected:
- build-image: success (no behavior change)
- deploy-staging plan + align: success (no change)
- deploy-staging security-check: should now PASS (R4 no longer fires on env_vars["NATS_AUTH_TOKEN"] because the value is `${NATS_AUTH_TOKEN}` literal in plan.json, not the resolved hex token; R4's `${`-skip fires correctly)
- deploy-staging apply: now reached for the first time! Will likely surface NEW failures (Task 5 below).

**Verification class:** version pin update (per matrix). Run version-skew audit per `superpowers:finishing-a-development-branch` Step 1c. Relaunch artifact = the auto-fired Deploy run on main; transcript captured + audit clean.

---

### Task 3: PR-B1 — workflow-plugin-migrations `--up-if-clean` on `up` + atlas Executor recover wrapper

Repo: `GoCodeAlone/workflow-plugin-migrations`. Branch: `feat/up-if-clean-and-atlas-recover` (fresh, off origin/main). Worktree: `/Users/jon/workspace/workflow-plugin-migrations/_worktrees/up-if-clean-and-recover/`.

Reference: `workflow/docs/plans/2026-05-02-staging-deploy-blockers-design.md` Sections "Blocker 2" + "Blocker 3".

**Files:**
- Modify: `pkg/cli/root.go` (add `--up-if-clean` flag to `up` cobra cmd; thread into the buildDriverAndRequest path)
- Modify: `internal/atlas/driver.go` (add `defer recover()` wrappers around `ex.ExecuteN(ctx, 0)` at line ~51, `ex.Pending(ctx)` at line ~158, and any other Execute call in Down at line ~209)
- Modify: `internal/golangmigrate/driver.go` (also support up-if-clean semantics for the golangmigrate driver)
- Test: `pkg/cli/root_test.go` (test that `up --up-if-clean` accepts the flag + behaves correctly)
- Test: `internal/atlas/driver_test.go` (test that recover wraps panic → typed error)
- Modify: `CHANGELOG.md` (entry under Unreleased)

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow-plugin-migrations
git fetch origin main
git worktree add _worktrees/up-if-clean-and-recover origin/main -b feat/up-if-clean-and-atlas-recover
cd _worktrees/up-if-clean-and-recover
```

**Step 2: Write failing test for `up --up-if-clean` flag acceptance**

Edit `pkg/cli/root_test.go`. Add:

```go
func TestUpCmd_AcceptsUpIfCleanFlag(t *testing.T) {
	cmd := newUpCmd()
	flag := cmd.Flags().Lookup("up-if-clean")
	if flag == nil {
		t.Fatal("up command missing --up-if-clean flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("up-if-clean default: got %q, want false", flag.DefValue)
	}
}

func TestUpCmd_UpIfCleanIsNoopWhenAlreadyClean(t *testing.T) {
	// Stub the driver to return zero applied migrations (clean state)
	stubDriver := &fakeUpDriver{appliedCount: 0}
	old := buildDriverAndRequestForTest
	buildDriverAndRequestForTest = func(cmd *cobra.Command) (interfaces.MigrationDriver, interfaces.MigrationRequest, error) {
		return stubDriver, interfaces.MigrationRequest{}, nil
	}
	defer func() { buildDriverAndRequestForTest = old }()

	cmd := newUpCmd()
	cmd.SetArgs([]string{"--up-if-clean"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("up --up-if-clean against clean DB: got error %v, want nil", err)
	}
}
```

If `buildDriverAndRequestForTest` doesn't exist as a test seam, introduce it in the implementation step. Pattern: `var buildDriverAndRequestForTest = realBuildDriverAndRequest` at package level; tests override + restore via defer.

**Step 3: Run test — confirm fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./pkg/cli/ -run TestUpCmd -v
```

Expected: FAIL ("up command missing --up-if-clean flag" + "buildDriverAndRequestForTest undefined").

**Step 4: Implement — add `--up-if-clean` to `up` cmd**

Modify `pkg/cli/root.go newUpCmd()` (current at line 99):

```go
func newUpCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "up",
        Short: "Apply all pending migrations",
        Long: `Apply all pending migrations against the configured database.

When --up-if-clean is set, the command is idempotent: if no migrations are
pending (database already at latest version), exits 0 quietly. Without
--up-if-clean, the same condition still exits 0 (since there's nothing to
do), but the flag makes the deploy-script intent explicit. Crucially, the
flag must be accepted by cobra so deploy CMDs that pass it succeed.`,
        RunE: func(cmd *cobra.Command, _ []string) error {
            upIfClean, _ := cmd.Flags().GetBool("up-if-clean")
            d, req, err := buildDriverAndRequestForTest(cmd)
            if err != nil {
                return err
            }
            result, err := d.Up(context.Background(), req)
            if err != nil {
                return fmt.Errorf("migrate up: %w", err)
            }
            if len(result.Applied) == 0 {
                if upIfClean {
                    fmt.Println("up-if-clean: no pending migrations; database is clean.")
                } else {
                    fmt.Println("No pending migrations.")
                }
                return nil
            }
            fmt.Printf("Applied %d migration(s): %v\n", len(result.Applied), result.Applied)
            return nil
        },
    }
    sharedFlags(cmd)
    cmd.Flags().Bool("up-if-clean", false, "Idempotent up: exit 0 quietly when no migrations are pending. Required for deploy CMDs that may re-run against an already-current database.")
    return cmd
}

// buildDriverAndRequestForTest is the package-level seam that lets tests
// stub out driver construction. Production calls go straight through to
// buildDriverAndRequest.
var buildDriverAndRequestForTest = buildDriverAndRequest
```

Update `repair-dirty` subcommand (line ~257) to also call through `buildDriverAndRequestForTest` for symmetry.

**Step 5: Run flag tests — confirm pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./pkg/cli/ -run TestUpCmd -v
```

Expected: PASS.

**Step 6: Write failing test for atlas Executor recover wrapper**

Edit `internal/atlas/driver_test.go`. Add:

```go
func TestUp_RecoversAtlasExecutorPanic(t *testing.T) {
	// Inject a fake atlas executor that panics on ExecuteN.
	old := newAtlasExecutorForTest
	newAtlasExecutorForTest = func(drv migrate.Driver, dir migrate.Dir, rrw migrate.RevisionReadWriter, opts ...migrate.ExecutorOption) (atlasExecutor, error) {
		return &panickingExecutor{}, nil
	}
	defer func() { newAtlasExecutorForTest = old }()

	d := &Driver{}
	_, err := d.Up(context.Background(), interfaces.MigrationRequest{
		DSN:       "ignored-by-fake",
		SourceDir: "/ignored",
	})
	if err == nil {
		t.Fatal("expected typed error from recovered panic, got nil")
	}
	if !strings.Contains(err.Error(), "atlas-execute panic") {
		t.Errorf("error should mention atlas-execute panic; got %v", err)
	}
}

type panickingExecutor struct{}

func (p *panickingExecutor) ExecuteN(_ context.Context, _ int) error {
	a := []int{}
	_ = a[0] // intentional index out of range
	return nil
}

func (p *panickingExecutor) Pending(_ context.Context) ([]migrate.File, error) {
	return nil, nil
}
```

**Step 7: Run test — confirm fails**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./internal/atlas/ -run TestUp_RecoversAtlasExecutorPanic -v
```

Expected: FAIL with the test process actually panicking + dying (no recover yet).

**Step 8: Implement recover wrapper + executor seam**

Modify `internal/atlas/driver.go`. Add at package level (near the other `var` declarations):

```go
// atlasExecutor is the subset of *atlmigrate.Executor methods this
// driver uses. Defined as an interface so tests can inject a panicking
// fake to verify the recover wrapper.
type atlasExecutor interface {
    ExecuteN(ctx context.Context, n int) error
    Pending(ctx context.Context) ([]migrate.File, error)
}

// newAtlasExecutorForTest is the seam tests use to inject a fake
// executor. Production calls go straight through to atlmigrate.NewExecutor.
var newAtlasExecutorForTest = func(drv migrate.Driver, dir migrate.Dir, rrw migrate.RevisionReadWriter, opts ...migrate.ExecutorOption) (atlasExecutor, error) {
    return atlmigrate.NewExecutor(drv, dir, rrw, opts...)
}

// runWithRecover wraps an atlas Executor call (which is known to panic
// on certain malformed inputs at upstream
// ariga.io/atlas/sql/migrate.(*Executor).Execute — see workflow#513) and
// converts panics into typed errors. Without this wrapper, a malformed
// migration corpus crashes the process; with it, the caller gets a clear
// error mentioning the phase, which can be retried, escalated, or
// surfaced in deploy-time diagnostics.
func runWithRecover(phase string, fn func() error) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("%s panic: %v", phase, r)
        }
    }()
    return fn()
}
```

Then refactor `Up()` (line ~46) to use the seam + wrapper:

```go
func (d *Driver) Up(ctx context.Context, req interfaces.MigrationRequest) (interfaces.MigrationResult, error) {
    start := time.Now()
    db, dir, rrw, drv, cleanup, err := open(req)
    if err != nil {
        return interfaces.MigrationResult{}, err
    }
    defer cleanup()
    _ = db

    ex, err := newAtlasExecutorForTest(drv, dir, rrw, atlmigrate.WithAllowDirty(true))
    if err != nil {
        return interfaces.MigrationResult{}, fmt.Errorf("atlas: executor: %w", err)
    }

    if err := runWithRecover("atlas-execute", func() error {
        return ex.ExecuteN(ctx, 0)
    }); err != nil && !errors.Is(err, atlmigrate.ErrNoPendingFiles) {
        return interfaces.MigrationResult{}, fmt.Errorf("atlas up: %w", err)
    }

    // ... rest unchanged
}
```

Similarly wrap `ex.Pending(ctx)` in `Status()` and any Execute calls in `Down()` with `runWithRecover("atlas-pending", ...)` and `runWithRecover("atlas-down", ...)`.

**Step 9: Run recover test — confirm pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./internal/atlas/ -run TestUp_RecoversAtlasExecutorPanic -v
```

Expected: PASS — process survives + returns error containing "atlas-execute panic".

**Step 10: Run full test suite — no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./...
```

Expected: PASS across all packages.

**Step 11: Update CHANGELOG.md**

Prepend under Unreleased (or `## [0.3.7]` if convention uses release-numbered sections):

```markdown
## [Unreleased]

### Added

- `up --up-if-clean` flag: makes the `up` subcommand idempotent in deploy
  CMDs. Same effect as plain `up` when no migrations are pending (exits
  0 quietly with a slightly more informative message); the difference is
  that the flag is now accepted by cobra. Resolves
  GoCodeAlone/core-dump#150 where deploy CMDs passing `--up-if-clean`
  to `up` were rejected by cobra in v0.3.6 because the flag was only
  registered on `repair-dirty`.

### Fixed

- atlas Executor panic recovery: `Up()`, `Status()`, and `Down()` in the
  atlas driver now wrap calls into `ariga.io/atlas/sql/migrate.(*Executor).*`
  with `defer recover()` so an upstream-library panic
  (`runtime error: index out of range [0] with length 0` observed in
  GoCodeAlone/workflow#513) becomes a typed error instead of killing the
  process. The error message includes the phase name
  (`atlas-execute panic`, `atlas-pending panic`, `atlas-down panic`) so
  callers can identify which atlas operation panicked. Root-cause
  investigation of the upstream atlas bug is still open; this is the
  defensive fix.
```

**Step 12: Commit + push + open PR**

```bash
git add pkg/cli/root.go pkg/cli/root_test.go internal/atlas/driver.go internal/atlas/driver_test.go internal/golangmigrate/driver.go CHANGELOG.md
git commit -m "$(cat <<'EOF'
feat(migrate): --up-if-clean on up + atlas Executor recover wrapper

Two upstream fixes for staging-deploy blockers:

1. core-dump#150 — `--up-if-clean` flag was only on `repair-dirty`, not
   on `up`. core-dump's Dockerfile.migrate CMD invokes
   `workflow-migrate up ... --up-if-clean` which cobra rejects in v0.3.6.
   Add the flag to `up` as an idempotency hint; effective behavior is
   the same as plain `up` (already exits 0 when no migrations pending),
   but the flag is now accepted so deploy CMDs that pass it succeed.

2. workflow#513 — atlas Executor.Execute panics with
   `runtime error: index out of range [0] with length 0` against the
   core-dump migrations corpus. Reproduces on both postgres:18-alpine
   AND apache/age:release_PG18_1.7.0 (not a postgres-side issue).
   Defensive recover wrapper around all atlas Executor calls converts
   the panic into a typed error containing the phase name
   (atlas-execute panic / atlas-pending panic / atlas-down panic) so
   callers can identify which atlas operation panicked. Root-cause
   investigation deferred; this is the must-have defensive fix.

Driver-level seam (atlasExecutor interface + newAtlasExecutorForTest
package var) lets tests inject a panicking fake to validate the recover
wrapper. Same pattern used by buildDriverAndRequestForTest on the CLI
side.

See workflow/docs/plans/2026-05-02-staging-deploy-blockers-design.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push -u origin feat/up-if-clean-and-atlas-recover
gh pr create --title "feat(migrate): --up-if-clean on up + atlas Executor recover wrapper" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Two upstream fixes for staging-deploy blockers in GoCodeAlone/core-dump:

1. **core-dump#150** — `--up-if-clean` flag was only on the `repair-dirty` subcommand in v0.3.6. core-dump's `Dockerfile.migrate` CMD passes it to `up`, which cobra rejects. Add the flag to `up` as an idempotency hint.
2. **workflow#513** — atlas Executor.Execute panics with `index out of range [0] with length 0` against core-dump's migrations corpus. Defensive recover wrapper converts the panic into a typed error.

## Changes

- `pkg/cli/root.go`: register `--up-if-clean` on `up` subcommand; thread through; introduce `buildDriverAndRequestForTest` test seam.
- `internal/atlas/driver.go`: introduce `atlasExecutor` interface + `newAtlasExecutorForTest` test seam; wrap `ExecuteN`/`Pending`/`Execute` calls in `Up`/`Status`/`Down` with `runWithRecover()` that converts panics → typed errors.
- `pkg/cli/root_test.go`: flag-acceptance test + idempotency-on-clean-DB test.
- `internal/atlas/driver_test.go`: recover-wrapper test using `panickingExecutor` fake.
- `CHANGELOG.md`: Unreleased entry.

## Test plan

- [x] `go test ./pkg/cli/` green (new flag tests + existing tests)
- [x] `go test ./internal/atlas/` green (new recover test + existing tests)
- [x] `go test ./...` green (no regressions)
- [ ] CI green
- [ ] After release v0.3.7 + core-dump's Dockerfile.migrate pin bump: pre_deploy migrate runs against staging without process death; if atlas still panics on core-dump corpus, the wrapper catches it + surfaces the phase name.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If `--reviewer "Copilot"` errors, retry via API. DM team-lead with branch + commit SHA + PR URL.

**Verification class:** internal logic refactor + new CLI flag (matrix). Unit tests green + flag-presence test = sufficient for PR-time. Runtime validation happens in Task 4 after the new image lands in core-dump.

---

### Task 4: PR-B2 — bump core-dump Dockerfile.migrate pin to new workflow-plugin-migrations release containing PR-B1

Repo: `GoCodeAlone/core-dump`. Branch: `chore/bump-migrate-image-pin`. Worktree: `/Users/jon/workspace/core-dump/_worktrees/bump-migrate-pin-B2/`.

PREREQUISITE: PR-B1 merged + workflow-plugin-migrations release tag containing it exists + the new workflow-migrate Docker image is published to ghcr.io. Confirm with `gh release list --repo GoCodeAlone/workflow-plugin-migrations --limit 3` AND `docker manifest inspect ghcr.io/gocodealone/workflow-migrate:<NEW_TAG>` (or check the GHCR UI).

**Files:**
- Modify: `Dockerfile.migrate` (image tag + sha256 digest)
- Modify: `docker-compose.yml` (migrate service image tag + sha256 digest, if pinned by digest there too)

**Step 1: Confirm release + image**

```bash
gh release list --repo GoCodeAlone/workflow-plugin-migrations --limit 3
docker manifest inspect ghcr.io/gocodealone/workflow-migrate:<NEW_TAG> | jq .
```

Capture the new image digest from manifest output.

**Step 2: Set up worktree**

```bash
cd /Users/jon/workspace/core-dump
git fetch origin main
git worktree add _worktrees/bump-migrate-pin-B2 origin/main -b chore/bump-migrate-image-pin
cd _worktrees/bump-migrate-pin-B2
```

**Step 3: Update image pins**

```bash
NEW_TAG=v0.3.7  # substitute
NEW_DIGEST=sha256:<from-step-1>

# Replace in Dockerfile.migrate FROM line
sed -i.bak "s|ghcr.io/gocodealone/workflow-migrate:v0\.3\.6@sha256:[a-f0-9]\{64\}|ghcr.io/gocodealone/workflow-migrate:$NEW_TAG@$NEW_DIGEST|g" Dockerfile.migrate && rm Dockerfile.migrate.bak

# Same in docker-compose.yml if present
sed -i.bak "s|ghcr.io/gocodealone/workflow-migrate:v0\.3\.6@sha256:[a-f0-9]\{64\}|ghcr.io/gocodealone/workflow-migrate:$NEW_TAG@$NEW_DIGEST|g" docker-compose.yml && rm docker-compose.yml.bak 2>/dev/null
```

**Step 4: Verify diff**

```bash
git diff --stat
git diff
```

Expected: 1-2 files; only the FROM/image line(s) touched.

**Step 5: Commit + push + open PR**

```bash
git add Dockerfile.migrate docker-compose.yml
git commit -m "chore: bump workflow-migrate image pin to <NEW_TAG>

Picks up workflow-plugin-migrations PR <PR-B1-NUMBER>:
- --up-if-clean accepted on \`up\` subcommand (resolves
  GoCodeAlone/core-dump#150)
- atlas Executor panic recovery (defensive fix for
  GoCodeAlone/workflow#513)

Pure version-pin bump.

See workflow/docs/plans/2026-05-02-staging-deploy-blockers-design.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
"
git push -u origin chore/bump-migrate-image-pin
gh pr create --title "chore: bump workflow-migrate image pin to <NEW_TAG>" --reviewer "Copilot" --body "Picks up workflow-plugin-migrations PR <PR-B1-NUMBER> (#150 + #513). Pure version-pin bump."
```

**Step 6: Admin-merge in same turn**

Per workspace memory feedback_version_bump_immediate_merge — admin-merge with `--squash --admin --delete-branch` once CI green.

**Step 7: Post-merge verification**

```bash
# Wait for CI on new main → Deploy auto-fires
gh run list --repo GoCodeAlone/core-dump --workflow Deploy --branch main --limit 1
gh run watch <deploy-run-id> --repo GoCodeAlone/core-dump
```

Expected:
- All previous gates: success
- deploy-staging apply: now runs (PR-A2 unblocked it via env_vars preservation)
- pre_deploy migrate job: starts (cobra now accepts --up-if-clean)
- pre_deploy migrate: either succeeds (atlas-against-core-dump-corpus works) OR fails with "atlas-execute panic: ..." typed error (atlas still panics but now process survives + caller sees phase name)

Either outcome is progress. The first outcome means the chain reaches App Platform deploy. The second outcome means we're now at "investigate why atlas panics on core-dump corpus" — file as Task 5+ iteration if so.

**Verification class:** version pin update (matrix). Run version-skew audit per `superpowers:finishing-a-development-branch` Step 1c.

---

### Task 5+: Iterate — fix what surfaces during apply

After Tasks 1-4 land, the deploy chain progresses to whichever step fails next. Each new failure spawns its own iteration task following the same upstream-first pattern:

1. **Triage:** read the failure log; identify the failing component (wfctl rule, plugin Initialize, plugin driver Apply, App Platform health probe, App Platform deploy, etc.)
2. **Decide:** is this an upstream bug (workflow / workflow-plugin-* ) or a downstream config issue (core-dump infra.yaml / workflow yaml / Dockerfile)?
3. **Fix upstream when possible.** Per user direction "prefer upstream fixes". If the fix is a workflow-plugin-digitalocean change, file/fix there. If wfctl, file/fix there. If core-dump's infra.yaml is genuinely wrong, fix there (per the PR #153 pattern).
4. **NO doctl/openssl/gh-secret fallback. NO bypassing wfctl.**
5. **Re-trigger Deploy** after the fix lands; observe the next failure.

Predictable Task 5+ candidates (in expected failure order):

- **Task 5 (likely):** atlas still panics on core-dump migrations corpus, even with the recover wrapper catching it. Need to bisect the corpus to find the offending migration file. File-level fix in core-dump (correct the malformed migration) OR upstream atlas issue if it's a library-level slice access bug.
- **Task 6 (possible):** App Platform deploy health-check timing — staging app takes longer than 60s soak window in deploy.yml verification step. Tune the soak parameter OR fix the slow startup.
- **Task 7 (possible):** NATS service-discovery DNS — `coredump-nats-staging.internal:4222` doesn't resolve. App Platform internal DNS quirk OR infra.yaml expose:internal misconfiguration.
- **Task 8 (possible):** trusted_sources on managed Postgres — App Platform service can't connect to DB because trusted_sources doesn't include the App Platform service identity.
- **Task 9 (possible):** Some other thing we haven't predicted.

For each iteration task, follow the same superpowers pipeline: brainstorm → write plan → alignment-check → subagent-driven-development → finishing → pr-monitoring. The team-lead orchestrates; implementer executes; spec/code reviewers gate.

The plan terminates when `curl https://<staging-url>/healthz` returns 200 from the post-merge Deploy verification step. At that point, save a project memory + report to user.

**Out of scope for Task 5+:**
- workflow#516 lint drift cleanup
- workflow#514 wfctl build push pipeline restoration (currently buildx stopgap)
- v0.20.2 ghost tag cleanup
- Steam release pipeline (separate workflow)
- Production environment uncomment + first prod deploy (post-staging-soak follow-on)

---

## Cross-task notes

**Branch hygiene:** Each task uses a fresh branch off origin/main. The `design/staging-deploy-blockers` branch (this plan's home) does NOT get implementation commits.

**Review discipline:**
- All PRs get Copilot reviewer (via API if --reviewer flag fails).
- Workflow + plugin PRs (Tasks 1, 3, Task 5+ when in those repos): IAC_PLUGIN_REVIEW_CHECKLIST.md 8-bug-class scan + structpb-boundary scan + adversarial framing per workspace memory v5.2.0.
- core-dump PRs (Tasks 2, 4, Task 5+ when in core-dump): infra.yaml-shape parity with BMW pattern + downstream-wiring check (env_var consumers in workflow yamls match infra.yaml output).
- All ghost-flag verification per `feedback_copilot_ghost_flags_verify_file_content`.
- All admin-merge per `feedback_admin_override_pr_merge`.

**Active monitoring:** per `feedback_active_pr_monitoring` — actively poll PR state on every signal during CI/Copilot windows.

**No doctl/openssl/gh-secret fallback** anywhere. Full wfctl dogfooding.

**Auto-chaining:** per `feedback_continuous_autonomous_phases` — after each PR merges and validation succeeds, immediately spawn the next task without re-asking permission.

**Workflow#516 lint drift:** workflow PRs (Tasks 1, possibly Task 5+) will hit the same pre-existing Lint failure. Admin-merge with override per `feedback_admin_override_pr_merge`.

**Release cutting:** after PR-A1 merges, team-lead cuts a workflow release (likely v0.20.4 patch since env_vars preservation is a behavioral change to plan output but additive — existing consumers unaffected). After PR-B1 merges, team-lead cuts a workflow-plugin-migrations release (v0.3.7 patch). Per the v0.20.2 ghost-tag lesson: `git fetch + checkout main + pull --ff-only` BEFORE local `git tag`, OR use `gh api` to create the tag pointing at the exact correct commit SHA.

## System Impact

(Carried forward from design doc.)

- **Auth/secrets:** Tasks 1+2 change when env-var values get resolved (parse-time → apply-time injection). Same secret material; different timing. No new auth surface.
- **Plan/state semantics:** plan.json now contains `${VAR}` literals in env_vars submaps post-Task 1. State store unaffected. Existing plans (pre-change) still apply correctly.
- **Migration semantics:** `--up-if-clean` on `up` is idempotent + accepted-by-cobra. Backwards-compat: existing callers (no flag) unchanged.
- **Atlas recover:** converts process-killing panic → typed error. Strictly safer.
- **All other System Impact Matrix categories** (anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None.
