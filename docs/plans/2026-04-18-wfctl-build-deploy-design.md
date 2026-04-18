# wfctl Build + Deploy Orchestration — Design

**Status:** Approved
**Date:** 2026-04-18
**Scope:** `workflow` (wfctl + engine), downstream consumers (BMW, DND, core-dump, future)

## Problem

The deploy pipelines shipped for BMW, workflow-dnd, and core-dump each run 73–155 lines of GitHub Actions YAML. Most of that is generic build/push/auth plumbing repeated verbatim across repos:

- `doctl registry create` / `login`
- `docker build` with `--secret`, multi-registry `-t` flags
- `docker push` to DOCR + GHCR + tag chains
- `git config url.insteadOf` for private Go modules
- `actions/setup-node` + `npm ci && npm run build`
- `curl` of plugin release artifacts with hardcoded versions
- `actions/delete-package-versions` + custom `doctl` tag-pruning scripts for retention

Every repo maintains its own copy. Version bumps, security patches, and provider changes mean N-way updates. A new consumer starts with a 150-line template instead of a declarative config.

**wfctl already owns half of this** (`wfctl infra`, `wfctl ci run`, `wfctl plugin install`, `wfctl registry` for plugin catalog). The other half belongs in wfctl too, so deploy pipelines compress to "checkout + setup-wfctl + a few wfctl commands" and consumers stop carrying build/registry/retention logic.

## Goal

One declarative `infra.yaml` drives the full deploy pipeline. GH Actions YAML becomes event-shape + trigger wiring only; everything substantive (build, registry auth, push, prune, plugin install, deploy, teardown) happens inside `wfctl` commands the user can also run locally.

**Target reductions:**

| File | Current | Target |
|---|---|---|
| core-dump deploy.yml | 129 | ~45 |
| BMW deploy.yml | 155 | ~50 |
| DND deploy.yml | 73 | ~45 |
| Each retention.yml | ~60 | ~15 |

## Non-goals

- Replacing GoReleaser entirely (it handles Homebrew/Scoop/changelog; keep for where that matters)
- Multi-cloud simultaneous deploys (run two `wfctl ci run` calls)
- Built-in Slack/PagerDuty (use `hooks.post_deploy` shell escape)
- Full GitOps (ArgoCD/Flux) integration — wfctl commits image bumps to a GitOps repo max, not manifest rollout

## Design

### Section 1 — New command family: `wfctl build`

Single top-level orchestrator that chains subcommands based on `ci.build` config; each subcommand also invocable standalone.

- `wfctl build` — runs go + ui + image + push (unless `--no-push`)
- `wfctl build <target-type>` where type is `go`, `ui`, `image`, `push`, `custom` — standalone dispatch
- Flags: `--dry-run`, `--only <name>`, `--skip <name>`, `--tag <override>`, `--format json|yaml|table`

Two container-build backends:
- `method: dockerfile` — wraps existing `step.container_build` (module/pipeline_step_container_build.go)
- `method: ko` — `github.com/google/ko` for Dockerfile-less Go → OCI. Hardened defaults (distroless + non-root + SBOM).

### Section 2 — New command family: `wfctl registry` (container registries)

**Naming decision:** existing plugin-catalog command renames to `wfctl plugin-registry`; container-registry becomes `wfctl registry`. Reflects the mental model most users already have (Docker/Kubernetes "registry" = container registry).

- `wfctl registry login` — auth to all declared registries
- `wfctl registry push <ref>` — push a built image
- `wfctl registry prune` — garbage collection + tag retention; replaces all per-repo `retention.yml` scripts

Provider plugins handle per-registry specifics:
- `type: do` → workflow-plugin-digitalocean (doctl API)
- `type: github` → workflow-plugin-github (GH REST)
- `type: gitlab` → workflow-plugin-gitlab
- `type: aws` → workflow-plugin-aws (ECR)
- `type: gcp` → workflow-plugin-gcp (Artifact Registry)
- `type: azure` → workflow-plugin-azure (ACR)

### Section 3 — Enhanced `wfctl plugin install`

Extends existing command:
- **Batch install** from `requires.plugins[]` (no per-plugin CLI calls)
- **Version source of truth**: `requires.plugins[].version` pinned → wfctl lockfile (`plugin_lockfile.go` exists) → workflow-registry `latest`
- **No hardcoded versions in CI** — the `curl .../releases/download/v0.2.1/...` pattern in BMW's current deploy.yml becomes `wfctl plugin install` that reads the manifest
- **Private repo support via provider plugins** — GitHub, GitLab, etc. No hardcoded env var names; yaml declares the auth source
- **System-wide `git config insteadOf`** — wfctl writes it before `go mod download` / release-asset fetch so private Go modules work uniformly

### Section 4 — Config schema extensions

`CIBuildConfig` in `config/ci_config.go` extends:
- `containers[]` gains `method`, `dockerfile`, `ko_package`, `ko_base_image`, `push_to`, `secrets[]`, `platforms[]`, `build_args{}`, `cache{from, to}`, `target`, `labels{}`, `extra_flags[]`
- `targets[]` (renamed from `binaries[]`) gains `type:` (language dispatch — see Section 16)
- New top-level `ci.registries[]` — declares registry targets with `name`, `type`, `path`, `auth{env|file|aws_profile|vault}`, `retention{keep_latest, untagged_ttl, schedule}`
- New top-level `ci.build.security{}` — `hardened`, `sbom`, `provenance`, `sign`, `non_root`, `base_image_policy`
- New top-level `ci.build.custom[]` — escape hatch for `command:` + `outputs:`

### Section 5 — `wfctl ci init` update

Generator emits ~45-line deploy.yml. Target shape:

```yaml
name: Deploy
permissions: { contents: read, packages: write }
concurrency: { group: deploy-${{ github.ref }}, cancel-in-progress: false }
on:
  workflow_run: { workflows: ["CI"], types: [completed], branches: [main] }
jobs:
  build-image:
    if: github.event.workflow_run.conclusion == 'success'
    runs-on: ubuntu-latest
    outputs: { sha: ${{ steps.build.outputs.sha }} }
    steps:
      - uses: actions/checkout@v4
        with: { ref: ${{ github.event.workflow_run.head_sha || github.sha }} }
      - uses: GoCodeAlone/setup-wfctl@v1
      - id: build
        run: wfctl build --push --format json
        env:
          DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          RELEASES_TOKEN: ${{ secrets.RELEASES_TOKEN }}
  deploy-staging:
    needs: [build-image]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { ref: ${{ github.event.workflow_run.head_sha || github.sha }} }
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env staging
        env: { ... }
  deploy-prod:
    needs: [build-image, deploy-staging]
    environment: prod
    # ... identical shape, --env prod
```

Retention workflow also generated — ~15 lines wrapping `wfctl registry prune`.

### Section 6 — `imports:` support

Already honored by `wfctl infra` (v0.13.0). Confirm `ci.build` / `ci.registries` loading uses the same `config.LoadFromFile` path — inherit automatically.

### Section 7 — GoReleaser audit per consumer

As part of Phase 2 simplification PRs:
- **BMW**: delete `release.yml` + `.goreleaser.yml`. Nothing external consumes the server binary release.
- **DND, core-dump**: trim `.goreleaser.yml` to client/desktop artifacts only (Steam distribution). Server binary target removed (Docker handles it).
- **workflow**: no change (downstream consumers fetch wfctl + workflow-server via `setup-wfctl@v1` + curl).

### Section 8 — Two-phase rollout

**Phase 1 (workflow v0.14.0):**
- Schema additions (ci.build container fields, ci.registries, ci.build.security, targets[] with type:)
- `wfctl build` command family (go, ui, image, push, custom, top-level orchestrator)
- `wfctl registry` container commands (login, push, prune)
- Enhanced `wfctl plugin install` (batch, lockfile, private, git insteadOf)
- Rename existing plugin-catalog `wfctl registry` → `wfctl plugin-registry`
- Updated `wfctl ci init` emits minimal YAML
- Built-in builder plugins: go, nodejs, custom
- Tutorial + reference docs (Section 17)

**Phase 2 (consumer simplification, one PR per repo):**
- BMW, DND, core-dump each get a PR that:
  - Adds `ci.build` + `ci.registries` blocks to infra.yaml
  - Regenerates deploy.yml via `wfctl ci init`
  - Replaces retention.yml with `wfctl registry prune` wrapper
  - For BMW: deletes release.yml + .goreleaser.yml
  - For DND/core-dump: trims .goreleaser.yml to client-only

### Section 9 — Configurability (Unix philosophy)

Core principles:
- **Structured common fields + `extra_flags` escape hatch everywhere** — cover 80% inline, keep 20% flexible
- **Pluggable deploy targets** (DO App Platform, Kubernetes, ECS, Nomad, Cloud Run, SSH+systemd) via provider plugins
- **Pluggable health checks** (http, grpc, tcp, sql, exec, composite with all_of/any_of)
- **Hookable pre/post/on-failure** per deploy phase
- **SHA/tag source configurable** (env priority list, command fallback)
- **Environment-specific build overrides** (same `environments:` merge pattern as modules)
- **Backend selection** (docker, podman, nerdctl, buildkit)
- **Auth pluggable** (env, file, aws_profile, vault — whatever the provider plugin supports)
- **Every subcommand standalone** — `wfctl build go`, `wfctl registry push`, `wfctl ci run --phase X` each work alone
- **Structured output** (`--format json|yaml|table`) for chaining
- **Skip/force flags liberal** — `--skip-push`, `--no-cache`, `--force-rebuild`, `--dry-run`
- **Split config from secrets** — infra.yaml committed, secrets always via provider

### Section 10 — Hardened defaults (security-first)

Defaults matter when most users accept them:

```yaml
ci:
  build:
    security:
      hardened: true         # default
      sbom: true             # default — syft generates, attached as OCI artifact
      provenance: slsa-3     # default — BuildKit attestation
      sign: false            # opt-in (needs cosign key setup)
      non_root: true         # default — enforced for ko, warned for dockerfile
      base_image_policy:
        allow_prefixes: [cgr.dev/chainguard/, gcr.io/distroless/]
```

- SBOM always. ~50KB per image. Supply-chain hygiene is table stakes.
- Provenance always. SLSA-3 via BuildKit.
- Signing opt-in (cosign requires key management — too much friction for default).
- `wfctl build --security-audit` standalone: lints Dockerfile, checks base image CVE tracking, scans for embedded secrets, exits non-zero on critical findings.
- Opting out requires explicit `security.hardened: false` (logs warning).

### Section 11 — Local dev extension (`wfctl dev`)

Existing `wfctl dev` command honors `ci.build` with `environments.local` overrides:

```yaml
environments:
  local:
    provider: docker      # docker | podman — not digitalocean
    exposure:
      method: port_forward   # localhost:8080
    build:
      backend:
        container: docker
      targets:
        - name: server
          config:
            extra_flags: ["-gcflags=all=-N -l"]  # delve-friendly
            cgo: true                             # native dev libs OK
      containers:
        - name: app
          method: dockerfile   # skip ko locally for faster iteration
          cache: { from: [{ type: local }] }
      security:
        hardened: false         # local doesn't need distroless
        sbom: false
```

`wfctl dev up`, `wfctl dev logs --follow`, `wfctl dev exec server sh`, `wfctl dev watch` (fsnotify hot-reload — already exists). No auth, no registry pushes, image stays in local daemon.

**Local ⇔ CI symmetry principle:** same `infra.yaml` drives both. `environments:` overrides pick the shape. No "works in dev, breaks in CI" drift.

### Section 12 — Bring-your-own-image (escape hatches)

Three paths for users who don't want wfctl's build:

**12a. Custom Dockerfile** (already in Section 4 via `method: dockerfile`) — user's Dockerfile verbatim, wfctl handles build + push.

**12b. External pre-built image** — wfctl skips build, deploys directly:
```yaml
ci:
  build:
    containers:
      - name: legacy-service
        external: true
        source:
          registry: docr      # ref into ci.registries[]
          path: legacy-service
          tag_from:
            - env: LEGACY_IMAGE_TAG
            - command: "cat .legacy-version"
```

**12c. Shell-out** — full escape:
```yaml
ci:
  build:
    custom:
      - name: my-weird-thing
        command: "./scripts/build.sh"
        outputs: ["./dist/special.tar"]
```

### Section 13 — Language-agnostic builds (polyglot support)

Rename `binaries[]` → `targets[]` with `type:` dispatch:

```yaml
ci:
  build:
    targets:
      - name: server
        type: go
        config: { ldflags: [...], tags: [release], cgo: false }
      - name: indexer
        type: rust
        config: { profile: release, features: [encryption] }
      - name: lambda
        type: python
        config: { entry: handler.py, bundle: pyinstaller }
      - name: ui
        type: nodejs
        config: { script: build }
      - name: legacy
        type: cmake
        config: { target: release }
      - name: weird
        type: custom
        config: { command: "./build.sh", outputs: ["./dist/weird.bin"] }
```

Built-in builders v0.14.0: `go`, `nodejs`, `custom`.
External plugins: `workflow-plugin-builder-rust`, `-python`, `-jvm`, `-cmake`, etc. Install via `wfctl plugin install builder-rust`.

Each builder plugin implements:
```go
type Builder interface {
    Name() string
    Validate(cfg Config) error
    Build(ctx Context, cfg Config, out Outputs) error
    SecurityLint(cfg Config) []Finding
}
```

### Section 14 — CGO handling inside the Go builder

```yaml
ci:
  build:
    targets:
      - name: server-with-sqlite
        type: go
        config:
          cgo: true
          builder_image: "golang:1.26-bookworm"   # alpine | bookworm | custom
          system_packages:                         # apt/apk during build
            - libsqlite3-dev
          link_mode: static                        # static (musl) | dynamic (glibc)
          runtime_packages:                        # baked into final image for dynamic link
            - libsqlite3
```

Go builder auto-selects:
- `cgo: false` (default) → `golang:1.26-alpine` + `CGO_ENABLED=0` + fully static. Distroless runtime.
- `cgo: true, link_mode: static` → musl-gcc + static link. Distroless runtime still OK.
- `cgo: true, link_mode: dynamic` → bookworm + glibc. Runtime needs bookworm-slim base with runtime_packages.

### Section 15 — Non-software builds

Nothing in the schema says "software". `type: custom` runs anything — Hugo docs, Terraform plans, ML model training, content packs. wfctl orchestrates push + gate + retention the same way. This shifts wfctl from "Go builder" to "artifact builder" — whatever artifact + wherever it ships.

### Section 16 — Updated reduction estimates

| Project shape | Config complexity | deploy.yml lines |
|---|---|---|
| Standard (wfctl builds everything) | Minimum | ~45 |
| Pre-built images (deploy only) | +external: true | ~25 |
| Polyglot (Go + Rust + Python) | +2 builder plugins | ~50 |
| CGO-heavy (SQLite/libvips) | +system_packages | ~50 |
| Bring-your-own-Dockerfile | method: dockerfile | ~45 |
| Local-only dev | n/a | 0 (just `wfctl dev up`) |

### Section 17 — Documentation strategy (tutorial + manual)

Two-track docs under `workflow/docs/`:

**Tutorial:** `docs/tutorials/build-deploy-pipeline.md` — progressive examples, copy-paste-ready, each section builds on previous:

1. Hello world — single Go server → DO App Platform
2. Add a UI (Go + React/Vite) — `type: nodejs` target
3. Multi-env (staging + prod) via `environments:`
4. Polyglot — add Rust worker, Python lambda
5. CGO — embedded SQLite with system packages
6. Bring-your-own Dockerfile
7. Bring-your-own image (`external: true`)
8. Multi-registry (DOCR primary + GHCR mirror)
9. Retention policy per registry
10. Local dev with `wfctl dev up`
11. Custom health checks (gRPC native, TCP, composite)
12. Deploy hooks (pre-migrations, post-cache-warm)
13. Non-software builds (Hugo site)
14. Alternative deploy targets (Kubernetes via plugin)
15. Signing + attestation for production releases
16. Debugging a failed deploy (troubleshooting)

Each section: working YAML snippet + actual `wfctl` command + expected output.

**Reference manual:** `docs/manual/build-deploy/` — full schema + command reference:

- `01-ci-build-schema.md` — every field in `ci.build`
- `02-ci-registries-schema.md` — full `ci.registries` reference
- `03-ci-deploy-environments.md` — `ci.deploy.environments` + healthCheck kinds + hooks
- `04-builder-plugins.md` — authoring contract + each built-in builder's config
- `05-cli-reference.md` — every `wfctl build` / `wfctl registry` / `wfctl plugin install` flag
- `06-auth-providers.md` — per-provider auth patterns (DO, GH, GitLab, AWS, GCP, Azure, Vault)
- `07-security-hardening.md` — defaults, audit flags, signing setup, SBOM format
- `08-local-dev.md` — `wfctl dev` full reference
- `09-troubleshooting.md` — symptoms + diagnoses for common failures

Cross-links: tutorial sections link to manual pages for deeper detail; manual pages link back to tutorial examples.

Phase 1 includes both docs tracks. Phase 2 consumer PRs reference tutorial sections in their own README updates (BMW README: "see `workflow/docs/tutorials/build-deploy-pipeline.md#hello-world`" rather than duplicating the whole thing).

---

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| ko dependency bloats wfctl binary | ko is ~10MB. Ship as optional plugin (`workflow-plugin-builder-go-ko`) if size matters; default builder uses `step.container_build` (docker CLI) |
| Builder plugin contract locks us in | Interface is versioned; backward-compat guarantees on minor bumps; major bumps require plugin ecosystem coordination (already practiced for workflow plugins) |
| `wfctl registry` naming collision with existing plugin-catalog command | Rename during Phase 1 with deprecation notice. `wfctl registry` (plugin catalog) emits deprecation warning → points users to `wfctl plugin-registry`. Removal in v1.0 |
| Private plugin install breaks on GitLab | Provider plugin contract accommodates per-provider differences. Built-in github + gitlab providers cover 95% of users |
| Hardened defaults break existing builds | Opt-out path exists; migration note in CHANGELOG; Phase 2 PRs surface the warning for each consumer |
| SBOM generation slows builds | syft is fast (~seconds); parallelized with build; skip via `security.sbom: false` |
| Local + CI symmetry breaks when environments diverge | `environments.local` overrides documented with guardrails; linter warns if local has significantly different container base than prod |

## Out-of-scope (tracked, not in this plan)

- Full ArgoCD/Flux GitOps integration (wfctl just commits image bumps; rollout stays owned)
- Multi-cloud simultaneous deploys (users chain wfctl calls)
- Built-in notification providers (use `hooks.post_deploy`)
- Replacing GoReleaser for Homebrew/Scoop/changelog generation
- Secret scanning in the `--security-audit` lint (trivy/trufflehog integration is a separate plugin)

## Dependency graph

```
workflow v0.14.0 (D1)
        │
        ├─► Tutorial + manual (D2, parallel)
        │
        └─► Phase 2 consumer simplification PRs
                    ├─► BMW simplification
                    ├─► DND simplification
                    └─► core-dump simplification
```

D1 gates D2 + Phase 2. D2 + Phase 2 can run in parallel after D1 ships.
