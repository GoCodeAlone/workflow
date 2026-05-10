# ADR 0028 — Task 17 wfctl-side dispatch is pure typed-pb (no interfaces.X fallback)

**Status:** Accepted (2026-05-10)
**Authors:** implementer-2, spec-reviewer, team-lead
**Related:** ADR 0024 (strict-contracts force-cutover), ADR 0026 (typed-client adapter, no marshalling proxy), Task 17 (`docs/plans/2026-05-10-strict-contracts-force-cutover.md`)

## Context

Plan §Task 17 directs converting 5 wfctl-side dispatch sites from
`if x, ok := provider.(interfaces.X); ok { x.Method(...) }` to typed
pb.IaC* RPC calls via the typed-client accessors added on
`*typedIaCAdapter` (Task 30 / PR #605). The 5 sites:

- `cmd/wfctl/infra_cleanup.go` — interfaces.Enumerator → pb.IaCProviderEnumeratorClient.EnumerateByTag
- `cmd/wfctl/infra_apply_refresh.go` — interfaces.DriftConfigDetector → pb.IaCProviderDriftConfigDetectorClient.DetectDriftConfig
- `cmd/wfctl/infra_status_drift.go` — interfaces.DriftConfigDetector → same
- `cmd/wfctl/infra_bootstrap.go` — interfaces.ProviderCredentialRevoker capability discovery via typedIaCAdapter.CredentialRevoker()
- `cmd/wfctl/infra_align_rules.go` — interfaces.ProviderValidator → pb.IaCProviderValidatorClient.ValidatePlan

The implementer's first push (commit 63e1cdf8) used a **typed-then-fallback**
pattern: prefer typed pb when `provider.(*typedIaCAdapter)` succeeds, fall
back to the legacy interfaces.X type-assert otherwise. Rationale: avoid
~10 test-fixture rewrites; preserve the interfaces.X integration point
for non-wfctl consumers; typedIaCAdapter satisfies the interfaces.X anyway
so runtime behavior was equivalent.

Spec-reviewer + team-lead rejected this in favor of a **pure-typed**
cutover (no interfaces.X fallback in wfctl dispatch). This ADR records
the decision and the migration approach.

## Decision

The 5 wfctl dispatch sites use **only** the typed pb path:

```go
adapter, ok := provider.(*typedIaCAdapter)
if !ok {
    return fmt.Errorf("...: provider %T is not a typed IaC adapter — re-load via discoverAndLoadIaCProvider", provider)
}
if cli := adapter.X(); cli != nil {
    resp, err := cli.TypedRPC(ctx, &pb.XRequest{...})
    // ...
} else {
    // optional service not registered — skip per dispatch-specific semantics
}
```

The `interfaces.X` type-assertion is removed from every wfctl call
site. Test fixtures that previously injected fake `interfaces.IaCProvider`
implementations are migrated to construct a real `*typedIaCAdapter`
backed by an in-process bufconn-served pb.IaC* server (precedent:
PR #603's `iac_e2e_test.go`).

The `interfaces.IaCProvider` + sub-interface definitions remain in
`interfaces/` for engine-side consumers (per ADR 0024); only wfctl's
dispatch sites are pure typed.

## Rationale

The user mandate `feedback_force_strict_contracts_no_compat` is framed
at **code shape**, not runtime exercise. Both paths "working" was the
exact rejected stance for the legacy additive-strict-contracts plan
(2026-04-26); the same logic applies to wfctl-side dual-path dispatch.

Concrete failure modes the typed-then-fallback pattern preserves:

1. **Loader-gate weakening.** Future PR loosens `discoverAndLoadIaCProvider`
   (e.g., adds a back-compat path for a v1 plugin); non-typed providers
   reach dispatch sites; fallback fires silently against them; behavior
   reverts to v0.27.x without surfacing the regression. Pure typed +
   hard-fail surfaces immediately.

2. **Test-fixture DI leak.** A test that injects a fake
   `interfaces.IaCProvider` against a wfctl dispatch site is
   accidentally exercising the fallback branch — masking production-shape
   bugs. Pure typed + bufconn fixtures means the test path uses the
   same dispatch shape as production.

3. **Future contributor cargo-culting.** A new dispatch site copy-paste
   from one of the 5 carries the dual-path pattern forward. The strict-
   contract surface accretes. Pure typed at the existing sites prevents
   the pattern from being in the codebase.

4. **Reviewer cognitive load.** Every PR touching one of the 5 carries
   "is the typed branch right? is the fallback right? does the order
   matter?" Pure typed collapses this to one path.

## Consequences

### Positive

- wfctl dispatch shape is unambiguous: typed pb at every IaC call site.
- Loader-gate regressions surface immediately as typed errors at
  dispatch.
- Test-fixture leakage is impossible (no fallback branch to fire).
- `interfaces.X` definitions stay in `interfaces/` for engine-side
  consumers without bleeding into wfctl as a viable dispatch alternative.

### Negative

- ~10 test fixtures must migrate from `fake interfaces.IaCProvider`
  to bufconn-backed `*typedIaCAdapter`. Per-test cost varies; the
  pattern is well-established by PR #603's `iac_e2e_test.go` and
  PR #609's `discover_typed_loader_test.go`.
- Test fixtures that exercise `interfaces.ErrProviderMethodUnimplemented`
  semantics need to surface as gRPC `codes.Unimplemented` through
  bufconn (the typed adapter translates back to the sentinel — the
  test invariant is preserved at the dispatch site, not the source
  shape).
- Out-of-org consumers that wrote a custom `interfaces.IaCProvider`
  implementation expecting wfctl to type-assert against it are blocked.
  This is the strict-cutover trade explicitly accepted by the user
  mandate (per ADR 0024).

## Alternatives rejected

### Typed-then-fallback (the original PR #618 commit 63e1cdf8 implementation)

Discussed above. Rejected because code-shape clarity outweighs
runtime equivalence; failure modes 1–4 above are real.

### interfaces-only (no typed-pb at wfctl sites)

Status quo before Task 17. Doesn't close the bug class the strict-
contracts cutover targets — every IaC dispatch goes through Go-
interface indirection rather than typed pb.

### Hybrid (typed-pb at wfctl, interfaces.X retained for adapter satisfiability)

Considered. Adapter still satisfies interfaces.X (compile-time guards
in iac_typed_adapter.go), so this option is the de-facto chosen path.
The ADR clarifies that the **adapter satisfying interfaces.X is fine**
(engine consumers want it); the **dispatch sites using interfaces.X
type-assertion** is what's rejected.

## Migration

### Test fixtures

Migrate from `fake interfaces.IaCProvider` to bufconn-backed real
adapter. Per-fixture pattern:

```go
// Before:
fake := &fakeIaCProvider{} // implements interfaces.IaCProvider
adapter = fake

// After:
lis := bufconn.Listen(1024 * 1024)
srv := grpc.NewServer()
pb.RegisterIaCProviderRequiredServer(srv, &stubRequiredServer{...})
pb.RegisterIaCProviderEnumeratorServer(srv, &stubEnumeratorServer{...})
go srv.Serve(lis)
conn, _ := grpc.NewClient(
    "passthrough://bufnet",
    grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
adapter := newTypedIaCAdapter(conn, map[string]bool{
    iacServiceRequired:   true,
    iacServiceEnumerator: true,
    // ... whichever optional services this test needs
})
```

Affected tests (10 files, scope-locked enumeration):
- `cmd/wfctl/infra_cleanup_test.go`
- `cmd/wfctl/infra_status_drift_test.go`
- `cmd/wfctl/infra_apply_refresh_test.go`
- `cmd/wfctl/infra_bootstrap_force_rotate_test.go`
- `cmd/wfctl/infra_align_rules_test.go`
- `cmd/wfctl/infra_strict_mode_test.go`
- `cmd/wfctl/infra_rotate_and_prune_test.go`
- `cmd/wfctl/infra_audit_keys_test.go`
- `cmd/wfctl/dryrun_test.go`
- `cmd/wfctl/infra_provider_dispatch_test.go`

### Strict-mode invariant translation

Tests like `TestInfraAuditKeysCmd_ProviderMissingBridge_FailsLoud`
that depend on `interfaces.ErrProviderMethodUnimplemented` semantics
migrate to: configure the bufconn server to NOT register the optional
service (so the typed accessor returns nil) OR configure the registered
service to return `status.Error(codes.Unimplemented, ...)` from the
RPC. The dispatch site translates these to `interfaces.ErrProviderMethodUnimplemented`
via the existing `translateRPCErr` helper in `iac_typed_adapter.go`,
preserving the operator-visible error shape the test asserts.

## Precedent

- ADR 0024 (strict-contracts force-cutover): code-shape over runtime
  exercise.
- PR #603 (`iac_e2e_test.go`): bufconn + RegisterAllIaCProviderServices
  pattern for in-process typed-RPC tests.
- PR #609 (`discover_typed_loader_test.go`): boundary-test pattern
  using stubIaCAdapter to exercise the typed loader without subprocess.

## Open questions

None at landing. The fixture migration cost is concrete + bounded
(10 files, established pattern). If specific fixtures hit a
"bufconn can't reproduce X" blocker, surface to team-lead per Task 17
PR's escalation path.
