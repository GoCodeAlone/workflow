package external

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCredentialIssuerExternalLoaderLifecycle(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "credential-fixture"
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}

	buildCredentialIssuerFixture(t, filepath.Join(pluginDir, pluginName), true)
	writeCredentialFixtureManifest(t, pluginDir, pluginName)

	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("load credential issuer fixture: %v", err)
	}
	client := adapter.client.CredentialIssuerClient()
	if client == nil {
		t.Fatal("loaded plugin has no typed credential issuer client")
	}

	t.Run("runtime declarations and contract advertisement", func(t *testing.T) {
		if !registryAdvertisesService(adapter.ContractRegistry(), pb.CredentialIssuer_ServiceDesc.ServiceName) {
			t.Fatalf("contract registry does not advertise %s: %v", pb.CredentialIssuer_ServiceDesc.ServiceName, adapter.ContractRegistry())
		}
		if !registryContainsContractType(adapter.ContractRegistry(), "fixture.service") {
			t.Fatalf("credential issuer advertisement replaced provider contracts: %v", adapter.ContractRegistry())
		}
		response, err := client.DescribeSources(context.Background(), &pb.CredentialSourceDeclarationsRequest{})
		if err != nil {
			t.Fatalf("DescribeSources: %v", err)
		}
		if response.GetError() != nil {
			t.Fatalf("DescribeSources returned error: %v", response.GetError())
		}
		if len(response.GetSources()) != 1 {
			t.Fatalf("source declarations = %d, want 1", len(response.GetSources()))
		}
		source := response.GetSources()[0]
		if source.GetSource() != "fixture.api-key" || source.GetIdentifierKey() != "identifier" {
			t.Fatalf("unexpected source declaration: %v", source)
		}
		if source.GetConcurrencyMode() != pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT {
			t.Fatalf("concurrency mode = %s", source.GetConcurrencyMode())
		}
		if len(source.GetOutputs()) != 2 || source.GetOutputs()[0].GetSensitive() || !source.GetOutputs()[1].GetSensitive() {
			t.Fatalf("runtime output declarations lost sensitivity: %v", source.GetOutputs())
		}
	})

	t.Run("issue list delete", func(t *testing.T) {
		issued, err := client.Issue(context.Background(), &pb.CredentialIssueRequest{
			OperationId: "operation-1",
			Source:      "fixture.api-key",
			Selector:    &pb.CredentialSelector{LogicalName: "primary"},
			ConfigJson:  []byte(`{"scope":"test"}`),
		})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if issued.GetError() != nil {
			t.Fatalf("Issue returned error: %v", issued.GetError())
		}
		if issued.GetIdentifier() != "credential-operation-1" || issued.GetIdentifierSensitive() {
			t.Fatalf("unexpected issued identifier: %v", issued)
		}
		if issued.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED {
			t.Fatalf("issue reconciliation = %s", issued.GetReconciliationState())
		}
		if len(issued.GetOutputs()) != 2 || issued.GetOutputs()[0].GetSensitive() || !issued.GetOutputs()[1].GetSensitive() {
			t.Fatalf("SDK did not enforce declared output sensitivity: %v", issued.GetOutputs())
		}

		firstPage, err := client.List(context.Background(), &pb.CredentialListRequest{
			Source:   "fixture.api-key",
			Selector: &pb.CredentialSelector{LogicalName: "primary"},
			PageSize: 1,
		})
		if err != nil {
			t.Fatalf("List first page: %v", err)
		}
		if len(firstPage.GetCredentials()) != 1 || firstPage.GetNextPageToken() != "page-2" {
			t.Fatalf("unexpected first page: %v", firstPage)
		}
		secondPage, err := client.List(context.Background(), &pb.CredentialListRequest{
			Source:    "fixture.api-key",
			Selector:  &pb.CredentialSelector{LogicalName: "primary"},
			PageToken: firstPage.GetNextPageToken(),
			PageSize:  1,
		})
		if err != nil {
			t.Fatalf("List second page: %v", err)
		}
		if len(secondPage.GetCredentials()) != 1 || secondPage.GetNextPageToken() != "" {
			t.Fatalf("unexpected second page: %v", secondPage)
		}

		deleted, err := client.Delete(context.Background(), &pb.CredentialDeleteRequest{
			OperationId: "delete-1",
			Source:      "fixture.api-key",
			Identifier:  issued.GetIdentifier(),
		})
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if deleted.GetError() != nil || deleted.GetIdentifier() != issued.GetIdentifier() {
			t.Fatalf("unexpected delete response: %v", deleted)
		}
	})

	t.Run("cancellation propagates to provider", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := client.Issue(ctx, &pb.CredentialIssueRequest{
				OperationId: "cancel-1",
				Source:      "fixture.api-key",
				Selector:    &pb.CredentialSelector{LogicalName: "wait-for-cancel"},
			})
			result <- err
		}()
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "entered"), "provider did not enter Issue")
		cancel()
		var err error
		select {
		case err = <-result:
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Issue did not return")
		}
		if status.Code(err) != codes.Canceled {
			t.Fatalf("Issue cancellation status = %s, want Canceled (error %v)", status.Code(err), err)
		}
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "cancelled"), "provider did not observe canceled gRPC context")
	})

	t.Run("undeclared output fails closed", func(t *testing.T) {
		response, err := client.Issue(context.Background(), &pb.CredentialIssueRequest{
			OperationId: "undeclared-1",
			Source:      "fixture.api-key",
			Selector:    &pb.CredentialSelector{LogicalName: "undeclared-output"},
		})
		if err != nil {
			t.Fatalf("Issue undeclared output: %v", err)
		}
		if response.GetError().GetCode() != "undeclared_output" {
			t.Fatalf("undeclared output code = %q", response.GetError().GetCode())
		}
		if len(response.GetOutputs()) != 0 || response.GetIdentifier() != "" {
			t.Fatalf("undeclared response retained values: %v", response)
		}
		serialized := response.String()
		for _, forbidden := range []string{"provider-controlled-output-name", "undeclared-secret-value"} {
			if strings.Contains(serialized, forbidden) {
				t.Fatalf("undeclared output response leaked %q: %s", forbidden, serialized)
			}
		}
	})

	t.Run("provider errors are structured and redacted", func(t *testing.T) {
		for _, logicalName := range []string{"structured-error", "plain-error"} {
			response, err := client.Issue(context.Background(), &pb.CredentialIssueRequest{
				OperationId: "error-1",
				Source:      "fixture.api-key",
				Selector:    &pb.CredentialSelector{LogicalName: logicalName},
				ConfigJson:  []byte(`{"token":"request-secret-value"}`),
			})
			if err != nil {
				t.Fatalf("Issue %s: %v", logicalName, err)
			}
			if response.GetError() == nil || response.GetError().GetCode() == "" {
				t.Fatalf("Issue %s did not return structured error: %v", logicalName, response)
			}
			if len(response.GetOutputs()) != 0 || response.GetIdentifier() != "" {
				t.Fatalf("Issue %s retained output values: %v", logicalName, response)
			}
			serialized := response.String()
			for _, forbidden := range []string{"structured-secret-value", "plain-secret-value", "request-secret-value"} {
				if strings.Contains(serialized, forbidden) {
					t.Fatalf("Issue %s leaked %q: %s", logicalName, forbidden, serialized)
				}
			}
		}
	})
}

func TestCredentialIssuerOptionalServiceBackwardCompatibility(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "legacy-fixture"
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}
	buildCredentialIssuerFixture(t, filepath.Join(pluginDir, pluginName), false)
	writeCredentialFixtureManifest(t, pluginDir, pluginName)

	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("load legacy fixture: %v", err)
	}
	if registryAdvertisesService(adapter.ContractRegistry(), pb.CredentialIssuer_ServiceDesc.ServiceName) {
		t.Fatalf("legacy plugin unexpectedly advertised %s", pb.CredentialIssuer_ServiceDesc.ServiceName)
	}
	client := adapter.client.CredentialIssuerClient()
	if client == nil {
		t.Fatal("legacy plugin has no shared gRPC connection")
	}
	_, err = client.DescribeSources(context.Background(), &pb.CredentialSourceDeclarationsRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("legacy DescribeSources status = %s, want Unimplemented (error %v)", status.Code(err), err)
	}
}

func registryAdvertisesService(registry *pb.ContractRegistry, serviceName string) bool {
	if registry == nil {
		return false
	}
	for _, descriptor := range registry.GetContracts() {
		if descriptor.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE && descriptor.GetServiceName() == serviceName {
			return true
		}
	}
	return false
}

func registryContainsContractType(registry *pb.ContractRegistry, contractType string) bool {
	if registry == nil {
		return false
	}
	for _, descriptor := range registry.GetContracts() {
		if descriptor.GetContractType() == contractType {
			return true
		}
	}
	return false
}

func waitForCredentialFixtureMarker(t *testing.T, path, failure string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeCredentialFixtureManifest(t *testing.T, pluginDir, pluginName string) {
	t.Helper()
	manifest := `{
		"name":"` + pluginName + `",
		"version":"1.0.0",
		"author":"Workflow tests",
		"description":"credential issuer transport fixture",
		"credentialSources":[{
			"source":"fixture.api-key",
			"concurrencyMode":"provider_idempotent",
			"outputs":[{"key":"identifier","sensitive":false},{"key":"secret"}],
			"identifierKey":"identifier"
		}]
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
}

func buildCredentialIssuerFixture(t *testing.T, output string, registerIssuer bool) {
	t.Helper()
	sourceDir := t.TempDir()
	providerName := filepath.Base(output)
	serve := "sdk.Serve(p)"
	issuerImplementation := ""
	if registerIssuer {
		serve = "sdk.Serve(p, sdk.WithCredentialIssuerProvider(p))"
		issuerImplementation = credentialIssuerFixtureImplementation
	}
	source := `package main

import (
	"context"
	"errors"
	"os"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type provider struct{}

var (
	_ = context.Background
	_ = errors.New
	_ = os.WriteFile
	_ = (*pb.CredentialIssueRequest)(nil)
)

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name: "` + providerName + `",
		Version: "1.0.0",
		Author: "Workflow tests",
		Description: "credential issuer transport fixture",
	}
}

func (*provider) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{{
		Kind: pb.ContractKind_CONTRACT_KIND_SERVICE,
		Mode: pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		ServiceName: "fixture.LegacyService",
		ContractType: "fixture.service",
	}}}
}

` + issuerImplementation + `

func main() {
	p := &provider{}
	` + serve + `
}
`
	sourcePath := filepath.Join(sourceDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write credential issuer fixture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, sourcePath)
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build credential issuer fixture for %s/%s: %v\n%s", runtime.GOOS, runtime.GOARCH, err, strings.TrimSpace(string(combined)))
	}
	if info, err := os.Stat(output); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("fixture is not executable: %s (%v)", output, err)
	}
}

const credentialIssuerFixtureImplementation = `
func (*provider) CredentialSources() []*pb.CredentialSourceDeclaration {
	return []*pb.CredentialSourceDeclaration{{
		Source: "fixture.api-key",
		ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
		Outputs: []*pb.CredentialOutputDeclaration{
			{Key: "identifier", Sensitive: false},
			{Key: "secret", Sensitive: true},
		},
		IdentifierKey: "identifier",
	}}
}

func (*provider) Issue(ctx context.Context, request *pb.CredentialIssueRequest) (*pb.CredentialIssueResponse, error) {
	switch request.GetSelector().GetLogicalName() {
	case "wait-for-cancel":
		_ = os.WriteFile("entered", []byte("observed"), 0600)
		<-ctx.Done()
		_ = os.WriteFile("cancelled", []byte("observed"), 0600)
		return nil, ctx.Err()
	case "undeclared-output":
		return &pb.CredentialIssueResponse{
			Outputs: []*pb.CredentialOutput{
				{Key: "identifier", Value: []byte("credential-undeclared")},
				{Key: "provider-controlled-output-name", Value: []byte("undeclared-secret-value")},
			},
			Identifier: "credential-undeclared",
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		}, nil
	case "structured-error":
		return &pb.CredentialIssueResponse{
			Outputs: []*pb.CredentialOutput{{Key: "secret", Value: []byte("structured-secret-value")}},
			Identifier: "structured-secret-value",
			Error: &pb.CredentialOperationError{Code: "provider_rejected", Message: "structured-secret-value"},
		}, nil
	case "plain-error":
		return nil, errors.New("plain-secret-value")
	default:
		identifier := "credential-" + request.GetOperationId()
		return &pb.CredentialIssueResponse{
			Outputs: []*pb.CredentialOutput{
				{Key: "identifier", Value: []byte(identifier), Sensitive: true},
				{Key: "secret", Value: []byte("issued-secret-value"), Sensitive: false},
			},
			Identifier: identifier,
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		}, nil
	}
}

func (*provider) List(_ context.Context, request *pb.CredentialListRequest) (*pb.CredentialListResponse, error) {
	if request.GetPageToken() == "" {
		return &pb.CredentialListResponse{
			Credentials: []*pb.CredentialRecord{{Identifier: "credential-page-1", LogicalName: "primary"}},
			NextPageToken: "page-2",
		}, nil
	}
	return &pb.CredentialListResponse{
		Credentials: []*pb.CredentialRecord{{Identifier: "credential-page-2", LogicalName: "primary"}},
	}, nil
}

func (*provider) Delete(_ context.Context, request *pb.CredentialDeleteRequest) (*pb.CredentialDeleteResponse, error) {
	return &pb.CredentialDeleteResponse{
		Identifier: request.GetIdentifier(),
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}, nil
}
`
