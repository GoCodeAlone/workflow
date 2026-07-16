package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

var Version = "dev"

//go:embed plugin.json
var manifestData []byte

type provider struct{}

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name: "verify-provider", Version: "0.0.0", Author: "test fixture",
		Description: "Provider capability declarations match runtime services",
	}
}

func (*provider) CredentialSources() []*pb.CredentialSourceDeclaration {
	if os.Getenv("WFCTL_TEST_PROVIDER_DECLARATION_ERROR") == "1" {
		fmt.Fprintln(os.Stderr, "SENTINEL_PROVIDER_STDERR_SECRET")
		return []*pb.CredentialSourceDeclaration{{}}
	}
	return []*pb.CredentialSourceDeclaration{{
		Source:          "example.source",
		ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
		Outputs: []*pb.CredentialOutputDeclaration{
			{Key: "identifier", Sensitive: false},
			{Key: "secret", Sensitive: true},
		},
		IdentifierKey: "identifier",
	}}
}

func (*provider) Issue(context.Context, *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error) {
	return &pb.CredentialIssueResponse{
		Outputs: []*pb.CredentialOutput{
			{Key: "identifier", Value: []byte("fixture-id"), Sensitive: false},
			{Key: "secret", Value: []byte("fixture-secret"), Sensitive: true},
		},
		Identifier: "fixture-id", IdentifierSensitive: false,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}, nil
}

func (*provider) List(context.Context, *pb.CredentialListRequest) (*pb.CredentialListResponse, error) {
	return &pb.CredentialListResponse{}, nil
}

func (*provider) Delete(_ context.Context, request *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error) {
	return &pb.CredentialDeleteResponse{
		Identifier:          request.GetIdentifier(),
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}, nil
}

func (*provider) CredentialResolvers() []*pb.CredentialResolverDeclaration {
	return []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}}
}

func (*provider) Resolve(_ context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(), AccessKey: "fixture-resolver-access", SecretKey: "fixture-resolver-secret",
	}}, nil
}

func (*provider) ContainerRegistries() []*pb.ContainerRegistryDeclaration {
	return []*pb.ContainerRegistryDeclaration{{
		Type: "example-registry", Operations: []string{"login", "logout", "push", "prune"},
	}}
}

func (*provider) Login(context.Context, *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult(), nil
}

func (*provider) Logout(context.Context, *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult(), nil
}

func (*provider) Push(context.Context, *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult(), nil
}

func (*provider) Prune(context.Context, *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult(), nil
}

func registryResult() *pb.ContainerRegistryOperationResponse {
	return &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{}}
}

func main() {
	p := &provider{}
	sdk.Serve(p,
		sdk.WithManifestProvider(sdk.MustEmbedManifest(manifestData)),
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)),
		sdk.WithCredentialIssuerProvider(p),
		sdk.WithCredentialResolverProvider(p),
		sdk.WithContainerRegistryProvider(p),
	)
}
