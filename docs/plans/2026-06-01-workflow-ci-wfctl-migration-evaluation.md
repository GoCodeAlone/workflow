# Workflow CI wfctl Migration Evaluation

**Date:** 2026-06-01
**Issue:** https://github.com/GoCodeAlone/workflow/issues/616
**Status:** Evaluation complete

## Goal

Evaluate whether the Workflow repository's GitHub Actions build, test, and
release jobs should move to `setup-wfctl` plus `wfctl` commands, and identify
the gaps that should be split into focused follow-up work.

## Summary

Do not replace the Workflow repository's CI and release workflows wholesale with
generated `wfctl ci` output yet. Keep GitHub Actions as the scheduler and
security boundary for this repo, while increasing dogfooding by calling `wfctl`
inside targeted jobs where the command already owns the lifecycle logic.

The current `wfctl ci plan` / `wfctl ci generate` surface is built around
config-derived application deployment pipelines: plan, apply, migrations, smoke,
plugin install, and multi-platform rendering for GitHub Actions, GitLab,
Jenkins, and CircleCI. The Workflow repo's CI is a product-repository pipeline:
Go+UI build embedding, race tests, generated examples checks, CodeQL, OSV,
cross-plugin compatibility, conformance budget gates, vendored proto staleness,
snapshot releases, multi-binary release packaging, ko images, checksums, and
repository-dispatch fanout. Those are real repo operations, but they are not yet
represented by `cigen.CIPlan`.

## Global Design Guidance

Source: `README.md`, `docs/REPO_LAYOUT.md`, and
`decisions/0045-ci-generation-boundary.md`.

| guidance | evaluation response |
|---|---|
| `wfctl` should reduce CI/CD DSL duplication | Keep moving repeated lifecycle logic into `wfctl` commands, then call those commands from Actions. |
| `cigen` owns core CI generation; provider plugins are not renderer owners | Do not move GitHub Actions rendering into `workflow-plugin-github` or infra plugins now. |
| `workflow-plugin-ci-generator` consumes core `cigen` | Preserve that boundary; pluginization would need a new `ci.renderer` contract. |
| Keep generated scaffolds and examples in documented roots | Treat generated CI as an artifact for apps/plugins, not as scratch committed to root. |

## Current Workflow Inventory

The repo currently uses hand-authored workflows for:

| workflow | current responsibility | wfctl replacement readiness |
|---|---|---|
| `.github/workflows/ci.yml` | Go race tests, coverage, lint, build, UI build, example build/lint, go mod tidy, example config validation, cloud-SDK guard checks | Partial. `wfctl ci run` can run build/test phases for app configs, but does not model this repo's UI embed, examples module, coverage upload, lint action, or bespoke guard checks. |
| `.github/workflows/release.yml` | Tagged release: tests, UI package, ko image, cross-platform binaries for server/wfctl/LSP, checksums, GitHub release, Homebrew and downstream repo dispatch | Not ready. No `wfctl release` or `CIPlan` release asset model exists. |
| `.github/workflows/pre-release.yml` | Snapshot release on `main`, binaries, admin UI tarball, snapshot image | Not ready for generated replacement; could later share release helpers with `wfctl release`. |
| `.github/workflows/create-release.yml` | Manual semver tag creation and workflow_call into release | Not ready; this is GitHub release orchestration and tag mutation. |
| `.github/workflows/cross-plugin-build-test.yml` | Compile AWS/GCP/Azure plugins against this PR plus typed-IaC E2E | Keep hand-authored for now; it intentionally checks out sibling repos and patches `go.mod` replaces. |
| `.github/workflows/conformance-smoke.yml` | Budget-gated DigitalOcean conformance smoke and cleanup | Keep hand-authored scheduler; use `wfctl` for cleanup/apply actions where possible. |
| `.github/workflows/codeql.yml` and `osv-scanner.yml` | GitHub security scanners | Keep native Actions integrations. |
| `.github/workflows/proto-vendor-staleness.yml` | Vendored infra proto drift check | Keep until there is a general `wfctl proto vendor-check` command. |

## Current wfctl CI Capabilities

Already shipped:

- `wfctl ci plan` derives a platform-neutral `cigen.CIPlan`.
- `wfctl ci generate` renders GitHub Actions, GitLab CI, Jenkins, and CircleCI.
- `wfctl ci run` executes build, test, and deploy phases from workflow config.
- `wfctl ci init` emits bootstrap GitHub Actions/GitLab CI that delegates to
  `wfctl ci run`.
- `wfctl ci validate` validates CI config sections.
- `cigen` supports plan/apply, per-phase deploys, migrations, smoke checks,
  plugin install, secret scoping, and plan guards.

Main gaps for replacing this repo's workflows:

- No model for repository-native Go package matrices, example submodules, or UI
  asset embedding before Go tests.
- No first-class lint, coverage upload, CodeQL, OSV, benchmark, or custom guard
  job concepts in `CIPlan`.
- No release asset model for cross-platform binaries, admin UI tarballs,
  checksums, ko images, SBOMs, Homebrew dispatch, registry sync dispatch, or
  downstream scenario bumps.
- No native cross-repo compatibility job model that checks out plugin repos,
  applies local `replace` directives, and builds against the PR checkout.
- Generated GitHub Actions permissions are intentionally generic compared with
  the repo's least-privilege per-job permissions.

## Recommendation

### 1. Keep generated CI for downstream apps and plugins

`wfctl ci generate` should remain the portable CI authoring path for applications
and plugins built with Workflow configs. It is not currently the right source of
truth for the Workflow engine repo's own release pipeline.

### 2. Dogfood wfctl inside hand-authored Actions

Where `wfctl` already owns a lifecycle action, call it directly from the existing
workflow:

- Keep using `wfctl validate` / schema tests for example and scenario configs.
- Keep using `wfctl infra cleanup` in conformance cleanup paths.
- Prefer future `wfctl` commands for proto drift, release checks, registry sync,
  and cross-plugin contract checks before adding more shell.
- Use `setup-wfctl` for workflows that should exercise the released CLI rather
  than the PR checkout. For PR-sensitive checks, build `./cmd/wfctl` from the
  checkout so the job tests the proposed code.

### 3. Add focused product gaps before migration

Split implementation into small follow-up issues instead of one CI rewrite:

| follow-up | purpose |
|---|---|
| `wfctl release plan` / `wfctl release build-assets` evaluation | Model release artifacts, checksums, image metadata, and downstream dispatch inputs before touching `release.yml`. |
| `wfctl repo check` or narrower commands | Move bespoke guards such as proto staleness and cloud-SDK import bans into tested Go commands where they are product policy, not ad hoc shell. |
| `cigen` repo-profile design | Decide whether `CIPlan` should grow repo-native build/test jobs, or whether app-deploy CI and repo CI should stay separate. |
| `wfctl cross-plugin check` | Generalize the existing cross-plugin build workflow into a command that takes plugin repo names and a local workflow checkout path. |
| `setup-wfctl` adoption audit | Identify jobs that should use the latest released CLI versus a locally built PR CLI. |

### 4. Do not move first-party renderers into provider plugins now

`workflow-plugin-github`, `workflow-plugin-gitlab`, and
`workflow-plugin-infra` should not own these renderers today. CI generation has a
bootstrap dependency problem: users often need CI before plugins are installed.
ADR 0045's core-first boundary remains correct.

A future plugin-provider model could make sense only for third-party CI systems
or proprietary renderers. That design needs a `ci.renderer` contract with
discovery, versioning, offline behavior, fallback semantics, and a clear answer
for how `wfctl ci generate` works before plugin installation.

## Security Review

Current hand-authored workflows use job-specific permissions and native GitHub
security products. A generated replacement would risk broadening permissions
unless `cigen` learns per-job least-privilege output for every repo-specific
operation. Keep CodeQL, OSV, package read/write scopes, release write scopes,
OIDC, and repository-dispatch tokens explicitly modeled in Actions until `wfctl`
has equivalent policy-aware renderers.

Generated CI must not obscure secret provenance. `wfctl` commands should continue
to accept secret names and environment references, never secret values.

## Infrastructure Impact

No infrastructure changes are made by this evaluation. The recommended path
avoids changing release or CI behavior until each follow-up has local tests and
CI proof. Future migration PRs that affect deployment, release publishing, OIDC,
or cloud conformance must include rollback notes and dry-run evidence.

## Multi-Component Validation

Before any replacement PR changes production workflows, validate against real
boundaries:

- Render or update the candidate workflow and run `gh workflow run` on a branch
  or use a `pull_request` test PR.
- For PR-sensitive jobs, prove the workflow uses the PR checkout, not the latest
  release.
- For released-CLI dogfooding jobs, prove `setup-wfctl` installs the intended
  tag and that behavior is compatible with the repo state.
- For cross-plugin checks, compile at least AWS/GCP/Azure against the PR checkout
  with `go mod edit -replace github.com/GoCodeAlone/workflow=../workflow`.

## Assumptions

- The goal is progressive dogfooding and DSL reduction, not replacing
  GitHub-native security products with weaker abstractions.
- App/plugin generated CI and the Workflow repo's own release pipeline are
  related but not identical products.
- Existing Actions workflows are trusted until a focused replacement proves
  equivalent behavior.
- The `cigen` core-first renderer boundary remains accepted project direction.

## Rollback

This evaluation changes documentation only. Rollback is reverting this file.

For future CI migration PRs, rollback must be explicit per workflow: restore the
previous YAML file, re-run the prior workflow on the same branch or tag, and
confirm no release artifacts or cloud resources were created unexpectedly.

## Decision

Close #616 as evaluated. Do not perform a broad workflow rewrite now. Use the
follow-up list above to move individual pieces into `wfctl` only after each
piece has a product command, tests, and a parity check against the current
GitHub Actions behavior.
