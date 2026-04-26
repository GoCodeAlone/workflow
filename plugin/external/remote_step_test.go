package external

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
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
func (c *stubPluginServiceClient) GetContractRegistry(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.ContractRegistry, error) {
	return &pb.ContractRegistry{}, nil
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
// not panic and sends a nil Config in the request (so the plugin receives nil,
// not an empty map).
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
	if stub.lastRequest.Config != nil {
		t.Errorf("expected nil Config for step with no config, got %v", stub.lastRequest.Config)
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

func TestRemoteStep_Execute_StrictContractSendsTypedPayloads(t *testing.T) {
	stub := &stubPluginServiceClient{
		response: &pb.ExecuteStepResponse{TypedOutput: mustAnyFromMapForTest(t, "workflow.plugin.v1.Manifest", map[string]any{
			"name":    "typed-output",
			"version": "v1",
		})},
	}
	contract := &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
		StepType:      "test.strict",
		ConfigMessage: "workflow.plugin.v1.Manifest",
		InputMessage:  "workflow.plugin.v1.Manifest",
		OutputMessage: "workflow.plugin.v1.Manifest",
		Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	}
	step := NewRemoteStep("test-step", "handle-strict", stub, map[string]any{
		"name":    "typed-config",
		"version": "v1",
	}, contract)

	pc := module.NewPipelineContext(map[string]any{
		"name":    "typed-input",
		"version": "v1",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if stub.lastRequest == nil {
		t.Fatal("expected ExecuteStep to be called")
	}
	if stub.lastRequest.Config != nil {
		t.Fatalf("expected strict step to omit legacy Config, got %v", stub.lastRequest.Config)
	}
	if stub.lastRequest.Current != nil {
		t.Fatalf("expected strict step to omit legacy Current, got %v", stub.lastRequest.Current)
	}
	assertAnyTypeForTest(t, stub.lastRequest.TypedConfig, "workflow.plugin.v1.Manifest")
	assertAnyTypeForTest(t, stub.lastRequest.TypedInput, "workflow.plugin.v1.Manifest")
	if result.Output["name"] != "typed-output" {
		t.Fatalf("expected typed output to be decoded, got %#v", result.Output)
	}
}

func TestRemoteStep_Execute_StrictContractFailsClosedWithoutCodec(t *testing.T) {
	stub := &stubPluginServiceClient{}
	contract := &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
		StepType:      "test.strict",
		ConfigMessage: "workflow.plugin.v1.DoesNotExist",
		InputMessage:  "workflow.plugin.v1.DoesNotExist",
		Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	}
	step := NewRemoteStep("test-step", "handle-strict", stub, map[string]any{
		"name": "legacy-only",
	}, contract)

	_, err := step.Execute(context.Background(), module.NewPipelineContext(map[string]any{"name": "legacy-only"}, nil))
	if err == nil {
		t.Fatal("expected strict step execution to fail without generated codec")
	}
	if !strings.Contains(err.Error(), "STRICT_PROTO") {
		t.Fatalf("expected strict failure to mention STRICT_PROTO, got %v", err)
	}
	if stub.lastRequest != nil {
		t.Fatal("expected strict failure before ExecuteStep RPC")
	}
}

func mustAnyFromMapForTest(t *testing.T, messageName string, values map[string]any) *anypb.Any {
	t.Helper()
	typed, err := mapToTypedAny(messageName, values, nil)
	if err != nil {
		t.Fatalf("mapToTypedAny(%s): %v", messageName, err)
	}
	return typed
}

func assertAnyTypeForTest(t *testing.T, got *anypb.Any, messageName string) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected typed Any for %s", messageName)
	}
	if !strings.HasSuffix(got.TypeUrl, "/"+messageName) {
		t.Fatalf("expected Any type %s, got %s", messageName, got.TypeUrl)
	}
	var msg pb.Manifest
	if err := got.UnmarshalTo(&msg); err != nil {
		t.Fatalf("unmarshal typed Any: %v", err)
	}
	if msg.Name == "" {
		raw, _ := protojson.Marshal(got)
		t.Fatalf("expected typed Any to contain manifest name, got %s", raw)
	}
}
