package sdk

import (
	"context"
	"testing"

	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Internal test package — `package sdk`, NOT `sdk_test`. The bridge type and
// its diskManifest field are unexported; the parallel external test file
// `iacserver_test.go` lives in `package sdk_test` and cannot reach them.
// Per workflow plan F R2-4.

// TestIaCBridgeGetManifestUsesProvider locks in the precedence rule for the
// IaC bridge: when IaCServeOptions.ManifestProvider populated the bridge's
// diskManifest, GetManifest returns it as a *pb.Manifest instead of the
// Unimplemented sentinel.
func TestIaCBridgeGetManifestUsesProvider(t *testing.T) {
	disk := &pluginpkg.PluginManifest{Name: "do", Version: "1.0.12", Description: "DO IaC"}
	bridge := &iacPluginServiceBridge{
		grpcSrv:      grpc.NewServer(),
		diskManifest: disk,
	}
	got, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Version != "1.0.12" {
		t.Fatalf("Version = %q, want 1.0.12", got.Version)
	}
}

// TestIaCBridgeGetManifestUnimplementedWhenNoProvider covers the no-manifest
// path: bridge returns codes.Unimplemented so the engine's manager-side
// disk-fallback (Task 1) takes over.
func TestIaCBridgeGetManifestUnimplementedWhenNoProvider(t *testing.T) {
	bridge := &iacPluginServiceBridge{grpcSrv: grpc.NewServer()}
	_, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
	if err == nil {
		t.Fatalf("GetManifest: want Unimplemented error, got nil")
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("status.Code = %v, want Unimplemented", status.Code(err))
	}
}

// Compile-time guard: bridge must satisfy pb.PluginServiceServer.
var _ pb.PluginServiceServer = (*iacPluginServiceBridge)(nil)
