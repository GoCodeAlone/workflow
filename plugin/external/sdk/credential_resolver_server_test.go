package sdk

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type credentialResolverTestProvider struct {
	declarations []*pb.CredentialResolverDeclaration
	response     *pb.CredentialResolveResponse
	err          error
	calls        atomic.Int32
	resolve      func(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error)
}

func (p *credentialResolverTestProvider) CredentialResolvers() []*pb.CredentialResolverDeclaration {
	return p.declarations
}

func (p *credentialResolverTestProvider) Resolve(ctx context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	p.calls.Add(1)
	if p.resolve != nil {
		return p.resolve(ctx, request)
	}
	return p.response, p.err
}

func TestCredentialResolverPreservesFullOutputAndClonesProviderResponse(t *testing.T) {
	shared := &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: "aws", Region: "region", AccessKey: "access", SecretKey: "secret",
		SessionToken: "session", RoleArn: "role", ProjectId: "project",
		ServiceAccountJson: []byte("service-account"), TenantId: "tenant", ClientId: "client",
		ClientSecret: "client-secret", SubscriptionId: "subscription",
		Kubeconfig: []byte("kubeconfig"), Context: "context", Token: "token",
		Extra: map[string]string{"credential_source": "fixture"},
	}}
	provider := &credentialResolverTestProvider{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		response:     shared,
	}
	server := newCredentialResolverServer(provider)
	response, err := server.Resolve(context.Background(), &pb.CredentialResolveRequest{
		Provider: "aws", CredentialType: "static", ConfigJson: []byte(`{"secret":"request-secret"}`),
	})
	if err != nil || response.GetError() != nil {
		t.Fatalf("Resolve = %v, %v", response, err)
	}
	if !proto.Equal(response, shared) {
		t.Fatalf("full output changed:\n got %v\nwant %v", response, shared)
	}
	response.Credentials.Extra["credential_source"] = "changed"
	response.Credentials.ServiceAccountJson[0] = 'X'
	if shared.GetCredentials().GetExtra()["credential_source"] != "fixture" || string(shared.GetCredentials().GetServiceAccountJson()) != "service-account" {
		t.Fatalf("provider response was not deep-cloned: %v", shared)
	}
}

func TestCredentialResolverRejectsMismatchBeforeProvider(t *testing.T) {
	provider := &credentialResolverTestProvider{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
	}
	server := newCredentialResolverServer(provider)
	for _, request := range []*pb.CredentialResolveRequest{
		{Provider: "gcp", CredentialType: "static"},
		{Provider: "aws", CredentialType: "env"},
		{Provider: "aws", CredentialType: "static", ConfigJson: []byte(`{"broken"`)},
	} {
		response, err := server.Resolve(context.Background(), request)
		if err != nil {
			t.Fatalf("Resolve(%v): %v", request, err)
		}
		if response.GetError() == nil || response.GetCredentials() != nil {
			t.Fatalf("Resolve(%v) = %v, want sanitized error", request, response)
		}
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider called %d times for invalid selections", provider.calls.Load())
	}
}

func TestCredentialResolverRejectsNonProviderOwnedDeclarations(t *testing.T) {
	for _, test := range []struct {
		name        string
		provider    string
		credentials []string
	}{
		{name: "mock remains core local", provider: "mock", credentials: []string{"static"}},
		{name: "kubernetes remains core local", provider: "kubernetes", credentials: []string{"static"}},
		{name: "unknown provider", provider: "unsupported", credentials: []string{"static"}},
		{name: "unknown aws type", provider: "aws", credentials: []string{"application_default"}},
		{name: "unknown gcp type", provider: "gcp", credentials: []string{"profile"}},
		{name: "unknown azure type", provider: "azure", credentials: []string{"role_arn"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &credentialResolverTestProvider{declarations: []*pb.CredentialResolverDeclaration{{
				Provider: test.provider, CredentialTypes: test.credentials,
			}}}
			server := newCredentialResolverServer(provider)
			if server.validate() == nil {
				t.Fatalf("declaration unexpectedly accepted: %v", provider.declarations)
			}
			response, err := server.DescribeResolvers(context.Background(), &pb.CredentialResolverDeclarationsRequest{})
			if err != nil || response.GetError().GetCode() != "invalid_declaration" || len(response.GetResolvers()) != 0 {
				t.Fatalf("DescribeResolvers = %v, %v", response, err)
			}
		})
	}
}

func TestCredentialResolverRedactsProviderErrorsAndClearsPayload(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *pb.CredentialResolveResponse
		err      error
		wantCode string
	}{
		{
			name: "structured",
			response: &pb.CredentialResolveResponse{
				Credentials: &pb.ResolvedCloudCredentials{SecretKey: "structured-secret"},
				Error:       &pb.CredentialResolutionError{Code: "expired_token", Message: "structured-secret", Retryable: true},
			},
			wantCode: "expired_token",
		},
		{name: "plain", err: errors.New("plain-secret"), wantCode: "provider_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &credentialResolverTestProvider{
				declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
				response:     test.response,
				err:          test.err,
			}
			response, err := newCredentialResolverServer(provider).Resolve(context.Background(), &pb.CredentialResolveRequest{
				Provider: "aws", CredentialType: "static", ConfigJson: []byte(`{"token":"request-secret"}`),
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if response.GetError().GetCode() != test.wantCode || response.GetCredentials() != nil {
				t.Fatalf("Resolve = %v", response)
			}
			serialized := response.String()
			for _, forbidden := range []string{"structured-secret", "plain-secret", "request-secret"} {
				if strings.Contains(serialized, forbidden) {
					t.Fatalf("response leaked %q: %s", forbidden, serialized)
				}
			}
		})
	}
}

func TestCredentialResolverCancellationReachesProvider(t *testing.T) {
	provider := &credentialResolverTestProvider{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		resolve: func(ctx context.Context, _ *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newCredentialResolverServer(provider).Resolve(ctx, &pb.CredentialResolveRequest{Provider: "aws", CredentialType: "static"})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("Resolve cancellation = %v, want Canceled", err)
	}
}

func TestCredentialResolverDescribeAndCanonicalRegistryMerge(t *testing.T) {
	provider := &credentialResolverTestProvider{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static", "env"}}},
	}
	services := &providerServices{credentialResolver: newCredentialResolverServer(provider)}
	described, err := services.credentialResolver.DescribeResolvers(context.Background(), &pb.CredentialResolverDeclarationsRequest{})
	if err != nil || described.GetError() != nil || len(described.GetResolvers()) != 1 {
		t.Fatalf("DescribeResolvers = %v, %v", described, err)
	}
	described.Resolvers[0].CredentialTypes[0] = "changed"
	if provider.declarations[0].GetCredentialTypes()[0] != "static" {
		t.Fatalf("DescribeResolvers mutated provider declaration: %v", provider.declarations)
	}

	base := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.Unrelated", ContractType: "fixture.unrelated"},
			{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: pb.CredentialResolver_ServiceDesc.ServiceName, ContractType: "stale"},
			{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.Wrong", ContractType: CredentialResolverContractID},
		},
		FileDescriptorSet: &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("fixture.proto")}}},
	}
	original := proto.Clone(base).(*pb.ContractRegistry)
	merged := mergeProviderServiceContracts(base, services)
	if !proto.Equal(base, original) || !proto.Equal(merged.GetFileDescriptorSet(), base.GetFileDescriptorSet()) {
		t.Fatalf("registry merge mutated or lost base metadata: base=%v merged=%v", base, merged)
	}
	var canonical, unrelated int
	for _, descriptor := range merged.GetContracts() {
		if descriptor.GetServiceName() == pb.CredentialResolver_ServiceDesc.ServiceName || descriptor.GetContractType() == CredentialResolverContractID {
			canonical++
			if descriptor.GetProtocolVersion() != CredentialResolverProtocolVersion {
				t.Errorf("non-canonical resolver descriptor: %v", descriptor)
			}
		}
		if descriptor.GetContractType() == "fixture.unrelated" {
			unrelated++
		}
	}
	if canonical != 1 || unrelated != 1 {
		t.Fatalf("merged contracts = %v", merged.GetContracts())
	}
}
