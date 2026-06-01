# Registry Sync Verify-Capabilities Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Implement the `wfctl plugin registry-sync --verify-capabilities` gap tracked by workflow#762.

**Architecture:** Reuse `wfctl plugin verify-capabilities` by extracting a manifest-path helper, then have registry-sync download the current-platform release artifact, extract/locate the plugin binary, and verify it against the registry manifest. Keep the behavior additive and bounded to the existing registry-sync default mode.

**Tech Stack:** Go 1.26, existing `gh` CLI release access, existing plugin gRPC spawn path, standard-library tar/gzip extraction.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~250

**Out of scope:**
- Replacing workflow-registry's authoritative bash parity cycle.
- Layer 3b plugin-repo fanout from workflow#760.
- Full SemVer 2.0.0 prerelease support.
- Verifying non-current-platform release assets.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): implement registry-sync capability verification | Task 1, Task 2, Task 3 | feat/762-registry-sync-verify-capabilities |

**Status:** Draft

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md`, `docs/PLUGIN_RELEASE_GATES.md`, `docs/plans/2026-05-23-wfctl-registry-sync.md`, `docs/plans/2026-05-24-verify-capabilities-design.md`.

| guidance | design response |
|---|---|
| Prefer focused tests first | Add failing tests for the missing flag behavior and helper selection before production edits. |
| Update docs for CLI behavior | Update `docs/PLUGIN_RELEASE_GATES.md` registry-sync section. |
| Keep plugin contracts centralized in wfctl | Reuse `verify-capabilities`; do not duplicate manifest diff rules. |
| Runtime binary execution is security-sensitive | Keep explicit warning in docs and execute only release artifacts selected by current OS/arch. |

## Security Review

`--verify-capabilities` executes downloaded plugin binaries. This is already the posture of `wfctl plugin verify-capabilities`; registry-sync must document the same trust boundary and use `gh release download` against the manifest repository/tag rather than unauthenticated ad hoc URL execution.

## Infrastructure Impact

No cloud resources, migrations, or deployment changes. The command performs network access to GitHub releases and writes only when existing `--fix` is supplied for manifest drift.

## Multi-Component Validation

Focused unit tests cover command behavior and artifact selection. A smoke command against `workflow-registry` with a single plugin verifies the command path reaches GitHub release metadata without requiring a full registry sweep.

## Assumptions

- Registry manifests contain plugin-manifest-compatible fields plus extra registry metadata.
- Plugin release assets include a current-platform tarball named with parseable OS/arch suffix.
- Registry aliases may be shorter than runtime plugin names; registry-side verification checks runtime version/contract freshness and intentionally skips strict name equality.
- `gh` is available in registry-sync environments, as it already is for the existing subcommand.

## Rollback

Revert the PR. Existing registry-sync behavior without `--verify-capabilities` is unchanged; callers not using the flag are unaffected.

### Task 1: Tests

**Files:**
- Modify: `cmd/wfctl/plugin_registry_sync_test.go`

**Steps:**
1. Add a test that `verifyCapabilitiesForRegistryPlugin` is invoked when `--verify-capabilities` is requested and returns errors instead of printing a stub note.
2. Add a test for selecting the current-platform asset from release assets.
3. Run `GOWORK=off go test ./cmd/wfctl -run 'TestPluginRegistrySync' -count=1`; expected RED because production helpers do not exist / flag is still stubbed.

### Task 2: Implementation

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin_registry_sync.go`

**Steps:**
1. Extract `verifyPluginManifestAgainstBinary(binary, manifestPath string) error` from `runPluginVerifyCapabilities`.
2. Add current-platform release asset selection.
3. Add `gh release download` + tar extraction + executable discovery.
4. Replace the stub note with real verification.
5. Run focused tests; expected PASS.

### Task 3: Docs and Verification

**Files:**
- Modify: `docs/PLUGIN_RELEASE_GATES.md`

**Steps:**
1. Document what `--verify-capabilities` now does and the execution trust boundary.
2. Run `GOWORK=off go test ./cmd/wfctl -run 'TestPluginRegistrySync|TestPluginVerifyCapabilities' -count=1`.
3. Run `GOWORK=off go test ./cmd/wfctl -count=1`.
4. Run `GOWORK=off golangci-lint run ./cmd/wfctl` if available.
