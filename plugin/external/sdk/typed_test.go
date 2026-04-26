package sdk

import (
	"context"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type typedStepProvider struct {
	minimalProvider
	TypedStepProvider
}

type countingTypedStepProvider struct {
	minimalProvider
	factory *TypedStepFactory[*wrapperspb.StringValue, *wrapperspb.StringValue, *wrapperspb.StringValue]
	created int
}

type mixedStepProvider struct {
	minimalProvider
	typed         *TypedStepFactory[*wrapperspb.StringValue, *wrapperspb.StringValue, *wrapperspb.StringValue]
	legacyCreated int
}

type countingTypedModuleProvider struct {
	minimalProvider
	factory *TypedModuleFactory[*wrapperspb.StringValue]
	created int
}

func TestTypedStepRejectsMismatchedInputType(t *testing.T) {
	srv := newGRPCServer(&typedStepProvider{TypedStepProvider: NewTypedStepFactory(
		"test.typed",
		wrapperspb.String("configured"),
		wrapperspb.String(""),
		func(ctx context.Context, req TypedStepRequest[*wrapperspb.StringValue, *wrapperspb.StringValue]) (*TypedStepResult[*wrapperspb.StringValue], error) {
			return &TypedStepResult[*wrapperspb.StringValue]{
				Output: wrapperspb.String(req.Config.Value + ":" + req.Input.Value),
			}, nil
		},
	)})

	handle := createTypedTestStep(t, srv, wrapperspb.String("configured"))
	mismatchedInput, err := anypb.New(wrapperspb.Int64(42))
	if err != nil {
		t.Fatalf("pack mismatched input: %v", err)
	}

	resp, err := srv.ExecuteStep(context.Background(), &pb.ExecuteStepRequest{
		HandleId:    handle,
		TypedInput:  mismatchedInput,
		TypedConfig: mustPackTypedTestMessage(t, wrapperspb.String("configured")),
	})
	if err != nil {
		t.Fatalf("ExecuteStep returned rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected type mismatch error, got empty error")
	}
	if !strings.Contains(resp.Error, "typed input") {
		t.Fatalf("expected typed input error, got %q", resp.Error)
	}
	if !strings.Contains(resp.Error, "google.protobuf.StringValue") {
		t.Fatalf("expected expected message name in error, got %q", resp.Error)
	}
}

func TestTypedStepExecutesWithCorrectInputType(t *testing.T) {
	srv := newGRPCServer(&typedStepProvider{TypedStepProvider: NewTypedStepFactory(
		"test.typed",
		wrapperspb.String("configured"),
		wrapperspb.String(""),
		func(ctx context.Context, req TypedStepRequest[*wrapperspb.StringValue, *wrapperspb.StringValue]) (*TypedStepResult[*wrapperspb.StringValue], error) {
			return &TypedStepResult[*wrapperspb.StringValue]{
				Output: wrapperspb.String(req.Config.Value + ":" + req.Input.Value),
			}, nil
		},
	)})

	handle := createTypedTestStep(t, srv, wrapperspb.String("create-config"))

	resp, err := srv.ExecuteStep(context.Background(), &pb.ExecuteStepRequest{
		HandleId:   handle,
		TypedInput: mustPackTypedTestMessage(t, wrapperspb.String("run-input")),
	})
	if err != nil {
		t.Fatalf("ExecuteStep returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if resp.TypedOutput == nil {
		t.Fatal("expected typed output, got nil")
	}

	var output wrapperspb.StringValue
	if err := resp.TypedOutput.UnmarshalTo(&output); err != nil {
		t.Fatalf("unpack typed output: %v", err)
	}
	if output.Value != "create-config:run-input" {
		t.Fatalf("expected typed output %q, got %q", "create-config:run-input", output.Value)
	}
}

func TestTypedStepRejectsNilHandlerWithoutPanic(t *testing.T) {
	step := NewTypedStepInstance[*wrapperspb.StringValue, *wrapperspb.StringValue, *wrapperspb.StringValue](
		wrapperspb.String("configured"),
		wrapperspb.String(""),
		nil,
	)

	resp, err := step.executeTyped(context.Background(), &pb.ExecuteStepRequest{
		TypedInput: mustPackTypedTestMessage(t, wrapperspb.String("run-input")),
	})
	if err != nil {
		t.Fatalf("executeTyped returned rpc error: %v", err)
	}
	if !strings.Contains(resp.Error, "typed step handler is nil") {
		t.Fatalf("expected nil handler error, got %q", resp.Error)
	}
}

func TestTypedStepProviderValidatesConfigBeforeCreation(t *testing.T) {
	provider := &countingTypedStepProvider{
		factory: NewTypedStepFactory(
			"test.typed",
			wrapperspb.String("configured"),
			wrapperspb.String(""),
			func(ctx context.Context, req TypedStepRequest[*wrapperspb.StringValue, *wrapperspb.StringValue]) (*TypedStepResult[*wrapperspb.StringValue], error) {
				return &TypedStepResult[*wrapperspb.StringValue]{Output: wrapperspb.String(req.Input.Value)}, nil
			},
		),
	}
	srv := newGRPCServer(provider)

	resp, err := srv.CreateStep(context.Background(), &pb.CreateStepRequest{
		Type:        "test.typed",
		Name:        "typed",
		TypedConfig: mustPackTypedTestMessage(t, wrapperspb.Int64(42)),
	})
	if err != nil {
		t.Fatalf("CreateStep returned rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected mismatched config error, got empty error")
	}
	if provider.created != 0 {
		t.Fatalf("expected no step creation after invalid config, got %d", provider.created)
	}
}

func TestMixedTypedAndLegacyStepProviderFallsBackByType(t *testing.T) {
	provider := &mixedStepProvider{
		typed: NewTypedStepFactory(
			"test.typed",
			wrapperspb.String("configured"),
			wrapperspb.String(""),
			func(ctx context.Context, req TypedStepRequest[*wrapperspb.StringValue, *wrapperspb.StringValue]) (*TypedStepResult[*wrapperspb.StringValue], error) {
				return &TypedStepResult[*wrapperspb.StringValue]{Output: wrapperspb.String(req.Input.Value)}, nil
			},
		),
	}
	srv := newGRPCServer(provider)

	types, err := srv.GetStepTypes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStepTypes returned rpc error: %v", err)
	}
	if len(types.Types) != 2 || types.Types[0] != "test.typed" || types.Types[1] != "test.legacy" {
		t.Fatalf("expected typed and legacy step types, got %#v", types.Types)
	}

	resp, err := srv.CreateStep(context.Background(), &pb.CreateStepRequest{
		Type: "test.legacy",
		Name: "legacy",
	})
	if err != nil {
		t.Fatalf("CreateStep returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected create error: %s", resp.Error)
	}
	if resp.HandleId == "" {
		t.Fatal("expected handle ID")
	}
	if provider.legacyCreated != 1 {
		t.Fatalf("expected legacy provider to create one step, got %d", provider.legacyCreated)
	}
}

func TestTypedModuleConfigAndMismatch(t *testing.T) {
	module := NewTypedModuleInstance(wrapperspb.String("default"), noopModule{})

	if err := module.setTypedConfig(mustPackTypedTestMessage(t, wrapperspb.String("configured"))); err != nil {
		t.Fatalf("set typed config: %v", err)
	}
	if got := module.TypedConfig().Value; got != "configured" {
		t.Fatalf("expected typed module config %q, got %q", "configured", got)
	}

	err := module.setTypedConfig(mustPackTypedTestMessage(t, wrapperspb.Int64(42)))
	if err == nil {
		t.Fatal("expected mismatched module config error, got nil")
	}
	if !strings.Contains(err.Error(), "typed config") {
		t.Fatalf("expected typed config error, got %q", err.Error())
	}
}

func TestTypedModuleProviderValidatesConfigBeforeCreation(t *testing.T) {
	provider := &countingTypedModuleProvider{
		factory: NewTypedModuleFactory(
			"test.module",
			wrapperspb.String("configured"),
			func(name string, config *wrapperspb.StringValue) (ModuleInstance, error) {
				return noopModule{}, nil
			},
		),
	}
	srv := newGRPCServer(provider)

	resp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type:        "test.module",
		Name:        "typed",
		TypedConfig: mustPackTypedTestMessage(t, wrapperspb.Int64(42)),
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected mismatched config error, got empty error")
	}
	if provider.created != 0 {
		t.Fatalf("expected no module creation after invalid config, got %d", provider.created)
	}
}

func TestTypedModuleProviderWiresMessageAwareModule(t *testing.T) {
	aware := &messageAwareNoopModule{}
	provider := &countingTypedModuleProvider{
		factory: NewTypedModuleFactory(
			"test.module",
			wrapperspb.String("configured"),
			func(name string, config *wrapperspb.StringValue) (ModuleInstance, error) {
				return aware, nil
			},
		),
	}
	srv := newGRPCServer(provider)
	srv.SetCallbackClient(pb.NewEngineCallbackServiceClient(nil))

	resp, err := srv.CreateModule(context.Background(), &pb.CreateModuleRequest{
		Type:        "test.module",
		Name:        "typed",
		TypedConfig: mustPackTypedTestMessage(t, wrapperspb.String("configured")),
	})
	if err != nil {
		t.Fatalf("CreateModule returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected create error: %s", resp.Error)
	}
	if !aware.publisherSet {
		t.Fatal("expected typed module to receive message publisher")
	}
	if !aware.subscriberSet {
		t.Fatal("expected typed module to receive message subscriber")
	}
}

func TestTypedModuleNilWrappedModuleReturnsError(t *testing.T) {
	module := NewTypedModuleInstance[*wrapperspb.StringValue](wrapperspb.String("default"), nil)

	if err := module.Init(); !strings.Contains(err.Error(), "typed module instance is nil") {
		t.Fatalf("expected nil module Init error, got %v", err)
	}
	if err := module.Start(context.Background()); !strings.Contains(err.Error(), "typed module instance is nil") {
		t.Fatalf("expected nil module Start error, got %v", err)
	}
	if err := module.Stop(context.Background()); !strings.Contains(err.Error(), "typed module instance is nil") {
		t.Fatalf("expected nil module Stop error, got %v", err)
	}
}

func createTypedTestStep(t *testing.T, srv *grpcServer, config *wrapperspb.StringValue) string {
	t.Helper()

	resp, err := srv.CreateStep(context.Background(), &pb.CreateStepRequest{
		Type:        "test.typed",
		Name:        "typed",
		TypedConfig: mustPackTypedTestMessage(t, config),
	})
	if err != nil {
		t.Fatalf("CreateStep returned rpc error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected create error: %s", resp.Error)
	}
	if resp.HandleId == "" {
		t.Fatal("expected handle ID")
	}
	return resp.HandleId
}

func mustPackTypedTestMessage(t *testing.T, msg proto.Message) *anypb.Any {
	t.Helper()

	anyMsg, err := anypb.New(msg)
	if err != nil {
		t.Fatalf("pack typed message: %v", err)
	}
	return anyMsg
}

func (p *countingTypedStepProvider) TypedStepTypes() []string {
	return p.factory.TypedStepTypes()
}

func (p *countingTypedStepProvider) CreateTypedStep(typeName, name string, config *anypb.Any) (StepInstance, error) {
	step, err := p.factory.CreateTypedStep(typeName, name, config)
	if err != nil {
		return nil, err
	}
	p.created++
	return step, nil
}

func (p *mixedStepProvider) StepTypes() []string {
	return []string{"test.legacy"}
}

func (p *mixedStepProvider) TypedStepTypes() []string {
	return p.typed.TypedStepTypes()
}

func (p *mixedStepProvider) CreateStep(typeName, name string, config map[string]any) (StepInstance, error) {
	if typeName != "test.legacy" {
		return nil, ErrTypedContractNotHandled
	}
	p.legacyCreated++
	return legacyStep{}, nil
}

func (p *mixedStepProvider) CreateTypedStep(typeName, name string, config *anypb.Any) (StepInstance, error) {
	return p.typed.CreateTypedStep(typeName, name, config)
}

func (p *countingTypedModuleProvider) TypedModuleTypes() []string {
	return p.factory.TypedModuleTypes()
}

func (p *countingTypedModuleProvider) CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error) {
	module, err := p.factory.CreateTypedModule(typeName, name, config)
	if err != nil {
		return nil, err
	}
	p.created++
	return module, nil
}

type legacyStep struct{}

func (legacyStep) Execute(context.Context, map[string]any, map[string]map[string]any, map[string]any, map[string]any, map[string]any) (*StepResult, error) {
	return &StepResult{}, nil
}

type noopModule struct{}

func (noopModule) Init() error {
	return nil
}

func (noopModule) Start(context.Context) error {
	return nil
}

func (noopModule) Stop(context.Context) error {
	return nil
}

type messageAwareNoopModule struct {
	noopModule
	publisherSet  bool
	subscriberSet bool
}

func (m *messageAwareNoopModule) SetMessagePublisher(MessagePublisher) {
	m.publisherSet = true
}

func (m *messageAwareNoopModule) SetMessageSubscriber(MessageSubscriber) {
	m.subscriberSet = true
}
