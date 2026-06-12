# Control Plane Descriptor Validation Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Complete T566 by proving Workflow/wfctl can consume released
`workflow-plugin-control-plane` descriptor contracts without runtime/plugin-load
cycles, then close the workflow-compute roadmap row.

**Architecture:** Add test-only released-module fixtures in Workflow/wfctl.
The tests pin `workflow-plugin-control-plane` v0.1.0, load its real module
metadata through existing `editor-bundle`, call released validators for negative
inputs, and assert non-test wfctl dependencies stay clean. A second
workflow-compute PR records T566 completion evidence and keeps T567-T569 queued.

**Tech Stack:** Go 1.26, wfctl, plugin.contracts.json, protobuf descriptor
sets, GitHub Actions, autodev scope lock.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 2
**Tasks:** 6
**Estimated Lines of Change:** ~650

**Out of scope:**
- DigitalOcean provider handoff fixture implementation; T567 owns it.
- workflow-compute descriptor adapter/runtime/dashboard consumption; T568 owns it.
- scenario proof through workflow-scenarios/workflow-compute-scenarios; T569 owns it.
- New public CLI subcommands, plugin runtime loading, registry services,
  staging deploys, production deploys, migrations, cloud resources, and
  confidential CPU/GPU.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | test(wfctl): validate control-plane descriptor bundles | Task 1, Task 2, Task 3, Task 4 | workflow:feat/control-plane-descriptor-validation |
| 2 | docs: close control-plane descriptor validation phase | Task 5, Task 6 | workflow-compute:docs/control-plane-descriptor-validation |

**Status:** Locked 2026-06-12T15:00:00Z

### Task 1: Pin Released Control-Plane Module And Add Fixture Helpers

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `cmd/wfctl/control_plane_descriptor_validation_test.go`

Steps:

1. Add `github.com/GoCodeAlone/workflow-plugin-control-plane v0.1.0` to
   Workflow `go.mod`.
2. In `cmd/wfctl/control_plane_descriptor_validation_test.go`, add a helper:
   - runs `go list -m -f {{.Dir}} github.com/GoCodeAlone/workflow-plugin-control-plane`;
   - forces `GOWORK=off`;
   - fails with command output if the released module cannot resolve;
   - checks `plugin.json`, `plugin.contracts.json`, and
     `descriptorsets/control_plane.binpb` exist in the resolved module dir.
3. Add a focused test that asserts the helper resolves v0.1.0 by checking the
   module's `go.mod` module path and released contract metadata files.
4. Verify:

   ```bash
   GOWORK=off go test ./cmd/wfctl -run TestControlPlaneReleasedModuleFixture -count=1
   ```

   Expected: exits 0 and resolves the released module without using a local
   sibling checkout path.

Rollback: revert the commit; no runtime behavior changed.

### Task 2: Add Descriptor-Bundle Validation Fixture

**Files:**
- Modify: `cmd/wfctl/control_plane_descriptor_validation_test.go`

Steps:

1. Write a test that calls:

   ```go
   runEditorBundle([]string{"--registry=false", "--plugin-dir", moduleDir, "--output", outPath})
   ```

2. Decode bundle JSON and assert:
   - contracts include `message:control_plane.descriptors.v1alpha1`,
     `message:control_plane.envelopes.v1alpha1`, and
     `message:control_plane.registry.v1alpha1`;
   - each contract has `protocolVersion == "control-plane.v1alpha1"`;
   - each contract keeps descriptor set ref
     `descriptorsets/control_plane.binpb`;
   - message metadata includes
     `workflow_plugin_control_plane.descriptors.v1alpha1.RouteActionDescriptor`,
     `workflow_plugin_control_plane.envelopes.v1alpha1.ControlPlaneEnvelope`,
     and
     `workflow_plugin_control_plane.registry.v1alpha1.DescriptorRegistration`;
   - `descriptorSets["descriptorsets/control_plane.binpb"].externalRef`
     matches the released descriptor-set path.
3. Add a negative temp-fixture test that corrupts control-plane
   `plugin.contracts.json` to an invalid `schemaDigest` and verifies
   `runEditorBundle` fails via existing strict descriptor validation rather
   than silently omitting the bad contract.
4. Verify:

   ```bash
   GOWORK=off go test ./cmd/wfctl -run 'TestControlPlaneDescriptorBundle|TestControlPlaneDescriptorBundleRejectsInvalidSchemaDigest' -count=1
   ```

   Expected: exits 0; failure case error contains `invalid_message_contract_descriptor`.

Rollback: revert the commit; no runtime behavior changed.

### Task 3: Add Released Validator Negative Cases And No-Cycle Guard

**Files:**
- Modify: `cmd/wfctl/control_plane_descriptor_validation_test.go`

Steps:

1. Import released packages in `_test.go` only:
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/descriptors`
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/descriptors/pb`
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/envelopes`
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/envelopes/pb`
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/registry`
   - `github.com/GoCodeAlone/workflow-plugin-control-plane/registry/pb`
2. Add valid constructors for route/action descriptor, envelope, and
   descriptor registration.
3. Add negative cases:
   - schema digest malformed;
   - raw/network provenance ref;
   - invalid/empty downgrade-floor version shape;
   - stale revocation freshness;
   - raw tenant/actor/resource handles in envelopes;
   - provider handoff input schema digest mismatch shape.
4. Add `TestControlPlaneDescriptorValidationDoesNotEnterRuntimeDeps`:
   - runs `go list -deps ./cmd/wfctl` with `GOWORK=off`;
   - fails if output contains `github.com/GoCodeAlone/workflow-plugin-control-plane`.
5. Verify:

   ```bash
   GOWORK=off go test ./cmd/wfctl -run 'TestControlPlaneReleasedValidatorsRejectInvalidInputs|TestControlPlaneDescriptorValidationDoesNotEnterRuntimeDeps' -count=1
   ```

   Expected: exits 0; no-cycle guard uses non-test `go list -deps ./cmd/wfctl`.

Rollback: revert the commit; no runtime behavior changed.

### Task 4: Document And Verify Workflow PR

**Files:**
- Modify: `docs/WFCTL.md`

Steps:

1. Add a concise `wfctl editor-bundle` note explaining that descriptor-only
   message contract plugins, including `workflow-plugin-control-plane`, are
   loaded through `plugin.contracts.json` and descriptor-set refs; this does
   not execute the plugin binary or grant host authority.
2. Run:

   ```bash
   GOWORK=off go test ./cmd/wfctl ./schema ./plugin/external/... -count=1
   GOWORK=off go test ./... -count=1
   GOWORK=off go list -deps ./cmd/wfctl | rg 'github.com/GoCodeAlone/workflow-plugin-control-plane'
   git diff --check
   rg -n '/Users/[[:alnum:]_.-]+' docs/plans/2026-06-12-control-plane-descriptor-validation-design.md docs/plans/2026-06-12-control-plane-descriptor-validation-design-review.md docs/plans/2026-06-12-control-plane-descriptor-validation.md docs/WFCTL.md cmd/wfctl/control_plane_descriptor_validation_test.go
   ```

   Expected: Go tests exit 0; dependency scan exits 1 with no matches; diff
   check exits 0; machine-path scan exits 1 with no matches.
3. Push, create workflow PR, run `gh --version` immediately before and after
   `gh pr create`, add `copilot-pull-request-reviewer`, monitor CI/reviews,
   fix findings, and admin-squash merge only when green.

Rollback: revert workflow PR; no release tag is required because no runtime API
or production behavior is added.

### Task 5: Close T566 In Workflow-Compute Roadmap

**Files:**
- Modify in `workflow-compute`: `SPEC.md`
- Modify in `workflow-compute`: `docs/plans/deferred.md`
- Modify in `workflow-compute`: `provider_catalog_boundary_test.go`
- Create in `workflow-compute`: `docs/plans/2026-06-12-control-plane-descriptor-validation-design.md`
- Create in `workflow-compute`: `docs/plans/2026-06-12-control-plane-descriptor-validation.md`

Steps:

1. Create a workflow-compute worktree branch
   `docs/control-plane-descriptor-validation` from current `origin/main`.
2. Backport a compact design/plan evidence note that cites:
   - workflow PR number and merge commit;
   - workflow PR checks/reviews;
   - post-merge workflow main CI evidence;
   - released `workflow-plugin-control-plane v0.1.0` fixture evidence.
3. Add/update guard tests requiring:
   - `T566|x|add Workflow and wfctl descriptor-bundle validation fixtures`;
   - `T567`, `T568`, and `T569` remain queued;
   - evidence strings for workflow PR, merge commit, CI run IDs, and
     descriptor-only/no-cycle proof;
   - no wording that transfers host-owned authz, persistence, dispatch,
     credential, trust-root, private-key, rollout, approval, or deployment
     authority to the public package.
4. Update `SPEC.md` and `docs/plans/deferred.md` to mark T566 complete while
   keeping T567-T569 queued.
5. Verify in workflow-compute:

   ```bash
   GOWORK=off go test . -run 'TestSpecRecordsControlPlaneDescriptorValidationPhase|TestSpecRecordsControlPlanePublicPackagePhase|TestControlPlaneAuthorityBoundaryRejectsAuthorityTransferLanguage' -count=1
   GOWORK=off go test ./... -count=1
   git diff --check
   rg -n '/Users/[[:alnum:]_.-]+' SPEC.md docs/plans/deferred.md provider_catalog_boundary_test.go docs/plans/2026-06-12-control-plane-descriptor-validation-design.md docs/plans/2026-06-12-control-plane-descriptor-validation.md
   ```

   Expected: Go tests exit 0; diff check exits 0; machine-path scan exits 1
   with no matches.

Rollback: revert workflow-compute closure PR; workflow PR can remain as test
coverage if valid.

### Task 6: Review, Align, Lock, Merge, And Record Phase

**Files:**
- Create: `docs/plans/2026-06-12-control-plane-descriptor-validation-plan-review.md`
- Create: `docs/plans/2026-06-12-control-plane-descriptor-validation-alignment.md`
- Create/delete during lifecycle:
  `docs/plans/2026-06-12-control-plane-descriptor-validation.md.scope-lock`
- Update outside PR after merge:
  `/Users/<name>/workspace/.autodev/state/phase-progress.jsonl`

Steps:

1. Run adversarial plan review and alignment check before implementation.
2. On alignment PASS, apply scope lock:

   ```bash
   bash /Users/<name>/.codex/plugins/cache/autodev-marketplace/autodev/6.5.0/hooks/scope-lock-apply docs/plans/2026-06-12-control-plane-descriptor-validation.md
   ```

3. Before each PR, verify scope against the plan and branch state.
4. For both PRs, run `gh --version` immediately before and after `gh pr create`,
   add `copilot-pull-request-reviewer`, monitor CI/reviews, fix findings, and
   admin merge only when green.
5. After both PRs are green, complete the scope lock:

   ```bash
   bash /Users/<name>/.codex/plugins/cache/autodev-marketplace/autodev/6.5.0/hooks/scope-lock-complete docs/plans/2026-06-12-control-plane-descriptor-validation.md --evidence "<workflow PR/CI/review plus workflow-compute closure evidence>"
   ```

6. Append phase progress:

   ```json
   {"ts":"<UTC>","ev":"phase","pl":"2026-06-12-control-plane-descriptor-validation.md","ph":"T566 Workflow/wfctl control-plane descriptor validation","st":"done","e":"<workflow PR/CI plus workflow-compute closure evidence>","nx":"T567 DigitalOcean control-plane provider handoff fixture phase"}
   ```

Rollback: if the lock is completed before workflow-compute merge, include the
completion commit in the closure PR and re-run CI. If completion happens after
merge, open a tiny closure PR before T567.
