# Design: Issue #653 Phase 3 — Disposition of `platform/providers/aws/` Tree

**Date:** 2026-05-13
**Issue:** [#653](https://github.com/GoCodeAlone/workflow/issues/653)
**Branch:** `feat/issue-653-phase3-aws-drivers`
**Prior phases:**
- Phase 1 (PR #657): Removed legacy `platform.aws_*` module types from the engine core.
- Phase 2 (PR #659): Stripped AWS SDK from `codebuild` and `eks` backends in `module/platform_kubernetes_kind.go`.

---

## Architectural Question

Are `platform/providers/aws/` and `provider/aws/` (the two in-scope trees):

- **(a) REDUNDANT** with `workflow-plugin-aws` v1.0.0? → delete from core
- **(b) SERVING A DIFFERENT LAYER** (core's `platform.Provider` / `provider.CloudProvider` interfaces before the plugin abstraction)? → keep with documentation
- **(c) DUAL-USE INTERMEDIATE** (used by both core wfctl and the plugin)? → refactor into shared

---

## Consumer Trace (Evidence)

### Tree 1: `platform/providers/aws/` (build tag `//go:build aws`)

**Interface implemented:** `platform.Provider` (defined in `platform/provider.go`)

The `platform.Provider` interface is a **capability-based declarative infrastructure abstraction** distinct from `interfaces.IaCProvider`. It supports:
- `Capabilities() []CapabilityType`
- `MapCapability(decl CapabilityDeclaration, pctx *PlatformContext) ([]ResourcePlan, error)`
- `ResourceDriver(resourceType) (platform.ResourceDriver, error)`

**Who calls `platform.Provider`?**
- `handlers/platform.go` — `PlatformWorkflowHandler.ConfigureWorkflow` looks up `"platform.provider"` from the service registry and assigns it
- `module/pipeline_step_platform_plan.go` — type-asserts context key to `platform.Provider`
- `module/pipeline_step_platform_apply.go` — same
- `module/pipeline_step_platform_destroy.go` — same
- `module/pipeline_step_drift_check.go` — same
- `module/platform_reconciliation_trigger.go` — type-asserts service to `platform.Provider`

**Who imports `platform/providers/aws`?**
- **Nobody.** The only import of the package's sub-tree is `driver_factories.go` importing `platform/providers/aws/drivers` — self-referential within the package. Zero external consumers.

**How is a `platform.Provider` of type "aws" instantiated at runtime?**
- No code in the codebase creates an `aws.NewProvider()` (from this package) outside its own test.
- The `platform.provider` module type in `plugins/platform/plugin.go` creates a `module.NewServiceModule()` — a generic service holder, not an AWS-specific provider.
- The `PlatformWorkflowHandler` accepts any `platform.Provider` injected via service registry, but no code injects the `platform/providers/aws` implementation.

**Build tag implication:**
Every file in `platform/providers/aws/` carries `//go:build aws`. The normal `go test ./...` and `go build ./...` in CI (no `-tags aws`) **never compile this code.** No CI job uses the `aws` build tag.

### Tree 2: `provider/aws/` (no build tag)

**Interface implemented:** `provider.CloudProvider` (defined in `provider/provider.go`)

This is the **deploy-pipeline AWS adapter**:
- `Deploy(ctx, DeployRequest) (*DeployResult, error)` — ECS Fargate + EKS routing
- `GetDeploymentStatus`, `Rollback`, `TestConnection`, `GetMetrics`, `PushImage`, etc.
- Registered via `init()` → `plugin.RegisterNativePluginFactory` (loaded unconditionally in `cmd/server/main.go`)

**Who calls `provider.CloudProvider`?**
- `module/pipeline_step_deploy.go` — the `step.deploy_rolling` pipeline step
- `deploy/executor/executor.go` — the deployment executor
- `cmd/server/main.go` — side-effect import `_ "github.com/GoCodeAlone/workflow/provider/aws"`

**Key distinction:** `provider.CloudProvider` is a container deployment abstraction (ECS/EKS services), not an IaC resource provisioner. It is orthogonal to both `platform.Provider` and `interfaces.IaCProvider`.

### Tree 3: `platform/providers/aws/drivers/` (subdirectory of Tree 1)

These are `platform.ResourceDriver` implementations (not `interfaces.ResourceDriver`) for:
`aws.eks_cluster`, `aws.eks_nodegroup`, `aws.vpc`, `aws.rds`, `aws.sqs`, `aws.iam`, `aws.alb`

All carry `//go:build aws`. All are only imported by `platform/providers/aws/driver_factories.go`.

---

## Disposition Analysis

### `platform/providers/aws/` and `platform/providers/aws/drivers/` → Disposition **(b): SERVING A DIFFERENT LAYER, but unreachable**

The `platform.Provider` layer is a legitimate, actively-used interface (consumed by 5+ pipeline steps and the reconciliation trigger). `DockerComposeProvider` (in `platform/providers/dockercompose/`) and `MockProvider` (in `platform/providers/mock/`) are live, tested implementations.

However, the AWS implementation specifically is:
1. **Dead code in practice** — zero external callers; never compiled without `-tags aws`; no CI exercises it; no YAML config example uses it
2. **Not superseded by `workflow-plugin-aws`** — the plugin implements `interfaces.IaCProvider`; the in-core `platform.Provider` interface is a separate, parallel abstraction for the `platform.*` module system
3. **Not a migration stub** — it is a full (non-trivial) implementation that diverged from `interfaces.IaCProvider` semantics

The correct disposition is: **document the layer boundary clearly, then tombstone (delete) the dead AWS implementation** because:
- It cannot be exercised by users (build-tag-gated, no wiring)
- It duplicates AWS SDK dependencies in a branch no CI validates
- It is a maintenance burden: future AWS SDK upgrades require keeping this code compilable even though no test runs it
- The `platform.*` module system (for the general interface) is still valid; what is dead is specifically the AWS implementation

### `provider/aws/` → **Keep as-is**

This is live, tested, and wired. It serves a completely different purpose (ECS/EKS deploy pipeline) than either `platform/providers/aws` or `workflow-plugin-aws`. Phase 3 does NOT touch `provider/aws/`.

---

## Proposed Design

### Option A (Recommended): Tombstone the AWS `platform.Provider` implementation + document the layer

**What changes:**
1. Delete `platform/providers/aws/` (all files including `drivers/` subdirectory) — 24 files, ~2,000 LOC
2. Add doc comment to `platform/provider.go` explaining the two-layer architecture:
   - `platform.Provider` (in-core, used by `platform.*` module system and pipeline steps)
   - `interfaces.IaCProvider` (gRPC plugin boundary, used by wfctl `infra.*` command suite)
   - `provider.CloudProvider` (deploy pipeline, used by `step.deploy_rolling`)
3. Promote `service/eks` from the lenient CI step ("must only appear in platform/ and provider/") into the strict ban step AND the `go.mod` gate — the Phase 2 comment at `.github/workflows/ci.yml:417–418` explicitly delegates this to Phase 3. Additionally, add the AWS SDK packages that are exclusive to `platform/providers/aws/` (ec2, dynamodb, elasticloadbalancingv2, rds, sqs, iam) to the banned packages list. `service/eks` stays in go.mod because `provider/aws/` legitimately uses it.
4. Write an ADR `decisions/0032-platform-provider-aws-tombstone.md` explaining the tombstone decision and the layer boundary

**What does NOT change:** `provider/aws/` (unchanged), `platform.Provider` interface (unchanged), `DockerComposeProvider`, `MockProvider`, pipeline steps, reconciliation trigger.

### Option B: Keep the AWS `platform.Provider` implementation with documentation only

**Reasoning:** The `platform.Provider` interface is valid. The AWS implementation is complete. Users could theoretically use it with `-tags aws` builds.

**Why not recommended:**
- No user has used it — confirmed by absence of any example YAML, no docs, no CI exercise
- The `-tags aws` build path is completely undocumented; no setup guide exists
- Maintaining two parallel AWS abstractions (`platform.Provider` vs `interfaces.IaCProvider`) creates confusion for contributors
- The AWS SDK packages used here (`ec2`, `dynamodb`, `s3`, `sts`, `iam`, `elasticloadbalancingv2`, `rds`, `sqs`) are not listed in `go.mod` because the build tag prevents them from being included — they would need to be added to use this code, which is a non-trivial backward-compatibility change

### Option C: Migrate drivers to `interfaces.ResourceDriver` and absorb into plugin

Not viable without:
- A separate `workflow-plugin-aws` PR (out of scope for core)
- Full `interfaces.ResourceDriver` semantics (different signature from `platform.ResourceDriver`)
- `workflow-plugin-aws` already has its own implementations of all 7 resource types

---

## Chosen Disposition

**Option A: Tombstone the dead AWS `platform.Provider` implementation.**

This is architectural cleanup, not force-cutover. The `platform.Provider` interface is preserved. `DockerComposeProvider` and `MockProvider` remain. Only the AWS-specific, build-tag-gated, zero-consumer implementation is removed.

---

## Scope

### In Scope (Phase 3)
- Delete `platform/providers/aws/` directory (24 files)
- Add architectural layer-boundary doc comment to `platform/provider.go`
- Add ADR `decisions/0032-platform-provider-aws-tombstone.md`
- Promote `service/eks` CI gate: move from lenient-allowed-in-platform step to strict ban step + go.mod gate (Phase 2 CI comment at ci.yml:417–418 hands this off to Phase 3)
- Add banned packages exclusive to the deleted tree: `service/ec2`, `service/dynamodb`, `service/elasticloadbalancingv2`, `service/rds`, `service/sqs`, `service/iam` — these are not present in `provider/aws/` or anywhere else

### Out of Scope (Phase 3)
- `provider/aws/` — no changes
- `platform/providers/dockercompose/` — no changes
- `platform/providers/mock/` — no changes
- `platform.Provider` interface — no changes
- Any changes to `workflow-plugin-aws` repository

---

## Assumptions

1. **No user builds with `-tags aws`** — confirmed by: no CI job uses this tag, no example config, no documentation mentions it. If this assumption is false, the tombstone would break a hidden build path. Mitigation: the ADR records the rationale so future maintainers understand why it was removed.
2. **`workflow-plugin-aws` is the canonical AWS IaC path** — confirmed by Phase 1 design doc and issue #653 mandate.
3. **`platform.Provider` interface is preserved** — confirmed: `DockerComposeProvider` and the pipeline step consumers remain.
4. **AWS SDK packages exclusive to this tree are not in go.mod/go.sum** — `ec2`, `dynamodb`, `elasticloadbalancingv2`, `rds`, `sqs`, `iam` are only used by the build-tag-gated `platform/providers/aws/` tree. `service/eks` IS in go.mod because `provider/aws/plugin.go` (no build tag) uses it; it must not be removed from go.mod in Phase 3 since `provider/aws/` is kept. `service/s3` also needs verification: check if it appears outside this tree.
5. **`service/eks` promotion is safe** — after `platform/providers/aws/drivers/eks_cluster.go` and `eks_nodegroup.go` are deleted, the only remaining callers of `service/eks` are in `provider/aws/` (deploy pipeline). The Phase 2 CI gate correctly anticipates this: the lenient step (`--exclude-dir=platform`) can be tightened to remove the `--exclude-dir=platform` exclusion, because the only remaining legitimate `eks` caller (`provider/aws/`) is still excluded by `--exclude-dir=provider`.

---

## Rollback

This phase does not affect runtime. All deleted code is build-tag-gated (`//go:build aws`) and never compiled in production builds. Rollback is `git revert <merge-sha>` with no service restart required.

---

## Self-Challenge Round

**1. Laziest plausible solution:** Add a single `// DEAD CODE — not compiled; see ADR 0020` comment to `platform/providers/aws/provider.go` and leave the files in place. This avoids deletion work but leaves the maintenance burden and confusion. Not recommended: dead code left in place accumulates over time, and this tree has no recovery path.

**2. Most fragile assumption:** "No user builds with `-tags aws`." If a downstream consumer uses this build tag in their CI, the tombstone breaks them. Evidence against: the tag is undocumented, no CI exercises it, no example YAML uses it. The ADR records the decision so they can pinpoint why.

**3. YAGNI sweep:** Option C (absorb into plugin) is YAGNI — it would require coordinating changes to `workflow-plugin-aws` and creating a shared package that neither currently needs.

**4. Partial failure:** The only partial failure risk is `go mod tidy` producing unexpected results if any of the AWS SDK packages were pulled in transitively. This is mitigated by running `go mod tidy` and verifying `go.sum` in the implementation task.

**5. Repo precedent conflict:** Phase 1 removed `platform.aws_*` modules; Phase 2 removed EKS/codebuild backends. This Phase 3 deletion is consistent with the issue #653 mandate. The precedent is clearly established by the two prior phases.
