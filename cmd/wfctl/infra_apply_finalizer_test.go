package main

// infra_apply_finalizer_test.go — branch-coverage tests for the
// OnPlanComplete closure built by statePersistenceHooks (workflow#695
// Phase 2.5). Locks the wfctl-side wiring contract that the engine-side
// apply.go deferred handler relies on:
//
//   - Branch A: provider is not a *typedIaCAdapter → no-op nil
//     (backward compat — in-process fakes, legacy provider shapes).
//   - Branch B: adapter.Finalizer() returns nil → no-op nil
//     (ADR 0024 negative signal — plugin did not register
//     IaCProviderFinalizer; no compat shim).
//   - Branch C: FinalizeApply RPC returns a transport error →
//     wrapped as "FinalizeApply gRPC: %w" (errors.Is round-trips).
//   - Branch D: response has errors[] → aggregated into the
//     consumer-visible format string
//     "plugin finalize: N driver(s) failed: <res>/<act>: <err>; ..."
//     (ADR 0040 per-driver attribution preservation).
//   - Branch E: success path (no errors) → nil.
//
// The error-format string in Branch D is a load-bearing wire-visible
// contract — downstream code in apply.go's deferred handler renders it
// into result.Errors as the "<plan-finalize>" entry; operator-facing
// diagnostic format would silently drift if rewritten. Locked here.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// stubFinalizerServer satisfies pb.IaCProviderFinalizerServer with a
// caller-provided handler. nil handler → empty response (Branch E success).
type stubFinalizerServer struct {
	pb.UnimplementedIaCProviderFinalizerServer
	handler func(*pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error)
}

func (s *stubFinalizerServer) FinalizeApply(_ context.Context, req *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
	if s.handler == nil {
		return &pb.FinalizeApplyResponse{}, nil
	}
	return s.handler(req)
}

// requiredFinalizerStub satisfies pb.IaCProviderRequiredServer with the
// Unimplemented embed — the in-process gRPC server needs Required
// registered for the adapter's RequiredClient construction; no RPC
// against Required fires in these tests, so unimplemented is fine.
// Local to this file because the sdk_test package's analogous
// fullProviderStub lives in plugin/external/sdk and isn't reachable
// from cmd/wfctl/main.
type requiredFinalizerStub struct {
	pb.UnimplementedIaCProviderRequiredServer
}

// newFinalizerAdapter spins up an in-process gRPC server that registers
// stubFinalizerServer (with the supplied handler) plus the Required stub,
// dials it, and returns a *typedIaCAdapter with iacServiceFinalizer in
// its registered set so adapter.Finalizer() returns a live client.
// Cleanup drains the server + conn.
func newFinalizerAdapter(t *testing.T, handler func(*pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error)) *typedIaCAdapter {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	// Register only the bare minimum: Required (so RequiredClient
	// construction works) + Finalizer (the unit under test). No other
	// optional services — keeps the surface minimal.
	pb.RegisterIaCProviderRequiredServer(srv, &requiredFinalizerStub{})
	pb.RegisterIaCProviderFinalizerServer(srv, &stubFinalizerServer{handler: handler})
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
	})
	return newTypedIaCAdapter(conn, map[string]bool{
		iacServiceFinalizer: true,
	})
}

// TestStatePersistenceHooks_OnPlanComplete_NonAdapterProviderNoOps
// covers Branch A: provider that doesn't type-assert to *typedIaCAdapter
// → closure returns nil (backward compat for in-process fakes and any
// future non-adapter IaCProvider shapes). Locks the pre-Phase-2.5
// behavior preservation for plugins that don't go through wfctl's
// typed adapter.
func TestStatePersistenceHooks_OnPlanComplete_NonAdapterProviderNoOps(t *testing.T) {
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		inProcessFakeProvider{}, // satisfies interfaces.IaCProvider but is NOT *typedIaCAdapter
		"do",
		"plan-id-noop",
		nil,
	)
	if hooks.OnPlanComplete == nil {
		t.Fatal("OnPlanComplete closure must be wired even for non-adapter providers")
	}
	if err := hooks.OnPlanComplete(t.Context()); err != nil {
		t.Errorf("non-adapter provider: expected nil err (no-op), got %v", err)
	}
}

// TestStatePersistenceHooks_OnPlanComplete_NilFinalizerNoOps covers
// Branch B: real *typedIaCAdapter but its Finalizer() returns nil
// because the plugin did not register IaCProviderFinalizer. Per
// ADR 0024 the absence of registration is the negative signal — closure
// silently no-ops so plugins that don't opt in preserve their pre-
// Phase-2.5 behavior.
func TestStatePersistenceHooks_OnPlanComplete_NilFinalizerNoOps(t *testing.T) {
	// Build an adapter WITHOUT iacServiceFinalizer in the registered
	// set so adapter.Finalizer() returns nil. dialLazyConn (defined in
	// iac_typed_adapter_test.go) gives us a real conn against an empty
	// in-process server.
	adapter := newTypedIaCAdapter(dialLazyConn(t), map[string]bool{
		// no iacServiceFinalizer entry
	})
	if adapter.Finalizer() != nil {
		t.Fatal("test fixture invariant: adapter.Finalizer() should be nil with empty registered set")
	}
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		adapter,
		"do",
		"plan-id-nofin",
		nil,
	)
	if err := hooks.OnPlanComplete(t.Context()); err != nil {
		t.Errorf("nil Finalizer: expected nil err (ADR 0024 negative-signal no-op), got %v", err)
	}
}

// TestStatePersistenceHooks_OnPlanComplete_GRPCTransportError covers
// Branch C: FinalizeApply RPC fails at the transport layer (e.g., a
// codes.Internal panic on the server side). Closure must wrap with
// "FinalizeApply gRPC: %w" so callers can both read the prefix AND
// recover the underlying gRPC status via the unwrap chain.
func TestStatePersistenceHooks_OnPlanComplete_GRPCTransportError(t *testing.T) {
	sentinel := status.Error(codes.Internal, "server blew up")
	adapter := newFinalizerAdapter(t, func(_ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
		return nil, sentinel
	})
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		adapter,
		"do",
		"plan-id-grpc-err",
		nil,
	)
	err := hooks.OnPlanComplete(t.Context())
	if err == nil {
		t.Fatal("expected wrapped gRPC transport error")
	}
	if !strings.HasPrefix(err.Error(), "FinalizeApply gRPC: ") {
		t.Errorf("expected 'FinalizeApply gRPC:' prefix; got: %v", err)
	}
	// Status round-trip — the wrap uses %w so status.FromError can recover
	// the underlying codes.Internal from the unwrap chain. Locks the
	// caller-side classification contract (retry/backoff/etc.).
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Internal {
		t.Errorf("expected status.FromError to recover codes.Internal; ok=%v code=%v", ok, st.Code())
	}
}

// TestStatePersistenceHooks_OnPlanComplete_AggregatesPerDriverErrors
// covers Branch D — THE load-bearing wire-visible error-format contract.
// FinalizeApplyResponse.errors[] carries per-driver attribution
// (Resource/Action/Error). Closure aggregates into a single err message
// preserving each entry's shape so the operator-facing diagnostic in
// result.Errors["<plan-finalize>"] keeps per-driver detail.
//
// Locked format: "plugin finalize: N driver(s) failed: R1/A1: E1; R2/A2: E2"
func TestStatePersistenceHooks_OnPlanComplete_AggregatesPerDriverErrors(t *testing.T) {
	adapter := newFinalizerAdapter(t, func(_ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
		return &pb.FinalizeApplyResponse{
			Errors: []*pb.ActionError{
				{Resource: "infra.database", Action: "deferred_update", Error: "trusted_sources flush failed: 504"},
				{Resource: "infra.spaces", Action: "deferred_update", Error: "cors update rejected"},
			},
		}, nil
	})
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		adapter,
		"do",
		"plan-id-agg",
		nil,
	)
	err := hooks.OnPlanComplete(t.Context())
	if err == nil {
		t.Fatal("expected aggregated err from response with errors[]")
	}
	got := err.Error()
	wantPrefix := "plugin finalize: 2 driver(s) failed: "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("expected prefix %q; got: %q", wantPrefix, got)
	}
	// Per-driver entries must appear with Resource/Action/Error in that
	// order — locks the format-string field ordering (ADR 0040 invariant).
	wantEntries := []string{
		"infra.database/deferred_update: trusted_sources flush failed: 504",
		"infra.spaces/deferred_update: cors update rejected",
	}
	for _, want := range wantEntries {
		if !strings.Contains(got, want) {
			t.Errorf("expected per-driver entry %q in err; got: %q", want, got)
		}
	}
	// Separator between entries — "; " keeps the aggregate parseable on
	// one line for log scrapers.
	if !strings.Contains(got, "; ") {
		t.Errorf("expected '; ' separator between per-driver entries; got: %q", got)
	}
}

// TestStatePersistenceHooks_OnPlanComplete_SuccessReturnsNil covers
// Branch E: the plugin's FinalizeApply succeeded with empty errors[]
// → closure returns nil (clean success exit). The engine-side defer
// then proceeds without appending a "<plan-finalize>" entry to
// result.Errors and the outer return remains (result, nil).
func TestStatePersistenceHooks_OnPlanComplete_SuccessReturnsNil(t *testing.T) {
	adapter := newFinalizerAdapter(t, func(req *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
		if req.GetPlanId() != "plan-id-success" {
			return nil, fmt.Errorf("unexpected plan_id: %q", req.GetPlanId())
		}
		return &pb.FinalizeApplyResponse{}, nil // empty errors[]
	})
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		adapter,
		"do",
		"plan-id-success",
		nil,
	)
	if err := hooks.OnPlanComplete(t.Context()); err != nil {
		t.Errorf("success path: expected nil err, got %v", err)
	}
}

// Sentinel sanity — guard against future refactors that drop the
// errors.Is round-trip on the gRPC wrap. (TestGRPCTransportError above
// asserts status.FromError; this one asserts the simpler errors.Is form
// callers may use for classification.)
func TestStatePersistenceHooks_OnPlanComplete_GRPCErrorPreservesErrorsIs(t *testing.T) {
	sentinel := status.Error(codes.Unavailable, "no route to host")
	adapter := newFinalizerAdapter(t, func(_ *pb.FinalizeApplyRequest) (*pb.FinalizeApplyResponse, error) {
		return nil, sentinel
	})
	hooks := statePersistenceHooks(
		&noopStateStore{},
		&fakeSecretsProvider{stored: map[string]string{}},
		adapter,
		"do",
		"plan-id-isstatus",
		nil,
	)
	err := hooks.OnPlanComplete(t.Context())
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected errors.Is(err, sentinel) to round-trip; err=%v", err)
	}
}
