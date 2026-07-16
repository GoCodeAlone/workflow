package sdk

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type containerRegistryTestProvider struct {
	declarations []*pb.ContainerRegistryDeclaration
	response     *pb.ContainerRegistryOperationResponse
	err          error
	calls        atomic.Int32
	request      any
}

func (p *containerRegistryTestProvider) ContainerRegistries() []*pb.ContainerRegistryDeclaration {
	return p.declarations
}

func (p *containerRegistryTestProvider) Login(_ context.Context, request *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error) {
	p.calls.Add(1)
	p.request = request
	mutateContainerRegistryRequest(request.GetRegistry())
	return p.response, p.err
}

func (p *containerRegistryTestProvider) Logout(_ context.Context, request *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error) {
	p.calls.Add(1)
	p.request = request
	mutateContainerRegistryRequest(request.GetRegistry())
	return p.response, p.err
}

func (p *containerRegistryTestProvider) Push(_ context.Context, request *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error) {
	p.calls.Add(1)
	p.request = request
	mutateContainerRegistryRequest(request.GetRegistry())
	request.ImageReference = "provider-mutated-image"
	return p.response, p.err
}

func (p *containerRegistryTestProvider) Prune(_ context.Context, request *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error) {
	p.calls.Add(1)
	p.request = request
	mutateContainerRegistryRequest(request.GetRegistry())
	return p.response, p.err
}

func mutateContainerRegistryRequest(registry *pb.ContainerRegistryConfig) {
	if registry == nil {
		return
	}
	registry.Name = "provider-mutated"
	if registry.Auth != nil && registry.Auth.Vault != nil {
		registry.Auth.Vault.Address = "provider-mutated"
	}
	if registry.Retention != nil {
		registry.Retention.Schedule = "provider-mutated"
	}
}

func validContainerRegistryTestProvider() *containerRegistryTestProvider {
	return &containerRegistryTestProvider{
		declarations: []*pb.ContainerRegistryDeclaration{{
			Type: "registry.test", Operations: []string{"login", "logout", "push", "prune"},
		}},
		response: &pb.ContainerRegistryOperationResponse{
			Result: &pb.ContainerRegistryResult{Output: []byte("provider output")},
		},
	}
}

func TestContainerRegistryCanonicalizesAndClonesDeclarations(t *testing.T) {
	provider := validContainerRegistryTestProvider()
	provider.declarations = []*pb.ContainerRegistryDeclaration{
		{Type: " z.registry ", Operations: []string{" prune ", "push", " login ", "logout"}},
		{Type: "a.registry", Operations: []string{"logout", "login"}},
	}
	unknown := protowire.AppendTag(nil, 100, protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, []byte("provider-declaration-secret"))
	provider.declarations[0].ProtoReflect().SetUnknown(unknown)
	server := newContainerRegistryServer(provider)
	if err := server.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	first, err := server.DescribeRegistries(context.Background(), &pb.ContainerRegistryDeclarationsRequest{})
	if err != nil || first.GetError() != nil {
		t.Fatalf("DescribeRegistries = %v, %v", first, err)
	}
	if len(first.GetRegistries()) != 2 || first.GetRegistries()[0].GetType() != "a.registry" || first.GetRegistries()[1].GetType() != "z.registry" {
		t.Fatalf("declaration order = %v", first.GetRegistries())
	}
	if !sameSDKStrings(first.GetRegistries()[1].GetOperations(), []string{"login", "logout", "push", "prune"}) {
		t.Fatalf("operation order = %v", first.GetRegistries()[1].GetOperations())
	}
	wire, marshalErr := proto.Marshal(first)
	if marshalErr != nil {
		t.Fatalf("marshal declarations: %v", marshalErr)
	}
	if bytes.Contains(wire, []byte("provider-declaration-secret")) {
		t.Fatalf("provider declaration unknown fields crossed response boundary: %x", wire)
	}
	for _, declaration := range first.GetRegistries() {
		if len(declaration.ProtoReflect().GetUnknown()) != 0 {
			t.Fatalf("declaration unknown fields crossed: %x", declaration.ProtoReflect().GetUnknown())
		}
	}
	first.Registries[0].Type = "caller-mutated"
	provider.declarations[0].Type = "provider-mutated"
	second, err := server.DescribeRegistries(context.Background(), &pb.ContainerRegistryDeclarationsRequest{})
	if err != nil || second.GetRegistries()[0].GetType() != "a.registry" || second.GetRegistries()[1].GetType() != "z.registry" {
		t.Fatalf("declarations were not defensively cloned: %v, %v", second, err)
	}
}

func TestContainerRegistryRejectsInvalidProvidersAndDeclarations(t *testing.T) {
	var typedNil *containerRegistryTestProvider
	tests := []struct {
		name     string
		provider ContainerRegistryProvider
		contains string
	}{
		{name: "nil", provider: nil, contains: "nil"},
		{name: "typed nil", provider: typedNil, contains: "nil"},
		{name: "none", provider: &containerRegistryTestProvider{}, contains: "at least one"},
		{name: "nil declaration", provider: containerRegistryProviderWithDeclarations(nil), contains: "nil"},
		{name: "empty type", provider: containerRegistryProviderWithDeclarations(&pb.ContainerRegistryDeclaration{Type: " ", Operations: []string{"login"}}), contains: "type"},
		{name: "duplicate canonical type", provider: containerRegistryProviderWithDeclarations(
			&pb.ContainerRegistryDeclaration{Type: "registry.test", Operations: []string{"login"}},
			&pb.ContainerRegistryDeclaration{Type: " registry.test ", Operations: []string{"logout"}},
		), contains: "duplicated"},
		{name: "no operations", provider: containerRegistryProviderWithDeclarations(&pb.ContainerRegistryDeclaration{Type: "registry.test"}), contains: "operation"},
		{name: "empty operation", provider: containerRegistryProviderWithDeclarations(&pb.ContainerRegistryDeclaration{Type: "registry.test", Operations: []string{" "}}), contains: "operation"},
		{name: "duplicate canonical operation", provider: containerRegistryProviderWithDeclarations(&pb.ContainerRegistryDeclaration{Type: "registry.test", Operations: []string{"login", " login "}}), contains: "duplicates"},
		{name: "unknown operation", provider: containerRegistryProviderWithDeclarations(&pb.ContainerRegistryDeclaration{Type: "registry.test", Operations: []string{"delete"}}), contains: "unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newContainerRegistryServer(test.provider)
			if err := server.validate(); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("validate = %v, want %q", err, test.contains)
			}
			response, err := server.DescribeRegistries(context.Background(), &pb.ContainerRegistryDeclarationsRequest{})
			if err != nil || response.GetError().GetCode() != "invalid_declaration" || len(response.GetRegistries()) != 0 {
				t.Fatalf("DescribeRegistries = %v, %v", response, err)
			}
		})
	}
}

func containerRegistryProviderWithDeclarations(declarations ...*pb.ContainerRegistryDeclaration) *containerRegistryTestProvider {
	return &containerRegistryTestProvider{declarations: declarations}
}

func TestContainerRegistryAllOperationsCloneRequestsAndResponses(t *testing.T) {
	registry := &pb.ContainerRegistryConfig{
		Name: "caller-owned", Type: "registry.test", Path: "team/project",
		Auth:      &pb.ContainerRegistryAuth{Vault: &pb.ContainerRegistryVaultAuth{Address: "caller-owned", Path: "secret/path"}},
		Retention: &pb.ContainerRegistryRetention{KeepLatest: 3, UntaggedTtl: "24h", Schedule: "caller-owned"},
	}
	for _, test := range []struct {
		name string
		call func(*containerRegistryServer) (*pb.ContainerRegistryOperationResponse, error)
	}{
		{name: "login", call: func(server *containerRegistryServer) (*pb.ContainerRegistryOperationResponse, error) {
			return server.Login(context.Background(), &pb.ContainerRegistryLoginRequest{Registry: registry, DryRun: true})
		}},
		{name: "logout", call: func(server *containerRegistryServer) (*pb.ContainerRegistryOperationResponse, error) {
			return server.Logout(context.Background(), &pb.ContainerRegistryLogoutRequest{Registry: registry, DryRun: true})
		}},
		{name: "push", call: func(server *containerRegistryServer) (*pb.ContainerRegistryOperationResponse, error) {
			return server.Push(context.Background(), &pb.ContainerRegistryPushRequest{Registry: registry, DryRun: true, ImageReference: "caller-owned-image"})
		}},
		{name: "prune", call: func(server *containerRegistryServer) (*pb.ContainerRegistryOperationResponse, error) {
			return server.Prune(context.Background(), &pb.ContainerRegistryPruneRequest{Registry: registry, DryRun: true})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := validContainerRegistryTestProvider()
			server := newContainerRegistryServer(provider)
			before := proto.Clone(registry).(*pb.ContainerRegistryConfig)
			response, err := test.call(server)
			if err != nil || response.GetError() != nil || string(response.GetResult().GetOutput()) != "provider output" {
				t.Fatalf("%s = %v, %v", test.name, response, err)
			}
			if !proto.Equal(registry, before) {
				t.Fatalf("provider mutated caller registry:\n got %v\nwant %v", registry, before)
			}
			response.Result.Output[0] = 'X'
			if string(provider.response.GetResult().GetOutput()) != "provider output" {
				t.Fatalf("caller mutated provider response: %q", provider.response.GetResult().GetOutput())
			}
		})
	}
}

func TestContainerRegistryRejectsMismatchAndUnsafeProviderResults(t *testing.T) {
	provider := validContainerRegistryTestProvider()
	server := newContainerRegistryServer(provider)

	missingRequest, err := server.Login(context.Background(), nil)
	if err != nil || missingRequest.GetError().GetCode() != "registry_type_required" || provider.calls.Load() != 0 {
		t.Fatalf("missing request = %v, %v; calls = %d", missingRequest, err, provider.calls.Load())
	}
	unsupportedType, err := server.Login(context.Background(), &pb.ContainerRegistryLoginRequest{Registry: &pb.ContainerRegistryConfig{Type: "registry.test "}})
	if err != nil || unsupportedType.GetError().GetCode() != "unsupported_registry_type" || provider.calls.Load() != 0 {
		t.Fatalf("unsupported type = %v, %v; calls = %d", unsupportedType, err, provider.calls.Load())
	}
	missingImage, err := server.Push(context.Background(), &pb.ContainerRegistryPushRequest{Registry: &pb.ContainerRegistryConfig{Type: "registry.test"}})
	if err != nil || missingImage.GetError().GetCode() != "image_reference_required" || provider.calls.Load() != 0 {
		t.Fatalf("missing image = %v, %v; calls = %d", missingImage, err, provider.calls.Load())
	}
	server = newContainerRegistryServer(&containerRegistryTestProvider{
		declarations: []*pb.ContainerRegistryDeclaration{{Type: "registry.test", Operations: []string{"login"}}},
	})
	unsupportedOperation, err := server.Push(context.Background(), &pb.ContainerRegistryPushRequest{Registry: &pb.ContainerRegistryConfig{Type: "registry.test"}, ImageReference: "image:v1"})
	if err != nil || unsupportedOperation.GetError().GetCode() != "unsupported_operation" || server.provider.(*containerRegistryTestProvider).calls.Load() != 0 {
		t.Fatalf("unsupported operation = %v, %v", unsupportedOperation, err)
	}

	for _, test := range []struct {
		name     string
		response *pb.ContainerRegistryOperationResponse
		err      error
		wantCode string
	}{
		{name: "plain error", err: errors.New("provider-secret"), wantCode: "provider_error"},
		{name: "structured error", response: &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{Output: []byte("provider-secret")}, Error: &pb.ContainerRegistryError{Code: "safe_code", Message: "provider-secret", Retryable: true}}, wantCode: "safe_code"},
		{name: "invalid structured error code", response: &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{Output: []byte("provider-secret")}, Error: &pb.ContainerRegistryError{Code: "BAD provider-secret", Message: "provider-secret", Retryable: true}}, wantCode: "provider_error"},
		{name: "nil response", wantCode: "empty_response"},
		{name: "missing result", response: &pb.ContainerRegistryOperationResponse{}, wantCode: "empty_response"},
		{name: "oversized output", response: &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{Output: make([]byte, maxContainerRegistryOutputBytes+1)}}, wantCode: "invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := validContainerRegistryTestProvider()
			provider.response = test.response
			provider.err = test.err
			response, err := newContainerRegistryServer(provider).Prune(context.Background(), &pb.ContainerRegistryPruneRequest{Registry: &pb.ContainerRegistryConfig{Type: "registry.test"}})
			if err != nil || response.GetError().GetCode() != test.wantCode || response.GetResult() != nil {
				t.Fatalf("Prune = %v, %v", response, err)
			}
			if strings.Contains(response.String(), "provider-secret") {
				t.Fatalf("provider error leaked: %s", response)
			}
		})
	}
}

func TestContainerRegistryContractAdvertisementIsCanonicalAndNonMutating(t *testing.T) {
	services := &providerServices{containerRegistry: newContainerRegistryServer(validContainerRegistryTestProvider())}
	base := &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.Unrelated", ContractType: "fixture.service"},
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: pb.ContainerRegistry_ServiceDesc.ServiceName, ContractType: "wrong.contract"},
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.WrongService", ContractType: ContainerRegistryContractID},
	}}
	original := proto.Clone(base).(*pb.ContractRegistry)
	merged := mergeProviderServiceContracts(base, services)
	if !proto.Equal(base, original) {
		t.Fatalf("base contract registry mutated:\n got %v\nwant %v", base, original)
	}
	var canonical *pb.ContractDescriptor
	for _, descriptor := range merged.GetContracts() {
		if descriptor.GetServiceName() == pb.ContainerRegistry_ServiceDesc.ServiceName || descriptor.GetContractType() == ContainerRegistryContractID {
			if canonical != nil {
				t.Fatalf("multiple container registry descriptors: %v", merged.GetContracts())
			}
			canonical = descriptor
		}
	}
	if canonical == nil || canonical.GetKind() != pb.ContractKind_CONTRACT_KIND_SERVICE ||
		canonical.GetMode() != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO ||
		canonical.GetServiceName() != pb.ContainerRegistry_ServiceDesc.ServiceName ||
		canonical.GetContractType() != ContainerRegistryContractID ||
		canonical.GetProtocolVersion() != ContainerRegistryProtocolVersion {
		t.Fatalf("canonical descriptor = %v", canonical)
	}
	if len(merged.GetContracts()) != 2 || merged.GetContracts()[0].GetContractType() != "fixture.service" {
		t.Fatalf("merged contracts = %v", merged.GetContracts())
	}
}

func TestContainerRegistryStripsProviderUnknownFields(t *testing.T) {
	unknown := protowire.AppendTag(nil, 100, protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, []byte("provider-unknown-secret"))
	for _, test := range []struct {
		name      string
		response  *pb.ContainerRegistryOperationResponse
		wantError string
	}{
		{
			name: "success",
			response: &pb.ContainerRegistryOperationResponse{
				Result: &pb.ContainerRegistryResult{Output: []byte("safe output")},
			},
		},
		{
			name: "error",
			response: &pb.ContainerRegistryOperationResponse{
				Result: &pb.ContainerRegistryResult{Output: []byte("provider-known-secret")},
				Error:  &pb.ContainerRegistryError{Code: "safe_code", Message: "provider-known-secret", Retryable: true},
			},
			wantError: "safe_code",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.response.ProtoReflect().SetUnknown(append([]byte(nil), unknown...))
			if test.response.GetResult() != nil {
				test.response.GetResult().ProtoReflect().SetUnknown(append([]byte(nil), unknown...))
			}
			if test.response.GetError() != nil {
				test.response.GetError().ProtoReflect().SetUnknown(append([]byte(nil), unknown...))
			}
			provider := validContainerRegistryTestProvider()
			provider.response = test.response
			response, err := newContainerRegistryServer(provider).Prune(context.Background(), &pb.ContainerRegistryPruneRequest{
				Registry: &pb.ContainerRegistryConfig{Type: "registry.test"},
			})
			if err != nil {
				t.Fatalf("Prune: %v", err)
			}
			wire, marshalErr := proto.Marshal(response)
			if marshalErr != nil {
				t.Fatalf("marshal response: %v", marshalErr)
			}
			if bytes.Contains(wire, []byte("provider-unknown-secret")) || bytes.Contains(wire, []byte("provider-known-secret")) {
				t.Fatalf("provider-controlled bytes crossed response boundary: %x", wire)
			}
			if len(response.ProtoReflect().GetUnknown()) != 0 {
				t.Fatalf("top-level unknown fields crossed: %x", response.ProtoReflect().GetUnknown())
			}
			if test.wantError == "" {
				if response.GetError() != nil || string(response.GetResult().GetOutput()) != "safe output" || len(response.GetResult().ProtoReflect().GetUnknown()) != 0 {
					t.Fatalf("success response = %v", response)
				}
				return
			}
			if response.GetResult() != nil || response.GetError().GetCode() != test.wantError || !response.GetError().GetRetryable() || len(response.GetError().ProtoReflect().GetUnknown()) != 0 {
				t.Fatalf("error response = %v", response)
			}
		})
	}
}

func TestContainerRegistryAcceptsOutputLimitWithIndependentCopy(t *testing.T) {
	provider := validContainerRegistryTestProvider()
	provider.response.Result.Output = bytes.Repeat([]byte{'a'}, maxContainerRegistryOutputBytes)
	response, err := newContainerRegistryServer(provider).Prune(context.Background(), &pb.ContainerRegistryPruneRequest{
		Registry: &pb.ContainerRegistryConfig{Type: "registry.test"},
	})
	if err != nil || response.GetError() != nil || len(response.GetResult().GetOutput()) != maxContainerRegistryOutputBytes {
		t.Fatalf("Prune at output limit = %v, %v", response, err)
	}
	provider.response.Result.Output[0] = 'b'
	if response.GetResult().GetOutput()[0] != 'a' {
		t.Fatal("provider retained caller response output storage")
	}
	response.Result.Output[1] = 'c'
	if provider.response.GetResult().GetOutput()[1] != 'a' {
		t.Fatal("caller retained provider response output storage")
	}
}

func sameSDKStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
