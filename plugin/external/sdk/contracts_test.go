package sdk

import (
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

func TestBuildContractRegistryForPlugin_NilServer(t *testing.T) {
	reg := BuildContractRegistryForPlugin(nil, "workflow.plugin.external.iac.")
	if reg == nil {
		t.Fatal("want non-nil")
	}
	if len(reg.Contracts) != 0 {
		t.Errorf("want 0 contracts; got %d", len(reg.Contracts))
	}
}

func TestBuildContractRegistryForPlugin_FiltersByPrefix(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &stubPluginService{})
	go func() {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		_ = s.Serve(l)
	}()
	defer s.Stop()
	reg := BuildContractRegistryForPlugin(s, "workflow.plugin.external.iac.")
	if len(reg.Contracts) != 1 {
		t.Fatalf("want 1 contract (iac.IaCProviderRequired); got %d: %v", len(reg.Contracts), reg.Contracts)
	}
	if reg.Contracts[0].ServiceName != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("unexpected service: %s", reg.Contracts[0].ServiceName)
	}
}

type stubIaCRequired struct {
	pb.UnimplementedIaCProviderRequiredServer
}
type stubPluginService struct {
	pb.UnimplementedPluginServiceServer
}
