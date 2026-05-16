package main

// iac_typed_adapter_test.go — unit + in-process gRPC integration tests for
// the typedIaCAdapter (Task 30 of the strict-contracts force-cutover
// plan, docs/plans/2026-05-10-strict-contracts-force-cutover.md).
//
// Coverage:
//   - Compile-time interface satisfaction (covered via package-scope
//     `var _ interfaces.X = (*typedIaCAdapter)(nil)` in the production
//     file; this file repeats the assertion as a runtime guard so a
//     refactor that drops a method while removing the production
//     guard fails the test rather than the build).
//   - Optional-method gating: when the matching optional client is nil
//     the call returns interfaces.ErrProviderMethodUnimplemented
//     (errors.Is satisfied) so dispatch sites continue to skip the
//     provider as designed.
//   - DriftClass enum round-trip preserves all four classifications.
//   - End-to-end Name/Version/EnumerateAll round-trip through an
//     in-process gRPC server proves the adapter wires the typed RPC
//     correctly without spawning a real plugin subprocess.

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestTypedAdapter_SatisfiesIaCProvider asserts the adapter's Go
// interface conformance at runtime so refactors that drop a method
// fail tests rather than relying on the package-scope compile guard.
func TestTypedAdapter_SatisfiesIaCProvider(t *testing.T) {
	a := &typedIaCAdapter{}

	if _, ok := any(a).(interfaces.IaCProvider); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.IaCProvider")
	}
	if _, ok := any(a).(interfaces.Enumerator); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.Enumerator")
	}
	if _, ok := any(a).(interfaces.EnumeratorAll); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.EnumeratorAll")
	}
	if _, ok := any(a).(interfaces.DriftConfigDetector); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.DriftConfigDetector")
	}
	if _, ok := any(a).(interfaces.ProviderValidator); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.ProviderValidator")
	}
	if _, ok := any(a).(interfaces.ProviderCredentialRevoker); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.ProviderCredentialRevoker")
	}
	if _, ok := any(a).(interfaces.ProviderMigrationRepairer); !ok {
		t.Fatalf("typedIaCAdapter must satisfy interfaces.ProviderMigrationRepairer")
	}
}

// TestTypedAdapter_OptionalReturnsUnimplementedSentinel verifies every
// optional-method path returns interfaces.ErrProviderMethodUnimplemented
// (errors.Is-satisfied) when the matching client is nil. Without this
// guarantee the v0.27.1 iterate-and-skip semantics break.
func TestTypedAdapter_OptionalReturnsUnimplementedSentinel(t *testing.T) {
	a := &typedIaCAdapter{} // every optional client nil

	tests := []struct {
		name string
		call func() error
	}{
		{"EnumerateAll", func() error {
			_, err := a.EnumerateAll(context.Background(), "infra.spaces_key")
			return err
		}},
		{"EnumerateByTag", func() error {
			_, err := a.EnumerateByTag(context.Background(), "production")
			return err
		}},
		{"DetectDrift", func() error {
			_, err := a.DetectDrift(context.Background(), nil)
			return err
		}},
		{"DetectDriftWithSpecs", func() error {
			_, err := a.DetectDriftWithSpecs(context.Background(), nil, nil)
			return err
		}},
		{"RevokeProviderCredential", func() error {
			return a.RevokeProviderCredential(context.Background(), "digitalocean.spaces", "key-1")
		}},
		{"RepairDirtyMigration", func() error {
			_, err := a.RepairDirtyMigration(context.Background(), interfaces.MigrationRepairRequest{})
			return err
		}},
		{"ResourceDriver", func() error {
			_, err := a.ResourceDriver("infra.database")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatalf("%s: expected ErrProviderMethodUnimplemented, got nil", tc.name)
			}
			if !errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
				t.Fatalf("%s: error %v not errors.Is ErrProviderMethodUnimplemented", tc.name, err)
			}
		})
	}
}

// TestTypedAdapter_ValidatePlanReturnsNilWhenValidatorAbsent — the Go
// interfaces.ProviderValidator.ValidatePlan signature returns []diag
// (no error). When the validator client is nil we return nil
// diagnostics so callers that type-asserted-then-iterated continue to
// behave identically to "provider does not implement validation".
func TestTypedAdapter_ValidatePlanReturnsNilWhenValidatorAbsent(t *testing.T) {
	a := &typedIaCAdapter{}
	if got := a.ValidatePlan(&interfaces.IaCPlan{}); got != nil {
		t.Fatalf("expected nil diagnostics when validator absent; got %d", len(got))
	}
}

// TestTypedAdapter_DriftClassEnumRoundTrip ensures every DriftClass
// constant survives the proto-enum conversion in both directions —
// regression guard against silent drop to DriftClassUnknown.
func TestTypedAdapter_DriftClassEnumRoundTrip(t *testing.T) {
	cases := []interfaces.DriftClass{
		interfaces.DriftClassUnknown,
		interfaces.DriftClassInSync,
		interfaces.DriftClassGhost,
		interfaces.DriftClassConfig,
	}
	for _, c := range cases {
		got := driftClassFromPB(driftClassToPB(c))
		if got != c {
			t.Errorf("DriftClass round-trip: %q → %v → %q", c, driftClassToPB(c), got)
		}
	}
}

// TestTypedAdapter_TranslateRPCErrSurfacesUnimplemented asserts that
// gRPC Unimplemented status (the wire-level signal a plugin emits when
// an optional method is not registered) is translated to the
// interfaces.ErrProviderMethodUnimplemented sentinel callers iterate
// on.
func TestTypedAdapter_TranslateRPCErrSurfacesUnimplemented(t *testing.T) {
	if got := translateRPCErr(nil); got != nil {
		t.Fatalf("nil error must pass through; got %v", got)
	}
	other := errors.New("transport reset")
	if got := translateRPCErr(other); got != other {
		t.Fatalf("non-Unimplemented error must pass through unchanged; got %v", got)
	}
	// Build a real gRPC Unimplemented status so status.Code(err) ==
	// codes.Unimplemented exactly the way a server would emit it.
	srv, conn := startTestServer(t, &enumeratorOnlyStub{}, true /*registerEnumerator*/)
	defer srv.Stop()
	defer conn.Close()
	a := newTypedIaCAdapter(conn, map[string]bool{
		iacServiceEnumerator: true,
	})
	// Required Plan call against a server that only registered
	// IaCProviderEnumerator must produce a real codes.Unimplemented
	// error from grpc-go's default unknown-service handler.
	_, err := a.Plan(context.Background(), nil, nil)
	if err == nil {
		t.Fatalf("expected Unimplemented from server with no Required service; got nil")
	}
	translated := translateRPCErr(err)
	if !errors.Is(translated, interfaces.ErrProviderMethodUnimplemented) {
		t.Fatalf("Unimplemented status not translated; got %v", err)
	}
	// Per Copilot MINOR-1 on PR #605: the translation must wrap with
	// `%w/%w` so callers can recover the original gRPC status from the
	// unwrap chain via status.FromError. Without this, retry-classifier
	// callsites that distinguish codes.Unimplemented vs
	// codes.Unavailable lose the signal.
	if st, ok := status.FromError(translated); !ok || st.Code() != codes.Unimplemented {
		t.Fatalf("status.FromError must recover codes.Unimplemented from the unwrap chain; got ok=%v code=%v", ok, st.Code())
	}
}

// TestTypedAdapter_EndToEnd_NameVersionEnumerateAll proves the
// adapter wires a typed RPC to a real (in-process) gRPC server that
// implements the typed pb interfaces. Catches signature drift between
// the adapter's encode/decode helpers and the pb message shapes.
func TestTypedAdapter_EndToEnd_NameVersionEnumerateAll(t *testing.T) {
	srv, conn := startTestServer(t, &fullStubProvider{
		name:    "stub-provider",
		version: "v0.0.1-test",
		enumerated: []*pb.ResourceOutput{
			{Name: "spaces-key-1", Type: "infra.spaces_key", ProviderId: "key-aaaaa"},
			{Name: "spaces-key-2", Type: "infra.spaces_key", ProviderId: "key-bbbbb"},
		},
	}, true /*registerEnumerator*/)
	defer srv.Stop()
	defer conn.Close()

	adapter := newTypedIaCAdapter(conn, map[string]bool{
		iacServiceEnumerator: true,
	})

	if got := adapter.Name(); got != "stub-provider" {
		t.Errorf("Name() = %q, want %q", got, "stub-provider")
	}
	if got := adapter.Version(); got != "v0.0.1-test" {
		t.Errorf("Version() = %q, want %q", got, "v0.0.1-test")
	}

	out, err := adapter.EnumerateAll(context.Background(), "infra.spaces_key")
	if err != nil {
		t.Fatalf("EnumerateAll: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("EnumerateAll returned %d outputs; want 2", len(out))
	}
	if out[0].Name != "spaces-key-1" || out[0].ProviderID != "key-aaaaa" {
		t.Errorf("EnumerateAll[0] mismatch: %+v", out[0])
	}
}

// ─── In-process gRPC test fixture ───────────────────────────────────────────

// ─── ADR-0029 capability-extension tests ─────────────────────────────────

// TestTypedAdapter_SupportedCanonicalKeys_PluginOverride exercises the
// regression closure: plugin declares a strict subset of canonical keys
// in CapabilitiesResponse, adapter returns those (not the wfctl-side
// default).
func TestTypedAdapter_SupportedCanonicalKeys_PluginOverride(t *testing.T) {
	provider := &fullStubProvider{
		name:          "do",
		version:       "v1.0.0",
		canonicalKeys: []string{"infra.spaces", "infra.spaces_key", "infra.droplet"},
	}
	srv, conn := startTestServer(t, provider, false)
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = conn.Close() })

	adapter := newTypedIaCAdapter(conn, nil)
	got := adapter.SupportedCanonicalKeys()
	want := []string{"infra.spaces", "infra.spaces_key", "infra.droplet"}
	if len(got) != len(want) {
		t.Fatalf("SupportedCanonicalKeys returned %d keys; want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SupportedCanonicalKeys[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

// TestTypedAdapter_SupportedCanonicalKeys_FallbackToDefault exercises
// the empty-canonical-keys path: adapter falls back to
// interfaces.CanonicalKeys() so plugins without an override work as
// before. Comparison is set-based since the underlying default's
// iteration order isn't guaranteed.
func TestTypedAdapter_SupportedCanonicalKeys_FallbackToDefault(t *testing.T) {
	provider := &fullStubProvider{name: "stub", version: "v0"} // no canonical_keys
	srv, conn := startTestServer(t, provider, false)
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = conn.Close() })

	adapter := newTypedIaCAdapter(conn, nil)
	got := adapter.SupportedCanonicalKeys()
	want := interfaces.CanonicalKeys()
	if len(got) != len(want) {
		t.Fatalf("SupportedCanonicalKeys returned %d keys; want %d (default fallback)", len(got), len(want))
	}
	wantSet := make(map[string]bool, len(want))
	for _, k := range want {
		wantSet[k] = true
	}
	for _, k := range got {
		if !wantSet[k] {
			t.Errorf("returned key %q not in interfaces.CanonicalKeys() default set", k)
		}
	}
}

// TestTypedAdapter_ComputePlanVersion_PluginDeclares verifies
// CapabilitiesResponse.compute_plan_version surfaces through the adapter
// for ComputePlanVersionDeclarer dispatch.
func TestTypedAdapter_ComputePlanVersion_PluginDeclares(t *testing.T) {
	provider := &fullStubProvider{name: "do", version: "v1.0.0", computePlanVersion: "v2"}
	srv, conn := startTestServer(t, provider, false)
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = conn.Close() })

	adapter := newTypedIaCAdapter(conn, nil)
	if got := adapter.ComputePlanVersion(); got != "v2" {
		t.Errorf("ComputePlanVersion = %q; want %q", got, "v2")
	}

	// DispatchVersionFor honors the declaration.
	if got := wfctlhelpers.DispatchVersionFor(adapter); got != "v2" {
		t.Errorf("DispatchVersionFor = %q; want %q", got, "v2")
	}
}

// TestTypedAdapter_ComputePlanVersion_EmptyMeansV1 verifies plugins that
// don't declare compute_plan_version get the legacy "v1" dispatch path
// via DispatchVersionFor's default-on-empty rule.
func TestTypedAdapter_ComputePlanVersion_EmptyMeansV1(t *testing.T) {
	provider := &fullStubProvider{name: "stub", version: "v0"} // no compute_plan_version
	srv, conn := startTestServer(t, provider, false)
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = conn.Close() })

	adapter := newTypedIaCAdapter(conn, nil)
	if got := adapter.ComputePlanVersion(); got != "" {
		t.Errorf("ComputePlanVersion = %q; want empty (no declaration)", got)
	}
	if got := wfctlhelpers.DispatchVersionFor(adapter); got != "v1" {
		t.Errorf("DispatchVersionFor = %q; want %q (empty → v1)", got, "v1")
	}
}

// TestTypedAdapter_CapabilitiesCacheReusedAcrossCalls verifies the
// CapabilitiesResponse is fetched at most once across repeated accessor
// calls (avoids RPC thrash on the dispatch hot path).
func TestTypedAdapter_CapabilitiesCacheReusedAcrossCalls(t *testing.T) {
	provider := &countingCapabilitiesProvider{computePlanVersion: "v2"}
	srv, conn := startTestServer(t, provider, false)
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = conn.Close() })

	adapter := newTypedIaCAdapter(conn, nil)
	for i := 0; i < 5; i++ {
		_ = adapter.ComputePlanVersion()
		_ = adapter.SupportedCanonicalKeys()
		_ = adapter.Capabilities()
	}
	if provider.calls != 1 {
		t.Errorf("Capabilities RPC called %d times; want 1 (cache miss after first call)", provider.calls)
	}
}

// countingCapabilitiesProvider counts Capabilities() RPC invocations to
// verify caching behavior.
type countingCapabilitiesProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	computePlanVersion string
	calls              int
}

func (p *countingCapabilitiesProvider) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	p.calls++
	return &pb.CapabilitiesResponse{ComputePlanVersion: p.computePlanVersion}, nil
}

// startTestServer spins up an in-process gRPC server registered with
// the supplied IaCProviderRequiredServer (and optionally the matching
// enumerator) on a localhost ephemeral port. Returns the server and a
// dial-back ClientConn the caller wraps in a typedIaCAdapter.
func startTestServer(t *testing.T, provider any, registerEnumerator bool) (*grpc.Server, *grpc.ClientConn) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	if req, ok := provider.(pb.IaCProviderRequiredServer); ok {
		pb.RegisterIaCProviderRequiredServer(srv, req)
	}
	if registerEnumerator {
		if e, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
			pb.RegisterIaCProviderEnumeratorServer(srv, e)
		}
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return srv, conn
}

// fullStubProvider satisfies pb.IaCProviderRequiredServer +
// pb.IaCProviderEnumeratorServer with canned responses for the
// end-to-end test. Embedding the Unimplemented servers means new RPCs
// added later don't break existing tests.
type fullStubProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer

	name               string
	version            string
	enumerated         []*pb.ResourceOutput
	canonicalKeys      []string // ADR-0029: empty = adapter falls back to interfaces.CanonicalKeys()
	computePlanVersion string   // ADR-0029: empty = adapter returns "" (DispatchVersionFor → "v1")
}

func (s *fullStubProvider) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	return &pb.CapabilitiesResponse{
		CanonicalKeys:      s.canonicalKeys,
		ComputePlanVersion: s.computePlanVersion,
	}, nil
}

func (s *fullStubProvider) Name(_ context.Context, _ *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: s.name}, nil
}

func (s *fullStubProvider) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: s.version}, nil
}

func (s *fullStubProvider) EnumerateAll(_ context.Context, _ *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {
	return &pb.EnumerateAllResponse{Outputs: s.enumerated}, nil
}

// enumeratorOnlyStub registers ONLY the enumerator service (via
// startTestServer's registerEnumerator=true gate). The Required
// service is intentionally absent so calling Plan exercises the
// codes.Unimplemented path.
type enumeratorOnlyStub struct {
	pb.UnimplementedIaCProviderEnumeratorServer
}

func (s *enumeratorOnlyStub) EnumerateAll(_ context.Context, _ *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {
	return &pb.EnumerateAllResponse{}, nil
}

// TestApplyResultFromPB_DecodesActions verifies applyResultFromPB
// translates pb.ActionResult entries into interfaces.ActionOutcome with
// the correct ActionStatus mapping and Error pass-through. Per workflow#640
// Phase 2 + ADR 0040; T3 of v2-lifecycle-phase2 plan.
func TestApplyResultFromPB_DecodesActions(t *testing.T) {
	pbResult := &pb.ApplyResult{
		PlanId: "plan-1",
		Actions: []*pb.ActionResult{
			{ActionIndex: 0, Status: pb.ActionStatus_ACTION_STATUS_SUCCESS},
			{ActionIndex: 1, Status: pb.ActionStatus_ACTION_STATUS_ERROR, Error: "create failed"},
			{ActionIndex: 2, Status: pb.ActionStatus_ACTION_STATUS_DELETE_FAILED, Error: "AWS API error"},
		},
	}
	got, err := applyResultFromPB(pbResult)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(got.Actions))
	}
	if got.Actions[0].ActionIndex != 0 || got.Actions[0].Status != interfaces.ActionStatusSuccess {
		t.Errorf("action 0: got %+v, want {0, Success}", got.Actions[0])
	}
	if got.Actions[1].Status != interfaces.ActionStatusError || got.Actions[1].Error != "create failed" {
		t.Errorf("action 1: got %+v, want {1, Error, \"create failed\"}", got.Actions[1])
	}
	if got.Actions[2].Status != interfaces.ActionStatusDeleteFailed || got.Actions[2].Error != "AWS API error" {
		t.Errorf("action 2: got %+v, want {2, DeleteFailed, \"AWS API error\"}", got.Actions[2])
	}
}

// TestApplyResultFromPB_RejectsUNSPECIFIED ensures a plugin sending
// ACTION_STATUS_UNSPECIFIED gets rejected at the decode boundary so
// wfctl never tries to dispatch a v2 hook on a forgotten-populate
// outcome. Per ADR 0040 invariant 2: strict cutover, no graceful
// fallback. Error message MUST mention "UNSPECIFIED" + action_index.
func TestApplyResultFromPB_RejectsUNSPECIFIED(t *testing.T) {
	pbResult := &pb.ApplyResult{
		Actions: []*pb.ActionResult{
			{ActionIndex: 0, Status: pb.ActionStatus_ACTION_STATUS_SUCCESS},
			{ActionIndex: 7, Status: pb.ActionStatus_ACTION_STATUS_UNSPECIFIED},
		},
	}
	_, err := applyResultFromPB(pbResult)
	if err == nil {
		t.Fatal("expected error on UNSPECIFIED status, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "UNSPECIFIED") {
		t.Errorf("error should mention UNSPECIFIED: %v", err)
	}
	if !strings.Contains(msg, "7") {
		t.Errorf("error should mention offending action_index=7: %v", err)
	}
}

// TestApplyResultFromPB_EmptyActionsRoundTrip confirms plugins on the
// v1 capability shim (no Actions emitted) decode cleanly without
// error. Pins the slice contract explicitly: applyResultFromPB always
// returns a non-nil empty slice (via make([]T, 0, ...)) to match the
// sibling Resources/Errors fields' convention. A refactor that returns
// nil would change downstream nil-check semantics and fails this test.
func TestApplyResultFromPB_EmptyActionsRoundTrip(t *testing.T) {
	pbResult := &pb.ApplyResult{PlanId: "plan-empty"}
	got, err := applyResultFromPB(pbResult)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Actions == nil {
		t.Errorf("expected non-nil empty Actions slice, got nil")
	}
	if len(got.Actions) != 0 {
		t.Errorf("expected 0 actions for empty pb.Actions, got %d: %+v", len(got.Actions), got.Actions)
	}
}

// TestApplyResultFromPB_RejectsUnknownStatus exercises the wire-drift
// defense: a Phase 2.3+ plugin emitting a reserved tag (4 or 5) against
// an older wfctl must fail loud at decode rather than silently degrade
// to ActionStatusUnspecified. Per ADR 0040 invariant 2.
func TestApplyResultFromPB_RejectsUnknownStatus(t *testing.T) {
	pbResult := &pb.ApplyResult{
		Actions: []*pb.ActionResult{
			{ActionIndex: 0, Status: pb.ActionStatus_ACTION_STATUS_SUCCESS},
			{ActionIndex: 3, Status: pb.ActionStatus(99)},
		},
	}
	_, err := applyResultFromPB(pbResult)
	if err == nil {
		t.Fatal("expected error on unknown ActionStatus, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown ActionStatus=99") {
		t.Errorf("error should name the wire value: %v", err)
	}
	if !strings.Contains(msg, "action_index=3") {
		t.Errorf("error should name the offending action_index: %v", err)
	}
}

// TestMapPBActionStatusToInterface_KnownValues pins the four declared
// tags 0-3 to their interfaces.ActionStatus mirrors, ok=true.
func TestMapPBActionStatusToInterface_KnownValues(t *testing.T) {
	cases := []struct {
		name string
		in   pb.ActionStatus
		want interfaces.ActionStatus
	}{
		{"UNSPECIFIED", pb.ActionStatus_ACTION_STATUS_UNSPECIFIED, interfaces.ActionStatusUnspecified},
		{"SUCCESS", pb.ActionStatus_ACTION_STATUS_SUCCESS, interfaces.ActionStatusSuccess},
		{"ERROR", pb.ActionStatus_ACTION_STATUS_ERROR, interfaces.ActionStatusError},
		{"DELETE_FAILED", pb.ActionStatus_ACTION_STATUS_DELETE_FAILED, interfaces.ActionStatusDeleteFailed},
	}
	for _, c := range cases {
		got, ok := mapPBActionStatusToInterface(c.in)
		if !ok {
			t.Errorf("%s: ok=false, want true", c.name)
		}
		if got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// TestMapPBActionStatusToInterface_UnknownValueFailsClosed pins the
// fail-closed wire-drift defense at the helper level: any tag outside
// 0-3 returns (Unspecified, false). Per ADR 0040 invariant 2.
func TestMapPBActionStatusToInterface_UnknownValueFailsClosed(t *testing.T) {
	got, ok := mapPBActionStatusToInterface(pb.ActionStatus(99))
	if ok {
		t.Errorf("ok=true for unknown tag, want false")
	}
	if got != interfaces.ActionStatusUnspecified {
		t.Errorf("unknown tag mapped to %d, want ActionStatusUnspecified (0)", got)
	}
}
