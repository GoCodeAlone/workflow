# Issue #653 Phase 3 — Tombstone `platform/providers/aws/` and promote `service/eks` CI gate

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Delete the dead, build-tag-gated `platform/providers/aws/` tree from workflow core, remove its exclusive AWS SDK dependencies from `go.mod`, promote the `service/eks` CI gate from lenient to strict, and document the three-layer provider architecture in `platform/provider.go`.

**Architecture:** All 24 files in `platform/providers/aws/` carry `//go:build aws` and have zero external callers. They implement `platform.Provider` (not `interfaces.IaCProvider`), which is a separate, live interface with `DockerComposeProvider` and `MockProvider` as its remaining implementations. Deleting the dead AWS implementation frees 6 exclusive AWS SDK dependencies from `go.mod`, allows the Phase 2 CI gate placeholder for `service/eks` to be promoted to strict enforcement, and eliminates future maintenance burden for code that CI never compiles.

**Tech Stack:** Go, `go mod tidy`, GitHub Actions CI YAML

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~2,100 deleted, ~80 added (net ~-2,020)

**Out of scope:**
- `provider/aws/` (deploy-pipeline ECS/EKS adapter) — no changes
- `platform/providers/dockercompose/` — no changes
- `platform/providers/mock/` — no changes
- `platform.Provider` interface (`platform/provider.go`) — interface unchanged; doc comment added only
- Any changes to `workflow-plugin-aws` repository

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(#653): Phase 3 — tombstone platform/providers/aws/ + promote eks CI gate | Task 1, Task 2, Task 3, Task 4 | feat/issue-653-phase3-aws-drivers |

**Status:** Locked 2026-05-13T18:30:00Z

---

### Task 1: Delete `platform/providers/aws/` and its `drivers/` subtree

**Files:**
- Delete: `platform/providers/aws/aws_config.go`
- Delete: `platform/providers/aws/capability_mapper.go`
- Delete: `platform/providers/aws/credential_broker.go`
- Delete: `platform/providers/aws/credential_broker_test.go`
- Delete: `platform/providers/aws/driver_factories.go`
- Delete: `platform/providers/aws/provider.go`
- Delete: `platform/providers/aws/provider_test.go`
- Delete: `platform/providers/aws/state_store.go`
- Delete: `platform/providers/aws/state_store_test.go`
- Delete: `platform/providers/aws/drivers/alb.go`
- Delete: `platform/providers/aws/drivers/alb_test.go`
- Delete: `platform/providers/aws/drivers/eks_cluster.go`
- Delete: `platform/providers/aws/drivers/eks_cluster_test.go`
- Delete: `platform/providers/aws/drivers/eks_nodegroup.go`
- Delete: `platform/providers/aws/drivers/eks_nodegroup_test.go`
- Delete: `platform/providers/aws/drivers/helpers.go`
- Delete: `platform/providers/aws/drivers/iam.go`
- Delete: `platform/providers/aws/drivers/iam_test.go`
- Delete: `platform/providers/aws/drivers/rds.go`
- Delete: `platform/providers/aws/drivers/rds_test.go`
- Delete: `platform/providers/aws/drivers/sqs.go`
- Delete: `platform/providers/aws/drivers/sqs_test.go`
- Delete: `platform/providers/aws/drivers/vpc.go`
- Delete: `platform/providers/aws/drivers/vpc_test.go`

**Step 1: Write the regression gate first (TDD)**

Before deleting anything, add the absent-package assertion for the packages that will be freed to `module/aws_absent_test.go`. This gate is scoped to `module/` only and confirms no module code imports these packages. Run it now to confirm it passes (all clean before deletion):

```go
// In module/aws_absent_test.go, add to the freed slice:
"aws-sdk-go-v2/service/ec2",
"aws-sdk-go-v2/service/dynamodb",        // nosql_dynamodb.go only references it in a comment, not an import
"aws-sdk-go-v2/service/elasticloadbalancingv2",
"aws-sdk-go-v2/service/rds",
"aws-sdk-go-v2/service/sqs",
```

Note: `service/iam` and `service/eks` are intentionally NOT added here because `iam/aws.go`, `plugin/rbac/aws.go`, and `provider/aws/` legitimately import them.

**Step 2: Run regression gate (must PASS before deletion)**

```bash
cd /Users/jon/workspace/workflow && go test ./module/ -run TestAWSServicePackagesAbsent -v
```

Expected: `PASS` — no module file imports ec2/dynamodb/elasticloadbalancingv2/rds/sqs

**Step 3: Delete all 24 files**

```bash
rm -rf /Users/jon/workspace/workflow/platform/providers/aws
```

**Step 4: Verify `platform.Provider` interface file and remaining providers untouched**

```bash
ls /Users/jon/workspace/workflow/platform/providers/
```

Expected output: `dockercompose/  mock/` (no `aws/` directory)

```bash
test -f /Users/jon/workspace/workflow/platform/provider.go && echo "interface present"
```

Expected: `interface present`

**Step 5: Run go build to confirm no broken imports**

```bash
cd /Users/jon/workspace/workflow && go build ./...
```

Expected: exit 0, no errors. The `//go:build aws` tag means these files were never compiled in normal builds, so there should be zero breakage.

**Step 6: Run module-level tests**

```bash
cd /Users/jon/workspace/workflow && go test ./platform/... -v 2>&1 | tail -20
```

Expected: all tests pass; `DockerComposeProvider` and `MockProvider` tests still green.

**Step 7: Commit**

```bash
cd /Users/jon/workspace/workflow && git add -A platform/providers/aws/ module/aws_absent_test.go && git commit -m "feat(#653): T1 — delete platform/providers/aws/ + add absent-package gate"
```

Rollback: `git revert <sha>` (no service restart needed; deleted code was build-tag-gated and never compiled in production).

---

### Task 2: Remove exclusive AWS SDK dependencies from `go.mod` and `go.sum`

**Files:**
- Modify: `go.mod` (remove 6 exclusive dependencies)
- Modify: `go.sum` (updated by `go mod tidy`)

**Step 1: Verify which packages are now unused**

The following packages were imported ONLY by `platform/providers/aws/` (confirmed: no other non-tag-gated callers exist):
- `github.com/aws/aws-sdk-go-v2/service/ec2` — VPCDriver
- `github.com/aws/aws-sdk-go-v2/service/dynamodb` — AWSS3StateStore DynamoDB locking
- `github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2` — ALBDriver
- `github.com/aws/aws-sdk-go-v2/service/rds` — RDSDriver
- `github.com/aws/aws-sdk-go-v2/service/sqs` — SQSDriver

The following stay in `go.mod` because other code uses them (NOT removed by this task):
- `service/eks` — `provider/aws/` (deploy pipeline, 3 files)
- `service/iam` — `iam/aws.go` + `plugin/rbac/aws.go`
- `service/s3` — `module/iac_state_spaces.go`, `module/pipeline_step_s3_upload.go`, `artifact/s3.go`
- `service/sts` — `iam/aws.go`, `module/cloud_account_aws.go`, `provider/aws/plugin.go`

**Step 2: Run `go mod tidy`**

```bash
cd /Users/jon/workspace/workflow && go mod tidy
```

Expected: `go.mod` loses the 5 packages listed above (ec2, dynamodb, elasticloadbalancingv2, rds, sqs). `go.sum` is updated. `service/eks`, `service/iam`, `service/s3`, `service/sts` remain.

**Step 3: Verify the 5 packages are gone from `go.mod`**

```bash
grep -E "service/(ec2|rds|sqs|elasticloadbalancingv2|dynamodb)" /Users/jon/workspace/workflow/go.mod
```

Expected: no output (all 5 removed)

**Step 4: Verify the kept packages remain**

```bash
grep -E "service/(eks|iam|s3|sts)" /Users/jon/workspace/workflow/go.mod
```

Expected: 4 lines, one for each of eks, iam, s3, sts

**Step 5: Final build + test**

```bash
cd /Users/jon/workspace/workflow && go build ./... && go test ./... 2>&1 | tail -10
```

Expected: exit 0, all tests pass

**Step 6: Commit**

```bash
cd /Users/jon/workspace/workflow && git add go.mod go.sum && git commit -m "feat(#653): T2 — go mod tidy removes 5 exclusive AWS SDK deps (ec2/dynamodb/elb/rds/sqs)"
```

Rollback: `git revert <sha>` + `go mod tidy` to restore; no service restart needed.

---

### Task 3: Promote `service/eks` CI gate from lenient to strict + add exclusive packages to banned list

**Files:**
- Modify: `.github/workflows/ci.yml`

The Phase 2 CI gate at `.github/workflows/ci.yml:416–430` has a "lenient" step that excluded `platform/` and `provider/` from the `service/eks` ban. The comment at line 417–418 explicitly says:

> "When Phase 3 removes those callers, promote service/eks into the step above and add it to the go.mod gate as well."

After Task 1 deletes `platform/providers/aws/drivers/eks_cluster.go` and `eks_nodegroup.go`, the only remaining legitimate caller of `service/eks` is `provider/aws/` (deploy pipeline). The lenient step should now exclude only `provider/` (not `platform/`), and we should add `service/eks` to the strict banned list for everything outside `provider/`.

We also add `service/ec2`, `service/dynamodb`, `service/elasticloadbalancingv2`, `service/rds`, `service/sqs` to the strict banned packages (they are now exclusively absent from the codebase after Task 1+2).

**Step 1: Write the updated CI step content**

Replace the entire `aws-sdk-banned` job in `.github/workflows/ci.yml`:

```yaml
  aws-sdk-banned:
    name: Verify removed AWS SDK packages are not imported (issue #653)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Grep gate — no *.go file (repo-wide) may import fully-removed AWS service packages
        # Scans the whole repo. service/eks is allowed only in provider/ (ECS/EKS deploy pipeline).
        # platform/providers/aws/ was deleted in Phase 3; provider/aws/ (deploy pipeline) is kept.
        run: |
          ! grep -rn --include="*.go" \
              --exclude-dir=_worktrees \
              --exclude-dir=.worktrees \
              --exclude-dir=.claude \
              --exclude="aws_absent_test.go" \
              -e "aws-sdk-go-v2/service/apigatewayv2" \
              -e "aws-sdk-go-v2/service/applicationautoscaling" \
              -e "aws-sdk-go-v2/service/route53" \
              -e "aws-sdk-go-v2/service/codebuild" \
              -e "aws-sdk-go-v2/service/ec2" \
              -e "aws-sdk-go-v2/service/dynamodb" \
              -e "aws-sdk-go-v2/service/elasticloadbalancingv2" \
              -e "aws-sdk-go-v2/service/rds" \
              -e "aws-sdk-go-v2/service/sqs" \
              .
      - name: Grep gate — service/eks must only appear in provider/ (deploy pipeline)
        # platform/providers/aws/ was deleted in Phase 3; only provider/aws/ legitimately uses
        # service/eks for ECS/EKS deploy pipeline. Any new import outside provider/ is a regression.
        run: |
          ! grep -rn --include="*.go" \
              --exclude-dir=_worktrees \
              --exclude-dir=.worktrees \
              --exclude-dir=.claude \
              --exclude="aws_absent_test.go" \
              --exclude-dir="provider" \
              -e "aws-sdk-go-v2/service/eks" \
              .
      - name: Grep gate — go.mod files must not list removed AWS SDK packages
        # service/eks is intentionally omitted from this go.mod gate: provider/aws/ (deploy
        # pipeline) has a legitimate caller. ec2/dynamodb/elb/rds/sqs are added here because
        # platform/providers/aws/ (their only consumer) was deleted in Phase 3.
        run: |
          ! grep -qH \
              -e "aws-sdk-go-v2/service/apigatewayv2" \
              -e "aws-sdk-go-v2/service/applicationautoscaling" \
              -e "aws-sdk-go-v2/service/route53" \
              -e "aws-sdk-go-v2/service/ec2" \
              -e "aws-sdk-go-v2/service/dynamodb" \
              -e "aws-sdk-go-v2/service/elasticloadbalancingv2" \
              -e "aws-sdk-go-v2/service/rds" \
              -e "aws-sdk-go-v2/service/sqs" \
              go.mod example/go.mod
```

**Step 2: Verify the grep gate passes locally against the current state of the repo**

```bash
# Simulate the strict banned packages gate (should exit 0 = no matches = gate passes)
! grep -rn --include="*.go" \
    --exclude-dir=_worktrees \
    --exclude-dir=.worktrees \
    --exclude-dir=.claude \
    --exclude="aws_absent_test.go" \
    -e "aws-sdk-go-v2/service/ec2" \
    -e "aws-sdk-go-v2/service/dynamodb" \
    -e "aws-sdk-go-v2/service/elasticloadbalancingv2" \
    -e "aws-sdk-go-v2/service/rds" \
    -e "aws-sdk-go-v2/service/sqs" \
    /Users/jon/workspace/workflow && echo "gate passes"
```

Expected: `gate passes` (no matches)

```bash
# Simulate the eks gate: eks should only appear in provider/ — check nothing outside provider/ uses it
! grep -rn --include="*.go" \
    --exclude-dir=_worktrees \
    --exclude-dir=.worktrees \
    --exclude-dir=.claude \
    --exclude="aws_absent_test.go" \
    --exclude-dir="provider" \
    -e "aws-sdk-go-v2/service/eks" \
    /Users/jon/workspace/workflow && echo "eks gate passes"
```

Expected: `eks gate passes`

```bash
# Simulate the go.mod gate
! grep -qH \
    -e "aws-sdk-go-v2/service/ec2" \
    -e "aws-sdk-go-v2/service/dynamodb" \
    -e "aws-sdk-go-v2/service/elasticloadbalancingv2" \
    -e "aws-sdk-go-v2/service/rds" \
    -e "aws-sdk-go-v2/service/sqs" \
    /Users/jon/workspace/workflow/go.mod && echo "go.mod gate passes"
```

Expected: `go.mod gate passes`

**Step 3: Commit**

```bash
cd /Users/jon/workspace/workflow && git add .github/workflows/ci.yml && git commit -m "feat(#653): T3 — promote service/eks CI gate + add ec2/dynamodb/elb/rds/sqs to strict ban"
```

---

### Task 4: Add architectural doc comment to `platform/provider.go` and ADR 0032

**Files:**
- Modify: `platform/provider.go`
- Create: `decisions/0032-platform-provider-aws-tombstone.md`

**Step 1: Add three-layer architecture doc comment to `platform/provider.go`**

Replace the existing `// Provider is the top-level interface...` comment block at the top of the `Provider` interface with the expanded version below. The interface signature itself is NOT changed — only the doc comment:

```go
// Provider is the top-level interface for an infrastructure provider.
// A provider manages a collection of resource drivers and maps abstract
// capabilities to provider-specific resource types. Providers are registered
// with the engine and selected based on the platform configuration.
//
// # Three-Layer Provider Architecture
//
// The workflow engine has three distinct provider abstractions. Each serves a
// different layer and MUST NOT be confused with the others:
//
//   - platform.Provider (this interface) — in-core, capability-based declarative
//     abstraction used by the platform.* module system and pipeline steps
//     (step.iac_plan, step.iac_apply, step.platform_template, etc.).
//     Live implementations: DockerComposeProvider, MockProvider.
//     The AWS implementation (platform/providers/aws/) was deleted in workflow
//     v0.53.0 (issue #653 Phase 3) because it was build-tag-gated dead code
//     with zero callers; see ADR-0032.
//
//   - interfaces.IaCProvider (in interfaces/iac_provider.go) — the gRPC plugin
//     boundary interface used by the wfctl `infra.*` command suite. Implemented
//     by external plugins (workflow-plugin-aws, workflow-plugin-gcp, etc.) via
//     the typedIaCAdapter. This is the canonical AWS IaC path since v0.53.0.
//
//   - provider.CloudProvider (in provider/provider.go) — deploy-pipeline
//     abstraction for container deployments (ECS/EKS/GKE). Used by
//     step.deploy_rolling and the deployment executor. Orthogonal to both
//     platform.Provider and interfaces.IaCProvider.
//
// When adding a new cloud provider implementation, choose the layer that matches
// the use case:
//   - IaC resource provisioning (VPCs, DBs, clusters): implement interfaces.IaCProvider
//     as an external gRPC plugin.
//   - Container deployment pipelines: implement provider.CloudProvider in provider/.
//   - Local/mock/test capability-based planning: implement platform.Provider here.
```

**Step 2: Verify `go build ./platform/...` still passes after comment change**

```bash
cd /Users/jon/workspace/workflow && go build ./platform/...
```

Expected: exit 0

**Step 3: Write ADR 0032**

Create `/Users/jon/workspace/workflow/decisions/0032-platform-provider-aws-tombstone.md`:

```markdown
# ADR-0032: Tombstone `platform/providers/aws/` dead code

**Date:** 2026-05-13
**Status:** Accepted
**Related:** Issue #653, Phase 3; ADR-0024 (IaC typed force-cutover); Phase 1 (PR #657); Phase 2 (PR #659)

## Context

`platform/providers/aws/` implemented `platform.Provider` for Amazon Web Services,
gating all 24 files behind `//go:build aws`. Investigation for issue #653 Phase 3
revealed:

1. Zero external callers: no code outside the package itself (or its own tests)
   ever imported or instantiated `platform/providers/aws.NewProvider()`.
2. No CI coverage: no CI job runs `go test -tags aws ./...` or `go build -tags aws ./...`.
3. No user documentation: no example YAML config, no guide, no mention in DOCUMENTATION.md.
4. Not superseded by `workflow-plugin-aws`: that plugin implements `interfaces.IaCProvider`
   (the gRPC plugin boundary), which is a separate, orthogonal abstraction from
   `platform.Provider` (the capability-based in-core abstraction).
5. AWS SDK maintenance burden: `service/ec2`, `service/dynamodb`,
   `service/elasticloadbalancingv2`, `service/rds`, and `service/sqs` were listed in
   `go.mod` solely as transitive requirements of this dead tree.

## Decision

Delete `platform/providers/aws/` and its `drivers/` subtree (24 files, ~2,000 LOC).
Preserve the `platform.Provider` interface and its live implementations
(`DockerComposeProvider`, `MockProvider`).

Promote the Phase 2 CI gate placeholder for `service/eks` to strict enforcement
(removing the `--exclude-dir=platform` exemption) and add the 5 exclusive packages
to the banned list in `go.mod` and the grep gate.

## Consequences

- **Positive:** 5 AWS SDK packages removed from `go.mod`; CI gate tightened; ~2,000 LOC
  of dead code eliminated; no future AWS SDK upgrade compatibility burden for unused code.
- **Neutral:** The `platform.Provider` interface and its two live implementations
  (`DockerComposeProvider`, `MockProvider`) are completely unaffected.
- **Breaking (theoretical):** Any downstream project building workflow core with
  `-tags aws` would lose the `platform/providers/aws` package. Evidence that any
  such project exists: none (build tag undocumented, no CI exercises it, no example
  YAML uses it).
- **Canonical AWS IaC path:** `workflow-plugin-aws` (implements `interfaces.IaCProvider`)
  is the only supported AWS IaC integration since workflow v0.53.0.
```

**Step 4: Run full test suite**

```bash
cd /Users/jon/workspace/workflow && go test ./... 2>&1 | grep -E "FAIL|ok" | tail -30
```

Expected: all packages report `ok`; no `FAIL` lines

**Step 5: Commit**

```bash
cd /Users/jon/workspace/workflow && git add platform/provider.go decisions/0032-platform-provider-aws-tombstone.md && git commit -m "docs(#653): T4 — three-layer provider architecture comment + ADR-0032 tombstone"
```

---

## Verification Summary

After all 4 tasks, verify the complete state:

```bash
# 1. No platform/providers/aws directory
test ! -d /Users/jon/workspace/workflow/platform/providers/aws && echo "PASS: tree deleted"

# 2. Exclusive packages removed from go.mod
! grep -E "service/(ec2|rds|sqs|elasticloadbalancingv2|dynamodb)" /Users/jon/workspace/workflow/go.mod && echo "PASS: exclusive deps removed"

# 3. Kept packages still present
grep -c "service/\(eks\|iam\|s3\|sts\)" /Users/jon/workspace/workflow/go.mod | grep -q "4" && echo "PASS: 4 kept packages present"

# 4. Full build clean
cd /Users/jon/workspace/workflow && go build ./... && echo "PASS: build clean"

# 5. All tests pass
cd /Users/jon/workspace/workflow && go test ./... 2>&1 | grep -c "^ok" | grep -v "^0$" && echo "PASS: tests green"
```
