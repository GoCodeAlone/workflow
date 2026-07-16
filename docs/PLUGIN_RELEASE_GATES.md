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
6. **Consumed Workflow contracts are explicit**: every Workflow provider
   contract used by the plugin is listed in root `plugin.json` under
   `consumesContracts[]`, with its inclusive protocol range. Workflow's
   cross-repository compatibility gate reads this field; repository names are
   never used to infer contracts.

## Contract-consumer compatibility gate

Workflow keeps `.github/contract-consumers.json` as a generated cache of public,
released consumers. Each record binds a plugin repository and `vSEMVER` tag to
an exact commit SHA, source-manifest and GoReleaser-config SHA-256, release
entrypoint and binary basename, accepted first-build environment and release
targets, complete release ldflags, version ldflag targets, and the manifest's
`consumesContracts[]`. The authoritative input is always a clean checkout whose
`HEAD`, local release tag, and GitHub origin match that released commit.

After publishing a plugin release that adds or changes a consumed contract,
regenerate the complete cache from exact checkouts. Pass every current consumer
in one invocation. Omission alone cannot retire a consumer:

```bash
.github/workflows/scripts/generate-contract-consumers.sh \
  --output .github/contract-consumers.json \
  --consumer GoCodeAlone/workflow-plugin-foo v1.2.3 \
    <40-character-release-commit> ../workflow-plugin-foo

# CI/parity form: exits non-zero if the committed bytes are stale.
.github/workflows/scripts/generate-contract-consumers.sh \
  --check .github/contract-consumers.json \
  --consumer GoCodeAlone/workflow-plugin-foo v1.2.3 \
    <40-character-release-commit> ../workflow-plugin-foo
```

The generator reads the first GoReleaser build from that checkout so the cache
uses the entrypoint, exact static binary basename, reproducible environment,
and full `{{.Version}}` ldflags the released plugin actually ships; it does not
assume a `cmd/<repository-name>` layout. Accepted environment entries are limited to
`CGO_ENABLED` and the canonical public `GOPRIVATE=github.com/GoCodeAlone/*`
setting. The parsed release target must include Linux/AMD64, and the runner
builds only when its actual `GOOS`/`GOARCH` is among the released targets. The
supported first-build keys are `id`, `main`, `binary`, `env`, `goos`, `goarch`,
and `ldflags`; every other parsed key and environment entry fails closed until
the runner explicitly supports it. A release must contain exactly one of
`.goreleaser.yaml` or `.goreleaser.yml`; dual configs, duplicate YAML mapping
keys, and aliases fail closed.

`.github/contract-path-map.json` maps known Workflow contract paths to contract
IDs and their current protocol versions. Every production selection freezes
the complete canonical path map, candidate cache, and Git-derived changed paths
in parent-shell memory and checks the map plus protocol-test source against the
trusted selector digests. The authoritative selection job never sets up or
executes candidate Go. A separate output-free wire-validation job compiles all
four valid provider services, emits the complete protocol map returned by the
runtime `ContractRegistry` path, and compares that record with its frozen map;
an affected consumer whose inclusive range excludes that version fails
selection. SDK, loader, shared-protobuf, rename deletions, all `.github` changes,
and unknown code changes select every relevant cached consumer;
documentation-only changes select none. Selection is deterministic, uses at
most ten consumers per shard, and uploads its evidence JSON on every pull
request. Early validation failures retain the exact cache and path-map inputs
plus a structured evidence record with the selector exit status and changed
paths; candidate symlinks are rejected before any input is copied into the
artifact. Incompatibility failures keep the selector's consumer, contract, and
decision details. A pull request may add or update cache records for runner
rederivation, but cannot change repository identity, regress an existing
repository's release, rebind an existing tag to a different commit, or remove a
trusted-base consumer by omission. Retirement
requires one reviewed change that removes the cache record and adds its exact
`owner/repository` to the digest-bound path map's `retiredRepositories` list;
the selector rejects both an unlisted removal and a retired repository that
remains cached.

For each selected consumer, public CI anonymously fetches the exact tag,
verifies its commit and cached release-metadata hashes, regenerates the complete
cache record from that checkout and requires byte equality, applies a local Go
module replacement to the Workflow pull-request checkout, compiles the release
entrypoint to its exact release basename with the accepted build environment
and complete release ldflags, and runs
`wfctl plugin verify-capabilities` against its real binary. This gate never uses
cloud secrets, OIDC, provider APIs, provider CLIs, or self-hosted runners. A
plugin whose startup or capability RPC requires live provider access is not a
valid public contract consumer.

The public-workflow policy hash-binds the selector, runner, and orchestration
regression. The runner also binds the exact generator digest before
regeneration, so every PR-supplied shell executable is inside the same reviewed
trust boundary. The regression always sends a hermetic SDK release through the
production runner and a real local `wfctl` plugin handshake, even while the
committed consumer cache is empty.

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
      - uses: GoCodeAlone/setup-wfctl@526e23ee7d3cae9ba8ba09d87090879e04c7aab2 # v1

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

`setup-wfctl` defaults to the latest released Workflow CLI and logs the
resolved version before download. Do not hand-roll `curl` downloads or pin a
specific wfctl version in plugin release workflows unless the workflow documents
a compatibility reason. Audit existing release workflows with:

```
wfctl plugin release-workflow
wfctl plugin release-workflow --fix
```

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
- Does NOT build the binary — operator's responsibility.
- Does NOT verify `minEngineVersion` at runtime (not on `pb.Manifest`).

See `docs/plans/2026-05-24-verify-capabilities-design.md` for full design.
