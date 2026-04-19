# CLI Reference

Complete reference for all `wfctl` commands added or updated in v0.14.0.

---

## `wfctl build`

Top-level build orchestrator. Chains `go → ui → image → push` based on config.

```
wfctl build [flags]
wfctl build <subcommand> [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `workflow.yaml` | Config file path |
| `--dry-run` | false | Print planned actions without executing |
| `--only <name>` | — | Build only targets matching this name (comma-separated) |
| `--skip <name>` | — | Skip targets matching this name |
| `--tag <tag>` | — | Override image tag for all container targets |
| `--format <fmt>` | `table` | Output format: `table`, `json`, `yaml` |
| `--no-push` | false | Build images but do not push |
| `--env <name>` | — | Apply environment overrides from `environments.<name>.build` |
| `--security-audit` | — | Run security linting; exit 1 on critical findings |

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `wfctl build go` | Build all `type: go` targets |
| `wfctl build ui` | Build all `type: nodejs` targets |
| `wfctl build image` | Build all container images |
| `wfctl build push` | Push built images to declared registries |
| `wfctl build custom` | Run all `type: custom` targets |

### Env var

| Variable | Effect |
|----------|--------|
| `WFCTL_BUILD_DRY_RUN=1` | Equivalent to `--dry-run` |

---

## `wfctl build go`

```
wfctl build go [--config <file>] [--target <name>]
```

| Flag | Description |
|------|-------------|
| `--config` | Config file (default: `workflow.yaml`) |
| `--target` | Build only the named go target |

---

## `wfctl build ui`

```
wfctl build ui [--config <file>] [--target <name>]
```

Wraps the `nodejs` builder plugin.

---

## `wfctl build image`

```
wfctl build image [--config <file>] [--dry-run] [--tag <tag>]
```

Builds all `ci.build.containers[]` entries. External containers are resolved (not built).

---

## `wfctl build push`

```
wfctl build push [--config <file>]
```

Pushes each container's image to every registry listed in `push_to[]`.

---

## `wfctl build custom`

```
wfctl build custom [--config <file>] [--target <name>]
```

Wraps the `custom` builder plugin.

---

## `wfctl registry`

Container registry commands. (Plugin catalog commands moved to `wfctl plugin-registry`.)

### `wfctl registry login`

```
wfctl registry login [--config <file>] [--registry <name>] [--dry-run]
```

Logs into all declared registries (or the named one). DO provider uses `doctl registry login`; GitHub provider uses `docker login ghcr.io`.

### `wfctl registry push`

```
wfctl registry push [--config <file>] [--image <ref>]
```

Pushes image refs to all declared registries.

### `wfctl registry prune`

```
wfctl registry prune [--config <file>] [--registry <name>] [--dry-run]
```

Runs garbage collection and prunes tags beyond `retention.keep_latest`. Preserves `latest`.

---

## `wfctl plugin install` (updated)

```
wfctl plugin install [flags] [<name>[@<version>]]
```

### New flag: `--from-config`

```
wfctl plugin install --from-config <workflow.yaml> [--plugin-dir <dir>]
```

Reads `requires.plugins[]` from the workflow config and installs each plugin that is not already installed. Skips already-installed plugins.

Auth blocks are supported for private repos:

```yaml
requires:
  plugins:
    - name: workflow-plugin-payments
      source: github.com/MyOrg/workflow-plugin-payments
      auth:
        env: RELEASES_TOKEN
```

---

## `wfctl ci init` (updated)

```
wfctl ci init [--config <file>] [--platform github-actions|gitlab-ci] [--output <path>]
```

For GitHub Actions, now emits **three files**:

| File | Description |
|------|-------------|
| `.github/workflows/ci.yml` | Build + test pipeline (unchanged) |
| `.github/workflows/deploy.yml` | Minimal deploy pipeline (new in v0.14.0) |
| `.github/workflows/registry-retention.yml` | Prune cron (emitted only if registries have `retention.schedule`) |

---

## `wfctl plugin-registry`

Renamed from `wfctl registry` (v0.13.0 and earlier). The old `wfctl registry` subcommand now refers to container registries. The plugin catalog is at `wfctl plugin-registry`.

```
wfctl plugin-registry list
wfctl plugin-registry search <query>
wfctl plugin-registry info <name>
```

A deprecation alias `wfctl registry` (routing to `wfctl plugin-registry`) is kept until v1.0.

---

*See also:* [Tutorial](../../tutorials/build-deploy-pipeline.md) · [07 — Security Hardening](./07-security-hardening.md)
