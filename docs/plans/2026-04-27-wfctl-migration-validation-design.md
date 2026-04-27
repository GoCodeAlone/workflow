---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
external_refs:
  - "workflow-plugin-migrations: migrate lint/test/repair-dirty commands"
verification:
  last_checked: 2026-04-27
  commands:
    - 'rg -n "migrate test|repair-dirty|dirty|workflow-migrate|migration" pkg cmd internal -S'
    - 'rg -n "func runMigrate|cmd-migrate|migrate" cmd/wfctl -S'
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Migration Validation and Repair Orchestration — Design

**Status:** Approved under standing autonomous development direction
**Date:** 2026-04-27
**Scope:** `workflow` (`wfctl` orchestration) plus `workflow-plugin-migrations` CLI/provider contracts

## Problem

Application repositories are carrying too much migration safety logic in CI. BMW now has deploy-time guards and PR migration checks, but those safeguards are project-specific shell and GitHub Actions code. The same class of failure can recur in any Workflow app: a migration applies partially, marks the database dirty, and deploys keep failing until a carefully guarded repair command runs.

`workflow-plugin-migrations` already owns the right low-level primitives:

- `migrate lint` for static migration checks.
- `migrate test` for full-cycle up/down validation against an ephemeral database.
- `migrate status` for current/pending/dirty state.
- `migrate repair-dirty` for exact-version guarded metadata repair.

`wfctl` does not yet provide a portable, first-class orchestration surface that turns those plugin primitives into a standard CI/deploy guard across GitHub Actions, GitLab, Jenkins, or local runs.

## Goals

1. Add a portable `wfctl migrations validate` command that can replace app-specific migration CI shell.
2. Validate PR migration changes from the latest mainline baseline, not just from an empty database.
3. Add a deploy guard that fails before app rollout if the target database is dirty or migration prerequisites did not pass for the same commit.
4. Add a repair runbook command that can emit and execute safe, typed-confirmation `repair-dirty` invocations through provider plugins.
5. Keep migration engines in plugins. Do not grow the wfctl binary with every migration driver.

## Non-Goals

- Reimplementing golang-migrate, goose, Atlas, or database drivers in `wfctl`.
- Making destructive repair automatic in production.
- Replacing provider environment gates. `wfctl` should expose portable decisions and structured output; CI systems decide how to request human approval.
- Solving all destructive IaC approval flows in this workstream. This design should be compatible with that later HITL work.

## Approaches Considered

### Approach A — Put migration test logic directly in wfctl

`wfctl` would open databases, parse migration directories, and run up/down checks itself.

Rejected. It makes the binary larger, duplicates plugin behavior, and would drift from driver-specific semantics.

### Approach B — wfctl orchestrates plugin CLI commands

`wfctl migrations validate` discovers migration modules from config, resolves environment settings and secrets, installs/loads the migration plugin, and invokes plugin CLI commands such as `migrate lint`, `migrate test`, `migrate status`, and `migrate repair-dirty`.

Recommended. It dogfoods plugin CLI support, keeps drivers out of wfctl, and gives every CI platform the same command surface.

### Approach C — Only generate GitHub workflow YAML

`wfctl ci init` would keep emitting app-specific migration jobs.

Rejected as the main solution. Generated YAML can wrap the portable command, but the behavior must live in `wfctl` so local, GitLab, Jenkins, and agent workflows use the same checks.

## Design

### Command Surface

Add a new `wfctl migrations` command family. Keep the existing local SQLite-oriented `wfctl migrate` for Workflow engine internal schema migration until it can be retired or renamed.

Initial commands:

- `wfctl migrations validate --config infra.yaml --env staging`
- `wfctl migrations status --config infra.yaml --env prod --format json`
- `wfctl migrations repair-dirty --config infra.yaml --env prod --expected-dirty-version <v> --force-version <v> --confirm-force FORCE_MIGRATION_METADATA`
- `wfctl migrations ci-check --config infra.yaml --env prod --commit <sha> --require-same-sha`

`validate` runs static lint, fresh database conformance, and baseline/candidate validation. `status` is read-only and reports current, pending, dirty, and driver metadata. `repair-dirty` delegates to the migration plugin and refuses production mutation without exact dirty version, force target, and typed confirmation. `ci-check` is the deploy-facing guard and emits structured failure reasons.

### Config

Add a portable migration declaration under `ci.migrations[]` or an equivalent existing deploy section if one already exists:

```yaml
ci:
  migrations:
    - name: app
      plugin: workflow-plugin-migrations
      driver: golang-migrate
      source_dir: migrations
      database:
        env: DATABASE_URL
      baseline:
        ref: origin/main
        mode: apply-before-candidate
      validation:
        lint: true
        fresh_cycle: true
        baseline_candidate: true
        forbid_dirty: true
```

Environment overrides use the same config resolution rules as other wfctl surfaces. Secret resolution goes through existing wfctl secret/env plumbing; command logs must never print raw DSNs.

### Baseline/Candidate Validation

For PR checks, `wfctl migrations validate` should:

1. Discover changed migration sources by comparing the candidate ref to `baseline.ref`.
2. Create an ephemeral database.
3. Check out or materialize the baseline migration directory and apply it.
4. Overlay or switch to the candidate migration directory.
5. Apply candidate migrations.
6. Assert status is clean, no pending migrations remain, and down/up cycle works when configured.

This catches the class of errors where migrations pass from an empty database but fail when applied after the latest mainline state.

### Deploy Guard

`wfctl migrations ci-check` should be safe to run before `wfctl ci run --phase deploy`.

It should fail closed when:

- The configured database is dirty.
- The migration validation result for the deploy SHA is absent or failed.
- The migration plugin cannot be loaded.
- A required secret is missing.

The command should return machine-readable JSON containing `decision`, `reasons[]`, `destructive`, and `human_approval_required`. For production dirty-state repair, `human_approval_required` is true and includes the exact command that must be approved.

### Repair Flow

`wfctl migrations repair-dirty` is a guarded wrapper around the migration plugin’s `repair-dirty` command.

Rules:

- Require exact dirty version match.
- Require explicit force target.
- Require typed confirmation.
- In non-dev envs, emit a structured approval request before mutation unless `--approved-token` or an equivalent CI environment gate marker is present.
- Support `--then-up` and `--up-if-clean` for idempotent reruns.
- Always print post-repair status with current version and dirty flag.

### CI Generation

`wfctl ci init` should emit minimal wrappers:

- A migration validation workflow/job that runs `wfctl migrations validate`.
- A deploy prerequisite step that runs `wfctl migrations ci-check`.
- Optional GitHub environment gating when `human_approval_required` is true, without making GitHub the only supported approval mechanism.

## Testing

Unit tests in `workflow` should cover config parsing, environment resolution, plugin CLI dispatch arguments, JSON output, and deploy guard decisions.

Integration tests should use `workflow-plugin-migrations` fixture migrations and ephemeral Postgres where available. The minimum launch validation is:

```sh
wfctl migrations validate --config testdata/migrations/infra.yaml --env ci --format json
```

Expected output includes `decision: pass`, `dirty: false`, and the migration source name.

Repair tests should create a known dirty database state, verify repair refuses wrong versions, then verify correct typed confirmation repairs metadata and reports `dirty: false`.

## Rollout

1. Implement the `wfctl migrations` command family behind plugin CLI dispatch.
2. Add docs and `wfctl ci init` generation updates.
3. Prepare a BMW rollout plan and compatibility fixture in this Workflow PR, then execute the BMW repository replacement as a follow-up PR after the Workflow release is available.
4. Track the same migration check pattern for other Workflow apps so each can adopt the portable command after any app-specific prerequisites are known.

## Success Criteria

- BMW no longer carries custom migration baseline/check shell beyond minimal CI wrappers after the follow-up BMW rollout PR ships.
- PR migration checks validate latest-main baseline plus candidate migrations.
- Deploys fail before app rollout when target database state is dirty.
- Dirty repair has an auditable, typed-confirmation, human-gated path.
- Migration driver behavior remains plugin-owned.
