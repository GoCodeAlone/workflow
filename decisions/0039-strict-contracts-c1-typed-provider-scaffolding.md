# 0039. Strict-contracts validation gap (c1 ruling) + IaCServeOptions.TypedModules/TypedSteps SDK scaffolding

**Status:** Accepted
**Date:** 2026-05-15
**Decision-makers:** autonomous pipeline (cloud-sdk-bcd team), Jon (operator — direction 2026-05-15)
**Related:** decisions/0038 (plugin modules on IaC serve bridge — Approach A), decisions/0024 (iac typed force cutover), decisions/0031 (strict-contracts ergonomics), docs/plans/2026-05-15-plugin-modules-on-iac.md, workflow PR #683 (SDK extension), workflow PR #686 (Typed* future-prep), workflow-plugin-aws PR #15, workflow-plugin-gcp PR #9

## Context

Mid-execution of plan-2 (Tasks 3-11), the strict-contracts validator at `cmd/wfctl/plugin_audit.go:328` `addPluginContractKindFindings` was discovered to require every advertised type in `capabilities.{moduleTypes,stepTypes}` to have a `mode:"strict"` descriptor in `plugin.contracts.json`. The aws + gcp plugin work shipped via plan-2 PR 2 + PR 3 used the legacy `sdk.ModuleProvider` / `sdk.StepProvider` interfaces (config-Struct / map[string]any) — the ONLY interfaces available on the IaC serve bridge in the version of the SDK that landed in plan-2 PR 1 (workflow v0.53.0). Without strict descriptors, `wfctl plugin audit` rejects the plugin manifest as non-compliant.

Three resolution paths were weighed:

- **(c1)** Keep the legacy `ModuleProvider`/`StepProvider` Go implementations on aws/gcp; add **proto definitions + strict descriptors** to each plugin's `plugin.contracts.json` to satisfy the validator. The descriptors document the eventual typed-Provider migration target without forcing it now.
- **(c2)** Postpone the validator gate by relaxing the audit rule for IaC-bridge-served plugins (require strict only when a `TypedModuleProvider`/`TypedStepProvider` is registered). Soft on the long-term goal.
- **(B1)** Eagerly migrate aws + gcp to `TypedModuleProvider`/`TypedStepProvider`. Largest scope; requires the SDK extension itself.

User direction (verbatim 2026-05-15): "only DigitalOcean module is in active use by any projects right now. So only DO would need migration, and I believe all active projects are already using DO strict plugin. So you should be able to implement strict without maintaining legacy, just document the change". Interpreted: no parallel legacy-Provider VERSIONS need to ship (no v1.0.x compat track); but the existing plan-2 work that already coded against the legacy ModuleProvider interface is acceptable so long as the audit gate is satisfied.

Independent of the resolution, the SDK author had already added `TypedModuleProvider` + `TypedStepProvider` interfaces to `plugin/external/sdk/typed.go` ahead of any plugin migration. This presented the question: does the plan-2 SDK extension (decisions/0038's `IaCServeOptions.Modules/Steps`) ALSO carry parallel `TypedModules`/`TypedSteps` fields (additive future-prep), or wait until the first consumer needs typed-Provider migration?

## Decision

**Adopt (c1) for plan-2 plugin shipping** + **adopt the SDK Typed-fields extension as future-prep** (workflow PR #686, merged before PR 4/5).

**(c1) shape (shipped in workflow-plugin-aws PR #15 + workflow-plugin-gcp PR #9):**
- Each plugin defines `internal/contracts/{aws,gcp}_plan2.proto` declaring strict-typed config / input / output messages for the new module types (`aws.credentials`, `storage.s3`, `step.s3_upload`, `gcp.credentials`, `storage.gcs`).
- The proto-generated `.pb.go` types are committed alongside.
- `plugin.contracts.json` adds `mode:"strict"` descriptors for each new type, pointing at the proto messages.
- The Go implementation continues to use the legacy `sdk.ModuleProvider`/`sdk.StepProvider` interfaces — no behavioral change at the gRPC boundary; the validator is satisfied by the descriptor presence.

**(SDK Typed-fields extension shape — merged via workflow PR #686 at 3a992ee8):**
- `IaCServeOptions` now carries `TypedModules map[string]TypedModuleProvider` + `TypedSteps map[string]TypedStepProvider` alongside the legacy `Modules`/`Steps` fields.
- `mapBackedProvider` implements `TypedModuleProvider` + `TypedStepProvider` in addition to the legacy `ModuleProvider` + `StepProvider` impls.
- `CreateTypedModule`/`CreateTypedStep` return `ErrTypedContractNotHandled` when the type isn't in the typed map; `grpc_server.go` falls through to legacy `CreateModule`/`CreateStep` (Typed-first → legacy-fallback contract that the strict-cutover already implements at the bridge level).
- `mergeTypeLists` in `grpc_server.go` already produces the union when a provider implements both interfaces — so legacy-only consumers see no change, typed-only consumers work, and dual-implementation consumers (a future migration) work without an SDK refactor.
- 6 new tests in `plugin/external/sdk/iacserver_modules_test.go` cover: typed/legacy union; typed-first dispatch; legacy fallback; typed-only-no-legacy.

**(c2) rejected** — relaxing the validator for IaC-bridge plugins erodes the strict-contracts cutover's value (decisions/0024). The validator is the ONE place where descriptor presence is enforced before bytes cross the wire; weakening it for a transitional gap creates a permanent escape hatch.

**(B1) rejected for plan-2** — would have lengthened the plan-2 plugin PRs by ~3-4 days of typed-Provider migration work blocking the Phase B/C core deletions. The user's "just get the remaining plugins finished up" mandate (verbatim 2026-05-15) prioritized the deletions over the migration.

## Consequences

- **Plan-2 plugins ship aws v1.1.0 + gcp v1.1.0 with strict descriptors satisfying `wfctl plugin audit`.** Operators see no degradation; the manifest passes the same gate as the older 4 plugins (azure, DO, aws v1.0.x, gcp v1.0.x).
- **The SDK now carries the future-prep typed-Provider scaffolding** so the eventual migration (whenever a consumer actually needs typed dispatch end-to-end) is a drop-in: replace the plugin's `Modules`/`Steps` map values with `TypedModules`/`TypedSteps` map values pointing at typed-Provider implementations. No SDK PR required at migration time.
- **The proto schemas in each plugin's `internal/contracts/` are now the canonical source of truth for the eventual typed migration** — they describe the same shape as the legacy config-Struct types but in proto. Drift is the risk: if the legacy Go config Struct evolves but the proto doesn't (or vice-versa), the typed migration will require a reconciliation pass. Mitigated by: the legacy types are now small + stable (config + input + output for ~5 module types each); the protos are committed in the same repo so visible to reviewers.
- **No workflow-core change required.** The workflow `v0.53.0` floor declared by plan-2 PR 1 (#683) carries everything needed; PR #686 added the SDK fields without bumping the engine version (held until a consumer needs typed dispatch — currently none).
- **Aws + gcp legacy ModuleProvider Go implementations are now load-bearing** — they answer real plugin requests. Maintenance follows the same pattern as the older 4 plugins (azure, DO).
- **Cost** — each plugin carries duplicated schema (Go config Struct + proto). When the typed migration lands, the Go config Struct deletes; until then the duplication is the visible cost of the (c1) ruling.
- **Testing surface unchanged** — plugin behavioral tests still hit the legacy Provider; the new strict descriptors add only manifest-validation coverage (already exercised by `wfctl plugin audit` in cross-plugin-build CI).

## Migration path (when a future consumer needs typed dispatch)

1. Implementer writes a `TypedModuleProvider` (or `TypedStepProvider`) for the target type, consuming the existing proto config message.
2. Plugin `cmd/.../main.go` registers the typed Provider in `IaCServeOptions.TypedModules` (or `TypedSteps`) instead of `Modules`/`Steps`.
3. Legacy Provider implementation deletes (no consumer left).
4. Plugin tag bump: minor (typed migration is additive at the gRPC boundary; the strict descriptor was already declared by (c1)).
5. Workflow tag bump only required if a new SDK helper is needed; not required for the migration itself.

Future ADR may follow if a typed migration uncovers SDK constraints (e.g., MessagePublisher/MessageSubscriber wiring for typed Providers — currently a Non-Goal per decisions/0038 Sub-decision).
