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

func TestContainerRegistryExternalLoaderLifecycle(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "container-registry-fixture"
	pluginDir := prepareContainerRegistryFixture(t, pluginsDir, pluginName, true, false)

	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if !registryAdvertisesService(adapter.ContractRegistry(), pb.ContainerRegistry_ServiceDesc.ServiceName) {
		t.Fatalf("container registry contract not advertised: %v", adapter.ContractRegistry())
	}
	if !registryContainsContractType(adapter.ContractRegistry(), "fixture.service") {
		t.Fatalf("container registry advertisement replaced provider contracts: %v", adapter.ContractRegistry())
	}
	containerRegistryContract := findContainerRegistryServiceContract(adapter.ContractRegistry())
	if containerRegistryContract.GetKind() != pb.ContractKind_CONTRACT_KIND_SERVICE ||
		containerRegistryContract.GetMode() != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO ||
		containerRegistryContract.GetContractType() != "workflow.provider.container-registry" ||
		containerRegistryContract.GetProtoPackage() != "workflow.plugin.external.registry" ||
		containerRegistryContract.GetGoImportPath() != "github.com/GoCodeAlone/workflow/plugin/external/proto" ||
		containerRegistryContract.GetProtocolVersion() != "1" {
		t.Fatalf("container registry contract metadata = %v", containerRegistryContract)
	}
	client := adapter.client.ContainerRegistryClient()
	if client == nil {
		t.Fatal("loaded plugin has no typed container registry client")
	}

	described, err := client.DescribeRegistries(context.Background(), &pb.ContainerRegistryDeclarationsRequest{})
	if err != nil || described.GetError() != nil {
		t.Fatalf("DescribeRegistries = %v, %v", described, err)
	}
	if len(described.GetRegistries()) != 2 {
		t.Fatalf("runtime registry declarations = %v", described.GetRegistries())
	}
	if got := described.GetRegistries()[0]; got.GetType() != "fixture.login-only" || !sameStrings(got.GetOperations(), []string{"login"}) {
		t.Fatalf("first declaration = %v", got)
	}
	if got := described.GetRegistries()[1]; got.GetType() != "fixture.registry" || !sameStrings(got.GetOperations(), []string{"login", "logout", "push", "prune"}) {
		t.Fatalf("second declaration = %v", got)
	}

	registry := completeContainerRegistryConfig()
	for _, test := range []struct {
		name string
		call func(context.Context) (*pb.ContainerRegistryOperationResponse, error)
	}{
		{name: "login", call: func(ctx context.Context) (*pb.ContainerRegistryOperationResponse, error) {
			return client.Login(ctx, &pb.ContainerRegistryLoginRequest{Registry: registry, DryRun: true})
		}},
		{name: "logout", call: func(ctx context.Context) (*pb.ContainerRegistryOperationResponse, error) {
			return client.Logout(ctx, &pb.ContainerRegistryLogoutRequest{Registry: registry, DryRun: true})
		}},
		{name: "push", call: func(ctx context.Context) (*pb.ContainerRegistryOperationResponse, error) {
			return client.Push(ctx, &pb.ContainerRegistryPushRequest{Registry: registry, DryRun: true, ImageReference: "registry.example/team/image:v1"})
		}},
		{name: "prune", call: func(ctx context.Context) (*pb.ContainerRegistryOperationResponse, error) {
			return client.Prune(ctx, &pb.ContainerRegistryPruneRequest{Registry: registry, DryRun: true})
		}},
	} {
		t.Run(test.name+" preserves config and dry-run", func(t *testing.T) {
			before := proto.Clone(registry).(*pb.ContainerRegistryConfig)
			response, err := test.call(context.Background())
			if err != nil || response.GetError() != nil {
				t.Fatalf("%s = %v, %v", test.name, response, err)
			}
			if !proto.Equal(registry, before) {
				t.Fatalf("%s mutated caller registry:\n got %v\nwant %v", test.name, registry, before)
			}
			output := string(response.GetResult().GetOutput())
			for _, required := range []string{
				test.name + ":", `"dryRun":true`, `"name":"primary"`, `"type":"fixture.registry"`,
				`"path":"team/project"`, `"env":"REGISTRY_TOKEN"`, `"file":"/tmp/registry-auth.json"`,
				`"awsProfile":"fixture-profile"`, `"address":"https://vault.example"`, `"path":"secret/registry"`,
				`"keepLatest":"1099511627776"`, `"untaggedTtl":"48h"`, `"schedule":"0 2 * * *"`,
				`"apiBaseUrl":"https://registry-api.example"`,
			} {
				if !strings.Contains(output, required) {
					t.Fatalf("%s output missing %q: %s", test.name, required, output)
				}
			}
			if test.name == "push" && !strings.Contains(output, `"imageReference":"registry.example/team/image:v1"`) {
				t.Fatalf("push output lost image reference: %s", output)
			}
		})
	}

	t.Run("selection and operation mismatch fail before provider", func(t *testing.T) {
		for _, test := range []struct {
			name       string
			registry   *pb.ContainerRegistryConfig
			wantCode   string
			markerPath string
		}{
			{
				name:       "exact type",
				registry:   &pb.ContainerRegistryConfig{Name: "must-not-run-exact", Type: "fixture.registry "},
				wantCode:   "unsupported_registry_type",
				markerPath: filepath.Join(pluginDir, "called-login-must-not-run-exact"),
			},
			{
				name:       "declared operation",
				registry:   &pb.ContainerRegistryConfig{Name: "must-not-run-operation", Type: "fixture.login-only"},
				wantCode:   "unsupported_operation",
				markerPath: filepath.Join(pluginDir, "called-push-must-not-run-operation"),
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				var response *pb.ContainerRegistryOperationResponse
				var err error
				if test.name == "exact type" {
					response, err = client.Login(context.Background(), &pb.ContainerRegistryLoginRequest{Registry: test.registry})
				} else {
					response, err = client.Push(context.Background(), &pb.ContainerRegistryPushRequest{Registry: test.registry, ImageReference: "image:v1"})
				}
				if err != nil || response.GetError().GetCode() != test.wantCode || response.GetResult() != nil {
					t.Fatalf("mismatch response = %v, %v", response, err)
				}
				if _, statErr := os.Stat(test.markerPath); !os.IsNotExist(statErr) {
					t.Fatalf("provider executed before mismatch rejection: %v", statErr)
				}
			})
		}
	})

	t.Run("provider errors are structured and redacted", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			wantCode  string
			retryable bool
		}{
			{name: "plain-error", wantCode: "provider_error"},
			{name: "structured-error", wantCode: "api_denied", retryable: true},
		} {
			response, err := client.Prune(context.Background(), &pb.ContainerRegistryPruneRequest{
				Registry: &pb.ContainerRegistryConfig{Name: test.name, Type: "fixture.registry", Auth: &pb.ContainerRegistryAuth{Env: "request-secret"}},
			})
			if err != nil || response.GetError().GetCode() != test.wantCode || response.GetError().GetRetryable() != test.retryable || response.GetResult() != nil {
				t.Fatalf("Prune(%s) = %v, %v", test.name, response, err)
			}
			serialized := response.String()
			for _, forbidden := range []string{"plain-provider-secret", "structured-provider-secret", "request-secret"} {
				if strings.Contains(serialized, forbidden) {
					t.Fatalf("Prune(%s) leaked %q: %s", test.name, forbidden, serialized)
				}
			}
		}
	})

	t.Run("cancellation propagates to provider", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, callErr := client.Login(ctx, &pb.ContainerRegistryLoginRequest{
				Registry: &pb.ContainerRegistryConfig{Name: "wait-for-cancel", Type: "fixture.registry"},
			})
			result <- callErr
		}()
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "entered"), "provider did not enter Login")
		cancel()
		select {
		case err := <-result:
			if status.Code(err) != codes.Canceled {
				t.Fatalf("Login cancellation status = %s, want Canceled (error %v)", status.Code(err), err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Login did not return")
		}
		waitForCredentialFixtureMarker(t, filepath.Join(pluginDir, "cancelled"), "provider did not observe canceled gRPC context")
	})
}

func TestContainerRegistryOptionalServiceBackwardCompatibility(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "legacy-registry-fixture"
	prepareContainerRegistryFixture(t, pluginsDir, pluginName, false, false)
	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin legacy: %v", err)
	}
	if registryAdvertisesService(adapter.ContractRegistry(), pb.ContainerRegistry_ServiceDesc.ServiceName) {
		t.Fatalf("legacy plugin unexpectedly advertised %s", pb.ContainerRegistry_ServiceDesc.ServiceName)
	}
	client := adapter.client.ContainerRegistryClient()
	if client == nil {
		t.Fatal("legacy plugin has no shared gRPC connection")
	}
	_, err = client.DescribeRegistries(context.Background(), &pb.ContainerRegistryDeclarationsRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("legacy DescribeRegistries status = %s, want Unimplemented (error %v)", status.Code(err), err)
	}
}

func TestContainerRegistryDuplicateRuntimeTypeFailsBeforeLoad(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "duplicate-registry-fixture"
	prepareContainerRegistryFixture(t, pluginsDir, pluginName, true, true)
	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	if _, err := manager.LoadPlugin(pluginName); err == nil {
		t.Fatal("LoadPlugin accepted duplicate runtime declarations")
	}
}

func completeContainerRegistryConfig() *pb.ContainerRegistryConfig {
	return &pb.ContainerRegistryConfig{
		Name: "primary", Type: "fixture.registry", Path: "team/project",
		Auth: &pb.ContainerRegistryAuth{
			Env: "REGISTRY_TOKEN", File: "/tmp/registry-auth.json", AwsProfile: "fixture-profile",
			Vault: &pb.ContainerRegistryVaultAuth{Address: "https://vault.example", Path: "secret/registry"},
		},
		Retention:  &pb.ContainerRegistryRetention{KeepLatest: 1 << 40, UntaggedTtl: "48h", Schedule: "0 2 * * *"},
		ApiBaseUrl: "https://registry-api.example",
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func findContainerRegistryServiceContract(registry *pb.ContractRegistry) *pb.ContractDescriptor {
	if registry == nil {
		return nil
	}
	for _, descriptor := range registry.GetContracts() {
		if descriptor.GetServiceName() == pb.ContainerRegistry_ServiceDesc.ServiceName {
			return descriptor
		}
	}
	return nil
}

func prepareContainerRegistryFixture(t *testing.T, pluginsDir, pluginName string, register, duplicate bool) string {
	t.Helper()
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}
	buildContainerRegistryFixture(t, filepath.Join(pluginDir, pluginName), pluginName, register, duplicate)
	manifest := `{
		"name":"` + pluginName + `",
		"version":"1.0.0",
		"author":"Workflow tests",
		"description":"container registry transport fixture",
		"containerRegistries":[
			{"type":"fixture.registry","operations":["login","logout","push","prune"]},
			{"type":"fixture.login-only","operations":["login"]}
		]
	}`
	if !register {
		manifest = `{"name":"` + pluginName + `","version":"1.0.0","author":"Workflow tests","description":"legacy registry fixture"}`
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	return pluginDir
}

func buildContainerRegistryFixture(t *testing.T, output, providerName string, register, duplicate bool) {
	t.Helper()
	sourceDir := t.TempDir()
	serve := "sdk.Serve(p)"
	implementation := ""
	if register {
		serve = "sdk.Serve(p, sdk.WithContainerRegistryProvider(p))"
		implementation = containerRegistryFixtureImplementation
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

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type provider struct{}

var (
	_ = context.Background
	_ = errors.New
	_ = os.WriteFile
	_ = protojson.Marshal
	_ = proto.Clone
	_ = (*pb.ContainerRegistryLoginRequest)(nil)
	duplicateDeclarations = ` + duplicateLiteral + `
)

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{Name: "` + providerName + `", Version: "1.0.0", Author: "Workflow tests", Description: "container registry transport fixture"}
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
		t.Fatalf("write container registry fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, sourcePath)
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build container registry fixture for %s/%s: %v\n%s", runtime.GOOS, runtime.GOARCH, err, strings.TrimSpace(string(combined)))
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat fixture executable %s: %v", output, err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("fixture is not executable: %s", output)
	}
}

const containerRegistryFixtureImplementation = `
func (*provider) ContainerRegistries() []*pb.ContainerRegistryDeclaration {
	declarations := []*pb.ContainerRegistryDeclaration{
		{Type: " fixture.registry ", Operations: []string{" prune ", "login", "push", "logout"}},
		{Type: "fixture.login-only", Operations: []string{"login"}},
	}
	if duplicateDeclarations {
		declarations = append(declarations, &pb.ContainerRegistryDeclaration{Type: "fixture.registry", Operations: []string{"login"}})
	}
	return declarations
}

func (*provider) Login(ctx context.Context, request *pb.ContainerRegistryLoginRequest) (*pb.ContainerRegistryOperationResponse, error) {
	if request.GetRegistry().GetName() == "wait-for-cancel" {
		_ = os.WriteFile("entered", []byte("entered"), 0o600)
		<-ctx.Done()
		_ = os.WriteFile("cancelled", []byte("cancelled"), 0o600)
		return nil, ctx.Err()
	}
	return registryResult("login", request, request.GetRegistry())
}

func (*provider) Logout(_ context.Context, request *pb.ContainerRegistryLogoutRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult("logout", request, request.GetRegistry())
}

func (*provider) Push(_ context.Context, request *pb.ContainerRegistryPushRequest) (*pb.ContainerRegistryOperationResponse, error) {
	return registryResult("push", request, request.GetRegistry())
}

func (*provider) Prune(_ context.Context, request *pb.ContainerRegistryPruneRequest) (*pb.ContainerRegistryOperationResponse, error) {
	switch request.GetRegistry().GetName() {
	case "plain-error":
		return nil, errors.New("plain-provider-secret")
	case "structured-error":
		return &pb.ContainerRegistryOperationResponse{
			Result: &pb.ContainerRegistryResult{Output: []byte("structured-provider-secret")},
			Error: &pb.ContainerRegistryError{Code: "api_denied", Message: "structured-provider-secret", Retryable: true},
		}, nil
	default:
		return registryResult("prune", request, request.GetRegistry())
	}
}

func registryResult(operation string, request any, registry *pb.ContainerRegistryConfig) (*pb.ContainerRegistryOperationResponse, error) {
	message, ok := request.(proto.Message)
	if !ok {
		return nil, errors.New("unexpected request type")
	}
	payload, err := protojson.Marshal(message)
	if err != nil {
		return nil, err
	}
	_ = os.WriteFile("called-"+operation+"-"+registry.GetName(), []byte("called"), 0o600)
	if registry.Auth != nil && registry.Auth.Vault != nil {
		registry.Auth.Vault.Address = "provider-mutated"
	}
	return &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{Output: append([]byte(operation+":"), payload...)}}, nil
}
`
