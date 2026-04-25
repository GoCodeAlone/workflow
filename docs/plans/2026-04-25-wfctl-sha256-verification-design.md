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
| Lockfile-based reinstalls (`wfctl-lock.yaml` SHA present) | ⚠️ not enforced | ✅ use lockfile SHA |
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
download binary
if manifest SHA256 non-empty:
    verify against manifest SHA256
elif binary URL is a GitHub release URL:
    fetch checksums.txt from same owner/repo/tag
    if found: parse and verify
    if not found or asset not listed: return error
    (unless --skip-checksum is set)
else (non-GitHub URL, no SHA in manifest):
    return error unless --skip-checksum
write verified SHA to lockfile entry
```

The `checksums.txt` format is the goreleaser standard:
```
<sha256hex>  <filename>
```
Parsing reuses the same logic already in `verifyAssetChecksum` (used by `wfctl update`).

The function that derives the `checksums.txt` URL from a release asset URL is extracted
into a shared helper so both `plugin install` and `wfctl update` use identical logic.

### 2. Plugin install with `--url`

When a user specifies `--url <url>` directly:

- If `--sha256 <hex>` is also provided: download, verify, succeed.
- If the URL is a GitHub release asset and no `--sha256` is given: auto-fetch
  `checksums.txt` as above.
- Otherwise: fail with a message explaining how to supply the expected hash.

This prevents `--url` from being a silent bypass for the integrity policy.

### 3. `wfctl update` hardening

The self-update path already calls `findChecksumAsset`. Change the guard from
`if checksumAsset != nil` to a hard failure when no checksum asset is found,
unless `--skip-checksum` is passed.

### 4. Lockfile write-back

After a successful verified install (via manifest SHA or auto-fetched checksums.txt),
write the verified SHA256 to the corresponding `WfctlLockPlatform.SHA256` field in
`.wfctl-lock.yaml`. On subsequent installs (same plugin version), use the lockfile SHA as
the authoritative source — skip the extra `checksums.txt` HTTP call.

This field already exists in the lockfile schema (`WfctlLockPlatform.SHA256`); this
change makes it load-on-install and enforce-on-reinstall.

### 5. Escape hatch: `--skip-checksum`

A flag `--skip-checksum` is added to `wfctl plugin install` and `wfctl update`. When set,
all SHA256 checks are bypassed with a stderr warning:

```
warning: --skip-checksum is set; binary integrity not verified
```

The flag name `--skip-checksum` is intentionally longer than `--no-verify` to create
friction and make it visible in CI logs. It is not documented in `--help` short form; it
appears in the extended help only.

---

## Error messages

When verification fails or SHA is unavailable:

```
error: plugin "foo" downloaded from <url> has no SHA-256 in its manifest and
no checksums.txt was found at <release-url>/checksums.txt.

To proceed without verification (not recommended):
  wfctl plugin install foo --skip-checksum

To add verification, ask the plugin author to publish a checksums.txt
alongside their release assets (goreleaser does this automatically).
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
runs unconditionally on every install; cosign runs only when `verify.signature_identity`
is configured in `app.yaml`'s `requires.plugins` entry.

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
