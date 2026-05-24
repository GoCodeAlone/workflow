# `wfctl plugin verify-capabilities` Design

**Issue:** [workflow#765](https://github.com/GoCodeAlone/workflow/issues/765)
**Status:** Revised 2026-05-24 (cycle 2 — adversarial review found 3 Critical) — awaiting re-review
**Author:** Jon Langevin
**Parent contract:** workflow#762 (plugin-version contract); workflow#764 (Layer 3b 71-PR sweep that wired all 64 plugin repos)

## Revision history

- **Cycle 1** (2026-05-24): initial design. FAILED adversarial review (3 Critical).
- **Cycle 2** (2026-05-24, this version): pivoted to Manifest-scalars + ContractRegistry-only diff (reviewer Option 3); dropped build-from-source (reviewer Option 2); reuses spawn-and-dial pattern from `plugin_conformance.go:462-515` (reviewer Option 1 partial); fixed handshake import; fixed version-diff rule; added contract-diff for user-intent's "+ declared contract" half.

## Problem

`wfctl plugin validate-contract` is a SOURCE-tree static analysis pass: it verifies plugin.json parses, capabilities are populated, main.go calls `sdk.ResolveBuildVersion`, goreleaser carries `-X *.Version=` ldflag. It does NOT verify that the resulting binary, when spawned, actually advertises those capabilities at runtime.

A plugin can pass `validate-contract` and still ship a binary whose `PluginService.GetManifest` returns an empty BuildVersion, or whose typed-IaC service registration drifts from `plugin.json` declarations. Workflow#764's audit identified this gap: 4 of 66 repos READY by static check, but no test confirms runtime equivalence.

Now that all 64 plugin repos are wired with `sdk.WithBuildVersion` and `sdk.ResolveBuildVersion`, the runtime manifest carries the build-time tag and the SDK exposes contract discovery. The truth-check is finally implementable.

## Solution

New subcommand `wfctl plugin verify-capabilities` that spawns the plugin binary as a go-plugin subprocess, gRPC-dials, calls `PluginService.GetManifest` AND `PluginService.GetContractRegistry`, and diffs results against the source-tree `plugin.json`.

### Synopsis

```
wfctl plugin verify-capabilities --binary <path> <plugin-dir>
```

`--binary` is REQUIRED. (Cycle-1 build-from-source convenience dropped; see reviewer Option 2 — caused false-PASS in dev when ldflag paths varied per repo. Documented invocation pattern:

```bash
# Local development:
go build -o /tmp/p ./cmd/<name>
wfctl plugin verify-capabilities --binary /tmp/p .

# CI (post-goreleaser):
wfctl plugin verify-capabilities --binary dist/<name>_linux_amd64/<name> .
```

### Behavior

1. Read `<plugin-dir>/plugin.json` → declared `PluginManifest` (parse via `workflow/plugin.PluginManifest`).
2. Spawn `<binary>` via shared `spawnAndDial(ctx, binaryPath) (*external.PluginAdapter, func() cleanup)` helper extracted from `cmd/wfctl/plugin_conformance.go:462-515`. Uses `external.Handshake` from `workflow/plugin/external/handshake.go:23`. Returns the typed adapter + a deferred cleanup that kills the process.
3. Two RPC calls, in order:
   - **`PluginService.GetManifest(Empty) → pb.Manifest`** — covers Name + Version + Author + Description + ConfigMutable + SampleCategory (the only 6 scalar fields on `pb.Manifest`).
   - **`PluginService.GetContractRegistry(Empty) → pb.ContractRegistry`** — enumerates the typed gRPC services the plugin registered (e.g. `pb.IaCProviderRequiredServer`, `pb.IaCProviderFinalizerServer`).
4. Diff strict:

| Field | Source A | Source B | Rule |
|---|---|---|---|
| `Name` | binary `Manifest.Name` | plugin.json `name` | exact string equal |
| `Version` | binary `Manifest.Version` | plugin.json `version` + git tag context | rule below |
| `MinEngineVersion` | n/a (not on wire) | plugin.json `minEngineVersion` | static check only — not verified at runtime; flagged in design notes per F1 |
| `ModuleTypes`/`StepTypes`/`TriggerTypes` lists | n/a (not on Manifest; per-type RPCs Unimplemented in IaC bridge per F2) | plugin.json `capabilities.*` | static-check via `validate-contract` only; NOT in scope here |
| Contract services | binary `ContractRegistry.contracts[*].service_name` | plugin.json `capabilities.iacResources` etc. (if declared) | set-equal (sort then compare) — only if plugin.json declares iac capabilities |

**Version rule** (resolves cycle-1 inconsistency F5):

| plugin.json.version | binary Manifest.Version | Rule |
|---|---|---|
| `"0.0.0"` (dev sentinel) | non-empty AND not `"0.0.0"` | PASS (binary was ldflag-built; verify-capabilities running on a CI artifact) |
| `"0.0.0"` (dev sentinel) | `"0.0.0"` (sentinel; ldflag never fired) | FAIL — ldflag injection missing; the truth-loop bug verify-capabilities exists to catch |
| `"X.Y.Z"` (release manifest) | `"vX.Y.Z"` or `"X.Y.Z"` | PASS — normalized comparison; strip leading `v` from binary value before compare |
| `"X.Y.Z"` (release manifest) | empty / `"0.0.0"` / anything else | FAIL — drift between declared release version and shipped binary |

5. Exit 0 on clean. Exit 1 with field-by-field report:
   ```
   FAIL  workflow-plugin-foo (plugin.json)
   error: 2 mismatch(es)
     - version: plugin.json="1.2.3"; binary Manifest.Version="" (sdk.WithBuildVersion not wired or ldflag missing)
     - contracts: plugin.json declares [iac.foo]; binary advertises [iac.foo, iac.bar]
       extra-in-binary: [iac.bar]
   ```

### CI integration

Append step to scaffold-template `release.yml` post-goreleaser, pre-publish:

```yaml
- name: Verify capabilities (post-build runtime check)
  run: wfctl plugin verify-capabilities --binary dist/<name>_linux_amd64/<name>/<name> .
```

The scaffold-side wiring is a follow-up commit on `scaffold-workflow-plugin` after this workflow PR lands, not part of this design's scope.

## Files (workflow repo)

- `cmd/wfctl/plugin_spawn.go` — NEW; extracts `spawnAndDial(ctx, binaryPath) (*external.PluginAdapter, func())` from `plugin_conformance.go:462-515`. Both `plugin conformance` AND new `plugin verify-capabilities` call it. Refactor cleanup belongs in conformance file.
- `cmd/wfctl/plugin_conformance.go` — refactored to call new shared helper; behavior unchanged.
- `cmd/wfctl/plugin_verify_capabilities.go` — NEW; subcommand entry + diff impl.
- `cmd/wfctl/plugin_verify_capabilities_test.go` — table-driven tests using a stub plugin binary built at test time via `go build` invocation within the test (no external fixtures).
- `cmd/wfctl/plugin.go` — register `case "verify-capabilities"`.
- `docs/PLUGIN_RELEASE_GATES.md` — append `Verify-Capabilities` section.

## Architecture choices (cycle-2 revised)

| Choice | Picked | Rejected (reason) |
|---|---|---|
| Surface | new subcommand | flag on validate-contract (mixes static + runtime; harder to skip in CI when binary unavailable); flag on `plugin conformance` (conformance is IaC-typed-only today; verify targets all 64 repos) |
| Binary source | REQUIRE `--binary` | build-from-source default — REJECTED cycle 2 per reviewer Option 2; admitted false-PASS in dev when ldflag paths vary (gcp `provider/`, security root, edge-compute `internal/plugin/`); recommended invocation is documented in §Synopsis |
| Diff scope | Manifest scalars + ContractRegistry | per-type RPCs (GetModuleTypes/GetStepTypes/GetTriggerTypes) — REJECTED cycle 2 per reviewer Option 3 + F2; IaC bridge returns Unimplemented for these; verify-capabilities collapses to Manifest + ContractRegistry (the only runtime surface that works uniformly) |
| Version diff rule | matrix in §Behavior step 4 (dev-sentinel-vs-real, normalized v-prefix) | cycle-1 "non-empty" — REJECTED cycle 2 per F5; broke truth-loop premise (the field user said was load-bearing) |
| Spawn-and-dial | extract shared helper, refactor conformance | re-implement from scratch (REJECTED per F3); add unconditional verify to conformance (REJECTED — different scope, see Surface row) |
| Capability list semantics | set (sort then compare) | sequence (brittle on canonical order) |

## Assumptions

1. **`PluginService.GetManifest` exists and returns 6 scalars uniformly across all plugin types.** Verified per `/tmp/wfprobe/plugin/external/proto/plugin.proto:96-104` (Manifest message def) + `/tmp/wfprobe/plugin/external/sdk/grpc_server.go:148-174` (non-IaC impl) + `/tmp/wfprobe/plugin/external/sdk/iacserver.go:301` (iacPluginServiceBridge.GetManifest). Cycle 1 assumption #1 was too broad ("manifest carries the type lists") — corrected to the actual wire-shape this cycle.

2. **`external.Handshake` is exported at `workflow/plugin/external/handshake.go:23`.** Verified. wfctl already imports this package in `plugin_conformance.go`. (Cycle-1 assumption #2 had wrong import path; corrected this cycle per F4.)

3. **`PluginService.GetContractRegistry` returns the registered typed gRPC services with `service_name`, `kind`, `mode` per `pb.ContractDescriptor`.** Verified per `/tmp/wfprobe/plugin/external/sdk/iacserver.go:297-301` + `/tmp/wfprobe/plugin/external/sdk/grpc_server.go` (non-IaC bridge). All plugins built with workflow v0.62.0+ register at least the PluginService bridge → ContractRegistry call succeeds.

4. **`plugin.json.capabilities` field shape is the authoritative on-disk schema for the diff.** Verified per `/tmp/wfprobe/plugin/manifest.go:43-47` — fields are `ModuleTypes`, `StepTypes`, `TriggerTypes`, `WorkflowTypes`, `WiringHooks`. For IaC, the relevant declarations live under different keys (TBD during implementation — read `PluginManifest` struct precisely; per F1, cycle-1 assumed wrong field names).

5. **CI runner already has wfctl pinned to the release containing this PR.** Already handled by `GoCodeAlone/setup-wfctl@v1` step in scaffold release.yml; CI step is additive.

6. **--binary path is the exact post-goreleaser binary that will publish.** Documented in §Synopsis CI invocation. If operator passes a stale binary, verify-capabilities runs against stale binary — that's the operator's bug, not the design's.

## Failure modes addressed

- **Spawn fails (binary doesn't run)**: hard exit 1 with goplugin's error (handled by shared spawnAndDial helper).
- **gRPC-dial fails (handshake mismatch / process exits)**: hard exit 1 (same path).
- **GetManifest returns `Unimplemented`**: hard exit 1 with "plugin SDK appears stale; expected GetManifest available since workflow v0.20" (cite when SDK started serving it).
- **GetContractRegistry returns `Unimplemented`**: WARN-only — older SDK versions; skip the contract-diff and report partial verification.
- **Plugin process leaks (verify exits between spawn + RPC)**: explicit `client.Kill()` in defer + tmpfile cleanup in spawnAndDial helper.
- **Malformed plugin.json**: existing `PluginManifest.Validate()` before spawning; reuse from validate-contract path.
- **Mid-RPC plugin crash**: gRPC error surfaces as failed call; exit 1 with the RPC error message.

## Rollback

Runtime-affecting change class: this PR adds a CLI subcommand + (follow-up) a CI step. Rollback path:

- **Subcommand**: revert workflow PR; subcommand stops being registered; `wfctl plugin verify-capabilities` → "unknown subcommand" error. Existing pipelines unaffected — nothing depends on it yet.
- **Shared spawnAndDial helper refactor**: revert is part of the same PR; conformance returns to inline pattern; no behavior change in conformance.
- **CI step** (scaffold follow-up): revert the scaffold-template PR; release.yml stops invoking the subcommand; existing release pipelines still pass.

Backwards-compat: subcommand is purely additive. wfctl callers without verify-capabilities continue to work; downstream consumers only see the new subcommand when they upgrade their CI's wfctl pin.

## Decisions to record

Two non-trivial trade-offs that warrant ADRs per `recording-decisions`:

1. **--binary required (no build-from-source)** — chose explicit-binary requirement over dev-mode convenience to avoid the per-repo ldflag-path divergence that produces false-PASS results. ADR target: `decisions/NNNN-verify-capabilities-binary-required.md`.

2. **Scope limited to Manifest + ContractRegistry** — chose NOT to walk per-type RPCs (GetModuleTypes/GetStepTypes/GetTriggerTypes) because the IaC SDK bridge returns `Unimplemented` for them; full per-type verification stays in `validate-contract` (static, source-of-truth from plugin.json). ADR target: `decisions/NNNN-verify-capabilities-scope.md`.

## What this design does NOT do (explicit non-goals)

- **Does NOT verify ModuleTypes/StepTypes/TriggerTypes** at runtime. Those lists are declared in plugin.json and statically lintable; per F1 they are not on the wire and per F2 the per-type RPCs are Unimplemented for IaC plugins. Static check (`validate-contract`) remains the truth source for those.
- **Does NOT build the binary**. Operator must produce one (local: `go build`; CI: goreleaser). Documented in §Synopsis.
- **Does NOT verify `minEngineVersion`** at runtime — not on `pb.Manifest`. Static-check responsibility (already in `validate-contract`).
- **Does NOT run inside `plugin conformance`** (subcommand stays separate per architecture choice). Shared spawn-and-dial helper is the only refactor.
- **Does NOT use `--json` output mode** (defer per YAGNI; follow-up if needed).
- **Does NOT support multi-binary repos** (verify-capabilities runs against the binary passed via `--binary`; multi-binary needs multiple invocations).
