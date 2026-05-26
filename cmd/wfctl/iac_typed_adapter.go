package main

// iac_typed_adapter.go — Task 30 of the strict-contracts force-cutover plan
// (docs/plans/2026-05-10-strict-contracts-force-cutover.md, rev5).
//
// Adapter that wraps the typed pb.IaC* gRPC clients (Task 3) and satisfies
// the existing interfaces.IaCProvider Go interface. Engine consumers
// (module/infra_module.go, iac/wfctlhelpers/apply.go, etc.) keep calling
// interfaces.IaCProvider methods unchanged; the adapter translates each
// call to a typed RPC on the underlying pb.IaCProviderRequiredClient (or
// the matching optional client). No string dispatch, no map[string]any
// crossing the wire — everything goes through generated typed messages.
// Free-form provider-config / output payloads carry as JSON bytes (per
// proto §config_json / outputs_json) so the engine boundary stays
// strongly typed without ossifying provider-specific shapes.
//
// Per ADR-0026 (Task 14): this is NOT a hand-written marshalling proxy
// of the kind the legacy remoteIaCProvider was. Each Go-interface method
// maps 1:1 to a typed RPC; optional sub-interfaces (Enumerator,
// EnumeratorAll, ProviderValidator, etc.) are always satisfied at the Go
// type level (so v0.27.1 behaviour is preserved — type-assert sites
// continue to compile) but return interfaces.ErrProviderMethodUnimplemented
// at call time when the underlying optional service was never registered
// by the plugin. Callers use errors.Is to skip those providers, matching
// the legacy proxy semantics.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// Fully-qualified gRPC service names — the wfctl loader gates optional
// client construction on whether the plugin's ContractRegistry advertises
// each name (Task 5). Constants are declared at package scope so callers
// (e.g. the loader) can reference the same strings without duplication.
const (
	iacServiceRequired          = "workflow.plugin.external.iac.IaCProviderRequired"
	iacServiceEnumerator        = "workflow.plugin.external.iac.IaCProviderEnumerator"
	iacServiceDriftDetector     = "workflow.plugin.external.iac.IaCProviderDriftDetector"
	iacServiceCredentialRevoker = "workflow.plugin.external.iac.IaCProviderCredentialRevoker"
	iacServiceMigrationRepairer = "workflow.plugin.external.iac.IaCProviderMigrationRepairer"
	iacServiceValidator         = "workflow.plugin.external.iac.IaCProviderValidator"
	iacServiceDriftConfigDetect = "workflow.plugin.external.iac.IaCProviderDriftConfigDetector"
	iacServiceLogCapture        = "workflow.plugin.external.iac.IaCProviderLogCapture"
	iacServiceFinalizer         = "workflow.plugin.external.iac.IaCProviderFinalizer"
	iacServiceResourceDriver    = "workflow.plugin.external.iac.ResourceDriver"
	iacServiceRequirementMapper = "workflow.plugin.external.iac.IaCProviderRequirementMapper"
)

// typedIaCAdapter implements interfaces.IaCProvider on top of the typed
// pb.IaC* gRPC clients. Optional clients are nil when the plugin did not
// register the corresponding service — call paths gated on those clients
// return interfaces.ErrProviderMethodUnimplemented.
//
// Capability cache (cachedCaps): the plugin's CapabilitiesResponse is
// fetched lazily on the first call to Capabilities() / SupportedCanonicalKeys()
// / ComputePlanVersion() and reused for the adapter's lifetime. Capabilities
// are advertised once at plugin startup and don't change during a wfctl
// invocation; caching lets per-call accessors (notably the apply-time
// dispatch decision) avoid an RPC round-trip per access. Per ADR-0029.
type typedIaCAdapter struct {
	conn *grpc.ClientConn

	required     pb.IaCProviderRequiredClient
	enumerator   pb.IaCProviderEnumeratorClient
	drift        pb.IaCProviderDriftDetectorClient
	revoker      pb.IaCProviderCredentialRevokerClient
	repairer     pb.IaCProviderMigrationRepairerClient
	validator    pb.IaCProviderValidatorClient
	driftCfg     pb.IaCProviderDriftConfigDetectorClient
	logCapture   pb.IaCProviderLogCaptureClient
	finalizer    pb.IaCProviderFinalizerClient
	resourceDriv pb.ResourceDriverClient
	reqMapper    pb.IaCProviderRequirementMapperClient

	// cachedCaps memoizes the plugin's CapabilitiesResponse. Access via
	// fetchCapabilities — never read this field directly.
	cachedCaps *pb.CapabilitiesResponse
	capsErr    error
	capsFetch  bool // true once first fetch attempt completed (success OR error)
}

// newTypedIaCAdapter builds an adapter from a live gRPC connection plus a
// set of fully-qualified service names the plugin advertised through its
// ContractRegistry RPC (Task 5). The required client is always
// constructed; optional clients are constructed only when the matching
// service name appears in `registered`. Passing a nil/empty `registered`
// is valid — the adapter exposes only the required surface in that case.
func newTypedIaCAdapter(conn *grpc.ClientConn, registered map[string]bool) *typedIaCAdapter {
	a := &typedIaCAdapter{
		conn:     conn,
		required: pb.NewIaCProviderRequiredClient(conn),
	}
	if registered[iacServiceEnumerator] {
		a.enumerator = pb.NewIaCProviderEnumeratorClient(conn)
	}
	if registered[iacServiceDriftDetector] {
		a.drift = pb.NewIaCProviderDriftDetectorClient(conn)
	}
	if registered[iacServiceCredentialRevoker] {
		a.revoker = pb.NewIaCProviderCredentialRevokerClient(conn)
	}
	if registered[iacServiceMigrationRepairer] {
		a.repairer = pb.NewIaCProviderMigrationRepairerClient(conn)
	}
	if registered[iacServiceValidator] {
		a.validator = pb.NewIaCProviderValidatorClient(conn)
	}
	if registered[iacServiceDriftConfigDetect] {
		a.driftCfg = pb.NewIaCProviderDriftConfigDetectorClient(conn)
	}
	if registered[iacServiceLogCapture] {
		a.logCapture = pb.NewIaCProviderLogCaptureClient(conn)
	}
	if registered[iacServiceFinalizer] {
		a.finalizer = pb.NewIaCProviderFinalizerClient(conn)
	}
	if registered[iacServiceResourceDriver] {
		a.resourceDriv = pb.NewResourceDriverClient(conn)
	}
	if registered[iacServiceRequirementMapper] {
		a.reqMapper = pb.NewIaCProviderRequirementMapperClient(conn)
	}
	return a
}

// ─── Typed-client accessors (Task 17 capability discovery) ──────────────────
//
// Each accessor returns the underlying typed pb client for the named
// optional service, or nil if the plugin's ContractRegistry didn't
// advertise it. wfctl dispatch sites that previously did
// `if x, ok := provider.(interfaces.X); ok { x.Method(...) }` now
// type-assert to *typedIaCAdapter and use these accessors. The
// non-typed branch is per-site UX (ADR-0028 §Per-site dispatch UX) —
// hard-error at single-shot sites, soft-skip at iteration sites, e.g.:
//
//	// Hard-error (single-shot — cleanup, apply-refresh):
//	adapter, ok := provider.(*typedIaCAdapter)
//	if !ok {
//	    return fmt.Errorf("provider %T is not a typed IaC adapter", provider)
//	}
//	if cli := adapter.Enumerator(); cli != nil {
//	    resp, err := cli.EnumerateByTag(ctx, &pb.EnumerateByTagRequest{Tag: t})
//	    // ...
//	}
//
//	// Soft-skip (iteration — status-drift, R-A10, bootstrap revoker):
//	adapter, ok := provider.(*typedIaCAdapter)
//	if !ok {
//	    fmt.Printf("WARNING: provider %q is not a typed adapter\n", name)
//	    continue // or return false / nil-skip per site
//	}
//
// Either way the legacy interfaces.X fallback is gone. The interfaces.X
// definitions remain in `interfaces/` for engine-side / module-factory
// consumers — wfctl call sites are pure typed-pb (no string dispatch,
// no Go-interface indirection at the wfctl boundary).

// RequiredClient returns the typed pb.IaCProviderRequiredClient. Always
// non-nil (the loader rejects plugins that don't register the required
// service via the AssertIaCPluginAdvertisesRequiredService gate in
// PR #610). Exposed for symmetry with the optional accessors and for
// dispatch sites that want to call required RPCs directly without going
// through the interfaces.IaCProvider Go-interface methods.
func (a *typedIaCAdapter) RequiredClient() pb.IaCProviderRequiredClient {
	return a.required
}

// Enumerator returns the typed pb.IaCProviderEnumeratorClient or nil
// when the plugin did not register IaCProviderEnumerator. Used by
// `wfctl infra cleanup --tag` (EnumerateByTag) and
// `wfctl infra audit-keys` / `wfctl infra prune` (EnumerateAll).
func (a *typedIaCAdapter) Enumerator() pb.IaCProviderEnumeratorClient {
	return a.enumerator
}

// DriftDetector returns the typed pb.IaCProviderDriftDetectorClient or
// nil when the plugin did not register IaCProviderDriftDetector.
func (a *typedIaCAdapter) DriftDetector() pb.IaCProviderDriftDetectorClient {
	return a.drift
}

// DriftConfigDetector returns the typed
// pb.IaCProviderDriftConfigDetectorClient or nil when the plugin did
// not register IaCProviderDriftConfigDetector. Used by
// `wfctl infra status drift` and `wfctl infra apply --refresh`
// to short-circuit between DetectDriftWithSpecs (config-aware) and
// the required IaCProvider.DetectDrift (existence-only) per ADR 0016.
func (a *typedIaCAdapter) DriftConfigDetector() pb.IaCProviderDriftConfigDetectorClient {
	return a.driftCfg
}

// LogCapture returns the typed pb.IaCProviderLogCaptureClient or nil
// when the plugin did not register IaCProviderLogCapture. Used by
// `wfctl logs capture`.
func (a *typedIaCAdapter) LogCapture() pb.IaCProviderLogCaptureClient {
	return a.logCapture
}

// CredentialRevoker returns the typed
// pb.IaCProviderCredentialRevokerClient or nil when the plugin did not
// register IaCProviderCredentialRevoker. Used by
// `wfctl infra bootstrap --force-rotate` to invalidate the OLD
// provider credential after the new one is minted (ADR 0012).
func (a *typedIaCAdapter) CredentialRevoker() pb.IaCProviderCredentialRevokerClient {
	return a.revoker
}

// MigrationRepairer returns the typed
// pb.IaCProviderMigrationRepairerClient or nil when the plugin did not
// register IaCProviderMigrationRepairer.
func (a *typedIaCAdapter) MigrationRepairer() pb.IaCProviderMigrationRepairerClient {
	return a.repairer
}

// Validator returns the typed pb.IaCProviderValidatorClient or nil
// when the plugin did not register IaCProviderValidator. Used by R-A10
// (`wfctl infra align --strict`) to surface provider-side cross-
// resource constraint diagnostics at plan time.
func (a *typedIaCAdapter) Validator() pb.IaCProviderValidatorClient {
	return a.validator
}

// ResourceDriverClient returns the typed pb.ResourceDriverClient or
// nil when the plugin did not register ResourceDriver. Each per-type
// dispatch carries the resource_type on every RPC, matching the DO
// plugin's 14-driver type-routing pattern in Task 11.
func (a *typedIaCAdapter) ResourceDriverClient() pb.ResourceDriverClient {
	return a.resourceDriv
}

// Finalizer returns the typed pb.IaCProviderFinalizerClient or nil when
// the plugin did not register IaCProviderFinalizer. Used by the v2 apply
// path's statePersistenceHooks helper (cmd/wfctl/infra_apply.go) to gate
// the ApplyPlanHooks.OnPlanComplete wiring on service-presence — a nil
// return means no FinalizeApply RPC is invoked. Per ADR 0024 the absence
// of the registration is the negative signal (no compat shim, no
// NotSupported flag). Per workflow#695 Phase 2.5.
func (a *typedIaCAdapter) Finalizer() pb.IaCProviderFinalizerClient {
	return a.finalizer
}

// CapabilitiesWithContext returns CapabilitiesResponse with caller-supplied
// context. Bypasses fetchCapabilities's adapter-lifetime cache — used by
// the load-time workflow#699 gate which must not poison the cache on
// transient failure (cycle-3 I-NEW-6).
func (a *typedIaCAdapter) CapabilitiesWithContext(ctx context.Context) (*pb.CapabilitiesResponse, error) {
	return a.required.Capabilities(ctx, &pb.CapabilitiesRequest{})
}

// translateRPCErr converts a gRPC Unimplemented status (the wire signal a
// plugin emits when an optional method is not supported) into the stable
// interfaces.ErrProviderMethodUnimplemented sentinel callers iterate on
// via errors.Is. Other errors pass through unchanged so the underlying
// gRPC status code remains observable to callers that wrap typed
// retry / classification logic around the call.
//
// Wraps with %w/%w (not %w/%s) so callers can recover BOTH the
// interfaces sentinel via errors.Is AND the underlying gRPC status via
// status.FromError walking the unwrap chain. Without the second %w the
// status code/details get demoted to a flat string and consumers that
// classify by code (rate-limit retry, transient backoff, etc.) lose the
// signal.
func translateRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.Unimplemented {
		return fmt.Errorf("%w: %w", interfaces.ErrProviderMethodUnimplemented, err)
	}
	return err
}

// unimplementedOptional builds the sentinel error returned when the plugin
// did not register the optional service backing this method. Callers use
// errors.Is(err, interfaces.ErrProviderMethodUnimplemented) to skip the
// provider — matching the legacy remoteIaCProvider semantics that
// dispatch sites (cmd/wfctl/infra_audit_keys.go, infra_cleanup.go,
// infra_prune.go) already handle.
func unimplementedOptional(serviceName string) error {
	return fmt.Errorf("%w: optional service %q not registered by plugin",
		interfaces.ErrProviderMethodUnimplemented, serviceName)
}

// ─── Required IaCProvider methods ───────────────────────────────────────────

// Name and Version below intentionally swallow RPC errors and return ""
// — the Go interface signatures `Name() string` and `Version() string`
// permit no error return, so any transport failure is indistinguishable
// from an empty plugin response. We log at the standard logger so
// operators have a trail when troubleshooting "why is my provider
// nameless"; the contract itself can't change without a wfctlhelpers
// signature break (out of Task 30 scope).

func (a *typedIaCAdapter) Name() string {
	resp, err := a.required.Name(context.Background(), &pb.NameRequest{})
	if err != nil {
		log.Printf("typed adapter: Name() RPC failed: %v", err)
		return ""
	}
	return resp.GetName()
}

func (a *typedIaCAdapter) Version() string {
	resp, err := a.required.Version(context.Background(), &pb.VersionRequest{})
	if err != nil {
		log.Printf("typed adapter: Version() RPC failed: %v", err)
		return ""
	}
	return resp.GetVersion()
}

func (a *typedIaCAdapter) Initialize(ctx context.Context, config map[string]any) error {
	cfgJSON, err := marshalJSONMap(config)
	if err != nil {
		return fmt.Errorf("typed adapter: marshal Initialize config: %w", err)
	}
	_, err = a.required.Initialize(ctx, &pb.InitializeRequest{ConfigJson: cfgJSON})
	return err
}

// fetchCapabilities returns the plugin's CapabilitiesResponse, caching the
// first result for the adapter's lifetime. RPC errors are also cached so
// repeated accesses don't repeatedly fail against an unreachable plugin.
// Capabilities are advertised at plugin startup and don't change during
// a wfctl invocation; caching is correct + cheap.
func (a *typedIaCAdapter) fetchCapabilities() (*pb.CapabilitiesResponse, error) {
	if a.capsFetch {
		return a.cachedCaps, a.capsErr
	}
	a.capsFetch = true
	resp, err := a.required.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
	if err != nil {
		a.capsErr = err
		return nil, err
	}
	a.cachedCaps = resp
	return resp, nil
}

func (a *typedIaCAdapter) Capabilities() []interfaces.IaCCapabilityDeclaration {
	resp, err := a.fetchCapabilities()
	if err != nil {
		return nil
	}
	out := make([]interfaces.IaCCapabilityDeclaration, 0, len(resp.GetCapabilities()))
	for _, c := range resp.GetCapabilities() {
		out = append(out, interfaces.IaCCapabilityDeclaration{
			ResourceType: c.GetResourceType(),
			Tier:         int(c.GetTier()),
			Operations:   append([]string(nil), c.GetOperations()...),
		})
	}
	return out
}

func (a *typedIaCAdapter) Plan(ctx context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	pbDesired, err := specsToPB(desired)
	if err != nil {
		return nil, fmt.Errorf("typed adapter: encode Plan desired: %w", err)
	}
	pbCurrent, err := statesToPB(current)
	if err != nil {
		return nil, fmt.Errorf("typed adapter: encode Plan current: %w", err)
	}
	resp, err := a.required.Plan(ctx, &pb.PlanRequest{Desired: pbDesired, Current: pbCurrent})
	if err != nil {
		return nil, err
	}
	return planFromPB(resp.GetPlan())
}

func (a *typedIaCAdapter) Destroy(ctx context.Context, resources []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	resp, err := a.required.Destroy(ctx, &pb.DestroyRequest{Refs: refsToPB(resources)})
	if err != nil {
		return nil, err
	}
	return destroyResultFromPB(resp.GetResult()), nil
}

func (a *typedIaCAdapter) Status(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	resp, err := a.required.Status(ctx, &pb.StatusRequest{Refs: refsToPB(resources)})
	if err != nil {
		return nil, err
	}
	return statusesFromPB(resp.GetStatuses())
}

func (a *typedIaCAdapter) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	if a.drift == nil {
		return nil, unimplementedOptional(iacServiceDriftDetector)
	}
	resp, err := a.drift.DetectDrift(ctx, &pb.DetectDriftRequest{Refs: refsToPB(resources)})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	return driftsFromPB(resp.GetDrifts())
}

func (a *typedIaCAdapter) Import(ctx context.Context, cloudID string, resourceType string) (*interfaces.ResourceState, error) {
	resp, err := a.required.Import(ctx, &pb.ImportRequest{ProviderId: cloudID, ResourceType: resourceType})
	if err != nil {
		return nil, err
	}
	return stateFromPB(resp.GetState())
}

func (a *typedIaCAdapter) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	resp, err := a.required.ResolveSizing(context.Background(), &pb.ResolveSizingRequest{
		ResourceType: resourceType,
		Size:         string(size),
		Hints:        hintsToPB(hints),
	})
	if err != nil {
		return nil, err
	}
	return sizingFromPB(resp.GetSizing())
}

func (a *typedIaCAdapter) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	if a.resourceDriv == nil {
		return nil, unimplementedOptional(iacServiceResourceDriver)
	}
	return &typedResourceDriver{client: a.resourceDriv, resourceType: resourceType}, nil
}

// SupportedCanonicalKeys returns the canonical IaC config keys this
// plugin supports. Reads from the cached CapabilitiesResponse:
//   - non-empty CapabilitiesResponse.canonical_keys → use those (provider
//     declared a strict subset, e.g. DO plugin removing loadbalancer/vpc/k8s)
//   - empty list OR Capabilities RPC failure → fall back to
//     interfaces.CanonicalKeys() wfctl-side default
//
// Per ADR-0029. Closes the regression where the typed cutover lost the
// per-provider override path that legacy remoteIaCProvider routed via
// InvokeService("SupportedCanonicalKeys", ...).
func (a *typedIaCAdapter) SupportedCanonicalKeys() []string {
	resp, err := a.fetchCapabilities()
	if err == nil && resp != nil {
		if keys := resp.GetCanonicalKeys(); len(keys) > 0 {
			return append([]string(nil), keys...)
		}
	}
	return interfaces.CanonicalKeys()
}

func (a *typedIaCAdapter) BootstrapStateBackend(ctx context.Context, cfg map[string]any) (*interfaces.BootstrapResult, error) {
	cfgJSON, err := marshalJSONMap(cfg)
	if err != nil {
		return nil, fmt.Errorf("typed adapter: marshal BootstrapStateBackend cfg: %w", err)
	}
	resp, err := a.required.BootstrapStateBackend(ctx, &pb.BootstrapStateBackendRequest{ConfigJson: cfgJSON})
	if err != nil {
		return nil, err
	}
	r := resp.GetResult()
	if r == nil {
		return nil, nil
	}
	// Defensive copy of EnvVars: r.GetEnvVars() returns the proto's
	// underlying map; handing the same reference to callers exposes
	// proto-internal state to mutation. copyStringMap already nil-safe.
	return &interfaces.BootstrapResult{
		Bucket:   r.GetBucket(),
		Region:   r.GetRegion(),
		Endpoint: r.GetEndpoint(),
		EnvVars:  copyStringMap(r.GetEnvVars()),
	}, nil
}

func (a *typedIaCAdapter) Close() error {
	if a.conn == nil {
		return nil
	}
	return a.conn.Close()
}

// ─── Optional sub-interface methods ─────────────────────────────────────────
//
// Each method below is declared on *typedIaCAdapter so type-assertion
// `p.(interfaces.X)` always succeeds (matching the legacy proxy
// behaviour). When the underlying optional client was never wired (plugin
// did not register the service), the method returns
// interfaces.ErrProviderMethodUnimplemented so callers can errors.Is and
// skip — preserving the v0.27.1 iterate-and-skip semantics.

// EnumerateAll satisfies interfaces.EnumeratorAll.
func (a *typedIaCAdapter) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	if a.enumerator == nil {
		return nil, unimplementedOptional(iacServiceEnumerator)
	}
	resp, err := a.enumerator.EnumerateAll(ctx, &pb.EnumerateAllRequest{ResourceType: resourceType})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	out := make([]*interfaces.ResourceOutput, 0, len(resp.GetOutputs()))
	for _, o := range resp.GetOutputs() {
		ro, err := outputFromPB(o)
		if err != nil {
			return nil, fmt.Errorf("typed adapter: decode EnumerateAll output: %w", err)
		}
		out = append(out, ro)
	}
	return out, nil
}

// EnumerateByTag satisfies interfaces.Enumerator.
func (a *typedIaCAdapter) EnumerateByTag(ctx context.Context, tag string) ([]interfaces.ResourceRef, error) {
	if a.enumerator == nil {
		return nil, unimplementedOptional(iacServiceEnumerator)
	}
	resp, err := a.enumerator.EnumerateByTag(ctx, &pb.EnumerateByTagRequest{Tag: tag})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	return refsFromPB(resp.GetRefs()), nil
}

// DetectDriftWithSpecs satisfies interfaces.DriftConfigDetector. Routed
// through the typed IaCProviderDriftConfigDetector service when the
// plugin advertises it.
func (a *typedIaCAdapter) DetectDriftWithSpecs(ctx context.Context, resources []interfaces.ResourceRef, specs map[string]interfaces.ResourceSpec) ([]interfaces.DriftResult, error) {
	if a.driftCfg == nil {
		return nil, unimplementedOptional(iacServiceDriftConfigDetect)
	}
	pbSpecs := make(map[string]*pb.ResourceSpec, len(specs))
	for k, s := range specs {
		ps, err := specToPB(s)
		if err != nil {
			return nil, fmt.Errorf("typed adapter: encode DetectDriftWithSpecs specs[%s]: %w", k, err)
		}
		pbSpecs[k] = ps
	}
	resp, err := a.driftCfg.DetectDriftConfig(ctx, &pb.DetectDriftConfigRequest{
		Refs:  refsToPB(resources),
		Specs: pbSpecs,
	})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	return driftsFromPB(resp.GetDrifts())
}

// ValidatePlan satisfies interfaces.ProviderValidator. Note signature
// difference from the proto: the Go interface returns []PlanDiagnostic
// only (no error); we therefore swallow gRPC errors and return nil
// diagnostics on RPC failure — matching the existing remoteIaCProvider
// behaviour and the legacy semantics consumers depend on.
func (a *typedIaCAdapter) ValidatePlan(plan *interfaces.IaCPlan) []interfaces.PlanDiagnostic {
	if a.validator == nil {
		return nil
	}
	pbPlan, err := planToPB(plan)
	if err != nil {
		return nil
	}
	resp, err := a.validator.ValidatePlan(context.Background(), &pb.ValidatePlanRequest{Plan: pbPlan})
	if err != nil {
		return nil
	}
	out := make([]interfaces.PlanDiagnostic, 0, len(resp.GetDiagnostics()))
	for _, d := range resp.GetDiagnostics() {
		out = append(out, interfaces.PlanDiagnostic{
			Severity: planDiagnosticSeverityFromPB(d.GetSeverity()),
			Resource: d.GetResource(),
			Field:    d.GetField(),
			Message:  d.GetMessage(),
		})
	}
	return out
}

// RevokeProviderCredential satisfies interfaces.ProviderCredentialRevoker.
func (a *typedIaCAdapter) RevokeProviderCredential(ctx context.Context, source string, credentialID string) error {
	if a.revoker == nil {
		return unimplementedOptional(iacServiceCredentialRevoker)
	}
	_, err := a.revoker.RevokeProviderCredential(ctx, &pb.RevokeProviderCredentialRequest{
		Source:       source,
		CredentialId: credentialID,
	})
	return translateRPCErr(err)
}

// RepairDirtyMigration satisfies interfaces.ProviderMigrationRepairer.
func (a *typedIaCAdapter) RepairDirtyMigration(ctx context.Context, req interfaces.MigrationRepairRequest) (*interfaces.MigrationRepairResult, error) {
	if a.repairer == nil {
		return nil, unimplementedOptional(iacServiceMigrationRepairer)
	}
	resp, err := a.repairer.RepairDirtyMigration(ctx, &pb.RepairDirtyMigrationRequest{
		Request: migrationRepairRequestToPB(req),
	})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	return migrationRepairResultFromPB(resp.GetResult()), nil
}

// CaptureLogs satisfies interfaces.LogCaptureProvider.
func (a *typedIaCAdapter) CaptureLogs(ctx context.Context, req interfaces.LogCaptureRequest, sink interfaces.LogCaptureSink) error {
	if a.logCapture == nil {
		return unimplementedOptional(iacServiceLogCapture)
	}
	pbReq, err := logCaptureRequestToPB(req)
	if err != nil {
		return err
	}
	stream, err := a.logCapture.CaptureLogs(ctx, pbReq)
	if err != nil {
		return translateRPCErr(err)
	}
	for {
		chunk, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				return nil
			}
			return translateRPCErr(recvErr)
		}
		if sink != nil {
			if err := sink.WriteLogChunk(interfaces.LogChunk{
				Data:   append([]byte(nil), chunk.GetData()...),
				Source: chunk.GetSource(),
				EOF:    chunk.GetEof(),
			}); err != nil {
				return err
			}
		}
		if chunk.GetEof() {
			return nil
		}
	}
}

// ─── typedResourceDriver (per-type ResourceDriver wrapper) ──────────────────

// typedResourceDriver implements interfaces.ResourceDriver on top of the
// pb.ResourceDriverClient + a fixed resource_type. Each RPC carries the
// resource_type so a single server-side ResourceDriver can dispatch to
// the per-type driver implementation (DO plugin's 14-driver router in
// Task 11).
type typedResourceDriver struct {
	client       pb.ResourceDriverClient
	resourceType string
}

func (d *typedResourceDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	pbSpec, err := specToPB(spec)
	if err != nil {
		return nil, fmt.Errorf("typed driver %s: encode Create spec: %w", d.resourceType, err)
	}
	resp, err := d.client.Create(ctx, &pb.ResourceCreateRequest{ResourceType: d.resourceType, Spec: pbSpec})
	if err != nil {
		return nil, err
	}
	return outputFromPB(resp.GetOutput())
}

func (d *typedResourceDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	resp, err := d.client.Read(ctx, &pb.ResourceReadRequest{ResourceType: d.resourceType, Ref: refToPB(ref)})
	if err != nil {
		return nil, err
	}
	return outputFromPB(resp.GetOutput())
}

func (d *typedResourceDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	pbSpec, err := specToPB(spec)
	if err != nil {
		return nil, fmt.Errorf("typed driver %s: encode Update spec: %w", d.resourceType, err)
	}
	resp, err := d.client.Update(ctx, &pb.ResourceUpdateRequest{ResourceType: d.resourceType, Ref: refToPB(ref), Spec: pbSpec})
	if err != nil {
		return nil, err
	}
	return outputFromPB(resp.GetOutput())
}

func (d *typedResourceDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	_, err := d.client.Delete(ctx, &pb.ResourceDeleteRequest{ResourceType: d.resourceType, Ref: refToPB(ref)})
	return err
}

func (d *typedResourceDriver) Diff(ctx context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	pbSpec, err := specToPB(desired)
	if err != nil {
		return nil, fmt.Errorf("typed driver %s: encode Diff desired: %w", d.resourceType, err)
	}
	pbCurrent, err := outputToPB(current)
	if err != nil {
		return nil, fmt.Errorf("typed driver %s: encode Diff current: %w", d.resourceType, err)
	}
	resp, err := d.client.Diff(ctx, &pb.ResourceDiffRequest{ResourceType: d.resourceType, Desired: pbSpec, Current: pbCurrent})
	if err != nil {
		return nil, err
	}
	return diffResultFromPB(resp.GetResult())
}

func (d *typedResourceDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	resp, err := d.client.HealthCheck(ctx, &pb.ResourceHealthCheckRequest{ResourceType: d.resourceType, Ref: refToPB(ref)})
	if err != nil {
		return nil, err
	}
	r := resp.GetResult()
	if r == nil {
		return nil, nil
	}
	return &interfaces.HealthResult{Healthy: r.GetHealthy(), Message: r.GetMessage()}, nil
}

func (d *typedResourceDriver) Scale(ctx context.Context, ref interfaces.ResourceRef, replicas int) (*interfaces.ResourceOutput, error) {
	if replicas < math.MinInt32 || replicas > math.MaxInt32 {
		return nil, fmt.Errorf("typed driver %s: scale replicas %d out of int32 range", d.resourceType, replicas)
	}
	resp, err := d.client.Scale(ctx, &pb.ResourceScaleRequest{ResourceType: d.resourceType, Ref: refToPB(ref), Replicas: int32(replicas)}) //nolint:gosec // G115: range-checked above
	if err != nil {
		return nil, err
	}
	return outputFromPB(resp.GetOutput())
}

func (d *typedResourceDriver) SensitiveKeys() []string {
	resp, err := d.client.SensitiveKeys(context.Background(), &pb.SensitiveKeysRequest{ResourceType: d.resourceType})
	if err != nil {
		return nil
	}
	return append([]string(nil), resp.GetKeys()...)
}

func (d *typedResourceDriver) AdoptionRef(spec interfaces.ResourceSpec) (interfaces.ResourceRef, bool, error) {
	switch spec.Type {
	case "infra.dns", "infra.dns_delegation":
		ref := interfaces.ResourceRef{Name: spec.Name, Type: spec.Type}
		if providerID, _ := spec.Config["provider_id"].(string); providerID != "" {
			ref.ProviderID = providerID
		} else if domain, _ := spec.Config["domain"].(string); domain != "" {
			ref.ProviderID = domain
		} else {
			ref.ProviderID = spec.Name
		}
		return ref, true, nil
	default:
		if !boolFromAny(spec.Config["adopt_existing"]) {
			return interfaces.ResourceRef{}, false, nil
		}
		if spec.Name == "" {
			return interfaces.ResourceRef{}, false, fmt.Errorf("%s adoption requires resource name", spec.Type)
		}
		return interfaces.ResourceRef{Name: spec.Name, Type: spec.Type}, true, nil
	}
}

func (d *typedResourceDriver) SupportsConfigAdoption() bool {
	return true
}

// Troubleshoot satisfies interfaces.Troubleshooter (optional). gRPC
// Unimplemented (the legitimate negative signal when the plugin's
// driver does not implement Troubleshoot) is translated to
// interfaces.ErrProviderMethodUnimplemented so callers can errors.Is
// and fall back to the original failure message.
func (d *typedResourceDriver) Troubleshoot(ctx context.Context, ref interfaces.ResourceRef, failureMsg string) ([]interfaces.Diagnostic, error) {
	resp, err := d.client.Troubleshoot(ctx, &pb.TroubleshootRequest{
		ResourceType: d.resourceType,
		Ref:          refToPB(ref),
		FailureMsg:   failureMsg,
	})
	if err != nil {
		return nil, translateRPCErr(err)
	}
	out := make([]interfaces.Diagnostic, 0, len(resp.GetDiagnostics()))
	for _, d := range resp.GetDiagnostics() {
		out = append(out, interfaces.Diagnostic{
			ID:     d.GetId(),
			Phase:  d.GetPhase(),
			Cause:  d.GetCause(),
			At:     timeFromPB(d.GetAt()),
			Detail: d.GetDetail(),
		})
	}
	return out, nil
}

// ─── Marshalling helpers ────────────────────────────────────────────────────

func marshalJSONMap(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func unmarshalJSONMap(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalJSONAny(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func unmarshalJSONAny(b []byte) (any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func refToPB(r interfaces.ResourceRef) *pb.ResourceRef {
	return &pb.ResourceRef{Name: r.Name, Type: r.Type, ProviderId: r.ProviderID}
}

func refFromPB(r *pb.ResourceRef) interfaces.ResourceRef {
	if r == nil {
		return interfaces.ResourceRef{}
	}
	return interfaces.ResourceRef{Name: r.GetName(), Type: r.GetType(), ProviderID: r.GetProviderId()}
}

func refsToPB(refs []interfaces.ResourceRef) []*pb.ResourceRef {
	out := make([]*pb.ResourceRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, refToPB(r))
	}
	return out
}

func refsFromPB(refs []*pb.ResourceRef) []interfaces.ResourceRef {
	out := make([]interfaces.ResourceRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, refFromPB(r))
	}
	return out
}

func hintsToPB(h *interfaces.ResourceHints) *pb.ResourceHints {
	if h == nil {
		return nil
	}
	return &pb.ResourceHints{Cpu: h.CPU, Memory: h.Memory, Storage: h.Storage}
}

func hintsFromPB(h *pb.ResourceHints) *interfaces.ResourceHints {
	if h == nil {
		return nil
	}
	return &interfaces.ResourceHints{CPU: h.GetCpu(), Memory: h.GetMemory(), Storage: h.GetStorage()}
}

func specToPB(s interfaces.ResourceSpec) (*pb.ResourceSpec, error) {
	cfgJSON, err := marshalJSONMap(s.Config)
	if err != nil {
		return nil, err
	}
	return &pb.ResourceSpec{
		Name:       s.Name,
		Type:       s.Type,
		ConfigJson: cfgJSON,
		Size:       string(s.Size),
		Hints:      hintsToPB(s.Hints),
		DependsOn:  append([]string(nil), s.DependsOn...),
	}, nil
}

func specsToPB(specs []interfaces.ResourceSpec) ([]*pb.ResourceSpec, error) {
	out := make([]*pb.ResourceSpec, 0, len(specs))
	for _, s := range specs {
		ps, err := specToPB(s)
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, nil
}

func specFromPB(s *pb.ResourceSpec) (interfaces.ResourceSpec, error) {
	if s == nil {
		return interfaces.ResourceSpec{}, nil
	}
	cfg, err := unmarshalJSONMap(s.GetConfigJson())
	if err != nil {
		return interfaces.ResourceSpec{}, err
	}
	return interfaces.ResourceSpec{
		Name:      s.GetName(),
		Type:      s.GetType(),
		Config:    cfg,
		Size:      interfaces.Size(s.GetSize()),
		Hints:     hintsFromPB(s.GetHints()),
		DependsOn: append([]string(nil), s.GetDependsOn()...),
	}, nil
}

func stateToPB(st *interfaces.ResourceState) (*pb.ResourceState, error) {
	appliedJSON, err := marshalJSONMap(st.AppliedConfig)
	if err != nil {
		return nil, err
	}
	outputsJSON, err := marshalJSONMap(st.Outputs)
	if err != nil {
		return nil, err
	}
	return &pb.ResourceState{
		Id:                  st.ID,
		Name:                st.Name,
		Type:                st.Type,
		Provider:            st.Provider,
		ProviderRef:         st.ProviderRef,
		ProviderId:          st.ProviderID,
		ConfigHash:          st.ConfigHash,
		AppliedConfigJson:   appliedJSON,
		AppliedConfigSource: st.AppliedConfigSource,
		OutputsJson:         outputsJSON,
		Dependencies:        append([]string(nil), st.Dependencies...),
		CreatedAt:           timeToPB(st.CreatedAt),
		UpdatedAt:           timeToPB(st.UpdatedAt),
		LastDriftCheck:      timeToPB(st.LastDriftCheck),
	}, nil
}

func stateFromPB(s *pb.ResourceState) (*interfaces.ResourceState, error) {
	if s == nil {
		return nil, nil
	}
	applied, err := unmarshalJSONMap(s.GetAppliedConfigJson())
	if err != nil {
		return nil, err
	}
	outputs, err := unmarshalJSONMap(s.GetOutputsJson())
	if err != nil {
		return nil, err
	}
	return &interfaces.ResourceState{
		ID:                  s.GetId(),
		Name:                s.GetName(),
		Type:                s.GetType(),
		Provider:            s.GetProvider(),
		ProviderRef:         s.GetProviderRef(),
		ProviderID:          s.GetProviderId(),
		ConfigHash:          s.GetConfigHash(),
		AppliedConfig:       applied,
		AppliedConfigSource: s.GetAppliedConfigSource(),
		Outputs:             outputs,
		Dependencies:        append([]string(nil), s.GetDependencies()...),
		CreatedAt:           timeFromPB(s.GetCreatedAt()),
		UpdatedAt:           timeFromPB(s.GetUpdatedAt()),
		LastDriftCheck:      timeFromPB(s.GetLastDriftCheck()),
	}, nil
}

func statesToPB(states []interfaces.ResourceState) ([]*pb.ResourceState, error) {
	out := make([]*pb.ResourceState, 0, len(states))
	// Index iteration: interfaces.ResourceState is ~240 bytes;
	// per-iteration value-copy cost flagged by gocritic rangeValCopy.
	for i := range states {
		ps, err := stateToPB(&states[i])
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, nil
}

func outputToPB(o *interfaces.ResourceOutput) (*pb.ResourceOutput, error) {
	if o == nil {
		return nil, nil
	}
	outputsJSON, err := marshalJSONMap(o.Outputs)
	if err != nil {
		return nil, err
	}
	sensitive := make(map[string]bool, len(o.Sensitive))
	for k, v := range o.Sensitive {
		sensitive[k] = v
	}
	return &pb.ResourceOutput{
		Name:        o.Name,
		Type:        o.Type,
		ProviderId:  o.ProviderID,
		OutputsJson: outputsJSON,
		Sensitive:   sensitive,
		Status:      o.Status,
	}, nil
}

func outputFromPB(o *pb.ResourceOutput) (*interfaces.ResourceOutput, error) {
	if o == nil {
		return nil, nil
	}
	outputs, err := unmarshalJSONMap(o.GetOutputsJson())
	if err != nil {
		return nil, err
	}
	sensitive := make(map[string]bool, len(o.GetSensitive()))
	for k, v := range o.GetSensitive() {
		sensitive[k] = v
	}
	return &interfaces.ResourceOutput{
		Name:       o.GetName(),
		Type:       o.GetType(),
		ProviderID: o.GetProviderId(),
		Outputs:    outputs,
		Sensitive:  sensitive,
		Status:     o.GetStatus(),
	}, nil
}

func statusesFromPB(ss []*pb.ResourceStatus) ([]interfaces.ResourceStatus, error) {
	out := make([]interfaces.ResourceStatus, 0, len(ss))
	for _, s := range ss {
		o, err := unmarshalJSONMap(s.GetOutputsJson())
		if err != nil {
			return nil, err
		}
		out = append(out, interfaces.ResourceStatus{
			Name:       s.GetName(),
			Type:       s.GetType(),
			ProviderID: s.GetProviderId(),
			Status:     s.GetStatus(),
			Outputs:    o,
		})
	}
	return out, nil
}

func driftClassToPB(c interfaces.DriftClass) pb.DriftClass {
	switch c {
	case interfaces.DriftClassInSync:
		return pb.DriftClass_DRIFT_CLASS_IN_SYNC
	case interfaces.DriftClassGhost:
		return pb.DriftClass_DRIFT_CLASS_GHOST
	case interfaces.DriftClassConfig:
		return pb.DriftClass_DRIFT_CLASS_CONFIG
	default:
		return pb.DriftClass_DRIFT_CLASS_UNKNOWN
	}
}

func driftClassFromPB(c pb.DriftClass) interfaces.DriftClass {
	switch c {
	case pb.DriftClass_DRIFT_CLASS_IN_SYNC:
		return interfaces.DriftClassInSync
	case pb.DriftClass_DRIFT_CLASS_GHOST:
		return interfaces.DriftClassGhost
	case pb.DriftClass_DRIFT_CLASS_CONFIG:
		return interfaces.DriftClassConfig
	default:
		return interfaces.DriftClassUnknown
	}
}

func driftsFromPB(drifts []*pb.DriftResult) ([]interfaces.DriftResult, error) {
	out := make([]interfaces.DriftResult, 0, len(drifts))
	for _, d := range drifts {
		expected, err := unmarshalJSONMap(d.GetExpectedJson())
		if err != nil {
			return nil, err
		}
		actual, err := unmarshalJSONMap(d.GetActualJson())
		if err != nil {
			return nil, err
		}
		out = append(out, interfaces.DriftResult{
			Name:     d.GetName(),
			Type:     d.GetType(),
			Drifted:  d.GetDrifted(),
			Class:    driftClassFromPB(d.GetClass()),
			Expected: expected,
			Actual:   actual,
			Fields:   append([]string(nil), d.GetFields()...),
		})
	}
	return out, nil
}

func planActionToPB(a *interfaces.PlanAction) (*pb.PlanAction, error) {
	pbSpec, err := specToPB(a.Resource)
	if err != nil {
		return nil, err
	}
	var pbCurrent *pb.ResourceState
	if a.Current != nil {
		pbCurrent, err = stateToPB(a.Current)
		if err != nil {
			return nil, err
		}
	}
	pbChanges, err := changesToPB(a.Changes)
	if err != nil {
		return nil, err
	}
	return &pb.PlanAction{
		Action:             a.Action,
		Resource:           pbSpec,
		Current:            pbCurrent,
		Changes:            pbChanges,
		ResolvedConfigHash: a.ResolvedConfigHash,
	}, nil
}

func planActionFromPB(a *pb.PlanAction) (interfaces.PlanAction, error) {
	if a == nil {
		return interfaces.PlanAction{}, nil
	}
	spec, err := specFromPB(a.GetResource())
	if err != nil {
		return interfaces.PlanAction{}, err
	}
	var current *interfaces.ResourceState
	if a.GetCurrent() != nil {
		current, err = stateFromPB(a.GetCurrent())
		if err != nil {
			return interfaces.PlanAction{}, err
		}
	}
	changes, err := changesFromPB(a.GetChanges())
	if err != nil {
		return interfaces.PlanAction{}, err
	}
	return interfaces.PlanAction{
		Action:             a.GetAction(),
		Resource:           spec,
		Current:            current,
		Changes:            changes,
		ResolvedConfigHash: a.GetResolvedConfigHash(),
	}, nil
}

func changesToPB(changes []interfaces.FieldChange) ([]*pb.FieldChange, error) {
	out := make([]*pb.FieldChange, 0, len(changes))
	for _, c := range changes {
		oldJSON, err := marshalJSONAny(c.Old)
		if err != nil {
			return nil, err
		}
		newJSON, err := marshalJSONAny(c.New)
		if err != nil {
			return nil, err
		}
		out = append(out, &pb.FieldChange{
			Path:     c.Path,
			OldJson:  oldJSON,
			NewJson:  newJSON,
			ForceNew: c.ForceNew,
		})
	}
	return out, nil
}

func changesFromPB(changes []*pb.FieldChange) ([]interfaces.FieldChange, error) {
	out := make([]interfaces.FieldChange, 0, len(changes))
	for _, c := range changes {
		oldVal, err := unmarshalJSONAny(c.GetOldJson())
		if err != nil {
			return nil, err
		}
		newVal, err := unmarshalJSONAny(c.GetNewJson())
		if err != nil {
			return nil, err
		}
		out = append(out, interfaces.FieldChange{
			Path:     c.GetPath(),
			Old:      oldVal,
			New:      newVal,
			ForceNew: c.GetForceNew(),
		})
	}
	return out, nil
}

func planToPB(p *interfaces.IaCPlan) (*pb.IaCPlan, error) {
	if p == nil {
		return nil, nil
	}
	pbActions := make([]*pb.PlanAction, 0, len(p.Actions))
	// Index iteration: interfaces.PlanAction is ~152 bytes;
	// per-iteration value-copy cost flagged by gocritic rangeValCopy.
	for i := range p.Actions {
		pa, err := planActionToPB(&p.Actions[i])
		if err != nil {
			return nil, err
		}
		pbActions = append(pbActions, pa)
	}
	if p.SchemaVersion < math.MinInt32 || p.SchemaVersion > math.MaxInt32 {
		return nil, fmt.Errorf("typed adapter: plan SchemaVersion %d out of int32 range", p.SchemaVersion)
	}
	return &pb.IaCPlan{
		Id:            p.ID,
		Actions:       pbActions,
		CreatedAt:     timeToPB(p.CreatedAt),
		DesiredHash:   p.DesiredHash,
		SchemaVersion: int32(p.SchemaVersion), //nolint:gosec // G115: range-checked above
		InputSnapshot: copyStringMap(p.InputSnapshot),
	}, nil
}

func planFromPB(p *pb.IaCPlan) (*interfaces.IaCPlan, error) {
	if p == nil {
		return nil, nil
	}
	actions := make([]interfaces.PlanAction, 0, len(p.GetActions()))
	for _, a := range p.GetActions() {
		pa, err := planActionFromPB(a)
		if err != nil {
			return nil, err
		}
		actions = append(actions, pa)
	}
	return &interfaces.IaCPlan{
		ID:            p.GetId(),
		Actions:       actions,
		CreatedAt:     timeFromPB(p.GetCreatedAt()),
		DesiredHash:   p.GetDesiredHash(),
		SchemaVersion: int(p.GetSchemaVersion()),
		InputSnapshot: copyStringMap(p.GetInputSnapshot()),
	}, nil
}

func destroyResultFromPB(r *pb.DestroyResult) *interfaces.DestroyResult {
	if r == nil {
		return nil
	}
	errs := make([]interfaces.ActionError, 0, len(r.GetErrors()))
	for _, e := range r.GetErrors() {
		errs = append(errs, interfaces.ActionError{Resource: e.GetResource(), Action: e.GetAction(), Error: e.GetError()})
	}
	return &interfaces.DestroyResult{Destroyed: append([]string(nil), r.GetDestroyed()...), Errors: errs}
}

func sizingFromPB(s *pb.ProviderSizing) (*interfaces.ProviderSizing, error) {
	if s == nil {
		return nil, nil
	}
	specs, err := unmarshalJSONMap(s.GetSpecsJson())
	if err != nil {
		return nil, err
	}
	return &interfaces.ProviderSizing{InstanceType: s.GetInstanceType(), Specs: specs}, nil
}

func diffResultFromPB(r *pb.DiffResult) (*interfaces.DiffResult, error) {
	if r == nil {
		return nil, nil
	}
	changes, err := changesFromPB(r.GetChanges())
	if err != nil {
		return nil, err
	}
	return &interfaces.DiffResult{NeedsUpdate: r.GetNeedsUpdate(), NeedsReplace: r.GetNeedsReplace(), Changes: changes}, nil
}

func planDiagnosticSeverityFromPB(s pb.PlanDiagnosticSeverity) interfaces.PlanDiagnosticSeverity {
	switch s {
	case pb.PlanDiagnosticSeverity_PLAN_DIAGNOSTIC_WARNING:
		return interfaces.PlanDiagnosticWarning
	case pb.PlanDiagnosticSeverity_PLAN_DIAGNOSTIC_ERROR:
		return interfaces.PlanDiagnosticError
	default:
		return interfaces.PlanDiagnosticInfo
	}
}

func migrationRepairRequestToPB(r interfaces.MigrationRepairRequest) *pb.MigrationRepairRequest {
	// TimeoutSeconds is operator-supplied (CLI flag); clamp to int32 range
	// to avoid silent overflow. Real-world values are seconds-scale (≤ a few
	// hours), well within int32 bounds; the clamp is defensive.
	timeout := r.TimeoutSeconds
	if timeout > math.MaxInt32 {
		timeout = math.MaxInt32
	} else if timeout < 0 {
		timeout = 0
	}
	return &pb.MigrationRepairRequest{
		AppResourceName:      r.AppResourceName,
		DatabaseResourceName: r.DatabaseResourceName,
		JobImage:             r.JobImage,
		SourceDir:            r.SourceDir,
		ExpectedDirtyVersion: r.ExpectedDirtyVersion,
		ForceVersion:         r.ForceVersion,
		ThenUp:               r.ThenUp,
		UpIfClean:            r.UpIfClean,
		ConfirmForce:         r.ConfirmForce,
		Env:                  copyStringMap(r.Env),
		TimeoutSeconds:       int32(timeout), //nolint:gosec // G115: clamped above
	}
}

func migrationRepairResultFromPB(r *pb.MigrationRepairResult) *interfaces.MigrationRepairResult {
	if r == nil {
		return nil
	}
	diags := make([]interfaces.Diagnostic, 0, len(r.GetDiagnostics()))
	for _, d := range r.GetDiagnostics() {
		diags = append(diags, interfaces.Diagnostic{
			ID:     d.GetId(),
			Phase:  d.GetPhase(),
			Cause:  d.GetCause(),
			At:     timeFromPB(d.GetAt()),
			Detail: d.GetDetail(),
		})
	}
	return &interfaces.MigrationRepairResult{
		ProviderJobID: r.GetProviderJobId(),
		Status:        r.GetStatus(),
		Applied:       append([]string(nil), r.GetApplied()...),
		Logs:          r.GetLogs(),
		Diagnostics:   diags,
	}
}

func logCaptureRequestToPB(r interfaces.LogCaptureRequest) (*pb.CaptureLogsRequest, error) {
	tailLines := r.TailLines
	if tailLines < 0 {
		tailLines = 0
	} else if tailLines > math.MaxInt32 {
		tailLines = math.MaxInt32
	}
	logType, err := logCaptureTypeToPB(r.LogType)
	if err != nil {
		return nil, err
	}
	return &pb.CaptureLogsRequest{
		ResourceName:    r.ResourceName,
		ResourceType:    r.ResourceType,
		ProviderId:      r.ProviderID,
		ComponentName:   r.ComponentName,
		LogType:         logType,
		TailLines:       int32(tailLines), //nolint:gosec // G115: clamped above
		Follow:          r.Follow,
		DurationSeconds: r.DurationSeconds,
		DeploymentId:    r.DeploymentID,
	}, nil
}

func logCaptureTypeToPB(s string) (pb.LogCaptureType, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BUILD":
		return pb.LogCaptureType_LOG_CAPTURE_TYPE_BUILD, nil
	case "DEPLOY":
		return pb.LogCaptureType_LOG_CAPTURE_TYPE_DEPLOY, nil
	case "", "RUN":
		return pb.LogCaptureType_LOG_CAPTURE_TYPE_RUN, nil
	case "RUN_RESTARTED":
		return pb.LogCaptureType_LOG_CAPTURE_TYPE_RUN_RESTARTED, nil
	default:
		return pb.LogCaptureType_LOG_CAPTURE_TYPE_UNSPECIFIED,
			fmt.Errorf("log capture: unsupported log type %q (want BUILD, DEPLOY, RUN, or RUN_RESTARTED)", s)
	}
}

func timeToPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func timeFromPB(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Compile-time guards: every relevant interface MUST be satisfied. A
// signature drift on any of these will fail the build at this file
// rather than at the call site.
var (
	_ interfaces.IaCProvider               = (*typedIaCAdapter)(nil)
	_ interfaces.Enumerator                = (*typedIaCAdapter)(nil)
	_ interfaces.EnumeratorAll             = (*typedIaCAdapter)(nil)
	_ interfaces.DriftConfigDetector       = (*typedIaCAdapter)(nil)
	_ interfaces.ProviderValidator         = (*typedIaCAdapter)(nil)
	_ interfaces.ProviderCredentialRevoker = (*typedIaCAdapter)(nil)
	_ interfaces.ProviderMigrationRepairer = (*typedIaCAdapter)(nil)
	_ interfaces.ResourceDriver            = (*typedResourceDriver)(nil)
	_ interfaces.ResourceAdoptionLocator   = (*typedResourceDriver)(nil)
	_ interfaces.Troubleshooter            = (*typedResourceDriver)(nil)
)
