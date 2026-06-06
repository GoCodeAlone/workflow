# Plugin Release Gates (workflow#758)

Plugin authors must satisfy a small contract so that:

- Plugin binaries surface their build-injected version through the gRPC `GetManifest` RPC (operator + engine observability).
- The committed `plugin.json.version` field stops drifting between releases (no more `sync-plugin-version.yml` PR pileup).
- Non-semver tags cannot enter the public registry.

This page documents the contract, the migration steps, and the `wfctl plugin validate-contract` gate that enforces it at release time.

## Contract

Every plugin's source repository MUST:

1. **`plugin.json` carries a sentinel version**: committed `.version` field is `"0.0.0"`. The release tag is the truth; the committed file is a structural placeholder. `PluginManifest.Validate()` accepts `0.0.0` (parses through `ParseSemver`).
2. **`capabilities` and `minEngineVersion` populated**: these are read by `workflow-registry/scripts/sync-versions.sh` at tag time (`fetch_plugin_json` path). Stale capabilities cause registry to publish wrong type info; freshness is the maintainer's responsibility pre-tag.
3. **Goreleaser injects the tag via ldflag**: `.goreleaser.yaml` (or `.goreleaser.yml`) carries an `ldflags` line matching the regex `-X .*\.Version=`. Standard pattern:

   ```yaml
   builds:
     - id: workflow-plugin-foo
       ldflags:
         - -s -w -X github.com/GoCodeAlone/workflow-plugin-foo/internal.Version={{.Version}}
   ```

   The injected package-level Go var's name is flexible — DO plugin uses `internal.Version`, AWS uses `provider.ProviderVersion`. wfctl validates the ldflag's PRESENCE, not the symbol path.
4. **`cmd/**/main.go` (or any `.go` at repo root) wires `sdk.ResolveBuildVersion`**: the plugin's serve binary calls `sdk.ResolveBuildVersion(<that Version var>)` and passes the result to either `IaCServeOptions.BuildVersion` (for IaC plugins via `sdk.ServeIaCPlugin`) or `sdk.WithBuildVersion(...)` (for non-IaC plugins via `sdk.Serve`).

   Example (IaC):

   ```go
   import (
       "github.com/GoCodeAlone/workflow-plugin-foo/internal"
       sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
   )
   func main() {
       sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
           BuildVersion: sdk.ResolveBuildVersion(internal.Version),
       })
   }
   ```

   Example (non-IaC):

   ```go
   sdk.Serve(internal.NewFooPlugin(),
       sdk.WithManifestProvider(manifest),
       sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
   )
   ```
5. **Release tag is publish-grade semver**: `^v[0-9]+\.[0-9]+\.[0-9]+$`. Pre-release strings (`-rc1`, `-alpha.1`, `-feat-foo`, `+meta`) are rejected by both `wfctl plugin validate-contract --for-publish` and `workflow-registry`. Pre-release publishing is deferred to a separate design that updates `ParseSemver` end-to-end.

## release.yml two-step gate

Each plugin's `.github/workflows/release.yml` runs the gate twice — once before the build (static contract + tag format) and once after the build (tarball-version-equals-tag):

```yaml
on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with: { go-version-file: go.mod }
      - uses: GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1
        with: { version: v0.61.0 }

      # 1. Pre-build gate: static contract + tag format
      - name: Validate plugin contract for publish (pre-build)
        run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .

      # 2. Build (goreleaser mutates plugin.json or writes .release/plugin.json)
      - uses: goreleaser/goreleaser-action@5daf1e915a5f0af01ddbcd89a43b8061ff4f1a89 # v7
        with:
          distribution: goreleaser
          version: '~> v2'
          args: release --clean --release-notes=/dev/null

      # 3. Post-build gate: tarball carries the tag (run BEFORE Publish-release)
      - name: Verify shipped plugin.json carries tag (post-build)
        run: |
          if [ -f .release/plugin.json ]; then
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir .release .
          else
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir . .
          fi

      # 4. Promote the GitHub Release out of draft
      - name: Publish release (was draft during asset upload)
        env: { GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }} }
        run: gh release edit ${{ github.ref_name }} --draft=false --repo ${{ github.repository }}
```

Malformed tag → step 1 fails. Stale capabilities or missing ldflag → step 1 fails. Goreleaser mis-writing the tarball plugin.json → step 3 fails. None of these promote the release to public.

## What got deleted

- `.github/workflows/sync-plugin-version.yml`: gone. The committed `plugin.json.version` no longer syncs with the tag. Goreleaser's `before:` hook continues to write the tag into the shipped tarball.
- `chore: sync plugin.json version to vX.Y.Z` bot PRs: no longer fire.
- The MISMATCH warning in `workflow-registry/scripts/sync-versions.sh` is unaffected — it compares registry's local manifest copy against the upstream tag, not against the plugin repo's committed plugin.json.

## Operator-visible build version

For release builds, plugins surface their tag via `GetManifest`:

```
$ wfctl plugin info workflow-plugin-foo
Name:        workflow-plugin-foo
Version:     v1.2.3    # from binary's runtime, not disk plugin.json
...
```

For local `go build` / dev installs (no ldflag injection), the binary reports `(devel) [@ abc1234.dirty]` so operators see the test-build nature in the version string. `wfctl plugin install --local <dir>` reads the committed `plugin.json.version` (the sentinel `0.0.0`) but the binary's runtime GetManifest is authoritative.

## Migration checklist (per plugin repo)

1. `git rm .github/workflows/sync-plugin-version.yml`
2. Edit `cmd/**/main.go` to call `sdk.ResolveBuildVersion(<existing Version var>)` and wire via `IaCServeOptions.BuildVersion` or `sdk.WithBuildVersion`. If no Version var exists in the package the goreleaser ldflag targets, add `var Version = "dev"`.
3. Set `plugin.json.version` to `"0.0.0"`.
4. Verify `.goreleaser.{yaml,yml}` has `-X .*\.Version=` ldflag.
5. Edit `.github/workflows/release.yml` to add the `setup-wfctl` step + pre-build + post-build `wfctl plugin validate-contract` invocations (snippet above).
6. Locally: `wfctl plugin validate-contract .` must PASS.
7. Open PR, CI green, admin-merge.
8. Tag next release. release.yml's gates fire on tag push.

## Registry sync (workflow#762)

`workflow-registry`'s daily cron uses `wfctl plugin registry-sync` to walk
every plugin manifest, fetch the upstream release tag via `gh`, gate it
against the same publish-grade-semver regex as `wfctl plugin
validate-contract --for-publish` (shared in `cmd/wfctl/plugin_release_grade_semver.go`),
and update `plugins/<name>/manifest.json`'s version + downloads URLs +
capabilities when drift is detected.

```
wfctl plugin registry-sync [--fix] [--plugin <name>] [--verify-capabilities] [--registry-dir <path>]
wfctl plugin registry-sync core --workflow-repo <path> [--fix] [--registry-dir <path>]
wfctl plugin registry-sync readme [--check] [--registry-dir <path>]
```

Replaces three registry maintenance scripts (`scripts/sync-versions.sh`,
`scripts/sync-core-manifests.sh`, `scripts/generate-readme.sh`) with one
Go entrypoint. `registry-sync`, `registry-sync core`, and
`registry-sync readme` own the native behavior; `workflow-registry` can keep
thin compatibility wrappers during migration, but the source of truth should
be the `wfctl` implementation.

**Defense in depth — type allowlist:** registry-sync rejects any
`plugin.json.type` value outside `{external, builtin, core, iac}`. In
particular, `type: "scaffold"` (used by `scaffold-workflow-plugin` +
`scaffold-workflow-plugin-private`) is rejected to catch accidental
re-registration of the scaffold repos as plugins.

**Defense in depth — runtime capability verification:** when
`--verify-capabilities` is set, registry-sync downloads the upstream release
asset for the current `GOOS/GOARCH`, extracts the plugin binary, and runs the
same runtime `GetManifest` check as `wfctl plugin verify-capabilities` against
the registry manifest. Registry aliases may use short names such as `github`
while the binary reports `workflow-plugin-github`, so this registry-side check
does not enforce strict name equality; the standalone
`wfctl plugin verify-capabilities` command remains strict for source-tree
`plugin.json` checks. This is intentionally slow and executes downloaded plugin
binaries; only use it in trusted registry maintenance environments.

## Registry-side gate (defense in depth)

`workflow-registry/scripts/sync-versions.sh` rejects ingest of any plugin whose upstream release tag is not strict-semver:

```bash
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver"
  continue
fi
```

Same regex as `wfctl plugin validate-contract --for-publish`. Catches plugins that bypass release.yml (self-hosted runner, manual upload, force-push).

## References

- Tracking issue: [workflow#758](https://github.com/GoCodeAlone/workflow/issues/758)
- Design + plan: `docs/plans/2026-05-23-plugin-version-discipline-design.md` + `docs/plans/2026-05-23-plugin-version-discipline.md`
- SDK: `plugin/external/sdk/buildversion.go`, `plugin/external/sdk/iacserver.go` (IaCServeOptions.BuildVersion), `plugin/external/sdk/serve.go` (WithBuildVersion)
- wfctl: `cmd/wfctl/plugin_validate_contract.go`
- Registry: `workflow-registry/scripts/sync-versions.sh`

## Verify-Capabilities (workflow#765 — runtime truth-check)

`wfctl plugin verify-capabilities` is the runtime sibling of `validate-contract`:
it spawns the plugin binary, calls `PluginService.GetManifest`, and verifies
the returned `Name` + `Version` match `plugin.json`. Catches the
**ldflag-missing truth-loop bug**: a plugin can pass `validate-contract`
(static check) and still ship a binary whose `Manifest.Version` is the
SDK's `(devel) [@ sha]` sentinel because the goreleaser ldflag never fired.

### Synopsis

```
wfctl plugin verify-capabilities --binary <path> <plugin-dir>
```

`--binary` REQUIRED (no build-from-source — operator builds via goreleaser
or `go build`).

⚠ **Executes the binary** as a subprocess. Only run against artifacts you trust.

### Local development

```bash
go build -ldflags="-X github.com/GoCodeAlone/workflow-plugin-<name>/internal.Version=v1.2.3" \
  -o /tmp/p ./cmd/<name>
wfctl plugin verify-capabilities --binary /tmp/p .
```

### CI integration (release.yml post-goreleaser, pre-publish)

```yaml
- name: Verify capabilities (post-build runtime check)
  run: |
    RUNNER_ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    BIN=$(jq -r --arg arch "$RUNNER_ARCH" \
      '[.[] | select(.type=="Binary" and .goos=="linux" and .goarch==$arch)] | .[0].path // ""' \
      dist/artifacts.json)
    "${RUNNER_TEMP}/wfctl-bin/wfctl" plugin verify-capabilities --binary "$BIN" .
```

### Version diff matrix

| plugin.json `version` | binary `Manifest.Version` | Outcome |
|---|---|---|
| `"0.0.0"` (sentinel) | non-sentinel (`"v1.2.3"`) | PASS — CI artifact under verification |
| `"0.0.0"` | sentinel (`""`, `"dev"`, `"0.0.0"`, `"(devel)..."`) | FAIL — ldflag missing |
| `"X.Y.Z"` (release) | `"vX.Y.Z"` or `"X.Y.Z"` | PASS — normalize leading v |
| `"X.Y.Z"` | sentinel | FAIL — ldflag missing |
| `"X.Y.Z"` | anything else | FAIL — version drift |

### Non-goals

- Does NOT walk per-type RPCs (`GetModuleTypes`/`GetStepTypes`/`GetTriggerTypes`) — IaC bridge returns Unimplemented.
- Does NOT diff `GetContractRegistry` — deferred to workflow#766 (requires `capabilities.iacServices` schema first).
- Does NOT build the binary — operator's responsibility.
- Does NOT verify `minEngineVersion` at runtime (not on `pb.Manifest`).

See `docs/plans/2026-05-24-verify-capabilities-design.md` for full design.
