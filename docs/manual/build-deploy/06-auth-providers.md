# Auth Providers

How wfctl authenticates with container registries and private plugin sources.

---

## Registry auth

Registry credentials are declared in `ci.registries[].auth` and never hardcoded.

### `auth.env`

The most common pattern: read the token from a named environment variable.

```yaml
auth:
  env: DIGITALOCEAN_TOKEN
```

In CI, set the corresponding GitHub secret (`DIGITALOCEAN_TOKEN`). `wfctl ci init` includes it in the generated deploy workflow's `env:` block automatically.

### `auth.file`

Read credentials from a file (e.g. a Docker config.json or service account JSON).

```yaml
auth:
  file: /run/secrets/registry-credentials
```

### `auth.aws_profile`

For AWS ECR: use a named AWS CLI profile.

```yaml
auth:
  aws_profile: ci-deploy
```

### `auth.vault`

Fetch credentials from HashiCorp Vault at runtime.

```yaml
auth:
  vault:
    address: https://vault.myorg.internal
    path: secret/data/registry/docr
```

---

## Provider login mechanics

### DigitalOcean (`type: do`)

Runs `doctl registry login --expiry-seconds 3600` with `DIGITALOCEAN_TOKEN` injected into the process environment. Dry-run prints the planned command without executing.

### GitHub Container Registry (`type: github`)

Runs `docker login ghcr.io --username x-access-token --password-stdin` with the token piped via stdin.

### Stub providers

`gitlab`, `aws`, `gcp`, `azure` return `ErrNotImplemented` in v0.14.0. Full implementations are tracked in the issue tracker.

---

## Private plugin repos

Plugins from private GitHub/GitLab repos can be installed with an auth block in `requires.plugins[]`:

```yaml
requires:
  plugins:
    - name: workflow-plugin-payments
      source: github.com/MyOrg/workflow-plugin-payments
      auth:
        env: RELEASES_TOKEN
```

Before fetching, wfctl:
1. Reads `RELEASES_TOKEN` from the environment.
2. Writes `git config --global url."https://x-access-token:TOKEN@github.com/".insteadOf "https://github.com/"`.
3. Sets `GOPRIVATE=github.com`.
4. Installs the plugin.
5. Undoes the git config and restores `GOPRIVATE`.

### GitLab support (T31)

For GitLab sources, set `provider: gitlab` in the auth block. The URL rewriting uses `oauth2` instead of `x-access-token`:

```yaml
auth:
  provider: gitlab
  env: GITLAB_TOKEN
```

---

## Security considerations

- Never commit tokens to source control. Always use env vars or secrets managers.
- `auth.env` values are resolved at runtime; the field stores only the variable *name*, not the value.
- `wfctl registry login --dry-run` prints the planned command with `<token>` redacted.

---

*See also:* [02 — ci.registries schema](./02-ci-registries-schema.md) · [Tutorial §8 — Multi-registry](../../tutorials/build-deploy-pipeline.md#8-multi-registry-docr--ghcr)
