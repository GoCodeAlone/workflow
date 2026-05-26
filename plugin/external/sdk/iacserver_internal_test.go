package sdk

import (
	"context"
	"strings"
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
// Unimplemented sentinel. Uses a manifest that passes PluginManifest.Validate
// (Name/Version/Author/Description all populated) so it mirrors the production
// shape and asserts every mapped field — that way a regression in any of the
// four field copies fails this test.
func TestIaCBridgeGetManifestUsesProvider(t *testing.T) {
	disk := &pluginpkg.PluginManifest{
		Name:        "do",
		Version:     "1.0.12",
		Author:      "GoCodeAlone",
		Description: "DigitalOcean IaC plugin",
	}
	if err := disk.Validate(); err != nil {
		t.Fatalf("test fixture invalid: %v", err)
	}
	bridge := &iacPluginServiceBridge{
		grpcSrv:      grpc.NewServer(),
		diskManifest: disk,
	}
	got, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.GetName() != disk.Name {
		t.Fatalf("Name = %q, want %q", got.GetName(), disk.Name)
	}
	if got.GetVersion() != disk.Version {
		t.Fatalf("Version = %q, want %q", got.GetVersion(), disk.Version)
	}
	if got.GetAuthor() != disk.Author {
		t.Fatalf("Author = %q, want %q", got.GetAuthor(), disk.Author)
	}
	if got.GetDescription() != disk.Description {
		t.Fatalf("Description = %q, want %q", got.GetDescription(), disk.Description)
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

// TestIaCBridgeGetManifest_BuildVersionOverridesDiskVersion locks the
// single-channel precedence rule for workflow#758: when bridge.buildVersion
// is non-empty, it overrides diskManifest.Version in the response.
func TestIaCBridgeGetManifest_BuildVersionOverridesDiskVersion(t *testing.T) {
	disk := &pluginpkg.PluginManifest{
		Name: "do", Version: "1.0.0", Author: "GoCodeAlone", Description: "test",
	}
	bridge := &iacPluginServiceBridge{
		grpcSrv:      grpc.NewServer(),
		diskManifest: disk,
		buildVersion: "v1.2.3",
	}
	got, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.GetVersion() != "v1.2.3" {
		t.Errorf("Version = %q, want BuildVersion-augmented v1.2.3", got.GetVersion())
	}
	// Other fields still come from diskManifest.
	if got.GetName() != disk.Name {
		t.Errorf("Name = %q, want %q (other fields unchanged)", got.GetName(), disk.Name)
	}
}

// TestIaCBridgeGetManifest_BuildVersionOnlyNoDisk: when there's no
// diskManifest but BuildVersion is set, the bridge still returns a Manifest
// (not Unimplemented) carrying only the runtime version.
func TestIaCBridgeGetManifest_BuildVersionOnlyNoDisk(t *testing.T) {
	bridge := &iacPluginServiceBridge{
		grpcSrv:      grpc.NewServer(),
		buildVersion: "v2.0.0",
	}
	got, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.GetVersion() != "v2.0.0" {
		t.Errorf("Version = %q, want v2.0.0", got.GetVersion())
	}
}

// Compile-time guard: bridge must satisfy pb.PluginServiceServer.
var _ pb.PluginServiceServer = (*iacPluginServiceBridge)(nil)

func TestIaCBridge_ContractRegistry_FiltersInfra(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &iacPluginServiceBridge{grpcSrv: s})
	bridge := &iacPluginServiceBridge{grpcSrv: s}
	reg, err := bridge.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range reg.Contracts {
		if !strings.HasPrefix(c.ServiceName, "workflow.plugin.external.iac.") {
			t.Errorf("bridge surfaced non-iac service %q after filter", c.ServiceName)
		}
	}
	found := false
	for _, c := range reg.Contracts {
		if c.ServiceName == "workflow.plugin.external.iac.IaCProviderRequired" {
			found = true
		}
	}
	if !found {
		t.Error("expected IaCProviderRequired in filtered registry")
	}
}
