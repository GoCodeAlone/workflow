# Issue #653 Phase 2 — AWS Operational-Tooling Audit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Strip AWS SDK backends from `codebuild.go` and `platform_kubernetes_kind.go`, drop `service/codebuild` and `service/eks` from `go.mod`, and extend the CI banned-packages gate — leaving mock/non-AWS backends fully functional.

**Architecture:** Backend-stub pattern from Phase 1. Replace `codebuildAWSBackend` with `codebuildAWSErrorBackend` (mirrors `awsRoute53ErrorBackend` from `platform_dns_backends.go`). Replace `eksBackend` with `eksErrorBackend` (same pattern). No module/step type registrations change. `nosql_dynamodb.go` and `pipeline_step_s3_upload.go` are left untouched (no real SDK import; no go.mod win, respectively).

**Tech Stack:** Go, `go mod tidy`, `filepath.WalkDir` (for regression test), `golangci-lint`.

**Base branch:** origin/main (Phase 1 merged as PR #657 sha 950a0f0a)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~300 deletions + ~80 additions

**Out of scope:**
- Moving `aws.codebuild` or EKS backend to `workflow-plugin-aws` (Phase 3)
- Removing `cloud_account_aws.go` / `cloud_account_aws_creds.go` (deferred)
- Removing `pipeline_step_s3_upload.go` (no go.mod win; step is useful as S3-compat)
- Changing `nosql_dynamodb.go` (zero real SDK imports — no action needed)
- Removing `service/s3`, `service/ec2`, `service/ecs`, `service/sts`, `service/sqs`, `service/cloudwatch` (surviving users)
- Removing `awsconfig/config` or `credentials` packages (surviving users)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(#653): Phase 2 — strip AWS SDK from codebuild + EKS backends | Task 1, Task 2, Task 3, Task 4 | feat/issue-653-phase2-aws-operational |

**Status:** Draft

---

## Key codebase facts for implementer

**Read the design doc first:** `docs/plans/2026-05-13-issue-653-phase2-aws-operational-design.md`

**Exact precedent files to mirror:**
- `module/platform_dns_backends.go` lines 85–120 → exact pattern for error backend structs
- `module/platform_dns_test.go` lines 239–275 → exact pattern for migration-error tests
- `module/aws_absent_test.go` → add `codebuild` + `eks` to `freed` slice

**Error message format (must match for test assertions):**
```
aws.codebuild %q: AWS CodeBuild backend removed from workflow core in v0.53.0 (issue #653).\n
Set provider: mock to continue using the in-memory mock backend.\n
Install workflow-plugin-aws to use the real AWS backend: https://github.com/GoCodeAlone/workflow-plugin-aws
```

```
platform.kubernetes %q: EKS cluster backend removed from workflow core in v0.53.0 (issue #653).\n
Use cluster_type: kind for local development.\n
Install workflow-plugin-aws to manage EKS clusters: https://github.com/GoCodeAlone/workflow-plugin-aws
```

**`legacyaws.RemovedInVersion` = `"v0.53.0"`** (from `internal/legacyaws/types.go`)

**`codebuild.go` backend selection (lines 136–148):**
```go
if providerType == "aws" {
    m.backend = &codebuildAWSBackend{}  // REPLACE with &codebuildAWSErrorBackend{}
} else {
    m.backend = &codebuildMockBackend{}
}
```

**`platform_kubernetes_kind.go` EKS init registration (file bottom, `init()` function):**
```go
RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
    return &eksBackend{}, nil  // REPLACE with &eksErrorBackend{}
})
```

**CI gate location:** `.github/workflows/ci.yml` — `aws-sdk-banned` job, `Grep gate` steps. Extend both the `*.go` grep and `go.mod` grep to add `-e "aws-sdk-go-v2/service/codebuild"` and `-e "aws-sdk-go-v2/service/eks"`.

**`aws_absent_test.go`** — `freed` slice must include `"aws-sdk-go-v2/service/codebuild"` and `"aws-sdk-go-v2/service/eks"` (same pattern as existing Phase 1 entries).

**Lint discipline:** Run `golangci-lint run ./...` locally before pushing (Phase 1 retro: lint not run on derived files caused a CI failure).

**Working directory:** `/Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational`

---

### Task 1: Strip `codebuildAWSBackend` from `module/codebuild.go`

**Files:**
- Modify: `module/codebuild.go` (replace `codebuildAWSBackend` with `codebuildAWSErrorBackend`; remove `service/codebuild` + `service/codebuild/types` imports)
- Modify: `module/codebuild_test.go` (add `TestCodeBuildAWSBackendMigrationError` test)

**Step 1: Write the failing test for the migration error**

Add to the END of `module/codebuild_test.go` (after the last test function):

```go
// ─── AWS CodeBuild migration error (issue #653 Phase 2) ──────────────────────

func TestCodeBuildAWSBackendMigrationError(t *testing.T) {
    app := module.NewMockApplication()
    m := module.NewCodeBuildModule("test-build", map[string]any{
        "provider": "aws",
        "region":   "us-east-1",
    })
    if err := m.Init(app); err != nil {
        t.Fatalf("Init should succeed (backend registered): %v", err)
    }
    // Migration error fires at operation time, not Init time.
    if err := m.CreateProject(); err == nil {
        t.Fatal("expected migration error from CreateProject() for provider: aws, got nil")
    }
    errStr := m.CreateProject().Error()
    for _, want := range []string{"workflow-plugin-aws", "v0.53.0", "provider: mock"} {
        if !strings.Contains(errStr, want) {
            t.Errorf("error should mention %q, got: %s", want, errStr)
        }
    }
}
```

**Step 2: Run the test to verify it fails with the WRONG error (not the migration error)**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -run TestCodeBuildAWSBackendMigrationError -v
```
Expected: FAIL — the test assertion `strings.Contains(errStr, "provider: mock")` fails.
The current `codebuildAWSBackend.createProject()` makes a real AWS API call and returns a
credential error (e.g., "no EC2 IMDS role found"), NOT the migration error with "provider: mock".
This confirms the test correctly distinguishes the migration error from the real AWS error.

**Step 3: Replace `codebuildAWSBackend` with `codebuildAWSErrorBackend` in `module/codebuild.go`**

3a. Remove the entire `codebuildAWSBackend` struct and all its methods (lines 372–589 in current file — the `// ─── AWS CodeBuild backend ─────────────────────────────────────────────────────` section through end of file, plus `awsCodeBuildToInternal` helper).

3b. Add `codebuildAWSErrorBackend` immediately after the `codebuildMockBackend` section:

```go
// ─── AWS CodeBuild migration error backend ────────────────────────────────────

// codebuildAWSErrorBackend is registered when provider: aws is set, after the
// real AWS CodeBuild backend was removed from workflow core in v0.53.0 (issue #653).
// All methods return an actionable migration error directing the operator to
// workflow-plugin-aws.
type codebuildAWSErrorBackend struct{}

func (b *codebuildAWSErrorBackend) createProject(m *CodeBuildModule) error {
	return b.err(m)
}

func (b *codebuildAWSErrorBackend) deleteProject(m *CodeBuildModule) error {
	return b.err(m)
}

func (b *codebuildAWSErrorBackend) startBuild(m *CodeBuildModule, _ map[string]string) (*CodeBuildBuild, error) {
	return nil, b.err(m)
}

func (b *codebuildAWSErrorBackend) getBuildStatus(m *CodeBuildModule, _ string) (*CodeBuildBuild, error) {
	return nil, b.err(m)
}

func (b *codebuildAWSErrorBackend) getBuildLogs(m *CodeBuildModule, _ string) ([]string, error) {
	return nil, b.err(m)
}

func (b *codebuildAWSErrorBackend) listBuilds(m *CodeBuildModule) ([]*CodeBuildBuild, error) {
	return nil, b.err(m)
}

func (b *codebuildAWSErrorBackend) err(m *CodeBuildModule) error {
	return fmt.Errorf(
		"aws.codebuild %q: AWS CodeBuild backend removed from workflow core in %s (issue #653).\n"+
			"Set provider: mock to continue using the in-memory mock backend.\n"+
			"Install workflow-plugin-aws to use the real AWS backend: https://github.com/GoCodeAlone/workflow-plugin-aws",
		m.name, legacyaws.RemovedInVersion,
	)
}
```

3c. Update the `Init()` backend selection (around line 143):
```go
// BEFORE:
if providerType == "aws" {
    m.backend = &codebuildAWSBackend{}
}

// AFTER:
if providerType == "aws" {
    m.backend = &codebuildAWSErrorBackend{}
}
```

3d. Update imports: Remove `"github.com/aws/aws-sdk-go-v2/service/codebuild"` and `cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"`. Add `"github.com/GoCodeAlone/workflow/internal/legacyaws"`.

**Step 4: Run the test to verify it passes**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -run TestCodeBuildAWSBackendMigrationError -v
```
Expected: `PASS`

**Step 5: Run the full module test suite to verify no regressions**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -v 2>&1 | tail -20
```
Expected: All tests pass. No `FAIL` lines.

**Step 6: Run golangci-lint to verify no lint regressions**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
golangci-lint run ./module/... 2>&1 | head -30
```
Expected: No output (or only pre-existing warnings, none in `codebuild.go`).

**Step 7: Commit**

Rollback: `git revert <sha>` restores `codebuildAWSBackend`; run `go mod tidy` to restore `service/codebuild` in `go.mod`.

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
git add module/codebuild.go module/codebuild_test.go
git commit -m "feat(#653/p2): replace codebuildAWSBackend with migration error stub"
```

---

### Task 2: Strip `eksBackend` from `module/platform_kubernetes_kind.go`

**Files:**
- Modify: `module/platform_kubernetes_kind.go` (replace `eksBackend` with `eksErrorBackend`; remove `service/eks`, `eks/types`, `aws` imports)
- Modify: `module/platform_kubernetes_test.go` (add `TestPlatformKubernetes_EKSBackendMigrationError` test)

**Step 1: Write the failing test for the EKS migration error**

Check existing test file structure first:
```bash
grep -n "TestPlatformKubernetes\|func Test" /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational/module/platform_kubernetes_test.go | head -20
```

Add to the END of `module/platform_kubernetes_test.go`:

```go
// ─── EKS migration error (issue #653 Phase 2) ────────────────────────────────

func TestPlatformKubernetes_EKSBackendMigrationError(t *testing.T) {
    app := module.NewMockApplication()
    m := module.NewPlatformKubernetes("test-cluster", map[string]any{
        "cluster_type": "eks",
        "region":       "us-east-1",
    })
    if err := m.Init(app); err != nil {
        t.Fatalf("Init should succeed (backend registered): %v", err)
    }
    // Migration error fires at operation time, not Init time.
    _, err := m.Plan()
    if err == nil {
        t.Fatal("expected migration error from Plan() for cluster_type: eks, got nil")
    }
    errStr := err.Error()
    for _, want := range []string{"workflow-plugin-aws", "v0.53.0", "cluster_type: kind"} {
        if !strings.Contains(errStr, want) {
            t.Errorf("error should mention %q, got: %s", want, errStr)
        }
    }
}
```

**Step 2: Run the test to verify it fails**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -run TestPlatformKubernetes_EKSBackendMigrationError -v
```
Expected: FAIL — `expected migration error ... got nil` (EKS backend currently tries to make real AWS calls which fail differently).

**Step 3: Replace `eksBackend` with `eksErrorBackend` in `module/platform_kubernetes_kind.go`**

3a. Remove the entire `eksBackend` struct and all its methods (the `// ─── EKS backend ─────────────────────────────────────────────────────────────` section through the end of the `eksBackend` methods, approximately lines 88–311 of the current file). Do NOT remove `kindBackend`, `gkeBackend`, or `aksBackend`.

3b. Add `eksErrorBackend` immediately after the `kindBackend` section (after `kindBackend`'s `init()` lines that register `kind` and `k3s`):

```go
// ─── EKS migration error backend ──────────────────────────────────────────────

// eksErrorBackend is registered under cluster_type "eks" after the real EKS backend
// was removed from workflow core in v0.53.0 (issue #653).
// All methods return an actionable migration error directing the operator to
// workflow-plugin-aws.
type eksErrorBackend struct{}

func (b *eksErrorBackend) plan(k *PlatformKubernetes) (*PlatformPlan, error) {
	return nil, b.err(k)
}

func (b *eksErrorBackend) apply(k *PlatformKubernetes) (*PlatformResult, error) {
	return nil, b.err(k)
}

func (b *eksErrorBackend) status(k *PlatformKubernetes) (*KubernetesClusterState, error) {
	return k.state, b.err(k)
}

func (b *eksErrorBackend) destroy(k *PlatformKubernetes) error {
	return b.err(k)
}

func (b *eksErrorBackend) err(k *PlatformKubernetes) error {
	return fmt.Errorf(
		"platform.kubernetes %q: EKS cluster backend removed from workflow core in %s (issue #653).\n"+
			"Use cluster_type: kind for local development.\n"+
			"Install workflow-plugin-aws to manage EKS clusters: https://github.com/GoCodeAlone/workflow-plugin-aws",
		k.name, legacyaws.RemovedInVersion,
	)
}
```

3c. Update the `init()` function at the bottom of the file:
```go
// BEFORE:
RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
    return &eksBackend{}, nil
})

// AFTER:
RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
    return &eksErrorBackend{}, nil
})
```

3d. Update imports: Remove `"github.com/aws/aws-sdk-go-v2/aws"`, `"github.com/aws/aws-sdk-go-v2/service/eks"`, and `ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"`. Add `"github.com/GoCodeAlone/workflow/internal/legacyaws"`.

Also remove `"errors"` import — **confirmed**: `"errors"` is used only by `eksBackend` for `errors.As(err, &notFound)` and `errors.As(err, &alreadyExists)`. `gkeBackend` and `aksBackend` use `strings.Contains` only, not `errors.As`. Remove `"errors"`. Keep `"strings"` (used by `gkeBackend` and `aksBackend`).

**Step 4: Run the test to verify it passes**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -run TestPlatformKubernetes_EKSBackendMigrationError -v
```
Expected: `PASS`

**Step 5: Run the full module test suite**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -v 2>&1 | tail -20
```
Expected: All tests pass.

**Step 6: Run golangci-lint**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
golangci-lint run ./module/... 2>&1 | head -30
```
Expected: No output.

**Step 7: Commit**

Rollback: `git revert <sha>` restores `eksBackend`; run `go mod tidy` to restore `service/eks` in `go.mod`.

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
git add module/platform_kubernetes_kind.go module/platform_kubernetes_test.go
git commit -m "feat(#653/p2): replace eksBackend with migration error stub"
```

---

### Task 3: Drop freed SDK packages via `go mod tidy` + extend CI gate

**Files:**
- Modify: `go.mod`, `go.sum` (via `go mod tidy`)
- Modify: `.github/workflows/ci.yml` (extend `aws-sdk-banned` gate)
- Modify: `module/aws_absent_test.go` (add `service/codebuild` and `service/eks` to `freed` slice)

**SERIAL DEPENDENCY:** Task 3 MUST run after Tasks 1 and 2 are fully committed. Step 1 below verifies this before proceeding.

**Step 1: Verify Tasks 1 and 2 are complete — no remaining imports of `service/codebuild` or `service/eks`**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
grep -rn "service/codebuild\|service/eks" --include="*.go" .
```
Expected: Zero output. If any output appears, STOP — Task 1 or Task 2 is incomplete. Do not proceed to Step 2 until the imports are removed.

**Step 2: Run `go mod tidy` to drop freed packages**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go mod tidy 2>&1
```
Expected: No errors. Packages `service/codebuild`, `service/eks`, and `service/dynamodb` (phantom) removed from `go.mod`.

**Step 3: Verify the packages were dropped from `go.mod`**

```bash
grep "service/codebuild\|service/eks\|service/dynamodb" /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational/go.mod
```
Expected: No output (all three removed).

**Step 4: Update `module/aws_absent_test.go` — add `service/codebuild` and `service/eks` to `freed` slice**

Current `freed` slice:
```go
freed := []string{
    "aws-sdk-go-v2/service/apigatewayv2",
    "aws-sdk-go-v2/service/applicationautoscaling",
    "aws-sdk-go-v2/service/route53",
}
```

Updated:
```go
freed := []string{
    "aws-sdk-go-v2/service/apigatewayv2",
    "aws-sdk-go-v2/service/applicationautoscaling",
    "aws-sdk-go-v2/service/route53",
    "aws-sdk-go-v2/service/codebuild",
    "aws-sdk-go-v2/service/eks",
}
```

**Step 5: Run `aws_absent_test.go` regression test**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./module/... -run TestAWSServicePackagesAbsent -v
```
Expected: `PASS` — the freed packages are no longer imported.

**Step 6: Extend `.github/workflows/ci.yml` `aws-sdk-banned` gate**

Locate the `aws-sdk-banned` job in `.github/workflows/ci.yml`. Extend both steps:

```yaml
# Grep gate — *.go files must not import removed AWS service packages
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
      -e "aws-sdk-go-v2/service/eks" \
      .

# Grep gate — go.mod files must not list removed AWS SDK packages
run: |
  ! grep -qH \
      -e "aws-sdk-go-v2/service/apigatewayv2" \
      -e "aws-sdk-go-v2/service/applicationautoscaling" \
      -e "aws-sdk-go-v2/service/route53" \
      -e "aws-sdk-go-v2/service/codebuild" \
      -e "aws-sdk-go-v2/service/eks" \
      go.mod example/go.mod
```

**Step 7: Build the full binary to verify compilation**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go build ./... 2>&1
```
Expected: No output (clean build).

**Step 8: Run the full test suite**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
go test ./... 2>&1 | grep -E "FAIL|ok" | tail -30
```
Expected: All lines show `ok`. No `FAIL` lines.

**Step 9: Commit**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
git add go.mod go.sum module/aws_absent_test.go .github/workflows/ci.yml
git commit -m "chore(#653/p2): go mod tidy + extend aws-sdk-banned CI gate (T3)"
```

---

### Task 4: Create PR and verify CI passes

**Files:** No code changes. PR creation + CI monitoring only.

**Step 1: Push branch**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
git push -u origin feat/issue-653-phase2-aws-operational
```
Expected: Branch pushed to remote. `git push` exits 0.

**Step 2: Verify commit log is clean**

```bash
cd /Users/jon/workspace/workflow/.claude/worktrees/feat-phase2-aws-operational
git log --oneline origin/feat/issue-653-aws-iac-cutover-v2..HEAD
```
Expected: 3 commits (T1, T2, T3) visible.

**Step 3: Create the PR**

Note: Target `main` — Phase 1 branch (`feat/issue-653-aws-iac-cutover-v2`) was merged to main as PR #657 (sha `950a0f0a`).

```bash
gh pr create \
  --base main \
  --head feat/issue-653-phase2-aws-operational \
  --title "feat(#653): Phase 2 — strip AWS SDK from codebuild + EKS backends" \
  --body "$(cat <<'EOF'
## Summary

Phase 2 of issue #653: audit + dispose 4 AWS operational-tooling files in workflow core.

**4-file dispositions:**
- `module/codebuild.go`: replace `codebuildAWSBackend` with `codebuildAWSErrorBackend` → drops `service/codebuild` from go.mod
- `module/platform_kubernetes_kind.go`: replace `eksBackend` with `eksErrorBackend` → drops `service/eks` from go.mod
- `module/nosql_dynamodb.go`: no change (zero real SDK import — only in doc comment)
- `module/pipeline_step_s3_upload.go`: no change (no go.mod win; step is S3-compat utility, not AWS-specific)

**SDK packages dropped:** `service/codebuild`, `service/eks`, `service/dynamodb` (phantom)

**Pattern:** mirrors Phase 1's `awsRoute53ErrorBackend` — mock backend stays functional, AWS backend replaced with migration error directing users to workflow-plugin-aws.

## Test plan
- [ ] `TestCodeBuildAWSBackendMigrationError` added + passes
- [ ] `TestPlatformKubernetes_EKSBackendMigrationError` added + passes
- [ ] `TestAWSServicePackagesAbsent` extended to cover codebuild + eks + passes
- [ ] CI `aws-sdk-banned` gate extended + CI green
- [ ] Full `go test ./...` green

## Related
- Issue #653
- Phase 1 PR: #657 (merged)
- Design doc: `docs/plans/2026-05-13-issue-653-phase2-aws-operational-design.md`

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
Expected: PR URL printed.

**Step 4: Add Copilot reviewer**

```bash
gh pr edit <PR_NUMBER> --add-reviewer "@copilot"
```
Expected: `@copilot` added as reviewer (use literal `@copilot`).

**Step 5: Monitor CI**

```bash
gh pr checks <PR_NUMBER> --watch
```
Expected: All checks green. Specifically: `aws-sdk-banned` check passes.

Rollback: `git revert` the 3 task commits (T1, T2, T3) if CI fails unrecoverably. The revert restores all SDK imports, CI gate extensions, and test changes in a single revertible commit.
