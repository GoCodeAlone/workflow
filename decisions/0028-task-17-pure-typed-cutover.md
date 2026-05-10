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

## Per-site dispatch UX

The pure-typed code-shape mandate is uniform across all 5 sites:
`provider.(*typedIaCAdapter)` is the only valid input shape; no
`interfaces.X` fallback. **Severity** of the non-typed-input branch,
however, is per-site UX based on iteration semantics. A halt-on-bad-
provider response at an iteration site would lose visibility into the
other providers' results — a regression vs the legacy
iterate-and-skip semantics. The 5 sites split as follows:

| Site | Severity | Per-site rationale |
|---|---|---|
| `cmd/wfctl/infra_cleanup.go:104` (Enumerator) | **Hard-error** (return + collect) | Single-shot operation against a bounded provider list. Surfacing fixture-leak loudly is correct; halting one bad provider while continuing others is captured by the existing `totalErrs = append(...)` + `continue` pattern. Operator sees the problem, batch result still distinguishes successes from failures. |
| `cmd/wfctl/infra_apply_refresh.go:69` (DriftConfigDetector) | **Hard-error** (return) | Single-shot drift detection feeding the prune decision. Apply-refresh prunes ghosts; a fixture-leak that silently returned "no drift" would suppress prunes operators expected — same anti-pattern the dispatch is designed to prevent. Hard-error is operationally correct. |
| `cmd/wfctl/infra_status_drift.go:107` (DriftConfigDetector) | **Soft-skip** (warn + return false) | Per-provider iteration in `driftGroup`. Halting on one bad provider would suppress drift visibility for every other provider in the loop — strictly worse for operators triaging multi-provider drift state. The warning log is the auditable signal; "no drift reported" for a single provider is the graceful-degradation behavior. |
| `cmd/wfctl/infra_align_rules.go:782` (Validator, R-A10) | **Silent-skip** (`continue`) | R-A10's contract is "treat unimplemented validator as not-applicable" (rule iterates over align providers and only emits findings from those that opt in). A non-typed provider that reaches this loop would not produce diagnostics under the legacy `interfaces.ProviderValidator` type-assert either; the silent-skip preserves that contract. Hard-error here would abort the align batch on a fixture-leak, masking other rules' findings. |
| `cmd/wfctl/infra_bootstrap.go:348` (CredentialRevoker) | **Soft-skip** (warn + skip revocation) | Bootstrap completes secret minting; the typed-adapter capability gate decides whether old credentials get auto-revoked. A non-typed provider reaching this site means revocation is unavailable; the bootstrap itself remains correct, the operator sees a stderr warning advising manual revocation. Halting bootstrap on revoker absence would block secret rotation entirely on a fixture-leak — a strictly worse failure mode. |

### Canonical rule

> Pure typed-pb dispatch at all sites; non-typed input rejection
> severity is per-site UX based on iteration semantics. New dispatch
> sites default to hard-error unless graceful-degradation is
> operationally required.

The "graceful-degradation is operationally required" bar is met when
the dispatch site iterates over a heterogeneous provider list (or
sub-rules) where halting on one bad input would lose visibility into
the others' results, **and** the soft-skip path emits an auditable
warn-log so operators can recognize + act on the gap. Both conditions
must hold; soft-skip without auditability collapses back into the
"silent fallback" failure mode the cutover exists to prevent.

### Why this is not the rejected silent-fallback shape

The original Round-1 rejection (commit 63e1cdf8 → CHANGES REQUESTED)
was at code-shape: `interfaces.X` fallback dispatch was invisible to
operators (no log line), invoked the legacy proxy's behavior, and
masked the loader-gate's pre-flight contract. The current per-site
soft-skip pattern differs in three observable ways:

1. **The fallback path no longer exists.** All 5 sites first do
   `adapter, ok := p.(*typedIaCAdapter)`; the only branch is "use
   typed-pb" or "skip with warn-log". There is no second dispatch
   shape that takes the call.
2. **Soft-skip is auditable.** Each soft-skip site emits a stderr
   warn-log identifying the provider + the reason (`provider %T is
   not a typed IaC adapter — re-load via discoverAndLoadIaCProvider`
   or analogous). Operators see fixture leaks and loader-gate
   regressions in the same shape as any other operator-facing
   diagnostic.
3. **Continue-semantics, not equivalent-behavior.** The legacy
   fallback returned a real result via the interfaces.X path —
   indistinguishable from a typed-pb success at the call site. The
   soft-skip path returns the no-op result for the operation
   (false / nil-skip / empty diagnostics), which is observably
   distinct: status-drift reports no-drift, R-A10 emits no findings,
   bootstrap leaves old credentials in place. Operators reading the
   output see the gap.

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
