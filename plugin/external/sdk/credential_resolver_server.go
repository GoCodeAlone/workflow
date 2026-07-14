package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// CredentialResolverContractID is the manifest/runtime contract identifier.
	CredentialResolverContractID = "workflow.provider.credential-resolver"
	// CredentialResolverProtocolVersion is the typed transport version
	// advertised through the runtime ContractRegistry.
	CredentialResolverProtocolVersion = "1"
)

var supportedCredentialResolverTypes = map[string]map[string]struct{}{
	"aws":   credentialResolverStringSet("static", "env", "profile", "role_arn"),
	"gcp":   credentialResolverStringSet("static", "env", "service_account_json", "service_account_key", "workload_identity", "application_default"),
	"azure": credentialResolverStringSet("static", "env", "client_credentials", "managed_identity", "cli"),
}

func credentialResolverStringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// CredentialResolverProvider is the provider-owned implementation behind the
// optional CredentialResolver gRPC service. CredentialResolvers is the
// authoritative runtime provider/type declaration set.
type CredentialResolverProvider interface {
	CredentialResolvers() []*pb.CredentialResolverDeclaration
	Resolve(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error)
}

// WithCredentialResolverProvider registers provider as the optional, typed
// CredentialResolver service. Legacy plugins that omit this option keep their
// existing PluginService-only surface.
func WithCredentialResolverProvider(provider CredentialResolverProvider) ServeOption {
	return func(server *grpcServer) {
		server.providerServices.credentialResolver = newCredentialResolverServer(provider)
	}
}

type credentialResolverServer struct {
	pb.UnimplementedCredentialResolverServer

	provider CredentialResolverProvider
	declared map[string]struct{}
	ordered  []*pb.CredentialResolverDeclaration
	err      error
}

func newCredentialResolverServer(provider CredentialResolverProvider) *credentialResolverServer {
	server := &credentialResolverServer{provider: provider}
	if provider == nil || isTypedNil(provider) {
		server.err = fmt.Errorf("credential resolver provider is nil")
		return server
	}
	server.declared, server.ordered, server.err = validateCredentialResolverDeclarations(provider.CredentialResolvers())
	return server
}

func validateCredentialResolverDeclarations(declarations []*pb.CredentialResolverDeclaration) (map[string]struct{}, []*pb.CredentialResolverDeclaration, error) {
	if len(declarations) == 0 {
		return nil, nil, fmt.Errorf("credential resolver must declare at least one provider")
	}
	declared := make(map[string]struct{})
	providers := make(map[string]struct{}, len(declarations))
	ordered := make([]*pb.CredentialResolverDeclaration, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration == nil {
			return nil, nil, fmt.Errorf("credential resolver declaration is nil")
		}
		provider := strings.TrimSpace(declaration.GetProvider())
		if provider == "" {
			return nil, nil, fmt.Errorf("credential resolver provider is required")
		}
		if _, exists := providers[provider]; exists {
			return nil, nil, fmt.Errorf("credential resolver provider %q is duplicated", provider)
		}
		allowedTypes, supported := supportedCredentialResolverTypes[provider]
		if !supported {
			return nil, nil, fmt.Errorf("credential resolver provider %q is unsupported", provider)
		}
		providers[provider] = struct{}{}
		if len(declaration.GetCredentialTypes()) == 0 {
			return nil, nil, fmt.Errorf("credential resolver provider %q has no credential types", provider)
		}
		cloned := proto.Clone(declaration).(*pb.CredentialResolverDeclaration)
		cloned.Provider = provider
		for index, credentialType := range cloned.GetCredentialTypes() {
			credentialType = strings.TrimSpace(credentialType)
			if credentialType == "" {
				return nil, nil, fmt.Errorf("credential resolver provider %q has an empty credential type", provider)
			}
			if _, supported := allowedTypes[credentialType]; !supported {
				return nil, nil, fmt.Errorf("credential resolver provider %q has unsupported credential type %q", provider, credentialType)
			}
			key := credentialResolverKey(provider, credentialType)
			if _, exists := declared[key]; exists {
				return nil, nil, fmt.Errorf("credential resolver provider %q duplicates credential type %q", provider, credentialType)
			}
			declared[key] = struct{}{}
			cloned.CredentialTypes[index] = credentialType
		}
		ordered = append(ordered, cloned)
	}
	return declared, ordered, nil
}

func credentialResolverKey(provider, credentialType string) string {
	return provider + "\x00" + credentialType
}

func (s *credentialResolverServer) validate() error {
	if s == nil {
		return fmt.Errorf("credential resolver server is nil")
	}
	return s.err
}

func (s *credentialResolverServer) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest) (*pb.CredentialResolverDeclarationsResponse, error) {
	if err := s.validate(); err != nil {
		return &pb.CredentialResolverDeclarationsResponse{Error: credentialResolutionFailure("invalid_declaration", false)}, nil //nolint:nilerr // application error is carried in the typed response
	}
	declarations := make([]*pb.CredentialResolverDeclaration, 0, len(s.ordered))
	for _, declaration := range s.ordered {
		declarations = append(declarations, proto.Clone(declaration).(*pb.CredentialResolverDeclaration))
	}
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: declarations}, nil
}

func (s *credentialResolverServer) Resolve(ctx context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	if err := s.validateRequest(request); err != nil {
		return &pb.CredentialResolveResponse{Error: err}, nil
	}
	providerResponse, providerErr := s.provider.Resolve(ctx, request)
	var response *pb.CredentialResolveResponse
	if providerResponse != nil {
		response = proto.Clone(providerResponse).(*pb.CredentialResolveResponse)
	}
	if providerErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return &pb.CredentialResolveResponse{Error: credentialResolutionFailure("provider_error", false)}, nil
	}
	if response == nil {
		return &pb.CredentialResolveResponse{Error: credentialResolutionFailure("empty_response", false)}, nil
	}
	if response.GetError() != nil {
		response.Credentials = nil
		response.Error = redactCredentialResolutionError(response.GetError())
		return response, nil
	}
	if response.GetCredentials() == nil {
		return &pb.CredentialResolveResponse{Error: credentialResolutionFailure("empty_response", false)}, nil
	}
	if response.GetCredentials().GetProvider() == "" {
		response.Credentials.Provider = request.GetProvider()
	} else if response.GetCredentials().GetProvider() != request.GetProvider() {
		return &pb.CredentialResolveResponse{Error: credentialResolutionFailure("invalid_response", false)}, nil
	}
	return response, nil
}

func (s *credentialResolverServer) validateRequest(request *pb.CredentialResolveRequest) *pb.CredentialResolutionError {
	if err := s.validate(); err != nil {
		return credentialResolutionFailure("invalid_declaration", false)
	}
	if request == nil || strings.TrimSpace(request.GetProvider()) == "" || strings.TrimSpace(request.GetCredentialType()) == "" {
		return credentialResolutionFailure("selection_required", false)
	}
	if _, exists := s.declared[credentialResolverKey(request.GetProvider(), request.GetCredentialType())]; !exists {
		return credentialResolutionFailure("unsupported_credential_type", false)
	}
	if len(request.GetConfigJson()) > 0 && !json.Valid(request.GetConfigJson()) {
		return credentialResolutionFailure("invalid_config", false)
	}
	return nil
}

func credentialResolutionFailure(code string, retryable bool) *pb.CredentialResolutionError {
	return &pb.CredentialResolutionError{
		Code:      code,
		Message:   "credential resolver resolve failed",
		Retryable: retryable,
	}
}

func redactCredentialResolutionError(providerError *pb.CredentialResolutionError) *pb.CredentialResolutionError {
	if providerError == nil {
		return nil
	}
	code := providerError.GetCode()
	if !credentialErrorCodePattern.MatchString(code) {
		code = "provider_error"
	}
	return credentialResolutionFailure(code, providerError.GetRetryable())
}
