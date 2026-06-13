# wfctl env setup Command Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Make `wfctl env setup` the primary unified environment input setup command while keeping `wfctl secrets setup` and `wfctl vars setup` compatible.

**Architecture:** Add a small `cmd/wfctl/env.go` command group that delegates `setup` to the existing manifest-backed setup engine. Keep `secrets setup` and `vars setup` behavior intact and update help/docs so the new primary command is discoverable without noisy runtime warnings.

**Tech Stack:** Go CLI code in `cmd/wfctl`, existing prompt/setup engine, existing Go test suite, Markdown docs.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~350

**Out of scope:**
- Adding `wfctl env status`.
- Adding new provider environment, secret, or variable contracts.
- Removing or warning on compatibility aliases before a Workflow 1.0 migration policy.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add `wfctl env setup` command and aliases | Task 1, Task 2, Task 3 | feat/env-setup-command |

**Status:** Draft

## Task 1: Add `wfctl env` Command Group

**Files:**
- Create: `cmd/wfctl/env.go`
- Modify: `cmd/wfctl/main.go`
- Modify: `cmd/wfctl/wfctl.yaml`
- Test: `cmd/wfctl/env_test.go`

**Steps:**
1. Write failing tests for `runEnv([]string{"-h"})`, missing action behavior, and `setup -h` usage.
2. Add `runEnv` with actions `setup`, `-h`, `--help`, and `help`.
3. Implement `runEnvSetup` as a thin delegate to manifest-backed setup. It should default to `wfctl.yaml` discovery when no `--manifest` is supplied, matching current `secrets setup` auto-discovery behavior.
4. Register `env` in `commands` and embedded `wfctl.yaml` help metadata.
5. Run focused tests.

**Verification:**
- `GOWORK=off go test ./cmd/wfctl -run 'TestRunEnv|TestEmbeddedCLIRegistersEnv' -count=1`
- Expected: tests exit 0; `wfctl env -h` help describes environment input setup, not only provider environments.

**Rollback:** Revert the command file, command registration, and embedded CLI metadata; existing `secrets setup` remains the setup surface.

## Task 2: Add Kind Filtering And Alias Help

**Files:**
- Modify: `cmd/wfctl/secrets_setup_manifest.go`
- Modify: `cmd/wfctl/secrets_setup.go`
- Modify: `cmd/wfctl/vars.go`
- Test: `cmd/wfctl/secrets_setup_manifest_test.go`
- Test: `cmd/wfctl/vars_setup_config_test.go`

**Steps:**
1. Write failing tests for `--kind secret`, `--kind var`, and invalid `--kind`.
2. Add a manifest setup `kind` filter that accepts `all`, `secret`, and `var`; default `all`.
3. Make `wfctl env setup --kind secret|var` pass that filter through the existing engine.
4. Update `secrets setup` and `vars setup` help to mention `wfctl env setup` as the primary unified flow. Use wording "secrets setup" for compatibility text; do not mention "manifest setup" as the migrated concept.
5. Keep compatibility aliases quiet during normal execution.

**Verification:**
- `GOWORK=off go test ./cmd/wfctl -run 'Test.*Manifest.*Kind|TestRunVars|TestRunSecretsSetup' -count=1`
- Expected: kind-filter tests show only matching input types are selected; help text names `wfctl env setup`.

**Rollback:** Revert kind filtering and help changes. Existing setup remains all-kind behavior.

## Task 3: Documentation And Full CLI Verification

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `docs/wfctl-secrets-scopes.md`
- Modify: `docs/plans/2026-06-13-wfctl-env-setup-command.md`

**Steps:**
1. Update docs so `wfctl env setup` is the recommended unified command.
2. Keep `wfctl secrets setup` documented for secret-specific and compatibility use.
3. Keep `wfctl vars setup` documented as non-secret-specific compatibility use.
4. Run focused and broader verification.
5. Record verification output in the final PR body.

**Verification:**
- `GOWORK=off go test ./cmd/wfctl -count=1`
- `GOWORK=off golangci-lint run --timeout=10m`
- `GOWORK=off go run ./cmd/wfctl env -h`
- `GOWORK=off go run ./cmd/wfctl env setup -h`
- `GOWORK=off go run ./cmd/wfctl secrets setup -h`
- `GOWORK=off go run ./cmd/wfctl vars setup -h`
- Expected: commands exit 0 for help, tests/lint exit 0, and help/docs consistently center `wfctl env setup`.

**Rollback:** Revert docs and command changes; publish a patch release only after the reverted command surface is verified.

