---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: fc4c3e7
  - repo: workflow
    commit: 2942255
  - repo: workflow
    commit: 856a7b9
  - repo: workflow-dnd
    commit: d2273b3d
  - repo: buymywishlist
    commit: f5f7e0c
external_refs:
  - "workflow:docs/tutorials/deploy-pipeline.md"
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "ResolveForEnv|--env|LoadFromFile|deploy-pipeline" config cmd docs -S'
    - 'rg -n "wfctl infra|wfctl ci run|infra.yaml|environments:|deploy-staging|deploy-prod" /Users/jon/workspace/workflow-dnd /Users/jon/workspace/buymywishlist -S'
  result: pass
supersedes: []
superseded_by: []
---

# Deploy Pipeline Multi-Env — Design

**Status:** Approved
**Date:** 2026-04-17
**Scope:** `workflow`, `workflow-dnd`, `buymywishlist`

## Problem

Two gaps revealed by comparing `workflow-dnd` and `buymywishlist` deploy pipelines:

1. **`wfctl infra` silently ignores multi-env config.** `config.WorkflowConfig.Environments` (top-level) and `InfraResourceConfig.Environments` (per-resource) are defined in the types but never consumed by the infra commands. DND built its pipeline without knowing the feature existed and wrote a single `infra/staging.yaml` with no prod path. BMW has a separate `infra.yaml` (prod only) behind a manual `workflow_dispatch` gate.
2. **`wfctl infra` does not honor `imports:`.** Unlike every other `wfctl` subcommand, the infra commands use `os.ReadFile` + `yaml.Unmarshal` directly instead of `config.LoadFromFile`. So `imports:` work everywhere except infra. Users who try to share common modules across env configs will be silently ignored.

BMW is also missing a staging environment, an auto-deploy path on merge, and uses the older `platform.do_*` module types instead of the modern `infra.*` types.

## Goal

One coherent deploy pattern that:

- Uses a single `infra.yaml` with `environments: { staging, prod }` — no per-env files.
- Supports `imports:` so shared pieces can live in a common file.
- Ships via `wfctl ci init`-generated GitHub Actions: `build-image → deploy-staging → healthCheck → deploy-prod` with auto-promotion.
- Is documented in a tutorial so the next project doesn't repeat DND's mistake.

## Non-goals

- Full IaC drift detection UX overhaul.
- Multi-region or multi-cloud promotion.
- Richer smoke-test framework — `CIHealthCheck` (path + timeout) is sufficient for v1.
- Approval-gated prod — auto-promote is the chosen model. `requireApproval: true` remains available for projects that want it.

## Design

### Deliverable 1 — Engine: complete the multi-env work in `wfctl infra`

Single coordinated change in `workflow/`. All edits in `cmd/wfctl/infra*.go` plus a new env-resolution helper in the `config/` package.

**1a. Switch infra commands to `config.LoadFromFile`.** Replace raw `os.ReadFile` + `yaml.Unmarshal` with the canonical loader. This pulls in `imports:` support for free. Affects `discoverInfraModules`, `resolveInfraConfig`, and every plan/apply/bootstrap/destroy/status/drift/import handler.

**1b. Add `--env <name>` flag** to `wfctl infra plan|apply|bootstrap|destroy|status|drift|import`.

**1c. Per-resource env resolution.** New function in `config/`:

```go
// ResolveForEnv returns the effective strategy/provider/config/image/port for a
// resource in the named environment, merging resource.Environments[env] over
// the top-level fields. Returns (nil, false) if the resource is not present in
// that env (lets prod-only resources exist).
func (r *InfraResourceConfig) ResolveForEnv(envName string) (*ResolvedResource, bool)
```

Resources where `environments[env]` is absent use the top-level values (backward compatible with today's configs). Resources where `environments[env]` is explicitly set to `null` are skipped — that's how prod-only DNS works.

**1d. Top-level `environments[env]` application.**
- Merge `environments[env].envVars` into the rendered container env for any provisioned service.
- Default `region` / `provider` from `environments[env]` when a resource omits its own.
- Pass `environments[env].secretsStoreOverride` into `injectSecrets(ctx, cfg, envName)` — the function already accepts `envName`, it's just currently only called from `ci run`.

**1e. `requireApproval` — no engine change needed.** `wfctl ci init` already emits `environment: <name>` in generated GHA when `ci.deploy.environments[n].requireApproval == true`. GH's native environment-approval UI handles the gate.

**1f. Tests.**
- Unit tests for `ResolveForEnv` covering: env absent → uses top-level; env present → merges; env null → skipped.
- E2E test with a fixture config containing staging (small sizing) + prod (large sizing): `wfctl infra plan --env staging` returns staging shape, `--env prod` returns prod shape.
- E2E test with `imports: ["shared.yaml"]` — confirms infra commands resolve imports.

Ship as `workflow v0.11.0`. No breaking changes — `--env` is new, existing configs without `environments:` blocks continue to work.

### Deliverable 2 — Tutorial at `workflow/docs/tutorials/deploy-pipeline.md`

How-to, copy-paste-ready, against v0.11.0. Table of contents:

1. Minimal single-env infra.yaml
2. Second env via `environments:` + per-resource overrides
3. Sharing across configs with `imports:`
4. `ci.deploy.environments` + `healthCheck` for each env
5. Generate GH Actions: `wfctl ci init --platform github-actions`
6. Customize generated workflow: add build/push between build-test and deploy-staging
7. Auto-promote: prod job `needs: [deploy-staging]` — staging health check acts as the gate
8. Optional: `requireApproval: true` + GH Environments for manual prod gates
9. `wfctl infra bootstrap` + `secrets: provider: github` + `generate:` for zero-secret-prep
10. Troubleshooting: drift, state lock, GHCR↔DOCR cutover, missing env flag

Cross-links with `docs/DEPLOYMENT_GUIDE.md` (that guide is about running the engine; this one is about shipping *your* app through the engine). Add a reciprocal link from `DEPLOYMENT_GUIDE.md`.

### Deliverable 3 — Retrofit `workflow-dnd` to the new pattern

- Delete `infra/staging.yaml`, create single `infra.yaml` at repo root.
- `environments: { staging: {...} }` initially. Prod can land in a follow-up — not blocking.
- `ci.deploy.environments.staging.healthCheck: { path: /health, timeout: 30s }`.
- Regenerate `.github/workflows/deploy.yml` via `wfctl ci init`.
- Re-add the docker build/push step between generated `build-test` and `deploy-staging` — match the existing deploy.yml's current DOCR publish flow.
- Keep existing secrets: `DIGITALOCEAN_TOKEN`, `GH_MANAGEMENT_TOKEN`.

This acts as dogfood validation for the tutorial. If the tutorial's instructions don't map cleanly onto a real retrofit, fix the tutorial before BMW starts.

### Deliverable 4 — BMW: full parity

**Config migration.** Replace `infra.yaml`'s `platform.do_*` modules with `infra.*`:
- `platform.do_database` → `infra.database`
- `platform.do_networking` (vpc+fw) → `infra.vpc` + `infra.firewall`
- `platform.do_app` → `infra.container_service`
- `platform.do_dns` → `infra.dns`
- New: `infra.registry` for DOCR

Single file with `environments: { staging, prod }`. Per-resource `environments:` overrides:
- `bmw-database`: staging `db-s-1vcpu-1gb` (~$15/mo), prod matches current sizing.
- `bmw-container`: staging `instance_count: 1`, prod current sizing.
- `bmw-dns`: present only under `environments.prod` (staging uses DO default `*.ondigitalocean.app`).
- `bmw-container.environments.*.dockerImage`: `registry.digitalocean.com/bmw-registry/buymywishlist:${GITHUB_SHA}`.

**Secrets.** Add `secrets:` block with `provider: github` and `generate:` for `JWT_SECRET` (random_hex 32) and `SPACES` (provider_credential, DO Spaces). Pre-set secrets that can't be generated (`STRIPE_SECRET_KEY`, `RELEASES_TOKEN`) stay as GH repo secrets.

**CI.** `ci.deploy.environments`:
- `staging`: `provider: digitalocean`, `healthCheck: { path: /health, timeout: 30s }`, `requireApproval: false`.
- `prod`: same shape, `requireApproval: false` (auto-promote).

**GitHub Actions.** Generate `.github/workflows/deploy.yml` via `wfctl ci init`. Customize:
- Cross-compile `workflow-server` (from `/Users/jon/workspace/workflow`) + `bmw-plugin` (local `cmd/bmw-plugin`) for `linux/amd64` with `CGO_ENABLED=0`.
- `doctl registry login` → `docker build -f Dockerfile.prebuilt` → push to DOCR tagged with `${GITHUB_SHA}` and `latest`.
- Job chain: `build-image → deploy-staging → deploy-prod` with `needs:` in between. Both deploy jobs run `wfctl ci run --phase deploy --env <name>`.
- Runners: `ubuntu-latest` for the deploy workflow. `ci.yml` and `release.yml` stay self-hosted.

**Cleanup.**
- Delete `.github/workflows/infra.yml` (the old `workflow_dispatch`-only job).
- Keep GHCR publish as a parallel step in `build-image` for one release cycle after first successful prod deploy, then remove.
- Drop the `-uall` and any lingering `git add -A` patterns in hooks or scripts touched by this migration.

## Staging domain, smoke coverage, GHCR sunset — decisions

- **Staging domain:** DO default `*.ondigitalocean.app`. No DNS resource under `environments.staging`. Prod keeps `buymywishlist.com` via `bmw-dns` under `environments.prod` only.
- **Smoke coverage:** shallow `healthCheck: /health` for v1. Do not extend `CIHealthCheck` yet. Richer smoke (auth + wishlist create/read/delete) follows in a separate plan when the current depth produces a false-negative promotion.
- **GHCR sunset:** two successful prod deploys via DOCR, then remove the GHCR publish step in a follow-up PR. Don't put a date on it — tie it to observed success.

## Dependency graph

```
workflow v0.11.0 (D1)
        │
        ├─► tutorial (D2) ─────┐
        │                      ├─► dogfood via DND (D3) ─► BMW migration (D4)
        └──────────────────────┘
```

D2, D3, D4 all gated on D1 merging. D3 runs before D4 so any tutorial gaps surface in the lower-stakes repo first.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| `ResolveForEnv` regresses single-env configs | Unit tests cover: env absent → top-level passthrough. Existing scenarios run in CI. |
| `config.LoadFromFile` switch breaks infra commands | All existing infra scenarios must pass green before merge. `LoadFromFile` is the default loader in every other `wfctl` subcommand, so the behavior change is "start honoring the same schema as everywhere else". |
| Auto-promote ships a breaking prod regression that `healthCheck /health` doesn't catch | Accepted risk. The fix-forward path is fast (revert commit, redeploy). Richer smoke is a known follow-up. |
| DOCR $5/mo storage fills up | Add retention policy in the DOCR registry resource config; DO auto-prunes old tags when configured. |
| BMW DOCR cutover breaks prod | Keep GHCR publish for one cycle; `infra.yaml` prod `dockerImage:` can be flipped back in one commit. |
| `imports:` in infra changes config discovery semantics | Document in tutorial section 3. Add a CHANGELOG entry noting that infra configs now resolve `imports:` consistently with the rest of wfctl. |

## Out-of-scope follow-ups (tracked but not in this plan)

- Richer smoke framework (multi-step pipeline assertions via `step.http_request` chained).
- `wfctl infra diff --env a --env b` to surface unintentional drift between env resolutions.
- Full prod in DND.
- Approval-gated promotion pattern docs and example.
- GHCR publish removal in BMW (post two clean prod deploys).
- **Native image-registry retention in `wfctl infra`.** Downstream consumers (buymywishlist, workflow-dnd, core-dump) currently run per-repo `.github/workflows/registry-retention.yml` workflows that call `doctl registry garbage-collection start` + tag-pruning bash + `actions/delete-package-versions@v5` for GHCR. This logic is duplicated across every consumer repo and re-implemented per provider. Engine work: add retention fields to `infra.registry` module schema (`retention_policy: { keep_latest: 20, untagged_ttl: 168h, schedule: "0 7 * * 0" }`), wire `wfctl infra gc` or a scheduled `step.registry_gc` that calls the provider-native GC endpoint (DO, ECR, GCR, ACR) plus tag pruning based on `keep_latest`. Downstream consumers then drop their retention workflow and declare retention in `infra.yaml`. DO's `doctl registry garbage-collection start --force --include-untagged-manifests` maps cleanly; ECR has lifecycle policies (JSON); GCR has retention policies (JSON). Schema should be provider-agnostic and delegate to plugin.
