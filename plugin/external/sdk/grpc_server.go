package sdk

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// grpcServer implements pb.PluginServiceServer by delegating to a PluginProvider.
type grpcServer struct {
	pb.UnimplementedPluginServiceServer

	provider PluginProvider

	mu      sync.RWMutex
	modules map[string]ModuleInstance
	steps   map[string]StepInstance
}

// newGRPCServer creates a gRPC server implementation wrapping the given provider.
func newGRPCServer(provider PluginProvider) *grpcServer {
	return &grpcServer{
		provider: provider,
		modules:  make(map[string]ModuleInstance),
		steps:    make(map[string]StepInstance),
	}
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
	return &pb.TypeList{}, nil
}

func (s *grpcServer) GetModuleSchemas(_ context.Context, _ *emptypb.Empty) (*pb.ModuleSchemaList, error) {
	return &pb.ModuleSchemaList{}, nil
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

// --- Service RPCs ---

func (s *grpcServer) InvokeService(_ context.Context, _ *pb.InvokeServiceRequest) (*pb.InvokeServiceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "InvokeService not implemented")
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
