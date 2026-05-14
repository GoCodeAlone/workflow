# 0036. IaCStateBackend.Configure RPC for backend config plumbing

**Status:** Accepted
**Date:** 2026-05-14
**Decision-makers:** autonomous pipeline (plan-authoring), Jon (operator — autonomous mandate)
**Related:** docs/plans/2026-05-14-cloud-sdk-extraction-bcd.md (PR 1 + PR 2), docs/plans/2026-05-14-cloud-sdk-extraction-design.md, decisions/0035-iac-state-backend-plugin-integration-surface.md

## Context

Phase A shipped the `IaCStateBackend` gRPC contract (7 RPCs: `GetState`/`SaveState`/`ListStates`/`DeleteState`/`Lock`/`Unlock`/`ListBackendNames`), the host-side `grpcIaCStateStore`, the engine host-wiring, and `workflow-plugin-azure v1.1.0` serving `azure_blob`. Phase A's PR 5 then deleted the in-core `azure_blob` backend.

Authoring the Phase B/C/D plan surfaced a gap that the design and both Phase-A adversarial passes missed — and that the B/C/D adversarial pass also missed: **the `IaCStateBackend` contract has no RPC that carries backend configuration** (bucket / region / account URL / credentials / endpoint). The 7 RPCs operate on `resource_id` + `IaCState`; none carries the `iac.state` module's YAML config. `engine.go`'s `loadPluginInternal` registers the client by name but never passes `m.config`. `iac_module.go`'s `default:` arm does `newGRPCIaCStateStore(client)` with no config path. The `workflow-plugin-azure` source confirms it explicitly: *"The state-backend contract has no Initialize RPC of its own — backend configuration plumbing (account URL, container, credential) is a follow-up PR. Until then the store is set via setStateStore (engine wiring / tests)."*

Consequence: Phase A's plugin-served `azure_blob` round-trips state in tests but is **non-functional end-to-end** — the plugin's store is `nil` and every RPC returns `FailedPrecondition`. B/C/D inherits this for `s3`/`gcs`/`spaces`, and for `spaces` it is worse than "incomplete": Phase B deletes the *functional* in-core `spaces` backend, so without config plumbing `backend: spaces` becomes a **functional regression**, not an extraction. This is `decisions/0035`'s lesson recurring — a load-bearing seam was assumed, not grep-verified.

## Decision

Add a **`Configure` RPC to the `IaCStateBackend` service**: `rpc Configure(ConfigureRequest) returns (ConfigureResponse)` where `ConfigureRequest { string backend_name = 1; bytes config_json = 2; }` — `config_json` is the JSON-encoded `iac.state` module config `map[string]any`. This mirrors the existing `InitializeRequest.config_json` pattern already in `iac.proto` for `IaCProviderRequired`, and respects the `iac.proto` hard invariant (no `structpb`/`Any`; free-form maps cross as JSON `bytes`). The host (`iac_module.go` `Init()`) JSON-encodes `m.config` and calls `Configure` before wrapping the client in `grpcIaCStateStore`. The plugin's `Configure` handler decodes the config, constructs the real SDK-backed store, and sets it (the `setStateStore` path the azure plugin already has). `workflow-plugin-azure` is retrofitted in the same plan (PR 2) so the gap is closed for Azure too, not just B/C/D.

**One config per backend-name per plugin process** is an accepted limitation: the Phase-A registry maps a backend *name* to *one* gRPC client, and the State RPCs carry no backend-instance identity, so two `iac.state` modules using the same backend name share one plugin-side store (last `Configure` wins). This is inherent to the Phase-A registry shape, not introduced here; a workflow config almost always has one `iac.state` module. Documented as a known limitation.

Alternatives rejected:
- **Config in every State request** — bloats every `GetState`/`SaveState` call with static config; the benchmark (decisions in `2026-05-14-iac-state-backend-benchmark.md`) already showed per-call payload size is the cost driver. Rejected.
- **A separate `IaCStateBackendConfig` service** — a second service for one RPC; `Configure` belongs on the same service the config configures. Rejected.
- **Keep deferring it ("follow-up PR")** — Phase A's choice. Rejected: B/C/D's `spaces` clean-break converts the deferral into a user-visible regression; `decisions/0035` already established that assumed-seam gaps get fixed in-plan, not filed.

## Consequences

- **B/C/D ships functional** — plugin-served `s3`/`gcs`/`spaces` actually work; the `spaces` clean-break is a true extraction, not a regression.
- **Azure is retroactively fixed** — PR 2 retrofits `workflow-plugin-azure`; `azure_blob` becomes functional end-to-end (it was not, post-Phase-A).
- **`iac.proto` is touched again** — one additive RPC + two messages on the already-merged contract. Additive; strict-contract invariants unaffected; plugins regenerate.
- **Plan grows** — B/C/D gains PR 1 (host) + PR 2 (azure retrofit) and each plugin's state-backend task also implements `Configure`. ~24 → ~29 tasks, 8 → 10 PRs.
- **Cost to undo** — reverting means plugin backends go back to non-functional; not a real rollback target. The forward path is the only sane one.
- **Known limitation recorded** — one config per backend-name per plugin process; revisit only if a real multi-`iac.state`-module config appears.
