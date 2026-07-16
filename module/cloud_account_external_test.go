package module_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type cloudAccountExternalResolverServer struct {
	pb.UnimplementedCredentialResolverServer

	declarations []*pb.CredentialResolverDeclaration
	mu           sync.Mutex
	requests     []*pb.CredentialResolveRequest
	resolve      func(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error)
}

func (s *cloudAccountExternalResolverServer) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest) (*pb.CredentialResolverDeclarationsResponse, error) {
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: s.declarations}, nil
}

func (s *cloudAccountExternalResolverServer) Resolve(ctx context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()
	if s.resolve != nil {
		return s.resolve(ctx, request)
	}
	return &pb.CredentialResolveResponse{Credentials: fullExternalCredentials(request.GetProvider())}, nil
}

func (s *cloudAccountExternalResolverServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *cloudAccountExternalResolverServer) capturedRequests() []*pb.CredentialResolveRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*pb.CredentialResolveRequest(nil), s.requests...)
}

func fullExternalCredentials(provider string) *pb.ResolvedCloudCredentials {
	return &pb.ResolvedCloudCredentials{
		Provider: provider, Region: "external-region", AccessKey: "access", SecretKey: "secret",
		SessionToken: "session", RoleArn: "role", ProjectId: "project",
		ServiceAccountJson: []byte("service-account"), TenantId: "tenant", ClientId: "client",
		ClientSecret: "client-secret", SubscriptionId: "subscription",
		Kubeconfig: []byte("kubeconfig"), Context: "context", Token: "token",
		Extra: map[string]string{"credential_source": "external"},
	}
}

func newCloudAccountExternalResolverClient(t *testing.T, server pb.CredentialResolverServer) pb.CredentialResolverClient {
	t.Helper()
	listener := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = listener.Close() })
	grpcServer := grpc.NewServer()
	if server != nil {
		pb.RegisterCredentialResolverServer(grpcServer, server)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///credential-resolver",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCredentialResolverClient(conn)
}

func registerCloudAccountExternalResolver(t *testing.T, server *cloudAccountExternalResolverServer) {
	t.Helper()
	unregister, err := module.RegisterExternalCredentialResolver(context.Background(), newCloudAccountExternalResolverClient(t, server))
	if err != nil {
		t.Fatalf("RegisterExternalCredentialResolver: %v", err)
	}
	t.Cleanup(unregister)
}

func TestCloudAccountExternalPreservesEveryCredentialTypeAndFullOutput(t *testing.T) {
	allowed := map[string][]string{
		"aws":   {"static", "env", "profile", "role_arn"},
		"gcp":   {"static", "env", "service_account_json", "service_account_key", "workload_identity", "application_default"},
		"azure": {"static", "env", "client_credentials", "managed_identity", "cli"},
	}
	server := &cloudAccountExternalResolverServer{}
	for provider, credentialTypes := range allowed {
		server.declarations = append(server.declarations, &pb.CredentialResolverDeclaration{Provider: provider, CredentialTypes: credentialTypes})
	}
	registerCloudAccountExternalResolver(t, server)

	for provider, credentialTypes := range allowed {
		for _, credentialType := range credentialTypes {
			t.Run(provider+"_"+credentialType, func(t *testing.T) {
				account := module.NewCloudAccount("external-account", map[string]any{
					"provider": provider,
					"region":   "core-region-must-not-win",
					"credentials": map[string]any{
						"type": credentialType,
						"opaque_provider_config": map[string]any{
							"secret": "forwarded-only",
						},
					},
				})
				if err := account.Init(module.NewMockApplication()); err != nil {
					t.Fatalf("Init: %v", err)
				}
				credentials, err := account.GetCredentials(context.Background())
				if err != nil {
					t.Fatalf("GetCredentials: %v", err)
				}
				assertFullExternalCredentials(t, credentials, provider)
			})
		}
	}

	requests := server.capturedRequests()
	if len(requests) != 15 {
		t.Fatalf("Resolve calls = %d, want 15", len(requests))
	}
	for _, request := range requests {
		var forwarded map[string]any
		if err := json.Unmarshal(request.GetConfigJson(), &forwarded); err != nil {
			t.Fatalf("forwarded config is not JSON: %v", err)
		}
		credentials, _ := forwarded["credentials"].(map[string]any)
		opaque, _ := credentials["opaque_provider_config"].(map[string]any)
		if opaque["secret"] != "forwarded-only" {
			t.Fatalf("provider config was interpreted or lost: %s", request.GetConfigJson())
		}
	}
}

func assertFullExternalCredentials(t *testing.T, credentials *module.CloudCredentials, provider string) {
	t.Helper()
	if credentials.Provider != provider || credentials.Region != "external-region" ||
		credentials.AccessKey != "access" || credentials.SecretKey != "secret" ||
		credentials.SessionToken != "session" || credentials.RoleARN != "role" ||
		credentials.ProjectID != "project" || string(credentials.ServiceAccountJSON) != "service-account" ||
		credentials.TenantID != "tenant" || credentials.ClientID != "client" ||
		credentials.ClientSecret != "client-secret" || credentials.SubscriptionID != "subscription" ||
		string(credentials.Kubeconfig) != "kubeconfig" || credentials.Context != "context" ||
		credentials.Token != "token" || credentials.Extra["credential_source"] != "external" {
		t.Fatalf("full external output was not preserved: %+v", credentials)
	}
}

func TestCloudAccountExternalSelectionFailsBeforeInvocation(t *testing.T) {
	t.Run("zero match gives install guidance", func(t *testing.T) {
		_, err := module.ResolveExternalCloudCredentials(context.Background(), "uninstalled", "static", map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "install a plugin") {
			t.Fatalf("zero-match error = %v", err)
		}
	})

	t.Run("provider and type mismatch", func(t *testing.T) {
		server := &cloudAccountExternalResolverServer{declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}}}
		registerCloudAccountExternalResolver(t, server)
		for _, selection := range [][2]string{{"gcp", "static"}, {"aws", "env"}} {
			_, err := module.ResolveExternalCloudCredentials(context.Background(), selection[0], selection[1], map[string]any{})
			if err == nil || !strings.Contains(err.Error(), "install a plugin") {
				t.Fatalf("ResolveExternalCloudCredentials(%v) = %v", selection, err)
			}
		}
		if server.requestCount() != 0 {
			t.Fatalf("mismatched selection invoked provider %d times", server.requestCount())
		}
	})

	t.Run("multiple exact matches", func(t *testing.T) {
		first := &cloudAccountExternalResolverServer{declarations: []*pb.CredentialResolverDeclaration{{Provider: "azure", CredentialTypes: []string{"static"}}}}
		second := &cloudAccountExternalResolverServer{declarations: []*pb.CredentialResolverDeclaration{{Provider: "azure", CredentialTypes: []string{"static"}}}}
		registerCloudAccountExternalResolver(t, first)
		registerCloudAccountExternalResolver(t, second)
		_, err := module.ResolveExternalCloudCredentials(context.Background(), "azure", "static", map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "multiple external credential resolvers") {
			t.Fatalf("multiple-match error = %v", err)
		}
		if first.requestCount() != 0 || second.requestCount() != 0 {
			t.Fatalf("collision invoked providers: first=%d second=%d", first.requestCount(), second.requestCount())
		}
	})
}

func TestCloudAccountExternalRegistrationRejectsCoreLocalAndUnknownDeclarations(t *testing.T) {
	for _, test := range []struct {
		name            string
		provider        string
		credentialTypes []string
	}{
		{name: "mock", provider: "mock", credentialTypes: []string{"static"}},
		{name: "kubernetes", provider: "kubernetes", credentialTypes: []string{"static"}},
		{name: "unknown provider", provider: "unsupported", credentialTypes: []string{"static"}},
		{name: "unknown aws type", provider: "aws", credentialTypes: []string{"application_default"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &cloudAccountExternalResolverServer{declarations: []*pb.CredentialResolverDeclaration{{
				Provider: test.provider, CredentialTypes: test.credentialTypes,
			}}}
			cleanup, err := module.RegisterExternalCredentialResolver(context.Background(), newCloudAccountExternalResolverClient(t, server))
			if cleanup != nil || err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("registration returned cleanup=%t, error=%v", cleanup != nil, err)
			}
			if server.requestCount() != 0 {
				t.Fatalf("invalid declaration invoked Resolve %d times", server.requestCount())
			}
		})
	}
}

func TestCloudAccountExternalPreservesConfiguredRegionWhenResponseOmitsIt(t *testing.T) {
	server := &cloudAccountExternalResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		resolve: func(_ context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
			return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{Provider: request.GetProvider()}}, nil
		},
	}
	registerCloudAccountExternalResolver(t, server)
	account := module.NewCloudAccount("region-fallback", map[string]any{
		"provider": "aws",
		"region":   "configured-region",
		"credentials": map[string]any{
			"type": "static",
		},
	})
	if err := account.Init(module.NewMockApplication()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil || credentials.Region != "configured-region" {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
}

func TestCloudAccountExternalCompatibilityMissingCredentialsPreservesTopLevelFields(t *testing.T) {
	for _, test := range []struct {
		name        string
		provider    string
		config      map[string]any
		wantProject string
		wantSub     string
	}{
		{
			name:        "gcp project",
			provider:    "gcp",
			config:      map[string]any{"project_id": "top-level-project"},
			wantProject: "top-level-project",
		},
		{
			name:     "azure subscription",
			provider: "azure",
			config:   map[string]any{"subscription_id": "top-level-subscription"},
			wantSub:  "top-level-subscription",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := map[string]any{"provider": test.provider}
			for key, value := range test.config {
				config[key] = value
			}
			account := module.NewCloudAccount("compatibility", config)
			if err := account.Init(module.NewMockApplication()); err != nil {
				t.Fatalf("Init: %v", err)
			}
			credentials, err := account.GetCredentials(context.Background())
			if err != nil {
				t.Fatalf("GetCredentials: %v", err)
			}
			if credentials.ProjectID != test.wantProject || credentials.SubscriptionID != test.wantSub {
				t.Fatalf("top-level compatibility fields = project %q subscription %q", credentials.ProjectID, credentials.SubscriptionID)
			}
		})
	}
}

func TestCloudAccountExternalDoesNotInterpretProviderShapedTopLevelFields(t *testing.T) {
	server := &cloudAccountExternalResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "gcp", CredentialTypes: []string{"static"}}},
		resolve: func(_ context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
			return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{Provider: request.GetProvider()}}, nil
		},
	}
	registerCloudAccountExternalResolver(t, server)
	account := module.NewCloudAccount("opaque-provider-fields", map[string]any{
		"provider":   "gcp",
		"project_id": "must-remain-opaque",
		"credentials": map[string]any{
			"type": "static",
		},
	})
	if err := account.Init(module.NewMockApplication()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if credentials.ProjectID != "" {
		t.Fatalf("external path interpreted project_id: %+v", credentials)
	}
}

func TestCloudAccountExternalErrorsAreRedactedAndPayloadCleared(t *testing.T) {
	for _, test := range []struct {
		name          string
		resolve       func(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error)
		code          string
		wantTyped     bool
		wantRetryable bool
	}{
		{
			name: "structured",
			resolve: func(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
				return &pb.CredentialResolveResponse{
					Credentials: &pb.ResolvedCloudCredentials{Provider: "aws", SecretKey: "payload-secret"},
					Error:       &pb.CredentialResolutionError{Code: "expired_token", Message: "structured-secret", Retryable: true},
				}, nil
			},
			code:          "expired_token",
			wantTyped:     true,
			wantRetryable: true,
		},
		{
			name: "plain",
			resolve: func(context.Context, *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
				return nil, status.Error(13, "plain-secret")
			},
			code: "transport_Internal",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &cloudAccountExternalResolverServer{
				declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
				resolve:      test.resolve,
			}
			registerCloudAccountExternalResolver(t, server)
			credentials, err := module.ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{"secret": "request-secret"})
			if err == nil || credentials != nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("resolution = %+v, %v", credentials, err)
			}
			var typedError *module.ExternalCredentialResolutionError
			if gotTyped := errors.As(err, &typedError); gotTyped != test.wantTyped {
				t.Fatalf("errors.As typed resolution error = %t, want %t (error %v)", gotTyped, test.wantTyped, err)
			}
			if typedError != nil && (typedError.Code != test.code || typedError.Retryable != test.wantRetryable) {
				t.Fatalf("typed resolution error = %+v", typedError)
			}
			for _, forbidden := range []string{"payload-secret", "structured-secret", "plain-secret", "request-secret"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("error leaked %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestCloudAccountExternalCancellationReachesService(t *testing.T) {
	entered := make(chan struct{})
	server := &cloudAccountExternalResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "gcp", CredentialTypes: []string{"env"}}},
		resolve: func(ctx context.Context, _ *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	registerCloudAccountExternalResolver(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := module.ResolveExternalCloudCredentials(ctx, "gcp", "env", map[string]any{})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("service was not invoked")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled resolution did not return")
	}
}

func TestCloudAccountExternalLegacyAbsenceIsOptional(t *testing.T) {
	client := newCloudAccountExternalResolverClient(t, nil)
	unregister, err := module.RegisterExternalCredentialResolver(context.Background(), client)
	if unregister != nil || err == nil || !strings.Contains(err.Error(), "does not serve the optional CredentialResolver contract") {
		t.Fatalf("legacy registration returned cleanup=%t, error=%v", unregister != nil, err)
	}
	_, err = module.ResolveExternalCloudCredentials(context.Background(), "legacy", "static", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "install a plugin") {
		t.Fatalf("legacy zero-match guidance = %v", err)
	}
}
