package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	registrypkg "github.com/GoCodeAlone/workflow/plugin/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestProviderCapabilityIndexSelectsAllFamiliesExactly(t *testing.T) {
	index, err := newProviderCapabilityIndex([]providerCapabilityOwner{
		{
			Name:    "workflow-plugin-example",
			Version: "1.2.3",
			Declarations: config.ProviderDeclarations{
				CredentialSources: []config.CredentialSourceDecl{{
					Source:          "example.object-store",
					ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
					Outputs: []config.CredentialOutputDecl{
						{Key: "access_key"},
						{Key: "secret_key"},
					},
					IdentifierKey: "access_key",
				}},
				CredentialResolvers: []config.CredentialResolverDecl{{Provider: "aws", CredentialTypes: []string{"static", "env"}}},
				KubernetesBackends:  []config.KubernetesBackendDecl{{Name: "example-k8s", ResourceType: "infra.example_cluster"}},
				ContainerRegistries: []config.ContainerRegistryDecl{{Type: "example-registry", Operations: []string{"login", "logout", "push", "prune"}}},
				SecretStores:        []config.SecretStoreDecl{{Type: "example-secrets", Operations: []string{"get", "list"}, Scopes: []string{"account", "region"}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		selectRoute func() (providerCapabilityRoute, error)
	}{
		{"credential source", func() (providerCapabilityRoute, error) { return index.selectCredentialSource("example.object-store") }},
		{"credential resolver", func() (providerCapabilityRoute, error) { return index.selectCredentialResolver("aws", "env") }},
		{"kubernetes backend", func() (providerCapabilityRoute, error) { return index.selectKubernetesBackend("example-k8s") }},
		{"container registry", func() (providerCapabilityRoute, error) {
			return index.selectContainerRegistry("example-registry", "push")
		}},
		{"secret store", func() (providerCapabilityRoute, error) {
			return index.selectSecretStore("example-secrets", "list", "region")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route, selectErr := test.selectRoute()
			if selectErr != nil {
				t.Fatal(selectErr)
			}
			if route.PluginName != "workflow-plugin-example" || route.PluginVersion != "1.2.3" {
				t.Fatalf("route = %+v", route)
			}
		})
	}

	for _, selectRoute := range []func() (providerCapabilityRoute, error){
		func() (providerCapabilityRoute, error) { return index.selectCredentialSource("Example.Object-Store") },
		func() (providerCapabilityRoute, error) { return index.selectCredentialResolver("aws", "profile") },
		func() (providerCapabilityRoute, error) { return index.selectKubernetesBackend("example") },
		func() (providerCapabilityRoute, error) {
			return index.selectContainerRegistry("example-registry", "pull")
		},
		func() (providerCapabilityRoute, error) {
			return index.selectSecretStore("example-secrets", "list", "project")
		},
	} {
		_, selectErr := selectRoute()
		if selectErr == nil || !strings.Contains(selectErr.Error(), "wfctl plugin install") || !strings.Contains(selectErr.Error(), "upgrade") {
			t.Fatalf("zero-match error = %v, want install/version guidance", selectErr)
		}
	}
	_, unsupportedRegistryOperation := index.selectContainerRegistry("example-registry", "pull")
	var unsupported providerCapabilityNotFoundError
	if !errors.As(unsupportedRegistryOperation, &unsupported) || unsupported.family != "container registry" {
		t.Fatalf("installed owner with unsupported operation must not look like a zero-owner fallback: %v", unsupportedRegistryOperation)
	}
}

func TestRunContainerRegistryCapabilityRoutesEveryOperation(t *testing.T) {
	client := &recordingContainerRegistryClient{}
	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
		return client, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

	registry := config.CIRegistry{
		Name:       "example",
		Type:       "example-registry",
		Path:       "registry.example.test/team",
		APIBaseURL: "https://api.example.test",
		Auth:       &config.CIRegistryAuth{Env: "EXAMPLE_TOKEN", AWSProfile: "example-profile", Vault: &config.CIRegistryVaultAuth{Address: "https://vault.example.test", Path: "secret/registry"}},
		Retention:  &config.CIRegistryRetention{KeepLatest: 7, UntaggedTTL: "24h", Schedule: "daily"},
	}
	for _, operation := range []string{"login", "logout", "push", "prune"} {
		handled, err := runContainerRegistryCapability(context.Background(), t.TempDir(), operation, registry, "registry.example.test/team/app:v1", true, io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		if !handled {
			t.Fatalf("%s was not handled", operation)
		}
	}
	if got, want := strings.Join(client.operations, ","), "login,logout,push,prune"; got != want {
		t.Fatalf("operations = %q, want %q", got, want)
	}
	if client.registry.GetName() != registry.Name || client.registry.GetAuth().GetVault().GetPath() != registry.Auth.Vault.Path || client.registry.GetRetention().GetKeepLatest() != int64(registry.Retention.KeepLatest) {
		t.Fatalf("typed registry config = %+v", client.registry)
	}
	if client.imageReference != "registry.example.test/team/app:v1" || !client.dryRun {
		t.Fatalf("push image=%q dryRun=%v", client.imageReference, client.dryRun)
	}
}

func TestRunContainerRegistryCapabilityRejectsEmptyResponseForEveryOperation(t *testing.T) {
	client := &recordingContainerRegistryClient{emptyResponse: true}
	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
		return client, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

	for _, operation := range []string{"login", "logout", "push", "prune"} {
		t.Run(operation, func(t *testing.T) {
			registry := config.CIRegistry{Name: "example", Type: "example-registry"}
			handled, err := runContainerRegistryCapability(context.Background(), t.TempDir(), operation, registry, "registry.example.test/team/app:v1", true, io.Discard)
			if !handled || err == nil || !strings.Contains(err.Error(), "empty_response") {
				t.Fatalf("handled=%v error=%v, want sanitized empty_response", handled, err)
			}
		})
	}
}

func TestRunContainerRegistryCapabilityPropagatesOutputWriteFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		writer io.Writer
	}{
		{name: "error", writer: registryErrorWriter{}},
		{name: "short", writer: registryShortWriter{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingContainerRegistryClient{output: []byte("provider output")}
			oldResolve := resolveContainerRegistryCapability
			resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
				return client, func() {}, true, nil
			}
			t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

			handled, err := runContainerRegistryCapability(
				context.Background(), t.TempDir(), "login", config.CIRegistry{Name: "example", Type: "example-registry"}, "", false,
				test.writer,
			)
			if !handled || err == nil || !strings.Contains(err.Error(), "write output") {
				t.Fatalf("handled=%v error=%v", handled, err)
			}
		})
	}
}

type registryErrorWriter struct{}

func (registryErrorWriter) Write([]byte) (int, error) {
	return 0, errors.New("simulated output failure")
}

type registryShortWriter struct{}

func (registryShortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestContainerRegistryCapabilityStopsBeforeCanceledMutation(t *testing.T) {
	for _, operation := range []string{"login", "logout", "push", "prune"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			client := &recordingContainerRegistryClient{}
			handled, err := executeContainerRegistryCapability(ctx, &preparedContainerRegistryCapability{
				client: client, handled: true, operation: operation,
				registry: config.CIRegistry{Name: "example", Type: "example-registry"}, imageReference: "registry.example/app:tag",
			}, false, io.Discard)
			if !handled || !errors.Is(err, context.Canceled) || len(client.operations) != 0 {
				t.Fatalf("handled=%t error=%v operations=%v", handled, err, client.operations)
			}
		})
	}
}

func TestProviderCapabilityErrorsSuppressProviderText(t *testing.T) {
	registryClient := &recordingContainerRegistryClient{operationError: &pb.ContainerRegistryError{
		Code: "provider_failed", Message: "sensitive-value",
	}}
	oldRegistryResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
		return registryClient, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldRegistryResolve })
	_, err := runContainerRegistryCapability(context.Background(), t.TempDir(), "login", config.CIRegistry{Name: "example", Type: "example-registry"}, "", false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "provider_failed") || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("registry error=%v", err)
	}

	issuerClient := &recordingCredentialIssuerClient{issueResponse: &pb.CredentialIssueResponse{Error: &pb.CredentialOperationError{
		Code: "issue_failed", Message: "sensitive-value",
	}}}
	withCredentialIssuerResolver(t, issuerClient, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	_, _, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !strings.Contains(err.Error(), "issue_failed") || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("issuer error=%v", err)
	}
}

func TestProviderCapabilityTransportErrorsExposeOnlyCanonicalCodes(t *testing.T) {
	const providerText = "provider-sensitive-transport-detail"
	registryClient := &recordingContainerRegistryClient{
		operationTransportErr: status.Error(codes.Internal, providerText),
	}
	oldRegistryResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
		return registryClient, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldRegistryResolve })
	_, err := runContainerRegistryCapability(context.Background(), t.TempDir(), "login", config.CIRegistry{Name: "example", Type: "example-registry"}, "", false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), codes.Internal.String()) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("registry transport error=%v", err)
	}

	issuerClient := &recordingCredentialIssuerClient{
		issueErr: status.Error(codes.ResourceExhausted, providerText),
	}
	withCredentialIssuerResolver(t, issuerClient, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	_, _, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !strings.Contains(err.Error(), codes.ResourceExhausted.String()) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("issuer transport error=%v", err)
	}
}

func TestRegistryCommandsPreferTypedPluginCapability(t *testing.T) {
	client := &recordingContainerRegistryClient{}
	var selections []string
	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(_ context.Context, pluginDir, registryType, operation string) (pb.ContainerRegistryClient, func(), bool, error) {
		selections = append(selections, pluginDir+":"+registryType+":"+operation)
		return client, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	configPath := filepath.Join(dir, "workflow.yaml")
	configBody := `ci:
  registries:
    - name: example
      type: example-registry
      path: registry.example.test/team
  build:
    containers:
      - name: app
        push_to: [example]
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := []struct {
		name string
		run  func([]string) error
	}{
		{"login", runRegistryLogin},
		{"logout", runRegistryLogout},
		{"prune", runRegistryPrune},
		{"push", runRegistryPush},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			if err := command.run([]string{"--config", configPath, "--plugin-dir", pluginDir, "--dry-run"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	wantSelections := []string{
		pluginDir + ":example-registry:login",
		pluginDir + ":example-registry:logout",
		pluginDir + ":example-registry:prune",
		pluginDir + ":example-registry:push",
	}
	if got, want := strings.Join(selections, ","), strings.Join(wantSelections, ","); got != want {
		t.Fatalf("selections = %q, want %q", got, want)
	}
	if got, want := strings.Join(client.operations, ","), "login,logout,prune,push"; got != want {
		t.Fatalf("typed operations = %q, want %q", got, want)
	}
}

func TestRegistryCommandsPreflightEveryRouteBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	configBody := `ci:
  registries:
    - name: first
      type: first-registry
      path: registry.example.test/first
    - name: second
      type: second-registry
      path: registry.example.test/second
  build:
    containers:
      - name: app
        push_to: [first, second]
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := []struct {
		name string
		run  func([]string) error
	}{
		{name: "login", run: runRegistryLogin},
		{name: "logout", run: runRegistryLogout},
		{name: "prune", run: runRegistryPrune},
		{name: "push", run: runRegistryPush},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			client := &recordingContainerRegistryClient{}
			oldResolve := resolveContainerRegistryCapability
			resolveContainerRegistryCapability = func(_ context.Context, _, registryType, _ string) (pb.ContainerRegistryClient, func(), bool, error) {
				if registryType == "second-registry" {
					return nil, nil, false, errors.New("later route collision")
				}
				return client, func() {}, true, nil
			}
			t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

			err := command.run([]string{"--config", configPath, "--plugin-dir", filepath.Join(dir, "plugins"), "--dry-run"})
			if err == nil || !strings.Contains(err.Error(), "later route collision") {
				t.Fatalf("error=%v", err)
			}
			if len(client.operations) != 0 {
				t.Fatalf("first registry mutated before later route passed preflight: %v", client.operations)
			}
		})
	}
}

func TestRegistryCommandsStopBeforeOperationWhenCommandCanceled(t *testing.T) {
	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	client := &recordingContainerRegistryClient{}
	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(ctx context.Context, _, _, _ string) (pb.ContainerRegistryClient, func(), bool, error) {
		client.discoveryContextCanceled = errors.Is(ctx.Err(), context.Canceled)
		return client, func() {}, true, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(`ci:
  registries:
    - name: example
      type: example-registry
      path: registry.example.test/team
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runRegistryLogin([]string{"--config", configPath, "--dry-run"})
	if !errors.Is(err, context.Canceled) || !client.discoveryContextCanceled || len(client.operations) != 0 {
		t.Fatalf("error=%v discovery canceled=%v operations=%v", err, client.discoveryContextCanceled, client.operations)
	}
}

func TestRegistryCommandsBoundDiscoveryAndLegacyOperations(t *testing.T) {
	const registryType = "bounded-context-registry-test"
	legacy := &deadlineRecordingRegistryProvider{name: registryType}
	registrypkg.Register(legacy)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(`ci:
  registries:
    - name: example
      type: bounded-context-registry-test
      path: registry.example.test/team
  build:
    containers:
      - name: app
        push_to: [example]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	var discoveryDeadlines int
	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(ctx context.Context, _, _, _ string) (pb.ContainerRegistryClient, func(), bool, error) {
		if _, ok := ctx.Deadline(); ok {
			discoveryDeadlines++
		}
		return nil, nil, false, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })

	commands := []func([]string) error{runRegistryLogin, runRegistryLogout, runRegistryPrune, runRegistryPush}
	for _, command := range commands {
		if err := command([]string{"--config", configPath, "--plugin-dir", filepath.Join(dir, "plugins")}); err != nil {
			t.Fatal(err)
		}
	}
	if discoveryDeadlines != len(commands) {
		t.Fatalf("bounded discovery calls=%d, want %d", discoveryDeadlines, len(commands))
	}
	if got := strings.Join(legacy.operations, ","); got != "login,logout,prune,push" {
		t.Fatalf("legacy operations=%q", got)
	}
	if legacy.deadlineCalls != len(commands) {
		t.Fatalf("bounded legacy calls=%d, want %d", legacy.deadlineCalls, len(commands))
	}
}

func TestRegistryPushBoundsDirectDockerFallback(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(`ci:
  registries:
    - name: example
      type: direct-docker-context-test
      path: registry.example.test/team
  build:
    containers:
      - name: app
        push_to: [example]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	oldResolve := resolveContainerRegistryCapability
	resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
		return nil, nil, false, nil
	}
	t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })
	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	sawDeadline := false
	var pushedReference string
	originalDockerPush := dockerPushToRegistry
	dockerPushToRegistry = func(ctx context.Context, ref string) error {
		_, sawDeadline = ctx.Deadline()
		pushedReference = ref
		return nil
	}
	t.Cleanup(func() { dockerPushToRegistry = originalDockerPush })
	if err := runRegistryPush([]string{"--config", configPath, "--plugin-dir", filepath.Join(dir, "plugins")}); err != nil {
		t.Fatal(err)
	}
	if !sawDeadline {
		t.Fatal("direct Docker fallback did not receive the bounded command context")
	}
	if pushedReference != "registry.example.test/team/app:latest" {
		t.Fatalf("auto-detected Docker reference=%q", pushedReference)
	}

	explicitReference := "other.example.test/explicit/app:v2"
	if err := runRegistryPush([]string{"--config", configPath, "--plugin-dir", filepath.Join(dir, "plugins"), "--image", explicitReference}); err != nil {
		t.Fatal(err)
	}
	if pushedReference != explicitReference {
		t.Fatalf("explicit Docker reference=%q, want %q", pushedReference, explicitReference)
	}
}

func TestRegistryCommandsStopAfterCancellationBetweenOperations(t *testing.T) {
	commands := []struct {
		name string
		run  func([]string) error
	}{
		{name: "login", run: runRegistryLogin},
		{name: "logout", run: runRegistryLogout},
		{name: "prune", run: runRegistryPrune},
		{name: "push", run: runRegistryPush},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			registryType := "cancel-between-" + command.name + "-registry-test"
			legacy := &deadlineRecordingRegistryProvider{name: registryType}
			registrypkg.Register(legacy)
			dir := t.TempDir()
			configPath := filepath.Join(dir, "workflow.yaml")
			configBody := fmt.Sprintf(`ci:
  registries:
    - name: first
      type: %s
      path: registry.example.test/first
    - name: second
      type: %s
      path: registry.example.test/second
  build:
    containers:
      - name: app
        push_to: [first, second]
`, registryType, registryType)
			if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
				t.Fatal(err)
			}

			oldResolve := resolveContainerRegistryCapability
			resolveContainerRegistryCapability = func(context.Context, string, string, string) (pb.ContainerRegistryClient, func(), bool, error) {
				return nil, nil, false, nil
			}
			t.Cleanup(func() { resolveContainerRegistryCapability = oldResolve })
			originalCommandContext := providerCommandContext
			providerCommandContext = func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				legacy.cancelAfterFirst = cancel
				return ctx, cancel
			}
			t.Cleanup(func() { providerCommandContext = originalCommandContext })

			err := command.run([]string{"--config", configPath, "--plugin-dir", filepath.Join(dir, "plugins")})
			if !errors.Is(err, context.Canceled) || len(legacy.operations) != 1 {
				t.Fatalf("error=%v operations=%v, want cancellation after first", err, legacy.operations)
			}
		})
	}
}

func TestCredentialIssuerSingleWriterRequiresNonInteractiveAcknowledgement(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source:          "example.source",
		ConcurrencyMode: config.CredentialConcurrencySingleWriter,
		Outputs:         []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}},
		IdentifierKey:   "id",
	})
	t.Setenv("WFCTL_ACK_SINGLE_WRITER", "true")
	_, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir:       t.TempDir(),
		Source:         "example.source",
		LogicalName:    "deploy-key",
		NonInteractive: true,
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "--ack-single-writer") {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	if client.issueCalls != 0 {
		t.Fatalf("Issue calls = %d, want zero before acknowledgement", client.issueCalls)
	}
	ackStateDir := t.TempDir()
	result, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir:        ackStateDir,
		Source:          "example.source",
		LogicalName:     "deploy-key",
		NonInteractive:  true,
		AckSingleWriter: true,
	})
	if err != nil || !handled || result == nil || client.issueCalls != 1 {
		t.Fatalf("acknowledged issue: result=%v handled=%v error=%v calls=%d", result, handled, err, client.issueCalls)
	}
	state, err := loadCredentialOperationState(ackStateDir, "example.source", "deploy-key")
	if err != nil || state.Acknowledgement != "flag_acknowledged" {
		t.Fatalf("acknowledgement state=%+v error=%v", state, err)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatalf("finalize acknowledged issue: %v", err)
	}
}

func TestCredentialIssuerSingleWriterInteractiveConfirmation(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencySingleWriter,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	oldConfirm := credentialSingleWriterConfirm
	confirmed := false
	credentialSingleWriterConfirm = func(string) (bool, error) { return confirmed, nil }
	t.Cleanup(func() { credentialSingleWriterConfirm = oldConfirm })
	request := credentialIssuerRequest{StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key"}
	if _, _, err := runCredentialIssuerCapability(context.Background(), request); err == nil || client.issueCalls != 0 {
		t.Fatalf("denied confirmation error=%v Issue calls=%d", err, client.issueCalls)
	}
	confirmed = true
	request.StateDir = t.TempDir()
	result, handled, err := runCredentialIssuerCapability(context.Background(), request)
	if err != nil || !handled || result == nil || client.issueCalls != 1 {
		t.Fatalf("confirmed issue result=%v handled=%v error=%v Issue calls=%d", result, handled, err, client.issueCalls)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatalf("finalize confirmed issue: %v", err)
	}
}

func TestCredentialIssuerPersistsOperationAndStoreFailureIdentifier(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: &pb.CredentialIssueResponse{
		Outputs:             []*pb.CredentialOutput{{Key: "id", Value: []byte("credential-123"), Sensitive: true}, {Key: "secret", Value: []byte("sensitive-value"), Sensitive: true}},
		Identifier:          "credential-123",
		IdentifierSensitive: true,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}, deleteResponse: &pb.CredentialDeleteResponse{
		Identifier: "credential-123", IdentifierSensitive: true,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source:          "example.source",
		ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs:         []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}},
		IdentifierKey:   "id",
	})
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	result, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir:    stateDir,
		Source:      "example.source",
		LogicalName: "deploy-key",
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	if client.operationID == "" || result.OperationID != client.operationID {
		t.Fatalf("operation IDs result=%q request=%q", result.OperationID, client.operationID)
	}
	if string(result.Outputs["secret"]) != "sensitive-value" {
		t.Fatal("issuer outputs not returned")
	}
	storeErr := result.Finalize(errors.New("store failed"))
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	state, err := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != credentialOperationRolledBack || state.Identifier != "" || !state.IdentifierSensitive || state.OperationID != client.operationID || state.RollbackOperationID != client.deleteOperationID {
		t.Fatalf("state = %+v", state)
	}
	if client.deleteCalls != 1 || client.deleteOperationID != client.operationID+"-rollback" || client.deleteIdentifier != "credential-123" {
		t.Fatalf("rollback Delete calls=%d operation=%q identifier=%q", client.deleteCalls, client.deleteOperationID, client.deleteIdentifier)
	}
	if state.PluginName != "workflow-plugin-example" || state.PluginVersion != "1.2.3" || state.ConcurrencyMode != config.CredentialConcurrencyProviderIdempotent || state.Acknowledgement != "not_required" {
		t.Fatalf("issuer audit context = %+v", state)
	}
	for _, event := range state.Audit {
		expectedOperationID := state.OperationID
		if event.Status == credentialOperationRollbackUnknown || event.Status == credentialOperationRolledBack {
			expectedOperationID = state.RollbackOperationID
		}
		if event.OperationID != expectedOperationID || event.PluginName != state.PluginName || event.PluginVersion != state.PluginVersion || event.ConcurrencyMode != state.ConcurrencyMode || event.Acknowledgement != state.Acknowledgement {
			t.Fatalf("audit event lacks operation attribution: %+v", event)
		}
	}
	stateBytes := string(mustReadCredentialState(t, stateDir))
	if strings.Contains(stateBytes, "sensitive-value") || strings.Contains(stateBytes, "credential-123") {
		t.Fatal("durable operation state leaked a credential value or sensitive identifier")
	}
	stateInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if stateInfo.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode=%v", stateInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			info, err := entry.Info()
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("state file mode=%v error=%v", info.Mode().Perm(), err)
			}
		}
	}
	result, handled, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "stored-key"})
	if err != nil || !handled {
		t.Fatalf("second issue handled=%v error=%v", handled, err)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatal(err)
	}
	stored, err := loadCredentialOperationState(stateDir, "example.source", "stored-key")
	if err != nil || stored.Status != credentialOperationStored {
		t.Fatalf("stored state=%+v error=%v", stored, err)
	}
	previousID := stored.OperationID
	rotated, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "stored-key"})
	if err != nil || !handled || rotated.OperationID == previousID {
		t.Fatalf("fresh operation after stored: result=%+v handled=%v error=%v", rotated, handled, err)
	}
	defer func() { _ = rotated.Finalize(nil) }()
	rotatedState, err := loadCredentialOperationState(stateDir, "example.source", "stored-key")
	if err != nil || len(rotatedState.Audit) < len(stored.Audit)+1 || rotatedState.Audit[0].OperationID != previousID || rotatedState.Audit[0].PluginName != "workflow-plugin-example" || rotatedState.Audit[0].Acknowledgement != "not_required" {
		t.Fatalf("preserved audit state=%+v error=%v", rotatedState, err)
	}
}

func TestCredentialIssuerValidationFailureUsesManifestIdentifierSensitivity(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: &pb.CredentialIssueResponse{
		Outputs: []*pb.CredentialOutput{
			{Key: "id", Value: []byte("credential-123"), Sensitive: false},
			{Key: "secret", Value: []byte("sensitive-value"), Sensitive: true},
		},
		Identifier:          "credential-123",
		IdentifierSensitive: false,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source:          "example.source",
		ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs:         []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}},
		IdentifierKey:   "id",
	})
	stateDir := t.TempDir()
	_, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !handled || !strings.Contains(err.Error(), "sensitivity") {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	state, loadErr := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.Identifier != "" || !state.IdentifierSensitive {
		t.Fatalf("state persisted runtime sensitivity downgrade: %+v", state)
	}
	if stateBytes := string(mustReadCredentialState(t, stateDir)); strings.Contains(stateBytes, "credential-123") {
		t.Fatal("durable state leaked a manifest-sensitive identifier after response validation failed")
	}
}

func TestCredentialIssuerStoreFailureBlocksAfterUncertainRollback(t *testing.T) {
	client := &recordingCredentialIssuerClient{
		issueResponse: confirmedCredentialIssueResponse(),
		deleteResponse: &pb.CredentialDeleteResponse{
			Identifier:          "credential-123",
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_UNKNOWN,
		},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	stateDir := t.TempDir()
	result, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err != nil || !handled {
		t.Fatalf("issue handled=%v error=%v", handled, err)
	}
	if err := result.Finalize(errors.New("store failed")); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("rollback error=%v", err)
	}
	state, loadErr := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if loadErr != nil || state.Status != credentialOperationRollbackUnknown {
		t.Fatalf("state=%+v error=%v", state, loadErr)
	}
	if _, _, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key",
	}); err == nil || client.issueCalls != 1 {
		t.Fatalf("uncertain rollback error=%v Issue calls=%d", err, client.issueCalls)
	}
}

func TestCredentialIssuerLostResponseReconcilesWithoutBlindRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		records    []*pb.CredentialRecord
		wantStatus credentialOperationStatus
	}{
		{"zero", nil, credentialOperationUnknown},
		{"one", []*pb.CredentialRecord{{Identifier: "credential-1"}}, credentialOperationUnknownCreated},
		{"multiple", []*pb.CredentialRecord{{Identifier: "credential-1"}, {Identifier: "credential-2"}}, credentialOperationAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingCredentialIssuerClient{issueErr: context.DeadlineExceeded, listRecords: test.records}
			withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
				Source:          "example.source",
				ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
				Outputs:         []config.CredentialOutputDecl{{Key: "id"}},
				IdentifierKey:   "id",
			})
			stateDir := t.TempDir()
			request := credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key"}
			if _, handled, err := runCredentialIssuerCapability(context.Background(), request); !handled || err == nil {
				t.Fatalf("handled=%v error=%v", handled, err)
			}
			state, err := loadCredentialOperationState(stateDir, request.Source, request.LogicalName)
			if err != nil {
				t.Fatal(err)
			}
			if state.Status != test.wantStatus {
				t.Fatalf("status=%q, want %q", state.Status, test.wantStatus)
			}
			if _, _, err := runCredentialIssuerCapability(context.Background(), request); err == nil {
				t.Fatal("uncertain operation must block a later automatic retry")
			}
			if client.issueCalls != 1 {
				t.Fatalf("Issue calls=%d, want one", client.issueCalls)
			}
		})
	}
}

func TestCredentialIssuerLockAndStateAccessFailBeforeIssue(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	stateDir := t.TempDir()
	release, err := acquireCredentialOperationLock(stateDir, "example.source", "../../deploy-key")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "../../deploy-key"})
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("lock error=%v", err)
	}
	release()
	if client.issueCalls != 0 {
		t.Fatalf("Issue calls=%d before lock acquisition", client.issueCalls)
	}

	badStateDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badStateDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: badStateDir, LockDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil {
		t.Fatal("want pre-Issue durable state access error")
	}
	if client.issueCalls != 0 {
		t.Fatalf("Issue calls=%d after state write failure", client.issueCalls)
	}
}

func TestCredentialIssuerPreparationWaitsForSingleWriterAcknowledgement(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencySingleWriter,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	preparationCalls := 0
	_, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key", NonInteractive: true,
		BeforeIssue: func(context.Context, bool) error {
			preparationCalls++
			return nil
		},
	})
	if err == nil || !handled || !strings.Contains(err.Error(), "--ack-single-writer") {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	if preparationCalls != 0 || client.issueCalls != 0 {
		t.Fatalf("preparation calls=%d Issue calls=%d before acknowledgement", preparationCalls, client.issueCalls)
	}
}

func TestCredentialIssuerPreparationRunsUnderLockAfterDurableState(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	declaration := config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	}
	withCredentialIssuerResolver(t, client, declaration)
	stateDir := t.TempDir()
	lockDir := t.TempDir()
	request := credentialIssuerRequest{
		StateDir: stateDir, LockDir: lockDir, Source: declaration.Source, LogicalName: "deploy-key",
	}
	request.BeforeIssue = func(context.Context, bool) error {
		state, err := loadCredentialOperationState(stateDir, request.Source, request.LogicalName)
		if err != nil {
			return err
		}
		if state.Status != credentialOperationPreparing || state.OperationID == "" {
			return fmt.Errorf("preparation state=%+v", state)
		}
		if _, err := acquireCredentialOperationLock(lockDir, request.Source, request.LogicalName); err == nil || !strings.Contains(err.Error(), "locked") {
			return fmt.Errorf("preparation did not retain host-global lock: %v", err)
		}
		return nil
	}
	result, handled, err := runCredentialIssuerCapability(context.Background(), request)
	if err != nil || !handled || result == nil || client.issueCalls != 1 {
		t.Fatalf("result=%v handled=%v error=%v Issue calls=%d", result, handled, err, client.issueCalls)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialIssuerPreparationRetryReusesOperationID(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	declaration := config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	}
	withCredentialIssuerResolver(t, client, declaration)
	stateDir := t.TempDir()
	request := credentialIssuerRequest{
		StateDir: stateDir, Source: declaration.Source, LogicalName: "deploy-key",
		ConfigJSON: []byte(`{"scope":"one"}`), PreparationKey: "prune:one",
	}
	request.BeforeIssue = func(context.Context, bool) error { return errors.New("partial prune") }
	if _, handled, err := runCredentialIssuerCapability(context.Background(), request); err == nil || !handled {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	prepared, err := loadCredentialOperationState(stateDir, request.Source, request.LogicalName)
	if err != nil || prepared.Status != credentialOperationPreparing || client.issueCalls != 0 {
		t.Fatalf("prepared state=%+v error=%v Issue calls=%d", prepared, err, client.issueCalls)
	}
	changed := request
	changed.ConfigJSON = []byte(`{"scope":"two"}`)
	changed.PreparationKey = "prune:two"
	changedPreparationCalls := 0
	changed.BeforeIssue = func(context.Context, bool) error {
		changedPreparationCalls++
		return nil
	}
	if _, handled, err := runCredentialIssuerCapability(context.Background(), changed); err == nil || !handled || !strings.Contains(err.Error(), "inputs changed") {
		t.Fatalf("changed preparation handled=%v error=%v", handled, err)
	}
	if changedPreparationCalls != 0 || client.issueCalls != 0 {
		t.Fatalf("changed preparation calls=%d Issue calls=%d", changedPreparationCalls, client.issueCalls)
	}
	request.BeforeIssue = func(context.Context, bool) error { return nil }
	result, handled, err := runCredentialIssuerCapability(context.Background(), request)
	if err != nil || !handled || result == nil || result.OperationID != prepared.OperationID || client.operationID != prepared.OperationID {
		t.Fatalf("result=%+v handled=%v error=%v Issue operation=%q", result, handled, err, client.operationID)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialOperationLockReacquiresCrashLeftFile(t *testing.T) {
	stateDir := t.TempDir()
	lockPath := strings.TrimSuffix(credentialOperationFile(stateDir, "example.source", "deploy-key"), ".json") + ".lock"
	if err := os.WriteFile(lockPath, []byte("crashed owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireCredentialOperationLock(stateDir, "example.source", "deploy-key")
	if err != nil {
		t.Fatalf("reacquire crash-left lock: %v", err)
	}
	release()
}

func TestCredentialIssuerLockIsHostGlobalThroughFinalize(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	lockDir := t.TempDir()
	first, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), LockDir: lockDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err != nil || !handled || first == nil {
		t.Fatalf("first issue result=%v handled=%v error=%v", first, handled, err)
	}
	_, handled, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), LockDir: lockDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !handled || !strings.Contains(err.Error(), "locked") || client.issueCalls != 1 {
		t.Fatalf("cross-project contention handled=%v error=%v Issue calls=%d", handled, err, client.issueCalls)
	}
	if err := first.Finalize(nil); err != nil {
		t.Fatal(err)
	}
	second, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), LockDir: lockDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err != nil || !handled || second == nil || client.issueCalls != 2 {
		t.Fatalf("post-finalize result=%v handled=%v error=%v Issue calls=%d", second, handled, err, client.issueCalls)
	}
	if err := second.Finalize(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialOperationStateDirectoryUsesUserStateAndProjectNamespace(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	first, err := credentialOperationStateDirForConfig(filepath.Join(t.TempDir(), "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := credentialOperationStateDirForConfig(filepath.Join(t.TempDir(), "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, filepath.Join(stateRoot, "provider-operations")+string(os.PathSeparator)) || first == second {
		t.Fatalf("state directories first=%q second=%q", first, second)
	}
	projectDir := t.TempDir()
	configPath := filepath.Join(projectDir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte("version: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectState, err := credentialOperationStateDirForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	renamedState, err := credentialOperationStateDirForConfig(filepath.Join(projectDir, "infra.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if projectState != renamedState {
		t.Fatalf("renamed config changed project state namespace: %q != %q", projectState, renamedState)
	}
	if runtime.GOOS != "windows" {
		aliasPath := filepath.Join(t.TempDir(), "workflow-link.yaml")
		if err := os.Symlink(configPath, aliasPath); err != nil {
			t.Fatal(err)
		}
		aliasState, err := credentialOperationStateDirForConfig(aliasPath)
		if err != nil {
			t.Fatal(err)
		}
		if projectState != aliasState {
			t.Fatalf("symlinked config changed project state namespace: %q != %q", projectState, aliasState)
		}
	}
	lockDir, err := credentialOperationLockDir()
	if err != nil {
		t.Fatal(err)
	}
	if lockDir != filepath.Join(stateRoot, "provider-operation-locks") || strings.HasPrefix(lockDir, first) || strings.HasPrefix(lockDir, second) {
		t.Fatalf("host-global lock directory=%q state directories=%q,%q", lockDir, first, second)
	}
}

func TestCredentialIssuerExistingStartedOperationReusesIDWithoutIssue(t *testing.T) {
	client := &recordingCredentialIssuerClient{listRecords: []*pb.CredentialRecord{{Identifier: "credential-1", IdentifierSensitive: true}}}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	stateDir := t.TempDir()
	seed := &credentialOperationState{
		OperationID: "existing-operation", Source: "example.source", LogicalName: "deploy-key",
		PluginName: "workflow-plugin-example", PluginVersion: "1.2.3",
		ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent, Acknowledgement: "not_required",
	}
	if err := persistCredentialOperationState(stateDir, seed, credentialOperationStarted); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key"})
	if err == nil {
		t.Fatal("want existing-operation reconciliation block")
	}
	if !strings.Contains(err.Error(), string(credentialOperationUnknownCreated)) || strings.Contains(err.Error(), string(credentialOperationStarted)) {
		t.Fatalf("reconciliation diagnostic status=%v, want %s", err, credentialOperationUnknownCreated)
	}
	if strings.Contains(err.Error(), "credential-1") {
		t.Fatalf("sensitive identifier leaked in diagnostic: %v", err)
	}
	if client.listContextCanceled || client.listOperationID != seed.OperationID || client.issueCalls != 0 {
		t.Fatalf("reconcile canceled=%v list operation=%q Issue calls=%d", client.listContextCanceled, client.listOperationID, client.issueCalls)
	}
	_, _, err = runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key"})
	if err == nil || client.issueCalls != 0 {
		t.Fatalf("existing unresolved state error=%v Issue calls=%d", err, client.issueCalls)
	}
	state, err := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if err != nil || state.OperationID != seed.OperationID || state.Identifier != "" || !state.IdentifierSensitive {
		t.Fatalf("state=%+v error=%v", state, err)
	}
}

func TestCredentialIssuerReconciliationUsesCommandContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &recordingCredentialIssuerClient{
		issueErr: context.Canceled, listRecords: []*pb.CredentialRecord{{Identifier: "credential-1", IdentifierSensitive: true}},
		issueCallback: cancel,
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	_, _, err := runCredentialIssuerCapability(ctx, credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !client.listContextCanceled || client.listOperationID != client.operationID || client.issueCalls != 1 {
		t.Fatalf("error=%v reconcile canceled=%v list operation=%q issue operation=%q Issue calls=%d", err, client.listContextCanceled, client.listOperationID, client.operationID, client.issueCalls)
	}
}

func TestCredentialIssuerCleanupStopsWhenCommandCanceled(t *testing.T) {
	declaration := config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	}
	for _, test := range []struct {
		name       string
		identifier string
		finalize   bool
	}{
		{name: "delete previous", identifier: "previous-credential"},
		{name: "rollback", identifier: "credential-123", finalize: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			client := &recordingCredentialIssuerClient{
				issueResponse: confirmedCredentialIssueResponse(),
				deleteResponse: &pb.CredentialDeleteResponse{
					Identifier: test.identifier, ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				},
			}
			withCredentialIssuerResolver(t, client, declaration)
			result, handled, err := runCredentialIssuerCapability(ctx, credentialIssuerRequest{
				StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
			})
			if err != nil || !handled {
				t.Fatalf("handled=%v error=%v", handled, err)
			}
			cancel()
			if test.finalize {
				err = result.Finalize(errors.New("store failed"))
			} else {
				err = result.DeletePrevious(test.identifier)
				_ = result.Finalize(nil)
			}
			if !errors.Is(err, context.Canceled) || client.deleteCalls != 0 {
				t.Fatalf("error=%v delete calls=%d", err, client.deleteCalls)
			}
		})
	}
}

func TestCredentialIssuerCleanupIsBoundedWhileCommandIsLive(t *testing.T) {
	client := &recordingCredentialIssuerClient{
		issueResponse: confirmedCredentialIssueResponse(),
		deleteResponse: &pb.CredentialDeleteResponse{
			Identifier: "previous-credential", ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	result, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v error=%v", handled, err)
	}
	if err := result.DeletePrevious("previous-credential"); err != nil {
		t.Fatal(err)
	}
	if !client.deleteContextDeadline || client.deleteContextCanceled {
		t.Fatalf("canceled=%v deadline=%v", client.deleteContextCanceled, client.deleteContextDeadline)
	}
	if err := result.Finalize(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialIssuerStopsBeforeIssueWhenPreparationCancels(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, handled, err := runCredentialIssuerCapability(ctx, credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
		BeforeIssue: func(context.Context, bool) error {
			cancel()
			return nil
		},
	})
	if !handled || !errors.Is(err, context.Canceled) || client.issueCalls != 0 {
		t.Fatalf("handled=%v error=%v Issue calls=%d", handled, err, client.issueCalls)
	}
}

func TestCredentialIssuerChecksCancellationImmediatelyBeforePreparation(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	ctx := &cancelOnSecondErrContext{Context: context.Background(), done: make(chan struct{})}
	preparationCalls := 0
	_, handled, err := runCredentialIssuerCapability(ctx, credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
		BeforeIssue: func(context.Context, bool) error {
			preparationCalls++
			return nil
		},
	})
	if !handled || !errors.Is(err, context.Canceled) || preparationCalls != 0 || client.issueCalls != 0 {
		t.Fatalf("handled=%v error=%v preparation calls=%d Issue calls=%d", handled, err, preparationCalls, client.issueCalls)
	}
}

func TestCredentialIssuerAlreadyCancelledStopsBeforeDurableMutation(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stateDir := t.TempDir()
	_, handled, err := runCredentialIssuerCapability(ctx, credentialIssuerRequest{
		StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if handled || !errors.Is(err, context.Canceled) || client.issueCalls != 0 {
		t.Fatalf("handled=%v error=%v Issue calls=%d", handled, err, client.issueCalls)
	}
	if _, statErr := os.Stat(credentialOperationFile(stateDir, "example.source", "deploy-key")); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled operation created durable state: %v", statErr)
	}
}

func TestCredentialIssuerReconciliationIsBoundedAndRedactsTransportErrors(t *testing.T) {
	client := &recordingCredentialIssuerClient{issueErr: errors.New("sensitive-value"), alwaysNextPage: true}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	stateDir := t.TempDir()
	_, _, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key"})
	if err == nil {
		t.Fatal("want uncertain operation error")
	}
	if client.listCalls != 10 {
		t.Fatalf("List calls=%d, want bounded 10", client.listCalls)
	}
	if strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("diagnostic leaked provider error: %v", err)
	}
	state, loadErr := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if loadErr != nil || state.Status != credentialOperationUnknown || state.Identifier != "" {
		t.Fatalf("partial inventory state=%+v error=%v", state, loadErr)
	}
}

func TestCredentialIssuerReconciliationRejectsSelectorMismatch(t *testing.T) {
	client := &recordingCredentialIssuerClient{
		issueErr: errors.New("lost response"),
		listRecords: []*pb.CredentialRecord{{
			Identifier: "credential-1", LogicalName: "different-name", OperationId: "different-operation",
		}},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	stateDir := t.TempDir()
	_, _, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: stateDir, Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !strings.Contains(err.Error(), "selector mismatch") {
		t.Fatalf("reconciliation error=%v", err)
	}
	state, loadErr := loadCredentialOperationState(stateDir, "example.source", "deploy-key")
	if loadErr != nil || state.Status != credentialOperationUnknown || state.Identifier != "" {
		t.Fatalf("state=%+v error=%v", state, loadErr)
	}
}

func TestCredentialIssuerReconciliationReturnsPersistenceFailure(t *testing.T) {
	client := &recordingCredentialIssuerClient{
		issueErr: errors.New("SENSITIVE_PROVIDER_ERROR"),
		listRecords: []*pb.CredentialRecord{{
			Identifier: "credential-1", LogicalName: "different-name", OperationId: "different-operation",
		}},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	originalPersist := persistCredentialReconciliationState
	persistCredentialReconciliationState = func(string, *credentialOperationState, credentialOperationStatus) error {
		return errors.New("persist failed")
	}
	t.Cleanup(func() { persistCredentialReconciliationState = originalPersist })
	_, _, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !strings.Contains(err.Error(), "selector mismatch") || !strings.Contains(err.Error(), "persist failed") {
		t.Fatalf("reconciliation error=%v", err)
	}
	if strings.Contains(err.Error(), "SENSITIVE_PROVIDER_ERROR") {
		t.Fatalf("provider error leaked: %v", err)
	}
}

func TestCredentialIssuerIssueRPCIsBoundedAndReconcilesAfterTimeout(t *testing.T) {
	client := &recordingCredentialIssuerClient{
		issueWaitForContext: true,
		listRecords:         []*pb.CredentialRecord{{Identifier: "credential-1", IdentifierSensitive: true}},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
	})
	originalTimeout := credentialIssueTimeout
	credentialIssueTimeout = 10 * time.Millisecond
	t.Cleanup(func() { credentialIssueTimeout = originalTimeout })
	started := time.Now()
	_, handled, err := runCredentialIssuerCapability(context.Background(), credentialIssuerRequest{
		StateDir: t.TempDir(), Source: "example.source", LogicalName: "deploy-key",
	})
	if err == nil || !handled || !client.issueContextCanceled || client.listCalls == 0 {
		t.Fatalf("handled=%v error=%v issue canceled=%v List calls=%d", handled, err, client.issueContextCanceled, client.listCalls)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded Issue took %s", elapsed)
	}
}

func withCredentialIssuerResolver(t *testing.T, client pb.CredentialIssuerClient, declaration config.CredentialSourceDecl) {
	t.Helper()
	oldResolve := resolveCredentialIssuerCapability
	oldSleep := credentialReconcileSleep
	resolveCredentialIssuerCapability = func(context.Context, string, string) (pb.CredentialIssuerClient, func(), config.CredentialSourceDecl, string, string, bool, error) {
		return client, func() {}, declaration, "workflow-plugin-example", "1.2.3", true, nil
	}
	credentialReconcileSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() {
		resolveCredentialIssuerCapability = oldResolve
		credentialReconcileSleep = oldSleep
	})
}

type recordingCredentialIssuerClient struct {
	issueCalls            int
	operationID           string
	issueResponse         *pb.CredentialIssueResponse
	issueErr              error
	issueWaitForContext   bool
	issueContextCanceled  bool
	issueCallback         func()
	listRecords           []*pb.CredentialRecord
	listCalls             int
	listOperationID       string
	listContextCanceled   bool
	alwaysNextPage        bool
	deleteCalls           int
	deleteOperationID     string
	deleteIdentifier      string
	deleteResponse        *pb.CredentialDeleteResponse
	deleteErr             error
	deleteContextCanceled bool
	deleteContextDeadline bool
}

type cancelOnSecondErrContext struct {
	context.Context
	done     chan struct{}
	errCalls int
	canceled bool
}

func (c *cancelOnSecondErrContext) Done() <-chan struct{} { return c.done }

func (c *cancelOnSecondErrContext) Err() error {
	c.errCalls++
	if c.errCalls < 2 {
		return nil
	}
	if !c.canceled {
		close(c.done)
		c.canceled = true
	}
	return context.Canceled
}

func (c *recordingCredentialIssuerClient) DescribeSources(context.Context, *pb.CredentialSourceDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialSourceDeclarationsResponse, error) {
	return &pb.CredentialSourceDeclarationsResponse{}, nil
}

func (c *recordingCredentialIssuerClient) Issue(ctx context.Context, request *pb.CredentialIssueRequest, _ ...grpc.CallOption) (*pb.CredentialIssueResponse, error) {
	c.issueCalls++
	c.operationID = request.GetOperationId()
	if c.issueCallback != nil {
		c.issueCallback()
	}
	if c.issueWaitForContext {
		<-ctx.Done()
		c.issueContextCanceled = true
		return nil, ctx.Err()
	}
	return c.issueResponse, c.issueErr
}

func (c *recordingCredentialIssuerClient) List(ctx context.Context, request *pb.CredentialListRequest, _ ...grpc.CallOption) (*pb.CredentialListResponse, error) {
	c.listCalls++
	c.listContextCanceled = c.listContextCanceled || ctx.Err() != nil
	c.listOperationID = request.GetSelector().GetOperationId()
	next := ""
	if c.alwaysNextPage {
		next = "next"
	}
	records := make([]*pb.CredentialRecord, 0, len(c.listRecords))
	for _, record := range c.listRecords {
		if record == nil {
			records = append(records, nil)
			continue
		}
		cloned := proto.Clone(record).(*pb.CredentialRecord)
		if cloned.LogicalName == "" {
			cloned.LogicalName = request.GetSelector().GetLogicalName()
		}
		if cloned.OperationId == "" {
			cloned.OperationId = request.GetSelector().GetOperationId()
		}
		records = append(records, cloned)
	}
	return &pb.CredentialListResponse{Credentials: records, NextPageToken: next}, nil
}

func confirmedCredentialIssueResponse() *pb.CredentialIssueResponse {
	return &pb.CredentialIssueResponse{
		Outputs:             []*pb.CredentialOutput{{Key: "id", Value: []byte("credential-123"), Sensitive: true}, {Key: "secret", Value: []byte("sensitive-value"), Sensitive: true}},
		Identifier:          "credential-123",
		IdentifierSensitive: true,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}
}

func (c *recordingCredentialIssuerClient) Delete(ctx context.Context, request *pb.CredentialDeleteRequest, _ ...grpc.CallOption) (*pb.CredentialDeleteResponse, error) {
	c.deleteCalls++
	c.deleteContextCanceled = c.deleteContextCanceled || ctx.Err() != nil
	_, c.deleteContextDeadline = ctx.Deadline()
	c.deleteOperationID = request.GetOperationId()
	c.deleteIdentifier = request.GetIdentifier()
	return c.deleteResponse, c.deleteErr
}

func mustReadCredentialState(t *testing.T, stateDir string) []byte {
	t.Helper()
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join(stateDir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatal("credential operation state file not found")
	return nil
}

type recordingContainerRegistryClient struct {
	operations               []string
	registry                 *pb.ContainerRegistryConfig
	imageReference           string
	dryRun                   bool
	operationError           *pb.ContainerRegistryError
	operationTransportErr    error
	emptyResponse            bool
	output                   []byte
	discoveryContextCanceled bool
}

type deadlineRecordingRegistryProvider struct {
	name             string
	operations       []string
	deadlineCalls    int
	cancelAfterFirst context.CancelFunc
}

func (p *deadlineRecordingRegistryProvider) Name() string { return p.name }

func (p *deadlineRecordingRegistryProvider) record(ctx registrypkg.Context, operation string) error {
	p.operations = append(p.operations, operation)
	if _, ok := ctx.Deadline(); ok {
		p.deadlineCalls++
	}
	if len(p.operations) == 1 && p.cancelAfterFirst != nil {
		p.cancelAfterFirst()
	}
	return nil
}

func (p *deadlineRecordingRegistryProvider) Login(ctx registrypkg.Context, _ registrypkg.ProviderConfig) error {
	return p.record(ctx, "login")
}

func (p *deadlineRecordingRegistryProvider) Logout(ctx registrypkg.Context, _ registrypkg.ProviderConfig) error {
	return p.record(ctx, "logout")
}

func (p *deadlineRecordingRegistryProvider) Push(ctx registrypkg.Context, _ registrypkg.ProviderConfig, _ string) error {
	return p.record(ctx, "push")
}

func (p *deadlineRecordingRegistryProvider) Prune(ctx registrypkg.Context, _ registrypkg.ProviderConfig) error {
	return p.record(ctx, "prune")
}

func (c *recordingContainerRegistryClient) DescribeRegistries(context.Context, *pb.ContainerRegistryDeclarationsRequest, ...grpc.CallOption) (*pb.ContainerRegistryDeclarationsResponse, error) {
	return &pb.ContainerRegistryDeclarationsResponse{}, nil
}

func (c *recordingContainerRegistryClient) Login(_ context.Context, request *pb.ContainerRegistryLoginRequest, _ ...grpc.CallOption) (*pb.ContainerRegistryOperationResponse, error) {
	c.record("login", request.GetRegistry(), request.GetDryRun(), "")
	if c.operationTransportErr != nil {
		return nil, c.operationTransportErr
	}
	return c.response(), nil
}

func (c *recordingContainerRegistryClient) Logout(_ context.Context, request *pb.ContainerRegistryLogoutRequest, _ ...grpc.CallOption) (*pb.ContainerRegistryOperationResponse, error) {
	c.record("logout", request.GetRegistry(), request.GetDryRun(), "")
	if c.operationTransportErr != nil {
		return nil, c.operationTransportErr
	}
	return c.response(), nil
}

func (c *recordingContainerRegistryClient) Push(_ context.Context, request *pb.ContainerRegistryPushRequest, _ ...grpc.CallOption) (*pb.ContainerRegistryOperationResponse, error) {
	c.record("push", request.GetRegistry(), request.GetDryRun(), request.GetImageReference())
	if c.operationTransportErr != nil {
		return nil, c.operationTransportErr
	}
	return c.response(), nil
}

func (c *recordingContainerRegistryClient) Prune(_ context.Context, request *pb.ContainerRegistryPruneRequest, _ ...grpc.CallOption) (*pb.ContainerRegistryOperationResponse, error) {
	c.record("prune", request.GetRegistry(), request.GetDryRun(), "")
	if c.operationTransportErr != nil {
		return nil, c.operationTransportErr
	}
	return c.response(), nil
}

func (c *recordingContainerRegistryClient) response() *pb.ContainerRegistryOperationResponse {
	if c.emptyResponse {
		return &pb.ContainerRegistryOperationResponse{}
	}
	return &pb.ContainerRegistryOperationResponse{Result: &pb.ContainerRegistryResult{Output: c.output}, Error: c.operationError}
}

func (c *recordingContainerRegistryClient) record(operation string, registry *pb.ContainerRegistryConfig, dryRun bool, image string) {
	c.operations = append(c.operations, operation)
	c.registry = registry
	c.dryRun = dryRun
	if operation == "push" {
		c.imageReference = image
	}
}

func TestProviderCapabilityIndexRejectsCollisionBeforeRouting(t *testing.T) {
	owners := []providerCapabilityOwner{
		{Name: "z-plugin", Version: "2.0.0", Declarations: config.ProviderDeclarations{ContainerRegistries: []config.ContainerRegistryDecl{{Type: "shared", Operations: []string{"login"}}}}},
		{Name: "a-plugin", Version: "1.0.0", Declarations: config.ProviderDeclarations{ContainerRegistries: []config.ContainerRegistryDecl{{Type: "shared", Operations: []string{"login"}}}}},
	}
	index, err := newProviderCapabilityIndex(owners)
	if err != nil {
		t.Fatal(err)
	}
	_, err = index.selectContainerRegistry("shared", "login")
	if err == nil {
		t.Fatal("want collision error")
	}
	if got := err.Error(); !strings.Contains(got, "a-plugin@1.0.0, z-plugin@2.0.0") || !strings.Contains(got, "collision") {
		t.Fatalf("collision error = %q", got)
	}
}

func TestProviderCapabilityIndexRejectsSplitOwnershipBeforeOperationSelection(t *testing.T) {
	index, err := newProviderCapabilityIndex([]providerCapabilityOwner{
		{Name: "a-plugin", Version: "1.0.0", Declarations: config.ProviderDeclarations{
			CredentialSources: []config.CredentialSourceDecl{{
				Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
				Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
			}},
			CredentialResolvers: []config.CredentialResolverDecl{{Provider: "aws", CredentialTypes: []string{"static"}}},
			KubernetesBackends:  []config.KubernetesBackendDecl{{Name: "a-cluster", ResourceType: "infra.example_cluster"}},
			ContainerRegistries: []config.ContainerRegistryDecl{{Type: "example-registry", Operations: []string{"login"}}},
			SecretStores:        []config.SecretStoreDecl{{Type: "example-secrets", Operations: []string{"get"}, Scopes: []string{"account"}}},
		}},
		{Name: "b-plugin", Version: "2.0.0", Declarations: config.ProviderDeclarations{
			CredentialSources: []config.CredentialSourceDecl{{
				Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
				Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
			}},
			CredentialResolvers: []config.CredentialResolverDecl{{Provider: "aws", CredentialTypes: []string{"env"}}},
			KubernetesBackends:  []config.KubernetesBackendDecl{{Name: "b-cluster", ResourceType: "infra.example_cluster"}},
			ContainerRegistries: []config.ContainerRegistryDecl{{Type: "example-registry", Operations: []string{"push"}}},
			SecretStores:        []config.SecretStoreDecl{{Type: "example-secrets", Operations: []string{"list"}, Scopes: []string{"region"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	selectors := []struct {
		name        string
		selectRoute func() (providerCapabilityRoute, error)
	}{
		{"credential source", func() (providerCapabilityRoute, error) { return index.selectCredentialSource("example.source") }},
		{"credential resolver provider", func() (providerCapabilityRoute, error) { return index.selectCredentialResolver("aws", "static") }},
		{"kubernetes resource type", func() (providerCapabilityRoute, error) { return index.selectKubernetesBackend("a-cluster") }},
		{"container registry type", func() (providerCapabilityRoute, error) {
			return index.selectContainerRegistry("example-registry", "login")
		}},
		{"secret store type", func() (providerCapabilityRoute, error) {
			return index.selectSecretStore("example-secrets", "get", "account")
		}},
	}
	for _, selector := range selectors {
		t.Run(selector.name, func(t *testing.T) {
			_, err := selector.selectRoute()
			if err == nil || !strings.Contains(err.Error(), "collision") || !strings.Contains(err.Error(), "a-plugin@1.0.0, b-plugin@2.0.0") {
				t.Fatalf("collision error=%v", err)
			}
		})
	}
}

func TestLoadProviderCapabilityIndexRejectsMalformedAndInvalidManifests(t *testing.T) {
	pluginDir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		dir := filepath.Join(pluginDir, name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("broken", `{`)
	if _, err := loadProviderCapabilityIndex(pluginDir); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("malformed manifest error = %v", err)
	}

	if err := os.RemoveAll(filepath.Join(pluginDir, "broken")); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"name":                "invalid",
		"version":             "1.0.0",
		"author":              "Workflow tests",
		"description":         "invalid provider declaration fixture",
		"containerRegistries": []map[string]any{{"type": "example", "operations": []string{"pull"}}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	write("invalid", string(data))
	if _, err := loadProviderCapabilityIndex(pluginDir); err == nil || !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("invalid manifest error = %v", err)
	}

	if err := os.RemoveAll(filepath.Join(pluginDir, "invalid")); err != nil {
		t.Fatal(err)
	}
	write("example", `{"name":"workflow-plugin-example","version":"1.0.0","author":"Workflow tests","description":"normalized installed layout","containerRegistries":[{"type":"example","operations":["login"]}]}`)
	index, err := loadProviderCapabilityIndex(pluginDir)
	if err != nil {
		t.Fatalf("load normalized installed layout: %v", err)
	}
	if route, selectErr := index.selectContainerRegistry("example", "login"); selectErr != nil || route.PluginName != "workflow-plugin-example" || route.PluginInstallName != "example" {
		t.Fatalf("normalized layout route=%+v error=%v", route, selectErr)
	}

	if err := os.RemoveAll(filepath.Join(pluginDir, "example")); err != nil {
		t.Fatal(err)
	}
	write("wrong-directory", `{"name":"different-plugin","version":"1.0.0","author":"Workflow tests","description":"layout mismatch fixture"}`)
	if _, err := loadProviderCapabilityIndex(pluginDir); err == nil || !strings.Contains(err.Error(), "wrong-directory") || !strings.Contains(err.Error(), "different-plugin") {
		t.Fatalf("layout mismatch error = %v", err)
	}
}

func TestCompareProviderDeclarationsWithRuntimeRequiresExactParity(t *testing.T) {
	declared := config.ProviderDeclarations{
		CredentialSources: []config.CredentialSourceDecl{{
			Source:          "example.source",
			ConcurrencyMode: config.CredentialConcurrencySingleWriter,
			Outputs:         []config.CredentialOutputDecl{{Key: "id"}},
			IdentifierKey:   "id",
		}},
		CredentialResolvers: []config.CredentialResolverDecl{{Provider: "aws", CredentialTypes: []string{"static"}}},
		KubernetesBackends:  []config.KubernetesBackendDecl{{Name: "example-k8s", ResourceType: "infra.example_cluster"}},
		ContainerRegistries: []config.ContainerRegistryDecl{{Type: "example-registry", Operations: []string{"login"}}},
		SecretStores:        []config.SecretStoreDecl{{Type: "example-store", Operations: []string{"get"}, Scopes: []string{"account"}}},
	}
	runtime := providerRuntimeDeclarations{
		AdvertisedServices: map[string]bool{
			pb.CredentialIssuer_ServiceDesc.ServiceName:   true,
			pb.CredentialResolver_ServiceDesc.ServiceName: true,
			pb.ResourceDriver_ServiceDesc.ServiceName:     true,
			pb.ContainerRegistry_ServiceDesc.ServiceName:  true,
			pb.SecretStore_ServiceDesc.ServiceName:        true,
		},
		CredentialSources: []*pb.CredentialSourceDeclaration{{
			Source:          "example.source",
			ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_SINGLE_WRITER_REQUIRED,
			Outputs:         []*pb.CredentialOutputDeclaration{{Key: "id", Sensitive: true}},
			IdentifierKey:   "id",
		}},
		CredentialResolvers:     []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		KubernetesResourceTypes: []string{"infra.example_cluster"},
		ContainerRegistries:     []*pb.ContainerRegistryDeclaration{{Type: "example-registry", Operations: []string{"login"}}},
		SecretStores:            []*pb.SecretStoreDeclaration{{Type: "example-store", Operations: []string{"get"}, Scopes: []string{"account"}}},
	}
	if failures := compareProviderDeclarationsWithRuntime(declared, runtime); len(failures) != 0 {
		t.Fatalf("matching declarations failed: %v", failures)
	}

	delete(runtime.AdvertisedServices, pb.ContainerRegistry_ServiceDesc.ServiceName)
	runtime.SecretStores = append(runtime.SecretStores, &pb.SecretStoreDeclaration{Type: "undeclared", Operations: []string{"get"}, Scopes: []string{"account"}})
	failures := compareProviderDeclarationsWithRuntime(declared, runtime)
	joined := strings.Join(failures, "; ")
	if !strings.Contains(joined, "containerRegistries") || !strings.Contains(joined, "does not advertise") {
		t.Fatalf("missing service failures = %v", failures)
	}
	if !strings.Contains(joined, "secretStores") || !strings.Contains(joined, "undeclared") {
		t.Fatalf("extra runtime declaration failures = %v", failures)
	}
}

func TestCompareProviderDeclarationsWithRuntimeRejectsUndeclaredServedFamily(t *testing.T) {
	runtime := providerRuntimeDeclarations{
		AdvertisedServices: map[string]bool{pb.CredentialIssuer_ServiceDesc.ServiceName: true},
		CredentialSources: []*pb.CredentialSourceDeclaration{{
			Source:          "undeclared",
			ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
			Outputs:         []*pb.CredentialOutputDeclaration{{Key: "id", Sensitive: true}},
			IdentifierKey:   "id",
		}},
	}
	failures := compareProviderDeclarationsWithRuntime(config.ProviderDeclarations{}, runtime)
	if joined := strings.Join(failures, "; "); !strings.Contains(joined, "credentialSources") || !strings.Contains(joined, "undeclared") {
		t.Fatalf("failures = %v", failures)
	}
}

func TestCompareProviderDeclarationsWithRuntimeRejectsMismatchInEveryFamily(t *testing.T) {
	declared := config.ProviderDeclarations{
		CredentialSources: []config.CredentialSourceDecl{{
			Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
			Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
		}},
		CredentialResolvers: []config.CredentialResolverDecl{{Provider: "example", CredentialTypes: []string{"static"}}},
		KubernetesBackends:  []config.KubernetesBackendDecl{{Name: "example-k8s", ResourceType: "infra.example_cluster"}},
		ContainerRegistries: []config.ContainerRegistryDecl{{Type: "example-registry", Operations: []string{"login"}}},
		SecretStores:        []config.SecretStoreDecl{{Type: "example-store", Operations: []string{"get"}, Scopes: []string{"account"}}},
	}
	runtimeForTest := func() providerRuntimeDeclarations {
		return providerRuntimeDeclarations{
			AdvertisedServices: map[string]bool{
				pb.CredentialIssuer_ServiceDesc.ServiceName:   true,
				pb.CredentialResolver_ServiceDesc.ServiceName: true,
				pb.ResourceDriver_ServiceDesc.ServiceName:     true,
				pb.ContainerRegistry_ServiceDesc.ServiceName:  true,
				pb.SecretStore_ServiceDesc.ServiceName:        true,
			},
			CredentialSources: []*pb.CredentialSourceDeclaration{{
				Source: "example.source", ConcurrencyMode: pb.CredentialConcurrencyMode_CREDENTIAL_CONCURRENCY_MODE_PROVIDER_IDEMPOTENT,
				Outputs: []*pb.CredentialOutputDeclaration{{Key: "id", Sensitive: true}}, IdentifierKey: "id",
			}},
			CredentialResolvers:     []*pb.CredentialResolverDeclaration{{Provider: "example", CredentialTypes: []string{"static"}}},
			KubernetesResourceTypes: []string{"infra.example_cluster"},
			ContainerRegistries:     []*pb.ContainerRegistryDeclaration{{Type: "example-registry", Operations: []string{"login"}}},
			SecretStores:            []*pb.SecretStoreDeclaration{{Type: "example-store", Operations: []string{"get"}, Scopes: []string{"account"}}},
		}
	}
	tests := []struct {
		family string
		mutate func(*providerRuntimeDeclarations)
	}{
		{"credentialSources", func(runtime *providerRuntimeDeclarations) { runtime.CredentialSources[0].Source = "different.source" }},
		{"credentialResolvers", func(runtime *providerRuntimeDeclarations) { runtime.CredentialResolvers[0].Provider = "different" }},
		{"kubernetesBackends", func(runtime *providerRuntimeDeclarations) { runtime.KubernetesResourceTypes = nil }},
		{"containerRegistries", func(runtime *providerRuntimeDeclarations) {
			runtime.ContainerRegistries[0].Operations = []string{"push"}
		}},
		{"secretStores", func(runtime *providerRuntimeDeclarations) { runtime.SecretStores[0].Scopes = []string{"region"} }},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			runtime := runtimeForTest()
			test.mutate(&runtime)
			if failures := compareProviderDeclarationsWithRuntime(declared, runtime); len(failures) == 0 || !strings.Contains(strings.Join(failures, "; "), test.family) {
				t.Fatalf("failures=%v", failures)
			}
		})
	}
}

func TestCompareProviderDeclarationsWithRuntimeAllowsUnrelatedIaCResourceTypes(t *testing.T) {
	declared := config.ProviderDeclarations{KubernetesBackends: []config.KubernetesBackendDecl{{
		Name: "example-k8s", ResourceType: "infra.example_cluster",
	}}}
	runtime := providerRuntimeDeclarations{
		AdvertisedServices: map[string]bool{pb.ResourceDriver_ServiceDesc.ServiceName: true},
		KubernetesResourceTypes: []string{
			"infra.example_database",
			"infra.example_cluster",
			"infra.example_network",
		},
	}
	if failures := compareProviderDeclarationsWithRuntime(declared, runtime); len(failures) != 0 {
		t.Fatalf("unrelated IaC resource types must not fail kubernetes backend parity: %v", failures)
	}

	runtime.KubernetesResourceTypes = []string{"infra.example_database"}
	if failures := compareProviderDeclarationsWithRuntime(declared, runtime); len(failures) != 1 || !strings.Contains(failures[0], "infra.example_cluster") {
		t.Fatalf("missing backend resource type failures = %v", failures)
	}
}
