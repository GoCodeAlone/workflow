package external

import (
	"context"
	"errors"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newTestAdapter builds an ExternalPluginAdapter with a populated manifest
// and optional config fragment without a real gRPC connection.
func newTestAdapter(manifest *pb.Manifest, configFragment []byte) *ExternalPluginAdapter {
	return &ExternalPluginAdapter{
		name:           manifest.Name,
		manifest:       manifest,
		configFragment: configFragment,
	}
}

type adapterTestPluginServiceClient struct {
	stubPluginServiceClient
	manifest    *pb.Manifest
	registry    *pb.ContractRegistry
	registryErr error
}

func (c *adapterTestPluginServiceClient) GetManifest(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.Manifest, error) {
	return c.manifest, nil
}

func (c *adapterTestPluginServiceClient) GetContractRegistry(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.ContractRegistry, error) {
	if c.registryErr != nil {
		return nil, c.registryErr
	}
	return c.registry, nil
}

func TestIsSamplePlugin_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "my-sample",
		SampleCategory: "ecommerce",
	}, nil)
	if !a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=true when SampleCategory is set")
	}
}

func TestIsSamplePlugin_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "regular-plugin"}, nil)
	if a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=false when SampleCategory is empty")
	}
}

func TestIsConfigMutable_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:          "mutable-plugin",
		ConfigMutable: true,
	}, nil)
	if !a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=true")
	}
}

func TestIsConfigMutable_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "immutable-plugin"}, nil)
	if a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=false when not set")
	}
}

func TestSampleCategory(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "cat-plugin",
		SampleCategory: "analytics",
	}, nil)
	if a.SampleCategory() != "analytics" {
		t.Errorf("expected SampleCategory=analytics, got %q", a.SampleCategory())
	}
}

func TestConfigFragmentBytes(t *testing.T) {
	frag := []byte("modules:\n  - name: foo\n")
	a := newTestAdapter(&pb.Manifest{Name: "frag-plugin"}, frag)
	if string(a.ConfigFragmentBytes()) != string(frag) {
		t.Errorf("expected config fragment %q, got %q", frag, a.ConfigFragmentBytes())
	}
}

func TestConfigFragmentBytes_Nil(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "empty-plugin"}, nil)
	if a.ConfigFragmentBytes() != nil {
		t.Error("expected nil config fragment")
	}
}

func TestContractRegistry(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "test.echo",
				ConfigMessage: "workflow.plugins.test.v1.EchoConfig",
				InputMessage:  "workflow.plugins.test.v1.EchoInput",
				OutputMessage: "workflow.plugins.test.v1.EchoOutput",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest: &pb.Manifest{Name: "contract-plugin"},
		registry: registry,
	}})
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	got := a.ContractRegistry()
	if got == nil {
		t.Fatal("expected contract registry")
	}
	if len(got.Contracts) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(got.Contracts))
	}
	descriptor := got.Contracts[0]
	if descriptor.Kind != pb.ContractKind_CONTRACT_KIND_STEP {
		t.Errorf("expected step contract kind, got %v", descriptor.Kind)
	}
	if descriptor.StepType != "test.echo" {
		t.Errorf("expected step type test.echo, got %q", descriptor.StepType)
	}
	if descriptor.ConfigMessage != "workflow.plugins.test.v1.EchoConfig" {
		t.Errorf("expected config message, got %q", descriptor.ConfigMessage)
	}
	if descriptor.InputMessage != "workflow.plugins.test.v1.EchoInput" {
		t.Errorf("expected input message, got %q", descriptor.InputMessage)
	}
	if descriptor.OutputMessage != "workflow.plugins.test.v1.EchoOutput" {
		t.Errorf("expected output message, got %q", descriptor.OutputMessage)
	}
	if descriptor.Mode != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		t.Errorf("expected strict typed mode, got %v", descriptor.Mode)
	}
}

func TestContractRegistry_FetchErrorIsRecordedWithoutFailingAdapter(t *testing.T) {
	errBoom := errors.New("connection reset")
	a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest:    &pb.Manifest{Name: "legacy-plugin"},
		registryErr: errBoom,
	}})
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter should not fail on optional registry fetch: %v", err)
	}
	if a.ContractRegistry() != nil {
		t.Fatal("expected nil contract registry when fetch fails")
	}
	if !errors.Is(a.ContractRegistryError(), errBoom) {
		t.Fatalf("expected recorded registry error, got %v", a.ContractRegistryError())
	}
}

func TestContractRegistry_UnimplementedUsesEmptyRegistry(t *testing.T) {
	a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest:    &pb.Manifest{Name: "legacy-plugin"},
		registryErr: status.Error(codes.Unimplemented, "method GetContractRegistry not implemented"),
	}})
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter should not fail on unimplemented registry: %v", err)
	}
	if a.ContractRegistry() == nil {
		t.Fatal("expected empty registry for unimplemented registry RPC")
	}
	if len(a.ContractRegistry().Contracts) != 0 {
		t.Fatalf("expected no contracts for legacy plugin, got %d", len(a.ContractRegistry().Contracts))
	}
	if a.ContractRegistryError() != nil {
		t.Fatalf("expected no recorded error for unimplemented registry RPC, got %v", a.ContractRegistryError())
	}
}
