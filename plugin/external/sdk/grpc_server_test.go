package sdk

import (
	"context"
	"errors"
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

// --- minimal test providers ---

type minimalProvider struct{}

func (p *minimalProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Author:  "tester",
	}
}

// assetProvider embeds AssetProvider in a PluginProvider.
type assetProvider struct {
	minimalProvider
	assets map[string][]byte
}

func (p *assetProvider) GetAsset(path string) ([]byte, string, error) {
	data, ok := p.assets[path]
	if !ok {
		return nil, "", errors.New("asset not found: " + path)
	}
	ct := detectContentType(path)
	return data, ct, nil
}

// sampleProvider returns manifest with ConfigMutable and SampleCategory set.
type sampleProvider struct{}

func (p *sampleProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:           "sample-plugin",
		Version:        "1.0.0",
		Author:         "tester",
		ConfigMutable:  true,
		SampleCategory: "ecommerce",
	}
}

type contractProvider struct {
	minimalProvider
}

func (p *contractProvider) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{
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
}

// --- tests ---

func TestGetAsset_WithAssetProvider(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"index.html": []byte("<html>hello</html>"),
			"app.js":     []byte("console.log('hi')"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if string(resp.Content) != "<html>hello</html>" {
		t.Errorf("expected html content, got %q", resp.Content)
	}
	if resp.ContentType != "text/html" {
		t.Errorf("expected text/html content type, got %q", resp.ContentType)
	}
}

func TestGetAsset_JSMimeType(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"app.js": []byte("var x = 1;"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "app.js"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ContentType != "application/javascript" {
		t.Errorf("expected application/javascript, got %q", resp.ContentType)
	}
}

func TestGetAsset_AssetNotFound(t *testing.T) {
	provider := &assetProvider{assets: map[string][]byte{}}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "missing.txt"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error for missing asset, got empty error")
	}
}

func TestGetAsset_WithoutAssetProvider(t *testing.T) {
	srv := newGRPCServer(&minimalProvider{})

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error when AssetProvider not implemented")
	}
}

func TestGetManifest_NewFields(t *testing.T) {
	srv := newGRPCServer(&sampleProvider{})

	m, err := srv.GetManifest(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.ConfigMutable {
		t.Error("expected ConfigMutable=true")
	}
	if m.SampleCategory != "ecommerce" {
		t.Errorf("expected SampleCategory=ecommerce, got %q", m.SampleCategory)
	}
}

func TestGetContractRegistry_WithProvider(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterPluginServiceServer(server, newGRPCServer(&contractProvider{}))
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := pb.NewPluginServiceClient(conn)
	registry, err := client.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(registry.Contracts) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(registry.Contracts))
	}
	descriptor := registry.Contracts[0]
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

// detectContentType maps common extensions to MIME types.
func detectContentType(path string) string {
	switch {
	case len(path) > 5 && path[len(path)-5:] == ".html":
		return "text/html"
	case len(path) > 4 && path[len(path)-4:] == ".css":
		return "text/css"
	case len(path) > 3 && path[len(path)-3:] == ".js":
		return "application/javascript"
	case len(path) > 4 && path[len(path)-4:] == ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}
