package external

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// stubPluginServiceClient is a minimal PluginServiceClient that captures
// ExecuteStep requests for assertion in tests.
type stubPluginServiceClient struct {
	pb.UnimplementedPluginServiceServer // provides no-op implementations

	lastRequest *pb.ExecuteStepRequest
	response    *pb.ExecuteStepResponse
}

// ExecuteStep records the request and returns the configured response.
func (c *stubPluginServiceClient) ExecuteStep(_ context.Context, req *pb.ExecuteStepRequest, _ ...grpc.CallOption) (*pb.ExecuteStepResponse, error) {
	c.lastRequest = req
	if c.response != nil {
		return c.response, nil
	}
	return &pb.ExecuteStepResponse{Output: &structpb.Struct{}}, nil
}

// Implement the remaining PluginServiceClient interface methods as no-ops.
func (c *stubPluginServiceClient) GetManifest(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.Manifest, error) {
	return &pb.Manifest{}, nil
}
func (c *stubPluginServiceClient) GetModuleTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{}, nil
}
func (c *stubPluginServiceClient) GetStepTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{}, nil
}
func (c *stubPluginServiceClient) GetTriggerTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{}, nil
}
func (c *stubPluginServiceClient) GetModuleSchemas(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.ModuleSchemaList, error) {
	return &pb.ModuleSchemaList{}, nil
}
func (c *stubPluginServiceClient) CreateModule(_ context.Context, _ *pb.CreateModuleRequest, _ ...grpc.CallOption) (*pb.HandleResponse, error) {
	return &pb.HandleResponse{}, nil
}
func (c *stubPluginServiceClient) InitModule(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}
func (c *stubPluginServiceClient) StartModule(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}
func (c *stubPluginServiceClient) StopModule(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}
func (c *stubPluginServiceClient) DestroyModule(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}
func (c *stubPluginServiceClient) CreateStep(_ context.Context, _ *pb.CreateStepRequest, _ ...grpc.CallOption) (*pb.HandleResponse, error) {
	return &pb.HandleResponse{}, nil
}
func (c *stubPluginServiceClient) DestroyStep(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}
func (c *stubPluginServiceClient) InvokeService(_ context.Context, _ *pb.InvokeServiceRequest, _ ...grpc.CallOption) (*pb.InvokeServiceResponse, error) {
	return &pb.InvokeServiceResponse{}, nil
}
func (c *stubPluginServiceClient) DeliverMessage(_ context.Context, _ *pb.DeliverMessageRequest, _ ...grpc.CallOption) (*pb.DeliverMessageResponse, error) {
	return &pb.DeliverMessageResponse{}, nil
}
func (c *stubPluginServiceClient) GetConfigFragment(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.ConfigFragmentResponse, error) {
	return &pb.ConfigFragmentResponse{}, nil
}
func (c *stubPluginServiceClient) GetAsset(_ context.Context, _ *pb.GetAssetRequest, _ ...grpc.CallOption) (*pb.GetAssetResponse, error) {
	return &pb.GetAssetResponse{}, nil
}

// TestRemoteStep_Execute_ResolvesTemplatesInConfig verifies that template
// expressions in step config are resolved against the PipelineContext before
// the gRPC ExecuteStep call is made.
func TestRemoteStep_Execute_ResolvesTemplatesInConfig(t *testing.T) {
	stub := &stubPluginServiceClient{}
	cfg := map[string]any{
		"user_id": `{{ index .steps "fetch-user" "row" "id" }}`,
		"static":  "plain-value",
	}
	step := NewRemoteStep("test-step", "handle-1", stub, cfg)

	pc := module.NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch-user", map[string]any{
		"row": map[string]any{"id": "user-42"},
	})

	_, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stub.lastRequest == nil {
		t.Fatal("expected ExecuteStep to be called")
	}

	sent := stub.lastRequest.Config.AsMap()

	if sent["user_id"] != "user-42" {
		t.Errorf("expected config user_id='user-42', got %q", sent["user_id"])
	}
	if sent["static"] != "plain-value" {
		t.Errorf("expected config static='plain-value', got %q", sent["static"])
	}
}

// TestRemoteStep_Execute_NilConfig verifies that a step with no config does
// not panic and sends an empty (or nil) config in the request.
func TestRemoteStep_Execute_NilConfig(t *testing.T) {
	stub := &stubPluginServiceClient{}
	step := NewRemoteStep("test-step", "handle-2", stub, nil)

	pc := module.NewPipelineContext(nil, nil)

	_, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.lastRequest == nil {
		t.Fatal("expected ExecuteStep to be called")
	}
}

// TestRemoteStep_Execute_StaticConfigPassthrough verifies that a config with
// no template expressions is passed through unmodified.
func TestRemoteStep_Execute_StaticConfigPassthrough(t *testing.T) {
	stub := &stubPluginServiceClient{}
	cfg := map[string]any{
		"endpoint": "https://api.example.com",
		"timeout":  float64(30),
	}
	step := NewRemoteStep("test-step", "handle-3", stub, cfg)

	_, err := step.Execute(context.Background(), module.NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sent := stub.lastRequest.Config.AsMap()
	if sent["endpoint"] != "https://api.example.com" {
		t.Errorf("expected endpoint='https://api.example.com', got %q", sent["endpoint"])
	}
	if sent["timeout"] != float64(30) {
		t.Errorf("expected timeout=30, got %v", sent["timeout"])
	}
}
