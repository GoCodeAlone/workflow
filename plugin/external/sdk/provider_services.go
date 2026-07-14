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
	credentialIssuer *credentialIssuerServer
}

func (services *providerServices) register(server *grpc.Server) error {
	if services == nil || services.credentialIssuer == nil {
		return nil
	}
	if err := services.credentialIssuer.validate(); err != nil {
		return fmt.Errorf("register credential issuer service: %w", err)
	}
	pb.RegisterCredentialIssuerServer(server, services.credentialIssuer)
	return nil
}

func (services *providerServices) contractDescriptors() []*pb.ContractDescriptor {
	if services == nil || services.credentialIssuer == nil || services.credentialIssuer.validate() != nil {
		return nil
	}
	return []*pb.ContractDescriptor{{
		Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
		Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		ServiceName:     pb.CredentialIssuer_ServiceDesc.ServiceName,
		ContractType:    CredentialIssuerContractID,
		ProtoPackage:    "workflow.plugin.external.credentials",
		MessageNames:    []string{"CredentialSourceDeclaration", "CredentialIssueRequest", "CredentialIssueResponse", "CredentialListRequest", "CredentialListResponse", "CredentialDeleteRequest", "CredentialDeleteResponse"},
		GoImportPath:    providerServicesGoImportPath,
		ProtocolVersion: CredentialIssuerProtocolVersion,
	}}
}

func mergeProviderServiceContracts(base *pb.ContractRegistry, services *providerServices) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if base != nil {
		registry = proto.Clone(base).(*pb.ContractRegistry)
	}
	seen := make(map[string]struct{}, len(registry.GetContracts()))
	for _, descriptor := range registry.GetContracts() {
		if descriptor != nil && descriptor.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			seen[descriptor.GetServiceName()] = struct{}{}
		}
	}
	for _, descriptor := range services.contractDescriptors() {
		if _, exists := seen[descriptor.GetServiceName()]; exists {
			continue
		}
		registry.Contracts = append(registry.Contracts, descriptor)
	}
	return registry
}
