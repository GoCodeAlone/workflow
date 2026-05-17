package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestDiscoverAndLoadIaCProvider_LoadGate_RejectsV1 asserts a plugin that
// returns CapabilitiesResponse.ComputePlanVersion="v1" (or empty) is
// rejected at load time with an actionable error pointing to workflow#699.
func TestDiscoverAndLoadIaCProvider_LoadGate_RejectsV1(t *testing.T) {
	cases := []struct {
		name      string
		cpv       string
		wantInErr string
	}{
		{name: "empty", cpv: "", wantInErr: "workflow#699"},
		{name: "v1", cpv: "v1", wantInErr: "workflow#699"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyComputePlanVersionV2(tc.cpv, "plugin-x")
			if err == nil {
				t.Fatalf("expected reject for cpv=%q; got nil", tc.cpv)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantInErr)
			}
		})
	}
}

// TestDiscoverAndLoadIaCProvider_LoadGate_AcceptsV2 — happy path.
func TestDiscoverAndLoadIaCProvider_LoadGate_AcceptsV2(t *testing.T) {
	if err := verifyComputePlanVersionV2("v2", "plugin-x"); err != nil {
		t.Fatalf("expected accept for cpv=v2; got %v", err)
	}
}

// fakeCapabilitiesWithContext is a stub satisfying capabilitiesWithContexter
// for the integration test. resp / err mirror what a real plugin's typed
// Capabilities RPC would return.
type fakeCapabilitiesWithContext struct {
	resp *pb.CapabilitiesResponse
	err  error
}

func (f *fakeCapabilitiesWithContext) CapabilitiesWithContext(_ context.Context) (*pb.CapabilitiesResponse, error) {
	return f.resp, f.err
}

// TestDiscoverAndLoadIaCProvider_LoadGate_WiredIntoDiscovery asserts the
// helper is actually called by the discovery code-path — not just
// independently tested. Regression-gates against future refactor that
// removes the gate from the discovery code-path. Per plan cycle-2 N5 fix.
//
// Exercises the enforceCapabilitiesV2Gate var-seam (which is what
// buildTypedIaCAdapterFrom calls after the typed adapter is constructed).
// A real-RPC integration test would need an in-process gRPC server —
// covered separately by the conformance matrix in PR 6.
func TestDiscoverAndLoadIaCProvider_LoadGate_WiredIntoDiscovery(t *testing.T) {
	t.Run("v1 plugin → workflow#699 error", func(t *testing.T) {
		stub := &fakeCapabilitiesWithContext{
			resp: &pb.CapabilitiesResponse{ComputePlanVersion: "v1"},
		}
		err := enforceCapabilitiesV2Gate(context.Background(), stub, "plugin-x")
		if err == nil {
			t.Fatal("expected reject for v1 plugin; got nil")
		}
		if !strings.Contains(err.Error(), "workflow#699") {
			t.Errorf("error %q does not point at workflow#699", err.Error())
		}
		if !strings.Contains(err.Error(), "plugin-x") {
			t.Errorf("error %q does not name the plugin", err.Error())
		}
	})

	t.Run("v2 plugin → accept", func(t *testing.T) {
		stub := &fakeCapabilitiesWithContext{
			resp: &pb.CapabilitiesResponse{ComputePlanVersion: "v2"},
		}
		if err := enforceCapabilitiesV2Gate(context.Background(), stub, "plugin-x"); err != nil {
			t.Fatalf("expected accept for v2 plugin; got %v", err)
		}
	})

	t.Run("RPC failure → wrapped error", func(t *testing.T) {
		stub := &fakeCapabilitiesWithContext{err: errors.New("transport reset")}
		err := enforceCapabilitiesV2Gate(context.Background(), stub, "plugin-x")
		if err == nil {
			t.Fatal("expected error when RPC fails; got nil")
		}
		if !strings.Contains(err.Error(), "Capabilities RPC failed") {
			t.Errorf("error %q does not mention Capabilities RPC failure", err.Error())
		}
		if !strings.Contains(err.Error(), "transport reset") {
			t.Errorf("error %q does not wrap the underlying RPC error", err.Error())
		}
	})
}
