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
//	adapter := providerclient.New(conn)
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
	"log"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Adapter wraps the pb.IaCProviderRequired gRPC client (and optional
// pb.IaCProviderRegionListerClient) as interfaces.IaCProvider +
// interfaces.IaCProviderRegionLister. The conn-backed optional clients are
// always constructed: the gRPC channel multiplexes them cheaply, and the
// optional-service guard is at the plugin's server side (Unimplemented).
//
// Compile-time guards are in adapter_test.go.
type Adapter struct {
	conn         grpc.ClientConnInterface
	required     pb.IaCProviderRequiredClient
	regionLister pb.IaCProviderRegionListerClient
}

// New constructs an Adapter over conn. Both the required and optional
// region-lister clients are constructed eagerly against the shared conn;
// the connection multiplexes them at zero marginal cost. If the plugin
// does not serve the optional service, calls to ListRegions return
// interfaces.ErrProviderMethodUnimplemented.
func New(conn grpc.ClientConnInterface) *Adapter {
	return &Adapter{
		conn:         conn,
		required:     pb.NewIaCProviderRequiredClient(conn),
		regionLister: pb.NewIaCProviderRegionListerClient(conn),
	}
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

// Capabilities calls the IaCProviderRequired.Capabilities RPC and translates
// the response to []interfaces.IaCCapabilityDeclaration.
func (a *Adapter) Capabilities() []interfaces.IaCCapabilityDeclaration {
	resp, err := a.required.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
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

// DetectDrift calls IaCProviderRequired.Status to satisfy the IaCProvider interface.
// This is a stub implementation — the required interface needs DetectDrift, so we
// surface a not-implemented error. Full drift detection is available via the
// IaCProviderDriftDetector optional service, which is not in scope for PR-1.
func (a *Adapter) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, fmt.Errorf("%w: DetectDrift optional service not wired in PR-1 adapter",
		interfaces.ErrProviderMethodUnimplemented)
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

// SupportedCanonicalKeys returns the keys from Capabilities, or the global
// canonical key set if the plugin doesn't declare a subset.
func (a *Adapter) SupportedCanonicalKeys() []string {
	resp, err := a.required.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
	if err == nil && resp != nil {
		if keys := resp.GetCanonicalKeys(); len(keys) > 0 {
			return append([]string(nil), keys...)
		}
	}
	return interfaces.CanonicalKeys()
}

// BootstrapStateBackend calls IaCProviderRequired.BootstrapStateBackend.
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
		Bucket:  r.GetBucket(),
		Region:  r.GetRegion(),
		EnvVars: copyStringMap(r.GetEnvVars()),
	}, nil
}

// Close is a no-op on the adapter — the connection lifecycle is owned by the
// host's plugin manager (see ExternalPluginAdapter.Conn docs).
func (a *Adapter) Close() error {
	return nil
}

// ─── interfaces.IaCProviderRegionLister ─────────────────────────────────────

// ListRegions calls the IaCProviderRegionLister.ListRegions RPC and returns
// region identifiers sorted by the server. If the plugin does not implement
// the service (gRPC Unimplemented) an ErrProviderMethodUnimplemented is returned
// so callers can fall back to a static catalog.
func (a *Adapter) ListRegions(ctx context.Context, env string) ([]string, error) {
	resp, err := a.regionLister.ListRegions(ctx, &pb.ListRegionsRequest{EnvName: env})
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

// ─── Proto↔interfaces conversion helpers ────────────────────────────────────
//
// These mirror the same-named functions in cmd/wfctl/iac_typed_adapter.go
// (package main, not importable by core). They are intentionally minimal —
// only the subset required by the PR-1 methods above (Plan, Status, Destroy,
// ListRegions). Additional converters can be added as PR-2 (step impl) needs
// them. Do NOT import cmd/wfctl.

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
