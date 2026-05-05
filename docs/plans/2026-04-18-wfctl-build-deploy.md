---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: ee2f925
  - repo: workflow
    commit: 90257d9
  - repo: workflow
    commit: 48440c7
  - repo: workflow-dnd
    commit: 02c4815e
external_refs:
  - "core-dump: infra.yaml ci.registries evidence"
  - "buymywishlist: app.yaml/infra.yaml ci.registries evidence"
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "func runBuild|wfctl build|func runRegistry|CIRegistry|CIBuildSecurity|GenerateSBOM" cmd config plugins docs -S'
    - 'rg -n "wfctl build|wfctl registry|ci.registries|registries:" /Users/jon/workspace/core-dump /Users/jon/workspace/workflow-dnd /Users/jon/workspace/buymywishlist -S'
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Build + Deploy Orchestration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship `workflow v0.14.0` with `wfctl build`, `wfctl registry` (container), enhanced `wfctl plugin install`, hardened defaults, builder plugin contract, and tutorial + manual — so downstream deploy.yml files drop from 73–155 lines to ~25–50 lines.

**Architecture:** Extend `config/ci_config.go` schema (new fields on CIBuildContainer, new `CIRegistry` + `CIBuildSecurity` types, rename Binaries→Targets with `type:` dispatch). Add `Builder` interface + built-in `go` and `nodejs` and `custom` builders in `plugins/builder-*`. Add `wfctl build` command family under `cmd/wfctl/build*.go` and `wfctl registry` container commands under `cmd/wfctl/registry_container*.go` (existing plugin-catalog command renames to `wfctl plugin-registry`). Update `wfctl ci init` emitter. Write tutorial + 9-page manual.

**Tech Stack:** Go 1.26, `github.com/google/ko` (optional builder plugin), `github.com/anchore/syft` (SBOM), BuildKit secrets for private Go modules, `gopkg.in/yaml.v3` (config), `modular` (engine).

**Design doc:** `docs/plans/2026-04-18-wfctl-build-deploy-design.md` (17 sections, approved 2026-04-18)

**Scope caveat:** This plan is ~100 tasks across 11 phases. Execute phase-by-phase with review + release between phases. Phases 1–3 block everything else (schema + builder contract + core build command). Phases 4–9 can parallelize in small batches. Phase 10 (docs) can start after Phase 3 API stabilizes.

---

## Phase 1 — Config schema foundations

### Task 1: Baseline — verify existing tests green

```
cd /Users/jon/workspace/workflow/.worktrees/design-wfctl-build
GOWORK=off go test ./config/... -count=1
GOWORK=off go test ./cmd/wfctl/... -count=1
```

Record pass/fail. Commit nothing.

### Task 2: Add `CIRegistry` + `CIRegistryAuth` + `CIRegistryRetention` types

**Files:**
- Create: `config/ci_registry.go`
- Create: `config/ci_registry_test.go`

**Step 1: Failing test** — YAML unmarshal into new types:

```go
package config

import (
    "testing"
    "gopkg.in/yaml.v3"
)

func TestCIRegistry_Unmarshal(t *testing.T) {
    src := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/coredump-registry
      auth:
        env: DIGITALOCEAN_TOKEN
      retention:
        keep_latest: 20
        untagged_ttl: 168h
        schedule: "0 7 * * 0"
`
    var cfg WorkflowConfig
    if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if cfg.CI == nil || len(cfg.CI.Registries) != 1 {
        t.Fatalf("expected 1 registry, got %v", cfg.CI)
    }
    r := cfg.CI.Registries[0]
    if r.Name != "docr" || r.Type != "do" || r.Auth.Env != "DIGITALOCEAN_TOKEN" {
        t.Fatalf("unexpected registry: %+v", r)
    }
    if r.Retention == nil || r.Retention.KeepLatest != 20 {
        t.Fatalf("retention missing: %+v", r)
    }
}
```

**Step 2: Run test — confirm FAIL** (`Registries` does not exist).

**Step 3: Add types** to `config/ci_registry.go`:

```go
package config

type CIRegistry struct {
    Name      string               `json:"name" yaml:"name"`
    Type      string               `json:"type" yaml:"type"`
    Path      string               `json:"path" yaml:"path"`
    Auth      *CIRegistryAuth      `json:"auth,omitempty" yaml:"auth,omitempty"`
    Retention *CIRegistryRetention `json:"retention,omitempty" yaml:"retention,omitempty"`
}

type CIRegistryAuth struct {
    Env        string `json:"env,omitempty" yaml:"env,omitempty"`
    File       string `json:"file,omitempty" yaml:"file,omitempty"`
    AWSProfile string `json:"aws_profile,omitempty" yaml:"aws_profile,omitempty"`
    Vault      *CIRegistryVaultAuth `json:"vault,omitempty" yaml:"vault,omitempty"`
}

type CIRegistryVaultAuth struct {
    Address string `json:"address" yaml:"address"`
    Path    string `json:"path" yaml:"path"`
}

type CIRegistryRetention struct {
    KeepLatest  int    `json:"keep_latest,omitempty" yaml:"keep_latest,omitempty"`
    UntaggedTTL string `json:"untagged_ttl,omitempty" yaml:"untagged_ttl,omitempty"`
    Schedule    string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
}
```

Add `Registries []CIRegistry` field to existing `CIConfig` struct in `config/ci_config.go`.

**Step 4: Run test — PASS.**

**Step 5: Commit.**

```
git add config/ci_registry.go config/ci_registry_test.go config/ci_config.go
git commit -m "config: add CIRegistry + CIRegistryAuth + CIRegistryRetention schema"
```

### Task 3: Extend `CIContainerTarget` with new container-build fields

**Files:**
- Modify: `config/ci_config.go` — find `CIContainerTarget` struct
- Create: `config/ci_container_target_test.go`

Add fields to `CIContainerTarget`: `Method`, `Dockerfile`, `KoPackage`, `KoBaseImage`, `KoBare`, `Platforms`, `BuildArgs`, `Secrets`, `Cache`, `Target`, `Labels`, `ExtraFlags`, `External`, `Source`, `PushTo`.

New sub-types: `CIContainerSecret`, `CIContainerCache`, `CIContainerCacheRef`, `CIExternalSource`.

**Step 1: Test** — unmarshal realistic YAML with all new fields.
**Step 2-4:** Standard TDD — test fails, add fields, test passes.
**Step 5: Commit.**

### Task 4: Rename `Binaries` → `Targets` with `type:` dispatch

**Files:**
- Modify: `config/ci_config.go`
- Create: `config/ci_target_test.go`

Backward-compat consideration: old configs use `binaries:`, new use `targets:`. Support both via custom `UnmarshalYAML`:
- If `binaries:` present, coerce each entry to `{type: go, name, path, config: {legacy fields}}`
- If `targets:` present, use directly
- Emit a deprecation warning log when `binaries:` is encountered

Structure:
```go
type CITarget struct {
    Name   string         `json:"name" yaml:"name"`
    Type   string         `json:"type" yaml:"type"` // go | nodejs | rust | python | custom | ...
    Path   string         `json:"path,omitempty" yaml:"path,omitempty"`
    Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
    Environments map[string]*CITargetOverride `json:"environments,omitempty" yaml:"environments,omitempty"`
}

type CITargetOverride struct {
    Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}
```

Tests: parse old `binaries:` config and parse new `targets:` config; both should populate `cfg.CI.Build.Targets`.

**Commit:** `config: add CITarget type; accept legacy binaries: via compat shim`

### Task 5: Add `CIBuildSecurity` type

**Files:**
- Modify: `config/ci_config.go`
- Create: `config/ci_build_security_test.go`

Add `Security *CIBuildSecurity` to `CIBuildConfig`. Type:

```go
type CIBuildSecurity struct {
    Hardened          bool                    `json:"hardened" yaml:"hardened"`
    SBOM              bool                    `json:"sbom" yaml:"sbom"`
    Provenance        string                  `json:"provenance,omitempty" yaml:"provenance,omitempty"`
    Sign              bool                    `json:"sign,omitempty" yaml:"sign,omitempty"`
    NonRoot           bool                    `json:"non_root" yaml:"non_root"`
    BaseImagePolicy   *CIBaseImagePolicy      `json:"base_image_policy,omitempty" yaml:"base_image_policy,omitempty"`
}

type CIBaseImagePolicy struct {
    AllowPrefixes []string `json:"allow_prefixes,omitempty" yaml:"allow_prefixes,omitempty"`
    DenyPrefixes  []string `json:"deny_prefixes,omitempty" yaml:"deny_prefixes,omitempty"`
}
```

Defaults (in a method `func (s *CIBuildSecurity) ApplyDefaults()`):
- If `Security` is nil → treat as `{Hardened: true, SBOM: true, Provenance: "slsa-3", NonRoot: true}`
- If `Hardened` explicitly false → log warning, honor user's choice

**Commit:** `config: add CIBuildSecurity schema with hardened defaults`

### Task 6: Validation

**Files:**
- Modify: `config/ci_config.go` — extend existing `Validate()`
- Create: `config/ci_validate_build_test.go`

Enforce:
- Each `CIContainerTarget.Method` is one of `"dockerfile"`, `"ko"`, `""` (empty = dockerfile default)
- If `Method == "ko"`, `KoPackage` required
- If `Method == "dockerfile"`, `Dockerfile` required
- Each `CIRegistry.Name` unique; `Type` matches known provider
- Each `PushTo[]` entry references a declared `Registries[].name`
- Each `CITarget.Type` is a known builder (`go`, `nodejs`, `custom`) or installed plugin
- `Retention.KeepLatest` ≥ 1; `UntaggedTTL` parses as duration

Emit descriptive errors with field path context. Test cases for each failure mode.

**Commit:** `config: validate ci.build, ci.registries, ci.build.security`

---

## Phase 2 — Builder plugin contract

### Task 7: Define `Builder` interface

**Files:**
- Create: `plugin/builder/builder.go`
- Create: `plugin/builder/builder_test.go`

Interface:

```go
package builder

type Builder interface {
    Name() string
    Validate(cfg Config) error
    Build(ctx Context, cfg Config, out *Outputs) error
    SecurityLint(cfg Config) []Finding
}

type Config struct {
    TargetName string
    Path       string
    Fields     map[string]any
    Env        map[string]string
    Security   *SecurityConfig
}

type Outputs struct {
    Artifacts []Artifact
}

type Artifact struct {
    Name     string
    Kind     string      // binary | image | bundle | other
    Paths    []string    // local filesystem paths produced
    Metadata map[string]any
}

type Finding struct {
    Severity string   // info | warn | critical
    Message  string
    File     string
    Line     int
}
```

Unit test: mock Builder, verify interface contract.

**Commit:** `plugin/builder: define Builder interface`

### Task 8: Built-in `go` builder

**Files:**
- Create: `plugins/builder-go/plugin.go`
- Create: `plugins/builder-go/go_builder.go`
- Create: `plugins/builder-go/go_builder_test.go`

Responsibilities:
- Parse `config.Fields` into Go-specific config (ldflags, tags, cgo, builder_image, system_packages, link_mode, cgo_cflags, cgo_ldflags, extra_flags, runtime_packages)
- Construct `go build` invocation
- For `cgo: true`, select builder_image (alpine vs bookworm) and inject `CGO_CFLAGS`/`CGO_LDFLAGS`
- Cross-compile per `GOOS`/`GOARCH` (reads from outer `CITarget` fields or config)
- SecurityLint: warn if building with `-ldflags=-X main.secret=`; warn if `cgo: true` without `link_mode`; warn if builder_image is not in a known-safe list

Tests: unit tests mocking exec.Command; verify correct args passed for typical configs.

**Commit:** `plugins/builder-go: implement Go builder plugin`

### Task 9: Built-in `nodejs` builder

**Files:**
- Create: `plugins/builder-nodejs/plugin.go`
- Create: `plugins/builder-nodejs/nodejs_builder.go`
- Create: `plugins/builder-nodejs/nodejs_builder_test.go`

Responsibilities:
- Parse config: `script`, `cwd`, `bundle`, `npm_flags`, `node_version`
- Run `npm ci && npm run <script>` (or yarn/pnpm via `package_manager` field)
- Collect artifacts (default `./dist/`)
- SecurityLint: warn if no `package-lock.json` present; warn if `npm install` used instead of `npm ci`

**Commit:** `plugins/builder-nodejs: implement Node.js builder plugin`

### Task 10: Built-in `custom` builder

**Files:**
- Create: `plugins/builder-custom/plugin.go`
- Create: `plugins/builder-custom/custom_builder.go`
- Create: `plugins/builder-custom/custom_builder_test.go`

Responsibilities:
- Parse config: `command`, `outputs`, `env`, `timeout`
- Run `command` via `sh -c`
- Collect `outputs[]` paths as artifacts
- SecurityLint: always returns `[]Finding{ {Severity: "warn", Message: "custom builder cannot enforce hardening"} }`

**Commit:** `plugins/builder-custom: implement custom builder plugin`

### Task 11: Builder registry (dispatch)

**Files:**
- Create: `plugin/builder/registry.go`
- Create: `plugin/builder/registry_test.go`

Central lookup for dispatching a `CITarget.Type` to a `Builder` implementation. Built-in builders register at init time; third-party plugins register via their main plugin hook.

```go
func Register(b Builder) { ... }
func Get(name string) (Builder, bool) { ... }
func List() []Builder { ... }
```

Tests: register built-ins, look them up, verify unknown returns `false`.

**Commit:** `plugin/builder: add builder registry with init-time registration`

### Task 12: Wire builders into engine factory

**Files:**
- Modify: `engine.go` or `plugins/all/all.go` — register builders

Include `builder-go`, `builder-nodejs`, `builder-custom` in default engine builds so `wfctl` CLI has them available without plugin install.

**Commit:** `engine: register built-in builder plugins`

---

## Phase 3 — `wfctl build` command

### Task 13: `wfctl build` top-level dispatcher

**Files:**
- Create: `cmd/wfctl/build.go`
- Create: `cmd/wfctl/build_test.go`
- Modify: `cmd/wfctl/main.go` — add `"build": runBuild` to command map
- Modify: `cmd/wfctl/wfctl.yaml` — add `build` command entry + `cmd-build` pipeline

Responsibilities:
- Parse flags: `--config`, `--dry-run`, `--only <name>`, `--skip <name>`, `--tag`, `--format json|yaml|table`, `--no-push`, `--env`
- Load config via `config.LoadFromFile` (inherits imports support)
- Dispatch to sub-handlers for each target type
- Aggregate outputs into `Outputs` struct
- Emit structured output based on `--format`

Subcommands: `wfctl build <type>` routes to `runBuildTarget(type, args)` where `type` is `go`, `ui`, `image`, `push`, `custom`. Each is a file under `cmd/wfctl/build_<type>.go`.

TDD approach: start with `wfctl build --dry-run` that prints planned actions without executing. Test asserts the plan output matches expected steps for a fixture config.

**Commit per sub-step** — dispatcher first, then each subcommand.

### Task 14: `wfctl build go` subcommand

**Files:**
- Create: `cmd/wfctl/build_go.go`

Wraps `builder-go`'s `Build()`. Flags: `--target <name>` to build specific target. Test: fixture config with one go target → `wfctl build go --dry-run` prints expected `go build` command.

**Commit:** `wfctl build go: dispatch to Go builder plugin`

### Task 15: `wfctl build ui` subcommand

Same pattern, wraps `builder-nodejs`.

**Commit:** `wfctl build ui: dispatch to Node.js builder plugin`

### Task 16: `wfctl build image` subcommand

**Files:**
- Create: `cmd/wfctl/build_image.go`

Responsibilities:
- For each `CIContainerTarget` in config:
  - If `external: true`, resolve source tag via `tag_from:` chain, skip build
  - If `method: ko` → invoke `ko build` with config
  - If `method: dockerfile` → invoke `docker build` with BuildKit secrets, platforms, build_args, cache
- Populate output with `image:tag` references

Tests: fixture configs for each method.

**Commit:** `wfctl build image: dispatch dockerfile + ko backends + external refs`

### Task 17: `wfctl build push` subcommand

**Files:**
- Create: `cmd/wfctl/build_push.go`

Reads build outputs + `ci.registries[]` + each container's `push_to[]`. For each registry, calls provider plugin to auth (if not already) + pushes image ref. Uses `docker push` for now (BuildKit handles multi-arch inline).

**Commit:** `wfctl build push: push built images to declared registries`

### Task 18: `wfctl build custom` subcommand

Wraps `builder-custom`. Simple.

**Commit:** `wfctl build custom: dispatch shell-out custom builder`

### Task 19: Top-level `wfctl build` chains subcommands

Back to `cmd/wfctl/build.go`. The top-level command reads config, determines which target types are present, and calls each sub-handler in order: `go` → `ui` → `image` → `push` (unless `--no-push`). Honors `--only` / `--skip`. Honors `--env` for environment-specific overrides.

End-to-end test: fixture with go + nodejs + image target → `wfctl build --dry-run` prints all four phases' plans.

**Commit:** `wfctl build: orchestrator chains subcommands per ci.build config`

---

## Phase 4 — `wfctl registry` container commands + plugin-registry rename

### Task 20: Rename existing `wfctl registry` → `wfctl plugin-registry`

**Files:**
- Rename: `cmd/wfctl/registry.go` → `cmd/wfctl/plugin_registry.go`
- Modify: `cmd/wfctl/main.go` — add `"plugin-registry": runPluginRegistry`; keep `"registry"` pointing at an alias handler that logs deprecation + delegates
- Modify: `cmd/wfctl/wfctl.yaml` — add `plugin-registry` entry; keep `registry` with deprecation note in description

Deprecation alias handler:

```go
func runRegistryDeprecated(args []string) error {
    fmt.Fprintln(os.Stderr, "DEPRECATED: `wfctl registry` now refers to container registries. Use `wfctl plugin-registry` for the plugin catalog. This alias will be removed in v1.0.")
    return runPluginRegistry(args)
}
```

Tests: invoke each — both succeed with expected behavior; deprecation warning emitted for `wfctl registry` (plugin path).

**Commit:** `wfctl: rename plugin-catalog registry to plugin-registry; keep alias`

### Task 21: New `wfctl registry` container-command dispatcher

**Files:**
- Create: `cmd/wfctl/registry_container.go`

Routes `wfctl registry login|push|prune|logout` to sub-handlers.

**Commit:** `wfctl registry: new container-registry command dispatcher`

### Task 22: `wfctl registry login` (DO provider first)

**Files:**
- Create: `cmd/wfctl/registry_login.go`
- Create: `plugin/registry/provider.go` — RegistryProvider interface
- Create: `plugins/registry-do/plugin.go` — DO provider

Interface:

```go
type RegistryProvider interface {
    Name() string
    Login(ctx Context, reg config.CIRegistry) error
    Push(ctx Context, reg config.CIRegistry, imageRef string) error
    Prune(ctx Context, reg config.CIRegistry) error
}
```

DO provider wraps `doctl registry login` invocations; uses `CIRegistry.Auth.Env` for token.

Test: fixture with DO registry → `wfctl registry login --dry-run` asserts correct `doctl` invocation.

**Commit:** `wfctl registry login: DO provider implementation`

### Task 23: `wfctl registry login` GH provider

**Files:**
- Create: `plugins/registry-github/plugin.go`

Wraps `docker login ghcr.io`. Auth via `GITHUB_TOKEN` env.

**Commit:** `wfctl registry login: GitHub Container Registry provider`

### Task 24: `wfctl registry push`

Reuses provider `Push()` method. Pushes a single image ref to a specified registry or all declared registries.

**Commit:** `wfctl registry push: push image refs to declared registries`

### Task 25: `wfctl registry prune` (DO)

DO provider implements `Prune()`:
1. Run `doctl registry garbage-collection start --force --include-untagged-manifests`
2. List tags, sort by updated_at, delete tags beyond `retention.keep_latest`
3. Preserve tag `latest`

Test: unit test with mocked doctl, assert correct sequence.

**Commit:** `wfctl registry prune: DO provider garbage collection + tag pruning`

### Task 26: `wfctl registry prune` (GH)

GH provider implements `Prune()`:
1. List package versions via GH API
2. Delete versions beyond `keep_latest`

**Commit:** `wfctl registry prune: GHCR provider version cleanup via GH API`

### Task 27: Additional providers (stubbed)

**Files:**
- Create: `plugins/registry-gitlab/plugin.go` (TODO implementation; registers + returns ErrNotImplemented for now)
- Same for `registry-aws`, `registry-gcp`, `registry-azure`

Stubs register themselves + return `ErrNotImplemented` with a message pointing at the issue tracker. Built-in contract is complete for DO + GH in v0.14.0.

**Commit:** `plugins/registry-*: stub GitLab/AWS/GCP/Azure provider plugins`

---

## Phase 5 — Enhanced `wfctl plugin install`

### Task 28: Batch install from `requires.plugins[]`

**Files:**
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_deps.go`

New flag `--from-config` reads `cfg.Requires.Plugins[]`, iterates, calls existing install logic for each. Skip already-installed.

**Commit:** `wfctl plugin install: --from-config batch install from requires.plugins`

### Task 29: Lockfile integration

`plugin_lockfile.go` already exists. Wire `wfctl plugin install --from-config` to:
1. Load lockfile if present
2. Install each plugin at the lockfile's pinned version
3. If version missing in lockfile, install latest and write to lockfile

Support `wfctl plugin lock` command to regenerate lockfile explicitly.

**Commit:** `wfctl plugin install: honor lockfile; add plugin lock command`

### Task 30: Private repo support — env + git insteadOf

**Files:**
- Modify: `cmd/wfctl/plugin_install.go`
- Create: `cmd/wfctl/plugin_auth.go`

`CIRegistryAuth` pattern reused: plugin declarations in `requires.plugins[]` can specify an auth block:

```yaml
requires:
  plugins:
    - name: workflow-plugin-payments
      source: github.com/GoCodeAlone/workflow-plugin-payments
      auth:
        env: RELEASES_TOKEN
```

Before fetching: write `git config --global url."https://x-access-token:${TOKEN}@github.com/".insteadOf "https://github.com/"`. Also set `GOPRIVATE` env var. Undo the git config at end.

**Commit:** `wfctl plugin install: private repo auth via env + git insteadOf`

### Task 31: Provider-agnostic auth (GitLab support)

Extend the auth block to accept `provider:` field:

```yaml
requires:
  plugins:
    - name: some-gitlab-plugin
      source: gitlab.com/example/plugin
      auth:
        provider: gitlab
        env: GITLAB_TOKEN
```

Provider plugins (github, gitlab) handle the specific URL rewriting. GitHub rewrites with `x-access-token`; GitLab rewrites with `oauth2`.

**Commit:** `wfctl plugin install: provider-agnostic auth supporting GitLab`

---

## Phase 6 — Hardened defaults + security audit

### Task 32: SBOM generation (syft integration)

**Files:**
- Create: `cmd/wfctl/build_sbom.go`

When `CIBuildSecurity.SBOM == true` (default), after each image build:
1. Invoke `syft <image-ref> -o cyclonedx-json > sbom.json`
2. Attach SBOM as OCI artifact via `oras attach` OR via `cosign attach sbom` if cosign available

Use `github.com/anchore/syft/syft` library for in-process generation (faster than shelling out). Alternative: shell out to `syft` binary if it's on PATH, fallback to library.

**Commit:** `wfctl build: generate + attach SBOM per image (syft)`

### Task 33: Provenance (BuildKit attestation)

For `method: dockerfile` builds: pass `--attest=type=provenance,mode=max` to `docker buildx build`. For `method: ko`: ko supports `--provenance` flag natively.

**Commit:** `wfctl build: emit BuildKit provenance attestation per image`

### Task 34: `wfctl build --security-audit`

**Files:**
- Create: `cmd/wfctl/build_security_audit.go`

Standalone subcommand. For each target:
- Call `builder.SecurityLint(cfg)` → aggregate findings
- For dockerfile targets: read Dockerfile, lint for:
  - `USER root` or missing `USER` → critical
  - `FROM <base>:latest` without pinning → warn
  - `ADD` URLs from untrusted sources → warn
  - Commands embedding secrets → critical
- Base image policy check: `FROM <prefix>` must be in `allow_prefixes` if set

Exit code: 0 if no critical, 1 if critical.

**Commit:** `wfctl build --security-audit: Dockerfile + base image + builder linting`

### Task 35: Apply hardened defaults when `Security` config is nil

**Files:**
- Modify: `config/ci_config.go` — `ApplyDefaults()` called at load time

After `config.LoadFromFile`, call `cfg.CI.Build.Security.ApplyDefaults()`. Sets `Hardened: true`, `SBOM: true`, `Provenance: "slsa-3"`, `NonRoot: true` if nil.

**Commit:** `config: apply CIBuildSecurity defaults at load time`

### Task 36: Warn on opt-out

In `Validate()`, if `Security.Hardened == false`, log `Warning: hardened defaults disabled — images may not meet supply-chain baseline`.

**Commit:** `config: warn on security.hardened: false opt-out`

---

## Phase 7 — Local dev symmetry

### Task 37: `environments.local` build override merge

**Files:**
- Modify: `cmd/wfctl/dev.go`
- Create: `cmd/wfctl/dev_build.go`

`wfctl dev up` calls `wfctl build --env local`. The env-override resolution logic already works (v0.13.0). Local's `environments.local.build` block overrides `ci.build` values same as module overrides.

Test: fixture with top-level `ci.build` + `environments.local.build` overrides → resolved config for env=local has merged values.

**Commit:** `wfctl dev up: honor environments.local.build overrides`

### Task 38: Local skips hardening

Add logic: if `env == "local"` and `Security` is nil, default to `Hardened: false, SBOM: false` (don't force distroless on dev iteration speed).

**Commit:** `wfctl dev up: local env defaults security.hardened=false for fast iteration`

### Task 39: Local build cache

`environments.local.build.containers[].cache: {from: [{type: local}]}` — use Docker's local layer cache for fast iteration.

**Commit:** `wfctl dev up: local cache mode for containers`

---

## Phase 8 — External image support

### Task 40: `external: true` in container config

**Files:**
- Modify: `cmd/wfctl/build_image.go`

When a `CIContainerTarget.External == true`:
- Skip docker build
- Resolve tag via `Source.TagFrom` chain (env → command)
- Emit artifact with `{registry.path}/{source.path}:{resolved-tag}`

Test: fixture with one external + one built container → verify external tag comes from env var, built image goes through normal flow.

**Commit:** `wfctl build image: support external: true for pre-built images`

### Task 41: `tag_from:` resolution chain

**Files:**
- Create: `cmd/wfctl/build_resolve_tag.go`

Generic tag resolver used by both external containers AND built containers:

```go
func ResolveTag(tagFrom []TagFromEntry, fallback string) string {
    for _, e := range tagFrom {
        if e.Env != "" {
            if v := os.Getenv(e.Env); v != "" { return v }
        }
        if e.Command != "" {
            out, err := exec.Command("sh", "-c", e.Command).Output()
            if err == nil && len(out) > 0 { return strings.TrimSpace(string(out)) }
        }
    }
    return fallback
}
```

**Commit:** `wfctl build: tag resolution chain (env + command fallback)`

---

## Phase 9 — `wfctl ci init` emitter update

### Task 42: Update emitter to produce minimal deploy.yml

**Files:**
- Modify: `cmd/wfctl/ci_init.go`

Replace the current job-generation logic with the target shape from design Section 5. Key bits:
- `build-image` job uses `wfctl build --push --format json` (one step)
- `deploy-*` jobs use `wfctl ci run --phase deploy --env <name>` (one step)
- `concurrency:` block
- `workflow_run:` trigger only (not push directly)
- Correct `github.event.workflow_run.head_sha` SHA pinning for checkouts

Test: fixture `ci.deploy.environments + ci.build + ci.registries` → generated YAML matches golden file.

**Commit:** `wfctl ci init: emit minimal deploy.yml (~45 lines) using wfctl build`

### Task 43: Generate retention.yml

**Files:**
- Modify: `cmd/wfctl/ci_init.go`

When `ci.registries[]` contains entries with `retention:` blocks, also emit `.github/workflows/registry-retention.yml` that wraps `wfctl registry prune` on schedule.

Test: fixture with retention → output contains retention.yml.

**Commit:** `wfctl ci init: emit registry-retention.yml for configured retention`

---

## Phase 10 — Documentation

### Task 44: Tutorial skeleton

**Files:**
- Create: `docs/tutorials/build-deploy-pipeline.md`

16 sections per design Section 17. Start with skeleton: all section headings + one-sentence summaries + TODO placeholders.

**Commit:** `docs: scaffold build-deploy tutorial`

### Task 45-59: Tutorial sections, one per task

Each task adds one tutorial section with:
- Goal
- Prerequisites  
- Step-by-step YAML snippet
- Actual `wfctl` commands
- Expected output (copy-pasted from real runs)
- Common pitfalls

Sections (one Task each — 44 through 59):
- Hello world Go + DO App Platform
- Add a UI target
- Multi-env (staging + prod)
- Polyglot (Go + Rust + Python)
- CGO with embedded SQLite
- Custom Dockerfile
- External image
- Multi-registry DOCR + GHCR
- Retention policy
- Local dev with wfctl dev up
- Custom health checks (gRPC, TCP, composite)
- Deploy hooks
- Non-software builds (Hugo)
- Alternative deploy target (Kubernetes)
- Signing + attestation
- Debugging a failed deploy

**Commit per section.**

### Task 60: Manual page `01-ci-build-schema.md`

**Files:**
- Create: `docs/manual/build-deploy/01-ci-build-schema.md`

Complete reference for every field in `ci.build`. Copy schema from Go struct, document each field's meaning, valid values, defaults, examples.

**Commit:** `docs/manual: ci.build schema reference`

### Tasks 61-68: Manual pages 2–9

One task per manual page (per design Section 17):
- 02-ci-registries-schema.md
- 03-ci-deploy-environments.md
- 04-builder-plugins.md (authoring contract + built-in configs)
- 05-cli-reference.md (every command + flag)
- 06-auth-providers.md
- 07-security-hardening.md
- 08-local-dev.md
- 09-troubleshooting.md

Cross-link tutorial ↔ manual.

**Commit per page.**

### Task 69: Tutorial + manual cross-links

Final pass: ensure every tutorial example links to the relevant manual section for deeper detail, every manual page links back to tutorial examples that use the feature.

**Commit:** `docs: cross-link tutorial and manual`

---

## Phase 11 — Release v0.14.0

### Task 70: CHANGELOG

**Files:**
- Modify: `CHANGELOG.md`

New section `## [0.14.0] - 2026-MM-DD` documenting:
- **Added**: wfctl build, wfctl registry (containers), builder plugin contract, ci.build schema, ci.registries schema, ci.build.security, CITarget with type: dispatch, SBOM + provenance defaults, local dev symmetry, external image support, tutorial, 9-page manual
- **Changed**: `wfctl registry` now refers to container registries. Plugin catalog moved to `wfctl plugin-registry`. Deprecation alias kept; removal in v1.0.
- **Notes**: Hardened defaults on by default. Opt out with `security.hardened: false`. See `docs/manual/build-deploy/07-security-hardening.md`.

**Commit:** `chore: v0.14.0 CHANGELOG`

### Task 71: Full test suite green

```
GOWORK=off go test -race -count=1 ./...
gofmt -l $(find . -name '*.go' -not -path './vendor/*' -not -path './.worktrees/*')
golangci-lint run
```

All green. Any failures → fix before tag.

**Commit:** `fix: any last-mile lint/test fixes for v0.14.0`

### Task 72: Tag + release

```
git tag v0.14.0
git push origin main v0.14.0
```

Release workflow takes over. Verify binaries published + proxy indexed before closing the plan.

---

## Out-of-scope (tracked separately)

- Rust, Python, JVM, cmake builder plugins (external — shipped as `workflow-plugin-builder-*`)
- Kubernetes / ECS / Nomad / Cloud Run deploy targets (external — provider plugins)
- GitOps integration (ArgoCD/Flux)
- Multi-cloud simultaneous deploys
- Built-in notification providers (Slack/PagerDuty)

## Phase 12 — Consumer migration (follow-up plans)

After v0.14.0 ships:
- `docs/plans/2026-MM-DD-bmw-simplification.md` — one PR on buymywishlist
- `docs/plans/2026-MM-DD-dnd-simplification.md` — one PR on workflow-dnd (includes GoReleaser trim)
- `docs/plans/2026-MM-DD-core-dump-simplification.md` — one PR on core-dump (includes GoReleaser trim)

These are separate plans; each consumer gets dedicated execution cycle + review.

---

## Verification checklist before requesting merge on v0.14.0

- [ ] All 72 tasks' commits land cleanly on `design/wfctl-build-deploy` branch (renamed to `feat/v0.14.0-build-deploy` before PR)
- [ ] `go test -race -count=1 ./...` green (excluding any pre-existing documented failures)
- [ ] `golangci-lint run` clean
- [ ] `gofmt -l` empty
- [ ] Generated deploy.yml for a fixture matches golden file
- [ ] `wfctl build --dry-run` against every tutorial fixture succeeds
- [ ] Tutorial commands run end-to-end against a sandbox DO account (manual verification)
- [ ] SBOM generates + attaches for one test image
- [ ] `wfctl registry prune --dry-run` against DO + GH shows expected tags-to-delete

## Skills referenced

- @superpowers:test-driven-development — every schema/builder/command task follows failing-test-first
- @superpowers:verification-before-completion — verification checklist gates the PR
- @superpowers:using-git-worktrees — worktree already exists at `.worktrees/design-wfctl-build`
