package sdk

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestContractPathMapRuntimeProtocols(t *testing.T) {
	supportedCredentialResolverTypes["protocol.test"] = credentialResolverStringSet("static")
	t.Cleanup(func() {
		delete(supportedCredentialResolverTypes, "protocol.test")
	})

	services := &providerServices{
		credentialIssuer:   newCredentialIssuerServer(protocolCredentialIssuer{}),
		credentialResolver: newCredentialResolverServer(protocolCredentialResolver{}),
		containerRegistry:  newContainerRegistryServer(protocolContainerRegistry{}),
		secretStore:        newSecretStoreServer(protocolSecretStore{}),
	}
	actual := make(map[string]int)
	for _, descriptor := range services.contractDescriptors() {
		protocol, err := strconv.Atoi(descriptor.GetProtocolVersion())
		if err != nil || protocol < 1 {
			t.Fatalf("runtime contract %q has invalid protocol %q", descriptor.GetContractType(), descriptor.GetProtocolVersion())
		}
		if _, duplicate := actual[descriptor.GetContractType()]; duplicate {
			t.Fatalf("runtime contract %q is duplicated", descriptor.GetContractType())
		}
		actual[descriptor.GetContractType()] = protocol
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("encode runtime contract protocols: %v", err)
	}
	t.Logf("WORKFLOW_CONTRACT_PROTOCOLS=%s", encoded)
}

type protocolCredentialIssuer struct{}

func (protocolCredentialIssuer) CredentialSources() []*pb.CredentialSourceDeclaration {
	return []*pb.CredentialSourceDeclaration{{
		Source:          "protocol.test",
		ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
		Outputs:         []*pb.CredentialOutputDeclaration{{Key: "id"}},
		IdentifierKey:   "id",
	}}
}

func (protocolCredentialIssuer) Issue(context.Context, *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error) {
	return nil, nil
}

func (protocolCredentialIssuer) List(context.Context, *pb.CredentialListRequest) (*pb.CredentialListResponse, error) {
	return nil, nil
}

func (protocolCredentialIssuer) Delete(context.Context, *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error) {
	return nil, nil
}

type protocolCredentialResolver struct{}

func (protocolCredentialResolver) CredentialResolvers() []*pb.CredentialResolverDeclaration {
	return []*pb.CredentialResolverDeclaration{{Provider: "protocol.test", CredentialTypes: []string{"static"}}}
}

func (protocolCredentialResolver) Resolve(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	return nil, nil
}

type protocolContainerRegistry struct{}

func (protocolContainerRegistry) ContainerRegistries() []*pb.ContainerRegistryDeclaration {
	return []*pb.ContainerRegistryDeclaration{{Type: "protocol.test", Operations: []string{"login"}}}
}

func (protocolContainerRegistry) Login(context.Context, *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return nil, nil
}

func (protocolContainerRegistry) Logout(context.Context, *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return nil, nil
}

func (protocolContainerRegistry) Push(context.Context, *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return nil, nil
}

func (protocolContainerRegistry) Prune(context.Context, *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return nil, nil
}

type protocolSecretStore struct{}

func (protocolSecretStore) SecretStores() []*pb.SecretStoreDeclaration {
	return []*pb.SecretStoreDeclaration{{Type: "protocol.test", Operations: []string{"get"}, Scopes: []string{"project"}}}
}

func (protocolSecretStore) Get(context.Context, *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error) {
	return nil, nil
}

func (protocolSecretStore) List(context.Context, *pb.SecretStoreListRequest) (*pb.SecretStoreListResponse, error) {
	return nil, nil
}

func (protocolSecretStore) StatAll(context.Context, *pb.SecretStoreStatAllRequest) (*pb.SecretStoreStatAllResponse, error) {
	return nil, nil
}

func (protocolSecretStore) CheckAccess(context.Context, *pb.SecretStoreCheckAccessRequest) (*pb.SecretStoreCheckAccessResponse, error) {
	return nil, nil
}
