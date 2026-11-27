# `ci.registries` Schema Reference

Complete field reference for the `ci.registries` block.

---

## Structure

```yaml
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg/my-image
      auth:
        env: DIGITALOCEAN_TOKEN
      retention:
        keep_latest: 20
        untagged_ttl: 168h
        schedule: "0 7 * * 0"
```

---

## `ci.registries[]`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | ✓ | Unique registry name; used in `containers[].push_to` |
| `type` | string | ✓ | Provider: `do`, `github`, `gitlab`*, `aws`*, `gcp`*, `azure`* |
| `path` | string | ✓ | Registry base path (without tag) |
| `auth` | Auth | — | Credentials for login/push |
| `retention` | Retention | — | Tag pruning policy |

> \* Stub providers in v0.14.0 — return `ErrNotImplemented`. Full implementations in later releases.

---

## `auth`

| Field | Type | Description |
|-------|------|-------------|
| `env` | string | Environment variable name containing the token |
| `file` | string | Path to a credentials file |
| `aws_profile` | string | AWS profile name (aws provider) |
| `vault` | VaultAuth | HashiCorp Vault path for credentials |

### `vault`

```yaml
auth:
  vault:
    address: https://vault.myorg.internal
    path: secret/data/registry/docr
```

---

## `retention`

| Field | Type | Description |
|-------|------|-------------|
| `keep_latest` | int | Requested number of recent tags to retain (min: 1); provider support is required |
| `untagged_ttl` | duration | Requested untagged age limit (e.g. `168h`); DOCR and GHCR pruning reject this unsupported policy |
| `schedule` | cron | Cron expression for automatic pruning (e.g. `0 7 * * 0`) |

DOCR and GHCR pruning currently fail closed, including `--dry-run`. Retention
requires an exact `registry.digitalocean.com/<registry>/<repository>` or
`ghcr.io/<org>/<package>` path; registry/org roots, tags and digest suffixes are
refused. Login and push can still use registry/org base paths.

These providers cannot enforce the configured untagged TTL or protect deployed
and rollback digests during their old deletion operations. DOCR's registry-wide
GC cannot implement repository-scoped retention. GHCR's whole-version deletion
can remove tagged images. Exact scopes and `keep_latest` alone do not establish
preservation, recoverability or approval of each destructive action. Pruning
therefore returns an error before reading credentials or making commands/API
calls. All images remain intact. There is no approval flag that bypasses this
containment; a future implementation must enforce those prerequisites first.

### `wfctl ci init` integration

When any registry declares `retention.schedule`, `wfctl ci init` emits `.github/workflows/registry-retention.yml`. DOCR/GHCR jobs using the current providers fail with the safety refusal and perform no deletion:

```yaml
on:
  schedule:
    - cron: '0 7 * * 0'
jobs:
  prune:
    steps:
      - run: wfctl registry prune --registry docr
        env:
          DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
```

---

## Validation rules

- `name` must be unique across all registries.
- `type` must match a registered provider.
- `containers[].push_to` entries must reference a declared `registries[].name`.
- `retention.keep_latest` must be ≥ 1.
- `retention.untagged_ttl` must parse as a Go duration string.

---

## Examples by provider

### DigitalOcean Container Registry

```yaml
- name: docr
  type: do
  path: registry.digitalocean.com/myorg
  auth:
    env: DIGITALOCEAN_TOKEN
```

### GitHub Container Registry (GHCR)

```yaml
- name: ghcr
  type: github
  path: ghcr.io/myorg
  auth:
    env: GITHUB_TOKEN
```

### Both registries (dual-push)

```yaml
registries:
  - name: docr
    type: do
    path: registry.digitalocean.com/myorg
    auth: { env: DIGITALOCEAN_TOKEN }
  - name: ghcr
    type: github
    path: ghcr.io/myorg
    auth: { env: GITHUB_TOKEN }
```

Then reference both in `containers[].push_to: [docr, ghcr]`.

---

*See also:* [Tutorial §8 — Multi-registry](../../tutorials/build-deploy-pipeline.md#8-multi-registry-docr--ghcr) · [06 — Auth Providers](./06-auth-providers.md) · [05 — CLI Reference](./05-cli-reference.md)
