# Unified Environment Setup Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Let one setup flow configure both provider secrets and non-secret variables, with provider-safe defaults and custom storage-name mapping.

**Architecture:** Reuse existing manifest/config discovery, add a typed setup input abstraction, route writes through the existing secret or variable provider interface, and make name mapping apply before status checks, writes, and optional YAML rewrites.

**Tech Stack:** Go 1.26, `cmd/wfctl`, `secrets`, `config`, `gopkg.in/yaml.v3`, existing prompt/table helpers.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~900

**Out of scope:**
- Removing `wfctl secrets setup` or `wfctl vars setup` before Workflow 1.0.
- Implementing GitHub selected-repository selection for org visibility.
- Adding variable support to non-GitHub providers in workflow core.
- Releasing downstream plugin repos from this workflow-core PR; those are separate cascade PRs.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add unified env setup mapping and safer provider defaults | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/unified-env-setup |

**Status:** Locked 2026-06-12T22:54:28Z

## Task 1: Provider Defaults

**Files:**
- Modify: `secrets/github_provider.go`
- Modify: `cmd/wfctl/secrets_setup_plugin.go`
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Modify: `cmd/wfctl/vars_setup_plugin.go`
- Test: `secrets/github_provider_test.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`

**Steps:**
1. Write failing tests proving empty org visibility becomes `private` for GitHub org secrets and variables.
2. Change GitHub org provider default visibility from `all` to `private`.
3. Change CLI flag defaults and configured store fallback visibility from `all` to `private`.
4. Update docs references from default `all` to `private`.
5. Run `GOWORK=off go test ./secrets ./cmd/wfctl -run 'Test.*Visibility|Test.*GitHub.*Org' -count=1`.

**Expected:** GitHub org payloads include `visibility: private` unless explicitly overridden.

**Rollback:** revert task; GitHub org writes return to old broad default.

## Task 2: Unified Setup Input Model

**Files:**
- Create: `cmd/wfctl/env_setup_model.go`
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Modify: `cmd/wfctl/vars_setup_config.go`
- Test: `cmd/wfctl/env_setup_model_test.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`

**Steps:**
1. Write failing tests for discovery that combines plugin `required_secrets[]`, plugin `required_config[]`, `secrets.entries`, `vars.entries`, `variables.entries`, and config env refs.
2. Add typed input structs with logical name, storage name, kind, sensitivity, sources, targets, and store hint.
3. Convert manifest discovery to produce typed inputs while keeping compatibility wrappers for secret-only callers.
4. Run targeted tests.

**Expected:** mixed plugin/app inputs are sorted, deduped, and correctly classified as secret or var.

**Rollback:** revert task; discovery returns to secret-only behavior.

## Task 3: Name Mapping And Status

**Files:**
- Modify: `cmd/wfctl/env_setup_model.go`
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Modify: `cmd/wfctl/vars_setup_plugin.go`
- Test: `cmd/wfctl/env_setup_model_test.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`

**Steps:**
1. Write failing tests where `--name-map NAMECHEAP_API_KEY=GCA_NC_API_KEY` causes status checks to call provider `Check("GCA_NC_API_KEY")`.
2. Add repeatable `--name-map LOGICAL=STORED` parsing.
3. Apply mapping before all provider status checks, reads from env/literals, and writes.
4. Ensure prompts show logical and stored names without printing values.
5. Run targeted tests.

**Expected:** mapped names are used for provider calls; logical names remain visible in source/prompt context.

**Rollback:** revert task; custom mapping disappears.

## Task 4: Mixed Secret And Var Writes

**Files:**
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Modify: `cmd/wfctl/vars_setup_plugin.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`

**Steps:**
1. Write failing tests proving mixed setup calls `Set` for secret inputs and `SetVariable` for variable inputs.
2. Add target/provider wrappers that expose both secret and variable status/write operations when supported.
3. Skip or fail unsupported variable writes with a clear provider-specific message.
4. Preserve `wfctl vars setup` behavior for non-secret-only flows.
5. Run targeted tests.

**Expected:** one setup run can configure `NAMECHEAP_API_KEY` as a secret and `NAMECHEAP_CLIENT_IP` as a variable.

**Rollback:** revert task; users run separate commands.

## Task 5: Config Rewrite

**Files:**
- Create: `cmd/wfctl/env_setup_rewrite.go`
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Test: `cmd/wfctl/env_setup_rewrite_test.go`

**Steps:**
1. Write failing tests for YAML scalar rewrite from `${NAMECHEAP_API_KEY}` to `${GCA_NC_API_KEY}` with comments and unrelated refs preserved.
2. Add explicit `--write-config` flag for manifest setup.
3. Rewrite only matched `${LOGICAL}` references after provider writes succeed.
4. Report rewritten files and no-op mappings.
5. Run targeted tests.

**Expected:** config YAML is only changed when requested and only env references matching supplied mappings are rewritten.

**Rollback:** revert task or git revert rewritten app config.

## Task 6: Docs And Verification

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `docs/wfctl-secrets-scopes.md`
- Modify: `docs/iac-dns-providers.md`
- Modify: `docs/plans/2026-06-12-unified-env-setup-design.md`
- All touched files

**Steps:**
1. Document unified setup behavior, backwards compatibility, name mapping, and provider defaults.
2. Run `gofmt` on changed Go files.
3. Run `GOWORK=off go test ./cmd/wfctl ./secrets ./config -run 'Test.*EnvSetup|Test.*Manifest|Test.*Variables|Test.*Visibility|Test.*GitHub' -count=1`.
4. Run `GOWORK=off go test ./cmd/wfctl ./secrets ./config`.
5. Run `git diff --check`.
6. Open PR, add Copilot reviewer, monitor CI.

**Expected:** local verification exits 0 and PR checks are green.

**Rollback:** revert the single workflow core PR.
