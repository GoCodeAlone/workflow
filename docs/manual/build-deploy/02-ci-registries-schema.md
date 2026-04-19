# `ci.registries` Schema Reference

Complete field reference for the `ci.registries` block.

---

## Structure

```yaml
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
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
| `keep_latest` | int | Keep this many most-recent tags (min: 1) |
| `untagged_ttl` | duration | Delete untagged manifests older than this (e.g. `168h`) |
| `schedule` | cron | Cron expression for automatic pruning (e.g. `0 7 * * 0`) |

The `latest` tag is always preserved by `wfctl registry prune`.

### `wfctl ci init` integration

When any registry declares `retention.schedule`, `wfctl ci init` emits `.github/workflows/registry-retention.yml`:

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
