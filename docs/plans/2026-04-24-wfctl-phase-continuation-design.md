---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: cc22551
  - repo: workflow
    commit: f0faf96
  - repo: workflow
    commit: b50e3f3
  - repo: workflow
    commit: 856a7b9
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - rg -n "ResolveForEnv|resourceName = resolved.Name|resolveInfraOutput" cmd/wfctl config
    - GOWORK=off go test ./interfaces ./config ./platform ./cmd/wfctl -run 'Test(Migration|Tenant|Canonical|BuildHook|PluginCLI|ScaffoldDockerfile|ResolveForEnv|ConfigHash|ApplyInfraModules|Diagnostic|Troubleshoot|ProviderID|ValidateProviderID|PluginInstall|ParseChecksums|Audit|WfctlManifest|WfctlLockfile|PluginLock|PluginAdd|PluginRemove|MigratePlugins|InfraOutputs)' -count=1
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Phase Continuation — Module Env-Resolution Consistency — Design

**Status:** Approved (autonomous pipeline, 2026-04-24)

**Goal:** Close the class of bugs where one wfctl code path applies per-environment config overrides (`ModuleConfig.ResolveForEnv`) and another doesn't, producing **different identities** for the same logical resource and creating duplicate cloud resources or failed lookups. v0.18.9 patches the remaining known gaps (ci run deploy, infra_output source) and introduces a `config.ResolveModuleForEnv` helper to make future call sites default-correct.

**Why:** BMW deploy run 24888583717 (`wfctl ci run --phase deploy`) created a duplicate DO App Platform app with name "bmw-app" (the module's base name) while `wfctl infra apply` had minutes earlier created "bmw-staging" (the env-resolved name). Both attempts found no existing resource by their respective name variants, each proceeded to CREATE, result: two apps in DO. The "no deployment found" health-check failure was a downstream symptom of DO refusing to spawn the second app's deployment (name collision).

## Root cause

`cmd/wfctl/deploy_providers.go:769` inside `newPluginDeployProvider`:
```go
findByType := func(target string) bool {
    for i := range wfCfg.Modules {
        m := &wfCfg.Modules[i]
        ...
        cfg, ok := resolveModCfg(m)  // returns env-merged config map
        ...
        resourceName = m.Name         // ← BUG: uses base name, ignores env override
        resourceType = m.Type
        resourceCfg = cfg
        return true
    }
}
```

`resolveModCfg` DOES apply `ResolveForEnv` and returns the env-merged config. But the surrounding code then reads `m.Name` (base) instead of `resolved.Name` (env-overridden). The fix for `ResolvedModule.Name` (lifted from `Config["name"]` in v0.18.7) is ignored here.

**Fix pattern** already used successfully in `config/module_resolve_env.go:64-69`:
```go
if strings.HasPrefix(resolved.Type, "infra.") {
    if n, ok := resolved.Config["name"].(string); ok && n != "" {
        resolved.Name = n
        delete(resolved.Config, "name")
    }
}
```

`pluginDeployProvider` needs to adopt the same resolved identity.

## Scope — related gaps

Searching for `m.Name` direct usage after `resolveModCfg` / ResolveForEnv in wfctl reveals multiple call sites:

| Location | What it reads | Should read |
|---|---|---|
| `cmd/wfctl/deploy_providers.go:769` (ci run deploy) | `m.Name` | `resolved.Name` |
| `cmd/wfctl/deploy_providers.go:796` (fallback path) | `m.Name` | `resolved.Name` |
| `cmd/wfctl/infra_secrets.go` (`infra_output.source` parser) | module name in dot-path | env-resolved module name (task #56) |
| Any other phase reading modules by name | — | audit pass |

All suffer from the same class: **env-resolution helper returns the merged config but caller reads `m.Name` instead of the resolved name**.

## Architecture

### The helper pattern

Introduce `config.ResolveModuleForEnv(m *ModuleConfig, envName string) (*ResolvedModule, bool)` as the blessed API. It already exists — `ModuleConfig.ResolveForEnv` returns a `*ResolvedModule` with `Name` and `Config` correctly populated. The API is fine; **callers just need to use both fields of the return value**.

Rather than introduce a new helper, enforce usage:

1. **Fix the deploy_providers.go call sites** (lines 769, 796) to replace `resourceName = m.Name` with `resourceName = resolved.Name` where `resolved` is the `*ResolvedModule` from ResolveForEnv.
2. **Refactor `resolveModCfg` closure** to return `*ResolvedModule` instead of just the Config map — so the resolved.Name is available at the call site. Call signature becomes:
   ```go
   resolveModule := func(m *config.ModuleConfig) (*config.ResolvedModule, bool) {
       if envName == "" {
           return &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: m.Config}, true
       }
       return m.ResolveForEnv(envName)
   }
   ```
3. **Update all consumers** (three call sites in deploy_providers.go + `infra_secrets.go` for infra_output) to read `resolved.Name` and `resolved.Config`.

### Testing strategy

Regression tests — would have caught tonight's bug:

- `TestPluginDeployProvider_UsesEnvResolvedName` — construct a wfConfig with an `infra.container_service` module named `bmw-app` that has `environments.staging.config.name: bmw-staging`; call `newPluginDeployProvider(..., "staging")`; assert the returned provider's `resourceName == "bmw-staging"`, not `"bmw-app"`.
- `TestPluginDeployProvider_FallsBackToModuleNameWhenNoEnv` — envName="" case keeps base module name.
- `TestInfraOutput_EnvResolvesModuleSource` — `wfctl infra apply` with secrets.generate `infra_output` source `bmw-database.uri`; env override renames module to `bmw-staging-db`; infra_output finds state for `bmw-staging-db`, not `bmw-database`. (Addresses task #56.)
- `TestCIRunDeploy_PlansUpdateNotCreate_AfterInfraApply` — end-to-end: infra apply creates resource → ci run deploy should plan UPDATE (found by env-resolved name) not CREATE. Uses fake driver capturing Read/Create calls.

### Out of scope (defer to separate work)

- **Unifying deploy-phase state with IaC state** — the bug isn't really a state-sharing issue. Both subsystems use `driver.Read(ctx, ResourceRef{Name})` to find resources, which queries the cloud directly. As long as both use the SAME NAME, they find the same resource. No state coordination layer needed.
- **DAG-style pipeline orchestration** (`wfctl deploy --env X` as a meta-command) — larger scope, not a fix for this specific bug.
- **Deploy-phase typed-args refactor** — covered by v0.19.0 Feature C.

## Rollout phases

**Phase 1 (workflow v0.18.9):**
- Fix `deploy_providers.go:769` and `:796` to use `resolved.Name`.
- Refactor `resolveModCfg` closure to return `*ResolvedModule`.
- Add `TestPluginDeployProvider_UsesEnvResolvedName` regression test.
- Merge + tag v0.18.9.

**Phase 2 (workflow v0.18.9 or v0.19.0):**
- Fix `infra_secrets.go` `infra_output` source resolution (task #56).
- Add `TestInfraOutput_EnvResolvesModuleSource` regression test.
- Bundle into v0.18.9 if small enough; else defer to v0.19.0.

**Phase 3 (workflow v0.19.0):**
- Audit remaining call sites that read `m.Name` after env resolution. Patch any found.
- Documentation: DOCUMENTATION.md section on env-resolution consistency + the `resolved.Name` vs `m.Name` distinction.
- Add a `TestAllEnvResolutionCallSites` lint-style test or CI check (grep for `m.Name` in paths that use env resolution; flag patterns that don't use the ResolvedModule).

**Phase 4 (BMW unblock):**
- User triggers teardown-staging (wipes duplicate apps + state).
- BMW bumps setup-wfctl pin to v0.18.9.
- Deploy auto-fires, env-resolved names flow consistently, health check passes.

## Data flow (BMW staging, post-fix)

```
wfctl infra apply --env staging
  → planResourcesForEnv: ResolveForEnv(staging) → resolved.Name="bmw-staging"
  → platform.ComputePlan → Plan action: create bmw-staging
  → provider.Apply creates DO App Platform with name "bmw-staging"
  → state save: ResourceState{Name:"bmw-staging", ProviderID:<UUID>}

wfctl ci run --phase deploy --env staging
  → newPluginDeployProvider(...staging): resolveModule(m) → resolved.Name="bmw-staging"
  → pluginDeployProvider.resourceName = "bmw-staging"     (was "bmw-app" before fix)
  → Deploy: driver.Read(ref{Name:"bmw-staging"}) → finds existing DO app
  → driver.Update(ref, spec) → pushes new image to existing app (id=<UUID>)
  → health-check polls the same UUID → 200 → deploy succeeds
```

## Success criteria

- Unit tests added, all passing.
- `wfctl ci run --phase deploy --env staging` on BMW's config returns `pluginDeployProvider.resourceName == "bmw-staging"` (not `"bmw-app"`).
- End-to-end BMW retry post-v0.18.9 bump + teardown: staging /healthz 200, prod /healthz 200 auto-promote.
- No recurrence of duplicate DO resources from env-resolution mismatch (regression test gate at CI).
- DOCUMENTATION.md has a clear section warning future implementers: "always use `resolved.Name` from `ResolveForEnv`, never `m.Name`, in paths that consume modules with per-env overrides."

## Non-goals (explicit)

- State-sharing architecture between IaC and CI phases — the bug was about names, not state stores.
- Pipeline DAG / unified `wfctl deploy` meta-command — out of scope; later release.
- Changing `ResolveForEnv` signature / semantics — the function is correct, callers aren't using it correctly.
- Moving ci run deploy to read from IaC state — not needed; `driver.Read(name)` finds the same cloud resource if both paths use the same name.
