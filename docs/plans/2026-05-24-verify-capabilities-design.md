# `wfctl plugin verify-capabilities` Design

**Issue:** [workflow#765](https://github.com/GoCodeAlone/workflow/issues/765)
**Status:** Approved 2026-05-24 — awaiting adversarial design review
**Author:** Jon Langevin
**Parent contract:** workflow#762 (plugin-version contract); workflow#764 (Layer 3b 71-PR sweep that wired all 64 plugin repos)

## Problem

`wfctl plugin validate-contract` is a SOURCE-tree static analysis pass: it verifies plugin.json parses, capabilities are populated, main.go calls `sdk.ResolveBuildVersion`, goreleaser carries `-X *.Version=` ldflag. It does NOT verify that the resulting binary, when spawned, actually advertises those capabilities at runtime.

A plugin can pass `validate-contract` and still ship a binary whose `PluginService.GetManifest` reports different capabilities (or an empty BuildVersion) than declared. This is the gap workflow#764's audit identified: 4 of 66 repos READY by static check, but no test confirms runtime equivalence.

Now that all 64 plugin repos are wired with `sdk.WithBuildVersion` and `sdk.ResolveBuildVersion`, the runtime manifest carries the build-time tag and the declared capabilities. The truth-check is finally implementable.

## Solution

New subcommand `wfctl plugin verify-capabilities` that spawns the plugin binary as a go-plugin subprocess, gRPC-dials, calls `PluginService.GetManifest`, and diffs the returned `Manifest` against the source-tree `plugin.json`.

### Synopsis

```
wfctl plugin verify-capabilities [--binary <path>] [--build-tag <tag>] <plugin-dir>
```

### Behavior

1. Read `<plugin-dir>/plugin.json` → declared `PluginManifest` (parse via `workflow/plugin.PluginManifest`).
2. Resolve binary:
   - `--binary <path>` supplied: use as-is. Skip build.
   - Else: discover plugin's main package by convention (`<plugin-dir>/cmd/<name>/main.go` where `<name>` matches `plugin.json.name` OR sole `cmd/*/main.go` if only one). Build with:
     ```
     go build -ldflags="-X <module>/internal.Version=<tag>" -o <tmpfile> ./cmd/<name>
     ```
     `<tag>` = `--build-tag` if set, else `v0.0.0-verify`.
3. Spawn binary via `goplugin.NewClient` with workflow's canonical ext handshake.
4. gRPC-dial; call `pb.PluginServiceClient.GetManifest(Empty)`.
5. Diff strict:

| Field | Comparison | Failure |
|---|---|---|
| `name` | exact string equal | plugin.json.name ≠ manifest.name |
| `version` | non-empty | manifest.version is "" (BuildVersion never wired) |
| `minEngineVersion` | exact string equal | drift |
| `moduleTypes` | set-equal (sort then compare) | missing/extra entry |
| `stepTypes` | set-equal | missing/extra entry |
| `triggerTypes` | set-equal | missing/extra entry |
| `resourceTypes` | set-equal | missing/extra entry |
| `iacTypes` | set-equal | missing/extra entry |

6. Exit 0 on clean. Exit 1 with field-by-field report:
   ```
   FAIL  workflow-plugin-foo v0.0.0-verify (plugin.json)
   error: 2 mismatch(es)
     - moduleTypes: declared [a, b]; binary advertises [a, c]
       missing-from-binary: [b]; extra-in-binary: [c]
     - version: binary advertises "" (sdk.WithBuildVersion not wired)
   ```
7. Cleanup: kill spawned process; rm tmpfile if built.

### CI integration

Append step to scaffold-template `release.yml` post-goreleaser, pre-publish:

```yaml
- name: Verify capabilities (post-build runtime check)
  run: wfctl plugin verify-capabilities --binary dist/<name>_linux_amd64/<name> .
```

This closes the truth loop: the binary that ships IS the one whose manifest gets verified. Drift between declared and runtime is caught BEFORE the release publishes.

The scaffold-side wiring is a follow-up commit on `scaffold-workflow-plugin` after this workflow PR lands, not part of this design's scope.

## Files (workflow repo)

- `cmd/wfctl/plugin_verify_capabilities.go` — subcommand entry + impl
- `cmd/wfctl/plugin_verify_capabilities_test.go` — table-driven tests (sample plugin compiled at test time)
- `cmd/wfctl/plugin.go` — register `case "verify-capabilities"` in dispatcher
- `docs/PLUGIN_RELEASE_GATES.md` — append `Verify-capabilities` section

## Architecture choices

| Choice | Picked | Rejected (reason) |
|---|---|---|
| Surface | new subcommand | flag on validate-contract (mixes static + runtime; harder to skip in CI when binary unavailable) |
| Binary source | build by default + `--binary` override | require `--binary` always (more friction for dev); `go run` (debug.ReadBuildInfo can't surface ldflag tag) |
| Diff strictness | strict (exact / set-equal) | permissive warn-only (weak CI gate); --strict/--permissive flag (more surface; defer YAGNI) |
| CI integration | release.yml post-build | ci.yml on PR (binary not built yet); both (two integration points) |
| Capability list semantics | set (sort both before compare) | sequence (brittle on canonical order) |

## Assumptions

1. `sdk.ServePluginFull` / `sdk.Serve` / `sdk.ServeIaCPlugin` all wire `PluginService.GetManifest` via the SDK-internal `grpcServer.GetManifest` impl. Verified: workflow v0.62.0 SDK source (`plugin/external/sdk/grpc_server.go:148` and `plugin/external/sdk/iacserver.go:301` for the iacPluginServiceBridge). No plugin type opts out.
2. The go-plugin handshake config used by SDK is importable from `workflow/plugin/external/sdk` (or exposed via a public helper in `workflow/plugin/external`). If only internal: wfctl already imports the SDK; same import path works.
3. Plugin's main-package directory is discoverable from `<plugin-dir>` by convention:
   - Single `cmd/*/main.go` → use it.
   - Multiple → require `cmd/<plugin-name>/main.go` to exist where `<plugin-name>` = plugin.json's `name`.
   - Neither matches → exit 1 with "unable to locate main package; pass --binary explicitly".
4. `--build-tag` ldflag path matches the canonical `-X <module>/internal.Version=<tag>` pattern (workflow#762 contract). For non-internal Version locations (gcp: `provider/...`; security: root pkg; edge-compute: `internal/plugin/...`), the design must derive the path from plugin.json's main module + `internal.Version` convention. **Mitigation**: if --binary is supplied, no build → no ldflag path issue. Build-from-source mode is best-effort dev convenience; production verification uses --binary.
5. CI runner has `go` available when build-from-source path is used. Release pipelines that use `--binary dist/...` skip this.
6. Plugins that don't satisfy IaCProviderRequiredServer / ModuleProvider interfaces are still spawnable; SDK handles "no provider" gracefully via the bridge. Verified per SDK source.

## Failure modes addressed

- **Spawn fails (binary doesn't run)**: hard exit 1 with goplugin's error message.
- **gRPC-dial fails (handshake mismatch / process exits)**: hard exit 1.
- **GetManifest RPC returns Unimplemented**: hard exit 1 with "plugin SDK appears stale; expected GetManifest available since workflow v0.40".
- **Plugin process leaks (verify exits between spawn + RPC)**: explicit `client.Kill()` in defer + tmpfile cleanup via `t.Cleanup` in tests / `defer os.Remove` in CLI.
- **Malformed plugin.json**: existing validate-contract logic; reuse PluginManifest.Validate() before spawning.

## Rollback

Runtime-affecting change class: this PR adds a CLI subcommand + (follow-up) a CI step. Rollback path:

- **Subcommand**: revert workflow PR; subcommand stops being registered; `wfctl plugin verify-capabilities` → "unknown subcommand" error. Existing pipelines unaffected — nothing depends on it yet.
- **CI step** (scaffold follow-up): revert the scaffold-template PR; release.yml stops invoking the subcommand; existing release pipelines still pass.

Backwards-compat: subcommand is purely additive. wfctl v0.62.0 callers without verify-capabilities continue to work; downstream consumers only see the new subcommand when they upgrade their CI's wfctl pin to ≥ the release containing this PR.

## Decisions to record

This design makes one non-trivial trade-off that warrants an ADR per `recording-decisions`:

- **Build-from-source default vs --binary-only**: chose build-from-source for dev convenience despite goreleaser-logic duplication risk (mitigated by recommending --binary in CI). This is divergent from `validate-contract` which is pure-static. ADR target: `decisions/NNNN-verify-capabilities-build-strategy.md`.

## Open questions for adversarial review

- Should verify-capabilities ALSO be invoked from `ci.yml` (PR gate) on every push, building from source? Today's design defers (release.yml only). Trade-off: PR-time verification catches drift earlier but doubles build cost per PR.
- Should the diff report machine-readable JSON via `--json` flag for downstream tooling? Defer per YAGNI; workflow#762 follow-up if needed.
- For multi-binary repos (migrations: 2 binaries), should verify-capabilities run against EACH? Today's design verifies the plugin matching plugin.json.name only. Multi-binary follow-up if needed.
