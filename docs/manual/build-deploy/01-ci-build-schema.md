# `ci.build` Schema Reference

Complete field reference for the `ci.build` block in your workflow config.

---

## Top-level structure

```yaml
ci:
  build:
    targets: []          # Go/Node.js/custom binary targets
    containers: []       # Container image build targets
    assets: []           # Non-binary build artifacts (bundles, etc.)
    security: {}         # Supply-chain hardening defaults
```

---

## `ci.build.targets[]`

Typed build targets dispatched to builder plugins. Supersedes the legacy `binaries:` field.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | ✓ | Unique target name |
| `type` | string | ✓ | Builder type: `go`, `nodejs`, `custom` (or installed plugin name) |
| `path` | string | ✓ (not custom) | Source path passed to the builder |
| `config` | map | — | Builder-specific config (see builder docs) |
| `environments` | map | — | Per-environment config overrides |

### `config` for `type: go`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ldflags` | string | — | `-ldflags` passed to `go build` |
| `tags` | string | — | `-tags` build constraint |
| `cgo` | bool | false | Enable CGO |
| `link_mode` | string | — | `external` or `internal` (required when `cgo: true`) |
| `builder_image` | string | — | Docker image for CGO builds |
| `os` | string | — | `GOOS` override |
| `arch` | string | — | `GOARCH` override |
| `extra_flags` | string[] | — | Additional flags appended to `go build` |

### `config` for `type: nodejs`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `script` | string | ✓ | npm script name to run (e.g. `build`) |
| `cwd` | string | target `path` | Working directory |
| `node_version` | string | — | Node.js version (informational) |
| `package_manager` | string | `npm` | `npm`, `yarn`, or `pnpm` |
| `npm_flags` | string | — | Extra flags passed to the install step |

### `config` for `type: custom`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `command` | string | ✓ | Shell command run via `sh -c` |
| `outputs` | string[] | — | Paths to collect as artifacts |
| `env` | map | — | Extra environment variables |
| `timeout` | duration | — | Command timeout (e.g. `120s`) |

### `environments` overrides

```yaml
targets:
  - name: server
    type: go
    path: ./cmd/server
    config:
      ldflags: "-s -w"
    environments:
      local:
        config:
          ldflags: ""
          race: true
```

---

## `ci.build.containers[]`

Container images to build and push.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | ✓ | Unique container name |
| `method` | string | `dockerfile` | Build method: `dockerfile` or `ko` |
| `dockerfile` | string | ✓ (dockerfile) | Path to Dockerfile |
| `context` | string | `.` | Build context directory |
| `registry` | string | — | Registry prefix (deprecated; use `push_to`) |
| `tag` | string | `latest` | Image tag |
| `ko_package` | string | ✓ (ko) | Go package path for ko |
| `ko_base_image` | string | — | Base image for ko |
| `ko_bare` | bool | false | Use `--bare` with ko |
| `platforms` | string[] | — | BuildKit platforms (e.g. `linux/amd64,linux/arm64`) |
| `build_args` | map | — | `--build-arg KEY=VALUE` pairs |
| `secrets` | BuildSecret[] | — | BuildKit secrets |
| `cache` | CacheConfig | — | BuildKit cache import/export |
| `target` | string | — | Multi-stage build target |
| `labels` | map | — | OCI image labels |
| `extra_flags` | string[] | — | Extra flags appended to `docker build` |
| `external` | bool | false | Reference a pre-built image; skip local build |
| `source` | ExternalSource | — | Tag resolution for external images |
| `push_to` | string[] | — | Registry names from `ci.registries[].name` |

### `secrets[]` (BuildKit)

```yaml
secrets:
  - id: goprivate_token
    env: GOPRIVATE_TOKEN      # read from this env var
  - id: npmrc
    src: .npmrc               # read from this file path
```

### `cache`

```yaml
cache:
  from:
    - type: registry          # pull from registry cache
      ref: registry.io/myorg/api:cache
    - type: local             # use local Docker cache (injected for local env)
  to:
    - type: registry
      ref: registry.io/myorg/api:cache
```

### `source` (external images)

```yaml
source:
  ref: registry.digitalocean.com/myorg/base   # base image ref (tag resolved separately)
  tag_from:
    - env: BASE_IMAGE_TAG                      # try env var first
    - command: "git describe --tags --abbrev=0" # fall back to shell command
```

---

## `ci.build.security`

Supply-chain hardening applied to all image builds. **All fields default to secure values** — omitting this block enables hardened defaults.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hardened` | bool | `true` | Enable all hardening (distroless, non-root) |
| `sbom` | bool | `true` | Generate + attach CycloneDX SBOM via syft |
| `provenance` | string | `slsa-3` | Provenance level: `slsa-3`, `slsa-2`, or `""` |
| `sign` | bool | false | Sign with cosign |
| `non_root` | bool | `true` | Enforce non-root USER in Dockerfile |
| `base_image_policy` | Policy | — | Restrict allowed base images |

### `base_image_policy`

```yaml
security:
  base_image_policy:
    allow_prefixes:
      - gcr.io/distroless/
      - cgr.dev/chainguard/
    deny_prefixes:
      - ubuntu:latest
```

### Opting out

```yaml
ci:
  build:
    security:
      hardened: false
      sbom: false
```

> **Warning**: disabling `hardened` emits a supply-chain warning during `wfctl ci validate`.

---

## `ci.build.assets[]`

Non-binary artifacts (e.g. Hugo sites, static bundles).

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Asset name |
| `build` | string | Build command |
| `path` | string | Output path |

---

## Legacy: `binaries:` → `targets:`

Configs using the legacy `binaries:` key are automatically coerced to `targets:` with `type: go` on load. A deprecation warning is logged. Migrate to `targets:` to silence it:

```yaml
# Before (legacy)
binaries:
  - name: server
    path: ./cmd/server

# After
targets:
  - name: server
    type: go
    path: ./cmd/server
```

---

*See also:* [Tutorial](../../tutorials/build-deploy-pipeline.md) · [02 — ci.registries](./02-ci-registries-schema.md) · [04 — Builder Plugins](./04-builder-plugins.md)
