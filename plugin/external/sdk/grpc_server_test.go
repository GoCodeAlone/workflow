package sdk

import (
	"context"
	"errors"
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// --- minimal test providers ---

// mustMapToStruct is a test helper wrapping mapToStruct; it fails the test if
// the map contains a structpb-incompatible value (e.g. chan, func).
func mustMapToStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := mapToStruct(m)
	if err != nil {
		t.Fatalf("mustMapToStruct: %v", err)
	}
	return s
}

type minimalProvider struct{}

func (p *minimalProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Author:  "tester",
	}
}

// assetProvider embeds AssetProvider in a PluginProvider.
type assetProvider struct {
	minimalProvider
	assets map[string][]byte
}

func (p *assetProvider) GetAsset(path string) ([]byte, string, error) {
	data, ok := p.assets[path]
	if !ok {
		return nil, "", errors.New("asset not found: " + path)
	}
	ct := detectContentType(path)
	return data, ct, nil
}

// sampleProvider returns manifest with ConfigMutable and SampleCategory set.
type sampleProvider struct{}

func (p *sampleProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:           "sample-plugin",
		Version:        "1.0.0",
		Author:         "tester",
		ConfigMutable:  true,
		SampleCategory: "ecommerce",
	}
}

type contractProvider struct {
	minimalProvider
}

func (p *contractProvider) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{
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
}

type triggerOnlyProvider struct {
	minimalProvider
	created *recordingTrigger
}

func (p *triggerOnlyProvider) TriggerTypes() []string {
	return []string{"trigger.test"}
}

func (p *triggerOnlyProvider) CreateTrigger(typeName string, config map[string]any, cb TriggerCallback) (TriggerInstance, error) {
	if typeName != "trigger.test" {
		return nil, errors.New("unexpected trigger type: " + typeName)
	}
	p.created = &recordingTrigger{
		config: config,
		cb:     cb,
	}
	return p.created, nil
}

type recordingTrigger struct {
	config map[string]any
	cb     TriggerCallback
	starts int
	stops  int
}

func (t *recordingTrigger) Start(context.Context) error {
	t.starts++
	return nil
}

func (t *recordingTrigger) Stop(context.Context) error {
	t.stops++
	return nil
}

type recordingCallbackClient struct {
	req *pb.TriggerWorkflowRequest
}

func (c *recordingCallbackClient) TriggerWorkflow(_ context.Context, req *pb.TriggerWorkflowRequest, _ ...grpc.CallOption) (*pb.ErrorResponse, error) {
	c.req = req
	return &pb.ErrorResponse{}, nil
}

func (c *recordingCallbackClient) GetService(context.Context, *pb.GetServiceRequest, ...grpc.CallOption) (*pb.GetServiceResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *recordingCallbackClient) Log(context.Context, *pb.LogRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, errors.New("not implemented")
}

func (c *recordingCallbackClient) PublishMessage(context.Context, *pb.PublishMessageRequest, ...grpc.CallOption) (*pb.PublishMessageResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *recordingCallbackClient) Subscribe(context.Context, *pb.SubscribeRequest, ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *recordingCallbackClient) Unsubscribe(context.Context, *pb.UnsubscribeRequest, ...grpc.CallOption) (*pb.ErrorResponse, error) {
	return nil, errors.New("not implemented")
}

type moduleAndTriggerProvider struct {
	triggerOnlyProvider
	module        ModuleInstance
	moduleCreated int
}

func (p *moduleAndTriggerProvider) ModuleTypes() []string {
	return []string{"trigger.test"}
}

func (p *moduleAndTriggerProvider) CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error) {
	if typeName != "trigger.test" {
		return nil, errors.New("unexpected module type: " + typeName)
	}
	p.moduleCreated++
	return p.module, nil
}

type typedServiceProvider struct {
	minimalProvider
	module ModuleInstance
}

func (p *typedServiceProvider) ModuleTypes() []string {
	return []string{"typed.service"}
}

func (p *typedServiceProvider) CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error) {
	return p.module, nil
}

type typedServiceFactoryProvider struct {
	minimalProvider
	TypedModuleProvider
}

type triggerProviderForTest struct {
	minimalProvider
	lastType   string
	lastConfig map[string]any
	lastCB     TriggerCallback
	trigger    *triggerInstanceForTest
}

func (p *triggerProviderForTest) TriggerTypes() []string {
	return []string{"compute.completed"}
}

func (p *triggerProviderForTest) CreateTrigger(typeName string, config map[string]any, cb TriggerCallback) (TriggerInstance, error) {
	p.lastType = typeName
	p.lastConfig = config
	p.lastCB = cb
	p.trigger = &triggerInstanceForTest{}
	return p.trigger, nil
}

type triggerInstanceForTest struct {
	started bool
	stopped bool
}

func (t *triggerInstanceForTest) Start(context.Context) error {
	t.started = true
	return nil
}

func (t *triggerInstanceForTest) Stop(context.Context) error {
	t.stopped = true
	return nil
}

type typedServiceModule struct {
	lastMethod string
	lastInput  *anypb.Any
}

func (m *typedServiceModule) Init() error {
	return nil
}

func (m *typedServiceModule) Start(context.Context) error {
	return nil
}

func (m *typedServiceModule) Stop(context.Context) error {
	return nil
}

func (m *typedServiceModule) InvokeTypedMethod(method string, input *anypb.Any) (*anypb.Any, error) {
	m.lastMethod = method
	m.lastInput = input
	return anypb.New(wrapperspb.String("typed-output"))
}

func (m *typedServiceModule) InvokeMethod(method string, args map[string]any) (map[string]any, error) {
	m.lastMethod = method
	return map[string]any{"value": args["value"]}, nil
}

type contextServiceModule struct {
	typedServiceModule
	contextErr error
	called     bool
	err        error
}

func (m *contextServiceModule) InvokeMethodContext(ctx context.Context, method string, args map[string]any) (map[string]any, error) {
	m.called = true
	m.lastMethod = method
	m.contextErr = ctx.Err()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.err != nil {
		return nil, m.err
	}
	return map[string]any{"value": args["value"]}, nil
}

type typedServiceModuleWithoutTypedInvoker struct{}

func (typedServiceModuleWithoutTypedInvoker) Init() error {
	return nil
}

func (typedServiceModuleWithoutTypedInvoker) Start(context.Context) error {
	return nil
}

func (typedServiceModuleWithoutTypedInvoker) Stop(context.Context) error {
	return nil
}

// --- tests ---

func TestGetAsset_WithAssetProvider(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"index.html": []byte("<html>hello</html>"),
			"app.js":     []byte("console.log('hi')"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if string(resp.Content) != "<html>hello</html>" {
		t.Errorf("expected html content, got %q", resp.Content)
	}
	if resp.ContentType != "text/html" {
		t.Errorf("expected text/html content type, got %q", resp.ContentType)
	}
}

func TestTriggerProviderCreatesTriggerThroughLifecycle(t *testing.T) {
	provider := &triggerOnlyProvider{}
	srv := newGRPCServer(provider)
	callback := &recordingCallbackClient{}
	srv.SetCallbackClient(callback)

	types, err := srv.GetTriggerTypes(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetTriggerTypes returned rpc error: %v", err)
	}
	if len(types.Types) != 1 || types.Types[0] != "trigger.test" {
		t.Fatalf("expected trigger.test type, got %#v", types.Types)
	}

	createResp, err := srv.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type:   "trigger.test",
		Name:   "pipeline:test-trigger",
		Config: mustMapToStruct(t, map[string]any{"pool": "private"}),
	})
	if err != nil {
		t.Fatalf("CreateTrigger returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected CreateTrigger application error: %s", createResp.Error)
	}
	if createResp.HandleId == "" {
		t.Fatal("CreateTrigger returned empty HandleId")
	}
	if provider.created == nil {
		t.Fatal("expected CreateTrigger to be called")
	}
	if got := provider.created.config["pool"]; got != "private" {
		t.Fatalf("expected trigger config to be forwarded, got %#v", provider.created.config)
	}

	if resp, err := srv.InitModule(context.Background(), &pb.HandleRequest{HandleId: createResp.HandleId}); err != nil || resp.Error != "" {
		t.Fatalf("InitModule = (%v, %v), want no error", resp, err)
	}
	if resp, err := srv.StartModule(context.Background(), &pb.HandleRequest{HandleId: createResp.HandleId}); err != nil || resp.Error != "" {
		t.Fatalf("StartModule = (%v, %v), want no error", resp, err)
	}
	if provider.created.starts != 1 {
		t.Fatalf("expected one trigger start, got %d", provider.created.starts)
	}

	if err := provider.created.cb("completed", map[string]any{"task_id": "task-1"}); err != nil {
		t.Fatalf("trigger callback returned error: %v", err)
	}
	if callback.req == nil {
		t.Fatal("expected callback request")
	}
	if callback.req.TriggerType != "pipeline:test-trigger" || callback.req.Action != "completed" {
		t.Fatalf("unexpected callback request: %#v", callback.req)
	}
	if got := callback.req.Data.AsMap()["task_id"]; got != "task-1" {
		t.Fatalf("expected callback task_id, got %#v", callback.req.Data.AsMap())
	}

	if resp, err := srv.StopModule(context.Background(), &pb.HandleRequest{HandleId: createResp.HandleId}); err != nil || resp.Error != "" {
		t.Fatalf("StopModule = (%v, %v), want no error", resp, err)
	}
	if provider.created.stops != 1 {
		t.Fatalf("expected one trigger stop, got %d", provider.created.stops)
	}
}

func TestTriggerProviderCallbackFallsBackToTriggerType(t *testing.T) {
	provider := &triggerOnlyProvider{}
	srv := newGRPCServer(provider)
	callback := &recordingCallbackClient{}
	srv.SetCallbackClient(callback)

	createResp, err := srv.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type: "trigger.test",
	})
	if err != nil {
		t.Fatalf("CreateTrigger returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected CreateTrigger application error: %s", createResp.Error)
	}
	if err := provider.created.cb("completed", map[string]any{"task_id": "task-1"}); err != nil {
		t.Fatalf("trigger callback returned error: %v", err)
	}
	if callback.req == nil {
		t.Fatal("expected callback request")
	}
	if callback.req.TriggerType != "trigger.test" {
		t.Fatalf("expected callback fallback trigger type, got %#v", callback.req)
	}
}

func TestSetCallbackClientClosesExistingBrokerConnection(t *testing.T) {
	conn, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	srv := newGRPCServer(&triggerOnlyProvider{})
	srv.callbackConn = conn
	srv.SetCallbackClient(&recordingCallbackClient{})

	if srv.callbackConn != nil {
		t.Fatal("expected callbackConn to be cleared")
	}
	if got := conn.GetState(); got != connectivity.Shutdown {
		t.Fatalf("expected previous callback connection to be closed, state=%s", got)
	}
}

func TestCreateModulePrefersModuleProviderWhenTypeAlsoTrigger(t *testing.T) {
	module := &typedServiceModule{}
	provider := &moduleAndTriggerProvider{
		triggerOnlyProvider: triggerOnlyProvider{},
		module:              module,
	}
	srv := newGRPCServer(provider)

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "trigger.test",
		Name: "module-with-trigger-name",
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected CreateModule application error: %s", createResp.Error)
	}
	if provider.moduleCreated != 1 {
		t.Fatalf("expected module provider to handle CreateModule, got %d", provider.moduleCreated)
	}
	if provider.created != nil {
		t.Fatal("CreateModule should not route to TriggerProvider")
	}

	triggerResp, err := srv.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type: "trigger.test",
		Name: "trigger",
	})
	if err != nil {
		t.Fatalf("CreateTrigger returned rpc error: %v", err)
	}
	if triggerResp.Error != "" {
		t.Fatalf("unexpected CreateTrigger application error: %s", triggerResp.Error)
	}
	if provider.created == nil {
		t.Fatal("expected CreateTrigger to route to TriggerProvider")
	}
}

func TestGetAsset_JSMimeType(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"app.js": []byte("var x = 1;"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "app.js"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ContentType != "application/javascript" {
		t.Errorf("expected application/javascript, got %q", resp.ContentType)
	}
}

func TestGetAsset_AssetNotFound(t *testing.T) {
	provider := &assetProvider{assets: map[string][]byte{}}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "missing.txt"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error for missing asset, got empty error")
	}
}

func TestGetAsset_WithoutAssetProvider(t *testing.T) {
	srv := newGRPCServer(&minimalProvider{})

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error when AssetProvider not implemented")
	}
}

func TestGetManifest_NewFields(t *testing.T) {
	srv := newGRPCServer(&sampleProvider{})

	m, err := srv.GetManifest(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.ConfigMutable {
		t.Error("expected ConfigMutable=true")
	}
	if m.SampleCategory != "ecommerce" {
		t.Errorf("expected SampleCategory=ecommerce, got %q", m.SampleCategory)
	}
}

func TestGetContractRegistry_WithProvider(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterPluginServiceServer(server, newGRPCServer(&contractProvider{}))
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := pb.NewPluginServiceClient(conn)
	registry, err := client.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(registry.Contracts) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(registry.Contracts))
	}
	descriptor := registry.Contracts[0]
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

func TestInvokeService_WithTypedInvoker(t *testing.T) {
	module := &typedServiceModule{}
	srv := newGRPCServer(&typedServiceProvider{module: module})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "typed.service",
		Name: "typed",
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected create error: %s", createResp.Error)
	}
	input, err := anypb.New(wrapperspb.String("typed-input"))
	if err != nil {
		t.Fatalf("pack typed input: %v", err)
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId:   createResp.HandleId,
		Method:     "Echo",
		TypedInput: input,
	})
	if err != nil {
		t.Fatalf("InvokeService returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected invoke error: %s", resp.Error)
	}
	if module.lastMethod != "Echo" {
		t.Fatalf("expected method Echo, got %q", module.lastMethod)
	}
	if module.lastInput == nil {
		t.Fatal("expected typed input to reach module")
	}
	if resp.TypedOutput == nil {
		t.Fatal("expected typed output")
	}
	var output wrapperspb.StringValue
	if err := resp.TypedOutput.UnmarshalTo(&output); err != nil {
		t.Fatalf("unpack typed output: %v", err)
	}
	if output.Value != "typed-output" {
		t.Fatalf("expected typed output value, got %q", output.Value)
	}
}

func TestCreateTrigger_DispatchesTriggerProviderTypes(t *testing.T) {
	provider := &triggerProviderForTest{}
	srv := newGRPCServer(provider)

	createResp, err := srv.CreateTrigger(context.Background(), &pb.CreateTriggerRequest{
		Type:   "compute.completed",
		Name:   "compute.completed",
		Config: mustMapToStruct(t, map[string]any{"task_status": "succeeded"}),
	})
	if err != nil {
		t.Fatalf("CreateTrigger returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected create error: %s", createResp.Error)
	}
	if provider.lastType != "compute.completed" {
		t.Fatalf("CreateTrigger type = %q", provider.lastType)
	}
	if provider.lastConfig["task_status"] != "succeeded" {
		t.Fatalf("CreateTrigger config = %#v", provider.lastConfig)
	}

	if _, err := srv.StartModule(context.Background(), &pb.HandleRequest{HandleId: createResp.HandleId}); err != nil {
		t.Fatalf("StartModule: %v", err)
	}
	if provider.trigger == nil || !provider.trigger.started {
		t.Fatal("trigger instance was not started through module lifecycle")
	}
	if _, err := srv.StopModule(context.Background(), &pb.HandleRequest{HandleId: createResp.HandleId}); err != nil {
		t.Fatalf("StopModule: %v", err)
	}
	if !provider.trigger.stopped {
		t.Fatal("trigger instance was not stopped through module lifecycle")
	}
}

func TestInvokeService_WithTypedModuleFactoryForwardsTypedInvoker(t *testing.T) {
	module := &typedServiceModule{}
	srv := newGRPCServer(&typedServiceFactoryProvider{
		TypedModuleProvider: NewTypedModuleFactory(
			"typed.service",
			wrapperspb.String("configured"),
			func(name string, config *wrapperspb.StringValue) (ModuleInstance, error) {
				return module, nil
			},
		),
	})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type:        "typed.service",
		Name:        "typed",
		TypedConfig: mustPackGRPCTestMessage(t, wrapperspb.String("configured")),
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected create error: %s", createResp.Error)
	}
	input, err := anypb.New(wrapperspb.String("typed-input"))
	if err != nil {
		t.Fatalf("pack typed input: %v", err)
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId:   createResp.HandleId,
		Method:     "Echo",
		TypedInput: input,
	})
	if err != nil {
		t.Fatalf("InvokeService returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected invoke error: %s", resp.Error)
	}
	if module.lastMethod != "Echo" {
		t.Fatalf("expected method Echo, got %q", module.lastMethod)
	}
	if resp.TypedOutput == nil {
		t.Fatal("expected typed output")
	}
}

func TestInvokeService_WithTypedModuleFactoryForwardsLegacyInvoker(t *testing.T) {
	module := &typedServiceModule{}
	srv := newGRPCServer(&typedServiceFactoryProvider{
		TypedModuleProvider: NewTypedModuleFactory(
			"typed.service",
			wrapperspb.String("configured"),
			func(name string, config *wrapperspb.StringValue) (ModuleInstance, error) {
				return module, nil
			},
		),
	})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type:        "typed.service",
		Name:        "typed",
		TypedConfig: mustPackGRPCTestMessage(t, wrapperspb.String("configured")),
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected create error: %s", createResp.Error)
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId: createResp.HandleId,
		Method:   "Echo",
		Args: mustMapToStruct(t, map[string]any{
			"value": "legacy-input",
		}),
	})
	if err != nil {
		t.Fatalf("InvokeService returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected invoke error: %s", resp.Error)
	}
	if module.lastMethod != "Echo" {
		t.Fatalf("expected method Echo, got %q", module.lastMethod)
	}
	if got := resp.Result.AsMap()["value"]; got != "legacy-input" {
		t.Fatalf("expected legacy result value, got %#v", got)
	}
}

func TestInvokeService_ForwardsContextToLegacyInvoker(t *testing.T) {
	module := &contextServiceModule{}
	srv := newGRPCServer(&typedServiceProvider{module: module})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "typed.service",
		Name: "typed",
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := srv.InvokeService(ctx, &pb.InvokeServiceRequest{
		HandleId: createResp.HandleId,
		Method:   "Echo",
		Args: mustMapToStruct(t, map[string]any{
			"value": "legacy-input",
		}),
	})
	if err != nil {
		t.Fatalf("InvokeService returned rpc error: %v", err)
	}
	if !module.called {
		t.Fatal("context-aware invoker was not called")
	}
	if module.contextErr == nil {
		t.Fatal("expected canceled context to reach invoker")
	}
	if resp.Error == "" {
		t.Fatal("expected response error from canceled context")
	}
}

func TestInvokeService_PreservesStatusErrors(t *testing.T) {
	module := &contextServiceModule{err: status.Error(codes.Unimplemented, "provider does not implement ProviderMigrationRepairer")}
	srv := newGRPCServer(&typedServiceProvider{module: module})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "typed.service",
		Name: "typed",
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId: createResp.HandleId,
		Method:   "IaCProvider.RepairDirtyMigration",
		Args:     mustMapToStruct(t, map[string]any{}),
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("InvokeService error code = %v, want Unimplemented (resp=%+v, err=%v)", status.Code(err), resp, err)
	}
}

func TestInvokeService_WithTypedInputRequiresTypedInvoker(t *testing.T) {
	srv := newGRPCServer(&typedServiceProvider{module: &typedServiceModuleWithoutTypedInvoker{}})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "typed.service",
		Name: "typed",
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	input, err := anypb.New(wrapperspb.String("typed-input"))
	if err != nil {
		t.Fatalf("pack typed input: %v", err)
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId:   createResp.HandleId,
		Method:     "Echo",
		TypedInput: input,
	})
	if err != nil {
		t.Fatalf("InvokeService returned rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected missing TypedServiceInvoker error")
	}
}

// TestMapToStruct_SDK_PropagatesError verifies that the SDK's local mapToStruct
// surfaces structpb.NewStruct errors instead of silently dropping data
// (workflow#537 — mirrors the same test in plugin/external/convert_test.go).
func TestMapToStruct_SDK_PropagatesError(t *testing.T) {
	m := map[string]any{
		"ok":  "value",
		"bad": make(chan int), // chan is not structpb-representable
	}
	s, err := mapToStruct(m)
	if err == nil {
		t.Fatal("expected error from structpb.NewStruct on chan, got nil")
	}
	if s != nil {
		t.Errorf("expected nil struct on error, got %v", s)
	}
}

// TestInvokeService_PropagatesOutputEncodingError verifies that InvokeService
// surfaces a structpb-encoding failure in the Response.Error field instead of
// silently returning an empty result (workflow#537).
func TestInvokeService_PropagatesOutputEncodingError(t *testing.T) {
	// Module whose InvokeMethod returns a value that structpb cannot encode.
	badModule := &badOutputModule{}
	srv := newGRPCServer(&typedServiceProvider{module: badModule})

	createResp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type: "typed.service",
		Name: "svc",
	})
	if err != nil {
		t.Fatalf("CreateModule rpc error: %v", err)
	}
	if createResp.Error != "" {
		t.Fatalf("unexpected CreateModule application error: %s", createResp.Error)
	}
	if createResp.HandleId == "" {
		t.Fatal("CreateModule returned empty HandleId")
	}

	resp, err := srv.InvokeService(context.Background(), &pb.InvokeServiceRequest{
		HandleId: createResp.HandleId,
		Method:   "BadOutput",
	})
	if err != nil {
		t.Fatalf("InvokeService returned unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected encoding error in Response.Error, got empty string")
	}
}

// badOutputModule returns a map with a chan value which structpb cannot encode.
type badOutputModule struct{}

func (badOutputModule) Init() error                 { return nil }
func (badOutputModule) Start(context.Context) error { return nil }
func (badOutputModule) Stop(context.Context) error  { return nil }
func (badOutputModule) InvokeMethod(_ string, _ map[string]any) (map[string]any, error) {
	return map[string]any{"bad": make(chan int)}, nil
}

func mustPackGRPCTestMessage(t *testing.T, msg proto.Message) *anypb.Any {
	t.Helper()
	typed, err := anypb.New(msg)
	if err != nil {
		t.Fatalf("pack typed message: %v", err)
	}
	return typed
}

// detectContentType maps common extensions to MIME types.
func detectContentType(path string) string {
	switch {
	case len(path) > 5 && path[len(path)-5:] == ".html":
		return "text/html"
	case len(path) > 4 && path[len(path)-4:] == ".css":
		return "text/css"
	case len(path) > 3 && path[len(path)-3:] == ".js":
		return "application/javascript"
	case len(path) > 4 && path[len(path)-4:] == ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}
