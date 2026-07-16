package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// SecretStoreContractID is the manifest/runtime contract identifier.
	SecretStoreContractID = "workflow.provider.secret-store"
	// SecretStoreProtocolVersion is the typed transport version advertised
	// through the runtime ContractRegistry.
	SecretStoreProtocolVersion = "1"

	// Transport limits keep provider-owned opaque and sensitive data bounded.
	// A zero page size uses the default; the maximum is accepted exactly.
	maxSecretStoreTypeBytes            = 256
	maxSecretStoreScopeBytes           = 256
	maxSecretStoreNameBytes            = 1 << 10
	maxSecretStoreConfigBytes          = 64 << 10
	maxSecretStoreValueBytes           = 1 << 20
	defaultSecretStorePageSize         = 100
	maxSecretStorePageSize       int32 = 1000
	maxSecretStorePageTokenBytes       = 4 << 10
)

var secretStoreOperationOrder = []string{"get", "list", "stat_all", "check_access"}

// SecretStoreProvider is the provider-owned implementation behind the
// optional read-only SecretStore gRPC service. SecretStores is authoritative.
type SecretStoreProvider interface {
	SecretStores() []*pb.SecretStoreDeclaration
	Get(context.Context, *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error)
	List(context.Context, *pb.SecretStoreListRequest) (*pb.SecretStoreListResponse, error)
	StatAll(context.Context, *pb.SecretStoreStatAllRequest) (*pb.SecretStoreStatAllResponse, error)
	CheckAccess(context.Context, *pb.SecretStoreCheckAccessRequest) (*pb.SecretStoreCheckAccessResponse, error)
}

// WithSecretStoreProvider registers provider as the optional typed SecretStore
// service. Legacy plugins that omit this option remain PluginService-only.
func WithSecretStoreProvider(provider SecretStoreProvider) ServeOption {
	return func(server *grpcServer) {
		server.providerServices.secretStore = newSecretStoreServer(provider)
	}
}

type secretStoreDeclaration struct {
	operations map[string]struct{}
	scopes     map[string]struct{}
}

type secretStoreServer struct {
	pb.UnimplementedSecretStoreServer

	provider SecretStoreProvider
	stores   map[string]secretStoreDeclaration
	ordered  []*pb.SecretStoreDeclaration
	err      error
}

func newSecretStoreServer(provider SecretStoreProvider) *secretStoreServer {
	server := &secretStoreServer{provider: provider}
	if provider == nil || isTypedNil(provider) {
		server.err = fmt.Errorf("secret store provider is nil")
		return server
	}
	server.stores, server.ordered, server.err = validateSecretStoreDeclarations(provider.SecretStores())
	return server
}

func validateSecretStoreDeclarations(declarations []*pb.SecretStoreDeclaration) (map[string]secretStoreDeclaration, []*pb.SecretStoreDeclaration, error) {
	if len(declarations) == 0 {
		return nil, nil, fmt.Errorf("secret store provider must declare at least one store type")
	}
	allowedOperations := make(map[string]struct{}, len(secretStoreOperationOrder))
	for _, operation := range secretStoreOperationOrder {
		allowedOperations[operation] = struct{}{}
	}
	stores := make(map[string]secretStoreDeclaration, len(declarations))
	ordered := make([]*pb.SecretStoreDeclaration, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration == nil {
			return nil, nil, fmt.Errorf("secret store declaration is nil")
		}
		storeType := strings.TrimSpace(declaration.GetType())
		if !validSecretStoreIdentifier(storeType, maxSecretStoreTypeBytes) {
			return nil, nil, fmt.Errorf("secret store type is invalid")
		}
		if _, duplicate := stores[storeType]; duplicate {
			return nil, nil, fmt.Errorf("secret store type %q is duplicated", storeType)
		}
		if len(declaration.GetOperations()) == 0 {
			return nil, nil, fmt.Errorf("secret store type %q must declare at least one operation", storeType)
		}
		operations := make(map[string]struct{}, len(declaration.GetOperations()))
		for _, value := range declaration.GetOperations() {
			operation := strings.TrimSpace(value)
			if operation == "" {
				return nil, nil, fmt.Errorf("secret store type %q has an empty operation", storeType)
			}
			if _, allowed := allowedOperations[operation]; !allowed {
				return nil, nil, fmt.Errorf("secret store type %q has unsupported operation %q", storeType, operation)
			}
			if _, duplicate := operations[operation]; duplicate {
				return nil, nil, fmt.Errorf("secret store type %q duplicates operation %q", storeType, operation)
			}
			operations[operation] = struct{}{}
		}
		if len(declaration.GetScopes()) == 0 {
			return nil, nil, fmt.Errorf("secret store type %q must declare at least one scope", storeType)
		}
		scopes := make(map[string]struct{}, len(declaration.GetScopes()))
		canonicalScopes := make([]string, 0, len(declaration.GetScopes()))
		for _, value := range declaration.GetScopes() {
			scope := strings.TrimSpace(value)
			if !validSecretStoreIdentifier(scope, maxSecretStoreScopeBytes) {
				return nil, nil, fmt.Errorf("secret store type %q has an invalid scope", storeType)
			}
			if _, duplicate := scopes[scope]; duplicate {
				return nil, nil, fmt.Errorf("secret store type %q duplicates scope %q", storeType, scope)
			}
			scopes[scope] = struct{}{}
			canonicalScopes = append(canonicalScopes, scope)
		}
		canonicalOperations := make([]string, 0, len(operations))
		for _, operation := range secretStoreOperationOrder {
			if _, declared := operations[operation]; declared {
				canonicalOperations = append(canonicalOperations, operation)
			}
		}
		sort.Strings(canonicalScopes)
		stores[storeType] = secretStoreDeclaration{operations: operations, scopes: scopes}
		ordered = append(ordered, &pb.SecretStoreDeclaration{
			Type:       storeType,
			Operations: append([]string(nil), canonicalOperations...),
			Scopes:     append([]string(nil), canonicalScopes...),
		})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].GetType() < ordered[j].GetType() })
	return stores, ordered, nil
}

func validSecretStoreIdentifier(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= limit
}

func (s *secretStoreServer) validate() error {
	if s == nil {
		return fmt.Errorf("secret store server is nil")
	}
	return s.err
}

func (s *secretStoreServer) DescribeSecretStores(context.Context, *pb.SecretStoreDeclarationsRequest) (*pb.SecretStoreDeclarationsResponse, error) {
	if err := s.validate(); err != nil {
		return &pb.SecretStoreDeclarationsResponse{Error: secretStoreFailure("describe", "invalid_declaration", false)}, nil //nolint:nilerr // typed application error
	}
	stores := make([]*pb.SecretStoreDeclaration, 0, len(s.ordered))
	for _, declaration := range s.ordered {
		stores = append(stores, &pb.SecretStoreDeclaration{
			Type:       declaration.GetType(),
			Operations: append([]string(nil), declaration.GetOperations()...),
			Scopes:     append([]string(nil), declaration.GetScopes()...),
		})
	}
	return &pb.SecretStoreDeclarationsResponse{Stores: stores}, nil
}

func (s *secretStoreServer) Get(ctx context.Context, request *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error) {
	if requestError := s.validateTarget(request.GetTarget(), "get"); requestError != nil {
		return &pb.SecretStoreGetResponse{Error: requestError}, nil
	}
	if !validSecretStoreName(request.GetKey()) || strings.TrimSpace(request.GetKey()) != request.GetKey() {
		code := "invalid_key"
		if strings.TrimSpace(request.GetKey()) == "" {
			code = "key_required"
		}
		return &pb.SecretStoreGetResponse{Error: secretStoreFailure("get", code, false)}, nil
	}
	providerResponse, providerErr := s.provider.Get(ctx, proto.Clone(request).(*pb.SecretStoreGetRequest))
	if normalizedErr := secretStoreContextOrProviderError(ctx, "get", providerErr); normalizedErr != nil {
		if normalizedErr.rpc != nil {
			return nil, normalizedErr.rpc
		}
		return &pb.SecretStoreGetResponse{Error: normalizedErr.typed}, nil
	}
	if providerResponse == nil {
		return &pb.SecretStoreGetResponse{Error: secretStoreFailure("get", "empty_response", false)}, nil
	}
	if providerResponse.GetError() != nil {
		return &pb.SecretStoreGetResponse{Error: redactSecretStoreError("get", providerResponse.GetError())}, nil
	}
	if providerResponse.GetResult() == nil {
		return &pb.SecretStoreGetResponse{Error: secretStoreFailure("get", "empty_response", false)}, nil
	}
	if len(providerResponse.GetResult().GetValue()) > maxSecretStoreValueBytes {
		return &pb.SecretStoreGetResponse{Error: secretStoreFailure("get", "invalid_response", false)}, nil
	}
	return &pb.SecretStoreGetResponse{Result: &pb.SecretStoreGetResult{Value: bytes.Clone(providerResponse.GetResult().GetValue())}}, nil
}

func (s *secretStoreServer) List(ctx context.Context, request *pb.SecretStoreListRequest) (*pb.SecretStoreListResponse, error) {
	if requestError := s.validateTarget(request.GetTarget(), "list"); requestError != nil {
		return &pb.SecretStoreListResponse{Error: requestError}, nil
	}
	pageSize, requestError := validateSecretStorePagination("list", request.GetPageSize(), request.GetPageToken())
	if requestError != nil {
		return &pb.SecretStoreListResponse{Error: requestError}, nil
	}
	providerRequest := proto.Clone(request).(*pb.SecretStoreListRequest)
	providerRequest.PageSize = int32(pageSize)
	providerResponse, providerErr := s.provider.List(ctx, providerRequest)
	if normalizedErr := secretStoreContextOrProviderError(ctx, "list", providerErr); normalizedErr != nil {
		if normalizedErr.rpc != nil {
			return nil, normalizedErr.rpc
		}
		return &pb.SecretStoreListResponse{Error: normalizedErr.typed}, nil
	}
	if providerResponse == nil {
		return &pb.SecretStoreListResponse{Error: secretStoreFailure("list", "empty_response", false)}, nil
	}
	if providerResponse.GetError() != nil {
		return &pb.SecretStoreListResponse{Error: redactSecretStoreError("list", providerResponse.GetError())}, nil
	}
	if providerResponse.GetResult() == nil {
		return &pb.SecretStoreListResponse{Error: secretStoreFailure("list", "empty_response", false)}, nil
	}
	result := providerResponse.GetResult()
	if !validSecretStorePage(len(result.GetNames()), pageSize, request.GetPageToken(), result.GetNextPageToken()) {
		return &pb.SecretStoreListResponse{Error: secretStoreFailure("list", "invalid_response", false)}, nil
	}
	names := make([]string, 0, len(result.GetNames()))
	seen := make(map[string]struct{}, len(result.GetNames()))
	for _, name := range result.GetNames() {
		if !validSecretStoreName(name) || strings.TrimSpace(name) != name {
			return &pb.SecretStoreListResponse{Error: secretStoreFailure("list", "invalid_response", false)}, nil
		}
		if _, duplicate := seen[name]; duplicate {
			return &pb.SecretStoreListResponse{Error: secretStoreFailure("list", "invalid_response", false)}, nil
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return &pb.SecretStoreListResponse{Result: &pb.SecretStoreListResult{
		Names: names, NextPageToken: bytes.Clone(result.GetNextPageToken()),
	}}, nil
}

func (s *secretStoreServer) StatAll(ctx context.Context, request *pb.SecretStoreStatAllRequest) (*pb.SecretStoreStatAllResponse, error) {
	if requestError := s.validateTarget(request.GetTarget(), "stat_all"); requestError != nil {
		return &pb.SecretStoreStatAllResponse{Error: requestError}, nil
	}
	pageSize, requestError := validateSecretStorePagination("stat_all", request.GetPageSize(), request.GetPageToken())
	if requestError != nil {
		return &pb.SecretStoreStatAllResponse{Error: requestError}, nil
	}
	providerRequest := proto.Clone(request).(*pb.SecretStoreStatAllRequest)
	providerRequest.PageSize = int32(pageSize)
	providerResponse, providerErr := s.provider.StatAll(ctx, providerRequest)
	if normalizedErr := secretStoreContextOrProviderError(ctx, "stat_all", providerErr); normalizedErr != nil {
		if normalizedErr.rpc != nil {
			return nil, normalizedErr.rpc
		}
		return &pb.SecretStoreStatAllResponse{Error: normalizedErr.typed}, nil
	}
	if providerResponse == nil {
		return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "empty_response", false)}, nil
	}
	if providerResponse.GetError() != nil {
		return &pb.SecretStoreStatAllResponse{Error: redactSecretStoreError("stat_all", providerResponse.GetError())}, nil
	}
	if providerResponse.GetResult() == nil {
		return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "empty_response", false)}, nil
	}
	result := providerResponse.GetResult()
	if !validSecretStorePage(len(result.GetItems()), pageSize, request.GetPageToken(), result.GetNextPageToken()) {
		return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "invalid_response", false)}, nil
	}
	items := make([]*pb.SecretStoreMetadata, 0, len(result.GetItems()))
	seen := make(map[string]struct{}, len(result.GetItems()))
	for _, item := range result.GetItems() {
		if item == nil || !validSecretStoreName(item.GetName()) || strings.TrimSpace(item.GetName()) != item.GetName() {
			return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "invalid_response", false)}, nil
		}
		if _, duplicate := seen[item.GetName()]; duplicate {
			return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "invalid_response", false)}, nil
		}
		seen[item.GetName()] = struct{}{}
		var updatedAt *timestamppb.Timestamp
		if item.GetUpdatedAt() != nil {
			if err := item.GetUpdatedAt().CheckValid(); err != nil {
				return &pb.SecretStoreStatAllResponse{Error: secretStoreFailure("stat_all", "invalid_response", false)}, nil
			}
			updatedAt = &timestamppb.Timestamp{Seconds: item.GetUpdatedAt().GetSeconds(), Nanos: item.GetUpdatedAt().GetNanos()}
		}
		items = append(items, &pb.SecretStoreMetadata{Name: item.GetName(), Exists: item.GetExists(), UpdatedAt: updatedAt})
	}
	return &pb.SecretStoreStatAllResponse{Result: &pb.SecretStoreStatAllResult{
		Items: items, NextPageToken: bytes.Clone(result.GetNextPageToken()),
	}}, nil
}

func (s *secretStoreServer) CheckAccess(ctx context.Context, request *pb.SecretStoreCheckAccessRequest) (*pb.SecretStoreCheckAccessResponse, error) {
	if requestError := s.validateTarget(request.GetTarget(), "check_access"); requestError != nil {
		return &pb.SecretStoreCheckAccessResponse{Error: requestError}, nil
	}
	providerResponse, providerErr := s.provider.CheckAccess(ctx, proto.Clone(request).(*pb.SecretStoreCheckAccessRequest))
	if normalizedErr := secretStoreContextOrProviderError(ctx, "check_access", providerErr); normalizedErr != nil {
		if normalizedErr.rpc != nil {
			return nil, normalizedErr.rpc
		}
		return &pb.SecretStoreCheckAccessResponse{Error: normalizedErr.typed}, nil
	}
	if providerResponse == nil {
		return &pb.SecretStoreCheckAccessResponse{Error: secretStoreFailure("check_access", "empty_response", false)}, nil
	}
	if providerResponse.GetError() != nil {
		return &pb.SecretStoreCheckAccessResponse{Error: redactSecretStoreError("check_access", providerResponse.GetError())}, nil
	}
	return &pb.SecretStoreCheckAccessResponse{}, nil
}

func (s *secretStoreServer) validateTarget(target *pb.SecretStoreTarget, operation string) *pb.SecretStoreError {
	if err := s.validate(); err != nil {
		return secretStoreFailure(operation, "invalid_declaration", false)
	}
	if target == nil || target.GetType() == "" {
		return secretStoreFailure(operation, "target_type_required", false)
	}
	declaration, exists := s.stores[target.GetType()]
	if !exists {
		return secretStoreFailure(operation, "unsupported_store_type", false)
	}
	if target.GetScope() == "" {
		return secretStoreFailure(operation, "target_scope_required", false)
	}
	if _, declared := declaration.scopes[target.GetScope()]; !declared {
		return secretStoreFailure(operation, "unsupported_scope", false)
	}
	if _, declared := declaration.operations[operation]; !declared {
		return secretStoreFailure(operation, "unsupported_operation", false)
	}
	if !validSecretStoreConfig(target.GetConfigJson()) {
		return secretStoreFailure(operation, "invalid_config", false)
	}
	return nil
}

func validSecretStoreConfig(configJSON []byte) bool {
	if len(configJSON) == 0 {
		return true
	}
	if len(configJSON) > maxSecretStoreConfigBytes || !utf8.Valid(configJSON) || !json.Valid(configJSON) {
		return false
	}
	trimmed := bytes.TrimSpace(configJSON)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validSecretStoreName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.ValidString(name) && len(name) <= maxSecretStoreNameBytes
}

func validateSecretStorePagination(operation string, pageSize int32, pageToken []byte) (int, *pb.SecretStoreError) {
	if pageSize < 0 || pageSize > maxSecretStorePageSize {
		return 0, secretStoreFailure(operation, "invalid_page_size", false)
	}
	if len(pageToken) > maxSecretStorePageTokenBytes {
		return 0, secretStoreFailure(operation, "invalid_page_token", false)
	}
	if pageSize == 0 {
		return defaultSecretStorePageSize, nil
	}
	return int(pageSize), nil
}

func validSecretStorePage(count, pageSize int, requestToken, nextToken []byte) bool {
	if count > pageSize || len(nextToken) > maxSecretStorePageTokenBytes {
		return false
	}
	if len(nextToken) == 0 {
		return true
	}
	return len(requestToken) == 0 || !bytes.Equal(requestToken, nextToken)
}

type secretStoreNormalizedError struct {
	typed *pb.SecretStoreError
	rpc   error
}

func secretStoreContextOrProviderError(ctx context.Context, operation string, providerErr error) *secretStoreNormalizedError {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &secretStoreNormalizedError{rpc: status.FromContextError(ctxErr).Err()}
	}
	if providerErr != nil {
		return &secretStoreNormalizedError{typed: secretStoreFailure(operation, "provider_error", false)}
	}
	return nil
}

func secretStoreFailure(operation, code string, retryable bool) *pb.SecretStoreError {
	return &pb.SecretStoreError{Code: code, Message: "secret store " + operation + " failed", Retryable: retryable}
}

func redactSecretStoreError(operation string, providerError *pb.SecretStoreError) *pb.SecretStoreError {
	if providerError == nil {
		return nil
	}
	code := providerError.GetCode()
	if !credentialErrorCodePattern.MatchString(code) {
		code = "provider_error"
	}
	return secretStoreFailure(operation, code, providerError.GetRetryable())
}
