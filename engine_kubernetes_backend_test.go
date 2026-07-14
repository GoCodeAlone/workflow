package workflow

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	platformplugin "github.com/GoCodeAlone/workflow/plugins/platform"
	"google.golang.org/grpc"
)

const testEngineKubernetesBackendRegistryServiceName = "workflow.internal.kubernetes-backend-registry"

type engineKubernetesBackendRegistryView interface {
	ResolveKubernetesBackend(string) (workflowmodule.KubernetesBackendBinding, string, bool)
}

type engineKubernetesBackendPlugin struct {
	plugin.BaseEnginePlugin
	clients         map[string]pb.ResourceDriverClient
	moduleFactories map[string]plugin.ModuleFactory
	configHooks     []plugin.ConfigTransformHook
}

func newEngineKubernetesBackendPlugin(name string, clients map[string]pb.ResourceDriverClient) *engineKubernetesBackendPlugin {
	declarations := make([]plugin.KubernetesBackendDecl, 0, len(clients))
	for backend := range clients {
		declarations = append(declarations, plugin.KubernetesBackendDecl{
			Name:         backend,
			ResourceType: "infra.test_cluster",
		})
	}
	return &engineKubernetesBackendPlugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        name,
				PluginVersion:     "1.0.0",
				PluginDescription: "test kubernetes backend plugin",
			},
			Manifest: plugin.PluginManifest{
				Name:               name,
				Version:            "1.0.0",
				Author:             "test",
				Description:        "test kubernetes backend plugin",
				KubernetesBackends: declarations,
			},
		},
		clients: clients,
	}
}

func (p *engineKubernetesBackendPlugin) KubernetesBackendClients() (map[string]pb.ResourceDriverClient, error) {
	return p.clients, nil
}

func (p *engineKubernetesBackendPlugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return p.moduleFactories
}

func (p *engineKubernetesBackendPlugin) ConfigTransformHooks() []plugin.ConfigTransformHook {
	return p.configHooks
}

type engineKubernetesResourceDriverClient struct {
	id               string
	readCalls        atomic.Int32
	readResourceType atomic.Value
}

func (*engineKubernetesResourceDriverClient) Create(context.Context, *pb.ResourceCreateRequest, ...grpc.CallOption) (*pb.ResourceCreateResponse, error) {
	return &pb.ResourceCreateResponse{}, nil
}
func (c *engineKubernetesResourceDriverClient) Read(_ context.Context, request *pb.ResourceReadRequest, _ ...grpc.CallOption) (*pb.ResourceReadResponse, error) {
	c.readResourceType.Store(request.GetResourceType())
	c.readCalls.Add(1)
	return &pb.ResourceReadResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) Update(context.Context, *pb.ResourceUpdateRequest, ...grpc.CallOption) (*pb.ResourceUpdateResponse, error) {
	return &pb.ResourceUpdateResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) Delete(context.Context, *pb.ResourceDeleteRequest, ...grpc.CallOption) (*pb.ResourceDeleteResponse, error) {
	return &pb.ResourceDeleteResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) Diff(context.Context, *pb.ResourceDiffRequest, ...grpc.CallOption) (*pb.ResourceDiffResponse, error) {
	return &pb.ResourceDiffResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) Scale(context.Context, *pb.ResourceScaleRequest, ...grpc.CallOption) (*pb.ResourceScaleResponse, error) {
	return &pb.ResourceScaleResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) HealthCheck(context.Context, *pb.ResourceHealthCheckRequest, ...grpc.CallOption) (*pb.ResourceHealthCheckResponse, error) {
	return &pb.ResourceHealthCheckResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) SensitiveKeys(context.Context, *pb.SensitiveKeysRequest, ...grpc.CallOption) (*pb.SensitiveKeysResponse, error) {
	return &pb.SensitiveKeysResponse{}, nil
}
func (*engineKubernetesResourceDriverClient) Troubleshoot(context.Context, *pb.TroubleshootRequest, ...grpc.CallOption) (*pb.TroubleshootResponse, error) {
	return &pb.TroubleshootResponse{}, nil
}

func emptyKubernetesBackendWorkflowConfig() *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
}

func engineKubernetesBackendRegistry(t *testing.T, app modular.Application) engineKubernetesBackendRegistryView {
	t.Helper()
	service, ok := app.SvcRegistry()[testEngineKubernetesBackendRegistryServiceName]
	if !ok {
		t.Fatalf("engine did not publish scoped kubernetes backend registry service %q", testEngineKubernetesBackendRegistryServiceName)
	}
	registry, ok := service.(engineKubernetesBackendRegistryView)
	if !ok {
		t.Fatalf("registry service = %T, want engineKubernetesBackendRegistryView", service)
	}
	return registry
}

func TestEngineKubernetesBackendsRejectCrossOwnerCollisionAtomically(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	ownerAClient := &engineKubernetesResourceDriverClient{id: "owner-a"}
	ownerBClient := &engineKubernetesResourceDriverClient{id: "owner-b"}
	if err := engine.LoadPlugin(newEngineKubernetesBackendPlugin("provider-a", map[string]pb.ResourceDriverClient{
		"shared":   ownerAClient,
		"stable-a": ownerAClient,
	})); err != nil {
		t.Fatalf("LoadPlugin(provider-a): %v", err)
	}

	providerB := newEngineKubernetesBackendPlugin("provider-b", map[string]pb.ResourceDriverClient{
		"new-b":  ownerBClient,
		"shared": ownerBClient,
	})
	providerB.Manifest.ModuleTypes = []string{"provider-b.marker"}
	providerB.moduleFactories = map[string]plugin.ModuleFactory{
		"provider-b.marker": func(name string, cfg map[string]any) modular.Module {
			return workflowmodule.NewServiceModule(name, cfg)
		},
	}
	providerB.configHooks = []plugin.ConfigTransformHook{{
		Name: "provider-b.marker-hook",
		Hook: func(*config.WorkflowConfig) error { return nil },
	}}
	err := engine.LoadPlugin(providerB)
	if err == nil || !strings.Contains(err.Error(), "provider-a") || !strings.Contains(err.Error(), "provider-b") {
		t.Fatalf("LoadPlugin(provider-b) error = %v, want cross-owner shared backend collision", err)
	}
	for _, loaded := range engine.PluginLoader().LoadedPlugins() {
		if loaded.EngineManifest().Name == "provider-b" {
			t.Fatal("rejected provider-b remained in PluginLoader().LoadedPlugins()")
		}
	}
	for _, loaded := range engine.LoadedPlugins() {
		if loaded.EngineManifest().Name == "provider-b" {
			t.Fatal("rejected provider-b remained in engine.LoadedPlugins()")
		}
	}
	for _, moduleType := range engine.RegisteredModuleTypes() {
		if moduleType == "provider-b.marker" {
			t.Fatal("rejected provider-b module factory mutated engine registry")
		}
	}
	for _, hook := range engine.PluginLoader().ConfigTransformHooks() {
		if hook.Name == "provider-b.marker-hook" {
			t.Fatal("rejected provider-b config hook mutated PluginLoader")
		}
	}
	if err := engine.BuildFromConfig(emptyKubernetesBackendWorkflowConfig()); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	registry := engineKubernetesBackendRegistry(t, app)
	got, owner, ok := registry.ResolveKubernetesBackend("shared")
	if !ok || owner != "provider-a" || got.Client != ownerAClient || got.ResourceType != "infra.test_cluster" {
		t.Fatalf("shared = (%v, %q, %v), want owner-a registration unchanged", got, owner, ok)
	}
	if got, owner, ok := registry.ResolveKubernetesBackend("new-b"); ok {
		t.Fatalf("new-b leaked from rejected batch: (%v, %q, %v)", got, owner, ok)
	}
	if got, owner, ok := registry.ResolveKubernetesBackend("stable-a"); !ok || owner != "provider-a" || got.Client != ownerAClient {
		t.Fatalf("stable-a changed after rejected batch: (%v, %q, %v)", got, owner, ok)
	}
}

func TestEngineKubernetesBackendsAreIsolatedAcrossCandidateEngines(t *testing.T) {
	appA := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	appB := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engineA := NewStdEngine(appA, &mock.Logger{})
	engineB := NewStdEngine(appB, &mock.Logger{})
	clientA := &engineKubernetesResourceDriverClient{id: "candidate-a"}
	clientB := &engineKubernetesResourceDriverClient{id: "candidate-b"}

	if err := engineA.LoadPlugin(newEngineKubernetesBackendPlugin("same-provider", map[string]pb.ResourceDriverClient{"shared-candidate": clientA})); err != nil {
		t.Fatalf("engineA.LoadPlugin: %v", err)
	}
	if err := engineB.LoadPlugin(newEngineKubernetesBackendPlugin("same-provider", map[string]pb.ResourceDriverClient{"shared-candidate": clientB})); err != nil {
		t.Fatalf("engineB.LoadPlugin: %v", err)
	}
	if err := engineA.BuildFromConfig(emptyKubernetesBackendWorkflowConfig()); err != nil {
		t.Fatalf("engineA.BuildFromConfig: %v", err)
	}
	if err := engineB.BuildFromConfig(emptyKubernetesBackendWorkflowConfig()); err != nil {
		t.Fatalf("engineB.BuildFromConfig: %v", err)
	}

	gotA, ownerA, okA := engineKubernetesBackendRegistry(t, appA).ResolveKubernetesBackend("shared-candidate")
	gotB, ownerB, okB := engineKubernetesBackendRegistry(t, appB).ResolveKubernetesBackend("shared-candidate")
	if !okA || ownerA != "same-provider" || gotA.Client != clientA || gotA.ResourceType != "infra.test_cluster" {
		t.Fatalf("engine A selection = (%v, %q, %v), want its own client", gotA, ownerA, okA)
	}
	if !okB || ownerB != "same-provider" || gotB.Client != clientB || gotB.ResourceType != "infra.test_cluster" {
		t.Fatalf("engine B selection = (%v, %q, %v), want its own client", gotB, ownerB, okB)
	}
}

func TestEngineKubernetesBackendRegistryIsAvailableDuringModuleInit(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	client := &engineKubernetesResourceDriverClient{id: "aks-provider"}

	if err := engine.LoadPlugin(platformplugin.New()); err != nil {
		t.Fatalf("LoadPlugin(platform): %v", err)
	}
	if err := engine.LoadPlugin(newEngineKubernetesBackendPlugin("azure-provider", map[string]pb.ResourceDriverClient{
		"aks": client,
	})); err != nil {
		t.Fatalf("LoadPlugin(azure-provider): %v", err)
	}
	cfg := emptyKubernetesBackendWorkflowConfig()
	cfg.Modules = []config.ModuleConfig{{
		Name:   "aks-cluster",
		Type:   "platform.kubernetes",
		Config: map[string]any{"type": "aks"},
	}}
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	var cluster *workflowmodule.PlatformKubernetes
	if err := app.GetService("aks-cluster", &cluster); err != nil {
		t.Fatalf("GetService(aks-cluster): %v", err)
	}
	if _, err := cluster.Plan(); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := client.readCalls.Load(); got != 1 {
		t.Fatalf("provider Read calls = %d, want 1; retained core AKS backend was selected", got)
	}
	if got, _ := client.readResourceType.Load().(string); got != "infra.test_cluster" {
		t.Fatalf("provider Read resource type = %q, want exact manifest declaration", got)
	}
}

func TestEngineKubernetesBackendBindingsRequireManifestRuntimeParity(t *testing.T) {
	tests := []struct {
		name   string
		plugin func() *engineKubernetesBackendPlugin
		want   string
	}{
		{
			name: "manifest declaration missing runtime client",
			plugin: func() *engineKubernetesBackendPlugin {
				p := newEngineKubernetesBackendPlugin("missing-client", map[string]pb.ResourceDriverClient{
					"served": &engineKubernetesResourceDriverClient{id: "served"},
				})
				p.Manifest.KubernetesBackends = append(p.Manifest.KubernetesBackends, plugin.KubernetesBackendDecl{
					Name: "missing", ResourceType: "infra.missing",
				})
				return p
			},
			want: "has no runtime client",
		},
		{
			name: "runtime client missing manifest declaration",
			plugin: func() *engineKubernetesBackendPlugin {
				p := newEngineKubernetesBackendPlugin("extra-client", map[string]pb.ResourceDriverClient{
					"served": &engineKubernetesResourceDriverClient{id: "served"},
					"extra":  &engineKubernetesResourceDriverClient{id: "extra"},
				})
				p.Manifest.KubernetesBackends = p.Manifest.KubernetesBackends[:1]
				p.Manifest.KubernetesBackends[0] = plugin.KubernetesBackendDecl{Name: "served", ResourceType: "infra.served"}
				return p
			},
			want: "is not declared in the manifest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
			engine := NewStdEngine(app, &mock.Logger{})
			err := engine.LoadPlugin(tt.plugin())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadPlugin error = %v, want %q", err, tt.want)
			}
			if len(engine.PluginLoader().LoadedPlugins()) != 0 || len(engine.LoadedPlugins()) != 0 {
				t.Fatalf("manifest/runtime mismatch mutated plugin state: loader=%d engine=%d",
					len(engine.PluginLoader().LoadedPlugins()), len(engine.LoadedPlugins()))
			}
		})
	}
}

var _ plugin.KubernetesBackendProvider = (*engineKubernetesBackendPlugin)(nil)
var _ pb.ResourceDriverClient = (*engineKubernetesResourceDriverClient)(nil)
