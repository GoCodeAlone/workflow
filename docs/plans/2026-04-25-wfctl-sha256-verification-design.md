# Design: wfctl SHA-256 Verification by Default for Binary Downloads

**Date**: 2026-04-25
**Status**: Draft — awaiting approval before implementation

---

## Motivation

wfctl downloads external plugin binaries at install time. Today SHA-256 verification is
**opt-in**: the check runs only when the registry manifest includes a `sha256` field. If
the field is absent the download proceeds silently without any integrity check.

Similarly, `wfctl update` (self-update) verifies against a `checksums.txt` release asset
*only if the asset exists* — no failure when it is absent.

The goal of this change is to make verification **fail-closed by default**: a binary
download that cannot be verified must not succeed without an explicit opt-out.

A related gap is `--url` installs, where a user supplies a download URL directly and
there is no manifest SHA to check against.

---

## Scope

| Surface | Current | Target |
|---|---|---|
| `wfctl plugin install` — registry manifest with SHA | ✅ verifies | unchanged |
| `wfctl plugin install` — registry manifest without SHA | ⚠️ skips silently | ❌ fails closed |
| `wfctl plugin install --url <url>` | ⚠️ no verification | ❌ requires `--sha256 <hex>` or auto-fetch |
| `wfctl update` (self-update) — `checksums.txt` present | ✅ verifies | unchanged |
| `wfctl update` — `checksums.txt` absent | ⚠️ skips silently | ❌ fails closed |
| Lockfile-based reinstalls (`.wfctl-lock.yaml` SHA present) | ✅ already enforced — hard-fails on mismatch | unchanged |
| `setup-wfctl` action | ⚠️ no verification | out of scope (separate repo) |

---

## Approaches Considered

### Approach A — Strict fail-closed on missing SHA, no auto-fetch

Change the `if dl.SHA256 != ""` guard to a hard failure when SHA is absent. Every
registry manifest must include a `sha256` field. Provide `--skip-checksum` as an escape
hatch.

**Pro**: Simple, no extra HTTP calls, deterministic.  
**Con**: Breaks any plugin manifest not yet publishing SHA256. Requires coordinated
manifest updates across all plugins before landing.

### Approach B — Auto-fetch `checksums.txt` with fail-closed fallback *(recommended)*

When `dl.SHA256 == ""`, attempt to fetch a goreleaser-style `checksums.txt` file from
the same GitHub release (same tag, same repo). Parse it to find the expected hash for the
downloaded asset. If the file is found and the asset matches: verify and continue. If the
file is not found or the asset is not listed: fail with a clear error.

**Pro**: Works immediately for all GoCodeAlone plugins (goreleaser publishes
`checksums.txt` for every release). Zero manifest changes required. Mirrors the pattern
already used by `wfctl update`. Lockfile records the verified SHA, so CI reinstalls skip
the extra HTTP call.  
**Con**: One extra HTTP request per first install of a manifest-less plugin.

### Approach C — Lockfile-first, checksums.txt as fallback

On install, check the lockfile for a recorded SHA256 first. Use it if present (no HTTP
call). Fall back to Approach B for cache misses. This is the long-term evolution once
lockfile adoption is higher.

**Pro**: Zero extra HTTP calls for lockfile users; reproducible CI.  
**Con**: Adds lockfile as a dependency before it has wide adoption.

**Chosen approach**: B as the implementation baseline, with Approach C's lockfile-first
logic layered on top (the lockfile fields already exist; using them is additive).

---

## Design

### 1. Plugin install: auto-fetch `checksums.txt`

**`installPluginFromManifest`** is modified as follows:

```
download binary archive
if manifest SHA256 non-empty:
    verify archive against manifest SHA256
elif binary URL is a GitHub release URL:
    fetch checksums.txt from same owner/repo/tag
    if found: parse and verify archive hash
    if not found or asset not listed: return error
    (unless --skip-checksum is set)
else (non-GitHub URL, no SHA in manifest):
    return error unless --skip-checksum
extract binary from archive
compute binary SHA256 post-extraction
write binary SHA to WfctlLockPluginEntry.SHA256 in lockfile
```

The `checksums.txt` format is the goreleaser standard:
```
<sha256hex>  <filename>
```
Parsing uses the goreleaser standard format (`<sha256hex>  <filename>`), the same format as
the existing `verifyAssetChecksum` in `update.go`.

A new shared helper `lookupChecksumForURL(downloadURL string) (string, error)` is added. It
derives the `checksums.txt` URL from the release asset URL (stripping the asset filename and
appending `checksums.txt`), downloads it, parses each `<sha256hex>  <filename>` line, and
returns the expected SHA256 hex for the asset matching the basename of `downloadURL`.

`assetName` is derived internally by parsing the URL first, then URL-decoding only the path
component before extracting the basename — equivalent to:
```go
u, _ := url.Parse(downloadURL)
assetName, _ := url.PathUnescape(path.Base(u.Path))
```
This avoids passing the full raw URL string (which includes query/fragment text) to
`path.Base`, and ensures URL-encoded characters in filenames are decoded before matching
against plain-text `checksums.txt` entries.

The `wfctl update` path is refactored to call this same helper, replacing its current
`findChecksumAsset(rel.Assets)` + `verifyAssetChecksum` pattern — the new helper derives the
URL from the asset's `BrowserDownloadURL` directly rather than searching the API asset list
for a `checksums.txt` entry. Both paths become consistent and share the same verification
logic.

### 2. Plugin install with `--url`

When a user specifies `--url <url>` directly:

- If `--sha256 <hex>` is also provided: download, verify, succeed.
- If the URL matches the GitHub release download pattern and no `--sha256` is given:
  auto-fetch `checksums.txt` via `lookupChecksumForURL`. Fail if the file is absent or the
  asset is not listed. The match uses the same constraints as `parseGitHubReleaseDownloadURL`:
  HTTPS scheme only; host lowercased and compared — must equal `github.com` or have a
  `.github.com` suffix (the dot separator prevents `evilgithub.com` from matching; trailing
  dots stripped before comparison); no userinfo component (`user:pass@`) allowed; no
  non-default port (port field must be empty, implying 443); path must be exactly
  `/owner/repo/releases/download/tag/filename` (six non-empty segments).
- For any other URL with no `--sha256`: fail with a clear error explaining how to provide
  a hash or use `--skip-checksum`.

This prevents `--url` from being a silent bypass for the integrity policy regardless of
whether the user provides an explicit hash or relies on auto-fetch for GitHub releases.

### 3. `wfctl update` hardening

The self-update path already calls `findChecksumAsset`. Change the guard from
`if checksumAsset != nil` to a hard failure when no checksum asset is found,
unless `--skip-checksum` is passed.

### 4. Lockfile write-back

Both `WfctlLockPlatform.SHA256` and `WfctlLockPluginEntry.SHA256` represent
**installed-binary SHAs** — hashes of the extracted plugin binary on disk, NOT of the
download archive. This is confirmed by `installFromWfctlLockfile`, which verifies both
fields against `hashFileSHA256(destDir/fsName)` after extraction.

The archive SHA verified against `checksums.txt` at download time is a separate,
transient check — it is not stored in any lockfile field.

Current write-back behavior:

- `WfctlLockPluginEntry.SHA256` (top-level binary hash): written by `installFromWfctlLockfile`
  after each successful install via `hashFileSHA256(binaryPath)`. Enforced on subsequent
  reinstalls by both `installFromLockfile` and `installFromWfctlLockfile` (hard-fail on
  mismatch when non-empty).
- `WfctlLockPlatform.SHA256` (per-platform binary hash): verified when non-empty, but
  **not written** by any current code path — it must be pre-populated externally (e.g. by
  `wfctl plugin lock` or a future write-back).

This design adds post-extraction binary SHA write-back to `WfctlLockPluginEntry.SHA256` in
the `installPluginFromManifest` path (first install from a registry manifest or `--url`):
after the archive is verified and extracted, `hashFileSHA256` is called on the installed
binary and the result is written to `WfctlLockPluginEntry.SHA256`. The archive hash
verified against `checksums.txt` is NOT written to the lockfile. The
`installFromWfctlLockfile` path already performs this binary SHA write-back. On subsequent
installs (same plugin version), the lockfile enforces the binary-level hash without a
network round-trip — the `checksums.txt` fetch is skipped entirely for lockfile reinstalls.

### 5. Escape hatch: `--skip-checksum`

A flag `--skip-checksum` is added to `wfctl plugin install` and `wfctl update`. When set,
all SHA256 checks are bypassed with a stderr warning:

```
warning: --skip-checksum is set; binary integrity not verified
```

The flag name `--skip-checksum` is intentionally longer than `--no-verify` to create
friction and make it visible in CI logs. It is registered via the standard Go `flag` package
and appears in `wfctl plugin install --help` and `wfctl update --help` output alongside all
other flags.

---

## Error messages

**Case A — GitHub release URL, `checksums.txt` absent or asset not listed:**
```
error: plugin "foo": no SHA-256 in manifest and checksums.txt not found or
asset not listed at https://github.com/OWNER/REPO/releases/download/TAG/checksums.txt

To proceed without verification (not recommended):
  wfctl plugin install --skip-checksum foo

To add verification, ask the plugin author to publish a checksums.txt
alongside their release assets (goreleaser does this automatically).
```
The derived checksums.txt URL is always printed verbatim so users can curl it manually.

**Case B — Non-GitHub URL, no `--sha256` provided:**
```
error: plugin "foo": URL https://example.com/plugin.tar.gz is not a GitHub
release URL and no --sha256 was provided; cannot verify integrity.

Provide a checksum:
  wfctl plugin install --url https://example.com/plugin.tar.gz --sha256 <hex>

Or proceed without verification (not recommended):
  wfctl plugin install --skip-checksum --url https://example.com/plugin.tar.gz foo
```

When the checksum mismatches:

```
error: checksum mismatch for "foo":
  got:  <actual-hex>
  want: <expected-hex>
This may indicate a corrupted download or a supply-chain attack.
```

---

## Interaction with supply-chain plugin

SHA-256 verification is the **integrity layer**: it confirms the downloaded bytes match
what the release published. The supply-chain plugin (cosign `install_verify` hook) is the
**authenticity layer**: it confirms the binary was signed by the expected OIDC identity.

These are independent and complementary:

```
download → SHA-256 verify (this design) → cosign verify (install_verify hook, opt-in)
```

This design does not change the cosign hook path. The two layers can coexist: SHA-256
verification runs by default on every install and is fail-closed — a binary that cannot be
verified does not install. It can be bypassed with `--skip-checksum` (emits a warning), or
implicitly skipped for a lockfile entry with an empty SHA (the SHA is recorded on first
successful install). Cosign verification runs only when `verify.signature` is set to `required` in
`app.yaml`'s `requires.plugins[*].verify` entry (`PluginVerifyConfig.Signature`).
The `verify` block also controls `sbom` (SBOM presence) and `vuln_policy` (OSV scan
policy) — all independent of SHA-256 integrity verification.

---

## Migration path

| Plugin source | Current SHA status | After this change |
|---|---|---|
| GoCodeAlone registry plugins (goreleaser) | SHA absent in manifest | Auto-fetched from `checksums.txt` — transparent |
| Third-party plugin, goreleaser build | SHA absent | Auto-fetched — transparent |
| Third-party plugin, no goreleaser | SHA absent, no `checksums.txt` | Fails; author must publish checksum or user uses `--skip-checksum` |
| Any plugin, manifest already has SHA | SHA present | Unchanged behavior |
| Lockfile reinstall | SHA in lockfile | Used directly — no HTTP call |

No changes to existing registry manifests are required to unblock this landing. The
`checksums.txt` auto-fetch handles all GoCodeAlone-published plugins immediately.

Publishing SHA256 directly in registry manifests remains the preferred long-term form
(eliminates the extra HTTP call on first install) and should be added to the goreleaser
workflow as a follow-up.

---

## Testing

- Unit: `verifyChecksum` (existing), `parseChecksumsTxt` (new helper), lockfile write-back
- Integration: mock HTTP server serving a release asset + `checksums.txt` — install
  succeeds with correct hash, fails with tampered hash, fails with missing checksums.txt
  (no `--skip-checksum`), succeeds with `--skip-checksum`
- `wfctl update` test: mock release without checksums.txt → hard failure

---

## Out of scope

- `setup-wfctl` GitHub Action (separate repo — tracked separately)
- Adding SHA256 to existing registry manifest JSON files (follow-up)
- Publishing `checksums.txt` URLs in registry manifests (follow-up)
- Cosign verification changes (separate supply-chain concern)
