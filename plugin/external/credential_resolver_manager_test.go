package external

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/module"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type managerCredentialResolverServer struct {
	pb.UnimplementedCredentialResolverServer
	declarations   []*pb.CredentialResolverDeclaration
	accessKey      string
	calls          atomic.Int32
	resolveStarted chan struct{}
	resolveRelease <-chan struct{}
	startOnce      sync.Once
}

func (s *managerCredentialResolverServer) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest) (*pb.CredentialResolverDeclarationsResponse, error) {
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: s.declarations}, nil
}

func (s *managerCredentialResolverServer) Resolve(ctx context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	s.calls.Add(1)
	if s.resolveStarted != nil {
		s.startOnce.Do(func() { close(s.resolveStarted) })
	}
	if s.resolveRelease != nil {
		select {
		case <-s.resolveRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(), AccessKey: s.accessKey,
	}}, nil
}

func newManagerCredentialResolverAdapter(t *testing.T, server pb.CredentialResolverServer, advertised bool) *ExternalPluginAdapter {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	t.Cleanup(func() { _ = listener.Close() })
	grpcServer := grpc.NewServer()
	pb.RegisterCredentialResolverServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///manager-credential-resolver",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	registry := &pb.ContractRegistry{}
	if advertised {
		registry.Contracts = []*pb.ContractDescriptor{{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: pb.CredentialResolver_ServiceDesc.ServiceName,
		}}
	}
	return &ExternalPluginAdapter{
		name:             "resolver-fixture",
		client:           &PluginClient{conn: conn},
		contractRegistry: registry,
	}
}

func managerCloudAccountAccessKey(t *testing.T) (string, error) {
	t.Helper()
	account := module.NewCloudAccount("manager-account", map[string]any{
		"provider": "aws",
		"credentials": map[string]any{
			"type":      "static",
			"accessKey": "builtin-access",
		},
	})
	if err := account.Init(module.NewMockApplication()); err != nil {
		return "", err
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil {
		return "", err
	}
	return credentials.AccessKey, nil
}

func TestExternalPluginManagerCredentialResolverLoadUnloadLifecycle(t *testing.T) {
	server := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "external-access",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, server, true)}, nil
	}
	if _, err := manager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "external-access" {
		t.Fatalf("loaded cloud.account access key = %q, %v", accessKey, err)
	}
	if server.calls.Load() != 1 {
		t.Fatalf("external Resolve calls = %d, want 1", server.calls.Load())
	}
	if err := manager.UnloadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("UnloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "builtin-access" {
		t.Fatalf("unloaded cloud.account access key = %q, %v", accessKey, err)
	}
}

func TestExternalPluginManagerCredentialResolverRequiresAdvertisement(t *testing.T) {
	server := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "must-not-run",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, server, false)}, nil
	}
	if _, err := manager.LoadPlugin("legacy-fixture"); err != nil {
		t.Fatalf("LoadPlugin legacy: %v", err)
	}
	t.Cleanup(manager.Shutdown)
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "builtin-access" {
		t.Fatalf("unadvertised cloud.account access key = %q, %v", accessKey, err)
	}
	if server.calls.Load() != 0 {
		t.Fatalf("unadvertised resolver called %d times", server.calls.Load())
	}
}

func TestExternalPluginManagerCredentialResolverInvalidCandidateFailsClosed(t *testing.T) {
	server := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "kubernetes", CredentialTypes: []string{"static"}}},
		accessKey:    "must-not-run",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, server, true)}, nil
	}
	_, err := manager.LoadPlugin("invalid-fixture")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("LoadPlugin invalid declaration = %v", err)
	}
	if manager.IsLoaded("invalid-fixture") {
		t.Fatal("invalid resolver candidate remained loaded")
	}
	if accessKey, resolveErr := managerCloudAccountAccessKey(t); resolveErr != nil || accessKey != "builtin-access" {
		t.Fatalf("invalid candidate affected registry: access key %q, %v", accessKey, resolveErr)
	}
}

func TestExternalPluginManagerCredentialResolverReloadLifecycle(t *testing.T) {
	oldServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "old-access",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, oldServer, true)}, nil
	}
	if _, err := manager.LoadPlugin("reload-fixture"); err != nil {
		t.Fatalf("initial LoadPlugin: %v", err)
	}
	oldRegistration := manager.credentialResolverRegistrations["reload-fixture"]
	if oldRegistration == nil {
		t.Fatal("manager did not retain the prepared resolver registration")
	}
	t.Cleanup(manager.Shutdown)

	invalidServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "mock", CredentialTypes: []string{"static"}}},
	}
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, invalidServer, true)}, nil
	}
	if _, err := manager.ReloadPlugin("reload-fixture"); err == nil {
		t.Fatal("invalid reload unexpectedly succeeded")
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "old-access" {
		t.Fatalf("failed reload did not preserve old resolver: access key %q, %v", accessKey, err)
	}
	if manager.credentialResolverRegistrations["reload-fixture"] != oldRegistration {
		t.Fatal("failed reload replaced the old resolver registration")
	}

	newServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "new-access",
	}
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, newServer, true)}, nil
	}
	if _, err := manager.ReloadPlugin("reload-fixture"); err != nil {
		t.Fatalf("valid ReloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "new-access" {
		t.Fatalf("successful reload left stale resolver: access key %q, %v", accessKey, err)
	}
	if manager.credentialResolverRegistrations["reload-fixture"] == oldRegistration {
		t.Fatal("successful reload retained the old resolver registration")
	}

	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, newServer, false)}, nil
	}
	if _, err := manager.ReloadPlugin("reload-fixture"); err != nil {
		t.Fatalf("advertised-to-unadvertised ReloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "builtin-access" {
		t.Fatalf("unadvertised reload retained resolver: access key %q, %v", accessKey, err)
	}
	if manager.credentialResolverRegistrations["reload-fixture"] == nil {
		t.Fatal("unadvertised reload did not retain an owner tombstone")
	}

	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, newServer, true)}, nil
	}
	if _, err := manager.ReloadPlugin("reload-fixture"); err != nil {
		t.Fatalf("unadvertised-to-advertised ReloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "new-access" {
		t.Fatalf("advertised reload did not activate resolver: access key %q, %v", accessKey, err)
	}
}

func TestExternalPluginManagerCredentialResolverShutdownCleansRegistration(t *testing.T) {
	server := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "external-access",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, server, true)}, nil
	}
	if _, err := manager.LoadPlugin("shutdown-fixture"); err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	manager.Shutdown()
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "builtin-access" {
		t.Fatalf("shutdown left resolver registered: access key %q, %v", accessKey, err)
	}
}

func TestExternalPluginManagersShareResolverOwnerByCanonicalDirectoryAndName(t *testing.T) {
	pluginsDir := t.TempDir()
	startupServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "startup-access",
	}
	adminServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "admin-access",
	}
	differentServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "different-access",
	}

	startupManager := NewExternalPluginManager(pluginsDir, nil)
	startupManager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, startupServer, true)}, nil
	}
	t.Cleanup(startupManager.Shutdown)
	adminManager := NewExternalPluginManager(pluginsDir+string(filepath.Separator)+".", nil)
	adminManager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, adminServer, true)}, nil
	}
	t.Cleanup(adminManager.Shutdown)
	differentManager := NewExternalPluginManager(pluginsDir, nil)
	differentManager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, differentServer, true)}, nil
	}
	t.Cleanup(differentManager.Shutdown)

	if _, err := startupManager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("startup LoadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "startup-access" {
		t.Fatalf("startup resolver = %q, %v", accessKey, err)
	}
	if _, err := adminManager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("admin LoadPlugin same identity: %v", err)
	}

	const callers = 32
	results := make(chan string, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			accessKey, err := managerCloudAccountAccessKey(t)
			if err != nil {
				results <- "error:" + err.Error()
				return
			}
			results <- accessKey
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result != "admin-access" {
			t.Fatalf("same-owner concurrent resolution = %q", result)
		}
	}

	if err := adminManager.UnloadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("admin UnloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "startup-access" {
		t.Fatalf("startup resolver after admin unload = %q, %v", accessKey, err)
	}

	if _, err := differentManager.LoadPlugin("different-fixture"); err != nil {
		t.Fatalf("different identity LoadPlugin: %v", err)
	}
	if _, err := managerCloudAccountAccessKey(t); err == nil || !strings.Contains(err.Error(), "multiple external credential resolvers") {
		t.Fatalf("different owner resolution = %q, %v; want collision", "", err)
	}
	if err := differentManager.UnloadPlugin("different-fixture"); err != nil {
		t.Fatalf("different identity UnloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "startup-access" {
		t.Fatalf("startup resolver after different owner unload = %q, %v", accessKey, err)
	}
}

func TestExternalPluginManagerCredentialResolverOwnerIsCanonicalAndUnambiguous(t *testing.T) {
	baseDir := t.TempDir()
	canonicalManager := NewExternalPluginManager(baseDir, nil)
	equivalentManager := NewExternalPluginManager(filepath.Join(baseDir, "."), nil)
	canonicalOwner, err := canonicalManager.credentialResolverOwner("resolver-fixture")
	if err != nil {
		t.Fatalf("canonical owner: %v", err)
	}
	equivalentOwner, err := equivalentManager.credentialResolverOwner("resolver-fixture")
	if err != nil {
		t.Fatalf("equivalent owner: %v", err)
	}
	if canonicalOwner != equivalentOwner {
		t.Fatalf("equivalent plugin paths produced different owners: %q != %q", canonicalOwner, equivalentOwner)
	}

	leftManager := NewExternalPluginManager(filepath.Join(baseDir, "a"), nil)
	rightManager := NewExternalPluginManager(filepath.Join(baseDir, "a", "b"), nil)
	leftOwner, err := leftManager.credentialResolverOwner("b/c")
	if err != nil {
		t.Fatalf("left owner: %v", err)
	}
	rightOwner, err := rightManager.credentialResolverOwner("c")
	if err != nil {
		t.Fatalf("right owner: %v", err)
	}
	if leftOwner == rightOwner {
		t.Fatalf("owner encoding is ambiguous: %q", leftOwner)
	}
}

func TestExternalPluginManagerStagesCandidateResolversUntilActivation(t *testing.T) {
	pluginsDir := t.TempDir()
	liveServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "live-access",
	}
	candidateServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "candidate-access",
	}
	liveManager := NewExternalPluginManager(pluginsDir, nil)
	liveManager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, liveServer, true)}, nil
	}
	t.Cleanup(liveManager.Shutdown)
	if _, err := liveManager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("live LoadPlugin: %v", err)
	}

	candidateManager := NewExternalPluginManager(pluginsDir, nil)
	if err := candidateManager.StageCredentialResolvers(); err != nil {
		t.Fatalf("StageCredentialResolvers: %v", err)
	}
	candidateManager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, candidateServer, true)}, nil
	}
	t.Cleanup(candidateManager.Shutdown)
	if _, err := candidateManager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("candidate LoadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "live-access" {
		t.Fatalf("staged candidate displaced live resolver: %q, %v", accessKey, err)
	}

	app := module.NewMockApplication()
	if err := app.RegisterService(module.ExternalCredentialResolverRegistrationProviderServiceName, candidateManager); err != nil {
		t.Fatalf("register candidate manager: %v", err)
	}
	account := module.NewCloudAccount("candidate-account", map[string]any{
		"provider":    "aws",
		"credentials": map[string]any{"type": "static"},
	})
	if err := account.Init(app); err != nil {
		t.Fatalf("candidate cloud.account Init: %v", err)
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil || credentials.AccessKey != "candidate-access" {
		t.Fatalf("candidate scoped credentials = %+v, %v", credentials, err)
	}

	if err := candidateManager.ActivateCredentialResolvers(); err != nil {
		t.Fatalf("ActivateCredentialResolvers: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "candidate-access" {
		t.Fatalf("activated candidate resolver = %q, %v", accessKey, err)
	}
	candidateManager.Shutdown()
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "live-access" {
		t.Fatalf("live resolver after candidate cleanup = %q, %v", accessKey, err)
	}
}

func TestExternalPluginManagerReloadDrainsInFlightResolverBeforeKillingOldClient(t *testing.T) {
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseOld:
		default:
			close(releaseOld)
		}
	})
	oldServer := &managerCredentialResolverServer{
		declarations:   []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:      "old-access",
		resolveStarted: oldStarted,
		resolveRelease: releaseOld,
	}
	newServer := &managerCredentialResolverServer{
		declarations: []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}},
		accessKey:    "new-access",
	}
	manager := NewExternalPluginManager(t.TempDir(), nil)
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, oldServer, true)}, nil
	}
	t.Cleanup(manager.Shutdown)
	if _, err := manager.LoadPlugin("resolver-fixture"); err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	oldResult := make(chan string, 1)
	go func() {
		credentials, resolveErr := module.ResolveExternalCloudCredentials(context.Background(), "aws", "static", map[string]any{})
		if resolveErr != nil {
			oldResult <- "error:" + resolveErr.Error()
			return
		}
		oldResult <- credentials.AccessKey
	}()
	<-oldStarted

	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}, adapter: newManagerCredentialResolverAdapter(t, newServer, true)}, nil
	}
	reloadDone := make(chan error, 1)
	go func() {
		_, reloadErr := manager.ReloadPlugin("resolver-fixture")
		reloadDone <- reloadErr
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		credentials, resolveErr := module.ResolveExternalCloudCredentials(ctx, "aws", "static", map[string]any{})
		cancel()
		if resolveErr == nil && credentials.AccessKey == "new-access" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new resolver did not become selectable during old drain: %+v, %v", credentials, resolveErr)
		}
	}
	select {
	case err := <-reloadDone:
		t.Fatalf("ReloadPlugin returned before old resolution drained: %v", err)
	default:
	}
	close(releaseOld)
	if result := <-oldResult; result != "old-access" {
		t.Fatalf("in-flight old resolution = %q", result)
	}
	if err := <-reloadDone; err != nil {
		t.Fatalf("ReloadPlugin: %v", err)
	}
	if accessKey, err := managerCloudAccountAccessKey(t); err != nil || accessKey != "new-access" {
		t.Fatalf("post-reload resolver = %q, %v", accessKey, err)
	}
}
