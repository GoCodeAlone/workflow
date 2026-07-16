package sdk

import (
	"fmt"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const providerServicesGoImportPath = "github.com/GoCodeAlone/workflow/plugin/external/proto"

// providerServices owns the optional provider-level transports registered by
// Serve options. Keeping registration and contract advertisement together
// prevents an implemented service from being served but undiscoverable.
type providerServices struct {
	credentialIssuer   *credentialIssuerServer
	credentialResolver *credentialResolverServer
	containerRegistry  *containerRegistryServer
	secretStore        *secretStoreServer
}

func (services *providerServices) register(server *grpc.Server) error {
	if services == nil {
		return nil
	}
	if services.credentialIssuer != nil {
		if err := services.credentialIssuer.validate(); err != nil {
			return fmt.Errorf("register credential issuer service: %w", err)
		}
		pb.RegisterCredentialIssuerServer(server, services.credentialIssuer)
	}
	if services.credentialResolver != nil {
		if err := services.credentialResolver.validate(); err != nil {
			return fmt.Errorf("register credential resolver service: %w", err)
		}
		pb.RegisterCredentialResolverServer(server, services.credentialResolver)
	}
	if services.containerRegistry != nil {
		if err := services.containerRegistry.validate(); err != nil {
			return fmt.Errorf("register container registry service: %w", err)
		}
		pb.RegisterContainerRegistryServer(server, services.containerRegistry)
	}
	if services.secretStore != nil {
		if err := services.secretStore.validate(); err != nil {
			return fmt.Errorf("register secret store service: %w", err)
		}
		pb.RegisterSecretStoreServer(server, services.secretStore)
	}
	return nil
}

func (services *providerServices) contractDescriptors() []*pb.ContractDescriptor {
	if services == nil {
		return nil
	}
	var descriptors []*pb.ContractDescriptor
	if services.credentialIssuer != nil && services.credentialIssuer.validate() == nil {
		descriptors = append(descriptors, &pb.ContractDescriptor{
			Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
			Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			ServiceName:     pb.CredentialIssuer_ServiceDesc.ServiceName,
			ContractType:    CredentialIssuerContractID,
			ProtoPackage:    "workflow.plugin.external.credentials",
			MessageNames:    []string{"CredentialSourceDeclaration", "CredentialIssueRequest", "CredentialIssueResponse", "CredentialListRequest", "CredentialListResponse", "CredentialDeleteRequest", "CredentialDeleteResponse"},
			GoImportPath:    providerServicesGoImportPath,
			ProtocolVersion: CredentialIssuerProtocolVersion,
		})
	}
	if services.credentialResolver != nil && services.credentialResolver.validate() == nil {
		descriptors = append(descriptors, &pb.ContractDescriptor{
			Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
			Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			ServiceName:     pb.CredentialResolver_ServiceDesc.ServiceName,
			ContractType:    CredentialResolverContractID,
			ProtoPackage:    "workflow.plugin.external.credentials",
			MessageNames:    []string{"CredentialResolverDeclaration", "CredentialResolveRequest", "CredentialResolveResponse", "ResolvedCloudCredentials", "CredentialResolutionError"},
			GoImportPath:    providerServicesGoImportPath,
			ProtocolVersion: CredentialResolverProtocolVersion,
		})
	}
	if services.containerRegistry != nil && services.containerRegistry.validate() == nil {
		descriptors = append(descriptors, &pb.ContractDescriptor{
			Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
			Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			ServiceName:     pb.ContainerRegistry_ServiceDesc.ServiceName,
			ContractType:    ContainerRegistryContractID,
			ProtoPackage:    "workflow.plugin.external.registry",
			MessageNames:    []string{"ContainerRegistryDeclaration", "ContainerRegistryConfig", "ContainerRegistryLoginRequest", "ContainerRegistryLogoutRequest", "ContainerRegistryPushRequest", "ContainerRegistryPruneRequest", "ContainerRegistryOperationResponse", "ContainerRegistryResult", "ContainerRegistryError"},
			GoImportPath:    providerServicesGoImportPath,
			ProtocolVersion: ContainerRegistryProtocolVersion,
		})
	}
	if services.secretStore != nil && services.secretStore.validate() == nil {
		descriptors = append(descriptors, &pb.ContractDescriptor{
			Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
			Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			ServiceName:     pb.SecretStore_ServiceDesc.ServiceName,
			ContractType:    SecretStoreContractID,
			ProtoPackage:    "workflow.plugin.external.secrets",
			MessageNames:    []string{"SecretStoreDeclaration", "SecretStoreTarget", "SecretStoreGetRequest", "SecretStoreGetResult", "SecretStoreGetResponse", "SecretStoreListRequest", "SecretStoreListResult", "SecretStoreListResponse", "SecretStoreStatAllRequest", "SecretStoreMetadata", "SecretStoreStatAllResult", "SecretStoreStatAllResponse", "SecretStoreCheckAccessRequest", "SecretStoreCheckAccessResponse", "SecretStoreError"},
			GoImportPath:    providerServicesGoImportPath,
			ProtocolVersion: SecretStoreProtocolVersion,
		})
	}
	return descriptors
}

func mergeProviderServiceContracts(base *pb.ContractRegistry, services *providerServices) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if base != nil {
		registry = proto.Clone(base).(*pb.ContractRegistry)
	}
	canonical := services.contractDescriptors()
	if len(canonical) == 0 {
		return registry
	}
	filtered := make([]*pb.ContractDescriptor, 0, len(registry.GetContracts())+len(canonical))
	for _, descriptor := range registry.GetContracts() {
		if providerServiceContractCollision(descriptor, canonical) {
			continue
		}
		filtered = append(filtered, descriptor)
	}
	filtered = append(filtered, canonical...)
	registry.Contracts = filtered
	return registry
}

func providerServiceContractCollision(descriptor *pb.ContractDescriptor, canonical []*pb.ContractDescriptor) bool {
	if descriptor == nil {
		return false
	}
	for _, expected := range canonical {
		if descriptor.GetServiceName() == expected.GetServiceName() {
			return true
		}
		if expected.GetContractType() != "" && descriptor.GetContractType() == expected.GetContractType() {
			return true
		}
	}
	return false
}
