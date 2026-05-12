package external

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newTestAdapter builds an ExternalPluginAdapter with a populated manifest
// and optional config fragment without a real gRPC connection.
func newTestAdapter(manifest *pb.Manifest, configFragment []byte) *ExternalPluginAdapter {
	return &ExternalPluginAdapter{
		name:           manifest.Name,
		manifest:       manifest,
		configFragment: configFragment,
	}
}

type adapterTestPluginServiceClient struct {
	stubPluginServiceClient
	manifest          *pb.Manifest
	manifestResp      *pb.Manifest // alternative to manifest; takes precedence when set
	manifestErr       error        // when non-nil, GetManifest returns (nil, err)
	registry          *pb.ContractRegistry
	registryErr       error
	moduleTypes       []string
	stepTypes         []string
	triggerTypes      []string
	lastCreateModReq  *pb.CreateModuleRequest
	lastCreateStepReq *pb.CreateStepRequest
}

func (c *adapterTestPluginServiceClient) GetManifest(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.Manifest, error) {
	if c.manifestErr != nil {
		return nil, c.manifestErr
	}
	if c.manifestResp != nil {
		return c.manifestResp, nil
	}
	return c.manifest, nil
}

func (c *adapterTestPluginServiceClient) GetContractRegistry(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.ContractRegistry, error) {
	if c.registryErr != nil {
		return nil, c.registryErr
	}
	return c.registry, nil
}

func (c *adapterTestPluginServiceClient) GetStepTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{Types: c.stepTypes}, nil
}

func (c *adapterTestPluginServiceClient) GetTriggerTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{Types: c.triggerTypes}, nil
}

func (c *adapterTestPluginServiceClient) GetModuleTypes(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.TypeList, error) {
	return &pb.TypeList{Types: c.moduleTypes}, nil
}

func (c *adapterTestPluginServiceClient) CreateModule(_ context.Context, req *pb.CreateModuleRequest, _ ...grpc.CallOption) (*pb.HandleResponse, error) {
	c.lastCreateModReq = req
	return &pb.HandleResponse{HandleId: "module-handle"}, nil
}

func (c *adapterTestPluginServiceClient) CreateStep(_ context.Context, req *pb.CreateStepRequest, _ ...grpc.CallOption) (*pb.HandleResponse, error) {
	c.lastCreateStepReq = req
	return &pb.HandleResponse{HandleId: "step-handle"}, nil
}

func (c *adapterTestPluginServiceClient) InitModule(_ context.Context, _ *pb.HandleRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return &pb.ErrorResponse{}, nil
}

func TestIsSamplePlugin_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "my-sample",
		SampleCategory: "ecommerce",
	}, nil)
	if !a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=true when SampleCategory is set")
	}
}

func TestIsSamplePlugin_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "regular-plugin"}, nil)
	if a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=false when SampleCategory is empty")
	}
}

func TestIsConfigMutable_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:          "mutable-plugin",
		ConfigMutable: true,
	}, nil)
	if !a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=true")
	}
}

func TestIsConfigMutable_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "immutable-plugin"}, nil)
	if a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=false when not set")
	}
}

func TestSampleCategory(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "cat-plugin",
		SampleCategory: "analytics",
	}, nil)
	if a.SampleCategory() != "analytics" {
		t.Errorf("expected SampleCategory=analytics, got %q", a.SampleCategory())
	}
}

func TestConfigFragmentBytes(t *testing.T) {
	frag := []byte("modules:\n  - name: foo\n")
	a := newTestAdapter(&pb.Manifest{Name: "frag-plugin"}, frag)
	if string(a.ConfigFragmentBytes()) != string(frag) {
		t.Errorf("expected config fragment %q, got %q", frag, a.ConfigFragmentBytes())
	}
}

func TestConfigFragmentBytes_Nil(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "empty-plugin"}, nil)
	if a.ConfigFragmentBytes() != nil {
		t.Error("expected nil config fragment")
	}
}

func TestContractRegistry(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "test.echo",
				ConfigMessage: "workflow.plugins.test.v1.EchoConfig",
				InputMessage:  "workflow.plugins.test.v1.EchoInput",
				OutputMessage: "workflow.plugins.test.v1.EchoOutput",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest: &pb.Manifest{Name: "contract-plugin"},
		registry: registry,
	}}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	got := a.ContractRegistry()
	if got == nil {
		t.Fatal("expected contract registry")
	}
	if len(got.Contracts) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(got.Contracts))
	}
	descriptor := got.Contracts[0]
	if descriptor.Kind != pb.ContractKind_CONTRACT_KIND_STEP {
		t.Errorf("expected step contract kind, got %v", descriptor.Kind)
	}
	if descriptor.StepType != "test.echo" {
		t.Errorf("expected step type test.echo, got %q", descriptor.StepType)
	}
	if descriptor.ConfigMessage != "workflow.plugins.test.v1.EchoConfig" {
		t.Errorf("expected config message, got %q", descriptor.ConfigMessage)
	}
	if descriptor.InputMessage != "workflow.plugins.test.v1.EchoInput" {
		t.Errorf("expected input message, got %q", descriptor.InputMessage)
	}
	if descriptor.OutputMessage != "workflow.plugins.test.v1.EchoOutput" {
		t.Errorf("expected output message, got %q", descriptor.OutputMessage)
	}
	if descriptor.Mode != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
		t.Errorf("expected strict typed mode, got %v", descriptor.Mode)
	}
}

func TestContractRegistry_FetchErrorIsRecordedWithoutFailingAdapter(t *testing.T) {
	errBoom := errors.New("connection reset")
	a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest:    &pb.Manifest{Name: "legacy-plugin"},
		registryErr: errBoom,
	}}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter should not fail on optional registry fetch: %v", err)
	}
	if a.ContractRegistry() != nil {
		t.Fatal("expected nil contract registry when fetch fails")
	}
	if !errors.Is(a.ContractRegistryError(), errBoom) {
		t.Fatalf("expected recorded registry error, got %v", a.ContractRegistryError())
	}
}

func TestContractRegistry_UnimplementedUsesEmptyRegistry(t *testing.T) {
	a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifest:    &pb.Manifest{Name: "legacy-plugin"},
		registryErr: status.Error(codes.Unimplemented, "method GetContractRegistry not implemented"),
	}}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter should not fail on unimplemented registry: %v", err)
	}
	if a.ContractRegistry() == nil {
		t.Fatal("expected empty registry for unimplemented registry RPC")
	}
	if len(a.ContractRegistry().Contracts) != 0 {
		t.Fatalf("expected no contracts for legacy plugin, got %d", len(a.ContractRegistry().Contracts))
	}
	if a.ContractRegistryError() != nil {
		t.Fatalf("expected no recorded error for unimplemented registry RPC, got %v", a.ContractRegistryError())
	}
}

func TestNewExternalPluginAdapterConfiguresCallbackBroker(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:     &pb.Manifest{Name: "callback-plugin"},
		registry:     &pb.ContractRegistry{},
		triggerTypes: []string{"trigger.test"},
	}
	_, err := NewExternalPluginAdapter("callback-plugin", &PluginClient{
		client:           client,
		callbackBrokerID: 42,
	}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if client.configureCallbackReq == nil {
		t.Fatal("expected ConfigureCallback request")
	}
	if client.configureCallbackReq.BrokerId != 42 {
		t.Fatalf("expected broker id 42, got %d", client.configureCallbackReq.BrokerId)
	}
}

func TestNewExternalPluginAdapterSkipsCallbackForLegacyPluginWithoutTriggers(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest: &pb.Manifest{Name: "legacy-plugin"},
		registry: &pb.ContractRegistry{},
	}
	_, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{
		client:           client,
		callbackBrokerID: 42,
	}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if client.configureCallbackReq != nil {
		t.Fatal("did not expect ConfigureCallback request for plugin without triggers")
	}
}

func TestNewExternalPluginAdapterDisablesTriggersWhenCallbackUnsupported(t *testing.T) {
	client := &unimplementedConfigureCallbackClient{
		adapterTestPluginServiceClient: adapterTestPluginServiceClient{
			manifest:     &pb.Manifest{Name: "legacy-trigger-plugin"},
			registry:     &pb.ContractRegistry{},
			triggerTypes: []string{"trigger.test"},
		},
	}
	adapter, err := NewExternalPluginAdapter("legacy-trigger-plugin", &PluginClient{
		client:           client,
		callbackBrokerID: 42,
	}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter should preserve module/step compatibility: %v", err)
	}
	if factories := adapter.TriggerFactories(); factories != nil {
		t.Fatalf("expected trigger factories disabled when callback setup is unsupported, got %#v", factories)
	}
}

func TestNewExternalPluginAdapterDisablesTriggersWithoutCallbackBroker(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:     &pb.Manifest{Name: "trigger-plugin"},
		registry:     &pb.ContractRegistry{},
		triggerTypes: []string{"trigger.test"},
	}
	adapter, err := NewExternalPluginAdapter("trigger-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if factories := adapter.TriggerFactories(); factories != nil {
		t.Fatalf("expected trigger factories disabled without callback broker, got %#v", factories)
	}
}

func TestTriggerFactoryDefersCreateUntilConfigure(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:     &pb.Manifest{Name: "trigger-plugin"},
		registry:     &pb.ContractRegistry{},
		triggerTypes: []string{"trigger.test"},
	}
	adapter, err := NewExternalPluginAdapter("trigger-plugin", &PluginClient{
		client:           client,
		callbackBrokerID: 42,
	}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	factories := adapter.TriggerFactories()
	factory := factories["trigger.test"]
	if factory == nil {
		t.Fatal("expected trigger.test factory")
	}
	instance := factory()
	trigger, ok := instance.(*RemoteTrigger)
	if !ok {
		t.Fatalf("expected *RemoteTrigger, got %T", instance)
	}
	if client.lastCreateTriggerReq != nil {
		t.Fatal("trigger factory should not create remote trigger before Configure")
	}

	err = trigger.Configure(nil, map[string]any{"pool": "private"})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if client.lastCreateTriggerReq == nil {
		t.Fatal("expected CreateTrigger request during Configure")
	}
	if client.lastCreateTriggerReq.Config.AsMap()["pool"] != "private" {
		t.Fatalf("expected trigger config to be forwarded, got %#v", client.lastCreateTriggerReq.Config.AsMap())
	}
}

// errorOnCreateModuleClient overrides CreateModule to return a plugin-reported
// error in the response Error field (not as a gRPC error).
type errorOnCreateModuleClient struct {
	adapterTestPluginServiceClient
	createModuleError string
}

func (c *errorOnCreateModuleClient) CreateModule(_ context.Context, req *pb.CreateModuleRequest, _ ...grpc.CallOption) (*pb.HandleResponse, error) {
	c.lastCreateModReq = req
	return &pb.HandleResponse{Error: c.createModuleError}, nil
}

type unimplementedConfigureCallbackClient struct {
	adapterTestPluginServiceClient
}

func (c *unimplementedConfigureCallbackClient) ConfigureCallback(context.Context, *pb.ConfigureCallbackRequest, ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method ConfigureCallback not implemented")
}

// TestModuleFactoriesPropagatesPluginError is a regression gate for the class
// invariant: when CreateModule returns a non-empty Error field, ModuleFactories
// must return an *errorModule wrapping the plugin's message — not bare nil.
// Previously the condition `if createErr != nil || createResp.Error != ""` fell
// through to `return nil`, silently discarding the plugin diagnostic.
func TestModuleFactoriesPropagatesPluginError(t *testing.T) {
	const pluginErrMsg = "digitalocean: missing required config key 'token'"
	client := &errorOnCreateModuleClient{
		adapterTestPluginServiceClient: adapterTestPluginServiceClient{
			manifest:    &pb.Manifest{Name: "test-plugin"},
			moduleTypes: []string{"iac.provider"},
		},
		createModuleError: pluginErrMsg,
	}
	a, err := NewExternalPluginAdapter("test-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	factories := a.ModuleFactories()
	factory, ok := factories["iac.provider"]
	if !ok {
		t.Fatal("expected iac.provider factory to be registered")
	}

	mod := factory("test-provider", map[string]any{})
	if mod == nil {
		t.Fatal("expected *errorModule, got nil — plugin error was swallowed")
	}
	errMod, ok := mod.(*errorModule)
	if !ok {
		t.Fatalf("expected *errorModule, got %T", mod)
	}
	if errMod.err == nil {
		t.Fatal("errorModule has nil err")
	}
	if !strings.Contains(errMod.err.Error(), pluginErrMsg) {
		t.Errorf("expected plugin error message %q in propagated error, got: %v", pluginErrMsg, errMod.err)
	}
}

func TestExternalPluginAdapter_ServiceContractsAttachByModuleType(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_SERVICE,
				ModuleType:    "security.scanner",
				ServiceName:   "security.Scanner",
				Method:        "ScanSAST",
				InputMessage:  "workflow.plugin.v1.Manifest",
				OutputMessage: "workflow.plugin.v1.Manifest",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	a := newExternalPluginAdapterWithContractRegistry(&pb.Manifest{Name: "contract-plugin"}, registry)

	contracts := a.contracts.servicesFor("security.scanner")
	contract := contracts["ScanSAST"]
	if contract == nil {
		t.Fatal("expected service contract to attach to module type")
	}
	if contract.ServiceName != "security.Scanner" {
		t.Fatalf("expected original service name to be preserved, got %q", contract.ServiceName)
	}
}

func TestExternalPluginAdapter_ServiceContractsDoNotAttachEmptyServiceNameAcrossModules(t *testing.T) {
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:         pb.ContractKind_CONTRACT_KIND_SERVICE,
				ModuleType:   "payments.processor",
				Method:       "Authorize",
				InputMessage: "workflow.plugin.v1.Manifest",
				Mode:         pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		},
	}
	a := newExternalPluginAdapterWithContractRegistry(&pb.Manifest{Name: "contract-plugin"}, registry)

	contracts := a.contracts.servicesFor("security.scanner")
	if contract := contracts["Authorize"]; contract != nil {
		t.Fatalf("expected unrelated empty-service descriptor not to attach, got %#v", contract)
	}
}

func TestExternalPluginAdapter_ContractModuleFactoryPropagatesTypedConfigErrors(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:    &pb.Manifest{Name: "contract-plugin"},
		moduleTypes: []string{"test.strict_module"},
		registry: &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_MODULE,
				ModuleType:    "test.strict_module",
				ConfigMessage: "workflow.plugin.v1.DoesNotExist",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		}},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	factory := a.ModuleFactories()["test.strict_module"]
	if factory == nil {
		t.Fatal("expected strict module factory")
	}
	module := factory("strict-module", map[string]any{"name": "legacy-only"})
	if module == nil {
		t.Fatal("expected non-nil module that preserves strict config error")
	}
	if client.lastCreateModReq != nil {
		t.Fatal("expected strict failure before CreateModule RPC")
	}
	initErr := module.Init(nil)
	if initErr == nil {
		t.Fatal("expected Init to return strict config error")
	}
	if !strings.Contains(initErr.Error(), "STRICT_PROTO") {
		t.Fatalf("expected strict failure to mention STRICT_PROTO, got %v", initErr)
	}
}

func TestExternalPluginAdapter_ContractStepFactorySendsTypedConfig(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:  &pb.Manifest{Name: "contract-plugin"},
		stepTypes: []string{"test.strict"},
		registry: &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "test.strict",
				ConfigMessage: "workflow.plugin.v1.Manifest",
				InputMessage:  "workflow.plugin.v1.Manifest",
				OutputMessage: "workflow.plugin.v1.Manifest",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		}},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	factory := a.StepFactories()["test.strict"]
	if factory == nil {
		t.Fatal("expected strict step factory")
	}
	step, err := factory("strict-step", map[string]any{
		"name":    "typed-config",
		"version": "v1",
	}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}
	if step == nil {
		t.Fatal("expected remote step")
	}
	if client.lastCreateStepReq == nil {
		t.Fatal("expected CreateStep request")
	}
	if client.lastCreateStepReq.Config != nil {
		t.Fatalf("expected strict step creation to omit legacy Config, got %v", client.lastCreateStepReq.Config)
	}
	assertAnyTypeForTest(t, client.lastCreateStepReq.TypedConfig, "workflow.plugin.v1.Manifest")
}

func TestExternalPluginAdapter_ContractStepFactoryProtoWithLegacySendsBothConfigForms(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:  &pb.Manifest{Name: "contract-plugin"},
		stepTypes: []string{"test.compat"},
		registry: &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "test.compat",
				ConfigMessage: "workflow.plugin.v1.Manifest",
				InputMessage:  "workflow.plugin.v1.Manifest",
				OutputMessage: "workflow.plugin.v1.Manifest",
				Mode:          pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT,
			},
		}},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	_, err = a.StepFactories()["test.compat"]("compat-step", map[string]any{
		"name":    "typed-config",
		"version": "v1",
	}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}
	if client.lastCreateStepReq == nil {
		t.Fatal("expected CreateStep request")
	}
	if client.lastCreateStepReq.Config == nil {
		t.Fatal("expected compatibility mode to keep legacy Config")
	}
	assertAnyTypeForTest(t, client.lastCreateStepReq.TypedConfig, "workflow.plugin.v1.Manifest")
}

func TestExternalPluginAdapter_ContractStepFactoryUsesPluginOwnedDescriptors(t *testing.T) {
	const configMessage = "workflow.plugins.test.v1.DynamicConfig"
	client := &adapterTestPluginServiceClient{
		manifest:  &pb.Manifest{Name: "contract-plugin"},
		stepTypes: []string{"test.strict"},
		registry: &pb.ContractRegistry{
			FileDescriptorSet: dynamicContractFileDescriptorSet(),
			Contracts: []*pb.ContractDescriptor{
				{
					Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
					StepType:      "test.strict",
					ConfigMessage: configMessage,
					InputMessage:  "workflow.plugins.test.v1.DynamicInput",
					OutputMessage: "workflow.plugins.test.v1.DynamicOutput",
					Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
				},
			},
		},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	_, err = a.StepFactories()["test.strict"]("strict-step", map[string]any{
		"platform":   "github_actions",
		"output_dir": "/tmp/ci",
	}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}
	if client.lastCreateStepReq == nil || client.lastCreateStepReq.TypedConfig == nil {
		t.Fatal("expected typed config request")
	}
	if client.lastCreateStepReq.Config != nil {
		t.Fatalf("expected dynamic strict step creation to omit legacy Config, got %v", client.lastCreateStepReq.Config)
	}
	if got := client.lastCreateStepReq.TypedConfig.MessageName(); got != configMessage {
		t.Fatalf("expected Any message %s, got %s", configMessage, got)
	}
	msg, err := newMessageByName(configMessage, a.contractTypes)
	if err != nil {
		t.Fatalf("new dynamic message: %v", err)
	}
	if err := client.lastCreateStepReq.TypedConfig.UnmarshalTo(msg); err != nil {
		t.Fatalf("unmarshal dynamic typed config: %v", err)
	}
	platform := msg.ProtoReflect().Descriptor().Fields().ByName("platform")
	if got := msg.ProtoReflect().Get(platform).String(); got != "github_actions" {
		t.Fatalf("expected platform github_actions, got %q", got)
	}
}

func TestExternalPluginAdapter_MalformedDescriptorSetRecordsError(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest: &pb.Manifest{Name: "contract-plugin"},
		registry: &pb.ContractRegistry{
			FileDescriptorSet: malformedContractFileDescriptorSet(),
		},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if a.contractTypes != nil {
		t.Fatal("expected no dynamic type resolver for malformed descriptors")
	}
	if a.ContractRegistryError() == nil {
		t.Fatal("expected malformed descriptor parse error to be recorded")
	}
	if !strings.Contains(a.ContractRegistryError().Error(), "parse contract registry descriptors") {
		t.Fatalf("expected descriptor parse context, got %v", a.ContractRegistryError())
	}
}

func TestExternalPluginAdapter_RemoteTriggerDelaysCreateUntilConfigure(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:     &pb.Manifest{Name: "trigger-plugin"},
		triggerTypes: []string{"compute.completed"},
	}
	a, err := NewExternalPluginAdapter("trigger-plugin", &PluginClient{
		client:           client,
		callbackBrokerID: 42,
	}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	factory := a.TriggerFactories()["compute.completed"]
	if factory == nil {
		t.Fatal("missing trigger factory")
	}
	instance := factory()
	trigger, ok := instance.(*RemoteTrigger)
	if !ok {
		t.Fatalf("factory type = %T, want *RemoteTrigger", instance)
	}
	if client.lastCreateTriggerReq != nil {
		t.Fatal("trigger factory should not create remote handle before config is available")
	}
	if err := trigger.Configure(nil, map[string]any{"task_status": "succeeded"}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if client.lastCreateTriggerReq == nil {
		t.Fatal("Configure did not create remote trigger handle")
	}
	if client.lastCreateTriggerReq.Type != "compute.completed" {
		t.Fatalf("CreateTrigger type = %q", client.lastCreateTriggerReq.Type)
	}
	if got := client.lastCreateTriggerReq.Config.AsMap()["task_status"]; got != "succeeded" {
		t.Fatalf("trigger config did not reach CreateTrigger: %#v", client.lastCreateTriggerReq.Config.AsMap())
	}
}

func TestTypedAnyToMapNormalizesIntegerFields(t *testing.T) {
	const outputMessage = "workflow.plugins.test.v1.DynamicOutput"
	registry := &pb.ContractRegistry{
		FileDescriptorSet: dynamicContractFileDescriptorSet(),
	}
	a := newExternalPluginAdapterWithContractRegistry(&pb.Manifest{Name: "contract-plugin"}, registry)
	msg, err := newMessageByName(outputMessage, a.contractTypes)
	if err != nil {
		t.Fatalf("new dynamic message: %v", err)
	}
	fields := msg.ProtoReflect().Descriptor().Fields()
	msg.ProtoReflect().Set(fields.ByName("platform"), protoreflect.ValueOfString("github_actions"))
	msg.ProtoReflect().Set(fields.ByName("file_count"), protoreflect.ValueOfInt32(2))
	payload, err := anypb.New(msg)
	if err != nil {
		t.Fatalf("pack dynamic output: %v", err)
	}

	values, err := typedAnyToMap(payload, outputMessage, a.contractTypes)
	if err != nil {
		t.Fatalf("typedAnyToMap: %v", err)
	}
	if got, ok := values["file_count"].(int); !ok || got != 2 {
		t.Fatalf("expected file_count int(2), got %T(%v)", values["file_count"], values["file_count"])
	}
}

func TestRegisterFileMessagesReturnsDuplicateError(t *testing.T) {
	files, err := protodesc.NewFiles(dynamicContractFileDescriptorSet())
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}
	file, err := files.FindFileByPath("dynamic_contract.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}
	types := new(protoregistry.Types)
	if err := registerFileMessages(types, file.Messages()); err != nil {
		t.Fatalf("first registerFileMessages: %v", err)
	}
	if err := registerFileMessages(types, file.Messages()); err == nil {
		t.Fatal("expected duplicate message registration error")
	}
}

func TestExternalPluginAdapter_ContractStepFactoryFailsClosedWithoutCodec(t *testing.T) {
	client := &adapterTestPluginServiceClient{
		manifest:  &pb.Manifest{Name: "contract-plugin"},
		stepTypes: []string{"test.strict"},
		registry: &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "test.strict",
				ConfigMessage: "workflow.plugin.v1.DoesNotExist",
				InputMessage:  "workflow.plugin.v1.DoesNotExist",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
			},
		}},
	}
	a, err := NewExternalPluginAdapter("contract-plugin", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}

	_, err = a.StepFactories()["test.strict"]("strict-step", map[string]any{"name": "legacy-only"}, nil)
	if err == nil {
		t.Fatal("expected strict factory to fail without generated codec")
	}
	if !strings.Contains(err.Error(), "STRICT_PROTO") {
		t.Fatalf("expected strict failure to mention STRICT_PROTO, got %v", err)
	}
	if client.lastCreateStepReq != nil {
		t.Fatal("expected strict failure before CreateStep RPC")
	}
}

func dynamicContractFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	int32Type := descriptorpb.FieldDescriptorProto_TYPE_INT32
	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		{
			Name:    stringPtr("dynamic_contract.proto"),
			Package: stringPtr("workflow.plugins.test.v1"),
			Syntax:  stringPtr("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: stringPtr("DynamicConfig"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: stringPtr("platform"), JsonName: stringPtr("platform"), Number: int32Ptr(1), Label: &label, Type: &stringType},
						{Name: stringPtr("output_dir"), JsonName: stringPtr("outputDir"), Number: int32Ptr(2), Label: &label, Type: &stringType},
					},
				},
				{
					Name: stringPtr("DynamicInput"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: stringPtr("platform"), JsonName: stringPtr("platform"), Number: int32Ptr(1), Label: &label, Type: &stringType},
					},
				},
				{
					Name: stringPtr("DynamicOutput"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: stringPtr("platform"), JsonName: stringPtr("platform"), Number: int32Ptr(1), Label: &label, Type: &stringType},
						{Name: stringPtr("file_count"), JsonName: stringPtr("fileCount"), Number: int32Ptr(2), Label: &label, Type: &int32Type},
					},
				},
			},
		},
	}}
}

func malformedContractFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		{
			Name:       stringPtr("malformed_contract.proto"),
			Package:    stringPtr("workflow.plugins.test.v1"),
			Syntax:     stringPtr("proto3"),
			Dependency: []string{"missing_dependency.proto"},
		},
	}}
}

func stringPtr(v string) *string { return &v }
func int32Ptr(v int32) *int32    { return &v }

// unimplementedManifestClient simulates a strict-cutover IaC plugin whose
// PluginService bridge implements GetContractRegistry but leaves GetManifest
// unimplemented (workflow-plugin-digitalocean v1.0.0+ behavior).
type unimplementedManifestClient struct {
	adapterTestPluginServiceClient
}

func (c *unimplementedManifestClient) GetManifest(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.Manifest, error) {
	return nil, status.Error(codes.Unimplemented, "method GetManifest not implemented")
}

// TestNewExternalPluginAdapter_GetManifestUnimplemented_SynthesizesFromName
// asserts that NewExternalPluginAdapter tolerates GetManifest returning
// codes.Unimplemented and synthesizes a minimal manifest from the param name.
// Regression coverage for strict-cutover IaC plugins (DO v1.0.0+) whose
// iacPluginServiceBridge only wires GetContractRegistry.
func TestNewExternalPluginAdapter_GetManifestUnimplemented_SynthesizesFromName(t *testing.T) {
	client := &unimplementedManifestClient{
		adapterTestPluginServiceClient: adapterTestPluginServiceClient{
			registry: &pb.ContractRegistry{},
		},
	}
	a, err := NewExternalPluginAdapter("digitalocean", &PluginClient{client: client}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter must tolerate Unimplemented GetManifest: %v", err)
	}
	if a.Name() != "digitalocean" {
		t.Fatalf("expected synthesized manifest Name=digitalocean, got %q", a.Name())
	}
	if a.Version() != "" {
		t.Fatalf("expected empty synthesized manifest Version, got %q", a.Version())
	}
}

// TestNewExternalPluginAdapter_GetManifestNonUnimplementedError_Fails asserts
// that non-Unimplemented errors from GetManifest still surface — only
// Unimplemented is tolerated.
func TestNewExternalPluginAdapter_GetManifestNonUnimplementedError_Fails(t *testing.T) {
	client := &adapterTestPluginServiceClient{}
	// Override GetManifest to return Internal.
	failingClient := &failingManifestClient{adapterTestPluginServiceClient: *client}
	_, err := NewExternalPluginAdapter("broken-plugin", &PluginClient{client: failingClient}, nil)
	if err == nil {
		t.Fatal("expected error from non-Unimplemented GetManifest failure")
	}
	if !strings.Contains(err.Error(), "get manifest from plugin broken-plugin") {
		t.Fatalf("expected wrapped error mentioning plugin name, got: %v", err)
	}
}

type failingManifestClient struct {
	adapterTestPluginServiceClient
}

func (c *failingManifestClient) GetManifest(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*pb.Manifest, error) {
	return nil, status.Error(codes.Internal, "boom")
}

// TestNewExternalPluginAdapterDiskManifestFallback verifies that when the
// plugin's gRPC GetManifest RPC returns codes.Unimplemented (strict-cutover IaC
// plugins served via sdk.ServeIaCPlugin), the disk-loaded *plugin.PluginManifest
// is field-mapped into the adapter's cached *pb.Manifest so accessors like
// Version() / Description() return the canonical disk values rather than empty.
func TestNewExternalPluginAdapterDiskManifestFallback(t *testing.T) {
	disk := &plugin.PluginManifest{
		Name:           "iac-plugin",
		Version:        "1.0.11",
		Author:         "GoCodeAlone",
		Description:    "DigitalOcean IaC provider",
		ConfigMutable:  true,
		SampleCategory: "iac",
	}
	a, err := NewExternalPluginAdapter("iac-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
	}}, disk)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if got := a.Version(); got != "1.0.11" {
		t.Fatalf("Version() = %q, want 1.0.11 (disk fallback)", got)
	}
	if got := a.Description(); got != "DigitalOcean IaC provider" {
		t.Fatalf("Description() = %q, want disk value", got)
	}
}

// TestNewExternalPluginAdapterDiskManifestNilStillWorks verifies that when the
// plugin's gRPC GetManifest returns Unimplemented AND no disk manifest is
// provided (nil), the adapter still constructs successfully by synthesizing a
// minimal *pb.Manifest from the param name — preserving PR #627 tolerance.
func TestNewExternalPluginAdapterDiskManifestNilStillWorks(t *testing.T) {
	a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
		manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
	}}, nil)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter with nil disk: %v", err)
	}
	if got := a.Name(); got != "legacy-plugin" {
		t.Fatalf("Name() = %q, want legacy-plugin (constructor name fallback)", got)
	}
	if got := a.Version(); got != "" {
		t.Fatalf("Version() = %q, want empty (no disk, no gRPC)", got)
	}
}

// TestNewExternalPluginAdapterDiskOverlayWhenGRPCReturnsEmptyVersion exercises
// the empty-Version-but-no-error overlay path (R2-1): gRPC returns a valid
// pb.Manifest with empty Version (defensive case, e.g. a misconfigured plugin),
// and the disk-manifest overlay must populate the cached manifest so
// EngineManifest()/Validate() can succeed downstream.
func TestNewExternalPluginAdapterDiskOverlayWhenGRPCReturnsEmptyVersion(t *testing.T) {
	disk := &plugin.PluginManifest{
		Name: "x", Version: "1.0.11", Author: "GoCodeAlone", Description: "DO IaC",
	}
	a, err := NewExternalPluginAdapter("x", &PluginClient{client: &adapterTestPluginServiceClient{
		manifestResp: &pb.Manifest{Name: "x", Version: ""},
	}}, disk)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if got := a.Version(); got != "1.0.11" {
		t.Fatalf("Version() = %q, want 1.0.11 (disk overlay when gRPC Version empty)", got)
	}
	em := a.EngineManifest()
	if em.Author != "GoCodeAlone" {
		t.Fatalf("EngineManifest().Author = %q, want GoCodeAlone (disk overlay)", em.Author)
	}
}

// TestNewExternalPluginAdapterPrefersGRPCWhenVersionPresent (F10 regression)
// locks in the precedence rule: when both gRPC and disk manifests contain
// non-empty Version, gRPC WINS. Disk is fallback for missing-or-empty gRPC
// fields only — never an override.
func TestNewExternalPluginAdapterPrefersGRPCWhenVersionPresent(t *testing.T) {
	disk := &plugin.PluginManifest{
		Name: "x", Version: "9.9.9", Author: "disk", Description: "disk desc",
	}
	a, err := NewExternalPluginAdapter("x", &PluginClient{client: &adapterTestPluginServiceClient{
		manifestResp: &pb.Manifest{Name: "x", Version: "1.0.0", Author: "grpc", Description: "grpc desc"},
	}}, disk)
	if err != nil {
		t.Fatalf("NewExternalPluginAdapter: %v", err)
	}
	if got := a.Version(); got != "1.0.0" {
		t.Fatalf("Version() = %q, want 1.0.0 (gRPC wins over disk)", got)
	}
	if got := a.Description(); got != "grpc desc" {
		t.Fatalf("Description() = %q, want grpc desc (gRPC wins)", got)
	}
}
