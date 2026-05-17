# IaCProvider.Apply Hard-Removal Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Hard-delete `IaCProvider.Apply` across workflow + 4 IaC plugins (aws/gcp/azure/DO) + 4 registry manifests, eliminating the sentinel-stub runtime-failure surface DO v1.4.0 introduced.

**Architecture:** 10-PR coordinated cascade. PR 1 ships workflow `v0.56.0-rc1` (proto deletion + interface deletion + load-time Capabilities-RPC gate). PRs 2-5 ship plugin `v2.0.0-rc1` tags in parallel (drop Apply method + iacserver handler). PR 6 runs conformance matrix + tags `workflow v0.56.0`. PRs 7-10 ship plugin `v2.0.0` final tags + registry manifest bumps as fan-out from PR 6.

**Tech Stack:** Go 1.24, gRPC (buf for proto), GoReleaser v2, GoCodeAlone/modular framework. No new dependencies introduced.

**Base branch:** `main` (per-PR feature branches: `feat/699-*`)

---

## Scope Manifest

**PR Count:** 10
**Tasks:** 36
**Estimated Lines of Change:** ~1500 deletions, ~200 additions (mostly deletions; the design is force-cutover cleanup)

**Out of scope:**
- Approach B (optional `IaCProviderLegacyApplier` service) — documented as soft-add-back rollback only, NOT shipped here.
- General registry manifest-derivation refactor (the existing 4 stale registry pins are caught up in PRs 7-10 incidentally; the larger derivation refactor remains a separate followup queue item).
- Other IaC interface segregation work (e.g., extracting `BootstrapStateBackend` to its own optional service) — scope-locked OUT.
- Engine-side compensation auto-attempt (Phase 2.4 deferred candidate per project_open_followup_queue.md) — separate cascade.
- Module/Step/Trigger interface changes — unchanged; this is IaC-only per ADR 0024.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | workflow: IaCProvider.Apply removal + Capabilities-RPC load gate (rc1) | 1, 2, 3, 4, 5, 6, 7, 8, 9 | feat/699-iac-apply-removal-rc |
| 2 | workflow-plugin-digitalocean: drop Apply (v2.0.0-rc1) | 10, 11, 12 | feat/699-drop-apply |
| 3 | workflow-plugin-aws: drop Apply (v2.0.0-rc1) | 13, 14, 15 | feat/699-drop-apply |
| 4 | workflow-plugin-gcp: drop Apply (v2.0.0-rc1) | 16, 17, 18, 19 | feat/699-drop-apply |
| 5 | workflow-plugin-azure: drop Apply (v2.0.0-rc1) | 20, 21, 22 | feat/699-drop-apply |
| 6 | workflow: plugin conformance matrix + final v0.56.0 tag | 23, 24, 25 | feat/699-conformance-final |
| 7 | workflow-plugin-digitalocean: final v2.0.0 + registry manifest | 26, 27, 28 | feat/699-final |
| 8 | workflow-plugin-aws: final v2.0.0 + registry manifest | 29, 30, 31 | feat/699-final |
| 9 | workflow-plugin-gcp: final v2.0.0 + registry manifest | 32, 33, 34 | feat/699-final |
| 10 | workflow-plugin-azure: final v2.0.0 + registry manifest | 35, 36 | feat/699-final |

**Status:** Draft

---

## Cross-cutting prerequisites

Before PR 1 starts, the executing agent MUST:

1. **File gcp sync-plugin-version followup issue.** `gh issue create -R GoCodeAlone/workflow-plugin-gcp --title "sync-plugin-version workflow: plugin.json (1.1.0) lags live tag (v1.2.0)" --body "Discovered during workflow#699 cascade. plugin.json on main shows version: 1.1.0 but live tag is v1.2.0. The sync-plugin-version GitHub Action should bump plugin.json on tag — verify wiring."`. Capture issue number; patch into the design doc + PR 4 description as `m-NEW-1 followup`.

2. **Clean up stale worktrees that reference deleted symbols.** Run from `/Users/jon/workspace/workflow`: `grep -rln 'wfctlhelpers.DispatchVersionV2\|wfctlhelpers.ComputePlanVersionDeclarer\|pb.ApplyResult\|pb.ApplyRequest\|pb.ApplyResponse\|applyResultFromPB' --include='*.go' _worktrees/ .claude/worktrees/`. For each matching worktree: rebase onto post-PR-1 main OR `git worktree remove --force <path>` if abandoned. Cycle-3 reviewer identified `_worktrees/wf663-topo`, `_worktrees/phase-b-core-deletion`, `_worktrees/phase2.5-cleanup` as likely candidates but the grep is the source of truth.

3. **Verify A1 hasn't drifted.** For each `workflow-plugin-{aws,gcp,azure,digitalocean}`: `grep -q '"v2"' internal/iacserver.go` MUST exit 0. If any plugin no longer declares `ComputePlanVersion: "v2"` in its Capabilities, halt the cascade — the design assumes all 4 are v2.

---

## PR 1 — workflow rc1

**Branch:** `feat/699-iac-apply-removal-rc`
**Final tag:** `v0.56.0-rc1`

### Task 1: Delete v1 dispatch branches in cmd/wfctl/infra_apply.go (both call sites)

**Files:**
- Modify: `cmd/wfctl/infra_apply.go:465-540` (function `runInfraApply`)
- Modify: `cmd/wfctl/infra_apply.go:1660-1730` (function `applyPrecomputedPlanWithStore`, declared at `:1604`)

**Step 1: Verify both call sites exist BEFORE editing**

```bash
grep -n usedV2Dispatch cmd/wfctl/infra_apply.go
```

Expected: 6 lines (467, 472, 536, 1662, 1664, 1711) — both functions have the v1/v2 dispatch fork.

**Step 2: Write the failing test for runInfraApply collapsed path**

`cmd/wfctl/infra_apply_v2_only_test.go` (new file):

```go
package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestInfraApply_V2OnlyDispatch_NoV1Branch asserts runInfraApply collapses
// to a single v2-only dispatch after workflow#699 removes provider.Apply.
// The presence of any conditional branch on a v1-vs-v2 selector is a
// regression: per ADR 0024, v2 is the only supported dispatch.
func TestInfraApply_V2OnlyDispatch_NoV1Branch(t *testing.T) {
	t.Run("collapses dispatch when typedIaCAdapter declares no ComputePlanVersion method", func(t *testing.T) {
		// stub provider satisfies the trimmed interfaces.IaCProvider
		// (no Apply method) and has no ComputePlanVersion declarer.
		// runInfraApply MUST route through wfctlhelpers.ApplyPlanWithHooks
		// and MUST NOT type-assert against a v1 dispatch.
		var p interfaces.IaCProvider = &stubV2OnlyProvider{}
		if _, ok := p.(interface{ Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) }); ok {
			t.Fatalf("provider unexpectedly satisfies legacy Apply interface")
		}
	})
}

type stubV2OnlyProvider struct{}

func (*stubV2OnlyProvider) Name() string                              { return "stub" }
func (*stubV2OnlyProvider) Version() string                           { return "0.0.0" }
func (*stubV2OnlyProvider) Initialize(context.Context, map[string]any) error { return nil }
func (*stubV2OnlyProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (*stubV2OnlyProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) { return nil, nil }
func (*stubV2OnlyProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) { return nil, nil }
func (*stubV2OnlyProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) { return nil, nil }
func (*stubV2OnlyProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) { return nil, nil }
func (*stubV2OnlyProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) { return nil, nil }
func (*stubV2OnlyProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) { return nil, nil }
func (*stubV2OnlyProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) { return nil, nil }
func (*stubV2OnlyProvider) SupportedCanonicalKeys() []string { return nil }
func (*stubV2OnlyProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) { return nil, nil }
func (*stubV2OnlyProvider) Close() error { return nil }
```

**Step 3: Run test — expect compile fail (stub has Apply removed but interface still has it)**

```bash
go test -run TestInfraApply_V2OnlyDispatch_NoV1Branch ./cmd/wfctl/
```

Expected: compile error — `*stubV2OnlyProvider does not implement interfaces.IaCProvider (missing method Apply)`. This proves the interface still declares Apply.

**Step 4: Edit runInfraApply (lines 465-540) — collapse v1/v2 branch**

In `cmd/wfctl/infra_apply.go`, replace the block at lines 465-487 (the `if wfctlhelpers.DispatchVersionFor(provider) == wfctlhelpers.DispatchVersionV2 { ... } else { result, err = provider.Apply(ctx, &plan) }` block) with:

```go
// v2 is the only supported dispatch per ADR 0024 + workflow#699.
hooks := statePersistenceHooks(store, secretsProvider, provider, providerType, hydratedOut)
result, err := applyV2ApplyPlanWithHooksFn(ctx, provider, &plan, hooks)
if result != nil {
	printDriftReportIfAny(w, result)
}
```

Remove the `usedV2Dispatch` variable declaration and any references in the surrounding error/result handling at line 536 (replace `if usedV2Dispatch { ... }` with the body unconditionally).

**Step 5: Edit applyPrecomputedPlanWithStore (lines 1660-1730) — same collapse**

Apply the identical collapse pattern to the block at lines 1660-1722. Remove `usedV2Dispatch` variable at `:1662`, conditional at `:1711`.

**Step 6: Verify both collapses removed all 5 `usedV2Dispatch` references**

```bash
grep -c usedV2Dispatch cmd/wfctl/infra_apply.go
```

Expected: `0`

**Step 7: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_v2_only_test.go
git commit -m "feat(wfctl): collapse v1/v2 apply dispatch to v2-only (workflow#699 PR 1 task 1)"
```

**Rollback:** revert commit → both v1 dispatch branches restored.

---

### Task 2: Delete typedIaCAdapter.Apply + ComputePlanVersion + applyResultFromPB + ApplyRequest encoding

**Files:**
- Modify: `cmd/wfctl/iac_typed_adapter.go:345-355` (`Apply` method)
- Modify: `cmd/wfctl/iac_typed_adapter.go:447-461` (`ComputePlanVersion` method)
- Modify: `cmd/wfctl/iac_typed_adapter.go:1193-1290` (`applyResultFromPB` function + helpers)
- Modify: `cmd/wfctl/iac_typed_adapter.go:1340-1350` (`_ wfctlhelpers.ComputePlanVersionDeclarer = (*typedIaCAdapter)(nil)` interface assertion)
- Modify: `cmd/wfctl/iac_typed_adapter_test.go:500-600` (tests that reference `pb.ApplyResult` / `pb.ApplyResponse` / `pb.ApplyRequest`)

**Step 1: Verify symbols exist**

```bash
grep -n 'func (a \*typedIaCAdapter) Apply\b\|func (a \*typedIaCAdapter) ComputePlanVersion\|func applyResultFromPB\|_ wfctlhelpers.ComputePlanVersionDeclarer' cmd/wfctl/iac_typed_adapter.go
```

Expected: 4 line matches (one per symbol above).

**Step 2: Delete methods + helper + interface assertion**

Open `cmd/wfctl/iac_typed_adapter.go` and delete the four blocks. Preserve all other adapter methods (Plan, Destroy, Status, etc.).

Open `cmd/wfctl/iac_typed_adapter_test.go` and delete tests that import `pb.ApplyResult`, `pb.ApplyResponse`, or `pb.ApplyRequest` (lines ~510-600 per cycle-3 inventory).

**Step 3: Run build (expect failure — `wfctlhelpers.ComputePlanVersionDeclarer` is still referenced)**

```bash
go build ./cmd/wfctl/
```

Expected: compile error — `undefined: wfctlhelpers.ComputePlanVersionDeclarer` (because the type assertion at `:1348` is deleted but the import isn't reachable yet from Task 3).

**Step 4: (Defer build verification to after Task 3.)**

Move on.

**Step 5: Commit (compile may not be green yet — sequential PR within single branch)**

```bash
git add cmd/wfctl/iac_typed_adapter.go cmd/wfctl/iac_typed_adapter_test.go
git commit -m "feat(wfctl): delete typedIaCAdapter.Apply + ComputePlanVersion + applyResultFromPB (workflow#699 PR 1 task 2)"
```

**Rollback:** revert commit → adapter Apply restored.

---

### Task 3: Delete iac/wfctlhelpers/dispatch.go

**Files:**
- Delete: `iac/wfctlhelpers/dispatch.go`
- Delete: `iac/wfctlhelpers/dispatch_test.go` (if exists)

**Step 1: Verify nothing else imports the deleted symbols**

```bash
grep -rn 'wfctlhelpers.DispatchVersionV2\|wfctlhelpers.DispatchVersionFor\|wfctlhelpers.ComputePlanVersionDeclarer' --include='*.go' . | grep -v _worktrees | grep -v .claude/worktrees
```

Expected: 0 lines (Tasks 1-2 already removed the only consumers; cross-cutting prereq #2 cleaned worktrees).

**Step 2: Delete the files**

```bash
git rm iac/wfctlhelpers/dispatch.go iac/wfctlhelpers/dispatch_test.go 2>/dev/null || git rm iac/wfctlhelpers/dispatch.go
```

**Step 3: Run build**

```bash
go build ./...
```

Expected: green (no remaining references to deleted symbols).

**Step 4: Commit**

```bash
git add -u
git commit -m "feat(iac): delete wfctlhelpers/dispatch.go — v2 is sole dispatch path (workflow#699 PR 1 task 3)"
```

**Rollback:** revert commit → dispatch helpers restored.

---

### Task 4: Add load-time Capabilities-RPC gate in discoverAndLoadIaCProvider

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go:192-250` (function `discoverAndLoadIaCProvider`)
- Modify: `cmd/wfctl/deploy_providers.go:162-170` (function `findIaCPluginDir` switch — add deprecation log)
- Create: `cmd/wfctl/deploy_providers_load_gate_test.go`

**Step 1: Write failing test for the load gate**

`cmd/wfctl/deploy_providers_load_gate_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// TestDiscoverAndLoadIaCProvider_LoadGate_RejectsV1 asserts a plugin that
// returns CapabilitiesResponse.ComputePlanVersion="v1" (or empty) is
// rejected at load time with an actionable error pointing to workflow#699.
func TestDiscoverAndLoadIaCProvider_LoadGate_RejectsV1(t *testing.T) {
	cases := []struct {
		name      string
		cpv       string
		wantInErr string
	}{
		{name: "empty", cpv: "", wantInErr: "workflow#699"},
		{name: "v1", cpv: "v1", wantInErr: "workflow#699"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyComputePlanVersionV2(tc.cpv, "plugin-x")
			if err == nil {
				t.Fatalf("expected reject for cpv=%q; got nil", tc.cpv)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantInErr)
			}
		})
	}
}

// TestDiscoverAndLoadIaCProvider_LoadGate_AcceptsV2 — happy path.
func TestDiscoverAndLoadIaCProvider_LoadGate_AcceptsV2(t *testing.T) {
	if err := verifyComputePlanVersionV2("v2", "plugin-x"); err != nil {
		t.Fatalf("expected accept for cpv=v2; got %v", err)
	}
}
```

**Step 2: Run test — expect FAIL (`verifyComputePlanVersionV2` undefined)**

```bash
go test -run TestDiscoverAndLoadIaCProvider_LoadGate ./cmd/wfctl/
```

Expected: FAIL — `undefined: verifyComputePlanVersionV2`.

**Step 3: Implement `verifyComputePlanVersionV2` helper + wire into `discoverAndLoadIaCProvider`**

Append to `cmd/wfctl/deploy_providers.go`:

```go
// verifyComputePlanVersionV2 rejects a plugin whose
// CapabilitiesResponse.compute_plan_version is not "v2". Called from
// discoverAndLoadIaCProvider after the typed adapter handshake; the
// rejection error is operator-facing — it MUST name the plugin and
// point at workflow#699.
func verifyComputePlanVersionV2(cpv, pluginName string) error {
	if cpv == "v2" {
		return nil
	}
	return fmt.Errorf(
		"plugin %q declares CapabilitiesResponse.compute_plan_version = %q; "+
			"workflow v0.56.0+ requires \"v2\" (see workflow#699 — upgrade plugin to v2.0.0 or higher)",
		pluginName, cpv,
	)
}
```

Modify `discoverAndLoadIaCProvider` (locate the line right after `typedIaCAdapter` is constructed and before it is returned). Add:

```go
// Per workflow#699: gate provider load on the typed
// CapabilitiesResponse.compute_plan_version field. The 10s timeout
// bounds a hung plugin handshake; the call is NOT shared with the
// long-lived fetchCapabilities cache (transient failures must not
// poison the adapter for the entire invocation).
capsCtx, capsCancel := context.WithTimeout(ctx, 10*time.Second)
defer capsCancel()
capsResp, capsErr := adapter.required.Capabilities(capsCtx, &pb.CapabilitiesRequest{})
if capsErr != nil {
	return nil, nil, fmt.Errorf("plugin %q: Capabilities RPC failed: %w (see workflow#699)", pluginName, capsErr)
}
if err := verifyComputePlanVersionV2(capsResp.GetComputePlanVersion(), pluginName); err != nil {
	return nil, nil, err
}
```

Leave `findIaCPluginDir`'s switch at `:162-170` UNCHANGED (it already accepts `"", "v1", "v2"`). Move the deprecation log emission to `discoverAndLoadIaCProvider` (cycle-2 plan-review Finding #7 fix — `findIaCPluginDir` may be called multiple times per wfctl invocation, but `discoverAndLoadIaCProvider` is called exactly once per resolve):

```go
// Inside discoverAndLoadIaCProvider, AFTER findIaCPluginDir returns + BEFORE the Capabilities gate.
// computePlanVersion is the value findIaCPluginDir returned.
switch computePlanVersion {
case "":
	log.Printf("plugin %q: deprecation — manifest iacProvider.computePlanVersion is empty; declare \"v2\" explicitly (workflow#699)", pluginName)
case "v1":
	log.Printf("plugin %q: deprecation — manifest iacProvider.computePlanVersion=\"v1\"; load-time gate will reject this (workflow#699)", pluginName)
}
```

**Step 4: Run test**

```bash
go test -run TestDiscoverAndLoadIaCProvider_LoadGate ./cmd/wfctl/ -v
```

Expected: PASS on all 3 sub-tests.

**Step 5: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_load_gate_test.go
git commit -m "feat(wfctl): load-time Capabilities-RPC gate enforces ComputePlanVersion=v2 (workflow#699 PR 1 task 4)"
```

**Rollback:** revert commit → no load-time gate; the deleted v1 dispatch from Task 1 leaves no enforcement, so this rollback MUST also revert Task 1.

---

### Task 5: Delete rpc Apply from iac.proto + delete dead messages + regenerate

**Files:**
- Modify: `plugin/external/proto/iac.proto:34` (delete `rpc Apply` line; add removal comment)
- Modify: `plugin/external/proto/iac.proto:330-410` (delete `message ApplyRequest`, `message ApplyResponse`, `message ApplyResult`, `message ActionResult`)
- Modify: `plugin/external/proto/iac.pb.go` (regenerated)
- Modify: `plugin/external/proto/iac_grpc.pb.go` (regenerated)
- Modify: `Makefile` (add lint step)

**Step 1: Inspect the proto messages slated for deletion + verify ActionResult has zero external consumers (cycle-2 plan-review Finding #6 fix)**

```bash
grep -n 'message ApplyRequest\|message ApplyResponse\|message ApplyResult\|message ActionResult\|rpc Apply' plugin/external/proto/iac.proto
grep -n 'ActionResult\b' plugin/external/proto/iac.proto
```

Expected: first grep returns ≥5 line matches. Second grep MUST show `ActionResult` referenced ONLY inside `message ActionResult { ... }` block AND inside `message ApplyResult.actions = 7` field. If any OTHER reference exists (e.g., from FinalizeApply or hook telemetry), HALT and re-design — ActionResult cannot be deleted.

**Step 2: Edit iac.proto**

In `plugin/external/proto/iac.proto`, delete `rpc Apply(ApplyRequest) returns (ApplyResponse);` (line 34). Add right above the `service IaCProviderRequired` definition (or as the last line inside it):

```
  // Method "Apply" was removed per workflow#699 (2026-05-17); v2 dispatch
  // routes through ResourceDriver per-action + IaCProviderFinalizer.
  // Do not re-introduce. CI lint guards re-appearance (see Makefile).
```

Delete the four message blocks (`ApplyRequest`, `ApplyResponse`, `ApplyResult`, `ActionResult`). Each is contiguous; the design's PR 1 step 5 enumerates them.

**Step 3: Regenerate proto**

```bash
cd plugin/external/proto && buf generate && cd -
```

If `buf` is not installed: install per the existing project setup (see `CONTRIBUTING.md` or `Makefile` target `proto-gen`).

Expected: `iac.pb.go` and `iac_grpc.pb.go` shrink (no `ApplyRequest`/`Response`/`Result`/`ActionResult` Go types; no `Apply` method on the `IaCProviderRequiredClient` interface).

**Step 4: Add Makefile lint step**

In `Makefile`, locate the existing `lint:` target (single-line `golangci-lint run --timeout=5m` per cycle-2 plan-review I-NEW finding). Convert it to a multi-line recipe and append the proto guard:

```makefile
lint:
	golangci-lint run --timeout=5m
	@if grep -q 'rpc Apply' plugin/external/proto/iac.proto; then \
		echo "workflow#699: rpc Apply re-introduced in iac.proto; see decisions/0024-iac-typed-force-cutover.md"; \
		exit 1; \
	else \
		echo "workflow#699 guard: rpc Apply correctly absent"; \
	fi
```

If `buf` is not installed in the environment running this PR: install via `go install github.com/bufbuild/buf/cmd/buf@latest`. The repo's `buf.gen.yaml` is the generation config; no separate `proto-gen` Makefile target exists today.

**Step 5: Run build (expect FAIL — interface still has Apply, plugins haven't dropped their handlers yet)**

```bash
go build ./...
```

Expected: compile errors in `plugin/external/sdk/iacserver.go` (type-assert references `pb.IaCProviderRequiredServer.Apply` no longer exists). Tasks 6+7 fix.

**Step 6: Commit**

```bash
git add plugin/external/proto/iac.proto plugin/external/proto/iac.pb.go plugin/external/proto/iac_grpc.pb.go Makefile
git commit -m "feat(proto): delete rpc Apply + ApplyRequest/Response/Result/ActionResult; CI lint guard (workflow#699 PR 1 task 5)"
```

**Rollback:** revert commit → proto restored; CI lint removed.

---

### Task 6: Delete Apply from interfaces.IaCProvider

**Files:**
- Modify: `interfaces/iac_provider.go:17` (delete `Apply(...)` line + its comment if standalone)

**Step 1: Edit interfaces/iac_provider.go**

Delete the line:

```go
Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error)
```

from the `IaCProvider` interface. Leave `Plan`, `Destroy`, and the rest unchanged.

**Step 2: Run build**

```bash
go build ./interfaces/ ./iac/... ./plugin/... ./cmd/wfctl/
```

Expected: SOME packages green, SOME still broken (the `cmd/iac-codemod` package — handled in Task 9; the SDK iacserver still type-asserts — handled in Task 7).

**Step 3: Commit**

```bash
git add interfaces/iac_provider.go
git commit -m "feat(interfaces): delete IaCProvider.Apply method (workflow#699 PR 1 task 6)"
```

**Rollback:** revert commit → interface restored.

---

### Task 7: Update plugin/external/sdk/iacserver.go type-assert

**Files:**
- Modify: `plugin/external/sdk/iacserver.go:112-121` (`required` type assertion against `pb.IaCProviderRequiredServer`)

**Step 1: Verify type-assert location**

```bash
grep -n 'pb.IaCProviderRequiredServer\|required, ok := provider' plugin/external/sdk/iacserver.go
```

Expected: line 112 + 114.

**Step 2: Update godoc / comments referencing Apply**

In `plugin/external/sdk/iacserver.go`, remove any comments that enumerate `Apply` as a required method. The Go type assertion at line 112 (`provider.(pb.IaCProviderRequiredServer)`) AUTOMATICALLY tightens because `pb.IaCProviderRequiredServer` no longer declares `Apply` after Task 5.

**Step 3: Run build**

```bash
go build ./plugin/external/sdk/...
```

Expected: green (the type assertion compiles against the trimmed pb interface).

**Step 4: Commit**

```bash
git add plugin/external/sdk/iacserver.go
git commit -m "feat(sdk): align iacserver type-assert with trimmed pb.IaCProviderRequiredServer (workflow#699 PR 1 task 7)"
```

**Rollback:** revert commit → comment changes restored (auto-tightening of type-assert remains via Task 5).

---

### Task 8: Tighten wftest/bdd + iactest fakeprovider + delete obsolete test coverage

**Files:**
- Modify: `iac/iactest/fakeprovider.go:42-46` (delete `DispatchVersion` field) + `:69-72` (delete `ComputePlanVersion()` method) — cycle-2 plan-review C2 fix; this stub is consumed by 8+ `cmd/wfctl/*_test.go` files and will break `go build ./...` in Task 9 if not cleaned up here.
- Modify: `wftest/bdd/strict_iac.go` (drop `Apply` row from `iacServiceChecks`)
- Modify: `cmd/wfctl/iac_loader_gate_test.go` (drop v1 dispatch coverage)
- Modify: `cmd/wfctl/plugin_audit_iac_test.go` (drop v1 dispatch coverage)
- Modify: `cmd/wfctl/plugin_audit.go` (drop v1 dispatch coverage)
- Modify: `plugin/external/proto/iac_proto_test.go` (delete `pb.ApplyResult`-using tests)
- Modify: `iac/iactest/fakeprovider_test.go` (if it exists; verify with `ls iac/iactest/`) — drop any `DispatchVersion`/`ComputePlanVersion` coverage. Update consumer tests in `cmd/wfctl/` that set `iactest.NoopProvider{DispatchVersion: "v2"}` — remove the field.

**Step 1: Audit test files**

```bash
grep -rn '\.Apply(\|pb.ApplyResult\|pb.ApplyRequest\|pb.ApplyResponse' cmd/wfctl/ wftest/ plugin/external/proto/ --include='*.go'
```

Capture line numbers per file. For each match: if it's a v1 Apply call OR a deleted-proto-message reference, delete the test function entirely (don't try to refactor; the v2 coverage in `iac/wfctlhelpers/apply*_test.go` is comprehensive).

**Step 2: Edit each file**

Delete the matched tests / drop the `Apply` row from the `iacServiceChecks` slice/map literal in `wftest/bdd/strict_iac.go`.

**Step 3: Run full test suite**

```bash
go test ./wftest/... ./cmd/wfctl/... ./plugin/external/...
```

Expected: green (or skip-marker if any test needs a follow-up; halt and address).

**Step 4: Commit**

```bash
git add wftest/bdd/strict_iac.go cmd/wfctl/iac_loader_gate_test.go cmd/wfctl/plugin_audit_iac_test.go cmd/wfctl/plugin_audit.go plugin/external/proto/iac_proto_test.go
git commit -m "test: drop v1 Apply coverage + iacServiceChecks row (workflow#699 PR 1 task 8)"
```

**Rollback:** revert commit → tests restored (but they'd fail against the trimmed interface).

---

### Task 9: Delete cmd/iac-codemod + Makefile cleanup + run full build/vet + tag rc1

**Files:**
- Delete: `cmd/iac-codemod/` (entire directory)
- Modify: `Makefile` — delete `.PHONY` entries `build-iac-codemod` and `migrate-providers` (line 1); delete `build-iac-codemod:` target block (~lines 86-91); delete `migrate-providers:` target block (~lines 113-125); remove `iac-codemod` from any `clean:` rule (~line 131). Verify with `grep -c iac-codemod Makefile` → 0 after edit.
- Modify: `docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md` (if it exists and references iac-codemod) — strike codemod section as completed-and-removed.
- Modify: `CHANGELOG.md`

**Step 1: Verify codemod usage**

```bash
grep -rln 'cmd/iac-codemod\|iac-codemod' --include='*.go' --include='*.md' --include='*.yaml' . | grep -v _worktrees | grep -v .claude/worktrees
```

Expected: only references inside `cmd/iac-codemod/` itself OR design docs. No production callers.

**Step 2: Delete the directory**

```bash
git rm -r cmd/iac-codemod/
```

**Step 3: Run full build + vet pre-merge gate**

```bash
go build ./... && go vet ./... && go test ./...
```

Expected: green across the board.

**Step 4: Write CHANGELOG entry**

Append to `CHANGELOG.md`:

```markdown
## [Unreleased]

### Breaking changes (workflow#699)

- `interfaces.IaCProvider.Apply` removed. Plugins must implement v2 dispatch (declare `CapabilitiesResponse.compute_plan_version="v2"`) and drop their `Apply` method.
- `pb.IaCProviderRequired.Apply` RPC removed; `ApplyRequest`/`ApplyResponse`/`ApplyResult`/`ActionResult` proto messages deleted.
- `iac/wfctlhelpers/dispatch.go` package deleted (`ComputePlanVersionDeclarer`, `DispatchVersionFor`, `DispatchVersionV2`); v2 is the only supported dispatch.
- `cmd/iac-codemod` deleted (migration tool no longer needed).
- Load-time enforcement: `discoverAndLoadIaCProvider` now calls `Capabilities` at plugin handshake and rejects providers whose `ComputePlanVersion != "v2"`.
- Minimum plugin versions: aws v2.0.0+, gcp v2.0.0+, azure v2.0.0+, digitalocean v2.0.0+.
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(workflow): delete cmd/iac-codemod (dead post-cutover) + CHANGELOG (workflow#699 PR 1 task 9)"
```

**Step 6: Tag rc1**

```bash
git push -u origin feat/699-iac-apply-removal-rc
gh pr create --title "workflow#699: IaCProvider.Apply hard-removal (v0.56.0-rc1)" --body "$(cat <<'EOF'
## Summary
- Hard-delete IaCProvider.Apply across interface + proto + wfctl dispatch
- Load-time Capabilities-RPC gate replaces parse-time plugin.json switch
- Delete cmd/iac-codemod (migration tool obsolete)
- See docs/plans/2026-05-17-iac-provider-apply-removal-design.md for full design

## Test plan
- [ ] CI green (proto regen + lint + tests)
- [ ] Manual: build each plugin v2.0.0-rc1 against this rc; verify load-gate accepts v2 + rejects v1/empty
- [ ] Tag v0.56.0-rc1 on merge
EOF
)"
```

After merge: tag `v0.56.0-rc1` against the merged commit.

**Rollback:** revert PR → codemod restored, CHANGELOG entry removed.

---

## PR 2 — workflow-plugin-digitalocean rc1

**Repo:** `workflow-plugin-digitalocean`
**Branch:** `feat/699-drop-apply`
**Final tag:** `v2.0.0-rc1`

### Task 10: Bump workflow SDK pin to v0.56.0-rc1 + drop DOProvider.Apply + ErrApplyV1Removed

**Files:**
- Modify: `go.mod` (`github.com/GoCodeAlone/workflow` pin)
- Modify: `internal/provider.go` (delete `DOProvider.Apply` method + `ErrApplyV1Removed` constant)
- Delete: `internal/provider_apply_stub_test.go` (sentinel regression test obsolete)

**Step 1: Bump go.mod**

```bash
go get github.com/GoCodeAlone/workflow@v0.56.0-rc1
go mod tidy
```

If `v0.56.0-rc1` isn't on proxy.golang.org yet: use `GOFLAGS='-mod=mod' go mod edit -replace github.com/GoCodeAlone/workflow=github.com/GoCodeAlone/workflow@<commit-sha-of-rc1>` or `go.work` replace.

**Step 2: Delete DOProvider.Apply + ErrApplyV1Removed**

In `internal/provider.go`, delete the `func (p *DOProvider) Apply(...)` block (it returns `ErrApplyV1Removed`) and the `var ErrApplyV1Removed = ...` declaration.

```bash
git rm internal/provider_apply_stub_test.go
```

**Step 3: Run build + tests**

```bash
go build ./... && go test ./internal/...
```

Expected: green (the v1 stub was unreachable post-Phase-2.5).

**Step 4: Commit**

```bash
git add go.mod go.sum internal/provider.go
git rm internal/provider_apply_stub_test.go 2>/dev/null  # already done
git commit -m "feat: drop DOProvider.Apply + ErrApplyV1Removed; bump workflow v0.56.0-rc1 (workflow#699 PR 2 task 10)"
```

**Rollback:** revert commit → Apply stub + sentinel restored; SDK pin reverted.

---

### Task 11: Delete doIaCServer.Apply RPC handler + applyResultToPB helpers

**Files:**
- Modify: `internal/iacserver.go:263-277` (`doIaCServer.Apply` RPC handler)
- Modify: `internal/iacserver.go` (`applyResultToPB` helper — locate via grep)

**Step 1: Delete the RPC handler**

In `internal/iacserver.go`, delete the `func (s *doIaCServer) Apply(...)` function (~lines 263-277).

**Step 2: Delete applyResultToPB + helpers**

```bash
grep -n 'func applyResultToPB\|func actionsToPB\|func actionToPB' internal/iacserver.go
```

For each match: delete the function. They're now unreachable.

**Step 3: Run build**

```bash
go build ./...
```

Expected: green (the gRPC server no longer needs to handle the deleted RPC).

**Step 4: Commit**

```bash
git add internal/iacserver.go
git commit -m "feat: delete doIaCServer.Apply RPC handler + applyResultToPB helpers (workflow#699 PR 2 task 11)"
```

**Rollback:** revert commit → handler + helpers restored.

---

### Task 12: Bump plugin.json + tag rc1

**Files:**
- Modify: `plugin.json` (`version: 2.0.0-rc1`, `minEngineVersion: 0.56.0`)
- Modify: `CHANGELOG.md`

**Step 1: Bump manifest**

Edit `plugin.json`: set `"version": "2.0.0-rc1"` and `"minEngineVersion": "0.56.0"`.

**Step 2: CHANGELOG entry**

```markdown
## [2.0.0-rc1] - 2026-05-17

### Breaking changes
- Removed `DOProvider.Apply` and the `ErrApplyV1Removed` sentinel; v2 dispatch is the only path (per workflow#699).
- Removed `doIaCServer.Apply` gRPC handler.
- Requires workflow v0.56.0+.

### Reason
- Eliminates the runtime-failure surface from v1.4.0 sentinel-stub per ADR 0024 compile-time-safety mandate.
```

**Step 3: Verify with iacserver_test smoke**

```bash
go test ./internal/iacserver_test.go -v
```

Expected: PASS — Capabilities RPC still returns `ComputePlanVersion: "v2"`.

**Step 4: Commit, push, PR, tag**

```bash
git add plugin.json CHANGELOG.md
git commit -m "release: workflow-plugin-digitalocean v2.0.0-rc1 (workflow#699)"
git push -u origin feat/699-drop-apply
gh pr create --title "workflow#699: drop DOProvider.Apply (v2.0.0-rc1)" --body "Requires workflow v0.56.0-rc1+. See workflow#699."
```

After merge: tag `v2.0.0-rc1`.

**Rollback:** revert PR → DOProvider.Apply restored; tag yanked.

---

## PR 3 — workflow-plugin-aws rc1

**Repo:** `workflow-plugin-aws`
**Branch:** `feat/699-drop-apply`
**Final tag:** `v2.0.0-rc1`

### Task 13: Bump SDK pin + drop AWSProvider.Apply

**Files:**
- Modify: `go.mod`
- Modify: `provider/provider.go:237-300` (delete `AWSProvider.Apply` function)

**Steps:** mirror PR 2 Task 10, swap `DOProvider` for `AWSProvider`.

**Verification:** `go build ./... && go test ./provider/...` green.

**Commit:** `feat: drop AWSProvider.Apply; bump workflow v0.56.0-rc1 (workflow#699 PR 3 task 13)`

**Rollback:** revert commit.

---

### Task 14: Delete awsIaCServer.Apply RPC handler + applyResultToPB

**Step 1: Verify symbol locations (cycle-2 plan-review Finding #8 fix)**

```bash
grep -n 'func (s \*awsIaCServer) Apply\b\|func applyResultToPB\|func actionsToPB\|func actionToPB' internal/iacserver.go
```

Capture line numbers from output; use those (not the plan's approximate values).

**Files (line numbers verified in Step 1):**
- Modify: `internal/iacserver.go` (delete `awsIaCServer.Apply`)
- Modify: `internal/iacserver.go` (delete `applyResultToPB` + helpers)

**Steps:** mirror PR 2 Task 11.

**Commit:** `feat: delete awsIaCServer.Apply RPC handler + applyResultToPB (workflow#699 PR 3 task 14)`

**Rollback:** revert commit.

---

### Task 15: Bump plugin.json + tag rc1

**Files:**
- Modify: `plugin.json` (`version: 2.0.0-rc1`, `minEngineVersion: 0.56.0`)
- Modify: `CHANGELOG.md`

**Steps:** mirror PR 2 Task 12.

**Commit:** `release: workflow-plugin-aws v2.0.0-rc1 (workflow#699)`

After merge: tag `v2.0.0-rc1`.

**Rollback:** revert PR.

---

## PR 4 — workflow-plugin-gcp rc1

**Repo:** `workflow-plugin-gcp`
**Branch:** `feat/699-drop-apply`
**Final tag:** `v2.0.0-rc1`

### Task 16: File sync-plugin-version followup issue

**Step 1:** From any directory:

```bash
gh issue create -R GoCodeAlone/workflow-plugin-gcp \
  --title "sync-plugin-version workflow: plugin.json (1.1.0) lags live tag (v1.2.0)" \
  --body "Discovered during workflow#699 cascade. plugin.json on main shows version 1.1.0 but live tag is v1.2.0. The sync-plugin-version GitHub Action should bump plugin.json on tag — verify wiring. Cycle-3 adversarial-review finding m-NEW-1 from docs/plans/2026-05-17-iac-provider-apply-removal-design.md."
```

Capture the issue number; patch into PR description + design doc.

**Step 2:** (No code change.) Move on.

---

### Task 17: Bump SDK pin + drop GCPProvider.Apply

Mirror PR 2 Task 10, swap `DOProvider` → `GCPProvider`. File path: `provider/provider.go:226-...`.

**Commit:** `feat: drop GCPProvider.Apply; bump workflow v0.56.0-rc1 (workflow#699 PR 4 task 17)`

---

### Task 18: Delete gcpIaCServer.Apply RPC handler + applyResultToPB

Mirror PR 3 Task 14 — including the Step 1 grep verification of symbol locations. Do NOT assume line numbers; verify per-plugin.

**Commit:** `feat: delete gcpIaCServer.Apply RPC handler + applyResultToPB (workflow#699 PR 4 task 18)`

---

### Task 19: Bump plugin.json + tag rc1

Mirror PR 2 Task 12. Bump plugin.json `1.1.0` → `2.0.0-rc1`.

After merge: tag `v2.0.0-rc1`.

---

## PR 5 — workflow-plugin-azure rc1

**Repo:** `workflow-plugin-azure`
**Branch:** `feat/699-drop-apply`
**Final tag:** `v2.0.0-rc1`

### Task 20: Bump SDK pin + drop AzureProvider.Apply

Mirror PR 2 Task 10. File path: `internal/provider.go:138-...`.

**Commit:** `feat: drop AzureProvider.Apply; bump workflow v0.56.0-rc1 (workflow#699 PR 5 task 20)`

---

### Task 21: Delete azureIaCServer.Apply RPC handler + applyResultToPB

Mirror PR 3 Task 14 — including the Step 1 grep verification of symbol locations. Do NOT assume line numbers; verify per-plugin.

**Commit:** `feat: delete azureIaCServer.Apply RPC handler + applyResultToPB (workflow#699 PR 5 task 21)`

---

### Task 22: Bump plugin.json + tag rc1

Mirror PR 2 Task 12.

After merge: tag `v2.0.0-rc1`.

---

## PR 6 — workflow conformance + final tag

**Repo:** `workflow`
**Branch:** `feat/699-conformance-final`
**Final tag:** `v0.56.0`

### Task 23: Add plugin-conformance matrix to CI

**Pre-flight gate (run BEFORE opening PR 6):**

```bash
for p in aws gcp azure digitalocean; do
  gh release view v2.0.0-rc1 -R GoCodeAlone/workflow-plugin-$p > /dev/null || { echo "MISSING: workflow-plugin-$p v2.0.0-rc1"; exit 1; }
done
echo "All 4 plugin v2.0.0-rc1 tags present."
```

**Files:**
- Modify: `.github/workflows/ci.yml` (verified path; the repo uses `.yml` not `.yaml`) — add matrix step

**Step 1: Add CI step**

Append to the test job in `.github/workflows/ci.yaml`:

```yaml
  iac-plugin-conformance:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        plugin: [aws, gcp, azure, digitalocean]
    steps:
      - uses: actions/checkout@v4
        with:
          repository: GoCodeAlone/workflow-plugin-${{ matrix.plugin }}
          ref: v2.0.0-rc1
          path: plugin-${{ matrix.plugin }}
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Pin workflow SDK to PR commit
        run: |
          cd plugin-${{ matrix.plugin }}
          go mod edit -replace github.com/GoCodeAlone/workflow=${{ github.workspace }}
          go mod tidy
      - name: Build plugin
        run: cd plugin-${{ matrix.plugin }} && go build ./...
      - name: Run iacserver smoke
        run: cd plugin-${{ matrix.plugin }} && go test ./internal/iacserver_test.go -v
```

**Step 2: Push branch + open PR**

```bash
git checkout -b feat/699-conformance-final
git add .github/workflows/ci.yaml
git commit -m "ci: plugin-conformance matrix for IaC providers (workflow#699 PR 6 task 23)"
git push -u origin feat/699-conformance-final
gh pr create --title "workflow#699: conformance matrix + final v0.56.0 tag" --body "Validates all 4 plugin v2.0.0-rc1 tags build against this branch."
```

**Step 3: Wait for matrix to go green**

If any plugin fails: halt cascade. Investigate. Likely cause: a plugin still references a deleted symbol; fix in that plugin's repo before merging this PR.

**Rollback:** revert PR → matrix step removed.

---

### Task 24: Tag v0.56.0 final

**Step 1:** After PR 6 merges to main:

```bash
git checkout main && git pull
git tag v0.56.0
git push --tags
```

**Step 2:** Verify GoReleaser publishes:

```bash
gh release view v0.56.0
```

Expected: release notes auto-generated from CHANGELOG.

---

### Task 25: Update workflow memory + tracker

**Step 1:** Update `/Users/jon/.claude/projects/-Users-jon-workspace/memory/project_open_followup_queue.md`:

Mark workflow#699 done; add link to v0.56.0 release.

**Step 2:** Update `/Users/jon/.claude/projects/-Users-jon-workspace/memory/MEMORY.md` plugin inventory:

- workflow v0.56.0 (was v0.55.x)

**Step 3:** Create new memory file `/Users/jon/.claude/projects/-Users-jon-workspace/memory/project_workflow_699_apply_removal_shipped.md` documenting the cascade outcome.

(No git commit — memory files live in the user's local `~/.claude/`.)

---

## PR 7 — workflow-plugin-digitalocean final v2.0.0 + registry

**Repos:** `workflow-plugin-digitalocean` + `workflow-registry`
**Branches:** `feat/699-final` (plugin) + `feat/699-do-manifest` (registry)
**Final tag:** `v2.0.0` (plugin)

### Task 26: Bump SDK pin v0.56.0-rc1 → v0.56.0 + plugin.json v2.0.0

**Files:**
- Modify: `go.mod`
- Modify: `plugin.json`
- Modify: `CHANGELOG.md`

```bash
go get github.com/GoCodeAlone/workflow@v0.56.0
go mod tidy
# edit plugin.json: version: "2.0.0"
# edit CHANGELOG.md: promote rc1 entry to 2.0.0
git add -A
git commit -m "release: workflow-plugin-digitalocean v2.0.0 (workflow#699)"
git push -u origin feat/699-final
gh pr create --title "workflow#699: workflow-plugin-digitalocean v2.0.0" --body "Final tag bump after workflow v0.56.0."
```

After merge: tag `v2.0.0`.

**Rollback:** revert PR → v1.4.0 restored as recommended live tag.

---

### Task 27: Update workflow-registry DO manifest

**Files:**
- Modify: `workflow-registry/v1/plugins/digitalocean/manifest.json` (bump `version: 1.0.12` → `2.0.0`, `minEngineVersion: 0.51.0` → `0.56.0`)

**Step 1:** Edit the file:

```json
{
  "name": "workflow-plugin-digitalocean",
  "version": "2.0.0",
  "minEngineVersion": "0.56.0",
  ...
}
```

**Step 2:** Open PR:

```bash
cd /Users/jon/workspace/workflow-registry
git checkout -b feat/699-do-manifest
git add v1/plugins/digitalocean/manifest.json
git commit -m "feat(do): bump workflow-plugin-digitalocean to v2.0.0 (workflow#699)"
git push -u origin feat/699-do-manifest
gh pr create --title "workflow#699: bump DO plugin to v2.0.0" --body "Registry pin catches up from 1.0.12 → 2.0.0. Rollback floor: v1.4.0 (live tag before this cascade)."
```

After merge: registry is live immediately (no tag axis).

**Rollback:** revert PR → registry pin reverts to v1.0.12 (operators on pre-v2 install path); recommend `wfctl plugin install --force` and pin to v1.4.0 explicitly.

---

### Task 28: Smoke-validate DO v2.0.0 against workflow v0.56.0

**Step 1:** From a clean checkout:

```bash
mkdir -p /tmp/699-smoke && cd /tmp/699-smoke
git clone --depth 1 --branch v0.56.0 https://github.com/GoCodeAlone/workflow.git
git clone --depth 1 --branch v2.0.0 https://github.com/GoCodeAlone/workflow-plugin-digitalocean.git
cd workflow-plugin-digitalocean
go build -o ../do-plugin ./cmd/...
cd ../workflow
go build -o wfctl ./cmd/wfctl
./wfctl plugin info digitalocean --plugin-dir /tmp/699-smoke
```

Expected: plugin loads, `ComputePlanVersion=v2` accepted, no v1 dispatch warnings.

**Step 2:** No commit — record transcript at `docs/runtime-validation/2026-05-17-do-v2-smoke.md` in the workflow repo (separate housekeeping commit).

---

## PR 8 — workflow-plugin-aws final v2.0.0 + registry

### Task 29: Bump SDK pin + plugin.json v2.0.0

Mirror PR 7 Task 26. **Rollback floor:** aws v1.2.1.

### Task 30: Update workflow-registry aws manifest

Mirror PR 7 Task 27. File: `workflow-registry/v1/plugins/aws/manifest.json`. Pin currently `0.1.2` → bump to `2.0.0`.

### Task 31: Smoke-validate aws v2.0.0

Mirror PR 7 Task 28 (substitute `aws` for `digitalocean`).

---

## PR 9 — workflow-plugin-gcp final v2.0.0 + registry

### Task 32: Bump SDK pin + plugin.json v2.0.0

Mirror PR 7 Task 26. **Rollback floor:** gcp v1.2.0.

### Task 33: Update workflow-registry gcp manifest

Mirror PR 7 Task 27. File: `workflow-registry/v1/plugins/gcp/manifest.json`. Pin currently `0.1.3` → bump to `2.0.0`.

### Task 34: Smoke-validate gcp v2.0.0

Mirror PR 7 Task 28.

---

## PR 10 — workflow-plugin-azure final v2.0.0 + registry

### Task 35: Bump SDK pin + plugin.json v2.0.0

Mirror PR 7 Task 26. **Rollback floor:** azure v1.2.1.

### Task 36: Update workflow-registry azure manifest

Mirror PR 7 Task 27. File: `workflow-registry/v1/plugins/azure/manifest.json`. Pin currently `0.1.2` → bump to `2.0.0`.

**Explicit smoke-validate step (cycle-2 plan-review Finding #12 fix):**

```bash
mkdir -p /tmp/699-smoke-azure && cd /tmp/699-smoke-azure
git clone --depth 1 --branch v0.56.0 https://github.com/GoCodeAlone/workflow.git
git clone --depth 1 --branch v2.0.0 https://github.com/GoCodeAlone/workflow-plugin-azure.git
cd workflow-plugin-azure && go build -o ../azure-plugin ./cmd/... && cd ..
cd workflow && go build -o wfctl ./cmd/wfctl
./wfctl plugin info azure --plugin-dir /tmp/699-smoke-azure
```

Expected: plugin loads; `ComputePlanVersion=v2` accepted; no v1 dispatch warnings.
