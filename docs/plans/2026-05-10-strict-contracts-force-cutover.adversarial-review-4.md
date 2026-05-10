---
status: approved
review_cycle: 4
target: docs/plans/2026-05-10-strict-contracts-force-cutover.md
target_commit: 9a034372
phase: plan
date: 2026-05-10
verdict: PASS
---

# Adversarial Review — Strict-Contracts Force-Cutover Plan (Cycle 4, plan-phase)

**Phase:** plan (cycle 4)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover.md` (commit `9a034372`)
**Cycle 3 baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover.adversarial-review-3.md` (commit `4daf0b49`) — verdict FAIL with 1 Critical (C-1) + 1 Important (I-1).
**Design baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (rev5, 4 design-cycle PASSes).

**Verdict: PASS.** Both cycle 3 findings are resolved. No new Critical or Important findings surfaced. Per "don't nitpick" — Minor cosmetic items omitted.

---

## Cycle 3 finding-resolution verification

| Cycle 3 finding | Rev4 claim | Verified? |
|---|---|---|
| **C-1** Task 20-bis nonexistent `--state-file --dry-run` flags + Task 22 retained rejected `docker run ghcr.io/gocodealone/wfctl:v0.14.2` | Both tasks rewritten to use operator-captured fixture from existing Spaces backend; no broken CLI invocations remain | **YES — see verification below.** Both Task 20-bis (workflow PR 4 pre-flight, plan lines 1341-1391) AND Task 22 (core-dump PR 5 CI gate, plan lines 1475-1569) now share the operator-captured-fixture model. Neither task invokes the rejected `docker run ghcr.io/gocodealone/wfctl:v0.14.2` image. Neither task references the nonexistent `--state-file` or `--dry-run` flags. Verification mechanism is runnable. |
| **I-1** Task 4-bis pseudo-code structurally inconsistent with goplugin API + PR 3 Task 9 referenced wrong type name `sdk.ServeOptions` | Task 4-bis rewritten using per-plugin `GRPCServer(broker, s)` callback pattern (matches existing `servePlugin` at `serve.go:42-56`); PR 3 Task 9 updated to `sdk.IaCServeOptions` | **YES — see verification below.** Task 4-bis (plan lines 545-602) now uses `iacGRPCPlugin` struct with `GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error` per-plugin callback (line 568-570) inside `plugin.Serve(&plugin.ServeConfig{...})` (line 584-590); structurally identical shape to existing `servePlugin` at `serve.go:42-56`. PR 3 Task 9 line 987 now writes `sdk.IaCServeOptions{...}` matching the type defined at Task 4-bis line 555. Symbol-name collision resolved. |

---

## C-1 detailed verification

**Task 20-bis Step 1 (plan lines 1347-1361)** now reads:

```bash
# Operator-side: download from Spaces backend
aws s3 cp \
  s3://coredump-staging/iac-state/state.json \
  test/fixtures/state-v0.14.2.json \
  --endpoint-url=https://nyc3.digitaloceanspaces.com
```

The fixture is captured ONCE, manually, by the operator from the existing v0.14.2-produced state file in their staging Spaces backend. There is no `wfctl` invocation, no docker invocation, no `--state-file` or `--dry-run` flag dependency. The `s3 cp` command uses the documented Spaces S3-compatible endpoint and is independently verifiable.

**Task 20-bis Step 2** then writes a Go test (`cmd/wfctl/state_compat_test.go`) that asserts v1.0.0 reads the fixture cleanly via `TestStateFileCompat_v0_14_2_to_v1_0_0`. Implementer can use the existing `cmd/wfctl/deploy_state.go:LoadState()` (verified extant: `package main`, `func LoadState(path string) (*DeploymentState, error)`) — this is in-tree, no separate-binary refactor needed.

**Task 22 (plan lines 1475-1569)** mirrors Task 20-bis with a separate-binary form (`cmd/state_compat_check/main.go`) plus a CI gate. The CHANGELOG also matches: line 1477 explicitly cites cycle 3 C-1 and labels the rev4 mechanism "operator-captured fixture model." The Go program imports `github.com/GoCodeAlone/workflow/iac/state` which is illustrative — the plan's hedge (line 1538: "If the actual v1.0.0 state package import path differs, adapt") acknowledges the implementer chooses the closest extant decoder. This is documentation/import-path-rename only; the verification mechanism (decode the fixture via v1.0.0 code, fail loud on schema error) is runnable.

The fixture metadata header (lines 1487-1497) wraps the real state JSON with a `_fixture_meta` field which `json.Unmarshal` silently ignores when decoding into the workflow `DeploymentState` struct (verified at `cmd/wfctl/deploy_state.go:88-98`). Metadata-only header doesn't break the decoder.

**Cycle 3 C-1 RESOLVED.** No nonexistent flags remain. No rejected docker invocations remain. Mechanism is runnable end-to-end via existing on-disk decoders.

---

## I-1 detailed verification

**Task 4-bis pseudo-code (plan lines 553-594):**

```go
type iacGRPCPlugin struct {
    plugin.NetRPCUnsupportedPlugin
    provider any
}

func (p *iacGRPCPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
    return RegisterAllIaCProviderServices(s, p.provider)
}

func (p *iacGRPCPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
    return nil, fmt.Errorf("iac plugin GRPCClient not used; wfctl uses typed pb client directly")
}

func ServeIaCPlugin(provider any, opts IaCServeOptions) {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: opts.PluginInfo.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "iac": &iacGRPCPlugin{provider: provider},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

This structure is consistent with the existing `servePlugin` shape at `plugin/external/sdk/serve.go:42-60`. Service registration happens INSIDE the per-plugin `GRPCServer(broker, s)` callback, exactly matching the pattern cycle 3 I-1 required. The `RegisterAllIaCProviderServices` call (Task 4 helper) is invoked at the correct lifecycle point — inside the framework-managed `*grpc.Server` callback, not on a pre-built server.

The plan body explicitly cites the reference example (line 594): "consistent with the existing `plugin/external/sdk/serve.go:42-56` `servePlugin` flow that other plugin types use. We're not extracting from `sdk.Serve` (cycle 3 I-1's error in rev3); we're using the same hashicorp/go-plugin entrypoint with a different `Plugins` map entry for IaC." This explicitly acknowledges and corrects the cycle 3 mistake.

**PR 3 Task 9 (plan line 987):**

```diff
+    sdk.ServeIaCPlugin(iacServer, sdk.IaCServeOptions{
+        PluginInfo: &sdk.PluginInfo{HandshakeConfig: sharedHandshakeConfig},
+    })
```

Symbol name `sdk.IaCServeOptions` matches the type defined at Task 4-bis line 555. The cycle 3 mismatch (`sdk.ServeOptions` vs `sdk.IaCServeOptions`) is fixed. Plan line 996 explicitly notes: "Symbol name MUST be `IaCServeOptions` (not `ServeOptions`) — IaC-specific naming avoids collision with future generic helpers."

**Cycle 3 I-1 RESOLVED.** API shape is consistent with goplugin's actual interface. Symbol names match across PR 2 and PR 3.

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | NONE | Operator-captured fixture mechanism is independently verifiable (Spaces S3-compat endpoint, documented bucket); no assumption about wfctl flag surface |
| **Repo-precedent conflicts** | NONE | Task 4-bis pseudo-code matches existing `servePlugin` shape verbatim; reference cited inline |
| **YAGNI violations** | NONE | Two-variable model is conditional (cycle 2 I-3-NEW) on Task 20-bis result; not over-built |
| **Missing failure modes** | NONE | Task 20-bis Step "If FAIL" branch documented (file separate compat-shim PR; hold PRs 5-9 until shim lands) |
| **Security / privacy** | NONE | No new attack surface; operator-captured fixture handling notes the production-state origin |
| **Rollback story** | INTACT | Each runtime-affecting task (7, 9, 16, 18, 20, 21, 22) has explicit Rollback note |
| **Simpler alternative not considered** | NONE | Operator-captured fixture IS the simpler alternative cycle 3 C-1 escalated; adopted |
| **User-intent drift** | NONE | "No compat shim" intent honored; force-cutover semantics preserved |
| **Verification-class mismatch** | NONE | State-file-compat verification is now runnable (operator-captured fixture + Go decoder) |
| **Hidden serial dependencies** | NONE NEW | Operator's one-time fixture capture happens before PR 4 prep (documented at Task 20-bis intro) |
| **Missing rollback wiring** | NONE | All runtime-affecting tasks document rollback |
| **Over/under-decomposition** | NONE blocking | PR 4 size is intentional (atomic cutover per design rev5) |
| **Symbol-name consistency across tasks** | NONE | `sdk.IaCServeOptions` consistent between Task 4-bis (PR 2) and Task 9 (PR 3) |

---

## Plan-vs-design alignment

| Design claim | Plan implementation | Status |
|---|---|---|
| State-file format invariant (F-3) verified at PR 4 pre-flight | Task 20-bis uses operator-captured fixture + Go test via existing `LoadState()` | **HONORED** |
| `sdk.ServeIaCPlugin` is a single API call replacing sdk.Serve+manual-registration | API + struct defined; pseudo-code uses goplugin per-plugin callback per the actual API; symbol name consistent across PR 2 + PR 3 | **HONORED** |
| cross-plugin-build catches DO wire incompat at workflow-CI time | Task 6 step 717-725 specifies subprocess build + typed RPC test | **HONORED** |
| Adapter NOT a re-marshalling wrapper (ADR-0026) | Task 15-bis adapter wraps typed pb client + dispatches typed calls | **HONORED** |
| Engine consumers see no API change | Task 16 returns `interfaces.IaCProvider` (the adapter); engine consumers unchanged | **HONORED** |
| 2026-04-26 plan superseded for IaC scope | SUPERSEDED-NOTICE.md sibling file (PR 1 Task 1) | **HONORED** |
| Two-variable consumer model conditional on Task 20-bis | Decision tree at Task 21 intro (lines 1394-1401); selects branch from 20-bis result | **HONORED** |

---

## Verdict reasoning

Both cycle 3 findings are resolved against the actual artifacts:

- **C-1** — replaced with operator-captured fixture model in BOTH Task 20-bis and Task 22 (lockstep update per cycle 3 escalation #1). Task 20-bis uses an in-tree `cmd/wfctl/state_compat_test.go` that calls the existing `LoadState()` function (verified extant). Task 22 mirrors with a separate-binary CI gate; the import-path detail in the binary is hedged in plan prose. No nonexistent flags. No rejected docker invocations. Verification mechanism is runnable.

- **I-1** — Task 4-bis pseudo-code rewritten to use the per-plugin `GRPCServer(broker, s) error` callback inside `plugin.Serve(&plugin.ServeConfig{...})`, structurally consistent with the existing `servePlugin` shape at `serve.go:42-56` (plan explicitly cites the reference). PR 3 Task 9 updated to use `sdk.IaCServeOptions{...}` matching the Task 4-bis type definition. Symbol-name and API-shape consistency restored.

Cycle 1 I-4 (PR 5 cascade-block on state-file-compat) and cycle 2 I-3-NEW (Task 21 conditional) remain resolved from rev3.

**Per skill rules** (PASS only with ZERO Critical + every Important resolved/escalated): plan has 0 Critical and 0 Important. **PASS.** Plan is ready for executing-plans / scope-lock / build-phase.

Per "don't nitpick" filter — cosmetic items omitted from this report:
- Task 4-bis line 564 embeds `plugin.NetRPCUnsupportedPlugin` which doesn't exist in the GoCodeAlone/go-plugin v1.7.0 fork (the fork's `Plugin` interface has only 2 methods, no net-rpc surface). Implementer-side compile fix; the explicitly-cited reference example (`servePlugin` one paragraph below) shows the correct shape (no embed needed). Trivial mechanical resolution.
- Task 22 Step 2's Go program imports `github.com/GoCodeAlone/workflow/iac/state` (no such package; actual state struct lives in `cmd/wfctl/deploy_state.go:DeploymentState` or `interfaces/iac_state.go:ResourceState`). Plan line 1538 hedges explicitly. Implementer picks the closest extant decoder — Task 20-bis already shows the in-tree pattern.
- `sdk.PluginInfo` struct shape is referenced (line 558) but not defined inline. Plan introduces it as a new wrapper type; implementer can pick the single-field shape (`HandshakeConfig`). Internal consistency between PR 2 and PR 3 is preserved.

These are mechanical clean-up items the implementer resolves at compile time with the explicitly-cited reference example one paragraph away. They do not block plan correctness or invite parallel-implementer divergence (the original cycle 2 C-2-NEW concern), so they are out of scope per the user's explicit "don't nitpick" directive.

---

## Escalation summary

None. Plan is approved at the plan-phase adversarial-review gate. Recommend: proceed to scope-lock + executing-plans.
