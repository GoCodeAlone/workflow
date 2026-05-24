# `wfctl plugin verify-capabilities` Design

**Issue:** [workflow#765](https://github.com/GoCodeAlone/workflow/issues/765)
**Status:** Revised 2026-05-24 (cycle 3 — scope-down per cycle-2 review) — awaiting re-review
**Author:** Jon Langevin
**Parent contract:** workflow#762 (plugin-version contract); workflow#764 (Layer 3b 71-PR sweep)

## Revision history

- **Cycle 1**: initial design. FAILED — 3 Critical (diff fields not on wire; IaC bridge Unimplemented; handshake path wrong).
- **Cycle 2**: pivot to Manifest scalars + ContractRegistry. FAILED — 2 Critical (plugin.json has no iacResources key; BuildContractRegistry returns ALL services including infra-internal noise).
- **Cycle 3** (this version): scope-down per reviewer Option 2. **Drop contract-diff entirely**; defer to follow-up issue (#766 to be filed). Verify Manifest scalars + correct sentinel-pattern Version. Test fixtures specified per `plugin_validate_contract` precedent.

## Problem

`wfctl plugin validate-contract` is a SOURCE-tree static analysis pass. It verifies the source tree but cannot verify the BINARY actually surfaces what plugin.json declares at runtime. Workflow#764's Layer 3b sweep wired all 64 plugin repos with `sdk.WithBuildVersion + sdk.ResolveBuildVersion + ldflag`. But nothing yet verifies that the SHIPPED binary's runtime `Manifest.Version` matches the build tag.

The single load-bearing truth-loop the user named: **"binary's BuildVersion is populated and matches the release tag — proving ldflag fired during goreleaser build."** This is the bug class verify-capabilities exists to catch.

## Solution

New subcommand `wfctl plugin verify-capabilities` that spawns the plugin binary as a go-plugin subprocess, calls `PluginService.GetManifest`, and verifies the returned `Manifest.Version` is non-sentinel + matches plugin.json + git tag context. Scope strictly limited to fields the SDK reliably surfaces; broader contract-diff deferred to follow-up.

### Synopsis

```
wfctl plugin verify-capabilities --binary <path> <plugin-dir>
```

`--binary` REQUIRED (cycle-1 build-from-source dropped per reviewer Option 2; produced false-PASS in dev when ldflag paths varied per repo). Documented invocation:

```bash
# Local development:
go build -ldflags="-X github.com/GoCodeAlone/workflow-plugin-<name>/internal.Version=v1.2.3" \
  -o /tmp/p ./cmd/<name>
wfctl plugin verify-capabilities --binary /tmp/p .

# CI (post-goreleaser, in release.yml):
wfctl plugin verify-capabilities --binary "$(jq -r '.[0].path' dist/artifacts.json)" .
# (jq picks the right architecture's binary from goreleaser's artifacts.json
# manifest — avoids hard-coding goreleaser layout per F-NEW-6 cycle 2)
```

### Behavior

1. Load `<plugin-dir>/plugin.json`. Parse + run `PluginManifest.Validate()` (reuse from validate-contract).
2. Spawn `<binary>` via shared `spawnAndDial(ctx, binaryPath) (*external.PluginAdapter, func())` helper extracted from `cmd/wfctl/plugin_conformance.go:462-515`. Uses `external.Handshake` from `workflow/plugin/external/handshake.go:23`.
3. Call `PluginService.GetManifest(Empty) → pb.Manifest` (6 scalar fields per `plugin/external/proto/plugin.proto:96-104`).
4. Diff strict:

| Field | Source A (plugin.json) | Source B (binary `Manifest`) | Rule |
|---|---|---|---|
| `Name` | `name` | `Name` | exact string equal; FAIL on drift |
| `Version` | `version` | `Version` | matrix below |

**Version rule** (cycle-3, addresses F-NEW-3 with correct sentinel pattern):

The dev-sentinel set is `{"", "(devel)", "0.0.0"}` plus any string starting with `"(devel)"` (since `buildInfoVersion()` returns `"(devel) [@ <sha>[.dirty]]"`). Source: `/tmp/wfprobe/plugin/external/sdk/buildversion.go:36-42`.

```
isSentinel(v) := v == "" || v == "0.0.0" || strings.HasPrefix(v, "(devel)")
```

| plugin.json `version` | binary `Manifest.Version` | Outcome | Rationale |
|---|---|---|---|
| `"0.0.0"` (dev sentinel) | non-sentinel (e.g. `"v1.2.3"`) | **PASS** | binary built via ldflag-injecting CI; verify-capabilities running on a real artifact |
| `"0.0.0"` | sentinel (`""` / `"(devel)..."` / `"0.0.0"`) | **FAIL** | ldflag injection missing — the truth-loop bug verify-capabilities exists to catch |
| `"X.Y.Z"` (release) | `"vX.Y.Z"` or `"X.Y.Z"` | **PASS** | normalize: strip leading `v` from binary, then exact compare to plugin.json |
| `"X.Y.Z"` | sentinel | **FAIL** | plugin.json declares release tag but binary lacks ldflag injection |
| `"X.Y.Z"` | non-sentinel and not `X.Y.Z` | **FAIL** | drift between declared release version and shipped binary |

5. Exit 0 on clean. Exit 1 with report:
   ```
   FAIL  workflow-plugin-foo (plugin.json)
   error: 1 mismatch
     - version: plugin.json="1.2.3"; binary Manifest.Version="(devel) [@ a1b2c3d]"
       (sdk.ResolveBuildVersion returned the build-info fallback; ldflag injection missing)
   ```

### CI integration

Append to scaffold-template `release.yml` post-goreleaser, pre-publish:

```yaml
- name: Verify capabilities (post-build runtime check)
  run: |
    BIN=$(jq -r '[.[] | select(.type=="Binary") | .path] | .[0]' dist/artifacts.json)
    "${RUNNER_TEMP}/wfctl-bin/wfctl" plugin verify-capabilities --binary "$BIN" .
```

`dist/artifacts.json` is goreleaser's manifest of all built artifacts; jq picks the first binary (any arch — verify-capabilities only needs ONE binary to confirm ldflag fired). Avoids hard-coding goreleaser's directory layout per cycle-2 F-NEW-6.

Scaffold-side wiring is a follow-up commit on `scaffold-workflow-plugin` after this workflow PR lands — not part of this design's scope.

## Files (workflow repo)

- `cmd/wfctl/plugin_spawn.go` — NEW; extracts `spawnAndDial(ctx, binaryPath) (*external.PluginAdapter, func())` from `plugin_conformance.go:462-515`. Both `plugin conformance` AND new `plugin verify-capabilities` call it.
- `cmd/wfctl/plugin_conformance.go` — refactored to call new shared helper; behavior unchanged.
- `cmd/wfctl/plugin_verify_capabilities.go` — NEW; subcommand entry + diff impl.
- `cmd/wfctl/plugin_verify_capabilities_test.go` — table-driven tests against `testdata/verify_capabilities/<scenario>/`.
- `cmd/wfctl/testdata/verify_capabilities/` — NEW fixture tree, mirrors `cmd/wfctl/testdata/plugin_validate_contract/` precedent:
  - `good/` — plugin.json `version="0.0.0"`, ldflag-injected binary tag `v0.1.0`. Expect PASS.
  - `release-good/` — plugin.json `version="1.2.3"`, ldflag tag `v1.2.3`. Expect PASS.
  - `missing-ldflag/` — plugin.json `version="0.0.0"`, no ldflag (binary surfaces sentinel `(devel)`). Expect FAIL.
  - `version-drift/` — plugin.json `version="1.2.3"`, ldflag tag `v0.9.0`. Expect FAIL.
  - `name-drift/` — plugin.json `name="foo"`, binary advertises `Name="bar"`. Expect FAIL.

  Each scenario contains `plugin.json` + `cmd/plugin/main.go` (minimal `sdk.Serve` stub). Tests compile the fixture via `go build` invocation at test-time (one fixture per scenario), then run verify-capabilities against the compiled binary in `t.TempDir()`. Pattern mirrors existing `validate-contract` test approach where source fixtures + plugin.json live in testdata.
- `cmd/wfctl/plugin.go` — register `case "verify-capabilities"`.
- `docs/PLUGIN_RELEASE_GATES.md` — append `Verify-Capabilities` section.

## Architecture choices (cycle-3)

| Choice | Picked | Rejected (reason) |
|---|---|---|
| Surface | new subcommand | flag on validate-contract (cycle 2 considered + rejected: mixes static + runtime); flag on `plugin conformance` (conformance IaC-typed-only today) |
| Binary source | REQUIRE `--binary` | build-from-source default — rejected cycle 2: false-PASS in dev with per-repo ldflag-path variance |
| Diff scope | Manifest.Name + Manifest.Version ONLY | + per-type RPCs (rejected cycle 2: Unimplemented in IaC bridge); + ContractRegistry (rejected cycle 3: plugin.json has no iacResources LHS + BuildContractRegistry returns infra-internal noise; defer to follow-up #766) |
| Version diff rule | sentinel-pattern matrix (`{"", "(devel)...", "0.0.0"}`) | cycle-1 "non-empty" (broke truth-loop); cycle-2 literal "0.0.0" (didn't match SDK's `(devel)` output) |
| Spawn-and-dial | extract shared helper, refactor conformance | re-implement from scratch (cycle 1 F3); leave conformance unchanged (duplicates ~200 LOC) |
| CI binary path | `jq -r '...' dist/artifacts.json` lookup | hard-coded `dist/<name>_linux_amd64/<name>` (cycle 2 F-NEW-6: goreleaser layout varies by arch + goamd64 level) |

## Assumptions

1. **`PluginService.GetManifest` exists + uniformly returns 6 scalars across all plugin types.** Verified: `/tmp/wfprobe/plugin/external/proto/plugin.proto:96-104` defines `Manifest{name, version, author, description, config_mutable, sample_category}`. Non-IaC impl at `plugin/external/sdk/grpc_server.go:148-174`. IaC bridge impl at `plugin/external/sdk/iacserver.go:301`. All plugins built with workflow v0.62.0+ serve this RPC. (Pre-v0.20 plugins predate the RPC; not in our 64-repo target set per #764 audit — all pinned to v0.62.0.)

2. **`external.Handshake` is exported at `workflow/plugin/external/handshake.go:23`.** Verified. wfctl imports it in `plugin_conformance.go`.

3. **`sdk.ResolveBuildVersion` sentinel set is `{"", "dev", "(devel)"}` plus the function returns `"(devel) [@ sha[.dirty]]"` from build-info fallback.** Verified at `/tmp/wfprobe/plugin/external/sdk/buildversion.go:36-42`. Diff matrix's `isSentinel()` predicate covers all SDK-emitted sentinel forms.

4. **plugin.json `version` field is canonical authority for declared version.** Verified at `plugin/manifest.go`. Set by goreleaser before-hook at release time per workflow#762 contract.

5. **CI runner has `jq` available.** `jq` is preinstalled on `ubuntu-latest` GitHub runners (verified standard image). Custom runners must install it.

6. **`--binary` path points to the exact post-goreleaser binary that will publish.** Operator responsibility. Documented in §Synopsis with `jq dist/artifacts.json` pattern for CI.

## Failure modes addressed

- **Spawn fails**: hard exit 1 with goplugin error (handled by shared spawnAndDial).
- **gRPC-dial fails**: hard exit 1.
- **GetManifest returns Unimplemented**: hard exit 1 with "plugin SDK appears stale; expected GetManifest available since workflow v0.20".
- **Plugin process leaks**: explicit `client.Kill()` in defer + cleanup via spawnAndDial helper.
- **Malformed plugin.json**: reuse `PluginManifest.Validate()`.
- **Mid-RPC plugin crash**: gRPC error surfaces; exit 1 with the error message.
- **plugin.json declares version "1.2.3" but binary ldflag never fired**: matrix row "X.Y.Z + sentinel → FAIL" catches this — the primary truth-loop bug class.
- **plugin.json declares "0.0.0" sentinel but binary somehow has non-sentinel Version**: matrix row "0.0.0 + non-sentinel → PASS" — acceptable, indicates CI artifact under verification.

## Rollback

Runtime-affecting change class (CLI subcommand + CI step). Rollback path:

- **Subcommand**: revert workflow PR; subcommand stops being registered. Existing pipelines unaffected — nothing depends on it yet.
- **Shared spawnAndDial helper refactor**: revert is part of same PR; conformance returns to inline pattern; no behavior change in conformance.
- **CI step** (scaffold follow-up): revert scaffold-template PR; release.yml stops invoking the subcommand; existing release pipelines still pass.

Backwards-compat: subcommand is purely additive. Older wfctl callers continue to work.

## Decisions to record (ADRs)

1. **--binary required (no build-from-source)** — chose explicit-binary requirement over dev-mode convenience to avoid per-repo ldflag-path divergence. ADR target: `decisions/NNNN-verify-capabilities-binary-required.md`.

2. **Scope limited to Name + Version** — chose NOT to verify ContractRegistry (no plugin.json LHS exists today + BuildContractRegistry returns infra-internal services). Follow-up issue (to be filed: workflow#766) introduces `capabilities.iacServices` schema on PluginManifest + a server-side `BuildContractRegistryForPlugin()` filter; cycle-4 of verify-capabilities can then add the contract-diff against a clean wire surface. ADR target: `decisions/NNNN-verify-capabilities-scope-name-version-only.md`.

## What this design does NOT do (explicit non-goals)

- **Does NOT verify ModuleTypes/StepTypes/TriggerTypes** at runtime (per-type RPCs Unimplemented in IaC bridge; per-cycle-1 F2). Static check via `validate-contract` is authoritative.
- **Does NOT verify typed-contract surface** via `GetContractRegistry` (no plugin.json LHS + binary side emits infra-internal services as noise; cycle-2 F-NEW-1 + F-NEW-2). Deferred to follow-up issue.
- **Does NOT build the binary** — operator must produce one (local: `go build` with explicit ldflag; CI: goreleaser).
- **Does NOT verify `minEngineVersion`** at runtime (not on `pb.Manifest`). Static-check responsibility.
- **Does NOT run inside `plugin conformance`** (separate subcommand; shared helper is the only overlap).
- **Does NOT support `--json` output mode** (defer YAGNI; follow-up).
- **Does NOT support multi-binary repos** (runs against the binary passed via `--binary`; multi-binary repos invoke multiple times).
- **Does NOT verify Author/Description/ConfigMutable/SampleCategory** Manifest fields (Author/Description are display strings, drift not load-bearing; ConfigMutable/SampleCategory are runtime configuration not contract surface). Scope limited to fields that catch real bugs.

## Open questions

None blocking. Implementation can proceed.
