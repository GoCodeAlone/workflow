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
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: GoCodeAlone/setup-wfctl@v1
        with: { version: v0.61.0 }

      # 1. Pre-build gate: static contract + tag format
      - name: Validate plugin contract for publish (pre-build)
        run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .

      # 2. Build (goreleaser mutates plugin.json or writes .release/plugin.json)
      - uses: goreleaser/goreleaser-action@v7
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
