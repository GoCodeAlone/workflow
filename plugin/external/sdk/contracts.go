package sdk

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// MessageContract describes a descriptor-only protobuf message contract.
type MessageContract struct {
	ContractType    string
	ProtoPackage    string
	MessageNames    []string
	GoImportPath    string
	SchemaDigest    string
	ProtocolVersion string
}

// BuildMessageContractRegistry returns a registry containing MESSAGE-kind
// descriptors. Descriptor-only plugins can expose these contracts statically
// in plugin.contracts.json; runtime-backed plugins can return the same shape
// from ContractRegistry for parity tests.
func BuildMessageContractRegistry(contracts ...MessageContract) (*pb.ContractRegistry, error) {
	registry := &pb.ContractRegistry{}
	for _, contract := range contracts {
		descriptor, err := contractDescriptorForMessageContract(contract)
		if err != nil {
			return nil, err
		}
		registry.Contracts = append(registry.Contracts, descriptor)
	}
	return registry, nil
}

func contractDescriptorForMessageContract(contract MessageContract) (*pb.ContractDescriptor, error) {
	if strings.TrimSpace(contract.ContractType) == "" {
		return nil, fmt.Errorf("message contract type is required")
	}
	if strings.TrimSpace(contract.ProtoPackage) == "" {
		return nil, fmt.Errorf("message contract proto package is required")
	}
	if len(contract.MessageNames) == 0 {
		return nil, fmt.Errorf("message contract must declare at least one message")
	}
	if strings.TrimSpace(contract.SchemaDigest) == "" {
		return nil, fmt.Errorf("message contract schema digest is required")
	}
	if strings.TrimSpace(contract.ProtocolVersion) == "" {
		return nil, fmt.Errorf("message contract protocol version is required")
	}
	names := make([]string, 0, len(contract.MessageNames))
	for _, name := range contract.MessageNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("message contract contains empty message name")
		}
		names = append(names, name)
	}
	return &pb.ContractDescriptor{
		Kind:            pb.ContractKind_CONTRACT_KIND_MESSAGE,
		Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		ContractType:    contract.ContractType,
		ProtoPackage:    contract.ProtoPackage,
		MessageNames:    names,
		GoImportPath:    contract.GoImportPath,
		SchemaDigest:    contract.SchemaDigest,
		ProtocolVersion: contract.ProtocolVersion,
	}, nil
}

// BuildContractRegistry enumerates the gRPC services registered on
// grpcSrv and returns a *pb.ContractRegistry with a SERVICE-kind
// ContractDescriptor for each one. Mode is set to
// CONTRACT_MODE_STRICT_PROTO so the host can distinguish typed IaC
// services from the legacy structpb-mode contracts produced by
// Module/Step/Trigger ContractProvider implementations.
//
// Why this exists (per cycle 3 I-1 of the strict-contracts force-cutover
// design): wfctl needs a single mechanism to discover "is the optional
// service registered on this plugin handle?". Reusing the existing
// ContractRegistry shape keeps Module/Step/Trigger and IaC capability
// discovery on the same wire surface — no new server-reflection
// dependency required.
//
// The helper is safe to call with a nil server; it returns an empty
// (but non-nil) ContractRegistry. Service descriptors are emitted in a
// deterministic alphabetical order so callers can rely on stable
// FileDescriptorSet-adjacent output for diff/compare operations and
// the wftest BDD test in Task 15.
//
// IaC plugin authors typically wire this into their ContractProvider
// implementation:
//
//	func (p *plugin) ContractRegistry() *pb.ContractRegistry {
//	    return sdk.BuildContractRegistry(p.grpcServer)
//	}
//
// where p.grpcServer was captured inside the iacGRPCPlugin.GRPCServer
// callback at startup. The ContractProvider hook keeps the wfctl-side
// GetContractRegistry RPC path unchanged.
func BuildContractRegistry(grpcSrv *grpc.Server) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if grpcSrv == nil {
		return registry
	}
	info := grpcSrv.GetServiceInfo()
	names := make([]string, 0, len(info))
	for name := range info {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		registry.Contracts = append(registry.Contracts, &pb.ContractDescriptor{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: name,
			Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		})
	}
	return registry
}

// BuildContractRegistryForPlugin enumerates gRPC services registered on
// grpcSrv whose name STARTS WITH namespacePrefix and returns a
// *pb.ContractRegistry with one SERVICE-kind STRICT_PROTO ContractDescriptor
// per matching service. Filters out go-plugin infra services (PluginService,
// GRPCBroker, GRPCStdio, grpc.health.v1.Health) so downstream contract-diff
// (workflow#767) sees only plugin-owned services.
//
// Safe to call with nil server; returns empty (but non-nil) registry.
// Names alphabetically sorted for stable diff output.
//
// Typical caller: iacPluginServiceBridge.GetContractRegistry derives prefix
// from pb.IaCProviderRequired_ServiceDesc.ServiceName minus the ".IaCProviderRequired"
// suffix so the filter cannot drift from the .proto package declaration.
//
// BuildContractRegistry (full-surface, no filter) is retained for callers
// that want every registered service.
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespacePrefix string) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if grpcSrv == nil {
		return registry
	}
	info := grpcSrv.GetServiceInfo()
	names := make([]string, 0, len(info))
	for name := range info {
		if strings.HasPrefix(name, namespacePrefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		registry.Contracts = append(registry.Contracts, &pb.ContractDescriptor{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: name,
			Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		})
	}
	return registry
}
