package sdk

import (
	"sort"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

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
