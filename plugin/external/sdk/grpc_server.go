package sdk

import (
	"context"
	"fmt"
	"os"
	"sync"

	goplugin "github.com/GoCodeAlone/go-plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// grpcServer implements pb.PluginServiceServer by delegating to a PluginProvider.
type grpcServer struct {
	pb.UnimplementedPluginServiceServer

	provider PluginProvider

	mu              sync.RWMutex
	modules         map[string]ModuleInstance
	steps           map[string]StepInstance
	messageHandlers map[string]func(payload []byte, metadata map[string]string) error

	callbackClient pb.EngineCallbackServiceClient
	broker         *goplugin.GRPCBroker
}

// newGRPCServer creates a gRPC server implementation wrapping the given provider.
func newGRPCServer(provider PluginProvider) *grpcServer {
	return &grpcServer{
		provider:        provider,
		modules:         make(map[string]ModuleInstance),
		steps:           make(map[string]StepInstance),
		messageHandlers: make(map[string]func(payload []byte, metadata map[string]string) error),
	}
}

// setBroker stores the go-plugin broker for later use in dialing the host callback.
func (s *grpcServer) setBroker(broker *goplugin.GRPCBroker) {
	s.mu.Lock()
	s.broker = broker
	s.mu.Unlock()
}

// SetCallbackClient stores the host callback gRPC client so that modules can
// publish messages and manage subscriptions via the host.
func (s *grpcServer) SetCallbackClient(client pb.EngineCallbackServiceClient) {
	s.mu.Lock()
	s.callbackClient = client
	s.mu.Unlock()
}

// --- Message pub/sub helpers ---

// grpcPublisher implements MessagePublisher by calling the host callback.
type grpcPublisher struct {
	handleID string
	client   pb.EngineCallbackServiceClient
}

func (p *grpcPublisher) Publish(topic string, payload []byte, metadata map[string]string) (string, error) {
	resp, err := p.client.PublishMessage(context.Background(), &pb.PublishMessageRequest{
		HandleId: p.handleID,
		Topic:    topic,
		Payload:  payload,
		Metadata: metadata,
	})
	if err != nil {
		return "", fmt.Errorf("publish message: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("publish message: %s", resp.Error)
	}
	return resp.MessageId, nil
}

// grpcSubscriber implements MessageSubscriber by calling the host callback and
// registering a local handler for DeliverMessage calls from the host.
type grpcSubscriber struct {
	handleID string
	client   pb.EngineCallbackServiceClient
	server   *grpcServer
}

func (s *grpcSubscriber) Subscribe(topic string, handler func(payload []byte, metadata map[string]string) error) error {
	resp, err := s.client.Subscribe(context.Background(), &pb.SubscribeRequest{
		HandleId: s.handleID,
		Topic:    topic,
	})
	if err != nil {
		return fmt.Errorf("subscribe to %s: %w", topic, err)
	}
	if resp.Error != "" {
		return fmt.Errorf("subscribe to %s: %s", topic, resp.Error)
	}
	key := messageHandlerKey(s.handleID, topic)
	s.server.mu.Lock()
	s.server.messageHandlers[key] = handler
	s.server.mu.Unlock()
	return nil
}

func (s *grpcSubscriber) Unsubscribe(topic string) error {
	resp, err := s.client.Unsubscribe(context.Background(), &pb.UnsubscribeRequest{
		HandleId: s.handleID,
		Topic:    topic,
	})
	if err != nil {
		return fmt.Errorf("unsubscribe from %s: %w", topic, err)
	}
	if resp.Error != "" {
		return fmt.Errorf("unsubscribe from %s: %s", topic, resp.Error)
	}
	key := messageHandlerKey(s.handleID, topic)
	s.server.mu.Lock()
	delete(s.server.messageHandlers, key)
	s.server.mu.Unlock()
	return nil
}

// --- Metadata RPCs ---

func (s *grpcServer) GetManifest(_ context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
	m := s.provider.Manifest()
	return &pb.Manifest{
		Name:        m.Name,
		Version:     m.Version,
		Author:      m.Author,
		Description: m.Description,
	}, nil
}

func (s *grpcServer) GetModuleTypes(_ context.Context, _ *emptypb.Empty) (*pb.TypeList, error) {
	if mp, ok := s.provider.(ModuleProvider); ok {
		return &pb.TypeList{Types: mp.ModuleTypes()}, nil
	}
	return &pb.TypeList{}, nil
}

func (s *grpcServer) GetStepTypes(_ context.Context, _ *emptypb.Empty) (*pb.TypeList, error) {
	if sp, ok := s.provider.(StepProvider); ok {
		return &pb.TypeList{Types: sp.StepTypes()}, nil
	}
	return &pb.TypeList{}, nil
}

func (s *grpcServer) GetTriggerTypes(_ context.Context, _ *emptypb.Empty) (*pb.TypeList, error) {
	if tp, ok := s.provider.(TriggerProvider); ok {
		return &pb.TypeList{Types: tp.TriggerTypes()}, nil
	}
	return &pb.TypeList{}, nil
}

func (s *grpcServer) GetModuleSchemas(_ context.Context, _ *emptypb.Empty) (*pb.ModuleSchemaList, error) {
	sp, ok := s.provider.(SchemaProvider)
	if !ok {
		return &pb.ModuleSchemaList{}, nil
	}
	schemas := sp.ModuleSchemas()
	pbSchemas := make([]*pb.ModuleSchema, 0, len(schemas))
	for i := range schemas {
		sd := &schemas[i]
		pbSchema := &pb.ModuleSchema{
			Type:        sd.Type,
			Label:       sd.Label,
			Category:    sd.Category,
			Description: sd.Description,
		}
		for _, inp := range sd.Inputs {
			pbSchema.Inputs = append(pbSchema.Inputs, &pb.ServiceIODef{
				Name:        inp.Name,
				Type:        inp.Type,
				Description: inp.Description,
			})
		}
		for _, out := range sd.Outputs {
			pbSchema.Outputs = append(pbSchema.Outputs, &pb.ServiceIODef{
				Name:        out.Name,
				Type:        out.Type,
				Description: out.Description,
			})
		}
		for _, cf := range sd.ConfigFields {
			pbSchema.ConfigFields = append(pbSchema.ConfigFields, &pb.ConfigFieldDef{
				Name:         cf.Name,
				Type:         cf.Type,
				Description:  cf.Description,
				DefaultValue: cf.DefaultValue,
				Required:     cf.Required,
				Options:      cf.Options,
			})
		}
		pbSchemas = append(pbSchemas, pbSchema)
	}
	return &pb.ModuleSchemaList{Schemas: pbSchemas}, nil
}

// --- Module lifecycle RPCs ---

func (s *grpcServer) CreateModule(_ context.Context, req *pb.CreateModuleRequest) (*pb.HandleResponse, error) {
	mp, ok := s.provider.(ModuleProvider)
	if !ok {
		return &pb.HandleResponse{Error: "plugin does not provide modules"}, nil
	}

	inst, err := mp.CreateModule(req.Type, req.Name, structToMap(req.Config))
	if err != nil {
		return &pb.HandleResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}

	handle := uuid.New().String()

	// Wire up message capabilities if the module supports them and we have a callback client.
	if mam, ok := inst.(MessageAwareModule); ok {
		s.mu.RLock()
		cb := s.callbackClient
		s.mu.RUnlock()
		if cb != nil {
			mam.SetMessagePublisher(&grpcPublisher{handleID: handle, client: cb})
			mam.SetMessageSubscriber(&grpcSubscriber{handleID: handle, client: cb, server: s})
		}
	}

	s.mu.Lock()
	s.modules[handle] = inst
	s.mu.Unlock()

	return &pb.HandleResponse{HandleId: handle}, nil
}

func (s *grpcServer) InitModule(_ context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	s.mu.RLock()
	inst, ok := s.modules[req.HandleId]
	s.mu.RUnlock()
	if !ok {
		return &pb.ErrorResponse{Error: fmt.Sprintf("unknown module handle: %s", req.HandleId)}, nil
	}
	if err := inst.Init(); err != nil {
		return &pb.ErrorResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}
	return &pb.ErrorResponse{}, nil
}

func (s *grpcServer) StartModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	s.mu.RLock()
	inst, ok := s.modules[req.HandleId]
	s.mu.RUnlock()
	if !ok {
		return &pb.ErrorResponse{Error: fmt.Sprintf("unknown module handle: %s", req.HandleId)}, nil
	}
	if err := inst.Start(ctx); err != nil {
		return &pb.ErrorResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}
	return &pb.ErrorResponse{}, nil
}

func (s *grpcServer) StopModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	s.mu.RLock()
	inst, ok := s.modules[req.HandleId]
	s.mu.RUnlock()
	if !ok {
		return &pb.ErrorResponse{Error: fmt.Sprintf("unknown module handle: %s", req.HandleId)}, nil
	}
	if err := inst.Stop(ctx); err != nil {
		return &pb.ErrorResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}
	return &pb.ErrorResponse{}, nil
}

func (s *grpcServer) DestroyModule(_ context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	s.mu.Lock()
	_, ok := s.modules[req.HandleId]
	if ok {
		delete(s.modules, req.HandleId)
	}
	s.mu.Unlock()
	if !ok {
		return &pb.ErrorResponse{Error: fmt.Sprintf("unknown module handle: %s", req.HandleId)}, nil
	}
	return &pb.ErrorResponse{}, nil
}

// --- Step lifecycle RPCs ---

func (s *grpcServer) CreateStep(_ context.Context, req *pb.CreateStepRequest) (*pb.HandleResponse, error) {
	sp, ok := s.provider.(StepProvider)
	if !ok {
		return &pb.HandleResponse{Error: "plugin does not provide steps"}, nil
	}

	inst, err := sp.CreateStep(req.Type, req.Name, structToMap(req.Config))
	if err != nil {
		return &pb.HandleResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}

	handle := uuid.New().String()
	s.mu.Lock()
	s.steps[handle] = inst
	s.mu.Unlock()

	return &pb.HandleResponse{HandleId: handle}, nil
}

func (s *grpcServer) ExecuteStep(ctx context.Context, req *pb.ExecuteStepRequest) (*pb.ExecuteStepResponse, error) {
	s.mu.RLock()
	inst, ok := s.steps[req.HandleId]
	s.mu.RUnlock()
	if !ok {
		return &pb.ExecuteStepResponse{Error: fmt.Sprintf("unknown step handle: %s", req.HandleId)}, nil
	}

	// Convert proto step_outputs map to Go map
	stepOutputs := make(map[string]map[string]any, len(req.StepOutputs))
	for k, v := range req.StepOutputs {
		stepOutputs[k] = structToMap(v)
	}

	result, err := inst.Execute(ctx, structToMap(req.TriggerData), stepOutputs, structToMap(req.Current), structToMap(req.Metadata))
	if err != nil {
		return &pb.ExecuteStepResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}

	return &pb.ExecuteStepResponse{
		Output:       mapToStruct(result.Output),
		StopPipeline: result.StopPipeline,
	}, nil
}

func (s *grpcServer) DestroyStep(_ context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	s.mu.Lock()
	_, ok := s.steps[req.HandleId]
	if ok {
		delete(s.steps, req.HandleId)
	}
	s.mu.Unlock()
	if !ok {
		return &pb.ErrorResponse{Error: fmt.Sprintf("unknown step handle: %s", req.HandleId)}, nil
	}
	return &pb.ErrorResponse{}, nil
}

// --- Config fragment RPC ---

func (s *grpcServer) GetConfigFragment(_ context.Context, _ *emptypb.Empty) (*pb.ConfigFragmentResponse, error) {
	cp, ok := s.provider.(ConfigProvider)
	if !ok {
		return &pb.ConfigFragmentResponse{}, nil
	}
	data, err := cp.ConfigFragment()
	if err != nil {
		return nil, err
	}
	dir, _ := os.Getwd()
	return &pb.ConfigFragmentResponse{
		YamlConfig: data,
		PluginDir:  dir,
	}, nil
}

// --- Service RPCs ---

func (s *grpcServer) InvokeService(_ context.Context, _ *pb.InvokeServiceRequest) (*pb.InvokeServiceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "InvokeService not implemented")
}

// --- Message delivery RPC ---

// DeliverMessage routes an incoming message from the host to the correct module handler.
func (s *grpcServer) DeliverMessage(_ context.Context, req *pb.DeliverMessageRequest) (*pb.DeliverMessageResponse, error) {
	key := messageHandlerKey(req.HandleId, req.Topic)
	s.mu.RLock()
	handler, ok := s.messageHandlers[key]
	s.mu.RUnlock()

	if !ok {
		return &pb.DeliverMessageResponse{Error: fmt.Sprintf("no message handler for handle %s topic %s", req.HandleId, req.Topic)}, nil
	}

	if err := handler(req.Payload, req.Metadata); err != nil {
		return &pb.DeliverMessageResponse{Error: err.Error()}, nil //nolint:nilerr // app error in response field
	}
	return &pb.DeliverMessageResponse{Acknowledged: true}, nil
}

// messageHandlerKey returns the map key for a handle+topic pair.
func messageHandlerKey(handleID, topic string) string {
	return handleID + "\x00" + topic
}

// --- Helpers ---

func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func mapToStruct(m map[string]any) *structpb.Struct {
	if m == nil {
		return nil
	}
	s, _ := structpb.NewStruct(m)
	return s
}
