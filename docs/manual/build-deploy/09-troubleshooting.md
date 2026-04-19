# Troubleshooting

Diagnose and fix common issues with `wfctl build`, `wfctl registry`, and `wfctl ci init`.

---

## Diagnostics checklist

Before diving into specific errors, run these two commands:

```sh
# 1. Validate your config (catches schema errors, missing fields, bad references)
wfctl ci validate --config infra.yaml

# 2. Dry-run to preview all planned actions
WFCTL_BUILD_DRY_RUN=1 wfctl build --config infra.yaml
```

---

## Build errors

### `go builder: path is required`

**Cause**: A `type: go` target is missing its `path:` field.

**Fix**:
```yaml
targets:
  - name: server
    type: go
    path: ./cmd/server   # ← add this
```

---

### `nodejs builder: script is required`

**Cause**: A `type: nodejs` target has no `config.script`.

**Fix**:
```yaml
targets:
  - name: frontend
    type: nodejs
    path: ./ui
    config:
      script: build   # ← add this
```

---

### `custom builder cannot enforce hardening` (SecurityLint warn)

**Cause**: `type: custom` always emits this warning. It is informational only.

**Fix**: Expected behavior — no action needed. If running `--security-audit`, this will not cause an exit-1 (it's severity `warn`, not `critical`).

---

### `dockerfile build ... exit status 1`

**Cause**: Docker build failed. Detailed output is printed to stderr.

**Debug**:
```sh
docker build --file Dockerfile --tag test:local .   # run directly
```

---

### `syft not found on PATH`

**Cause**: SBOM generation requires `syft` binary.

**Fix**: Install syft:
```sh
brew install syft               # macOS
# or
curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh
```

Or disable SBOM for local dev via `environments.local.build.security.sbom: false`.

---

## Registry errors

### `doctl registry login: exit status 1`

**Cause**: `DIGITALOCEAN_TOKEN` is not set or is invalid.

**Fix**:
```sh
export DIGITALOCEAN_TOKEN=your_token
wfctl registry login --registry docr --dry-run   # verify
```

In CI, ensure the secret is configured in GitHub Actions.

---

### `push_to references undeclared registry`

**Cause**: A `containers[].push_to` entry names a registry that isn't in `ci.registries[]`.

**Fix**: Match the name exactly (case-sensitive):
```yaml
registries:
  - name: docr   # ← this exact string
containers:
  - name: api
    push_to: [docr]   # ← must match
```

---

### `docker login ghcr.io: unauthorized`

**Cause**: `GITHUB_TOKEN` lacks `packages: write` permission.

**Fix**: In GitHub Actions, add to your workflow:
```yaml
permissions:
  packages: write
```

`wfctl ci init` includes this in generated `deploy.yml`.

---

## Config errors

### `ci.build.containers[N]: method=dockerfile requires dockerfile field`

**Cause**: Container target has no `dockerfile:` field.

**Fix**:
```yaml
containers:
  - name: api
    method: dockerfile
    dockerfile: Dockerfile   # ← add this
```

---

### `retention.untagged_ttl is not a valid duration`

**Cause**: The TTL string doesn't parse as a Go duration.

**Fix**: Use valid Go duration syntax: `168h` (7 days), `720h` (30 days). Not `7d` or `1 week`.

---

### `circular import detected`

**Cause**: Config file A imports B which imports A.

**Fix**: Flatten the import chain. Use a shared base config imported by both.

---

## CI pipeline errors

### Deploy job fails: `git clone failed`

**Cause**: The `ref:` passed to `actions/checkout` resolves to a non-existent SHA.

**Fix**: Generated `deploy.yml` uses `${{ github.event.workflow_run.head_sha || github.sha }}`. If the workflow is triggered manually, `github.sha` is used as fallback.

---

### Security audit exits 1

**Cause**: `wfctl build --security-audit` found a critical finding.

**Common critical findings**:
- `USER root` in Dockerfile — add `USER nonroot` before `ENTRYPOINT`.
- Missing `USER` directive — same fix.
- Secret embedded in build command — use BuildKit `--secret` instead.
- Base image violates `allow_prefixes` policy — switch to an allowed base.

---

## Getting help

- File bugs at [github.com/GoCodeAlone/workflow/issues](https://github.com/GoCodeAlone/workflow/issues).
- Run `wfctl help` or `wfctl <command> --help` for flag documentation.
- Use `WFCTL_BUILD_DRY_RUN=1` liberally — it never executes any build or push commands.

---

*See also:* [Tutorial §16 — Debugging](../../tutorials/build-deploy-pipeline.md#16-debugging-a-failed-deploy) · [05 — CLI Reference](./05-cli-reference.md)
