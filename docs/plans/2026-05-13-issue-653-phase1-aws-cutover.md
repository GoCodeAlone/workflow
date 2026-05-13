# Issue #653 Phase 1 — Remove AWS IaC Modules from Workflow Core Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Force-cutover 6 legacy AWS IaC module types from workflow core to workflow-plugin-aws v0.2.0, deleting 14 files, stripping registration sites, adding actionable migration errors, and dropping freed AWS SDK packages from go.mod.

**Architecture:** Single-PR deletion of legacy modules with no compat layer, mirroring #617 godo removal. New `internal/legacyaws` leaf package carries shared constants/formatters. New `modernize/legacy_aws_rule.go` mirrors `modernize/legacy_do_rule.go`. The `platform.dns` module type stays (generic + mock backend); only the Route53 backend is replaced with a migration error stub. `cloud_account_aws.go` + `cloud_account_aws_creds.go` are explicitly NOT deleted (Phase 2 scope).

**Tech Stack:** Go, `go mod tidy`, `filepath.WalkDir`, `go/parser` for regression test, `golangci-lint`, `gopkg.in/yaml.v3`.

**Base branch:** origin/main (tracking remote, includes #617 godo removal at c55a56e5)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~2500 deletions + ~500 additions

**Out of scope:**
- Removing `cloud_account_aws.go` / `cloud_account_aws_creds.go` (Phase 2)
- Removing the generic `platform.dns` module type or `step.dns_*` step types
- Removing `codebuild.go`, `nosql_dynamodb.go`, `pipeline_step_s3_upload.go`, `platform_kubernetes_kind.go` (Phase 2)
- Removing `platform/providers/aws/drivers/` (Phase 3)
- Backwards-compatible shim modules
- Removing `service/ec2`, `service/ecs`, `service/sts`, `credentials/stscreds`, `service/cloudwatch` (all have surviving users)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(#653): remove legacy AWS IaC modules from workflow core | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/issue-653-aws-iac-cutover-v2 |

**Status:** Draft

---

## Key codebase facts for implementer

**Read the design doc first:** `docs/plans/2026-05-13-issue-653-phase1-aws-cutover-design.md` — it contains the complete file deletion manifest, parity matrix, migration error strings, and architectural decisions.

**Exact precedent files to mirror:**
- `internal/legacydo/types.go` → new `internal/legacyaws/types.go`
- `modernize/legacy_do_rule.go` → new `modernize/legacy_aws_rule.go`
- `engine.go` lines 394-402, 514-516 → legacyaws check goes in the same location
- `module/pipeline_step_registry.go` lines 45-46 → legacyaws check after legacydo check
- `cmd/wfctl/validate.go` lines 146-178 → legacyaws module+step sweep + DNS provider check
- `cmd/wfctl/ci_validate.go` lines 136-165 → same pattern
- `modernize/modernize.go` `AllRules()` line 46 → add `legacyAWSRule()` after `legacyDORule()`

**Exact engine.go pattern for unknown module type (lines 506-509 and 514-516):**
```go
factory, exists := e.moduleFactories[modCfg.Type]
if !exists {
    // legacydo check first (already present):
    if legacydo.IsModuleType(modCfg.Type) {
        _, iacLoaded := e.moduleFactories["iac.provider"]
        return legacydo.FormatModuleError(modCfg.Type, modCfg.Name, iacLoaded)
    }
    // legacyaws check (new — mirrors legacydo pattern):
    if legacyaws.IsModuleType(modCfg.Type) {
        _, iacLoaded := e.moduleFactories["iac.provider"]
        return legacyaws.FormatModuleError(modCfg.Type, modCfg.Name, iacLoaded)
    }
    return fmt.Errorf("unknown module type %q for module %q — ensure the required plugin is loaded", modCfg.Type, modCfg.Name)
}
```

**platform_dns.go `init()` function (lines 66-73) — replace aws backend registration:**
```go
func init() {
    RegisterDNSBackend("mock", func(_ map[string]any) (dnsBackend, error) {
        return &mockDNSBackend{}, nil
    })
    // BEFORE: returned &route53Backend{}
    // AFTER: return migration error
    RegisterDNSBackend("aws", func(_ map[string]any) (dnsBackend, error) {
        return &awsRoute53ErrorBackend{}, nil
    })
}
```

**RemovedInVersion for legacyaws:** `"v0.53.0"` (next minor after v0.52.0 godo removal)

**Module types to migrate (4):**
- `platform.ecs` → `infra.container_service`
- `platform.apigateway` → `infra.api_gateway`
- `platform.autoscaling` → `infra.autoscaling_group` (1→1, not auto-injecting provider)
- `platform.networking` → `infra.vpc` + `infra.firewall` (1→2 split, not auto-fixable)

**Step types to guard (15, none auto-rewritten):**
- ecs: `step.ecs_plan`, `step.ecs_apply`, `step.ecs_status`, `step.ecs_destroy`
- apigw: `step.apigw_plan`, `step.apigw_apply`, `step.apigw_status`, `step.apigw_destroy`
- scaling: `step.scaling_plan`, `step.scaling_apply`, `step.scaling_status`, `step.scaling_destroy`
- networking: `step.network_plan`, `step.network_apply`, `step.network_status` (no step.network_destroy)

**`infra.autoscaling_group` in `plugins/infra/plugin.go`** — just add the string to `infraTypes []string`. The `ModuleSchemas()` function auto-generates the schema from `infraTypes`; no manual schema entry needed.

---

### Task 1: Delete 14 files + partial edits to api_gateway_test.go and app_container.go

**Files:**
- Delete: `module/platform_ecs.go`
- Delete: `module/platform_ecs_test.go`
- Delete: `module/pipeline_step_ecs.go`
- Delete: `module/platform_apigateway.go`
- Delete: `module/platform_apigateway_test.go`
- Delete: `module/pipeline_step_apigateway.go`
- Delete: `module/aws_api_gateway.go`
- Delete: `module/platform_autoscaling.go`
- Delete: `module/platform_autoscaling_test.go`
- Delete: `module/pipeline_step_autoscaling.go`
- Delete: `module/platform_networking.go`
- Delete: `module/platform_networking_test.go`
- Delete: `module/pipeline_step_networking.go`
- Delete: `module/platform_aws_integration_test.go`
- Modify: `module/api_gateway_test.go` — remove 3 TestAWSAPIGateway_* functions
- Modify: `module/app_container.go` — remove ECS-specific code (C-3 fix)
- Create: `module/aws_absent_test.go`

**Step 1: Delete the 14 files**

```bash
git rm module/platform_ecs.go module/platform_ecs_test.go module/pipeline_step_ecs.go
git rm module/platform_apigateway.go module/platform_apigateway_test.go module/pipeline_step_apigateway.go
git rm module/aws_api_gateway.go
git rm module/platform_autoscaling.go module/platform_autoscaling_test.go module/pipeline_step_autoscaling.go
git rm module/platform_networking.go module/platform_networking_test.go module/pipeline_step_networking.go
git rm module/platform_aws_integration_test.go
```

Expected: 14 files removed from git index.

**Step 2: Edit module/api_gateway_test.go — remove the 3 TestAWSAPIGateway_* functions**

Read the file first, then identify and delete these three test functions entirely:
- `TestAWSAPIGateway_Basic`
- `TestAWSAPIGateway_SyncRoutesStub`
- `TestAWSAPIGateway_SyncRoutesRequiresAPIID`

Keep all other test functions (19 generic HTTP gateway tests). Remove any `import` for `"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"` or similar if it only served those 3 tests.

**Step 3: Edit module/app_container.go — remove all ECS-specific code (C-3 fix)**

`app_container.go` currently has:
- `ECSAppManifests` struct (lines ~77-81)
- `ECSAppTaskDef` struct (lines ~83-89, uses `ECSContainer` type from deleted platform_ecs.go)
- `ECSAppServiceCfg` struct (lines ~91-97)
- `case *PlatformECS:` branch in `Init()` type switch (line ~130)
- Error message at line ~134 references "platform.ecs"
- Comment at line ~14 references "platform.ecs"
- Logger.Warn at line ~147 references "platform.ecs"
- `ecsAppBackend` struct (line ~593) and all its methods
- `buildECSManifests()` function

Remove all of the above. The resulting `Init()` type switch should only have `case *PlatformKubernetes:` and the `default:` error. Update the default error to:
```go
return fmt.Errorf("app.container %q: environment %q is not a platform.kubernetes module (got %T); platform.ecs was removed — use infra.container_service with workflow-plugin-aws", m.name, envName, svc)
```

Update the doc comment at top (line ~14) to remove `platform.ecs` reference:
```go
//	environment: name of a platform.kubernetes module (service registry)
```

Update logger.Warn hint (line ~147):
```go
"hint", "set 'environment' to a platform.kubernetes module, or ensure KUBECONFIG / ~/.kube/config is present")
```

**Step 4: Write the regression gate test module/aws_absent_test.go**

This test verifies the freed AWS SDK service packages are never re-imported. Uses `filepath.WalkDir` (NOT `filepath.Glob`) per #617 retro lesson.

```go
package module_test

import (
    "go/parser"
    "go/token"
    "io/fs"
    "path/filepath"
    "strings"
    "testing"
)

// TestAWSServicePackagesAbsent verifies that the freed AWS SDK service packages
// are not imported anywhere in the module/ directory (issue #653).
// Uses filepath.WalkDir (recursive) — NOT filepath.Glob — per #617 retro.
func TestAWSServicePackagesAbsent(t *testing.T) {
    freed := []string{
        "aws-sdk-go-v2/service/apigatewayv2",
        "aws-sdk-go-v2/service/applicationautoscaling",
        "aws-sdk-go-v2/service/route53",
    }

    fset := token.NewFileSet()
    err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() || !strings.HasSuffix(path, ".go") {
            return nil
        }
        if strings.HasSuffix(path, "aws_absent_test.go") {
            return nil // skip self
        }
        f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
        if parseErr != nil {
            return nil // skip unparseable files
        }
        for _, imp := range f.Imports {
            importPath := strings.Trim(imp.Path.Value, `"`)
            for _, pkg := range freed {
                if strings.Contains(importPath, pkg) {
                    t.Errorf("%s imports freed package %q", path, importPath)
                }
            }
        }
        return nil
    })
    if err != nil {
        t.Fatalf("WalkDir: %v", err)
    }
}
```

**Step 5: Run the tests to verify they compile (registration sites will fail, expected)**

```bash
cd /Users/jon/workspace/workflow && go build ./module/... 2>&1 | head -40
```

Expected: compile errors in `plugins/platform/plugin.go` (references deleted factories) — these are expected and will be fixed in Task 3. The `module/` package itself should compile cleanly after the edits.

If `module/` package has compile errors (other than missing platform_ecs types), diagnose and fix before committing.

**Step 6: Run the regression test to verify it passes**

```bash
cd /Users/jon/workspace/workflow/module && go test -run TestAWSServicePackagesAbsent -v 2>&1
```

Expected: `PASS` — module/ has no imports for the freed packages.

**Step 7: Commit Task 1**

```bash
git add module/aws_absent_test.go module/api_gateway_test.go module/app_container.go
git commit -m "feat(#653): T1 — delete legacy AWS IaC module files + regression gate"
```

---

### Task 2: Replace platform_dns_backends.go with mock-only version + error stub

**Files:**
- Replace: `module/platform_dns_backends.go` (full rewrite — delete route53Backend, add awsRoute53ErrorBackend)
- Modify: `module/platform_dns.go` (update init() — replace route53Backend with awsRoute53ErrorBackend)
- Modify: `module/platform_dns_test.go` (add test for provider: aws migration error)

**Step 1: Write the failing test for the AWS provider migration error**

Add to `module/platform_dns_test.go`:

```go
func TestPlatformDNS_AWSBackendMigrationError(t *testing.T) {
    cfg := map[string]any{"provider": "aws", "zone": map[string]any{"name": "example.com"}}
    m := NewPlatformDNS("test-dns", cfg)
    app := mock.NewMockApplication()
    err := m.Init(app)
    if err == nil {
        t.Fatal("expected migration error for provider: aws, got nil")
    }
    errStr := err.Error()
    if !strings.Contains(errStr, "infra.dns") {
        t.Errorf("error should mention infra.dns, got: %s", errStr)
    }
    if !strings.Contains(errStr, "workflow-plugin-aws") {
        t.Errorf("error should mention workflow-plugin-aws, got: %s", errStr)
    }
}
```

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestPlatformDNS_AWSBackendMigrationError -v 2>&1`
Expected: compile error (awsRoute53ErrorBackend doesn't exist yet) or test FAIL (mock backend registered under "aws" currently would not return an error).

**Step 2: Replace module/platform_dns_backends.go**

Write the new file keeping only `mockDNSBackend` and adding `awsRoute53ErrorBackend`:

```go
package module

import (
    "fmt"
)

// ─── Mock backend ─────────────────────────────────────────────────────────────

// mockDNSBackend is an in-memory DNS backend for testing and local use.
// No real DNS API calls are made; state is tracked in memory.
type mockDNSBackend struct{}

func (b *mockDNSBackend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
    zone := m.zoneConfig()
    records := m.recordConfigs()
    plan := &DNSPlan{
        Zone:    zone,
        Records: records,
    }

    switch m.state.Status {
    case "pending":
        plan.Changes = append(plan.Changes, fmt.Sprintf("create zone %q", zone.Name))
        for _, r := range records {
            plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
        }
    case "active":
        // diff existing records vs desired
        existing := map[string]DNSRecordConfig{}
        for _, r := range m.state.Records {
            existing[r.Name+"/"+r.Type] = r
        }
        for _, r := range records {
            key := r.Name + "/" + r.Type
            if e, ok := existing[key]; !ok {
                plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
            } else if e.Value != r.Value || e.TTL != r.TTL {
                plan.Changes = append(plan.Changes, fmt.Sprintf("update %s record %q: %q -> %q", r.Type, r.Name, e.Value, r.Value))
            }
        }
        if len(plan.Changes) == 0 {
            plan.Changes = []string{"no changes"}
        }
    case "deleted":
        plan.Changes = append(plan.Changes, fmt.Sprintf("create zone %q (previously deleted)", zone.Name))
        for _, r := range records {
            plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
        }
    default:
        plan.Changes = []string{fmt.Sprintf("zone status=%s, no action", m.state.Status)}
    }

    return plan, nil
}

func (b *mockDNSBackend) applyDNS(m *PlatformDNS) (*DNSState, error) {
    if m.state.Status == "active" {
        m.state.Records = m.recordConfigs()
        return m.state, nil
    }

    zone := m.zoneConfig()
    m.state.ZoneID = fmt.Sprintf("mock-zone-%s", zone.Name)
    m.state.ZoneName = zone.Name
    m.state.Records = m.recordConfigs()
    m.state.Status = "active"
    return m.state, nil
}

func (b *mockDNSBackend) statusDNS(m *PlatformDNS) (*DNSState, error) {
    return m.state, nil
}

func (b *mockDNSBackend) destroyDNS(m *PlatformDNS) error {
    if m.state.Status == "deleted" {
        return nil
    }
    m.state.Status = "deleting"
    m.state.Records = nil
    m.state.Status = "deleted"
    return nil
}

// ─── AWS Route53 migration error backend ──────────────────────────────────────

// awsRoute53ErrorBackend is registered under provider "aws" after the Route53
// backend was removed from workflow core in v0.53.0 (issue #653).
// All methods return the actionable migration error directing the operator to
// infra.dns + workflow-plugin-aws.
type awsRoute53ErrorBackend struct{}

func (b *awsRoute53ErrorBackend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
    return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) applyDNS(m *PlatformDNS) (*DNSState, error) {
    return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) statusDNS(m *PlatformDNS) (*DNSState, error) {
    return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) destroyDNS(m *PlatformDNS) error {
    return b.err(m)
}

func (b *awsRoute53ErrorBackend) err(m *PlatformDNS) error {
    return fmt.Errorf(
        "platform.dns %q: AWS Route53 backend removed from workflow core in v0.53.0 (issue #653).\n"+
            "Migrate to: infra.dns (provider: aws) with workflow-plugin-aws v0.2.0+.\n"+
            "Install: https://github.com/GoCodeAlone/workflow-plugin-aws\n"+
            "See docs/migrations/v0.53.0-aws-iac-removal.md",
        m.name,
    )
}
```

**Step 3: Update platform_dns.go init() to use the error backend**

In `module/platform_dns.go` at the `init()` function (lines 66-73), change:
```go
RegisterDNSBackend("aws", func(_ map[string]any) (dnsBackend, error) {
    return &route53Backend{}, nil
})
```
to:
```go
RegisterDNSBackend("aws", func(_ map[string]any) (dnsBackend, error) {
    return &awsRoute53ErrorBackend{}, nil
})
```

No import changes needed — `awsRoute53ErrorBackend` is in the same `module` package.

**Step 4: Run the failing test to verify it now passes**

```bash
cd /Users/jon/workspace/workflow && go test ./module/ -run TestPlatformDNS_AWSBackendMigrationError -v 2>&1
```

Expected:
```
--- PASS: TestPlatformDNS_AWSBackendMigrationError (0.00s)
```

**Step 5: Run all module DNS tests**

```bash
cd /Users/jon/workspace/workflow && go test ./module/ -run TestPlatformDNS -v 2>&1
```

Expected: all DNS tests pass (mock backend tests should pass unchanged).

**Step 6: Commit Task 2**

```bash
git add module/platform_dns_backends.go module/platform_dns.go module/platform_dns_test.go
git commit -m "feat(#653): T2 — replace Route53 backend with migration error stub"
```

---

### Task 3: Strip registration sites + add infra.autoscaling_group + regenerate golden

**Files:**
- Modify: `plugins/platform/plugin.go`
- Modify: `plugins/platform/plugin_test.go`
- Modify: `plugins/infra/plugin.go`
- Modify: `schema/schema.go`
- Modify: `schema/module_schema.go`
- Modify: `schema/step_schema_builtins.go`
- Modify: `cmd/wfctl/type_registry.go`
- Modify: `module/multi_region.go` (error message string)
- Modify: `DOCUMENTATION.md`
- Update: `schema/testdata/editor-schemas.golden.json` (auto-regenerated)

**Step 1: Edit plugins/platform/plugin.go**

Remove these entries from `ModuleTypes` (map or slice — read the actual file first):
- `platform.ecs`
- `platform.apigateway`
- `platform.autoscaling`
- `platform.networking`

Remove the corresponding 4 module factory cases (switch or map entries for those types).

Remove these 15 step types from `StepTypes`/`StepFactories`:
- `step.ecs_plan`, `step.ecs_apply`, `step.ecs_status`, `step.ecs_destroy`
- `step.apigw_plan`, `step.apigw_apply`, `step.apigw_status`, `step.apigw_destroy`
- `step.scaling_plan`, `step.scaling_apply`, `step.scaling_status`, `step.scaling_destroy`
- `step.network_plan`, `step.network_apply`, `step.network_status`

Keep all `platform.dns`, `step.dns_*`, and other types.

**Step 2: Edit plugins/platform/plugin_test.go**

Remove the 4 module type string assertions for the deleted types.
Remove the 15 step type string assertions for the deleted steps.

**Step 3: Edit plugins/infra/plugin.go — add infra.autoscaling_group**

In `var infraTypes = []string{...}` (currently 13 entries, lines 15-28), add:
```go
"infra.autoscaling_group",
```

After this addition, `infraTypes` has 14 entries. The `ModuleSchemas()` function auto-generates the schema — no other changes needed in this file.

**Step 4: Edit schema/schema.go**

Remove these 4 strings from the module type list:
- `"platform.ecs"`
- `"platform.apigateway"`
- `"platform.autoscaling"`
- `"platform.networking"`

**Step 5: Edit schema/module_schema.go**

1. Remove the 4 module schema structs/entries for `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking`. Read the file to find the exact struct literals and remove them.

2. Update the `platform.dns` provider `ConfigFieldDef` description. Find the `platform.dns` schema entry and update the `provider` field description to:
```
"Provider backend: mock | aws (aws Route53 backend removed in v0.53.0; use infra.dns + workflow-plugin-aws)"
```

**Step 6: Edit schema/step_schema_builtins.go**

Remove the 15 `schema.Register(...)` calls for:
- `step.ecs_plan`, `step.ecs_apply`, `step.ecs_status`, `step.ecs_destroy`
- `step.apigw_plan`, `step.apigw_apply`, `step.apigw_status`, `step.apigw_destroy`
- `step.scaling_plan`, `step.scaling_apply`, `step.scaling_status`, `step.scaling_destroy`
- `step.network_plan`, `step.network_apply`, `step.network_status`

**Step 7: Edit cmd/wfctl/type_registry.go**

Remove entries for `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking`.

Add entry for `"infra.autoscaling_group"` — mirror the pattern used for other `infra.*` entries in that file.

**Step 8: Edit module/multi_region.go line ~117**

Change the error string from referencing `platform.ecs` to `infra.container_service`:
```go
return fmt.Errorf("platform.region %q: provider %q is not yet supported; use AWS ALB directly via platform.kubernetes or infra.container_service modules (workflow-plugin-aws)", m.name, providerType)
```

**Step 9: Edit DOCUMENTATION.md**

Remove these module rows: `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking`.
Remove these 15 step rows: all `step.ecs_*`, `step.apigw_*`, `step.scaling_*`, `step.network_*` entries.
Keep `platform.dns` module row and `step.dns_plan`, `step.dns_apply`, `step.dns_status` rows.
Add `infra.autoscaling_group` to the infra module types table.
Add a paragraph after the platform module types section:

```
> **AWS IaC modules removed (v0.53.0):** `platform.ecs`, `platform.apigateway`, `platform.autoscaling`, `platform.networking` were removed from workflow core and are now provided by [workflow-plugin-aws](https://github.com/GoCodeAlone/workflow-plugin-aws) v0.2.0+ as `infra.container_service`, `infra.api_gateway`, `infra.autoscaling_group`, `infra.vpc`/`infra.firewall`. See `docs/migrations/v0.53.0-aws-iac-removal.md`.
```

**Step 10: Verify go build compiles after registration edits**

```bash
cd /Users/jon/workspace/workflow && go build ./... 2>&1 | grep -v "_worktrees\|.claude" | head -30
```

Expected: 0 compile errors (T4 migration errors will be wired in next task, so the `if !exists` branch just returns the generic error for now — that's fine).

**Step 11: Regenerate the golden file**

```bash
cd /Users/jon/workspace/workflow && UPDATE_GOLDEN=1 go test ./schema/ -run TestEditorSchemasGoldenFile -v 2>&1
```

Expected: `--- PASS: TestEditorSchemasGoldenFile` with "updated golden file" log line.

**Step 12: Run schema tests**

```bash
cd /Users/jon/workspace/workflow && go test ./schema/ -v 2>&1 | tail -20
```

Expected: all schema tests PASS.

**Step 13: Run platform plugin tests**

```bash
cd /Users/jon/workspace/workflow && go test ./plugins/platform/... -v 2>&1 | tail -20
```

Expected: all platform plugin tests PASS.

**Step 14: Commit Task 3**

```bash
git add plugins/platform/plugin.go plugins/platform/plugin_test.go \
    plugins/infra/plugin.go schema/schema.go schema/module_schema.go \
    schema/step_schema_builtins.go cmd/wfctl/type_registry.go \
    module/multi_region.go DOCUMENTATION.md \
    schema/testdata/editor-schemas.golden.json
git commit -m "feat(#653): T3 — strip registration sites, add infra.autoscaling_group, regen golden"
```

---

### Task 4: Add internal/legacyaws + wire migration errors

**Files:**
- Create: `internal/legacyaws/types.go`
- Modify: `engine.go`
- Modify: `module/pipeline_step_registry.go`
- Modify: `cmd/wfctl/validate.go`
- Modify: `cmd/wfctl/ci_validate.go`
- Create: `engine_legacyaws_test.go` (or add to existing engine test file)
- Modify: `module/pipeline_step_registry_test.go` (add legacyaws step tests)
- Modify: `cmd/wfctl/validate_test.go` (add legacyaws validate tests)

**Step 1: Write the failing tests first (TDD)**

Add to engine tests (create `engine_legacyaws_test.go`):

```go
package workflow_test

import (
    "strings"
    "testing"
)

func TestBuildFromConfig_LegacyAWSModuleError(t *testing.T) {
    legacyTypes := []string{"platform.ecs", "platform.apigateway", "platform.autoscaling", "platform.networking"}
    for _, typ := range legacyTypes {
        t.Run(typ, func(t *testing.T) {
            cfg := minimalConfigWithModule(typ, "my-module") // helper from engine_test.go
            _, err := BuildFromConfig(cfg)
            if err == nil {
                t.Fatalf("expected migration error for %q, got nil", typ)
            }
            errStr := err.Error()
            if !strings.Contains(errStr, "v0.53.0") {
                t.Errorf("error for %q missing version, got: %s", typ, errStr)
            }
            if !strings.Contains(errStr, "workflow-plugin-aws") {
                t.Errorf("error for %q missing plugin hint, got: %s", typ, errStr)
            }
        })
    }
}
```

Add to pipeline step registry tests:
```go
func TestStepRegistry_LegacyAWSStepError(t *testing.T) {
    legacySteps := []string{
        "step.ecs_plan", "step.ecs_apply", "step.ecs_status", "step.ecs_destroy",
        "step.apigw_plan", "step.apigw_apply", "step.apigw_status", "step.apigw_destroy",
        "step.scaling_plan", "step.scaling_apply", "step.scaling_status", "step.scaling_destroy",
        "step.network_plan", "step.network_apply", "step.network_status",
    }
    r := NewStepRegistry() // read actual constructor from pipeline_step_registry.go
    for _, st := range legacySteps {
        t.Run(st, func(t *testing.T) {
            _, err := r.Create(st, nil, nil)
            if err == nil {
                t.Fatalf("expected migration error for %q, got nil", st)
            }
            if !strings.Contains(err.Error(), "v0.53.0") {
                t.Errorf("error for %q missing version: %s", st, err)
            }
        })
    }
}
```

Run: `go test ./... -run "TestBuildFromConfig_LegacyAWS|TestStepRegistry_LegacyAWS" 2>&1 | head -20`
Expected: compile error (legacyaws package doesn't exist yet).

**Step 2: Create internal/legacyaws/types.go**

```go
// Package legacyaws holds the read-only data and message formatters for the
// legacy AWS IaC module + step types removed in issue #653. Lives in
// internal/ so that both module/ and modernize/ can import it without a
// cycle (module transitively imports modernize via plugin, so modernize
// cannot import module).
package legacyaws

import (
    "fmt"
    "sort"
    "strings"
)

// RemovedInVersion is the workflow tag that ships issue #653's force-cutover.
const RemovedInVersion = "v0.53.0"

// ModuleTypes maps each removed legacy AWS module type to its infra.* successor.
var ModuleTypes = map[string]string{
    "platform.ecs":         "infra.container_service",
    "platform.apigateway":  "infra.api_gateway",
    "platform.autoscaling": "infra.autoscaling_group",
    "platform.networking":  "infra.vpc + infra.firewall",
}

// StepTypes maps each removed legacy AWS step type to its successor.
// step.network_destroy is intentionally absent — it never existed.
var StepTypes = map[string]string{
    "step.ecs_plan":      "step.iac_plan (against an infra.container_service module); required config keys: platform + state_store",
    "step.ecs_apply":     "step.iac_apply (against an infra.container_service module); required config keys: platform + state_store",
    "step.ecs_status":    "step.iac_status (against an infra.container_service module); required config keys: platform + state_store",
    "step.ecs_destroy":   "step.iac_destroy (against an infra.container_service module); required config keys: platform + state_store",
    "step.apigw_plan":    "step.iac_plan (against an infra.api_gateway module); required config keys: platform + state_store",
    "step.apigw_apply":   "step.iac_apply (against an infra.api_gateway module); required config keys: platform + state_store",
    "step.apigw_status":  "step.iac_status (against an infra.api_gateway module); required config keys: platform + state_store",
    "step.apigw_destroy": "step.iac_destroy (against an infra.api_gateway module); required config keys: platform + state_store",
    "step.scaling_plan":    "step.iac_plan (against an infra.autoscaling_group module); required config keys: platform + state_store",
    "step.scaling_apply":   "step.iac_apply (against an infra.autoscaling_group module); required config keys: platform + state_store",
    "step.scaling_status":  "step.iac_status (against an infra.autoscaling_group module); required config keys: platform + state_store",
    "step.scaling_destroy": "step.iac_destroy (against an infra.autoscaling_group module); required config keys: platform + state_store",
    "step.network_plan":   "step.iac_plan (against an infra.vpc or infra.firewall module); required config keys: platform + state_store",
    "step.network_apply":  "step.iac_apply (against an infra.vpc or infra.firewall module); required config keys: platform + state_store",
    "step.network_status": "step.iac_status (against an infra.vpc or infra.firewall module); required config keys: platform + state_store",
}

// IsModuleType reports whether t is a removed legacy AWS module type.
func IsModuleType(t string) bool { _, ok := ModuleTypes[t]; return ok }

// IsStepType reports whether t is a removed legacy AWS step type.
func IsStepType(t string) bool { _, ok := StepTypes[t]; return ok }

// FormatModuleError builds the actionable migration error for a legacy AWS module type.
func FormatModuleError(legacyType, moduleName string, iacProviderLoaded bool) error {
    successor, ok := ModuleTypes[legacyType]
    if !ok {
        return nil
    }
    pluginLine := "Install workflow-plugin-aws: https://github.com/GoCodeAlone/workflow-plugin-aws"
    if iacProviderLoaded {
        pluginLine = "workflow-plugin-aws is already loaded; your config still references the legacy module name."
    }
    var b strings.Builder
    fmt.Fprintf(&b, "unsupported legacy module type %q (module %q): this type was removed from workflow core in %s — AWS IaC moved to workflow-plugin-aws.\n\n", legacyType, moduleName, RemovedInVersion)
    b.WriteString(pluginLine)
    b.WriteString("\n\nMigrate this module to: ")
    b.WriteString(successor)
    b.WriteString(" (provider: aws)\n\nFull mapping:\n")
    keys := make([]string, 0, len(ModuleTypes))
    for k := range ModuleTypes {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        fmt.Fprintf(&b, "  %s → %s\n", k, ModuleTypes[k])
    }
    b.WriteString("\nSee docs/migrations/v0.53.0-aws-iac-removal.md")
    return fmt.Errorf("%s", b.String())
}

// FormatStepError builds the actionable migration error for a legacy AWS step type.
func FormatStepError(legacyType string, iacProviderLoaded bool) error {
    successor, ok := StepTypes[legacyType]
    if !ok {
        return nil
    }
    pluginLine := "Install workflow-plugin-aws: https://github.com/GoCodeAlone/workflow-plugin-aws"
    if iacProviderLoaded {
        pluginLine = "workflow-plugin-aws is already loaded; your config still references the legacy step name."
    }
    var b strings.Builder
    fmt.Fprintf(&b, "unsupported legacy step type %q: this step was removed from workflow core in %s — AWS IaC moved to workflow-plugin-aws.\n\n", legacyType, RemovedInVersion)
    b.WriteString(pluginLine)
    b.WriteString("\n\nMigrate this step to: ")
    b.WriteString(successor)
    b.WriteString("\n\nSee docs/migrations/v0.53.0-aws-iac-removal.md")
    return fmt.Errorf("%s", b.String())
}

// FormatDNSProviderAWSError returns the migration error for platform.dns
// configured with provider: aws (Route53 backend removed in issue #653).
func FormatDNSProviderAWSError(moduleName string) error {
    return fmt.Errorf(
        "platform.dns %q: AWS Route53 backend removed from workflow core in %s (issue #653).\n"+
            "Migrate to: infra.dns (provider: aws) with workflow-plugin-aws v0.2.0+.\n"+
            "Install: https://github.com/GoCodeAlone/workflow-plugin-aws\n"+
            "See docs/migrations/v0.53.0-aws-iac-removal.md",
        moduleName, RemovedInVersion,
    )
}
```

**Step 3: Wire legacyaws into engine.go**

In `engine.go`, add the import:
```go
"github.com/GoCodeAlone/workflow/internal/legacyaws"
```

In `BuildFromConfig()`, in the `WithExtraModuleTypes` injection loop (around line 394-402, where legacydo types are injected), add legacyaws types:
```go
for t := range legacyaws.ModuleTypes {
    extra = append(extra, t)
}
```

In the `if !exists` branch (around line 514-516), after the legacydo check, add:
```go
if legacyaws.IsModuleType(modCfg.Type) {
    _, iacLoaded := e.moduleFactories["iac.provider"]
    return legacyaws.FormatModuleError(modCfg.Type, modCfg.Name, iacLoaded)
}
```

**Step 4: Wire legacyaws into module/pipeline_step_registry.go**

Add import:
```go
"github.com/GoCodeAlone/workflow/internal/legacyaws"
```

In `Create()`, after the legacydo check (line ~45-46), add:
```go
if legacyaws.IsStepType(stepType) {
    return nil, legacyaws.FormatStepError(stepType, r.iacProviderLoaded)
}
```

**Step 5: Wire legacyaws into cmd/wfctl/validate.go**

Add import `"github.com/GoCodeAlone/workflow/internal/legacyaws"`.

In the `WithExtraModuleTypes` injection loop (line ~146-150, currently only legacydo), add:
```go
for t := range legacyaws.ModuleTypes {
    opts = append(opts, schema.WithExtraModuleTypes(t))
}
```

In the post-validate module sweep (line ~159-163), after the legacydo module check, add:
```go
if legacyaws.IsModuleType(m.Type) {
    return legacyaws.FormatModuleError(m.Type, m.Name, false)
}
// DNS provider: aws check
if m.Type == "platform.dns" {
    if cfg, ok := m.Config.(map[string]any); ok {
        if provider, _ := cfg["provider"].(string); provider == "aws" {
            return legacyaws.FormatDNSProviderAWSError(m.Name)
        }
    }
}
```

In the post-validate step sweep (line ~173-177), add:
```go
if legacyaws.IsStepType(s.Type) {
    return legacyaws.FormatStepError(s.Type, false)
}
```

**Step 6: Wire legacyaws into cmd/wfctl/ci_validate.go**

Same additions as validate.go — mirror exactly.

**Step 7: Run the tests**

```bash
cd /Users/jon/workspace/workflow && go test ./... -run "TestBuildFromConfig_LegacyAWS|TestStepRegistry_LegacyAWS|TestValidate.*Legacy" -v 2>&1 | tail -30
```

Expected: all tests PASS.

**Step 8: Run full test suite**

```bash
cd /Users/jon/workspace/workflow && go test ./... 2>&1 | tail -30
```

Expected: all tests PASS. If failures, diagnose and fix before committing.

**Step 9: Commit Task 4**

```bash
git add internal/legacyaws/types.go engine.go module/pipeline_step_registry.go \
    cmd/wfctl/validate.go cmd/wfctl/ci_validate.go \
    engine_legacyaws_test.go module/pipeline_step_registry_test.go \
    cmd/wfctl/validate_test.go
git commit -m "feat(#653): T4 — add internal/legacyaws + wire migration errors in engine + wfctl"
```

---

### Task 5: wfctl modernize rule + migration doc

**Files:**
- Create: `modernize/legacy_aws_rule.go`
- Modify: `modernize/modernize.go` (add legacyAWSRule to AllRules)
- Create: `docs/migrations/v0.53.0-aws-iac-removal.md`
- Create: `modernize/legacy_aws_rule_test.go` (tests for the rule)

**Step 1: Write the failing tests for the modernize rule**

Create `modernize/legacy_aws_rule_test.go`:

```go
package modernize_test

import (
    "strings"
    "testing"

    "github.com/GoCodeAlone/workflow/modernize"
)

func TestLegacyAWSRule_Check(t *testing.T) {
    rule := findRule("legacy-aws-types") // read AllRules, find by ID
    tests := []struct {
        name    string
        yaml    string
        wantMsg string
        fixable bool
    }{
        {
            name: "platform.ecs detected",
            yaml: "modules:\n  - name: my-ecs\n    type: platform.ecs\n",
            wantMsg: "infra.container_service",
            fixable: true,
        },
        {
            name: "platform.networking not auto-fixable",
            yaml: "modules:\n  - name: my-net\n    type: platform.networking\n",
            wantMsg: "infra.vpc",
            fixable: false,
        },
        {
            name: "step.ecs_apply not auto-fixable",
            yaml: "pipelines:\n  deploy:\n    steps:\n      - type: step.ecs_apply\n",
            wantMsg: "step.iac_apply",
            fixable: false,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            findings := check(rule, tt.yaml)
            if len(findings) == 0 {
                t.Fatal("expected findings, got none")
            }
            if !strings.Contains(findings[0].Message, tt.wantMsg) {
                t.Errorf("message %q doesn't contain %q", findings[0].Message, tt.wantMsg)
            }
            if findings[0].Fixable != tt.fixable {
                t.Errorf("fixable: got %v, want %v", findings[0].Fixable, tt.fixable)
            }
        })
    }
}

func TestLegacyAWSRule_Fix(t *testing.T) {
    rule := findRule("legacy-aws-types")
    // Auto-fixable: platform.ecs → infra.container_service
    input := "modules:\n  - name: my-ecs\n    type: platform.ecs\n"
    result, changes := fix(rule, input)
    if len(changes) == 0 {
        t.Fatal("expected changes, got none")
    }
    if !strings.Contains(result, "infra.container_service") {
        t.Errorf("result %q doesn't contain infra.container_service", result)
    }
    // platform.networking NOT auto-fixed
    input2 := "modules:\n  - name: my-net\n    type: platform.networking\n"
    _, changes2 := fix(rule, input2)
    if len(changes2) != 0 {
        t.Error("platform.networking should not be auto-fixed")
    }
}
```

Read `modernize/legacy_do_rule_test.go` to find the `findRule`, `check`, `fix` helpers and mirror them.

Run: `go test ./modernize/ -run TestLegacyAWSRule -v 2>&1`
Expected: compile error (rule doesn't exist yet).

**Step 2: Create modernize/legacy_aws_rule.go**

Mirror `modernize/legacy_do_rule.go` exactly. Key differences:
- Rule ID: `"legacy-aws-types"`
- Description: `"Rewrite legacy AWS module/step types to infra.* IaC successors (issue #653)."`
- `moduleMap`: `platform.ecs→infra.container_service`, `platform.apigateway→infra.api_gateway`, `platform.autoscaling→infra.autoscaling_group` (auto-fixable; NOT `platform.networking` — 1→2 split)
- `gapTypes`: `platform.networking → "splits into infra.vpc + infra.firewall — manual rewrite required"`
- `stepMap`: all 15 step types with successors (all marked `Fixable: false` — step config-shape mismatch per #617 retro)
- Import: `"github.com/GoCodeAlone/workflow/internal/legacyaws"` (for `legacyaws.RemovedInVersion`)
- Use `walkTypeNodes` (already defined in legacy_do_rule.go — same package, usable directly)

```go
package modernize

import (
    "fmt"

    "github.com/GoCodeAlone/workflow/internal/legacyaws"
    "gopkg.in/yaml.v3"
)

// legacyAWSRule flags legacy AWS module + step types and rewrites
// module types to their infra.* IaC successors (issue #653).
//
// IMPORTANT: The Fix function ONLY renames the type: key for 3 module types
// (ecs, apigateway, autoscaling). platform.networking is NOT auto-rewritten
// (1→2 split). All 15 step types are NOT auto-rewritten: step.iac_* require
// different config keys (platform + state_store) vs legacy keys. Operator must
// rewrite step config manually per migration guide (docs/migrations/v0.53.0-aws-iac-removal.md).
func legacyAWSRule() Rule {
    moduleMap := map[string]string{
        "platform.ecs":         "infra.container_service",
        "platform.apigateway":  "infra.api_gateway",
        "platform.autoscaling": "infra.autoscaling_group",
        // platform.networking intentionally NOT auto-fixed: 1→2 split
    }
    stepMap := map[string]string{
        "step.ecs_plan":      "step.iac_plan",
        "step.ecs_apply":     "step.iac_apply",
        "step.ecs_status":    "step.iac_status",
        "step.ecs_destroy":   "step.iac_destroy",
        "step.apigw_plan":    "step.iac_plan",
        "step.apigw_apply":   "step.iac_apply",
        "step.apigw_status":  "step.iac_status",
        "step.apigw_destroy": "step.iac_destroy",
        "step.scaling_plan":    "step.iac_plan",
        "step.scaling_apply":   "step.iac_apply",
        "step.scaling_status":  "step.iac_status",
        "step.scaling_destroy": "step.iac_destroy",
        "step.network_plan":   "step.iac_plan",
        "step.network_apply":  "step.iac_apply",
        "step.network_status": "step.iac_status",
    }
    gapTypes := map[string]string{
        "platform.networking": "splits into infra.vpc + infra.firewall — manual rewrite required",
    }

    return Rule{
        ID:          "legacy-aws-types",
        Description: "Rewrite legacy AWS module/step types to infra.* IaC successors (issue #653).",
        Severity:    "error",
        Check: func(root *yaml.Node, raw []byte) []Finding {
            var out []Finding
            walkTypeNodes(root, func(typeVal *yaml.Node) {
                if successor, ok := moduleMap[typeVal.Value]; ok {
                    out = append(out, Finding{
                        RuleID:  "legacy-aws-types",
                        Line:    typeVal.Line,
                        Message: fmt.Sprintf("%s removed in %s; rewrite to %s (provider: aws) — requires workflow-plugin-aws", typeVal.Value, legacyaws.RemovedInVersion, successor),
                        Fixable: true,
                    })
                }
                if successor, ok := stepMap[typeVal.Value]; ok {
                    out = append(out, Finding{
                        RuleID:  "legacy-aws-types",
                        Line:    typeVal.Line,
                        Message: fmt.Sprintf("%s removed in %s; manually rewrite to %s with config keys platform + state_store (see docs/migrations/v0.53.0-aws-iac-removal.md) — requires workflow-plugin-aws", typeVal.Value, legacyaws.RemovedInVersion, successor),
                        Fixable: false,
                    })
                }
                if reason, ok := gapTypes[typeVal.Value]; ok {
                    out = append(out, Finding{
                        RuleID:  "legacy-aws-types",
                        Line:    typeVal.Line,
                        Message: fmt.Sprintf("%s removed in %s — %s", typeVal.Value, legacyaws.RemovedInVersion, reason),
                        Fixable: false,
                    })
                }
            })
            return out
        },
        Fix: func(root *yaml.Node) []Change {
            var out []Change
            walkTypeNodes(root, func(typeVal *yaml.Node) {
                if successor, ok := moduleMap[typeVal.Value]; ok {
                    old := typeVal.Value
                    typeVal.Value = successor
                    out = append(out, Change{
                        RuleID:      "legacy-aws-types",
                        Line:        typeVal.Line,
                        Description: fmt.Sprintf("rewrote %s → %s", old, successor),
                    })
                }
                // stepMap and gapTypes are intentionally NOT rewritten.
            })
            return out
        },
    }
}
```

**Step 3: Register in modernize/modernize.go**

In `AllRules()` (line 36-48), add `legacyAWSRule()` after `legacyDORule()`:
```go
legacyDORule(),
legacyAWSRule(),
```

**Step 4: Run the modernize rule tests**

```bash
cd /Users/jon/workspace/workflow && go test ./modernize/ -run TestLegacyAWSRule -v 2>&1
```

Expected: `--- PASS: TestLegacyAWSRule_Check` and `--- PASS: TestLegacyAWSRule_Fix`.

**Step 5: Create docs/migrations/v0.53.0-aws-iac-removal.md**

Write a migration guide. Keep it concise. Cover:
- What changed and when (v0.53.0, issue #653)
- The 4 module type renames (with config diff examples)
- The 15 step type changes (manual — different config keys)
- The platform.dns provider: aws change (→ infra.dns)
- wfctl modernize command usage
- workflow-plugin-aws install link

**Step 6: Run all modernize tests**

```bash
cd /Users/jon/workspace/workflow && go test ./modernize/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

**Step 7: Commit Task 5**

```bash
git add modernize/legacy_aws_rule.go modernize/legacy_aws_rule_test.go \
    modernize/modernize.go docs/migrations/v0.53.0-aws-iac-removal.md
git commit -m "feat(#653): T5 — wfctl modernize rule legacy-aws-types + migration doc"
```

---

### Task 6: go mod tidy + CI grep gate

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `example/go.mod`, `example/go.sum`
- Modify: `.github/workflows/ci.yml` (add aws-sdk-banned job)

**Step 1: Run go mod tidy on root**

```bash
cd /Users/jon/workspace/workflow && go mod tidy 2>&1
```

Expected: no errors. Verify:

```bash
grep "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" go.mod
```

Expected: 0 matches (all 3 freed packages dropped).

**Step 2: Run go mod tidy on example/**

```bash
cd /Users/jon/workspace/workflow/example && go mod tidy 2>&1
```

Expected: no errors. Verify:

```bash
grep "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" /Users/jon/workspace/workflow/example/go.mod
```

Expected: 0 matches.

**Step 3: Run full test suite post-tidy to confirm nothing broken**

```bash
cd /Users/jon/workspace/workflow && go test ./... 2>&1 | tail -30
```

Expected: all tests PASS.

**Step 4: Add the CI grep gate to .github/workflows/ci.yml**

Find the `godo-banned` job in `.github/workflows/ci.yml` (around lines 380-395) and add a parallel `aws-sdk-banned` job immediately after it with the same structure:

```yaml
  aws-sdk-banned:
    name: aws-sdk-service-packages-banned
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Verify freed AWS SDK service packages absent from Go source
        run: |
          ! grep -rn --include="*.go" \
            --exclude-dir=_worktrees \
            --exclude-dir=.worktrees \
            --exclude-dir=.claude \
            --exclude="aws_absent_test.go" \
            "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" .
      - name: Verify freed AWS SDK service packages absent from go.mod files
        run: |
          ! grep -qH "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" go.mod example/go.mod
```

**Step 5: Run the grep gate locally to verify it passes**

```bash
cd /Users/jon/workspace/workflow && ! grep -rn --include="*.go" \
  --exclude-dir=_worktrees \
  --exclude-dir=.worktrees \
  --exclude-dir=.claude \
  --exclude="aws_absent_test.go" \
  "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" . && echo "PASS: no freed imports found"
```

Expected: `PASS: no freed imports found`

```bash
! grep -qH "aws-sdk-go-v2/service/apigatewayv2\|aws-sdk-go-v2/service/applicationautoscaling\|aws-sdk-go-v2/service/route53" /Users/jon/workspace/workflow/go.mod /Users/jon/workspace/workflow/example/go.mod && echo "PASS: go.mod clean"
```

Expected: `PASS: go.mod clean`

**Step 6: Run go build for final compile check**

```bash
cd /Users/jon/workspace/workflow && go build ./... 2>&1 | grep -v "_worktrees\|.claude"
```

Expected: 0 errors.

**Step 7: Downstream consumer grep (Assumption #2 check)**

```bash
for repo in buymywishlist core-dump workflow-cloud workflow-scenarios; do
  if [ -d "/Users/jon/workspace/$repo" ]; then
    echo "=== $repo ==="
    grep -rn "platform\.ecs\|platform\.apigateway\|platform\.autoscaling\|platform\.networking" /Users/jon/workspace/$repo --include="*.yaml" --include="*.yml" | grep -v "node_modules\|vendor\|_worktrees" || echo "clean"
  fi
done
```

Expected: "clean" for all repos (or document any findings before opening PR).

**Step 8: Commit Task 6**

```bash
git add go.mod go.sum example/go.mod example/go.sum .github/workflows/ci.yml
git commit -m "feat(#653): T6 — go mod tidy drops freed AWS SDK service packages + CI grep gate"
```

---

## Pre-PR verification (run after all 6 tasks)

```bash
# Full test suite
cd /Users/jon/workspace/workflow && go test ./... 2>&1 | tail -20

# Race detector
cd /Users/jon/workspace/workflow && go test -race ./... 2>&1 | tail -10

# Lint
cd /Users/jon/workspace/workflow && go fmt ./... && golangci-lint run 2>&1 | tail -20

# Build both binaries
cd /Users/jon/workspace/workflow && go build -o /tmp/workflow-server ./cmd/server && echo "server OK"
cd /Users/jon/workspace/workflow && go build -o /tmp/wfctl ./cmd/wfctl && echo "wfctl OK"

# Smoke: wfctl modernize detects platform.ecs
echo "modules:\n  - name: my-app\n    type: platform.ecs" > /tmp/test-legacy-aws.yaml
/tmp/wfctl modernize /tmp/test-legacy-aws.yaml 2>&1

# Smoke: wfctl validate rejects platform.ecs with actionable error
/tmp/wfctl validate /tmp/test-legacy-aws.yaml 2>&1 | grep "v0.53.0"
```

Expected for each:
- Test suite: all PASS
- Race: all PASS
- Lint: 0 errors
- `server OK`, `wfctl OK`
- modernize: shows finding with `infra.container_service` in message
- validate: error contains `v0.53.0`

Rollback: `git revert <merge-sha>` to revert if any post-merge issue; consumers pin previous tag; `cloud_account_aws.go` was never removed so AWS credential resolution continues unaffected.
