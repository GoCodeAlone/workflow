// Package providerclient provides a core-importable gRPC-client adapter that
// wraps the plugin/external/proto IaC gRPC services as interfaces.IaCProvider
// (+ optional sub-interfaces). It is the counterpart of the wfctl-only
// typedIaCAdapter (cmd/wfctl/iac_typed_adapter.go, package main), rebuilt in a
// core-importable package so the engine can register external iac.provider
// plugins as services via app.RegisterService — the foundational gap that v1/v1.1
// of infra-admin silently lacked (design doc §Cycle 3 resolution C1).
//
// Usage (via WiringHook in plugin/external/adapter.go):
//
//	adapter := providerclient.New(conn, advertisedServices)
//	app.RegisterService(moduleName, adapter)
//
// Steps resolve the provider via:
//
//	app.GetService(cfg.Provider, &provider) // provider is interfaces.IaCProvider
package providerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Fully-qualified gRPC service names for optional IaC services. These mirror
// the constants in cmd/wfctl/iac_typed_adapter.go and are sourced from the
// generated proto ServiceDesc so they cannot drift. The WiringHook in
// plugin/external/adapter.go passes the advertised set here; optional gRPC
// clients are constructed only when the corresponding name is present.
const (
	// IaCServiceRegionLister is the gRPC service name for the optional
	// IaCProviderRegionLister service.
	IaCServiceRegionLister = "workflow.plugin.external.iac.IaCProviderRegionLister"
	// IaCServiceDriftDetector is the gRPC service name for the optional
	// IaCProviderDriftDetector service.
	IaCServiceDriftDetector = "workflow.plugin.external.iac.IaCProviderDriftDetector"
	// IaCServiceRunner is the gRPC service name for the optional
	// IaCProviderRunner service.
	IaCServiceRunner = "workflow.plugin.external.iac.IaCProviderRunner"
)

// RegionListerProvider is a capability-discovery interface implemented by
// *Adapter when the plugin advertised IaCProviderRegionLister. Steps that want
// to prefer provider-sourced region lists type-assert the registered
// interfaces.IaCProvider to RegionListerProvider and call RegionLister().
// A nil return from RegionLister() means the plugin did not advertise the
// service — the step MUST fall back to its static catalog.
type RegionListerProvider interface {
	// RegionLister returns the region-lister capability object, or nil when
	// the plugin did not advertise IaCProviderRegionLister.
	RegionLister() interfaces.IaCProviderRegionLister
}

// driftDetectorAdapter is the value returned by DriftDetector() when the
// plugin advertises the IaCProviderDriftDetector service. It wraps the gRPC
// client and satisfies interfaces.DriftConfigDetector so consumers can route
// DetectDriftWithSpecs through the optional service.
//
// DetectDrift (existence-only) is routed through the required IaCProvider
// interface on *Adapter. The full config-aware drift (DetectDriftWithSpecs)
// lives here, mirroring how typedIaCAdapter routes driftCfg.
type driftDetectorAdapter struct {
	client pb.IaCProviderDriftDetectorClient
}

// DetectDriftWithSpecs calls the optional IaCProviderDriftDetector.DetectDrift
// gRPC (existence-only variant routed through the optional service). Steps that
// need the config-aware path use the DriftConfigDetector interface accessor.
func (d *driftDetectorAdapter) DetectDriftWithSpecs(ctx context.Context, resources []interfaces.ResourceRef, specs map[string]interfaces.ResourceSpec) ([]interfaces.DriftResult, error) {
	// Build the per-ref spec map for the RPC. Absent specs for a ref instruct
	// the provider to fall back to existence-only behavior for that ref.
	pbSpecs := make(map[string]*pb.ResourceSpec, len(specs))
	for k, v := range specs {
		ps, err := specToPB(v)
		if err != nil {
			return nil, fmt.Errorf("providerclient: encode DetectDriftWithSpecs specs[%s]: %w", k, err)
		}
		pbSpecs[k] = ps
	}
	resp, err := d.client.DetectDriftWithSpecs(ctx, &pb.DetectDriftWithSpecsRequest{
		Refs:  refsToPB(resources),
		Specs: pbSpecs,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("%w: DetectDriftWithSpecs not implemented by plugin",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return nil, err
	}
	return driftsFromPB(resp.GetDrifts())
}

// DriftDetectorProvider is a capability-discovery interface implemented by
// *Adapter when the plugin advertised IaCProviderDriftDetector. Steps that
// need config-aware drift detection type-assert the registered
// interfaces.IaCProvider to DriftDetectorProvider and call DriftDetector().
// A nil return means the plugin did not advertise the service.
type DriftDetectorProvider interface {
	// DriftDetector returns the drift-detector capability object (satisfying
	// interfaces.DriftConfigDetector), or nil when the plugin did not advertise
	// IaCProviderDriftDetector.
	DriftDetector() interfaces.DriftConfigDetector
}

// RunnerProvider is a capability-discovery interface implemented by *Adapter
// when the plugin advertised IaCProviderRunner. Callers use Runner() rather
// than asserting *Adapter directly to interfaces.IaCProviderRunner so absence
// remains visible as nil.
type RunnerProvider interface {
	Runner() interfaces.IaCProviderRunner
}

type runnerAdapter struct {
	client pb.IaCProviderRunnerClient
}

func (r *runnerAdapter) RunJob(ctx context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	resp, err := r.client.RunJob(ctx, jobSpecToPB(spec))
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("%w: IaCProviderRunner not registered by plugin",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return nil, err
	}
	return jobHandleFromPB(resp), nil
}

func (r *runnerAdapter) JobStatus(ctx context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	resp, err := r.client.JobStatus(ctx, jobHandleToPB(handle))
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("%w: IaCProviderRunner not registered by plugin",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return nil, err
	}
	return jobStatusFromPB(resp), nil
}

func (r *runnerAdapter) JobLogs(ctx context.Context, handle interfaces.JobHandle, sink interfaces.LogCaptureSink) error {
	stream, err := r.client.JobLogs(ctx, jobHandleToPB(handle))
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return fmt.Errorf("%w: IaCProviderRunner not registered by plugin",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return err
	}
	for {
		chunk, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				return nil
			}
			if status.Code(recvErr) == codes.Unimplemented {
				return fmt.Errorf("%w: IaCProviderRunner not registered by plugin",
					interfaces.ErrProviderMethodUnimplemented)
			}
			return recvErr
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

// Adapter wraps the pb.IaCProviderRequired gRPC client (and advertisement-gated
// optional clients) as interfaces.IaCProvider. Optional sub-interfaces
// (IaCProviderRegionLister, DriftConfigDetector) are exposed via typed accessors
// (RegionLister(), DriftDetector()) that return nil when the plugin did not
// advertise the corresponding gRPC service.
//
// The critical invariant: *Adapter does NOT unconditionally satisfy
// interfaces.IaCProviderRegionLister. Callers MUST type-assert to
// RegionListerProvider and call RegionLister() to discover the capability, then
// use the returned object. This mirrors typedIaCAdapter which returns nil
// optional clients for unadvertised services — enabling catalog steps'
// "static fallback if unadvertised" path to fire correctly.
//
// Compile-time guards are in adapter_test.go.
type Adapter struct {
	conn         grpc.ClientConnInterface
	required     pb.IaCProviderRequiredClient
	regionLister *regionListerImpl     // nil when IaCServiceRegionLister not advertised
	drift        *driftDetectorAdapter // nil when IaCServiceDriftDetector not advertised
	runner       *runnerAdapter        // nil when IaCServiceRunner not advertised

	// Capabilities cache. Populated on first call to fetchCapabilities via
	// capsOnce; reused for the adapter's lifetime (capabilities don't change
	// during a session). Mirrors typedIaCAdapter.cachedCaps + capsFetch.
	capsOnce   sync.Once
	cachedCaps *pb.CapabilitiesResponse
	capsErr    error
}

// regionListerImpl wraps pb.IaCProviderRegionListerClient and satisfies
// interfaces.IaCProviderRegionLister. It is the concrete type returned by
// Adapter.RegionLister().
type regionListerImpl struct {
	client pb.IaCProviderRegionListerClient
}

func (r *regionListerImpl) ListRegions(ctx context.Context, env string) ([]string, error) {
	resp, err := r.client.ListRegions(ctx, &pb.ListRegionsRequest{EnvName: env})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("%w: IaCProviderRegionLister not registered by plugin",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return nil, err
	}
	regions := make([]string, 0, len(resp.GetRegions()))
	for _, r := range resp.GetRegions() {
		if name := r.GetName(); name != "" {
			regions = append(regions, name)
		}
	}
	return regions, nil
}

// New constructs an Adapter over conn. Optional gRPC clients (RegionLister,
// DriftDetector) are constructed ONLY when the matching service name appears in
// advertisedServices. Passing nil or an empty map is valid — the adapter
// exposes only the required surface in that case.
//
// advertisedServices should be populated from the plugin's ContractRegistry
// (see plugin/external/adapter.go WiringHooks). The service-name constants
// IaCServiceRegionLister and IaCServiceDriftDetector are provided for the
// caller's convenience.
func New(conn grpc.ClientConnInterface, advertisedServices map[string]bool) *Adapter {
	a := &Adapter{
		conn:     conn,
		required: pb.NewIaCProviderRequiredClient(conn),
	}
	if advertisedServices[IaCServiceRegionLister] {
		a.regionLister = &regionListerImpl{client: pb.NewIaCProviderRegionListerClient(conn)}
	}
	if advertisedServices[IaCServiceDriftDetector] {
		a.drift = &driftDetectorAdapter{client: pb.NewIaCProviderDriftDetectorClient(conn)}
	}
	if advertisedServices[IaCServiceRunner] {
		a.runner = &runnerAdapter{client: pb.NewIaCProviderRunnerClient(conn)}
	}
	return a
}

// RegionLister implements RegionListerProvider. Returns the region-lister
// capability object when the plugin advertised IaCProviderRegionLister, or nil
// when it did not. Callers MUST nil-check before using.
func (a *Adapter) RegionLister() interfaces.IaCProviderRegionLister {
	if a.regionLister == nil {
		return nil
	}
	return a.regionLister
}

// DriftDetector implements DriftDetectorProvider. Returns the drift-detector
// capability object (satisfying interfaces.DriftConfigDetector) when the plugin
// advertised IaCProviderDriftDetector, or nil when it did not. Callers MUST
// nil-check before using.
func (a *Adapter) DriftDetector() interfaces.DriftConfigDetector {
	if a.drift == nil {
		return nil
	}
	return a.drift
}

// Runner implements RunnerProvider. Returns the provider-runner capability
// object when the plugin advertised IaCProviderRunner, or nil when it did not.
func (a *Adapter) Runner() interfaces.IaCProviderRunner {
	if a.runner == nil {
		return nil
	}
	return a.runner
}

// ─── interfaces.IaCProvider ──────────────────────────────────────────────────

// Name calls the IaCProviderRequired.Name RPC. Errors are logged and return "".
func (a *Adapter) Name() string {
	resp, err := a.required.Name(context.Background(), &pb.NameRequest{})
	if err != nil {
		log.Printf("providerclient: Name() RPC failed: %v", err)
		return ""
	}
	return resp.GetName()
}

// Version calls the IaCProviderRequired.Version RPC. Errors are logged and return "".
func (a *Adapter) Version() string {
	resp, err := a.required.Version(context.Background(), &pb.VersionRequest{})
	if err != nil {
		log.Printf("providerclient: Version() RPC failed: %v", err)
		return ""
	}
	return resp.GetVersion()
}

// Initialize calls the IaCProviderRequired.Initialize RPC.
func (a *Adapter) Initialize(ctx context.Context, cfg map[string]any) error {
	cfgJSON, err := marshalJSONMap(cfg)
	if err != nil {
		return fmt.Errorf("providerclient: marshal Initialize config: %w", err)
	}
	_, err = a.required.Initialize(ctx, &pb.InitializeRequest{ConfigJson: cfgJSON})
	return err
}

// fetchCapabilities returns the plugin's CapabilitiesResponse, caching the
// first result for the adapter's lifetime via sync.Once. RPC errors are also
// cached so repeated accesses don't repeatedly fail against an unreachable
// plugin. Capabilities are advertised at plugin startup and don't change
// during a session; caching is correct and cheap. Mirrors typedIaCAdapter.
func (a *Adapter) fetchCapabilities() (*pb.CapabilitiesResponse, error) {
	a.capsOnce.Do(func() {
		resp, err := a.required.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
		if err != nil {
			a.capsErr = err
			return
		}
		a.cachedCaps = resp
	})
	return a.cachedCaps, a.capsErr
}

// Capabilities calls the IaCProviderRequired.Capabilities RPC (cached) and
// translates the response to []interfaces.IaCCapabilityDeclaration.
func (a *Adapter) Capabilities() []interfaces.IaCCapabilityDeclaration {
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

// Plan calls IaCProviderRequired.Plan and translates proto↔interfaces types.
func (a *Adapter) Plan(ctx context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	pbDesired, err := specsToPB(desired)
	if err != nil {
		return nil, fmt.Errorf("providerclient: encode Plan desired: %w", err)
	}
	pbCurrent, err := statesToPB(current)
	if err != nil {
		return nil, fmt.Errorf("providerclient: encode Plan current: %w", err)
	}
	resp, err := a.required.Plan(ctx, &pb.PlanRequest{Desired: pbDesired, Current: pbCurrent})
	if err != nil {
		return nil, err
	}
	return planFromPB(resp.GetPlan())
}

// Destroy calls IaCProviderRequired.Destroy and translates proto↔interfaces types.
func (a *Adapter) Destroy(ctx context.Context, resources []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	resp, err := a.required.Destroy(ctx, &pb.DestroyRequest{Refs: refsToPB(resources)})
	if err != nil {
		return nil, err
	}
	return destroyResultFromPB(resp.GetResult()), nil
}

// Status calls IaCProviderRequired.Status and translates proto↔interfaces types.
func (a *Adapter) Status(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	resp, err := a.required.Status(ctx, &pb.StatusRequest{Refs: refsToPB(resources)})
	if err != nil {
		return nil, err
	}
	return statusesFromPB(resp.GetStatuses())
}

// DetectDrift routes through the optional IaCProviderDriftDetector gRPC service
// when the plugin advertised it (advertisement-gated in New). Falls back to
// ErrProviderMethodUnimplemented when the detector was not advertised — callers
// use errors.Is to skip or fall back to static drift logic.
func (a *Adapter) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	if a.drift == nil {
		return nil, fmt.Errorf("%w: IaCProviderDriftDetector not advertised by plugin",
			interfaces.ErrProviderMethodUnimplemented)
	}
	resp, err := a.drift.client.DetectDrift(ctx, &pb.DetectDriftRequest{Refs: refsToPB(resources)})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, fmt.Errorf("%w: IaCProviderDriftDetector.DetectDrift not implemented",
				interfaces.ErrProviderMethodUnimplemented)
		}
		return nil, err
	}
	return driftsFromPB(resp.GetDrifts())
}

// Import calls IaCProviderRequired.Import.
func (a *Adapter) Import(ctx context.Context, cloudID string, resourceType string) (*interfaces.ResourceState, error) {
	resp, err := a.required.Import(ctx, &pb.ImportRequest{ProviderId: cloudID, ResourceType: resourceType})
	if err != nil {
		return nil, err
	}
	return stateFromPB(resp.GetState())
}

// ResolveSizing calls IaCProviderRequired.ResolveSizing.
func (a *Adapter) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
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

// ResourceDriver returns an error — the full ResourceDriver optional service is
// not in PR-1 scope. Steps that need per-action CRUD use wfctlhelpers.ApplyPlanWithHooks
// which dispatches through the provider's ResourceDriver on the plugin side.
func (a *Adapter) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	return nil, fmt.Errorf("%w: ResourceDriver optional service not wired in PR-1 adapter",
		interfaces.ErrProviderMethodUnimplemented)
}

// SupportedCanonicalKeys returns the canonical keys from the cached
// CapabilitiesResponse, or the global set when the plugin doesn't declare a
// subset. Reuses fetchCapabilities so the Capabilities RPC is called at most
// once per adapter lifetime (per ADR-0029 / typedIaCAdapter precedent).
func (a *Adapter) SupportedCanonicalKeys() []string {
	resp, err := a.fetchCapabilities()
	if err == nil && resp != nil {
		if keys := resp.GetCanonicalKeys(); len(keys) > 0 {
			return append([]string(nil), keys...)
		}
	}
	return interfaces.CanonicalKeys()
}

// BootstrapStateBackend calls IaCProviderRequired.BootstrapStateBackend.
// All three result fields (Bucket, Region, Endpoint) are mapped from the proto
// response — Endpoint carries the S3-compatible API URL (e.g. DO Spaces endpoint)
// that was silently dropped in the original implementation.
func (a *Adapter) BootstrapStateBackend(ctx context.Context, cfg map[string]any) (*interfaces.BootstrapResult, error) {
	cfgJSON, err := marshalJSONMap(cfg)
	if err != nil {
		return nil, fmt.Errorf("providerclient: marshal BootstrapStateBackend cfg: %w", err)
	}
	resp, err := a.required.BootstrapStateBackend(ctx, &pb.BootstrapStateBackendRequest{ConfigJson: cfgJSON})
	if err != nil {
		return nil, err
	}
	r := resp.GetResult()
	if r == nil {
		return nil, nil
	}
	return &interfaces.BootstrapResult{
		Bucket:   r.GetBucket(),
		Region:   r.GetRegion(),
		Endpoint: r.GetEndpoint(),
		EnvVars:  copyStringMap(r.GetEnvVars()),
	}, nil
}

// Close is a no-op on the adapter — the connection lifecycle is owned by the
// host's plugin manager (see ExternalPluginAdapter.Conn docs).
func (a *Adapter) Close() error {
	return nil
}

// ─── Proto↔interfaces conversion helpers ────────────────────────────────────
//
// These mirror the same-named functions in cmd/wfctl/iac_typed_adapter.go
// (package main, not importable by core). They are intentionally minimal —
// only the subset required by the PR-1 methods above (Plan, Status, Destroy,
// DetectDrift, ListRegions). Additional converters can be added as PR-2 (step
// impl) needs them. Do NOT import cmd/wfctl.

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

func refToPB(r interfaces.ResourceRef) *pb.ResourceRef {
	return &pb.ResourceRef{Name: r.Name, Type: r.Type, ProviderId: r.ProviderID}
}

func refsToPB(refs []interfaces.ResourceRef) []*pb.ResourceRef {
	out := make([]*pb.ResourceRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, refToPB(r))
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
	for i := range states {
		ps, err := stateToPB(&states[i])
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, nil
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

// driftsFromPB translates []*pb.DriftResult to []interfaces.DriftResult.
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

func jobSpecToPB(s interfaces.JobSpec) *pb.JobSpec {
	out := &pb.JobSpec{
		Name:          s.Name,
		Kind:          s.Kind,
		Image:         s.Image,
		RunCommand:    s.RunCommand,
		EnvVars:       copyStringMap(s.EnvVars),
		EnvVarsSecret: copyStringMap(s.EnvVarsSecret),
		Cron:          s.Cron,
	}
	if s.Termination != nil {
		out.Termination = &pb.JobTerminationSpec{
			DrainSeconds:       clampInt32(s.Termination.DrainSeconds),
			GracePeriodSeconds: clampInt32(s.Termination.GracePeriodSeconds),
		}
	}
	if len(s.Alerts) > 0 {
		out.Alerts = make([]*pb.JobAlertSpec, 0, len(s.Alerts))
		for _, alert := range s.Alerts {
			out.Alerts = append(out.Alerts, &pb.JobAlertSpec{
				Rule:     alert.Rule,
				Operator: alert.Operator,
				Value:    alert.Value,
				Window:   alert.Window,
				Disabled: alert.Disabled,
			})
		}
	}
	if len(s.LogDestinations) > 0 {
		out.LogDestinations = make([]*pb.JobLogDestinationSpec, 0, len(s.LogDestinations))
		for _, dest := range s.LogDestinations {
			out.LogDestinations = append(out.LogDestinations, &pb.JobLogDestinationSpec{
				Name:     dest.Name,
				Endpoint: dest.Endpoint,
				Headers:  copyStringMap(dest.Headers),
				Tls:      dest.TLS,
			})
		}
	}
	return out
}

func jobHandleToPB(h interfaces.JobHandle) *pb.JobHandle {
	return &pb.JobHandle{
		Id:       h.ID,
		Name:     h.Name,
		Provider: h.Provider,
		Metadata: copyStringMap(h.Metadata),
	}
}

func jobHandleFromPB(h *pb.JobHandle) *interfaces.JobHandle {
	if h == nil {
		return nil
	}
	return &interfaces.JobHandle{
		ID:       h.GetId(),
		Name:     h.GetName(),
		Provider: h.GetProvider(),
		Metadata: copyStringMap(h.GetMetadata()),
	}
}

func jobStatusFromPB(s *pb.JobStatusReply) *interfaces.JobStatusReply {
	if s == nil {
		return nil
	}
	handle := interfaces.JobHandle{}
	if h := jobHandleFromPB(s.GetHandle()); h != nil {
		handle = *h
	}
	return &interfaces.JobStatusReply{
		Handle:   handle,
		State:    jobStateFromPB(s.GetState()),
		ExitCode: int(s.GetExitCode()),
		Message:  s.GetMessage(),
	}
}

func jobStateFromPB(s pb.JobState) interfaces.JobState {
	switch s {
	case pb.JobState_JOB_STATE_PENDING:
		return interfaces.JobStatePending
	case pb.JobState_JOB_STATE_RUNNING:
		return interfaces.JobStateRunning
	case pb.JobState_JOB_STATE_SUCCEEDED:
		return interfaces.JobStateSucceeded
	case pb.JobState_JOB_STATE_FAILED:
		return interfaces.JobStateFailed
	case pb.JobState_JOB_STATE_CANCELLED:
		return interfaces.JobStateCancelled
	default:
		return interfaces.JobStateUnknown
	}
}

func clampInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) //nolint:gosec // G115: value is clamped to int32 bounds above.
}
