package requirements

import (
	"context"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

func TestExternalRequirementDiscovery(t *testing.T) {
	client := &fakeRequirementDiscoveryClient{
		resp: &pb.DiscoverRequirementsResponse{
			Requirements: []*pb.IaCRequirement{{
				Key:  "observability.telemetry.default",
				Kind: pb.RequirementKind_REQUIREMENT_KIND_OBSERVABILITY,
			}},
		},
	}
	provider := ExternalDiscoveryProvider{
		Client:           client,
		ModuleConfigJSON: []byte(`{"backends":["otel"]}`),
	}

	reqs, err := provider.IaCRequirements(context.Background(), Input{Environment: "production"})
	if err != nil {
		t.Fatalf("IaCRequirements: %v", err)
	}
	if string(client.last.GetModuleConfigJson()) != `{"backends":["otel"]}` {
		t.Fatalf("module config json = %s", string(client.last.GetModuleConfigJson()))
	}
	if client.last.GetContext().GetEnvironment() != "production" {
		t.Fatalf("context environment = %q", client.last.GetContext().GetEnvironment())
	}
	assertHasRequirement(t, reqs, "observability.telemetry.default", KindObservability)
}

type fakeRequirementDiscoveryClient struct {
	pb.IaCRequirementDiscoveryClient
	last *pb.DiscoverRequirementsRequest
	resp *pb.DiscoverRequirementsResponse
	err  error
}

func (f *fakeRequirementDiscoveryClient) DiscoverRequirements(_ context.Context, req *pb.DiscoverRequirementsRequest, _ ...grpc.CallOption) (*pb.DiscoverRequirementsResponse, error) {
	f.last = req
	return f.resp, f.err
}
