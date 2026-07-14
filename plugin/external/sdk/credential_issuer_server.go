package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// CredentialIssuerContractID is the manifest/runtime contract identifier.
	CredentialIssuerContractID = "workflow.provider.credential-issuer"
	// CredentialIssuerProtocolVersion is the typed transport version advertised
	// through the runtime ContractRegistry.
	CredentialIssuerProtocolVersion = "1"
)

var credentialErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// CredentialIssuerProvider is the provider-owned implementation behind the
// optional CredentialIssuer gRPC service. CredentialSources is authoritative:
// the SDK validates every returned output against these runtime declarations.
// Callers cannot provide or override the allowlist.
type CredentialIssuerProvider interface {
	CredentialSources() []*pb.CredentialSourceDeclaration
	Issue(context.Context, *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error)
	List(context.Context, *pb.CredentialListRequest) (*pb.CredentialListResponse, error)
	Delete(context.Context, *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error)
}

// WithCredentialIssuerProvider registers provider as an optional, typed
// CredentialIssuer service. Older plugins that omit this option retain their
// existing PluginService-only surface.
func WithCredentialIssuerProvider(provider CredentialIssuerProvider) ServeOption {
	return func(server *grpcServer) {
		server.providerServices.credentialIssuer = newCredentialIssuerServer(provider)
	}
}

type credentialIssuerServer struct {
	pb.UnimplementedCredentialIssuerServer

	provider CredentialIssuerProvider
	sources  map[string]*pb.CredentialSourceDeclaration
	ordered  []*pb.CredentialSourceDeclaration
	err      error
}

func newCredentialIssuerServer(provider CredentialIssuerProvider) *credentialIssuerServer {
	server := &credentialIssuerServer{provider: provider}
	if provider == nil || isTypedNil(provider) {
		server.err = fmt.Errorf("credential issuer provider is nil")
		return server
	}
	server.sources, server.ordered, server.err = validateCredentialSourceDeclarations(provider.CredentialSources())
	return server
}

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func validateCredentialSourceDeclarations(declarations []*pb.CredentialSourceDeclaration) (map[string]*pb.CredentialSourceDeclaration, []*pb.CredentialSourceDeclaration, error) {
	if len(declarations) == 0 {
		return nil, nil, fmt.Errorf("credential issuer must declare at least one source")
	}
	sources := make(map[string]*pb.CredentialSourceDeclaration, len(declarations))
	ordered := make([]*pb.CredentialSourceDeclaration, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration == nil {
			return nil, nil, fmt.Errorf("credential issuer declaration is nil")
		}
		source := strings.TrimSpace(declaration.GetSource())
		if source == "" {
			return nil, nil, fmt.Errorf("credential issuer source is required")
		}
		if _, exists := sources[source]; exists {
			return nil, nil, fmt.Errorf("credential issuer source %q is duplicated", source)
		}
		switch declaration.GetConcurrencyMode() {
		case pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
			pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_SINGLE_WRITER_REQUIRED:
		default:
			return nil, nil, fmt.Errorf("credential issuer source %q has invalid concurrency mode %s", source, declaration.GetConcurrencyMode())
		}
		outputs := make(map[string]struct{}, len(declaration.GetOutputs()))
		for _, output := range declaration.GetOutputs() {
			if output == nil || strings.TrimSpace(output.GetKey()) == "" {
				return nil, nil, fmt.Errorf("credential issuer source %q has an empty output key", source)
			}
			if _, exists := outputs[output.GetKey()]; exists {
				return nil, nil, fmt.Errorf("credential issuer source %q duplicates output %q", source, output.GetKey())
			}
			outputs[output.GetKey()] = struct{}{}
		}
		if len(outputs) == 0 {
			return nil, nil, fmt.Errorf("credential issuer source %q has no outputs", source)
		}
		if _, exists := outputs[declaration.GetIdentifierKey()]; !exists {
			return nil, nil, fmt.Errorf("credential issuer source %q identifier key %q is not a declared output", source, declaration.GetIdentifierKey())
		}
		cloned := proto.Clone(declaration).(*pb.CredentialSourceDeclaration)
		cloned.Source = source
		sources[source] = cloned
		ordered = append(ordered, cloned)
	}
	return sources, ordered, nil
}

func (s *credentialIssuerServer) validate() error {
	if s == nil {
		return fmt.Errorf("credential issuer server is nil")
	}
	return s.err
}

func (s *credentialIssuerServer) DescribeSources(context.Context, *pb.CredentialSourceDeclarationsRequest) (*pb.CredentialSourceDeclarationsResponse, error) {
	if err := s.validate(); err != nil {
		return &pb.CredentialSourceDeclarationsResponse{Error: credentialOperationFailure("describe", "invalid_declaration", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)}, nil //nolint:nilerr // application error is carried in the typed response
	}
	sources := make([]*pb.CredentialSourceDeclaration, 0, len(s.ordered))
	for _, declaration := range s.ordered {
		sources = append(sources, proto.Clone(declaration).(*pb.CredentialSourceDeclaration))
	}
	return &pb.CredentialSourceDeclarationsResponse{Sources: sources}, nil
}

func (s *credentialIssuerServer) Issue(ctx context.Context, request *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error) {
	declaration, requestError := s.validateIssueRequest(request)
	if requestError != nil {
		return &pb.CredentialIssueResponse{Error: requestError}, nil
	}
	providerResponse, err := s.provider.Issue(ctx, request)
	var response *pb.CredentialIssueResponse
	if providerResponse != nil {
		response = proto.Clone(providerResponse).(*pb.CredentialIssueResponse)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return credentialIssueFailure("provider_error", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN), nil
	}
	if response == nil {
		return credentialIssueFailure("empty_response", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN), nil
	}
	if response.GetError() != nil {
		response.Outputs = nil
		response.Identifier = ""
		response.IdentifierSensitive = false
		identifierSensitive := credentialOutputDeclarations(declaration)[declaration.GetIdentifierKey()].GetSensitive()
		response.Error = redactCredentialOperationError("issue", response.GetError(), identifierSensitive, response.GetReconciliationState())
		response.ReconciliationState = response.Error.GetReconciliationState()
		return response, nil
	}
	if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED {
		return credentialIssueFailure("invalid_reconciliation_state", normalizeCredentialReconciliationState(response.GetReconciliationState(), pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN)), nil
	}

	allowed := credentialOutputDeclarations(declaration)
	seen := make(map[string]struct{}, len(response.GetOutputs()))
	var identifierOutput []byte
	var identifierCount int
	for _, output := range response.GetOutputs() {
		if output == nil {
			return credentialIssueFailure("invalid_response", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
		}
		declared, exists := allowed[output.GetKey()]
		if !exists {
			return credentialIssueFailure("undeclared_output", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
		}
		if _, duplicate := seen[output.GetKey()]; duplicate {
			if output.GetKey() == declaration.GetIdentifierKey() {
				return credentialIssueFailure("invalid_identifier", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
			}
			return credentialIssueFailure("invalid_response", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
		}
		seen[output.GetKey()] = struct{}{}
		output.Sensitive = declared.GetSensitive()
		if output.GetKey() == declaration.GetIdentifierKey() {
			identifierCount++
			identifierOutput = output.GetValue()
		}
	}
	if identifierCount != 1 || len(identifierOutput) == 0 || !utf8.Valid(identifierOutput) {
		return credentialIssueFailure("invalid_identifier", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
	}
	identifier := string(identifierOutput)
	if response.GetIdentifier() == "" {
		response.Identifier = identifier
	} else if response.GetIdentifier() != identifier {
		return credentialIssueFailure("invalid_identifier", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED), nil
	}
	identifierDeclaration := allowed[declaration.GetIdentifierKey()]
	response.IdentifierSensitive = identifierDeclaration.GetSensitive()
	return response, nil
}

func (s *credentialIssuerServer) List(ctx context.Context, request *pb.CredentialListRequest) (*pb.CredentialListResponse, error) {
	declaration, requestError := s.validateListRequest(request)
	if requestError != nil {
		return &pb.CredentialListResponse{Error: requestError}, nil
	}
	providerResponse, err := s.provider.List(ctx, request)
	var response *pb.CredentialListResponse
	if providerResponse != nil {
		response = proto.Clone(providerResponse).(*pb.CredentialListResponse)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return &pb.CredentialListResponse{Error: credentialOperationFailure("list", "provider_error", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)}, nil
	}
	if response == nil {
		return &pb.CredentialListResponse{Error: credentialOperationFailure("list", "empty_response", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)}, nil
	}
	if response.GetError() != nil {
		response.Credentials = nil
		response.NextPageToken = ""
		identifierSensitive := credentialOutputDeclarations(declaration)[declaration.GetIdentifierKey()].GetSensitive()
		response.Error = redactCredentialOperationError("list", response.GetError(), identifierSensitive, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
		return response, nil
	}
	identifierSensitive := credentialOutputDeclarations(declaration)[declaration.GetIdentifierKey()].GetSensitive()
	for _, credential := range response.GetCredentials() {
		if credential != nil {
			credential.IdentifierSensitive = identifierSensitive
		}
	}
	return response, nil
}

func (s *credentialIssuerServer) Delete(ctx context.Context, request *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error) {
	declaration, requestError := s.validateDeleteRequest(request)
	if requestError != nil {
		return &pb.CredentialDeleteResponse{Error: requestError}, nil
	}
	providerResponse, err := s.provider.Delete(ctx, request)
	var response *pb.CredentialDeleteResponse
	if providerResponse != nil {
		response = proto.Clone(providerResponse).(*pb.CredentialDeleteResponse)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, status.FromContextError(ctxErr).Err()
		}
		return credentialDeleteFailure("provider_error", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN), nil
	}
	if response == nil {
		return credentialDeleteFailure("empty_response", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN), nil
	}
	identifierSensitive := credentialOutputDeclarations(declaration)[declaration.GetIdentifierKey()].GetSensitive()
	if response.GetError() != nil {
		response.Identifier = ""
		response.IdentifierSensitive = false
		response.Error = redactCredentialOperationError("delete", response.GetError(), identifierSensitive, response.GetReconciliationState())
		response.ReconciliationState = response.Error.GetReconciliationState()
		return response, nil
	}
	if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED {
		return credentialDeleteFailure("invalid_reconciliation_state", normalizeCredentialReconciliationState(response.GetReconciliationState(), pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN)), nil
	}
	if response.GetIdentifier() == "" || response.GetIdentifier() != request.GetIdentifier() {
		return credentialDeleteFailure("invalid_identifier", pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN), nil
	}
	response.IdentifierSensitive = identifierSensitive
	return response, nil
}

func (s *credentialIssuerServer) validateIssueRequest(request *pb.CredentialIssueRequest) (*pb.CredentialSourceDeclaration, *pb.CredentialOperationError) {
	if err := s.validate(); err != nil {
		return nil, credentialOperationFailure("issue", "invalid_declaration", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if request == nil || strings.TrimSpace(request.GetOperationId()) == "" {
		return nil, credentialOperationFailure("issue", "operation_id_required", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	declaration, ok := s.sources[request.GetSource()]
	if !ok {
		return nil, credentialOperationFailure("issue", "unknown_source", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if request.GetSelector() == nil || strings.TrimSpace(request.GetSelector().GetLogicalName()) == "" {
		return nil, credentialOperationFailure("issue", "selector_required", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if len(request.GetConfigJson()) > 0 && !json.Valid(request.GetConfigJson()) {
		return nil, credentialOperationFailure("issue", "invalid_config", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	return declaration, nil
}

func (s *credentialIssuerServer) validateListRequest(request *pb.CredentialListRequest) (*pb.CredentialSourceDeclaration, *pb.CredentialOperationError) {
	if err := s.validate(); err != nil {
		return nil, credentialOperationFailure("list", "invalid_declaration", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if request == nil {
		return nil, credentialOperationFailure("list", "request_required", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	declaration, ok := s.sources[request.GetSource()]
	if !ok {
		return nil, credentialOperationFailure("list", "unknown_source", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	return declaration, nil
}

func (s *credentialIssuerServer) validateDeleteRequest(request *pb.CredentialDeleteRequest) (*pb.CredentialSourceDeclaration, *pb.CredentialOperationError) {
	if err := s.validate(); err != nil {
		return nil, credentialOperationFailure("delete", "invalid_declaration", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if request == nil || strings.TrimSpace(request.GetOperationId()) == "" {
		return nil, credentialOperationFailure("delete", "operation_id_required", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	declaration, ok := s.sources[request.GetSource()]
	if !ok {
		return nil, credentialOperationFailure("delete", "unknown_source", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	if strings.TrimSpace(request.GetIdentifier()) == "" {
		return nil, credentialOperationFailure("delete", "identifier_required", false, pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED)
	}
	return declaration, nil
}

func credentialOutputDeclarations(declaration *pb.CredentialSourceDeclaration) map[string]*pb.CredentialOutputDeclaration {
	outputs := make(map[string]*pb.CredentialOutputDeclaration, len(declaration.GetOutputs()))
	for _, output := range declaration.GetOutputs() {
		outputs[output.GetKey()] = output
	}
	return outputs
}

func credentialIssueFailure(code string, state pb.CredentialReconciliationState) *pb.CredentialIssueResponse {
	return &pb.CredentialIssueResponse{
		ReconciliationState: state,
		Error:               credentialOperationFailure("issue", code, false, state),
	}
}

func credentialDeleteFailure(code string, state pb.CredentialReconciliationState) *pb.CredentialDeleteResponse {
	return &pb.CredentialDeleteResponse{
		ReconciliationState: state,
		Error:               credentialOperationFailure("delete", code, false, state),
	}
}

func credentialOperationFailure(operation, code string, retryable bool, state pb.CredentialReconciliationState) *pb.CredentialOperationError {
	return &pb.CredentialOperationError{
		Code:                code,
		Message:             fmt.Sprintf("credential issuer %s failed", operation),
		Retryable:           retryable,
		ReconciliationState: state,
	}
}

func redactCredentialOperationError(operation string, providerError *pb.CredentialOperationError, identifierSensitive bool, fallbackState pb.CredentialReconciliationState) *pb.CredentialOperationError {
	if providerError == nil {
		return nil
	}
	code := providerError.GetCode()
	if !credentialErrorCodePattern.MatchString(code) {
		code = "provider_error"
	}
	identifiers := make([]*pb.CredentialIdentifier, 0, len(providerError.GetIdentifiers()))
	for _, identifier := range providerError.GetIdentifiers() {
		if identifier != nil {
			cloned := proto.Clone(identifier).(*pb.CredentialIdentifier)
			cloned.Sensitive = identifierSensitive
			identifiers = append(identifiers, cloned)
		}
	}
	state := normalizeCredentialReconciliationState(providerError.GetReconciliationState(), fallbackState)
	retryable := providerError.GetRetryable() && credentialReconciliationAllowsRetry(state)
	return &pb.CredentialOperationError{
		Code:                code,
		Message:             fmt.Sprintf("credential issuer %s failed", operation),
		Retryable:           retryable,
		ReconciliationState: state,
		Identifiers:         identifiers,
	}
}

func normalizeCredentialReconciliationState(state, fallback pb.CredentialReconciliationState) pb.CredentialReconciliationState {
	if state == pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED {
		state = fallback
	}
	switch state {
	case pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_AMBIGUOUS:
		return state
	default:
		return pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN
	}
}

func credentialReconciliationAllowsRetry(state pb.CredentialReconciliationState) bool {
	return state == pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED
}
