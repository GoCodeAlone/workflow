package sdk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type credentialIssuerTestProvider struct {
	sources        []*pb.CredentialSourceDeclaration
	issueResponse  *pb.CredentialIssueResponse
	listResponse   *pb.CredentialListResponse
	deleteResponse *pb.CredentialDeleteResponse
}

func (p *credentialIssuerTestProvider) CredentialSources() []*pb.CredentialSourceDeclaration {
	return p.sources
}

func (p *credentialIssuerTestProvider) Issue(context.Context, *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error) {
	return p.issueResponse, nil
}

func (p *credentialIssuerTestProvider) List(context.Context, *pb.CredentialListRequest) (*pb.CredentialListResponse, error) {
	return p.listResponse, nil
}

func (p *credentialIssuerTestProvider) Delete(context.Context, *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error) {
	return p.deleteResponse, nil
}

func credentialIssuerTestSource(source string, identifierSensitive bool) *pb.CredentialSourceDeclaration {
	return &pb.CredentialSourceDeclaration{
		Source:          source,
		ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
		Outputs: []*pb.CredentialOutputDeclaration{
			{Key: "identifier", Sensitive: identifierSensitive},
			{Key: "secret", Sensitive: true},
		},
		IdentifierKey: "identifier",
	}
}

func credentialIssuerIssueRequest(source string) *pb.CredentialIssueRequest {
	return &pb.CredentialIssueRequest{
		OperationId: "operation-1",
		Source:      source,
		Selector:    &pb.CredentialSelector{LogicalName: "fixture"},
	}
}

func credentialIssuerDeleteRequest(source, identifier string) *pb.CredentialDeleteRequest {
	return &pb.CredentialDeleteRequest{
		OperationId: "delete-1",
		Source:      source,
		Identifier:  identifier,
	}
}

func TestCredentialIssuerStructuredErrorReconciliationIsNonRetryable(t *testing.T) {
	states := []pb.CredentialReconciliationState{
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_AMBIGUOUS,
	}
	for _, state := range states {
		state := state
		t.Run(state.String()+"/Issue", func(t *testing.T) {
			providerResponse := &pb.CredentialIssueResponse{
				Outputs:             []*pb.CredentialOutput{{Key: "secret", Value: []byte("must-not-leak")}},
				Identifier:          "must-not-leak",
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				Error: &pb.CredentialOperationError{
					Code:                "provider_error",
					Message:             "must-not-leak",
					Retryable:           true,
					ReconciliationState: state,
				},
			}
			original := proto.Clone(providerResponse).(*pb.CredentialIssueResponse)
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources:       []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				issueResponse: providerResponse,
			})
			response, err := server.Issue(context.Background(), credentialIssuerIssueRequest("fixture"))
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			assertUnsafeCredentialError(t, response.GetError(), response.GetReconciliationState(), state)
			if len(response.GetOutputs()) != 0 || response.GetIdentifier() != "" || strings.Contains(response.String(), "must-not-leak") {
				t.Fatalf("Issue error leaked provider values: %v", response)
			}
			if !proto.Equal(providerResponse, original) {
				t.Fatalf("Issue mutated provider response:\n got %v\nwant %v", providerResponse, original)
			}
		})

		t.Run(state.String()+"/Delete", func(t *testing.T) {
			providerResponse := &pb.CredentialDeleteResponse{
				Identifier:          "must-not-leak",
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				Error: &pb.CredentialOperationError{
					Code:                "provider_error",
					Message:             "must-not-leak",
					Retryable:           true,
					ReconciliationState: state,
				},
			}
			original := proto.Clone(providerResponse).(*pb.CredentialDeleteResponse)
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources:        []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				deleteResponse: providerResponse,
			})
			response, err := server.Delete(context.Background(), credentialIssuerDeleteRequest("fixture", "expected-id"))
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			assertUnsafeCredentialError(t, response.GetError(), response.GetReconciliationState(), state)
			if response.GetIdentifier() != "" || strings.Contains(response.String(), "must-not-leak") {
				t.Fatalf("Delete error leaked provider values: %v", response)
			}
			if !proto.Equal(providerResponse, original) {
				t.Fatalf("Delete mutated provider response:\n got %v\nwant %v", providerResponse, original)
			}
		})
	}
}

func assertUnsafeCredentialError(t *testing.T, operationError *pb.CredentialOperationError, responseState, wantState pb.CredentialReconciliationState) {
	t.Helper()
	if operationError == nil {
		t.Fatal("missing structured operation error")
	}
	if operationError.GetRetryable() {
		t.Fatalf("unsafe reconciliation state %s remained retryable", operationError.GetReconciliationState())
	}
	if operationError.GetReconciliationState() != wantState || responseState != wantState {
		t.Fatalf("reconciliation mismatch: response=%s error=%s want=%s", responseState, operationError.GetReconciliationState(), wantState)
	}
}

func TestCredentialIssuerIssueRequiresCoherentIdentifierOutput(t *testing.T) {
	tests := []struct {
		name               string
		response           *pb.CredentialIssueResponse
		wantIdentifier     string
		wantError          string
		forbiddenFragments []string
	}{
		{
			name: "derive response identifier",
			response: &pb.CredentialIssueResponse{
				Outputs: []*pb.CredentialOutput{
					{Key: "identifier", Value: []byte("derived-id")},
					{Key: "secret", Value: []byte("secret-value")},
				},
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
			},
			wantIdentifier: "derived-id",
		},
		{
			name: "mismatch",
			response: &pb.CredentialIssueResponse{
				Outputs:             []*pb.CredentialOutput{{Key: "identifier", Value: []byte("output-secret-id")}},
				Identifier:          "response-secret-id",
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
			},
			wantError:          "invalid_identifier",
			forbiddenFragments: []string{"output-secret-id", "response-secret-id"},
		},
		{
			name: "missing identifier output",
			response: &pb.CredentialIssueResponse{
				Outputs:             []*pb.CredentialOutput{{Key: "secret", Value: []byte("secret-value")}},
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
			},
			wantError:          "invalid_identifier",
			forbiddenFragments: []string{"secret-value"},
		},
		{
			name: "empty identifier output",
			response: &pb.CredentialIssueResponse{
				Outputs:             []*pb.CredentialOutput{{Key: "identifier"}},
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
			},
			wantError: "invalid_identifier",
		},
		{
			name: "duplicate identifier output",
			response: &pb.CredentialIssueResponse{
				Outputs: []*pb.CredentialOutput{
					{Key: "identifier", Value: []byte("first-secret-id")},
					{Key: "identifier", Value: []byte("second-secret-id")},
				},
				ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
			},
			wantError:          "invalid_identifier",
			forbiddenFragments: []string{"first-secret-id", "second-secret-id"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources:       []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				issueResponse: test.response,
			})
			response, err := server.Issue(context.Background(), credentialIssuerIssueRequest("fixture"))
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			if test.wantError == "" {
				if response.GetError() != nil || response.GetIdentifier() != test.wantIdentifier {
					t.Fatalf("Issue response = %v, want identifier %q", response, test.wantIdentifier)
				}
				return
			}
			if response.GetError().GetCode() != test.wantError || response.GetError().GetRetryable() {
				t.Fatalf("Issue error = %v, want nonretryable %q", response.GetError(), test.wantError)
			}
			if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED || response.GetError().GetReconciliationState() != response.GetReconciliationState() {
				t.Fatalf("Issue invalid response reconciliation mismatch: %v", response)
			}
			if len(response.GetOutputs()) != 0 || response.GetIdentifier() != "" {
				t.Fatalf("Issue invalid response retained values: %v", response)
			}
			for _, fragment := range test.forbiddenFragments {
				if strings.Contains(response.String(), fragment) {
					t.Fatalf("Issue error leaked %q: %v", fragment, response)
				}
			}
		})
	}
}

func TestCredentialIssuerRejectsNonConfirmedSuccessStates(t *testing.T) {
	states := []pb.CredentialReconciliationState{
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN_CREATED,
		pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_AMBIGUOUS,
	}
	for _, state := range states {
		state := state
		wantState := state
		if wantState == pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNSPECIFIED {
			wantState = pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN
		}
		t.Run(state.String()+"/Issue", func(t *testing.T) {
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources: []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				issueResponse: &pb.CredentialIssueResponse{
					Outputs:             []*pb.CredentialOutput{{Key: "identifier", Value: []byte("must-not-leak")}},
					ReconciliationState: state,
				},
			})
			response, err := server.Issue(context.Background(), credentialIssuerIssueRequest("fixture"))
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			assertUnsafeCredentialError(t, response.GetError(), response.GetReconciliationState(), wantState)
			if len(response.GetOutputs()) != 0 || strings.Contains(response.String(), "must-not-leak") {
				t.Fatalf("Issue unsafe success leaked values: %v", response)
			}
		})
		t.Run(state.String()+"/Delete", func(t *testing.T) {
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources:        []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				deleteResponse: &pb.CredentialDeleteResponse{Identifier: "must-not-leak", ReconciliationState: state},
			})
			response, err := server.Delete(context.Background(), credentialIssuerDeleteRequest("fixture", "expected-id"))
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			assertUnsafeCredentialError(t, response.GetError(), response.GetReconciliationState(), wantState)
			if response.GetIdentifier() != "" || strings.Contains(response.String(), "must-not-leak") {
				t.Fatalf("Delete unsafe success leaked values: %v", response)
			}
		})
	}
}

func TestCredentialIssuerDeleteRequiresExactIdentifierAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		name       string
		identifier string
		wantError  bool
	}{
		{name: "exact", identifier: "expected-id"},
		{name: "empty", wantError: true},
		{name: "mismatch", identifier: "other-secret-id", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newCredentialIssuerServer(&credentialIssuerTestProvider{
				sources: []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)},
				deleteResponse: &pb.CredentialDeleteResponse{
					Identifier:          test.identifier,
					ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				},
			})
			response, err := server.Delete(context.Background(), credentialIssuerDeleteRequest("fixture", "expected-id"))
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if !test.wantError {
				if response.GetError() != nil || response.GetIdentifier() != "expected-id" {
					t.Fatalf("Delete exact response = %v", response)
				}
				return
			}
			if response.GetError().GetCode() != "invalid_identifier" || response.GetError().GetRetryable() {
				t.Fatalf("Delete invalid acknowledgement error = %v", response.GetError())
			}
			if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN || response.GetError().GetReconciliationState() != response.GetReconciliationState() {
				t.Fatalf("Delete invalid acknowledgement reconciliation = %v", response)
			}
			if response.GetIdentifier() != "" || (test.identifier != "" && strings.Contains(response.String(), test.identifier)) {
				t.Fatalf("Delete invalid acknowledgement leaked identifier: %v", response)
			}
		})
	}
}

func TestCredentialIssuerClonesSharedResponsesPerSource(t *testing.T) {
	sharedIssue := &pb.CredentialIssueResponse{
		Outputs:             []*pb.CredentialOutput{{Key: "identifier", Value: []byte("shared-id")}, {Key: "secret", Value: []byte("shared-secret")}},
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}
	sharedList := &pb.CredentialListResponse{Credentials: []*pb.CredentialRecord{{Identifier: "shared-id"}}}
	sharedDelete := &pb.CredentialDeleteResponse{
		Identifier:          "shared-id",
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}
	provider := &credentialIssuerTestProvider{
		sources: []*pb.CredentialSourceDeclaration{
			credentialIssuerTestSource("public", false),
			credentialIssuerTestSource("private", true),
		},
		issueResponse:  sharedIssue,
		listResponse:   sharedList,
		deleteResponse: sharedDelete,
	}
	server := newCredentialIssuerServer(provider)

	const iterations = 32
	var wait sync.WaitGroup
	errors := make(chan error, iterations*2)
	for _, source := range []string{"public", "private"} {
		source := source
		wantSensitive := source == "private"
		for range iterations {
			wait.Add(1)
			go func() {
				defer wait.Done()
				issued, err := server.Issue(context.Background(), credentialIssuerIssueRequest(source))
				if err != nil || issued.GetError() != nil || issued.GetIdentifier() != "shared-id" || issued.GetIdentifierSensitive() != wantSensitive || issued.GetOutputs()[0].GetSensitive() != wantSensitive {
					errors <- fmt.Errorf("Issue(%s) = %v, %v", source, issued, err)
					return
				}
				listed, err := server.List(context.Background(), &pb.CredentialListRequest{Source: source})
				if err != nil || listed.GetError() != nil || listed.GetCredentials()[0].GetIdentifierSensitive() != wantSensitive {
					errors <- fmt.Errorf("List(%s) = %v, %v", source, listed, err)
					return
				}
				deleted, err := server.Delete(context.Background(), credentialIssuerDeleteRequest(source, "shared-id"))
				if err != nil || deleted.GetError() != nil || deleted.GetIdentifierSensitive() != wantSensitive {
					errors <- fmt.Errorf("Delete(%s) = %v, %v", source, deleted, err)
				}
			}()
		}
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if sharedIssue.GetIdentifier() != "" || sharedIssue.GetOutputs()[0].GetSensitive() || sharedList.GetCredentials()[0].GetIdentifierSensitive() || sharedDelete.GetIdentifierSensitive() {
		t.Fatalf("provider-owned shared responses were mutated: issue=%v list=%v delete=%v", sharedIssue, sharedList, sharedDelete)
	}
}

func TestMergeProviderServiceContractsCanonicalizesCredentialIssuer(t *testing.T) {
	provider := &credentialIssuerTestProvider{sources: []*pb.CredentialSourceDeclaration{credentialIssuerTestSource("fixture", false)}}
	services := &providerServices{credentialIssuer: newCredentialIssuerServer(provider)}
	canonical := services.contractDescriptors()[0]
	unrelated := &pb.ContractDescriptor{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.Unrelated", ContractType: "fixture.unrelated"}

	tests := []struct {
		name       string
		collisions []*pb.ContractDescriptor
	}{
		{
			name: "generic service-name collision",
			collisions: []*pb.ContractDescriptor{{
				Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
				ServiceName: pb.CredentialIssuer_ServiceDesc.ServiceName,
			}},
		},
		{
			name: "stale canonical descriptor",
			collisions: []*pb.ContractDescriptor{{
				Kind:            pb.ContractKind_CONTRACT_KIND_SERVICE,
				ServiceName:     pb.CredentialIssuer_ServiceDesc.ServiceName,
				ContractType:    CredentialIssuerContractID,
				ProtocolVersion: "0",
			}},
		},
		{
			name: "conflicting service and contract id descriptors",
			collisions: []*pb.ContractDescriptor{
				{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: pb.CredentialIssuer_ServiceDesc.ServiceName, ContractType: "wrong.contract"},
				{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.WrongService", ContractType: CredentialIssuerContractID},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &pb.ContractRegistry{
				Contracts:         append([]*pb.ContractDescriptor{unrelated}, test.collisions...),
				FileDescriptorSet: &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("fixture.proto")}}},
			}
			original := proto.Clone(base).(*pb.ContractRegistry)
			merged := mergeProviderServiceContracts(base, services)
			if !proto.Equal(base, original) {
				t.Fatalf("merge mutated base registry:\n got %v\nwant %v", base, original)
			}
			if !proto.Equal(merged.GetFileDescriptorSet(), base.GetFileDescriptorSet()) {
				t.Fatalf("merge lost FileDescriptorSet: %v", merged)
			}
			var canonicalCount int
			var unrelatedFound bool
			for _, descriptor := range merged.GetContracts() {
				if descriptor.GetServiceName() == "fixture.Unrelated" {
					unrelatedFound = true
				}
				if descriptor.GetServiceName() == pb.CredentialIssuer_ServiceDesc.ServiceName || descriptor.GetContractType() == CredentialIssuerContractID {
					canonicalCount++
					if !proto.Equal(descriptor, canonical) {
						t.Errorf("non-canonical credential issuer descriptor: %v", descriptor)
					}
				}
			}
			if canonicalCount != 1 || !unrelatedFound {
				t.Fatalf("merged contracts = %v, want one canonical and unrelated descriptor", merged.GetContracts())
			}
		})
	}
}
