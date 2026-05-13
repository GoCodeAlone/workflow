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
| `module/platform_doks_test.go` | 164 | tests |
| `module/cloud_account_do.go` | 74 | DO credential resolvers + `doClient()` |
| `module/pipeline_step_do.go` | 220 | 5 DO App Platform pipeline steps |
| **Total (12 files)** | **~3206** | |

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

### Migration error — modules + steps (both paths covered)

Two guards, one per registry. Both fire in the unknown-type fallback path so they are unreachable when the type is registered by a loaded plugin.

**Module guard** — in `engine.go BuildFromConfig` (unknown-module-type branch):

```
unsupported legacy module type %q: this type was removed from workflow core in v<NEXT>.

DigitalOcean IaC moved to workflow-plugin-digitalocean.
%s

Migrate this module to the equivalent infra.* IaC type:
  platform.do_app        → infra.container_service (provider: digitalocean)
  platform.do_database   → infra.database (provider: digitalocean)
  platform.do_dns        → infra.dns (provider: digitalocean)
  platform.do_networking → infra.vpc + infra.firewall (provider: digitalocean)
  platform.doks          → infra.k8s_cluster (provider: digitalocean)

See docs/migrations/v<NEXT>-godo-removal.md.
```

The middle `%s` line branches on plugin-loaded detection (closes adversarial finding I-2):

- If the `iac.provider` factory is registered in the engine's module factory map (`_, iacLoaded := e.moduleFactories["iac.provider"]`) → emit `"workflow-plugin-digitalocean is already loaded; your config still references the legacy module name."`
- Otherwise → emit `"Install workflow-plugin-digitalocean: https://github.com/GoCodeAlone/workflow-plugin-digitalocean"`

(The single-factory-map check is sufficient because the DO plugin is the only known publisher of `iac.provider` in the GoCodeAlone ecosystem. An ANDed check on `provider: digitalocean` infra.* bindings would have no clean implementation path at the engine layer and would not add discriminating signal — see cycle-3 review m-2.)

**Step guard** — in `module/pipeline_step_registry.go` (or `engine.go buildPipelineSteps`'s unknown-step-type branch), for the five legacy step types. Per-step messages because the mapping is NOT one-to-one (closes finding I-1):

```
step.do_deploy   → step.iac_apply (against an infra.container_service module)
step.do_destroy  → step.iac_destroy (against an infra.container_service module)
step.do_status   → step.iac_status (against an infra.container_service module)
step.do_logs     → no direct equivalent. The DO plugin attaches deploy logs
                   internally via its Troubleshoot hook on step.iac_apply
                   failure. For ad-hoc log fetch, use `wfctl infra logs`.
                   A pipeline-step replacement is tracked in
                   workflow-plugin-digitalocean issue <TBD>.
step.do_scale    → no direct equivalent. Update instance_count in the
                   infra.container_service module config and re-run
                   step.iac_apply. A first-class step.iac_scale is tracked
                   in workflow-plugin-digitalocean issue <TBD>.
```

The same plugin-loaded detection branches the step-guard prefix (`Install ... / already loaded ...`).

This satisfies acceptance criterion #3 for both modules and pipeline steps.

## Considered approaches

### Option A — Single-PR force-cutover (RECOMMENDED)

Delete in one PR: 11 module files, all registration sites, godo from go.mod, plus migration error + migration doc. Tag a new minor; CHANGELOG calls out breaking change.

**Pros:** Mirrors strict-contracts precedent; no duplication window; clean git history; Dependabot stops touching core immediately.
**Cons:** Any consumer YAML using `platform.do_*` breaks on engine upgrade. Mitigated by actionable error message + migration guide.

### Option B — Phased deprecation (REJECTED)

Mark legacy modules deprecated, gate behind a `LEGACY_DO_MODULES=1` env var, remove godo in a later release.

**Pros:** Soft landing.
**Cons:** Fights force-cutover precedent; perpetuates duplication; Dependabot still nags core during the window; doubles the work (two PRs, deprecation warnings, retest matrix); a "later release" reliably becomes "never."

### Option B′ — Go build tag fence (REJECTED)

Add `//go:build !workflow_strict` (or similar) to the six godo-importing files so the production binary excludes them while tests stay.

**Pros:** No deletion; tests keep running; "reversible."
**Cons:** Fails goal #1 — `godo` remains in `go.mod` because the build-tagged code still parses. Fails goal #4 — Dependabot still nags. Perpetuates ambiguity about "supported." Adds a build matrix for zero net benefit over Option A.

### Option C — Move-then-delete (REJECTED for DO; matches the AWS audit follow-up)

Audit DO plugin parity, file gap issues against `workflow-plugin-digitalocean`, fix gaps, then delete from core.

**Pros:** Surfaces gaps before consumer surprise.
**Cons:** Premature here — plugin v0.12.0 has been the de facto IaC provider in BMW deploys since v0.51.2 (memory: `project_strict_contracts_cutover_complete.md`); the legacy modules predate the IaC abstraction and produce a different (non-conformant) state shape. There is no parity to verify — the new path supersedes the old path with a different config schema. Migration is a config rewrite, not a code port.

The "move-then-delete" model fits the AWS audit better because parts of AWS legitimately stay (RBAC, secrets, artifact). For DO, every godo importer is replaced.

## Recommendation

**Option A**, one PR `feat: remove godo from core (issue #617)`.

### Companion: `wfctl modernize` rule (in scope of T5)

The engine already has a `modernize` command (`mcp__workflow__modernize` tool + wfctl subcommand) that auto-rewrites legacy YAML anti-patterns. Add five rewrite rules so user YAML migrates with one `wfctl modernize --write` invocation:

- `module/type: platform.do_app` → `module/type: infra.container_service` + inject `config.provider: digitalocean`
- `module/type: platform.do_database` → `module/type: infra.database` + provider
- `module/type: platform.do_dns` → `module/type: infra.dns` + provider
- `module/type: platform.do_networking` → split into `infra.vpc` + `infra.firewall` modules (lossy — emit a comment-prefixed warning when source has both `vpc` and `firewalls` keys with non-overlapping shapes)
- `module/type: platform.doks` → `module/type: infra.k8s_cluster` + provider
- `step/type: step.do_deploy/status/destroy` → `step.iac_apply/status/destroy` with `module` field re-bound to the migrated module name
- `step/type: step.do_logs/scale` → emit a `wfctl: cannot rewrite — see migration guide` annotation; do not delete the step (operator must address manually)

This reduces migration friction from manual-rewrite to one `wfctl modernize --apply <config.yaml>` invocation + manual review of the two annotated step types. (Flag is `--apply`, verified against `cmd/wfctl/modernize.go`.) Folds into T5.

## Assumptions (load-bearing)

1. **Plugin parity assumption:** `workflow-plugin-digitalocean` v0.12.0 covers every resource served by the deleted core modules. *Test:* the parity matrix below maps each legacy module to its plugin replacement; the matrix MUST be re-validated before merge.

2. **No internal consumers downstream:** No downstream repo's YAML still relies on `platform.do_*` / `step.do_*` types post-IaC migration. *Test:* the implementer greps `buymywishlist`, `core-dump`, `workflow-cloud`, `workflow-scenarios`, `ratchet`, `ratchet-cli` config trees for the legacy names before opening the PR. Any hit becomes either a migration PR in that repo (Option A still ships) or a blocker (revisit). **Sequencing constraint:** any `workflow-scenarios` migration PRs must merge before — or in the same batch as — the engine cutover tag is consumed by scenario-CI, otherwise scenario CI will fail with the (correctly) actionable migration errors.

3. **Schema allow-lists are advisory, not authoritative:** Removing entries from `schema/schema.go` does not silently re-allow them elsewhere — the registry is the only enforcement point and we're removing them there too. *Test:* `go test ./schema/...` after deletion.

4. **`go mod tidy` is sufficient to drop godo:** No other core file imports godo besides the listed eleven. *Test:* `grep -rn "digitalocean/godo" --include="*.go"` returns no results post-deletion (excluding worktree dirs).

5. **Engine v0.NEXT bump is acceptable:** This is a breaking change; CHANGELOG + a minor-version bump suffices. The user has authorized the autonomous pipeline to ship breaking changes (memory: `feedback_force_strict_contracts_no_compat.md`).

6. **DO plugin minEngineVersion `0.51.2` remains valid:** The plugin does not depend on any core symbol we're removing — it imports godo itself and only consumes the `iac.provider` interface from core. *Test:* the implementer runs `go build ./...` against the DO plugin with the post-cutover workflow module pinned via `replace`.

## Parity matrix — legacy core module → plugin replacement

| Legacy core type | Plugin replacement (`workflow-plugin-digitalocean` v0.12.0) | Notes |
|------------------|-------------------------------------------------------------|-------|
| `platform.do_app` | `infra.container_service` + provider `digitalocean` → driver `internal/drivers/app_platform.go` | App Platform spec maps, region routing, build spec, migration repair, image presence — all present in plugin. |
| `platform.do_database` | `infra.database` + provider `digitalocean` → driver `internal/drivers/database.go` | Managed PG / MySQL / Redis. |
| `platform.do_dns` | `infra.dns` + provider `digitalocean` → `internal/drivers/dns.go` | DNS zone + records. |
| `platform.do_networking` | `infra.vpc` + `infra.firewall` + provider `digitalocean` → `internal/drivers/vpc.go`, `internal/drivers/firewall.go` (per plugin manifest) | VPC + firewall split per IaC model. |
| `platform.doks` | `infra.k8s_cluster` + provider `digitalocean` (plugin manifest) | DOKS cluster + node pool. |
| `cloud.account` (DO resolver) | DO plugin manages its own DO API token via `iac.provider` credential broker | Plugin doesn't need the legacy resolver chain. |
| `step.do_deploy` | `step.iac_apply` against `infra.container_service` | 1:1 mapping; provider drives apply. |
| `step.do_status` | `step.iac_status` against `infra.container_service` | 1:1 mapping. |
| `step.do_destroy` | `step.iac_destroy` against `infra.container_service` | 1:1 mapping. |
| `step.do_logs` | **GAP** — no pipeline-step equivalent | DO plugin attaches logs via `Troubleshoot` hook internally on apply failure; ad-hoc fetch via `wfctl infra logs`. Tracked in plugin issue (filed pre-merge). Documented in migration guide. |
| `step.do_scale` | **GAP** — config-driven re-apply only | Update `instance_count` in `infra.container_service` config + re-run `step.iac_apply`. First-class `step.iac_scale` tracked in plugin issue (filed pre-merge). Documented in migration guide. |

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

1. **T1 — Delete legacy module + step files (12 files).** Pure deletion (all 12 rows in the "Module files" table above, including `module/platform_doks_test.go`); a new test asserts removal of registry entries.
2. **T2 — Strip registration sites (10 files).** Edits to `plugins/platform/plugin.go`, `schema/schema.go`, `schema/module_schema.go`, `schema/step_schema_builtins.go`, `cmd/wfctl/type_registry.go`, `cmd/wfctl/infra.go`, `cmd/wfctl/deploy_providers.go`, `cmd/wfctl/ci_run_dryrun.go`, `plugins/platform/plugin_test.go`, `module/multi_region.go`. Implementer also reviews `cmd/wfctl/infra_apply_test.go` line 1990 (negative-test fixture using `type: platform.do_app`) — replace with a synthetic non-existent type or remove if the negative case is redundant.
3. **T3 — Add load-time migration error + tests.** Engine fails-closed on legacy DO types with actionable message; new test fixtures cover all 5 legacy types.
4. **T4 — `go mod tidy` + grep gate.** Confirm zero `digitalocean/godo` imports remain in code AND in module files; update go.sum; add CI gate.

   Tidy steps:
   ```sh
   go mod tidy            # root module
   (cd example && go mod tidy)   # standalone example/ sub-module also pins godo as indirect
   ```

   Grep gate (fail-on-match, exact invocation — both gates use `!` to invert grep's exit code so a match becomes a failing CI step):
   ```sh
   # *.go gate:
   ! grep -rn --include="*.go" \
       --exclude-dir=_worktrees \
       --exclude-dir=.worktrees \
       --exclude-dir=.claude \
       "digitalocean/godo" .

   # go.mod gate (root + example/):
   ! grep -qH "digitalocean/godo" go.mod example/go.mod
   ```
   Gate lives in `.github/workflows/ci.yml` (or wherever `golangci-lint` already runs) as a fail-on-match step. Same grep also runs as a pre-commit step locally documented in CONTRIBUTING (no install required, repo-relative).
5. **T5 — Docs + CHANGELOG + migration guide + modernize rules.** Update `DOCUMENTATION.md`, prepend CHANGELOG breaking-change entry, add `docs/migrations/v<NEXT>-godo-removal.md` (5 module + 5 step mappings, plus explicit GAP callout for `step.do_logs` / `step.do_scale` with workaround YAML examples), implement the seven `wfctl modernize` rewrite rules above + test fixtures, file the two follow-up issues in `workflow-plugin-digitalocean` (`step.iac_logs`, `step.iac_scale`) and wire their issue numbers back into the migration error messages.

Post-merge: file follow-up issue **"#NEXT — Audit AWS SDK usage in workflow core (RBAC/secrets/artifact stay; IaC drivers reviewed for plugin move)"** so the AWS half of the issue's audit-points note is tracked.

## Rollback

This change affects build, package version, and runtime config loading. Rollback path:

- **Pre-merge:** revert the branch; no consumer impact.
- **Post-merge, pre-tag:** revert the PR; force a new minor without the change. No consumer impact.
- **Post-tag:** consumers pin the previous tag (`go get github.com/GoCodeAlone/workflow@v<PREVIOUS>`). Migration error reverts. Provided we have not advanced any consumer's pin in the same window, this is a clean fallback. CHANGELOG must call out the pinned-pre-cutover version explicitly.
- **State files written by the new path:** unaffected (state lives in `iac.state` backends, not in deleted code).

## Open questions (none blocking — autonomous pipeline proceeds)

- Should the migration error be a hard error or a warning + skip? **Decision (autonomous):** hard error. A silently-skipped module is worse than a failed load; goal #3 mandates actionable errors. Re-open if adversarial review pushes back.
- Should `cloud_account_do.go` deletion include removing the registered resolver names (`digitalocean/static`, `digitalocean/env`, `digitalocean/api_token`) from any global registry to prevent dead config keys? **Decision (autonomous):** the registry is purely additive via `init()`; deleting the file removes the `init()` call, which is itself the evidence that no DO resolver is registered. No separate test is added — `credentialResolvers` is unexported (`module/cloud_credential_resolver.go:14`) and adding an exported accessor solely for a one-shot self-evidencing assertion is API-surface-for-test-only. Verified instead by the build (no godo importer remains) and by the migration error path (which fires when a `cloud.account` with `provider: digitalocean` is loaded but no DO resolver is registered).

## Adversarial review history

### Cycle 3 (PASS — 0 Critical / 0 Important; 3 Minor incorporated) — 2026-05-13

- **m-1** Grep gates lacked `!`-prefix; `|| true` silently suppressed exit code → **fixed**: both gates now use `!` prefix to fail CI on match.
- **m-2** Plugin-loaded detection over-specified (AND of factory map + provider binding) → **fixed**: simplified to `_, iacLoaded := e.moduleFactories["iac.provider"]` with rationale.
- **m-3** Missing sequencing constraint for `workflow-scenarios` migration → **fixed**: explicit constraint added to Assumption #2.
- **t-1** T2 file count was 9; actual list contained 10 → **fixed**.
- **Cycle-1 and Cycle-2 fixes verified to hold.**

Verdict: PASS. Pipeline advances to writing-plans.

### Cycle 2 (FAIL) — 2026-05-13

- **I-1** (new) `module/platform_doks_test.go` (164 LOC) missing from deletion inventory → **fixed**: row added; total bumped to 12 files / ~3206 LOC; T1 scope updated.
- **m-1** (new) wfctl flag was `--write`, actual flag is `--apply` → **fixed**.
- **m-2** (new) `example/go.mod` carries `godo` as indirect dependency; T4 grep only covered `*.go` → **fixed**: `(cd example && go mod tidy)` added; second grep over `go.mod` files added.
- **Cycle-1 fixes verified to hold** — no regressions introduced by cycle-1 changes.

### Cycle 1 (FAIL) — 2026-05-13

- **C-1** Migration error covered modules but not `step.do_*` steps → **fixed**: added per-step guard + per-step migration message (modules/steps both branched on plugin-loaded detection).
- **I-1** Parity matrix collapsed 5 step types into 4 generic ones, hiding `step.do_logs` + `step.do_scale` capability gap → **fixed**: parity matrix now lists each step row separately, GAPs called out, two follow-up issues to be filed pre-merge in `workflow-plugin-digitalocean`.
- **I-2** Migration error misleads users who already have plugin loaded → **fixed**: migration error branches on plugin-loaded detection (different prefix for "install" vs "already loaded — config issue").
- **m-1** Grep gate worktree exclusion underspecified → **fixed**: exact `grep -rn ... --exclude-dir=...` invocation in T4.
- **m-2** `dns_*.go` → `dns.go` typo → **fixed**.
- **m-3** `cmd/wfctl/infra_apply_test.go:1990` fixture missing from T2 → **fixed**: explicitly called out for review.
- **Option B′ (build tag fence)** added to Considered approaches per reviewer suggestion (rejected with stated reason).
- **`wfctl modernize` companion** added per reviewer suggestion (in scope of T5).

## References

- Issue #617
- PR #421 (godo dependabot bump — the trigger signal)
- Memory: `feedback_force_strict_contracts_no_compat.md` — force-cutover precedent
- Memory: `project_strict_contracts_cutover_complete.md` — typed-gRPC cutover; DO plugin v1.0.1 strict-contracts release
- Memory: `project_do_plugin_typed_iac_gap.md` — DO plugin IaC service registration history
- `docs/plans/2026-04-17-deploy-pipeline-multi-env-design.md` lines 126-130 — legacy → `infra.*` mapping (decided long before this issue)
