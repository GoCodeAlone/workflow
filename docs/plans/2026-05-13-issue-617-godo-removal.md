# Remove godo from workflow core (issue #617) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Force-cutover delete the six godo-importing legacy DigitalOcean IaC modules + five legacy pipeline steps from workflow core, remove `github.com/digitalocean/godo` from `go.mod` (root + `example/`), and add actionable load-time migration errors so user configs that still reference the legacy types get a clear pointer to `workflow-plugin-digitalocean` v0.12.0+ and the `infra.*` IaC type system.

**Architecture:** Single PR. Pure deletion of 12 files + edits to 10 registration sites + new migration-error guards in `engine.go` (module path) and `module/pipeline_step_registry.go` (step path). Plugin-loaded detection via a single `_, ok := e.moduleFactories["iac.provider"]` lookup. CI gate: `!`-prefixed grep over `*.go` and `go.mod` files (root + `example/`) to prevent regression. `wfctl modernize` rules auto-rewrite legacy YAML.

**Tech Stack:** Go 1.26, `github.com/GoCodeAlone/workflow` engine, `cmd/wfctl`, `modernize/` package, GitHub Actions CI.

**Base branch:** `main`

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 5
**Estimated Lines of Change:** ~3206 deleted + ~400 added + ~80 edited = net ~−2700 (informational; not enforced)

**Out of scope:**
- AWS SDK audit (separate follow-up issue filed at end of T5, addresses `iam/`, `plugin/rbac/aws.go`, `artifact/s3.go`, `platform/providers/aws/` IaC drivers).
- New plugin-side step types (`step.iac_logs`, `step.iac_scale`) — tracked as follow-up issues in `workflow-plugin-digitalocean` (filed in T5; out of scope for this plan).
- Changes to `module/iac_state_spaces.go` — it uses `aws-sdk-go-v2` (not godo) for S3-compat blob access.
- Downstream consumer migration PRs (`buymywishlist-phase3`, `workflow-scenarios` scenarios 42/51) — tracked as follow-ups; the engine cutover PR ships independently with migration errors as the user-facing path.
- Compatibility shim, build tag fence, deprecation period — explicitly rejected in design.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat: remove godo from core (issue #617) | Task 1, Task 2, Task 3, Task 4, Task 5 | `feat/issue-617-godo-removal` |

**Status:** Locked 2026-05-13T00:00:00Z

---

### Task 1: Delete legacy DO module + step files

**Files:**
- Delete: `module/platform_do_app.go`
- Delete: `module/platform_do_app_test.go`
- Delete: `module/platform_do_database.go`
- Delete: `module/platform_do_database_test.go`
- Delete: `module/platform_do_dns.go`
- Delete: `module/platform_do_dns_test.go`
- Delete: `module/platform_do_networking.go`
- Delete: `module/platform_do_networking_test.go`
- Delete: `module/platform_doks.go`
- Delete: `module/platform_doks_test.go`
- Delete: `module/cloud_account_do.go`
- Delete: `module/pipeline_step_do.go`
- Test: `module/godo_absent_test.go` (new — asserts the godo import is gone from the package)

**Step 1: Write the failing test**

Create `module/godo_absent_test.go`:

```go
package module_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestGodoNotImported_InModulePackage asserts no file under module/ imports
// github.com/digitalocean/godo. This is the regression gate for issue #617.
func TestGodoNotImported_InModulePackage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/digitalocean/godo" {
				t.Errorf("%s imports github.com/digitalocean/godo (issue #617 — moved to workflow-plugin-digitalocean)", f)
			}
		}
	}
}
```

**Step 2: Run test to verify it fails (godo files still present)**

Run: `go test ./module -run TestGodoNotImported_InModulePackage -v`
Expected: FAIL with 6 lines naming each godo-importing file.

**Step 3: Delete the 12 legacy files**

```bash
rm module/platform_do_app.go module/platform_do_app_test.go \
   module/platform_do_database.go module/platform_do_database_test.go \
   module/platform_do_dns.go module/platform_do_dns_test.go \
   module/platform_do_networking.go module/platform_do_networking_test.go \
   module/platform_doks.go module/platform_doks_test.go \
   module/cloud_account_do.go module/pipeline_step_do.go
```

**Step 4: Verify package still parses (will fail at link time due to registrations — that is T2's problem)**

Run: `go vet ./module/...`
Expected: clean (or fails only on undefined symbols *outside* the `module` package, e.g., in `plugins/platform/`). If anything inside `module/` fails to compile, the deletion missed a sibling file — investigate.

**Step 5: Run T1 regression test**

Run: `go test ./module -run TestGodoNotImported_InModulePackage -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add module/godo_absent_test.go module/
git commit -m "$(cat <<'EOF'
feat(#617): delete legacy DO modules (godo importers)

Removes 12 files / ~3206 LOC. Registration sites cleaned in T2.

* platform_do_app.go + test
* platform_do_database.go + test
* platform_do_dns.go + test
* platform_do_networking.go + test
* platform_doks.go + test
* cloud_account_do.go (DO credential resolvers + doClient())
* pipeline_step_do.go (5 DO App Platform step types)

Adds godo_absent_test.go as a regression gate inside module/.
EOF
)"
```

**Rollback:** `git revert <T1-commit-sha>` restores the 12 files; combined with T2/T3 revert restores all registrations and migration errors.

---

### Task 2: Strip registration sites and remap detection hooks

**Files:**
- Modify: `plugins/platform/plugin.go` (drop 5 module factories + 5 step factories + 10 strings from `ModuleTypes` / `StepTypes` slices)
- Modify: `plugins/platform/plugin_test.go` (drop 10 string-presence assertions)
- Modify: `schema/schema.go` (drop 5 module-type entries + 5 step-type entries from the registry slices)
- Modify: `schema/module_schema.go` (drop 5 module schemas + 5 step descriptions)
- Modify: `schema/step_schema_builtins.go` (drop 5 `Register(&StepSchema{Type: "step.do_*"})` calls)
- Modify: `cmd/wfctl/type_registry.go` (drop 5 module entries + 5 step entries from the type-registry map)
- Modify: `cmd/wfctl/infra.go:577` — change `return t == "infra.container_service" || t == "platform.do_app"` to `return t == "infra.container_service"`.
- Modify: `cmd/wfctl/deploy_providers.go:419-424` — drop the `"platform.do_app"` line from the `deployTargetTypes` slice.
- Modify: `cmd/wfctl/ci_run_dryrun.go:178-183` — drop the `"platform.do_app"` line from the `deployTargetTypes` slice.
- Modify: `cmd/wfctl/deploy.go:839,901` — the `wfctl deploy cloud` subcommand collects modules via `strings.HasPrefix(m.Type, "platform.")` and errors with `"no platform.* modules found"` when none match. Post-cutover the user's modern config uses `infra.*` types; both call sites must include `infra.*` as well. Replace the prefix check with `strings.HasPrefix(m.Type, "platform.") || strings.HasPrefix(m.Type, "infra.")` and update the error message to `"no platform.* or infra.* modules found in config — nothing to deploy"`. Header comment on line 781 updated to reference both prefixes. **Rename the local slice variable `platformModules` to `deployTargetModules`** in the same edit so the name reflects what it now contains.
- Modify: `cmd/wfctl/validate.go:145` — inject `legacydo.ModuleTypes` keys into the local `opts` slice via `schema.WithExtraModuleTypes(...)` before calling `schema.ValidateConfig`, then add a post-`ValidateConfig` legacy-type sweep that emits `legacydo.FormatModuleError` / `legacydo.FormatStepError` if any module / step type is legacy. Without this, schema rejects legacy types with the generic `"unknown module type"` message before the migration error can fire (cycle-6 C-1).
- Modify: `cmd/wfctl/ci_validate.go:134` — same edit pattern as `validate.go`.
- Modify: `module/multi_region.go:123` — replace the error message text (see Step 3).
- Modify: `cmd/wfctl/infra_apply_test.go:1990` — the negative-test YAML fixture uses `type: platform.do_app`. Replace with `type: example.legacy_unknown` (a synthetic type that will never be registered) so the test's intent (negative coverage for unknown types) is preserved without referencing a removed type.
- Test: `cmd/wfctl/legacy_do_types_removed_test.go` (new — asserts the type registry no longer contains the legacy keys)

**Step 1: Write the failing test**

Create `cmd/wfctl/legacy_do_types_removed_test.go`:

```go
package main

import "testing"

// TestLegacyDOTypesAbsent_FromTypeRegistry locks the post-cutover state of
// cmd/wfctl/type_registry.go for issue #617. If any legacy type leaks back in,
// this test fires and the CI gate fires.
func TestLegacyDOTypesAbsent_FromTypeRegistry(t *testing.T) {
	modules := KnownModuleTypes()
	steps := KnownStepTypes()
	legacyModules := []string{
		"platform.do_app", "platform.do_database", "platform.do_dns",
		"platform.do_networking", "platform.doks",
	}
	legacySteps := []string{
		"step.do_deploy", "step.do_status", "step.do_logs",
		"step.do_scale", "step.do_destroy",
	}
	for _, tname := range legacyModules {
		if _, ok := modules[tname]; ok {
			t.Errorf("module type registry still contains legacy DO type %q (issue #617)", tname)
		}
	}
	for _, tname := range legacySteps {
		if _, ok := steps[tname]; ok {
			t.Errorf("step type registry still contains legacy DO type %q (issue #617)", tname)
		}
	}
}
```

(API confirmed against `cmd/wfctl/type_registry.go:25` `KnownModuleTypes()` and `:727` `KnownStepTypes()`.)

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/wfctl -run TestLegacyDOTypesAbsent_FromTypeRegistry -v`
Expected: FAIL with 10 lines naming each legacy type still in the registry.

**Step 3: Apply the registration deletions and detection-hook remappings**

For each file in the Files list, perform the listed deletion. The `module/multi_region.go:123` rewrite:

```go
// Before:
return fmt.Errorf("platform.region %q: provider %q is not yet supported; use platform.doks modules per region for DigitalOcean multi-region deployments", m.name, providerType)

// After:
return fmt.Errorf("platform.region %q: provider %q is not yet supported; for DigitalOcean multi-region, use infra.k8s_cluster modules per region with provider: digitalocean (requires workflow-plugin-digitalocean)", m.name, providerType)
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl -run TestLegacyDOTypesAbsent_FromTypeRegistry -v`
Expected: PASS.

Run: `go build ./...`
Expected: clean.

Run: `go test ./plugins/platform/... ./schema/... ./module/... ./cmd/wfctl/...`
Expected: PASS — any test that asserted the presence of a legacy `platform.do_*` or `step.do_*` was updated in this task to assert its absence.

**Step 5: Commit**

```bash
git add plugins/ schema/ cmd/wfctl/ module/multi_region.go cmd/wfctl/legacy_do_types_removed_test.go
git commit -m "$(cat <<'EOF'
feat(#617): strip DO registration sites + remap wfctl detection hooks

* plugins/platform: drop 5 module + 5 step factories.
* schema/*: drop 10 entries from registries and schema descriptions.
* cmd/wfctl/type_registry.go: drop 10 type entries.
* cmd/wfctl/{infra.go,deploy_providers.go,ci_run_dryrun.go}: remap
  isContainerType and deployTargetTypes to infra.container_service only.
* module/multi_region.go: rewrite DOKS multi-region hint to point at
  infra.k8s_cluster + workflow-plugin-digitalocean.
* cmd/wfctl/infra_apply_test.go: replace platform.do_app negative-test
  fixture with example.legacy_unknown synthetic type.

Adds legacy_do_types_removed_test.go as a registry-absence regression gate.
EOF
)"
```

**Rollback:** `git revert <T2-commit-sha>` restores all 10 registration sites; the package will fail to compile until T1 is also reverted (the factories reference deleted symbols).

---

### Task 3: Add load-time migration error guards (module + step)

**Files:**
- Modify: `engine.go:508` — replace the single `unknown module type` error with a legacy-DO-aware branch (see Step 3).
- Modify: `module/pipeline_step_registry.go:35` — replace the single `unknown step type` error with the same legacy-DO-aware branch for step types.
- Create: `internal/legacydo/types.go` — **leaf package** containing the legacy-type lookup tables, the `RemovedInVersion` constant, and the message-formatter helpers. Lives in `internal/` so neither `module/` nor `modernize/` transitively imports it via any indirect path. Both packages import it directly: `module/` for the engine + step guard wiring; `modernize/` for the rewrite rule. **Architectural reason:** `module` transitively imports `modernize` via `plugin` (`go list -deps github.com/GoCodeAlone/workflow/module | grep modernize` returns a match — `plugin/manifest.go` and `plugin/engine_plugin.go` both import `modernize`). Therefore `modernize` cannot import `module` directly; a shared leaf package is the only cycle-free way to share the constants.
- Test: `engine_legacy_do_migration_test.go` (new — covers all 5 module types × {plugin loaded, plugin not loaded})
- Test: `module/pipeline_step_legacy_do_migration_test.go` (new — covers all 5 step types × {plugin loaded, plugin not loaded})

**Step 1: Write the failing tests**

Create `engine_legacy_do_migration_test.go` at repo root (in-package — same package convention as `engine_test.go`):

```go
package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

// newTestEngine builds an isolated StdEngine with no plugins loaded — required
// so that the iac.provider factory-map lookup is deterministically false in
// the "plugin not loaded" test, and so that the manual AddModuleType stub in
// the "plugin loaded" test is the only factory in the map. This intentionally
// differs from setupEngineTest (engine_test.go), which calls loadAllPlugins.
// Reuses the `mockLogger` type already defined in engine_test.go — both files
// are in package workflow so the type is visible at compile time. DO NOT
// redeclare it here.
func newTestEngine(t *testing.T) *StdEngine {
	t.Helper()
	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	if err := app.Init(); err != nil {
		t.Fatalf("app.Init: %v", err)
	}
	return NewStdEngine(app, logger)
}

func TestLegacyDOModuleError_PluginNotLoaded(t *testing.T) {
	cases := []struct{ legacyType, hint string }{
		{"platform.do_app", "infra.container_service"},
		{"platform.do_database", "infra.database"},
		{"platform.do_dns", "infra.dns"},
		{"platform.do_networking", "infra.vpc"},
		{"platform.doks", "infra.k8s_cluster"},
	}
	for _, tc := range cases {
		t.Run(tc.legacyType, func(t *testing.T) {
			e := newTestEngine(t)
			cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{{Name: "x", Type: tc.legacyType, Config: map[string]any{}}}}
			err := e.BuildFromConfig(cfg)
			if err == nil {
				t.Fatalf("expected error for legacy type %q", tc.legacyType)
			}
			msg := err.Error()
			for _, want := range []string{
				"removed from workflow core",
				"workflow-plugin-digitalocean",
				"Install workflow-plugin-digitalocean",
				tc.hint,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error for %q missing %q; got: %s", tc.legacyType, want, msg)
				}
			}
		})
	}
}

func TestLegacyDOModuleError_PluginLoaded(t *testing.T) {
	e := newTestEngine(t)
	// Register a stub iac.provider factory to simulate workflow-plugin-digitalocean
	// being loaded. ModuleFactory signature: func(name string, config map[string]any) modular.Module.
	e.AddModuleType("iac.provider", func(name string, cfg map[string]any) modular.Module { return nil })

	cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{{Name: "x", Type: "platform.do_app", Config: map[string]any{}}}}
	err := e.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already loaded") {
		t.Errorf("plugin-loaded branch must say 'already loaded'; got: %s", msg)
	}
	if strings.Contains(msg, "Install workflow-plugin-digitalocean") {
		t.Errorf("plugin-loaded branch must NOT instruct install; got: %s", msg)
	}
}
```

(APIs verified: `NewStdEngine(app modular.Application, logger modular.Logger)` at `engine.go:146`; `AddModuleType(moduleType string, factory ModuleFactory)` at `engine.go:210`; `ModuleFactory` is `func(name string, config map[string]any) modular.Module`. Test package convention matches `engine_test.go:1` — `package workflow`.)

Create `module/pipeline_step_legacy_do_migration_test.go`:

```go
package module_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func TestLegacyDOStepError_PluginNotLoaded(t *testing.T) {
	// step.do_logs / step.do_scale have GAP messages; the others map 1:1 to step.iac_*.
	cases := []struct{ step, mustContain string }{
		{"step.do_deploy", "step.iac_apply"},
		{"step.do_status", "step.iac_status"},
		{"step.do_destroy", "step.iac_destroy"},
		{"step.do_logs", "wfctl infra logs"},
		{"step.do_scale", "instance_count"},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			r := module.NewStepRegistry() // fresh registry — iacProviderLoaded defaults to false
			_, err := r.Create(tc.step, "x", map[string]any{}, nil)
			if err == nil {
				t.Fatalf("expected error for %q", tc.step)
			}
			msg := err.Error()
			for _, want := range []string{
				"removed from workflow core",
				"workflow-plugin-digitalocean",
				"Install workflow-plugin-digitalocean",
				tc.mustContain,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error for %q missing %q; got: %s", tc.step, want, msg)
				}
			}
		})
	}
}

func TestLegacyDOStepError_PluginLoaded(t *testing.T) {
	// Symmetric to TestLegacyDOModuleError_PluginLoaded — flips the per-registry
	// flag and confirms the step guard's "already loaded" branch fires.
	r := module.NewStepRegistry()
	r.SetIaCProviderLoaded(true)
	_, err := r.Create("step.do_deploy", "x", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already loaded") {
		t.Errorf("plugin-loaded branch must say 'already loaded'; got: %s", msg)
	}
	if strings.Contains(msg, "Install workflow-plugin-digitalocean") {
		t.Errorf("plugin-loaded branch must NOT instruct install; got: %s", msg)
	}
}
```

(API verified: `module.NewStepRegistry()` at `module/pipeline_step_registry.go:18`; `(*StepRegistry).Create(stepType, name string, config map[string]any, app any)` at `:32`. Empty registry exercises the unknown-type fallback path where the legacy guard fires.)

**Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'TestLegacyDO(Module|Step)Error' -v`
Expected: FAIL — the engine currently emits the generic `"unknown module type %q for module %q"` and `"unknown step type: %s"` messages; neither mentions godo / workflow-plugin-digitalocean / infra.*.

**Step 3: Implement the migration helper and wire it into both error paths**

Create `internal/legacydo/types.go` (leaf package — imports only stdlib, so both `module/` and `modernize/` can import it without cycles):

```go
// Package legacydo holds the read-only data and message formatters for the
// legacy DigitalOcean module + step types removed in issue #617. Lives in
// internal/ so that both module/ and modernize/ can import it without a
// cycle (module transitively imports modernize via plugin, so modernize
// cannot import module).
package legacydo

import (
	"fmt"
	"sort"
	"strings"
)

// RemovedInVersion is the workflow tag that ships issue #617's force-cutover.
// Used in every legacy-DO migration error and in the wfctl modernize rule.
// Update both this constant and the docs/migrations/v<X>-godo-removal.md
// filename when the release tag is finalised.
const RemovedInVersion = "v0.52.0"

// ModuleTypes maps each removed legacy DigitalOcean module type to its
// infra.* IaC successor (issue #617).
var ModuleTypes = map[string]string{
	"platform.do_app":        "infra.container_service",
	"platform.do_database":   "infra.database",
	"platform.do_dns":        "infra.dns",
	"platform.do_networking": "infra.vpc + infra.firewall",
	"platform.doks":          "infra.k8s_cluster",
}

// StepTypes maps each removed legacy DigitalOcean step type to its
// successor or to a workaround when no 1:1 successor exists.
var StepTypes = map[string]string{
	"step.do_deploy":  "step.iac_apply (against an infra.container_service module)",
	"step.do_status":  "step.iac_status (against an infra.container_service module)",
	"step.do_destroy": "step.iac_destroy (against an infra.container_service module)",
	"step.do_logs":    "no direct pipeline-step equivalent; use `wfctl infra logs` ad-hoc, or rely on the DO plugin's Troubleshoot hook on step.iac_apply failure",
	"step.do_scale":   "no direct pipeline-step equivalent; update instance_count in the infra.container_service module config and re-run step.iac_apply",
}

// IsModuleType reports whether t is a removed legacy DO module type.
func IsModuleType(t string) bool { _, ok := ModuleTypes[t]; return ok }

// IsStepType reports whether t is a removed legacy DO step type.
func IsStepType(t string) bool { _, ok := StepTypes[t]; return ok }

// FormatModuleError builds the actionable migration error for a legacy
// DO module type. iacProviderLoaded indicates whether the iac.provider factory
// is registered in the engine — used to branch between the "install plugin"
// and "config-only issue" messages.
func FormatModuleError(legacyType, moduleName string, iacProviderLoaded bool) error {
	successor, ok := ModuleTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-digitalocean: https://github.com/GoCodeAlone/workflow-plugin-digitalocean"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-digitalocean is already loaded; your config still references the legacy module name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy module type %q (module %q): this type was removed from workflow core in %s — DigitalOcean IaC moved to workflow-plugin-digitalocean.\n\n", legacyType, moduleName, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this module to: ")
	b.WriteString(successor)
	b.WriteString(" (provider: digitalocean)\n\nFull mapping:\n")
	keys := make([]string, 0, len(ModuleTypes))
	for k := range ModuleTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s → %s\n", k, ModuleTypes[k])
	}
	b.WriteString("\nSee docs/migrations/v0.52.0-godo-removal.md")
	return fmt.Errorf("%s", b.String())
}

// FormatStepError builds the actionable migration error for a legacy
// DO step type.
func FormatStepError(legacyType string, iacProviderLoaded bool) error {
	successor, ok := StepTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-digitalocean: https://github.com/GoCodeAlone/workflow-plugin-digitalocean"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-digitalocean is already loaded; your config still references the legacy step name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy step type %q: this step was removed from workflow core in %s — DigitalOcean IaC moved to workflow-plugin-digitalocean.\n\n", legacyType, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this step to: ")
	b.WriteString(successor)
	b.WriteString("\n\nSee docs/migrations/v0.52.0-godo-removal.md")
	return fmt.Errorf("%s", b.String())
}
```

Modify `engine.go:508`:

```go
// Before:
factory, exists := e.moduleFactories[modCfg.Type]
if !exists {
    return fmt.Errorf("unknown module type %q for module %q — ensure the required plugin is loaded", modCfg.Type, modCfg.Name)
}

// After:
factory, exists := e.moduleFactories[modCfg.Type]
if !exists {
    if legacydo.IsModuleType(modCfg.Type) {
        _, iacLoaded := e.moduleFactories["iac.provider"]
        return legacydo.FormatModuleError(modCfg.Type, modCfg.Name, iacLoaded)
    }
    return fmt.Errorf("unknown module type %q for module %q — ensure the required plugin is loaded", modCfg.Type, modCfg.Name)
}
```

**Schema-validation ordering caveat (critical):** `schema.ValidateConfig(cfg, valOpts...)` at `engine.go:400` runs BEFORE the factory loop at `:506`. After T2 removes the five legacy DO types from `schema/schema.go`'s allow-list, schema validation will reject the config with the generic `"unknown module type"` schema error before the factory guard ever runs — making `legacydo.FormatModuleError` unreachable for module types. To fix, **add the five legacy DO module types to the `WithExtraModuleTypes` call** so schema validation passes them through and the factory-lookup guard becomes the rejection point. Restructure the existing guarded block (`engine.go:393-398`) into unconditional code (eliminates a `staticcheck SA4010` always-true-condition lint failure):

```go
// Replace engine.go:393-398 with:
extra := make([]string, 0, len(e.moduleFactories)+len(legacydo.ModuleTypes))
for t := range e.moduleFactories {
    extra = append(extra, t)
}
// Pass legacy DO module types through schema so the factory-loop guard
// (which emits legacydo.FormatModuleError) is the rejection point —
// schema rejection produces a generic error and would mask the
// actionable migration message (issue #617).
for t := range legacydo.ModuleTypes {
    extra = append(extra, t)
}
valOpts = append(valOpts, schema.WithExtraModuleTypes(extra...))
```

**Step types do NOT need a schema-level injection:** `schema.ValidateConfig` does not validate `pipelines[*].steps[*].type` (no `WithExtraStepTypes` function exists; verified). The step migration guard at the `StepRegistry.Create` rejection point is therefore the only gate for legacy step types, which is exactly what we want.

(Add `"github.com/GoCodeAlone/workflow/internal/legacydo"` to engine.go imports.)

**`wfctl validate` and `wfctl ci validate` (acceptance criterion #3):** these two commands call `schema.ValidateConfig` directly (`cmd/wfctl/validate.go:145`, `cmd/wfctl/ci_validate.go:134`) WITHOUT going through `engine.BuildFromConfig`. Without injecting `legacydo.ModuleTypes` into their local `opts` slices, they would emit the generic schema error instead of routing to the migration message. To satisfy AC3 on the validate paths, mirror the same injection in both wfctl commands. Add to **T2** (since these are wfctl-side registration / validation hooks alongside the other T2 wfctl edits):

```go
// In cmd/wfctl/validate.go validateFile() — before line 145 schema.ValidateConfig call,
// after the existing opts slice is assembled:
for t := range legacydo.ModuleTypes {
    opts = append(opts, schema.WithExtraModuleTypes(t))
}
// Same edit in cmd/wfctl/ci_validate.go ciValidateFile() before line 134.
```

After these edits, `wfctl validate` will skip schema rejection for legacy DO module types — but it does not call `BuildFromConfig`, so the factory-loop migration error won't fire either. The validate command needs to ALSO emit the migration error directly. Pattern:

```go
// After schema.ValidateConfig succeeds, add a post-pass that explicitly
// rejects legacy DO module types with the actionable message — wfctl
// validate's contract is "config is valid", and a legacy DO module type
// is NOT valid post-cutover even though we let schema pass it through.
//
// For validate.go (return error directly):
for _, m := range cfg.Modules {
    if legacydo.IsModuleType(m.Type) {
        // wfctl validate has no engine, so the plugin-loaded flag is always
        // false (validate doesn't know what plugins will be loaded at runtime).
        return legacydo.FormatModuleError(m.Type, m.Name, false)
    }
}

// cfg.Pipelines is map[string]any (verified at config/config.go:149) — NOT a
// typed slice. Mirror the engine's existing pattern (engine.go configurePipelines):
// marshal each entry to YAML then unmarshal into config.PipelineConfig before
// accessing .Steps. The naive `p.Steps` access does not compile.
for _, rawPipeline := range cfg.Pipelines {
    yamlBytes, err := yaml.Marshal(rawPipeline)
    if err != nil {
        continue
    }
    var pipeCfg config.PipelineConfig
    if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
        continue
    }
    for _, s := range pipeCfg.Steps {
        if legacydo.IsStepType(s.Type) {
            return legacydo.FormatStepError(s.Type, false)
        }
    }
}
```

For `ciValidateFile` (which returns `[]error`, accumulating), use `errs = append(errs, ...)` instead of `return`:

```go
// In ci_validate.go ciValidateFile() — same post-pass, but accumulate:
for _, m := range cfg.Modules {
    if legacydo.IsModuleType(m.Type) {
        errs = append(errs, legacydo.FormatModuleError(m.Type, m.Name, false))
    }
}
for _, rawPipeline := range cfg.Pipelines {
    yamlBytes, err := yaml.Marshal(rawPipeline)
    if err != nil {
        continue
    }
    var pipeCfg config.PipelineConfig
    if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
        continue
    }
    for _, s := range pipeCfg.Steps {
        if legacydo.IsStepType(s.Type) {
            errs = append(errs, legacydo.FormatStepError(s.Type, false))
        }
    }
}
```

Add `cmd/wfctl/validate.go` and `cmd/wfctl/ci_validate.go` to T2's Files list (already listed above).

**Automated test for the validate-path migration error** (T2):

Add to `cmd/wfctl/legacy_do_types_removed_test.go`:

```go
// TestValidateFile_LegacyDOModule_ReturnsActionableError verifies that
// wfctl validate emits the actionable migration error when the config
// references a removed legacy DO module type (issue #617). Covers AC3
// on the validate path (the engine path is covered by
// TestLegacyDOModuleError_PluginNotLoaded in the workflow package).
func TestValidateFile_LegacyDOModule_ReturnsActionableError(t *testing.T) {
    dir := t.TempDir()
    cfgPath := filepath.Join(dir, "legacy.yaml")
    yaml := []byte("modules:\n  - name: api\n    type: platform.do_app\n    config: {}\n")
    if err := os.WriteFile(cfgPath, yaml, 0o600); err != nil {
        t.Fatal(err)
    }
    err := validateFile(cfgPath)   // direct call into the validate.go entry point
    if err == nil {
        t.Fatal("expected error for legacy DO module type")
    }
    msg := err.Error()
    for _, want := range []string{
        "removed from workflow core",
        "workflow-plugin-digitalocean",
        "infra.container_service",
    } {
        if !strings.Contains(msg, want) {
            t.Errorf("error missing %q; got: %s", want, msg)
        }
    }
}

// Step variant covering ciValidateFile's accumulating return.
func TestCIValidateFile_LegacyDOStep_ReturnsActionableError(t *testing.T) {
    dir := t.TempDir()
    cfgPath := filepath.Join(dir, "legacy.yaml")
    yaml := []byte("pipelines:\n  deploy:\n    steps:\n      - type: step.do_deploy\n")
    if err := os.WriteFile(cfgPath, yaml, 0o600); err != nil {
        t.Fatal(err)
    }
    errs := ciValidateFile(cfgPath)
    if len(errs) == 0 {
        t.Fatal("expected error for legacy DO step type")
    }
    found := false
    for _, e := range errs {
        if strings.Contains(e.Error(), "step.iac_apply") && strings.Contains(e.Error(), "removed from workflow core") {
            found = true
            break
        }
    }
    if !found {
        t.Errorf("expected actionable migration error in errs; got: %v", errs)
    }
}
```

(Confirm `validateFile` and `ciValidateFile` function signatures match — adapt argument list if the actual signatures take `*FileSystem` / context / different shape; the test bodies should compile against whatever the real signatures are.)

For the step path, **avoid the package-level global** that cycle 4 reviewer flagged as a logic-race risk: instead, attach the `iacProviderLoaded` boolean to the `StepRegistry` as a field set by the engine before pipeline construction. Modify `module/pipeline_step_registry.go`:

```go
// Add to StepRegistry struct (around line 13):
type StepRegistry struct {
    factories         map[string]StepFactory
    iacProviderLoaded bool   // set by SetIaCProviderLoaded; consumed by Create
}

// New method on StepRegistry:
// SetIaCProviderLoaded is called by the engine after module factory registration
// is complete and before pipeline construction. Per-registry state — no global —
// so parallel test runs that build independent StepRegistry instances do not
// share or race the flag.
func (r *StepRegistry) SetIaCProviderLoaded(loaded bool) {
    r.iacProviderLoaded = loaded
}

// Modify (r *StepRegistry).Create at line 32:
func (r *StepRegistry) Create(stepType, name string, config map[string]any, app any) (PipelineStep, error) {
    factory, ok := r.factories[stepType]
    if !ok {
        if legacydo.IsStepType(stepType) {
            return nil, legacydo.FormatStepError(stepType, r.iacProviderLoaded)
        }
        return nil, fmt.Errorf("unknown step type: %s", stepType)
    }
    return factory(name, config, app)
}
```

Wire it in `engine.go` `BuildFromConfig` just before step construction. The engine field is `stepRegistry interfaces.StepRegistrar` at `engine.go:73`; `SetIaCProviderLoaded` is a method on `*module.StepRegistry`, NOT on the `StepRegistrar` interface. Use the same type-assertion pattern already used elsewhere in `engine.go:163,216`:

```go
_, iacLoaded := e.moduleFactories["iac.provider"]
if r, ok := e.stepRegistry.(*module.StepRegistry); ok {
    r.SetIaCProviderLoaded(iacLoaded)
}
```

(Do NOT extend the `StepRegistrar` interface — the method is private wiring between engine and the concrete registry; widening the interface adds a method burden to every alternate `StepRegistrar` implementor downstream for no benefit. The type-assertion pattern matches the precedent.)

**No package-level global, no atomic.Bool.**

(Add `"github.com/GoCodeAlone/workflow/internal/legacydo"` to pipeline_step_registry.go imports.)

(The Create-method patch above replaces the previous `return nil, fmt.Errorf("unknown step type: %s", stepType)` at line 35.)

**Step 4: Run tests to verify they pass**

Run: `go test ./... -run 'TestLegacyDO(Module|Step)Error' -v`
Expected: PASS (all 12 sub-cases — 5 modules × 1 not-loaded + 1 module loaded; 5 steps × 1 not-loaded).

Run: `go test ./...`
Expected: PASS overall (the existing tests untouched by T1/T2/T3 should still pass).

**Step 5: Commit**

```bash
git add internal/legacydo/ \
        engine.go module/pipeline_step_registry.go \
        engine_legacy_do_migration_test.go module/pipeline_step_legacy_do_migration_test.go
git commit -m "$(cat <<'EOF'
feat(#617): actionable migration errors for legacy DO types

Adds module.LegacyDOModuleTypes + LegacyDOStepTypes lookup tables and two
formatters (FormatLegacyDOModuleError, FormatLegacyDOStepError). Both branch
on whether iac.provider is registered in the engine's factory map:
  - not loaded → "Install workflow-plugin-digitalocean: <URL>"
  - loaded     → "already loaded; your config still references the legacy name"

Wired into engine.go:508 (module path) and pipeline_step_registry.go:35
(step path). SetIaCProviderLoaded bridges the boolean from engine to module
package.

Each step type gets a per-step message; step.do_logs and step.do_scale have
GAP messages with workarounds because no 1:1 pipeline-step successor exists
yet (tracked as follow-up issues in T5).
EOF
)"
```

**Rollback:** `git revert <T3-commit-sha>` restores generic unknown-type errors. Combined with T1/T2 revert, repository returns to pre-cutover state.

---

### Task 4: `go mod tidy` (root + example) + CI grep gate

**Files:**
- Modify: `go.mod` (drop `github.com/digitalocean/godo` direct require + transitive bumps via `go mod tidy`)
- Modify: `go.sum` (regenerated)
- Modify: `example/go.mod` (drop indirect godo)
- Modify: `example/go.sum` (regenerated)
- Modify: `.github/workflows/ci.yml` — add a `godo-banned` job that runs the `!`-prefixed greps.
- Test: this task's verification IS the CI gate itself; no new unit test.

**Step 1: Run the tidies and verify godo is gone**

```bash
go mod tidy
(cd example && go mod tidy)
```

**Step 2: Verify**

Run:
```bash
! grep -rn --include="*.go" \
    --exclude-dir=_worktrees \
    --exclude-dir=.worktrees \
    --exclude-dir=.claude \
    "digitalocean/godo" .
! grep -qH "digitalocean/godo" go.mod example/go.mod
```
Expected: BOTH commands exit 0 (no match → grep exits 1 → `!` inverts to 0 → success).

If either fails (i.e., grep finds godo), inspect: a transitive dependency may still pull it. Identify with `go mod why github.com/digitalocean/godo` and investigate.

**Step 3: Add the CI gate**

Modify `.github/workflows/ci.yml` to add a job (placed near `golangci-lint`):

```yaml
  godo-banned:
    name: Verify godo is not imported (issue #617)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Grep gate — *.go files must not import godo
        run: |
          ! grep -rn --include="*.go" \
              --exclude-dir=_worktrees \
              --exclude-dir=.worktrees \
              --exclude-dir=.claude \
              "digitalocean/godo" .
      - name: Grep gate — go.mod files must not list godo
        run: |
          ! grep -qH "digitalocean/godo" go.mod example/go.mod
```

(If `.github/workflows/ci.yml` does not exist or has a different name, locate the existing Go-build workflow file via `ls .github/workflows/` and add the job there. Adapt the runner/checkout action versions to match the rest of the file.)

**Step 4: Verify locally one more time, including the gate's exact commands**

```bash
bash -c '! grep -rn --include="*.go" --exclude-dir=_worktrees --exclude-dir=.worktrees --exclude-dir=.claude "digitalocean/godo" .'
echo "exit: $?"
# Expected: exit: 0
bash -c '! grep -qH "digitalocean/godo" go.mod example/go.mod'
echo "exit: $?"
# Expected: exit: 0
```

**Step 5: Commit**

```bash
git add go.mod go.sum example/go.mod example/go.sum .github/workflows/
git commit -m "$(cat <<'EOF'
feat(#617): drop godo from go.mod + add CI grep gate

* go mod tidy on root and example/ drops github.com/digitalocean/godo
  (direct from root, indirect from example/).
* New CI job 'godo-banned' fails the build on any *.go import of godo OR
  any mention of godo in go.mod files. Excludes _worktrees, .worktrees,
  and .claude (local agent state, not committed source).

This satisfies acceptance criterion #4 (dependabot bumps target the
provider repo, not workflow core).
EOF
)"
```

**Rollback:** `git revert <T4-commit-sha>` restores godo to go.mod and removes the CI gate. Combined with T1/T2/T3 revert returns to pre-cutover state.

---

### Task 5: Docs, CHANGELOG, migration guide, `wfctl modernize` rules + file follow-up issues

**Files:**
- Modify: `DOCUMENTATION.md` (replace the 5 module rows + 5 step rows in the platform tables with a single paragraph pointing at the DO plugin)
- Modify: `CHANGELOG.md` (prepend a `## v0.52.0` section with the breaking-change entry)
- Create: `docs/migrations/v0.52.0-godo-removal.md` (full migration guide — 5 module mappings + 5 step mappings + GAP callouts + before/after YAML examples + step-by-step migration recipe + ADR-style "why this was done")
- Create: `modernize/legacy_do_rule.go` (new modernize rules — see Step 3)
- Modify: `modernize/modernize.go` `AllRules()` to append the new rule
- Test: `modernize/legacy_do_rule_test.go` (new — covers Check + Fix for each of the 5 module + 5 step rewrites + 3 gap types)
- Create: `modernize/testdata/legacy-do-config.yaml` (committed smoke-test fixture exercising every legacy type)
- Create: `modernize/testdata/legacy-do-config.expected.yaml` (the post-`modernize --apply` output, used as a golden file for the smoke test in step 9 below)
- Modify: `cmd/wfctl/infra_apply.go:130-131` + `cmd/wfctl/infra.go:460` — comment hygiene: drop the "legacy DigitalOcean" phrasing in `hasPlatformModules` / `isInfraType` rationale comments. Both functions remain correct for the surviving `platform.*` types (e.g., `platform.kubernetes`, `platform.ecs`); only the DO-specific framing is stale.

**Step 1: Write the failing test**

Create `modernize/legacy_do_rule_test.go`:

```go
package modernize

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLegacyDORule_Rewrites(t *testing.T) {
	cases := []struct {
		name     string
		yamlIn   string
		wantNew  string   // must appear in fixed YAML
		wantDrop string   // must NOT appear in fixed YAML (the legacy type)
	}{
		{
			name:     "platform.do_app → infra.container_service (provider NOT auto-injected)",
			yamlIn:   "modules:\n  - name: api\n    type: platform.do_app\n    config:\n      region: nyc\n",
			wantNew:  "infra.container_service",
			wantDrop: "platform.do_app",
		},
		{
			name:     "platform.do_database → infra.database",
			yamlIn:   "modules:\n  - name: db\n    type: platform.do_database\n    config: {}\n",
			wantNew:  "infra.database",
			wantDrop: "platform.do_database",
		},
		{
			name:     "platform.do_dns → infra.dns",
			yamlIn:   "modules:\n  - name: dns\n    type: platform.do_dns\n    config: {}\n",
			wantNew:  "infra.dns",
			wantDrop: "platform.do_dns",
		},
		{
			name:     "platform.doks → infra.k8s_cluster",
			yamlIn:   "modules:\n  - name: k8s\n    type: platform.doks\n    config: {}\n",
			wantNew:  "infra.k8s_cluster",
			wantDrop: "platform.doks",
		},
		{
			name:     "step.do_deploy → step.iac_apply",
			yamlIn:   "pipelines:\n  - steps:\n      - type: step.do_deploy\n",
			wantNew:  "step.iac_apply",
			wantDrop: "step.do_deploy",
		},
	}
	rule := legacyDORule()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &root); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			findings := rule.Check(&root, []byte(tc.yamlIn))
			if len(findings) == 0 {
				t.Fatalf("expected a finding, got 0")
			}
			rule.Fix(&root)
			out, err := yaml.Marshal(&root)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(out)
			if !strings.Contains(s, tc.wantNew) {
				t.Errorf("fixed YAML missing %q; got:\n%s", tc.wantNew, s)
			}
			if strings.Contains(s, tc.wantDrop) {
				t.Errorf("fixed YAML still contains legacy %q; got:\n%s", tc.wantDrop, s)
			}
		})
	}
}

func TestLegacyDORule_GapTypesFlaggedNotRewritten(t *testing.T) {
	// step.do_logs, step.do_scale, and platform.do_networking have NO 1:1
	// auto-fixable successor. Rule must:
	//  - flag them as findings,
	//  - NOT modify the YAML (no silent loss).
	cases := []struct {
		name    string
		legacy  string
		yamlIn  string
	}{
		{"step.do_logs", "step.do_logs", "pipelines:\n  - steps:\n      - type: step.do_logs\n"},
		{"step.do_scale", "step.do_scale", "pipelines:\n  - steps:\n      - type: step.do_scale\n"},
		{"platform.do_networking", "platform.do_networking", "modules:\n  - name: net\n    type: platform.do_networking\n    config: {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &root); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			rule := legacyDORule()
			findings := rule.Check(&root, []byte(tc.yamlIn))
			if len(findings) == 0 {
				t.Fatalf("expected a finding for %q", tc.legacy)
			}
			if findings[0].Fixable {
				t.Errorf("%q must be marked Fixable: false (no auto-rewrite); got Fixable: true", tc.legacy)
			}
			rule.Fix(&root)
			out, _ := yaml.Marshal(&root)
			if !strings.Contains(string(out), tc.legacy) {
				t.Errorf("Fix MUST NOT remove legacy %q; got:\n%s", tc.legacy, out)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./modernize/... -run TestLegacyDORule -v`
Expected: FAIL with "undefined: legacyDORule".

**Step 3: Implement the rule**

Create `modernize/legacy_do_rule.go`:

```go
package modernize

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/internal/legacydo"
	"gopkg.in/yaml.v3"
)

// Import note: `modernize` MUST NOT import `module` directly. `module`
// transitively imports `modernize` via `plugin` (plugin/manifest.go +
// plugin/engine_plugin.go), so `modernize → module` creates an import cycle.
// Shared constants live in `internal/legacydo`, a leaf package that imports
// only stdlib and is safe for both `module` and `modernize` to consume.

// LegacyDORule rewrites legacy DigitalOcean module + step types to their
// infra.* IaC successors (issue #617).
//
// IMPORTANT: The Fix function ONLY renames the `type:` key — it does NOT
// inject the required `config.provider: digitalocean` setting, because that
// requires modifying a sibling mapping that may already contain unrelated
// keys the operator must review. The rule's Check Message and the migration
// guide both instruct the operator to add the provider key manually after
// running modernize. The committed `testdata/legacy-do-config.expected.yaml`
// fixture asserts the post-modernize shape: types renamed, provider NOT
// auto-added. Adding provider injection in a future iteration is tracked as
// a follow-up (see migration guide).
//
// Auto-fixable for 4 of 5 modules (platform.do_app/database/dns/doks) and
// 3 of 5 steps (step.do_deploy/status/destroy). The GAP types (do_networking
// splits 1→2; step.do_logs/scale have no pipeline-step successor) are flagged
// but not modified.
func legacyDORule() Rule {
	moduleMap := map[string]string{
		"platform.do_app":        "infra.container_service",
		"platform.do_database":   "infra.database",
		"platform.do_dns":        "infra.dns",
		"platform.doks":          "infra.k8s_cluster",
		// platform.do_networking is intentionally NOT auto-fixed: it splits
		// 1→2 (infra.vpc + infra.firewall), which requires structural
		// rewrite the operator must review.
	}
	stepMap := map[string]string{
		"step.do_deploy":  "step.iac_apply",
		"step.do_status":  "step.iac_status",
		"step.do_destroy": "step.iac_destroy",
	}
	gapTypes := map[string]string{
		"platform.do_networking": "splits into infra.vpc + infra.firewall — manual rewrite required",
		"step.do_logs":           "no pipeline-step successor; use `wfctl infra logs` or rely on DO plugin Troubleshoot",
		"step.do_scale":          "no pipeline-step successor; edit instance_count and re-run step.iac_apply",
	}

	return Rule{
		ID:          "legacy-do-types",
		Description: "Rewrite legacy DigitalOcean module/step types to infra.* IaC successors (issue #617).",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var out []Finding
			walkTypeNodes(root, func(typeVal *yaml.Node) {
				if successor, ok := moduleMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s; rewrite to %s (provider: digitalocean) — requires workflow-plugin-digitalocean", typeVal.Value, legacydo.RemovedInVersion, successor),
						Fixable: true,
					})
				}
				if successor, ok := stepMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s; rewrite to %s — requires workflow-plugin-digitalocean", typeVal.Value, legacydo.RemovedInVersion, successor),
						Fixable: true,
					})
				}
				if reason, ok := gapTypes[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s — %s", typeVal.Value, legacydo.RemovedInVersion, reason),
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
						RuleID:      "legacy-do-types",
						Line:        typeVal.Line,
						Description: fmt.Sprintf("rewrote %s → %s", old, successor),
					})
				}
				if successor, ok := stepMap[typeVal.Value]; ok {
					old := typeVal.Value
					typeVal.Value = successor
					out = append(out, Change{
						RuleID:      "legacy-do-types",
						Line:        typeVal.Line,
						Description: fmt.Sprintf("rewrote %s → %s", old, successor),
					})
				}
				// gapTypes are intentionally not modified.
			})
			return out
		},
	}
}

// walkTypeNodes traverses a YAML AST and invokes visit on every value node
// whose parent mapping key is "type". This differs from the package's existing
// walkNodes helper which visits every node — extracted as a separate helper
// because the type-key constraint produces tighter visitor code at call sites.
// If a future refactor unifies the two, prefer adding a key-filter parameter
// to walkNodes over keeping the duplication.
func walkTypeNodes(n *yaml.Node, visit func(*yaml.Node)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "type" && v.Kind == yaml.ScalarNode {
				visit(v)
			}
			walkTypeNodes(v, visit)
		}
		return
	}
	for _, c := range n.Content {
		walkTypeNodes(c, visit)
	}
}
```

Append to `modernize/modernize.go` `AllRules()`:

```go
return []Rule{
    hyphenStepsRule(),
    conditionalFieldRule(),
    dbQueryModeRule(),
    dbQueryIndexRule(),
    absoluteDbPathRule(),
    emptyRoutesRule(),
    camelCaseConfigRule(),
    requestParseConfigRule(),
    legacyDORule(),     // <-- ADD
}
```

**Step 4: Run rule tests to verify they pass**

Run: `go test ./modernize/... -run TestLegacyDORule -v`
Expected: PASS.

Run: `go test ./modernize/...`
Expected: PASS overall.

**Step 5: Write the docs + migration guide + CHANGELOG**

Modify `DOCUMENTATION.md`: locate the "Platform Modules" table containing the 5 `platform.do_*` rows and the "Platform Steps" table containing the 5 `step.do_*` rows. Replace each row block with:

```markdown
**DigitalOcean IaC modules and steps** were removed from workflow core in
v0.52.0 and moved to the
[workflow-plugin-digitalocean](https://github.com/GoCodeAlone/workflow-plugin-digitalocean)
external plugin. After loading the plugin, use the generic `infra.*` module
types with `provider: digitalocean` and the generic `step.iac_*` pipeline
steps. See [v0.52.0 migration guide](docs/migrations/v0.52.0-godo-removal.md).
```

Prepend to `CHANGELOG.md`:

```markdown
## v0.52.0 (2026-05-13) — BREAKING

### Removed (issue #617)

- All legacy DigitalOcean IaC modules (`platform.do_app`, `platform.do_database`, `platform.do_dns`, `platform.do_networking`, `platform.doks`) and the DO credential resolver `cloud_account_do.go`.
- All legacy DigitalOcean pipeline steps (`step.do_deploy`, `step.do_status`, `step.do_logs`, `step.do_scale`, `step.do_destroy`).
- The `github.com/digitalocean/godo` dependency from `go.mod` (root and `example/`).

### Migration

DigitalOcean IaC moved to [`workflow-plugin-digitalocean`](https://github.com/GoCodeAlone/workflow-plugin-digitalocean) v0.12.0+. After loading the plugin, replace legacy module types with `infra.*` types and `provider: digitalocean`. Run `wfctl modernize --apply <config.yaml>` to auto-rewrite supported types — **then manually add `provider: digitalocean` to each rewritten module's `config:` block** (the modernize rule does not inject the provider key; see the [migration guide](docs/migrations/v0.52.0-godo-removal.md) for the exact recipe). Two step types (`step.do_logs`, `step.do_scale`) have no 1:1 pipeline successor — workarounds documented in the migration guide.

Configs that still reference the legacy types now fail to load with an actionable error pointing to the plugin and the relevant `infra.*` successor.
```

Create `docs/migrations/v0.52.0-godo-removal.md`:

```markdown
# v0.52.0 — Removing godo from workflow core (issue #617)

## What changed

The five legacy `platform.do_*` modules, the `cloud.account` DO credential
resolver, and the five legacy `step.do_*` pipeline steps were removed from
workflow core. The `github.com/digitalocean/godo` dependency is no longer
pulled by the workflow module.

DigitalOcean IaC functionality moved entirely to
[`workflow-plugin-digitalocean`](https://github.com/GoCodeAlone/workflow-plugin-digitalocean)
v0.12.0+, which exposes the same resources through the generic `infra.*` IaC
type system with `provider: digitalocean`.

## Why

Workflow core should own IaC interfaces and orchestration, not provider SDKs.
Dependabot bumps to godo now target the DO plugin repo, not core. See ADR or
the design doc at `docs/plans/2026-05-13-issue-617-godo-removal-design.md`.

## Migration recipe

1. Install the DO plugin (v0.12.0+):
   ```yaml
   plugins:
     - name: digitalocean
       source: github.com/GoCodeAlone/workflow-plugin-digitalocean
       version: ">=0.12.0"
   ```

2. Run the modernizer over each affected YAML config:
   ```sh
   wfctl modernize --apply ./config/*.yaml
   ```
   This **renames the type field** for 4 module types and 3 step types
   automatically. Two step types (`step.do_logs`, `step.do_scale`) and one
   module type (`platform.do_networking`) are flagged but not auto-rewritten
   — see below.

3. **Add `provider: digitalocean` to each rewritten module's `config:`
   block.** The modernize rule does NOT auto-inject this key, because the
   `config:` block typically contains operator-authored settings that
   shouldn't be silently modified. Example:

   ```yaml
   # After modernize (type renamed, provider absent):
   modules:
     - name: api
       type: infra.container_service
       config:
         region: nyc        # <-- modernize left this alone

   # Operator adds provider key manually:
   modules:
     - name: api
       type: infra.container_service
       config:
         provider: digitalocean     # <-- ADD THIS
         region: nyc
   ```

   Forgetting this produces a load-time error:
   `infra module "api" (infra.container_service): 'provider' config is required`.

4. Manually address the GAP types listed below.

5. Re-run `wfctl validate` and `wfctl infra plan` to confirm the rewritten
   config loads and produces the same plan.

## Module type mapping

| Legacy type              | Successor                          | Auto-fix |
|--------------------------|-----------------------------------|----------|
| `platform.do_app`        | `infra.container_service`         | Yes      |
| `platform.do_database`   | `infra.database`                  | Yes      |
| `platform.do_dns`        | `infra.dns`                       | Yes      |
| `platform.do_networking` | `infra.vpc` + `infra.firewall`    | **No** — splits 1→2, manual review required |
| `platform.doks`          | `infra.k8s_cluster`               | Yes      |

All successors require `config.provider: digitalocean`.

## Step type mapping

| Legacy type        | Successor                                                          | Auto-fix |
|--------------------|--------------------------------------------------------------------|----------|
| `step.do_deploy`   | `step.iac_apply` (against an `infra.container_service` module)     | Yes      |
| `step.do_status`   | `step.iac_status` (against an `infra.container_service` module)    | Yes      |
| `step.do_destroy`  | `step.iac_destroy` (against an `infra.container_service` module)   | Yes      |
| `step.do_logs`     | **GAP** — no pipeline step successor; use `wfctl infra logs` ad-hoc, or rely on the DO plugin's Troubleshoot hook on `step.iac_apply` failure. Tracked: workflow-plugin-digitalocean issue <ISSUE-N> | **No** |
| `step.do_scale`    | **GAP** — no pipeline step successor; update `instance_count` in the `infra.container_service` module config and re-run `step.iac_apply`. Tracked: workflow-plugin-digitalocean issue <ISSUE-M> | **No** |

## Before / after examples

### App Platform

Before:
```yaml
modules:
  - name: api
    type: platform.do_app
    config:
      region: nyc
      services:
        - name: web
          image: registry.digitalocean.com/myorg/api:latest
```

After:
```yaml
modules:
  - name: api
    type: infra.container_service
    config:
      provider: digitalocean
      region: nyc
      services:
        - name: web
          image: registry.digitalocean.com/myorg/api:latest
```

### Pipeline step

Before:
```yaml
pipelines:
  - id: deploy
    steps:
      - type: step.do_deploy
        config: { app: api }
```

After:
```yaml
pipelines:
  - id: deploy
    steps:
      - type: step.iac_apply
        config: { module: api }
```

## Errors you may see

* `unsupported legacy module type "platform.do_app" (module "api"): this type was removed from workflow core in v0.52.0 — DigitalOcean IaC moved to workflow-plugin-digitalocean.` — fix the config per the table above; install the plugin if not already loaded.
* `unsupported legacy step type "step.do_logs": ...` — see GAP entry above; remove the step and use `wfctl infra logs` ad-hoc, or wait for `step.iac_logs` (tracked).

## Rollback

If your environment cannot upgrade in this cycle, pin to the previous workflow
core tag (`go get github.com/GoCodeAlone/workflow@v0.51.3`). The legacy modules
remain available there.
```

**Step 6: File two follow-up issues in `workflow-plugin-digitalocean` and wire their numbers into the migration error**

Using `gh`:

```bash
LOGS_ISSUE_BODY=$(cat <<'EOF'
Legacy step.do_logs in workflow core was removed in workflow v0.52.0 (issue
GoCodeAlone/workflow#617). There is no 1:1 pipeline-step successor in the
generic step.iac_* family yet. Current workaround for users: `wfctl infra logs`
ad-hoc, or rely on the DO plugin's Troubleshoot hook on step.iac_apply
failure. This issue tracks adding a first-class step.iac_logs (in core) or
step.app_logs (in the DO plugin's exposed step set).
EOF
)
SCALE_ISSUE_BODY=$(cat <<'EOF'
Legacy step.do_scale in workflow core was removed in workflow v0.52.0 (issue
GoCodeAlone/workflow#617). Current workaround: update instance_count in the
infra.container_service module config and re-run step.iac_apply. This issue
tracks adding a first-class step.iac_scale (config-less runtime scale).
EOF
)
gh issue create --repo GoCodeAlone/workflow-plugin-digitalocean \
  --title "Add step.iac_logs (or step.app_logs) — closes step.do_logs migration GAP from workflow#617" \
  --body "$LOGS_ISSUE_BODY"
gh issue create --repo GoCodeAlone/workflow-plugin-digitalocean \
  --title "Add step.iac_scale — closes step.do_scale migration GAP from workflow#617" \
  --body "$SCALE_ISSUE_BODY"
```

Capture the two issue URLs / numbers and patch the migration guide's two `<ISSUE-N> / <ISSUE-M>` placeholders. (The error text in `internal/legacydo/types.go` does not contain URL placeholders — only the migration guide does — so this step is doc-only.)

**Step 7: Verify the docs build / render**

Run: `find docs -name "*.md" -exec grep -l "TODO\|<ISSUE-" {} \;` — expected: no output (all placeholders resolved).

Run: `grep -n "platform.do_app\|step.do_deploy" DOCUMENTATION.md` — expected: no output (rows replaced).

**Step 8: Commit**

```bash
git add modernize/legacy_do_rule.go modernize/legacy_do_rule_test.go modernize/modernize.go \
        DOCUMENTATION.md CHANGELOG.md docs/migrations/v0.52.0-godo-removal.md
git commit -m "$(cat <<'EOF'
feat(#617): wfctl modernize rule + migration guide + CHANGELOG

* New modernize rule "legacy-do-types": auto-rewrites 5 module types and 3
  of 5 step types to infra.*; flags but does not modify the two GAP step
  types and the 1→2 platform.do_networking split.
* CHANGELOG: v0.52.0 BREAKING entry.
* docs/migrations/v0.52.0-godo-removal.md: full migration guide with
  mapping tables, before/after YAML, error reference, rollback note.
* DOCUMENTATION.md: replace 10 legacy rows with a pointer to the plugin
  and the migration guide.

Plus filed two follow-up issues in workflow-plugin-digitalocean for the
step.do_logs and step.do_scale GAPs.
EOF
)"
```

**Step 9: File the AWS audit follow-up issue in `GoCodeAlone/workflow`**

**Before writing the issue body, regenerate the in-scope file list from the current tree** rather than copying speculative names from the design:

```sh
# Discover actual aws-sdk-go-v2 importers in module/:
grep -rln "github.com/aws/aws-sdk-go-v2" --include="*.go" module/ | sort
# Also list drivers:
ls platform/providers/aws/drivers/*.go 2>/dev/null
# RBAC + non-IaC stays:
ls iam/aws*.go plugin/rbac/aws*.go artifact/s3*.go module/iac_state_spaces.go provider/aws/deploy.go 2>/dev/null
```

Then write the issue body. Template (replace the `<...>` placeholders with the actual grep output):

```bash
AWS_BODY=$(cat <<'EOF'
Continuation of #617. The DO half of the SDK audit shipped in v0.52.0 (godo
gone from core). This issue tracks the AWS half.

In scope (move to workflow-plugin-aws via the same Option A force-cutover
pattern used for #617):
<list of actual aws-sdk-go-v2 importers under module/ and IaC drivers under
platform/providers/aws/ from the grep above; one file per line>

Out of scope (justified non-IaC core surfaces; STAY in core):
- `iam/aws.go` — RBAC integration
- `plugin/rbac/aws.go` — RBAC plugin glue
- `artifact/s3.go` — generic S3-compat artifact storage
- `provider/aws/deploy.go` — IaC adapter (revisit if thin wrapper)
- `module/iac_state_spaces.go` — S3-compat state backend (also used by DO Spaces)

Goal: same as #617 — Dependabot bumps for AWS SDKs target the provider
plugin repo, not core, except for the surfaces above.
EOF
)
gh issue create --repo GoCodeAlone/workflow \
  --title "Audit AWS SDK usage in workflow core (RBAC/secrets/artifact stay; IaC drivers reviewed for plugin move)" \
  --body "$AWS_BODY"
```

**Step 10: Final commit (issue URLs back into the migration guide)**

After the two plugin issues are filed in Step 6, patch their numbers/URLs into `docs/migrations/v0.52.0-godo-removal.md`. The AWS follow-up issue from Step 9 does not need to be referenced in this PR — it lives independently. Commit the patched URLs:

```bash
git add docs/migrations/v0.52.0-godo-removal.md
git commit -m "docs(#617): wire workflow-plugin-digitalocean follow-up issue URLs into migration guide"
```

**Rollback:** `git revert <T5-commits>` removes the modernize rule, migration guide, CHANGELOG entry. Combined with T1/T2/T3/T4 revert returns to pre-cutover state. Plugin follow-up issues remain filed (they describe genuine gaps regardless of whether this PR ships).

---

## Verification per change class (summary)

| Task | Class | Verification |
|------|-------|--------------|
| T1 | Internal-logic refactor (pure deletion + import test) | `go test ./module -run TestGodoNotImported_InModulePackage` PASS |
| T2 | Internal-logic refactor (registry edits) | `go test ./cmd/wfctl -run TestLegacyDOTypesAbsent_FromTypeRegistry` PASS + `go build ./...` clean |
| T3 | Internal-logic refactor (new error path + helper) | `go test -run 'TestLegacyDO(Module|Step)Error'` PASS |
| T4 | **Version pin update** | `go mod tidy` clean + CI `godo-banned` job PASS + `! grep ...` locally exits 0. **Rollback:** revert T4 commit; godo returns to go.mod (no runtime effect because no code uses it after T1-T3 either). |
| T5 | Documentation + new CLI rule | `go test ./modernize -run TestLegacyDORule` PASS + `grep -n "platform.do_app" DOCUMENTATION.md` returns nothing + two plugin issues filed + AWS audit issue filed |

T4 is the only task with the runtime-launch-validation trigger (version-pin update), and the rollback note is included.

---

## End-of-PR checklist (run before opening PR)

1. `go test ./...` — all green.
1a. `go test -race ./...` — all green (the `module` package has parallel tests; while T3's per-registry instance field eliminates the global, `-race` is still mandatory to catch any future regression and to verify the engine→stepRegistry hook is goroutine-safe).
2. `! grep -rn --include="*.go" --exclude-dir=_worktrees --exclude-dir=.worktrees --exclude-dir=.claude "digitalocean/godo" .` exits 0.
3. `! grep -qH "digitalocean/godo" go.mod example/go.mod` exits 0.
4. `wfctl modernize --apply modernize/testdata/legacy-do-config.yaml` (fixture committed in T5) rewrites legacy types — verify against `modernize/testdata/legacy-do-config.expected.yaml`.
5. `go build ./cmd/wfctl && ./wfctl validate modernize/testdata/legacy-do-config.yaml` produces the actionable migration error and exits non-zero.
6. `go build ./cmd/server && ./server -config modernize/testdata/legacy-do-config.yaml` produces the same error and exits non-zero.
7. CHANGELOG.md has the v0.52.0 BREAKING entry at the top.
8. Two follow-up issues filed in `workflow-plugin-digitalocean`; URLs wired into the migration guide.
9. One follow-up issue filed in `workflow` for the AWS audit (no URL wiring needed — independent stream).
10. PR description references issue #617 and lists the breaking-change impact.

---

## Adversarial review history (plan phase)

### Cycle 7 (FAIL) — 2026-05-13

- **C-1** validate/ci_validate post-pass step sweep used naive `for _, p := range cfg.Pipelines { for _, s := range p.Steps {` but `cfg.Pipelines` is `map[string]any` (verified at `config/config.go:149`), not `[]PipelineConfig` — won't compile → **fixed**: T2 now uses yaml.Marshal/Unmarshal pattern matching `engine.go configurePipelines`. Also split out the `ciValidateFile` accumulating variant (`errs = append`) from the `validateFile` early-return variant.
- **I-1** No automated test for the validate-path migration error (only checklist item 5 covered it manually) → **fixed**: added `TestValidateFile_LegacyDOModule_ReturnsActionableError` and `TestCIValidateFile_LegacyDOStep_ReturnsActionableError` to T2.
- **Cycle 1-6 plan-phase fixes verified to hold.**

### Cycle 6 (FAIL) — 2026-05-13

- **C-1** Plan referenced a phantom `schema.WithExtraStepTypes` (no such function exists; `schema.ValidateConfig` only validates module types, not step types) → **fixed**: step-types schema injection removed; step migration guard at the `StepRegistry.Create` rejection point is the sole gate for legacy step types, which is correct because schema never validated them.
- **C-1 second part** `wfctl validate` (`cmd/wfctl/validate.go:145`) and `wfctl ci validate` (`cmd/wfctl/ci_validate.go:134`) call `schema.ValidateConfig` directly without going through `engine.BuildFromConfig`, so the migration error path is unreachable from validate → **fixed**: added both files to T2; pattern is (a) inject `legacydo.ModuleTypes` into local opts so schema passes legacy types through, (b) post-`ValidateConfig` sweep emits `legacydo.FormatModuleError` / `FormatStepError` for any legacy type found in modules / pipeline steps. AC3 now satisfied on the validate path.
- **I-1** `if len(e.moduleFactories) > 0 || true { ... }` triggers `staticcheck SA4010` always-true-condition → CI lint fails → **fixed**: replaced with unconditional code.
- **m-1** Cycle-5 history checklist line mentioned `WithExtraStepTypes` (which doesn't exist) → **fixed implicitly** by deleting the step-types schema injection from T3.
- **Cycle 1-5 plan-phase fixes verified to hold.**

### Cycle 5 (FAIL) — 2026-05-13

- **C-1** `schema.ValidateConfig` at `engine.go:400` fires BEFORE the factory loop at `:506` — removing the 5 legacy module types from `schema/schema.go`'s allow-list (T2) would cause the generic schema error to be returned ahead of the actionable `legacydo.FormatModuleError`, making the migration message unreachable → **fixed**: T3 wiring now adds the 5 legacy DO module types (and 5 step types) to `schema.WithExtraModuleTypes` / `WithExtraStepTypes` so schema passes them through to the factory guard, which is the real rejection point.
- **I-1** Plan wrote `e.stepRegistry.SetIaCProviderLoaded(iacLoaded)` but `e.stepRegistry` is `interfaces.StepRegistrar` (no such method on the interface) → would not compile → **fixed**: type-assertion pattern from `engine.go:163,216`: `if r, ok := e.stepRegistry.(*module.StepRegistry); ok { r.SetIaCProviderLoaded(iacLoaded) }`. Interface deliberately NOT widened — that would add a method burden to every downstream `StepRegistrar` for zero benefit.
- **I-2** End-of-PR checklist 1a still cited "T3 introduces a package-level atomic" — stale from cycle-3's pre-instance-field design → **fixed**.
- **m-1** `LegacyDORule()` was exported but all peer rule constructors (`hyphenStepsRule`, `dbQueryModeRule`, …) are unexported, and existing tests use `package modernize` (not `_test`) → **fixed**: renamed to `legacyDORule`; test file now uses internal `package modernize`; external `modernize` import dropped from the test.
- **Cycle 1/2/3/4 plan-phase fixes verified to hold.**

### Cycle 4 (FAIL) — 2026-05-13

- **C-1** Cycle 3's "no import cycle" claim was wrong — `module` transitively imports `modernize` via `plugin` (`go list -deps github.com/GoCodeAlone/workflow/module | grep modernize` returns `modernize` because `plugin/manifest.go` and `plugin/engine_plugin.go` import it). Therefore `modernize → module` IS a cycle → **fixed**: shared constants/formatters moved to a new leaf package `internal/legacydo/types.go` that imports only stdlib. Both `module/` (via the engine guard) and `modernize/` (via the rewrite rule) import `internal/legacydo` cycle-free.
- **I-1** Package-level `atomic.Bool iacProviderLoaded` is a logic-race surface (atomic.Bool blocks data-race detector but not test-order non-determinism when multiple tests mutate the flag) → **fixed**: replaced the global with a `StepRegistry.iacProviderLoaded` instance field; `r.SetIaCProviderLoaded(loaded)` sets it, `r.Create` reads it. Per-registry state; parallel tests can each own a fresh `NewStepRegistry()`.
- **I-2** Design doc proposed an "assert credential registry has zero `digitalocean/*` entries" test, but `credentialResolvers` is unexported and there is no accessor → **fixed (option a)**: design doc rewritten to remove the proposed test; rationale "registry is additive via init(); deleting file removes init()" is the evidence; no API-for-test-only added.
- **m-1** `platformModules` local variable in `deploy.go` would carry `infra.*` items after T2 — misleading name → **fixed**: T2 spec now includes the rename to `deployTargetModules`.
- **m-2** `newTestEngine` couples to `package workflow` for `mockLogger` visibility → **acknowledged as informational** in cycle 3; no plan change. Current package structure makes the coupling correct.
- **Cycle 1/2/3 plan-phase fixes verified to hold.**

### Cycle 3 (FAIL) — 2026-05-13

- **C-1** Plan declared `type mockLogger struct{}` in `engine_legacy_do_migration_test.go` (same package as `engine_test.go:482` which already declares it) → compile error → **fixed**: redeclaration removed; helper reuses the existing in-package type.
- **C-2** `legacyDORemovedInVersion` duplicate was justified by a falsely claimed import cycle (`go list -f '{{ join .Imports "\n" }}'` confirmed no cycle) → **fixed**: dropped the duplicate; modernize/legacy_do_rule.go now imports `module` and references `legacydo.RemovedInVersion` directly. Single source of truth.
- **I-1** Step "already loaded" branch had no test → **fixed**: added `TestLegacyDOStepError_PluginLoaded` symmetric to the engine equivalent.
- **m-1** CI snippet pinned `actions/checkout@v5` which doesn't exist (repo uses `@v4` everywhere) → **fixed**.
- **Cycle 1 and Cycle 2 plan-phase fixes verified to hold.**

### Cycle 2 (FAIL) — 2026-05-13

- **C-1** `wfctl modernize` Fix renamed `type:` but did not inject `config.provider: digitalocean` → produced YAML that fails to load → **fixed by scope-limit (Option 2)**: rule explicitly does not inject the provider key; the migration guide adds a manual step with example YAML and the error string the user will hit; rule docstring + test names + expected fixture all assert the scope-limited behaviour.
- **C-2** `cmd/wfctl/deploy.go:839,901` had its own `strings.HasPrefix(m.Type, "platform.")` collector + "no platform.* modules found" error — missed in T2 scope → **fixed**: file added to T2's edit list; both call sites updated to include `infra.*` prefix.
- **I-1** `newTestEngine` differs from `setupEngineTest` (no `loadAllPlugins`) — intentional but not documented → **fixed**: comment added explaining the intentional divergence.
- **I-2** `hasPlatformModules` + `isInfraType` rationale comments still cite DigitalOcean → **fixed**: comment-hygiene edit added to T5.
- **m-1** `newTestEngine` passed `nil` logger to `NewStdApplication`; deviated from existing test pattern → **fixed**: use `mockLogger{}` matching the canonical shape in engine_test.go.
- **m-2** `RemovedInVersion` declared in `module/` but hardcoded again in `modernize/` (import cycle prevents reuse) → **fixed**: explicit duplicate `legacydo.RemovedInVersion` in modernize with a documented "keep in sync" comment.
- **m-3** AWS audit issue body invented speculative file names → **fixed**: T5 Step 9 now runs the grep BEFORE writing the body and uses the grep output to populate the in-scope list.
- **Cycle 1 fixes verified to hold** — no regressions introduced.

### Cycle 1 (FAIL) — 2026-05-13

- **C-1** T3 engine test invented `workflow.NewEngine()` + `e.RegisterModuleFactory()` → **fixed**: use `NewStdEngine(app, app.Logger())` and `AddModuleType()` per `engine.go:146,210`; `package workflow`.
- **C-2** T3 step test invented `module.CreateStep()` → **fixed**: use `module.NewStepRegistry().Create()` per `module/pipeline_step_registry.go:18,32`.
- **I-1** T2 test invented `buildTypeRegistry()` → **fixed**: call `KnownModuleTypes()` + `KnownStepTypes()` directly.
- **I-2** Global `iacProviderLoaded` raced with parallel tests → **fixed**: `sync/atomic.Bool` + `IsIaCProviderLoaded()` accessor.
- **I-3** Missing test for `platform.do_networking` gap behaviour → **fixed**: gap-type test renamed and broadened to all three gap types.
- **m-1** New `walkTypeNodes` duplicates existing `walkNodes` → **acknowledged**: note added recommending unification in a future refactor; kept separate for now to preserve tight call-site code.
- **m-2** Version `v0.52.0` hardcoded in 7+ places → **fixed**: `legacydo.RemovedInVersion` constant.
- **m-3** Smoke fixture not committed → **fixed**: `modernize/testdata/legacy-do-config.yaml` + `.expected.yaml` added to T5.
- End-of-PR checklist: added `go test -race ./...` and pointed checklist items 4-6 at the committed fixture.

## References

- Design doc: `docs/plans/2026-05-13-issue-617-godo-removal-design.md`
- Issue: [GoCodeAlone/workflow#617](https://github.com/GoCodeAlone/workflow/issues/617)
- Trigger PR (Dependabot bump): [PR #421](https://github.com/GoCodeAlone/workflow/pull/421)
- Plugin: [`workflow-plugin-digitalocean`](https://github.com/GoCodeAlone/workflow-plugin-digitalocean) v0.12.0+
- Precedent: `feedback_force_strict_contracts_no_compat.md` (force-cutover pattern); `project_strict_contracts_cutover_complete.md` (typed-gRPC cutover, DO plugin v1.0.1)
