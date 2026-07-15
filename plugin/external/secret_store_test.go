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
	"google.golang.org/protobuf/proto"
)

func TestSecretStoreExternalLoaderLifecycle(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "secret-store-fixture"
	pluginDir := prepareSecretStoreFixture(t, pluginsDir, pluginName, true, false)

	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if !registryAdvertisesService(adapter.ContractRegistry(), pb.SecretStore_ServiceDesc.ServiceName) {
		t.Fatalf("secret store contract not advertised: %v", adapter.ContractRegistry())
	}
	if !registryContainsContractType(adapter.ContractRegistry(), "fixture.service") {
		t.Fatalf("secret store advertisement replaced provider contracts: %v", adapter.ContractRegistry())
	}
	contract := findSecretStoreServiceContract(adapter.ContractRegistry())
	if contract.GetKind() != pb.ContractKind_CONTRACT_KIND_SERVICE ||
		contract.GetMode() != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO ||
		contract.GetContractType() != "workflow.provider.secret-store" ||
		contract.GetProtoPackage() != "workflow.plugin.external.secrets" ||
		contract.GetGoImportPath() != "github.com/GoCodeAlone/workflow/plugin/external/proto" ||
		contract.GetProtocolVersion() != "1" {
		t.Fatalf("secret store contract metadata = %v", contract)
	}
	client := adapter.client.SecretStoreClient()
	if client == nil {
		t.Fatal("loaded plugin has no typed secret store client")
	}

	described, err := client.DescribeSecretStores(context.Background(), &pb.SecretStoreDeclarationsRequest{})
	if err != nil || described.GetError() != nil {
		t.Fatalf("DescribeSecretStores = %v, %v", described, err)
	}
	if len(described.GetStores()) != 2 {
		t.Fatalf("runtime secret store declarations = %v", described.GetStores())
	}
	if got := described.GetStores()[0]; got.GetType() != "fixture.get-only" || !sameStrings(got.GetOperations(), []string{"get"}) || !sameStrings(got.GetScopes(), []string{"account"}) {
		t.Fatalf("first declaration = %v", got)
	}
	if got := described.GetStores()[1]; got.GetType() != "fixture.store" || !sameStrings(got.GetOperations(), []string{"get", "list", "stat_all", "check_access"}) || !sameStrings(got.GetScopes(), []string{"account", "region"}) {
		t.Fatalf("second declaration = %v", got)
	}

	target := completeSecretStoreTarget()
	t.Run("Get preserves opaque config and caller storage", func(t *testing.T) {
		request := &pb.SecretStoreGetRequest{Target: target, Key: "database/password"}
		before := proto.Clone(request).(*pb.SecretStoreGetRequest)
		response, callErr := client.Get(context.Background(), request)
		if callErr != nil || response.GetError() != nil {
			t.Fatalf("Get = %v, %v", response, callErr)
		}
		want := "value:database/password:" + string(target.GetConfigJson())
		if string(response.GetResult().GetValue()) != want {
			t.Fatalf("Get value = %q, want %q", response.GetResult().GetValue(), want)
		}
		if !proto.Equal(request, before) {
			t.Fatalf("Get mutated caller request:\n got %v\nwant %v", request, before)
		}
		response.Result.Value[0] = 'X'
		again, callErr := client.Get(context.Background(), before)
		if callErr != nil || string(again.GetResult().GetValue()) != want {
			t.Fatalf("Get response storage was retained: %v, %v", again, callErr)
		}
	})

	t.Run("List paginates names without values", func(t *testing.T) {
		first, callErr := client.List(context.Background(), &pb.SecretStoreListRequest{Target: target, PageSize: 2})
		if callErr != nil || first.GetError() != nil || !sameStrings(first.GetResult().GetNames(), []string{"alpha", "beta"}) || string(first.GetResult().GetNextPageToken()) != "list-page-2" {
			t.Fatalf("List first page = %v, %v", first, callErr)
		}
		second, callErr := client.List(context.Background(), &pb.SecretStoreListRequest{Target: target, PageSize: 2, PageToken: first.GetResult().GetNextPageToken()})
		if callErr != nil || second.GetError() != nil || !sameStrings(second.GetResult().GetNames(), []string{"gamma"}) || len(second.GetResult().GetNextPageToken()) != 0 {
			t.Fatalf("List second page = %v, %v", second, callErr)
		}
		if strings.Contains(first.String()+second.String(), "fixture-sensitive-value") {
			t.Fatal("List returned a secret value")
		}
	})

	t.Run("StatAll paginates metadata without values", func(t *testing.T) {
		first, callErr := client.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: target, PageSize: 2})
		if callErr != nil || first.GetError() != nil || len(first.GetResult().GetItems()) != 2 || string(first.GetResult().GetNextPageToken()) != "stat-page-2" {
			t.Fatalf("StatAll first page = %v, %v", first, callErr)
		}
		if first.GetResult().GetItems()[0].GetName() != "alpha" || !first.GetResult().GetItems()[0].GetExists() || first.GetResult().GetItems()[0].GetUpdatedAt().GetSeconds() != 1_700_000_000 {
			t.Fatalf("StatAll first item = %v", first.GetResult().GetItems()[0])
		}
		second, callErr := client.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: target, PageSize: 2, PageToken: first.GetResult().GetNextPageToken()})
		if callErr != nil || second.GetError() != nil || len(second.GetResult().GetItems()) != 1 || second.GetResult().GetItems()[0].GetName() != "gamma" || len(second.GetResult().GetNextPageToken()) != 0 {
			t.Fatalf("StatAll second page = %v, %v", second, callErr)
		}
		if strings.Contains(first.String()+second.String(), "fixture-sensitive-value") {
			t.Fatal("StatAll returned a secret value")
		}
	})

	t.Run("CheckAccess returns only success or failure", func(t *testing.T) {
		response, callErr := client.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: target})
		if callErr != nil || response.GetError() != nil {
			t.Fatalf("CheckAccess = %v, %v", response, callErr)
		}
		if strings.Contains(response.String(), "fixture-sensitive-value") || strings.Contains(response.String(), "request-secret") {
			t.Fatalf("CheckAccess returned secret data: %v", response)
		}
	})

	t.Run("type scope and operation mismatch fail before provider", func(t *testing.T) {
		tests := []struct {
			name       string
			call       func() (proto.Message, error)
			wantCode   string
			markerPath string
		}{
			{
				name: "exact type",
				call: func() (proto.Message, error) {
					return client.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "fixture.store ", Scope: "must-not-run-type", ConfigJson: []byte(`{}`)}, Key: "key"})
				},
				wantCode: "unsupported_store_type", markerPath: filepath.Join(pluginDir, "called-get-must-not-run-type"),
			},
			{
				name: "exact scope",
				call: func() (proto.Message, error) {
					return client.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "fixture.store", Scope: "must-not-run-scope", ConfigJson: []byte(`{}`)}, Key: "key"})
				},
				wantCode: "unsupported_scope", markerPath: filepath.Join(pluginDir, "called-get-must-not-run-scope"),
			},
			{
				name: "declared operation",
				call: func() (proto.Message, error) {
					return client.List(context.Background(), &pb.SecretStoreListRequest{Target: &pb.SecretStoreTarget{Type: "fixture.get-only", Scope: "account", ConfigJson: []byte(`{}`)}})
				},
				wantCode: "unsupported_operation", markerPath: filepath.Join(pluginDir, "called-list-account"),
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				response, callErr := test.call()
				if callErr != nil || secretStoreExternalError(response).GetCode() != test.wantCode || secretStoreExternalHasResult(response) {
					t.Fatalf("mismatch response = %v, %v", response, callErr)
				}
				if _, statErr := os.Stat(test.markerPath); !os.IsNotExist(statErr) {
					t.Fatalf("provider executed before mismatch rejection: %v", statErr)
				}
			})
		}
	})

	t.Run("provider errors are structured and redacted", func(t *testing.T) {
		for _, test := range []struct {
			key       string
			wantCode  string
			retryable bool
		}{
			{key: "plain-error", wantCode: "provider_error"},
			{key: "structured-error", wantCode: "api_denied", retryable: true},
		} {
			response, callErr := client.Get(context.Background(), &pb.SecretStoreGetRequest{Target: target, Key: test.key})
			if callErr != nil || response.GetError().GetCode() != test.wantCode || response.GetError().GetRetryable() != test.retryable || response.GetResult() != nil {
				t.Fatalf("Get(%s) = %v, %v", test.key, response, callErr)
			}
			for _, forbidden := range []string{"plain-provider-secret", "structured-provider-secret", "request-secret", "fixture-sensitive-value"} {
				if strings.Contains(response.String(), forbidden) {
					t.Fatalf("Get(%s) leaked %q: %v", test.key, forbidden, response)
				}
			}
		}
	})

	t.Run("cancellation propagates to provider", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, callErr := client.Get(ctx, &pb.SecretStoreGetRequest{Target: target, Key: "wait-for-cancel"})
			result <- callErr
		}()
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "entered"), "provider did not enter Get")
		cancel()
		select {
		case callErr := <-result:
			if status.Code(callErr) != codes.Canceled {
				t.Fatalf("Get cancellation = %s, want Canceled (error %v)", status.Code(callErr), callErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Get did not return")
		}
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "cancelled"), "provider did not observe canceled context")
	})
}

func TestSecretStoreOptionalServiceBackwardCompatibility(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "legacy-secret-store-fixture"
	prepareSecretStoreFixture(t, pluginsDir, pluginName, false, false)
	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin legacy: %v", err)
	}
	if registryAdvertisesService(adapter.ContractRegistry(), pb.SecretStore_ServiceDesc.ServiceName) {
		t.Fatalf("legacy plugin unexpectedly advertised %s", pb.SecretStore_ServiceDesc.ServiceName)
	}
	client := adapter.client.SecretStoreClient()
	if client == nil {
		t.Fatal("legacy plugin has no shared gRPC connection")
	}
	_, err = client.DescribeSecretStores(context.Background(), &pb.SecretStoreDeclarationsRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("legacy DescribeSecretStores status = %s, want Unimplemented (error %v)", status.Code(err), err)
	}
}

func TestSecretStoreDuplicateRuntimeTypeFailsBeforeLoad(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "duplicate-secret-store-fixture"
	prepareSecretStoreFixture(t, pluginsDir, pluginName, true, true)
	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	if _, err := manager.LoadPlugin(pluginName); err == nil {
		t.Fatal("LoadPlugin accepted duplicate runtime declarations")
	}
}

func completeSecretStoreTarget() *pb.SecretStoreTarget {
	return &pb.SecretStoreTarget{
		Type:       "fixture.store",
		Scope:      "region",
		ConfigJson: []byte(`{"region":"us-test-1","nested":{"token":"request-secret"}}`),
	}
}

func findSecretStoreServiceContract(registry *pb.ContractRegistry) *pb.ContractDescriptor {
	if registry == nil {
		return nil
	}
	for _, descriptor := range registry.GetContracts() {
		if descriptor.GetServiceName() == pb.SecretStore_ServiceDesc.ServiceName {
			return descriptor
		}
	}
	return nil
}

func secretStoreExternalError(message proto.Message) *pb.SecretStoreError {
	switch response := message.(type) {
	case *pb.SecretStoreGetResponse:
		return response.GetError()
	case *pb.SecretStoreListResponse:
		return response.GetError()
	case *pb.SecretStoreStatAllResponse:
		return response.GetError()
	case *pb.SecretStoreCheckAccessResponse:
		return response.GetError()
	default:
		return nil
	}
}

func secretStoreExternalHasResult(message proto.Message) bool {
	switch response := message.(type) {
	case *pb.SecretStoreGetResponse:
		return response.GetResult() != nil
	case *pb.SecretStoreListResponse:
		return response.GetResult() != nil
	case *pb.SecretStoreStatAllResponse:
		return response.GetResult() != nil
	default:
		return false
	}
}

func prepareSecretStoreFixture(t *testing.T, pluginsDir, pluginName string, register, duplicate bool) string {
	t.Helper()
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}
	buildSecretStoreFixture(t, filepath.Join(pluginDir, pluginName), pluginName, register, duplicate)
	manifest := `{
		"name":"` + pluginName + `",
		"version":"1.0.0",
		"author":"Workflow tests",
		"description":"secret store transport fixture",
		"secretStores":[
			{"type":"fixture.store","operations":["get","list","stat_all","check_access"],"scopes":["account","region"]},
			{"type":"fixture.get-only","operations":["get"],"scopes":["account"]}
		]
	}`
	if !register {
		manifest = `{"name":"` + pluginName + `","version":"1.0.0","author":"Workflow tests","description":"legacy secret store fixture"}`
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	return pluginDir
}

func buildSecretStoreFixture(t *testing.T, output, providerName string, register, duplicate bool) {
	t.Helper()
	sourceDir := t.TempDir()
	serve := "sdk.Serve(p)"
	implementation := ""
	if register {
		serve = "sdk.Serve(p, sdk.WithSecretStoreProvider(p))"
		implementation = secretStoreFixtureImplementation
	}
	duplicateLiteral := "false"
	if duplicate {
		duplicateLiteral = "true"
	}
	source := `package main

import (
	"context"
	"errors"
	"os"
	"time"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type provider struct{}

var (
	_ = context.Background
	_ = errors.New
	_ = os.WriteFile
	_ = time.Now
	_ = timestamppb.New
	duplicateDeclarations = ` + duplicateLiteral + `
)

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{Name: "` + providerName + `", Version: "1.0.0", Author: "Workflow tests", Description: "secret store transport fixture"}
}

func (*provider) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.LegacyService", ContractType: "fixture.service"}}}
}

` + implementation + `

func main() {
	p := &provider{}
	` + serve + `
}
`
	sourcePath := filepath.Join(sourceDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write secret store fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, sourcePath)
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build secret store fixture for %s/%s: %v\n%s", runtime.GOOS, runtime.GOARCH, err, strings.TrimSpace(string(combined)))
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat fixture executable %s: %v", output, err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("fixture is not executable: %s", output)
	}
}

const secretStoreFixtureImplementation = `
func (*provider) SecretStores() []*pb.SecretStoreDeclaration {
	declarations := []*pb.SecretStoreDeclaration{
		{Type: " fixture.store ", Operations: []string{" stat_all ", "check_access", " get ", "list"}, Scopes: []string{" region ", "account"}},
		{Type: "fixture.get-only", Operations: []string{"get"}, Scopes: []string{"account"}},
	}
	if duplicateDeclarations {
		declarations = append(declarations, &pb.SecretStoreDeclaration{Type: "fixture.store", Operations: []string{"get"}, Scopes: []string{"account"}})
	}
	return declarations
}

func (*provider) Get(ctx context.Context, request *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error) {
	markSecretStoreCall("get", request.GetTarget().GetScope())
	if request.GetKey() == "wait-for-cancel" {
		_ = os.WriteFile("entered", []byte("entered"), 0o600)
		<-ctx.Done()
		_ = os.WriteFile("cancelled", []byte("cancelled"), 0o600)
		return nil, ctx.Err()
	}
	switch request.GetKey() {
	case "plain-error":
		return nil, errors.New("plain-provider-secret")
	case "structured-error":
		return &pb.SecretStoreGetResponse{
			Result: &pb.SecretStoreGetResult{Value: []byte("fixture-sensitive-value")},
			Error: &pb.SecretStoreError{Code: "api_denied", Message: "structured-provider-secret", Retryable: true},
		}, nil
	}
	value := append([]byte("value:"+request.GetKey()+":"), request.GetTarget().GetConfigJson()...)
	mutateSecretStoreTarget(request.GetTarget())
	request.Key = "provider-mutated"
	return &pb.SecretStoreGetResponse{Result: &pb.SecretStoreGetResult{Value: value}}, nil
}

func (*provider) List(_ context.Context, request *pb.SecretStoreListRequest) (*pb.SecretStoreListResponse, error) {
	markSecretStoreCall("list", request.GetTarget().GetScope())
	var result *pb.SecretStoreListResult
	if string(request.GetPageToken()) == "list-page-2" {
		result = &pb.SecretStoreListResult{Names: []string{"gamma"}}
	} else {
		result = &pb.SecretStoreListResult{Names: []string{"alpha", "beta"}, NextPageToken: []byte("list-page-2")}
	}
	mutateSecretStoreTarget(request.GetTarget())
	if len(request.PageToken) > 0 { request.PageToken[0] = 'X' }
	return &pb.SecretStoreListResponse{Result: result}, nil
}

func (*provider) StatAll(_ context.Context, request *pb.SecretStoreStatAllRequest) (*pb.SecretStoreStatAllResponse, error) {
	markSecretStoreCall("stat_all", request.GetTarget().GetScope())
	var result *pb.SecretStoreStatAllResult
	if string(request.GetPageToken()) == "stat-page-2" {
		result = &pb.SecretStoreStatAllResult{Items: []*pb.SecretStoreMetadata{{Name: "gamma", Exists: true}}}
	} else {
		result = &pb.SecretStoreStatAllResult{
			Items: []*pb.SecretStoreMetadata{
				{Name: "alpha", Exists: true, UpdatedAt: timestamppb.New(time.Unix(1_700_000_000, 123))},
				{Name: "beta", Exists: false},
			},
			NextPageToken: []byte("stat-page-2"),
		}
	}
	mutateSecretStoreTarget(request.GetTarget())
	if len(request.PageToken) > 0 { request.PageToken[0] = 'X' }
	return &pb.SecretStoreStatAllResponse{Result: result}, nil
}

func (*provider) CheckAccess(_ context.Context, request *pb.SecretStoreCheckAccessRequest) (*pb.SecretStoreCheckAccessResponse, error) {
	markSecretStoreCall("check_access", request.GetTarget().GetScope())
	mutateSecretStoreTarget(request.GetTarget())
	return &pb.SecretStoreCheckAccessResponse{}, nil
}

func mutateSecretStoreTarget(target *pb.SecretStoreTarget) {
	if target == nil { return }
	target.Type = "provider-mutated"
	target.Scope = "provider-mutated"
	if len(target.ConfigJson) > 0 { target.ConfigJson[0] = 'X' }
}

func markSecretStoreCall(operation, scope string) {
	_ = os.WriteFile("called-"+operation+"-"+scope, []byte("called"), 0o600)
}
`
