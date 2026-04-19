# Builder Plugins

Reference for the `Builder` interface contract and all built-in builder plugins.

---

## Builder interface

All builders implement `plugin/builder.Builder`:

```go
type Builder interface {
    Name() string
    Validate(cfg Config) error
    Build(ctx context.Context, cfg Config, out *Outputs) error
    SecurityLint(cfg Config) []Finding
}
```

| Method | Description |
|--------|-------------|
| `Name()` | Returns the `type:` value used in `ci.build.targets[].type` |
| `Validate(cfg)` | Returns an error if the config is missing required fields |
| `Build(ctx, cfg, out)` | Executes the build; appends to `out.Artifacts` |
| `SecurityLint(cfg)` | Returns security findings (non-fatal unless severity=critical) |

### `Config`

```go
type Config struct {
    TargetName string
    Path       string
    Fields     map[string]any   // builder-specific keys
    Env        map[string]string
    Security   *SecurityConfig
}
```

### `Artifact`

```go
type Artifact struct {
    Name     string
    Kind     string         // binary | image | bundle | other
    Paths    []string
    Metadata map[string]any
}
```

### `Finding` severity

| Severity | Meaning | Exit code impact |
|----------|---------|-----------------|
| `info` | Informational | None |
| `warn` | Deviation from best practice | None |
| `critical` | Security violation | Exit 1 (`--security-audit`) |

---

## Built-in: `go` builder

**Type**: `type: go`

Invokes `go build` with optional CGO support, cross-compilation, and ldflags injection.

### Dry-run

Set `WFCTL_BUILD_DRY_RUN=1` to print the planned `go build` invocation without executing.

### SecurityLint findings

| Finding | Trigger |
|---------|---------|
| `warn` | `ldflags` embeds a secret keyword (secret/token/password/key) via `-X` |
| `warn` | `cgo: true` without `link_mode` |
| `warn` | `builder_image` not in known-safe list |

---

## Built-in: `nodejs` builder

**Type**: `type: nodejs`

Runs `npm ci && npm run <script>` (or yarn/pnpm via `package_manager`). Collects `./dist/` as artifact.

### SecurityLint findings

| Finding | Trigger |
|---------|---------|
| `warn` | No `package-lock.json` present |
| `warn` | `npm install` used instead of `npm ci` |

---

## Built-in: `custom` builder

**Type**: `type: custom`

Runs an arbitrary shell command via `sh -c`. Always emits one SecurityLint finding.

### SecurityLint findings

| Finding | Trigger |
|---------|---------|
| `warn` | Always — `custom builder cannot enforce hardening` |

This is expected and non-blocking. Use `type: custom` for Rust, Python, Hugo, etc. until a dedicated plugin is installed.

---

## Writing a builder plugin

1. Create a Go module with a package that imports `github.com/GoCodeAlone/workflow/plugin/builder`.
2. Implement `builder.Builder`.
3. Register in `init()`: `builder.Register(New())`.
4. Distribute as a wfctl plugin (see [Plugin Authoring Guide](../../PLUGIN_AUTHORING.md)).

### Minimal example

```go
package mybuilder

import (
    "context"
    "fmt"
    "github.com/GoCodeAlone/workflow/plugin/builder"
)

func init() { builder.Register(&MyBuilder{}) }

type MyBuilder struct{}

func (b *MyBuilder) Name() string { return "mytype" }

func (b *MyBuilder) Validate(cfg builder.Config) error {
    if cfg.Path == "" {
        return fmt.Errorf("mytype builder: path is required")
    }
    return nil
}

func (b *MyBuilder) Build(ctx context.Context, cfg builder.Config, out *builder.Outputs) error {
    // ... build logic ...
    out.Artifacts = append(out.Artifacts, builder.Artifact{
        Name: cfg.TargetName, Kind: "binary", Paths: []string{"./dist/myapp"},
    })
    return nil
}

func (b *MyBuilder) SecurityLint(cfg builder.Config) []builder.Finding {
    return nil
}
```

---

*See also:* [01 — ci.build schema](./01-ci-build-schema.md) · [Tutorial §4 — Polyglot](../../tutorials/build-deploy-pipeline.md#4-polyglot-go--rust--python)
