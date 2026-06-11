# Provider Environment Secrets Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Make `wfctl secrets setup` respect YAML-declared environments and preflight provider environments before writing environment-scoped secrets.

**Architecture:** Core defines optional environment lifecycle contracts and derives desired environments from Workflow YAML. The existing GitHub provider implements environment list/validate/ensure for GitHub Actions Environments. `wfctl secrets setup --manifest` uses YAML-derived env names when constructing targets and ensures selected environment targets interactively before setting secrets.

**Tech Stack:** Go 1.26, existing `secrets` provider package, `cmd/wfctl`, `config`, GitHub REST API through existing provider HTTP client patterns.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 5
**Estimated Lines of Change:** ~650

**Out of scope:**
- Migrating implementation into `workflow-plugin-github`; this PR creates the contract and safe core behavior first.
- Managing GitHub environment protection reviewers/branches.
- Deleting provider environments.
- Auto-creating provider environments in non-interactive mode.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add provider environment preflight for secrets setup | Task 1, Task 2, Task 3, Task 4, Task 5 | feat/provider-env-secrets |

**Status:** Locked 2026-06-08T14:58:54Z

## Task 1: Provider Environment Contract

**Files:**
- Modify: `secrets/secrets.go`
- Modify: `secrets/github_provider.go`
- Test: `secrets/github_provider_test.go`

**Steps:**
1. Write failing tests for `GitHubSecretsProvider` listing, validating, and ensuring environments through GitHub environment endpoints.
2. Add `ProviderEnvironment` and optional `EnvironmentManager` interface to `secrets`.
3. Implement GitHub provider methods using existing HTTP request helpers and safe non-secret metadata.
4. Run `GOWORK=off go test ./secrets -run 'TestGitHubProvider_.*Environment|TestProvider' -count=1`.

**Expected:** tests pass and GitHub environment methods hit `/repos/{owner}/{repo}/environments`.

**Rollback:** revert this task; optional provider capability disappears and callers keep current behavior.

## Task 2: YAML Environment Discovery

**Files:**
- Create: `config/environment_discovery.go`
- Test: `config/environment_discovery_test.go`
- Modify: `docs/dsl-reference.md`

**Steps:**
1. Write failing tests for environment names derived from top-level `environments`, `ci.deploy.environments`, `platform.environment`, and `secretStores[*].config.environment`.
2. Implement `DesiredEnvironmentNames(cfg *WorkflowConfig) []string`.
3. Treat `${WORKFLOW_ENV}` placeholders as requiring runtime env input, not as a literal environment name.
4. Document that YAML declarations are desired state, not provider existence proof.
5. Run `GOWORK=off go test ./config -run TestDesiredEnvironmentNames -count=1`.

**Expected:** test output is green and derived env names are sorted/deduped.

**Rollback:** revert helper/docs; no persistent state changes.

## Task 3: Manifest Target Construction

**Files:**
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`

**Steps:**
1. Write failing tests proving manifest setup offers GitHub env targets for YAML-declared environments and does not offer default `github:env:local`.
2. Use `config.DesiredEnvironmentNames` when building fallback GitHub env targets.
3. Keep configured `secretStores[*].config.environment` targets provider-owned and explicit.
4. Run targeted manifest tests.

**Expected:** `github:env:<name>` targets appear only for explicit YAML env declarations or explicit CLI env input.

**Rollback:** revert task; manifest setup returns to repo/org plus configured secret store behavior.

## Task 4: Environment Preflight During Setup

**Files:**
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`
- Modify: `docs/WFCTL.md`
- Modify: `docs/wfctl-secrets-scopes.md`

**Steps:**
1. Write tests for selected environment targets: existing env validates, missing env is ensured in interactive setup, provider errors stop before secret writes.
2. Add a preflight step before setting selected target secrets.
3. In non-interactive mode, validate only; do not create missing environments.
4. Update docs to explain YAML-driven env discovery and provider-side ensure behavior.
5. Run targeted `cmd/wfctl` tests.

**Expected:** missing GitHub environments are created only on the interactive manifest path after target selection.

**Rollback:** revert task; no provider environment preflight occurs.

## Task 5: Verification And PR

**Files:**
- All touched files

**Steps:**
1. Run `gofmt` on changed Go files.
2. Run `GOWORK=off go test ./secrets ./config ./cmd/wfctl -run 'Test.*Environment|Test.*Manifest.*|TestRunSecretsSetupRejectsAutoGenKeysForManifestTarget' -count=1`.
3. Run `GOWORK=off golangci-lint run --timeout=10m`.
4. Run `git diff --check`.
5. Create PR, add Copilot reviewer, monitor checks/reviews.

**Expected:** local verification exits 0, PR CI green, no unresolved review threads.

**Rollback:** revert the PR; no data migration required.
