# Issue #653 Phase 1 — Remove AWS IaC Modules from Workflow Core

**Status:** Draft for adversarial review
**Owner:** autonomous pipeline
**Issue:** [GoCodeAlone/workflow#653](https://github.com/GoCodeAlone/workflow/issues/653)
**Date:** 2026-05-13
**Precedent:** `docs/plans/2026-05-13-issue-617-godo-removal-design.md` (#617 godo removal, merged c55a56e5)

---

## Summary

Workflow core directly imports `github.com/aws/aws-sdk-go-v2` service packages to back six legacy AWS IaC modules: `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.dns` (AWS Route53 backend only), `platform.networking`, and the standalone `AWSAPIGateway` helper (not registered as a module type). The same IaC surface is already implemented in `workflow-plugin-aws` v0.2.0 as proper IaC provider plugin drivers.

This design proposes a **single-PR force-cutover** that deletes the legacy AWS IaC surface, removes the now-unused AWS service SDK packages from `go.mod`, and emits actionable migration errors.

**Critical architectural finding:** `cloud_account_aws.go` and `cloud_account_aws_creds.go` are **NOT deleted** in Phase 1. They provide `AWSConfigProvider` interface + `AWSConfig()` method + credential resolvers used by out-of-scope files (`codebuild.go`, `platform_kubernetes_kind.go`, `secrets_aws.go`, `module/pipeline_step_s3_upload.go`). Deleting them would break these Phase 2 files which are explicitly out-of-scope. The correct parallel is `cloud_account_azure.go` (which is never deleted because Azure credential resolution stays in core). This is the primary divergence from #617's scope: the AWS credential resolver stays in core; only the IaC module implementations move.

**Critical finding: platform.dns is generic, not AWS-only.** The `platform.dns` module type supports pluggable backends (mock + aws/route53). The module registration, `platform_dns.go`, `pipeline_step_dns.go`, and the `step.dns_*` step types are generic and **stay**. Only the Route53 backend implementation in `platform_dns_backends.go` is deleted. The file is replaced by `platform_dns_backend_mock.go` (the `mockDNSBackend` from the deleted file) + a migration stub that emits the actionable error when `provider: aws` is configured.

---

## Goals (acceptance criteria from #653)

1. Workflow core no longer imports `service/ecs`, `service/apigatewayv2`, `service/applicationautoscaling`, `service/route53`, `service/ec2` for IaC module behavior.
2. AWS IaC behavior remains available through `workflow-plugin-aws` v0.2.0+.
3. `wfctl` errors remain actionable when a legacy AWS module type is referenced.
4. `go mod tidy` drops the freed service packages. Base packages (`aws-sdk-go-v2`, `config`, `credentials`, `service/sts`) remain because they are used by out-of-scope files.

## Non-goals

- Removing `cloud_account_aws.go` / `cloud_account_aws_creds.go` (Phase 2, blocked on codebuild/platform_kubernetes_kind removal).
- Removing the generic `platform.dns` module type or `step.dns_*` step types.
- Removing `codebuild.go`, `nosql_dynamodb.go`, `pipeline_step_s3_upload.go`, `platform_kubernetes_kind.go` (Phase 2 operational tooling audit).
- Removing `platform/providers/aws/drivers/` (Phase 3 architectural question).
- Backwards-compatible shim modules — force-cutover, no compat layer.
- Migration tooling beyond actionable load-time error + wfctl modernize rules.

---

## Current state — surface to remove

### Module files (deleted in Phase 1)

| File | Lines | Module type / purpose |
|------|-------|----------------------|
| `module/platform_ecs.go` | 571 | `platform.ecs` — AWS ECS/Fargate module |
| `module/platform_ecs_test.go` | ~150 | Unit tests |
| `module/pipeline_step_ecs.go` | ~120 | `step.ecs_plan/apply/status/destroy` factories |
| `module/platform_apigateway.go` | 519 | `platform.apigateway` — AWS API GW module |
| `module/platform_apigateway_test.go` | ~200 | Unit tests |
| `module/pipeline_step_apigateway.go` | ~100 | `step.apigw_plan/apply/status/destroy` factories |
| `module/aws_api_gateway.go` | 277 | `AWSAPIGateway` helper (unregistered, used only in tests) |
| `module/api_gateway_test.go` (partial) | 558 total | **Partial delete**: remove 3 `TestAWSAPIGateway_*` tests; keep 19 generic HTTP gateway tests |
| `module/platform_autoscaling.go` | 485 | `platform.autoscaling` — AWS App Auto Scaling module |
| `module/platform_autoscaling_test.go` | ~150 | Unit tests |
| `module/pipeline_step_autoscaling.go` | ~120 | `step.scaling_plan/apply/status/destroy` factories |
| `module/platform_networking.go` | 638 | `platform.networking` — AWS EC2/VPC module |
| `module/platform_networking_test.go` | ~200 | Unit tests |
| `module/pipeline_step_networking.go` | ~100 | `step.network_plan/apply/status/destroy` factories |
| `module/platform_dns_backends.go` | 358 | Route53 backend + mock backend |
| `module/platform_aws_integration_test.go` | ~140 | Integration tests for ECS + networking + DNS |

**Modified files (partial edit, not deleted):**

| File | Edit |
|------|------|
| `module/api_gateway_test.go` | Remove 3 `TestAWSAPIGateway_*` test functions only; keep all 19 generic HTTP gateway tests |
| `module/platform_dns_backends.go` | **Replace entire file**: delete route53Backend implementation; keep mockDNSBackend + add AWS route removed error stub |
| `module/app_container.go` | **C-3 fix**: Remove all ECS-specific code (`ECSAppManifests`, `ECSAppTaskDef`, `ECSAppServiceCfg`, `ecsAppBackend` + methods, `buildECSManifests()`); remove `case *PlatformECS:` type switch branch; update default-case error message. After edit: supports platform.kubernetes only; zero AWS SDK imports; compiles cleanly. |

**New file (split from platform_dns_backends.go):**

| File | Purpose |
|------|---------|
| (none — mock backend stays in platform_dns_backends.go after route53 removal) | See edit above |

### Registration / schema sites

| File | Edit |
|------|------|
| `plugins/platform/plugin.go` | Drop `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking` from `ModuleTypes`; drop 4 module factories; drop `step.ecs_*`, `step.apigw_*`, `step.scaling_*`, `step.network_*` from `StepTypes`; drop **15** step factories. Keep `platform.dns`, `step.dns_*`, and all other types. |
| `plugins/platform/plugin_test.go` | Drop the 4 module type + **15** step type string assertions. |
| `plugins/infra/plugin.go` | **ADD** `"infra.autoscaling_group"` to `infraTypes` slice (new 14th infra type). Required so that configs migrated from `platform.autoscaling` validate correctly. |
| `schema/schema.go` | Drop `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking` from module type list. |
| `schema/module_schema.go` | Drop 4 module schemas (`platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking`). **Update** `platform.dns` ConfigFieldDef for `provider` to `Description: "mock (aws Route53 backend removed; use infra.dns with workflow-plugin-aws)"`. **Note:** `infra.autoscaling_group` schema is auto-generated from `infraTypes` in `plugins/infra/plugin.go`'s `ModuleSchemas()` — no manual schema entry needed in `module_schema.go`. |
| `schema/step_schema_builtins.go` | Drop **15** step schema `Register` calls (ecs, apigw, scaling, network steps). |
| `cmd/wfctl/type_registry.go` | Drop `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking` entries. **ADD** `"infra.autoscaling_group"` entry (mirrors other infra.* entries). |
| `schema/testdata/editor-schemas.golden.json` | Update golden file via `UPDATE_GOLDEN=1 go test ./schema/ -run TestEditorSchemasGoldenFile`. |
| `module/multi_region.go:117` | Update error message that references `platform.ecs` to use `infra.container_service`. |
| `module/app_container.go` comment lines | Update doc comment (line 14) to remove `platform.ecs` mention; update logger.Warn hint (line 147) to remove `platform.ecs` mention. These are string-only changes covered in T1 alongside the code edits. |
| `DOCUMENTATION.md` | Remove 4 module rows + **15** step rows; keep `platform.dns` and `step.dns_*` rows; add paragraph pointing at `workflow-plugin-aws`. |
| `go.mod` / `go.sum` | `go mod tidy` after deletion drops freed service packages. |

### Step types removed (15 total)

| Module | Steps removed |
|--------|---------------|
| `platform.ecs` | `step.ecs_plan`, `step.ecs_apply`, `step.ecs_status`, `step.ecs_destroy` |
| `platform.apigateway` | `step.apigw_plan`, `step.apigw_apply`, `step.apigw_status`, `step.apigw_destroy` |
| `platform.autoscaling` | `step.scaling_plan`, `step.scaling_apply`, `step.scaling_status`, `step.scaling_destroy` |
| `platform.networking` | `step.network_plan`, `step.network_apply`, `step.network_status` (**no `step.network_destroy` exists**) |

**Remaining (stays):** `step.dns_plan`, `step.dns_apply`, `step.dns_status` — these back the generic `platform.dns` module which stays.

Count: 4 (ecs) + 4 (apigw) + 4 (scaling) + 3 (networking) = **15 step types**. Verified: `schema/step_schema_builtins.go` and `plugins/platform/plugin.go` StepFactories both have exactly 15 entries for these step types.

---

## Migration errors

### Module guard (4 types)

In `engine.go BuildFromConfig` (unknown-module-type branch, after legacydo check):

```
unsupported legacy module type %q (module %q): this type was removed from workflow core in v<NEXT>.

AWS IaC moved to workflow-plugin-aws.
%s

Migrate this module to the equivalent infra.* IaC type:
  platform.ecs        → infra.container_service (provider: aws)
  platform.apigateway → infra.api_gateway (provider: aws)
  platform.autoscaling → infra.autoscaling_group (provider: aws)
  platform.networking → infra.vpc + infra.firewall (provider: aws)

See docs/migrations/v<NEXT>-aws-iac-removal.md.
```

The `%s` line branches on plugin-loaded detection (mirrors #617 pattern):
- `_, iacLoaded := e.moduleFactories["iac.provider"]` → if true: `"workflow-plugin-aws is already loaded; your config still references the legacy module name."` else: `"Install workflow-plugin-aws: https://github.com/GoCodeAlone/workflow-plugin-aws"`

### Step guard (15 types)

In `module/pipeline_step_registry.go Create()` (unknown-step-type branch, after legacydo check):

```
step.ecs_plan/apply/status/destroy     → step.iac_plan/apply/status/destroy (against an infra.container_service module)
step.apigw_plan/apply/status/destroy   → step.iac_plan/apply/status/destroy (against an infra.api_gateway module)
step.scaling_plan/apply/status/destroy → step.iac_plan/apply/status/destroy (against an infra.autoscaling_group module)
step.network_plan/apply/status         → step.iac_plan/apply/status (against an infra.vpc + infra.firewall module)
  (note: step.network_destroy never existed; no mapping needed)
```

Same plugin-loaded detection branching as #617.

### platform.dns provider: aws guard

The Route53 backend removal is implemented in two places:

**Runtime guard** (in `platform.dns` Init(), via the `awsRoute53ErrorBackend` registered for provider `"aws"`):
```
platform.dns %q: AWS Route53 backend removed from workflow core in v<NEXT>.
Migrate to: infra.dns (provider: aws) with workflow-plugin-aws v0.2.0+.
Install: https://github.com/GoCodeAlone/workflow-plugin-aws
See docs/migrations/v<NEXT>-aws-iac-removal.md.
```

**Validate-path guard** (in `cmd/wfctl/validate.go` and `cmd/wfctl/ci_validate.go` post-ValidateConfig sweep): For any module with `type: platform.dns` and `config.provider: aws`, emit the same migration error string. This is required for goal #3 (actionable `wfctl` errors) — the runtime Init() error fires too late for `wfctl validate`.

Implementation: the post-ValidateConfig loop in `validate.go:161` (already exists for legacydo types) is extended to also check for `type: platform.dns` + `provider: aws` config key. The same extension applies to `ci_validate.go:148`.

---

## go.mod impact

After deleting the in-scope files, these packages become unreferenced in `module/` and are also not used anywhere else in core:

| Package | Freed by deletion of |
|---------|---------------------|
| `service/apigatewayv2` | platform_apigateway.go + aws_api_gateway.go |
| `service/applicationautoscaling` | platform_autoscaling.go |
| `service/route53` | platform_dns_backends.go |
| `service/ec2` | **Stays** — also used by `platform/providers/aws/drivers/vpc.go` |
| `service/ecs` | **Stays** — also used by `provider/aws/plugin.go` |
| `service/sts` | **Stays** — also used by `iam/aws.go`, `platform/providers/aws/`, `provider/aws/`, AND `cloud_account_aws.go` (kept) |
| `credentials/stscreds` | **Stays** — also used by `provider/aws/plugin.go` AND `cloud_account_aws.go` (kept) |
| `service/cloudwatch` | **Stays** — used by `provider/aws/plugin.go` |
| `aws-sdk-go-v2` (base) | **Stays** — used by everything above |
| `config` | **Stays** — used by `cloud_account_aws.go`, `iac_state_spaces.go`, etc. |
| `credentials` | **Stays** — used by `cloud_account_aws.go`, `cloud_account_aws_creds.go` |

`go mod tidy` drops exactly: `service/apigatewayv2`, `service/applicationautoscaling`, `service/route53` (and their transitive-only deps if any become unreferenced).

---

## internal/legacyaws package

Following the `internal/legacydo` precedent (plan cycle-4 finding from #617 retro), constants and formatters for the legacy AWS types must live in a new leaf package `internal/legacyaws/types.go`. This prevents the `module → plugin → modernize` import cycle that would occur if `modernize/legacy_aws_rule.go` imported from `module/`.

`internal/legacyaws/types.go`:
- `RemovedInVersion = "v0.53.0"` (next minor after v0.52.0)
- `ModuleTypes` — 4 legacy module types + successors
- `StepTypes` — **15** legacy step types + successors (note: `step.network_destroy` never existed)
- `DNSProviderAWSError` — migration error string for `platform.dns` provider: aws
- `IsModuleType()`, `IsStepType()`, `FormatModuleError()`, `FormatStepError()`, `FormatDNSProviderAWSError()`

---

## wfctl modernize rules

A new `modernize/legacy_aws_rule.go` mirrors `legacy_do_rule.go`:

Auto-fixable (rename type only, no provider injection):
- `platform.ecs` → `infra.container_service`
- `platform.apigateway` → `infra.api_gateway`
- `platform.autoscaling` → `infra.autoscaling_group`

Not auto-fixable (1→2 split, operator must review):
- `platform.networking` → splits into `infra.vpc` + `infra.firewall`

Not auto-fixable (step config-shape mismatch, per #617 retro lesson):
- All **15** step types → flagged with message; NOT auto-rewritten (step.iac_* require different config keys: `platform` + `state_store` instead of the legacy `service`/`cluster` keys). Operator must rewrite manually per migration guide.

Not auto-fixable (provider sub-key, not a type rename):
- `platform.dns` with `provider: aws` → flagged in the DNS-backend check pass; modernize emits a comment-style finding (not a type rewrite) pointing at `infra.dns` + `workflow-plugin-aws`.

---

## Parity matrix — legacy core type → plugin replacement

| Legacy core type | workflow-plugin-aws v0.2.0 successor | Notes |
|-----------------|---------------------------------------|-------|
| `platform.ecs` | `infra.container_service` (provider: aws) | ECS/Fargate driver in plugin |
| `platform.apigateway` | `infra.api_gateway` (provider: aws) | API GW v2 driver in plugin |
| `AWSAPIGateway` helper | N/A — deleted as internal helper; not a registered module type | Not user-facing |
| `platform.autoscaling` | `infra.autoscaling_group` (provider: aws) | **New resource type** added to plugin in workflow-plugin-aws#9 (per task context) |
| `platform.dns` (AWS Route53) | `infra.dns` (provider: aws) | Route53 driver in plugin; generic platform.dns + mock backend stay in core |
| `platform.networking` | `infra.vpc` + `infra.firewall` (provider: aws) | VPC + SG driver in plugin; 1→2 split same as DO networking |
| `cloud.account` (AWS resolver) | Stays in core — cloud_account_aws.go not deleted | AWSConfigProvider needed by Phase 2 files |
| `step.ecs_*` (4 steps) | `step.iac_plan/apply/status/destroy` against infra.container_service | Config key change: `service:` → `platform:` + `state_store:` |
| `step.apigw_*` (4 steps) | `step.iac_plan/apply/status/destroy` against infra.api_gateway | Config key change |
| `step.scaling_*` (4 steps) | `step.iac_plan/apply/status/destroy` against infra.autoscaling_group | Config key change |
| `step.network_*` (3 steps) | `step.iac_plan/apply/status` against infra.vpc / infra.firewall | Config key change; networking gap: no `step.iac_destroy` available for multi-resource split; `step.network_destroy` never existed so no 4th mapping needed |

---

## Considered approaches

### Option A — Single-PR force-cutover (RECOMMENDED)

Delete 6 module files, companion tests, companion step files; replace platform_dns_backends.go; edit 8 registration sites; add `internal/legacyaws`; add migration errors + modernize rules; `go mod tidy` drops 3 service packages. Keep `cloud_account_aws.go` + `cloud_account_aws_creds.go` explicitly in core.

**Pros:** Mirrors #617 precedent; clean git history; Dependabot stops touching freed packages immediately.
**Cons:** Breaks configs using legacy types on engine upgrade. Mitigated by actionable error + migration guide.

### Option B — Also delete cloud_account_aws.go (REJECTED)

Delete all 8 original-scope files including credential files.

**Rejected:** `codebuild.go`, `platform_kubernetes_kind.go`, `secrets_aws.go` all call `AWSConfig()` + `awsProviderFrom()`. Deleting these files causes compile failures in out-of-scope Phase 2 code. Phase 2 is a separate audit with no clean precedent — it cannot be force-cutover'd in the same PR.

### Option C — Keep platform_dns_backends.go intact, register AWS removal at init (REJECTED)

Keep Route53 backend file but have it panic/error at call time.

**Rejected:** The AWS SDK imports remain in go.mod if the file stays. Goal #1 requires removing `service/route53`. Replace file with mock-only version instead.

---

## Assumptions (load-bearing)

1. **Plugin parity:** `workflow-plugin-aws` v0.2.0 covers all 6 deleted module types via their `infra.*` successors. `infra.autoscaling_group` was added in workflow-plugin-aws#9 per task context. *Test:* parity matrix above; implementer verifies plugin manifest before PR. Note: `infra.autoscaling_group` is also added to workflow core's infra plugin in T3 so `wfctl validate` accepts it post-migration.

2. **No downstream consumer uses platform.ecs/apigateway/autoscaling/networking directly:** Grep `buymywishlist`, `core-dump`, `workflow-cloud`, `workflow-scenarios` before opening PR.

3. **`cloud_account_aws.go` stays explicitly:** The `AWSConfigProvider` interface, `AWSConfig()` method, and credential resolvers are part of `cloud.account`'s multi-cloud support, not IaC module implementations. Parallel to `cloud_account_azure.go`. Removing them is Phase 2 scope.

4. **platform.dns module type stays:** The generic DNS provisioner with mock backend is not AWS-specific. Only the Route53 backend is removed.

5. **`go mod tidy` drops exactly 3 service packages:** `apigatewayv2`, `applicationautoscaling`, `route53`. All other AWS SDK packages have surviving in-scope-keepers.

6. **engine v0.53.0 bump is acceptable:** This is a breaking change; CHANGELOG + minor-version bump.

7. **step config-shape mismatch (from #617 retro):** All 16 step types are NOT auto-rewritten by modernize. Step schemas differ (legacy uses `service:` or `gateway:` etc; `step.iac_*` uses `platform:` + `state_store:`). This was the #617 gate miss — applied pre-emptively here.

8. **WalkDir regression test (from #617 retro):** The AWS SDK absent test MUST use `filepath.WalkDir` (recursive), NOT `filepath.Glob("*.go")` (flat).

---

## Self-challenge round

1. **Laziest solution?** Could we add build tags to the 6 files so the production binary excludes them while keeping go.mod clean? No — go.mod is tidy-based; build tags don't affect `go mod tidy`. Build tags that exclude `*.go` files still leave their imports in the module graph. Option A remains the only path that removes `service/route53` from go.mod.

2. **Most fragile assumption?** Assumption #3 — keeping `cloud_account_aws.go` in core. If the user intended it to be deleted (as the original scope manifest suggests), this design deviates. Mitigation: the original scope comment in issue #653 says "cloud.account (AWS resolver) → plugin owns credential broker" — but this was written before knowing that `AWSConfigProvider` is consumed by Phase 2 files. Phase 2 files cannot be touched. The right call is to keep the file and document the divergence explicitly.

3. **What does this design solve that wasn't asked?** `platform_aws_integration_test.go` deletion — it tests the deleted modules and must go, even though it wasn't listed in the 8 original files. This is correctness, not scope-creep.

**Top 3 doubts for adversarial review:**
1. Does `platform.dns`'s `provider: aws` guard (runtime init error) fire before or after `platform.dns` is successfully registered as a module? If registered successfully then the engine doesn't emit the module-guard error — the DNS module would silently skip Route53 operations. The init error must fire hard.
2. Does `schema/testdata/editor-schemas.golden.json` need manual update or is it auto-regenerated? Getting this wrong causes test failures.
3. Is there a `cloud_account_integration_test.go` that tests `ValidateCredentials()` (which calls `AWSConfig()`)? Yes — and it stays (correctly) because those functions stay in core.

---

## Implementation plan (preview — full plan written by writing-plans skill)

Single PR, 6 tasks:

**T1 — Delete 14 files; partially edit api_gateway_test.go and app_container.go; add regression gate**
Delete (14 full deletions):
- module/platform_ecs.go, module/platform_ecs_test.go, module/pipeline_step_ecs.go
- module/platform_apigateway.go, module/platform_apigateway_test.go, module/pipeline_step_apigateway.go
- module/aws_api_gateway.go
- module/platform_autoscaling.go, module/platform_autoscaling_test.go, module/pipeline_step_autoscaling.go
- module/platform_networking.go, module/platform_networking_test.go, module/pipeline_step_networking.go
- module/platform_aws_integration_test.go

Partial edits (NOT deleted):
- module/api_gateway_test.go: remove `TestAWSAPIGateway_Basic`, `TestAWSAPIGateway_SyncRoutesStub`, `TestAWSAPIGateway_SyncRoutesRequiresAPIID`; keep all 19 generic HTTP gateway tests.
- **module/app_container.go** (C-3 fix): `app_container.go` references both `PlatformECS` (type switch at line 130) and `ECSContainer` (used in `ECSAppTaskDef.Containers` at lines 88, 639). `ECSContainer` is defined in `platform_ecs.go` (line 39) — deleting that file causes a compile failure. Additionally, after removing `case *PlatformECS:`, these ECS-specific declarations in `app_container.go` become dead code: `ECSAppManifests`, `ECSAppTaskDef`, `ECSAppServiceCfg`, `ecsAppBackend`, `buildECSManifests()`, `ECSContainer` (if moved in). Dead code should be removed, not left in place.
  - Remove all ECS-specific declarations from `app_container.go`: `ECSAppManifests`, `ECSAppTaskDef`, `ECSAppServiceCfg` (struct types at lines 77-97), `ecsAppBackend` struct + all its methods (lines 590-648), `buildECSManifests()` function (lines 628-648). `ECSContainer` is NOT moved in — it becomes dead along with the above and is deleted.
  - Remove `case *PlatformECS: m.backend = &ecsAppBackend{}; m.platformType = "ecs"` from the `Init()` type switch (line 130).
  - Update the default-case error message at line 134 to remove the `platform.ecs` reference: `"environment %q is not a platform.kubernetes module (got %T); platform.ecs was removed — use infra.container_service with workflow-plugin-aws"`.
  - **Result**: `app_container.go` supports only `platform.kubernetes` backends post-deletion; ECS manifest generation is completely removed. No AWS SDK imports are introduced. `app_container.go` compiles cleanly.

New: module/aws_absent_test.go (regression gate using filepath.WalkDir for the 3 freed service packages).

**T2 — Replace platform_dns_backends.go with mock-only version + error stub**
New file content: keep mockDNSBackend; delete route53Backend; add `awsRoute53ErrorBackend` (a struct implementing `dnsBackend`) that returns the migration error from all methods. Alternative considered: simply unregister "aws" from dnsBackendRegistry — rejected because the existing `"unsupported provider"` generic error is not actionable; the migration error must name `infra.dns` + `workflow-plugin-aws`. Update `platform_dns.go`'s `init()`: replace `return &route53Backend{}, nil` with `return &awsRoute53ErrorBackend{}, nil` (no import change needed — types stay in the same package). Update `schema/module_schema.go` platform.dns ConfigFieldDef for provider description (see T3). Add test in `module/platform_dns_test.go`: assert `provider: aws` returns error containing "infra.dns" and "workflow-plugin-aws".

**T3 — Strip registration sites, add infra.autoscaling_group, regenerate golden**
Edit these files:
- `plugins/platform/plugin.go`: drop 4 module + 15 step factories/type strings
- `plugins/platform/plugin_test.go`: drop 4 module + 15 step type assertions
- `plugins/infra/plugin.go`: ADD `"infra.autoscaling_group"` to infraTypes slice
- `schema/schema.go`: drop 4 platform.* module type strings
- `schema/module_schema.go`: drop 4 platform.* schemas; UPDATE platform.dns provider description. (`infra.autoscaling_group` schema auto-generated from infraTypes — no manual schema entry needed here.)
- `schema/step_schema_builtins.go`: drop 15 step schema Register calls
- `cmd/wfctl/type_registry.go`: drop 4 platform.* entries; ADD infra.autoscaling_group entry
- `module/multi_region.go:117`: update error message (platform.ecs → infra.container_service)
- `DOCUMENTATION.md`: remove 4 module rows + 15 step rows; **keep** platform.dns row and step.dns_* rows (3 rows); add paragraph pointing at workflow-plugin-aws; add infra.autoscaling_group row
- Regenerate `schema/testdata/editor-schemas.golden.json` via `UPDATE_GOLDEN=1 go test ./schema/ -run TestEditorSchemasGoldenFile`

**T4 — Add internal/legacyaws + migration errors**
New `internal/legacyaws/types.go` (4 module types, 15 step types, DNS provider error). Wire into:
- `engine.go` (module guard — existing legacydo pattern, add legacyaws check)
- `module/pipeline_step_registry.go` (step guard — add legacyaws check after legacydo check)
- `cmd/wfctl/validate.go` (module guard + step guard + DNS provider: aws sweep)
- `cmd/wfctl/ci_validate.go` (same additions)
Tests: engine path for all 4 module types; step path for all 15 step types; validate path for 4 module types; DNS provider: aws guard for both validate.go and ci_validate.go.

**T5 — wfctl modernize rule + migration doc**
New modernize/legacy_aws_rule.go. New docs/migrations/v0.53.0-aws-iac-removal.md. Register rule in modernize/rules.go.

**T6 — go mod tidy + CI grep gate**
Run `go mod tidy` on root AND `(cd example && go mod tidy)` (example/go.mod lists service/apigatewayv2 + service/applicationautoscaling as indirect; they must drop). Verify service/apigatewayv2, service/applicationautoscaling, service/route53 drop from BOTH go.mod and example/go.mod.

Add CI grep gate (two parts — mirrors #617's godo-banned gate):
```sh
# *.go files must not import the freed service packages:
! grep -rn --include="*.go" \
    --exclude-dir=_worktrees \
    --exclude-dir=.worktrees \
    --exclude-dir=.claude \
    --exclude="aws_absent_test.go" \
    "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" .

# go.mod files must not list the freed service packages:
! grep -qH "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" go.mod example/go.mod
```

---

## Rollback

- **Pre-merge:** revert branch; no consumer impact.
- **Post-merge, pre-tag:** revert PR; force a new minor without the change.
- **Post-tag:** consumers pin previous tag. Migration error guides them back. `cloud_account_aws.go` was never removed, so AWS credential resolution continues.

---

## Adversarial review history

### Cycle 1 (FAIL → revised) — 2026-05-13

- **C-1** `infra.autoscaling_group` missing from core infra type registry (plugins/infra/plugin.go, schema/module_schema.go, cmd/wfctl/type_registry.go) → **fixed**: T3 now adds it to all three sites.
- **C-2** `platform.dns` provider: aws guard only fires at runtime (Init()), bypassing `wfctl validate` → **fixed**: validate-path guard added to validate.go + ci_validate.go in T4; FormatDNSProviderAWSError added to internal/legacyaws.
- **I-1** Step count 15 not 16 (step.network_destroy never existed) → **fixed**: all "16" corrected to "15" throughout.
- **I-2** example/go.mod tidy + go.mod grep gate missing → **fixed**: T6 now explicitly runs `(cd example && go mod tidy)` and adds a second grep gate checking go.mod + example/go.mod.
- **I-3** platform.dns schema description not updated after Route53 backend removal → **fixed**: T3 now updates provider ConfigFieldDef description.
- **m-1** T1 file count ambiguous → **fixed**: explicit 14-deletion + 1-partial-edit list.
- **m-2** awsRoute53ErrorBackend vs simple unregister not justified → **fixed**: T2 now documents the rejected alternative and justification.
- **m-3** DOCUMENTATION.md DNS step rows not called out as staying → **fixed**: T3 explicitly says keep platform.dns row + step.dns_* rows.

### Cycle 2 (FAIL → revised) — 2026-05-13

- **C-3** `module/app_container.go` has `case *PlatformECS:` type switch (line 130) and uses `ECSContainer` struct (lines 88, 639) which is defined in `platform_ecs.go`. Deleting `platform_ecs.go` causes compile failure in `app_container.go`. This file was not in the original modification list. Additionally, after removing the `case *PlatformECS:` branch, `ECSAppManifests`, `ECSAppTaskDef`, `ECSAppServiceCfg`, `ecsAppBackend`, and `buildECSManifests()` all become dead code in `app_container.go`. → **Fixed**: T1 now includes `module/app_container.go` as a partial edit: remove ALL ECS-specific declarations (structs + methods + `buildECSManifests()`), remove `case *PlatformECS:` branch, update default error message. Result: `app_container.go` supports platform.kubernetes only; compiles cleanly; zero AWS SDK imports.

### Cycle 3 (PASS) — 2026-05-13

Bug-class scan:

| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Clean | Assumption 1 (infra.autoscaling_group parity) verified: workflow-plugin-aws v0.2.0 release notes confirm driver shipped. |
| Repo-precedent conflicts | Clean | Mirrors #617 godo pattern throughout. |
| YAGNI violations | Clean | awsRoute53ErrorBackend justified over simple unregister (actionable error message). |
| Missing failure modes | Clean | example/go.mod confirmed has the 3 freed packages as indirect; go mod tidy will drop them. |
| Security / privacy | Clean | Deletion removes SDK surface; no new auth boundaries introduced. |
| Rollback story | Clean | Pre/post-merge rollback documented. |
| Simpler alternative | Clean | Build-tag alternative considered and correctly rejected. |
| User-intent drift | Clean | Design solves exactly what #653 requests. |
| Over/under-decomposition | Clean | 6 tasks match complexity; each is ~5-30 min scope. |
| Verification-class mismatch | Clean | go build, go test, grep gate, golden regeneration all match their change classes. |
| Hidden serial deps | Clean | T1→T3 dep (delete before registration edit), T6 last (go mod tidy). All explicit. |
| Missing rollback wiring | Clean | Rollback section present and actionable. |

Additional refinements applied in cycle 3 (not findings, just precision):
- `schema/module_schema.go` entry for `infra.autoscaling_group` correctly noted as NOT needed — schema auto-generated from `infraTypes` in `plugins/infra/plugin.go:ModuleSchemas()`.
- `module/app_container.go` comment lines 14 and 147 added to string-update list.

**PASS — zero Critical findings. Design approved for writing-plans.**
