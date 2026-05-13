package conformance

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"google.golang.org/protobuf/types/known/structpb"
)

// scenarioDiffSurvivesGRPCRoundTrip asserts that ResourceOutput.Outputs
// (Diff input on the wire) and DiffResult.Changes (Diff output on the
// wire) survive the structpb encode/decode cycle that gRPC transport
// applies between wfctl and external IaC plugins.
//
// The wfctl-side remoteResourceDriver lives in cmd/wfctl (package main),
// so it cannot be imported here. Instead, the scenario wraps the
// provider's driver in a portable [roundtripDriver] that mirrors the
// same encode boundary — args map → structpb.NewStruct → AsMap on
// inputs, DiffResult map → structpb.NewStruct → AsMap on outputs. A
// provider whose Outputs or DiffResult.Changes carry types structpb
// cannot represent (chan, func, *time.Time without MarshalJSON, etc.)
// surfaces here as a [structpb.NewStruct] error before the delegate
// driver is dispatched.
//
// Smoke=false, RequiresCloud=false per design table row 3 — runs
// in-process against any provider that exposes a non-nil
// [interfaces.ResourceDriver] for "infra.vpc". For providers without
// such a driver, the scenario t.Skips rather than failing — the
// structpb contract is independent of any specific cloud target.
//
// Plan-spec deviation: the design table says "uses remoteResourceDriver
// wrapper + in-memory grpc". The actual remoteResourceDriver is
// unexported in cmd/wfctl, and a real grpc round-trip would couple this
// scenario to plugin/external — overkill for an encoding contract test.
// The portable equivalent is structpb.NewStruct/AsMap, which is what
// the wire layer (mapToStruct in plugin/external/convert.go) wraps.
func scenarioDiffSurvivesGRPCRoundTrip(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	// Two skip signals are honored: (a) ResourceDriver returns nil
	// without error, or (b) returns an error (the canonical idiom —
	// e.g., *platform.ResourceDriverNotFoundError). Either path is
	// read as "provider did not opt in to the structpb probe" rather
	// than a hard conformance failure. infra.vpc IS in the documented
	// abstract type set (DOCUMENTATION.md line 520), but providers
	// targeting niche surfaces (e.g., DNS-only, identity-only) may
	// legitimately not implement it; the structpb contract is
	// transport-layer and doesn't require a specific resource shape.
	drv, err := p.ResourceDriver("infra.vpc")
	if err != nil {
		t.Skipf("provider %s does not expose a ResourceDriver for infra.vpc "+
			"(structpb roundtrip probe is opt-in for providers exposing the type): %v",
			p.Name(), err)
		return
	}
	if drv == nil {
		t.Skipf("provider %s returned nil ResourceDriver for infra.vpc; "+
			"structpb roundtrip probe is opt-in", p.Name())
		return
	}

	// inputs covers all structpb-compatible JSON shapes: string,
	// number (int + float), bool, nil, []any, nested map[string]any.
	// A provider whose Outputs carry a non-structpb-friendly type
	// (chan, func, *time.Time without MarshalJSON, custom struct
	// without map representation) surfaces here as a NewStruct error
	// in roundtripDriver.Diff before the delegate is dispatched.
	inputs := map[string]any{
		"string":     "hello",
		"int_42":     42,
		"float_pi":   3.14,
		"bool_true":  true,
		"bool_false": false,
		"null":       nil,
		"list":       []any{"a", "b", float64(7)},
		"nested":     map[string]any{"k": "v", "n": float64(99)},
	}

	desired := interfaces.ResourceSpec{
		Name:   "vpc-grpc",
		Type:   "infra.vpc",
		Config: map[string]any{"region": "nyc3"},
	}
	current := &interfaces.ResourceOutput{
		ProviderID: "pid-grpc",
		Name:       "vpc-grpc",
		Type:       "infra.vpc",
		Status:     "ready",
		Outputs:    inputs,
	}

	rt := &roundtripDriver{delegate: drv}
	res, err := rt.Diff(context.Background(), desired, current)
	if err != nil {
		t.Fatalf("roundtripDriver.Diff: %v (Outputs/Changes must use structpb-compatible types: string/number/bool/nil/[]any/map[string]any)", err)
	}
	if res == nil {
		t.Fatal("roundtripDriver.Diff returned nil DiffResult after structpb roundtrip; the response decode must yield a non-nil value")
		return
	}

	// Each Change that survived the response-side roundtrip must
	// preserve its Path string — that field is structpb-faithful (no
	// type normalization) and an empty Path post-roundtrip means the
	// wire shape lost the field. Old/New values may legally normalize
	// (int → float64) per JSON's number representation; that is OK
	// and exercised implicitly by [structpb.NewStruct] succeeding above.
	for i, c := range res.Changes {
		if c.Path == "" {
			t.Errorf("Changes[%d].Path empty after structpb roundtrip; wire shape dropped the path field", i)
		}
	}
}

// roundtripDriver wraps a delegate [interfaces.ResourceDriver] and
// applies the same structpb encode/decode boundary that gRPC transport
// would apply between the wfctl-side remoteResourceDriver and a
// plugin-side server. Only Diff is instrumented (the scenario's only
// dispatch point); other methods pass through to the delegate
// unchanged.
//
// The encode boundary is two-sided:
//   - Inputs (desired spec, current output): packed into the args map
//     wfctl uses on the wire, round-tripped through
//     [structpb.NewStruct] + AsMap, then unpacked into reconstructed
//     ResourceSpec / ResourceOutput before dispatching to the delegate.
//   - Outputs (DiffResult): encoded into a map shape, round-tripped
//     through structpb, decoded back into a DiffResult.
type roundtripDriver struct {
	delegate interfaces.ResourceDriver
}

// Compile-time interface conformance — fails the build if
// [interfaces.ResourceDriver] drifts in a way the wrapper doesn't cover.
var _ interfaces.ResourceDriver = (*roundtripDriver)(nil)

// Diff runs a wire-shape encode/decode on inputs and outputs around the
// delegate's Diff call. Errors from structpb.NewStruct surface to the
// caller without invoking the delegate — that is the scenario's pin
// against unmarshalable types in Outputs.
func (d *roundtripDriver) Diff(ctx context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	args := map[string]any{
		"spec_name":   desired.Name,
		"spec_type":   desired.Type,
		"spec_config": desired.Config,
	}
	if current != nil {
		args["current_name"] = current.Name
		args["current_type"] = current.Type
		args["current_provider_id"] = current.ProviderID
		args["current_status"] = current.Status
		args["current_outputs"] = current.Outputs
	}
	rtArgs, err := structpbRoundtrip(args)
	if err != nil {
		return nil, fmt.Errorf("encode Diff args: %w", err)
	}

	rtDesired := decodeSpecArgs(rtArgs)
	rtCurrent := decodeOutputArgs(rtArgs)

	res, callErr := d.delegate.Diff(ctx, rtDesired, rtCurrent)
	if res == nil {
		// gocritic: returning res here is equivalent to returning nil
		// (we just checked res == nil) — be explicit so the contract
		// at the call site is unambiguous. callErr propagates either
		// way: a driver may legitimately return (nil, nil) for "no
		// changes" or (nil, err) for a failure path.
		return nil, callErr
	}

	resMap := encodeDiffResult(res)
	rtResMap, err := structpbRoundtrip(resMap)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("encode DiffResult: %w", err), callErr)
	}
	return decodeDiffResult(rtResMap), callErr
}

// structpbRoundtrip encodes m via [structpb.NewStruct] and decodes back
// via AsMap. Returns the original error from NewStruct on failure so
// callers can attribute the structpb-incompatible value to their
// inputs (the regression-pin contract).
func structpbRoundtrip(m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil, err
	}
	return s.AsMap(), nil
}

// decodeSpecArgs reconstructs a ResourceSpec from a roundtripped args
// map. Mirrors the server-side adapter that materialises a Spec from
// the wire-decoded structpb.
func decodeSpecArgs(m map[string]any) interfaces.ResourceSpec {
	spec := interfaces.ResourceSpec{}
	if v, ok := m["spec_name"].(string); ok {
		spec.Name = v
	}
	if v, ok := m["spec_type"].(string); ok {
		spec.Type = v
	}
	if v, ok := m["spec_config"].(map[string]any); ok {
		spec.Config = v
	}
	return spec
}

// decodeOutputArgs reconstructs a ResourceOutput from a roundtripped
// args map. Returns nil when no current_* keys are present, mirroring
// how remoteResourceDriver omits the current_* wire fields when current
// is nil upstream.
func decodeOutputArgs(m map[string]any) *interfaces.ResourceOutput {
	if _, ok := m["current_name"]; !ok {
		return nil
	}
	out := &interfaces.ResourceOutput{}
	if v, ok := m["current_name"].(string); ok {
		out.Name = v
	}
	if v, ok := m["current_type"].(string); ok {
		out.Type = v
	}
	if v, ok := m["current_provider_id"].(string); ok {
		out.ProviderID = v
	}
	if v, ok := m["current_status"].(string); ok {
		out.Status = v
	}
	if v, ok := m["current_outputs"].(map[string]any); ok {
		out.Outputs = v
	}
	return out
}

// encodeDiffResult serializes a DiffResult into the wire-shape map
// remoteResourceDriver consumes via decodeDiffResult on the way back.
// nil in → nil out (downstream callers omit the response struct).
func encodeDiffResult(r *interfaces.DiffResult) map[string]any {
	if r == nil {
		return nil
	}
	m := map[string]any{
		"needs_update":  r.NeedsUpdate,
		"needs_replace": r.NeedsReplace,
	}
	if len(r.Changes) > 0 {
		changes := make([]any, 0, len(r.Changes))
		for _, c := range r.Changes {
			changes = append(changes, map[string]any{
				"path":      c.Path,
				"old":       c.Old,
				"new":       c.New,
				"force_new": c.ForceNew,
			})
		}
		m["changes"] = changes
	}
	return m
}

// decodeDiffResult reconstructs a DiffResult from a roundtripped
// wire-shape map. Mirrors the wfctl-side decode path (deploy_providers.go
// remoteResourceDriver.Diff inverse) so the scenario reads back the
// same way wfctl does in production.
func decodeDiffResult(m map[string]any) *interfaces.DiffResult {
	if m == nil {
		return nil
	}
	res := &interfaces.DiffResult{}
	res.NeedsUpdate, _ = m["needs_update"].(bool)
	res.NeedsReplace, _ = m["needs_replace"].(bool)
	if raw, ok := m["changes"].([]any); ok {
		for _, c := range raw {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			fc := interfaces.FieldChange{}
			if s, ok := cm["path"].(string); ok {
				fc.Path = s
			}
			fc.Old = cm["old"]
			fc.New = cm["new"]
			fc.ForceNew, _ = cm["force_new"].(bool)
			res.Changes = append(res.Changes, fc)
		}
	}
	return res
}

// Pass-through stubs for non-Diff methods. The scenario only exercises
// Diff; other methods exist solely to satisfy [interfaces.ResourceDriver].
func (d *roundtripDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.delegate.Create(ctx, spec)
}
func (d *roundtripDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return d.delegate.Read(ctx, ref)
}
func (d *roundtripDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.delegate.Update(ctx, ref, spec)
}
func (d *roundtripDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.delegate.Delete(ctx, ref)
}
func (d *roundtripDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return d.delegate.HealthCheck(ctx, ref)
}
func (d *roundtripDriver) Scale(ctx context.Context, ref interfaces.ResourceRef, replicas int) (*interfaces.ResourceOutput, error) {
	return d.delegate.Scale(ctx, ref, replicas)
}
func (d *roundtripDriver) SensitiveKeys() []string {
	return d.delegate.SensitiveKeys()
}

func init() {
	register(Scenario{
		Name:          "Scenario_DiffSurvivesGRPCRoundTrip",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioDiffSurvivesGRPCRoundTrip,
	})
}
