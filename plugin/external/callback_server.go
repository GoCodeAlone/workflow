package external

import (
	"context"
	"log"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TriggerFunc is called when a plugin fires a workflow trigger.
type TriggerFunc func(triggerType, action string, data map[string]any) error

// ServiceLookupFunc checks if a named service exists in the host.
type ServiceLookupFunc func(name string) bool

// CallbackServer implements the EngineCallbackService gRPC server.
// It runs on the host and is called by plugin processes.
type CallbackServer struct {
	pb.UnimplementedEngineCallbackServiceServer

	onTrigger     TriggerFunc
	serviceLookup ServiceLookupFunc
	logger        *log.Logger
}

// NewCallbackServer creates a new callback server.
func NewCallbackServer(onTrigger TriggerFunc, lookup ServiceLookupFunc, logger *log.Logger) *CallbackServer {
	if logger == nil {
		logger = log.Default()
	}
	return &CallbackServer{
		onTrigger:     onTrigger,
		serviceLookup: lookup,
		logger:        logger,
	}
}

func (s *CallbackServer) TriggerWorkflow(_ context.Context, req *pb.TriggerWorkflowRequest) (*pb.ErrorResponse, error) {
	if s.onTrigger == nil {
		return &pb.ErrorResponse{Error: "trigger handler not configured"}, nil
	}
	err := s.onTrigger(req.TriggerType, req.Action, structToMap(req.Data))
	if err != nil {
		return &pb.ErrorResponse{Error: err.Error()}, nil //nolint:nilerr // gRPC error is nil; app error in response field
	}
	return &pb.ErrorResponse{}, nil
}

func (s *CallbackServer) GetService(_ context.Context, req *pb.GetServiceRequest) (*pb.GetServiceResponse, error) {
	found := false
	if s.serviceLookup != nil {
		found = s.serviceLookup(req.Name)
	}
	return &pb.GetServiceResponse{Found: found}, nil
}

func (s *CallbackServer) Log(_ context.Context, req *pb.LogRequest) (*emptypb.Empty, error) {
	fields := structToMap(req.Fields)
	s.logger.Printf("[plugin][%s] %s %v", req.Level, req.Message, fields)
	return &emptypb.Empty{}, nil
}
