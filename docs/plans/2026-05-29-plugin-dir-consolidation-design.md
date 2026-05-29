# Design: plugin/ ↔ plugins/ consolidation + registry tracking

Date: 2026-05-29
Repo: GoCodeAlone/workflow (+ GoCodeAlone/workflow-registry)
Status: Draft → adversarial-design-review

## Intent (user)

`plugin/` (singular) = general plugin-supporting FRAMEWORK (loader, manager,
manifest, sdk, builder, registry, installer, cosign, native/external support).
`plugins/` (plural) = built-in plugin IMPLEMENTATIONS, each `New() *Plugin`,
wired via `plugins/all/all.go`, and announced to workflow-registry. Some
implementations are misfiled under `plugin/` (e.g. `plugin/storebrowser`).
Consolidate; ensure all built-in plugins tracked in workflow-registry. Cut a
release when done.

## Findings (from classification investigation)

Two registration patterns: **A** EnginePlugin `New()` + `plugins/all` import;
**B** NativePlugin `init()`→`plugin.RegisterNativePluginFactory`, activated by
blank import in `cmd/server/main.go` + invoked via `plugin.BuiltinNativePlugins`.

`plugin/` subdir classification:

| subdir | type | action |
|---|---|---|
| admincore | IMPL (NativePlugin, pattern B; main.go:40) | MOVE → plugins/admincore |
| docmanager | IMPL (NativePlugin, pattern B; main.go:41) | MOVE → plugins/docmanager |
| storebrowser | IMPL (NativePlugin, pattern B; main.go:43) | MOVE → plugins/storebrowser |
| builder | FRAMEWORK (Builder iface+registry; used by plugins/builder-*, wfctl) | STAY |
| registry | FRAMEWORK (RegistryProvider iface+registry; used by plugins/registry-*) | STAY |
| external | FRAMEWORK (gRPC/go-plugin loader + author SDK) | STAY |
| sdk | FRAMEWORK (manifest schema, scaffolder, iaclint used by IaC plugin repos) | STAY |
| community | orphaned dead code (0 importers) | DELETE |
| rbac | orphaned dead code (0 importers) | DELETE |
| ai/{anthropic,openai,generic} | `ai.AIProvider` impls (NOT plugins; 0 importers) | MOVE → ai/providers/ |

Registry: `workflow-registry/plugins/<name>/manifest.json`
(`{name,version,author,source,type:builtin|external,tier,path,capabilities}`).
admincore/docmanager/storebrowser NOT tracked → add. `plugins/builder-*` and
`plugins/registry-*` are wfctl-internal (implement builder/registry ifaces, not
engine capabilities) → intentionally untracked; document, do not add.

## Decisions

### D1 — Move the 3 native-plugin impls to plugins/ (core)
`git mv plugin/{admincore,docmanager,storebrowser} plugins/`. Each keeps
importing `…/workflow/plugin` (framework) — no circular risk (plugins/ already
depends on plugin/; plugin/ never imports plugins/). Update the 3 blank imports
in `cmd/server/main.go` from `…/plugin/X` → `…/plugins/X`. Zero other importers.
No registration-pattern conversion (pattern B mechanism lives in
`plugin/builtins.go`, which stays).

### D2 — Wiring location for moved NativePlugins
They are NativePlugins, not EnginePlugins; `plugins/all` aggregates
EnginePlugins only. Keep their activation as blank imports in
`cmd/server/main.go` (unchanged behavior), NOT in `plugins/all`. Rationale:
`plugins/all.DefaultPlugins()` returns `[]EnginePlugin`; forcing NativePlugins
in would conflate two contracts. (Adversarial-review question: is a
`plugins/all` native-plugin aggregator worth adding? Default: no — minimal change.)

### D3 — DEFER orphaned dead code (do NOT delete this pass)
`plugin/community` (submission-validation tooling) + `plugin/rbac`
(auth bridge `BuiltinProvider`, has a 226-line test suite) are orphaned
(0 production importers, not wired, not registered). Per adversarial review:
deleting exported packages the user didn't ask to remove is scope-creep, and
`plugin/rbac` is plausibly intended-but-unwired. **Leave them untouched this
pass**; file a follow-up to delete-or-wire with an explicit decision. (Rev:
was "delete"; downgraded to defer to avoid removing possibly-intended
functionality without confirmation.) NOTE the stale `scripts/audit-cloud-symbols.sh:131`
comment references `plugin/rbac/aws.go` which never existed — leave for the
follow-up, not this pass.

### D4 — Relocate misfiled AI providers (MOVE-ONLY, no wiring)
`git mv plugin/ai/{anthropic,openai,generic} ai/providers/`. These implement
`ai.AIProvider` (sibling to `ai/llm`, `ai/copilot`), not the plugin contracts —
they do not belong in `plugins/`. **Move only; do NOT wire.** (Rev: adversarial
review found the proposed `initAIService` wiring is architecturally impossible —
`initAIService` registers `ai.WorkflowGenerator` impls, but these are
`ai.AIProvider` impls consumed by `ai.AIModelRegistry.RegisterProvider`, which
is owned by `plugins/ai/plugin.go`, not initAIService. Wiring there would also
be a behavior change — activating currently-dead providers + a new
ANTHROPIC_API_KEY startup dependency — which is out of scope for a structural
cleanup.) Add a doc comment in each provider's `New()` pointing to
`plugins/ai/plugin.go`'s `aiRegistry.RegisterProvider(...)` as the future
wiring site, and file a follow-up issue for provider activation. They remain
unreferenced after the move (same as today) — but now correctly located.

### D5 — Registry reconciliation
Add `workflow-registry/plugins/{admincore,docmanager,storebrowser}/manifest.json`
(type:builtin, source:github.com/GoCodeAlone/workflow, path:plugins/X, tier per
capability, capabilities from each plugin's pages/routes/steps). Audit all
`plugins/*` engine packages vs registry.
**Audit result (verified):** the only unregistered `plugins/*` dirs are `all`
(aggregator, not a plugin) + `builder-{custom,go,nodejs}` +
`registry-{aws,azure,do,gcp,github,gitlab}` (wfctl-internal builder/registry-
provider impls — no engine capabilities). **0 additional engine-plugin gaps**
beyond the 3 moved ones → D5 adds exactly 3 manifests; the 10 internal dirs stay
intentionally untracked. Follow the existing manifest.json schema
(`workflow-registry/schema/registry-schema.json`) + regen `v1/index.json` via
the registry build/script if required (verify tests/ pass).

## Scope / phases (PR grouping)

- **PR-1 (workflow):** D1 move 3 native plugins (admincore/docmanager/storebrowser
  → plugins/) + D2 wiring (blank imports stay in cmd/server/main.go) + fix admincore
  path refs: `DOCUMENTATION.md:1794,1814`, `docs/DEFERRED_ISSUES.md:118`. Self-contained;
  build+test green. (D3 community/rbac NOT touched — deferred.)
- **PR-2 (workflow):** D4 AI provider relocation, MOVE-ONLY (no wiring). Separable.
- **PR-3 (workflow-registry):** D5 — exactly 3 manifests (admincore/docmanager/
  storebrowser) + index regen.
- Then: cut workflow release **v0.65.0** (minor bump — D1 changes public import
  paths `plugin/X`→`plugins/X`; D4 moves public packages; pre-1.0 semver policy
  per docs/API_STABILITY.md treats public-package moves as minor).

Out of scope: renaming/restructuring the `plugin/` framework itself; changing
the EnginePlugin/NativePlugin contracts; external-plugin (`plugin/external`) SDK.

## Verification

- `go build ./...` + `go test ./...` green (esp. cmd/server, plugins/*, ai/*).
- `cmd/server` boots; native plugins still register (admincore pages,
  storebrowser/docmanager routes present) — runtime-launch check.
- workflow-registry schema-validates (scripts/ + tests/).
- `golangci-lint run`; gofmt.
- No dangling `plugin/{admincore,docmanager,storebrowser,community,rbac,ai}`
  references anywhere (grep go + non-go).

## Risks

- Import-path churn is tiny (3 lines main.go) — low.
- AI provider wiring (D4) could change runtime AI behavior → gate behind config;
  separable PR-2.
- Registry manifest schema mismatch → validate against schema + existing entries.
- `plugin/ai/*` move target (`ai/providers/`) is a judgment call (not `plugins/`)
  — flagged for adversarial review.
- community/rbac: DEFERRED (not deleted/moved this pass) — removes that risk.

## Rollback

Each PR is a single revertable commit, no data migration. No compat aliases
needed: moved packages have no external direct-type callers; activation is via
internal blank imports (cmd/server) / aggregator. Reverting PR-1 restores the
plugin/ paths. Downstream local worktrees with stale `plugin/{admincore,...}`
blank imports rebase onto the new paths (acceptable; internal).
