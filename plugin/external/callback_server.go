package external

import (
	"context"
	"fmt"
	"log"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TriggerFunc is called when a plugin fires a workflow trigger.
type TriggerFunc func(triggerType, action string, data map[string]any) error

// ServiceLookupFunc checks if a named service exists in the host.
type ServiceLookupFunc func(name string) bool

// MessagePublishFunc publishes a message to a named broker topic on behalf of a plugin.
type MessagePublishFunc func(brokerName, topic string, payload []byte, metadata map[string]string) (messageID string, err error)

// MessageSubscribeFunc subscribes to a topic on a named broker on behalf of a plugin.
// It returns a cancel function to remove the subscription and any error.
type MessageSubscribeFunc func(brokerName, topic string, handler func([]byte, map[string]string) error) (cancel func(), err error)

// CallbackServer implements the EngineCallbackService gRPC server.
// It runs on the host and is called by plugin processes.
type CallbackServer struct {
	pb.UnimplementedEngineCallbackServiceServer

	onTrigger        TriggerFunc
	serviceLookup    ServiceLookupFunc
	onPublishMessage MessagePublishFunc
	onSubscribe      MessageSubscribeFunc
	logger           *log.Logger

	subMu       sync.Mutex
	cancelFuncs map[string]func() // key: handleID + "\x00" + topic
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
		cancelFuncs:   make(map[string]func()),
	}
}

// SetMessagePublishFunc configures the callback for message publishing.
func (s *CallbackServer) SetMessagePublishFunc(fn MessagePublishFunc) {
	s.onPublishMessage = fn
}

// SetMessageSubscribeFunc configures the callback for message subscriptions.
func (s *CallbackServer) SetMessageSubscribeFunc(fn MessageSubscribeFunc) {
	s.onSubscribe = fn
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

func (s *CallbackServer) PublishMessage(_ context.Context, req *pb.PublishMessageRequest) (*pb.PublishMessageResponse, error) {
	if s.onPublishMessage == nil {
		return &pb.PublishMessageResponse{Error: "message publish handler not configured"}, nil
	}
	messageID, err := s.onPublishMessage("", req.Topic, req.Payload, req.Metadata)
	if err != nil {
		return &pb.PublishMessageResponse{Error: err.Error()}, nil //nolint:nilerr // gRPC error is nil; app error in response field
	}
	return &pb.PublishMessageResponse{MessageId: messageID}, nil
}

func (s *CallbackServer) Subscribe(_ context.Context, req *pb.SubscribeRequest) (*pb.ErrorResponse, error) {
	if s.onSubscribe == nil {
		return &pb.ErrorResponse{Error: "message subscribe handler not configured"}, nil
	}
	key := subscriptionKey(req.HandleId, req.Topic)

	// Check for duplicate subscription.
	s.subMu.Lock()
	if _, exists := s.cancelFuncs[key]; exists {
		s.subMu.Unlock()
		return &pb.ErrorResponse{Error: fmt.Sprintf("already subscribed to topic %s for handle %s", req.Topic, req.HandleId)}, nil
	}
	s.subMu.Unlock()

	cancel, err := s.onSubscribe(req.BrokerName, req.Topic, func(payload []byte, metadata map[string]string) error {
		// This handler is called by the host broker — delivery to the plugin
		// is handled by the host via DeliverMessage RPC (see adapter/remote_module).
		// For now we log. Full delivery is wired at a higher layer.
		s.logger.Printf("[plugin-sub][%s] received message on topic %s (%d bytes)", req.HandleId, req.Topic, len(payload))
		return nil
	})
	if err != nil {
		return &pb.ErrorResponse{Error: err.Error()}, nil //nolint:nilerr // gRPC error is nil; app error in response field
	}

	s.subMu.Lock()
	s.cancelFuncs[key] = cancel
	s.subMu.Unlock()

	return &pb.ErrorResponse{}, nil
}

func (s *CallbackServer) Unsubscribe(_ context.Context, req *pb.UnsubscribeRequest) (*pb.ErrorResponse, error) {
	key := subscriptionKey(req.HandleId, req.Topic)

	s.subMu.Lock()
	cancel, exists := s.cancelFuncs[key]
	if exists {
		delete(s.cancelFuncs, key)
	}
	s.subMu.Unlock()

	if !exists {
		return &pb.ErrorResponse{Error: fmt.Sprintf("no subscription found for topic %s handle %s", req.Topic, req.HandleId)}, nil
	}

	if cancel != nil {
		cancel()
	}
	return &pb.ErrorResponse{}, nil
}

// subscriptionKey returns the map key for a handle+topic subscription entry.
func subscriptionKey(handleID, topic string) string {
	return handleID + "\x00" + topic
}
