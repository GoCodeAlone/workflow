package sdk

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// ErrTypedContractNotHandled lets mixed typed/legacy providers decline a type
// so the server can fall back to the legacy provider path.
var ErrTypedContractNotHandled = errors.New("typed contract not handled")

// TypedStepRequest is passed to typed step handlers after protobuf Any payloads
// have been validated and unpacked.
type TypedStepRequest[C proto.Message, I proto.Message] struct {
	Config C
	Input  I

	TriggerData map[string]any
	StepOutputs map[string]map[string]any
	Current     map[string]any
	Metadata    map[string]any
}

// TypedStepResult is returned from typed step handlers and packed into Any.
type TypedStepResult[O proto.Message] struct {
	Output       O
	StopPipeline bool
}

// TypedStepHandler executes a typed step with protobuf config and input.
type TypedStepHandler[C proto.Message, I proto.Message, O proto.Message] func(context.Context, TypedStepRequest[C, I]) (*TypedStepResult[O], error)

// TypedStepProvider creates protobuf-typed step instances after validating
// typed_config. Implement this instead of StepProvider for strict typed steps.
type TypedStepProvider interface {
	TypedStepTypes() []string
	CreateTypedStep(typeName, name string, config *anypb.Any) (StepInstance, error)
}

// TypedStepFactory is a single-step TypedStepProvider implementation.
type TypedStepFactory[C proto.Message, I proto.Message, O proto.Message] struct {
	typeName        string
	configPrototype C
	inputPrototype  I
	handler         TypedStepHandler[C, I, O]
}

// NewTypedStepFactory returns a provider for one typed step type. The factory
// validates typed_config before returning an instance, so failed creation does
// not leak a partially-created step into plugin-local state.
func NewTypedStepFactory[C proto.Message, I proto.Message, O proto.Message](
	typeName string,
	configPrototype C,
	inputPrototype I,
	handler TypedStepHandler[C, I, O],
) *TypedStepFactory[C, I, O] {
	return &TypedStepFactory[C, I, O]{
		typeName:        typeName,
		configPrototype: configPrototype,
		inputPrototype:  inputPrototype,
		handler:         handler,
	}
}

func (f *TypedStepFactory[C, I, O]) TypedStepTypes() []string {
	return []string{f.typeName}
}

func (f *TypedStepFactory[C, I, O]) CreateTypedStep(typeName, _ string, config *anypb.Any) (StepInstance, error) {
	if typeName != f.typeName {
		return nil, fmt.Errorf("%w: step type %q", ErrTypedContractNotHandled, typeName)
	}
	if f.handler == nil {
		return nil, fmt.Errorf("typed step handler is nil")
	}
	step := NewTypedStepInstance(f.configPrototype, f.inputPrototype, f.handler)
	if err := step.setTypedConfig(config); err != nil {
		return nil, err
	}
	return step, nil
}

// TypedStepInstance adapts a protobuf-typed step implementation to the legacy
// StepInstance interface and the typed gRPC execution path.
type TypedStepInstance[C proto.Message, I proto.Message, O proto.Message] struct {
	configPrototype C
	inputPrototype  I
	config          C
	handler         TypedStepHandler[C, I, O]
	initErr         error
}

// NewTypedStepInstance returns a StepInstance that validates typed Any payloads
// before invoking handler. configPrototype and inputPrototype define the
// expected protobuf message types.
func NewTypedStepInstance[C proto.Message, I proto.Message, O proto.Message](
	configPrototype C,
	inputPrototype I,
	handler TypedStepHandler[C, I, O],
) *TypedStepInstance[C, I, O] {
	step := &TypedStepInstance[C, I, O]{
		configPrototype: configPrototype,
		inputPrototype:  inputPrototype,
		config:          cloneMessage(configPrototype),
		handler:         handler,
	}
	if handler == nil {
		step.initErr = fmt.Errorf("typed step handler is nil")
	}
	return step
}

// Execute keeps TypedStepInstance assignable to StepInstance. Typed plugins
// should normally execute through ExecuteStep with typed_input; legacy map-only
// execution cannot safely populate arbitrary protobuf messages.
func (s *TypedStepInstance[C, I, O]) Execute(context.Context, map[string]any, map[string]map[string]any, map[string]any, map[string]any, map[string]any) (*StepResult, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return nil, fmt.Errorf("typed step requires typed_input payload")
}

func (s *TypedStepInstance[C, I, O]) setTypedConfig(config *anypb.Any) error {
	if s.initErr != nil {
		return s.initErr
	}
	if config == nil {
		return nil
	}
	decoded, err := unpackAny[C](config, s.configPrototype, "typed config")
	if err != nil {
		return err
	}
	s.config = decoded
	return nil
}

func (s *TypedStepInstance[C, I, O]) executeTyped(ctx context.Context, req *pb.ExecuteStepRequest) (*pb.ExecuteStepResponse, error) {
	if s.initErr != nil {
		return &pb.ExecuteStepResponse{Error: s.initErr.Error()}, nil //nolint:nilerr // app error in response field
	}
	config := cloneMessage(s.config)
	if req.TypedConfig != nil {
		decoded, err := unpackAny[C](req.TypedConfig, s.configPrototype, "typed config")
		if err != nil {
			return &pb.ExecuteStepResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
		}
		config = decoded
	}

	input, err := unpackAny[I](req.TypedInput, s.inputPrototype, "typed input")
	if err != nil {
		return &pb.ExecuteStepResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}

	result, err := s.handler(ctx, TypedStepRequest[C, I]{
		Config:      config,
		Input:       input,
		TriggerData: structToMap(req.TriggerData),
		StepOutputs: typedStepOutputsToMap(req.StepOutputs),
		Current:     structToMap(req.Current),
		Metadata:    structToMap(req.Metadata),
	})
	if err != nil {
		return &pb.ExecuteStepResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}
	if result == nil {
		return &pb.ExecuteStepResponse{}, nil
	}

	var typedOutput *anypb.Any
	if !isNilProto(result.Output) {
		typedOutput, err = anypb.New(result.Output)
		if err != nil {
			return &pb.ExecuteStepResponse{Error: fmt.Sprintf("pack typed output: %v", err)}, nil
		}
	}
	return &pb.ExecuteStepResponse{
		TypedOutput:  typedOutput,
		StopPipeline: result.StopPipeline,
	}, nil
}

type typedStepAdapter interface {
	StepInstance
	setTypedConfig(*anypb.Any) error
	executeTyped(context.Context, *pb.ExecuteStepRequest) (*pb.ExecuteStepResponse, error)
}

// TypedModuleProvider creates protobuf-typed module instances after validating
// typed_config. Implement this instead of ModuleProvider for strict typed modules.
type TypedModuleProvider interface {
	TypedModuleTypes() []string
	CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error)
}

// TypedModuleCreator constructs a module after its typed config has been
// unpacked and validated.
type TypedModuleCreator[C proto.Message] func(name string, config C) (ModuleInstance, error)

// TypedModuleFactory is a single-module TypedModuleProvider implementation.
type TypedModuleFactory[C proto.Message] struct {
	typeName        string
	configPrototype C
	create          TypedModuleCreator[C]
}

// NewTypedModuleFactory returns a provider for one typed module type. The
// factory validates typed_config before invoking create.
func NewTypedModuleFactory[C proto.Message](
	typeName string,
	configPrototype C,
	create TypedModuleCreator[C],
) *TypedModuleFactory[C] {
	return &TypedModuleFactory[C]{
		typeName:        typeName,
		configPrototype: configPrototype,
		create:          create,
	}
}

func (f *TypedModuleFactory[C]) TypedModuleTypes() []string {
	return []string{f.typeName}
}

func (f *TypedModuleFactory[C]) CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error) {
	if typeName != f.typeName {
		return nil, fmt.Errorf("%w: module type %q", ErrTypedContractNotHandled, typeName)
	}
	if f.create == nil {
		return nil, fmt.Errorf("typed module creator is nil")
	}
	typedConfig := cloneMessage(f.configPrototype)
	if config != nil {
		decoded, err := unpackAny[C](config, f.configPrototype, "typed config")
		if err != nil {
			return nil, err
		}
		typedConfig = decoded
	}
	module, err := f.create(name, typedConfig)
	if err != nil {
		return nil, err
	}
	wrapped := NewTypedModuleInstance(f.configPrototype, module)
	wrapped.config = typedConfig
	return wrapped, nil
}

// TypedModuleInstance adapts protobuf-typed module config while preserving the
// normal ModuleInstance lifecycle interface.
type TypedModuleInstance[C proto.Message] struct {
	ModuleInstance
	configPrototype C
	config          C
}

// NewTypedModuleInstance returns a ModuleInstance wrapper that can accept typed
// module config from CreateModuleRequest. The wrapped instance still owns the
// module lifecycle.
func NewTypedModuleInstance[C proto.Message](configPrototype C, module ModuleInstance) *TypedModuleInstance[C] {
	return &TypedModuleInstance[C]{
		ModuleInstance:  module,
		configPrototype: configPrototype,
		config:          cloneMessage(configPrototype),
	}
}

func (m *TypedModuleInstance[C]) Init() error {
	if m.ModuleInstance == nil {
		return fmt.Errorf("typed module instance is nil")
	}
	return m.ModuleInstance.Init()
}

func (m *TypedModuleInstance[C]) Start(ctx context.Context) error {
	if m.ModuleInstance == nil {
		return fmt.Errorf("typed module instance is nil")
	}
	return m.ModuleInstance.Start(ctx)
}

func (m *TypedModuleInstance[C]) Stop(ctx context.Context) error {
	if m.ModuleInstance == nil {
		return fmt.Errorf("typed module instance is nil")
	}
	return m.ModuleInstance.Stop(ctx)
}

func (m *TypedModuleInstance[C]) SetMessagePublisher(pub MessagePublisher) {
	if mam, ok := m.ModuleInstance.(MessageAwareModule); ok {
		mam.SetMessagePublisher(pub)
	}
}

func (m *TypedModuleInstance[C]) SetMessageSubscriber(sub MessageSubscriber) {
	if mam, ok := m.ModuleInstance.(MessageAwareModule); ok {
		mam.SetMessageSubscriber(sub)
	}
}

func (m *TypedModuleInstance[C]) InvokeTypedMethod(method string, input *anypb.Any) (*anypb.Any, error) {
	invoker, ok := m.ModuleInstance.(TypedServiceInvoker)
	if !ok {
		return nil, fmt.Errorf("typed module instance does not implement TypedServiceInvoker")
	}
	return invoker.InvokeTypedMethod(method, input)
}

func (m *TypedModuleInstance[C]) InvokeMethod(method string, args map[string]any) (map[string]any, error) {
	invoker, ok := m.ModuleInstance.(ServiceInvoker)
	if !ok {
		return nil, fmt.Errorf("typed module instance does not implement ServiceInvoker")
	}
	return invoker.InvokeMethod(method, args)
}

// TypedConfig returns the unpacked module config most recently supplied by the
// host.
func (m *TypedModuleInstance[C]) TypedConfig() C {
	return cloneMessage(m.config)
}

func (m *TypedModuleInstance[C]) setTypedConfig(config *anypb.Any) error {
	if config == nil {
		return nil
	}
	decoded, err := unpackAny[C](config, m.configPrototype, "typed config")
	if err != nil {
		return err
	}
	m.config = decoded
	return nil
}

type typedModuleAdapter interface {
	ModuleInstance
	setTypedConfig(*anypb.Any) error
}

func unpackAny[T proto.Message](payload *anypb.Any, prototype T, field string) (T, error) {
	if payload == nil {
		var zero T
		return zero, fmt.Errorf("%s is required; expected %s", field, messageFullName(prototype))
	}
	expected := messageFullName(prototype)
	if payload.MessageName() != expected {
		var zero T
		return zero, fmt.Errorf("%s type mismatch: expected %s, got %s", field, expected, payload.MessageName())
	}
	msg := cloneMessage(prototype)
	if err := payload.UnmarshalTo(msg); err != nil {
		var zero T
		return zero, fmt.Errorf("unpack %s as %s: %w", field, expected, err)
	}
	return msg, nil
}

func cloneMessage[T proto.Message](msg T) T {
	if isNilProto(msg) {
		var zero T
		return zero
	}
	return proto.Clone(msg).(T)
}

func messageFullName(msg proto.Message) protoreflect.FullName {
	if isNilProto(msg) {
		return "<nil>"
	}
	return msg.ProtoReflect().Descriptor().FullName()
}

func isNilProto(msg proto.Message) bool {
	if msg == nil {
		return true
	}
	v := reflect.ValueOf(msg)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func typedStepOutputsToMap(outputs map[string]*structpb.Struct) map[string]map[string]any {
	if outputs == nil {
		return nil
	}
	stepOutputs := make(map[string]map[string]any, len(outputs))
	for k, v := range outputs {
		stepOutputs[k] = structToMap(v)
	}
	return stepOutputs
}
