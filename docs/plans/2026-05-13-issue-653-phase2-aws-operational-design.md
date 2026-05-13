# Issue #653 Phase 2 — Audit and Dispose 4 AWS Operational-Tooling Files

**Date:** 2026-05-13
**Author:** Claude Code (Sonnet 4.6)
**Related:** PR #657 (Phase 1), `docs/plans/2026-05-13-issue-653-phase1-aws-cutover-design.md`
**Issue:** https://github.com/GoCodeAlone/workflow/issues/653

---

## Executive Summary

Phase 2 audits 4 files in `module/` that import (or were suspected to import)
`aws-sdk-go-v2` but are NOT IaC providers. Outcome: 2 files need AWS-backend removal,
1 file needs no changes (no real SDK import), 1 file is exempted (no go.mod win and
step remains useful). Net: `service/codebuild` and `service/eks` removed from `go.mod`.

---

## 4-File Disposition

### File 1: `module/codebuild.go` (590 LOC) + `module/pipeline_step_codebuild.go` (291 LOC)

**Decision: STRIP AWS backend — replace `codebuildAWSBackend` with `codebuildAWSErrorBackend`.**

The file has two backends:
- `codebuildMockBackend` — pure in-memory, no SDK, fully functional for local dev
- `codebuildAWSBackend` — uses `aws-sdk-go-v2/service/codebuild` + `service/codebuild/types`

The `aws.codebuild` module type and 6 `step.codebuild_*` step types are registered in
`plugins/cicd/plugin.go`. These are operational CI/CD steps, not IaC. The mock backend
is the only backend needed in core. The real AWS backend belongs in `workflow-plugin-aws`.

**Pattern:** Mirrors Phase 1's `platform_dns.go` treatment — `awsRoute53ErrorBackend`
replaced the real Route53 backend. `codebuildAWSErrorBackend` replaces `codebuildAWSBackend`.

**SDK gain:** Drops `github.com/aws/aws-sdk-go-v2/service/codebuild` from `go.mod`
(only user). The companion `pipeline_step_codebuild.go` has no SDK imports — untouched.

**Error backend struct (confirmed compilable without SDK imports):**
```go
// codebuildAWSErrorBackend returns a migration error for all operations.
// It replaces codebuildAWSBackend after aws-sdk-go-v2/service/codebuild was removed from core.
type codebuildAWSErrorBackend struct{}

func (b *codebuildAWSErrorBackend) createProject(_ *CodeBuildModule) error {
    return fmt.Errorf("aws.codebuild: AWS CodeBuild backend removed from workflow core " +
        "(issue #653, removed in %s). Install workflow-plugin-aws to use the real AWS backend. " +
        "Set provider: mock to continue using the in-memory mock backend. " +
        "See: https://github.com/GoCodeAlone/workflow-plugin-aws", legacyaws.RemovedInVersion)
}
// deleteProject, startBuild, getBuildStatus, getBuildLogs, listBuilds — identical error.
```
No `service/codebuild` or `service/codebuild/types` import needed.

**Test impact:** `codebuild_test.go` contains `TestCodeBuildAWSBackend*` tests that
directly construct `codebuildAWSBackend{}`. These must be converted to
`TestCodeBuildAWSErrorBackend*` tests that verify the migration error message is returned.
Mock backend test paths (the majority of 753 LOC) compile and pass unchanged.

---

### File 2: `module/pipeline_step_s3_upload.go` (228 LOC)

**Decision: KEEP as-is. Exempt from Phase 2.**

Reasoning:
1. `step.s3_upload` is a general-purpose pipeline utility step (uploads artifacts to
   any S3-compatible storage — AWS S3, MinIO, LocalStack, DO Spaces). NOT AWS-specific.
2. `service/s3` already has surviving users: `s3_storage.go`, `iac_state_spaces.go`.
   Deleting this file cannot remove `service/s3` from `go.mod`.
3. `awsconfig/config` has surviving users: `cloud_account_aws.go`, `s3_storage.go`,
   `iac_state_spaces.go`. Removing this file cannot drop `config` from `go.mod`.
4. The step already has a clean `s3PutObjectAPI` interface for client injection (mock
   in tests). The MinIO/LocalStack `endpoint` path works without AWS credentials.

**No go.mod win, no architectural reason to remove.** This file is retained.

---

### File 3: `module/nosql_dynamodb.go` (95 LOC)

**Decision: KEEP as-is. No SDK import exists.**

The task description was incorrect about this file importing aws-sdk-go-v2. Inspecting
the actual import block shows only `context`, `fmt`, and `modular`. The AWS SDK packages
(`service/dynamodb`, `feature/dynamodb/attributevalue`) appear only in a Go doc comment:

```go
// Full AWS DynamoDB implementation would use:
//   - github.com/aws/aws-sdk-go-v2/service/dynamodb
```

The real AWS implementation is explicitly stubbed with:
```go
return fmt.Errorf("nosql.dynamodb %q: real AWS endpoint not yet implemented; use endpoint: local for testing", d.name)
```

The `nosql.dynamodb` module type is registered in `plugins/datastores/plugin.go` and
provides a useful in-memory mock for local dev. No changes needed.

**Bonus cleanup:** `service/dynamodb` appears in `go.mod` as a direct dependency despite
no actual importer. After Phase 2, `go mod tidy` drops it as a phantom dep. This is a
free side-effect of the `go mod tidy` run in T3, not a separate task.

---

### File 4: `module/platform_kubernetes_kind.go` (845 LOC)

**Decision: STRIP EKS backend — replace `eksBackend` with `eksErrorBackend`.**

The filename is misleading. The file actually contains 4 backends:
- `kindBackend` — pure in-memory mock (no SDK, no external calls), also used for `k3s`
- `eksBackend` — Amazon EKS via `aws-sdk-go-v2/service/eks` (REAL AWS SDK, to drop)
- `gkeBackend` — GKE via `google.golang.org/api/container/v1` (GCP API, stays)
- `aksBackend` — AKS via Azure Management REST API over `net/http` (no Azure SDK, stays)

The `eksBackend` is an IaC-adjacent capability (EKS = cloud-managed K8s cluster = infra).
It is the only real AWS SDK user in the file.

**Pattern:** Replace `eksBackend` with `eksErrorBackend` that returns a migration error
on `plan()`, `apply()`, `status()`, and `destroy()`. All other backends (`kind`, `k3s`,
`gke`, `aks`) remain fully functional.

**SDK gain:** Drops `github.com/aws/aws-sdk-go-v2/service/eks` from `go.mod` (only user).
Also drops `github.com/aws/aws-sdk-go-v2/aws` import from this file — **confirmed**:
`gkeBackend` uses `google.golang.org/api/container/v1` and `option` exclusively;
`aksBackend` uses `net/http`, `encoding/json`, `io`, `bytes`, and `net/url` exclusively.
Neither `gkeBackend` nor `aksBackend` calls `aws.String()`, `aws.ToString()`, or
any other `aws.*` helper. The `aws` package import in the file is used solely in
`eksBackend` methods (`aws.String(k.clusterName())`, `aws.Int32(...)`, etc.).

**Confirmed:** `cloud_account_aws.go` imports `aws-sdk-go-v2/aws`, so removing it from
`platform_kubernetes_kind.go` does NOT remove `aws` from `go.mod`.

**Error backend struct (confirmed compilable without SDK imports):**
```go
type eksErrorBackend struct{}

func (b *eksErrorBackend) plan(_ *PlatformKubernetes) (*PlatformPlan, error) {
    return nil, fmt.Errorf("eks cluster backend removed from workflow core (issue #653, " +
        "removed in %s). Install workflow-plugin-aws to manage EKS clusters. " +
        "See: https://github.com/GoCodeAlone/workflow-plugin-aws", legacyaws.RemovedInVersion)
}
// apply, status, destroy — identical error.
```

**Error message:**
```
eks cluster backend has been removed from workflow core (issue #653, removed in v0.53.0).
Install workflow-plugin-aws v0.3.0+ to manage EKS clusters.
See: https://github.com/GoCodeAlone/workflow-plugin-aws
```

---

## SDK Packages Dropped by Phase 2

| Package | Dropped? | Notes |
|---|---|---|
| `service/codebuild` | YES | Only user: `codebuild.go` `codebuildAWSBackend` |
| `service/eks` | YES | Only user: `platform_kubernetes_kind.go` `eksBackend` |
| `service/dynamodb` | YES (via `go mod tidy`) | Never actually imported — was in go.mod as phantom dep |
| `service/s3` | NO | Surviving users: `s3_storage.go`, `iac_state_spaces.go` |
| `awsconfig/config` | NO | Surviving users: `cloud_account_aws.go`, `s3_storage.go` |

---

## Files Changed

| File | Action |
|---|---|
| `module/codebuild.go` | Replace `codebuildAWSBackend` with `codebuildAWSErrorBackend`; remove `service/codebuild` + `codebuild/types` imports |
| `module/platform_kubernetes_kind.go` | Replace `eksBackend` with `eksErrorBackend`; remove `service/eks` + `eks/types` + `aws` imports |
| `module/pipeline_step_s3_upload.go` | No change |
| `module/nosql_dynamodb.go` | No change |
| `internal/legacyaws/types.go` | No change. Error messages inlined in error backend structs (see struct shapes above). `legacyaws` tracks removed module/step *types*; `aws.codebuild` and `platform.kubernetes` types are NOT removed — only their AWS backends are stubbed. |
| `go.mod` + `go.sum` | Drop `service/codebuild`, `service/eks`, `service/dynamodb` (via `go mod tidy`) |
| `.github/workflows/ci.yml` | Extend `aws-sdk-banned` gate to also ban `service/codebuild` and `service/eks` |
| `module/codebuild_test.go` | Convert `TestCodeBuildAWSBackend*` tests → `TestCodeBuildAWSErrorBackend*` that verify migration error message. Mock backend tests (majority of 753 LOC) unchanged. |

---

## Architecture

This is a **backend-stub pattern** (Phase 1 precedent: `awsRoute53ErrorBackend`).

```
codebuild.go:
  Before:  codebuildMockBackend | codebuildAWSBackend (real EKS calls)
  After:   codebuildMockBackend | codebuildAWSErrorBackend (returns migration error)

platform_kubernetes_kind.go:
  Before:  kindBackend | eksBackend (real EKS calls) | gkeBackend | aksBackend
  After:   kindBackend | eksErrorBackend (returns migration error) | gkeBackend | aksBackend
```

The `codebuild.go` `Init()` function selects backend by `provider` config key:
- `provider: mock` (or no account configured) → `codebuildMockBackend` (unchanged)
- `provider: aws` → `codebuildAWSErrorBackend` (was `codebuildAWSBackend`)

The `platform.kubernetes` `Init()` function selects backend from registry by `cluster_type`:
- `kind`, `k3s` → `kindBackend` (unchanged)
- `gke` → `gkeBackend` (unchanged)
- `aks` → `aksBackend` (unchanged)
- `eks` → `eksErrorBackend` (was `eksBackend`)

---

## Assumptions

1. `service/codebuild` has no other importers in the codebase — verified by grep.
2. `service/eks` has no other importers in the codebase — verified by grep.
3. `service/dynamodb` truly has no real importer (only appears in a doc comment) —
   verified by inspecting the `nosql_dynamodb.go` import block directly.
4. `aws.codebuild` module mock backend remains useful for local-dev testing without
   AWS credentials — keeping it does not create a migration blocker.
5. `platform.kubernetes` `kind`/`gke`/`aks` backends have no AWS SDK dependencies —
   verified by reading `platform_kubernetes_kind.go` import block.
6. The CI `aws-sdk-banned` grep gate pattern (from Phase 1) can be extended to cover
   the 2 newly removed packages without breaking the existing gate for the 3 Phase 1 packages.
7. `codebuild_test.go` tests the mock backend paths; they will still compile and pass
   after replacing the AWS backend. `TestCodeBuildAWSBackend*` tests must be converted
   to `TestCodeBuildAWSErrorBackend*` that verify the migration error text.
8. `module/codebuild.go` and `module/platform_kubernetes_kind.go` may import
   `internal/legacyaws` for `RemovedInVersion`. Import cycle check: `internal/legacyaws`
   is a leaf package (imports only `fmt`, `sort`, `strings`) — no reverse deps from module/.
   The import is cycle-free.

---

## Rollback

This PR has no runtime impact on users who do not configure `provider: aws` in an
`aws.codebuild` module or `cluster_type: eks` in a `platform.kubernetes` module.
Users who do use these providers already get AWS credential failures in production;
replacing the failure mode (SDK error → explicit migration error) is an improvement,
not a regression.

**Rollback steps (if needed):**
1. `git revert <merge-sha>` — restores both backends and CI gate additions
2. Run `go mod tidy` — restores `service/codebuild`, `service/eks`, `service/dynamodb` entries
3. Verify CI passes (the reverted commit also reverts the aws-sdk-banned gate extensions, so CI won't fail on restored imports)

---

## Migration Guide for Users

### `aws.codebuild` module (provider: aws)

Before (workflow ≤ v0.52.x):
```yaml
modules:
  - name: my_builds
    type: aws.codebuild
    config:
      account: my_aws_account
      provider: aws
      region: us-east-1
```

After (workflow v0.53.0+): Load `workflow-plugin-aws` (Phase 3, version TBD) and use the
same config. The `aws.codebuild` module type and all `step.codebuild_*` step types will be
provided by the plugin unchanged. No config key changes required.

Until workflow-plugin-aws adds the AWS CodeBuild backend, use `provider: mock` for local
development and testing.

### `platform.kubernetes` cluster with `cluster_type: eks`

Before:
```yaml
modules:
  - name: my_cluster
    type: platform.kubernetes
    config:
      account: my_aws_account
      cluster_type: eks
      role_arn: arn:aws:iam::123456789012:role/EKSRole
```

After (workflow v0.53.0+): Load `workflow-plugin-aws` (Phase 3, version TBD). The EKS
backend will be provided by the plugin. Config keys are unchanged. Until the plugin ships
the EKS backend, use `cluster_type: kind` for local development.
