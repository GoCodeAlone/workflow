package sdk

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// ContainerRegistryContractID is the manifest/runtime contract identifier.
	ContainerRegistryContractID = "workflow.provider.container-registry"
	// ContainerRegistryProtocolVersion is the typed transport version advertised
	// through the runtime ContractRegistry.
	ContainerRegistryProtocolVersion = "1"

	maxContainerRegistryOutputBytes = 1 << 20
)

var containerRegistryOperationOrder = []string{"login", "logout", "push", "prune"}

// ContainerRegistryProvider is the provider-owned implementation behind the
// optional ContainerRegistry gRPC service. ContainerRegistries is the
// authoritative runtime registry-type/operation declaration set.
type ContainerRegistryProvider interface {
	ContainerRegistries() []*pb.ContainerRegistryDeclaration
	Login(context.Context, *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error)
	Logout(context.Context, *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error)
	Push(context.Context, *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error)
	Prune(context.Context, *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error)
}

// WithContainerRegistryProvider registers provider as the optional, typed
// ContainerRegistry service. Legacy plugins that omit this option keep their
// existing PluginService-only surface.
func WithContainerRegistryProvider(provider ContainerRegistryProvider) ServeOption {
	return func(server *grpcServer) {
		server.providerServices.containerRegistry = newContainerRegistryServer(provider)
	}
}

type containerRegistryServer struct {
	pb.UnimplementedContainerRegistryServer

	provider   ContainerRegistryProvider
	registries map[string]map[string]struct{}
	ordered    []*pb.ContainerRegistryDeclaration
	err        error
}

func newContainerRegistryServer(provider ContainerRegistryProvider) *containerRegistryServer {
	server := &containerRegistryServer{provider: provider}
	if provider == nil || isTypedNil(provider) {
		server.err = fmt.Errorf("container registry provider is nil")
		return server
	}
	server.registries, server.ordered, server.err = validateContainerRegistryDeclarations(provider.ContainerRegistries())
	return server
}

func validateContainerRegistryDeclarations(declarations []*pb.ContainerRegistryDeclaration) (map[string]map[string]struct{}, []*pb.ContainerRegistryDeclaration, error) {
	if len(declarations) == 0 {
		return nil, nil, fmt.Errorf("container registry provider must declare at least one registry type")
	}
	registries := make(map[string]map[string]struct{}, len(declarations))
	ordered := make([]*pb.ContainerRegistryDeclaration, 0, len(declarations))
	allowedOperations := make(map[string]struct{}, len(containerRegistryOperationOrder))
	for _, operation := range containerRegistryOperationOrder {
		allowedOperations[operation] = struct{}{}
	}
	for _, declaration := range declarations {
		if declaration == nil {
			return nil, nil, fmt.Errorf("container registry declaration is nil")
		}
		registryType := strings.TrimSpace(declaration.GetType())
		if registryType == "" {
			return nil, nil, fmt.Errorf("container registry type is required")
		}
		if _, exists := registries[registryType]; exists {
			return nil, nil, fmt.Errorf("container registry type %q is duplicated", registryType)
		}
		if len(declaration.GetOperations()) == 0 {
			return nil, nil, fmt.Errorf("container registry type %q must declare at least one operation", registryType)
		}
		operations := make(map[string]struct{}, len(declaration.GetOperations()))
		for _, value := range declaration.GetOperations() {
			operation := strings.TrimSpace(value)
			if operation == "" {
				return nil, nil, fmt.Errorf("container registry type %q has an empty operation", registryType)
			}
			if _, supported := allowedOperations[operation]; !supported {
				return nil, nil, fmt.Errorf("container registry type %q has unsupported operation %q", registryType, operation)
			}
			if _, duplicate := operations[operation]; duplicate {
				return nil, nil, fmt.Errorf("container registry type %q duplicates operation %q", registryType, operation)
			}
			operations[operation] = struct{}{}
		}
		canonicalOperations := make([]string, 0, len(operations))
		for _, operation := range containerRegistryOperationOrder {
			if _, declared := operations[operation]; declared {
				canonicalOperations = append(canonicalOperations, operation)
			}
		}
		registries[registryType] = operations
		ordered = append(ordered, &pb.ContainerRegistryDeclaration{
			Type:       registryType,
			Operations: append([]string(nil), canonicalOperations...),
		})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].GetType() < ordered[j].GetType() })
	return registries, ordered, nil
}

func (s *containerRegistryServer) validate() error {
	if s == nil {
		return fmt.Errorf("container registry server is nil")
	}
	return s.err
}

func (s *containerRegistryServer) DescribeRegistries(context.Context, *pb.ContainerRegistryDeclarationsRequest) (*pb.ContainerRegistryDeclarationsResponse, error) {
	if err := s.validate(); err != nil {
		return &pb.ContainerRegistryDeclarationsResponse{Error: containerRegistryFailure("describe", "invalid_declaration", false)}, nil //nolint:nilerr // application error is carried in the typed response
	}
	declarations := make([]*pb.ContainerRegistryDeclaration, 0, len(s.ordered))
	for _, declaration := range s.ordered {
		declarations = append(declarations, proto.Clone(declaration).(*pb.ContainerRegistryDeclaration))
	}
	return &pb.ContainerRegistryDeclarationsResponse{Registries: declarations}, nil
}

func (s *containerRegistryServer) Login(ctx context.Context, request *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error) {
	if requestError := s.validateOperation(request.GetRegistry(), "login"); requestError != nil {
		return &pb.ContainerRegistryOperationResponse{Error: requestError}, nil
	}
	providerRequest := proto.Clone(request).(*pb.ContainerRegistryLoginRequest)
	response, err := s.provider.Login(ctx, providerRequest)
	return s.normalizeOperationResponse(ctx, "login", response, err)
}

func (s *containerRegistryServer) Logout(ctx context.Context, request *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error) {
	if requestError := s.validateOperation(request.GetRegistry(), "logout"); requestError != nil {
		return &pb.ContainerRegistryOperationResponse{Error: requestError}, nil
	}
	providerRequest := proto.Clone(request).(*pb.ContainerRegistryLogoutRequest)
	response, err := s.provider.Logout(ctx, providerRequest)
	return s.normalizeOperationResponse(ctx, "logout", response, err)
}

func (s *containerRegistryServer) Push(ctx context.Context, request *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error) {
	if requestError := s.validateOperation(request.GetRegistry(), "push"); requestError != nil {
		return &pb.ContainerRegistryOperationResponse{Error: requestError}, nil
	}
	if strings.TrimSpace(request.GetImageReference()) == "" {
		return &pb.ContainerRegistryOperationResponse{Error: containerRegistryFailure("push", "image_reference_required", false)}, nil
	}
	providerRequest := proto.Clone(request).(*pb.ContainerRegistryPushRequest)
	response, err := s.provider.Push(ctx, providerRequest)
	return s.normalizeOperationResponse(ctx, "push", response, err)
}

func (s *containerRegistryServer) Prune(ctx context.Context, request *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error) {
	if requestError := s.validateOperation(request.GetRegistry(), "prune"); requestError != nil {
		return &pb.ContainerRegistryOperationResponse{Error: requestError}, nil
	}
	providerRequest := proto.Clone(request).(*pb.ContainerRegistryPruneRequest)
	response, err := s.provider.Prune(ctx, providerRequest)
	return s.normalizeOperationResponse(ctx, "prune", response, err)
}

func (s *containerRegistryServer) validateOperation(registry *pb.ContainerRegistryConfig, operation string) *pb.ContainerRegistryError {
	if err := s.validate(); err != nil {
		return containerRegistryFailure(operation, "invalid_declaration", false)
	}
	if registry == nil || registry.GetType() == "" {
		return containerRegistryFailure(operation, "registry_type_required", false)
	}
	operations, exists := s.registries[registry.GetType()]
	if !exists {
		return containerRegistryFailure(operation, "unsupported_registry_type", false)
	}
	if _, declared := operations[operation]; !declared {
		return containerRegistryFailure(operation, "unsupported_operation", false)
	}
	return nil
}

func (s *containerRegistryServer) normalizeOperationResponse(ctx context.Context, operation string, providerResponse *pb.ContainerRegistryOperationResponse, providerErr error) (*pb.ContainerRegistryOperationResponse, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, status.FromContextError(ctxErr).Err()
	}
	if providerErr != nil {
		return &pb.ContainerRegistryOperationResponse{Error: containerRegistryFailure(operation, "provider_error", false)}, nil
	}
	if providerResponse == nil {
		return &pb.ContainerRegistryOperationResponse{Error: containerRegistryFailure(operation, "empty_response", false)}, nil
	}
	if providerResponse.GetError() != nil {
		return &pb.ContainerRegistryOperationResponse{
			Error: redactContainerRegistryError(operation, providerResponse.GetError()),
		}, nil
	}
	if providerResponse.GetResult() == nil {
		return &pb.ContainerRegistryOperationResponse{Error: containerRegistryFailure(operation, "empty_response", false)}, nil
	}
	if len(providerResponse.GetResult().GetOutput()) > maxContainerRegistryOutputBytes {
		return &pb.ContainerRegistryOperationResponse{Error: containerRegistryFailure(operation, "invalid_response", false)}, nil
	}
	return &pb.ContainerRegistryOperationResponse{
		Result: &pb.ContainerRegistryResult{Output: bytes.Clone(providerResponse.GetResult().GetOutput())},
	}, nil
}

func containerRegistryFailure(operation, code string, retryable bool) *pb.ContainerRegistryError {
	return &pb.ContainerRegistryError{
		Code:      code,
		Message:   "container registry " + operation + " failed",
		Retryable: retryable,
	}
}

func redactContainerRegistryError(operation string, providerError *pb.ContainerRegistryError) *pb.ContainerRegistryError {
	if providerError == nil {
		return nil
	}
	code := providerError.GetCode()
	if !credentialErrorCodePattern.MatchString(code) {
		code = "provider_error"
	}
	return containerRegistryFailure(operation, code, providerError.GetRetryable())
}
