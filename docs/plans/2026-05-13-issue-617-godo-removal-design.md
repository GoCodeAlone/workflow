# Issue #617 — Remove godo (DigitalOcean SDK) from Workflow Core

**Status:** Draft for adversarial review
**Owner:** autonomous pipeline (intel352)
**Issue:** [GoCodeAlone/workflow#617](https://github.com/GoCodeAlone/workflow/issues/617)
**Date:** 2026-05-13

## Summary

Workflow core directly imports `github.com/digitalocean/godo` to back six legacy IaC modules (`platform.do_app`, `platform.doks`, `platform.do_dns`, `platform.do_database`, `platform.do_networking`, `cloud.account` DO resolver) and five legacy pipeline steps (`step.do_deploy/status/logs/scale/destroy`). The same surface is already implemented in `workflow-plugin-digitalocean` v0.12.0 as a proper IaC provider plugin (`iac.provider` module type, gRPC, computePlanVersion v2). Dependabot bumps to godo therefore drift core, the wrong owner.

This design proposes a **single-PR force-cutover** that deletes the legacy DO surface from workflow core, removes `godo` from `go.mod`, and emits an actionable migration error when a user config still references the legacy module types. This mirrors the precedent established by the strict-contracts force-cutover (memory: `feedback_force_strict_contracts_no_compat.md`).

AWS SDK usage is **explicitly out of scope** for this issue — `iam/`, `plugin/rbac/aws.go`, `artifact/s3.go`, and IaC drivers under `platform/providers/aws/` are audited as a separate follow-up issue created at the end of this work.

## Goals (acceptance criteria from #617)

1. Workflow core no longer imports `github.com/digitalocean/godo` for IaC/App Platform behavior.
2. Existing DO App Platform behavior remains available through `workflow-plugin-digitalocean`.
3. `wfctl` errors remain actionable when a provider plugin is missing or a legacy DO module type is referenced.
4. Dependabot provider SDK bumps target the provider repo, not workflow core.

## Non-goals

- Removing AWS SDK from core (separate issue created at end of work).
- Replacing the DO plugin's existing functionality.
- Backwards-compatible shim modules — force-cutover, no compat layer.
- Touching `module/iac_state_spaces.go` (S3-compat backend uses `aws-sdk-go-v2`, not godo).
- Migration tooling beyond an actionable load-time error pointing at the migration guide.

## Current state — surface to remove

### Module files (godo importers)

| File | Lines | Purpose |
|------|------|---------|
| `module/platform_do_app.go` | 430 | App Platform module |
| `module/platform_do_app_test.go` | 399 | tests |
| `module/platform_do_database.go` | 263 | Managed Database module |
| `module/platform_do_database_test.go` | 66 | tests |
| `module/platform_do_dns.go` | 357 | DNS module |
| `module/platform_do_dns_test.go` | 270 | tests |
| `module/platform_do_networking.go` | 370 | VPC + firewall module |
| `module/platform_do_networking_test.go` | 264 | tests |
| `module/platform_doks.go` | 329 | DOKS Kubernetes module |
| `module/cloud_account_do.go` | 74 | DO credential resolvers + `doClient()` |
| `module/pipeline_step_do.go` | 220 | 5 DO App Platform pipeline steps |
| **Total** | **~3042** | |

### Registration / schema sites

| File | Edit |
|------|------|
| `plugins/platform/plugin.go` | Drop `platform.do_*` + `platform.doks` from `ModuleTypes`; drop 5 module factories; drop `step.do_*` from `StepTypes`; drop 5 step factories. |
| `plugins/platform/plugin_test.go` | Drop the 5 `step.do_*` + 5 `platform.do_*` assertions. |
| `schema/schema.go` | Drop 5 module-type entries + 5 step-type entries. |
| `schema/module_schema.go` | Drop 5 module schemas + 5 step descriptions. |
| `schema/step_schema_builtins.go` | Drop 5 step schema `Register` calls. |
| `cmd/wfctl/type_registry.go` | Drop 5 module + 5 step type-registry entries. |
| `cmd/wfctl/infra.go:577` | `return t == "infra.container_service"` (drop `|| t == "platform.do_app"`). |
| `cmd/wfctl/deploy_providers.go:419-424` | Drop `"platform.do_app"` from `deployTargetTypes`. |
| `cmd/wfctl/ci_run_dryrun.go:178-183` | Drop `"platform.do_app"` from `deployTargetTypes`. |
| `module/multi_region.go:123` | Rewrite error message to point at `workflow-plugin-digitalocean` + `infra.*` types. |
| `DOCUMENTATION.md` | Replace the 5 module rows + 5 step rows with a paragraph pointing at the DO plugin. |
| `go.mod` / `go.sum` | `go mod tidy` after deletion drops `github.com/digitalocean/godo` + transitive deps. |

### Migration error

Add a thin guard in the engine's module-loader that, when it encounters a config module of type `platform.doks` / `platform.do_app` / `platform.do_dns` / `platform.do_database` / `platform.do_networking` and the type is not registered by any loaded plugin, emits:

```
unsupported legacy module type %q: this type was removed from workflow core in v<NEXT>.
Use workflow-plugin-digitalocean (https://github.com/GoCodeAlone/workflow-plugin-digitalocean)
and migrate to the equivalent infra.* IaC type:
  platform.do_app        → infra.container_service (provider: digitalocean)
  platform.do_database   → infra.database (provider: digitalocean)
  platform.do_dns        → infra.dns (provider: digitalocean)
  platform.do_networking → infra.vpc + infra.firewall (provider: digitalocean)
  platform.doks          → infra.k8s_cluster (provider: digitalocean)
```

Implementation: add `legacyDOTypes` set + lookup in `engine.go BuildFromConfig` (or wherever unknown-module-type currently errors) producing the message above. The lookup is in the **unknown-type fallback path** — when the plugin IS loaded and registers the same names, that path is unreachable. (Plugin v0.12.0 does NOT register legacy names — it registers `iac.provider`. Therefore the error fires whenever a user with the new core + old config tries to load.)

This satisfies acceptance criterion #3.

## Considered approaches

### Option A — Single-PR force-cutover (RECOMMENDED)

Delete in one PR: 11 module files, all registration sites, godo from go.mod, plus migration error + migration doc. Tag a new minor; CHANGELOG calls out breaking change.

**Pros:** Mirrors strict-contracts precedent; no duplication window; clean git history; Dependabot stops touching core immediately.
**Cons:** Any consumer YAML using `platform.do_*` breaks on engine upgrade. Mitigated by actionable error message + migration guide.

### Option B — Phased deprecation (REJECTED)

Mark legacy modules deprecated, gate behind a `LEGACY_DO_MODULES=1` env var, remove godo in a later release.

**Pros:** Soft landing.
**Cons:** Fights force-cutover precedent; perpetuates duplication; Dependabot still nags core during the window; doubles the work (two PRs, deprecation warnings, retest matrix); a "later release" reliably becomes "never."

### Option C — Move-then-delete (REJECTED for DO; matches the AWS audit follow-up)

Audit DO plugin parity, file gap issues against `workflow-plugin-digitalocean`, fix gaps, then delete from core.

**Pros:** Surfaces gaps before consumer surprise.
**Cons:** Premature here — plugin v0.12.0 has been the de facto IaC provider in BMW deploys since v0.51.2 (memory: `project_strict_contracts_cutover_complete.md`); the legacy modules predate the IaC abstraction and produce a different (non-conformant) state shape. There is no parity to verify — the new path supersedes the old path with a different config schema. Migration is a config rewrite, not a code port.

The "move-then-delete" model fits the AWS audit better because parts of AWS legitimately stay (RBAC, secrets, artifact). For DO, every godo importer is replaced.

## Recommendation

**Option A**, one PR `feat: remove godo from core (issue #617)`.

## Assumptions (load-bearing)

1. **Plugin parity assumption:** `workflow-plugin-digitalocean` v0.12.0 covers every resource served by the deleted core modules. *Test:* the parity matrix below maps each legacy module to its plugin replacement; the matrix MUST be re-validated before merge.

2. **No internal consumers downstream:** No downstream repo's YAML still relies on `platform.do_*` / `step.do_*` types post-IaC migration. *Test:* the implementer greps `buymywishlist`, `core-dump`, `workflow-cloud`, `workflow-scenarios`, `ratchet`, `ratchet-cli` config trees for the legacy names before opening the PR. Any hit becomes either a migration PR in that repo (Option A still ships) or a blocker (revisit).

3. **Schema allow-lists are advisory, not authoritative:** Removing entries from `schema/schema.go` does not silently re-allow them elsewhere — the registry is the only enforcement point and we're removing them there too. *Test:* `go test ./schema/...` after deletion.

4. **`go mod tidy` is sufficient to drop godo:** No other core file imports godo besides the listed eleven. *Test:* `grep -rn "digitalocean/godo" --include="*.go"` returns no results post-deletion (excluding worktree dirs).

5. **Engine v0.NEXT bump is acceptable:** This is a breaking change; CHANGELOG + a minor-version bump suffices. The user has authorized the autonomous pipeline to ship breaking changes (memory: `feedback_force_strict_contracts_no_compat.md`).

6. **DO plugin minEngineVersion `0.51.2` remains valid:** The plugin does not depend on any core symbol we're removing — it imports godo itself and only consumes the `iac.provider` interface from core. *Test:* the implementer runs `go build ./...` against the DO plugin with the post-cutover workflow module pinned via `replace`.

## Parity matrix — legacy core module → plugin replacement

| Legacy core type | Plugin replacement (`workflow-plugin-digitalocean` v0.12.0) | Notes |
|------------------|-------------------------------------------------------------|-------|
| `platform.do_app` | `infra.container_service` + provider `digitalocean` → driver `internal/drivers/app_platform.go` | App Platform spec maps, region routing, build spec, migration repair, image presence — all present in plugin. |
| `platform.do_database` | `infra.database` + provider `digitalocean` → driver `internal/drivers/database.go` | Managed PG / MySQL / Redis. |
| `platform.do_dns` | `infra.dns` + provider `digitalocean` → `internal/drivers/dns_*.go` (declared in plugin docs) | DNS zone + records. |
| `platform.do_networking` | `infra.vpc` + `infra.firewall` + provider `digitalocean` → `internal/drivers/vpc.go`, `internal/drivers/firewall.go` (per plugin manifest) | VPC + firewall split per IaC model. |
| `platform.doks` | `infra.k8s_cluster` + provider `digitalocean` (plugin manifest) | DOKS cluster + node pool. |
| `cloud.account` (DO resolver) | DO plugin manages its own DO API token via `iac.provider` credential broker | Plugin doesn't need the legacy resolver chain. |
| `step.do_deploy/status/logs/scale/destroy` | `step.iac_plan/apply/status/destroy` (generic IaC steps) + plugin drivers | Generic steps drive any IaC provider; DO is no longer special-cased. |

If any cell of this matrix is wrong, the implementer files an issue against `workflow-plugin-digitalocean` BEFORE submitting the cutover PR, and the cutover PR blocks on that issue's fix.

## Self-challenge round

1. **Lazier solution?** A single `replace github.com/digitalocean/godo => ../shim` is lazier but doesn't satisfy goal #1 (godo still in go.mod). A `// nolint:godox` is laziest but ignores the problem. No lazier path satisfies all four goals.

2. **Most fragile assumption?** Assumption #2 — no downstream consumer still uses `platform.do_*`. Mitigation: the implementer's pre-PR grep step covers it; the load-time migration error covers the field; the CHANGELOG covers expectations. Cost of a missed hit = one follow-up migration PR in the affected repo.

3. **YAGNI sweep — what does this design solve that wasn't asked?** Two items examined:
   - Migration error message (kept — directly satisfies goal #3 "wfctl errors remain actionable").
   - Generic "legacy provider type" framework (dropped — only DO needs this today; AWS legacy types stay; adding a framework is premature abstraction).

4. **Partial failure surface:** `go mod tidy` fails on a transitive godo dep we missed → CI catches before merge. Plugin doesn't satisfy a parity cell → caught by the matrix re-validation step pre-merge; if discovered post-merge, fixed in plugin and core is unaffected because the symbol is gone from core. Config loads with a removed type → migration error fires; no silent skip.

5. **Fights existing pattern?** No. Force-cutover precedent: `feedback_force_strict_contracts_no_compat.md`. The IaC migration design (`docs/plans/2026-04-17-deploy-pipeline-multi-env-design.md` lines 126-130) explicitly maps the legacy DO types to `infra.*` — this design completes that migration.

**Top 3 doubts surfaced for adversarial review:**

1. Are there cached external YAML configs (BMW prod, core-dump prod) still using `platform.do_*` that will fail on next deploy? Migration error catches it but operators may not be reading the changelog.
2. Does the DO plugin's `iac.provider` IaC state shape converge with the legacy modules' state shape, or is there an unmigrated state-file flag day? (State-shape mismatch already known to exist — memory `project_strict_contracts_cutover_complete.md`.)
3. Does removing `cloud_account_do.go` break the credential resolver registry for any test or external module that registers a DO-flavored resolver? `RegisterCredentialResolver` is a global registry; removing one register call should be safe but the order-dependence is worth checking.

## Implementation plan (preview — full plan written by writing-plans skill)

Single PR, ~5 tasks:

1. **T1 — Delete legacy module + step files (11 files).** Pure deletion; tests assert removal.
2. **T2 — Strip registration sites (8 files).** Edits to `plugins/platform/plugin.go`, `schema/*.go`, `cmd/wfctl/type_registry.go`, `cmd/wfctl/infra.go`, `cmd/wfctl/deploy_providers.go`, `cmd/wfctl/ci_run_dryrun.go`, `plugins/platform/plugin_test.go`, `module/multi_region.go`.
3. **T3 — Add load-time migration error + tests.** Engine fails-closed on legacy DO types with actionable message; new test fixtures cover all 5 legacy types.
4. **T4 — `go mod tidy` + grep gate.** Confirm zero `digitalocean/godo` imports remain (excluding worktrees); update go.sum; CI gate added in CI workflow that re-greps on every PR to prevent regression.
5. **T5 — Docs + CHANGELOG + migration guide stub.** Update `DOCUMENTATION.md`, prepend a CHANGELOG breaking-change entry, add `docs/migrations/v<NEXT>-godo-removal.md` with the 5 legacy → `infra.*` mappings.

Post-merge: file follow-up issue **"#NEXT — Audit AWS SDK usage in workflow core (RBAC/secrets/artifact stay; IaC drivers reviewed for plugin move)"** so the AWS half of the issue's audit-points note is tracked.

## Rollback

This change affects build, package version, and runtime config loading. Rollback path:

- **Pre-merge:** revert the branch; no consumer impact.
- **Post-merge, pre-tag:** revert the PR; force a new minor without the change. No consumer impact.
- **Post-tag:** consumers pin the previous tag (`go get github.com/GoCodeAlone/workflow@v<PREVIOUS>`). Migration error reverts. Provided we have not advanced any consumer's pin in the same window, this is a clean fallback. CHANGELOG must call out the pinned-pre-cutover version explicitly.
- **State files written by the new path:** unaffected (state lives in `iac.state` backends, not in deleted code).

## Open questions (none blocking — autonomous pipeline proceeds)

- Should the migration error be a hard error or a warning + skip? **Decision (autonomous):** hard error. A silently-skipped module is worse than a failed load; goal #3 mandates actionable errors. Re-open if adversarial review pushes back.
- Should `cloud_account_do.go` deletion include removing the registered resolver names (`digitalocean/static`, `digitalocean/env`, `digitalocean/api_token`) from any global registry to prevent dead config keys? **Decision (autonomous):** yes — the registry is purely additive via init(); deleting the file removes the init(). Add a test that the credential registry has zero `digitalocean/*` entries post-deletion.

## References

- Issue #617
- PR #421 (godo dependabot bump — the trigger signal)
- Memory: `feedback_force_strict_contracts_no_compat.md` — force-cutover precedent
- Memory: `project_strict_contracts_cutover_complete.md` — typed-gRPC cutover; DO plugin v1.0.1 strict-contracts release
- Memory: `project_do_plugin_typed_iac_gap.md` — DO plugin IaC service registration history
- `docs/plans/2026-04-17-deploy-pipeline-multi-env-design.md` lines 126-130 — legacy → `infra.*` mapping (decided long before this issue)
