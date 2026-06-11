# Docs And Capability Ecosystem Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Ship the Workflow-owned foundation for capability-driven docs: generated catalog/crossrefs, generated Go API docs, useful app capability summaries, and consistent `wfctl` prompt cancellation.

**Architecture:** Extend the existing `capability/inventory` and `wfctl` docs/capability commands. Workflow remains the source of semantic extraction. Website and plugin repo work follows in later locked phases after these artifacts exist.

**Tech Stack:** Go stdlib, existing `config`, `plugin`, `capability/inventory`, existing `cmd/wfctl` patterns, Bubble Tea prompt helpers, `go list`, `go doc`, Markdown/JSON output.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~1700

**Out of scope:**
- Website Astro/Starlight Mermaid rendering.
- Editing all plugin repositories in this PR.
- Provider runtime behavior changes in GitHub/GitLab/AWS/Vault/etc.
- Replacing pkg.go.dev or running arbitrary plugin binaries for docs.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): add docs capability catalog foundation | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/docs-capability-ecosystem |

**Status:** Draft

## Global Design Guidance

Source: `docs/plans/2026-06-11-docs-capability-ecosystem-design.md`

| Guidance | Plan Response |
|---|---|
| Workflow owns extraction semantics | Implement catalog, crossrefs, and Go-doc generation in Workflow. |
| Website consumes artifacts | Emit stable JSON/Markdown under `docs/generated`. |
| Use existing Go tooling | Use `go list` and `go doc`; no external doc binary dependency. |
| Prompt cancellation must be consistent | Add shared prompt cancellation error and tests across prompt widgets. |

## Task 1: Docs-Facing Capability Catalog

**Files:**
- Update: `capability/inventory/types.go`
- Update: `capability/inventory/ecosystem.go`
- Create/update: `capability/inventory/catalog_test.go`
- Update: `cmd/wfctl/capability.go`
- Update: `cmd/wfctl/capability_test.go`

**Steps:**

1. Add catalog/cross-reference structs derived from the existing inventory:
   `Catalog`, `CatalogCapability`, `PluginReference`, `CapabilityCrossrefs`.
2. Implement a deterministic builder that filters raw uncategorized rows out of
   the primary catalog while preserving maintainer counts/findings.
3. Build cross references by plugin/provider name, capability ID, dependency,
   and provider family.
4. Add `wfctl capability catalog --format json|md --output <path>` and
   `wfctl capability crossrefs --format json --output <path>`.
5. Add tests with small registry/local plugin fixtures proving:
   - known capability rows include providers;
   - uncategorized rows are excluded from public catalog;
   - crossrefs include capability-to-plugin and plugin-to-capability links;
   - output is stable/sorted.

**Verification:**

```sh
GOWORK=off go test ./capability/inventory ./cmd/wfctl -run 'Test.*Capability.*Catalog|Test.*Capability.*Crossref' -count=1
```

## Task 2: Useful App Capability Check Output

**Files:**
- Update: `cmd/wfctl/capability.go`
- Update: `cmd/wfctl/capability_test.go`
- Update: `docs/WFCTL.md`

**Steps:**

1. Change default `wfctl capability check` text output to include a concise
   detected-capability summary before findings.
2. Add `--findings-only` to preserve warning-only output.
3. Keep JSON output unchanged unless a schema-compatible field is needed.
4. Add tests for:
   - healthy app with detected capabilities prints capability rows;
   - `--findings-only` prints the current warning-only/no-warning behavior;
   - findings still appear after the summary.
5. Regenerate/update CLI docs.

**Verification:**

```sh
GOWORK=off go test ./cmd/wfctl -run TestRunCapabilityCheck -count=1
```

## Task 3: Go API Docs Generator

**Files:**
- Create: `cmd/wfctl/docs_go.go`
- Create/update: `cmd/wfctl/docs_go_test.go`
- Update: `cmd/wfctl/docs.go` or command routing as appropriate
- Create: `docs/generated/go-docs/README.md`

**Steps:**

1. Add a generator command under the existing docs command family, using the
   local command style already present in `cmd/wfctl/docs.go`.
2. For each requested module root, run `go list -json ./...` and `go doc -all`
   per package with `GOWORK=off` in the child process environment.
3. Ignore `.git`, `.worktrees`, `_worktrees`, `vendor`, `node_modules`, generated
   docs output, and scratch directories.
4. Emit an index JSON with module path, package path, package name, doc path,
   version/ref, generation source (`local` in this phase), and errors for
   skipped packages. Preserve fields needed by the later website phase to
   prefer released docs and expose version navigation.
5. Emit one Markdown file per module with package docs suitable for website
   ingestion.
6. Add tests using a temporary Go module fixture and a fake command runner if
   needed to avoid depending on network/module downloads.

**Verification:**

```sh
GOWORK=off go test ./cmd/wfctl -run TestDocsGo -count=1
```

## Task 4: Prompt Cancellation Contract

**Files:**
- Update: `cmd/wfctl/internal/prompt/*.go`
- Update: `cmd/wfctl/internal/prompt/prompt_test.go`
- Update: `cmd/wfctl/secrets_setup_interactive.go`
- Update: `cmd/wfctl/wizard.go`
- Update/add tests around secrets setup cancellation if feasible without a TTY

**Steps:**

1. Add `prompt.ErrCancelled` distinct from `ErrNotInteractive`.
2. Treat `ctrl+c`, `esc`, and equivalent abort keys as cancellation in
   `Input`, `Confirm`, `Select`, and `MultiSelect`.
3. Keep `ErrNotInteractive` only for non-TTY detection.
4. Ensure secrets setup and the project wizard map cancellation to a concise
   cancellation result/error and do not continue prompting.
5. Add model-level tests for key handling so the cancel contract is verified
   without requiring a real terminal.

**Verification:**

```sh
GOWORK=off go test ./cmd/wfctl/internal/prompt ./cmd/wfctl -run 'Test.*Cancel|Test.*Cancelled|TestSecretsSetup' -count=1
```

## Task 5: Generate Phase-1 Artifacts

**Files:**
- Update: `docs/generated/capabilities/*`
- Create/update: `docs/generated/go-docs/*`
- Update: `docs/WFCTL.md`

**Steps:**

1. Regenerate ecosystem inventory.
2. Generate catalog and crossrefs.
3. Generate Go docs for Workflow core packages that are already public API
   relevant (`capability`, `capability/inventory`, `plugin`, `sdk`, config
   packages as available).
4. Keep generated outputs deterministic enough for review.

**Verification:**

```sh
GOWORK=off go run ./cmd/wfctl capability ecosystem --repo-root .. --registry data/registry --format json --output docs/generated/capabilities/ecosystem.json
GOWORK=off go run ./cmd/wfctl capability catalog --repo-root .. --registry data/registry --format json --output docs/generated/capabilities/catalog.json
GOWORK=off go run ./cmd/wfctl capability crossrefs --repo-root .. --registry data/registry --format json --output docs/generated/capabilities/crossrefs.json
GOWORK=off go test ./cmd/wfctl ./capability/... -count=1
```

## Task 6: PR, Monitoring, And Next-Phase Handoff

**Files:**
- Update: this plan with verification evidence if needed.
- Create PR against `main`.
- Create or update next-phase plan docs for website/plugin-doc batches.

**Steps:**

1. Run full relevant tests.
2. Commit phase-1 implementation.
3. Push branch and open PR.
4. Monitor CI, fix failures, admin-merge once green.
5. File or update next-phase tasks for:
   - website Mermaid/capability docs ingestion;
   - provider plugin doc batch;
   - GitLab secrets/environment management;
   - Cloudflare account/scope audit;
   - Namecheap client IP audit.

**Verification:**

```sh
GOWORK=off go test ./cmd/wfctl ./capability/... -count=1
git status --short
```
