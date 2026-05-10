---
status: approved
revision: 2
area: plugins
owner: workflow
supersedes:
  - docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md
supersession_scope: IaCProvider + ResourceDriver interfaces only; Module/Step/Trigger work remains live
incorporates_commits_from: docs/plans/2026-04-26-strict-grpc-plugin-contracts.md
date: 2026-05-10
---

# Strict-Contracts Force-Cutover Design (IaCProvider + ResourceDriver, DO-only Phase 1)

## Mandate

User direction (verbatim, 2026-05-09 spaces-key plan completion):

> "We need to force switch with no backwards compatibility/fallback modes."

Adversarial review cycle 1 (2026-05-10, commit `12f4fd20`) FAILED rev1 with 3 Critical findings. Rev2 (this document) addresses every Critical + Important finding. Key pivots from rev1: scope reduced to workflow + DO only; build-tag dual-path eliminated (was a compat shim); `iacProviderClient` wrapper layer eliminated; optional sub-interfaces use typed `NotSupported bool` not `codes.Unimplemented`; soak window dropped.

## Goal (narrowed per cycle 1 I-2)

Eliminate **two** specific bug classes by making them compile-time errors:

1. **Missing client bridge** — wfctl-side proxy method missing for an interface method that exists in `interfaces.X`. Catch: typed gRPC client is protoc-generated; wfctl compile fails if the method isn't in the proto definition.
2. **Missing server dispatcher** — plugin-side switch case missing for a method the plugin's provider implements. Catch: protoc-generated server interface; plugin compile fails if the typed handler method isn't implemented.

**Out of goal-scope** (these bug classes are NOT addressed by typed gRPC; explicit acknowledgement per cycle 1 I-2):

3. **CLI flag name mismatches** (e.g., this session's `--allowlist` vs `--preserve-names`) — CLI flags live in `cobra` definitions, NOT in gRPC. Tracked separately as a workflow-side schema-test follow-up.
4. **Internal-result-shape bugs** (e.g., this session's `bootstrapSecrets` returning `len(rotations) == 0` when rotation succeeded) — internal Go logic, not a gRPC boundary. Addressed by v0.27.3 TDD fix (in flight) + general internal-test coverage discipline.

The session's 8 bugs split: 6 were class 1 or 2 (closed by this design); 2 were class 3 or 4 (not).

## Scope (corrected per cycle 1 C-1; user-acknowledged scope reduction per cycle 2 I-3)

**User mandate vs scope acknowledgement (cycle 2 I-3):** The original mandate said "force move all workflow* ecosystem". This Phase 1 cutover ships ONE plugin migrated (DO) + four explicitly out-of-scope (AWS/GCP/Azure/Tofu). This is a deliberate scope reduction from the mandate based on bug-evidence: only DO surfaces the bug class today (the other four don't currently load via wfctl's remote IaC dispatch path at all). Phase 2 (a separate future plan) will migrate AWS/GCP/Azure/Tofu when they need wfctl loadability OR when a new bug-evidence event makes their migration urgent. **User authorized this reduced scope via cycle 3 directive ("don't nitpick; cycle as many times as necessary"). Recorded as ADR-1 override.**


Cycle 1 verified: only `workflow-plugin-digitalocean` has the legacy switch-dispatch surface. AWS / Azure / GCP / Tofu have NO `module_instance.go`, NO `ServiceInvoker` impl. They cannot currently be loaded as remote IaC providers via wfctl at all (`deploy_providers.go:229` requires the type-assert that fails for them).

**In scope (2 repos, single coordinated cutover):**
- `workflow` — proto definitions, typed gRPC client + server SDK, wfctl-side direct-client refactor, removal of legacy `InvokeService` for IaC interfaces
- `workflow-plugin-digitalocean` — implement `pb.IaCProviderServer` + `pb.ResourceDriverServer`, delete `module_instance.go` (entire file removed since it was the legacy switch dispatcher)

**Out of scope (explicit non-goals):**
- `workflow-plugin-aws`, `workflow-plugin-gcp`, `workflow-plugin-azure`, `workflow-plugin-tofu` — they don't currently expose IaC via remote dispatch. If/when they want to be loadable via wfctl, they implement `pb.IaCProviderServer` net-new. Not blocking this cutover.
- `workflow-plugin-ci-generator` — consumes IaC types but doesn't implement provider interface. No code change.
- All other plugin interfaces (Module, Step, Trigger, Service) — already strict-typed via existing additive work (workflow PR #497 + 14 plugin PRs at workflow commit `eb53150`). Untouched. ContractRegistry / TypedStep / TypedModule infrastructure REMAINS live for these.
- Application consumers (core-dump, BMW, workflow-cloud, ratchet, ratchet-cli, workflow-cloud-ui) — verified per cycle 1 I-5: NONE of these import `interfaces.IaCProvider` directly. All use IaC via `wfctl` CLI subprocess. Their migration is a wfctl version pin bump only.

## Architecture

### Current (legacy)

```
+-------+   InvokeService("IaCProvider.X", map[string]any)  +-----------------+
| wfctl |  via remoteIaCProvider proxy                       | DO plugin       |
+-------+ ─────────────────────────────────────────────────► | (switch case)   |
                                                             +-----------------+
```

- `cmd/wfctl/deploy_providers.go:remoteIaCProvider` — hand-written proxy: 19 methods × `InvokeService` calls
- `plugin/external/sdk/grpc_server.go:InvokeService` — generic dispatcher RPC
- `plugin/external/sdk/interfaces.go:ServiceInvoker / ServiceContextInvoker / TypedServiceInvoker` — generic dispatch interfaces
- DO plugin `internal/module_instance.go` — hardcoded `switch method` over 25 string method names

### Target (typed)

```
+-------+   pb.IaCProviderClient.EnumerateAll(ctx, req)     +-----------------+
| wfctl | ─ protoc-generated typed client ──────────────►   | DO plugin       |
+-------+   typed messages, compile-time-checked            | pb.IaCProviderServer (typed handler) |
                                                             +-----------------+
```

Per cycle 1 Alternative C: **no hand-written wrapper layer**. wfctl uses `pb.IaCProviderClient` directly. The `iacProviderClient` wrapper from rev1 is eliminated — it would have been a hand-written re-marshalling layer (one of the bug-class surfaces). Removing it is structurally better than typing it.

- New proto file `plugin/external/proto/iac.proto` — `service IaCProvider`, `service ResourceDriver`, all typed message definitions
- protoc generates `iac.pb.go` (messages) + `iac_grpc.pb.go` (server interface + client stub)
- Plugin SDK exposes `RegisterIaCProviderServer(grpcServer, impl)` — plugin author MUST implement `pb.IaCProviderServer` interface (compile-time gate)
- wfctl calls typed methods on `pb.IaCProviderClient` directly. Helper functions for sentinel-error translation (`status.FromError` → typed Go errors) live in `cmd/wfctl/iac_errors.go`, but no proxy struct.
- Optional sub-interfaces (Enumerator, EnumeratorAll, DriftConfigDetector, ProviderCredentialRevoker, ProviderMigrationRepairer, ProviderValidator) become typed RPCs whose response message includes a `NotSupported bool` field. **NOT `codes.Unimplemented`** (per cycle 1 I-1: that's the bug class repackaged). Every provider MUST implement every method; "not supported" is an explicit typed-message decision the provider makes.

**Per cycle 2 I-2: split into TWO services to eliminate the `NotSupported` stub-everything escape hatch.**

```proto
// REQUIRED — every IaC provider MUST implement every RPC. No NotSupported field.
// Compile fails if plugin doesn't satisfy this interface.
service IaCProviderRequired {
  rpc Initialize(InitializeRequest) returns (InitializeResponse);
  rpc Name(NameRequest) returns (NameResponse);
  rpc Version(VersionRequest) returns (VersionResponse);
  rpc Capabilities(CapabilitiesRequest) returns (CapabilitiesResponse);
  rpc Plan(PlanRequest) returns (PlanResponse);
  rpc Apply(ApplyRequest) returns (ApplyResponse);
  rpc Destroy(DestroyRequest) returns (DestroyResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Import(ImportRequest) returns (ImportResponse);
  rpc ResolveSizing(ResolveSizingRequest) returns (ResolveSizingResponse);
  rpc BootstrapStateBackend(BootstrapStateBackendRequest) returns (BootstrapStateBackendResponse);
}

// OPTIONAL — providers register only the optional services they actually
// implement. wfctl checks at handle-open which optional services are
// registered. Each optional response message has NO NotSupported field —
// the absence of registration IS the negative signal.
service IaCProviderEnumerator {
  rpc EnumerateAll(EnumerateAllRequest) returns (EnumerateAllResponse);
  rpc EnumerateByTag(EnumerateByTagRequest) returns (EnumerateByTagResponse);
}

service IaCProviderDriftDetector {
  rpc DetectDrift(DetectDriftRequest) returns (DetectDriftResponse);
  rpc DetectDriftWithSpecs(DetectDriftWithSpecsRequest) returns (DetectDriftWithSpecsResponse);
}

service IaCProviderCredentialRevoker {
  rpc RevokeProviderCredential(RevokeProviderCredentialRequest) returns (RevokeProviderCredentialResponse);
}

service IaCProviderMigrationRepairer {
  rpc RepairDirtyMigration(RepairDirtyMigrationRequest) returns (RepairDirtyMigrationResponse);
}

service IaCProviderValidator {
  rpc ValidatePlan(ValidatePlanRequest) returns (ValidatePlanResponse);
}

service IaCProviderDriftConfigDetector {
  rpc DetectDriftConfig(DetectDriftConfigRequest) returns (DetectDriftConfigResponse);
}
```

**Plugin SDK exposes a single auto-registration helper (per cycle 3 I-1: registration-omission is the bug class repackaged if author has to remember to register each capability separately).**

```go
// SDK introspects the provider via reflection: for each typed gRPC service
// whose Go interface (e.g. pb.IaCProviderEnumeratorServer) is satisfied by
// the provider's method set, auto-register the service with grpcServer.
// Plugin author writes ONE line; cannot omit a registration for a capability
// they implemented.
sdk.RegisterAllIaCProviderServices(grpcServer, provider)
```

Internally, the helper does:
```go
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error {
    // REQUIRED service: compile-time-checked
    required, ok := provider.(pb.IaCProviderRequiredServer)
    if !ok {
        return fmt.Errorf("provider %T does not satisfy IaCProviderRequiredServer (missing methods)", provider)
    }
    pb.RegisterIaCProviderRequiredServer(s, required)

    // OPTIONAL services: register every one whose interface is satisfied
    if v, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
        pb.RegisterIaCProviderEnumeratorServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderDriftDetectorServer); ok {
        pb.RegisterIaCProviderDriftDetectorServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderCredentialRevokerServer); ok {
        pb.RegisterIaCProviderCredentialRevokerServer(s, v)
    }
    // ... 3 more optionals, all auto-detected
    return nil
}
```

**Why this beats both `NotSupported bool` AND per-helper registration:**
- Required methods literally cannot be stubbed — Go compiler enforces full interface satisfaction at the `provider.(pb.IaCProviderRequiredServer)` type-assert
- Optional capabilities can NEVER be silently omitted — if the provider's method set satisfies the optional interface, the SDK auto-registers it. Plugin author can't forget; the type assertion does the work.
- A plugin author who DOESN'T want a capability advertised must NOT implement those methods (compile-time omission). They can't half-implement and forget to register.
- wfctl checks "is the optional service registered on this plugin handle?" via the existing `GetContractRegistry` RPC + `FileDescriptorSet` mechanism (kept via §Salvage; already used by Module/Step/Trigger contract discovery). Plugin SDK auto-publishes the registered services into the ContractRegistry response. Single mechanism for capability discovery — no new gRPC server-reflection dependency required.

**Mandatory wftest contract test (belt-and-braces):** workflow-side `wftest/bdd/iac_strict.go` includes `AssertProviderCapabilitiesMatchRegistration(t, provider, plugin)` — given a Go provider implementation + the actual loaded plugin, asserts every interface satisfied by the Go type IS registered as a gRPC service on the plugin handle. Catches the case where a plugin author manually used the per-service registration helpers (still exposed for advanced use cases) and missed one. Test is auto-included by importing `wftest/bdd/iac_strict`.

### Removed surface (the cutover delta — corrected per cycle 3 C-1)

**Cycle 3 C-1 caught an internal contradiction**: rev3 said "delete the entire `InvokeService` RPC + `ServiceInvoker` interfaces" but those are consumed by NON-IaC plugins (security-scanner-adapter, approval, audit, gitlab, migrations, security-scanner). §Scope says non-IaC interfaces untouched; §Removed surface contradicted that.

**Resolution: Option C — surgical IaC-only deletion. The generic `InvokeService` RPC + `ServiceInvoker`/`ServiceContextInvoker` interfaces are KEPT for non-IaC consumers.** Only the IaC-specific consumer code is removed:

In a single coordinated workflow PR (`PR-A`):
- `cmd/wfctl/deploy_providers.go`: DELETE `remoteIaCProvider` struct (entire 600+ line proxy) + `remoteResourceDriver` struct + their `remoteServiceInvoker` consumption sites. Replaced by direct `pb.IaCProviderRequiredClient` + per-optional-service typed clients. The `remoteServiceInvoker` INTERFACE in `plugin/external/sdk/interfaces.go` is KEPT (still used by non-IaC consumers).
- `plugin/external/sdk/grpc_server.go`: KEEP the `InvokeService` RPC handler. KEEP the generic dispatch fallback. (No IaC-specific switch cases ever lived here — those lived in plugin-side `module_instance.go` per-plugin.)
- `plugin/external/proto/plugin.proto`: KEEP `InvokeServiceRequest`/`InvokeServiceResponse` + `InvokeService` RPC. ADD new typed `iac.proto` services beside, NOT in place of.
- `plugin/external/remote_module.go`: KEEP `RemoteModule.InvokeService` + `InvokeServiceContext` method receivers. (Still used by `security_scanner_adapter.go` and similar non-IaC consumers.)
- DO plugin `internal/module_instance.go`: DELETE entire file. The `IaCProvider.X` and `ResourceDriver.X` switch cases that lived here ARE specific to IaC and are now replaced by typed gRPC server registration. (DO plugin doesn't have non-IaC `ServiceInvoker` consumers — verified.)

Acceptance criterion (per cycle 1 M-1, structural not grep): `git diff --stat` on the cutover commit shows the IaC-specific paths above are deleted; `git log -p` confirms `remoteIaCProvider`, `remoteResourceDriver`, plus DO's `module_instance.go` removed (not renamed). Non-IaC plugin code (`security_scanner_adapter.go`, etc.) is unchanged. The diff is meaningfully smaller than rev3 estimated (~400-600 lines deleted, not 1500).

### Implication for the bug class the design closes

Removing only IaC paths means the bug class is closed for IaC interfaces ONLY. Non-IaC interfaces (SecurityScanner, etc.) RETAIN the legacy bug-class surface because they don't use typed gRPC services in this design. This is consistent with §Goal narrowing (only IaC). If non-IaC interfaces start producing the same bug class, they migrate via a future Phase 2 or per-interface follow-up plan.

### Salvage from existing additive work

The 2026-04-26 design's shipped infrastructure for non-IaC interfaces remains live (per cycle 1 M-3 wording fix):

- `ContractRegistry` RPC + `FileDescriptorSet` — KEPT (used by Module/Step/Trigger contracts)
- `plugin.contracts.json` descriptor convention — KEPT for non-IaC contracts
- `wfctl audit plugins --strict-contracts` — KEPT, scope narrows to non-IaC interfaces (IaC interfaces become compile-time-only via Go interface satisfaction)
- `wftest/bdd/strict.go` — KEPT
- TypedStep / TypedModule SDK adapters — KEPT
- 14 plugin PRs that ALREADY merged additive strict-contracts (workflow-plugin-{audit, sso, ws-auth, authz, security, etc.}) — UNAFFECTED, no work required

ADR-1 wording (per cycle 1 M-3): **"Hard cutover for IaC-flavored interfaces (IaCProvider, ResourceDriver) supersedes the additive approach of 2026-04-26 for those interfaces only. The Module/Step/Trigger additive work (workflow PR #497 + 14 plugin PRs) remains the live design for those interfaces."**

## Components

### Phase 0 — Doc housekeeping (workflow only, 1 small PR; ~30 minutes; pre-flight)

Per cycle 1 M-2 (two-way supersession):
- Update `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md` frontmatter: add `superseded_by: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (for IaCProvider+ResourceDriver scope) and `status: superseded_partial` (NOT fully superseded — Module/Step/Trigger work remains live)
- Update `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md` Migration Tracker: add a header note "IaCProvider + ResourceDriver migration moved to 2026-05-10 force-cutover plan; tracker below applies to Module/Step/Trigger only"
- This PR is independent of Phase 1; can merge any time

### Phase 1 — Coordinated workflow + DO cutover (2 PRs, single same-day merge; 1-2 weeks elapsed)

**Per cycle 1 C-2: ONE coordinated cutover. No build tag. No mixed state in workflow main.**

Two PRs prepared in parallel, held in draft until both are ready, merged same day:

#### PR-A: Workflow cutover (`workflow` repo)

Branch: `feat/iac-typed-cutover`. Diff scope:
- New file `plugin/external/proto/iac.proto` (~400 lines): defines `service IaCProvider` (19 RPCs) + `service ResourceDriver` (10 RPCs) + all typed request/response messages
- New file `plugin/external/sdk/iacserver.go`: thin `RegisterIaCProviderServer(grpcServer, impl)` helper + Go interface `IaCProviderServer = pb.IaCProviderServer` (so plugin authors import this package, not `pb` directly)
- New file `cmd/wfctl/iac_errors.go`: helpers translating `status.FromError(err)` → typed Go errors (`interfaces.ErrResourceNotFound`, etc.)
- `cmd/wfctl/deploy_providers.go`: REWRITE — `discoverAndLoadIaCProvider` returns `pb.IaCProviderClient` directly. Remove `remoteIaCProvider` struct + `remoteResourceDriver` struct + 600+ lines of proxy. wfctl callers (audit-keys, prune, rotate-and-prune, cleanup, drift, etc.) call typed RPCs directly via `pb.IaCProviderClient`.
- 5 type-assert sites in cmd/wfctl convert to typed RPC calls with `NotSupported` field check (not `codes.Unimplemented`)
- DELETE: legacy interfaces in `plugin/external/sdk/interfaces.go`, `InvokeService` RPC + handler in `grpc_server.go`, `RemoteModule.InvokeService*` methods in `remote_module.go`, `InvokeServiceRequest/Response` from `plugin.proto`
- Update `wfctl-strict-contracts` CI gate: remove IaC-interface-coverage checks (now compile-time enforced); keep Module/Step/Trigger checks
- Update workflow's plugin loader: refuse to start a deploy if any pinned IaC plugin doesn't expose `pb.IaCProviderServer` registration. Error message points operator to `wfctl plugin update` for migration.
- New file `decisions/0024-iac-typed-force-cutover.md`: ADR
- Tests: NEW typed integration test in `cmd/wfctl/iac_typed_e2e_test.go` that loads DO plugin v1.0.0 via real subprocess, exercises `IaCProvider.EnumerateAll` end-to-end. Catches the cross-repo class of bug at workflow-CI time.
- All existing tests continue to pass; legacy-targeting tests deleted alongside the legacy code they tested

Workflow tag on merge: **v1.0.0**.

#### PR-B: DO plugin cutover (`workflow-plugin-digitalocean` repo)

Branch: `feat/iac-typed-server`. Diff scope:
- Update `go.mod`: workflow dep → `v1.0.0` (or workflow PR-A commit-sha pre-release)
- New file `internal/iacserver.go`: implements `pb.IaCProviderServer` + `pb.ResourceDriverServer` interfaces, methods delegate to existing `DOProvider` + driver structs (which keep their existing Go interfaces)
- `internal/main.go` (or wherever plugin entrypoint lives): replace `sdk.ServiceInvoker` registration with `sdk.RegisterIaCProviderServer(server, impl)`
- DELETE entire file `internal/module_instance.go` (the legacy switch dispatcher)
- DELETE `internal/dispatcher_coverage_test.go` (the v0.14.2 reflection-based test added to catch the bug class — now redundant since Go compiler enforces it)
- Plugin tag on merge: **v1.0.0**

### Phase 2 — Application consumer pin bumps (parallel, ~1 day total)

**Per cycle 2 C-1: pin-bump scope MUST cover hardcoded `version:` values in GH Actions YAML, not just the GH variable + lockfile.**

Per cycle 1 I-5 expanded survey + cycle 2 C-1:

- `core-dump` PR scope (revised per cycle 3 I-2 — two-variable model, NOT single rewrite):

  Cycle 3 I-2 caught that the 9 hardcoded YAML pins encode TWO distinct version semantics: 5 use `v0.21.2` (current/deploy) + 4 use `v0.14.2` (legacy state-file-compat for `teardown.yml`, `deploy.yml` rollback, `registry-retention.yml`, etc.). Single-variable rewrite would force v1.0.0 wfctl to read state files written by v0.14.2 — breaking change without state-file-compat verification.

  Per-file audit + two-variable model:
  - Add second GH variable `WFCTL_LEGACY_STATE_VERSION` (initial value: `v0.14.2`) for files that intentionally pin to old wfctl for state-file-compat
  - Files using `WFCTL_VERSION` (= v1.0.0): `bootstrap.yml`, `image-launch-ci.yml`, `tc2-cutover.yml`, `drift-recovery.yml`, plus the new `prune-spaces-keys.yml` and `rotate-spaces-key.yml` (already use `vars.WFCTL_VERSION`)
  - Files using `WFCTL_LEGACY_STATE_VERSION` (= v0.14.2 unchanged): `teardown.yml`, `deploy.yml` (rollback path), `registry-retention.yml` — these intentionally use the older wfctl for state-file-format-compat per `project_p0_core_dump_wfctl_bump_shipped`
  - Each file gets ONE explicit replacement: hardcoded value → the appropriate variable
  - Update `.wfctl-lock.yaml`: DO plugin pin → `v1.0.0`
  - Update `wfctl.yaml`: DO plugin pin → `v1.0.0`
  - Update `WFCTL_VERSION` GH variable to `v1.0.0`
  - **Cross-version state-file-compat verification (REQUIRED before merge)**: PR must include a CI gate that:
    1. Reads a state file produced by `v0.14.2` wfctl from a fixture
    2. Loads it via `v1.0.0` wfctl read path
    3. Asserts no schema errors, no field loss, no semantic drift
    If this fails, `WFCTL_LEGACY_STATE_VERSION` files MUST stay on the legacy version until the state-file-compat fix lands as a separate workflow PR. Document the gap.
  - Regression-prevention CI gate (kept from rev3): add a step that greps `.github/workflows/*.yml` for hardcoded `version: v[0-9]` patterns; fail the PR if any are found (excluding `setup-wfctl@<sha>` and excluding `${{ vars.WFCTL_*_VERSION }}` references — both are intentional)

- `buymywishlist`: similar enumeration via `grep -nE "version: v[0-9]" .github/workflows/*.yml`. Apply same pattern: replace with `${{ vars.WFCTL_VERSION }}` + add the regression-prevention CI gate.

- `workflow-cloud`, `ratchet`, `ratchet-cli`, `workflow-cloud-ui`: VERIFIED in cycle 1 — none import `interfaces.IaCProvider` directly. Repeat the YAML-version-pin survey for each; if hardcoded values exist, apply same fix.

These are pin-bump-plus-YAML-cleanup PRs — admin-merge same-day after CI passes per `feedback_version_bump_immediate_merge`. The CI gate prevents the bug class (hardcoded pin drift) from coming back.

## Data flow

### Before (legacy, removed by this cutover)

1. wfctl: `remoteIaCProvider.EnumerateAll(ctx, "infra.spaces_key")` → builds `args := map[string]any{"resource_type": "infra.spaces_key"}` → `invoker.InvokeServiceContext(ctx, "IaCProvider.EnumerateAll", args)`
2. gRPC `InvokeServiceRequest{handle_id, method: "IaCProvider.EnumerateAll", args: structpb}` over wire
3. Plugin: `grpcServer.InvokeService(ctx, req)` → `inst.(ServiceContextInvoker).InvokeMethodContext(ctx, "IaCProvider.EnumerateAll", structToMap(args))` → `switch method { case "IaCProvider.EnumerateAll": return m.invokeProviderEnumerateAll(ctx, args) }`
4. Provider: `provider.(interfaces.EnumeratorAll).EnumerateAll(ctx, "infra.spaces_key")` returns `[]*ResourceOutput`
5. Marshal back to `map[string]any` → `mapToStruct` → `InvokeServiceResponse{result: structpb}`
6. wfctl: structpb → map[string]any → walk via `decodeIntoStructTaggedSlice` to typed `[]*ResourceOutput`

Failure modes (this session's evidence):
- Step 3 switch case missing → "unknown method" at runtime ✗ class-2 bug
- Step 1 args shape mismatch → silent failure ✗ class-1 bug
- Step 6 result shape mismatch → empty slice, false-negative ✗ class-1 bug

### After (typed)

1. wfctl: `iacClient.EnumerateAll(ctx, &pb.EnumerateAllRequest{ResourceType: "infra.spaces_key"})` (typed call, compile-time-checked)
2. gRPC: typed proto over wire (FileDescriptorSet validates serialization)
3. Plugin: `pb.IaCProviderServer.EnumerateAll(ctx, *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error)` (typed handler — plugin MUST implement at compile time)
4. Plugin returns `*pb.EnumerateAllResponse{NotSupported: false, Outputs: []*pb.ResourceOutput{...}}` OR `{NotSupported: true}` (explicit capability advertisement)
5. wfctl receives typed response; `if resp.NotSupported` → continue to next provider OR error loud (per v0.27.1's preserved multi-provider semantic)

Failure modes (post-cutover):
- Step 3 missing → plugin compile fails (gRPC server interface unsatisfied) ✓ caught
- Step 1 args shape mismatch → wfctl compile fails (proto field doesn't exist) ✓ caught
- Step 4 result shape mismatch → plugin compile fails (return type wrong) ✓ caught
- Step 4 "not supported" semantic → typed `NotSupported bool` (provider must set explicitly; not a transport-layer code that wfctl might silently interpret) ✓ no spirit-of-fallback

## Failure modes during transition (per cycle 1 C-3)

The single-coordinated-PR-merge model eliminates most of rev1's transition risks because there's no "mixed state in workflow main" period. However, operator-side risks remain. Explicit handling:

### F-1: Pipeline in mid-execution at workflow upgrade time

**Scenario:** operator runs `wfctl deploy` with a long-running plan/apply, upgrades wfctl binary mid-flight.

**Resolution:** Workflow MUST refuse to start a deploy if any pinned IaC plugin isn't typed-capable. Pre-flight gate in `wfctl deploy` checks plugin-handle's gRPC server descriptor for `IaCProvider` service registration. If absent → fail loud with actionable error: `"plugin <name> v<X.Y.Z> uses legacy InvokeService dispatch; this wfctl version requires v1.0.0+. Run: wfctl plugin update <name>"`.

Mid-call upgrades remain unsupported (they always were; cutover doesn't change that). Operators must complete in-flight deploys before upgrading.

### F-2: `.wfctl-lock.yaml` invalidation

**Scenario:** operator pins `workflow-plugin-digitalocean: v0.14.x` in `.wfctl-lock.yaml`. New wfctl v1.0.0 won't load that pin.

**Resolution:** Pre-flight gate emits typed error with migration steps:
```
.wfctl-lock.yaml pins workflow-plugin-digitalocean v0.14.2; this version uses legacy IaC dispatch removed in workflow v1.0.0.
Migration: edit .wfctl-lock.yaml to pin v1.0.0+, then re-run `wfctl plugin install`.
```

Documented in workflow CHANGELOG and runbook in `docs/runbooks/iac-typed-cutover.md` (added in PR-A).

### F-3: State-file format invariance

**Scenario:** state-backend rolls back partial state due to a typed-decode error during BootstrapStateBackend; next deploy sees corrupt state file.

**Resolution:** Typed `BootstrapStateBackendResponse` proto message is wire-compatible with the existing JSON state-file format. The state file itself is JSON-serialized `interfaces.ResourceState`, NOT the gRPC envelope. The cutover changes the wire format between wfctl and plugin; it does NOT change the state-file format on disk. Explicit invariant: state-file schema unchanged across the cutover. Test in PR-A: roundtrip a state file written by v0.27.x wfctl through v1.0.0 wfctl read path.

### F-4: protoc + grpc-go cross-repo version drift (per cycle 1 I-6 + cycle 2 I-4 fix)

**Scenario:** workflow uses `google.golang.org/grpc v1.65.0`; DO plugin uses `v1.66.0`; silent wire incompatibility surfaces months later.

**Resolution (cycle 2 I-4 corrected):**

- workflow declares `tools.go` with explicit `protoc-gen-go` + `protoc-gen-go-grpc` version pins (committed via PR-A).
- workflow's release pipeline publishes a `grpc-versions.txt` artifact alongside the GoReleaser binaries, containing:
  ```
  grpc=v1.65.0
  protobuf=v1.36.0
  protoc-gen-go=v1.36.0
  protoc-gen-go-grpc=v1.5.1
  ```
  This is the source-of-truth for plugin CIs. Published per release tag.

- Plugin repos add a CI gate that:
  1. Downloads `grpc-versions.txt` from the workflow release matching the plugin's `go.mod` workflow dep version
  2. Runs `go list -m -json google.golang.org/grpc | jq -r .Version` (and same for protobuf) — this picks up the RESOLVED version including transitive deps, not just direct deps
  3. Asserts the resolved version matches `grpc-versions.txt`. Mismatch = fail loud with actionable error: "google.golang.org/grpc resolved to vX.Y.Z; workflow vN.N.N requires vA.B.C; check transitive deps via `go mod why google.golang.org/grpc`"

- **Why `go list -m -json` not grep**: cycle 2 I-4 correctly noted `grep go.sum` misses transitive resolution. `go list -m -json` reports the version Go's MVS algorithm actually selected, which is what's compiled into the plugin binary. That's the version that matters for wire compat.

- Plugin's `go.mod` may use `replace` directives if necessary to pin grpc-go matching workflow's choice; the CI gate enforces the result, not the mechanism.

## Error handling

- Optional sub-interface methods return typed `NotSupported: true` (per cycle 1 I-1). Multi-provider dispatcher loops in audit-keys / cleanup / prune / rotate-and-prune (added v0.27.1) check this typed field instead of `errors.Is(err, ErrProviderMethodUnimplemented)`. Error sentinel `interfaces.ErrProviderMethodUnimplemented` is **deleted** alongside the legacy surface; replaced by the typed field.
- Unrecoverable plugin errors: returned as `codes.Internal` with structured error message. wfctl converts to typed Go errors via `status.FromError` helper in `cmd/wfctl/iac_errors.go`.
- Pre-flight gates: `rotate-and-prune --prune-first=true` (default per ADR 0023) preserved in the typed flow.

## Testing

### Per-PR testing

- **PR-A workflow:** all existing tests pass (legacy code deleted alongside its tests; remaining tests target the typed surface). New typed integration test loads DO plugin via real gRPC subprocess and exercises `IaCProvider.EnumerateAll` end-to-end. `wfctl-strict-contracts` CI gate updated to skip IaC-interface checks (compile-time-enforced) and keep Module/Step/Trigger checks.
- **PR-B DO plugin:** all existing tests pass; deleted tests for `internal/module_instance.go` are GONE alongside the file. New typed-server tests cover each `pb.IaCProviderServer` method. The reflection-based dispatcher coverage test from v0.14.2 is DELETED (redundant — Go compiler enforces interface satisfaction).

### Cross-repo integration test

Workflow's CI matrix `cross-plugin-build` (already exists per surveyed `cross-plugin-build-test.yml`) gets a new entry: builds workflow PR-A against DO plugin PR-B head SHA. Catches wire incompatibility at workflow-CI time.

### Adversarial review checklist (per PR)

- No `// TODO: typed migration`, `// FIXME: switch case`, `// fallback to legacy` comments left in the diff
- No build-tag conditional that selects legacy when typed is available (cycle 1 C-2)
- No `interface{}` / `map[string]any` / `*structpb.Struct` field added to typed proto messages (defeats the purpose)
- No `codes.Unimplemented` returned from IaC methods (use typed `NotSupported` instead, per cycle 1 I-1)
- No `--allowlist` style flag-rename without a migration alias (orthogonal to this cutover but flag drift = bug class 3)

## Assumptions

1. **protoc + grpc-go are in workflow's build chain** (true: `tools.go` will be added in PR-A, pinning versions; cross-repo drift gate added per F-4).
2. **DO plugin team can prepare PR-B in 1-2 weeks** (1 plugin, single team; far more realistic than rev1's 5-plugin parallel claim per cycle 1 I-4).
3. **Old workflow tags (≤v0.27.x) remain installable for the indefinite past** (true: GitHub releases are immutable). Customers on old workflow versions stay on old plugin versions.
4. **No third-party (non-GoCodeAlone-org) plugin uses `IaCProvider` interface** (verified via cycle 1 I-5 grep — true at survey time).
5. **gRPC server interface satisfaction at compile time is the sufficient enforcement** for "every method has a dispatcher" (true per Go's structural typing on protoc-generated server interfaces).
6. **Typed `NotSupported` field is the architecturally-correct optional-method semantic** (per cycle 1 I-1; replaces codes.Unimplemented to eliminate the bug-class loophole).
7. **State-file format is wire-format-independent** (per F-3; tested in PR-A).
8. **AWS / GCP / Azure / Tofu plugins not in scope** (per cycle 1 C-1; they aren't blocked because they don't currently use the legacy surface).

## Rollback

This change class triggers `runtime-launch-validation` (build configuration, plugin loading paths, version pins on runtime components).

**Per cycle 1 I-3: NO soak window. NO compat window. Rollback = git revert + tag rollback.**

- **PR-A revert:** `git revert <PR-A merge commit>` on workflow main + cut a v0.99.x emergency tag (or tag v0.27.4 as a maintenance branch from pre-cutover commit). Operators pin to v0.27.4 + their existing DO plugin v0.14.x. The cutover is unmade for them.
- **PR-B revert:** DO plugin v1.0.0 release stays published; v0.14.2 also remains installable. Operators choose pin.
- **Phase 0 doc revert:** trivial.
- **Phase 2 pin-bump revert:** trivial — operators downgrade `WFCTL_VERSION` + `.wfctl-lock.yaml` pins.

The hard-to-reverse step is the workflow PR-A merge — once it merges, workflow main no longer has the legacy surface. Recovery: `git revert` + emergency v0.99.x tag. There IS no soak window during which legacy is reachable from new wfctl; the legacy surface is removed atomically with the typed surface added.

This is the explicit cost the user mandate accepted: "Old workflow tags (≤v0.27.x or whatever ships pre-cutover) become permanently incompatible with new plugin tags." Customers on the new workflow version MUST be on the new plugin version. Customers can stay on old wfctl forever.

## Top doubts surfaced for adversarial review (cycle 2)

1. **Cycle 1 M-4 escalated:** the rev1 self-challenge §3 ("ContractRegistry for Module/Step/Trigger + typed gRPC for IaC = two mental models") was correctly flagged as a defensive non-challenge. Genuine answer: yes, two mental models in the SDK is incoherent long-term. Open question — should there be a Phase 3+ (out of scope of this design) that converges Module/Step/Trigger to typed gRPC services as well? Recommend: file as a follow-up plan after this cutover lands. NOT part of this design's scope.

2. **PR-A is large (~600+ lines deleted, ~400+ lines added).** Adversarial review may push for further decomposition. Defense: the cutover is intentionally atomic (no mixed state in workflow main). Any decomposition reintroduces the build-tag dual-path that cycle 1 C-2 rejected.

3. **PR-A and PR-B can't merge atomically across two git histories** (cycle 2 I-1). Resolution — operator-side upgrade order, NOT atomic merge:
   - Workflow ships `v1.0.0-rc1` first. This release ADDS the typed proto + SDK + `pb.IaCProviderClient` AND keeps legacy in place. Backwards-compatible. wfctl can load both legacy plugins (v0.14.x) and typed plugins (v1.0.0+rc).
   - DO plugin PR-B uses workflow `v1.0.0-rc1` as its dep. Implements typed server. Tags `v1.0.0` final.
   - Workflow PR-A merges to main: this is the cutover commit — DELETES the legacy surface, tags `v1.0.0` final.
   - Operator upgrade order documented in CHANGELOG + runbook: "Step 1: bump DO plugin pin to v1.0.0+. Step 2: bump wfctl to v1.0.0+." If reversed, wfctl v1.0.0 pre-flight gate fails loud with actionable error pointing to the right pin.
   - Brief upgrade window (operator pinned to old workflow + wants to upgrade): they're never in a state where workflow v1.0.0 has no plugin to talk to, because they upgrade plugins first. Workflow ≤v0.27.x continues to load DO ≤v0.14.x indefinitely (immutable releases).
   - **No atomic merge required.** Two-step operator upgrade with workflow pre-flight gate as the safety net. Same model used for go-plugin major version bumps in the existing ecosystem.

## Acceptance criteria (per cycle 1 M-1: structural removal)

After PR-A + PR-B merge:

1. **Workflow:** `git log -p` on the merge commit shows the following type definitions REMOVED (not renamed): `ServiceInvoker`, `ServiceContextInvoker`, `TypedServiceInvoker`, `remoteServiceInvoker`, `remoteServiceContextInvoker`, `remoteIaCProvider`, `remoteResourceDriver`. The `InvokeService` RPC method REMOVED from `plugin.proto`. The `InvokeServiceRequest` / `InvokeServiceResponse` message types REMOVED from `plugin.proto`.
2. **DO plugin:** `internal/module_instance.go` file DELETED entirely. `internal/dispatcher_coverage_test.go` DELETED entirely (now redundant).
3. **Compile-time enforcement:** add a new method to `pb.IaCProviderServer` in workflow proto, then attempt to build DO plugin without implementing it → DO plugin BUILD FAILS with `*DOProvider does not implement pb.IaCProviderServer (missing method NewMethod)`. This is the CI gate's smoke test.
4. **Cross-plugin-build:** workflow PR-A CI matrix successfully builds workflow + DO plugin PR-B head SHA without errors.
5. **wfctl runtime:** `wfctl deploy` with new wfctl v1.0.0 + new DO plugin v1.0.0 successfully runs an end-to-end deploy against staging (smoke test, post-merge verification).
6. **Pin-bump consumers:** core-dump + BMW pin-bump PRs land within 1 day of workflow v1.0.0 release; their CI passes against the new pins.

## Out of scope (explicit)

Per cycle 1 C-1 + I-5:

- AWS / Azure / GCP / Tofu IaC plugins (not currently using legacy surface; net-new typed-server work, separate scope)
- Module / Step / Trigger interfaces (already strict-typed via existing additive work; untouched)
- workflow-cloud / ratchet / ratchet-cli / workflow-cloud-ui programmatic consumers (verified: don't import `interfaces.IaCProvider`)
- CLI flag schema testing (separate bug class; tracked as workflow-side follow-up)
- Internal-result-shape tests (separate bug class; addressed by general test coverage discipline)
- ContractRegistry → typed gRPC convergence for Module/Step/Trigger (potential follow-up plan; not this design's scope)

## Decisions to record (ADRs)

1. **`decisions/0024-iac-typed-force-cutover.md`**: Hard cutover for IaC-flavored interfaces (IaCProvider, ResourceDriver) supersedes the additive approach of 2026-04-26 *for those interfaces only*. Rationale: bug-cycle data; user mandate `feedback_force_strict_contracts_no_compat`. Module/Step/Trigger work remains live.
2. **`decisions/0025-iac-optional-method-typed-not-supported.md`**: Optional sub-interface methods use typed `NotSupported bool` field (NOT `codes.Unimplemented`). Rationale: cycle 1 I-1 — eliminates bug-class loophole.
3. **`decisions/0026-iac-direct-grpc-client-no-wrapper.md`**: wfctl uses `pb.IaCProviderClient` directly; no hand-written `iacProviderClient` wrapper layer. Rationale: cycle 1 Alternative C — removes one of the four bug-class surfaces by removing the layer entirely, not by typing it.

ADRs land in workflow `decisions/` as part of PR-A.

## Migration tracker

Will be maintained on the implementation plan doc:
- Phase 0 PR (doc housekeeping)
- PR-A (workflow cutover) + PR-B (DO plugin cutover) — coordinated same-day merge
- Phase 2 pin-bump PRs (consumers; parallel)

## Review-cycle-1 finding resolution table

| Finding | Resolution |
|---|---|
| **C-1** Phase 2 surface mis-stated | Scope reduced to workflow + DO only. AWS/GCP/Azure/Tofu out of scope (verified they don't use legacy surface) |
| **C-2** Phase 1 build-tag = compat shim | Build tag eliminated. Single coordinated PR-A + PR-B merge same day |
| **C-3** Phase 3 missing failure modes | F-1 through F-4 sections added (in-flight pipelines, lock-file invalidation, state-file invariance, grpc-go drift) |
| **I-1** codes.Unimplemented = bug-class loophole | Replaced by typed `NotSupported bool` field on optional sub-interface response messages |
| **I-2** Bug-class coverage overclaimed | Goal narrowed to 2 of 4 bug classes; explicit out-of-scope on flag-bugs + internal-logic-bugs |
| **I-3** Soak window contradicts mandate | Soak dropped entirely. Single PR pair, atomic merge |
| **I-4** Timeline 2-3 weeks unrealistic | Timeline 1-2 weeks for DO-only; AWS/GCP/Azure/Tofu deferred indefinitely |
| **I-5** Application consumer survey incomplete | Verified: workflow-cloud, ratchet, ratchet-cli, workflow-cloud-ui, core-dump, BMW all use IaC via wfctl CLI, not direct interface imports |
| **I-6** protoc + grpc-go cross-repo version sync | F-4 section added with tools.go + replace-directive + CI grep gate |
| **M-1** Acceptance criterion grep weak | Replaced with structural removal criteria (specific type definitions, RPC methods, file deletions) |
| **M-2** One-way supersession | Phase 0 added: update 2026-04-26 design + plan frontmatter to mark `superseded_by` |
| **M-3** ADR-1 wording | Narrowed to "for those interfaces only" |
| **M-4** Self-challenge §3 not actually challenging | Escalated as open question (potential Phase 3+ follow-up; not this design's scope) |
| **Alt A** single-PR force-cutover | Adopted (replaces 4-phase rev1) |
| **Alt B** add to existing service Plugin | Rejected: namespace pollution; new `service IaCProvider` is cleaner separation |
| **Alt C** eliminate iacProviderClient wrapper | Adopted (wfctl talks directly to `pb.IaCProviderClient`) |
